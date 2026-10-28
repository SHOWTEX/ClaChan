package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func createTestProfile(t *testing.T, s *Store, name string, owner bool) *Profile {
	t.Helper()
	p, err := s.createProfile(context.Background(), name, "hash", "salt", 1, owner)
	if err != nil {
		t.Fatalf("createProfile(%q): %v", name, err)
	}
	return p
}

func TestStoreConversationLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Test Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model", "be nice", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	if conv.Title != "New chat" || conv.SystemPrompt != "be nice" {
		t.Fatalf("unexpected new conversation: %+v", conv)
	}

	user, title, err := s.appendUserMessage(ctx, profile.ID, conv.ID, providerOpenRouter, "test/model", "  Explain  WAL mode\nplease ", nil)
	if err != nil {
		t.Fatalf("appendUserMessage: %v", err)
	}
	if want := "Explain WAL mode please"; title != want {
		t.Fatalf("derived title = %q, want %q", title, want)
	}

	if err := s.saveAssistant(ctx, profile.ID, conv.ID, newID(), "WAL is a journal mode.", "", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant insert: %v", err)
	}

	got, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].ID != user.ID || got.Messages[0].Role != "user" {
		t.Fatalf("first message = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "WAL is a journal mode." {
		t.Fatalf("second message = %+v", got.Messages[1])
	}
	if got.Title != "Explain WAL mode please" {
		t.Fatalf("title = %q", got.Title)
	}

	list, err := s.listConversations(ctx, profile.ID)
	if err != nil {
		t.Fatalf("listConversations: %v", err)
	}
	if len(list) != 1 || list[0].MessageCount != 2 || !strings.Contains(list[0].Preview, "WAL is") {
		t.Fatalf("list = %+v", list)
	}

	// Editing the first user turn drops the assistant reply.
	if _, err := s.editUserMessage(ctx, profile.ID, conv.ID, user.ID, "Explain WAL, briefly", providerOpenRouter, "test/model"); err != nil {
		t.Fatalf("editUserMessage: %v", err)
	}
	got, err = s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation after edit: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "Explain WAL, briefly" {
		t.Fatalf("after edit messages = %+v", got.Messages)
	}

	// Regenerate replaces the existing row instead of appending another.
	replyID := newID()
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, replyID, "first draft", "", nil, nil, nil, false); err != nil {
		t.Fatalf("saveAssistant: %v", err)
	}
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, replyID, "second draft", "", nil, nil, nil, true); err != nil {
		t.Fatalf("saveAssistant replace: %v", err)
	}
	got, err = s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation after regenerate: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[1].Content != "second draft" {
		t.Fatalf("after regenerate messages = %+v", got.Messages)
	}

	// Replacing a missing row falls back to an insert with the same id.
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, "deadbeef", "recovered", "", nil, nil, nil, true); err != nil {
		t.Fatalf("saveAssistant fallback: %v", err)
	}
	got, _ = s.getConversation(ctx, profile.ID, conv.ID)
	if len(got.Messages) != 3 || got.Messages[2].ID != "deadbeef" {
		t.Fatalf("fallback messages = %+v", got.Messages)
	}

	if err := s.deleteMessage(ctx, profile.ID, replyID); err != nil {
		t.Fatalf("deleteMessage: %v", err)
	}
	if err := s.deleteConversation(ctx, profile.ID, conv.ID); err != nil {
		t.Fatalf("deleteConversation: %v", err)
	}
	if _, err := s.getConversation(ctx, profile.ID, conv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("getConversation after delete = %v, want ErrNotFound", err)
	}
}

func TestStorePersistsMessageAttachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Attachment Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}

	attachments := []Attachment{{
		Kind: "image",
		Name: "screenshot.png",
		MIME: "image/png",
		Data: testDataURL("image/png", []byte("image bytes")),
	}}
	msg, title, err := s.appendUserMessage(ctx, profile.ID, conv.ID, providerOpenRouter, "test/model", "", attachments)
	if err != nil {
		t.Fatalf("appendUserMessage: %v", err)
	}
	if title != "screenshot.png" {
		t.Fatalf("attachment-only title = %q", title)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Name != "screenshot.png" {
		t.Fatalf("returned attachments = %+v", msg.Attachments)
	}

	got, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if len(got.Messages) != 1 || len(got.Messages[0].Attachments) != 1 {
		t.Fatalf("persisted messages = %+v", got.Messages)
	}
	if got.Messages[0].Attachments[0].Data != attachments[0].Data {
		t.Fatalf("attachment data changed: %+v", got.Messages[0].Attachments[0])
	}
}

func TestStorePersistsReasoningAndSteps(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Reasoning Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model",
		"", "", "", true, true, defaultToolCalls, "high")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	if conv.ReasoningEffort != "high" {
		t.Fatalf("created effort = %q", conv.ReasoningEffort)
	}

	steps := []ChatStep{{
		ID:        "step-1",
		Tool:      "web_search",
		Query:     "weather",
		Arguments: `{"query":"weather"}`,
		Status:    "completed",
		Reasoning: "I should search.",
		Result:    `{"query":"weather"}`,
		Sources:   []Citation{{URL: "https://example.com", Title: "Example", Excerpt: "Sunny"}},
	}}
	citations := []Citation{{URL: "https://example.com", Title: "Example", Excerpt: "Sunny"}}
	if err := s.saveAssistant(ctx, profile.ID, conv.ID, newID(), "It is sunny.",
		"A final thought.", steps, citations, &ToolRun{Enabled: true, MaxCalls: 6}, false); err != nil {
		t.Fatalf("saveAssistant: %v", err)
	}

	got, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if got.ReasoningEffort != "high" {
		t.Fatalf("stored effort = %q", got.ReasoningEffort)
	}
	reply := got.Messages[len(got.Messages)-1]
	if reply.Reasoning != "A final thought." {
		t.Fatalf("reasoning = %q", reply.Reasoning)
	}
	if len(reply.Steps) != 1 || reply.Steps[0].Tool != "web_search" ||
		reply.Steps[0].Reasoning != "I should search." ||
		len(reply.Steps[0].Sources) != 1 || reply.Steps[0].Sources[0].Excerpt != "Sunny" {
		t.Fatalf("steps = %+v", reply.Steps)
	}

	// Listing must carry the effort so the composer can restore it.
	list, err := s.listConversations(ctx, profile.ID)
	if err != nil || len(list) != 1 || list[0].ReasoningEffort != "high" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	effort := "low"
	if err := s.updateConversation(ctx, profile.ID, conv.ID, ConversationUpdate{ReasoningEffort: &effort}); err != nil {
		t.Fatalf("updateConversation: %v", err)
	}
	got, err = s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil || got.ReasoningEffort != "low" {
		t.Fatalf("updated effort = %+v, %v", got, err)
	}
}

func TestStoreProfileIsolation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ada := createTestProfile(t, s, "Ada", true)
	bob := createTestProfile(t, s, "Bob", false)

	conv, err := s.createConversation(ctx, ada.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	msg, _, err := s.appendUserMessage(ctx, ada.ID, conv.ID, providerOpenRouter, "test/model", "private question", nil)
	if err != nil {
		t.Fatalf("appendUserMessage: %v", err)
	}

	// Bob cannot read, list, edit, delete, or append to Ada's conversation.
	if _, err := s.getConversation(ctx, bob.ID, conv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob getConversation = %v, want ErrNotFound", err)
	}
	if list, err := s.listConversations(ctx, bob.ID); err != nil || len(list) != 0 {
		t.Fatalf("Bob listConversations = %+v, %v", list, err)
	}
	title := "hijacked"
	if err := s.updateConversation(ctx, bob.ID, conv.ID, ConversationUpdate{Title: &title}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob updateConversation = %v, want ErrNotFound", err)
	}
	if err := s.deleteConversation(ctx, bob.ID, conv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob deleteConversation = %v, want ErrNotFound", err)
	}
	if _, _, err := s.appendUserMessage(ctx, bob.ID, conv.ID, providerOpenRouter, "test/model", "injected", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob appendUserMessage = %v, want ErrNotFound", err)
	}
	if err := s.saveAssistant(ctx, bob.ID, conv.ID, newID(), "injected", "", nil, nil, nil, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob saveAssistant = %v, want ErrNotFound", err)
	}
	if err := s.deleteMessage(ctx, bob.ID, msg.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob deleteMessage = %v, want ErrNotFound", err)
	}

	// And Bob's own data stays separate.
	bobConv, err := s.createConversation(ctx, bob.ID, providerOpenRouter, "test/model", "", "", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("Bob createConversation: %v", err)
	}
	if list, err := s.listConversations(ctx, ada.ID); err != nil || len(list) != 1 || list[0].ID != conv.ID {
		t.Fatalf("Ada listConversations = %+v, %v", list, err)
	}
	if _, err := s.getConversation(ctx, ada.ID, bobConv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Ada read Bob's conversation = %v, want ErrNotFound", err)
	}
}

func TestConversationRoutingPersistence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	profile := createTestProfile(t, s, "Owner", true)

	conv, err := s.createConversation(ctx, profile.ID, providerOpenRouter, "test/model", "", "price", "", true, true, defaultToolCalls, "")
	if err != nil {
		t.Fatalf("createConversation: %v", err)
	}
	if conv.ProviderSort != "price" || conv.ProviderOnly != "" || !conv.AllowFallbacks {
		t.Fatalf("created routing = %+v", conv)
	}

	only := "deepinfra/turbo"
	noSort := ""
	allow := false
	if err := s.updateConversation(ctx, profile.ID, conv.ID, ConversationUpdate{
		ProviderOnly:   &only,
		ProviderSort:   &noSort,
		AllowFallbacks: &allow,
	}); err != nil {
		t.Fatalf("updateConversation: %v", err)
	}

	got, err := s.getConversation(ctx, profile.ID, conv.ID)
	if err != nil {
		t.Fatalf("getConversation: %v", err)
	}
	if got.ProviderOnly != only || got.ProviderSort != "" || got.AllowFallbacks {
		t.Fatalf("stored routing = %+v", got)
	}

	list, err := s.listConversations(ctx, profile.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].ProviderOnly != only || list[0].AllowFallbacks {
		t.Fatalf("list routing = %+v", list[0])
	}
}

func TestProfilesAndSessions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	owner := createTestProfile(t, s, "Owner", true)
	if owner.IsOwner != true {
		t.Fatalf("owner flag = %v", owner.IsOwner)
	}

	// Names are unique case-insensitively.
	if _, err := s.createProfile(ctx, "OWNER", "h", "s", 1, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate profile = %v, want ErrConflict", err)
	}
	// Only one owner can be created.
	if _, err := s.createProfile(ctx, "Second Owner", "h", "s", 1, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("second owner = %v, want ErrConflict", err)
	}

	second := createTestProfile(t, s, "Second", false)
	if second.IsOwner {
		t.Fatalf("second profile is owner")
	}
	profiles, err := s.listProfiles(ctx)
	if err != nil || len(profiles) != 2 {
		t.Fatalf("listProfiles = %+v, %v", profiles, err)
	}

	byName, err := s.profileByName(ctx, "second")
	if err != nil || byName.ID != second.ID {
		t.Fatalf("profileByName = %+v, %v", byName, err)
	}
	if _, err := s.profileByName(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing profile = %v", err)
	}

	if err := s.updateProfileDefaultModel(ctx, second.ID, "test/model"); err != nil {
		t.Fatalf("updateProfileDefaultModel: %v", err)
	}
	withDefault, err := s.profileByID(ctx, second.ID)
	if err != nil || withDefault.DefaultModel != "test/model" {
		t.Fatalf("default model = %+v, %v", withDefault, err)
	}

	if err := s.updateProfileIgnoredProviders(ctx, second.ID, "DeepInfra, novita"); err != nil {
		t.Fatalf("updateProfileIgnoredProviders: %v", err)
	}
	withIgnored, err := s.profileByID(ctx, second.ID)
	if err != nil || withIgnored.IgnoredProviders != "DeepInfra, novita" {
		t.Fatalf("ignored providers = %+v, %v", withIgnored, err)
	}
	list, err := s.listProfiles(ctx)
	if err != nil {
		t.Fatalf("listProfiles: %v", err)
	}
	var listed *Profile
	for i := range list {
		if list[i].ID == second.ID {
			listed = &list[i]
		}
	}
	if listed == nil || listed.IgnoredProviders != "DeepInfra, novita" {
		t.Fatalf("list profiles ignored = %+v", list)
	}

	prompt, err := s.createPrompt(ctx, second.ID, "Reviewer", "Review carefully.", true)
	if err != nil {
		t.Fatalf("createPrompt: %v", err)
	}
	prompts, err := s.listPrompts(ctx, second.ID)
	if err != nil || len(prompts) != 1 || prompts[0].ID != prompt.ID || !prompts[0].System {
		t.Fatalf("listPrompts = %+v, %v", prompts, err)
	}
	if _, err := s.listPrompts(ctx, owner.ID); err != nil {
		t.Fatalf("owner listPrompts: %v", err)
	}
	if got, err := s.listPrompts(ctx, owner.ID); err != nil || len(got) != 0 {
		t.Fatalf("owner prompts = %+v, %v", got, err)
	}
	if err := s.updatePrompt(ctx, owner.ID, prompt.ID, "Stolen", "No", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-profile update = %v, want ErrNotFound", err)
	}
	if err := s.deletePrompt(ctx, owner.ID, prompt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-profile delete = %v, want ErrNotFound", err)
	}
	if err := s.updateProfilePersona(ctx, second.ID, "Helper", "Be brief."); err != nil {
		t.Fatalf("updateProfilePersona: %v", err)
	}
	withPersona, err := s.profileByID(ctx, second.ID)
	if err != nil || withPersona.PersonaName != "Helper" || withPersona.PersonaPrompt != "Be brief." {
		t.Fatalf("persona = %+v, %v", withPersona, err)
	}

	// Sessions: valid, expired, per-profile deletion.
	now := nowMillis()
	if err := s.createSession(ctx, owner.ID, "valid-token", now+60_000); err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if err := s.createSession(ctx, owner.ID, "expired-token", now-1_000); err != nil {
		t.Fatalf("createSession expired: %v", err)
	}
	if err := s.createSession(ctx, second.ID, "second-token", now+60_000); err != nil {
		t.Fatalf("createSession second: %v", err)
	}

	p, err := s.sessionProfile(ctx, "valid-token", now)
	if err != nil || p.ID != owner.ID {
		t.Fatalf("sessionProfile = %+v, %v", p, err)
	}
	if _, err := s.sessionProfile(ctx, "expired-token", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session = %v, want ErrNotFound", err)
	}

	if err := s.deleteExpiredSessions(ctx, now); err != nil {
		t.Fatalf("deleteExpiredSessions: %v", err)
	}
	if _, err := s.sessionProfile(ctx, "expired-token", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session survived cleanup")
	}

	if err := s.deleteSessionsForProfile(ctx, owner.ID); err != nil {
		t.Fatalf("deleteSessionsForProfile: %v", err)
	}
	if _, err := s.sessionProfile(ctx, "valid-token", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner session survived deletion")
	}
	if _, err := s.sessionProfile(ctx, "second-token", now); err != nil {
		t.Fatalf("second session was deleted: %v", err)
	}

	if err := s.updateProfilePassword(ctx, second.ID, "newhash", "newsalt", 2); err != nil {
		t.Fatalf("updateProfilePassword: %v", err)
	}
	updated, err := s.profileByID(ctx, second.ID)
	if err != nil || updated.PasswordHash != "newhash" || updated.PasswordIters != 2 {
		t.Fatalf("updated profile = %+v, %v", updated, err)
	}
}

func TestFirstProfileClaimsOrphanConversations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := nowMillis()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO conversations (id, profile_id, title, provider, model, system_prompt, created_at, updated_at)
		VALUES ('orphan', NULL, 'Old chat', 'openrouter', 'test/model', '', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert orphan: %v", err)
	}

	owner := createTestProfile(t, s, "Owner", true)
	if _, err := s.getConversation(ctx, owner.ID, "orphan"); err != nil {
		t.Fatalf("first profile did not claim orphan: %v", err)
	}

	other := createTestProfile(t, s, "Other", false)
	if _, err := s.getConversation(ctx, other.ID, "orphan"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second profile can read orphan: %v", err)
	}
}

func TestStorePragmas(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	var journal string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}

	var sync int
	if err := s.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("synchronous: %v", err)
	}
	if sync != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", sync)
	}

	var fk int
	if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}
}

func TestDeriveTitle(t *testing.T) {
	cases := map[string]string{
		"":                      "New chat",
		"   ":                   "New chat",
		"hello\n\nworld":        "hello world",
		strings.Repeat("x", 80): strings.Repeat("x", 60) + "…",
	}
	for in, want := range cases {
		if got := deriveTitle(in); got != want {
			t.Errorf("deriveTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPreviewText(t *testing.T) {
	in := "## Mock reply\n\nHere is **bold** and [a link](https://example.com).\n\n| a | b |"
	want := "Mock reply Here is bold and a link. a b"
	if got := previewText(in); got != want {
		t.Errorf("previewText = %q, want %q", got, want)
	}
}

func TestSessionTokenHashing(t *testing.T) {
	token, hashed, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	if len(token) < 40 || hashed != hashToken(token) {
		t.Fatalf("bad session token: len=%d", len(token))
	}
	if hashToken(token) == token {
		t.Fatal("session token stored in plaintext")
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, salt, iters, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if iters != passwordIterations {
		t.Fatalf("iters = %d", iters)
	}
	if !verifyPassword("correct horse battery", hash, salt, iters) {
		t.Fatal("correct password rejected")
	}
	if verifyPassword("wrong", hash, salt, iters) {
		t.Fatal("wrong password accepted")
	}
}
