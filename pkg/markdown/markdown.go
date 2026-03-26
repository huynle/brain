// Package markdown provides markdown link extraction, checksum computation,
// word counting, lead extraction, and note utility functions.
// This is a reusable package that can be imported by external projects.
package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ExtractedLink represents a link found in markdown content.
type ExtractedLink struct {
	Href    string // raw link target (could be short_id, path, or URL)
	Title   string // link display text
	Type    string // "markdown" or "url"
	Snippet string // surrounding context (±50 chars)
}

// ---------------------------------------------------------------------------
// Compiled regex patterns
// ---------------------------------------------------------------------------

// linkRe matches markdown links [text](target).
// Go's regexp doesn't support lookbehinds, so we match an optional preceding
// character and then check it isn't '!'.
var linkRe = regexp.MustCompile(`(^|[^!])\[([^\]]*)\]\(([^)]+)\)`)

// urlPrefixRe matches http:// or https:// at the start of a string.
var urlPrefixRe = regexp.MustCompile(`^https?://`)

// Lead-stripping patterns
var (
	headingRe         = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	boldItalicStarRe  = regexp.MustCompile(`\*{1,3}([^*]+)\*{1,3}`)
	boldItalicUnderRe = regexp.MustCompile(`_{1,3}([^_]+)_{1,3}`)
	inlineCodeRe      = regexp.MustCompile("`([^`]+)`")
	imageRe           = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	linkStripRe       = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)
	strikethroughRe   = regexp.MustCompile(`~~([^~]+)~~`)
	whitespaceRe      = regexp.MustCompile(`\s+`)
)

// ---------------------------------------------------------------------------
// ExtractLinks
// ---------------------------------------------------------------------------

// ExtractLinks extracts markdown links from body text.
// Matches [text](target) but NOT ![alt](src) (image links).
// Each link includes ±50 characters of surrounding context as a snippet.
func ExtractLinks(markdown string) []ExtractedLink {
	if markdown == "" {
		return nil
	}

	matches := linkRe.FindAllStringSubmatchIndex(markdown, -1)
	links := make([]ExtractedLink, 0, len(matches))

	for _, loc := range matches {
		// loc indices: [full_start, full_end, group1_start, group1_end, group2_start, group2_end, group3_start, group3_end]
		// group1 = preceding char (or empty at start), group2 = title, group3 = href

		title := markdown[loc[4]:loc[5]]
		href := markdown[loc[6]:loc[7]]

		// Determine the actual link start (the '[' character).
		// If group1 matched a character, the link starts after it.
		linkStart := loc[0]
		if loc[2] != loc[3] {
			// group1 matched a non-empty char, link starts at group1_end
			linkStart = loc[3]
		}
		linkEnd := loc[1]

		// Classify link type
		linkType := "markdown"
		if urlPrefixRe.MatchString(href) {
			linkType = "url"
		}

		// Extract ±50 chars of surrounding context
		snippetStart := linkStart - 50
		if snippetStart < 0 {
			snippetStart = 0
		}
		snippetEnd := linkEnd + 50
		if snippetEnd > len(markdown) {
			snippetEnd = len(markdown)
		}
		snippet := markdown[snippetStart:snippetEnd]

		links = append(links, ExtractedLink{
			Href:    href,
			Title:   title,
			Type:    linkType,
			Snippet: snippet,
		})
	}

	return links
}

// ---------------------------------------------------------------------------
// ComputeChecksum
// ---------------------------------------------------------------------------

// ComputeChecksum returns the hex-encoded SHA-256 hash of content.
func ComputeChecksum(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// CountWords
// ---------------------------------------------------------------------------

// CountWords counts the number of words in body text.
// Splits on whitespace and filters empty strings.
func CountWords(body string) int {
	return len(strings.Fields(body))
}

// ---------------------------------------------------------------------------
// ExtractLead
// ---------------------------------------------------------------------------

// ExtractLead extracts the first non-empty paragraph from body,
// strips markdown formatting, and truncates to 200 characters.
func ExtractLead(body string) string {
	if body == "" {
		return ""
	}

	// Split into paragraphs (separated by one or more blank lines)
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(body, -1)

	// Find first non-empty paragraph
	var firstParagraph string
	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			firstParagraph = trimmed
			break
		}
	}
	if firstParagraph == "" {
		return ""
	}

	text := firstParagraph

	// Strip markdown formatting
	text = headingRe.ReplaceAllString(text, "")
	text = boldItalicStarRe.ReplaceAllString(text, "$1")
	text = boldItalicUnderRe.ReplaceAllString(text, "$1")
	text = inlineCodeRe.ReplaceAllString(text, "$1")
	text = imageRe.ReplaceAllString(text, "")
	text = linkStripRe.ReplaceAllString(text, "$1")
	text = strikethroughRe.ReplaceAllString(text, "$1")

	// Collapse whitespace
	text = whitespaceRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	// Truncate to 200 chars
	if len(text) > 200 {
		text = text[:200]
	}

	return text
}
