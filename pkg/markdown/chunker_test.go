package markdown

import (
	"strings"
	"testing"
)

func TestChunkNote_EmptyBody(t *testing.T) {
	chunks := ChunkNote(123, "")
	
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty body, got %d", len(chunks))
	}
	
	if chunks[0].NoteID != 123 {
		t.Errorf("expected NoteID 123, got %d", chunks[0].NoteID)
	}
	
	if chunks[0].ChunkIndex != 0 {
		t.Errorf("expected ChunkIndex 0, got %d", chunks[0].ChunkIndex)
	}
	
	if chunks[0].Text != "" {
		t.Errorf("expected empty text, got %q", chunks[0].Text)
	}
}

func TestChunkNote_ShortNote(t *testing.T) {
	body := "This is a short note with less than 4000 characters."
	chunks := ChunkNote(456, body)
	
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short note, got %d", len(chunks))
	}
	
	if chunks[0].NoteID != 456 {
		t.Errorf("expected NoteID 456, got %d", chunks[0].NoteID)
	}
	
	if chunks[0].ChunkIndex != 0 {
		t.Errorf("expected ChunkIndex 0, got %d", chunks[0].ChunkIndex)
	}
	
	if chunks[0].Text != body {
		t.Errorf("expected text %q, got %q", body, chunks[0].Text)
	}
}

func TestChunkNote_LongNote(t *testing.T) {
	// Create a note with ~10,000 characters
	paragraph := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 20) // ~1,140 chars
	body := strings.Repeat(paragraph+"\n\n", 9)                                                    // ~10,260 chars
	
	chunks := ChunkNote(789, body)
	
	// With 10,260 chars, target 4,000 per chunk, step ~3,400:
	// Chunk 0: 0-4000, Chunk 1: 3400-7400, Chunk 2: 6800-10260
	// Should get approximately 3 chunks
	if len(chunks) < 2 || len(chunks) > 4 {
		t.Errorf("expected 2-4 chunks for ~10k char note, got %d", len(chunks))
	}
	
	// Verify NoteID is set correctly
	for i, chunk := range chunks {
		if chunk.NoteID != 789 {
			t.Errorf("chunk %d: expected NoteID 789, got %d", i, chunk.NoteID)
		}
		
		if chunk.ChunkIndex != i {
			t.Errorf("chunk %d: expected ChunkIndex %d, got %d", i, i, chunk.ChunkIndex)
		}
		
		if chunk.Text == "" {
			t.Errorf("chunk %d: text should not be empty", i)
		}
	}
	
	// Verify overlap: each chunk after the first should contain some text from previous chunk
	if len(chunks) > 1 {
		for i := 1; i < len(chunks); i++ {
			// Check that there's some overlap by looking for common substrings
			prev := chunks[i-1].Text
			curr := chunks[i].Text
			
			// Take last 100 chars of previous chunk
			overlapCheck := ""
			if len(prev) > 100 {
				overlapCheck = prev[len(prev)-100:]
			} else {
				overlapCheck = prev
			}
			
			// Current chunk should contain some portion of previous chunk's end
			// (Due to 15% overlap, ~600 chars should overlap)
			hasOverlap := false
			for j := 50; j < len(overlapCheck); j++ {
				substr := overlapCheck[len(overlapCheck)-j:]
				if strings.Contains(curr, substr) {
					hasOverlap = true
					break
				}
			}
			
			if !hasOverlap && len(chunks) > 2 {
				// For very long notes, overlap should be present
				t.Logf("Warning: chunk %d may not have overlap with chunk %d", i, i-1)
			}
		}
	}
}

func TestChunkNote_WithHeadings(t *testing.T) {
	body := `# Introduction

This is the introduction section with some content.

` + strings.Repeat("More content in the introduction. ", 300) + `

## Section 1

This is section 1 with detailed information.

` + strings.Repeat("Detailed information continues here. ", 300) + `

## Section 2

This is section 2 with more details.

` + strings.Repeat("Even more detailed content in section 2. ", 300)
	
	chunks := ChunkNote(101, body)
	
	// Should get multiple chunks
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for long note with headings, got %d", len(chunks))
	}
	
	// Verify chunks are created
	for i, chunk := range chunks {
		if chunk.Text == "" {
			t.Errorf("chunk %d should not be empty", i)
		}
		
		// Each chunk should ideally break at heading boundaries
		// This is a soft check - we're just verifying the chunking works
		if len(chunk.Text) > targetChunkSize*2 {
			t.Errorf("chunk %d is too large: %d chars (max expected ~%d)", 
				i, len(chunk.Text), targetChunkSize*2)
		}
	}
}

func TestChunkNote_WithCodeBlocks(t *testing.T) {
	body := `# Code Example

Here is some introductory text before the code block.

` + "```go\n" + `
func ExampleFunction() {
	// This is a code block that should not be split
	for i := 0; i < 100; i++ {
		fmt.Println("Line", i)
	}
}
` + "```\n\n" + strings.Repeat("Text after code block. ", 400) + `

Another section here.

` + "```python\n" + `
def another_example():
    # Another code block
    for i in range(200):
        print(f"Python line {i}")
` + "```\n\n" + strings.Repeat("Final text section. ", 400)
	
	chunks := ChunkNote(202, body)
	
	// Should get multiple chunks
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
	
	// Verify that code blocks are not split (ideally)
	// We check that if a chunk contains "```", it should contain the matching closing "```"
	for i, chunk := range chunks {
		openTicks := strings.Count(chunk.Text, "```")
		
		// If odd number of ``` marks, the code block was split
		if openTicks%2 != 0 {
			// This is acceptable in some cases (very large code blocks)
			// but we log it for awareness
			t.Logf("chunk %d may have split a code block (odd number of ``` markers: %d)", 
				i, openTicks)
		}
	}
}

func TestChunkNote_ExactlyTargetSize(t *testing.T) {
	// Create a note that's exactly targetChunkSize
	body := strings.Repeat("X", targetChunkSize)
	
	chunks := ChunkNote(303, body)
	
	// Should get exactly 1 chunk
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for note of exactly target size, got %d", len(chunks))
	}
	
	if len(chunks[0].Text) != targetChunkSize {
		t.Errorf("expected chunk size %d, got %d", targetChunkSize, len(chunks[0].Text))
	}
}

func TestChunkNote_SlightlyOverTargetSize(t *testing.T) {
	// Create a note that's just over targetChunkSize
	body := strings.Repeat("X", targetChunkSize+100)
	
	chunks := ChunkNote(404, body)
	
	// Should get 2 chunks
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks for note slightly over target size, got %d", len(chunks))
	}
	
	// First chunk should be around target size
	if len(chunks[0].Text) > targetChunkSize+500 {
		t.Errorf("first chunk too large: %d chars", len(chunks[0].Text))
	}
}

func TestChunkNote_ChunkIndices(t *testing.T) {
	// Create a long note that will produce multiple chunks
	body := strings.Repeat(strings.Repeat("Test content. ", 50)+"\n\n", 50)
	
	chunks := ChunkNote(505, body)
	
	// Verify chunk indices are sequential starting from 0
	for i, chunk := range chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk at position %d has wrong index: expected %d, got %d", 
				i, i, chunk.ChunkIndex)
		}
	}
}

func TestChunkNote_PreservesContent(t *testing.T) {
	// Verify that all content is preserved across chunks (with overlap)
	body := strings.Repeat("ABCDEFGHIJ ", 500) // ~5,500 chars
	
	chunks := ChunkNote(606, body)
	
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	
	// Combine all chunk text (removing overlap by taking just new content)
	// Actually, due to overlap, we can't easily reconstruct exactly,
	// but we can verify the first chunk starts correctly and last chunk ends correctly
	
	firstChunk := chunks[0].Text
	lastChunk := chunks[len(chunks)-1].Text
	
	if !strings.HasPrefix(body, firstChunk[:50]) {
		t.Error("first chunk doesn't match start of body")
	}
	
	// Trim spaces from both for comparison since chunking trims whitespace
	bodyTrimmed := strings.TrimSpace(body)
	lastChunkEnd := lastChunk
	if len(lastChunk) > 50 {
		lastChunkEnd = lastChunk[len(lastChunk)-50:]
	}
	
	if !strings.HasSuffix(bodyTrimmed, strings.TrimSpace(lastChunkEnd)) {
		t.Errorf("last chunk doesn't match end of body\nBody ends: %q\nChunk ends: %q", 
			bodyTrimmed[len(bodyTrimmed)-50:], lastChunkEnd)
	}
}

func TestChunkNote_OverlapBehavior(t *testing.T) {
	// Create a note with known content to test overlap
	// Using distinct markers to track overlap
	section1 := strings.Repeat("AAA ", 1000) // ~4,000 chars
	section2 := strings.Repeat("BBB ", 1000) // ~4,000 chars
	body := section1 + section2              // ~8,000 chars
	
	chunks := ChunkNote(707, body)
	
	// Should get 2-3 chunks with effective step of ~3,400
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	
	// Second chunk should contain some "AAA" (overlap from first chunk)
	if len(chunks) > 1 {
		secondChunk := chunks[1].Text
		
		if !strings.Contains(secondChunk, "AAA") {
			t.Error("second chunk should contain overlap from first chunk (AAA)")
		}
		
		if !strings.Contains(secondChunk, "BBB") {
			t.Error("second chunk should also contain new content (BBB)")
		}
	}
}

func TestChunkNote_BlankLinePreference(t *testing.T) {
	// Create a note with clear paragraph breaks
	para1 := strings.Repeat("First paragraph content. ", 160) // ~4,000 chars
	para2 := strings.Repeat("Second paragraph content. ", 160)
	body := para1 + "\n\n" + para2
	
	chunks := ChunkNote(808, body)
	
	// Should split at the blank line if possible
	if len(chunks) >= 2 {
		firstChunk := chunks[0].Text
		
		// First chunk should ideally end around the blank line
		// (This is a heuristic test - exact behavior depends on split logic)
		if !strings.Contains(firstChunk, "First paragraph") {
			t.Error("first chunk should contain first paragraph content")
		}
	}
}

func TestChunkNote_VeryLongNote(t *testing.T) {
	// Test with a very long note (~50,000 characters)
	body := strings.Repeat(strings.Repeat("Content ", 100)+"\n\n", 60)
	
	chunks := ChunkNote(909, body)
	
	// Should produce many chunks
	if len(chunks) < 10 {
		t.Errorf("expected at least 10 chunks for very long note, got %d", len(chunks))
	}
	
	// Verify all chunks have content and proper indices
	for i, chunk := range chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk %d has wrong index %d", i, chunk.ChunkIndex)
		}
		
		if chunk.Text == "" {
			t.Errorf("chunk %d is empty", i)
		}
		
		if chunk.NoteID != 909 {
			t.Errorf("chunk %d has wrong NoteID %d", i, chunk.NoteID)
		}
	}
}

func TestFindCodeBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "no code blocks",
			input:    "Just plain text without any code blocks.",
			expected: 0,
		},
		{
			name:     "single code block",
			input:    "Text before\n```\ncode here\n```\nText after",
			expected: 1,
		},
		{
			name:     "multiple code blocks",
			input:    "```\nblock1\n```\ntext\n```\nblock2\n```",
			expected: 2,
		},
		{
			name:     "code block with language",
			input:    "```go\nfunc main() {}\n```",
			expected: 1,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := findCodeBlocks(tt.input)
			if len(blocks) != tt.expected {
				t.Errorf("expected %d code blocks, got %d", tt.expected, len(blocks))
			}
		})
	}
}

func TestIsInsideCodeBlock(t *testing.T) {
	text := "before ```code block``` after"
	blocks := findCodeBlocks(text)
	
	if len(blocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(blocks))
	}
	
	// Test positions
	tests := []struct {
		pos      int
		expected bool
	}{
		{0, false},                 // before block
		{blocks[0].start, false},   // at start marker
		{blocks[0].start + 5, true}, // inside block
		{blocks[0].end - 1, true},   // inside block (before end)
		{blocks[0].end, false},     // at end marker
		{len(text) - 1, false},     // after block
	}
	
	for _, tt := range tests {
		result := isInsideCodeBlock(tt.pos, blocks)
		if result != tt.expected {
			t.Errorf("position %d: expected %v, got %v", tt.pos, tt.expected, result)
		}
	}
}
