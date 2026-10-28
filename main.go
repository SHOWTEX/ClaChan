package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/time/rate"
)

//go:embed static
var staticFiles embed.FS

// version is set at build time with -X main.version for tagged releases.
var version = "1.0.0"

const (
	// maxStreams is a hard cap. A third concurrent generation is rejected with
	// 503 immediately: this box does not queue work it cannot finish.
	maxStreams = 2
)

type Server struct {
	cfg       *Config
	log       *slog.Logger
	static    fs.FS
	started   time.Time
	client    *http.Client
	providers map[string]*Provider
	models    *modelCache
	store     *Store
	limiter   *rate.Limiter
	sem       chan struct{}
	streams   atomic.Int32
	hashSem   chan struct{}
	logins    *loginLimiter
}

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1:]))
	}
	os.Exit(run())
}

func run() int {
	loadDotEnv(".env")
	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		logger.Error("open embedded static files", "err", err)
		return 1
	}

	store, err := openStore(cfg.DBPath)
	if err != nil {
		logger.Error("open database", "path", cfg.DBPath, "err", err)
		return 1
	}
	defer store.Close()

	// Housekeeping: expired sessions are useless and cheap to remove.
	if err := store.deleteExpiredSessions(context.Background(), nowMillis()); err != nil {
		logger.Warn("prune expired sessions", "err", err)
	}
	if n, err := store.countProfiles(context.Background()); err == nil && n == 0 {
		logger.Info("no profiles yet; the first visit creates the owner profile")
	}

	s := &Server{
		cfg:       cfg,
		log:       logger,
		static:    staticRoot,
		started:   time.Now(),
		client:    newHTTPClient(),
		providers: newProviders(cfg),
		store:     store,
		limiter:   rate.NewLimiter(rate.Limit(chatRatePerSecond), chatRateBurst),
		sem:       make(chan struct{}, maxStreams),
		hashSem:   make(chan struct{}, 2),
		logins:    newLoginLimiter(),
	}
	s.models = newModelCache(s.client, logger)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s.models.start(rootCtx, s.providers)
	if cfg.APIKey == "" {
		logger.Warn("OPENROUTER_API_KEY is not set; chat requests will be rejected until it is")
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Streaming responses must never be cut off by a write deadline.
		WriteTimeout: 0,
		BaseContext:  func(net.Listener) context.Context { return rootCtx },
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("clachan starting",
			"version", version,
			"addr", cfg.Addr,
			"db", cfg.DBPath,
			"provider", "openrouter",
			"api_key", cfg.APIKey != "",
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", "err", err)
		return 1
	case <-rootCtx.Done():
	}

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	return 0
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public: the login screen and health.
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	// Session required.
	mux.HandleFunc("POST /api/auth/password", s.requireSession(s.handleChangePassword))
	mux.HandleFunc("PATCH /api/profile", s.requireSession(s.handleUpdateProfile))
	mux.HandleFunc("GET /api/profiles", s.requireSession(s.handleListProfiles))
	mux.HandleFunc("POST /api/profiles", s.requireSession(s.handleCreateProfile))
	mux.HandleFunc("GET /api/prompts", s.requireSession(s.handleListPrompts))
	mux.HandleFunc("POST /api/prompts", s.requireSession(s.handleCreatePrompt))
	mux.HandleFunc("PATCH /api/prompts/{id}", s.requireSession(s.handleUpdatePrompt))
	mux.HandleFunc("DELETE /api/prompts/{id}", s.requireSession(s.handleDeletePrompt))
	mux.HandleFunc("GET /api/models", s.requireSession(s.handleModels))
	mux.HandleFunc("GET /api/model-endpoints", s.requireSession(s.handleModelEndpoints))
	mux.HandleFunc("GET /api/conversations", s.requireSession(s.handleListConversations))
	mux.HandleFunc("POST /api/conversations", s.requireSession(s.handleCreateConversation))
	mux.HandleFunc("POST /api/conversations/import", s.requireSession(s.handleImportConversation))
	mux.HandleFunc("GET /api/conversations/{id}", s.requireSession(s.handleGetConversation))
	mux.HandleFunc("PATCH /api/conversations/{id}", s.requireSession(s.handleUpdateConversation))
	mux.HandleFunc("DELETE /api/conversations/{id}", s.requireSession(s.handleDeleteConversation))
	mux.HandleFunc("DELETE /api/messages/{id}", s.requireSession(s.handleDeleteMessage))
	mux.HandleFunc("POST /api/chat", s.requireSession(s.handleChat))

	mux.Handle("GET /", s.staticHandler())
	return s.middleware(mux)
}

func (s *Server) staticHandler() http.Handler {
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

// newHTTPClient is shared by every upstream call. Idle connections are kept
// warm: repeated TLS handshakes are expensive on this CPU.
func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: transport}
}
