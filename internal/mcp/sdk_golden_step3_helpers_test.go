package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huynle/brain-api/internal/bridge"
)

// connectScriptedRunner is connectFakeRunner for the step-3 tools: the test
// supplies every response, and history/children frames (session_tail,
// session_children) are answered and recorded too. It is still a websocket
// served by the test itself: nothing is spawned, prompted or read anywhere.
func connectScriptedRunner(t *testing.T, apiURL, runnerID string, respond func(bridge.Frame) bridge.Frame) *fakeRunner {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wsURL := "ws" + strings.TrimPrefix(apiURL, "http") + "/api/v1/runners/" + runnerID + "/bridge"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		t.Fatalf("bridge dial: %v", err)
	}
	conn.SetReadLimit(bridge.MaxFrameBytes)
	f := &fakeRunner{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var fr bridge.Frame
			if json.Unmarshal(data, &fr) != nil {
				continue
			}
			switch fr.Type {
			case bridge.FrameReq, bridge.FrameSpawn, bridge.FrameKill:
				f.record(fr)
			case bridge.FrameHistory, bridge.FrameChildren:
				f.mu.Lock()
				f.frames = append(f.frames, fmt.Sprintf("%s session=%s recursive=%t depth=%d", fr.Type, fr.SessionID, fr.Recursive, fr.Depth))
				f.mu.Unlock()
			default:
				continue
			}
			out, _ := json.Marshal(respond(fr))
			if conn.Write(ctx, websocket.MessageText, out) != nil {
				return
			}
		}
	}()
	hello, _ := json.Marshal(bridge.Frame{Type: bridge.FrameHello, RunnerID: runnerID, Proto: 1})
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		cancel()
		<-done
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var list struct {
			Runners []struct {
				RunnerID        string `json:"runner_id"`
				BridgeConnected bool   `json:"bridge_connected"`
			} `json:"runners"`
		}
		getJSON(t, apiURL+"/api/v1/runners", &list)
		for _, r := range list.Runners {
			if r.RunnerID == runnerID && r.BridgeConnected {
				return f
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runner %s bridge never connected", runnerID)
	return nil
}

// sessionHistory is an OpenCode-shaped transcript: user text, assistant text,
// a completed tool part and a reasoning part (which session_tail must drop).
const sessionHistory = `[{"info":{"id":"msg_1","role":"user","time":{"created":1700000000000}},"parts":[{"id":"prt_1","type":"text","text":"please list files","time":{"start":1700000000000}}]},` +
	`{"info":{"id":"msg_2","role":"assistant","time":{"created":1700000001000}},"parts":[` +
	`{"id":"prt_2","type":"reasoning","text":"private chain of thought"},` +
	`{"id":"prt_3","type":"tool","tool":"bash","state":{"status":"completed","output":"a.txt\nb.txt"}},` +
	`{"id":"prt_4","type":"text","text":"Found two files. token=sk-ant-api03-SECRETSECRETSECRETSECRET","time":{"start":1700000002000,"end":1700000003000}}]}]`

// sessionChildren is a persisted parent_id tree two levels deep.
const sessionChildren = `[{"session_id":"ses_child_a","parent_id":"ses-parent","title":"Explore","children":[{"session_id":"ses_grandchild","parent_id":"ses_child_a","title":"Deep"}]},{"session_id":"ses_child_b","parent_id":"ses-parent","title":"Build"}]`

// scriptedResponse answers like a well-behaved OpenCode runner: prompts are
// accepted, history/children come from the fixtures above, and anything
// naming ses-missing fails as an unknown session.
func scriptedResponse(fr bridge.Frame) bridge.Frame {
	res := bridge.Frame{Type: bridge.FrameRes, ID: fr.ID}
	switch fr.Type {
	case bridge.FrameHistory:
		if fr.SessionID == "ses-missing" {
			res.Error = "session not found: ses-missing"
			break
		}
		if fr.SessionID == "ses-empty" {
			break
		}
		res.Body = json.RawMessage(sessionHistory)
	case bridge.FrameChildren:
		if fr.SessionID == "ses-missing" {
			res.Error = "session not found: ses-missing"
			break
		}
		res.Body = json.RawMessage(sessionChildren)
	case bridge.FrameReq:
		switch {
		case strings.Contains(fr.Path, "/session/ses-missing/"):
			res.Error = "unknown instance " + fr.InstanceID
		case strings.HasSuffix(fr.Path, "/prompt_async"):
			res.Status = http.StatusNoContent
		default:
			res.Status = http.StatusOK
			res.Body = json.RawMessage(`true`)
		}
	}
	return res
}

// entryFields creates an entry through the real REST API and returns its id.
func entryFields(t *testing.T, apiURL string, fields map[string]any) string {
	t.Helper()
	return createJSON(t, apiURL+"/api/v1/entries", fields, "id")
}

// apiJSON performs a REST call and decodes the JSON response, failing on
// any status >= 300.
func apiJSON(t *testing.T, method, url string, body any, out any) {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var msg map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		t.Fatalf("%s %s: status %d %v", method, url, resp.StatusCode, msg)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
}
