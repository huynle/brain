package bridge

import (
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// v2.0.18 permission events keep the v1 names but move the permission id:
// permission.asked carries it at data.id, permission.replied at data.requestID
// (v1 used properties.id). trackControlEvent must fold on asked and clear on
// replied. These are the real captured v2 payloads.
func TestTrackControlEvent_V2PermissionPayloads(t *testing.T) {
	conn := &runnerConn{live: make(map[string]*instanceLive)}
	const inst = "inst_perm"

	asked := json.RawMessage(`{"id":"evt_1","type":"permission.asked","data":{"id":"per_abc","sessionID":"ses_1","action":"shell"}}`)
	conn.trackControlEvent(inst, asked)
	if n := len(conn.live[inst].pendingPerm); n != 1 {
		t.Fatalf("after asked: pending = %d, want 1 (v2 id at data.id not parsed?)", n)
	}

	replied := json.RawMessage(`{"id":"evt_2","type":"permission.replied","data":{"sessionID":"ses_1","requestID":"per_abc","reply":"reject"}}`)
	conn.trackControlEvent(inst, replied)
	if n := len(conn.live[inst].pendingPerm); n != 0 {
		t.Fatalf("after replied: pending = %d, want 0 (v2 id at data.requestID not parsed?)", n)
	}
}

// v2 turn-lifecycle status events (captured live) must set busy/idle; the v1
// names are retained as fallbacks.
func TestTrackControlEvent_V2StatusEvents(t *testing.T) {
	conn := &runnerConn{live: make(map[string]*instanceLive)}
	const inst = "inst_status"

	conn.trackControlEvent(inst, json.RawMessage(`{"type":"session.execution.started","data":{}}`))
	if got := conn.live[inst].status; got != types.InstanceStatusBusy {
		t.Fatalf("after execution.started: status = %q, want busy", got)
	}
	conn.trackControlEvent(inst, json.RawMessage(`{"type":"session.execution.succeeded","data":{}}`))
	if got := conn.live[inst].status; got != types.InstanceStatusIdle {
		t.Fatalf("after execution.succeeded: status = %q, want idle", got)
	}
}
