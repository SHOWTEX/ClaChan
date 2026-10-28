package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	sessionCookieName = "clachan_session"
	sessionTTL        = 30 * 24 * time.Hour

	passwordIterations = 210_000
	minPasswordLength  = 8
	maxPasswordLength  = 200
	maxProfileNameLen  = 64
)

var (
	errHashBusy = errors.New("password hashing is busy")
	dummySalt   = []byte("clachan-dummy-salt")
)

type profileContextKey struct{}

func currentProfile(r *http.Request) *Profile {
	p, _ := r.Context().Value(profileContextKey{}).(*Profile)
	return p
}

/* ------------------------------------------------------------------ passwords */

func hashPassword(password string) (hash, salt string, iters int, err error) {
	rawSalt := make([]byte, 16)
	if _, err := rand.Read(rawSalt); err != nil {
		return "", "", 0, err
	}
	key, err := pbkdf2.Key(sha256.New, password, rawSalt, passwordIterations, 32)
	if err != nil {
		return "", "", 0, err
	}
	return base64.RawStdEncoding.EncodeToString(key),
		base64.RawStdEncoding.EncodeToString(rawSalt),
		passwordIterations,
		nil
}

func verifyPassword(password, encodedHash, encodedSalt string, iters int) bool {
	hash, err := base64.RawStdEncoding.DecodeString(encodedHash)
	if err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(encodedSalt)
	if err != nil || iters <= 0 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iters, len(hash))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(key, hash) == 1
}

// burnPasswordCheck costs the same as a real verification, so a missing
// profile is not distinguishable by timing.
func (s *Server) burnPasswordCheck(password string) {
	select {
	case s.hashSem <- struct{}{}:
		defer func() { <-s.hashSem }()
		_, _ = pbkdf2.Key(sha256.New, password, dummySalt, passwordIterations, 32)
	default:
		// Saturated by real logins; the rate limiter is the real guard.
	}
}

func (s *Server) derivePassword(password string) (hash, salt string, iters int, err error) {
	select {
	case s.hashSem <- struct{}{}:
		defer func() { <-s.hashSem }()
	default:
		return "", "", 0, errHashBusy
	}
	return hashPassword(password)
}

/* ------------------------------------------------------------------- sessions */

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newSessionToken() (token, hashed string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, profileID string) error {
	token, hashed, err := newSessionToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionTTL)
	if err := s.store.createSession(r.Context(), profileID, hashed, expires.UnixMilli()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(sessionTTL / time.Second),
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Server) authenticate(r *http.Request) (*Profile, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, ErrNotFound
	}
	return s.store.sessionProfile(r.Context(), hashToken(cookie.Value), nowMillis())
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
			return
		}
		ctx := context.WithValue(r.Context(), profileContextKey{}, profile)
		next(w, r.WithContext(ctx))
	}
}

// requireOwner is a handler-level check for profile administration.
func requireOwner(w http.ResponseWriter, profile *Profile) bool {
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return false
	}
	if !profile.IsOwner {
		writeError(w, http.StatusForbidden, "forbidden", "Only the owner can manage profiles.", 0)
		return false
	}
	return true
}

/* ---------------------------------------------------------------------- http */

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.listProfiles(r.Context())
	if err != nil {
		s.log.Error("list profiles", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load profiles.", 0)
		return
	}
	current, _ := s.authenticate(r)

	// The profile picker only needs identity. Keep per-profile settings out of
	// the public list; the signed-in `current` profile still carries them.
	for i := range profiles {
		profiles[i].DefaultModel = ""
		profiles[i].IgnoredProviders = ""
		profiles[i].PersonaName = ""
		profiles[i].PersonaPrompt = ""
		profiles[i].UserName = ""
		profiles[i].UserPersona = ""
	}

	writeJSON(w, http.StatusOK, struct {
		SetupRequired bool      `json:"setupRequired"`
		Profiles      []Profile `json:"profiles"`
		Current       *Profile  `json:"current"`
	}{
		SetupRequired: len(profiles) == 0,
		Profiles:      profiles,
		Current:       current,
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	name := strings.TrimSpace(req.Name)
	if err := validateProfileName(name); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if !s.loginAllowed(w, r) {
		return
	}

	count, err := s.store.countProfiles(r.Context())
	if err != nil {
		s.log.Error("count profiles", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not check setup state.", 0)
		return
	}
	if count > 0 {
		writeError(w, http.StatusForbidden, "setup_complete", "Setup is already complete. Sign in instead.", 0)
		return
	}

	hash, salt, iters, err := s.derivePassword(req.Password)
	if errors.Is(err, errHashBusy) {
		writeError(w, http.StatusTooManyRequests, "busy", "The server is busy. Try again in a moment.", 2)
		return
	}
	if err != nil {
		s.log.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Could not create the profile.", 0)
		return
	}

	profile, err := s.store.createProfile(r.Context(), name, hash, salt, iters, true)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusForbidden, "setup_complete", "Setup is already complete. Sign in instead.", 0)
		return
	}
	if err != nil {
		s.log.Error("create profile", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not create the profile.", 0)
		return
	}

	if err := s.startSession(w, r, profile.ID); err != nil {
		s.log.Error("start session", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Profile created, but sign-in failed.", 0)
		return
	}

	s.log.Info("owner profile created", "profile", profile.ID, "name", profile.Name)
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginAllowed(w, r) {
		return
	}

	var req struct {
		ProfileID string `json:"profileId"`
		Name      string `json:"name"`
		Password  string `json:"password"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	req.ProfileID = strings.TrimSpace(req.ProfileID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ProfileID == "" && req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Choose a profile.", 0)
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Enter your password.", 0)
		return
	}

	var (
		account *profileAuth
		err     error
	)
	if req.ProfileID != "" {
		account, err = s.store.profileByID(r.Context(), req.ProfileID)
	} else {
		account, err = s.store.profileByName(r.Context(), req.Name)
	}
	if err != nil {
		s.burnPasswordCheck(req.Password)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Wrong profile or password.", 0)
		return
	}

	if !verifyPassword(req.Password, account.PasswordHash, account.PasswordSalt, account.PasswordIters) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Wrong profile or password.", 0)
		return
	}

	if err := s.startSession(w, r, account.ID); err != nil {
		s.log.Error("start session", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not sign in.", 0)
		return
	}

	// Opportunistic cleanup; failures are not worth failing a login over.
	_ = s.store.deleteExpiredSessions(r.Context(), nowMillis())

	writeJSON(w, http.StatusOK, account.Profile)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		_ = s.store.deleteSession(r.Context(), hashToken(cookie.Value))
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	account, err := s.store.profileByID(r.Context(), profile.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the profile.", 0)
		return
	}
	if !verifyPassword(req.CurrentPassword, account.PasswordHash, account.PasswordSalt, account.PasswordIters) {
		writeError(w, http.StatusForbidden, "invalid_password", "The current password is wrong.", 0)
		return
	}

	hash, salt, iters, err := s.derivePassword(req.NewPassword)
	if errors.Is(err, errHashBusy) {
		writeError(w, http.StatusTooManyRequests, "busy", "The server is busy. Try again in a moment.", 2)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not change the password.", 0)
		return
	}

	if err := s.store.updateProfilePassword(r.Context(), profile.ID, hash, salt, iters); err != nil {
		writeError(w, http.StatusInternalServerError, "storage", "Could not change the password.", 0)
		return
	}

	// Other devices must sign in again. This device gets a fresh session.
	if err := s.store.deleteSessionsForProfile(r.Context(), profile.ID); err != nil {
		s.log.Warn("clear sessions", "err", err)
	}
	if err := s.startSession(w, r, profile.ID); err != nil {
		s.log.Error("start session after password change", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Password changed. Please sign in again.", 0)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	var req struct {
		DefaultModel     *string `json:"defaultModel"`
		IgnoredProviders *string `json:"ignoredProviders"`
		PersonaName      *string `json:"personaName"`
		PersonaPrompt    *string `json:"personaPrompt"`
		UserName         *string `json:"userName"`
		UserPersona      *string `json:"userPersona"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if req.DefaultModel == nil && req.IgnoredProviders == nil &&
		req.PersonaName == nil && req.PersonaPrompt == nil &&
		req.UserName == nil && req.UserPersona == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "No profile settings to update.", 0)
		return
	}

	if req.DefaultModel != nil {
		model := strings.TrimSpace(*req.DefaultModel)
		if model != "" && !validModelID(model) {
			writeError(w, http.StatusBadRequest, "bad_request", "That model ID is invalid.", 0)
			return
		}
		if err := s.store.updateProfileDefaultModel(r.Context(), profile.ID, model); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Profile not found.", 0)
				return
			}
			s.log.Error("update default model", "err", err)
			writeError(w, http.StatusInternalServerError, "storage", "Could not save the default model.", 0)
			return
		}
	}

	if req.IgnoredProviders != nil {
		ignored, err := normalizeIgnoredProviders(*req.IgnoredProviders)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
			return
		}
		if err := s.store.updateProfileIgnoredProviders(r.Context(), profile.ID, ignored); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Profile not found.", 0)
				return
			}
			s.log.Error("update ignored providers", "err", err)
			writeError(w, http.StatusInternalServerError, "storage", "Could not save ignored providers.", 0)
			return
		}
	}

	if req.PersonaName != nil || req.PersonaPrompt != nil {
		name := profile.PersonaName
		prompt := profile.PersonaPrompt
		if req.PersonaName != nil {
			name = strings.TrimSpace(*req.PersonaName)
		}
		if req.PersonaPrompt != nil {
			prompt = strings.TrimSpace(*req.PersonaPrompt)
		}
		if len(name) > 64 {
			writeError(w, http.StatusBadRequest, "bad_request", "Assistant names are limited to 64 characters.", 0)
			return
		}
		if len(prompt) > maxMessageBytes {
			writeError(w, http.StatusBadRequest, "bad_request", "That persona prompt is too long.", 0)
			return
		}
		if err := s.store.updateProfilePersona(r.Context(), profile.ID, name, prompt); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Profile not found.", 0)
				return
			}
			s.log.Error("update persona", "err", err)
			writeError(w, http.StatusInternalServerError, "storage", "Could not save the persona.", 0)
			return
		}
	}

	if req.UserName != nil || req.UserPersona != nil {
		name := profile.UserName
		persona := profile.UserPersona
		if req.UserName != nil {
			name = strings.TrimSpace(*req.UserName)
		}
		if req.UserPersona != nil {
			persona = strings.TrimSpace(*req.UserPersona)
		}
		if len(name) > 64 {
			writeError(w, http.StatusBadRequest, "bad_request", "Your name is limited to 64 characters.", 0)
			return
		}
		if len(persona) > maxMessageBytes {
			writeError(w, http.StatusBadRequest, "bad_request", "That user context is too long.", 0)
			return
		}
		if err := s.store.updateProfileUser(r.Context(), profile.ID, name, persona); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Profile not found.", 0)
				return
			}
			s.log.Error("update user profile", "err", err)
			writeError(w, http.StatusInternalServerError, "storage", "Could not save your profile.", 0)
			return
		}
	}

	updated, err := s.store.profileByID(r.Context(), profile.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage", "Could not reload the profile.", 0)
		return
	}
	writeJSON(w, http.StatusOK, updated.Profile)
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	if !requireOwner(w, currentProfile(r)) {
		return
	}
	profiles, err := s.store.listProfiles(r.Context())
	if err != nil {
		s.log.Error("list profiles", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load profiles.", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if !requireOwner(w, currentProfile(r)) {
		return
	}

	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateProfileName(name); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	hash, salt, iters, err := s.derivePassword(req.Password)
	if errors.Is(err, errHashBusy) {
		writeError(w, http.StatusTooManyRequests, "busy", "The server is busy. Try again in a moment.", 2)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not create the profile.", 0)
		return
	}

	profile, err := s.store.createProfile(r.Context(), name, hash, salt, iters, false)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "That profile name is already taken.", 0)
		return
	}
	if err != nil {
		s.log.Error("create profile", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not create the profile.", 0)
		return
	}

	s.log.Info("profile created", "profile", profile.ID, "name", profile.Name)
	writeJSON(w, http.StatusCreated, profile)
}

/* --------------------------------------------------------------------- utils */

func validateProfileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("A profile name is required.")
	}
	if len([]rune(name)) > maxProfileNameLen {
		return errors.New("That profile name is too long.")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("That profile name contains invalid characters.")
		}
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return errors.New("Use at least 8 characters for the password.")
	}
	if len(password) > maxPasswordLength {
		return errors.New("That password is too long.")
	}
	return nil
}

func (s *Server) loginAllowed(w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	if s.logins != nil && !s.logins.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"Too many attempts. Wait a moment and try again.", 30)
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// sameOriginRequest blocks cross-site mutations. SameSite=Lax already covers
// most of this; the Origin check closes the gap and catches odd clients.
func sameOriginRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // curl and other non-browser clients
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || strings.EqualFold(u.Host, "null") {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
}

type loginBucket struct {
	limiter *rate.Limiter
	last    time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{buckets: make(map[string]*loginBucket)}
}

func (l *loginLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[ip]
	if bucket == nil {
		// Burst 8, then one attempt every 15 seconds.
		bucket = &loginBucket{limiter: rate.NewLimiter(rate.Every(15*time.Second), 8)}
		l.buckets[ip] = bucket
	}
	bucket.last = now

	if len(l.buckets) > 64 {
		for key, value := range l.buckets {
			if now.Sub(value.last) > 10*time.Minute {
				delete(l.buckets, key)
			}
		}
	}
	return bucket.limiter.Allow()
}
