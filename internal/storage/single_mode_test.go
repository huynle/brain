package storage

import (
	"context"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestSingleModeCompatibilityFactory(t *testing.T) {
	s := &StorageLayer{} // All guards must precede SQL.
	m := reflect.ValueOf(s).MethodByName("SingleModeTokens")
	if !m.IsValid() {
		t.Fatal("missing single-mode-only compatibility factory")
	}
	for _, mode := range []tenant.Mode{tenant.ModeSingle, tenant.ModeMulti, "", "unknown"} {
		result := m.Call([]reflect.Value{reflect.ValueOf(mode)})
		if len(result) != 2 {
			t.Fatal("factory must return handle and error")
		}
		allowed := mode == tenant.ModeSingle
		if result[0].IsNil() == allowed || result[1].IsNil() != allowed {
			t.Fatalf("mode %q: handle=%v error=%v", mode, result[0], result[1])
		}
	}
}

func TestSingleModeCompatibilityMethodAllowlistAndZeroDenial(t *testing.T) {
	s, err := (&StorageLayer{}).SingleModeTokens(tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	exactMethods(t, s, []string{"GenerateToken", "CreateToken", "ListTokens", "GetTokenByName", "RevokeToken", "BootstrapToken"})
	ctx := context.Background()
	for _, invalid := range []*SingleModeTokenStore{nil, {}, {backing: &StorageLayer{}, mode: tenant.ModeMulti}} {
		_, gen := invalid.GenerateToken()
		create := invalid.CreateToken(ctx, "n", "t", "admin:*")
		_, list := invalid.ListTokens(ctx, true)
		_, get := invalid.GetTokenByName(ctx, "n")
		revoke := invalid.RevokeToken(ctx, "n")
		bootstrap := invalid.BootstrapToken(ctx, "n", "t", false)
		for _, err := range []error{gen, create, list, get, revoke, bootstrap} {
			if err == nil {
				t.Fatal("unbound/multi compatibility operation succeeded")
			}
		}
	}
}

func TestSingleModeCompatibilityRetainsTokenSemantics(t *testing.T) {
	owner := newTestStorage(t)
	compat, err := owner.SingleModeTokens(tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	control := controlHandle(t, owner)
	ctx := context.Background()
	secret, err := compat.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.BootstrapToken(ctx, "first", secret, false); err != nil {
		t.Fatal(err)
	}
	if got, err := control.ValidateToken(ctx, secret); err != nil || got.Scope != "admin:*" {
		t.Fatalf("bootstrap: %v %v", got, err)
	}
	if err := compat.CreateToken(ctx, "second", "second-secret", ""); err != nil {
		t.Fatal(err)
	}
	if got, err := compat.GetTokenByName(ctx, "second"); err != nil || got.Scope != "admin:*" {
		t.Fatalf("default scope: %v %v", got, err)
	}
	if err := compat.RevokeToken(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	active, err := compat.ListTokens(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%v err=%v", active, err)
	}
	all, err := compat.ListTokens(ctx, true)
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%v err=%v", all, err)
	}
	if _, err := control.ValidateToken(ctx, secret); err == nil {
		t.Fatal("revoked token validated")
	}
	if err := compat.RevokeToken(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if err := compat.BootstrapToken(ctx, "reopened", "new-secret", false); err == nil {
		t.Fatal("revocation reopened bootstrap")
	}
	if _, err := control.TokenAdmin(auth.DeploymentOperator{}); err == nil {
		t.Fatal("compatibility granted operator authority")
	}
}
