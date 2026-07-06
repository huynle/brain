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

// encodeMessage serializes msg as one line of newline-delimited JSON (NDJSON),
// matching what a compliant MCP stdio client sends.
func encodeMessage(msg any) string {
	data, _ := json.Marshal(msg)
	return string(data) + "\n"
}

// decodeResponses parses one-JSON-object-per-line responses from output.
// Blank lines are skipped. Returns the raw JSON of each response.
func decodeResponses(output string) ([]json.RawMessage, error) {
	var results []json.RawMessage
	for i, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Validate it's parseable JSON so a bad frame surfaces here,
		// not in every test.
		var probe any
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			return nil, fmt.Errorf("line %d: invalid JSON: %w", i, err)
		}
		results = append(results, json.RawMessage(line))
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

// =============================================================================
// Regression tests for the NDJSON stdio transport
// =============================================================================

// TestServer_NDJSON_LargePayload sends a tools/call whose arguments contain a
// content string well over the old 4 KiB bufio.NewReader default, and over
// 1 MiB, to prove the scanner buffer grows. This is the shape brain_save
// takes when saving big plans.
func TestServer_NDJSON_LargePayload(t *testing.T) {
	s := NewServer()

	// Handler echoes back len(content) so we can assert the whole thing
	// arrived intact.
	s.RegisterTool(Tool{
		Name:        "sink",
		Description: "Consumes big content",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"content": {Type: "string"}},
			Required:   []string{"content"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		c, _ := args["content"].(string)
		return fmt.Sprintf("len=%d", len(c)), nil
	})

	// 1.5 MiB payload — larger than both the old 4 KiB bufio default and
	// 1 MiB, small enough not to hit the 10 MiB cap.
	const size = 1500 * 1024
	big := strings.Repeat("x", size)

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "sink",
			"arguments": map[string]any{"content": big},
		},
	}

	stdin := strings.NewReader(encodeMessage(req))
	var stdout bytes.Buffer
	if err := s.Serve(context.Background(), stdin, &stdout); err != nil && err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := fmt.Sprintf("len=%d", size)
	if len(resp.Result.Content) != 1 || resp.Result.Content[0].Text != want {
		t.Errorf("got %+v, want text=%q", resp.Result.Content, want)
	}
}

// TestServer_NDJSON_EmbeddedNewlinesInString verifies that string values
// containing newlines (e.g. markdown bodies with real "\n" between lines,
// encoded as "\\n" on the wire) round-trip through the stdio transport.
// The MCP spec forbids literal newlines in the framing — but escaped
// newlines inside JSON string values are legal and must not break framing.
func TestServer_NDJSON_EmbeddedNewlinesInString(t *testing.T) {
	s := NewServer()

	var gotContent string
	s.RegisterTool(Tool{
		Name:        "capture",
		Description: "Captures content",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"content": {Type: "string"}},
			Required:   []string{"content"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		gotContent, _ = args["content"].(string)
		return "ok", nil
	})

	// This string has actual '\n' bytes in it. json.Marshal turns each
	// one into the two-character escape sequence \n on the wire, so the
	// resulting JSON object is a single line with no literal newlines.
	multiline := "# Title\nline one\nline two\n```go\nfmt.Println(\"hi\")\n```\n"

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "capture",
			"arguments": map[string]any{"content": multiline},
		},
	}

	wire := encodeMessage(req)
	// Sanity check the wire framing: exactly one '\n' at the end, no
	// literal newlines before it.
	if idx := strings.IndexByte(wire[:len(wire)-1], '\n'); idx != -1 {
		t.Fatalf("wire has embedded newline at %d; MCP spec violation. wire=%q", idx, wire)
	}

	stdin := strings.NewReader(wire)
	var stdout bytes.Buffer
	if err := s.Serve(context.Background(), stdin, &stdout); err != nil && err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if gotContent != multiline {
		t.Errorf("content mismatch:\n got  %q\n want %q", gotContent, multiline)
	}
}

// TestServer_NDJSON_MalformedJSON_ReturnsParseError proves the server no
// longer silently drops malformed input. The old code did `continue`, which
// left the client waiting forever. We now send back -32700 Parse error.
func TestServer_NDJSON_MalformedJSON_ReturnsParseError(t *testing.T) {
	s := NewServer()

	// Truncated JSON — closes the array but never closes the object.
	// This mirrors the exact shape of the client-side truncation bug we
	// were investigating.
	bad := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","tags":["a","b"]` + "\n"

	stdin := strings.NewReader(bad)
	var stdout bytes.Buffer
	if err := s.Serve(context.Background(), stdin, &stdout); err != nil && err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d\noutput=%q", len(responses), stdout.String())
	}

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(responses[0], &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("code = %d, want -32700 (Parse error)", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "Parse error") {
		t.Errorf("message = %q, want it to mention Parse error", resp.Error.Message)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
	}
}

// TestServer_NDJSON_BlankLinesBetweenMessages checks that stray blank lines
// between messages are tolerated. Some clients emit an extra newline for
// readability; the server should not treat that as an error.
func TestServer_NDJSON_BlankLinesBetweenMessages(t *testing.T) {
	s := NewServer()
	s.RegisterTool(Tool{
		Name: "ping", Description: "pong",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "pong", nil
	})

	req1 := encodeMessage(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "ping", "arguments": map[string]any{}},
	})
	req2 := encodeMessage(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "ping", "arguments": map[string]any{}},
	})

	// Insert stray blank lines around and between the two messages.
	input := "\n\n" + req1 + "\n\n" + req2 + "\n"

	stdin := strings.NewReader(input)
	var stdout bytes.Buffer
	if err := s.Serve(context.Background(), stdin, &stdout); err != nil && err != io.EOF {
		t.Fatalf("Serve: %v", err)
	}

	responses, err := decodeResponses(stdout.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d\noutput=%q", len(responses), stdout.String())
	}
}

// TestServer_NDJSON_MessageTooLarge verifies that when a client exceeds the
// maxMessageBytes cap, the server surfaces the error instead of hanging or
// crashing silently. This is the counterpart to the HTTP transport's
// 10 MiB LimitReader in http_transport.go.
func TestServer_NDJSON_MessageTooLarge(t *testing.T) {
	s := NewServer()

	// Build a line longer than maxMessageBytes. Content of the line
	// doesn't have to be valid JSON — bufio.Scanner will fail with
	// ErrTooLong before we ever try to parse.
	oversized := strings.Repeat("x", maxMessageBytes+16) + "\n"

	stdin := strings.NewReader(oversized)
	var stdout bytes.Buffer
	err := s.Serve(context.Background(), stdin, &stdout)
	if err == nil {
		t.Fatal("expected error for oversized message, got nil")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected bufio.ErrTooLong in error chain, got %v", err)
	}
}
