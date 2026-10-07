package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestStdioUsesPublicSDKTransport(t *testing.T) {
	t.Setenv("BRAIN_API_TOKEN", "stdio-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stdio-secret" {
			t.Error("missing configured stdio bearer")
		}
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":"Bad Request","message":"legacy validation message"}`)
	}))
	defer srv.Close()
	c, err := NewStdioSDKClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.httpClient.Transport.(*brain.HTTPTransport); !ok {
		t.Errorf("stdio uses %T, not public transport", c.httpClient.Transport)
	}
	if err := c.Request(context.Background(), "GET", "/health", nil, nil, nil); err == nil || err.Error() != "legacy validation message" {
		t.Errorf("legacy error changed: %v", err)
	}
	if NewAPIClient(srv.URL).httpClient.Transport != nil {
		t.Fatal("hosted default changed")
	}
	if c.WithAuthToken("replacement").authToken != "replacement" {
		t.Fatal("copy binding changed")
	}
	if _, err := NewStdioSDKClient("https://user:secret@example.com"); err == nil {
		t.Fatal("credential URL accepted")
	}
	if _, err := NewStdioSDKClient("https://example.com?token=secret"); err == nil {
		t.Fatal("query credential URL accepted")
	}
	if strings.Contains(c.baseURL, "secret") {
		t.Fatal("token placed in URL")
	}
}

func TestStdioBindingCompatibility(t *testing.T) {
	t.Setenv("BRAIN_API_TOKEN", "ambient-token")
	authorizations := make(chan string, 6)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations <- r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	legacy := NewAPIClient(srv.URL)
	hosted := legacy.WithAuthToken("hosted-token")
	stdio, err := NewStdioSDKClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRAIN_API_TOKEN", "changed-token")
	for _, c := range []*APIClient{legacy, hosted, stdio, stdio.WithAuthToken("replacement"), stdio} {
		if err := c.Request(context.Background(), "GET", "/health", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"", "Bearer hosted-token", "Bearer ambient-token", "Bearer replacement", "Bearer ambient-token"}
	var got []string
	for range want {
		got = append(got, <-authorizations)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bindings=%q want=%q", got, want)
	}
	t.Setenv("BRAIN_API_TOKEN", "")
	anonymous, err := NewStdioSDKClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := anonymous.Request(context.Background(), "GET", "/health", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if <-authorizations != "" {
		t.Fatal("auth-disabled stdio sent credentials")
	}
	t.Setenv("BRAIN_API_TOKEN", "invalid\nheader")
	if _, err := NewStdioSDKClient(srv.URL); err == nil {
		t.Fatal("invalid bearer accepted")
	}
}

func TestStdioCancellation(t *testing.T) {
	t.Setenv("BRAIN_API_TOKEN", "")
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, err := NewStdioSDKClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Request(ctx, "GET", "/health", nil, nil, nil) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result=%v", err)
	}
}

// A bad BRAIN_API_URL fails at startup with its stable code, not "HTTP 0".
func TestStdioBadURLReportsCode(t *testing.T) {
	t.Setenv("BRAIN_API_TOKEN", "")
	_, err := NewStdioSDKClient("ftp://example.com")
	if err == nil || !strings.Contains(err.Error(), "invalid_configuration") {
		t.Fatalf("err=%v", err)
	}
}

// The ambient token is never sent over plain http to a non-loopback host.
func TestStdioRefusesTokenOverRemotePlainHTTP(t *testing.T) {
	const secret = "stdio-token-SECRET"
	refused := []string{"http://brain.example.com", "http://10.0.0.5:3333", "http://localhost.evil.com", "http://127.0.0.1.evil.com:80", "HTTP://brain.example.com"}
	for _, u := range refused {
		t.Setenv("BRAIN_API_TOKEN", secret)
		_, err := NewStdioSDKClient(u)
		if err == nil || !strings.Contains(err.Error(), "insecure_transport") || !strings.Contains(err.Error(), "BRAIN_API_TOKEN") || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err=%v", u, err)
		}
	}
	allowed := []string{"https://brain.example.com", "http://127.0.0.1:3333", "http://localhost:3333", "http://[::1]:3333", "http://127.0.0.2"}
	for _, u := range allowed {
		t.Setenv("BRAIN_API_TOKEN", secret)
		if _, err := NewStdioSDKClient(u); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	// No token: nothing secret to protect, plain http to a remote host is unchanged.
	t.Setenv("BRAIN_API_TOKEN", "")
	if _, err := NewStdioSDKClient("http://brain.example.com"); err != nil {
		t.Errorf("token-less remote http refused: %v", err)
	}
}
