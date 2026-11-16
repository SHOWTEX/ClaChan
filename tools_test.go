package main

import "testing"

func toolParams(t *testing.T, tools []serverTool, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if tool.Type != name {
			continue
		}
		params, ok := tool.Parameters.(map[string]any)
		if !ok {
			t.Fatalf("%s parameters = %T, want map[string]any", name, tool.Parameters)
		}
		return params
	}
	t.Fatalf("%s missing from %v", name, tools)
	return nil
}

func TestBuildServerToolsCapsSearchesAndFetches(t *testing.T) {
	tools := buildServerTools(true, "Europe/London", 3)

	search := toolParams(t, tools, "openrouter:web_search")
	if got := search["max_uses"]; got != 3 {
		t.Fatalf("web_search max_uses = %v, want 3", got)
	}
	if got := search["max_results"]; got != 5 {
		t.Fatalf("web_search max_results = %v, want 5", got)
	}
	if got := search["max_total_results"]; got != 20 {
		t.Fatalf("web_search max_total_results = %v, want 20", got)
	}

	fetch := toolParams(t, tools, "openrouter:web_fetch")
	if got := fetch["max_uses"]; got != 3 {
		t.Fatalf("web_fetch max_uses = %v, want 3", got)
	}

	toolParams(t, tools, "openrouter:datetime")
	toolParams(t, tools, "openrouter:experimental__search_models")
}

func TestBuildServerToolsWithoutWeb(t *testing.T) {
	tools := buildServerTools(false, "", 6)

	for _, tool := range tools {
		switch tool.Type {
		case "openrouter:web_search", "openrouter:web_fetch":
			t.Fatalf("web tool %q present while web is off", tool.Type)
		}
	}

	if got := toolParams(t, tools, "openrouter:datetime")["timezone"]; got != "UTC" {
		t.Fatalf("datetime timezone = %v, want UTC", got)
	}
}

func TestBuildServerToolsClampsMaxCalls(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, defaultToolCalls},
		{-4, defaultToolCalls},
		{1, 1},
		{maxToolCallsLimit, maxToolCallsLimit},
		{maxToolCallsLimit + 1, maxToolCallsLimit},
	}
	for _, tc := range cases {
		tools := buildServerTools(true, "UTC", tc.in)
		if got := toolParams(t, tools, "openrouter:web_search")["max_uses"]; got != tc.want {
			t.Fatalf("max_uses for %d = %v, want %d", tc.in, got, tc.want)
		}
	}
}
