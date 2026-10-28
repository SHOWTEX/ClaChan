package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newAuthTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(http.NotFoundHandler())
	s := newTestServer(t, upstream)
	ts := httptest.NewServer(s.routes())
	t.Cleanup(func() {
		ts.Close()
		upstream.Close()
	})
	return s, ts
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return res.StatusCode, data
}

type authStatus struct {
	SetupRequired bool      `json:"setupRequired"`
	Profiles      []Profile `json:"profiles"`
	Current       *Profile  `json:"current"`
}

func getStatus(t *testing.T, client *http.Client, base string) authStatus {
	t.Helper()
	code, data := doJSON(t, client, http.MethodGet, base+"/api/auth/status", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, data)
	}
	var status authStatus
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return status
}

func TestAuthEndToEnd(t *testing.T) {
	_, ts := newAuthTestServer(t)
	base := ts.URL
	client := newCookieClient(t)

	status := getStatus(t, client, base)
	if !status.SetupRequired || len(status.Profiles) != 0 || status.Current != nil {
		t.Fatalf("initial status = %+v", status)
	}

	// Setup creates the owner, claims nothing, and signs in.
	code, data := doJSON(t, client, http.MethodPost, base+"/api/auth/setup",
		map[string]string{"name": "Ada", "password": "correct horse"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("setup = %d, body = %s", code, data)
	}
	var owner Profile
	if err := json.Unmarshal(data, &owner); err != nil || !owner.IsOwner || owner.Name != "Ada" {
		t.Fatalf("owner = %+v, %v", owner, err)
	}

	status = getStatus(t, client, base)
	if status.SetupRequired || status.Current == nil || status.Current.ID != owner.ID {
		t.Fatalf("status after setup = %+v", status)
	}

	// Setup is closed once a profile exists.
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/auth/setup",
		map[string]string{"name": "Eve", "password": "another password"}, nil)
	if code != http.StatusForbidden {
		t.Fatalf("second setup = %d, want 403", code)
	}

	// Authenticated conversation creation.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/conversations",
		map[string]string{"provider": "openrouter", "model": "test/model"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create conversation = %d, body = %s", code, data)
	}
	var adaConv Conversation
	if err := json.Unmarshal(data, &adaConv); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}
	if !adaConv.ToolsEnabled || adaConv.MaxToolCalls != defaultToolCalls {
		t.Fatalf("default tool settings = %+v", adaConv)
	}
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/conversations/"+adaConv.ID,
		map[string]any{"toolsEnabled": false, "maxToolCalls": 9, "reasoningEffort": "high"}, nil)
	if code != http.StatusOK {
		t.Fatalf("patch tools = %d, body = %s", code, data)
	}
	code, data = doJSON(t, client, http.MethodGet, base+"/api/conversations/"+adaConv.ID, nil, nil)
	var patched Conversation
	if err := json.Unmarshal(data, &patched); err != nil || patched.ToolsEnabled || patched.MaxToolCalls != 9 ||
		patched.ReasoningEffort != "high" {
		t.Fatalf("patched tools = %+v, %v", patched, err)
	}
	code, _ = doJSON(t, client, http.MethodPatch, base+"/api/conversations/"+adaConv.ID,
		map[string]any{"maxToolCalls": 31}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid tool cap = %d, want 400", code)
	}
	code, _ = doJSON(t, client, http.MethodPatch, base+"/api/conversations/"+adaConv.ID,
		map[string]any{"reasoningEffort": "turbo"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid reasoning effort = %d, want 400", code)
	}

	// Default model is part of the profile and syncs to every device.
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"defaultModel": "test/model"}, nil)
	if code != http.StatusOK {
		t.Fatalf("set default model = %d, body = %s", code, data)
	}
	status = getStatus(t, client, base)
	if status.Current == nil || status.Current.DefaultModel != "test/model" {
		t.Fatalf("default model in status = %+v", status.Current)
	}
	code, _ = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"defaultModel": "not a model"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid default model = %d, want 400", code)
	}
	code, _ = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"defaultModel": ""}, nil)
	if code != http.StatusOK {
		t.Fatalf("clear default model = %d", code)
	}

	// Ignored providers are normalised and profile-scoped.
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"ignoredProviders": " DeepInfra, novita,\ndeepinfra "}, nil)
	if code != http.StatusOK {
		t.Fatalf("set ignored providers = %d, body = %s", code, data)
	}
	status = getStatus(t, client, base)
	if status.Current == nil || status.Current.IgnoredProviders != "DeepInfra, novita" {
		t.Fatalf("ignored providers in status = %+v", status.Current)
	}
	code, _ = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"ignoredProviders": "bad<script>"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid ignored providers = %d, want 400", code)
	}

	// Persona settings are profile-level and default new chats.
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"personaName": "Ada's assistant", "personaPrompt": "Be precise."}, nil)
	if code != http.StatusOK {
		t.Fatalf("set persona = %d, body = %s", code, data)
	}
	status = getStatus(t, client, base)
	if status.Current == nil || status.Current.PersonaName != "Ada's assistant" ||
		status.Current.PersonaPrompt != "Be precise." {
		t.Fatalf("persona in status = %+v", status.Current)
	}
	publicStatus := getStatus(t, newCookieClient(t), base)
	if publicStatus.Current != nil || len(publicStatus.Profiles) != 1 ||
		publicStatus.Profiles[0].PersonaPrompt != "" || publicStatus.Profiles[0].DefaultModel != "" ||
		publicStatus.Profiles[0].UserPersona != "" {
		t.Fatalf("public status leaked profile settings = %+v", publicStatus)
	}
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/profile",
		map[string]string{"userName": "Ada Lovelace", "userPersona": "Prefers concise answers."}, nil)
	if code != http.StatusOK {
		t.Fatalf("set user profile = %d, body = %s", code, data)
	}
	status = getStatus(t, client, base)
	if status.Current == nil || status.Current.UserName != "Ada Lovelace" ||
		status.Current.UserPersona != "Prefers concise answers." {
		t.Fatalf("user profile in status = %+v", status.Current)
	}
	code, data = doJSON(t, client, http.MethodPost, base+"/api/conversations",
		map[string]string{"provider": "openrouter", "model": "test/model"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create persona conversation = %d, body = %s", code, data)
	}
	var personaConv Conversation
	if err := json.Unmarshal(data, &personaConv); err != nil || personaConv.SystemPrompt != "Be precise." {
		t.Fatalf("persona conversation = %+v, %v", personaConv, err)
	}

	// Prompt library CRUD.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/prompts",
		map[string]any{"title": "Reviewer", "body": "Review this code carefully.", "system": true}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create prompt = %d, body = %s", code, data)
	}
	var prompt Prompt
	if err := json.Unmarshal(data, &prompt); err != nil || prompt.Title != "Reviewer" || !prompt.System {
		t.Fatalf("prompt = %+v, %v", prompt, err)
	}
	code, data = doJSON(t, client, http.MethodGet, base+"/api/prompts", nil, nil)
	if code != http.StatusOK || !strings.Contains(string(data), "Reviewer") {
		t.Fatalf("list prompts = %d, body = %s", code, data)
	}
	code, data = doJSON(t, client, http.MethodPatch, base+"/api/prompts/"+prompt.ID,
		map[string]string{"title": "Code reviewer"}, nil)
	if code != http.StatusOK || !strings.Contains(string(data), "Code reviewer") {
		t.Fatalf("update prompt = %d, body = %s", code, data)
	}
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/prompts",
		map[string]string{"title": "", "body": "x"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid prompt = %d, want 400", code)
	}
	code, _ = doJSON(t, client, http.MethodDelete, base+"/api/prompts/"+prompt.ID, nil, nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete prompt = %d", code)
	}

	// Imported chats get fresh server IDs and stay scoped to the importing profile.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/conversations/import", map[string]any{
		"title":           "Imported review",
		"provider":        "openrouter",
		"model":           "test/model",
		"systemPrompt":    "Be careful.",
		"toolsEnabled":    false,
		"maxToolCalls":    9,
		"reasoningEffort": "medium",
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": "Review this.",
				"attachments": []Attachment{{
					Kind: "text",
					Name: "main.go",
					MIME: "text/plain",
					Data: testDataURL("text/plain", []byte("package main")),
				}},
			},
			{
				"role":      "assistant",
				"content":   "Looks fine.",
				"reasoning": "The code is short.",
				"steps": []ChatStep{{
					ID:      "step-1",
					Tool:    "web_fetch",
					Query:   "",
					Sources: []Citation{{URL: "https://example.com", Title: "Example"}},
				}},
				"citations": []Citation{{URL: "https://example.com", Title: "Example"}},
				"toolRun":   &ToolRun{Enabled: true, MaxCalls: 9, Calls: 1, Searches: 1},
			},
		},
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("import conversation = %d, body = %s", code, data)
	}
	var imported Conversation
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatalf("decode imported conversation: %v", err)
	}
	if imported.ID == "" || imported.Title != "Imported review" || imported.SystemPrompt != "Be careful." ||
		imported.ToolsEnabled || imported.MaxToolCalls != 9 || imported.ReasoningEffort != "medium" ||
		len(imported.Messages) != 2 || imported.Messages[0].ID == "" ||
		len(imported.Messages[0].Attachments) != 1 {
		t.Fatalf("imported conversation = %+v", imported)
	}
	reply := imported.Messages[1]
	if reply.Reasoning != "The code is short." || len(reply.Steps) != 1 || reply.Steps[0].Tool != "web_fetch" {
		t.Fatalf("imported reply reasoning = %+v", reply)
	}
	if len(reply.Citations) != 1 || reply.Citations[0].URL != "https://example.com" ||
		reply.ToolRun == nil || reply.ToolRun.Calls != 1 || reply.ToolRun.Searches != 1 {
		t.Fatalf("imported reply metadata = %+v", reply)
	}
	code, data = doJSON(t, client, http.MethodGet, base+"/api/conversations/"+imported.ID, nil, nil)
	if code != http.StatusOK || !strings.Contains(string(data), "Looks fine.") {
		t.Fatalf("load imported conversation = %d, body = %s", code, data)
	}
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/conversations/import", map[string]any{
		"messages": []map[string]string{{"role": "system", "content": "no"}},
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("import system role = %d, want 400", code)
	}

	// Logout revokes the session and locks the API.
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/auth/logout", nil, nil)
	if code != http.StatusNoContent {
		t.Fatalf("logout = %d", code)
	}
	code, _ = doJSON(t, client, http.MethodGet, base+"/api/conversations", nil, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("conversations after logout = %d, want 401", code)
	}

	// Wrong password is generic.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/auth/login",
		map[string]string{"profileId": owner.ID, "password": "wrong"}, nil)
	if code != http.StatusUnauthorized || !strings.Contains(string(data), "invalid_credentials") {
		t.Fatalf("wrong login = %d, body = %s", code, data)
	}

	code, data = doJSON(t, client, http.MethodPost, base+"/api/auth/login",
		map[string]string{"profileId": owner.ID, "password": "correct horse"}, nil)
	if code != http.StatusOK {
		t.Fatalf("login = %d, body = %s", code, data)
	}

	code, data = doJSON(t, client, http.MethodGet, base+"/api/conversations", nil, nil)
	if code != http.StatusOK || !strings.Contains(string(data), adaConv.ID) {
		t.Fatalf("conversations after login = %d, body = %s", code, data)
	}

	// The owner can add a profile. The new profile is not the owner.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/profiles",
		map[string]string{"name": "Bob", "password": "bobpassword"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create profile = %d, body = %s", code, data)
	}
	var bob Profile
	if err := json.Unmarshal(data, &bob); err != nil || bob.IsOwner {
		t.Fatalf("bob = %+v, %v", bob, err)
	}

	// Profile list is owner-only but status lists names for the login picker.
	code, data = doJSON(t, client, http.MethodGet, base+"/api/profiles", nil, nil)
	if code != http.StatusOK || !strings.Contains(string(data), "Bob") {
		t.Fatalf("list profiles = %d, body = %s", code, data)
	}

	// Switch to Bob.
	doJSON(t, client, http.MethodPost, base+"/api/auth/logout", nil, nil)
	code, data = doJSON(t, client, http.MethodPost, base+"/api/auth/login",
		map[string]string{"profileId": bob.ID, "password": "bobpassword"}, nil)
	if code != http.StatusOK {
		t.Fatalf("bob login = %d, body = %s", code, data)
	}

	// Bob has no chats and cannot see Ada's.
	code, data = doJSON(t, client, http.MethodGet, base+"/api/conversations", nil, nil)
	if code != http.StatusOK || strings.Contains(string(data), adaConv.ID) {
		t.Fatalf("bob conversations = %d, body = %s", code, data)
	}
	code, _ = doJSON(t, client, http.MethodGet, base+"/api/conversations/"+adaConv.ID, nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("bob reading ada's conversation = %d, want 404", code)
	}

	// Bob is not the owner.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/profiles",
		map[string]string{"name": "Mallory", "password": "mallorypassword"}, nil)
	if code != http.StatusForbidden {
		t.Fatalf("non-owner create profile = %d, body = %s", code, data)
	}

	// Cross-site mutation is blocked even with a valid session.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/conversations",
		map[string]string{"provider": "openrouter", "model": "test/model"},
		map[string]string{"Origin": "http://evil.example"})
	if code != http.StatusForbidden {
		t.Fatalf("cross-origin create = %d, body = %s", code, data)
	}

	// Same-origin mutations pass.
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/conversations",
		map[string]string{"provider": "openrouter", "model": "test/model"},
		map[string]string{"Origin": base})
	if code != http.StatusCreated {
		t.Fatalf("same-origin create = %d", code)
	}

	// Password change requires the current password, rotates the session, and
	// keeps this device signed in.
	code, data = doJSON(t, client, http.MethodPost, base+"/api/auth/password",
		map[string]string{"currentPassword": "wrong", "newPassword": "newpassword1"}, nil)
	if code != http.StatusForbidden {
		t.Fatalf("wrong current password = %d, body = %s", code, data)
	}
	code, data = doJSON(t, client, http.MethodPost, base+"/api/auth/password",
		map[string]string{"currentPassword": "bobpassword", "newPassword": "newpassword1"}, nil)
	if code != http.StatusNoContent {
		t.Fatalf("change password = %d, body = %s", code, data)
	}
	code, _ = doJSON(t, client, http.MethodGet, base+"/api/conversations", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("session after password change = %d, want 200", code)
	}

	// Old password no longer works; new one does.
	doJSON(t, client, http.MethodPost, base+"/api/auth/logout", nil, nil)
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/auth/login",
		map[string]string{"profileId": bob.ID, "password": "bobpassword"}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("old password after change = %d, want 401", code)
	}
	code, _ = doJSON(t, client, http.MethodPost, base+"/api/auth/login",
		map[string]string{"profileId": bob.ID, "password": "newpassword1"}, nil)
	if code != http.StatusOK {
		t.Fatalf("new password login = %d", code)
	}
}

func TestAuthValidation(t *testing.T) {
	if err := validatePassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := validatePassword(strings.Repeat("x", maxPasswordLength+1)); err == nil {
		t.Fatal("long password accepted")
	}
	if err := validatePassword("longenough"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	if err := validateProfileName(" "); err == nil {
		t.Fatal("blank name accepted")
	}
	if err := validateProfileName(strings.Repeat("x", maxProfileNameLen+1)); err == nil {
		t.Fatal("long name accepted")
	}
	if err := validateProfileName("Ada Lovelace"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestLoginLimiter(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < 8; i++ {
		if !limiter.allow("192.0.2.10") {
			t.Fatalf("attempt %d blocked too early", i+1)
		}
	}
	if limiter.allow("192.0.2.10") {
		t.Fatal("ninth attempt allowed")
	}
	if !limiter.allow("192.0.2.11") {
		t.Fatal("another address was limited")
	}
}
