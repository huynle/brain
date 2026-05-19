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

// Query returns log lines for a task with offset/limit pagination.
// Returns the lines, total count, and the actual offset used.
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
