package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Read OpenCode session transcripts straight from disk.
//
// OpenCode persists every session under its data dir, independent of any
// running server: messages at storage/message/<sessionID>/<messageID>.json and
// their parts at storage/part/<messageID>/<partID>.json. This lets remote
// control review a completed session after its server process has exited.
//
// The output is shaped to match GET /session/:id/message — an array of
// {info, parts} ordered oldest-first — so the browser renders it identically
// to a live transcript.

// opencodeDataDir returns OpenCode's data directory, resolved the same way
// OpenCode does (XDG_DATA_HOME or ~/.local/share, then /opencode).
func opencodeDataDir() (string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "opencode"), nil
}

// opencodeStorageDir returns OpenCode's on-disk storage directory (pre-1.x
// file-per-message layout).
func opencodeStorageDir() (string, error) {
	dataDir, err := opencodeDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "storage"), nil
}

// messageWithParts is the GET /session/:id/message element shape.
type messageWithParts struct {
	Info  json.RawMessage   `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

// readSessionHistory assembles a session's transcript from on-disk storage.
func readSessionHistory(sessionID string) ([]byte, error) {
	if strings.ContainsAny(sessionID, "/\\") || sessionID == "" {
		return nil, fmt.Errorf("invalid session id %q", sessionID)
	}
	storage, err := opencodeStorageDir()
	if err != nil {
		return nil, err
	}

	msgDir := filepath.Join(storage, "message", sessionID)
	entries, err := os.ReadDir(msgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session %s not found on this runner", sessionID)
		}
		return nil, fmt.Errorf("read messages: %w", err)
	}

	type loaded struct {
		created float64
		id      string
		msg     messageWithParts
	}
	loadedMsgs := make([]loaded, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(msgDir, e.Name()))
		if err != nil {
			continue
		}
		var meta struct {
			ID   string `json:"id"`
			Time struct {
				Created float64 `json:"created"`
			} `json:"time"`
		}
		_ = json.Unmarshal(raw, &meta)
		msgID := meta.ID
		if msgID == "" {
			msgID = strings.TrimSuffix(e.Name(), ".json")
		}

		loadedMsgs = append(loadedMsgs, loaded{
			created: meta.Time.Created,
			id:      msgID,
			msg: messageWithParts{
				Info:  json.RawMessage(raw),
				Parts: readMessageParts(storage, msgID),
			},
		})
	}

	// Oldest-first: by creation time, falling back to monotonic message id.
	sort.SliceStable(loadedMsgs, func(i, j int) bool {
		if loadedMsgs[i].created != loadedMsgs[j].created {
			return loadedMsgs[i].created < loadedMsgs[j].created
		}
		return loadedMsgs[i].id < loadedMsgs[j].id
	})

	out := make([]messageWithParts, 0, len(loadedMsgs))
	for _, m := range loadedMsgs {
		out = append(out, m.msg)
	}
	return json.Marshal(out)
}

// readMessageParts loads a message's parts from disk, ordered by part id.
func readMessageParts(storage, messageID string) []json.RawMessage {
	partDir := filepath.Join(storage, "part", messageID)
	entries, err := os.ReadDir(partDir)
	if err != nil {
		return []json.RawMessage{}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	parts := make([]json.RawMessage, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(partDir, name))
		if err != nil {
			continue
		}
		parts = append(parts, json.RawMessage(raw))
	}
	return parts
}
