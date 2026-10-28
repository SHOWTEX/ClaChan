package main

import (
	"net/url"
	"strings"
	"time"
)

const (
	defaultToolCalls  = 6
	maxToolCallsLimit = 30
)

// Citation is one source reported by OpenRouter's web tools. Excerpt is the
// short extract some search engines include with a citation.
type Citation struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// ChatStep is one completed tool step in an assistant reply. Reasoning is the
// thinking that immediately preceded the tool call.
type ChatStep struct {
	ID         string     `json:"id,omitempty"`
	Tool       string     `json:"tool"`
	Query      string     `json:"query,omitempty"`
	Arguments  string     `json:"arguments,omitempty"`
	Status     string     `json:"status,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	Result     string     `json:"result,omitempty"`
	Sources    []Citation `json:"sources,omitempty"`
	Error      string     `json:"error,omitempty"`
	DurationMS int64      `json:"durationMs,omitempty"`
}

// ToolActivity is one tool call surfaced during a stream. Server tools often
// appear only as usage counts, so the UI treats this list as optional detail.
type ToolActivity struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status,omitempty"`
	URL    string `json:"url,omitempty"`
}

// ToolRun is the persisted summary shown in the collapsible tool log.
type ToolRun struct {
	Enabled    bool           `json:"enabled"`
	MaxCalls   int            `json:"maxCalls"`
	Calls      int            `json:"calls,omitempty"`
	Searches   int            `json:"searches,omitempty"`
	Activities []ToolActivity `json:"activities,omitempty"`
}

type serverTool struct {
	Type       string `json:"type"`
	Parameters any    `json:"parameters,omitempty"`
}

// buildServerTools always includes free, low-risk tools. Web search and fetch
// follow the user's per-chat switch.
func buildServerTools(webEnabled bool, timezone string) []serverTool {
	if !validTimezone(timezone) {
		timezone = "UTC"
	}
	tools := []serverTool{
		{
			Type:       "openrouter:datetime",
			Parameters: map[string]any{"timezone": timezone},
		},
		{
			Type:       "openrouter:experimental__search_models",
			Parameters: map[string]any{"max_results": 5},
		},
	}
	if webEnabled {
		tools = append(tools,
			serverTool{
				Type: "openrouter:web_search",
				Parameters: map[string]any{
					"max_results":       5,
					"max_total_results": 20,
				},
			},
			serverTool{Type: "openrouter:web_fetch"},
		)
	}
	return tools
}

func validTimezone(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '/', r == '_', r == '-', r == '+':
		default:
			return false
		}
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

func clampToolCalls(value int) int {
	if value < 1 {
		return defaultToolCalls
	}
	if value > maxToolCallsLimit {
		return maxToolCallsLimit
	}
	return value
}

type urlCitationPayload struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type annotation struct {
	Type        string              `json:"type"`
	URL         string              `json:"url"`
	Title       string              `json:"title"`
	Content     string              `json:"content"`
	URLCitation *urlCitationPayload `json:"url_citation"`
}

func (a annotation) citation() (Citation, bool) {
	if !strings.EqualFold(a.Type, "url_citation") {
		return Citation{}, false
	}
	rawURL := strings.TrimSpace(a.URL)
	title := strings.TrimSpace(a.Title)
	excerpt := strings.TrimSpace(a.Content)
	if a.URLCitation != nil {
		if rawURL == "" {
			rawURL = strings.TrimSpace(a.URLCitation.URL)
		}
		if title == "" {
			title = strings.TrimSpace(a.URLCitation.Title)
		}
		if excerpt == "" {
			excerpt = strings.TrimSpace(a.URLCitation.Content)
		}
	}
	if !safeCitationURL(rawURL) {
		return Citation{}, false
	}
	if len(title) > 300 {
		title = truncate(title, 300)
	}
	return Citation{URL: rawURL, Title: title, Excerpt: truncate(excerpt, 1200)}, true
}

func safeCitationURL(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func buildToolRun(enabled bool, maxCalls int, usage *usage, activities []ToolActivity) *ToolRun {
	run := &ToolRun{
		Enabled:    enabled,
		MaxCalls:   maxCalls,
		Activities: activities,
		Calls:      len(activities),
	}
	if use := usage.toolUse(); use != nil {
		if use.ToolCallsExecuted > 0 {
			run.Calls = use.ToolCallsExecuted
		} else if use.ToolCallsRequested > 0 {
			run.Calls = use.ToolCallsRequested
		}
		run.Searches = use.WebSearchRequests
	}
	if run.Calls == 0 && run.Searches == 0 && len(run.Activities) == 0 {
		return nil
	}
	return run
}

func normalizeToolName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "openrouter:")
	name = strings.TrimPrefix(name, "experimental__")
	return name
}

func activitiesFromSteps(steps []ChatStep) []ToolActivity {
	activities := make([]ToolActivity, 0, len(steps))
	for _, step := range steps {
		if step.Tool == "" {
			continue
		}
		activities = append(activities, ToolActivity{
			Name:   step.Tool,
			Detail: truncate(step.Arguments, 180),
			Status: step.Status,
		})
	}
	return activities
}
