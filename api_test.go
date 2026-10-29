package main

import (
	"strings"
	"testing"
)

func TestValidatePromptAllowsLargeBody(t *testing.T) {
	body := strings.Repeat("x", 256<<10)
	title, got, err := validatePrompt("Large prompt", body)
	if err != nil {
		t.Fatalf("validatePrompt: %v", err)
	}
	if title != "Large prompt" || got != body {
		t.Fatal("validatePrompt changed the prompt")
	}
}

func TestValidatePromptRejectsOversizeBody(t *testing.T) {
	body := strings.Repeat("x", maxPromptBytes+1)
	if _, _, err := validatePrompt("Large prompt", body); err == nil {
		t.Fatal("validatePrompt accepted an oversize body")
	}
}

func TestValidatePromptTitleCountsCharacters(t *testing.T) {
	title := strings.Repeat("é", maxPromptTitle)
	if _, _, err := validatePrompt(title, "body"); err != nil {
		t.Fatalf("validatePrompt rejected %d characters: %v", maxPromptTitle, err)
	}
	title += "é"
	if _, _, err := validatePrompt(title, "body"); err == nil {
		t.Fatal("validatePrompt accepted too many characters")
	}
}
