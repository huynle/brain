package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sessionHistoryForPort fetches a session's transcript as the GET
// /session/{id}/message JSON array. Indirected for tests.
var sessionHistoryForPort = fetchSessionMessages

// fetchSessionMessages returns a session's transcript as the JSON array
// GET /session/{id}/message produces. It queries the live server first
// (via opcodeStatusClient), then falls back to on-disk storage
// (SQLite, then the file-per-message layout), mirroring
// BridgeClient.fetchSessionHistory's live-then-disk precedence.
func fetchSessionMessages(port int, sessionID string) ([]byte, error) {
	if port > 0 {
		url := fmt.Sprintf("http://localhost:%d/session/%s/message", port, sessionID)
		if resp, err := opcodeStatusClient.Get(url); err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && readErr == nil {
				return body, nil
			}
			// Fall through to on-disk read if the live server can't answer.
		}
	}
	if body, err := readSessionHistorySQLite(sessionID); err == nil {
		return body, nil
	}
	return readSessionHistory(sessionID)
}

// turnInfo is the minimal shape decoded from a message's `info` JSON.
type turnInfo struct {
	Role string `json:"role"`
	Time struct {
		Created   float64 `json:"created"`
		Completed float64 `json:"completed"`
	} `json:"time"`
}

// turnPartTime is the tolerant shape decoded from a message part's JSON.
type turnPartTime struct {
	Time struct {
		Created   float64 `json:"created"`
		Completed float64 `json:"completed"`
	} `json:"time"`
}

// checkOpencodeTurnEnded probes a session's transcript to decide whether the
// newest assistant turn has actually completed, independent of the raw
// /session/status busy flag (which lingers busy after a question-tool turn).
//
//   - ended: the latest assistant message carries a non-zero time.completed.
//   - lastActivity: the newest message/part time seen (created or completed),
//     used by the stall timer.
//   - ok: false on any transport/decode failure or when no assistant message
//     exists yet — callers must treat !ok like "unavailable" and never flip
//     state on a blip.
//
// It reads the live server first (GET /session/{id}/message on port), then
// falls back to on-disk storage, mirroring BridgeClient.fetchSessionHistory.
func checkOpencodeTurnEnded(port int, sessionID string) (ended bool, lastActivity time.Time, ok bool) {
	raw, err := sessionHistoryForPort(port, sessionID)
	if err != nil || raw == nil {
		return false, time.Time{}, false
	}

	var msgs []messageWithParts
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return false, time.Time{}, false
	}

	var (
		haveAssistant     bool
		latestAssistant   turnInfo
		latestAssistantAt float64 // Time.Created of the newest assistant msg
		newest            time.Time
	)

	bump := func(v float64) {
		if v <= 0 {
			return
		}
		t := time.UnixMilli(int64(v)).UTC()
		if t.After(newest) {
			newest = t
		}
	}

	for _, m := range msgs {
		var info turnInfo
		if err := json.Unmarshal(m.Info, &info); err == nil {
			bump(info.Time.Created)
			bump(info.Time.Completed)
			if info.Role == "assistant" {
				if !haveAssistant || info.Time.Created >= latestAssistantAt {
					haveAssistant = true
					latestAssistant = info
					latestAssistantAt = info.Time.Created
				}
			}
		}
		for _, p := range m.Parts {
			var pt turnPartTime
			if err := json.Unmarshal(p, &pt); err == nil {
				bump(pt.Time.Created)
				bump(pt.Time.Completed)
			}
		}
	}

	if !haveAssistant {
		return false, time.Time{}, false
	}

	ended = latestAssistant.Time.Completed > 0
	return ended, newest, true
}
