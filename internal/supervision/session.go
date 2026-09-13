// Package supervision contains bounded, read-only supervisor projections.
package supervision

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// SessionRecord is an upsert keyed by message and part, not an append-only log.
// Streaming parts can change between reads; the cursor rechecks the last part.
type SessionRecord struct {
	ID        string          `json:"id"`
	MessageID string          `json:"message_id"`
	Kind      string          `json:"kind"`
	Role      string          `json:"role,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Status    string          `json:"status,omitempty"`
	Text      string          `json:"text,omitempty"`
	Time      json.RawMessage `json:"time,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}
type SessionPage struct {
	Records       []SessionRecord `json:"records"`
	NextCursor    string          `json:"next_cursor"`
	Truncated     bool            `json:"truncated"`
	CursorExpired bool            `json:"cursor_expired"`
}
type sessionCursor struct {
	Scope  string
	ID     string
	Prefix string
	Hash   string
}

var credential = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|access[_-]?token|password|secret)\s*[=:]\s*["']?)[^\s"',;]+`)
var privateKey = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)

func Redact(s string) string {
	return credential.ReplaceAllString(privateKey.ReplaceAllString(s, "[REDACTED PRIVATE KEY]"), "${1}[REDACTED]")
}
func recordHash(r SessionRecord) string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func encodeCursor(c sessionCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SessionTail excludes reasoning, tool inputs, metadata and arbitrary executor
// fields. Only visible text and tool output/status enter the projection.
func SessionTail(raw []byte, scope, after string, limit, maxBytes int) (SessionPage, error) {
	page := SessionPage{Records: []SessionRecord{}}
	if limit < 1 || limit > 100 || maxBytes < 1024 || maxBytes > 65536 {
		return page, fmt.Errorf("limit must be 1..100 and max_bytes 1024..65536")
	}
	var messages []struct {
		Info struct {
			ID   string          `json:"id"`
			Role string          `json:"role"`
			Time json.RawMessage `json:"time"`
		} `json:"info"`
		Parts []struct {
			ID    string          `json:"id"`
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Tool  string          `json:"tool"`
			Time  json.RawMessage `json:"time"`
			State struct {
				Status string `json:"status"`
				Output string `json:"output"`
			} `json:"state"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return page, fmt.Errorf("invalid session history: %w", err)
	}
	records := []SessionRecord{}
	for _, m := range messages {
		for _, p := range m.Parts {
			if p.Type != "text" && p.Type != "tool" {
				continue
			}
			if m.Info.ID == "" || p.ID == "" {
				continue
			}
			text := p.Text
			if p.Type == "tool" {
				text = p.State.Output
			}
			tm := p.Time
			if len(tm) == 0 {
				tm = m.Info.Time
			}
			records = append(records, SessionRecord{ID: m.Info.ID + ":" + p.ID, MessageID: m.Info.ID, Kind: p.Type, Role: m.Info.Role, Tool: p.Tool, Status: p.State.Status, Text: Redact(text), Time: visibleTime(tm)})
		}
	}
	hashes := make([]string, len(records))
	prefixes := make([]string, len(records))
	digest := sha256.New()
	for i, r := range records {
		hashes[i] = recordHash(r)
		prefixes[i] = hex.EncodeToString(digest.Sum(nil))
		_, _ = digest.Write([]byte(hashes[i]))
	}
	start := max(0, len(records)-limit)
	if after != "" {
		var c sessionCursor
		b, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || len(b) > 2048 || json.Unmarshal(b, &c) != nil || c.Scope != scope {
			return page, fmt.Errorf("invalid cursor for this session")
		}
		found := false
		for i, r := range records {
			if r.ID == c.ID {
				start = i
				if c.Prefix != "" && c.Prefix != prefixes[i] {
					start = 0
					page.CursorExpired = true
				} else if hashes[i] == c.Hash {
					start++
				}
				found = true
				break
			}
		}
		page.CursorExpired = page.CursorExpired || !found
		page.NextCursor = after
	}
	page.Truncated = start > 0 && after == ""
	// Reserve JSON envelope/cursor overhead. Text is clipped at UTF-8 boundaries;
	// hashes bind the unabridged record so clipping does not cause repeat reads.
	used := 512
	for i := start; i < len(records); i++ {
		if len(page.Records) == limit {
			page.Truncated = true
			break
		}
		r := records[i]
		hash := hashes[i]
		b, _ := json.Marshal(r)
		if used+len(b) > maxBytes {
			if len(page.Records) > 0 {
				page.Truncated = true
				break
			}
			for len(r.Text) > 0 && used+len(b) > maxBytes {
				n := len(r.Text) / 2
				for n > 0 && !utf8.ValidString(r.Text[:n]) {
					n--
				}
				r.Text = r.Text[:n]
				r.Truncated = true
				b, _ = json.Marshal(r)
			}
			if used+len(b) > maxBytes {
				return page, fmt.Errorf("session record metadata exceeds byte bound")
			}
			page.Truncated = true
		}
		page.Records = append(page.Records, r)
		used += len(b)
		page.NextCursor = encodeCursor(sessionCursor{Scope: scope, ID: r.ID, Hash: hash, Prefix: prefixes[i]})
	}
	if strings.TrimSpace(string(raw)) == "null" {
		page.Records = []SessionRecord{}
	}
	for {
		encoded, _ := json.Marshal(page)
		if len(encoded) <= maxBytes {
			break
		}
		if len(page.Records) == 0 {
			return page, fmt.Errorf("cursor exceeds byte bound")
		}
		last := &page.Records[len(page.Records)-1]
		if len(last.Text) == 0 {
			return page, fmt.Errorf("session metadata exceeds byte bound")
		}
		n := len(last.Text) / 2
		for n > 0 && !utf8.ValidString(last.Text[:n]) {
			n--
		}
		last.Text = last.Text[:n]
		last.Truncated = true
		page.Truncated = true
	}
	return page, nil
}

// Executor timestamps have a small allowlist; arbitrary embedded fields cannot
// bypass the visible-output privacy or byte budget.
func visibleTime(raw json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	out := map[string]float64{}
	for _, key := range []string{"created", "completed", "start", "end"} {
		var value float64
		if data, ok := fields[key]; ok && json.Unmarshal(data, &value) == nil {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}
