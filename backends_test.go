package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeBackendURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"http://127.0.0.1:5001", "http://127.0.0.1:5001/v1", false},
		{"http://127.0.0.1:5001/", "http://127.0.0.1:5001/v1", false},
		{"http://127.0.0.1:5001/v1", "http://127.0.0.1:5001/v1", false},
		{"http://127.0.0.1:5001/v1/", "http://127.0.0.1:5001/v1", false},
		{"https://box.lan:8443/openai/v1", "https://box.lan:8443/openai/v1", false},
		{"  http://box.lan:1234  ", "http://box.lan:1234/v1", false},
		{"", "", true},
		{"box.lan:1234", "", true},
		{"ftp://box.lan", "", true},
		{"http://", "", true},
		{"http://user:pass@box.lan", "", true},
		{"http://box.lan/v1?x=1", "", true},
		{"http://box.lan/v1#frag", "", true},
		{"http://box.lan/../etc", "", true},
		{strings.Repeat("a", 400), "", true},
	}
	for _, tc := range cases {
		got, err := normalizeBackendURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeBackendURL(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeBackendURL(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeBackendURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidLocalModelID(t *testing.T) {
	valid := []string{
		"llama3.1:8b",
		"qwen2.5-coder:7b-instruct-q4_K_M",
		"mistral",
		"Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf",
		"org/model",
		"model (Q4_K_M)",
		"model [q4]",
		"a+b@c~d#e",
	}
	for _, id := range valid {
		if !validLocalModelID(id) {
			t.Errorf("validLocalModelID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",
		"   ",
		"/leading",
		"trailing/",
		"has\nnewline",
		"has\ttab",
		"quote\"mark",
		strings.Repeat("a", 201),
	}
	for _, id := range invalid {
		if validLocalModelID(id) {
			t.Errorf("validLocalModelID(%q) = true, want false", id)
		}
	}
}

func TestLocalChatMessagesFlattensAttachments(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "earlier question"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "system", Content: "ignored"},
	}
	attachments := []Attachment{
		{Kind: "text", Name: "notes.txt", Data: testDataURL("text/plain", []byte("hello file"))},
		{Kind: "image", Name: "photo.png", Data: testDataURL("image/png", []byte("img"))},
	}

	msgs := localChatMessages("be terse", history, "current question", attachments, true)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" || msgs[0].Content != "be terse" {
		t.Fatalf("system message = %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "earlier question" {
		t.Fatalf("history user = %+v", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "earlier answer" {
		t.Fatalf("history assistant = %+v", msgs[2])
	}
	last, ok := msgs[3].Content.(string)
	if !ok {
		t.Fatalf("user content type = %T", msgs[4].Content)
	}
	for _, want := range []string{"current question", "hello file", "Image attachment omitted"} {
		if !strings.Contains(last, want) {
			t.Fatalf("user content missing %q: %s", want, last)
		}
	}

	// Regenerate must not append the user turn again.
	msgs = localChatMessages("", history, "current question", nil, false)
	if len(msgs) != 2 {
		t.Fatalf("regenerate messages = %d, want 2: %+v", len(msgs), msgs)
	}
}

func localEventTypes(t *testing.T, out string) []string {
	t.Helper()
	var types []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &envelope); err != nil {
			t.Fatalf("bad event %q: %v", line, err)
		}
		types = append(types, envelope.Type)
	}
	return types
}

func runLocalTranslator(t *testing.T, input string) string {
	t.Helper()
	pr, pw := io.Pipe()
	go translateLocalStream(context.Background(), "Test", "local-model", strings.NewReader(input), pw)
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("read translated stream: %v", err)
	}
	return string(out)
}

func TestTranslateLocalStreamSSE(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"thinking "}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":", world"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	out := runLocalTranslator(t, input)
	types := localEventTypes(t, out)
	want := []string{
		"response.reasoning_summary_text.delta",
		"response.output_text.delta",
		"response.output_text.delta",
		"response.output_item.done",
		"response.output_item.done",
		"response.completed",
	}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v\n%s", types, want, out)
	}
	for _, wantText := range []string{"thinking ", "Hello", ", world", `"status":"completed"`} {
		if !strings.Contains(out, wantText) {
			t.Fatalf("output missing %q:\n%s", wantText, out)
		}
	}
}

func TestTranslateLocalStreamJSONFallback(t *testing.T) {
	input := `{"choices":[{"message":{"content":"whole answer"},"finish_reason":"stop"}]}`
	out := runLocalTranslator(t, input)
	types := localEventTypes(t, out)
	if len(types) == 0 || types[len(types)-1] != "response.completed" {
		t.Fatalf("event types = %v\n%s", types, out)
	}
	if !strings.Contains(out, "whole answer") {
		t.Fatalf("output missing text:\n%s", out)
	}
}

func TestTranslateLocalStreamTruncated(t *testing.T) {
	input := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"
	out := runLocalTranslator(t, input)
	types := localEventTypes(t, out)
	for _, typ := range types {
		if typ == "response.completed" {
			t.Fatalf("truncated stream must not complete:\n%s", out)
		}
	}
	if !strings.Contains(out, "partial") {
		t.Fatalf("partial text lost:\n%s", out)
	}
}

func TestTranslateLocalStreamLengthFinish(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"cut"}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		``,
	}, "\n")
	out := runLocalTranslator(t, input)
	if !strings.Contains(out, `"status":"incomplete"`) ||
		!strings.Contains(out, `"reason":"max_output_tokens"`) {
		t.Fatalf("length finish not mapped:\n%s", out)
	}
}

func TestStoreProfileBackends(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	owner := createTestProfile(t, s, "Owner", true)
	other := createTestProfile(t, s, "Other", false)

	saved := ProfileBackend{
		BackendID: backendOllama,
		Label:     "Desk Ollama",
		BaseURL:   "http://127.0.0.1:11434/v1",
		APIKey:    "secret",
		Model:     "llama3.1:8b",
	}
	if err := s.saveProfileBackend(ctx, owner.ID, saved); err != nil {
		t.Fatalf("saveProfileBackend: %v", err)
	}

	got, err := s.profileBackend(ctx, owner.ID, backendOllama)
	if err != nil {
		t.Fatalf("profileBackend: %v", err)
	}
	if got.Label != saved.Label || got.BaseURL != saved.BaseURL || got.APIKey != saved.APIKey || got.Model != saved.Model {
		t.Fatalf("backend = %+v", got)
	}

	// Another profile must not see it.
	if _, err := s.profileBackend(ctx, other.ID, backendOllama); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other profile read = %v, want ErrNotFound", err)
	}
	list, err := s.listProfileBackends(ctx, other.ID)
	if err != nil {
		t.Fatalf("listProfileBackends: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("other profile list = %+v", list)
	}

	// Upsert replaces the row rather than duplicating it.
	saved.BaseURL = "http://192.168.1.20:11434/v1"
	saved.APIKey = ""
	if err := s.saveProfileBackend(ctx, owner.ID, saved); err != nil {
		t.Fatalf("saveProfileBackend update: %v", err)
	}
	list, err = s.listProfileBackends(ctx, owner.ID)
	if err != nil {
		t.Fatalf("listProfileBackends: %v", err)
	}
	if len(list) != 1 || list[0].BaseURL != saved.BaseURL || list[0].APIKey != "" {
		t.Fatalf("updated list = %+v", list)
	}

	if err := s.deleteProfileBackend(ctx, owner.ID, backendOllama); err != nil {
		t.Fatalf("deleteProfileBackend: %v", err)
	}
	if _, err := s.profileBackend(ctx, owner.ID, backendOllama); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete = %v, want ErrNotFound", err)
	}
	if err := s.deleteProfileBackend(ctx, owner.ID, backendOllama); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestProviderForLocalBackend(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	env := newTestEnv(t, upstream)

	if _, err := env.s.providerFor(ctx, env.profile.ID, backendKobold); !errors.Is(err, ErrBackendNotConfigured) {
		t.Fatalf("unconfigured = %v, want ErrBackendNotConfigured", err)
	}
	if _, err := env.s.providerFor(ctx, env.profile.ID, "nope"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown = %v, want ErrUnknownProvider", err)
	}

	if err := env.s.store.saveProfileBackend(ctx, env.profile.ID, ProfileBackend{
		BackendID: backendKobold,
		BaseURL:   "http://127.0.0.1:5001/v1",
		APIKey:    "k",
		Model:     "kobold-model",
	}); err != nil {
		t.Fatalf("saveProfileBackend: %v", err)
	}

	provider, err := env.s.providerFor(ctx, env.profile.ID, backendKobold)
	if err != nil {
		t.Fatalf("providerFor: %v", err)
	}
	if !provider.Local || provider.Wire != "chat" || provider.BaseURL != "http://127.0.0.1:5001/v1" ||
		provider.APIKey != "k" || provider.DefaultModel != "kobold-model" {
		t.Fatalf("provider = %+v", provider)
	}
	if !provider.enabled() {
		t.Fatal("configured local provider must be enabled")
	}
}

func TestChatLocalBackend(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"local \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	if err := env.s.store.saveProfileBackend(ctx, env.profile.ID, ProfileBackend{
		BackendID: backendOllama,
		Label:     "Desk Ollama",
		BaseURL:   upstream.URL + "/v1",
		APIKey:    "local-key",
		Model:     "llama3.1:8b",
	}); err != nil {
		t.Fatalf("saveProfileBackend: %v", err)
	}

	conv, err := env.s.store.createConversation(ctx, env.profile.ID, backendOllama, "llama3.1:8b",
		"be terse", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	text := Attachment{Kind: "text", Name: "notes.txt", MIME: "text/plain", Data: testDataURL("text/plain", []byte("file body"))}
	body, _ := json.Marshal(map[string]any{
		"conversationId":  conv.ID,
		"provider":        backendOllama,
		"model":           "llama3.1:8b",
		"content":         "hello local",
		"attachments":     []Attachment{text},
		"toolsEnabled":    true,
		"maxToolCalls":    9,
		"reasoningEffort": "high",
	})
	rec := postChat(t, env.s, env.profile, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotAuth != "Bearer local-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	for _, want := range []string{
		`"model":"llama3.1:8b"`,
		`"stream":true`,
		`"role":"system"`,
		`"content":"be terse"`,
		`"role":"user"`,
		`hello local`,
		`file body`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("upstream body missing %q:\n%s", want, gotBody)
		}
	}
	for _, unwanted := range []string{`"tools"`, `"reasoning"`, `"provider"`, `"instructions"`, `"max_tool_calls"`} {
		if strings.Contains(gotBody, unwanted) {
			t.Fatalf("local request must not contain %q:\n%s", unwanted, gotBody)
		}
	}

	out := rec.Body.String()
	for _, want := range []string{"event: meta", "event: delta", "event: done", `"saved":true`, "local answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("response missing %q:\n%s", want, out)
		}
	}

	saved, err := env.s.store.getConversation(ctx, env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(saved.Messages) != 2 || saved.Messages[1].Content != "local answer" {
		t.Fatalf("saved messages = %+v", saved.Messages)
	}
	if saved.Provider != backendOllama {
		t.Fatalf("saved provider = %q", saved.Provider)
	}
}

func TestChatLocalRejectsImageAttachment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for a rejected attachment")
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	if err := env.s.store.saveProfileBackend(ctx, env.profile.ID, ProfileBackend{
		BackendID: backendLMStudio,
		BaseURL:   upstream.URL + "/v1",
	}); err != nil {
		t.Fatalf("saveProfileBackend: %v", err)
	}
	conv, _ := env.s.store.createConversation(ctx, env.profile.ID, backendLMStudio, "local-model",
		"", "", "", true, true, defaultToolCalls, "")

	image := Attachment{Kind: "image", Name: "photo.png", MIME: "image/png", Data: testDataURL("image/png", []byte("img"))}
	body, _ := json.Marshal(map[string]any{
		"conversationId": conv.ID,
		"provider":       backendLMStudio,
		"content":        "look",
		"attachments":    []Attachment{image},
	})
	rec := postChat(t, env.s, env.profile, string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "text-only") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestChatLocalKeepsStoredToolSettings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	if err := env.s.store.saveProfileBackend(ctx, env.profile.ID, ProfileBackend{
		BackendID: backendOllama,
		BaseURL:   upstream.URL + "/v1",
	}); err != nil {
		t.Fatalf("saveProfileBackend: %v", err)
	}
	conv, err := env.s.store.createConversation(ctx, env.profile.ID, backendOllama, "llama3.1:8b",
		"", "", "", true, true, 9, "high")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	// The browser sends 0 for local chats because there are no server tools.
	// That must not be rejected, and it must not erase the saved settings.
	body, _ := json.Marshal(map[string]any{
		"conversationId":  conv.ID,
		"provider":        backendOllama,
		"content":         "hi",
		"toolsEnabled":    false,
		"maxToolCalls":    0,
		"reasoningEffort": "",
	})
	rec := postChat(t, env.s, env.profile, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved, err := env.s.store.getConversation(ctx, env.profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if !saved.ToolsEnabled || saved.MaxToolCalls != 9 || saved.ReasoningEffort != "high" {
		t.Fatalf("local chat overwrote tool settings: %+v", saved)
	}
}

func TestChatLocalUnconfiguredBackend(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for an unconfigured backend")
	}))
	defer upstream.Close()

	ctx := context.Background()
	env := newTestEnv(t, upstream)
	conv, _ := env.s.store.createConversation(ctx, env.profile.ID, backendKobold, "kobold-model",
		"", "", "", true, true, defaultToolCalls, "")

	rec := postChat(t, env.s, env.profile, fmt.Sprintf(`{"conversationId":%q,"content":"hi"}`, conv.ID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
