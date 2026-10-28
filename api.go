package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

const (
	maxJSONBody            = 64 << 10
	maxPromptBytes         = 16 << 10
	maxPromptTitle         = 120
	maxImportBody          = 64 << 20
	maxImportMessages      = 500
	maxImportAttachmentSum = 48 << 20
)

// statusWriter records the status code for the request log. It intentionally
// exposes Unwrap so http.NewResponseController can still reach the underlying
// Flusher: wrapping it without Unwrap is exactly how SSE gets silently broken.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request",
					"err", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				if !sw.wroteHeader {
					writeError(sw, http.StatusInternalServerError, "internal", "Something went wrong.", 0)
				}
			}
			if isLoggedRequest(r) {
				s.log.Info("http",
					"method", r.Method,
					"path", r.URL.Path,
					"status", sw.statusCode(),
					"bytes", sw.bytes,
					"ms", time.Since(start).Milliseconds(),
				)
			}
		}()

		h := sw.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"img-src 'self' data:; "+
				"style-src 'self'; "+
				"script-src 'self'; "+
				"connect-src 'self'; "+
				"base-uri 'none'; "+
				"form-action 'none'; "+
				"frame-ancestors 'none'",
		)

		if !sameOriginRequest(r) {
			writeError(sw, http.StatusForbidden, "forbidden", "Cross-origin request blocked.", 0)
			return
		}

		next.ServeHTTP(sw, r)
	})
}

// isLoggedRequest keeps static asset noise out of the journal.
func isLoggedRequest(r *http.Request) bool {
	p := r.URL.Path
	return p == "/" || p == "/healthz" || strings.HasPrefix(p, "/api/")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := s.store.ping(ctx) == nil

	providers, fetchedAt := s.models.snapshot(s.providers)
	enabled := false
	for _, p := range providers {
		if p.Enabled {
			enabled = true
		}
	}

	status := http.StatusOK
	if !dbOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":              dbOK,
		"version":         version,
		"uptimeSeconds":   int64(time.Since(s.started).Seconds()),
		"streams":         s.streams.Load(),
		"maxStreams":      maxStreams,
		"providerEnabled": enabled,
		"modelsCachedAt":  fetchedAt,
		"db":              dbOK,
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	providers, fetchedAt := s.models.snapshot(s.providers)
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
		"fetchedAt": fetchedAt,
	})
}

func (s *Server) handleModelEndpoints(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	modelID := strings.TrimSpace(r.URL.Query().Get("model"))
	if !validModelID(modelID) {
		writeError(w, http.StatusBadRequest, "bad_request", "A valid model is required.", 0)
		return
	}
	provider := s.providers[providerOpenRouter]
	if provider == nil || !provider.enabled() {
		writeError(w, http.StatusBadGateway, "provider_auth", "No OpenRouter API key is configured.", 0)
		return
	}

	endpoints, err := s.models.endpointsFor(r.Context(), provider, modelID)
	if err != nil {
		s.log.Warn("load model endpoints", "model", modelID, "err", err)
		writeError(w, http.StatusBadGateway, "provider_error",
			"Could not load the providers for that model.", 0)
		return
	}

	// OpenRouter's public endpoint API is not account-aware, so account-wide
	// ignored providers cannot be discovered here. Hide endpoints the user has
	// listed as ignored, and drop dead endpoints so they can never be pinned.
	ignored := ignoredProviderList(profile.IgnoredProviders)
	visible := make([]Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.Available || endpointIgnored(endpoint, ignored) {
			continue
		}
		visible = append(visible, endpoint)
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": visible})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	list, err := s.store.listConversations(r.Context(), profile.ID)
	if err != nil {
		s.log.Error("list conversations", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not list conversations.", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	var req struct {
		Provider        string  `json:"provider"`
		Model           string  `json:"model"`
		SystemPrompt    *string `json:"systemPrompt"`
		ProviderSort    string  `json:"providerSort"`
		ProviderOnly    string  `json:"providerOnly"`
		AllowFallbacks  *bool   `json:"allowFallbacks"`
		ToolsEnabled    *bool   `json:"toolsEnabled"`
		MaxToolCalls    *int    `json:"maxToolCalls"`
		ReasoningEffort string  `json:"reasoningEffort"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		providerID = providerOpenRouter
	}
	provider, ok := s.providers[providerID]
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider.", 0)
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.cfg.DefaultModel
	}
	if model == "" {
		model = provider.DefaultModel
	}
	if len(model) > 200 {
		writeError(w, http.StatusBadRequest, "bad_request", "That model name is too long.", 0)
		return
	}

	prompt := strings.TrimSpace(profile.PersonaPrompt)
	if prompt == "" {
		prompt = s.cfg.DefaultSystemPrompt
	}
	if req.SystemPrompt != nil {
		prompt = strings.TrimSpace(*req.SystemPrompt)
	}
	if len(prompt) > maxMessageBytes {
		writeError(w, http.StatusBadRequest, "bad_request", "That system prompt is too long.", 0)
		return
	}

	providerSort := strings.TrimSpace(req.ProviderSort)
	providerOnly := strings.TrimSpace(req.ProviderOnly)
	if !validProviderSort(providerSort) {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider sort.", 0)
		return
	}
	if providerOnly != "" {
		if !validProviderTag(providerOnly) {
			writeError(w, http.StatusBadRequest, "bad_request", "That provider tag is invalid.", 0)
			return
		}
		providerSort = ""
	}
	allowFallbacks := true
	if req.AllowFallbacks != nil {
		allowFallbacks = *req.AllowFallbacks
	}
	toolsEnabled := true
	if req.ToolsEnabled != nil {
		toolsEnabled = *req.ToolsEnabled
	}
	maxToolCalls := defaultToolCalls
	if req.MaxToolCalls != nil {
		maxToolCalls = *req.MaxToolCalls
	}
	if maxToolCalls < 1 || maxToolCalls > maxToolCallsLimit {
		writeError(w, http.StatusBadRequest, "bad_request", "Tool calls must be between 1 and 30.", 0)
		return
	}
	reasoningEffort := strings.TrimSpace(req.ReasoningEffort)
	if reasoningEffort != "" && !validReasoningEffort(reasoningEffort) {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown reasoning effort.", 0)
		return
	}

	conv, err := s.store.createConversation(r.Context(), profile.ID, providerID, model, prompt,
		providerSort, providerOnly, allowFallbacks, toolsEnabled, maxToolCalls, reasoningEffort)
	if err != nil {
		s.log.Error("create conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not create the conversation.", 0)
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleImportConversation(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	var req struct {
		Title           string `json:"title"`
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		SystemPrompt    string `json:"systemPrompt"`
		ProviderSort    string `json:"providerSort"`
		ProviderOnly    string `json:"providerOnly"`
		AllowFallbacks  *bool  `json:"allowFallbacks"`
		ToolsEnabled    *bool  `json:"toolsEnabled"`
		MaxToolCalls    *int   `json:"maxToolCalls"`
		ReasoningEffort string `json:"reasoningEffort"`
		Messages        []struct {
			Role        string       `json:"role"`
			Content     string       `json:"content"`
			Attachments []Attachment `json:"attachments"`
			Reasoning   string       `json:"reasoning"`
			Steps       []ChatStep   `json:"steps"`
			Citations   []Citation   `json:"citations"`
			ToolRun     *ToolRun     `json:"toolRun"`
			CreatedAt   int64        `json:"createdAt"`
		} `json:"messages"`
	}
	if err := decodeJSON(w, r, &req, maxImportBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "That file has no messages.", 0)
		return
	}
	if len(req.Messages) > maxImportMessages {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("Imports are limited to %d messages.", maxImportMessages), 0)
		return
	}

	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		providerID = providerOpenRouter
	}
	provider, ok := s.providers[providerID]
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider.", 0)
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.cfg.DefaultModel
	}
	if model == "" {
		model = provider.DefaultModel
	}
	if len(model) > 200 {
		writeError(w, http.StatusBadRequest, "bad_request", "That model name is too long.", 0)
		return
	}

	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if len(systemPrompt) > maxMessageBytes {
		writeError(w, http.StatusBadRequest, "bad_request", "That system prompt is too long.", 0)
		return
	}

	providerSort := strings.TrimSpace(req.ProviderSort)
	providerOnly := strings.TrimSpace(req.ProviderOnly)
	if !validProviderSort(providerSort) {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider sort.", 0)
		return
	}
	if providerOnly != "" {
		if !validProviderTag(providerOnly) {
			writeError(w, http.StatusBadRequest, "bad_request", "That provider tag is invalid.", 0)
			return
		}
		providerSort = ""
	}
	allowFallbacks := true
	if req.AllowFallbacks != nil {
		allowFallbacks = *req.AllowFallbacks
	}
	toolsEnabled := true
	if req.ToolsEnabled != nil {
		toolsEnabled = *req.ToolsEnabled
	}
	importToolCalls := defaultToolCalls
	if req.MaxToolCalls != nil {
		importToolCalls = *req.MaxToolCalls
	}
	if importToolCalls < 1 || importToolCalls > maxToolCallsLimit {
		writeError(w, http.StatusBadRequest, "bad_request", "Tool calls must be between 1 and 30.", 0)
		return
	}
	reasoningEffort := strings.TrimSpace(req.ReasoningEffort)
	if reasoningEffort != "" && !validReasoningEffort(reasoningEffort) {
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown reasoning effort.", 0)
		return
	}

	messages := make([]Message, 0, len(req.Messages))
	attachmentSum := 0
	for _, raw := range req.Messages {
		role := strings.TrimSpace(raw.Role)
		if role != "user" && role != "assistant" {
			writeError(w, http.StatusBadRequest, "bad_request",
				"Imported messages must be user or assistant messages.", 0)
			return
		}
		if len(raw.Content) > maxMessageBytes {
			writeError(w, http.StatusBadRequest, "bad_request", "A message in that file is too long.", 0)
			return
		}
		attachments, err := validateAttachments(raw.Attachments)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
			return
		}
		for _, attachment := range attachments {
			attachmentSum += len(attachment.Data)
		}
		if attachmentSum > maxImportAttachmentSum {
			writeError(w, http.StatusBadRequest, "bad_request",
				"That conversation's attachments are too large to import.", 0)
			return
		}
		if role == "user" && raw.Content == "" && len(attachments) == 0 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"That conversation has an empty user message.", 0)
			return
		}

		messages = append(messages, Message{
			Role:        role,
			Content:     raw.Content,
			Attachments: attachments,
			Reasoning:   raw.Reasoning,
			Steps:       raw.Steps,
			Citations:   raw.Citations,
			ToolRun:     raw.ToolRun,
			CreatedAt:   raw.CreatedAt,
		})
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		for _, message := range messages {
			if message.Role != "user" {
				continue
			}
			if derived := deriveTitle(message.Content); derived != "New chat" {
				title = derived
			} else if len(message.Attachments) > 0 {
				title = deriveTitle(message.Attachments[0].Name)
			}
			if title != "" {
				break
			}
		}
	}
	if title == "" {
		title = "Imported chat"
	}
	if len([]rune(title)) > 200 {
		writeError(w, http.StatusBadRequest, "bad_request", "That title is too long.", 0)
		return
	}

	conv, err := s.store.importConversation(r.Context(), profile.ID, title, providerID, model,
		systemPrompt, providerSort, providerOnly, allowFallbacks, toolsEnabled, importToolCalls,
		reasoningEffort, messages)
	if err != nil {
		s.log.Error("import conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not import the conversation.", 0)
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	conv, err := s.store.getConversation(r.Context(), profile.ID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Conversation not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("get conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the conversation.", 0)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	var req struct {
		Title           *string `json:"title"`
		SystemPrompt    *string `json:"systemPrompt"`
		Provider        *string `json:"provider"`
		Model           *string `json:"model"`
		ProviderSort    *string `json:"providerSort"`
		ProviderOnly    *string `json:"providerOnly"`
		AllowFallbacks  *bool   `json:"allowFallbacks"`
		ToolsEnabled    *bool   `json:"toolsEnabled"`
		MaxToolCalls    *int    `json:"maxToolCalls"`
		ReasoningEffort *string `json:"reasoningEffort"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "A title cannot be empty.", 0)
			return
		}
		if len([]rune(title)) > 200 {
			writeError(w, http.StatusBadRequest, "bad_request", "That title is too long.", 0)
			return
		}
		req.Title = &title
	}
	if req.SystemPrompt != nil {
		prompt := strings.TrimSpace(*req.SystemPrompt)
		if len(prompt) > maxMessageBytes {
			writeError(w, http.StatusBadRequest, "bad_request", "That system prompt is too long.", 0)
			return
		}
		req.SystemPrompt = &prompt
	}
	if req.Provider != nil {
		id := strings.TrimSpace(*req.Provider)
		if _, ok := s.providers[id]; !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider.", 0)
			return
		}
		req.Provider = &id
	}
	if req.Model != nil {
		model := strings.TrimSpace(*req.Model)
		if len(model) > 200 {
			writeError(w, http.StatusBadRequest, "bad_request", "That model name is too long.", 0)
			return
		}
		req.Model = &model
	}
	if req.ProviderSort != nil {
		sort := strings.TrimSpace(*req.ProviderSort)
		if !validProviderSort(sort) {
			writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider sort.", 0)
			return
		}
		req.ProviderSort = &sort
	}
	if req.ProviderOnly != nil {
		only := strings.TrimSpace(*req.ProviderOnly)
		if only != "" && !validProviderTag(only) {
			writeError(w, http.StatusBadRequest, "bad_request", "That provider tag is invalid.", 0)
			return
		}
		req.ProviderOnly = &only
	}
	if req.MaxToolCalls != nil {
		if *req.MaxToolCalls < 1 || *req.MaxToolCalls > maxToolCallsLimit {
			writeError(w, http.StatusBadRequest, "bad_request", "Tool calls must be between 1 and 30.", 0)
			return
		}
	}
	if req.ReasoningEffort != nil {
		effort := strings.TrimSpace(*req.ReasoningEffort)
		if effort != "" && !validReasoningEffort(effort) {
			writeError(w, http.StatusBadRequest, "bad_request", "Unknown reasoning effort.", 0)
			return
		}
		req.ReasoningEffort = &effort
	}

	id := r.PathValue("id")
	upd := ConversationUpdate{
		Title:           req.Title,
		SystemPrompt:    req.SystemPrompt,
		Provider:        req.Provider,
		Model:           req.Model,
		ProviderSort:    req.ProviderSort,
		ProviderOnly:    req.ProviderOnly,
		AllowFallbacks:  req.AllowFallbacks,
		ToolsEnabled:    req.ToolsEnabled,
		MaxToolCalls:    req.MaxToolCalls,
		ReasoningEffort: req.ReasoningEffort,
	}
	err := s.store.updateConversation(r.Context(), profile.ID, id, upd)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Conversation not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("update conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not update the conversation.", 0)
		return
	}

	conv, err := s.store.getConversation(r.Context(), profile.ID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage", "Could not reload the conversation.", 0)
		return
	}
	conv.Messages = nil
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	err := s.store.deleteConversation(r.Context(), profile.ID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Conversation not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("delete conversation", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not delete the conversation.", 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	err := s.store.deleteMessage(r.Context(), profile.ID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Message not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("delete message", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not delete the message.", 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	prompts, err := s.store.listPrompts(r.Context(), profile.ID)
	if err != nil {
		s.log.Error("list prompts", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load prompts.", 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompts": prompts})
}

func (s *Server) handleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	var req struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		System bool   `json:"system"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	title, body, err := validatePrompt(req.Title, req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	prompt, err := s.store.createPrompt(r.Context(), profile.ID, title, body, req.System)
	if err != nil {
		s.log.Error("create prompt", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not save the prompt.", 0)
		return
	}
	writeJSON(w, http.StatusCreated, prompt)
}

func (s *Server) handleUpdatePrompt(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	var req struct {
		Title  *string `json:"title"`
		Body   *string `json:"body"`
		System *bool   `json:"system"`
	}
	if err := decodeJSON(w, r, &req, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	if req.Title == nil && req.Body == nil && req.System == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "No prompt changes supplied.", 0)
		return
	}

	id := r.PathValue("id")
	existing, err := s.store.promptByID(r.Context(), profile.ID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Prompt not found.", 0)
			return
		}
		s.log.Error("load prompt", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the prompt.", 0)
		return
	}

	title, body, system := existing.Title, existing.Body, existing.System
	if req.Title != nil {
		title = *req.Title
	}
	if req.Body != nil {
		body = *req.Body
	}
	if req.System != nil {
		system = *req.System
	}
	title, body, err = validatePrompt(title, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	if err := s.store.updatePrompt(r.Context(), profile.ID, id, title, body, system); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Prompt not found.", 0)
			return
		}
		s.log.Error("update prompt", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not update the prompt.", 0)
		return
	}
	updated, err := s.store.promptByID(r.Context(), profile.ID, id)
	if err != nil {
		s.log.Error("reload prompt", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not reload the prompt.", 0)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeletePrompt(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}
	err := s.store.deletePrompt(r.Context(), profile.ID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Prompt not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("delete prompt", "err", err)
		writeError(w, http.StatusInternalServerError, "storage", "Could not delete the prompt.", 0)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validatePrompt(title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", errors.New("Give the prompt a title.")
	}
	if len(title) > maxPromptTitle {
		return "", "", fmt.Errorf("Prompt titles are limited to %d characters.", maxPromptTitle)
	}
	if strings.TrimSpace(body) == "" {
		return "", "", errors.New("The prompt body cannot be empty.")
	}
	if len(body) > maxPromptBytes {
		return "", "", fmt.Errorf("Prompts are limited to %d KB.", maxPromptBytes>>10)
	}
	return title, body, nil
}

type errorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, retryAfter int) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	writeJSON(w, status, errorResponse{Error: code, Message: message, RetryAfter: retryAfter})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slogError("write json response", err)
	}
}

// decodeJSON decodes a single JSON object with a hard size limit. It rejects
// trailing data so a malformed request fails loudly instead of half-applying.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("request body is too large (limit %d bytes)", limit)
		}
		if errors.Is(err, io.EOF) {
			return errors.New("request body is empty")
		}
		return fmt.Errorf("invalid JSON: %v", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func slogError(msg string, err error) {
	slog.Default().Error(msg, "err", err)
}
