package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

// encodeMessage creates a Content-Length framed message.
func encodeMessage(msg any) string {
	data, _ := json.Marshal(msg)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
}

// decodeResponses parses Content-Length framed responses from output.
func decodeResponses(output string) ([]json.RawMessage, error) {
	var results []json.RawMessage
	remaining := output

	for len(remaining) > 0 {
		headerEnd := strings.Index(remaining, "\r\n\r\n")
		if headerEnd == -1 {
			break
		}

		header := remaining[:headerEnd]
		var contentLength int
		_, err := fmt.Sscanf(header, "Content-Length: %d", &contentLength)
		if err != nil {
			return nil, fmt.Errorf("parse Content-Length: %w", err)
		}

		contentStart := headerEnd + 4
		contentEnd := contentStart + contentLength
		if contentEnd > len(remaining) {
			return nil, fmt.Errorf("incomplete message: need %d bytes, have %d", contentLength, len(remaining)-contentStart)
		}

		results = append(results, json.RawMessage(remaining[contentStart:contentEnd]))
		remaining = remaining[contentEnd:]
	}

	return results, nil
}

// =============================================================================
// Server Tests
// =============================================================================

func TestServer_Initialize(t *testing.T) {
	s := NewServer()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
	}
	if resp.ID != 1 {
		t.Errorf("id = %d, want %d", resp.ID, 1)
	}
	if resp.Result.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocolVersion = %q, want %q", resp.Result.ProtocolVersion, "2024-11-05")
	}
	if resp.Result.ServerInfo.Name != "brain-mcp" {
		t.Errorf("serverInfo.name = %q, want %q", resp.Result.ServerInfo.Name, "brain-mcp")
	}
	if resp.Result.ServerInfo.Version != "1.0.0" {
		t.Errorf("serverInfo.version = %q, want %q", resp.Result.ServerInfo.Version, "1.0.0")
	}
	if resp.Result.Capabilities.Tools == nil {
		t.Error("capabilities.tools should not be nil")
	}
}

func TestServer_ToolsList(t *testing.T) {
	s := NewServer()
	s.RegisterTool(Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"name": {Type: "string"}},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "ok", nil
	})

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				InputSchema struct {
					Type       string         `json:"type"`
					Properties map[string]any `json:"properties"`
					Required   []string       `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Result.Tools))
	}

	tool := resp.Result.Tools[0]
	if tool.Name != "test_tool" {
		t.Errorf("tool name = %q, want %q", tool.Name, "test_tool")
	}
	if tool.Description != "A test tool" {
		t.Errorf("tool description = %q, want %q", tool.Description, "A test tool")
	}
	if tool.InputSchema.Type != "object" {
		t.Errorf("inputSchema.type = %q, want %q", tool.InputSchema.Type, "object")
	}
}

func TestServer_ToolsCall(t *testing.T) {
	s := NewServer()
	s.RegisterTool(Tool{
		Name:        "echo",
		Description: "Echoes input",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"message": {Type: "string"}},
			Required:   []string{"message"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		msg, _ := args["message"].(string)
		return "Echo: " + msg, nil
	})

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "hello"},
		},
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(resp.Result.Content))
	}

	if resp.Result.Content[0].Type != "text" {
		t.Errorf("content type = %q, want %q", resp.Result.Content[0].Type, "text")
	}
	if resp.Result.Content[0].Text != "Echo: hello" {
		t.Errorf("content text = %q, want %q", resp.Result.Content[0].Text, "Echo: hello")
	}
}

func TestServer_ToolsCall_UnknownTool(t *testing.T) {
	s := NewServer()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "nonexistent",
			"arguments": map[string]any{},
		},
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error response for unknown tool")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32602)
	}
}

func TestServer_ToolsCall_HandlerError(t *testing.T) {
	s := NewServer()
	s.RegisterTool(Tool{
		Name:        "failing",
		Description: "Always fails",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "", fmt.Errorf("something went wrong")
	})

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "failing",
			"arguments": map[string]any{},
		},
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	// Tool errors should return as text content, not JSON-RPC errors
	// (matching TypeScript behavior)
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(resp.Result.Content))
	}

	if !strings.Contains(resp.Result.Content[0].Text, "something went wrong") {
		t.Errorf("error text should contain error message, got %q", resp.Result.Content[0].Text)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	s := NewServer()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "unknown/method",
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32601)
	}
}

func TestServer_Notification_NoResponse(t *testing.T) {
	s := NewServer()

	// Notifications have no "id" field
	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}

	input := encodeMessage(notification)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	// Notifications should not produce a response
	if stdout.Len() != 0 {
		t.Errorf("expected no output for notification, got %d bytes: %s", stdout.Len(), stdout.String())
	}
}

func TestServer_MultipleMessages(t *testing.T) {
	s := NewServer()
	s.RegisterTool(Tool{
		Name:        "ping",
		Description: "Returns pong",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "pong", nil
	})

	// Send initialize + notification + tools/call in sequence
	var input strings.Builder
	input.WriteString(encodeMessage(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{},
	}))
	input.WriteString(encodeMessage(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}))
	input.WriteString(encodeMessage(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "ping",
			"arguments": map[string]any{},
		},
	}))

	stdin := strings.NewReader(input.String())
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	// Should get 2 responses (initialize + tools/call), notification produces none
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
}

func TestServer_StringID(t *testing.T) {
	s := NewServer()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      "abc-123",
		"method":  "initialize",
		"params":  map[string]any{},
	}

	input := encodeMessage(req)
	stdin := strings.NewReader(input)
	var stdout bytes.Buffer

	err := s.Serve(context.Background(), stdin, &stdout)
	if err != nil && err != io.EOF {
		t.Fatalf("Serve returned error: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	// Verify the string ID is echoed back
	var resp struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if string(resp.ID) != `"abc-123"` {
		t.Errorf("id = %s, want %q", resp.ID, "abc-123")
	}
}
