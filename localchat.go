package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// localChatRequest is deliberately minimal. Server tools, provider routing,
// reasoning effort and plugins are OpenRouter concepts; local servers reject
// unknown fields, so none of them are sent.
type localChatRequest struct {
	Model    string            `json:"model"`
	Messages []upstreamMessage `json:"messages"`
	Stream   bool              `json:"stream"`
}

// localResponses posts to an OpenAI-compatible /chat/completions endpoint and
// rewrites its stream into the OpenResponses event shape the rest of Clachan
// already consumes. The caller owns the returned body.
func (p *Provider) localResponses(ctx context.Context, client *http.Client, model string, msgs []upstreamMessage) (*http.Response, error) {
	payload, err := json.Marshal(localChatRequest{Model: model, Messages: msgs, Stream: true})
	if err != nil {
		return nil, fmt.Errorf("encode chat request: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	p.headers(req)

	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	}

	upstream := resp.Body
	reader, writer := io.Pipe()
	resp.Body = &adaptedBody{pipe: reader, upstream: upstream, cancel: cancel}
	go translateLocalStream(streamCtx, p.Label, model, upstream, writer)
	return resp, nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	c.cancel()
	return c.ReadCloser.Close()
}

type adaptedBody struct {
	pipe     *io.PipeReader
	upstream io.ReadCloser
	cancel   context.CancelFunc
	once     sync.Once
}

func (b *adaptedBody) Read(p []byte) (int, error) { return b.pipe.Read(p) }

func (b *adaptedBody) Close() error {
	b.once.Do(func() {
		// Cancel first so a translator blocked in a read or a pipe write can
		// exit, then release both ends.
		b.cancel()
		_ = b.upstream.Close()
		_ = b.pipe.Close()
	})
	return nil
}

type localStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          json.RawMessage `json:"content"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
			Reasoning        json.RawMessage `json:"reasoning"`
			Thinking         json.RawMessage `json:"thinking"`
		} `json:"delta"`
		Message *struct {
			Content          json.RawMessage `json:"content"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
			Reasoning        json.RawMessage `json:"reasoning"`
			Thinking         json.RawMessage `json:"thinking"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// translateLocalStream reads an OpenAI SSE (or a plain JSON response from a
// server that ignores stream=true) and writes OpenResponses events.
func translateLocalStream(ctx context.Context, label, model string, upstream io.Reader, writer *io.PipeWriter) {
	defer writer.Close()

	out := &localEventWriter{writer: writer, model: model}
	reader := bufio.NewReaderSize(upstream, 32<<10)

	first, err := readNonBlankLine(reader)
	if err != nil {
		if err != io.EOF && ctx.Err() == nil && first == "" {
			out.emitError("The " + label + " connection failed.")
		}
		return
	}

	if isSSELine(first) {
		out.readSSE(ctx, label, reader, first)
		return
	}

	// A server that ignores stream=true returns one JSON document. Read the
	// first line back into the payload and decode the whole body.
	var body bytes.Buffer
	body.WriteString(first)
	body.WriteByte('\n')
	if _, err := io.Copy(&body, io.LimitReader(reader, 32<<20)); err != nil && ctx.Err() == nil {
		out.emitError("The " + label + " connection failed.")
		return
	}
	out.readJSON(label, body.Bytes())
}

func readNonBlankLine(reader *bufio.Reader) (string, error) {
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) != "" {
			return line, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func isSSELine(line string) bool {
	return strings.HasPrefix(line, "data:") ||
		strings.HasPrefix(line, "event:") ||
		strings.HasPrefix(line, ":")
}

type localEventWriter struct {
	writer       *io.PipeWriter
	model        string
	content      strings.Builder
	reasoning    strings.Builder
	usage        *usage
	finishReason string
	terminal     bool
	broken       bool
	emittedItem  bool
}

func (w *localEventWriter) event(payload any) bool {
	if w.broken {
		return false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(w.writer, "data: %s\n\n", data); err != nil {
		w.broken = true
		return false
	}
	return true
}

func (w *localEventWriter) readSSE(ctx context.Context, label string, reader *bufio.Reader, first string) {
	line := first
	for {
		data, done := parseSSELine(line)
		if done {
			w.finish()
			return
		}
		if data != "" && !w.handleChunk(data) {
			return
		}

		next, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				w.emitError("The " + label + " connection dropped mid-reply.")
			}
			// EOF without [DONE] or finish_reason is a truncated stream: keep
			// the deltas that arrived, but do not claim the reply completed.
			return
		}
		line = strings.TrimRight(next, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
	}
}

func (w *localEventWriter) readJSON(label string, payload []byte) {
	if !w.handleChunk(string(payload)) {
		return
	}
	if w.terminal {
		return
	}
	if w.content.Len() == 0 && w.reasoning.Len() == 0 {
		w.emitError("The " + label + " server returned an empty response.")
		return
	}
	// A non-streaming reply is complete by definition; there is no [DONE].
	w.finish()
}

func (w *localEventWriter) handleChunk(data string) bool {
	var chunk localStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		// Some servers prefix errors with non-JSON text. Do not fail the whole
		// reply for one unreadable frame.
		return true
	}
	if chunk.Error != nil {
		message := strings.TrimSpace(chunk.Error.Message)
		if message == "" {
			message = "The backend reported an error."
		}
		w.emitError(message)
		return false
	}
	if chunk.Usage != nil {
		w.usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		deltaContent := rawText(choice.Delta.Content)
		deltaReasoning := firstText(
			rawText(choice.Delta.ReasoningContent),
			rawText(choice.Delta.Reasoning),
			rawText(choice.Delta.Thinking),
		)

		if choice.Message != nil {
			// Coalesce message-shaped chunks (a non-stream reply or a server
			// that sends the whole answer in one frame).
			if deltaContent == "" {
				deltaContent = rawText(choice.Message.Content)
			}
			if deltaReasoning == "" {
				deltaReasoning = firstText(
					rawText(choice.Message.ReasoningContent),
					rawText(choice.Message.Reasoning),
					rawText(choice.Message.Thinking),
				)
			}
		}

		if deltaReasoning != "" {
			w.reasoning.WriteString(deltaReasoning)
			if !w.event(map[string]any{
				"type":         "response.reasoning_summary_text.delta",
				"item_id":      "rs_local",
				"output_index": 0,
				"delta":        deltaReasoning,
			}) {
				return false
			}
		}
		if deltaContent != "" {
			w.content.WriteString(deltaContent)
			if !w.event(map[string]any{
				"type":         "response.output_text.delta",
				"item_id":      "msg_local",
				"output_index": 1,
				"delta":        deltaContent,
			}) {
				return false
			}
		}

		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			w.finishReason = strings.TrimSpace(*choice.FinishReason)
			w.finishReply()
			return false
		}
	}
	return true
}

func (w *localEventWriter) finish() {
	if w.terminal {
		return
	}
	w.finishReply()
}

func (w *localEventWriter) finishReply() {
	if w.terminal {
		return
	}
	w.terminal = true

	output := make([]responseItem, 0, 2)
	if w.reasoning.Len() > 0 {
		output = append(output, responseItem{
			ID:      "rs_local",
			Type:    "reasoning",
			Status:  "completed",
			Summary: []responseText{{Type: "summary_text", Text: w.reasoning.String()}},
		})
	}
	if w.content.Len() > 0 {
		content, _ := json.Marshal([]responseText{{Type: "output_text", Text: w.content.String()}})
		output = append(output, responseItem{
			ID:      "msg_local",
			Type:    "message",
			Status:  "completed",
			Role:    "assistant",
			Content: content,
		})
	}

	// Emit the terminal items so buffered or non-streaming servers still have
	// their text recovered by the shared Responses transcript builder.
	for index, item := range output {
		if !w.event(map[string]any{
			"type":         "response.output_item.done",
			"output_index": index,
			"item":         item,
		}) {
			return
		}
		w.emittedItem = true
	}

	status := "completed"
	result := map[string]any{
		"id":     "resp_local",
		"status": status,
		"model":  w.model,
		"output": output,
	}
	switch w.finishReason {
	case "length":
		result["status"] = "incomplete"
		result["incomplete_details"] = map[string]string{"reason": "max_output_tokens"}
	case "content_filter":
		result["status"] = "incomplete"
		result["incomplete_details"] = map[string]string{"reason": "content_filter"}
	}
	if w.usage != nil {
		result["usage"] = w.usage
	}
	w.event(map[string]any{"type": "response.completed", "response": result})
}

func (w *localEventWriter) emitError(message string) {
	if w.broken {
		return
	}
	w.terminal = true
	w.event(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "backend_error",
			"message": truncate(message, 300),
		},
	})
}

// rawText decodes a model field that is usually a string but may be null, an
// array of content parts, or a number on a broken server.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var out strings.Builder
		for _, part := range parts {
			out.WriteString(part.Text)
		}
		return out.String()
	}
	return ""
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// localChatMessages flattens a conversation into the plain text messages local
// servers expect. Text attachments are inlined; image and PDF attachments are
// replaced with a note because local backends are text-only in this release.
func localChatMessages(instructions string, history []Message, userContent string, userAttachments []Attachment, includeUser bool) []upstreamMessage {
	out := make([]upstreamMessage, 0, len(history)+2)
	if prompt := strings.TrimSpace(instructions); prompt != "" {
		out = append(out, upstreamMessage{Role: "system", Content: prompt})
	}
	for _, message := range history {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := localMessageText(message.Content, message.Attachments)
		if text == "" {
			continue
		}
		out = append(out, upstreamMessage{Role: message.Role, Content: text})
	}
	if includeUser {
		if text := localMessageText(userContent, userAttachments); text != "" {
			out = append(out, upstreamMessage{Role: "user", Content: text})
		}
	}
	return out
}

func localMessageText(text string, attachments []Attachment) string {
	text = strings.TrimSpace(text)
	parts := make([]string, 0, len(attachments)+1)
	if text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range attachments {
		switch attachment.Kind {
		case "text":
			parts = append(parts, "Attached file: "+attachment.Name+"\n\n"+attachmentText(attachment.Data))
		case "image":
			parts = append(parts, "[Image attachment omitted: "+attachment.Name+". Local backends are text-only.]")
		case "pdf":
			parts = append(parts, "[PDF attachment omitted: "+attachment.Name+". Local backends are text-only.]")
		}
	}
	return strings.Join(parts, "\n\n")
}

func hasNonTextAttachment(attachments []Attachment) bool {
	for _, attachment := range attachments {
		if attachment.Kind != "text" {
			return true
		}
	}
	return false
}
