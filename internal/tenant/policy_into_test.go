package tenant

import (
	"strings"
	"testing"
)

func TestIntoTrustedBoundaryAllowlist(t *testing.T) {
	for _, path := range []string{"internal/api/tenant_middleware.go", "internal/tenant/authorized.go", "internal/apiserver/tenant.go", "internal/mcpserver/tenant.go"} {
		for _, name := range []string{"Into", "BindAuthorized", "Parse", "MustParse"} {
			got, err := tenantPolicy(path, `package boundary; import scope "`+tenantImport+`"; var f = scope.`+name)
			want := 1
			if name == "Into" || name == "BindAuthorized" {
				want = 0
			}
			if err != nil || len(got) != want {
				t.Errorf("%s %s: %v, %v; want %d violations", path, name, got, err, want)
			}
		}
	}
	for _, path := range []string{"internal/api/tenant_middleware_extra.go", "internal/apiserver/nested/tenant.go", "internal/mcpserver/nested/tenant.go", "internal/tenant/nested/tenant.go", "internal/apiserver_extra/tenant.go", "internal/mcpserver_extra/tenant.go", "internal/tenant_extra/tenant.go"} {
		for _, name := range []string{"Into", "BindAuthorized"} {
			got, err := tenantPolicy(path, `package boundary; import scope "`+tenantImport+`"; var f = scope.`+name)
			if err != nil || len(got) != 1 {
				t.Errorf("%s %s: %v, %v; want violation", path, name, got, err)
			}
		}
	}
	for _, name := range []string{"Into", "BindAuthorized"} {
		got, err := tenantPolicy("internal/tenant/authorized.go", `package tenant; var f = `+name)
		if err != nil || len(got) != 0 {
			t.Errorf("same package %s: %v, %v", name, got, err)
		}
	}
}

func TestTrustedBindingReferences(t *testing.T) {
	for _, name := range []string{"Into", "BindAuthorized"} {
		for _, tc := range []struct {
			name, path, source string
			count              int
		}{
			{"default-call", "internal/service/policy.go", `package service; import "` + tenantImport + `"; func f() { tenant.BIND(ctx, id) }`, 1},
			{"alias-reference", "internal/service/policy.go", `package service; import scope "` + tenantImport + `"; var f = scope.BIND`, 1},
			{"build-tagged", "internal/service/policy.go", "//go:build tenant_policy_fixture\n\n" + `package service; import scope "` + tenantImport + `"; func f() { scope.BIND(ctx, id) }`, 1},
			{"test-exempt", "internal/service/nested/policy_test.go", `package service; import scope "` + tenantImport + `"; var f = scope.BIND`, 0},
			{"unrelated-import", "internal/service/policy.go", `package service; import tenant "other/package"; var f = tenant.BIND`, 0},
			{"shadowed-alias", "internal/service/policy.go", `package service; import scope "` + tenantImport + `"; func f(scope struct{ BIND func() }) { scope.BIND() }`, 0},
			{"unrelated-unqualified", "internal/service/policy.go", `package service; func BIND() {}; var f = BIND`, 0},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				got, err := tenantPolicy(tc.path, strings.ReplaceAll(tc.source, "BIND", name))
				if err != nil || len(got) != tc.count {
					t.Fatalf("violations = %v, %v; want %d", got, err, tc.count)
				}
				if tc.count > 0 && !strings.Contains(got[0], "tenant."+name+" requires review") {
					t.Fatalf("diagnostic does not identify restricted reference: %v", got)
				}
			})
		}
	}
}
