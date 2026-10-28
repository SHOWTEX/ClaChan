package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFetchUserModels(t *testing.T) {
	var gotAuth string
	var requests atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/models/user" {
			t.Errorf("path = %q, want /models/user", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "2" {
			io.WriteString(w, `{"data":[
				{"id":"test/beta","name":"Beta","canonical_slug":"test/beta","created":1700000000,"context_length":32000,
				 "pricing":{"prompt":"0.000001","completion":"0.000002"},
				 "architecture":{"input_modalities":["text"],"output_modalities":["text"]},
				 "supported_parameters":["tools"],"top_provider":{"max_completion_tokens":4096,"is_moderated":false}}
			],"links":{"next":null}}`)
			return
		}

		io.WriteString(w, `{
			"data":[
				{"id":"test/alpha","name":"Alpha","canonical_slug":"test/alpha","created":1600000000,"context_length":128000,
				 "pricing":{"prompt":"0.000003","completion":"0.000015"},
				 "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
				 "supported_parameters":["tools","reasoning"],
				 "reasoning":{"supported_efforts":["high","medium","low"],"default_effort":"medium","default_enabled":true,"mandatory":true},
				 "top_provider":{"max_completion_tokens":8192,"is_moderated":true}},
				{"id":"test/image","name":"Image only","created":1600000000,"context_length":0,
				 "pricing":{"prompt":"0","completion":"0"},
				 "architecture":{"input_modalities":["text"],"output_modalities":["image"]},
				 "supported_parameters":[],"top_provider":{}}
			],
			"links":{"next":"/models/user?cursor=2&output_modalities=text"}
		}`)
	}))
	defer upstream.Close()

	provider := &Provider{ID: providerOpenRouter, BaseURL: upstream.URL, APIKey: "secret-key"}
	cache := newModelCache(upstream.Client(), testLogger())

	models, err := cache.fetch(context.Background(), provider)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 (image-only skipped): %+v", len(models), models)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2 pages", requests.Load())
	}

	alpha := models[0]
	if alpha.ID != "test/alpha" || alpha.Name != "Alpha" || alpha.ContextLength != 128000 {
		t.Fatalf("alpha = %+v", alpha)
	}
	if !alpha.SupportsVision || !alpha.SupportsTools || !alpha.SupportsReasoning {
		t.Fatalf("alpha badges = %+v", alpha)
	}
	if math.Abs(alpha.PromptPricePerM-3) > 1e-9 || math.Abs(alpha.CompletionPricePerM-15) > 1e-9 {
		t.Fatalf("alpha pricing = %v / %v", alpha.PromptPricePerM, alpha.CompletionPricePerM)
	}
	if alpha.MaxCompletionTokens != 8192 || !alpha.IsModerated {
		t.Fatalf("alpha provider info = %+v", alpha)
	}
	if alpha.Reasoning == nil || alpha.Reasoning.AllEfforts || alpha.Reasoning.Mandatory == false ||
		alpha.Reasoning.DefaultEffort != "medium" ||
		strings.Join(alpha.Reasoning.SupportedEfforts, ",") != "high,medium,low" {
		t.Fatalf("alpha reasoning = %+v", alpha.Reasoning)
	}

	if models[1].ID != "test/beta" || !models[1].SupportsTools || models[1].SupportsVision {
		t.Fatalf("beta = %+v", models[1])
	}
}

func TestNormalizeReasoning(t *testing.T) {
	if normalizeReasoning(nil) != nil {
		t.Fatal("nil payload must stay nil")
	}

	all := normalizeReasoning(&reasoningPayload{SupportedEfforts: json.RawMessage(`null`)})
	if all == nil || !all.AllEfforts || len(all.SupportedEfforts) != len(allReasoningEfforts) {
		t.Fatalf("null efforts = %+v", all)
	}

	omitted := normalizeReasoning(&reasoningPayload{})
	if omitted == nil || omitted.AllEfforts || len(omitted.SupportedEfforts) != 0 || !omitted.DefaultEnabled {
		t.Fatalf("omitted efforts = %+v", omitted)
	}

	filtered := normalizeReasoning(&reasoningPayload{
		SupportedEfforts: json.RawMessage(`["HIGH","bogus","high"," low "]`),
		DefaultEffort:    "bogus",
	})
	if filtered == nil || strings.Join(filtered.SupportedEfforts, ",") != "high,low" || filtered.DefaultEffort != "" {
		t.Fatalf("filtered efforts = %+v", filtered)
	}

	cache := newModelCache(http.DefaultClient, testLogger())
	if got := cache.lookup(providerOpenRouter, "missing"); got != nil {
		t.Fatalf("lookup missing = %+v", got)
	}
	cache.models = map[string][]Model{
		providerOpenRouter: {{ID: "test/model", Name: "Test"}},
	}
	got := cache.lookup(providerOpenRouter, "test/model")
	if got == nil || got.Name != "Test" {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestFetchModelsFallsBackToPublic(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/models/user":
			http.Error(w, `{"error":{"message":"not available"}}`, http.StatusNotFound)
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[{"id":"public/one","name":"Public One","context_length":8192,
				"pricing":{"prompt":"0.000001","completion":"0.000002"},
				"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
				"supported_parameters":[],"top_provider":{}}],"links":{"next":null}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	provider := &Provider{ID: providerOpenRouter, BaseURL: upstream.URL, APIKey: "secret-key"}
	cache := newModelCache(upstream.Client(), testLogger())

	models, err := cache.fetch(context.Background(), provider)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(models) != 1 || models[0].ID != "public/one" {
		t.Fatalf("models = %+v", models)
	}
	want := []string{"/models/user", "/models"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestFetchEndpoints(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/models/deepseek/test/endpoints" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"endpoints":[
			{"name":"DeepInfra: DeepSeek","provider_name":"DeepInfra","tag":"deepinfra/fp8",
			 "context_length":128000,"max_completion_tokens":8192,
			 "pricing":{"prompt":"0.00000027","completion":"0.0000011"},
			 "quantization":"fp8","status":0,"uptime_last_30m":99.5,"uptime_last_1d":98.75,
			 "latency_last_30m":{"p50":250,"p75":300,"p90":400,"p99":800},
			 "throughput_last_30m":{"p50":45.2,"p75":38.5,"p90":28.3,"p99":15.1},
			 "native_tools":{"openrouter:web_search":{"type":"web_search_20260209"}},
			 "supported_parameters":["tools"],"supports_implicit_caching":true,"service_tier":"priority"},
			{"name":"Down","provider_name":"Down","tag":"down","context_length":8000,
			 "pricing":{"prompt":"0","completion":"0"},"quantization":null,"status":-1,
			 "uptime_last_30m":null,"latency_last_30m":null,"throughput_last_30m":null,
			 "native_tools":{},"supported_parameters":[],"supports_implicit_caching":false,"service_tier":null}
		]}}`)
	}))
	defer upstream.Close()

	provider := &Provider{ID: providerOpenRouter, BaseURL: upstream.URL, APIKey: "secret-key"}
	cache := newModelCache(upstream.Client(), testLogger())

	endpoints, err := cache.endpointsFor(context.Background(), provider, "deepseek/test")
	if err != nil {
		t.Fatalf("endpointsFor: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %d", len(endpoints))
	}

	first := endpoints[0]
	if first.ProviderName != "DeepInfra" || first.Tag != "deepinfra/fp8" || !first.Available {
		t.Fatalf("first endpoint = %+v", first)
	}
	if math.Abs(first.PromptPricePerM-0.27) > 1e-9 || math.Abs(first.CompletionPricePerM-1.1) > 1e-9 {
		t.Fatalf("endpoint pricing = %v / %v", first.PromptPricePerM, first.CompletionPricePerM)
	}
	if first.ContextLength != 128000 || first.LatencyMS != 250 || first.Throughput != 45.2 ||
		first.Uptime != 98.75 || first.Uptime30m != 99.5 {
		t.Fatalf("endpoint stats = %+v", first)
	}
	if first.Quantization != "fp8" || first.ServiceTier != "priority" {
		t.Fatalf("endpoint metadata = %+v", first)
	}
	if !first.NativeWebSearch || !first.SupportsTools || !first.SupportsCaching {
		t.Fatalf("endpoint badges = %+v", first)
	}
	if endpoints[1].Available {
		t.Fatalf("disabled endpoint marked available: %+v", endpoints[1])
	}

	if _, err := cache.endpointsFor(context.Background(), provider, "deepseek/test"); err != nil {
		t.Fatalf("second endpointsFor: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (cached)", calls.Load())
	}
}

func TestValidModelID(t *testing.T) {
	good := []string{"openai/gpt-4", "deepseek/deepseek-r1", "~anthropic/claude-sonnet-4", "google/gemini-3:free"}
	bad := []string{"", "gpt-4", "/gpt", "openai/", "openai/../secret", "openai/gpt 4", "openai/gpt?x=1"}
	for _, id := range good {
		if !validModelID(id) {
			t.Errorf("validModelID(%q) = false", id)
		}
	}
	for _, id := range bad {
		if validModelID(id) {
			t.Errorf("validModelID(%q) = true", id)
		}
	}
}

func TestIgnoredProviders(t *testing.T) {
	normalized, err := normalizeIgnoredProviders(" DeepInfra,\nnovita, deepinfra,  Google Vertex ")
	if err != nil {
		t.Fatalf("normalizeIgnoredProviders: %v", err)
	}
	if normalized != "DeepInfra, novita, Google Vertex" {
		t.Fatalf("normalized = %q", normalized)
	}
	if _, err := normalizeIgnoredProviders("bad<script>"); err == nil {
		t.Fatal("expected invalid provider to fail")
	}

	ignored := ignoredProviderList(normalized)
	if len(ignored) != 3 {
		t.Fatalf("list = %+v", ignored)
	}

	cases := []struct {
		endpoint Endpoint
		want     bool
	}{
		{Endpoint{Tag: "deepinfra/turbo", ProviderName: "DeepInfra"}, true},
		{Endpoint{Tag: "novita", ProviderName: "NovitaAI"}, true},
		{Endpoint{Tag: "google-vertex/us-east5", ProviderName: "Google Vertex"}, true},
		{Endpoint{Tag: "groq", ProviderName: "Groq"}, false},
	}
	for _, tc := range cases {
		if got := endpointIgnored(tc.endpoint, ignored); got != tc.want {
			t.Errorf("endpointIgnored(%+v) = %v, want %v", tc.endpoint, got, tc.want)
		}
	}
}

func TestProviderValidation(t *testing.T) {
	for _, sort := range []string{"", "price", "throughput", "latency"} {
		if !validProviderSort(sort) {
			t.Errorf("validProviderSort(%q) = false", sort)
		}
	}
	for _, sort := range []string{"fast", "PRICE", " uptime "} {
		if validProviderSort(sort) {
			t.Errorf("validProviderSort(%q) = true", sort)
		}
	}
	for _, tag := range []string{"deepinfra", "deepinfra/turbo", "google-vertex/us-east5"} {
		if !validProviderTag(tag) {
			t.Errorf("validProviderTag(%q) = false", tag)
		}
	}
	for _, tag := range []string{"", "deep infra", "a/../b", "tag?x=1"} {
		if validProviderTag(tag) {
			t.Errorf("validProviderTag(%q) = true", tag)
		}
	}
}
