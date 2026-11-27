package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// secretPrefix marks an encrypted value in SQLite. Anything without it is a
// legacy plaintext value from before secrets were encrypted at rest.
const secretPrefix = "enc:v1:"

const (
	secretKeyIterations = 210_000
	secretKeySalt       = "clachan-secret-key-v1"
)

var (
	errSecretUndecryptable = errors.New("secret cannot be decrypted with this server key")
	secretAAD              = []byte("clachan-secrets-v1")
)

// secretBox encrypts small profile secrets with AES-256-GCM. The key comes from
// CLACHAN_SECRET_KEY when set, otherwise from a generated secret.key beside the
// database. Losing both makes saved keys unrecoverable; it never makes them
// readable.
type secretBox struct {
	aead cipher.AEAD
}

func newSecretBox(key []byte) (*secretBox, error) {
	if len(key) == 0 {
		return nil, errors.New("empty secret key")
	}
	// A 32-byte key is used directly. A passphrase is stretched with PBKDF2 so
	// an operator can supply a memorable value without making offline guesses
	// cheap. A random key is still preferred.
	material := key
	if len(material) != 32 {
		derived, err := pbkdf2.Key(sha256.New, string(material), []byte(secretKeySalt), secretKeyIterations, 32)
		if err != nil {
			return nil, err
		}
		material = derived
	}
	block, err := aes.NewCipher(material)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

// loadSecretBox resolves the server's encryption key. CLACHAN_SECRET_KEY wins
// over the key file so a deployment can keep the key out of the data volume.
func loadSecretBox(dbPath string) (*secretBox, error) {
	if raw := strings.TrimSpace(os.Getenv("CLACHAN_SECRET_KEY")); raw != "" {
		key, err := parseSecretKey(raw)
		if err != nil {
			return nil, fmt.Errorf("CLACHAN_SECRET_KEY: %w", err)
		}
		return newSecretBox(key)
	}

	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	path := filepath.Join(dir, "secret.key")

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, parseErr := parseSecretKey(strings.TrimSpace(string(raw)))
		if parseErr != nil {
			return nil, fmt.Errorf("read %s: %w", path, parseErr)
		}
		return newSecretBox(key)
	case errors.Is(err, os.ErrNotExist):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate secret key: %w", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create secret key directory: %w", err)
		}
		// 0600 is enforced on Unix. On Windows the per-user install directory
		// already limits access.
		if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		return newSecretBox(key)
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
}

// parseSecretKey accepts 64 hex characters, base64 for exactly 32 bytes, or a
// passphrase that is stretched with PBKDF2 to 32 bytes.
func parseSecretKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("key is empty")
	}
	if len(raw) == 64 {
		if decoded, err := hex.DecodeString(raw); err == nil {
			return decoded, nil
		}
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding,
	} {
		if decoded, err := encoding.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if len(raw) < 16 {
		return nil, errors.New("use at least 16 characters or a 64-character hex key")
	}
	return pbkdf2.Key(sha256.New, raw, []byte(secretKeySalt), secretKeyIterations, 32)
}

func (b *secretBox) seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if b == nil {
		return "", errors.New("secret storage is not initialised")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := b.aead.Seal(nil, nonce, []byte(plaintext), secretAAD)
	payload := append(nonce, ciphertext...)
	return secretPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (b *secretBox) open(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if b == nil {
		return "", errors.New("secret storage is not initialised")
	}
	if !isSealedSecret(value) {
		return "", errors.New("value is not encrypted")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, secretPrefix))
	if err != nil {
		return "", errSecretUndecryptable
	}
	nonceSize := b.aead.NonceSize()
	if len(payload) < nonceSize+b.aead.Overhead() {
		return "", errSecretUndecryptable
	}
	plaintext, err := b.aead.Open(nil, payload[:nonceSize], payload[nonceSize:], secretAAD)
	if err != nil {
		return "", errSecretUndecryptable
	}
	return string(plaintext), nil
}

// openLoose lets a database written before encryption keep working until the
// startup migration has rewritten it.
func (b *secretBox) openLoose(value string) (string, error) {
	if value == "" || !isSealedSecret(value) {
		return value, nil
	}
	return b.open(value)
}

func isSealedSecret(value string) bool {
	return strings.HasPrefix(value, secretPrefix)
}

// keyFingerprint identifies a key without storing or logging it. It is used to
// keep model caches separate when profiles use different OpenRouter keys.
func keyFingerprint(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:12])
}

// migratePlaintextSecrets rewrites legacy plaintext backend keys. It runs at
// startup, before the HTTP server accepts requests.
func migratePlaintextSecrets(ctx context.Context, store *Store, box *secretBox) (int, error) {
	if store == nil || box == nil {
		return 0, nil
	}
	rows, err := store.listAllProfileBackends(ctx)
	if err != nil {
		return 0, err
	}
	migrated := 0
	for _, row := range rows {
		if row.APIKey == "" || isSealedSecret(row.APIKey) {
			continue
		}
		sealed, err := box.seal(row.APIKey)
		if err != nil {
			return migrated, fmt.Errorf("encrypt backend key for profile %s: %w", row.ProfileID, err)
		}
		if err := store.updateProfileBackendKey(ctx, row.ProfileID, row.BackendID, sealed); err != nil {
			return migrated, fmt.Errorf("save encrypted backend key for profile %s: %w", row.ProfileID, err)
		}
		migrated++
	}
	return migrated, nil
}
