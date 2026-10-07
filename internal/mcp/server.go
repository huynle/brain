// Package mcp implements the Brain Model Context Protocol (MCP) tool server.
//
// Protocol: JSON-RPC 2.0, served over the Streamable HTTP transport at /mcp
// by the Brain API (see http_transport.go). There is no stdio transport.
//
// Version: MCP 2024-11-05
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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

// Server is an MCP protocol server that dispatches JSON-RPC 2.0 requests to
// registered tools.
type Server struct {
	mu    sync.RWMutex
	tools map[string]registeredTool

	// caller is what a hosted client declared about itself in request
	// headers (see ParseCallerHeaders). Nil for headerless calls.
	caller *CallerContext
}

// NewServer creates a new MCP server.
func NewServer() *Server {
	return &Server{tools: make(map[string]registeredTool)}
}

// rejectLocalPathArg refuses a tool argument naming a path on the caller's
// machine. Tools run inside the Brain API, so the path would resolve on the
// API host's filesystem — failing, or silently touching a different file that
// happens to exist there. alternative names what the caller should do instead.
func rejectLocalPathArg(args map[string]any, arg, alternative string) error {
	if _, ok := args[arg]; !ok {
		return nil
	}
	return fmt.Errorf("%q is not supported: this MCP server runs inside the Brain API and cannot read or write files on your machine — %s", arg, alternative)
}

// ambientContextDescribesCaller reports whether executionContext() describes
// the client that made this call, rather than the process serving it.
//
// The fallback GetCachedContext is computed from the Brain API server's own
// working directory, shared by every client on it — so stamping origin
// provenance from it would brand every task with the API host's identity. A
// call describes its caller only through the caller headers.
func (s *Server) ambientContextDescribesCaller() bool {
	return s != nil && s.caller != nil
}

// executionContext is the context tool calls default from: the hosted
// caller's declared context when it sent one, else the process's own.
func (s *Server) executionContext() ExecutionContext {
	if s != nil && s.caller != nil {
		return s.caller.ExecutionContext()
	}
	return GetCachedContext()
}

// applyCallerProject defaults the project argument from the caller's working
// folder when the tool takes one and the caller did not name a project.
func (s *Server) applyCallerProject(tool Tool, args map[string]any) {
	if s.caller == nil || StringArgAlias(args, "", "project", "project_id", "projectId") != "" {
		return
	}
	project := s.caller.ExecutionContext().ProjectID
	if project == "" {
		return
	}
	for _, key := range []string{"project", "project_id"} {
		if _, ok := tool.InputSchema.Properties[key]; ok {
			args[key] = project
			return
		}
	}
}

// RegisterTool registers a tool with its handler.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = registeredTool{tool: tool, handler: handler}
}

// RegisteredTool is the exported view of a registered tool: its definition
// plus the handler that executes it. It lets callers outside this package
// (e.g. the in-process assistant) enumerate and adapt the full MCP tool set
// without reimplementing every tool.
type RegisteredTool struct {
	Tool    Tool
	Handler ToolHandler
}

// RegisteredTools returns a snapshot of every tool registered on this server.
// Order is unspecified (the backing map has no order); callers that need a
// stable ordering should sort by Tool.Name.
func (s *Server) RegisteredTools() []RegisteredTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RegisteredTool, 0, len(s.tools))
	for _, rt := range s.tools {
		out = append(out, RegisteredTool{Tool: rt.tool, Handler: rt.handler})
	}
	return out
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
	s.applyCallerProject(rt.tool, args)

	text, err := rt.handler(withCaller(ctx, s.caller), args)
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
