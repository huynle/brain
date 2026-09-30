package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// sessionHistoryForPort fetches a session's transcript as a JSON array of
// {info,parts} messages (the runner's internal shape, matching the on-disk
// readers and what the frontend transcript expects). Indirected for tests.
var sessionHistoryForPort = fetchSessionMessages

// v2Message is the flat message shape opencode v2 returns under
// GET /api/session/{id}/message .data[]. The discriminator is `type` (was
// `role` in the internal shape); assistant content parts are inline in
// `content` (v2 has no separate part rows).
type v2Message struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Time struct {
		Created   float64 `json:"created"`
		Streamed  float64 `json:"streamed"`
		Completed float64 `json:"completed"`
	} `json:"time"`
	Content []json.RawMessage `json:"content"`
}

// v2MessagesEnvelope is the v2 GET /api/session/{id}/message response wrapper.
type v2MessagesEnvelope struct {
	Data   []json.RawMessage `json:"data"`
	Cursor struct {
		Previous *string `json:"previous"`
		Next     *string `json:"next"`
	} `json:"cursor"`
}

// normalizeV2Messages converts one page of the v2 GET /api/session/{id}/message
// response ({"data":[flat messages],"cursor":{…}}) into the runner's internal
// []messageWithParts JSON. Each v2 message's `type` becomes info.role and its
// inline `content` array becomes parts, so turn-end detection and the frontend
// transcript keep working against the pre-v2 {info,parts} shape.
func normalizeV2Messages(raw []byte) ([]byte, error) {
	msgs, _, err := normalizeV2Page(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(msgs)
}

// normalizeV2Page decodes one v2 message page into internal messageWithParts
// plus the cursor's next token (empty when there are no more pages).
func normalizeV2Page(raw []byte) ([]messageWithParts, string, error) {
	var env v2MessagesEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, "", fmt.Errorf("decode v2 messages: %w", err)
	}
	out := make([]messageWithParts, 0, len(env.Data))
	for _, d := range env.Data {
		var m v2Message
		if err := json.Unmarshal(d, &m); err != nil {
			return nil, "", fmt.Errorf("decode v2 message: %w", err)
		}
		// Build the internal info object: carry id + time verbatim and map
		// the v2 `type` discriminator onto the legacy `role` field that
		// turn-end detection and the frontend read.
		info := map[string]json.RawMessage{}
		if err := json.Unmarshal(d, &info); err != nil {
			return nil, "", fmt.Errorf("decode v2 message info: %w", err)
		}
		info["role"] = mustJSON(m.Type)
		infoBytes, err := json.Marshal(info)
		if err != nil {
			return nil, "", err
		}
		parts := m.Content
		if parts == nil {
			parts = []json.RawMessage{}
		}
		out = append(out, messageWithParts{Info: infoBytes, Parts: parts})
	}
	next := ""
	if env.Cursor.Next != nil {
		next = *env.Cursor.Next
	}
	return out, next, nil
}

func mustJSON(v string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// fetchSessionMessages returns a session's transcript as a JSON array of
// {info,parts} messages. It queries the live v2 server first
// (GET /api/session/{id}/message with Basic auth, following cursor pagination
// and normalizing the v2 shape), then falls back to on-disk storage (SQLite,
// then the file-per-message layout), mirroring
// BridgeClient.fetchSessionHistory's live-then-disk precedence.
func fetchSessionMessages(port int, sessionID string, password string) ([]byte, error) {
	if port > 0 {
		if body, err := fetchLiveSessionMessagesV2(port, sessionID, password); err == nil {
			return body, nil
		}
		// Fall through to on-disk read if the live server can't answer.
	}
	if body, err := readSessionHistorySQLite(sessionID); err == nil {
		return body, nil
	}
	return readSessionHistory(sessionID)
}

// fetchLiveSessionMessagesV2 reads all pages of a session's messages from the
// live v2 server and returns them normalized into the internal
// []messageWithParts JSON (oldest-first). Pages are fetched with order=asc so
// concatenation preserves chronological order; the cursor.next token drives
// pagination, bounded so a runaway cursor can't loop forever.
func fetchLiveSessionMessagesV2(port int, sessionID string, password string) ([]byte, error) {
	c := newLocalOpenCodeClient(port, password)
	var all []messageWithParts
	cursor := ""
	const maxPages = 1000
	for page := 0; page < maxPages; page++ {
		path := "/session/" + sessionID + "/message?order=asc"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(context.Background(), http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := opcodeStatusClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil {
			return nil, fmt.Errorf("GET %s: status %d", c.url(path), resp.StatusCode)
		}
		msgs, next, err := normalizeV2Page(body)
		if err != nil {
			return nil, err
		}
		all = append(all, msgs...)
		if next == "" {
			break
		}
		cursor = next
	}
	return json.Marshal(all)
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
func checkOpencodeTurnEnded(port int, sessionID string, password string) (ended bool, lastActivity time.Time, ok bool) {
	raw, err := sessionHistoryForPort(port, sessionID, password)
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
