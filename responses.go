package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type responseSource struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type responseAction struct {
	Type    string           `json:"type"`
	Query   string           `json:"query"`
	Queries []string         `json:"queries"`
	URL     string           `json:"url"`
	Sources []responseSource `json:"sources"`
}

type responseText struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

type responseItem struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	Role       string          `json:"role"`
	CallID     string          `json:"call_id"`
	Name       string          `json:"name"`
	Action     *responseAction `json:"action"`
	URL        string          `json:"url"`
	Title      string          `json:"title"`
	HTTPStatus int             `json:"httpStatus"`
	Content    json.RawMessage `json:"content"`
	Summary    []responseText  `json:"summary"`
	Query      string          `json:"query"`
	Arguments  string          `json:"arguments"`
	DateTime   string          `json:"datetime"`
	Timezone   string          `json:"timezone"`
	Refusal    string          `json:"refusal"`
	Error      string          `json:"error"`

	raw json.RawMessage
}

func (item *responseItem) UnmarshalJSON(data []byte) error {
	type plain responseItem
	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*item = responseItem(parsed)
	item.raw = append(item.raw[:0], data...)
	return nil
}

type responsesResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Model  string `json:"model"`
	// OutputText is a convenience field some Responses implementations return
	// instead of (or in addition to) output items. Use it as a last resort.
	OutputText        string         `json:"output_text"`
	Output            []responseItem `json:"output"`
	Usage             *usage         `json:"usage"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type responsesEvent struct {
	Type           string           `json:"type"`
	OutputIndex    int              `json:"output_index"`
	ItemID         string           `json:"item_id"`
	ContentIndex   int              `json:"content_index"`
	SummaryIndex   int              `json:"summary_index"`
	SequenceNumber int              `json:"sequence_number"`
	Delta          string           `json:"delta"`
	Text           string           `json:"text"`
	Refusal        string           `json:"refusal"`
	Part           *responseText    `json:"part"`
	Item           *responseItem    `json:"item"`
	Annotation     *annotation      `json:"annotation"`
	Response       *responsesResult `json:"response"`
	Error          *struct {
		Type    string `json:"type"`
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (item *responseItem) reasoningText() string {
	if item == nil {
		return ""
	}
	var parts []string
	for _, part := range item.Summary {
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, item.contentText())
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (item *responseItem) contentText() string {
	if item == nil {
		return ""
	}
	if text := item.partText(); text != "" {
		return text
	}
	return strings.TrimSpace(item.Refusal)
}

func (item *responseItem) partText() string {
	if item == nil || len(item.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(item.Content, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var parts []responseText
	if err := json.Unmarshal(item.Content, &parts); err == nil {
		chunks := make([]string, 0, len(parts))
		for _, part := range parts {
			switch {
			case part.Text != "":
				chunks = append(chunks, part.Text)
			case part.Refusal != "":
				chunks = append(chunks, part.Refusal)
			}
		}
		return strings.TrimSpace(strings.Join(chunks, ""))
	}
	return ""
}

func (item *responseItem) messageText() string {
	if item == nil {
		return ""
	}
	if text := item.partText(); text != "" {
		return text
	}
	return item.Refusal
}

func isResponseToolType(value string) bool {
	switch value {
	case "web_search_call", "openrouter:web_search",
		"openrouter:web_fetch", "openrouter:datetime",
		"openrouter:experimental__search_models":
		return true
	}
	return false
}

func responseToolName(item *responseItem) string {
	if item == nil {
		return ""
	}
	switch item.Type {
	case "web_search_call", "openrouter:web_search":
		return "web_search"
	case "openrouter:web_fetch":
		return "web_fetch"
	case "openrouter:datetime":
		return "datetime"
	case "openrouter:experimental__search_models":
		return "search_models"
	}
	return normalizeToolName(item.Type)
}

func responseSearchQuery(item *responseItem) string {
	if item == nil {
		return ""
	}
	if item.Action != nil {
		if query := strings.TrimSpace(item.Action.Query); query != "" {
			return query
		}
		for _, query := range item.Action.Queries {
			if query = strings.TrimSpace(query); query != "" {
				return query
			}
		}
	}
	return strings.TrimSpace(item.Query)
}

func hostLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

func citationMap(citations []Citation) map[string]Citation {
	out := make(map[string]Citation, len(citations))
	for _, citation := range citations {
		if citation.URL == "" {
			continue
		}
		if existing, ok := out[citation.URL]; ok {
			if existing.Title == "" {
				existing.Title = citation.Title
			}
			if existing.Excerpt == "" {
				existing.Excerpt = citation.Excerpt
			}
			out[citation.URL] = existing
			continue
		}
		out[citation.URL] = citation
	}
	return out
}

func mergeStepSources(list []Citation, extra []Citation) []Citation {
	index := make(map[string]int, len(list)+len(extra))
	for i, source := range list {
		if source.URL != "" {
			index[source.URL] = i
		}
	}
	for _, source := range extra {
		if source.URL == "" {
			continue
		}
		if i, ok := index[source.URL]; ok {
			// A step is streamed before its final citation metadata arrives, so
			// fill in the title and excerpt rather than keeping the bare URL.
			if list[i].Title == "" || list[i].Title == hostLabel(source.URL) {
				if source.Title != "" {
					list[i].Title = source.Title
				}
			}
			if list[i].Excerpt == "" {
				list[i].Excerpt = source.Excerpt
			}
			continue
		}
		index[source.URL] = len(list)
		list = append(list, source)
	}
	return list
}

// mergeChatStep folds a second view of the same tool step into the one already
// emitted. Streaming sends the step when the upstream item finishes; the
// transcript is rebuilt after all events, when citations are known.
func mergeChatStep(existing *ChatStep, incoming ChatStep) {
	if existing.Tool == "" {
		existing.Tool = incoming.Tool
	}
	if existing.Provider == "" {
		existing.Provider = incoming.Provider
	}
	if existing.Query == "" {
		existing.Query = incoming.Query
	}
	if existing.Arguments == "" {
		existing.Arguments = incoming.Arguments
	}
	if existing.Status == "" {
		existing.Status = incoming.Status
	}
	if existing.Reasoning == "" {
		existing.Reasoning = incoming.Reasoning
	}
	if existing.Result == "" {
		existing.Result = incoming.Result
	}
	if existing.Error == "" {
		existing.Error = incoming.Error
	}
	if existing.DurationMS == 0 {
		existing.DurationMS = incoming.DurationMS
	}
	existing.Sources = mergeStepSources(existing.Sources, incoming.Sources)
}

func responseItemSources(item *responseItem, citations map[string]Citation) []Citation {
	if item == nil {
		return nil
	}
	var sources []Citation
	if item.Action != nil {
		for _, source := range item.Action.Sources {
			if !safeCitationURL(source.URL) {
				continue
			}
			value := Citation{URL: source.URL}
			if citation, ok := citations[source.URL]; ok {
				value = citation
			}
			if value.Title == "" {
				value.Title = hostLabel(source.URL)
			}
			sources = append(sources, value)
		}
	}
	if item.Type == "web_fetch" || item.Type == "openrouter:web_fetch" {
		if safeCitationURL(item.URL) {
			value := Citation{URL: item.URL, Title: item.Title}
			if citation, ok := citations[item.URL]; ok {
				if value.Title == "" {
					value.Title = citation.Title
				}
				value.Excerpt = citation.Excerpt
			}
			if value.Title == "" {
				value.Title = hostLabel(item.URL)
			}
			sources = mergeStepSources(sources, []Citation{value})
		}
	}
	return sources
}

func buildResponseStep(item *responseItem, reasoning string, citations map[string]Citation, elapsed time.Duration) ChatStep {
	tool := responseToolName(item)
	step := ChatStep{
		ID:         item.ID,
		Tool:       tool,
		Query:      responseSearchQuery(item),
		Status:     item.Status,
		Reasoning:  strings.TrimSpace(reasoning),
		Sources:    responseItemSources(item, citations),
		Error:      strings.TrimSpace(item.Error),
		DurationMS: elapsed.Milliseconds(),
	}
	if tool == "web_search" || tool == "web_fetch" {
		step.Provider = webProviderOpenRouter
	}
	if step.Status == "" {
		step.Status = "completed"
	}
	if step.ID == "" {
		step.ID = fmt.Sprintf("step-%d", time.Now().UnixNano())
	}

	switch tool {
	case "web_search":
		if step.Query == "" && item.Action != nil && len(item.Action.Queries) > 0 {
			step.Query = strings.Join(item.Action.Queries, " ")
		}
		if step.Query != "" {
			step.Arguments = mustJSON(map[string]any{"query": step.Query})
		}
	case "web_fetch":
		if item.URL != "" {
			step.Arguments = mustJSON(map[string]any{"url": item.URL})
		}
	case "datetime":
		step.Arguments = mustJSON(map[string]any{"timezone": item.Timezone})
	case "search_models":
		if item.Arguments != "" {
			step.Arguments = item.Arguments
		} else if item.Query != "" {
			step.Arguments = mustJSON(map[string]any{"query": item.Query})
		}
	}
	if step.Arguments == "" {
		step.Arguments = strings.TrimSpace(item.Arguments)
	}
	fillResponseStepResult(&step, item)
	return step
}

func fillResponseStepResult(step *ChatStep, item *responseItem) {
	if step == nil {
		return
	}
	if item == nil && step.Tool != "web_search" {
		return
	}
	var payload any
	switch step.Tool {
	case "web_search":
		results := make([]map[string]any, 0, len(step.Sources))
		for _, source := range step.Sources {
			entry := map[string]any{
				"title": source.Title,
				"url":   source.URL,
			}
			if source.Excerpt != "" {
				entry["excerpt"] = source.Excerpt
			}
			results = append(results, entry)
		}
		payload = map[string]any{
			"query":   step.Query,
			"results": results,
		}
	case "web_fetch":
		payload = map[string]any{
			"url":     item.URL,
			"title":   item.Title,
			"status":  item.HTTPStatus,
			"content": truncate(item.contentText(), 12000),
		}
	case "datetime":
		payload = map[string]any{
			"datetime": item.DateTime,
			"timezone": item.Timezone,
		}
	case "search_models":
		payload = map[string]any{
			"query":     item.Query,
			"arguments": item.Arguments,
		}
	default:
		if text := item.contentText(); text != "" {
			payload = text
		}
	}
	if payload == nil {
		return
	}
	if value, ok := payload.(string); ok {
		step.Result = truncate(value, 12000)
		return
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	step.Result = truncate(string(encoded), 12000)
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// buildResponseTranscript reconstructs the step list from the response's output
// items. Reasoning before a tool call belongs to that tool; reasoning left at
// the end belongs to the final answer.
func buildResponseTranscript(output []responseItem, reasoning map[int]string, citations []Citation) ([]ChatStep, string) {
	byURL := citationMap(citations)
	var steps []ChatStep
	var pending []string

	for index, item := range output {
		switch {
		case item.Type == "reasoning":
			text := strings.TrimSpace(reasoning[index])
			if text == "" {
				text = item.reasoningText()
			}
			if text != "" {
				pending = append(pending, text)
			}
		case item.Type == "function_call":
			// Tavily calls are executed by Clachan, and their reasoning is
			// attached by the chat loop. Do not leak it into the final answer.
			pending = nil
		case isResponseToolType(item.Type):
			step := buildResponseStep(&item, strings.Join(pending, "\n\n"), byURL, 0)
			steps = append(steps, step)
			pending = nil
		}
	}

	finalReasoning := strings.TrimSpace(strings.Join(pending, "\n\n"))
	attachUnmatchedCitations(steps, citations)
	return steps, finalReasoning
}

func attachUnmatchedCitations(steps []ChatStep, citations []Citation) {
	if len(steps) == 0 || len(citations) == 0 {
		return
	}
	used := make(map[string]bool)
	for _, step := range steps {
		for _, source := range step.Sources {
			used[source.URL] = true
		}
	}
	var unmatched []Citation
	for _, citation := range citations {
		if citation.URL != "" && !used[citation.URL] {
			unmatched = append(unmatched, citation)
		}
	}
	if len(unmatched) == 0 {
		return
	}

	// Provider-native search often reports citations only on the final message.
	// The last search step is the best available association.
	lastSearch := -1
	for i := range steps {
		if steps[i].Tool == "web_search" {
			lastSearch = i
		}
	}
	if lastSearch < 0 {
		return
	}
	steps[lastSearch].Sources = mergeStepSources(steps[lastSearch].Sources, unmatched)
	for i := range steps {
		if steps[i].Tool == "web_search" {
			fillResponseStepResult(&steps[i], nil)
		}
	}
}

func lastResponseMessageText(output []responseItem) string {
	for i := len(output) - 1; i >= 0; i-- {
		item := &output[i]
		if item.Type == "reasoning" || item.Type == "function_call" || isResponseToolType(item.Type) {
			continue
		}
		// Do not require type "message": some upstreams label the final item
		// differently, and the text is worth keeping either way.
		if text := item.messageText(); text != "" {
			return text
		}
	}
	return ""
}

// responseFunctionCalls lists the model-requested function calls in output
// order. Clachan executes these itself so the upstream never sees the Tavily
// key.
func responseFunctionCalls(output []responseItem) []functionCall {
	calls := make([]functionCall, 0, 2)
	for _, item := range output {
		if item.Type != "function_call" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		calls = append(calls, functionCall{
			ID:        strings.TrimSpace(item.ID),
			CallID:    strings.TrimSpace(item.CallID),
			Name:      name,
			Arguments: strings.TrimSpace(item.Arguments),
		})
	}
	return calls
}

// responseFunctionEcho builds the assistant items a stateless follow-up request
// must carry so each function_call_output has a matching call. Reasoning items
// are replayed verbatim so providers that sign their reasoning keep a valid
// chain across rounds.
func responseFunctionEcho(output []responseItem) []responseInputMessage {
	echo := make([]responseInputMessage, 0, len(output))
	seen := make(map[string]bool, len(output))
	for _, item := range output {
		switch item.Type {
		case "reasoning":
			if len(item.raw) == 0 {
				continue
			}
			key := "reasoning:" + strings.TrimSpace(item.ID)
			if key == "reasoning:" {
				key = "reasoning:" + string(item.raw)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			echo = append(echo, responseInputMessage{Type: "reasoning", raw: item.raw})
		case "function_call":
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			key := strings.TrimSpace(item.CallID)
			if key == "" {
				key = strings.TrimSpace(item.ID)
			}
			if key != "" && seen[key] {
				continue
			}
			if key != "" {
				seen[key] = true
			}
			status := strings.TrimSpace(item.Status)
			if status == "" {
				status = "completed"
			}
			echo = append(echo, responseInputMessage{
				Type:      "function_call",
				ID:        strings.TrimSpace(item.ID),
				CallID:    strings.TrimSpace(item.CallID),
				Name:      name,
				Arguments: strings.TrimSpace(item.Arguments),
				Status:    status,
			})
		case "message":
			if len(item.raw) > 0 && strings.TrimSpace(item.messageText()) != "" {
				echo = append(echo, responseInputMessage{Type: "message", raw: item.raw})
				continue
			}
			text := strings.TrimSpace(item.messageText())
			if text == "" {
				continue
			}
			status := strings.TrimSpace(item.Status)
			if status == "" {
				status = "completed"
			}
			echo = append(echo, responseInputMessage{
				Type:    "message",
				Role:    "assistant",
				ID:      strings.TrimSpace(item.ID),
				Content: text,
				Status:  status,
			})
		}
	}
	return echo
}

// stringField pulls a single string field out of an event payload. It is used
// as a fallback when decoding the full event shape fails because one unrelated
// field had an unexpected JSON type; a stray field must never discard the text.
func stringField(data, key string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
