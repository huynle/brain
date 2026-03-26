// Package mcp implements a Model Context Protocol (MCP) server
// for exposing Brain API tools to Claude Code and other MCP clients.
//
// Protocol: JSON-RPC 2.0 over stdin/stdout with Content-Length framing.
// Version: MCP 2024-11-05
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

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
}

// NewServer creates a new MCP server.
func NewServer() *Server {
	return &Server{
		tools: make(map[string]registeredTool),
	}
}

// RegisterTool registers a tool with its handler.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = registeredTool{tool: tool, handler: handler}
}

// jsonrpcRequest represents an incoming JSON-RPC 2.0 request or notification.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcResponse represents an outgoing JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError represents a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolCallParams represents the params for a tools/call request.
type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Serve reads JSON-RPC requests from r and writes responses to w.
// It blocks until r is closed (returns io.EOF) or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	reader := bufio.NewReader(r)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read Content-Length header
		contentLength, err := readContentLength(reader)
		if err != nil {
			if err == io.EOF {
				return err
			}
			// Skip malformed headers
			continue
		}

		// Read the body
		body := make([]byte, contentLength)
		_, err = io.ReadFull(reader, body)
		if err != nil {
			return fmt.Errorf("read body: %w", err)
		}

		// Parse the request
		var req jsonrpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			continue // Skip malformed JSON
		}

		// Notifications have no ID — don't send a response
		if req.ID == nil || string(req.ID) == "null" {
			continue
		}

		// Handle the request
		resp := s.handleRequest(ctx, &req)

		// Write the response
		if err := writeMessage(w, resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
}

// readContentLength reads the Content-Length header and the blank line separator.
func readContentLength(r *bufio.Reader) (int, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue // Skip empty lines between messages
		}

		if strings.HasPrefix(line, "Content-Length:") {
			lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			length, err := strconv.Atoi(lengthStr)
			if err != nil {
				return 0, fmt.Errorf("invalid Content-Length: %w", err)
			}

			// Read the blank line after the header
			separator, err := r.ReadString('\n')
			if err != nil {
				return 0, err
			}
			_ = separator // Should be "\r\n"

			return length, nil
		}
	}
}

// writeMessage writes a Content-Length framed JSON message.
func writeMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// handleRequest dispatches a JSON-RPC request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req *jsonrpcRequest) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return &jsonrpcResponse{
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
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpcError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// handleToolsList returns the list of registered tools.
func (s *Server) handleToolsList(req *jsonrpcRequest) *jsonrpcResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, rt := range s.tools {
		tools = append(tools, rt.tool)
	}

	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

// handleToolsCall dispatches a tool call to the registered handler.
func (s *Server) handleToolsCall(ctx context.Context, req *jsonrpcRequest) *jsonrpcResponse {
	var params toolCallParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonrpcError{
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
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpcError{
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
	if err != nil {
		// Match TypeScript behavior: tool errors are returned as text content
		text = fmt.Sprintf("Error: %v", err)
	}

	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": text},
			},
		},
	}
}
