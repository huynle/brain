// Package mcp provides HTTP transport for the MCP server.
//
// This implements the Streamable HTTP transport from the MCP specification
// (2025-03-26). It wraps the existing JSON-RPC server with an HTTP handler
// that accepts POST requests at /mcp and returns JSON responses.
//
// The transport supports:
//   - POST /mcp: JSON-RPC request → JSON response (application/json)
//   - GET /mcp: Returns 405 (server-initiated SSE not supported)
//   - DELETE /mcp: Terminates an existing session
//   - Mcp-Session-Id header for session tracking
package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
)

// HTTPHandler serves the MCP Streamable HTTP transport.
// It wraps an MCP Server and exposes it over HTTP at a single endpoint.
type HTTPHandler struct {
	server    *Server
	apiClient *APIClient // base API client (unauthenticated)

	// serverFactory creates a fresh MCP server with tools registered using
	// an authenticated API client. Called per-request to pass through auth.
	serverFactory func(client *APIClient) *Server

	mu       sync.RWMutex
	sessions map[string]bool // active session IDs
}

// NewHTTPHandler creates an HTTP handler that serves MCP over Streamable HTTP transport.
// The apiClient is the base client pointing at the brain API. Per-request, the caller's
// auth token is forwarded to create an authenticated client for tool execution.
func NewHTTPHandler(apiClient *APIClient) *HTTPHandler {
	return &HTTPHandler{
		apiClient: apiClient,
		serverFactory: func(client *APIClient) *Server {
			s := NewServer()
			RegisterBrainTools(s, client)
			RegisterTaskTools(s, client)
			RegisterFeatureTools(s, client)
			RegisterRunnerTools(s, client)
			RegisterObservabilityTools(s, client)
			RegisterProjectTools(s, client)
			RegisterControlTools(s, client)
			RegisterPlanningTools(s, client)
			RegisterWebhookTools(s, client)
			RegisterGoalTools(s, client)
			return s
		},
		sessions: make(map[string]bool),
	}
}

// ServeHTTP handles all HTTP methods for the MCP endpoint.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "SSE streaming not supported", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost processes a JSON-RPC request over HTTP.
func (h *HTTPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	// Read the request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		slog.Error("mcp http: failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	// Parse the JSON-RPC request
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		slog.Debug("mcp http: failed to parse JSON-RPC request", "error", err, "body", string(body))
		writeJSONRPCError(w, nil, -32700, "Parse error")
		return
	}

	// Handle notifications (no response expected)
	if req.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Create an authenticated MCP server for this request.
	// Extract the Bearer token from the request and forward it to the API client
	// so tool calls authenticate against the brain API.
	client := h.apiClient
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if token := extractBearer(authHeader); token != "" {
			client = h.apiClient.WithAuthToken(token)
		}
	}
	server := h.serverFactory(client)

	// Handle the request using the MCP server
	resp := server.HandleRequest(r.Context(), &req)

	// Session management: if this is an initialize response, create a session
	if req.Method == "initialize" && resp.Error == nil {
		sessionID := generateSessionID()
		h.mu.Lock()
		h.sessions[sessionID] = true
		h.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", sessionID)
		slog.Info("mcp http: new session created", "session_id", sessionID)
	}

	// If client sent a session ID, echo it back
	if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
		w.Header().Set("Mcp-Session-Id", sid)
	}

	// Write the JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("mcp http: failed to write response", "error", err)
	}
}

// handleDelete terminates an MCP HTTP session.
func (h *HTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Missing Mcp-Session-Id", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	_, ok := h.sessions[sessionID]
	if ok {
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()

	if !ok {
		http.Error(w, "Unknown Mcp-Session-Id", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeJSONRPCError writes a JSON-RPC error response.
func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// extractBearer extracts the token from a "Bearer <token>" Authorization header.
func extractBearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		return header[len(prefix):]
	}
	const prefixLower = "bearer "
	if len(header) > len(prefixLower) && header[:len(prefixLower)] == prefixLower {
		return header[len(prefixLower):]
	}
	return ""
}

// generateSessionID creates a cryptographically random session ID.
func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
