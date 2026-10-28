package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

const schemaConversations = `
CREATE TABLE IF NOT EXISTS conversations (
  id            TEXT PRIMARY KEY,
  profile_id    TEXT,
  title         TEXT NOT NULL DEFAULT 'New chat',
  provider      TEXT NOT NULL DEFAULT 'openrouter',
  model         TEXT NOT NULL DEFAULT '',
  system_prompt TEXT NOT NULL DEFAULT '',
  provider_sort TEXT NOT NULL DEFAULT '',
  provider_only TEXT NOT NULL DEFAULT '',
  allow_fallbacks INTEGER NOT NULL DEFAULT 1,
  tools_enabled INTEGER NOT NULL DEFAULT 1,
  max_tool_calls INTEGER NOT NULL DEFAULT 6,
  reasoning_effort TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);`

const schemaMessages = `
CREATE TABLE IF NOT EXISTS messages (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role            TEXT NOT NULL CHECK (role IN ('system','user','assistant')),
  content         TEXT NOT NULL,
  attachments     TEXT NOT NULL DEFAULT '[]',
  citations       TEXT NOT NULL DEFAULT '[]',
  tool_run        TEXT NOT NULL DEFAULT '',
  reasoning       TEXT NOT NULL DEFAULT '',
  steps           TEXT NOT NULL DEFAULT '[]',
  seq             INTEGER NOT NULL,
  created_at      INTEGER NOT NULL
);`

const schemaProfiles = `
CREATE TABLE IF NOT EXISTS profiles (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash  TEXT NOT NULL,
  password_salt  TEXT NOT NULL,
  password_iters INTEGER NOT NULL,
  is_owner       INTEGER NOT NULL DEFAULT 0,
  default_model  TEXT NOT NULL DEFAULT '',
  ignored_providers TEXT NOT NULL DEFAULT '',
  persona_name   TEXT NOT NULL DEFAULT '',
  persona_prompt TEXT NOT NULL DEFAULT '',
  user_name      TEXT NOT NULL DEFAULT '',
  user_persona   TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL
);`

const schemaPrompts = `
CREATE TABLE IF NOT EXISTS prompts (
  id         TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  is_system  INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`

const schemaSessions = `
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);`

var schemaStatements = []string{
	schemaConversations,
	schemaMessages,
	schemaProfiles,
	schemaPrompts,
	schemaSessions,
	`CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, seq);`,
	`CREATE INDEX IF NOT EXISTS idx_prompts_profile ON prompts(profile_id, updated_at DESC);`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_profile ON sessions(profile_id);`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);`,
}

type Store struct {
	db *sql.DB
}

type Message struct {
	ID          string       `json:"id"`
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Reasoning   string       `json:"reasoning,omitempty"`
	Steps       []ChatStep   `json:"steps,omitempty"`
	Citations   []Citation   `json:"citations,omitempty"`
	ToolRun     *ToolRun     `json:"toolRun,omitempty"`
	CreatedAt   int64        `json:"createdAt"`
}

type Conversation struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	SystemPrompt    string    `json:"systemPrompt,omitempty"`
	ProviderSort    string    `json:"providerSort,omitempty"`
	ProviderOnly    string    `json:"providerOnly,omitempty"`
	AllowFallbacks  bool      `json:"allowFallbacks"`
	ToolsEnabled    bool      `json:"toolsEnabled"`
	MaxToolCalls    int       `json:"maxToolCalls"`
	ReasoningEffort string    `json:"reasoningEffort,omitempty"`
	CreatedAt       int64     `json:"createdAt"`
	UpdatedAt       int64     `json:"updatedAt"`
	MessageCount    int       `json:"messageCount,omitempty"`
	Preview         string    `json:"preview,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
}

// ConversationUpdate carries only the fields a PATCH should change.
type ConversationUpdate struct {
	Title           *string
	SystemPrompt    *string
	Provider        *string
	Model           *string
	ProviderSort    *string
	ProviderOnly    *string
	AllowFallbacks  *bool
	ToolsEnabled    *bool
	MaxToolCalls    *int
	ReasoningEffort *string
}

// Profile is the public shape of an account. Password material never crosses
// this boundary.
type Profile struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	IsOwner          bool   `json:"isOwner"`
	DefaultModel     string `json:"defaultModel,omitempty"`
	IgnoredProviders string `json:"ignoredProviders,omitempty"`
	PersonaName      string `json:"personaName,omitempty"`
	PersonaPrompt    string `json:"personaPrompt,omitempty"`
	UserName         string `json:"userName,omitempty"`
	UserPersona      string `json:"userPersona,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
}

// Prompt is a reusable prompt saved by one profile.
type Prompt struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// profileAuth is the internal login shape.
type profileAuth struct {
	Profile
	PasswordHash  string
	PasswordSalt  string
	PasswordIters int
}

func openStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One writer, one reader, one connection. This is a 1GB box, and the
	// assistant write happens once per reply, not per token.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	for _, stmt := range schemaStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}

	// Databases created before profiles existed need the new column. The index
	// is created after the migration so it works on both old and new files.
	if err := ensureColumn(ctx, db, "conversations", "profile_id", "profile_id TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate conversations: %w", err)
	}
	for _, column := range []struct {
		name        string
		declaration string
	}{
		{"provider_sort", "provider_sort TEXT NOT NULL DEFAULT ''"},
		{"provider_only", "provider_only TEXT NOT NULL DEFAULT ''"},
		{"allow_fallbacks", "allow_fallbacks INTEGER NOT NULL DEFAULT 1"},
		{"tools_enabled", "tools_enabled INTEGER NOT NULL DEFAULT 1"},
		{"max_tool_calls", "max_tool_calls INTEGER NOT NULL DEFAULT 6"},
		{"reasoning_effort", "reasoning_effort TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(ctx, db, "conversations", column.name, column.declaration); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate conversations.%s: %w", column.name, err)
		}
	}
	for _, column := range []struct {
		name        string
		declaration string
	}{
		{"default_model", "default_model TEXT NOT NULL DEFAULT ''"},
		{"ignored_providers", "ignored_providers TEXT NOT NULL DEFAULT ''"},
		{"persona_name", "persona_name TEXT NOT NULL DEFAULT ''"},
		{"persona_prompt", "persona_prompt TEXT NOT NULL DEFAULT ''"},
		{"user_name", "user_name TEXT NOT NULL DEFAULT ''"},
		{"user_persona", "user_persona TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(ctx, db, "profiles", column.name, column.declaration); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate profiles.%s: %w", column.name, err)
		}
	}
	if err := ensureColumn(ctx, db, "prompts", "is_system", "is_system INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate prompts: %w", err)
	}
	if err := ensureColumn(ctx, db, "messages", "attachments", "attachments TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages: %w", err)
	}
	if err := ensureColumn(ctx, db, "messages", "citations", "citations TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages.citations: %w", err)
	}
	if err := ensureColumn(ctx, db, "messages", "tool_run", "tool_run TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages.tool_run: %w", err)
	}
	if err := ensureColumn(ctx, db, "messages", "reasoning", "reasoning TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages.reasoning: %w", err)
	}
	if err := ensureColumn(ctx, db, "messages", "steps", "steps TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate messages.steps: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_conversations_profile
		 ON conversations(profile_id, updated_at DESC)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index conversations: %w", err)
	}

	return &Store{db: db}, nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, declaration string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notNull    int
			defaultV   any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultV, &primaryKey); err != nil {
			return err
		}
		if strings.EqualFold(name, column) {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}

	_, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+declaration)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func nowMillis() int64 { return time.Now().UnixMilli() }

/* ------------------------------------------------------------------ profiles */

func (s *Store) countProfiles(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles").Scan(&n)
	return n, err
}

// createProfile creates an account. The first profile is the owner and claims
// any conversations that predate profiles, so upgrading does not hide history.
func (s *Store) createProfile(ctx context.Context, name, hash, salt string, iters int, owner bool) (*Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles").Scan(&count); err != nil {
		return nil, err
	}
	if owner && count > 0 {
		return nil, ErrConflict
	}

	var exists int
	switch err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM profiles WHERE name = ? COLLATE NOCASE", name).Scan(&exists); {
	case err == nil:
		return nil, ErrConflict
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	now := nowMillis()
	p := &Profile{ID: newID(), Name: name, IsOwner: owner, CreatedAt: now}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO profiles (id, name, password_hash, password_salt, password_iters, is_owner, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, hash, salt, iters, boolInt(owner), now); err != nil {
		return nil, err
	}

	if owner {
		if _, err := tx.ExecContext(ctx,
			"UPDATE conversations SET profile_id = ? WHERE profile_id IS NULL", p.ID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) listProfiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, is_owner, default_model, ignored_providers,
		        persona_name, persona_prompt, user_name, user_persona, created_at
		 FROM profiles ORDER BY created_at, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Profile{}
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.IsOwner, &p.DefaultModel, &p.IgnoredProviders,
			&p.PersonaName, &p.PersonaPrompt, &p.UserName, &p.UserPersona, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) profileByID(ctx context.Context, id string) (*profileAuth, error) {
	return s.scanProfileAuth(ctx, `
		SELECT id, name, is_owner, default_model, ignored_providers,
		       persona_name, persona_prompt, user_name, user_persona, created_at,
		       password_hash, password_salt, password_iters
		FROM profiles WHERE id = ?`, id)
}

func (s *Store) profileByName(ctx context.Context, name string) (*profileAuth, error) {
	return s.scanProfileAuth(ctx, `
		SELECT id, name, is_owner, default_model, ignored_providers,
		       persona_name, persona_prompt, user_name, user_persona, created_at,
		       password_hash, password_salt, password_iters
		FROM profiles WHERE name = ? COLLATE NOCASE`, name)
}

func (s *Store) scanProfileAuth(ctx context.Context, query string, arg any) (*profileAuth, error) {
	var p profileAuth
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&p.ID, &p.Name, &p.IsOwner, &p.DefaultModel, &p.IgnoredProviders,
		&p.PersonaName, &p.PersonaPrompt, &p.UserName, &p.UserPersona, &p.CreatedAt,
		&p.PasswordHash, &p.PasswordSalt, &p.PasswordIters,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) updateProfilePassword(ctx context.Context, profileID, hash, salt string, iters int) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE profiles SET password_hash = ?, password_salt = ?, password_iters = ? WHERE id = ?",
		hash, salt, iters, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) updateProfileDefaultModel(ctx context.Context, profileID, model string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE profiles SET default_model = ? WHERE id = ?", model, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) updateProfileIgnoredProviders(ctx context.Context, profileID, ignored string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE profiles SET ignored_providers = ? WHERE id = ?", ignored, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

/* ------------------------------------------------------------------ sessions */

func (s *Store) updateProfilePersona(ctx context.Context, profileID, name, prompt string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE profiles SET persona_name = ?, persona_prompt = ? WHERE id = ?",
		name, prompt, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) updateProfileUser(ctx context.Context, profileID, name, persona string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE profiles SET user_name = ?, user_persona = ? WHERE id = ?",
		name, persona, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

/* ------------------------------------------------------------------- prompts */

func (s *Store) listPrompts(ctx context.Context, profileID string) ([]Prompt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, body, is_system, created_at, updated_at
		FROM prompts
		WHERE profile_id = ?
		ORDER BY updated_at DESC, title COLLATE NOCASE
		LIMIT 500`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Prompt{}
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.Title, &p.Body, &p.System, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) createPrompt(ctx context.Context, profileID, title, body string, system bool) (*Prompt, error) {
	now := nowMillis()
	p := &Prompt{ID: newID(), Title: title, Body: body, System: system, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO prompts (id, profile_id, title, body, is_system, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, profileID, p.Title, p.Body, boolInt(p.System), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) promptByID(ctx context.Context, profileID, id string) (*Prompt, error) {
	var p Prompt
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, body, is_system, created_at, updated_at
		FROM prompts WHERE id = ? AND profile_id = ?`, id, profileID).
		Scan(&p.ID, &p.Title, &p.Body, &p.System, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) updatePrompt(ctx context.Context, profileID, id, title, body string, system bool) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE prompts SET title = ?, body = ?, is_system = ?, updated_at = ?
		WHERE id = ? AND profile_id = ?`,
		title, body, boolInt(system), nowMillis(), id, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) deletePrompt(ctx context.Context, profileID, id string) error {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM prompts WHERE id = ? AND profile_id = ?", id, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) createSession(ctx context.Context, profileID, tokenHash string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, profile_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		tokenHash, profileID, nowMillis(), expiresAt)
	return err
}

func (s *Store) sessionProfile(ctx context.Context, tokenHash string, now int64) (*Profile, error) {
	var p Profile
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.is_owner, p.default_model, p.ignored_providers,
		       p.persona_name, p.persona_prompt, p.user_name, p.user_persona, p.created_at
		FROM sessions s
		JOIN profiles p ON p.id = s.profile_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now).
		Scan(&p.ID, &p.Name, &p.IsOwner, &p.DefaultModel, &p.IgnoredProviders,
			&p.PersonaName, &p.PersonaPrompt, &p.UserName, &p.UserPersona, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) deleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) deleteSessionsForProfile(ctx context.Context, profileID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE profile_id = ?", profileID)
	return err
}

func (s *Store) deleteExpiredSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now)
	return err
}

/* ------------------------------------------------------------- conversations */

func (s *Store) createConversation(ctx context.Context, profileID, provider, model, systemPrompt, providerSort, providerOnly string, allowFallbacks, toolsEnabled bool, maxToolCalls int, reasoningEffort string) (*Conversation, error) {
	now := nowMillis()
	c := &Conversation{
		ID:              newID(),
		Title:           "New chat",
		Provider:        provider,
		Model:           model,
		SystemPrompt:    systemPrompt,
		ProviderSort:    providerSort,
		ProviderOnly:    providerOnly,
		AllowFallbacks:  allowFallbacks,
		ToolsEnabled:    toolsEnabled,
		MaxToolCalls:    clampToolCalls(maxToolCalls),
		ReasoningEffort: reasoningEffort,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversations (id, profile_id, title, provider, model, system_prompt, provider_sort, provider_only, allow_fallbacks, tools_enabled, max_tool_calls, reasoning_effort, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, profileID, c.Title, c.Provider, c.Model, c.SystemPrompt,
		c.ProviderSort, c.ProviderOnly, boolInt(c.AllowFallbacks),
		boolInt(c.ToolsEnabled), c.MaxToolCalls, c.ReasoningEffort, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) importConversation(ctx context.Context, profileID, title, provider, model, systemPrompt, providerSort, providerOnly string, allowFallbacks, toolsEnabled bool, maxToolCalls int, reasoningEffort string, messages []Message) (*Conversation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowMillis()
	c := &Conversation{
		ID:              newID(),
		Title:           title,
		Provider:        provider,
		Model:           model,
		SystemPrompt:    systemPrompt,
		ProviderSort:    providerSort,
		ProviderOnly:    providerOnly,
		AllowFallbacks:  allowFallbacks,
		ToolsEnabled:    toolsEnabled,
		MaxToolCalls:    clampToolCalls(maxToolCalls),
		ReasoningEffort: reasoningEffort,
		CreatedAt:       now,
		UpdatedAt:       now,
		MessageCount:    len(messages),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO conversations (id, profile_id, title, provider, model, system_prompt, provider_sort, provider_only, allow_fallbacks, tools_enabled, max_tool_calls, reasoning_effort, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, profileID, c.Title, c.Provider, c.Model, c.SystemPrompt,
		c.ProviderSort, c.ProviderOnly, boolInt(c.AllowFallbacks),
		boolInt(c.ToolsEnabled), c.MaxToolCalls, c.ReasoningEffort, c.CreatedAt, c.UpdatedAt); err != nil {
		return nil, err
	}

	for i := range messages {
		message := &messages[i]
		message.ID = newID()
		if message.CreatedAt <= 0 {
			message.CreatedAt = now
		}

		attachmentsJSON := []byte("[]")
		if len(message.Attachments) > 0 {
			attachmentsJSON, err = json.Marshal(message.Attachments)
			if err != nil {
				return nil, fmt.Errorf("encode message attachments: %w", err)
			}
		}
		citationsJSON := []byte("[]")
		if len(message.Citations) > 0 {
			citationsJSON, err = json.Marshal(message.Citations)
			if err != nil {
				return nil, fmt.Errorf("encode message citations: %w", err)
			}
		}
		toolRunJSON := []byte("")
		if message.ToolRun != nil {
			toolRunJSON, err = json.Marshal(message.ToolRun)
			if err != nil {
				return nil, fmt.Errorf("encode message tool run: %w", err)
			}
		}
		stepsJSON := []byte("[]")
		if len(message.Steps) > 0 {
			stepsJSON, err = json.Marshal(message.Steps)
			if err != nil {
				return nil, fmt.Errorf("encode message steps: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages (id, conversation_id, role, content, attachments, citations, tool_run, reasoning, steps, seq, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			message.ID, c.ID, message.Role, message.Content, string(attachmentsJSON),
			string(citationsJSON), string(toolRunJSON), message.Reasoning, string(stepsJSON),
			i+1, message.CreatedAt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	c.Messages = messages
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		c.Preview = previewText(last.Content)
		if c.Preview == "" && len(last.Attachments) > 0 {
			c.Preview = "[attachment]"
		}
	}
	return c, nil
}

func (s *Store) listConversations(ctx context.Context, profileID string) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.title, c.provider, c.model, c.provider_sort, c.provider_only, c.allow_fallbacks,
		       c.tools_enabled, c.max_tool_calls, c.reasoning_effort,
		       c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id),
		       COALESCE((
		           SELECT CASE
		               WHEN m.content != '' THEN m.content
		               WHEN m.attachments NOT IN ('[]', 'null', '') THEN '[attachment]'
		               ELSE ''
		           END
		           FROM messages m
		           WHERE m.conversation_id = c.id
		           ORDER BY m.seq DESC LIMIT 1
		       ), '')
		FROM conversations c
		WHERE c.profile_id = ?
		ORDER BY c.updated_at DESC
		LIMIT 500`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.Provider, &c.Model, &c.ProviderSort, &c.ProviderOnly, &c.AllowFallbacks,
			&c.ToolsEnabled, &c.MaxToolCalls, &c.ReasoningEffort,
			&c.CreatedAt, &c.UpdatedAt, &c.MessageCount, &c.Preview); err != nil {
			return nil, err
		}
		c.Preview = previewText(c.Preview)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) getConversation(ctx context.Context, profileID, id string) (*Conversation, error) {
	var c Conversation
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, provider, model, system_prompt, provider_sort, provider_only, allow_fallbacks,
		       tools_enabled, max_tool_calls, reasoning_effort, created_at, updated_at
		FROM conversations WHERE id = ? AND profile_id = ?`, id, profileID).
		Scan(&c.ID, &c.Title, &c.Provider, &c.Model, &c.SystemPrompt,
			&c.ProviderSort, &c.ProviderOnly, &c.AllowFallbacks,
			&c.ToolsEnabled, &c.MaxToolCalls, &c.ReasoningEffort, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, content, attachments, citations, tool_run, reasoning, steps, created_at
		FROM messages WHERE conversation_id = ?
		ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	c.Messages = []Message{}
	for rows.Next() {
		var m Message
		var attachmentsJSON, citationsJSON, toolRunJSON, stepsJSON string
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &attachmentsJSON,
			&citationsJSON, &toolRunJSON, &m.Reasoning, &stepsJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(attachmentsJSON), &m.Attachments); err != nil {
			return nil, fmt.Errorf("decode message attachments: %w", err)
		}
		if err := json.Unmarshal([]byte(citationsJSON), &m.Citations); err != nil {
			return nil, fmt.Errorf("decode message citations: %w", err)
		}
		if toolRunJSON != "" {
			if err := json.Unmarshal([]byte(toolRunJSON), &m.ToolRun); err != nil {
				return nil, fmt.Errorf("decode message tool run: %w", err)
			}
		}
		if stepsJSON != "" {
			if err := json.Unmarshal([]byte(stepsJSON), &m.Steps); err != nil {
				return nil, fmt.Errorf("decode message steps: %w", err)
			}
		}
		c.Messages = append(c.Messages, m)
	}
	return &c, rows.Err()
}

// updateConversation applies only the fields that are non-nil. SQL fragments
// are fixed strings; every value is parameterised.
func (s *Store) updateConversation(ctx context.Context, profileID, id string, upd ConversationUpdate) error {
	sets := make([]string, 0, 8)
	args := make([]any, 0, 9)

	if upd.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *upd.Title)
	}
	if upd.SystemPrompt != nil {
		sets = append(sets, "system_prompt = ?")
		args = append(args, *upd.SystemPrompt)
	}
	if upd.Provider != nil {
		sets = append(sets, "provider = ?")
		args = append(args, *upd.Provider)
	}
	if upd.Model != nil {
		sets = append(sets, "model = ?")
		args = append(args, *upd.Model)
	}
	if upd.ProviderSort != nil {
		sets = append(sets, "provider_sort = ?")
		args = append(args, *upd.ProviderSort)
	}
	if upd.ProviderOnly != nil {
		sets = append(sets, "provider_only = ?")
		args = append(args, *upd.ProviderOnly)
	}
	if upd.AllowFallbacks != nil {
		sets = append(sets, "allow_fallbacks = ?")
		args = append(args, boolInt(*upd.AllowFallbacks))
	}
	if upd.ToolsEnabled != nil {
		sets = append(sets, "tools_enabled = ?")
		args = append(args, boolInt(*upd.ToolsEnabled))
	}
	if upd.MaxToolCalls != nil {
		sets = append(sets, "max_tool_calls = ?")
		args = append(args, clampToolCalls(*upd.MaxToolCalls))
	}
	if upd.ReasoningEffort != nil {
		sets = append(sets, "reasoning_effort = ?")
		args = append(args, strings.TrimSpace(*upd.ReasoningEffort))
	}
	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, nowMillis(), id, profileID)

	res, err := s.db.ExecContext(ctx,
		"UPDATE conversations SET "+strings.Join(sets, ", ")+" WHERE id = ? AND profile_id = ?", args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) deleteConversation(ctx context.Context, profileID, id string) error {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM conversations WHERE id = ? AND profile_id = ?", id, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) deleteMessage(ctx context.Context, profileID, id string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM messages
		WHERE id = ? AND conversation_id IN (
			SELECT id FROM conversations WHERE profile_id = ?
		)`, id, profileID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// appendUserMessage inserts the user turn and updates the conversation in one
// transaction. It runs only after upstream accepted the request, so a rate
// limit or auth failure leaves no orphan message behind.
func (s *Store) appendUserMessage(ctx context.Context, profileID, convID, provider, model, content string, attachments []Attachment) (*Message, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	var title string
	err = tx.QueryRowContext(ctx,
		"SELECT title FROM conversations WHERE id = ? AND profile_id = ?", convID, profileID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}

	attachmentsJSON := []byte("[]")
	if len(attachments) > 0 {
		var err error
		attachmentsJSON, err = json.Marshal(attachments)
		if err != nil {
			return nil, "", fmt.Errorf("encode message attachments: %w", err)
		}
	}

	now := nowMillis()
	msg := &Message{ID: newID(), Role: "user", Content: content, Attachments: attachments, CreatedAt: now}

	var seq int64
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE conversation_id = ?", convID).Scan(&seq); err != nil {
		return nil, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, attachments, seq, created_at)
		 VALUES (?, ?, 'user', ?, ?, ?, ?)`,
		msg.ID, convID, content, string(attachmentsJSON), seq, now); err != nil {
		return nil, "", err
	}

	if title == "" || title == "New chat" {
		title = deriveTitle(content)
		if title == "New chat" && len(attachments) > 0 {
			title = deriveTitle(attachments[0].Name)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE conversations SET title = ?, provider = ?, model = ?, updated_at = ? WHERE id = ?",
		title, provider, model, now, convID); err != nil {
		return nil, "", err
	}

	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return msg, title, nil
}

// editUserMessage replaces a user message and drops everything that followed
// it, then re-runs the turn. Returns the (possibly unchanged) title.
func (s *Store) editUserMessage(ctx context.Context, profileID, convID, msgID, content, provider, model string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var title string
	if err := tx.QueryRowContext(ctx,
		"SELECT title FROM conversations WHERE id = ? AND profile_id = ?", convID, profileID).Scan(&title); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	var role string
	var seq int64
	err = tx.QueryRowContext(ctx,
		"SELECT role, seq FROM messages WHERE id = ? AND conversation_id = ?", msgID, convID).Scan(&role, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if role != "user" {
		return "", ErrNotFound
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE messages SET content = ? WHERE id = ? AND conversation_id = ?", content, msgID, convID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM messages WHERE conversation_id = ? AND seq > ?", convID, seq); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE conversations SET provider = ?, model = ?, updated_at = ? WHERE id = ?",
		provider, model, nowMillis(), convID); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return title, nil
}

// saveAssistant writes the buffered reply once. For a regenerate it updates
// the existing row; if that row vanished (deleted from another tab) it inserts
// under the same id rather than losing the reply.
func (s *Store) saveAssistant(ctx context.Context, profileID, convID, msgID, content, reasoning string, steps []ChatStep, citations []Citation, toolRun *ToolRun, replace bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM conversations WHERE id = ? AND profile_id = ?", convID, profileID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	citationsJSON := []byte("[]")
	if len(citations) > 0 {
		citationsJSON, err = json.Marshal(citations)
		if err != nil {
			return fmt.Errorf("encode citations: %w", err)
		}
	}
	toolRunJSON := []byte("")
	if toolRun != nil {
		toolRunJSON, err = json.Marshal(toolRun)
		if err != nil {
			return fmt.Errorf("encode tool run: %w", err)
		}
	}
	stepsJSON := []byte("[]")
	if len(steps) > 0 {
		stepsJSON, err = json.Marshal(steps)
		if err != nil {
			return fmt.Errorf("encode message steps: %w", err)
		}
	}

	now := nowMillis()
	insert := true

	if replace {
		res, err := tx.ExecContext(ctx,
			`UPDATE messages SET content = ?, reasoning = ?, steps = ?, citations = ?, tool_run = ?, created_at = ?
			 WHERE id = ? AND conversation_id = ? AND role = 'assistant'`,
			content, reasoning, string(stepsJSON), string(citationsJSON), string(toolRunJSON), now, msgID, convID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		insert = n == 0
	}

	if insert {
		var seq int64
		if err := tx.QueryRowContext(ctx,
			"SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE conversation_id = ?", convID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages (id, conversation_id, role, content, reasoning, steps, citations, tool_run, seq, created_at)
			 VALUES (?, ?, 'assistant', ?, ?, ?, ?, ?, ?, ?)`,
			msgID, convID, content, reasoning, string(stepsJSON), string(citationsJSON), string(toolRunJSON), seq, now); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE conversations SET updated_at = ? WHERE id = ?", now, convID); err != nil {
		return err
	}

	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func deriveTitle(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	if title == "" {
		return "New chat"
	}
	runes := []rune(title)
	if len(runes) > 60 {
		return strings.TrimSpace(string(runes[:60])) + "…"
	}
	return title
}

var markdownLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// previewText turns the newest message into one readable sidebar line instead
// of showing raw markdown.
func previewText(content string) string {
	text := markdownLink.ReplaceAllString(content, "$1")
	text = strings.NewReplacer(
		"#", "", "*", "", "`", "", "_", "", ">", "", "|", " ", "~", "",
	).Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, 120)
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
