package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type testEnv struct {
	s       *Server
	profile *Profile
}

func newTestServer(t *testing.T, upstream *httptest.Server) *Server {
	t.Helper()
	cfg := &Config{
		APIKey:       "test-key",
		BaseURL:      upstream.URL,
		Referer:      "http://clachan.test",
		AppTitle:     "Clachan Test",
		DefaultModel: "test/model",
	}
	s := &Server{
		cfg:       cfg,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		static:    os.DirFS("static"),
		store:     newTestStore(t),
		providers: newProviders(cfg),
		client:    upstream.Client(),
		limiter:   rate.NewLimiter(rate.Inf, 1),
		sem:       make(chan struct{}, maxStreams),
		hashSem:   make(chan struct{}, 2),
		logins:    newLoginLimiter(),
		started:   time.Now(),
	}
	s.models = newModelCache(upstream.Client(), s.log)
	return s
}

func newTestEnv(t *testing.T, upstream *httptest.Server) *testEnv {
	t.Helper()
	s := newTestServer(t, upstream)
	profile := createTestProfile(t, s.store, "Test User", true)
	return &testEnv{s: s, profile: profile}
}

func postChat(t *testing.T, s *Server, profile *Profile, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	if profile != nil {
		req = req.WithContext(context.WithValue(req.Context(), profileContextKey{}, profile))
	}
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)
	return rec
}

/* --------------------------------------------------------- responses mocks */

func emitEvent(w http.ResponseWriter, payload any) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func messageItem(text string) map[string]any {
	return map[string]any{
		"id":     "msg_1",
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []any{
			map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
		},
	}
}

func completedEvent(output []any) map[string]any {
	return map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "resp_1",
			"status": "completed",
			"model":  "test/model",
			"output": output,
			"usage": map[string]any{
				"input_tokens":  3,
				"output_tokens": 3,
				"total_tokens":  6,
			},
		},
	}
}

func emitTextResponse(w http.ResponseWriter, text string, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	emitEvent(w, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{},
		},
	})
	for _, chunk := range chunks {
		emitEvent(w, map[string]any{
			"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "delta": chunk,
		})
	}
	item := messageItem(text)
	emitEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
	emitEvent(w, completedEvent([]any{item}))
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestChatStreamsAndPersists(t *testing.T) {
	var gotAuth, gotReferer, gotPath, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		fmt.Fprint(w, ": OPENROUTER PROCESSING\n\n")
		emitTextResponse(w, "Hello, world", []string{"Hello", ", ", "world"})
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, err := env.s.store.createConversation(context.Background(), env.profile.ID, providerOpenRouter, "test/model", "be terse", "price", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	body := fmt.Sprintf(`{"conversationId":%q,"provider":"openrouter","model":"test/model","content":"  say hello  "}`, conv.ID)
	rec := postChat(t, env.s, env.profile, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	if gotPath != "/responses" {
		t.Fatalf("upstream path = %q, want /responses", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotReferer != "http://clachan.test" {
		t.Fatalf("HTTP-Referer = %q", gotReferer)
	}
	for _, want := range []string{
		`"stream":true`,
		`"instructions":"be terse"`,
		`"role":"user"`,
		`"content":"say hello"`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("upstream body missing %q: %s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, `"role":"system"`) {
		t.Fatalf("system prompt must be sent as instructions: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"provider":{"sort":"price"}`) {
		t.Fatalf("upstream body missing provider sort: %s", gotBody)
	}

	out := rec.Body.String()
	for _, want := range []string{"event: meta", "event: delta", "event: done", `"text":"Hello"`, `"saved":true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("response missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "event: meta") > strings.Index(out, "event: delta") {
		t.Fatalf("meta must precede deltas:\n%s", out)
	}

	saved, err := env.s.store.getConversation(context.Background(), env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %d, want 2: %+v", len(saved.Messages), saved.Messages)
	}
	if saved.Messages[0].Content != "say hello" {
		t.Fatalf("user content = %q", saved.Messages[0].Content)
	}
	if saved.Messages[1].Role != "assistant" || saved.Messages[1].Content != "Hello, world" {
		t.Fatalf("assistant = %+v", saved.Messages[1])
	}
	if saved.Title != "say hello" {
		t.Fatalf("title = %q", saved.Title)
	}
}

func TestChatSendsAndPersistsAttachments(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		emitTextResponse(w, "seen", []string{"seen"})
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, err := env.s.store.createConversation(context.Background(), env.profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	image := Attachment{
		Kind: "image",
		Name: "photo.jpg",
		MIME: "image/jpeg",
		Data: testDataURL("image/jpeg", []byte("image bytes")),
	}
	pdf := Attachment{
		Kind: "pdf",
		Name: "report.pdf",
		MIME: "application/pdf",
		Data: testDataURL("application/pdf", []byte("%PDF-1.4")),
	}
	body, _ := json.Marshal(map[string]any{
		"conversationId": conv.ID,
		"model":          "test/model",
		"content":        "what is this?",
		"attachments":    []Attachment{image, pdf},
	})

	rec := postChat(t, env.s, env.profile, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	for _, want := range []string{
		`"type":"input_image"`,
		`"image_url":"` + image.Data + `"`,
		`"type":"input_file"`,
		`"filename":"report.pdf"`,
		`"file_data":"` + pdf.Data + `"`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("upstream body missing %q:\n%s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, `"plugins"`) {
		t.Fatalf("Responses request must not use the Chat Completions file-parser plugin: %s", gotBody)
	}

	saved, err := env.s.store.getConversation(context.Background(), env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %+v", saved.Messages)
	}
	if len(saved.Messages[0].Attachments) != 2 ||
		saved.Messages[0].Attachments[0].Name != "photo.jpg" ||
		saved.Messages[0].Attachments[1].Name != "report.pdf" {
		t.Fatalf("persisted attachments = %+v", saved.Messages[0].Attachments)
	}
}

func TestChatSendsServerToolsAndCapturesCitations(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		w.Header().Set("Content-Type", "text/event-stream")
		reasoning := map[string]any{
			"id":      "rs_1",
			"type":    "reasoning",
			"status":  "completed",
			"summary": []any{map[string]any{"type": "summary_text", "text": "Need a search."}},
		}
		search := map[string]any{
			"id":     "ws_1",
			"type":   "openrouter:web_search",
			"status": "completed",
			"action": map[string]any{
				"type":    "search",
				"query":   "example",
				"sources": []any{map[string]any{"type": "url", "url": "https://example.com/a"}},
			},
		}
		message := messageItem("Search answer")

		emitEvent(w, map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item":         map[string]any{"id": "rs_1", "type": "reasoning", "status": "in_progress", "summary": []any{}},
		})
		emitEvent(w, map[string]any{"type": "response.reasoning_summary_text.delta", "output_index": 0, "summary_index": 0, "delta": "Need "})
		emitEvent(w, map[string]any{"type": "response.reasoning_summary_text.delta", "output_index": 0, "summary_index": 0, "delta": "a search."})
		emitEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": reasoning})
		emitEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": search})
		emitEvent(w, map[string]any{
			"type": "response.output_text.annotation.added",
			"annotation": map[string]any{
				"type":         "url_citation",
				"url_citation": map[string]any{"url": "https://example.com/a", "title": "Example result", "content": "An excerpt."},
			},
		})
		emitEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 2, "content_index": 0, "delta": "Search answer"})
		emitEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 2, "item": message})
		completed := completedEvent([]any{reasoning, search, message})
		completed["response"].(map[string]any)["usage"] = map[string]any{
			"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
			"server_tool_use_details": map[string]any{
				"tool_calls_executed": 2, "tool_calls_requested": 2, "web_search_requests": 1,
			},
		}
		emitEvent(w, completed)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, err := env.s.store.createConversation(context.Background(), env.profile.ID,
		providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	body := fmt.Sprintf(`{"conversationId":%q,"content":"search","toolsEnabled":true,"maxToolCalls":7,"timezone":"Europe/London"}`, conv.ID)
	rec := postChat(t, env.s, env.profile, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	for _, want := range []string{
		`"openrouter:datetime"`,
		`"openrouter:web_search"`,
		`"openrouter:web_fetch"`,
		`"openrouter:experimental__search_models"`,
		`"max_tool_calls":7`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("upstream body missing %q:\n%s", want, gotBody)
		}
	}

	out := rec.Body.String()
	for _, want := range []string{"event: reasoning", "event: step", "event: citation", "Example result", `"toolRun"`, `"steps"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("response missing %q:\n%s", want, out)
		}
	}

	saved, err := env.s.store.getConversation(context.Background(), env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %d", len(saved.Messages))
	}
	reply := saved.Messages[1]
	if len(reply.Citations) != 1 || reply.Citations[0].URL != "https://example.com/a" ||
		reply.Citations[0].Excerpt != "An excerpt." {
		t.Fatalf("citations = %+v", reply.Citations)
	}
	if len(reply.Steps) != 1 {
		t.Fatalf("steps = %+v", reply.Steps)
	}
	step := reply.Steps[0]
	if step.Tool != "web_search" || step.Query != "example" || step.Reasoning != "Need a search." {
		t.Fatalf("step = %+v", step)
	}
	if len(step.Sources) != 1 || step.Sources[0].Title != "Example result" {
		t.Fatalf("step sources = %+v", step.Sources)
	}
	if reply.ToolRun == nil || reply.ToolRun.Calls != 2 || reply.ToolRun.Searches != 1 ||
		len(reply.ToolRun.Activities) != 1 || reply.ToolRun.Activities[0].Name != "web_search" {
		t.Fatalf("tool run = %+v", reply.ToolRun)
	}
}

func TestChatSendsReasoningEffort(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		emitTextResponse(w, "ok", []string{"ok"})
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	env.s.models.models = map[string][]Model{
		providerOpenRouter: {{
			ID:                "test/model",
			SupportsReasoning: true,
			Reasoning: &ReasoningInfo{
				SupportedEfforts: []string{"high", "medium", "low"},
				DefaultEffort:    "medium",
				DefaultEnabled:   true,
			},
		}},
	}
	conv, err := env.s.store.createConversation(context.Background(), env.profile.ID,
		providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(
		`{"conversationId":%q,"content":"think","reasoningEffort":"low"}`, conv.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotBody, `"reasoning":{"effort":"low","summary":"auto"}`) {
		t.Fatalf("upstream body missing reasoning: %s", gotBody)
	}

	saved, err := env.s.store.getConversation(context.Background(), env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if saved.ReasoningEffort != "low" {
		t.Fatalf("saved effort = %q", saved.ReasoningEffort)
	}
}

func TestChatRejectsUnknownReasoningEffort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for an unknown effort")
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(context.Background(), env.profile.ID,
		providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(
		`{"conversationId":%q,"content":"think","reasoningEffort":"turbo"}`, conv.ID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatWebToolsCanBeDisabled(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		emitTextResponse(w, "ok", []string{"ok"})
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(context.Background(), env.profile.ID,
		providerOpenRouter, "test/model", "", "", "", true, false, 4, "")

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(`{"conversationId":%q,"content":"hi"}`, conv.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(gotBody, "openrouter:web_search") || strings.Contains(gotBody, "openrouter:web_fetch") {
		t.Fatalf("web tools present while disabled:\n%s", gotBody)
	}
	if !strings.Contains(gotBody, `"openrouter:datetime"`) || !strings.Contains(gotBody, `"max_tool_calls":4`) {
		t.Fatalf("always-on tools missing:\n%s", gotBody)
	}
}

func TestChatUpstreamRateLimitDoesNotPersist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"slow down","code":429}}`)
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(context.Background(), env.profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(`{"conversationId":%q,"content":"hi"}`, conv.ID))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3", got)
	}
	if !strings.Contains(rec.Body.String(), `"rate_limited"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	// Nothing may be persisted: the retry would duplicate the user turn.
	saved, _ := env.s.store.getConversation(context.Background(), env.profile.ID, conv.ID)
	if len(saved.Messages) != 0 {
		t.Fatalf("messages after 429 = %+v", saved.Messages)
	}
}

func TestChatBusyReturns503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called while saturated")
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	env.s.sem <- struct{}{}
	env.s.sem <- struct{}{}

	rec := postChat(t, env.s, env.profile, `{"conversationId":"anything","content":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"busy"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestChatRegenerateReplacesReply(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emitTextResponse(w, "fresh reply", []string{"fresh reply"})
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(ctx, env.profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if _, _, err := env.s.store.appendUserMessage(ctx, env.profile.ID, conv.ID, providerOpenRouter, "test/model", "question", nil); err != nil {
		t.Fatalf("append user: %v", err)
	}
	oldID := newID()
	if err := env.s.store.saveAssistant(ctx, env.profile.ID, conv.ID, oldID, "stale reply", "", nil, nil, nil, false); err != nil {
		t.Fatalf("save assistant: %v", err)
	}

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(`{"conversationId":%q,"action":"regenerate"}`, conv.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved, _ := env.s.store.getConversation(ctx, env.profile.ID, conv.ID)
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %+v", saved.Messages)
	}
	if saved.Messages[1].ID != oldID {
		t.Fatalf("assistant id changed: %q != %q", saved.Messages[1].ID, oldID)
	}
	if saved.Messages[1].Content != "fresh reply" {
		t.Fatalf("assistant content = %q", saved.Messages[1].Content)
	}
}

func TestChatEditTruncatesAndResends(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emitTextResponse(w, "answer to edited", []string{"answer to edited"})
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(ctx, env.profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	first, _, _ := env.s.store.appendUserMessage(ctx, env.profile.ID, conv.ID, providerOpenRouter, "test/model", "original", nil)
	_, _, _ = env.s.store.appendUserMessage(ctx, env.profile.ID, conv.ID, providerOpenRouter, "test/model", "follow up", nil)
	_ = env.s.store.saveAssistant(ctx, env.profile.ID, conv.ID, newID(), "old answer", "", nil, nil, nil, false)

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(
		`{"conversationId":%q,"action":"edit","messageId":%q,"content":"edited"}`, conv.ID, first.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved, _ := env.s.store.getConversation(ctx, env.profile.ID, conv.ID)
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %+v", saved.Messages)
	}
	if saved.Messages[0].ID != first.ID || saved.Messages[0].Content != "edited" {
		t.Fatalf("edited message = %+v", saved.Messages[0])
	}
	if saved.Messages[1].Content != "answer to edited" {
		t.Fatalf("assistant = %+v", saved.Messages[1])
	}
}

func TestChatRequiresProfile(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called without a profile")
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	rec := postChat(t, env.s, nil, `{"conversationId":"x","content":"hi"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatRejectsAnotherProfilesConversation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for a foreign conversation")
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(ctx, env.profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	other := createTestProfile(t, env.s.store, "Other", false)

	rec := postChat(t, env.s, other, fmt.Sprintf(`{"conversationId":%q,"content":"steal"}`, conv.ID))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatSpecificProviderRouting(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		emitTextResponse(w, "ok", []string{"ok"})
	}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)
	conv, err := env.s.store.createConversation(context.Background(), env.profile.ID,
		providerOpenRouter, "test/model", "", "", "deepinfra/turbo", false, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(`{"conversationId":%q,"content":"route"}`, conv.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotBody, `"provider":{"only":["deepinfra/turbo"],"allow_fallbacks":false}`) {
		t.Fatalf("upstream body missing provider only: %s", gotBody)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	got := buildSystemPrompt("Be precise.", "Clachan", "Ada", "Works on embedded systems.")
	want := "Your name is Clachan.\n\nBe precise.\n\n## About the user\nThe user's name is Ada.\nWorks on embedded systems."
	if got != want {
		t.Fatalf("buildSystemPrompt = %q, want %q", got, want)
	}

	// Do not duplicate a name already present in the instructions.
	got = buildSystemPrompt("You are Clachan, a precise assistant.", "Clachan", "", "")
	if got != "You are Clachan, a precise assistant." {
		t.Fatalf("duplicate name = %q", got)
	}
	if got := buildSystemPrompt("", "", "", "Prefers short answers."); got != "## About the user\nPrefers short answers." {
		t.Fatalf("user-only prompt = %q", got)
	}
	if got := buildSystemPrompt("", "", "", ""); got != "" {
		t.Fatalf("empty prompt = %q", got)
	}
}

func TestConversationProviderPrefsIgnoresPinnedIgnoredProvider(t *testing.T) {
	conv := &Conversation{ProviderOnly: "deepinfra/turbo", AllowFallbacks: false}

	prefs := conversationProviderPrefs(conv, []string{"DeepInfra"})
	if prefs == nil || len(prefs.Only) != 0 {
		t.Fatalf("ignored pin still sent: %+v", prefs)
	}
	if len(prefs.Ignore) != 1 || prefs.Ignore[0] != "DeepInfra" {
		t.Fatalf("ignore list = %+v", prefs.Ignore)
	}

	prefs = conversationProviderPrefs(conv, []string{"groq"})
	if len(prefs.Only) != 1 || prefs.Only[0] != "deepinfra/turbo" || prefs.AllowFallbacks == nil || *prefs.AllowFallbacks {
		t.Fatalf("pinned prefs = %+v", prefs)
	}

	if prefs := conversationProviderPrefs(&Conversation{}, nil); prefs != nil {
		t.Fatalf("empty prefs = %+v", prefs)
	}
}

func TestParseSSELine(t *testing.T) {
	cases := []struct {
		in   string
		data string
		done bool
	}{
		{"data: {\"a\":1}\n", `{"a":1}`, false},
		{"data: {\"a\":1}\r\n", `{"a":1}`, false},
		{"data:[DONE]\n", "", true},
		{"data: [DONE]\r\n", "", true},
		{": OPENROUTER PROCESSING\n", "", false},
		{"\n", "", false},
		{"event: meta\n", "", false},
		{"data:  spaced  \n", "spaced", false},
	}
	for _, tc := range cases {
		data, done := parseSSELine(tc.in)
		if data != tc.data || done != tc.done {
			t.Errorf("parseSSELine(%q) = (%q, %v), want (%q, %v)", tc.in, data, done, tc.data, tc.done)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7 {
		t.Errorf("seconds = %d", got)
	}
	if got := parseRetryAfter("0"); got != 1 {
		t.Errorf("clamp low = %d", got)
	}
	if got := parseRetryAfter("9999"); got != 120 {
		t.Errorf("clamp high = %d", got)
	}
	if got := parseRetryAfter(""); got != defaultRetryAfter {
		t.Errorf("empty = %d", got)
	}
	if got := parseRetryAfter("not-a-date"); got != defaultRetryAfter {
		t.Errorf("garbage = %d", got)
	}
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 8 || got > 10 {
		t.Errorf("http date = %d", got)
	}
}
