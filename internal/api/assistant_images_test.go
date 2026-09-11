package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestChatMessage_MarshalJSON_StringForm verifies a text-only message still
// serializes `content` as a plain string (unchanged legacy behavior).
func TestChatMessage_MarshalJSON_StringForm(t *testing.T) {
	m := chatMessage{Role: "user", Content: "hello"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["content"] != "hello" {
		t.Errorf("content = %#v, want plain string \"hello\"", got["content"])
	}
}

// TestChatMessage_MarshalJSON_MultimodalForm verifies that when ContentParts is
// set, `content` serializes as an OpenAI-style array of parts with text +
// image_url, and the plain Content string is not emitted.
func TestChatMessage_MarshalJSON_MultimodalForm(t *testing.T) {
	m := chatMessage{
		Role: "user",
		ContentParts: []contentPart{
			{Type: "text", Text: "look at this"},
			{Type: "image_url", ImageURL: &imageURLPart{URL: "data:image/png;base64,AAAA"}},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	parts, ok := got["content"].([]any)
	if !ok {
		t.Fatalf("content is not an array: %T (%v)", got["content"], got["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("want 2 content parts, got %d", len(parts))
	}
	text := parts[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "look at this" {
		t.Errorf("part[0] = %#v, want text part", text)
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("part[1] type = %v, want image_url", img["type"])
	}
	iu, ok := img["image_url"].(map[string]any)
	if !ok || iu["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("part[1] image_url = %#v, want data URL", img["image_url"])
	}
}

// TestValidImageDataURLs filters to well-formed base64 image data URLs only.
func TestValidImageDataURLs(t *testing.T) {
	in := []string{
		"data:image/png;base64,AAAA",  // ok
		"data:image/jpeg;base64,BBBB", // ok
		"  data:image/gif;base64,CCCC  ", // ok (trimmed)
		"data:text/plain;base64,DDDD", // wrong media type
		"data:image/png,EEEE",         // missing ;base64,
		"https://example.com/x.png",   // not a data URL
		"",                            // empty
	}
	got := validImageDataURLs(in)
	if len(got) != 3 {
		t.Fatalf("want 3 valid image URLs, got %d: %v", len(got), got)
	}
	for _, u := range got {
		if !strings.HasPrefix(u, "data:image/") || !strings.Contains(u, ";base64,") {
			t.Errorf("kept invalid url %q", u)
		}
	}
}

// TestBuildUserMessage_NoImages returns a plain-string user message and does
// not leak an images array into the JSON text view.
func TestBuildUserMessage_NoImages(t *testing.T) {
	req := AssistantChatRequest{Message: "hi", Project: "p1"}
	m := buildUserMessage(req)
	if m.Role != "user" {
		t.Fatalf("role = %q, want user", m.Role)
	}
	if len(m.ContentParts) != 0 {
		t.Fatalf("want no content parts, got %d", len(m.ContentParts))
	}
	if m.Content == "" {
		t.Fatalf("want a JSON text body")
	}
	if strings.Contains(m.Content, "\"images\"") {
		t.Errorf("images should be stripped from the text view: %s", m.Content)
	}
}

// TestBuildUserMessage_WithImages emits a multimodal message: one text part
// then one image_url part per valid image. Malformed images are dropped, and
// the text part must not restate the image data.
func TestBuildUserMessage_WithImages(t *testing.T) {
	req := AssistantChatRequest{
		Message: "what is this",
		Project: "p1",
		Images: []string{
			"data:image/png;base64,AAAA",
			"not-an-image",
			"data:image/jpeg;base64,BBBB",
		},
	}
	m := buildUserMessage(req)
	if len(m.ContentParts) != 3 { // 1 text + 2 valid images
		t.Fatalf("want 3 parts (text + 2 images), got %d", len(m.ContentParts))
	}
	if m.ContentParts[0].Type != "text" {
		t.Errorf("first part must be text, got %q", m.ContentParts[0].Type)
	}
	if strings.Contains(m.ContentParts[0].Text, "base64,") {
		t.Errorf("text part must not contain image data: %s", m.ContentParts[0].Text)
	}
	imgCount := 0
	for _, p := range m.ContentParts[1:] {
		if p.Type != "image_url" || p.ImageURL == nil {
			t.Errorf("expected image_url part, got %#v", p)
			continue
		}
		if !strings.HasPrefix(p.ImageURL.URL, "data:image/") {
			t.Errorf("image part url not a data URL: %q", p.ImageURL.URL)
		}
		imgCount++
	}
	if imgCount != 2 {
		t.Errorf("want 2 image parts, got %d", imgCount)
	}
	// A message with parts must not also carry a plain Content string.
	if m.Content != "" {
		t.Errorf("multimodal message should leave Content empty, got %q", m.Content)
	}
}
