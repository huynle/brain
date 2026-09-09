package apiserver

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/auth"
)

func TestBuildHTTPHandler_BindAuth(t *testing.T) {
	for _, tt := range []struct {
		name, host, hash, escape, authEnv string
		enabled, allowed                  bool
	}{
		{name: "localhost", host: "localhost", allowed: true},
		{name: "IPv4 loopback", host: "127.0.0.1", allowed: true},
		{name: "loopback range", host: "127.2.3.4", allowed: true},
		{name: "IPv6 loopback", host: "::1", allowed: true},
		{name: "expanded IPv6", host: "0:0:0:0:0:0:0:1", allowed: true},
		{name: "mapped loopback", host: "::ffff:127.0.0.1", allowed: true},
		{name: "wildcard", host: "0.0.0.0"},
		{name: "password is not auth", host: "0.0.0.0", hash: "configured-hash"},
		{name: "environment is not effective auth", host: "0.0.0.0", authEnv: "true", hash: "configured-hash"},
		{name: "explicit escape", host: "0.0.0.0", escape: "true", allowed: true},
		{name: "auth on wins over env", host: "0.0.0.0", enabled: true, authEnv: "false", allowed: true},
		{name: "empty is wildcard"},
		{name: "IPv6 wildcard", host: "::"},
		{name: "private address", host: "192.168.1.10"},
		{name: "IPv6 remote", host: "2001:db8::1"},
		{name: "hostname not inferred local", host: "brain.local"},
		{name: "invalid host", host: "localhost:3333"},
		{name: "escape typo", host: "0.0.0.0", escape: "ture"},
		{name: "escape false", host: "0.0.0.0", escape: "false"},
		{name: "escape numeric", host: "0.0.0.0", escape: "1"},
		{name: "escape uppercase", host: "0.0.0.0", escape: "TRUE"},
		{name: "escape whitespace", host: "0.0.0.0", escape: " true "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(auth.EnvPasswordHash, tt.hash)
			t.Setenv("ENABLE_AUTH", tt.authEnv)
			t.Setenv("BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND", tt.escape)
			dir := filepath.Join(t.TempDir(), "untouched")
			ctx, cancel := context.WithCancel(context.Background())
			h, dbPath, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: tt.host, BrainDir: dir, EnableAuth: tt.enabled})
			defer func() {
				cancel()
				if cleanup != nil {
					cleanup()
				}
			}()
			if !tt.allowed {
				assertBindAuthDenied(t, err, dir)
				if h != nil || dbPath != "" || cleanup != nil {
					t.Error("denied construction returned initialized resources")
				}
				return
			}
			if err != nil {
				t.Fatalf("allowed construction: %v", err)
			}
			// Verify the effective middleware decision, not merely construction.
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/entries", nil))
			want := http.StatusOK
			if tt.enabled {
				want = http.StatusUnauthorized
			}
			if r.Code != want {
				t.Errorf("request without credentials = %d, want %d: %s", r.Code, want, r.Body.String())
			}
		})
	}
}

func assertBindAuthDenied(t *testing.T, err error, dir string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "ENABLE_AUTH=true") || !strings.Contains(err.Error(), "BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND=true") {
		t.Errorf("want actionable bind/auth refusal, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("preflight touched storage directory: stat = %v", err)
	}
}

func TestRunServer_BindAuthDeniedBeforeStorageAndListen(t *testing.T) {
	oldLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	for _, hash := range []string{"", "configured-hash"} {
		t.Run("hash="+hash, func(t *testing.T) {
			t.Setenv(auth.EnvPasswordHash, hash)
			t.Setenv("ENABLE_AUTH", "true")
			t.Setenv("BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND", "")
			dir := filepath.Join(t.TempDir(), "untouched")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Invalid port keeps the RED test from ever exposing a listener.
			err := RunServer(ctx, ServerOptions{Host: "0.0.0.0", Port: -1, BrainDir: dir, LogLevel: "error"})
			assertBindAuthDenied(t, err, dir)
		})
	}
}

func TestBuildHTTPHandler_InsecureBindWarning(t *testing.T) {
	t.Setenv("BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND", "true")
	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	// Deliberately stop at filesystem initialization: no background log writers.
	dir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := buildHTTPHandler(context.Background(), ServerOptions{Host: "0.0.0.0", BrainDir: dir})
	if err == nil {
		t.Fatal("expected fixture storage error")
	}
	for _, text := range []string{"level=WARN", "unauthenticated", "0.0.0.0", "BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND"} {
		if !strings.Contains(logs.String(), text) {
			t.Errorf("warning missing %q: %s", text, logs.String())
		}
	}
}

func TestRunServer_BindAuthIPv6Loopback(t *testing.T) {
	t.Setenv("BRAIN_INSECURE_ALLOW_UNAUTHENTICATED_BIND", "")
	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		done <- RunServer(ctx, ServerOptions{Host: "::1", Port: port, BrainDir: dir, LogLevel: "error"})
	}()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("IPv6 server exited before readiness: %v", err)
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("IPv6 server did not become ready")
		case <-tick.C:
			resp, err := client.Get("http://" + address + "/api/v1/health")
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				continue
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("IPv6 loopback shutdown: %v", err)
			}
			return
		}
	}
}
