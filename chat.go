package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxChatBody       = 40 << 20
	maxMessageBytes   = 1 << 20
	idleStreamTimeout = 90 * time.Second
	maxStreamDuration = 15 * time.Minute
	defaultRetryAfter = 5
)

type chatRequest struct {
	ConversationID  string       `json:"conversationId"`
	Provider        string       `json:"provider,omitempty"`
	Model           string       `json:"model,omitempty"`
	Content         string       `json:"content,omitempty"`
	Attachments     []Attachment `json:"attachments,omitempty"`
	ToolsEnabled    *bool        `json:"toolsEnabled,omitempty"`
	MaxToolCalls    *int         `json:"maxToolCalls,omitempty"`
	ReasoningEffort *string      `json:"reasoningEffort,omitempty"`
	Timezone        string       `json:"timezone,omitempty"`
	Action          string       `json:"action,omitempty"` // "", "regenerate", "edit"
	MessageID       string       `json:"messageId,omitempty"`
}

type metaEvent struct {
	ConversationID     string `json:"conversationId"`
	UserMessageID      string `json:"userMessageId,omitempty"`
	AssistantMessageID string `json:"assistantMessageId"`
	Title              string `json:"title"`
	Model              string `json:"model"`
	Replaced           bool   `json:"replaced,omitempty"`
}

type deltaEvent struct {
	Text string `json:"text"`
}

type doneEvent struct {
	FinishReason string     `json:"finishReason,omitempty"`
	Usage        *usage     `json:"usage,omitempty"`
	Saved        bool       `json:"saved"`
	Content      string     `json:"content,omitempty"`
	Reasoning    string     `json:"reasoning,omitempty"`
	Steps        []ChatStep `json:"steps,omitempty"`
	Citations    []Citation `json:"citations,omitempty"`
	ToolRun      *ToolRun   `json:"toolRun,omitempty"`
}

type reasoningEvent struct {
	Text        string `json:"text"`
	ItemID      string `json:"itemId,omitempty"`
	OutputIndex int    `json:"outputIndex,omitempty"`
}

type stepEvent struct {
	Step ChatStep `json:"step"`
}

type citationEvent struct {
	Citations []Citation `json:"citations"`
}

type toolEvent struct {
	Activity ToolActivity `json:"activity"`
}

type errorEvent struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter,omitempty"`
}

// handleChat is the only streaming endpoint. The browser must use POST because
// EventSource cannot send a body; the server rebuilds history from SQLite and
// never trusts a client-supplied transcript.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	profile := currentProfile(r)
	if profile == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", 0)
		return
	}

	if !s.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"Slow down for a moment, then try again.", defaultRetryAfter)
		return
	}

	var req chatRequest
	if err := decodeJSON(w, r, &req, maxChatBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	if len(req.Content) > maxMessageBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "bad_request", "That message is too long.", 0)
		return
	}
	if req.ConversationID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "conversationId is required.", 0)
		return
	}

	attachments, err := validateAttachments(req.Attachments)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), 0)
		return
	}
	req.Attachments = attachments

	switch req.Action {
	case "":
		if req.Content == "" && len(req.Attachments) == 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "The message is empty.", 0)
			return
		}
	case "regenerate":
	case "edit":
		if req.MessageID == "" || req.Content == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "Editing requires messageId and content.", 0)
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown chat action.", 0)
		return
	}

	// Hard capacity: two streams, no queueing. A third request is told to come
	// back later before anything is written or persisted.
	select {
	case s.sem <- struct{}{}:
		s.streams.Add(1)
		defer func() {
			<-s.sem
			s.streams.Add(-1)
		}()
	default:
		writeError(w, http.StatusServiceUnavailable, "busy",
			"Clachan is already generating two replies. Try again in a moment.", 2)
		return
	}

	conv, err := s.store.getConversation(r.Context(), profile.ID, req.ConversationID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Conversation not found.", 0)
		return
	}
	if err != nil {
		s.log.Error("load conversation", "err", err, "conversation", req.ConversationID)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the conversation.", 0)
		return
	}

	providerID := req.Provider
	if providerID == "" {
		providerID = conv.Provider
	}
	if providerID == "" {
		providerID = providerOpenRouter
	}
	provider, err := s.providerFor(r.Context(), profile.ID, providerID)
	switch {
	case errors.Is(err, ErrUnknownProvider):
		writeError(w, http.StatusBadRequest, "bad_request", "Unknown provider.", 0)
		return
	case errors.Is(err, ErrBackendNotConfigured):
		writeError(w, http.StatusBadRequest, "bad_request",
			"That backend is not configured for this profile. Add it in Settings → Backends.", 0)
		return
	case err != nil:
		s.log.Error("resolve backend", "err", err, "provider", providerID)
		writeError(w, http.StatusInternalServerError, "storage", "Could not load the backend.", 0)
		return
	}
	if !provider.enabled() {
		message := "No OpenRouter API key is configured on the server."
		if provider.Local {
			message = provider.Label + " is not configured for this profile."
		}
		writeError(w, http.StatusBadGateway, "provider_auth", message, 0)
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = conv.Model
	}
	if model == "" {
		model = provider.DefaultModel
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "A model is required.", 0)
		return
	}
	if len(model) > 200 {
		writeError(w, http.StatusBadRequest, "bad_request", "That model name is too long.", 0)
		return
	}

	toolsEnabled := conv.ToolsEnabled
	if req.ToolsEnabled != nil {
		toolsEnabled = *req.ToolsEnabled
	}
	toolCalls := conv.MaxToolCalls
	if toolCalls == 0 {
		toolCalls = defaultToolCalls
	}
	// Local backends have no server tools, so the browser sends 0 to say "not
	// applicable". Only validate the range for providers that actually use it.
	if req.MaxToolCalls != nil && !provider.Local {
		if *req.MaxToolCalls < 1 || *req.MaxToolCalls > maxToolCallsLimit {
			writeError(w, http.StatusBadRequest, "bad_request", "Tool calls must be between 1 and 30.", 0)
			return
		}
		toolCalls = *req.MaxToolCalls
	}

	reasoningEffort := strings.TrimSpace(conv.ReasoningEffort)
	if req.ReasoningEffort != nil {
		reasoningEffort = strings.TrimSpace(*req.ReasoningEffort)
	}
	if provider.Local {
		// Server tools, provider routing and reasoning effort are OpenRouter
		// features. A local model would reject the payload fields, so drop them
		// instead of half-supporting them.
		toolsEnabled = false
		toolCalls = 0
		reasoningEffort = ""
	} else {
		if reasoningEffort != "" && !validReasoningEffort(reasoningEffort) {
			writeError(w, http.StatusBadRequest, "bad_request", "Unknown reasoning effort.", 0)
			return
		}
		if reasoningEffort != "" {
			if info := s.models.lookup(provider.ID, model); info != nil {
				if info.Reasoning == nil || len(info.Reasoning.SupportedEfforts) == 0 {
					writeError(w, http.StatusBadRequest, "bad_request",
						"This model does not expose reasoning effort.", 0)
					return
				}
				if !containsString(info.Reasoning.SupportedEfforts, reasoningEffort) {
					writeError(w, http.StatusBadRequest, "bad_request",
						"That reasoning effort is not available for this model.", 0)
					return
				}
			}
		}
	}

	timezone := strings.TrimSpace(req.Timezone)
	if !validTimezone(timezone) {
		timezone = "UTC"
	}

	history := conv.Messages
	var userContent string
	var userAttachments []Attachment
	var replaceID string

	switch req.Action {
	case "":
		userContent = req.Content
		userAttachments = req.Attachments
	case "regenerate":
		if len(history) == 0 || history[len(history)-1].Role != "assistant" {
			writeError(w, http.StatusBadRequest, "bad_request", "There is no reply to regenerate.", 0)
			return
		}
		last := history[len(history)-1]
		// The client names the reply it is regenerating. If the conversation has
		// moved on since that render (another tab), replacing the newest reply
		// would answer a different question, so refuse instead.
		if req.MessageID != "" && req.MessageID != last.ID {
			writeError(w, http.StatusConflict, "conflict",
				"That reply is no longer the latest message. Reload the conversation and try again.", 0)
			return
		}
		replaceID = last.ID
		history = history[:len(history)-1]
	case "edit":
		idx := -1
		for i := range history {
			if history[i].ID == req.MessageID {
				idx = i
				break
			}
		}
		if idx < 0 || history[idx].Role != "user" {
			writeError(w, http.StatusNotFound, "not_found", "Message not found.", 0)
			return
		}
		userContent = req.Content
		userAttachments = history[idx].Attachments
		history = history[:idx]
	}

	if provider.Local && req.Action == "" && hasNonTextAttachment(userAttachments) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"This backend is text-only. Remove image or PDF attachments, or switch to OpenRouter.", 0)
		return
	}

	systemPrompt := buildSystemPrompt(conv.SystemPrompt, profile.PersonaName, profile.UserName, profile.UserPersona)

	var (
		msgs        []responseInputMessage
		localMsgs   []upstreamMessage
		serverTools []serverTool
		reasoning   *reasoningRequest
	)
	if provider.Local {
		localMsgs = localChatMessages(systemPrompt, history, userContent, userAttachments, req.Action != "regenerate")
	} else {
		msgs = make([]responseInputMessage, 0, len(history)+1)
		for _, m := range history {
			if m.Role == "user" || m.Role == "assistant" {
				msgs = append(msgs, responseInputMessage{
					Type:    "message",
					Role:    m.Role,
					Content: responsesContent(m.Content, m.Attachments),
				})
			}
		}
		if req.Action != "regenerate" {
			msgs = append(msgs, responseInputMessage{
				Type:    "message",
				Role:    "user",
				Content: responsesContent(userContent, userAttachments),
			})
		}

		serverTools = buildServerTools(toolsEnabled, timezone)
		if info := s.models.lookup(provider.ID, model); info != nil && info.SupportsReasoning {
			reasoning = &reasoningRequest{Summary: "auto"}
			if reasoningEffort != "" {
				reasoning.Effort = reasoningEffort
			} else if info.Reasoning != nil && !info.Reasoning.DefaultEnabled && !info.Reasoning.Mandatory {
				reasoning = nil
			}
		}
	}

	upCtx, cancel := context.WithTimeout(r.Context(), maxStreamDuration)
	defer cancel()

	var stalled atomic.Bool
	watchdog := time.AfterFunc(idleStreamTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer watchdog.Stop()

	startedAt := time.Now()
	var resp *http.Response
	if provider.Local {
		resp, err = provider.localResponses(upCtx, s.client, model, localMsgs)
	} else {
		resp, err = provider.responses(upCtx, s.client, model, msgs, systemPrompt,
			conversationProviderPrefs(conv, ignoredProviderList(profile.IgnoredProviders)),
			serverTools, toolCalls, reasoning)
	}
	if err != nil {
		if r.Context().Err() != nil {
			return // client hung up before the request was sent
		}
		if errors.Is(err, context.DeadlineExceeded) {
			s.log.Warn("upstream timeout", "provider", provider.ID)
			writeError(w, http.StatusGatewayTimeout, "provider_timeout",
				provider.Label+" did not respond in time.", 0)
			return
		}
		s.log.Warn("upstream dial failed", "provider", provider.ID, "err", err)
		message := "Could not reach " + provider.Label + "."
		if provider.Local {
			message = "Could not reach " + provider.Label + " at " + provider.BaseURL + "."
		}
		writeError(w, http.StatusBadGateway, "provider_unreachable", message, 0)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.handleUpstreamStatus(w, resp, provider)
		return
	}

	// Persist only after upstream accepted the request, so a 429/401 leaves no
	// orphan user message and a retry is safe.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()

	var title string
	var userMsg *Message
	switch req.Action {
	case "":
		userMsg, title, err = s.store.appendUserMessage(writeCtx, profile.ID, conv.ID, provider.ID, model, userContent, userAttachments)
	case "edit":
		title, err = s.store.editUserMessage(writeCtx, profile.ID, conv.ID, req.MessageID, userContent, provider.ID, model)
	case "regenerate":
		title = conv.Title
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Conversation or message not found.", 0)
			return
		}
		s.log.Error("persist user turn", "err", err, "conversation", conv.ID)
		writeError(w, http.StatusInternalServerError, "storage", "Could not save the message.", 0)
		return
	}
	// Local backends ignore these, so do not overwrite the conversation's saved
	// OpenRouter tool and reasoning settings with the local defaults.
	if !provider.Local && (req.ToolsEnabled != nil || req.MaxToolCalls != nil || req.ReasoningEffort != nil) {
		_ = s.store.updateConversation(writeCtx, profile.ID, conv.ID, ConversationUpdate{
			ToolsEnabled:    &toolsEnabled,
			MaxToolCalls:    &toolCalls,
			ReasoningEffort: &reasoningEffort,
		})
	}

	assistantID := newID()
	replaced := false
	if req.Action == "regenerate" {
		assistantID = replaceID
		replaced = true
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sse := &sseWriter{w: w, rc: http.NewResponseController(w)}
	meta := metaEvent{
		ConversationID:     conv.ID,
		AssistantMessageID: assistantID,
		Title:              title,
		Model:              model,
		Replaced:           replaced,
	}
	if userMsg != nil {
		meta.UserMessageID = userMsg.ID
	}
	if req.Action == "edit" {
		meta.UserMessageID = req.MessageID
	}
	if err := sse.event("meta", meta); err != nil {
		return
	}

	var content strings.Builder
	// Buffered providers can deliver the answer only in terminal events
	// (output_text.done, content_part.done) instead of incremental deltas.
	var doneText string
	var capturedUsage *usage
	var completed *responsesResult
	finishReason := ""
	streamErr := ""
	eventCounts := make(map[string]int)
	decodeErrors := 0
	// A terminal upstream event ([DONE], response.completed, response.incomplete
	// or response.failed). Without one, the stream was cut short.
	terminalSeen := false
	clientGone := false

	var citations []Citation
	citationIndex := make(map[string]int)

	addCitation := func(value annotation) (Citation, bool) {
		citation, ok := value.citation()
		if !ok {
			return Citation{}, false
		}
		if index, seen := citationIndex[citation.URL]; seen {
			existing := &citations[index]
			changed := false
			if existing.Title == "" && citation.Title != "" {
				existing.Title = citation.Title
				changed = true
			}
			if existing.Excerpt == "" && citation.Excerpt != "" {
				existing.Excerpt = citation.Excerpt
				changed = true
			}
			return *existing, changed
		}
		citationIndex[citation.URL] = len(citations)
		citations = append(citations, citation)
		return citation, true
	}
	send := func(name string, payload any) bool {
		if err := sse.event(name, payload); err != nil {
			clientGone = true
			cancel()
			return false
		}
		return true
	}

	outputByIndex := make(map[int]responseItem)
	itemStarted := make(map[int]time.Time)
	reasoningByIndex := make(map[int]string)
	reasoningStreamed := make(map[int]bool)
	var pendingReasoning []string
	var streamedSteps []ChatStep

	reader := bufio.NewReaderSize(resp.Body, 32<<10)

read:
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			watchdog.Reset(idleStreamTimeout)

			data, done := parseSSELine(line)
			if done {
				terminalSeen = true
				break
			}
			if data == "" {
				continue
			}

			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(data), &envelope); err != nil || envelope.Type == "" {
				decodeErrors++
				continue
			}
			eventCounts[envelope.Type]++

			var event responsesEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// One unexpected field type must not throw away an entire text
				// delta, so keep the fields that matter and carry on.
				decodeErrors++
				event = responsesEvent{
					Type:    envelope.Type,
					Delta:   stringField(data, "delta"),
					Text:    stringField(data, "text"),
					Refusal: stringField(data, "refusal"),
					ItemID:  stringField(data, "item_id"),
				}
			}

			switch event.Type {
			case "response.output_item.added":
				if event.Item != nil {
					outputByIndex[event.OutputIndex] = *event.Item
					itemStarted[event.OutputIndex] = time.Now()
				}

			case "response.output_item.done":
				if event.Item == nil {
					continue
				}
				item := *event.Item
				outputByIndex[event.OutputIndex] = item
				start := itemStarted[event.OutputIndex]
				if start.IsZero() {
					start = time.Now()
				}

				if item.Type == "reasoning" {
					if !reasoningStreamed[event.OutputIndex] {
						if text := item.reasoningText(); text != "" {
							reasoningByIndex[event.OutputIndex] = text
							if !send("reasoning", reasoningEvent{
								Text:        text,
								ItemID:      item.ID,
								OutputIndex: event.OutputIndex,
							}) {
								break read
							}
						}
					}
					if text := strings.TrimSpace(reasoningByIndex[event.OutputIndex]); text != "" {
						pendingReasoning = append(pendingReasoning, text)
					}
					continue
				}

				if !isResponseToolType(item.Type) {
					continue
				}
				step := buildResponseStep(&item, strings.Join(pendingReasoning, "\n\n"),
					citationMap(citations), time.Since(start))
				pendingReasoning = nil
				streamedSteps = append(streamedSteps, step)
				if !send("step", stepEvent{Step: step}) {
					break read
				}

			case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
				if event.Delta == "" {
					continue
				}
				reasoningStreamed[event.OutputIndex] = true
				reasoningByIndex[event.OutputIndex] += event.Delta
				if !send("reasoning", reasoningEvent{
					Text:        event.Delta,
					ItemID:      event.ItemID,
					OutputIndex: event.OutputIndex,
				}) {
					break read
				}

			case "response.output_text.delta":
				if event.Delta == "" {
					continue
				}
				content.WriteString(event.Delta)
				if !send("delta", deltaEvent{Text: event.Delta}) {
					break read
				}

			case "response.refusal.delta":
				if event.Delta == "" {
					continue
				}
				content.WriteString(event.Delta)
				if !send("delta", deltaEvent{Text: event.Delta}) {
					break read
				}

			case "response.output_text.done":
				if event.Text != "" {
					doneText = event.Text
				}

			case "response.refusal.done":
				if event.Refusal != "" {
					doneText = event.Refusal
				}

			case "response.content_part.done":
				if event.Part != nil {
					switch {
					case event.Part.Text != "":
						doneText = event.Part.Text
					case event.Part.Refusal != "":
						doneText = event.Part.Refusal
					}
				}

			case "response.output_text.annotation.added":
				if event.Annotation == nil {
					continue
				}
				citation, changed := addCitation(*event.Annotation)
				if changed && !send("citation", citationEvent{Citations: []Citation{citation}}) {
					break read
				}

			case "response.completed":
				terminalSeen = true
				completed = event.Response
				if completed == nil {
					continue
				}
				capturedUsage = completed.Usage
				switch {
				case completed.IncompleteDetails != nil && completed.IncompleteDetails.Reason != "":
					finishReason = completed.IncompleteDetails.Reason
				case completed.Status != "":
					finishReason = completed.Status
				}
				if completed.Error != nil {
					streamErr = truncate(completed.Error.Message, 300)
				}
				for index, item := range completed.Output {
					outputByIndex[index] = item
				}

			case "response.incomplete", "response.failed":
				terminalSeen = true
				if event.Response != nil {
					capturedUsage = event.Response.Usage
					if event.Response.Error != nil {
						streamErr = truncate(event.Response.Error.Message, 300)
					}
					if event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason != "" {
						finishReason = event.Response.IncompleteDetails.Reason
					}
				}
				if streamErr == "" {
					streamErr = provider.Label + " stopped before the reply was complete."
				}

			case "error":
				if event.Error != nil {
					streamErr = truncate(event.Error.Message, 300)
				}
				if streamErr == "" {
					streamErr = provider.Label + " reported an error mid-stream."
				}
			}
		}

		if readErr != nil {
			if readErr != io.EOF && !clientGone && !stalled.Load() && r.Context().Err() == nil && upCtx.Err() == nil {
				streamErr = "The connection to " + provider.Label + " dropped."
			}
			break
		}
	}

	if r.Context().Err() != nil {
		clientGone = true
	}
	timedOut := !clientGone && !stalled.Load() && errors.Is(upCtx.Err(), context.DeadlineExceeded)
	// A clean EOF from OpenRouter without any terminal event means the stream
	// was truncated, not finished. Say so instead of presenting a partial reply
	// as complete.
	if !clientGone && !timedOut && !stalled.Load() && !terminalSeen && streamErr == "" {
		streamErr = "The connection to " + provider.Label + " closed before the reply finished."
	}

	indices := make([]int, 0, len(outputByIndex))
	for index := range outputByIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	orderedOutput := make([]responseItem, 0, len(indices))
	for _, index := range indices {
		orderedOutput = append(orderedOutput, outputByIndex[index])
	}

	text := content.String()
	if final := lastResponseMessageText(orderedOutput); final != "" {
		text = final
	}
	if strings.TrimSpace(text) == "" && strings.TrimSpace(doneText) != "" {
		text = doneText
	}
	if strings.TrimSpace(text) == "" && completed != nil && strings.TrimSpace(completed.OutputText) != "" {
		text = completed.OutputText
	}
	steps, finalReasoning := buildResponseTranscript(orderedOutput, reasoningByIndex, citations)
	if len(steps) == 0 && len(streamedSteps) > 0 {
		steps = streamedSteps
	}
	toolRun := buildToolRun(toolsEnabled, toolCalls, capturedUsage, activitiesFromSteps(steps))
	saved := false
	// A reasoning-only reply is still a real reply; keep it instead of discarding
	// it and telling the user that OpenRouter returned nothing.
	if text != "" || len(steps) > 0 || finalReasoning != "" {
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.store.saveAssistant(saveCtx, profile.ID, conv.ID, assistantID,
			text, finalReasoning, steps, citations, toolRun, replaced); err != nil {
			s.log.Error("persist assistant reply", "err", err, "conversation", conv.ID)
		} else {
			saved = true
		}
		saveCancel()
	}

	elapsed := time.Since(startedAt).Round(time.Millisecond)
	s.log.Info("chat completed",
		"conversation", conv.ID,
		"provider", provider.ID,
		"model", model,
		"chars", len(text),
		"reasoningChars", len(finalReasoning),
		"finish", finishReason,
		"streamed", !clientGone,
		"elapsed", elapsed.String(),
		"events", eventSummary(eventCounts),
		"decodeErrors", decodeErrors,
	)
	if text == "" && len(steps) == 0 {
		// Nothing renderable came back. The event histogram above is what makes
		// this diagnosable from the server log after the fact.
		s.log.Warn("reply had no text",
			"conversation", conv.ID,
			"model", model,
			"finish", finishReason,
			"reasoningChars", len(finalReasoning),
			"events", eventSummary(eventCounts),
		)
	}

	if clientGone {
		return
	}

	if stalled.Load() {
		_ = sse.event("error", errorEvent{
			Code:    "upstream_stalled",
			Message: provider.Label + " stopped sending data. Any partial reply was kept.",
		})
		return
	}
	if timedOut {
		_ = sse.event("error", errorEvent{
			Code:    "timeout",
			Message: "The reply took too long and was stopped. Any partial reply was kept.",
		})
		return
	}
	if streamErr != "" {
		_ = sse.event("error", errorEvent{Code: "provider_error", Message: streamErr})
		return
	}

	_ = sse.event("done", doneEvent{
		FinishReason: finishReason,
		Usage:        capturedUsage,
		Saved:        saved,
		Content:      text,
		Reasoning:    finalReasoning,
		Steps:        steps,
		Citations:    citations,
		ToolRun:      toolRun,
	})
}

// buildSystemPrompt joins the chat's instructions with the profile-level user
// context. User context applies to every chat; the assistant prompt stays
// per-conversation (new chats inherit the profile default).
func buildSystemPrompt(convPrompt, assistantName, userName, userPersona string) string {
	parts := make([]string, 0, 3)
	prompt := strings.TrimSpace(convPrompt)
	if name := strings.TrimSpace(assistantName); name != "" &&
		!strings.Contains(strings.ToLower(prompt), strings.ToLower(name)) {
		parts = append(parts, "Your name is "+name+".")
	}
	if prompt != "" {
		parts = append(parts, prompt)
	}

	context := make([]string, 0, 2)
	if name := strings.TrimSpace(userName); name != "" {
		context = append(context, "The user's name is "+name+".")
	}
	if persona := strings.TrimSpace(userPersona); persona != "" {
		context = append(context, persona)
	}
	if len(context) > 0 {
		parts = append(parts, "## About the user\n"+strings.Join(context, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func conversationProviderPrefs(conv *Conversation, ignored []string) *providerPreferences {
	prefs := &providerPreferences{Ignore: ignored}

	if conv != nil {
		if conv.ProviderOnly != "" && !endpointIgnored(Endpoint{Tag: conv.ProviderOnly}, ignored) {
			allow := conv.AllowFallbacks
			prefs.Only = []string{conv.ProviderOnly}
			prefs.AllowFallbacks = &allow
			return prefs
		}
		if conv.ProviderSort != "" {
			prefs.Sort = conv.ProviderSort
		}
	}

	if len(prefs.Ignore) == 0 && prefs.Sort == "" {
		return nil
	}
	return prefs
}

func (s *Server) handleUpstreamStatus(w http.ResponseWriter, resp *http.Response, provider *Provider) {
	detail := upstreamErrorMessage(resp)
	label := "OpenRouter"
	if provider != nil && provider.Label != "" {
		label = provider.Label
	}
	local := provider != nil && provider.Local

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		retry := parseRetryAfter(resp.Header.Get("Retry-After"))
		s.log.Warn("upstream rate limited", "provider", label, "status", resp.StatusCode, "retryAfter", retry)
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			label+" is rate limiting requests. Try again shortly.", retry)
	case http.StatusUnauthorized, http.StatusForbidden:
		s.log.Error("upstream rejected credentials", "provider", label, "status", resp.StatusCode)
		message := label + " rejected the server's API key."
		if local {
			message = label + " rejected the API key saved for this backend."
		}
		writeError(w, http.StatusBadGateway, "provider_auth", message, 0)
	case http.StatusPaymentRequired:
		s.log.Warn("upstream payment required", "provider", label, "detail", detail)
		writeError(w, http.StatusPaymentRequired, "provider_credits",
			label+" reports insufficient credits.", 0)
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		s.log.Warn("upstream rejected request", "provider", label, "status", resp.StatusCode, "detail", detail)
		message := label + " rejected the request."
		if detail != "" {
			message = label + ": " + detail
		}
		writeError(w, http.StatusBadRequest, "provider_rejected", message, 0)
	default:
		s.log.Error("upstream error", "provider", label, "status", resp.StatusCode, "detail", detail)
		writeError(w, http.StatusBadGateway, "provider_error", label+" returned an error.", 0)
	}
}

type sseWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// event writes one SSE frame and flushes on the terminating blank line. Without
// the flush the browser shows nothing until the entire generation is done.
func (s *sseWriter) event(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	return s.rc.Flush()
}

// parseSSELine returns a data payload and whether the stream is finished.
// Comments (": OPENROUTER PROCESSING"), blank lines and other fields are
// ignored by returning an empty payload.
func parseSSELine(line string) (data string, done bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" || line[0] == ':' || !strings.HasPrefix(line, "data:") {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "[DONE]" {
		return "", true
	}
	return payload, false
}

// eventSummary renders an SSE event histogram as a single bounded log field.
func eventSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", name, counts[name]))
	}
	return truncate(strings.Join(parts, " "), 600)
}

func parseRetryAfter(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultRetryAfter
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return clampRetry(secs)
	}
	if when, err := http.ParseTime(value); err == nil {
		return clampRetry(int(time.Until(when).Seconds()))
	}
	return defaultRetryAfter
}

func clampRetry(secs int) int {
	if secs < 1 {
		return 1
	}
	if secs > 120 {
		return 120
	}
	return secs
}
