package runner

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGenerateOpenCodePassword(t *testing.T) {
	p1 := generateOpenCodePassword()
	p2 := generateOpenCodePassword()

	if p1 == "" {
		t.Fatal("password is empty")
	}
	if len(p1) < 32 {
		t.Fatalf("password too short (%d chars); want >=32 for strength", len(p1))
	}
	if p1 == p2 {
		t.Fatalf("two generated passwords collided: %q", p1)
	}
	for _, r := range p1 {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("password contains non-hex rune %q in %q", r, p1)
		}
	}
}

func TestLocalOpenCodeClientAuthHeader(t *testing.T) {
	c := newLocalOpenCodeClient(8891, "secretpw")
	got := c.authHeader()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:secretpw"))
	if got != want {
		t.Fatalf("authHeader() = %q, want %q", got, want)
	}
}

func TestLocalOpenCodeClientURL(t *testing.T) {
	c := newLocalOpenCodeClient(1234, "pw")
	tests := []struct {
		path string
		want string
	}{
		{"/session", "http://127.0.0.1:1234/api/session"},
		{"/session/ses_x/message", "http://127.0.0.1:1234/api/session/ses_x/message"},
		{"/session/active", "http://127.0.0.1:1234/api/session/active"},
		{"/info", "http://127.0.0.1:1234/api/info"},
		{"/event", "http://127.0.0.1:1234/api/event"},
		{"session", "http://127.0.0.1:1234/api/session"},
	}
	for _, tt := range tests {
		if got := c.url(tt.path); got != tt.want {
			t.Errorf("url(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestLocalOpenCodeClientNewRequest(t *testing.T) {
	c := newLocalOpenCodeClient(9000, "pw2")
	req, err := c.newRequest(context.Background(), http.MethodPost, "/session/ses_a/prompt", strings.NewReader(`{"text":""}`))
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if req.URL.String() != "http://127.0.0.1:9000/api/session/ses_a/prompt" {
		t.Fatalf("url = %q", req.URL.String())
	}
	if got := req.Header.Get("Authorization"); got != c.authHeader() {
		t.Fatalf("Authorization = %q, want %q", got, c.authHeader())
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"text":""}` {
		t.Fatalf("body = %q", string(body))
	}
}

func TestLocalOpenCodeClientNewRequestEmptyPassword(t *testing.T) {
	c := newLocalOpenCodeClient(9000, "")
	req, err := c.newRequest(context.Background(), http.MethodGet, "/info", nil)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty for empty password", got)
	}
}

func TestInjectOpenCodePassword(t *testing.T) {
	env := map[string]string{"PATH": "/usr/bin"}
	injectOpenCodePassword(env, "hunter2")
	if env["OPENCODE_PASSWORD"] != "hunter2" {
		t.Fatalf("OPENCODE_PASSWORD = %q, want hunter2", env["OPENCODE_PASSWORD"])
	}
	env2 := map[string]string{}
	injectOpenCodePassword(env2, "")
	if _, ok := env2["OPENCODE_PASSWORD"]; ok {
		t.Fatalf("OPENCODE_PASSWORD set for empty password")
	}
}
