package markdown

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Compiled regex patterns for note utilities
// ---------------------------------------------------------------------------

var (
	slugNonAlphanumRe = regexp.MustCompile(`[^a-z0-9-]`)
	slugMultiHyphenRe = regexp.MustCompile(`-+`)
)

const shortIDChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// ---------------------------------------------------------------------------
// ExtractIDFromPath
// ---------------------------------------------------------------------------

// ExtractIDFromPath extracts the filename stem (without .md extension) from a path.
// e.g., "global/plan/abc12def.md" → "abc12def"
func ExtractIDFromPath(path string) string {
	if path == "" {
		return ""
	}
	// Get the last path component
	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]
	// Strip .md extension
	return strings.TrimSuffix(filename, ".md")
}

// ---------------------------------------------------------------------------
// GenerateShortID
// ---------------------------------------------------------------------------

// GenerateShortID generates an 8-character random alphanumeric ID (a-z0-9).
func GenerateShortID() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = shortIDChars[rand.Intn(len(shortIDChars))]
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// GenerateMarkdownLink
// ---------------------------------------------------------------------------

// GenerateMarkdownLink formats a markdown link as [title](id).
// If title is empty, the id is used as the display text.
func GenerateMarkdownLink(id string, title string) string {
	if title == "" {
		title = id
	}
	return fmt.Sprintf("[%s](%s)", title, id)
}

// ---------------------------------------------------------------------------
// Slugify
// ---------------------------------------------------------------------------

// Slugify converts text to a URL-friendly slug.
// Lowercase, spaces to hyphens, strip non-alphanumeric, collapse hyphens, max 64 chars.
func Slugify(text string) string {
	result := strings.ToLower(strings.TrimSpace(text))
	// Replace whitespace with hyphens
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, "-")
	// Remove non-alphanumeric characters (except hyphens)
	result = slugNonAlphanumRe.ReplaceAllString(result, "")
	// Collapse multiple hyphens
	result = slugMultiHyphenRe.ReplaceAllString(result, "-")
	// Strip leading/trailing hyphens
	result = strings.Trim(result, "-")
	// Truncate to 64 chars
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

// ---------------------------------------------------------------------------
// MatchesFilenamePattern
// ---------------------------------------------------------------------------

// MatchesFilenamePattern checks if a filename matches a pattern.
// Supports exact match or wildcard patterns with '*'.
// Both filename and pattern have .md extension stripped before matching.
// Matching is case-insensitive.
func MatchesFilenamePattern(filename, pattern string) bool {
	// Strip .md extension
	cleanFilename := strings.TrimSuffix(filename, ".md")
	cleanPattern := strings.TrimSuffix(pattern, ".md")

	// Exact match (no wildcards)
	if !strings.Contains(cleanPattern, "*") {
		return strings.EqualFold(cleanFilename, cleanPattern)
	}

	// Convert glob pattern to regex
	// Escape regex special chars except *, then replace * with .*
	escaped := regexp.QuoteMeta(cleanPattern)
	// QuoteMeta escapes *, so we need to un-escape it and replace with .*
	regexPattern := strings.ReplaceAll(escaped, `\*`, ".*")

	re, err := regexp.Compile("(?i)^" + regexPattern + "$")
	if err != nil {
		return false
	}
	return re.MatchString(cleanFilename)
}
