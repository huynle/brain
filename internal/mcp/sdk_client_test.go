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
		// A success status with a body that is not JSON is a decode failure,
		// reported with the JSON syntax error as the legacy client did.
		{"non-json success", 200, "text/html", "<html>ok</html>"},
		{"truncated json success", 200, "application/json", `{"reminder_id":`},
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

// The proxied remote-control calls (prompt, abort, permission) decode the
// instance's own body: a non-JSON 200 keeps the legacy decode text rather
// than the SDK's bare invalid_response (step-2 review follow-up).
func TestControlProxyNonJSONSuccessKeepsDecodeText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()
	s := NewServer()
	RegisterControlTools(s, NewAPIClient(srv.URL))
	ids := map[string]any{"runner_id": "r", "instance_id": "i", "session_id": "s"}
	for name, extra := range map[string]map[string]any{
		"control_send_prompt":   {"text": "hi"},
		"control_abort_session": {},
		"control_permission":    {"permission_id": "p", "response": "once"},
	} {
		args := map[string]any{}
		for k, v := range ids {
			args[k] = v
		}
		for k, v := range extra {
			args[k] = v
		}
		_, err := s.tools[name].handler(context.Background(), args)
		if err == nil || err.Error() != "decode response: invalid character 'o' in literal null (expecting 'u')" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
