package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoiceDiagnosticsRejectsContentAndAcceptsMetadata(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"attempt":"test-1","event":"audio_start","elapsed_ms":20,"android":true,"hands_free":true}`, 204},
		{`{"attempt":"test-1","event":"result","transcript":"private words"}`, 400},
		{`{"attempt":"test-1","event":"error","error":"arbitrary private text"}`, 400},
		{`{"attempt":"test-1","event":"unknown"}`, 400},
		{`{"attempt":"test-1","event":"started"} {}`, 400},
	} {
		w := httptest.NewRecorder()
		(&Handler{}).HandleAssistantVoiceDiagnostics(w, httptest.NewRequest("POST", "/voice-diagnostics", strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("status %d, want %d", w.Code, tc.code)
		}
	}
}
