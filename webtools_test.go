package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func tavilyTestServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return &Server{
		cfg:    &Config{TavilyBaseURL: upstream.URL},
		client: upstream.Client(),
	}
}

func TestTavilySearchAndExtract(t *testing.T) {
	var searchAuth, extractAuth string
	var searchQuery string
	s := tavilyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/search":
			searchAuth = r.Header.Get("Authorization")
			var req tavilySearchRequest
			_ = json.Unmarshal(body, &req)
			searchQuery = req.Query
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"query":"golang release","answer":"Go 1.27 is current.","results":[
				{"title":"Go releases","url":"https://go.dev/doc/devel/release","content":"Release notes.","score":0.91},
				{"title":"Bad URL","url":"javascript:alert(1)","content":"nope","score":0.1}]}`)
		case "/extract":
			extractAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"results":[{"url":"https://go.dev/doc/devel/release","raw_content":"Full release notes."}],
				"failed_results":[{"url":"https://bad.example","error":"timeout"}]}`)
		default:
			http.NotFound(w, r)
		}
	})

	result, err := s.tavilySearch(context.Background(), "tvly-key", "golang release", 5, "advanced")
	if err != nil {
		t.Fatalf("tavilySearch: %v", err)
	}
	if searchAuth != "Bearer tvly-key" || searchQuery != "golang release" {
		t.Fatalf("search auth/query = %q / %q", searchAuth, searchQuery)
	}
	if len(result.Results) != 2 || result.Results[0].Title != "Go releases" {
		t.Fatalf("search results = %+v", result.Results)
	}

	step, output, citations, err := s.runTavilyCall(context.Background(), "tvly-key", functionCall{
		CallID:    "call_1",
		Name:      tavilySearchFunction,
		Arguments: `{"query":"golang release","max_results":5}`,
	})
	if err != nil {
		t.Fatalf("runTavilyCall search: %v", err)
	}
	if step.Tool != "web_search" || step.Provider != webProviderTavily || step.Query != "golang release" {
		t.Fatalf("search step = %+v", step)
	}
	if len(citations) != 1 || citations[0].URL != "https://go.dev/doc/devel/release" {
		t.Fatalf("search citations = %+v", citations)
	}
	if !strings.Contains(output, "Go 1.27 is current.") {
		t.Fatalf("search output = %s", output)
	}

	step, output, citations, err = s.runTavilyCall(context.Background(), "tvly-key", functionCall{
		CallID:    "call_2",
		Name:      tavilyExtractFunction,
		Arguments: `{"urls":["https://go.dev/doc/devel/release","javascript:alert(1)","https://bad.example"]}`,
	})
	if err != nil {
		t.Fatalf("runTavilyCall extract: %v", err)
	}
	if extractAuth != "Bearer tvly-key" {
		t.Fatalf("extract auth = %q", extractAuth)
	}
	if step.Tool != "web_fetch" || len(citations) != 1 || citations[0].Excerpt != "Full release notes." {
		t.Fatalf("extract step = %+v citations = %+v", step, citations)
	}
	if !strings.Contains(output, "timeout") {
		t.Fatalf("extract output missing failed result: %s", output)
	}
}

func TestTavilyFailureSurfacesMessage(t *testing.T) {
	s := tavilyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"detail":{"error":"invalid api key"}}`)
	})

	step, output, citations, err := s.runTavilyCall(context.Background(), "bad-key", functionCall{
		CallID:    "call_1",
		Name:      tavilySearchFunction,
		Arguments: `{"query":"anything"}`,
	})
	if err == nil {
		t.Fatal("expected a Tavily error")
	}
	if step.Status != "failed" || !strings.Contains(step.Error, "invalid api key") {
		t.Fatalf("step = %+v", step)
	}
	if len(citations) != 0 || !strings.Contains(output, "Tavily search failed") {
		t.Fatalf("output = %q citations = %+v", output, citations)
	}
}

func TestTavilyMissingKeyFailsWithoutRequest(t *testing.T) {
	s := tavilyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("Tavily was called without a key")
	})
	step, _, _, err := s.runTavilyCall(context.Background(), "", functionCall{
		Name:      tavilySearchFunction,
		Arguments: `{"query":"anything"}`,
	})
	if err == nil || step.Status != "failed" {
		t.Fatalf("step = %+v err = %v", step, err)
	}
}
