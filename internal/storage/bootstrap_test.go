package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

const claimPath = "brain:system/install_claimed"

func claimContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func requireClosed(t *testing.T, err error) {
	t.Helper()
	var closed *BootstrapClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("want BootstrapClosedError, got %v", err)
	}
}

func requireClaim(t *testing.T, s *StorageLayer, want int) {
	t.Helper()
	var got int
	if err := s.db.QueryRowContext(claimContext(t), "SELECT COUNT(*) FROM entry_meta WHERE path = ?", claimPath).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("claim marker count = %d, want %d", got, want)
	}
}

func claimExec(t *testing.T, s *StorageLayer, query string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(claimContext(t), query, args...); err != nil {
		t.Fatal(err)
	}
}

func openClaimStore(t *testing.T, path string) *StorageLayer {
	t.Helper()
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBootstrapToken_TwoHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claim.db")
	a, b := openClaimStore(t, path), openClaimStore(t, path)
	ctx := claimContext(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, s := range []*StorageLayer{a, b} {
		go func(i int, s *StorageLayer) {
			<-start
			results <- s.BootstrapToken(ctx, fmt.Sprintf("admin-%d", i), fmt.Sprintf("secret-%d", i), false)
		}(i, s)
	}
	close(start)
	wins := 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else {
			requireClosed(t, err)
		}
	}
	if wins != 1 {
		t.Fatalf("bootstrap successes = %d, want exactly one", wins)
	}
	tokens, err := a.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].Scope != "admin:*" {
		t.Fatalf("tokens = %+v", tokens)
	}
	requireClaim(t, b, 1)
	if err := a.DeleteTokenPermanent(ctx, tokens[0].Name); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	c := openClaimStore(t, path)
	requireClosed(t, c.BootstrapToken(ctx, "again", "again", false))
}

func TestBootstrapToken_LegacyCredentials(t *testing.T) {
	for _, kind := range []string{"api", "oauth", "expired-oauth", "revoked-api", "password"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := claimContext(t)
			switch kind {
			case "api":
				claimExec(t, s, "INSERT INTO api_tokens(name, token) VALUES ('legacy', 'legacy')")
			case "revoked-api":
				claimExec(t, s, "INSERT INTO api_tokens(name, token, revoked_at) VALUES ('legacy', 'legacy', 'revoked')")
			case "oauth", "expired-oauth":
				expiry := time.Now().Add(time.Hour).Unix()
				if kind == "expired-oauth" {
					expiry = time.Now().Unix() - 1
				}
				claimExec(t, s, "INSERT INTO oauth_access_tokens(token, client_id, expires_at, created_at) VALUES ('legacy', 'client', ?, 1)", expiry)
			}
			err := s.BootstrapToken(ctx, "admin", "secret", kind == "password")
			if kind == "expired-oauth" || kind == "revoked-api" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireClosed(t, err)
				var count int
				if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_tokens WHERE name = 'admin'").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("refused bootstrap inserted token")
				}
			}
			requireClaim(t, s, 1)
			claimExec(t, s, "DELETE FROM api_tokens")
			claimExec(t, s, "DELETE FROM oauth_access_tokens")
			requireClosed(t, s.BootstrapToken(ctx, "later", "later", false))
		})
	}
}

func TestBootstrapToken_InsertFailureRollsBack(t *testing.T) {
	s := newTestStorage(t)
	ctx := claimContext(t)
	claimExec(t, s, "INSERT INTO api_tokens(name, token, revoked_at) VALUES ('used', 'old', 'revoked')")
	err := s.BootstrapToken(ctx, "used", "new", false)
	var closed *BootstrapClosedError
	if err == nil || errors.As(err, &closed) {
		t.Fatalf("want insertion error, got %v", err)
	}
	requireClaim(t, s, 0)
	if err := s.BootstrapToken(ctx, "fresh", "fresh", false); err != nil {
		t.Fatal(err)
	}
}

func TestInstallClaim_CredentialWrites(t *testing.T) {
	for _, kind := range []string{"api-revoke", "api-delete", "oauth-revoke", "oauth-client-revoke", "oauth-expiry", "expired-oauth"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := claimContext(t)
			if kind == "api-revoke" || kind == "api-delete" {
				if err := s.CreateToken(ctx, "existing", "secret", "read:*"); err != nil {
					t.Fatal(err)
				}
			} else {
				expiry := time.Now().Add(time.Hour).Unix()
				if kind == "expired-oauth" {
					expiry = time.Now().Unix() - 1
				}
				if err := s.CreateAccessToken(ctx, &OAuthAccessToken{Token: "secret", ClientID: "client", ExpiresAt: expiry}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "expired-oauth" {
				requireClaim(t, s, 0)
				if err := s.BootstrapToken(ctx, "admin", "admin", false); err != nil {
					t.Fatal(err)
				}
				return
			}
			requireClaim(t, s, 1)
			var err error
			switch kind {
			case "api-revoke":
				err = s.RevokeToken(ctx, "existing")
			case "api-delete":
				err = s.DeleteTokenPermanent(ctx, "existing")
			case "oauth-revoke":
				err = s.RevokeAccessToken(ctx, "secret")
			case "oauth-client-revoke":
				err = s.RevokeAccessTokensByClient(ctx, "client")
			case "oauth-expiry":
				claimExec(t, s, "UPDATE oauth_access_tokens SET expires_at = 1")
				err = s.CleanupExpiredAccessTokens(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			requireClosed(t, s.BootstrapToken(ctx, "admin", "admin", false))
		})
	}
}

func TestInstallClaim_StartupBackfill(t *testing.T) {
	for _, kind := range []string{"api", "oauth", "expired-oauth", "revoked-api"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			s := openClaimStore(t, path)
			if kind == "api" || kind == "revoked-api" {
				claimExec(t, s, "INSERT INTO api_tokens(name, token) VALUES ('legacy', 'legacy')")
				if kind == "revoked-api" {
					claimExec(t, s, "UPDATE api_tokens SET revoked_at = 'revoked'")
				}
			} else {
				expiry := time.Now().Add(time.Hour).Unix()
				if kind == "expired-oauth" {
					expiry = 1
				}
				claimExec(t, s, "INSERT INTO oauth_access_tokens(token, client_id, expires_at, created_at) VALUES ('legacy', 'client', ?, 1)", expiry)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openClaimStore(t, path)
			want := 1
			if kind == "expired-oauth" || kind == "revoked-api" {
				want = 0
			}
			requireClaim(t, s, want)
			claimExec(t, s, "DELETE FROM api_tokens")
			claimExec(t, s, "DELETE FROM oauth_access_tokens")
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openClaimStore(t, path)
			err := s.BootstrapToken(claimContext(t), "admin", "admin", false)
			if want == 1 {
				requireClosed(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInstallClaim_MarkPresenceAndStats(t *testing.T) {
	s := newTestStorage(t)
	ctx := claimContext(t)
	if err := s.MarkInstallClaimed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkInstallClaimed(ctx); err != nil {
		t.Fatal(err)
	}
	requireClaim(t, s, 1)
	claimExec(t, s, "UPDATE entry_meta SET access_count = 0, last_accessed = NULL, last_verified = NULL WHERE path = ?", claimPath)
	requireClosed(t, s.BootstrapToken(ctx, "admin", "secret", false))
	if err := s.RecordAccess(ctx, "projects/test/note/example.md"); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []*StatsOptions{nil, {Path: "projects/test/"}, {Paths: []string{"projects/test/", "brain:"}}} {
		stats, err := s.GetStats(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		if stats.TrackedCount != 1 || stats.TotalNotes != 0 || stats.StaleCount != 0 || stats.OrphanCount != 0 {
			t.Fatalf("stats = %+v", stats)
		}
	}
}

func TestInstallClaim_CredentialWriteAtomicity(t *testing.T) {
	for _, kind := range []string{"api", "oauth"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := claimContext(t)
			claimExec(t, s, "CREATE TRIGGER reject_claim BEFORE INSERT ON entry_meta BEGIN SELECT RAISE(ABORT, 'reject claim'); END")
			var err error
			table := "api_tokens"
			if kind == "api" {
				err = s.CreateToken(ctx, "admin", "secret", "")
			} else {
				table = "oauth_access_tokens"
				err = s.CreateAccessToken(ctx, &OAuthAccessToken{Token: "secret", ClientID: "client"})
			}
			if err == nil {
				t.Fatal("credential write succeeded despite marker failure")
			}
			var count int
			if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("credential persisted without claim")
			}
		})
	}
}

// Use a fixed second to verify the shared bootstrap/backfill predicate without
// a wall-clock rollover making an equality-boundary assertion flaky.
func TestInstallClaim_OAuthInclusiveBoundary(t *testing.T) {
	s := newTestStorage(t)
	const now int64 = 1234567890
	claimExec(t, s, "INSERT INTO oauth_access_tokens(token, client_id, expires_at, created_at) VALUES ('boundary', 'client', ?, 1)", now)
	for _, tc := range []struct {
		now  int64
		want bool
	}{{now - 1, true}, {now, true}, {now + 1, false}} {
		var active bool
		if err := s.db.QueryRowContext(claimContext(t), "SELECT "+activeInstallCredentials, tc.now).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active != tc.want {
			t.Errorf("at %d: active = %v, want %v", tc.now, active, tc.want)
		}
	}
}

func TestInstallClaim_CredentialInsertFailureRollsBack(t *testing.T) {
	for _, kind := range []string{"api", "oauth"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := claimContext(t)
			table := "api_tokens"
			create := func() error { return s.CreateToken(ctx, "admin", "secret", "") }
			if kind == "oauth" {
				table = "oauth_access_tokens"
				create = func() error { return s.CreateAccessToken(ctx, &OAuthAccessToken{Token: "secret", ClientID: "client"}) }
			}
			claimExec(t, s, "CREATE TRIGGER reject_credential BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT, 'reject credential'); END")
			if err := create(); err == nil {
				t.Fatal("expected credential insertion failure")
			}
			requireClaim(t, s, 0)
			claimExec(t, s, "DROP TRIGGER reject_credential")
			if err := s.BootstrapToken(ctx, "fresh", "fresh", false); err != nil {
				t.Fatal(err)
			}
		})
	}
}
