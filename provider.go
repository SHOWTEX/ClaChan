package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const providerOpenRouter = "openrouter"

// Provider is one OpenAI-compatible upstream. Clachan ships with OpenRouter
// only, but keeping the shape means a second provider is a config change, not
// a refactor.
type Provider struct {
	ID           string
	Label        string
	BaseURL      string
	APIKey       string
	Referer      string
	AppTitle     string
	DefaultModel string
}

func newProviders(cfg *Config) map[string]*Provider {
	p := &Provider{
		ID:           providerOpenRouter,
		Label:        "OpenRouter",
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Referer:      cfg.Referer,
		AppTitle:     cfg.AppTitle,
		DefaultModel: cfg.DefaultModel,
	}
	return map[string]*Provider{p.ID: p}
}

func (p *Provider) enabled() bool { return p.APIKey != "" }

func (p *Provider) headers(req *http.Request) {
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if p.Referer != "" {
		req.Header.Set("HTTP-Referer", p.Referer)
	}
	if p.AppTitle != "" {
		req.Header.Set("X-Title", p.AppTitle)
	}
}

type responseInputMessage struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type reasoningRequest struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// responses opens a streaming OpenResponses request. Unlike chat completions,
// this surface exposes server-tool output items and reasoning items, which is
// what the transcript needs in order to render separate steps.
func (p *Provider) responses(ctx context.Context, client *http.Client, model string, input []responseInputMessage,
	instructions string, prefs *providerPreferences, tools []serverTool,
	maxToolCalls int, reasoning *reasoningRequest) (*http.Response, error) {

	body := struct {
		Model        string                 `json:"model"`
		Input        []responseInputMessage `json:"input"`
		Instructions string                 `json:"instructions,omitempty"`
		Stream       bool                   `json:"stream"`
		Provider     *providerPreferences   `json:"provider,omitempty"`
		Tools        []serverTool           `json:"tools,omitempty"`
		MaxToolCalls int                    `json:"max_tool_calls,omitempty"`
		Reasoning    *reasoningRequest      `json:"reasoning,omitempty"`
	}{
		Model:        model,
		Input:        input,
		Instructions: instructions,
		Stream:       true,
		Provider:     prefs,
		Tools:        tools,
		MaxToolCalls: maxToolCalls,
		Reasoning:    reasoning,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode responses request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	p.headers(req)

	return client.Do(req)
}

// Model is the lean subset Clachan keeps in memory and sends to the browser.
// Pricing is normalised to USD per million tokens so the UI never has to parse
// OpenRouter's per-token decimal strings.
type Model struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	CanonicalSlug       string         `json:"canonicalSlug,omitempty"`
	ContextLength       int            `json:"contextLength,omitempty"`
	Created             int64          `json:"created,omitempty"`
	PromptPricePerM     float64        `json:"promptPricePerM,omitempty"`
	CompletionPricePerM float64        `json:"completionPricePerM,omitempty"`
	MaxCompletionTokens int            `json:"maxCompletionTokens,omitempty"`
	SupportsVision      bool           `json:"supportsVision,omitempty"`
	SupportsFiles       bool           `json:"supportsFiles,omitempty"`
	SupportsTools       bool           `json:"supportsTools,omitempty"`
	SupportsReasoning   bool           `json:"supportsReasoning,omitempty"`
	Reasoning           *ReasoningInfo `json:"reasoning,omitempty"`
	IsModerated         bool           `json:"isModerated,omitempty"`
	Free                bool           `json:"free,omitempty"`
}

// ReasoningInfo is the model-specific reasoning contract used by the effort
// menu. OpenRouter reports supported efforts in descending order.
type ReasoningInfo struct {
	SupportedEfforts  []string `json:"supportedEfforts,omitempty"`
	AllEfforts        bool     `json:"allEfforts,omitempty"`
	DefaultEffort     string   `json:"defaultEffort,omitempty"`
	DefaultEnabled    bool     `json:"defaultEnabled,omitempty"`
	Mandatory         bool     `json:"mandatory,omitempty"`
	SupportsMaxTokens bool     `json:"supportsMaxTokens,omitempty"`
}

var allReasoningEfforts = []string{"max", "xhigh", "high", "medium", "low", "minimal", "none"}

type reasoningPayload struct {
	SupportedEfforts  json.RawMessage `json:"supported_efforts"`
	DefaultEffort     string          `json:"default_effort"`
	DefaultEnabled    *bool           `json:"default_enabled"`
	Mandatory         bool            `json:"mandatory"`
	SupportsMaxTokens bool            `json:"supports_max_tokens"`
}

// Endpoint is one routable provider endpoint for a model.
type Endpoint struct {
	Name                string  `json:"name"`
	ProviderName        string  `json:"providerName"`
	Tag                 string  `json:"tag"`
	ContextLength       int     `json:"contextLength"`
	MaxCompletionTokens int     `json:"maxCompletionTokens,omitempty"`
	PromptPricePerM     float64 `json:"promptPricePerM"`
	CompletionPricePerM float64 `json:"completionPricePerM"`
	Quantization        string  `json:"quantization,omitempty"`
	ServiceTier         string  `json:"serviceTier,omitempty"`
	Status              int     `json:"status"`
	Available           bool    `json:"available"`
	Uptime              float64 `json:"uptime,omitempty"`
	Uptime30m           float64 `json:"uptime30m,omitempty"`
	LatencyMS           float64 `json:"latencyMs,omitempty"`
	Throughput          float64 `json:"throughput,omitempty"`
	NativeWebSearch     bool    `json:"nativeWebSearch,omitempty"`
	SupportsTools       bool    `json:"supportsTools,omitempty"`
	SupportsCaching     bool    `json:"supportsCaching,omitempty"`
}

type ProviderModels struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Enabled      bool    `json:"enabled"`
	DefaultModel string  `json:"defaultModel"`
	Models       []Model `json:"models"`
	Error        string  `json:"error,omitempty"`
}

type upstreamMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatPlugin struct {
	ID  string     `json:"id"`
	PDF *pdfPlugin `json:"pdf,omitempty"`
}

type pdfPlugin struct {
	Engine string `json:"engine"`
}

type providerPreferences struct {
	Sort           string   `json:"sort,omitempty"`
	Only           []string `json:"only,omitempty"`
	Ignore         []string `json:"ignore,omitempty"`
	AllowFallbacks *bool    `json:"allow_fallbacks,omitempty"`
}

type serverToolUse struct {
	ToolCallsExecuted  int `json:"tool_calls_executed"`
	ToolCallsRequested int `json:"tool_calls_requested"`
	WebSearchRequests  int `json:"web_search_requests"`
}

// usage mirrors both OpenRouter usage shapes: chat completions uses
// prompt/completion, while the Responses API uses input/output.
type usage struct {
	PromptTokens         int            `json:"prompt_tokens"`
	CompletionTokens     int            `json:"completion_tokens"`
	InputTokens          int            `json:"input_tokens"`
	OutputTokens         int            `json:"output_tokens"`
	TotalTokens          int            `json:"total_tokens"`
	ServerToolUse        *serverToolUse `json:"server_tool_use,omitempty"`
	ServerToolUseDetails *serverToolUse `json:"server_tool_use_details,omitempty"`
	OutputTokensDetails  *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
}

func (u *usage) toolUse() *serverToolUse {
	if u == nil {
		return nil
	}
	if u.ServerToolUseDetails != nil {
		return u.ServerToolUseDetails
	}
	return u.ServerToolUse
}

type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// streamChunk is one decoded SSE frame from a chat completion stream.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content     string           `json:"content"`
			Annotations []annotation     `json:"annotations"`
			ToolCalls   []streamToolCall `json:"tool_calls"`
		} `json:"delta"`
		Message *struct {
			Annotations []annotation `json:"annotations"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// chat opens a streaming chat completion. The caller owns resp.Body and must
// close it.
func (p *Provider) chat(ctx context.Context, client *http.Client, model string, msgs []upstreamMessage, prefs *providerPreferences, plugins []chatPlugin, tools []serverTool, maxToolCalls int) (*http.Response, error) {
	body := struct {
		Model         string               `json:"model"`
		Messages      []upstreamMessage    `json:"messages"`
		Stream        bool                 `json:"stream"`
		StreamOptions map[string]bool      `json:"stream_options,omitempty"`
		Provider      *providerPreferences `json:"provider,omitempty"`
		Plugins       []chatPlugin         `json:"plugins,omitempty"`
		Tools         []serverTool         `json:"tools,omitempty"`
		MaxToolCalls  int                  `json:"max_tool_calls,omitempty"`
	}{
		Model:    model,
		Messages: msgs,
		Stream:   true,
		// OpenRouter returns a final usage frame when asked.
		StreamOptions: map[string]bool{"include_usage": true},
		Provider:      prefs,
		Plugins:       plugins,
		Tools:         tools,
		MaxToolCalls:  maxToolCalls,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	p.headers(req)

	return client.Do(req)
}

// upstreamErrorMessage extracts a useful, bounded description from an error
// response. It consumes the body, so callers must not read it afterwards.
func upstreamErrorMessage(resp *http.Response) string {
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return ""
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &payload); err == nil && payload.Error.Message != "" {
		return truncate(payload.Error.Message, 300)
	}
	return truncate(strings.TrimSpace(string(b)), 300)
}

/* ------------------------------------------------------------------- models */

type modelCache struct {
	mu        sync.RWMutex
	client    *http.Client
	log       *slog.Logger
	models    map[string][]Model
	errs      map[string]string
	lastOK    map[string]time.Time
	fetchedAt time.Time
	endpoints map[string]endpointEntry
}

type endpointEntry struct {
	endpoints []Endpoint
	err       string
	fetched   time.Time
}

const (
	endpointTTL      = 10 * time.Minute
	endpointErrorTTL = time.Minute
)

func newModelCache(client *http.Client, log *slog.Logger) *modelCache {
	return &modelCache{
		client:    client,
		log:       log,
		models:    make(map[string][]Model),
		errs:      make(map[string]string),
		lastOK:    make(map[string]time.Time),
		endpoints: make(map[string]endpointEntry),
	}
}

// start warms the cache in the background so the server binds immediately even
// if the uplink is down, then retries failures and refreshes every six hours.
func (c *modelCache) start(ctx context.Context, providers map[string]*Provider) {
	go func() {
		c.refresh(ctx, providers, true)

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				full := time.Since(c.fetchedAtTime()) > 6*time.Hour
				c.refresh(ctx, providers, full)
			}
		}
	}()
}

func (c *modelCache) fetchedAtTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fetchedAt
}

func (c *modelCache) refresh(ctx context.Context, providers map[string]*Provider, full bool) {
	for id, p := range providers {
		if !p.enabled() {
			c.setErr(id, "OPENROUTER_API_KEY is not set on the server")
			continue
		}

		if !full {
			c.mu.RLock()
			hasErr := c.errs[id] != ""
			okAt := c.lastOK[id]
			count := len(c.models[id])
			c.mu.RUnlock()
			if !hasErr && count > 0 && time.Since(okAt) < 24*time.Hour {
				continue
			}
		}

		models, err := c.fetch(ctx, p)
		if err != nil {
			c.log.Warn("model list fetch failed", "provider", id, "err", err)
			c.setErr(id, err.Error())
			continue
		}

		c.mu.Lock()
		c.models[id] = models
		c.errs[id] = ""
		c.lastOK[id] = time.Now()
		c.fetchedAt = time.Now()
		c.mu.Unlock()
		c.log.Info("model list cached", "provider", id, "models", len(models))
	}
}

func (c *modelCache) setErr(id, msg string) {
	c.mu.Lock()
	c.errs[id] = msg
	c.mu.Unlock()
}

// fetch pulls the authenticated catalogue: /models/user reflects the key's
// provider preferences, privacy settings, and guardrails. It falls back to the
// public list only if the request fails before any page is read.
func (c *modelCache) fetch(ctx context.Context, p *Provider) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	first := p.BaseURL + "/models/user?output_modalities=text&limit=1000"
	if !p.enabled() {
		first = p.BaseURL + "/models"
	}

	models := make([]Model, 0, 400)
	seen := make(map[string]bool, 400)
	next := first

	for page := 0; page < 10 && next != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		p.headers(req)

		resp, err := c.client.Do(req)
		if err != nil {
			if page == 0 && p.enabled() {
				// An older key or a regional host may not expose /models/user.
				c.log.Warn("models/user failed; falling back to public models", "err", err)
				next = p.BaseURL + "/models"
				continue
			}
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			detail := upstreamErrorMessage(resp)
			resp.Body.Close()
			// A bad key is a configuration error, not a reason to silently show
			// the public catalogue. Other failures fall back once.
			if page == 0 && p.enabled() && resp.StatusCode != http.StatusUnauthorized {
				c.log.Warn("models/user rejected; falling back to public models", "status", resp.StatusCode, "detail", detail)
				next = p.BaseURL + "/models"
				continue
			}
			if detail == "" {
				return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
		}

		var payload struct {
			Data []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				CanonicalSlug string `json:"canonical_slug"`
				Created       int64  `json:"created"`
				ContextLength int    `json:"context_length"`
				Pricing       struct {
					Prompt     string `json:"prompt"`
					Completion string `json:"completion"`
				} `json:"pricing"`
				Architecture struct {
					InputModalities  []string `json:"input_modalities"`
					OutputModalities []string `json:"output_modalities"`
				} `json:"architecture"`
				SupportedParameters []string          `json:"supported_parameters"`
				Reasoning           *reasoningPayload `json:"reasoning"`
				TopProvider         struct {
					MaxCompletionTokens *int `json:"max_completion_tokens"`
					IsModerated         bool `json:"is_moderated"`
				} `json:"top_provider"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode model list: %w", err)
		}
		resp.Body.Close()

		for _, m := range payload.Data {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			if !supportsTextOutput(m.Architecture.OutputModalities) {
				continue
			}
			seen[m.ID] = true
			models = append(models, normalizeModel(m.ID, m.Name, m.CanonicalSlug, m.Created, m.ContextLength,
				m.Pricing.Prompt, m.Pricing.Completion, m.Architecture.InputModalities,
				m.SupportedParameters, m.Reasoning, m.TopProvider.MaxCompletionTokens, m.TopProvider.IsModerated))
		}

		next = resolveNextURL(next, payload.Links.Next)
	}

	if len(models) == 0 {
		return nil, errors.New("provider returned an empty model list")
	}
	return models, nil
}

// supportsTextOutput treats a missing modality list as text (the public list
// predates the field) and rejects image/embedding-only models.
func supportsTextOutput(all []string) bool {
	if len(all) == 0 {
		return true
	}
	for _, modality := range all {
		if modality == "text" {
			return true
		}
	}
	return false
}

func normalizeModel(id, name, slug string, created int64, contextLength int,
	promptPrice, completionPrice string, inputModalities, supported []string,
	reasoning *reasoningPayload, maxCompletion *int, moderated bool) Model {

	if name == "" {
		name = id
	}
	prompt := pricePerMillion(promptPrice)
	completion := pricePerMillion(completionPrice)

	model := Model{
		ID:                  id,
		Name:                name,
		CanonicalSlug:       slug,
		ContextLength:       contextLength,
		Created:             created,
		PromptPricePerM:     prompt,
		CompletionPricePerM: completion,
		IsModerated:         moderated,
		SupportsVision:      containsString(inputModalities, "image"),
		SupportsFiles:       containsString(inputModalities, "file"),
		SupportsTools:       containsString(supported, "tools"),
		SupportsReasoning: containsString(supported, "reasoning") ||
			containsString(supported, "include_reasoning") ||
			containsString(supported, "reasoning_effort") || reasoning != nil,
		Reasoning: normalizeReasoning(reasoning),
		Free:      prompt == 0 && completion == 0,
	}
	if maxCompletion != nil {
		model.MaxCompletionTokens = *maxCompletion
	}
	return model
}

func normalizeReasoning(payload *reasoningPayload) *ReasoningInfo {
	if payload == nil {
		return nil
	}
	info := &ReasoningInfo{
		DefaultEffort:     strings.TrimSpace(payload.DefaultEffort),
		Mandatory:         payload.Mandatory,
		SupportsMaxTokens: payload.SupportsMaxTokens,
	}
	if payload.DefaultEnabled != nil {
		info.DefaultEnabled = *payload.DefaultEnabled
	} else {
		info.DefaultEnabled = true
	}

	raw := strings.TrimSpace(string(payload.SupportedEfforts))
	switch {
	case raw == "":
		// Older catalogue entries omit the field. Keep the default-only menu.
	case raw == "null":
		// null means every gateway effort is accepted.
		info.AllEfforts = true
		info.SupportedEfforts = append([]string(nil), allReasoningEfforts...)
	default:
		var efforts []string
		if err := json.Unmarshal(payload.SupportedEfforts, &efforts); err == nil {
			seen := make(map[string]bool, len(efforts))
			for _, effort := range efforts {
				effort = strings.ToLower(strings.TrimSpace(effort))
				if !validReasoningEffort(effort) || effort == "" || seen[effort] {
					continue
				}
				seen[effort] = true
				info.SupportedEfforts = append(info.SupportedEfforts, effort)
			}
		}
	}
	if info.DefaultEffort != "" && !validReasoningEffort(info.DefaultEffort) {
		info.DefaultEffort = ""
	}
	return info
}

func validReasoningEffort(value string) bool {
	switch value {
	case "max", "xhigh", "high", "medium", "low", "minimal", "none":
		return true
	}
	return false
}

func (c *modelCache) lookup(providerID, modelID string) *Model {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.models[providerID] {
		if c.models[providerID][i].ID == modelID {
			model := c.models[providerID][i]
			return &model
		}
	}
	return nil
}

func pricePerMillion(perToken string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(perToken), 64)
	if err != nil || v < 0 {
		return 0
	}
	// Round away binary floating-point noise (0.09999999999999 -> 0.1).
	perMillion := v * 1_000_000
	return math.Round(perMillion*1_000_000) / 1_000_000
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func resolveNextURL(current, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "?") {
		return current + next
	}
	base, err := url.Parse(current)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(next)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return ""
	}
	return resolved.String()
}

/* ---------------------------------------------------------------- endpoints */

// endpointsFor returns cached provider endpoints for a model. Endpoint data is
// the source of the provider badges: context, price, p50 latency/throughput,
// uptime, quantization, and native tools.
func (c *modelCache) endpointsFor(ctx context.Context, p *Provider, modelID string) ([]Endpoint, error) {
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.endpoints[modelID]
	c.mu.Unlock()
	if ok {
		ttl := endpointTTL
		if entry.err != "" {
			ttl = endpointErrorTTL
		}
		if now.Sub(entry.fetched) < ttl {
			if entry.err != "" {
				return nil, errors.New(entry.err)
			}
			return entry.endpoints, nil
		}
	}

	endpoints, err := c.fetchEndpoints(ctx, p, modelID)

	c.mu.Lock()
	if c.endpoints == nil {
		c.endpoints = make(map[string]endpointEntry)
	}
	if len(c.endpoints) >= 100 {
		var oldestKey string
		var oldest time.Time
		for key, value := range c.endpoints {
			if oldestKey == "" || value.fetched.Before(oldest) {
				oldestKey, oldest = key, value.fetched
			}
		}
		delete(c.endpoints, oldestKey)
	}
	cached := endpointEntry{fetched: time.Now()}
	if err != nil {
		cached.err = err.Error()
		c.endpoints[modelID] = cached
		c.mu.Unlock()
		return nil, err
	}
	cached.endpoints = endpoints
	c.endpoints[modelID] = cached
	c.mu.Unlock()

	return endpoints, nil
}

func (c *modelCache) fetchEndpoints(ctx context.Context, p *Provider, modelID string) ([]Endpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpointsURL(modelID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	p.headers(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := upstreamErrorMessage(resp)
		if detail == "" {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}

	var payload struct {
		Data struct {
			Endpoints []struct {
				Name                string `json:"name"`
				ProviderName        string `json:"provider_name"`
				Tag                 string `json:"tag"`
				ContextLength       int    `json:"context_length"`
				MaxCompletionTokens *int   `json:"max_completion_tokens"`
				Pricing             struct {
					Prompt     string `json:"prompt"`
					Completion string `json:"completion"`
				} `json:"pricing"`
				Quantization            string                     `json:"quantization"`
				ServiceTier             *string                    `json:"service_tier"`
				Status                  int                        `json:"status"`
				UptimeLast30m           *float64                   `json:"uptime_last_30m"`
				UptimeLast1d            *float64                   `json:"uptime_last_1d"`
				LatencyLast30m          *percentileStat            `json:"latency_last_30m"`
				ThroughputLast30m       *percentileStat            `json:"throughput_last_30m"`
				NativeTools             map[string]json.RawMessage `json:"native_tools"`
				SupportedParameters     []string                   `json:"supported_parameters"`
				SupportsImplicitCaching bool                       `json:"supports_implicit_caching"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode endpoints: %w", err)
	}

	seen := make(map[string]bool, len(payload.Data.Endpoints))
	endpoints := make([]Endpoint, 0, len(payload.Data.Endpoints))
	for _, e := range payload.Data.Endpoints {
		if e.Tag == "" || seen[e.Tag] {
			continue
		}
		seen[e.Tag] = true

		name := e.Name
		if name == "" {
			name = e.ProviderName
		}
		ep := Endpoint{
			Name:                name,
			ProviderName:        e.ProviderName,
			Tag:                 e.Tag,
			ContextLength:       e.ContextLength,
			PromptPricePerM:     pricePerMillion(e.Pricing.Prompt),
			CompletionPricePerM: pricePerMillion(e.Pricing.Completion),
			Quantization:        e.Quantization,
			Status:              e.Status,
			Available:           e.Status == 0,
			NativeWebSearch:     hasNativeTool(e.NativeTools, "openrouter:web_search"),
			SupportsTools:       containsString(e.SupportedParameters, "tools"),
			SupportsCaching:     e.SupportsImplicitCaching,
		}
		if e.MaxCompletionTokens != nil {
			ep.MaxCompletionTokens = *e.MaxCompletionTokens
		}
		if e.UptimeLast30m != nil {
			ep.Uptime30m = *e.UptimeLast30m
		}
		// The public site's uptime column is a day-window number; the 30m
		// figure is almost always exactly 100 and reads as fake. Prefer 1d and
		// fall back to 30m only when the day window is unavailable.
		if e.UptimeLast1d != nil {
			ep.Uptime = *e.UptimeLast1d
		} else if e.UptimeLast30m != nil {
			ep.Uptime = *e.UptimeLast30m
		}
		if e.LatencyLast30m != nil {
			ep.LatencyMS = e.LatencyLast30m.P50
		}
		if e.ThroughputLast30m != nil {
			ep.Throughput = e.ThroughputLast30m.P50
		}
		if e.ServiceTier != nil {
			ep.ServiceTier = *e.ServiceTier
		}
		endpoints = append(endpoints, ep)
	}

	if len(endpoints) == 0 {
		return nil, errors.New("no endpoints returned")
	}
	return endpoints, nil
}

type percentileStat struct {
	P50 float64 `json:"p50"`
}

func hasNativeTool(tools map[string]json.RawMessage, name string) bool {
	if tools == nil {
		return false
	}
	_, ok := tools[name]
	return ok
}

func (p *Provider) endpointsURL(modelID string) string {
	parts := strings.Split(modelID, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return p.BaseURL + "/models/" + strings.Join(parts, "/") + "/endpoints"
}

// validModelID keeps the endpoint proxy from being used to walk arbitrary
// upstream paths.
func validModelID(id string) bool {
	if len(id) < 3 || len(id) > 200 || strings.Contains(id, "..") {
		return false
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") || !strings.Contains(id, "/") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '/', r == '-', r == '~', r == '@':
		default:
			return false
		}
	}
	return true
}

func validProviderSort(value string) bool {
	switch value {
	case "", "price", "throughput", "latency":
		return true
	default:
		return false
	}
}

func validProviderTag(tag string) bool {
	if tag == "" || len(tag) > 160 || strings.Contains(tag, "..") {
		return false
	}
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}

// normalizeIgnoredProviders turns a free-form list into a stable, de-duplicated
// comma-separated value. Accepts commas or newlines; keeps the user's casing for
// display, matching is case-insensitive.
func normalizeIgnoredProviders(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n", ",")
	parts := strings.Split(raw, ",")

	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if len(value) > 80 || !validIgnoredProvider(value) {
			return "", errors.New("provider names may only contain letters, numbers, spaces, and . _ : / - + &")
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	if len(out) > 50 {
		return "", errors.New("too many ignored providers (maximum 50)")
	}
	return strings.Join(out, ", "), nil
}

func validIgnoredProvider(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == ' ', r == '.', r == '_', r == ':', r == '/', r == '-', r == '+', r == '&':
		default:
			return false
		}
	}
	return true
}

func ignoredProviderList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// endpointIgnored matches either the full provider tag ("deepinfra/turbo"), a
// provider prefix ("deepinfra"), or the provider's display name.
func endpointIgnored(endpoint Endpoint, ignored []string) bool {
	tag := strings.ToLower(endpoint.Tag)
	name := strings.ToLower(endpoint.ProviderName)
	for _, raw := range ignored {
		want := strings.ToLower(strings.TrimSpace(raw))
		if want == "" {
			continue
		}
		if tag == want || name == want || strings.HasPrefix(tag, want+"/") {
			return true
		}
	}
	return false
}

func (c *modelCache) snapshot(providers map[string]*Provider) ([]ProviderModels, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ProviderModels, 0, len(ids))
	for _, id := range ids {
		p := providers[id]
		out = append(out, ProviderModels{
			ID:           id,
			Label:        p.Label,
			Enabled:      p.enabled(),
			DefaultModel: p.DefaultModel,
			Models:       c.models[id],
			Error:        c.errs[id],
		})
	}

	var fetched int64
	if !c.fetchedAt.IsZero() {
		fetched = c.fetchedAt.UnixMilli()
	}
	return out, fetched
}
