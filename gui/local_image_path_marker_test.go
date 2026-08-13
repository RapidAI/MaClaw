package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUserContentWithLocalStagingEmbedsSelectedImageForVisionModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selected.jpg")
	image, err := base64.StdEncoding.DecodeString("/9j/2wBDAP//////////////////////////////////////////////////////////////////////////////////////2wBDAf//////////////////////////////////////////////////////////////////////////////////////wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAf/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/9oADAMBAAIQAxAAAAH/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/9oACAEBAAEFAqf/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oACAEDAQE/Aaf/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oACAECAQE/Aaf/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/9oACAEBAAY/Ap//xAAUEAEAAAAAAAAAAAAAAAAAAAAA/9oACAEBAAE/If/EABQRAQAAAAAAAAAAAAAAAAAAABD/2gAIAQMBAT8Qn//EABQRAQAAAAAAAAAAAAAAAAAAABD/2gAIAQIBAT8Qn//Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}

	userText := strings.Join([]string{
		"What is in this image?",
		filePathPromptPrefix,
		path,
		"For image files, the host sends them directly to a vision-capable model when available. Analyze attached images first; do not re-capture them or use read_file on image bytes. Use OCR only for exact text when needed.",
	}, "\n")
	content := buildUserContentWithLocalStaging(userText, nil, "openai", true, nil, nil, true)
	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("content type = %T, want multimodal blocks", content)
	}

	wantURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(image)
	var text string
	imageFound := false
	for _, block := range blocks {
		item, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch item["type"] {
		case "text":
			text, _ = item["text"].(string)
		case "image_url":
			payload, _ := item["image_url"].(map[string]interface{})
			imageFound = payload["url"] == wantURL
		}
	}
	if !imageFound {
		t.Fatalf("selected image was not embedded as an image_url: %#v", blocks)
	}
	if !strings.Contains(text, "Analyze the image directly before calling tools") {
		t.Fatalf("vision-first host instruction missing from text block: %q", text)
	}
}

func TestSelectedLocalImageAttachmentsSkipsMissingAndDeduplicatesPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selected.png")
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAFgAH/6x1vJwAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.png")
	text := strings.Join([]string{filePathPromptPrefix, path, path, missing}, "\n")
	attachments, notes := selectedLocalImageAttachments(text)
	if len(attachments) != 1 || attachments[0].FileName != "selected.png" {
		t.Fatalf("attachments = %#v", attachments)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "missing.png") {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestSelectedLocalImageAttachmentsRejectsRenamedNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-an-image.png")
	if err := os.WriteFile(path, []byte("not image bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachments, notes := selectedLocalImageAttachments(filePathPromptPrefix + "\n" + path)
	if len(attachments) != 0 {
		t.Fatalf("attachments = %#v, want none", attachments)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "not a supported, valid raster image") {
		t.Fatalf("notes = %#v", notes)
	}
}

func TestSelectedLocalImageAttachmentsUsesDetectedMimeType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actually-png.jpg")
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAFgAH/6x1vJwAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	attachments, notes := selectedLocalImageAttachments(filePathPromptPrefix + "\n" + path)
	if len(notes) != 0 || len(attachments) != 1 {
		t.Fatalf("attachments=%#v notes=%#v", attachments, notes)
	}
	if attachments[0].MimeType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", attachments[0].MimeType)
	}
}

func TestSelectedLocalImagePathsStopsAtInstructionOrProse(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.png")
	mentionedLater := filepath.Join(t.TempDir(), "mentioned-later.png")
	text := strings.Join([]string{
		filePathPromptPrefix,
		first,
		"For image files, the host sends them directly to a vision-capable model when available.",
		"Do not upload " + mentionedLater + " because it is only mentioned in the request.",
	}, "\n")
	got := selectedLocalImagePaths(text)
	if len(got) != 1 || got[0] != first {
		t.Fatalf("paths = %#v, want only %q", got, first)
	}
}
