package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Local backends all speak the OpenAI chat-completions dialect. Keeping one
// spec table means adding another compatible server (vLLM, LocalAI, Jan, ...)
// is a one-line change rather than a new adapter.
const (
	backendKobold   = "kobold"
	backendOllama   = "ollama"
	backendLMStudio = "lmstudio"
	backendLlamaCPP = "llamacpp"
	backendCustom   = "custom"
)

var (
	ErrUnknownProvider      = errors.New("unknown provider")
	ErrBackendNotConfigured = errors.New("backend is not configured")
)

type backendSpec struct {
	ID         string
	Label      string
	DefaultURL string
	Hint       string
	Local      bool
}

var backendSpecs = []backendSpec{
	{
		ID:    providerOpenRouter,
		Label: "OpenRouter",
		Hint:  "Cloud models. The server API key lives in CLACHAN's .env file.",
	},
	{
		ID:         backendKobold,
		Label:      "KoboldCpp",
		DefaultURL: "http://127.0.0.1:5001/v1",
		Hint:       "KoboldCpp with its OpenAI-compatible API enabled.",
		Local:      true,
	},
	{
		ID:         backendOllama,
		Label:      "Ollama",
		DefaultURL: "http://127.0.0.1:11434/v1",
		Hint:       "Ollama's OpenAI-compatible endpoint.",
		Local:      true,
	},
	{
		ID:         backendLMStudio,
		Label:      "LM Studio",
		DefaultURL: "http://127.0.0.1:1234/v1",
		Hint:       "LM Studio's local server.",
		Local:      true,
	},
	{
		ID:         backendLlamaCPP,
		Label:      "llama.cpp",
		DefaultURL: "http://127.0.0.1:8080/v1",
		Hint:       "llama-server from llama.cpp.",
		Local:      true,
	},
	{
		ID:    backendCustom,
		Label: "Custom",
		Hint:  "Any server exposing /v1/chat/completions and /v1/models.",
		Local: true,
	},
}

func backendSpecFor(id string) (backendSpec, bool) {
	id = strings.TrimSpace(id)
	for _, spec := range backendSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return backendSpec{}, false
}

func localBackendSpecs() []backendSpec {
	out := make([]backendSpec, 0, len(backendSpecs))
	for _, spec := range backendSpecs {
		if spec.Local {
			out = append(out, spec)
		}
	}
	return out
}

// providerFor resolves a profile's saved backend into the in-memory provider
// shape used by the chat handler. OpenRouter stays server-wide; local backends
// are per profile.
func (s *Server) providerFor(ctx context.Context, profileID, providerID string) (*Provider, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		providerID = providerOpenRouter
	}
	if providerID == providerOpenRouter {
		provider, ok := s.providers[providerOpenRouter]
		if !ok {
			return nil, ErrUnknownProvider
		}
		return provider, nil
	}

	spec, ok := backendSpecFor(providerID)
	if !ok || !spec.Local {
		return nil, ErrUnknownProvider
	}

	saved, err := s.store.profileBackend(ctx, profileID, providerID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrBackendNotConfigured
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(saved.BaseURL) == "" {
		return nil, ErrBackendNotConfigured
	}

	label := strings.TrimSpace(saved.Label)
	if label == "" {
		label = spec.Label
	}
	return &Provider{
		ID:           providerID,
		Label:        label,
		BaseURL:      saved.BaseURL,
		APIKey:       saved.APIKey,
		DefaultModel: saved.Model,
		Wire:         "chat",
		Local:        true,
	}, nil
}

// normalizeBackendURL accepts the address a user would paste into a browser
// and turns it into the base every request is appended to. A bare host gets
// /v1 because every supported server exposes that prefix.
func normalizeBackendURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Enter the backend address.")
	}
	if len(raw) > 300 {
		return "", errors.New("That address is too long.")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("That address is not a valid URL.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("The address must start with http:// or https://.")
	}
	if parsed.Host == "" {
		return "", errors.New("The address needs a host, for example http://192.168.1.20:5001.")
	}
	if parsed.User != nil {
		return "", errors.New("Put the API key in the key field, not in the address.")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Remove the query string or fragment from the address.")
	}
	if strings.Contains(parsed.Path, "..") {
		return "", errors.New("That address contains an invalid path.")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" || path == "/" {
		path = "/v1"
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// validLocalModelID is deliberately looser than validModelID: Ollama uses
// name:tag and local servers often use spaces or bracketed quant names.
func validLocalModelID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 200 {
		return false
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == ' ', r == '.', r == '_', r == ':', r == '/', r == '-',
			r == '~', r == '@', r == '+', r == '#', r == '(', r == ')',
			r == '[', r == ']':
		default:
			return false
		}
	}
	return true
}

func validBackendLabel(label string) bool {
	if label == "" || len([]rune(label)) > 64 {
		return false
	}
	for _, r := range label {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validBackendAPIKey(key string) bool {
	if len(key) > 400 {
		return false
	}
	for _, r := range key {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

type backendInfo struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Hint       string `json:"hint,omitempty"`
	Local      bool   `json:"local"`
	Configured bool   `json:"configured"`
	DefaultURL string `json:"defaultUrl,omitempty"`
	BaseURL    string `json:"baseUrl,omitempty"`
	Model      string `json:"model,omitempty"`
	HasKey     bool   `json:"hasKey,omitempty"`
}

func (s *Server) handleListBackends(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	saved, err := s.store.listProfileBackends(r.Context(), profile.ID)
	if err != nil {
		s.log.Error("list backends", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load backends.", 0)
		return
	}
	byID := make(map[string]ProfileBackend, len(saved))
	for _, backend := range saved {
		byID[backend.BackendID] = backend
	}

	out := make([]backendInfo, 0, len(backendSpecs))
	for _, spec := range backendSpecs {
		info := backendInfo{
			ID:         spec.ID,
			Label:      spec.Label,
			Hint:       spec.Hint,
			Local:      spec.Local,
			DefaultURL: spec.DefaultURL,
		}
		if spec.ID == providerOpenRouter {
			if provider, ok := s.providers[providerOpenRouter]; ok {
				info.Configured = provider.enabled()
				info.BaseURL = provider.BaseURL
				info.Model = s.cfg.DefaultModel
				info.HasKey = provider.APIKey != ""
			}
		} else if backend, ok := byID[spec.ID]; ok {
			info.Configured = strings.TrimSpace(backend.BaseURL) != ""
			info.BaseURL = backend.BaseURL
			info.Model = backend.Model
			info.HasKey = backend.APIKey != ""
			if label := strings.TrimSpace(backend.Label); label != "" {
				info.Label = label
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": out})
}

func (s *Server) handleSaveBackend(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	spec, ok := backendSpecFor(id)
	if !ok || !spec.Local {
		writeError(w, http.StatusBadRequest, "bad_request", "That backend cannot be configured here.", 0)
		return
	}

	var req struct {
		BaseURL  string  `json:"baseUrl"`
		APIKey   *string `json:"apiKey"`
		ClearKey bool    `json:"clearKey"`
		Model    string  `json:"model"`
		Label    string  `json:"label"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	baseURL, err := normalizeBackendURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	model := strings.TrimSpace(req.Model)
	if model != "" && !validLocalModelID(model) {
		writeError(w, http.StatusBadRequest, "bad_request", "That model ID is invalid.", 0)
		return
	}

	label := strings.TrimSpace(req.Label)
	if label != "" && !validBackendLabel(label) {
		writeError(w, http.StatusBadRequest, "bad_request", "Backend names are limited to 64 characters.", 0)
		return
	}
	if label == "" {
		label = spec.Label
	}

	existing, err := s.store.profileBackend(r.Context(), profile.ID, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.log.Error("load backend", "err", err, "backend", id)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the backend.", 0)
		return
	}
	apiKey := ""
	if existing != nil {
		apiKey = existing.APIKey
	}
	switch {
	case req.ClearKey:
		apiKey = ""
	case req.APIKey != nil:
		// An empty box means "keep the saved key", so the browser never has
		// to resend a secret just to change the address.
		if value := strings.TrimSpace(*req.APIKey); value != "" {
			if !validBackendAPIKey(value) {
				writeError(w, http.StatusBadRequest, "bad_request", "That API key is invalid.", 0)
				return
			}
			apiKey = value
		}
	}

	saved := ProfileBackend{
		BackendID: id,
		Label:     label,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     model,
	}
	if err := s.store.saveProfileBackend(r.Context(), profile.ID, saved); err != nil {
		s.log.Error("save backend", "err", err, "backend", id)
		writeError(w, http.StatusInternalServerError, "storage", "Could not save the backend.", 0)
		return
	}

	writeJSON(w, http.StatusOK, backendInfo{
		ID:         id,
		Label:      label,
		Hint:       spec.Hint,
		Local:      true,
		Configured: true,
		DefaultURL: spec.DefaultURL,
		BaseURL:    baseURL,
		Model:      model,
		HasKey:     apiKey != "",
	})
}

func (s *Server) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	spec, ok := backendSpecFor(id)
	if !ok || !spec.Local {
		writeError(w, http.StatusBadRequest, "bad_request", "That backend cannot be removed here.", 0)
		return
	}
	err := s.store.deleteProfileBackend(r.Context(), profile.ID, id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Backend not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("delete backend", "err", err, "backend", id)
		writeError(w, http.StatusInternalServerError, "storage", "Could not remove the backend.", 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBackendModels(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == providerOpenRouter {
		writeError(w, http.StatusBadRequest, "bad_request", "OpenRouter models use /api/models.", 0)
		return
	}

	provider, err := s.providerFor(r.Context(), profile.ID, id)
	switch {
	case errors.Is(err, ErrUnknownProvider):
		writeError(w, http.StatusNotFound, "not_found", "Unknown backend.", 0)
		return
	case errors.Is(err, ErrBackendNotConfigured):
		writeError(w, http.StatusBadRequest, "backend_missing",
			"Configure this backend for your profile first.", 0)
		return
	case err != nil:
		s.log.Error("resolve backend", "err", err, "backend", id)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the backend.", 0)
		return
	}

	models, err := s.fetchBackendModels(r.Context(), provider)
	if err != nil {
		s.log.Warn("backend models failed", "backend", id, "err", err)
		writeError(w, http.StatusBadGateway, "backend_unreachable",
			provider.Label+": "+truncate(err.Error(), 300), 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// fetchBackendModels asks a local server for its catalogue. Every supported
// server exposes /v1/models, but the shape varies slightly, so accept both the
// OpenAI "data" field and Ollama's native "models" field.
func (s *Server) fetchBackendModels(ctx context.Context, provider *Provider) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	provider.headers(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the server (%v)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := upstreamErrorMessage(resp)
		if detail == "" {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}

	type modelEntry struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Created int64  `json:"created"`
	}
	var payload struct {
		Data   []modelEntry `json:"data"`
		Models []modelEntry `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("could not read the model list (%v)", err)
	}

	entries := payload.Data
	if len(entries) == 0 {
		entries = payload.Models
	}
	models := make([]Model, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = id
		}
		models = append(models, Model{ID: id, Name: name, Created: entry.Created})
		if len(models) >= 500 {
			break
		}
	}
	if len(models) == 0 {
		return nil, errors.New("the server returned no models")
	}
	sort.SliceStable(models, func(i, j int) bool {
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	return models, nil
}
