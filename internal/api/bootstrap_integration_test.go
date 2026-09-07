package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/storage"
)

func bootstrapStore(t *testing.T) *storage.StorageLayer {
	t.Helper()
	s, err := storage.New(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBootstrapHTTP_InstallAndPeerPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, peer, hatch, password, credential string
		want                                    int
	}{
		{name: "unclaimed IPv4", peer: "127.0.0.1:1234", want: 201},
		{name: "unclaimed IPv6", peer: "[::1]:1234", want: 201},
		{name: "mapped loopback", peer: "[::ffff:127.0.0.1]:1234", want: 201},
		{name: "remote spoofed headers", peer: "192.0.2.1:1234", want: 403},
		{name: "private peer", peer: "10.0.0.1:1234", want: 403},
		{name: "malformed", peer: "not-an-address", want: 403},
		{name: "bare IP", peer: "127.0.0.1", want: 403},
		{name: "hostname", peer: "localhost:1234", want: 403},
		{name: "empty", want: 403},
		{name: "hatch", peer: "192.0.2.1:1234", hatch: "true", want: 201},
		{name: "hatch not explicit", peer: "192.0.2.1:1234", hatch: "1", want: 403},
		{name: "legacy OAuth only", peer: "127.0.0.1:1234", credential: "oauth", want: 403},
		{name: "password", peer: "127.0.0.1:1234", password: "configured-hash", want: 403},
		{name: "whitespace password unconfigured", peer: "127.0.0.1:1234", password: "   ", want: 201},
		{name: "hatch cannot bypass password", peer: "192.0.2.1:1234", hatch: "true", password: "configured-hash", want: 403},
		{name: "hatch cannot bypass OAuth", peer: "192.0.2.1:1234", hatch: "true", credential: "oauth", want: 403},
		{name: "revoked credential stays claimed", peer: "127.0.0.1:1234", credential: "revoked", want: 403},
		{name: "hatch cannot bypass claim", peer: "192.0.2.1:1234", hatch: "true", credential: "deleted", want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BRAIN_ALLOW_REMOTE_BOOTSTRAP", tc.hatch)
			t.Setenv(auth.EnvPasswordHash, tc.password)
			s := bootstrapStore(t)
			ctx := context.Background()
			switch tc.credential {
			case "oauth":
				// Bypass modern credential writes to represent a legacy OAuth-only install.
				if _, err := s.DB().Exec("INSERT INTO oauth_access_tokens(token, client_id, expires_at, created_at) VALUES ('legacy', 'client', ?, 1)", time.Now().Add(time.Hour).Unix()); err != nil {
					t.Fatal(err)
				}
			case "revoked", "deleted":
				if err := s.CreateToken(ctx, "old", "old-secret", "admin:*"); err != nil {
					t.Fatal(err)
				}
				if tc.credential == "revoked" {
					if err := s.RevokeToken(ctx, "old"); err != nil {
						t.Fatal(err)
					}
				} else if err := s.DeleteTokenPermanent(ctx, "old"); err != nil {
					t.Fatal(err)
				}
			}
			h := NewHandler(&mockBrainService{}, WithTokenService(singleModeTokens(t, s)), WithCredentialVerifier(auth.NewVerifierFromEnv()))
			r := httptest.NewRequest(http.MethodPost, "/api/v1/tokens/bootstrap", strings.NewReader(`{"name":"first","scope":"read:*"}`))
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("X-Real-IP", "127.0.0.1")
			r.Header.Set("Forwarded", "for=127.0.0.1")
			w := httptest.NewRecorder()
			h.HandleBootstrapToken(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			got, err := s.GetTokenByName(ctx, "first")
			if tc.want == 201 {
				var body createTokenResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if err != nil || got.Scope != "admin:*" || got.Token != body.Token || body.Token == "" || body.CreatedAt == "" {
					t.Fatalf("invalid bootstrap token: %+v, %v", body, err)
				}
			} else if err == nil {
				t.Fatal("refused bootstrap wrote a token")
			}
		})
	}
}

func TestBootstrapHTTP_ConcurrentSingleWinner(t *testing.T) {
	t.Setenv("BRAIN_ALLOW_REMOTE_BOOTSTRAP", "")
	s := bootstrapStore(t)
	h := NewHandler(&mockBrainService{}, WithTokenService(singleModeTokens(t, s)))
	const requests = 24
	// Hold all real HTTP handlers at a barrier so requests are in flight together.
	arrived := make(chan struct{}, requests)
	start := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-start:
		case <-r.Context().Done():
			return
		}
		h.HandleBootstrapToken(w, r)
	}))
	defer srv.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	results := make(chan int, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Post(srv.URL, "application/json", strings.NewReader(fmt.Sprintf(`{"name":"candidate-%d"}`, i)))
			if err != nil {
				t.Error(err)
				results <- 0
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			results <- resp.StatusCode
		}(i)
	}
	for i := 0; i < requests; i++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			close(start)
			t.Fatal("HTTP requests did not reach barrier")
		}
	}
	close(start)
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[201] != 1 || counts[403] != requests-1 {
		t.Fatalf("HTTP statuses = %v, want one 201 and %d 403", counts, requests-1)
	}
	tokens, err := s.ListTokens(context.Background(), true)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("stored tokens = %d, err = %v", len(tokens), err)
	}
}
