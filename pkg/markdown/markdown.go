// Package markdown provides markdown link extraction, checksum computation,
// word counting, lead extraction, and note utility functions.
// This is a reusable package that can be imported by external projects.
package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
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
var linkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// wikiLinkRe matches wiki-links [[target]] and aliased [[target|display]].
var wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]|]+?)(?:\|([^\[\]]*))?\]\]`)

// isEmbed reports whether the match starting at start is preceded by '!',
// marking it an image (![alt](src)) or an Obsidian embed (![[target]]).
//
// Go's regexp has no lookbehind. These patterns used to carry a leading
// "(^|[^!])" group to stand in for one, but that group CONSUMES the preceding
// character, and FindAll does not return overlapping matches — so in
// "[a](1)[b](2)" the second link's required prefix had already been eaten by
// the first and it was silently dropped. Checking the byte directly costs
// nothing and has no such blind spot.
func isEmbed(s string, start int) bool {
	return start > 0 && s[start-1] == '!'
}

// fenceRe matches an opening or closing code fence (``` or ~~~, 3 or more).
var fenceRe = regexp.MustCompile("^[\t ]*(`{3,}|~{3,})")

// backtickRunRe matches a run of backticks, used to find inline code spans.
var backtickRunRe = regexp.MustCompile("`+")

// htmlCommentRe matches an HTML comment, including one spanning lines.
var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// urlPrefixRe matches http:// or https:// at the start of a string.
var urlPrefixRe = regexp.MustCompile(`^https?://`)

// attachmentPrefix identifies first-class brain attachment references.
const attachmentPrefix = "brain-attachment://"

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

// ExtractLinks extracts links from body text, in document order.
//
// Two syntaxes are recognised:
//   - markdown links [text](target), but NOT images ![alt](src)
//   - wiki-links [[target]] and [[target|display]], but NOT embeds ![[target]]
//
// Text inside fenced code blocks, inline code spans, and HTML comments is
// ignored: brain entries routinely document the link syntax itself, and
// treating those examples as real links pollutes the graph with placeholder
// targets such as "pattern-id".
//
// Each link includes ±50 characters of surrounding context as a snippet. The
// snippet is taken from the ORIGINAL text, so it still shows code that was
// masked for the purpose of matching.
func ExtractLinks(markdown string) []ExtractedLink {
	if markdown == "" {
		return nil
	}

	// Match against a copy with code regions blanked out. maskCode preserves
	// byte offsets, so every index below is valid in the original string too.
	searchable := maskCode(markdown)

	type positioned struct {
		start int
		link  ExtractedLink
	}
	var found []positioned

	// --- markdown links ----------------------------------------------------
	for _, loc := range linkRe.FindAllStringSubmatchIndex(searchable, -1) {
		// loc: [full_start, full_end, title_start, title_end, href_start, href_end]
		if isEmbed(searchable, loc[0]) {
			continue
		}
		title := markdown[loc[2]:loc[3]]
		href := markdown[loc[4]:loc[5]]

		// Classify link type
		linkType := "markdown"
		if strings.HasPrefix(href, attachmentPrefix) {
			linkType = "attachment"
		} else if urlPrefixRe.MatchString(href) {
			linkType = "url"
		}

		found = append(found, positioned{
			start: loc[0],
			link: ExtractedLink{
				Href:    href,
				Title:   title,
				Type:    linkType,
				Snippet: snippetAround(markdown, loc[0], loc[1]),
			},
		})
	}

	// --- wiki-links --------------------------------------------------------
	for _, loc := range wikiLinkRe.FindAllStringSubmatchIndex(searchable, -1) {
		// loc: [full_start, full_end, target_start, target_end, alias_start, alias_end]
		if isEmbed(searchable, loc[0]) {
			continue
		}
		target := strings.TrimSpace(markdown[loc[2]:loc[3]])
		if target == "" {
			continue
		}

		// Display text is the alias when present, otherwise the target itself.
		display := target
		if loc[4] != -1 {
			if alias := strings.TrimSpace(markdown[loc[4]:loc[5]]); alias != "" {
				display = alias
			}
		}

		found = append(found, positioned{
			start: loc[0],
			link: ExtractedLink{
				Href:    target,
				Title:   display,
				Type:    "wikilink",
				Snippet: snippetAround(markdown, loc[0], loc[1]),
			},
		})
	}

	if len(found) == 0 {
		return []ExtractedLink{}
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })

	links := make([]ExtractedLink, 0, len(found))
	for _, f := range found {
		links = append(links, f.link)
	}
	return links
}

// snippetAround returns the text around [start, end) with ±50 characters of
// context, clamped to the bounds of s.
func snippetAround(s string, start, end int) string {
	from := start - 50
	if from < 0 {
		from = 0
	}
	to := end + 50
	if to > len(s) {
		to = len(s)
	}
	return s[from:to]
}

// maskCode returns a copy of s with the contents of fenced code blocks, inline
// code spans, and HTML comments replaced by spaces. Byte offsets are preserved
// exactly, so match indices found in the result index the original string too.
//
// Newlines are kept so fence detection stays line-oriented; only the bytes
// that could form link syntax are blanked.
func maskCode(s string) string {
	out := []byte(s)

	blank := func(from, to int) {
		for i := from; i < to; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}

	// HTML comments first, and over the whole string rather than line by line:
	// they span lines, and the plan templates that motivated this keep their
	// example links commented out ("<!-- Link to patterns: [Name](pattern-id) -->").
	// Blanking them first also stops a fence or backtick inside a comment from
	// opening a region. An unterminated "<!--" matches nothing and masks
	// nothing, the same conservative choice made for an unclosed backtick.
	for _, loc := range htmlCommentRe.FindAllStringIndex(s, -1) {
		blank(loc[0], loc[1])
	}
	s = string(out)

	var openFence string // non-empty while inside a fenced block
	pos := 0
	for pos <= len(s) {
		lineEnd := strings.IndexByte(s[pos:], '\n')
		if lineEnd < 0 {
			lineEnd = len(s)
		} else {
			lineEnd += pos
		}
		line := s[pos:lineEnd]

		if openFence != "" {
			// Inside a fence: blank everything, and close on a matching fence.
			blank(pos, lineEnd)
			if m := fenceRe.FindStringSubmatch(line); m != nil && isClosingFence(m[1], openFence, line) {
				openFence = ""
			}
		} else if m := fenceRe.FindStringSubmatch(line); m != nil {
			openFence = m[1]
			blank(pos, lineEnd)
		} else {
			maskInlineCode(s, pos, lineEnd, blank)
		}

		if lineEnd == len(s) {
			break
		}
		pos = lineEnd + 1
	}

	return string(out)
}

// isClosingFence reports whether a fence marker closes a block opened by
// openFence: same fence character, at least as long, and nothing but
// whitespace after it.
func isClosingFence(marker, openFence, line string) bool {
	if marker[0] != openFence[0] || len(marker) < len(openFence) {
		return false
	}
	rest := line[strings.Index(line, marker)+len(marker):]
	return strings.TrimSpace(rest) == ""
}

// maskInlineCode blanks inline code spans within s[from:to]. A run of N
// backticks opens a span that the next run of exactly N backticks closes; an
// unclosed run masks nothing, so a stray backtick cannot swallow the line.
func maskInlineCode(s string, from, to int, blank func(int, int)) {
	line := s[from:to]
	runs := backtickRunRe.FindAllStringIndex(line, -1)
	for i := 0; i < len(runs); i++ {
		open := runs[i]
		openLen := open[1] - open[0]
		for j := i + 1; j < len(runs); j++ {
			if runs[j][1]-runs[j][0] != openLen {
				continue
			}
			blank(from+open[0], from+runs[j][1])
			i = j
			break
		}
	}
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
