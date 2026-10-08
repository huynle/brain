package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

// The SDK deliberately omits server messages from (*brain.Error).Error(), but
// MCP tool errors are what agents read, so the hosted MCP must keep rendering
// failures exactly as the hand-built client did. Each case runs the same
// failure through both paths and requires identical text.
func TestSDKCall_ErrorTextMatchesLegacyClient(t *testing.T) {
	cases := []struct {
		name   string
		status int
		ctype  string
		body   string
	}{
		{"message wins", 404, "application/json", `{"error":"Not Found","message":"reminder not found"}`},
		{"error only", 409, "application/json", `{"error":"conflict happened"}`},
		{"empty envelope", 400, "application/json", `{}`},
		{"non-json body", 500, "text/plain", "boom"},
		{"machine code ignored", 403, "application/json", `{"code":"forbidden","error":"Forbidden","message":"nope"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := NewAPIClient(srv.URL)
			legacy := c.Request(context.Background(), http.MethodGet, "/reminders/x", nil, nil, &map[string]any{})
			_, viaSDK := sdkCall(context.Background(), c, func(ctx context.Context, sc *brain.Client) (*brain.ReminderSummary, error) {
				return sc.Reminders().Get(ctx, "x")
			})
			if legacy == nil || viaSDK == nil {
				t.Fatalf("expected errors, legacy=%v sdk=%v", legacy, viaSDK)
			}
			if legacy.Error() != viaSDK.Error() {
				t.Fatalf("error text drift:\nlegacy: %q\nsdk:    %q", legacy.Error(), viaSDK.Error())
			}
		})
	}
}

func TestSDKCall_TransportErrorTextMatchesLegacyClient(t *testing.T) {
	c := NewAPIClient("http://127.0.0.1:1")
	legacy := c.Request(context.Background(), http.MethodGet, "/goals", nil, map[string]string{"project": "p", "status": "all"}, nil)
	_, viaSDK := sdkCall(context.Background(), c, func(ctx context.Context, sc *brain.Client) (*brain.ListGoalsResponse, error) {
		p, s := "p", "all"
		return sc.Goals().List(ctx, &brain.GoalsListParams{Project: &p, Status: &s})
	})
	if legacy == nil || viaSDK == nil || legacy.Error() != viaSDK.Error() {
		t.Fatalf("transport error drift:\nlegacy: %v\nsdk:    %v", legacy, viaSDK)
	}
}

// The SDK binding is built per call from the request-scoped APIClient: same
// loopback base, same caller token, nothing else.
func TestSDKCall_UsesClientBaseAndToken(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		fmt.Fprint(w, `{"reminder_id":"x","title":"t","state":"armed","status":"active","action":"notify","entry_id":"e"}`)
	}))
	defer srv.Close()
	base := NewAPIClient(srv.URL)
	for _, tc := range []struct {
		client *APIClient
		want   string
	}{
		{base, ""},
		{base.WithAuthToken("tok-1"), "Bearer tok-1"},
	} {
		if _, err := sdkCall(context.Background(), tc.client, func(ctx context.Context, sc *brain.Client) (*brain.ReminderSummary, error) {
			return sc.Reminders().Get(ctx, "x")
		}); err != nil {
			t.Fatal(err)
		}
		if gotAuth != tc.want || gotPath != "/api/v1/reminders/x" {
			t.Fatalf("auth=%q path=%q, want auth=%q", gotAuth, gotPath, tc.want)
		}
	}
}
