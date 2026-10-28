package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Config is the complete server configuration. Only the API key is sensitive.
type Config struct {
	Addr                string
	DBPath              string
	APIKey              string
	BaseURL             string
	Referer             string
	AppTitle            string
	DefaultModel        string
	DefaultSystemPrompt string
}

const (
	defaultAddr   = ":8080"
	defaultDBPath = "data/clachan.db"
	defaultBase   = "https://openrouter.ai/api/v1"
	defaultModel  = "deepseek/deepseek-v4.1-flash-1017"
	defaultOrg    = "https://github.com/SHOWTEX/ClaChan"
	defaultTitle  = "Clachan"

	// chatRatePerSecond and chatRateBurst protect the Pi from a runaway client
	// (a stuck tab retrying in a loop). The hard capacity limit is the stream
	// semaphore, not this.
	chatRatePerSecond = 1
	chatRateBurst     = 4
)

func loadConfig() *Config {
	return &Config{
		Addr:                env("CLACHAN_ADDR", defaultAddr),
		DBPath:              filepath.FromSlash(env("CLACHAN_DB", defaultDBPath)),
		APIKey:              strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		BaseURL:             strings.TrimRight(env("OPENROUTER_BASE_URL", defaultBase), "/"),
		Referer:             env("OPENROUTER_REFERER", defaultOrg),
		AppTitle:            env("OPENROUTER_TITLE", defaultTitle),
		DefaultModel:        env("DEFAULT_MODEL", defaultModel),
		DefaultSystemPrompt: env("DEFAULT_SYSTEM_PROMPT", ""),
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a minimal KEY=VALUE file. Values already present in the
// environment always win, so a systemd EnvironmentFile takes precedence over
// a stray .env left in the working directory.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
}
