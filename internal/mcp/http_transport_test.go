package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHandlerPostInitializeCreatesSession(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	rec := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Mcp-Session-Id"); len(got) != 32 {
		t.Fatalf("Mcp-Session-Id length = %d, want 32; value = %q", len(got), got)
	}
}

func TestHTTPHandlerPostEchoesProvidedSession(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	rec := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "client-session")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Mcp-Session-Id"); got != "client-session" {
		t.Fatalf("Mcp-Session-Id = %q, want client-session", got)
	}
}

func TestHTTPHandlerPostNotificationReturnsAccepted(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	rec := postMCP(t, handler, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

func TestHTTPHandlerPostInvalidJSONReturnsJSONRPCParseError(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	rec := postMCP(t, handler, `{`, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("error = %#v, want parse error -32700", resp.Error)
	}
}

func TestHTTPHandlerPostEmptyBodyReturnsBadRequest(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHTTPHandlerDeleteTerminatesExistingSession(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	initRec := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "")
	sessionID := initRec.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize did not return Mcp-Session-Id")
	}

	deleteRec := deleteMCP(handler, sessionID)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body = %s", deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}
	if deleteRec.Body.Len() != 0 {
		t.Fatalf("delete body = %q, want empty", deleteRec.Body.String())
	}

	repeatRec := deleteMCP(handler, sessionID)
	if repeatRec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want %d; body = %s", repeatRec.Code, http.StatusNotFound, repeatRec.Body.String())
	}
}

func TestHTTPHandlerDeleteRequiresSessionHeader(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))
	rec := deleteMCP(handler, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHTTPHandlerUnsupportedMethodsIncludeAllowHeader(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/mcp", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != "GET, POST, DELETE" {
				t.Fatalf("Allow = %q, want GET, POST, DELETE", got)
			}
		})
	}
}

func postMCP(t *testing.T, handler http.Handler, body, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func deleteMCP(handler http.Handler, sessionID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHTTPHandlerToolsListIncludesProjectTools(t *testing.T) {
	handler := NewHTTPHandler(NewAPIClient("http://127.0.0.1"))

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	for _, want := range []string{"context_resolve", "project_placement_get", "project_placement_put"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("tools/list response missing %q: %s", want, rec.Body.String())
		}
	}
}
