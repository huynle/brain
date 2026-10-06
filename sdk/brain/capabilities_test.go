package brain

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const capabilityFixture = `{"contract_version":"1.0.0","operations":["health.get"],"scripts":{"compiled":false,"configured":false,"deployment_available":false,"caller_authorized":false,"available":false}}`

func TestCapabilitiesNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
	}{
		{"valid", 200, capabilityFixture, ""},
		{"wrong_success", 201, capabilityFixture, "capability_discovery_unavailable"},
		{"old", 404, "secret", "unsupported_server"},
		{"unimplemented", 501, "secret", "unsupported_server"},
		{"auth", 401, "secret", "capability_auth_required"},
		{"forbidden", 403, "secret", "capability_auth_required"},
		{"unavailable", 503, "secret", "capability_discovery_unavailable"},
		{"version", 200, strings.Replace(capabilityFixture, "1.0.0", "0.1.0", 1), "incompatible_contract_version"},
		{"duplicate", 200, strings.Replace(capabilityFixture, `"compiled":false`, `"compiled":false,"compiled":false`, 1), "invalid_capability_manifest"},
		{"hidden", 200, strings.Replace(capabilityFixture, `"operations":`, `"principal":"secret","operations":`, 1), "invalid_capability_manifest"},
		{"flags", 200, strings.Replace(capabilityFixture, `"available":false`, `"available":true`, 1), "invalid_capability_manifest"},
		{"oversize", 200, strings.Repeat(" ", 65537), "invalid_capability_manifest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/capabilities" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("X-Request-ID", "discovery-id")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c, err := New(Config{BaseURL: srv.URL, Token: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			m, err := c.Capabilities(context.Background())
			if calls != 1 {
				t.Fatalf("requests=%d want=1", calls)
			}
			if tc.code == "" {
				if err != nil || m == nil || m.ContractVersion != ContractVersion || m.Scripts.Available {
					t.Fatalf("manifest=%+v err=%v", m, err)
				}
				return
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.code || e.Message != "" {
				t.Fatalf("error=%+v want=%s", e, tc.code)
			}
		})
	}
}

func TestCapabilitiesIndependentDimensions(t *testing.T) {
	for mask := 0; mask < 32; mask++ {
		m := CapabilityManifest{ContractVersion: ContractVersion, Operations: []string{"health.get"}, Scripts: ScriptAvailability{Compiled: mask&1 != 0, Configured: mask&2 != 0, DeploymentAvailable: mask&4 != 0, CallerAuthorized: mask&8 != 0, Available: mask&16 != 0}}
		body, _ := json.Marshal(m)
		got, err := decodeCapabilities(body)
		consistent := m.Scripts.Available == (mask&15 == 15)
		if consistent {
			if err != nil || got.Scripts != m.Scripts {
				t.Fatalf("mask %d got=%+v err=%v", mask, got, err)
			}
		} else if err == nil {
			t.Fatalf("mask %d accepted inconsistent availability", mask)
		}
	}
}

func TestCapabilitiesCancelledBinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("retired request reached server") }))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL})
	c.Close()
	if _, err := c.Capabilities(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("retired binding: %v", err)
	}
}
