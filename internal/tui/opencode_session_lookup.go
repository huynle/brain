package tui

import (
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
	_ "github.com/glebarez/go-sqlite"
)

func localOpenCodeSessionIDForTask(task types.BrainEntry) string {
	dbPath := localOpenCodeDBPath()
	if dbPath == "" {
		return ""
	}
	sessionID, _ := lookupOpenCodeSessionIDForTask(task, dbPath)
	return sessionID
}

func localOpenCodeDBPath() string {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "opencode", "opencode.db")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func lookupOpenCodeSessionIDForTask(task types.BrainEntry, dbPath string) (string, error) {
	needle := task.DirectPrompt
	if needle == "" {
		needle = task.Content
	}
	needle = sessionLookupNeedle(needle)
	if needle == "" || dbPath == "" {
		return "", nil
	}
	matchNeedle := needle
	if len(matchNeedle) > 80 {
		matchNeedle = matchNeedle[:80]
	}

	createdMs := parseTimeMillis(task.Created)
	modifiedMs := parseTimeMillis(task.Modified)
	startMs, endMs := sessionLookupWindow(createdMs, modifiedMs)

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT s.id, s.time_created, p.data
		FROM session s
		JOIN part p ON p.session_id = s.id
		WHERE s.time_created BETWEEN ? AND ?
		ORDER BY s.time_created DESC
	`, startMs, endMs)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	bestSessionID := ""
	bestDistance := int64(math.MaxInt64)
	for rows.Next() {
		var sessionID string
		var sessionCreated int64
		var data string
		if err := rows.Scan(&sessionID, &sessionCreated, &data); err != nil {
			return "", err
		}
		if !sessionPartMatchesNeedle(data, matchNeedle) {
			continue
		}
		distance := int64(0)
		if createdMs > 0 {
			distance = absInt64(sessionCreated - createdMs)
		}
		if bestSessionID == "" || distance < bestDistance {
			bestSessionID = sessionID
			bestDistance = distance
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return bestSessionID, nil
}

func sessionLookupWindow(createdMs, modifiedMs int64) (int64, int64) {
	const paddingMs = int64(10 * time.Minute / time.Millisecond)
	if createdMs == 0 && modifiedMs == 0 {
		nowMs := time.Now().UnixMilli()
		return nowMs - int64(30*24*time.Hour/time.Millisecond), nowMs
	}
	start := createdMs
	if start == 0 || (modifiedMs > 0 && modifiedMs < start) {
		start = modifiedMs
	}
	end := modifiedMs
	if end == 0 || createdMs > end {
		end = createdMs
	}
	return start - paddingMs, end + paddingMs
}

func sessionLookupNeedle(s string) string {
	s = normalizeSessionText(s)
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.TrimSpace(s)
}

func sessionPartMatchesNeedle(data, needle string) bool {
	var part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(data), &part); err != nil {
		return false
	}
	if part.Type != "text" || part.Text == "" {
		return false
	}
	return strings.Contains(normalizeSessionText(part.Text), needle)
}

func normalizeSessionText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func parseTimeMillis(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
