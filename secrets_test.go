package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretBoxRoundTrip(t *testing.T) {
	box, err := newSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("newSecretBox: %v", err)
	}

	sealed, err := box.seal("sk-or-v1-secret-value")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !isSealedSecret(sealed) {
		t.Fatalf("sealed value missing prefix: %q", sealed)
	}
	if strings.Contains(sealed, "secret-value") {
		t.Fatalf("ciphertext contains plaintext: %q", sealed)
	}
	opened, err := box.open(sealed)
	if err != nil || opened != "sk-or-v1-secret-value" {
		t.Fatalf("open = %q, %v", opened, err)
	}

	other, _ := newSecretBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.open(sealed); err == nil {
		t.Fatal("wrong key decrypted the secret")
	}
	if _, err := box.open("plaintext"); err == nil {
		t.Fatal("unsealed value was accepted by open")
	}
	if got, err := box.openLoose("plaintext"); err != nil || got != "plaintext" {
		t.Fatalf("openLoose legacy = %q, %v", got, err)
	}

	if keyFingerprint("key-a") == keyFingerprint("key-b") {
		t.Fatal("different keys share a fingerprint")
	}
	if keyFingerprint("") != "none" {
		t.Fatalf("empty fingerprint = %q", keyFingerprint(""))
	}
}

func TestLoadSecretBoxCreatesStableKeyFile(t *testing.T) {
	t.Setenv("CLACHAN_SECRET_KEY", "")
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data", "clachan.db")

	box, err := loadSecretBox(dbPath)
	if err != nil {
		t.Fatalf("loadSecretBox: %v", err)
	}
	sealed, err := box.seal("tvly-test-key")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	keyPath := filepath.Join(dir, "data", "secret.key")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("secret key was not created: %v", err)
	}
	if info.Size() < 32 {
		t.Fatalf("secret key file is too small: %d", info.Size())
	}

	reloaded, err := loadSecretBox(dbPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, err := reloaded.open(sealed); err != nil || got != "tvly-test-key" {
		t.Fatalf("reloaded open = %q, %v", got, err)
	}
}

func TestLoadSecretBoxPassphrase(t *testing.T) {
	t.Setenv("CLACHAN_SECRET_KEY", "a-long-enough-passphrase")
	dbPath := filepath.Join(t.TempDir(), "data", "clachan.db")

	box, err := loadSecretBox(dbPath)
	if err != nil {
		t.Fatalf("loadSecretBox: %v", err)
	}
	sealed, err := box.seal("sk-or-v1-passphrase-key")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	again, err := loadSecretBox(dbPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, err := again.open(sealed); err != nil || got != "sk-or-v1-passphrase-key" {
		t.Fatalf("reopened = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), "secret.key")); !os.IsNotExist(err) {
		t.Fatalf("passphrase mode wrote a key file: %v", err)
	}
}

func TestParseSecretKey(t *testing.T) {
	if _, err := parseSecretKey("short"); err == nil {
		t.Fatal("short passphrase was accepted")
	}
	hexKey := strings.Repeat("ab", 32)
	got, err := parseSecretKey(hexKey)
	if err != nil || len(got) != 32 {
		t.Fatalf("hex key = %d, %v", len(got), err)
	}
	pass, err := parseSecretKey("a-long-enough-passphrase")
	if err != nil || len(pass) == 0 {
		t.Fatalf("passphrase = %d, %v", len(pass), err)
	}
}

func TestMigratePlaintextBackendKeys(t *testing.T) {
	store := newTestStore(t)
	profile := createTestProfile(t, store, "Owner", true)
	ctx := context.Background()

	if err := store.saveProfileBackend(ctx, profile.ID, ProfileBackend{
		BackendID: backendOllama,
		BaseURL:   "http://127.0.0.1:11434/v1",
		APIKey:    "plain-local-key",
		Model:     "llama3",
	}); err != nil {
		t.Fatalf("save backend: %v", err)
	}

	box, err := newSecretBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("newSecretBox: %v", err)
	}
	count, err := migratePlaintextSecrets(ctx, store, box)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if count != 1 {
		t.Fatalf("migrated = %d, want 1", count)
	}

	saved, err := store.profileBackend(ctx, profile.ID, backendOllama)
	if err != nil {
		t.Fatalf("profileBackend: %v", err)
	}
	if !isSealedSecret(saved.APIKey) {
		t.Fatalf("key is still plaintext: %q", saved.APIKey)
	}
	opened, err := box.open(saved.APIKey)
	if err != nil || opened != "plain-local-key" {
		t.Fatalf("opened = %q, %v", opened, err)
	}

	again, err := migratePlaintextSecrets(ctx, store, box)
	if err != nil || again != 0 {
		t.Fatalf("second migration = %d, %v", again, err)
	}
}
