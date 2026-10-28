package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testDataURL(mime string, payload []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(payload)
}

func TestValidateAttachments(t *testing.T) {
	image := testDataURL("image/png", []byte("not-really-a-png"))
	pdf := testDataURL("application/pdf", []byte("%PDF-1.4"))
	text := testDataURL("text/plain", []byte("package main\n"))

	got, err := validateAttachments([]Attachment{
		{Kind: "image", Name: `..\pictures\shot.png`, MIME: "image/png", Data: image},
		{Kind: "PDF", Name: "report.pdf", MIME: "application/pdf", Data: pdf},
		{Kind: "text", Name: "main.go", MIME: "text/plain", Data: text},
	})
	if err != nil {
		t.Fatalf("validateAttachments: %v", err)
	}
	if len(got) != 3 || got[0].Kind != "image" || got[0].Name != "shot.png" ||
		got[1].Kind != "pdf" || got[2].Kind != "text" {
		t.Fatalf("validated = %+v", got)
	}
	if got[0].MIME != "image/png" || got[1].Name != "report.pdf" || got[2].Name != "main.go" {
		t.Fatalf("mime/name changed: %+v", got)
	}

	cases := []struct {
		name string
		in   []Attachment
	}{
		{"wrong kind", []Attachment{{Kind: "video", Name: "a.mp4", MIME: "video/mp4", Data: testDataURL("video/mp4", []byte("x"))}}},
		{"mime mismatch", []Attachment{{Kind: "image", Name: "a.png", MIME: "image/png", Data: testDataURL("image/jpeg", []byte("x"))}}},
		{"not base64", []Attachment{{Kind: "image", Name: "a.png", MIME: "image/png", Data: "data:image/png,raw"}}},
		{"not a data URL", []Attachment{{Kind: "pdf", Name: "a.pdf", MIME: "application/pdf", Data: "hello"}}},
	}
	for _, tc := range cases {
		if _, err := validateAttachments(tc.in); err == nil {
			t.Fatalf("%s: expected an error", tc.name)
		}
	}
}

func TestUpstreamContent(t *testing.T) {
	if got := upstreamContent("hello", nil); got != "hello" {
		t.Fatalf("text-only content = %#v", got)
	}

	image := Attachment{Kind: "image", Name: "a.jpg", MIME: "image/jpeg", Data: testDataURL("image/jpeg", []byte("x"))}
	pdf := Attachment{Kind: "pdf", Name: "a.pdf", MIME: "application/pdf", Data: testDataURL("application/pdf", []byte("y"))}
	text := Attachment{Kind: "text", Name: "main.go", MIME: "text/plain", Data: testDataURL("text/plain", []byte("package main"))}
	parts, ok := upstreamContent("look", []Attachment{image, pdf, text}).([]contentPart)
	if !ok {
		t.Fatalf("multimodal content is not a part list")
	}
	if len(parts) != 4 || parts[0].Type != "text" || parts[0].Text != "look" {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != image.Data {
		t.Fatalf("image part = %+v", parts[1])
	}
	if parts[2].Type != "file" || parts[2].File == nil || parts[2].File.Filename != "a.pdf" ||
		parts[2].File.FileData != pdf.Data {
		t.Fatalf("file part = %+v", parts[2])
	}
	if parts[3].Type != "text" || parts[3].Text != "Attached file: main.go\n\npackage main" {
		t.Fatalf("text part = %+v", parts[3])
	}

	if !hasPDFAttachment([]Attachment{pdf}) || hasPDFAttachment([]Attachment{image}) {
		t.Fatal("hasPDFAttachment did not distinguish file kinds")
	}
}

func TestAttachmentSizeLimit(t *testing.T) {
	tooLarge := testDataURL("image/png", make([]byte, maxImageAttachmentSize+1))
	_, err := validateAttachments([]Attachment{{Kind: "image", Name: "big.png", MIME: "image/png", Data: tooLarge}})
	if err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversized attachment error = %v", err)
	}
}
