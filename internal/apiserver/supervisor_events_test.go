package apiserver

import (
	"encoding/json"
	"github.com/huynle/brain-api/internal/types"
	"strings"
	"testing"
)

func TestSupervisorControlProjection(t *testing.T) {
	event, ok := projectSupervisorControl([]byte(`{"type":"permission.asked","properties":{"id":"perm","sessionID":"ses","patterns":["secret-command"],"metadata":{"token":"hidden"}}}`))
	if !ok || event.Type != types.EventSessionPermission || event.Metadata["session_id"] != "ses" {
		t.Fatal(event, ok)
	}
	raw, _ := json.Marshal(event)
	if strings.Contains(string(raw), "secret-command") || strings.Contains(string(raw), "hidden") {
		t.Fatal(string(raw))
	}
	if _, ok := projectSupervisorControl([]byte(`{"type":"message.part.updated","properties":{"text":"do not copy transcript"}}`)); ok {
		t.Fatal("transcript frame exported")
	}
	if event, ok := projectSupervisorControl([]byte(`{"type":"session.status","properties":{"sessionID":"ses","status":{"type":"busy"}}}`)); !ok || event.Type != types.EventSessionActivity {
		t.Fatal(event, ok)
	}
}
