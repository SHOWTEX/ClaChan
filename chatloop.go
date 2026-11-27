package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// chatLoopOptions is everything the multi-round Responses loop needs. The first
// upstream response has already been opened by handleChat; later rounds are
// opened here so Clachan can execute Tavily function calls between them.
type chatLoopOptions struct {
	resp         *http.Response
	sse          *sseWriter
	provider     *Provider
	profile      *Profile
	conv         *Conversation
	model        string
	systemPrompt string
	input        []responseInputMessage
	tools        []serverTool
	reasoning    *reasoningRequest
	toolsEnabled bool
	toolCalls    int
	webProvider  string
	tavilyKey    string
	timezone     string
	watchdog     *time.Timer
	ctx          context.Context
	requestCtx   context.Context
}

type chatLoopResult struct {
	text         string
	reasoning    string
	steps        []ChatStep
	citations    []Citation
	usage        *usage
	finishReason string
	streamErr    string
	clientGone   bool
	eventCounts  map[string]int
	decodeErrors int
}

type chatRoundOutcome struct {
	output        []responseItem
	usage         *usage
	text          string
	reasoning     string
	toolReasoning string
	fallbackSteps []ChatStep
	calls         []functionCall
	terminal      bool
	err           string
}

// mergeCompletedOutput folds the terminal output list into items already seen
// in the stream. Some upstreams omit reasoning items from the terminal list, so
// its array indexes no longer match the streamed output_index values. Matching
// by item ID first keeps a function_call from being counted twice.
func mergeCompletedOutput(dst map[int]responseItem, items []responseItem) {
	byID := make(map[string]int, len(dst))
	next := 0
	for index, item := range dst {
		if item.ID != "" {
			byID[item.ID] = index
		}
		if index >= next {
			next = index + 1
		}
	}
	for index, item := range items {
		if item.ID != "" {
			if existing, ok := byID[item.ID]; ok {
				dst[existing] = item
				continue
			}
		}
		if current, ok := dst[index]; !ok || current.ID == "" || current.ID == item.ID {
			dst[index] = item
			if item.ID != "" {
				byID[item.ID] = index
			}
			continue
		}
		for {
			if _, occupied := dst[next]; !occupied {
				break
			}
			next++
		}
		dst[next] = item
		if item.ID != "" {
			byID[item.ID] = next
		}
		next++
	}
}

func dedupeFunctionCalls(calls []functionCall) []functionCall {
	seen := make(map[string]bool, len(calls))
	out := calls[:0]
	for _, call := range calls {
		key := call.outputCallID()
		if key == "" {
			key = call.Name + "\x00" + call.Arguments
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, call)
	}
	return out
}

// runChatLoop streams one or more upstream responses. OpenRouter server tools
// need one response; Tavily function tools need a stateless follow-up request
// carrying the function_call_output items. Text, reasoning, steps, citations,
// and tool activities are emitted to the browser as they arrive.
func (s *Server) runChatLoop(opts chatLoopOptions) chatLoopResult {
	result := chatLoopResult{eventCounts: make(map[string]int)}
	var content strings.Builder
	var allSteps []ChatStep
	var citations []Citation
	citationIndex := make(map[string]int)
	stepIDs := make(map[string]int)

	addCitation := func(value annotation) (Citation, bool) {
		citation, ok := value.citation()
		if !ok {
			return Citation{}, false
		}
		if index, seen := citationIndex[citation.URL]; seen {
			existing := &citations[index]
			changed := false
			if existing.Title == "" && citation.Title != "" {
				existing.Title = citation.Title
				changed = true
			}
			if existing.Excerpt == "" && citation.Excerpt != "" {
				existing.Excerpt = citation.Excerpt
				changed = true
			}
			return *existing, changed
		}
		citationIndex[citation.URL] = len(citations)
		citations = append(citations, citation)
		return citation, true
	}

	send := func(name string, payload any) bool {
		if err := opts.sse.event(name, payload); err != nil {
			result.clientGone = true
			return false
		}
		return true
	}

	addCitations := func(list []Citation) {
		for _, citation := range list {
			if citation.URL == "" {
				continue
			}
			if index, seen := citationIndex[citation.URL]; seen {
				existing := &citations[index]
				if existing.Title == "" {
					existing.Title = citation.Title
				}
				if existing.Excerpt == "" {
					existing.Excerpt = citation.Excerpt
				}
				continue
			}
			citationIndex[citation.URL] = len(citations)
			citations = append(citations, citation)
			if !send("citation", citationEvent{Citations: []Citation{citation}}) {
				return
			}
		}
	}

	appendStep := func(step ChatStep) {
		if step.ID != "" {
			if index, seen := stepIDs[step.ID]; seen {
				mergeChatStep(&allSteps[index], step)
				return
			}
			stepIDs[step.ID] = len(allSteps)
		}
		allSteps = append(allSteps, step)
		_ = send("step", stepEvent{Step: step})
	}

	outputCallID := func(call functionCall) string {
		callID := call.outputCallID()
		if callID == "" {
			callID = "call_" + newID()
		}
		return callID
	}

	consumeRound := func(resp *http.Response, round int) chatRoundOutcome {
		var outcome chatRoundOutcome
		defer resp.Body.Close()

		outputByIndex := make(map[int]responseItem)
		itemStarted := make(map[int]time.Time)
		reasoningByIndex := make(map[int]string)
		reasoningStreamed := make(map[int]bool)
		functionArgs := make(map[int]string)
		var pendingReasoning []string
		var doneText string
		var roundCompleted *responsesResult
		roundStart := content.Len()

		reader := bufio.NewReaderSize(resp.Body, 32<<10)
	read:
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				opts.watchdog.Reset(idleStreamTimeout)

				data, done := parseSSELine(line)
				if done {
					outcome.terminal = true
					break
				}
				if data == "" {
					continue
				}

				var envelope struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal([]byte(data), &envelope); err != nil || envelope.Type == "" {
					result.decodeErrors++
					continue
				}
				result.eventCounts[envelope.Type]++

				var event responsesEvent
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					// One unexpected field type must not throw away an entire text
					// delta, so keep the fields that matter and carry on.
					result.decodeErrors++
					event = responsesEvent{
						Type:    envelope.Type,
						Delta:   stringField(data, "delta"),
						Text:    stringField(data, "text"),
						Refusal: stringField(data, "refusal"),
						ItemID:  stringField(data, "item_id"),
					}
				}

				switch event.Type {
				case "response.output_item.added":
					if event.Item != nil {
						outputByIndex[event.OutputIndex] = *event.Item
						itemStarted[event.OutputIndex] = time.Now()
					}

				case "response.output_item.done":
					if event.Item == nil {
						continue
					}
					item := *event.Item
					outputByIndex[event.OutputIndex] = item
					start := itemStarted[event.OutputIndex]
					if start.IsZero() {
						start = time.Now()
					}

					if item.Type == "reasoning" {
						if !reasoningStreamed[event.OutputIndex] {
							if text := item.reasoningText(); text != "" {
								reasoningByIndex[event.OutputIndex] = text
								if !send("reasoning", reasoningEvent{
									Text:        text,
									ItemID:      item.ID,
									OutputIndex: event.OutputIndex,
								}) {
									break read
								}
							}
						}
						if text := strings.TrimSpace(reasoningByIndex[event.OutputIndex]); text != "" {
							pendingReasoning = append(pendingReasoning, text)
						}
						continue
					}

					if !isResponseToolType(item.Type) {
						continue
					}
					step := buildResponseStep(&item, strings.Join(pendingReasoning, "\n\n"),
						citationMap(citations), time.Since(start))
					pendingReasoning = nil
					appendStep(step)
					if result.clientGone {
						break read
					}

				case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
					if event.Delta == "" {
						continue
					}
					reasoningStreamed[event.OutputIndex] = true
					reasoningByIndex[event.OutputIndex] += event.Delta
					if !send("reasoning", reasoningEvent{
						Text:        event.Delta,
						ItemID:      event.ItemID,
						OutputIndex: event.OutputIndex,
					}) {
						break read
					}

				case "response.function_call_arguments.delta":
					if event.Delta != "" {
						functionArgs[event.OutputIndex] += event.Delta
					}

				case "response.output_text.delta", "response.refusal.delta":
					if event.Delta == "" {
						continue
					}
					content.WriteString(event.Delta)
					if !send("delta", deltaEvent{Text: event.Delta}) {
						break read
					}

				case "response.output_text.done":
					if event.Text != "" {
						doneText = event.Text
					}

				case "response.refusal.done":
					if event.Refusal != "" {
						doneText = event.Refusal
					}

				case "response.content_part.done":
					if event.Part != nil {
						switch {
						case event.Part.Text != "":
							doneText = event.Part.Text
						case event.Part.Refusal != "":
							doneText = event.Part.Refusal
						}
					}

				case "response.output_text.annotation.added":
					if event.Annotation == nil {
						continue
					}
					citation, changed := addCitation(*event.Annotation)
					if changed && !send("citation", citationEvent{Citations: []Citation{citation}}) {
						break read
					}

				case "response.completed":
					outcome.terminal = true
					roundCompleted = event.Response
					if roundCompleted == nil {
						continue
					}
					outcome.usage = roundCompleted.Usage
					switch {
					case roundCompleted.IncompleteDetails != nil && roundCompleted.IncompleteDetails.Reason != "":
						result.finishReason = roundCompleted.IncompleteDetails.Reason
					case roundCompleted.Status != "":
						result.finishReason = roundCompleted.Status
					}
					if roundCompleted.Error != nil {
						outcome.err = truncate(roundCompleted.Error.Message, 300)
					}
					mergeCompletedOutput(outputByIndex, roundCompleted.Output)

				case "response.incomplete", "response.failed":
					outcome.terminal = true
					if event.Response != nil {
						outcome.usage = event.Response.Usage
						if event.Response.Error != nil {
							outcome.err = truncate(event.Response.Error.Message, 300)
						}
						if event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason != "" {
							result.finishReason = event.Response.IncompleteDetails.Reason
						}
					}
					if outcome.err == "" {
						outcome.err = opts.provider.Label + " stopped before the reply was complete."
					}

				case "error":
					if event.Error != nil {
						outcome.err = truncate(event.Error.Message, 300)
					}
					if outcome.err == "" {
						outcome.err = opts.provider.Label + " reported an error mid-stream."
					}
				}
			}

			if readErr != nil {
				if readErr != io.EOF && !result.clientGone && opts.requestCtx.Err() == nil && opts.ctx.Err() == nil {
					outcome.err = "The connection to " + opts.provider.Label + " dropped."
				}
				break
			}
		}

		if opts.requestCtx.Err() != nil {
			result.clientGone = true
		}
		if !result.clientGone && !outcome.terminal && outcome.err == "" && opts.ctx.Err() == nil {
			outcome.err = "The connection to " + opts.provider.Label + " closed before the reply finished."
		}

		indices := make([]int, 0, len(outputByIndex))
		for index := range outputByIndex {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		outcome.output = make([]responseItem, 0, len(indices))
		for _, index := range indices {
			item := outputByIndex[index]
			if item.Arguments == "" && functionArgs[index] != "" {
				item.Arguments = functionArgs[index]
				outputByIndex[index] = item
			}
			outcome.output = append(outcome.output, item)
		}
		for _, item := range outcome.output {
			if item.Type == "function_call" {
				outcome.toolReasoning = strings.Join(pendingReasoning, "\n\n")
				break
			}
		}

		streamed := content.String()[roundStart:]
		final := lastResponseMessageText(outcome.output)
		switch {
		case final != "" && final != streamed &&
			(strings.TrimSpace(streamed) == "" || strings.HasPrefix(final, streamed)):
			text := content.String()
			prefix := text[:roundStart]
			content.Reset()
			content.WriteString(prefix)
			content.WriteString(final)
			outcome.text = final
		case strings.TrimSpace(streamed) != "":
			outcome.text = streamed
		case strings.TrimSpace(doneText) != "":
			content.WriteString(doneText)
			outcome.text = doneText
		case roundCompleted != nil && strings.TrimSpace(roundCompleted.OutputText) != "":
			content.WriteString(roundCompleted.OutputText)
			outcome.text = roundCompleted.OutputText
		}

		transcriptSteps, transcriptReasoning := buildResponseTranscript(outcome.output, reasoningByIndex, citations)
		outcome.fallbackSteps = transcriptSteps
		outcome.reasoning = transcriptReasoning
		outcome.calls = dedupeFunctionCalls(responseFunctionCalls(outcome.output))
		return outcome
	}

	remainingCalls := opts.toolCalls
	if remainingCalls < 0 {
		remainingCalls = 0
	}
	webProvider := normalizeWebProvider(opts.webProvider)
	tools := opts.tools
	input := append([]responseInputMessage(nil), opts.input...)
	resp := opts.resp
	rounds := 0

	for {
		rounds++
		if result.clientGone {
			break
		}
		if rounds > maxToolCallsLimit*2+6 {
			result.streamErr = "The model kept requesting tools after the budget was exhausted."
			break
		}

		outcome := consumeRound(resp, rounds)
		result.usage = outcome.usage
		for _, step := range outcome.fallbackSteps {
			appendStep(step)
		}
		if result.clientGone {
			break
		}
		if outcome.err != "" {
			result.streamErr = outcome.err
			break
		}
		if opts.provider.Local {
			result.reasoning = outcome.reasoning
			break
		}
		if len(outcome.calls) == 0 {
			result.reasoning = outcome.reasoning
			break
		}

		input = append(input, responseFunctionEcho(outcome.output)...)
		fellBack := false
		attachedReasoning := false
		for _, call := range outcome.calls {
			callID := outputCallID(call)
			if remainingCalls <= 0 {
				input = append(input, responseInputMessage{
					Type:   "function_call_output",
					CallID: callID,
					Output: "The tool budget for this reply is exhausted. Answer from the information already gathered.",
				})
				continue
			}

			remainingCalls--
			if webProvider != webProviderTavily {
				input = append(input, responseInputMessage{
					Type:   "function_call_output",
					CallID: callID,
					Output: "That tool is not available. Answer from the information already gathered.",
				})
				continue
			}

			// Tavily is a plain HTTP call, so keep the idle watchdog alive
			// while it runs instead of letting it cancel a slow extraction.
			opts.watchdog.Reset(idleStreamTimeout)
			step, output, sources, err := s.runTavilyCall(opts.ctx, opts.tavilyKey, call)
			opts.watchdog.Reset(idleStreamTimeout)
			if !attachedReasoning && step.Reasoning == "" {
				step.Reasoning = outcome.toolReasoning
				attachedReasoning = true
			}
			appendStep(step)
			addCitations(sources)
			if result.clientGone {
				break
			}
			if err != nil {
				s.log.Warn("tavily tool failed; switching to OpenRouter", "tool", call.Name, "err", err)
				webProvider = webProviderOpenRouter
				fellBack = true
				// A failed search produced no research, so do not let it eat
				// the budget the OpenRouter fallback needs.
				remainingCalls++
			}
			input = append(input, responseInputMessage{
				Type:   "function_call_output",
				CallID: callID,
				Output: output,
			})
		}
		if result.clientGone {
			break
		}
		if fellBack {
			input = append(input, responseInputMessage{
				Type:    "message",
				Role:    "user",
				Content: "Tavily failed. Continue this reply using OpenRouter web search and fetch instead. Do not mention the provider switch unless it matters to the answer.",
			})
		}

		if remainingCalls <= 0 {
			tools = nil
		} else {
			tools = buildServerTools(webProvider, opts.toolsEnabled, opts.timezone, remainingCalls)
		}

		opts.watchdog.Reset(idleStreamTimeout)
		var err error
		resp, err = opts.provider.responses(opts.ctx, s.client, opts.model, input, opts.systemPrompt,
			conversationProviderPrefs(opts.conv, ignoredProviderList(opts.profile.IgnoredProviders)),
			tools, remainingCalls, opts.reasoning)
		if err != nil {
			if opts.requestCtx.Err() != nil {
				result.clientGone = true
				break
			}
			if errors.Is(err, context.DeadlineExceeded) {
				result.streamErr = opts.provider.Label + " did not respond in time."
			} else {
				result.streamErr = "Could not reach " + opts.provider.Label + "."
			}
			break
		}
		if resp.StatusCode != http.StatusOK {
			detail := upstreamErrorMessage(resp)
			resp.Body.Close()
			if detail == "" {
				detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			result.streamErr = opts.provider.Label + " stopped the tool round: " + truncate(detail, 300)
			break
		}
	}

	result.text = content.String()
	result.steps = allSteps
	result.citations = citations
	attachUnmatchedCitations(result.steps, result.citations)
	return result
}
