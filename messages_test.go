package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestStoreEditMessageInPlace(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model", "be nice", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	user, _, err := s.appendUserMessage(ctx, profile.ID, conv.ID, providerOpenRouter, "test/model", "original question", nil)
	if err != nil {
		t.Fatalf("appendUserMessage: %v", err)
	}
	assistantID := newID()
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, assistantID, "original answer", "", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant: %v", err)
	}

	title, err := s.editMessage(ctx, profile.ID, user.ID, "edited question")
	if err != nil {
		t.Fatalf("editMessage(user): %v", err)
	}
	if title != "edited question" {
		t.Fatalf("title = %q, want %q", title, "edited question")
	}

	got, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2 (edit must not truncate)", len(got.Messages))
	}
	if got.Messages[0].Content != "edited question" || got.Messages[1].Content != "original answer" {
		t.Fatalf("after user edit = %+v", got.Messages)
	}

	if _, err := s.editMessage(ctx, profile.ID, assistantID, "edited answer"); err != nil {
		t.Fatalf("editMessage(assistant): %v", err)
	}
	got, err = s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation after assistant edit: %v", err)
	}
	if got.Messages[0].Content != "edited question" || got.Messages[1].Content != "edited answer" {
		t.Fatalf("after assistant edit = %+v", got.Messages)
	}

	// A title the user renamed by hand is not overwritten.
	custom := "Custom title"
	if err := s.updateConversation(ctx, profile.ID, conv.ID, ConversationUpdate{Title: &custom}); err != nil {
		t.Fatalf("updateConversation: %v", err)
	}
	if _, err := s.editMessage(ctx, profile.ID, user.ID, "another question"); err != nil {
		t.Fatalf("editMessage after rename: %v", err)
	}
	got, err = s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation after rename: %v", err)
	}
	if got.Title != "Custom title" {
		t.Fatalf("title = %q, want the manual title", got.Title)
	}

	other := createTestProfile(t, s, "Other", false)
	if _, err := s.editMessage(ctx, other.ID, user.ID, "hijack"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit from another profile = %v, want ErrNotFound", err)
	}
	if _, err := s.editMessage(ctx, profile.ID, "missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit missing message = %v, want ErrNotFound", err)
	}
}

func TestStoreBranchConversation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model", "be nice", "price", "", false, true, 9, "high")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	first, _, err := s.appendUserMessage(ctx, profile.ID, conv.ID, providerOpenRouter, "test/model", "first question", nil)
	if err != nil {
		t.Fatalf("appendUserMessage(first): %v", err)
	}
	firstAnswer := newID()
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, firstAnswer, "first answer", "thinking", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant(first): %v", err)
	}
	second, _, err := s.appendUserMessage(ctx, profile.ID, conv.ID, providerOpenRouter, "test/model", "second question", nil)
	if err != nil {
		t.Fatalf("appendUserMessage(second): %v", err)
	}
	secondAnswer := newID()
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, secondAnswer, "second answer", "", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant(second): %v", err)
	}

	branch, err := s.branchConversation(ctx, profile.ID, conv.ID, firstAnswer)
	if err != nil {
		t.Fatalf("branchConversation: %v", err)
	}
	if branch.ID == conv.ID {
		t.Fatal("branch reused the source conversation ID")
	}
	if branch.Title != "Branch: first question" {
		t.Fatalf("branch title = %q", branch.Title)
	}
	if len(branch.Messages) != 2 {
		t.Fatalf("branch messages = %d, want 2", len(branch.Messages))
	}
	if branch.Messages[0].Content != "first question" || branch.Messages[1].Content != "first answer" {
		t.Fatalf("branch messages = %+v", branch.Messages)
	}
	if branch.Messages[0].ID == first.ID || branch.Messages[1].ID == firstAnswer {
		t.Fatal("branch reused source message IDs")
	}
	if branch.Messages[1].Reasoning != "thinking" {
		t.Fatalf("branch reasoning = %q", branch.Messages[1].Reasoning)
	}
	if branch.SystemPrompt != "be nice" || branch.ProviderSort != "price" ||
		branch.AllowFallbacks || !branch.ToolsEnabled || branch.MaxToolCalls != 9 ||
		branch.ReasoningEffort != "high" {
		t.Fatalf("branch settings = %+v", branch)
	}

	// The source conversation is untouched.
	source, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation(source): %v", err)
	}
	if len(source.Messages) != 4 {
		t.Fatalf("source messages = %d, want 4", len(source.Messages))
	}

	// Branching from the last message copies the whole transcript.
	full, err := s.branchConversation(ctx, profile.ID, conv.ID, secondAnswer)
	if err != nil {
		t.Fatalf("branchConversation(full): %v", err)
	}
	if len(full.Messages) != 4 {
		t.Fatalf("full branch messages = %d, want 4", len(full.Messages))
	}

	// Branching from a user message stops there.
	userBranch, err := s.branchConversation(ctx, profile.ID, conv.ID, second.ID)
	if err != nil {
		t.Fatalf("branchConversation(user): %v", err)
	}
	if len(userBranch.Messages) != 3 {
		t.Fatalf("user branch messages = %d, want 3", len(userBranch.Messages))
	}

	if _, err := s.branchConversation(ctx, profile.ID, conv.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("branch from missing message = %v, want ErrNotFound", err)
	}
	other := createTestProfile(t, s, "Other", false)
	if _, err := s.branchConversation(ctx, other.ID, conv.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("branch from another profile = %v, want ErrNotFound", err)
	}
}

func TestBranchTitle(t *testing.T) {
	cases := map[string]string{
		"":              "Branch",
		"New chat":      "Branch",
		"Hello":         "Branch: Hello",
		"Branch: Hello": "Branch: Hello",
	}
	for source, want := range cases {
		if got := branchTitle(source); got != want {
			t.Fatalf("branchTitle(%q) = %q, want %q", source, got, want)
		}
	}

	long := branchTitle(strings.Repeat("x", 300))
	if len([]rune(long)) != 200 {
		t.Fatalf("long branch title has %d runes, want 200", len([]rune(long)))
	}
}

func setupOwnerProfile(t *testing.T, client *http.Client, base string) Profile {
	t.Helper()
	code, data := doJSON(t, client, http.MethodPost, base+"/api/auth/setup",
		map[string]string{"name": "Owner", "password": "correct horse"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("setup = %d, body = %s", code, data)
	}
	var owner Profile
	if err := json.Unmarshal(data, &owner); err != nil {
		t.Fatalf("decode owner: %v", err)
	}
	return owner
}

func TestMessageEditAndBranchHandlers(t *testing.T) {
	ctx := context.Background()
	s, ts := newAuthTestServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	owner := setupOwnerProfile(t, client, ts.URL)

	conv, err := s.store.createConversation(ctx, owner.ID, providerOpenRouter, "test/model", "be nice", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	user, _, err := s.store.appendUserMessage(ctx, owner.ID, conv.ID, providerOpenRouter, "test/model", "first question", nil)
	if err != nil {
		t.Fatalf("appendUserMessage: %v", err)
	}
	assistantID := newID()
	if err := s.store.saveAssistant(ctx, owner.ID, conv.ID, assistantID, "first answer", "", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant: %v", err)
	}

	// Edit the user message in place.
	code, data := doJSON(t, client, http.MethodPatch, ts.URL+"/api/messages/"+user.ID,
		map[string]string{"content": "edited question"}, nil)
	if code != http.StatusOK {
		t.Fatalf("patch user message = %d, body = %s", code, data)
	}
	var edited struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(data, &edited); err != nil {
		t.Fatalf("decode edit response: %v", err)
	}
	if edited.ID != user.ID || edited.Content != "edited question" || edited.Title != "edited question" {
		t.Fatalf("edit response = %+v", edited)
	}

	got, err := s.store.getConversation(ctx, owner.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "edited question" {
		t.Fatalf("after user edit = %+v", got.Messages)
	}

	// Edit the assistant message in place.
	code, data = doJSON(t, client, http.MethodPatch, ts.URL+"/api/messages/"+assistantID,
		map[string]string{"content": "edited answer"}, nil)
	if code != http.StatusOK {
		t.Fatalf("patch assistant message = %d, body = %s", code, data)
	}
	got, err = s.store.getConversation(ctx, owner.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation after assistant edit: %v", err)
	}
	if got.Messages[1].Content != "edited answer" {
		t.Fatalf("after assistant edit = %+v", got.Messages)
	}

	// Empty content and unknown IDs are rejected.
	code, _ = doJSON(t, client, http.MethodPatch, ts.URL+"/api/messages/"+user.ID,
		map[string]string{"content": "   "}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("empty edit = %d, want 400", code)
	}
	code, _ = doJSON(t, client, http.MethodPatch, ts.URL+"/api/messages/missing",
		map[string]string{"content": "x"}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("missing edit = %d, want 404", code)
	}

	// Branch from the assistant message.
	code, data = doJSON(t, client, http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/branch",
		map[string]string{"messageId": assistantID}, nil)
	if code != http.StatusCreated {
		t.Fatalf("branch = %d, body = %s", code, data)
	}
	var branch Conversation
	if err := json.Unmarshal(data, &branch); err != nil {
		t.Fatalf("decode branch: %v", err)
	}
	if branch.ID == conv.ID || branch.Title != "Branch: edited question" {
		t.Fatalf("branch = %+v", branch)
	}
	if len(branch.Messages) != 2 {
		t.Fatalf("branch messages = %d, want 2", len(branch.Messages))
	}
	if branch.Messages[0].Content != "edited question" || branch.Messages[1].Content != "edited answer" {
		t.Fatalf("branch messages = %+v", branch.Messages)
	}
	if branch.Messages[0].ID == user.ID || branch.Messages[1].ID == assistantID {
		t.Fatal("branch reused source message IDs")
	}
	if branch.SystemPrompt != "be nice" || branch.Provider != providerOpenRouter || branch.Model != "test/model" {
		t.Fatalf("branch settings = %+v", branch)
	}

	// messageId is required.
	code, _ = doJSON(t, client, http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/branch",
		map[string]string{}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("branch without messageId = %d, want 400", code)
	}

	// A message from another profile is not visible.
	other := createTestProfile(t, s.store, "Other", false)
	otherConv, err := s.store.createConversation(ctx, other.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation(other): %v", err)
	}
	otherMsg, _, err := s.store.appendUserMessage(ctx, other.ID, otherConv.ID, providerOpenRouter, "test/model", "secret", nil)
	if err != nil {
		t.Fatalf("appendUserMessage(other): %v", err)
	}
	code, _ = doJSON(t, client, http.MethodPost, ts.URL+"/api/conversations/"+conv.ID+"/branch",
		map[string]string{"messageId": otherMsg.ID}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("branch from another profile = %d, want 404", code)
	}
}
