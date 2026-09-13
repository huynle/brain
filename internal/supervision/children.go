package supervision

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type Child struct {
	SessionID    string  `json:"session_id"`
	ParentID     string  `json:"parent_id"`
	Executor     string  `json:"executor"`
	State        string  `json:"state"`
	LastActivity *int64  `json:"last_activity"`
	Created      int64   `json:"created,omitempty"`
	Children     []Child `json:"children,omitempty"`
}
type ChildPage struct {
	Children      []Child `json:"children"`
	NextCursor    string  `json:"next_cursor,omitempty"`
	Truncated     bool    `json:"truncated"`
	CursorExpired bool    `json:"cursor_expired"`
}

// ChildrenPage flattens persisted linkage; it never invents live execution
// status or uses creation time as last activity. Changed trees expire cursors.
func ChildrenPage(raw []byte, scope, after string, limit int) (ChildPage, error) {
	p := ChildPage{Children: []Child{}}
	if limit < 1 || limit > 100 {
		return p, fmt.Errorf("limit must be 1..100")
	}
	var roots []Child
	if err := json.Unmarshal(raw, &roots); err != nil {
		return p, err
	}
	all := []Child{}
	seen := map[string]bool{}
	var walk func([]Child, int)
	walk = func(nodes []Child, depth int) {
		if depth > 5 {
			return
		}
		for _, c := range nodes {
			if seen[c.SessionID] || len(c.SessionID) > 128 || len(c.ParentID) > 128 {
				continue
			}
			seen[c.SessionID] = true
			walk(c.Children, depth+1)
			c.Children = nil
			c.Executor = "opencode"
			c.State = "unknown"
			c.LastActivity = nil
			all = append(all, c)
		}
	}
	walk(roots, 1)
	sort.Slice(all, func(i, j int) bool { return all[i].SessionID < all[j].SessionID })
	b, _ := json.Marshal(all)
	hash := sha256.Sum256(b)
	digest := hex.EncodeToString(hash[:])
	start := 0
	if after != "" {
		var c sessionCursor
		b, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || len(b) > 2048 || json.Unmarshal(b, &c) != nil || c.Scope != scope {
			return p, fmt.Errorf("invalid children cursor")
		}
		if c.Hash != digest {
			p.CursorExpired = true
		} else {
			for start < len(all) && all[start].SessionID <= c.ID {
				start++
			}
		}
	}
	end := min(start+limit, len(all))
	p.Children = all[start:end]
	p.Truncated = end < len(all)
	if end > start {
		p.NextCursor = encodeCursor(sessionCursor{Scope: scope, ID: all[end-1].SessionID, Hash: digest})
	}
	return p, nil
}
