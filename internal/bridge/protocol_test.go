package bridge

import "testing"

func TestAllowedRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{"GET", "/session", true},
		{"POST", "/session", true},
		{"GET", "/session/status", true},
		{"GET", "/session/ses_123/message", true},
		{"GET", "/session/ses_123/message?limit=50", true},
		{"POST", "/session/ses_123/prompt_async", true},
		{"POST", "/session/ses_123/permissions/perm_9", true},
		{"POST", "/session/ses_123/abort", true},
		{"GET", "/agent", true},
		{"GET", "/config/providers", true},
		{"GET", "/global/health", true},

		// Not allowlisted
		{"GET", "/event", false},
		{"GET", "/file/content", false},
		{"GET", "/find", false},
		{"PATCH", "/config", false},
		{"DELETE", "/session/ses_123", false},
		{"POST", "/session/status", false},
		{"GET", "/session/ses_123/shell", false},
		{"POST", "/session/ses_123/permissions", false},      // missing permission id
		{"POST", "/session//prompt_async", false},            // empty wildcard segment
		{"GET", "/session/ses_123/message/extra", false},     // extra segment
		{"POST", "/tui/append-prompt", false},
		{"GET", "", false},
	}

	for _, tt := range tests {
		if got := AllowedRequest(tt.method, tt.path); got != tt.want {
			t.Errorf("AllowedRequest(%s, %q) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}
