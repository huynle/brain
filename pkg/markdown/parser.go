package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/huynle/brain-api/pkg/frontmatter"
)

// ParsedFile is the result of parsing a markdown file from disk.
type ParsedFile struct {
	Path       string
	ShortID    string
	Title      string
	Lead       string
	Body       string
	RawContent string
	WordCount  int
	Checksum   string
	Metadata   *frontmatter.Frontmatter
	Type       *string
	Status     *string
	Priority   *string
	ProjectID  *string
	FeatureID  *string
	Tags       []string
	Created    string
	Modified   string
	Links      []ExtractedLink
}

// ParseFile reads a markdown file from disk and parses it into a ParsedFile.
//
// filePath is the relative path within brainDir (e.g., "projects/test/task/abc12def.md").
// brainDir is the absolute path to the brain root directory.
func ParseFile(filePath string, brainDir string) (*ParsedFile, error) {
	fullPath := filepath.Join(brainDir, filePath)

	// Read file
	rawBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("markdown: read file %q: %w", fullPath, err)
	}
	rawContent := string(rawBytes)

	// Get file stat for timestamps
	stat, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("markdown: stat file %q: %w", fullPath, err)
	}

	// Parse frontmatter + body
	doc, err := frontmatter.Parse(rawContent)
	if err != nil {
		return nil, fmt.Errorf("markdown: parse frontmatter %q: %w", filePath, err)
	}

	fm := &doc.Frontmatter
	body := doc.Body

	// Extract derived fields
	shortID := ExtractIDFromPath(filePath)
	checksum := ComputeChecksum(rawContent)
	links := ExtractLinks(body)
	wordCount := CountWords(body)
	lead := ExtractLead(body)

	// Title: from frontmatter, fallback to shortID
	title := fm.Title
	if title == "" {
		title = shortID
	}

	// Tags
	tags := fm.Tags
	if tags == nil {
		tags = []string{}
	}

	// Created: from frontmatter, fallback to file birth time
	created := fm.Created
	if created == "" {
		created = stat.ModTime().UTC().Format(time.RFC3339)
	}

	// Modified: from file mtime
	modified := stat.ModTime().UTC().Format(time.RFC3339)

	// Optional string fields — nil if empty
	pf := &ParsedFile{
		Path:       filePath,
		ShortID:    shortID,
		Title:      title,
		Lead:       lead,
		Body:       body,
		RawContent: rawContent,
		WordCount:  wordCount,
		Checksum:   checksum,
		Metadata:   fm,
		Type:       nilIfEmpty(fm.Type),
		Status:     nilIfEmpty(fm.Status),
		Priority:   nilIfEmpty(fm.Priority),
		ProjectID:  nilIfEmpty(fm.ProjectID),
		FeatureID:  nilIfEmpty(fm.FeatureID),
		Tags:       tags,
		Created:    created,
		Modified:   modified,
		Links:      links,
	}

	return pf, nil
}

// nilIfEmpty returns a pointer to s if non-empty, otherwise nil.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
