package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	maxAttachments         = 6
	maxImageAttachmentSize = 8 << 20
	maxPDFAttachmentSize   = 12 << 20
	maxTextAttachmentSize  = 512 << 10
	maxTotalAttachmentSize = 24 << 20
	maxAttachmentNameRunes = 120
)

// Attachment is one file sent with a user message. Data is a self-contained
// data URL so the message can be replayed without touching server disk.
type Attachment struct {
	Kind string `json:"kind"` // "image", "pdf", or "text"
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

type imagePart struct {
	URL string `json:"url"`
}

type filePart struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type contentPart struct {
	Type     string     `json:"type"`
	Text     string     `json:"text,omitempty"`
	ImageURL *imagePart `json:"image_url,omitempty"`
	File     *filePart  `json:"file,omitempty"`
}

// validateAttachments checks the client payload before any of it is sent
// upstream or written to SQLite. Size limits are on the decoded bytes; data URLs
// add roughly a third on top.
func validateAttachments(input []Attachment) ([]Attachment, error) {
	if len(input) > maxAttachments {
		return nil, fmt.Errorf("attach at most %d files", maxAttachments)
	}

	out := make([]Attachment, 0, len(input))
	total := 0
	for _, raw := range input {
		kind := strings.ToLower(strings.TrimSpace(raw.Kind))
		mime := strings.ToLower(strings.TrimSpace(raw.MIME))
		data := strings.TrimSpace(raw.Data)

		var limit int
		switch kind {
		case "image":
			switch mime {
			case "image/jpeg", "image/png", "image/webp", "image/gif":
			default:
				return nil, errors.New("images must be JPEG, PNG, WebP, or GIF")
			}
			limit = maxImageAttachmentSize
		case "pdf":
			if mime != "application/pdf" {
				return nil, errors.New("PDF attachments must be application/pdf")
			}
			limit = maxPDFAttachmentSize
		case "text":
			if !isTextAttachmentMIME(mime) {
				return nil, errors.New("text attachments must be text, JSON, XML, or similar")
			}
			limit = maxTextAttachmentSize
		default:
			return nil, errors.New("attachments must be images, PDFs, or text files")
		}

		size, err := decodeAttachmentData(data, mime, limit)
		if err != nil {
			return nil, err
		}
		total += size
		if total > maxTotalAttachmentSize {
			return nil, errors.New("attachments are too large together")
		}

		out = append(out, Attachment{
			Kind: kind,
			Name: sanitizeAttachmentName(raw.Name, kind),
			MIME: mime,
			Data: data,
		})
	}
	return out, nil
}

func decodeAttachmentData(data, wantMIME string, limit int) (int, error) {
	if !strings.HasPrefix(data, "data:") {
		return 0, errors.New("attachment data must be a data URL")
	}
	comma := strings.IndexByte(data, ',')
	if comma < 0 {
		return 0, errors.New("attachment data URL is malformed")
	}

	meta := data[len("data:"):comma]
	fields := strings.Split(meta, ";")
	if len(fields) < 2 || !strings.EqualFold(strings.TrimSpace(fields[0]), wantMIME) {
		return 0, fmt.Errorf("attachment data does not match %s", wantMIME)
	}
	isBase64 := false
	for _, field := range fields[1:] {
		if strings.EqualFold(strings.TrimSpace(field), "base64") {
			isBase64 = true
			break
		}
	}
	if !isBase64 {
		return 0, errors.New("attachment data must be base64 encoded")
	}

	payload := data[comma+1:]
	decodedLen := base64.StdEncoding.DecodedLen(len(payload))
	if decodedLen > limit+2 {
		return 0, fmt.Errorf("attachment is larger than %s", attachmentLimitLabel(limit))
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return 0, errors.New("attachment data is not valid base64")
	}
	if len(decoded) > limit {
		return 0, fmt.Errorf("attachment is larger than %s", attachmentLimitLabel(limit))
	}
	return len(decoded), nil
}

func attachmentLimitLabel(limit int) string {
	if limit >= 1<<20 {
		return fmt.Sprintf("%d MB", limit>>20)
	}
	return fmt.Sprintf("%d KB", limit>>10)
}

func isTextAttachmentMIME(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/javascript",
		"application/typescript", "application/x-yaml", "application/x-sh",
		"application/toml":
		return true
	default:
		return false
	}
}

func sanitizeAttachmentName(name, fallback string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if strings.TrimSpace(name) == "" {
		switch fallback {
		case "pdf":
			return "document.pdf"
		case "text":
			return "file.txt"
		default:
			return "image"
		}
	}
	runes := []rune(name)
	if len(runes) > maxAttachmentNameRunes {
		name = string(runes[:maxAttachmentNameRunes])
	}
	return name
}

// upstreamContent keeps text-only messages as plain strings and switches to
// OpenRouter's content-part array only when files are present.
func upstreamContent(text string, attachments []Attachment) any {
	text = strings.TrimSpace(text)
	if len(attachments) == 0 {
		return text
	}

	parts := make([]contentPart, 0, len(attachments)+1)
	if text != "" {
		parts = append(parts, contentPart{Type: "text", Text: text})
	}
	for _, attachment := range attachments {
		switch attachment.Kind {
		case "image":
			parts = append(parts, contentPart{
				Type:     "image_url",
				ImageURL: &imagePart{URL: attachment.Data},
			})
		case "pdf":
			parts = append(parts, contentPart{
				Type: "file",
				File: &filePart{
					Filename: attachment.Name,
					FileData: attachment.Data,
				},
			})
		case "text":
			parts = append(parts, contentPart{
				Type: "text",
				Text: "Attached file: " + attachment.Name + "\n\n" + attachmentText(attachment.Data),
			})
		}
	}
	return parts
}

type responseContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

// responsesContent maps a Clachan attachment to the OpenResponses input shape.
// Text-only turns stay plain strings, matching how a normal chat turn reads.
func responsesContent(text string, attachments []Attachment) any {
	text = strings.TrimSpace(text)
	if len(attachments) == 0 {
		return text
	}

	parts := make([]responseContentPart, 0, len(attachments)+1)
	if text != "" {
		parts = append(parts, responseContentPart{Type: "input_text", Text: text})
	}
	for _, attachment := range attachments {
		switch attachment.Kind {
		case "image":
			parts = append(parts, responseContentPart{
				Type:     "input_image",
				ImageURL: attachment.Data,
				Detail:   "auto",
			})
		case "pdf":
			parts = append(parts, responseContentPart{
				Type:     "input_file",
				Filename: attachment.Name,
				FileData: attachment.Data,
			})
		case "text":
			parts = append(parts, responseContentPart{
				Type: "input_text",
				Text: "Attached file: " + attachment.Name + "\n\n" + attachmentText(attachment.Data),
			})
		}
	}
	return parts
}

// attachmentText extracts the UTF-8 payload from a validated text data URL.
func attachmentText(data string) string {
	comma := strings.IndexByte(data, ',')
	if comma < 0 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(data[comma+1:])
	if err != nil {
		return ""
	}
	return string(decoded)
}

func hasPDFAttachment(attachments []Attachment) bool {
	for _, attachment := range attachments {
		if attachment.Kind == "pdf" {
			return true
		}
	}
	return false
}
