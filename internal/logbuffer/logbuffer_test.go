package logbuffer

import (
	"fmt"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func makeLine(content string) types.LogLine {
	return types.LogLine{
		Timestamp: "2025-01-01T00:00:00Z",
		Level:     "info",
		Content:   content,
	}
}

func makeLines(prefix string, count int) []types.LogLine {
	lines := make([]types.LogLine, count)
	for i := range lines {
		lines[i] = makeLine(fmt.Sprintf("%s-%d", prefix, i))
	}
	return lines
}

func TestNew_DefaultMaxLines(t *testing.T) {
	b := New(0)
	if b.maxLines != DefaultMaxLines {
		t.Errorf("expected maxLines=%d, got %d", DefaultMaxLines, b.maxLines)
	}

	b2 := New(-1)
	if b2.maxLines != DefaultMaxLines {
		t.Errorf("expected maxLines=%d, got %d", DefaultMaxLines, b2.maxLines)
	}
}

func TestNew_CustomMaxLines(t *testing.T) {
	b := New(500)
	if b.maxLines != 500 {
		t.Errorf("expected maxLines=500, got %d", b.maxLines)
	}
}

func TestAppend_BasicIngest(t *testing.T) {
	b := New(100)
	lines := makeLines("line", 3)

	accepted := b.Append("proj1", "task1", lines)
	if accepted != 3 {
		t.Errorf("expected accepted=3, got %d", accepted)
	}

	result, total := b.Query("proj1", "task1", 0, 100)
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 lines, got %d", len(result))
	}
}

func TestAppend_EmptyLines(t *testing.T) {
	b := New(100)
	accepted := b.Append("proj1", "task1", nil)
	if accepted != 0 {
		t.Errorf("expected accepted=0, got %d", accepted)
	}

	accepted = b.Append("proj1", "task1", []types.LogLine{})
	if accepted != 0 {
		t.Errorf("expected accepted=0, got %d", accepted)
	}
}

func TestAppend_BoundedEviction(t *testing.T) {
	b := New(5)

	// Fill with 5 lines
	b.Append("proj1", "task1", makeLines("old", 5))

	// Add 3 more — should evict oldest 3
	b.Append("proj1", "task1", makeLines("new", 3))

	result, total := b.Query("proj1", "task1", 0, 100)
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(result) != 5 {
		t.Errorf("expected 5 lines, got %d", len(result))
	}

	// First line should be old-2 (oldest 3 evicted: old-0, old-1, old-2... wait)
	// After 5 old lines + 3 new lines = 8, evict 3 oldest -> keep old-3, old-4, new-0, new-1, new-2
	if result[0].Content != "old-3" {
		t.Errorf("expected first line content='old-3', got '%s'", result[0].Content)
	}
	if result[4].Content != "new-2" {
		t.Errorf("expected last line content='new-2', got '%s'", result[4].Content)
	}
}

func TestAppend_LargerThanBuffer(t *testing.T) {
	b := New(3)

	// Append 10 lines to a buffer of size 3
	b.Append("proj1", "task1", makeLines("line", 10))

	result, total := b.Query("proj1", "task1", 0, 100)
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if result[0].Content != "line-7" {
		t.Errorf("expected first line content='line-7', got '%s'", result[0].Content)
	}
}

func TestQuery_SeparateTasks(t *testing.T) {
	b := New(100)

	b.Append("proj1", "task1", makeLines("t1", 3))
	b.Append("proj1", "task2", makeLines("t2", 5))

	result1, total1 := b.Query("proj1", "task1", 0, 100)
	result2, total2 := b.Query("proj1", "task2", 0, 100)

	if total1 != 3 || len(result1) != 3 {
		t.Errorf("task1: expected 3 lines, got %d (total=%d)", len(result1), total1)
	}
	if total2 != 5 || len(result2) != 5 {
		t.Errorf("task2: expected 5 lines, got %d (total=%d)", len(result2), total2)
	}
}

func TestQuery_Pagination(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 10))

	// Get first 3
	result, total := b.Query("proj1", "task1", 0, 3)
	if total != 10 {
		t.Errorf("expected total=10, got %d", total)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 lines, got %d", len(result))
	}
	if result[0].Content != "line-0" {
		t.Errorf("expected first='line-0', got '%s'", result[0].Content)
	}

	// Get lines 5-9
	result, _ = b.Query("proj1", "task1", 5, 100)
	if len(result) != 5 {
		t.Errorf("expected 5 lines from offset 5, got %d", len(result))
	}
	if result[0].Content != "line-5" {
		t.Errorf("expected first='line-5', got '%s'", result[0].Content)
	}
}

func TestQuery_OffsetBeyondTotal(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 3))

	result, total := b.Query("proj1", "task1", 100, 10)
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 lines, got %d", len(result))
	}
}

func TestQuery_NonexistentTask(t *testing.T) {
	b := New(100)

	result, total := b.Query("proj1", "nonexistent", 0, 100)
	if total != 0 {
		t.Errorf("expected total=0, got %d", total)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestQuery_NegativeOffset(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 5))

	result, total := b.Query("proj1", "task1", -5, 100)
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(result) != 5 {
		t.Errorf("expected 5 lines (negative offset treated as 0), got %d", len(result))
	}
}

func TestTail_FewerLinesThanLimit(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 3))

	result, total, offset := b.Tail("proj1", "task1", 10)
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if offset != 0 {
		t.Errorf("expected offset=0 when buffer is shorter than limit, got %d", offset)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(result))
	}
	if result[0].Content != "line-0" || result[2].Content != "line-2" {
		t.Errorf("expected oldest→newest line-0..line-2, got %q..%q",
			result[0].Content, result[2].Content)
	}
}

func TestTail_MoreLinesThanLimit_ReturnsTailNotHead(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 50))

	result, total, offset := b.Tail("proj1", "task1", 10)
	if total != 50 {
		t.Errorf("expected total=50, got %d", total)
	}
	if offset != 40 {
		t.Errorf("expected offset=40 (50-10), got %d", offset)
	}
	if len(result) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(result))
	}
	// The bug this guards: returning the OLDEST 10 (line-0..line-9).
	if result[0].Content != "line-40" {
		t.Errorf("expected first='line-40' (tail), got %q", result[0].Content)
	}
	if result[9].Content != "line-49" {
		t.Errorf("expected last='line-49' (newest), got %q", result[9].Content)
	}
	// Ordering contract: oldest→newest within the window.
	for i := range result {
		want := fmt.Sprintf("line-%d", 40+i)
		if result[i].Content != want {
			t.Fatalf("line %d: expected %q, got %q", i, want, result[i].Content)
		}
	}
}

func TestTail_ExactlyLimit(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 10))

	result, total, offset := b.Tail("proj1", "task1", 10)
	if total != 10 || offset != 0 || len(result) != 10 {
		t.Fatalf("expected total=10 offset=0 len=10, got total=%d offset=%d len=%d",
			total, offset, len(result))
	}
	if result[0].Content != "line-0" {
		t.Errorf("expected first='line-0', got %q", result[0].Content)
	}
}

func TestTail_AfterEviction(t *testing.T) {
	b := New(10)
	b.Append("proj1", "task1", makeLines("line", 25))

	// Ring holds only the newest 10 (line-15..line-24).
	result, total, offset := b.Tail("proj1", "task1", 5)
	if total != 10 {
		t.Errorf("expected total=10 (ring cap), got %d", total)
	}
	if offset != 5 {
		t.Errorf("expected offset=5 within the retained window, got %d", offset)
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(result))
	}
	if result[0].Content != "line-20" || result[4].Content != "line-24" {
		t.Errorf("expected line-20..line-24, got %q..%q",
			result[0].Content, result[4].Content)
	}
}

func TestTail_NonexistentTask(t *testing.T) {
	b := New(100)

	result, total, offset := b.Tail("proj1", "nonexistent", 10)
	if total != 0 || offset != 0 {
		t.Errorf("expected total=0 offset=0, got total=%d offset=%d", total, offset)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestTail_NonPositiveLimit(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 5))

	for _, limit := range []int{0, -1} {
		result, total, offset := b.Tail("proj1", "task1", limit)
		if len(result) != 0 {
			t.Errorf("limit=%d: expected 0 lines, got %d", limit, len(result))
		}
		if total != 5 {
			t.Errorf("limit=%d: expected total=5, got %d", limit, total)
		}
		if offset != 0 {
			t.Errorf("limit=%d: expected offset=0, got %d", limit, offset)
		}
	}
}

func TestTail_ReturnsCopy(t *testing.T) {
	b := New(100)
	b.Append("proj1", "task1", makeLines("line", 5))

	result, _, _ := b.Tail("proj1", "task1", 5)
	result[0].Content = "mutated"

	again, _, _ := b.Tail("proj1", "task1", 5)
	if again[0].Content != "line-0" {
		t.Errorf("Tail leaked the underlying slice: got %q", again[0].Content)
	}
}

func TestConcurrentAppendQuery(t *testing.T) {
	b := New(100)
	var wg sync.WaitGroup

	// Concurrent appends
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Append("proj1", "task1", makeLines(fmt.Sprintf("goroutine-%d", i), 10))
		}(i)
	}

	// Concurrent queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Query("proj1", "task1", 0, 50)
		}()
	}

	wg.Wait()

	// Should have at most 100 lines (maxLines)
	_, total := b.Query("proj1", "task1", 0, 200)
	if total > 100 {
		t.Errorf("expected total <= 100, got %d", total)
	}
}
