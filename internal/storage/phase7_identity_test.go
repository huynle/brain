package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/tenant"
)

// Only these identity tests stage v29. Public open/initialization stays at v28.
func phase7Store(t *testing.T, version int) *StorageLayer {
	t.Helper()
	s := &StorageLayer{db: archivedSchemaFixture(t, provenanceSources[0].revision)}
	s.db.SetMaxOpenConns(1)
	if version == 29 {
		relationalExec(t, s.db, "PRAGMA foreign_keys=OFF")
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := stageTenantRelationalSchema(tx); err != nil {
			t.Fatal(err)
		}
		if err := stageTenantFTS(tx); err != nil {
			t.Fatal(err)
		}
		relationalExec(t, tx, "INSERT INTO schema_version(version) VALUES(29)")
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		relationalExec(t, s.db, "PRAGMA foreign_keys=ON")
	}
	return s
}

func phase7ClaimCount(t *testing.T, s *StorageLayer, version, want int) {
	t.Helper()
	table := "entry_meta"
	if version == 29 {
		table = "operator_install_claim"
	}
	var got int
	if err := s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE path=?", installClaimedPath).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("v%d claim count=%d, want %d", version, got, want)
	}
	if version == 29 {
		if err := s.db.QueryRow("SELECT count(*) FROM entry_meta WHERE path=?", installClaimedPath).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != 0 {
			t.Fatal("operator claim leaked into workload metadata")
		}
	}
}

func TestPhase7ClaimWriters(t *testing.T) {
	for _, version := range []int{28, 29} {
		for _, kind := range []string{"api", "oauth", "saved-oauth", "bootstrap", "password", "refusal"} {
			t.Run(fmt.Sprintf("v%d/%s", version, kind), func(t *testing.T) {
				s := phase7Store(t, version)
				c := controlHandle(t, s)
				cap := operatorCapability(t)
				a, err := c.TokenAdmin(cap)
				if err != nil {
					t.Fatal(err)
				}
				ctx := claimContext(t)
				switch kind {
				case "api":
					err = a.CreateToken(ctx, "first", "secret", "")
				case "oauth":
					err = c.CreateAccessToken(ctx, &OAuthAccessToken{Token: "secret", ClientID: "client"})
				case "saved-oauth":
					err = c.SaveAccessToken(ctx, "secret", "client", "read:*", time.Now().Add(time.Hour).Unix())
				case "bootstrap":
					err = a.BootstrapToken(ctx, "first", "secret", false)
				case "password":
					err = c.MarkInstallClaimed(ctx, cap)
				case "refusal":
					requireClosed(t, a.BootstrapToken(ctx, "first", "secret", true))
				}
				if err != nil {
					t.Fatalf("claim writer failed: %v", err)
				}
				phase7ClaimCount(t, s, version, 1)
				// Revocation/deletion of all credentials must never reopen a claim.
				relationalExec(t, s.db, "DELETE FROM api_tokens; DELETE FROM oauth_access_tokens")
				requireClosed(t, a.BootstrapToken(ctx, "later", "later", false))
				phase7ClaimCount(t, s, version, 1)
			})
		}
	}
}

func TestPhase7IdentityNotPromotedToTenant(t *testing.T) {
	for _, value := range []any{(*StorageLayer)(nil), (*TenantStore)(nil)} {
		for _, name := range []string{"ValidateToken", "CreateToken", "CreateOAuthClient", "GetAccessToken", "BootstrapToken", "MarkInstallClaimed", "ListTenantRoots", "RegisterTenantRoots", "GenerateToken"} {
			if _, ok := reflect.TypeOf(value).MethodByName(name); ok {
				t.Errorf("%T still exposes %s", value, name)
			}
		}
	}
}

func TestPhase7ValidateTokenLifetime(t *testing.T) {
	s := newTestStorage(t)
	c := controlHandle(t, s)
	a, err := c.TokenAdmin(operatorCapability(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.CreateToken(ctx, "first", "secret", "read:*"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ValidateToken(ctx, "secret"); err != nil {
		t.Fatal(err)
	}
	// Returning from validation is the telemetry lifetime boundary, not a sleep.
	got, err := a.GetTokenByName(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUsed == "" {
		t.Fatal("validation returned with detached last_used work")
	}
	relationalExec(t, s.db, "CREATE TRIGGER fail_telemetry BEFORE UPDATE ON api_tokens BEGIN SELECT RAISE(ABORT,'telemetry unavailable'); END")
	if _, err := c.ValidateToken(ctx, "secret"); err != nil {
		t.Fatalf("best-effort telemetry denied valid auth: %v", err)
	}
	local, err := s.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	if local.TenantID() != tenant.Local {
		t.Fatal("identity operation rebound workload handle")
	}
}

func TestPhase7ClaimRollback(t *testing.T) {
	for _, version := range []int{28, 29} {
		for _, kind := range []string{"api", "oauth", "bootstrap"} {
			for _, failure := range []string{"claim", "credential"} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, kind, failure), func(t *testing.T) {
					s := phase7Store(t, version)
					c, a := controlHandle(t, s), adminHandle(t, s)
					ctx := claimContext(t)
					table := "api_tokens"
					if kind == "oauth" {
						table = "oauth_access_tokens"
					}
					failedTable := table
					if failure == "claim" {
						failedTable = "entry_meta"
						if version == 29 {
							failedTable = "operator_install_claim"
						}
					}
					relationalExec(t, s.db, "CREATE TRIGGER phase7_fail BEFORE INSERT ON "+failedTable+" BEGIN SELECT RAISE(ABORT,'phase7 insert failure'); END")
					var err error
					switch kind {
					case "api":
						err = a.CreateToken(ctx, "first", "secret", "")
					case "oauth":
						err = c.SaveAccessToken(ctx, "secret", "client", "read:*", time.Now().Add(time.Hour).Unix())
					case "bootstrap":
						err = a.BootstrapToken(ctx, "first", "secret", false)
					}
					var closed *BootstrapClosedError
					if err == nil || errors.As(err, &closed) {
						t.Fatalf("want insert error, got %v", err)
					}
					phase7ClaimCount(t, s, version, 0)
					var count int
					if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Fatal("credential survived failed transaction")
					}
					relationalExec(t, s.db, "DROP TRIGGER phase7_fail")
					if err := a.BootstrapToken(ctx, "retry", "retry", false); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestPhase7BackfillAndLegacyRefusal(t *testing.T) {
	for _, version := range []int{28, 29} {
		for _, kind := range []string{"api", "revoked", "oauth", "expired"} {
			for _, backfill := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/%s/backfill=%v", version, kind, backfill), func(t *testing.T) {
					s := phase7Store(t, version)
					ctx := claimContext(t)
					a := adminHandle(t, s)
					if kind == "api" || kind == "revoked" {
						relationalExec(t, s.db, "INSERT INTO api_tokens(name,token) VALUES('legacy','legacy')")
						if kind == "revoked" {
							relationalExec(t, s.db, "UPDATE api_tokens SET revoked_at='revoked'")
						}
					} else {
						expires := time.Now().Add(time.Hour).Unix()
						if kind == "expired" {
							expires = 1
						}
						claimExec(t, s, "INSERT INTO oauth_access_tokens(token,client_id,created_at,expires_at) VALUES('legacy','client',1,?)", expires)
					}
					active := kind == "api" || kind == "oauth"
					if backfill {
						if err := (identityStore{db: s.db}).backfillInstallClaim(ctx); err != nil {
							t.Fatal(err)
						}
						want := 0
						if active {
							want = 1
						}
						phase7ClaimCount(t, s, version, want)
					} else {
						err := a.BootstrapToken(ctx, "first", "first", false)
						if active {
							requireClosed(t, err)
						} else if err != nil {
							t.Fatal(err)
						}
						phase7ClaimCount(t, s, version, 1)
					}
					relationalExec(t, s.db, "DELETE FROM api_tokens; DELETE FROM oauth_access_tokens")
					// Restart backfill is idempotent, including with no live credentials.
					if err := (identityStore{db: s.db}).backfillInstallClaim(ctx); err != nil {
						t.Fatal(err)
					}
					err := a.BootstrapToken(ctx, "later", "later", false)
					if active || !backfill {
						requireClosed(t, err)
					} else if err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestPhase7CapabilityBeforeSQL(t *testing.T) {
	for _, version := range []int{28, 29} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			s := phase7Store(t, version)
			c := controlHandle(t, s)
			// A real admin:* credential authenticates but cannot mint operator power.
			a := adminHandle(t, s)
			ctx := claimContext(t)
			if err := a.CreateToken(ctx, "admin", "admin-secret", "admin:*"); err != nil {
				t.Fatal(err)
			}
			if got, err := c.ValidateToken(ctx, "admin-secret"); err != nil || got.Scope != "admin:*" {
				t.Fatalf("auth: %v %v", got, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			// A closed pool makes any accidental SQL observable as the wrong error.
			if err := c.MarkInstallClaimed(ctx, auth.DeploymentOperator{}); !errors.Is(err, auth.ErrOperatorRequired) {
				t.Fatal(err)
			}
			if _, err := c.TokenAdmin(auth.DeploymentOperator{}); !errors.Is(err, auth.ErrOperatorRequired) {
				t.Fatal(err)
			}
			if _, err := c.TenantRegistry(auth.DeploymentOperator{}); !errors.Is(err, auth.ErrOperatorRequired) {
				t.Fatal(err)
			}
			assertAdminDenied(t, &TokenAdmin{backing: s})
			r := &tenantRegistry{backing: s}
			if _, err := r.ListTenantRoots(ctx); !errors.Is(err, auth.ErrOperatorRequired) {
				t.Fatal(err)
			}
			// Generation is independent of the DB lifetime, but still capability gated.
			if value, err := a.GenerateToken(); err != nil || len(value) != 43 {
				t.Fatalf("generation: %q %v", value, err)
			}
		})
	}
}

func TestPhase7ControlAuthLifecycle(t *testing.T) {
	for _, version := range []int{28, 29} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			s := phase7Store(t, version)
			c, a := controlHandle(t, s), adminHandle(t, s)
			ctx := claimContext(t)
			if err := c.CreateOAuthClient(ctx, &OAuthClient{ClientID: "client", ClientSecret: "secret", RedirectURIs: []string{"http://localhost/callback"}}); err != nil {
				t.Fatal(err)
			}
			if got, err := c.GetOAuthClient(ctx, "client"); err != nil || got.ClientSecret != "secret" {
				t.Fatalf("client: %v %v", got, err)
			}
			for _, expired := range []bool{false, true} {
				expires := time.Now().Add(time.Hour).Unix()
				if expired {
					expires = 1
				}
				if err := c.CreateAuthCode(ctx, &OAuthAuthCode{Code: "code", ClientID: "client", ExpiresAt: expires}); err != nil {
					t.Fatal(err)
				}
				code, err := c.ConsumeAuthCode(ctx, "code")
				if expired {
					if err == nil || code != nil {
						t.Fatal("expired code authenticated")
					}
				} else if err != nil || code.ClientID != "client" {
					t.Fatalf("code: %v %v", code, err)
				}
				if _, err := c.ConsumeAuthCode(ctx, "code"); err == nil {
					t.Fatal("code replay authenticated")
				}
				if err := c.CreateRefreshToken(ctx, &OAuthRefreshToken{Token: "refresh", ClientID: "client", ExpiresAt: expires}); err != nil {
					t.Fatal(err)
				}
				refresh, err := c.ConsumeRefreshToken(ctx, "refresh")
				if expired {
					if err == nil || refresh != nil {
						t.Fatal("expired refresh authenticated")
					}
				} else if err != nil || refresh.ClientID != "client" {
					t.Fatalf("refresh: %v %v", refresh, err)
				}
				if _, err := c.ConsumeRefreshToken(ctx, "refresh"); err == nil {
					t.Fatal("refresh replay authenticated")
				}
			}
			// No client/code/refresh record is an active installation credential.
			phase7ClaimCount(t, s, version, 0)
			if err := c.CreateAccessToken(ctx, &OAuthAccessToken{Token: "expired", ClientID: "client", ExpiresAt: 1}); err != nil {
				t.Fatal(err)
			}
			if got, err := c.GetAccessToken(ctx, "expired"); err == nil || got != nil {
				t.Fatal("expired access authenticated")
			}
			phase7ClaimCount(t, s, version, 0)
			// Fixed-second equality verifies the predicate without a rollover race.
			const second int64 = 1234567890
			claimExec(t, s, "UPDATE oauth_access_tokens SET expires_at=?", second)
			for _, at := range []int64{second - 1, second, second + 1} {
				var active bool
				if err := s.db.QueryRowContext(ctx, "SELECT "+activeInstallCredentials, at).Scan(&active); err != nil {
					t.Fatal(err)
				}
				if active != (at <= second) {
					t.Fatalf("inclusive expiry at %d: %v", at, active)
				}
			}
			if err := a.CreateToken(ctx, "reader", "reader-secret", "read:*"); err != nil {
				t.Fatal(err)
			}
			if got, err := c.ValidateToken(ctx, "reader-secret"); err != nil || got.Scope != "read:*" {
				t.Fatalf("api auth: %v %v", got, err)
			}
			if err := a.RevokeToken(ctx, "reader"); err != nil {
				t.Fatal(err)
			}
			if got, err := c.ValidateToken(ctx, "reader-secret"); err == nil || got != nil {
				t.Fatal("revoked API token authenticated")
			}
			if err := a.DeleteTokenPermanent(ctx, "reader"); err != nil {
				t.Fatal(err)
			}
			requireClosed(t, a.BootstrapToken(ctx, "later", "later", false))
		})
	}
}
