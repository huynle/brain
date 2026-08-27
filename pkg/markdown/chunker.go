// Package markdown provides markdown link extraction, checksum computation,
// word counting, lead extraction, and note utility functions.
package markdown

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Chunk represents a text chunk from a note, suitable for embedding.
type Chunk struct {
	NoteID     int64  // ID of the source note
	ChunkIndex int    // 0-based index of this chunk within the note
	Text       string // The chunk text content
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// Target chunk size in characters
	targetChunkSize = 4000
	
	// Overlap percentage (15% = 0.15)
	overlapPercent = 0.15
	
	// Effective step size (85% of target)
	effectiveStep = int(float64(targetChunkSize) * (1.0 - overlapPercent))
)

// ---------------------------------------------------------------------------
// Compiled regex patterns
// ---------------------------------------------------------------------------

var (
	// Matches markdown headings (# Header)
	headingPattern = regexp.MustCompile(`(?m)^#{1,6}\s+.+$`)
	
	// Matches fenced code blocks (```...```)
	fencedCodePattern = regexp.MustCompile("(?s)```[^`]*```")
	
	// Matches blank lines
	blankLinePattern = regexp.MustCompile(`(?m)^\s*$`)
)

// ---------------------------------------------------------------------------
// ChunkNote
// ---------------------------------------------------------------------------

// ChunkNote splits a note body into embedding-ready chunks.
// 
// Chunking rules:
// - Target ~4,000 characters per chunk
// - Use ~15% overlap between chunks (effective step ≈ 3,400 chars)
// - Prefer splitting on markdown headings and blank lines
// - Avoid splitting inside fenced code blocks where feasible
// - Always produce at least one chunk
//
// Returns a slice of Chunk structs, each containing the note ID,
// chunk index, and text content.
func ChunkNote(noteID int64, body string) []Chunk {
	if body == "" {
		// Always produce at least one chunk, even if empty
		return []Chunk{{
			NoteID:     noteID,
			ChunkIndex: 0,
			Text:       "",
		}}
	}

	// Find all code block boundaries to avoid splitting within them
	codeBlocks := findCodeBlocks(body)
	
	var chunks []Chunk
	chunkIndex := 0
	pos := 0
	
	for pos < len(body) {
		// Determine chunk end position (target size from current position)
		chunkEnd := pos + targetChunkSize
		if chunkEnd >= len(body) {
			// Last chunk - take everything remaining
			chunks = append(chunks, Chunk{
				NoteID:     noteID,
				ChunkIndex: chunkIndex,
				Text:       strings.TrimSpace(body[pos:]),
			})
			break
		}
		
		// Find the best split point near chunkEnd
		splitPoint := findBestSplitPoint(body, pos, chunkEnd, codeBlocks)
		
		// Extract the chunk
		chunkText := strings.TrimSpace(body[pos:splitPoint])
		if chunkText != "" {
			chunks = append(chunks, Chunk{
				NoteID:     noteID,
				ChunkIndex: chunkIndex,
				Text:       chunkText,
			})
			chunkIndex++
		}
		
		// Move to next position with overlap
		// Start next chunk at (current start + effective step)
		pos = pos + effectiveStep
		
		// Ensure we make progress even if overlap calculation keeps us at same spot
		if pos >= splitPoint {
			pos = splitPoint
		}
		
		// If we haven't moved forward, force progress to avoid infinite loop
		if pos <= chunks[len(chunks)-1].ChunkIndex {
			pos = splitPoint + 1
		}
	}
	
	// Ensure we always produce at least one chunk
	if len(chunks) == 0 {
		chunks = append(chunks, Chunk{
			NoteID:     noteID,
			ChunkIndex: 0,
			Text:       strings.TrimSpace(body),
		})
	}
	
	return chunks
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// codeBlockRange represents the start and end positions of a code block
type codeBlockRange struct {
	start int
	end   int
}

// findCodeBlocks identifies all fenced code block boundaries in the text
func findCodeBlocks(text string) []codeBlockRange {
	matches := fencedCodePattern.FindAllStringIndex(text, -1)
	blocks := make([]codeBlockRange, 0, len(matches))
	
	for _, match := range matches {
		blocks = append(blocks, codeBlockRange{
			start: match[0],
			end:   match[1],
		})
	}
	
	return blocks
}

// isInsideCodeBlock checks if a position falls within any code block
func isInsideCodeBlock(pos int, codeBlocks []codeBlockRange) bool {
	for _, block := range codeBlocks {
		if pos > block.start && pos < block.end {
			return true
		}
	}
	return false
}

// findBestSplitPoint finds the best place to split the text near the target position.
// Preference order:
// 1. Markdown heading (after the heading line)
// 2. Blank line
// 3. End of sentence (. ! ?)
// 4. Whitespace
// 5. Target position (hard split)
func findBestSplitPoint(text string, start, target int, codeBlocks []codeBlockRange) int {
	// Don't go past the end of the text
	if target >= len(text) {
		return len(text)
	}
	
	// Search window: look backwards from target up to 500 chars
	searchStart := target - 500
	if searchStart < start {
		searchStart = start
	}
	
	// Search forward from target up to 200 chars for better split points
	searchEnd := target + 200
	if searchEnd > len(text) {
		searchEnd = len(text)
	}
	
	searchText := text[searchStart:searchEnd]
	bestPos := target - searchStart // default to target position
	bestPriority := 5                // lowest priority
	
	// 1. Look for markdown headings
	headingMatches := headingPattern.FindAllStringIndex(searchText, -1)
	for _, match := range headingMatches {
		absPos := searchStart + match[1] // end of heading line
		if absPos <= target+200 && absPos >= start && !isInsideCodeBlock(absPos, codeBlocks) {
			if bestPriority > 1 || (bestPriority == 1 && absPos < searchStart+bestPos) {
				bestPos = absPos - searchStart
				bestPriority = 1
			}
		}
	}
	
	// 2. Look for blank lines
	if bestPriority > 1 {
		blankMatches := blankLinePattern.FindAllStringIndex(searchText, -1)
		for _, match := range blankMatches {
			absPos := searchStart + match[1]
			if absPos <= target+100 && absPos >= start && !isInsideCodeBlock(absPos, codeBlocks) {
				if bestPriority > 2 || (bestPriority == 2 && absPos < searchStart+bestPos) {
					bestPos = absPos - searchStart
					bestPriority = 2
				}
			}
		}
	}
	
	// 3. Look for sentence endings
	if bestPriority > 2 {
		for i := len(searchText) - 1; i >= 0; i-- {
			absPos := searchStart + i
			if absPos > target+100 || absPos < searchStart {
				continue
			}
			
			ch := searchText[i]
			if (ch == '.' || ch == '!' || ch == '?') && !isInsideCodeBlock(absPos, codeBlocks) {
				// Check if followed by whitespace or end of text
				if i+1 >= len(searchText) || searchText[i+1] == ' ' || searchText[i+1] == '\n' {
					bestPos = i + 1
					bestPriority = 3
					break
				}
			}
		}
	}
	
	// 4. Look for any whitespace
	if bestPriority > 3 {
		for i := len(searchText) - 1; i >= 0; i-- {
			absPos := searchStart + i
			if absPos > target || absPos < searchStart {
				continue
			}
			
			ch := searchText[i]
			if (ch == ' ' || ch == '\n' || ch == '\t') && !isInsideCodeBlock(absPos, codeBlocks) {
				// Lowest tier — nothing reads bestPriority past this point,
				// so it is not updated.
				bestPos = i + 1
				break
			}
		}
	}
	
	return searchStart + bestPos
}
