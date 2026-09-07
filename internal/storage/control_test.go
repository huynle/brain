package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"golang.org/x/crypto/bcrypt"
)

func operatorCapability(t *testing.T) auth.DeploymentOperator {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(auth.EnvUsername, "owner")
	t.Setenv(auth.EnvPasswordHash, string(hash))
	cap, err := auth.NewVerifierFromEnv().AuthenticateOperator("owner", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return cap
}

func controlHandle(t *testing.T, s *StorageLayer) *ControlStore {
	t.Helper()
	c, err := s.Control()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func exactMethods(t *testing.T, value any, want []string) {
	t.Helper()
	typ := reflect.TypeOf(value)
	got := make([]string, typ.NumMethod())
	for i := range got {
		got[i] = typ.Method(i).Name
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s methods = %v; exact allowlist = %v", typ, got, want)
	}
}

// This is a control-plane allowlist, not P3.7's data-plane receiver ratchet.
func TestControlMethodAllowlistAndCapability(t *testing.T) {
	cap := operatorCapability(t)
	c := controlHandle(t, &StorageLayer{}) // No DB; all binding/rejection is pre-SQL.
	exactMethods(t, c, []string{"ValidateToken", "GetAccessToken", "TenantRegistry", "TokenAdmin", "MarkInstallClaimed", "CreateOAuthClient", "GetOAuthClient", "CreateAuthCode", "ConsumeAuthCode", "CreateAccessToken", "SaveAccessToken", "CreateRefreshToken", "ConsumeRefreshToken"})
	if err := c.MarkInstallClaimed(context.Background(), auth.DeploymentOperator{}); !errors.Is(err, auth.ErrOperatorRequired) {
		t.Fatalf("claim must reject before SQL: %v", err)
	}
	if repo, err := c.TenantRegistry(auth.DeploymentOperator{}); repo != nil || !errors.Is(err, auth.ErrOperatorRequired) {
		t.Fatalf("registry must reject before SQL: %v %v", repo, err)
	}
	if admin, err := c.TokenAdmin(auth.DeploymentOperator{}); admin != nil || !errors.Is(err, auth.ErrOperatorRequired) {
		t.Fatalf("token admin must reject before SQL: %v %v", admin, err)
	}
	repo, err := c.TenantRegistry(cap)
	if err != nil {
		t.Fatal(err)
	}
	exactMethods(t, repo, []string{"ListTenantRoots", "RegisterTenantRoots"})
	admin, err := c.TokenAdmin(cap)
	if err != nil {
		t.Fatal(err)
	}
	exactMethods(t, admin, []string{"GenerateToken", "CreateToken", "ListTokens", "GetTokenByName", "RevokeToken", "DeleteTokenPermanent", "CountActiveTokens", "BootstrapToken"})
	// Zero adapters cannot be used to bypass the constructor gate.
	zeroRepo := &tenantRegistry{}
	if _, err := zeroRepo.ListTenantRoots(context.Background()); !errors.Is(err, auth.ErrOperatorRequired) {
		t.Fatalf("zero registry: %v", err)
	}
	if err := zeroRepo.RegisterTenantRoots(context.Background(), tenantfs.Mapping{ID: tenant.Local}, nil); !errors.Is(err, auth.ErrOperatorRequired) {
		t.Fatalf("zero registry write: %v", err)
	}
	assertAdminDenied(t, &TokenAdmin{})
}

type tokenAdminContract interface {
	GenerateToken() (string, error)
	CreateToken(context.Context, string, string, string) error
	ListTokens(context.Context, ...bool) ([]Token, error)
	GetTokenByName(context.Context, string) (*Token, error)
	RevokeToken(context.Context, string) error
	DeleteTokenPermanent(context.Context, string) error
	CountActiveTokens(context.Context) (int, error)
	BootstrapToken(context.Context, string, string, bool) error
}

func assertAdminDenied(t *testing.T, a tokenAdminContract) {
	t.Helper()
	ctx := context.Background()
	_, generate := a.GenerateToken()
	create := a.CreateToken(ctx, "local", "admin-token", "admin:*")
	_, list := a.ListTokens(ctx)
	_, get := a.GetTokenByName(ctx, "local")
	revoke := a.RevokeToken(ctx, "local")
	remove := a.DeleteTokenPermanent(ctx, "local")
	_, count := a.CountActiveTokens(ctx)
	bootstrap := a.BootstrapToken(ctx, "first", "secret", false)
	for _, err := range []error{generate, create, list, get, revoke, remove, count, bootstrap} {
		if !errors.Is(err, auth.ErrOperatorRequired) {
			t.Fatalf("unguarded token admin operation: %v", err)
		}
	}
}

func TestControlSharedBacking(t *testing.T) {
	s := newTestStorage(t)
	cap := operatorCapability(t)
	ctx := context.Background()
	// No factory may rerun schema creation, modify pool policy, or stamp a tenant.
	s.db.SetMaxOpenConns(3)
	if _, err := s.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	c := controlHandle(t, s)
	local, err := s.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := c.TenantRegistry(cap)
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.TokenAdmin(cap)
	if err != nil {
		t.Fatal(err)
	}
	if local.DB() != s.db || other.DB() != s.db || s.db.Stats().MaxOpenConnections != 3 {
		t.Fatal("factory changed shared pool")
	}
	maps, err := repo.ListTenantRoots(ctx)
	if err != nil || len(maps) != 0 {
		t.Fatalf("binding registered tenant: %v %v", maps, err)
	}
	if _, err := s.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	s.db.SetMaxOpenConns(1)
	resolver, err := tenantfs.New(repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	m, err := resolver.ProvisionLocal(ctx, filepath.Join(base, "brain"), filepath.Join(base, "cas"))
	if err != nil {
		t.Fatal(err)
	}
	maps, err = s.ListTenantRoots(ctx)
	if err != nil || len(maps) != 1 || maps[0] != m {
		t.Fatalf("registry not shared: %v %v", maps, err)
	}
	token, err := a.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateToken(ctx, "owner", token, "read:*"); err != nil {
		t.Fatal(err)
	}
	got, err := c.ValidateToken(ctx, token)
	if err != nil || got.Name != "owner" || got.Scope != "read:*" {
		t.Fatalf("identity backing: %v %v", got, err)
	}
	got, err = a.GetTokenByName(ctx, "owner")
	if err != nil || got.Token != token {
		t.Fatalf("get token: %v %v", got, err)
	}
	list, err := a.ListTokens(ctx)
	if err != nil || len(list) != 1 || list[0].Token != token[:8] {
		t.Fatalf("list token: %v %v", list, err)
	}
	count, err := a.CountActiveTokens(ctx)
	if err != nil || count != 1 {
		t.Fatalf("count: %d %v", count, err)
	}
	if err := a.RevokeToken(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ValidateToken(ctx, token); err == nil {
		t.Fatal("revoked token validated")
	}
	list, err = a.ListTokens(ctx, true)
	if err != nil || len(list) != 1 || list[0].RevokedAt == "" {
		t.Fatalf("revoked list: %v %v", list, err)
	}
	if err := a.DeleteTokenPermanent(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTokenByName(ctx, "owner"); err == nil {
		t.Fatal("delete did not reach shared backing")
	}
	if err := s.SaveAccessToken(ctx, "oauth-token", "client", "read:*", 4102444800); err != nil {
		t.Fatal(err)
	}
	oauthToken, err := c.GetAccessToken(ctx, "oauth-token")
	if err != nil || oauthToken.ClientID != "client" {
		t.Fatalf("OAuth backing: %v %v", oauthToken, err)
	}
}
