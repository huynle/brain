// Package logbuffer provides a bounded in-memory log buffer for task logs.
// Each task has its own ring of log lines capped at a configurable maximum.
package logbuffer

import (
	"sync"

	"github.com/huynle/brain-api/internal/types"
)

// DefaultMaxLines is the default maximum number of log lines per task.
const DefaultMaxLines = 1000

// Buffer stores recent log lines per task with bounded memory.
type Buffer struct {
	mu       sync.RWMutex
	maxLines int
	// tasks maps "projectId:taskId" to a slice of log lines.
	tasks map[string][]types.LogLine
}

// New creates a new log buffer with the given max lines per task.
// If maxLines <= 0, DefaultMaxLines is used.
func New(maxLines int) *Buffer {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	return &Buffer{
		maxLines: maxLines,
		tasks:    make(map[string][]types.LogLine),
	}
}

// taskKey returns the composite key for a task's log buffer.
func taskKey(projectId, taskId string) string {
	return projectId + ":" + taskId
}

// Append adds log lines to a task's buffer, evicting oldest lines if the buffer exceeds maxLines.
// Returns the number of lines accepted.
func (b *Buffer) Append(projectId, taskId string, lines []types.LogLine) int {
	if len(lines) == 0 {
		return 0
	}

	key := taskKey(projectId, taskId)

	b.mu.Lock()
	defer b.mu.Unlock()

	existing := b.tasks[key]
	existing = append(existing, lines...)

	// Evict oldest lines if over capacity
	if len(existing) > b.maxLines {
		existing = existing[len(existing)-b.maxLines:]
	}

	b.tasks[key] = existing
	return len(lines)
}

// Query returns log lines for a task with head-indexed offset/limit pagination:
// the window starts at offset lines from the OLDEST retained line.
// Returns the lines (ordered oldest→newest) and the total retained count.
//
// Callers that supply only a limit almost always mean the newest lines, not the
// oldest — use Tail for that.
func (b *Buffer) Query(projectId, taskId string, offset, limit int) ([]types.LogLine, int) {
	key := taskKey(projectId, taskId)

	b.mu.RLock()
	defer b.mu.RUnlock()

	lines := b.tasks[key]
	total := len(lines)

	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return nil, total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	// Return a copy to avoid data races
	result := make([]types.LogLine, end-offset)
	copy(result, lines[offset:end])

	return result, total
}

// Tail returns the NEWEST limit log lines for a task, still ordered
// oldest→newest within the returned window. It returns the lines, the total
// retained count, and the offset of the first returned line within the retained
// window (0 when the buffer holds fewer lines than the limit), so callers can
// report a truthful offset and page backwards from the tail.
//
// This is the semantic a caller means when it passes a limit and no offset:
// "show me the end of the log". The ring evicts oldest-first, so Query(0, limit)
// on a long-running task returns a stale prefix instead.
func (b *Buffer) Tail(projectId, taskId string, limit int) ([]types.LogLine, int, int) {
	key := taskKey(projectId, taskId)

	b.mu.RLock()
	defer b.mu.RUnlock()

	lines := b.tasks[key]
	total := len(lines)

	if limit <= 0 || total == 0 {
		return nil, total, 0
	}

	offset := total - limit
	if offset < 0 {
		offset = 0
	}

	// Return a copy to avoid data races
	result := make([]types.LogLine, total-offset)
	copy(result, lines[offset:total])

	return result, total, offset
}
