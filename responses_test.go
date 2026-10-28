package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildResponseTranscriptGroupsReasoning(t *testing.T) {
	output := []responseItem{
		{
			ID: "rs1", Type: "reasoning", Status: "completed",
			Summary: []responseText{{Type: "summary_text", Text: "Look it up."}},
		},
		{
			ID: "ws1", Type: "openrouter:web_search", Status: "completed",
			Action: &responseAction{
				Type:    "search",
				Query:   "golang wal",
				Sources: []responseSource{{Type: "url", URL: "https://a.example/page"}},
			},
		},
		{
			ID: "rs2", Type: "reasoning", Status: "completed",
			Summary: []responseText{{Type: "summary_text", Text: "Now answer."}},
		},
		{ID: "msg1", Type: "message", Role: "assistant", Content: json.RawMessage(`"WAL is a journal mode."`)},
	}

	steps, finalReasoning := buildResponseTranscript(output, nil,
		[]Citation{{URL: "https://b.example", Title: "Second source"}})

	if len(steps) != 1 {
		t.Fatalf("steps = %+v", steps)
	}
	step := steps[0]
	if step.Tool != "web_search" || step.Query != "golang wal" || step.Reasoning != "Look it up." {
		t.Fatalf("step = %+v", step)
	}
	if len(step.Sources) != 2 {
		t.Fatalf("sources = %+v", step.Sources)
	}
	if step.Sources[0].Title != "a.example" || step.Sources[1].Title != "Second source" {
		t.Fatalf("source titles = %+v", step.Sources)
	}
	if !strings.Contains(step.Result, "https://a.example/page") || !strings.Contains(step.Result, "Second source") {
		t.Fatalf("result = %s", step.Result)
	}
	if finalReasoning != "Now answer." {
		t.Fatalf("final reasoning = %q", finalReasoning)
	}
}

func TestBuildResponseStepWebFetch(t *testing.T) {
	item := responseItem{
		ID: "wf1", Type: "openrouter:web_fetch", Status: "completed",
		URL: "https://example.com/doc", Title: "Example doc", HTTPStatus: 200,
		Content: json.RawMessage(`"Fetched body"`),
	}
	step := buildResponseStep(&item, "", nil, 0)

	if step.Tool != "web_fetch" || step.Arguments != `{"url":"https://example.com/doc"}` {
		t.Fatalf("step = %+v", step)
	}
	if !strings.Contains(step.Result, "Fetched body") || !strings.Contains(step.Result, "Example doc") {
		t.Fatalf("result = %s", step.Result)
	}
	if len(step.Sources) != 1 || step.Sources[0].URL != "https://example.com/doc" {
		t.Fatalf("sources = %+v", step.Sources)
	}
}

func TestBuildResponseTranscriptNoTools(t *testing.T) {
	output := []responseItem{
		{
			ID: "rs1", Type: "reasoning",
			Content: json.RawMessage(`[{"type":"reasoning_text","text":"Think first."}]`),
		},
		{ID: "msg1", Type: "message", Content: json.RawMessage(`"Answer"`)},
	}
	steps, reasoning := buildResponseTranscript(output, nil, nil)
	if len(steps) != 0 {
		t.Fatalf("steps = %+v", steps)
	}
	if reasoning != "Think first." {
		t.Fatalf("reasoning = %q", reasoning)
	}
	if got := lastResponseMessageText(output); got != "Answer" {
		t.Fatalf("last message = %q", got)
	}
}
