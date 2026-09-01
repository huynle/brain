// Package mcp implements a Model Context Protocol (MCP) server
// for exposing Brain API tools to Claude Code and other MCP clients.
//
// Protocol: JSON-RPC 2.0 over stdin/stdout with newline-delimited JSON (NDJSON)
// framing, per the MCP stdio transport specification:
// https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#stdio
//
// Each message is a single JSON object serialized on one line, terminated
// by a '\n'. Messages MUST NOT contain embedded (literal) newlines; any
// newlines inside JSON string values must be encoded as "\n".
//
// Version: MCP 2024-11-05
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// maxMessageBytes is the largest single JSON-RPC message the stdio transport
// will accept. the save tool with a large content payload can be several MB, so
// we allow up to 10 MiB. This matches the LimitReader cap in http_transport.go.
const maxMessageBytes = 10 * 1024 * 1024

// ToolHandler is the function signature for MCP tool implementations.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

// Property describes a single property in a JSON Schema.
type Property struct {
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	Items       *Property `json:"items,omitempty"`
}

// InputSchema describes the JSON Schema for a tool's input.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// Tool describes an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// registeredTool pairs a tool definition with its handler.
type registeredTool struct {
	tool    Tool
	handler ToolHandler
}

// Server is an MCP protocol server that handles JSON-RPC 2.0 messages
// over Content-Length framed streams.
type Server struct {
	mu    sync.RWMutex
	tools map[string]registeredTool

	// localFilesystem reports whether this server runs on the same machine as
	// the MCP client, i.e. whether a path the client names is a path this
	// process can open. Set at construction; never mutated afterwards.
	localFilesystem bool
}

// ServerOption configures optional Server behavior.
type ServerOption func(*Server)

// WithLocalFilesystem marks the server as sharing a filesystem with its client,
// which enables tool arguments that name local paths.
//
// It is off by default because the Brain API serves MCP in-process over HTTP:
// for those sessions the tool handler runs on the API host, so a path from the
// client resolves against the API host's filesystem. That fails outright, or —
// worse — silently reads or writes a different file that happens to exist at
// the same path there. Only the stdio transport, where the server is a child
// process of the client, gets this option.
func WithLocalFilesystem() ServerOption {
	return func(s *Server) { s.localFilesystem = true }
}

// NewServer creates a new MCP server.
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		tools: make(map[string]registeredTool),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// requireLocalFilesystem rejects a tool argument naming a path on the caller's
// machine when this server has no access to that machine. alternative names the
// argument the caller should reach for instead.
func (s *Server) requireLocalFilesystem(arg, alternative string) error {
	if s.localFilesystem {
		return nil
	}
	return fmt.Errorf("%q is unavailable on this MCP server: it runs inside the Brain API, so the path would resolve on the API host's filesystem instead of yours — %s", arg, alternative)
}

// ambientContextDescribesCaller reports whether GetCachedContext() describes
// the client that made this call, rather than the process serving it.
//
// GetCachedContext is a process-global computed once from os.Getwd(). Under
// stdio that is right: the server is a child of the client and inherits its
// working directory. Under the in-process HTTP transport it is the Brain API
// server's own directory and identity, shared by every client on it — so
// stamping origin provenance from it would brand every task with the API
// host's machine id and pin them all there.
//
// It shares the localFilesystem flag with requireLocalFilesystem because it
// is the same underlying fact: only the stdio transport is co-located with
// its caller.
func (s *Server) ambientContextDescribesCaller() bool {
	return s != nil && s.localFilesystem
}

// RegisterTool registers a tool with its handler.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = registeredTool{tool: tool, handler: handler}
}

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// IsNotification returns true if the request has no ID (i.e., is a JSON-RPC notification).
func (r *JSONRPCRequest) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// toolCallParams represents the params for a tools/call request.
type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Serve reads JSON-RPC requests from r and writes responses to w.
//
// Framing is newline-delimited JSON (NDJSON): each request is one JSON
// object on a line, terminated by '\n'. Each response is written the same
// way. Individual messages may be up to maxMessageBytes.
//
// Serve blocks until r is closed (returns io.EOF), ctx is cancelled, or
// a fatal write error occurs.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Give the scanner a buffer large enough for our biggest legitimate
	// message. Starting size 64 KiB grows as needed up to maxMessageBytes.
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Reply with a JSON-RPC parse error so the client can see
			// what happened instead of hanging on a missing response.
			parseErr := &JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: fmt.Sprintf("Parse error: %v", err),
				},
			}
			if werr := writeNDJSON(w, parseErr); werr != nil {
				return fmt.Errorf("write parse-error response: %w", werr)
			}
			continue
		}

		// Notifications have no ID — don't send a response.
		if req.IsNotification() {
			continue
		}

		resp := s.HandleRequest(ctx, &req)
		if err := writeNDJSON(w, resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		// bufio.ErrTooLong means the client sent a message larger than
		// maxMessageBytes. That's a client bug, not a server crash — but
		// we can't recover the stream, so return it and let the caller
		// restart if desired.
		return fmt.Errorf("stdio read: %w", err)
	}
	return io.EOF
}

// writeNDJSON writes a single JSON-RPC message followed by '\n'.
//
// Per the MCP stdio spec, the JSON encoding of the message must not contain
// embedded (literal) newlines. json.Marshal already escapes any \n inside
// string values as "\n", so a single json.Marshal output is guaranteed to
// occupy exactly one line.
func writeNDJSON(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

// handleRequest dispatches a JSON-RPC request to the appropriate handler.
// HandleRequest dispatches a JSON-RPC request to the appropriate method handler.
// Exported for use by HTTP transport.
func (s *Server) HandleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "brain-mcp",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		return s.handleToolsList(req)

	case "tools/call":
		return s.handleToolsCall(ctx, req)

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// handleToolsList returns the list of registered tools.
func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, rt := range s.tools {
		tools = append(tools, rt.tool)
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

// handleToolsCall dispatches a tool call to the registered handler.
func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params toolCallParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    -32602,
					Message: fmt.Sprintf("Invalid params: %v", err),
				},
			}
		}
	}

	s.mu.RLock()
	rt, ok := s.tools[params.Name]
	s.mu.RUnlock()

	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: fmt.Sprintf("Unknown tool: %s", params.Name),
			},
		}
	}

	args := params.Arguments
	if args == nil {
		args = make(map[string]any)
	}

	text, err := rt.handler(ctx, args)
	result := map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
	if err != nil {
		// Per the MCP spec, tool execution failures are reported as a normal
		// result with isError: true so the model can see the message and react.
		result["content"] = []map[string]string{
			{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
		}
		result["isError"] = true
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}
