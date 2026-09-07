package storage

import (
	"context"
	"testing"
	"time"
)

func TestControlIdentityFlowSharesPersistenceAndConsumption(t *testing.T) {
	owner := newTestStorage(t)
	c := controlHandle(t, owner)
	ctx := context.Background()
	if err := c.CreateOAuthClient(ctx, &OAuthClient{ClientID: "client", ClientSecret: "secret", RedirectURIs: []string{"http://localhost/callback"}}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetOAuthClient(ctx, "client"); err != nil || got.ClientSecret != "secret" {
		t.Fatalf("client=%v err=%v", got, err)
	}
	expires := time.Now().Add(time.Hour).Unix()
	if err := c.CreateAuthCode(ctx, &OAuthAuthCode{Code: "code", ClientID: "client", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.ConsumeAuthCode(ctx, "code"); err != nil || got.ClientID != "client" {
		t.Fatalf("code=%v err=%v", got, err)
	}
	if got, err := c.ConsumeAuthCode(ctx, "code"); got != nil || err == nil {
		t.Fatal("auth code replay accepted")
	}
	if err := c.CreateRefreshToken(ctx, &OAuthRefreshToken{Token: "refresh", ClientID: "client", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.ConsumeRefreshToken(ctx, "refresh"); err != nil || got.ClientID != "client" {
		t.Fatalf("refresh=%v err=%v", got, err)
	}
	if got, err := c.ConsumeRefreshToken(ctx, "refresh"); got != nil || err == nil {
		t.Fatal("refresh replay accepted")
	}
	if err := c.CreateAccessToken(ctx, &OAuthAccessToken{Token: "password", ClientID: "brain_password_session", Scope: "mcp control", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveAccessToken(ctx, "oauth", "client", "mcp", expires); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"password", "oauth"} {
		got, err := c.GetAccessToken(ctx, value)
		if err != nil || got.Token != value {
			t.Fatalf("access=%v err=%v", got, err)
		}
		if _, err := owner.GetAccessToken(ctx, value); err != nil {
			t.Fatal("identity has a different backing:", err)
		}
	}
	if err := owner.BootstrapToken(ctx, "late", "late-token", false); err == nil {
		t.Fatal("identity issuance did not permanently close bootstrap")
	}
}
