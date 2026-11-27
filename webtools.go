package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	webProviderOpenRouter = "openrouter"
	webProviderTavily     = "tavily"
	tavilyDefaultBase     = "https://api.tavily.com"

	tavilySearchFunction  = "tavily_search"
	tavilyExtractFunction = "tavily_extract"
)

func normalizeWebProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case webProviderTavily:
		return webProviderTavily
	default:
		return webProviderOpenRouter
	}
}

func validWebProvider(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", webProviderOpenRouter, webProviderTavily:
		return true
	}
	return false
}

func webProviderLabel(value string) string {
	if normalizeWebProvider(value) == webProviderTavily {
		return "Tavily"
	}
	return "OpenRouter"
}

/* ------------------------------------------------------------------ function tools */

func tavilySearchTool() serverTool {
	return serverTool{
		Type:        "function",
		Name:        tavilySearchFunction,
		Description: "Search the live web with Tavily. Use it for current events, facts that may have changed, or anything that needs sources. Prefer one focused query per call; call it again if the first results are not enough.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query. Be specific and include names, dates, or sites when useful.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "How many results to return (1-10). Defaults to 5.",
					"minimum":     1,
					"maximum":     10,
				},
				"search_depth": map[string]any{
					"type":        "string",
					"description": "basic is fast; advanced retrieves more content and costs more.",
					"enum":        []string{"basic", "advanced"},
				},
			},
			"required": []string{"query"},
		},
	}
}

func tavilyExtractTool() serverTool {
	return serverTool{
		Type:        "function",
		Name:        tavilyExtractFunction,
		Description: "Fetch and extract the readable content of up to five web pages with Tavily. Use it after a search when a result needs to be read in full or when the user supplies a URL.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"urls": map[string]any{
					"type":        "array",
					"description": "Absolute http(s) URLs to extract (1-5).",
					"items":       map[string]any{"type": "string"},
					"minItems":    1,
					"maxItems":    5,
				},
				"extract_depth": map[string]any{
					"type":        "string",
					"description": "basic is fast; advanced handles harder pages and costs more.",
					"enum":        []string{"basic", "advanced"},
				},
			},
			"required": []string{"urls"},
		},
	}
}

// functionCall is one function_call output item from a Responses stream.
type functionCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
}

func (c functionCall) outputCallID() string {
	if strings.TrimSpace(c.CallID) != "" {
		return strings.TrimSpace(c.CallID)
	}
	return strings.TrimSpace(c.ID)
}

type tavilySearchArgs struct {
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
}

type tavilyExtractArgs struct {
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth"`
}

/* ------------------------------------------------------------------------ tavily */

type tavilySearchRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeAnswer  bool     `json:"include_answer,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
}

type tavilySearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type tavilySearchResponse struct {
	Query        string               `json:"query"`
	Answer       string               `json:"answer"`
	Results      []tavilySearchResult `json:"results"`
	ResponseTime float64              `json:"response_time"`
}

type tavilyExtractRequest struct {
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth,omitempty"`
}

type tavilyExtractResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type tavilyExtractResponse struct {
	Results []tavilyExtractResult `json:"results"`
	Failed  []struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	} `json:"failed_results"`
}

func (s *Server) tavilyBaseURL() string {
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.TavilyBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(s.cfg.TavilyBaseURL), "/")
	}
	return tavilyDefaultBase
}

func (s *Server) tavilyPost(ctx context.Context, apiKey, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tavilyBaseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("X-Title", "Clachan")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Tavily (%v)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := tavilyErrorMessage(resp)
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return errors.New(detail)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("could not read the Tavily response (%v)", err)
	}
	return nil
}

func tavilyErrorMessage(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var payload struct {
		Detail struct {
			Error string `json:"error"`
		} `json:"detail"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &payload) == nil {
		switch {
		case payload.Detail.Error != "":
			return truncate(payload.Detail.Error, 300)
		case payload.Error != "":
			return truncate(payload.Error, 300)
		case payload.Message != "":
			return truncate(payload.Message, 300)
		}
	}
	return truncate(strings.TrimSpace(string(b)), 300)
}

func (s *Server) tavilySearch(ctx context.Context, apiKey, query string, maxResults int, depth string) (tavilySearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return tavilySearchResponse{}, errors.New("the search query is empty")
	}
	if len(query) > 2000 {
		query = truncate(query, 2000)
	}
	if maxResults < 1 || maxResults > 10 {
		maxResults = 5
	}
	depth = strings.ToLower(strings.TrimSpace(depth))
	if depth != "advanced" {
		depth = "basic"
	}
	var out tavilySearchResponse
	err := s.tavilyPost(ctx, apiKey, "/search", tavilySearchRequest{
		Query:       query,
		SearchDepth: depth,
		MaxResults:  maxResults,
	}, &out)
	return out, err
}

func (s *Server) tavilyExtract(ctx context.Context, apiKey string, urls []string, depth string) (tavilyExtractResponse, error) {
	if len(urls) == 0 {
		return tavilyExtractResponse{}, errors.New("no URLs were supplied")
	}
	if len(urls) > 5 {
		urls = urls[:5]
	}
	depth = strings.ToLower(strings.TrimSpace(depth))
	if depth != "advanced" {
		depth = "basic"
	}
	var out tavilyExtractResponse
	err := s.tavilyPost(ctx, apiKey, "/extract", tavilyExtractRequest{
		URLs:         urls,
		ExtractDepth: depth,
	}, &out)
	return out, err
}

/* ---------------------------------------------------------------- call execution */

// runTavilyCall executes one model-requested Tavily function. A failure is
// returned to the model as text so it can answer or try again, and the caller
// switches the remaining rounds to OpenRouter's web tools.
func (s *Server) runTavilyCall(ctx context.Context, apiKey string, call functionCall) (ChatStep, string, []Citation, error) {
	started := time.Now()
	step := ChatStep{
		ID:        "tavily-" + newID(),
		Provider:  webProviderTavily,
		Arguments: call.Arguments,
		Status:    "completed",
	}
	if apiKey == "" {
		step.Tool = "web_search"
		step.Status = "failed"
		step.Error = "Tavily is selected but no Tavily API key is saved for this profile."
		return step, step.Error, nil, errors.New(step.Error)
	}

	switch strings.TrimSpace(call.Name) {
	case tavilySearchFunction:
		var args tavilySearchArgs
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			step.Tool = "web_search"
			step.Status = "failed"
			step.Error = "The model sent invalid search arguments."
			return step, step.Error, nil, errors.New(step.Error)
		}
		step.Tool = "web_search"
		step.Query = strings.TrimSpace(args.Query)
		result, err := s.tavilySearch(ctx, apiKey, args.Query, args.MaxResults, args.SearchDepth)
		step.DurationMS = time.Since(started).Milliseconds()
		if err != nil {
			step.Status = "failed"
			step.Error = truncate(err.Error(), 300)
			return step, "Tavily search failed: " + step.Error, nil, err
		}

		type entry struct {
			Title   string  `json:"title,omitempty"`
			URL     string  `json:"url"`
			Content string  `json:"content,omitempty"`
			Score   float64 `json:"score,omitempty"`
		}
		output := struct {
			Query   string  `json:"query"`
			Answer  string  `json:"answer,omitempty"`
			Results []entry `json:"results"`
		}{Query: result.Query, Answer: truncate(strings.TrimSpace(result.Answer), 4000)}
		if output.Query == "" {
			output.Query = step.Query
		}
		citations := make([]Citation, 0, len(result.Results))
		for _, item := range result.Results {
			if !safeCitationURL(item.URL) {
				continue
			}
			citation := Citation{
				URL:     item.URL,
				Title:   truncate(strings.TrimSpace(item.Title), 300),
				Excerpt: truncate(strings.TrimSpace(item.Content), 1200),
			}
			if citation.Title == "" {
				citation.Title = hostLabel(item.URL)
			}
			citations = append(citations, citation)
			output.Results = append(output.Results, entry{
				Title:   citation.Title,
				URL:     item.URL,
				Content: truncate(strings.TrimSpace(item.Content), 2500),
				Score:   item.Score,
			})
		}
		step.Sources = citations
		encoded, _ := json.MarshalIndent(output, "", "  ")
		step.Result = truncate(string(encoded), 12000)
		return step, truncate(string(encoded), 48000), citations, nil

	case tavilyExtractFunction:
		var args tavilyExtractArgs
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			step.Tool = "web_fetch"
			step.Status = "failed"
			step.Error = "The model sent invalid fetch arguments."
			return step, step.Error, nil, errors.New(step.Error)
		}
		step.Tool = "web_fetch"
		urls := make([]string, 0, len(args.URLs))
		for _, raw := range args.URLs {
			raw = strings.TrimSpace(raw)
			if !safeCitationURL(raw) {
				continue
			}
			urls = append(urls, raw)
			if len(urls) >= 5 {
				break
			}
		}
		step.Query = strings.Join(urls, ", ")
		if len(urls) == 0 {
			step.Status = "failed"
			step.Error = "No valid http(s) URLs were supplied."
			return step, step.Error, nil, errors.New(step.Error)
		}
		result, err := s.tavilyExtract(ctx, apiKey, urls, args.ExtractDepth)
		step.DurationMS = time.Since(started).Milliseconds()
		if err != nil {
			step.Status = "failed"
			step.Error = truncate(err.Error(), 300)
			return step, "Tavily fetch failed: " + step.Error, nil, err
		}

		type entry struct {
			URL     string `json:"url"`
			Title   string `json:"title,omitempty"`
			Content string `json:"content,omitempty"`
		}
		output := struct {
			Results []entry `json:"results"`
			Failed  []struct {
				URL   string `json:"url"`
				Error string `json:"error,omitempty"`
			} `json:"failed,omitempty"`
		}{}
		var citations []Citation
		for _, item := range result.Results {
			if !safeCitationURL(item.URL) {
				continue
			}
			title := hostLabel(item.URL)
			citations = append(citations, Citation{
				URL:     item.URL,
				Title:   title,
				Excerpt: truncate(strings.TrimSpace(item.RawContent), 1200),
			})
			output.Results = append(output.Results, entry{
				URL:     item.URL,
				Title:   title,
				Content: truncate(strings.TrimSpace(item.RawContent), 12000),
			})
		}
		for _, failed := range result.Failed {
			output.Failed = append(output.Failed, struct {
				URL   string `json:"url"`
				Error string `json:"error,omitempty"`
			}{URL: failed.URL, Error: truncate(failed.Error, 300)})
		}
		step.Sources = citations
		encoded, _ := json.MarshalIndent(output, "", "  ")
		step.Result = truncate(string(encoded), 12000)
		return step, truncate(string(encoded), 48000), citations, nil

	default:
		step.Tool = normalizeToolName(call.Name)
		step.Status = "failed"
		step.Error = "Unknown Tavily tool: " + call.Name
		return step, step.Error, nil, errors.New(step.Error)
	}
}
