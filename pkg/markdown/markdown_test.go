package markdown

import (
	"strings"
	"testing"
)

// ===========================================================================
// ExtractLinks
// ===========================================================================

func TestExtractLinks_BasicMarkdownLink(t *testing.T) {
	md := `See [my note](abc12def) for details.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "abc12def" {
		t.Errorf("href = %q, want %q", links[0].Href, "abc12def")
	}
	if links[0].Title != "my note" {
		t.Errorf("title = %q, want %q", links[0].Title, "my note")
	}
	if links[0].Type != "markdown" {
		t.Errorf("type = %q, want %q", links[0].Type, "markdown")
	}
}

func TestExtractLinks_URLLink(t *testing.T) {
	md := `Visit [Google](https://google.com) now.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "https://google.com" {
		t.Errorf("href = %q, want %q", links[0].Href, "https://google.com")
	}
	if links[0].Type != "url" {
		t.Errorf("type = %q, want %q", links[0].Type, "url")
	}
}

func TestExtractLinks_HTTPLink(t *testing.T) {
	md := `Visit [Example](http://example.com) now.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Type != "url" {
		t.Errorf("type = %q, want %q", links[0].Type, "url")
	}
}

func TestExtractLinks_AttachmentLink(t *testing.T) {
	md := `See [source PDF](brain-attachment://att_x) and [note](abc12def).`
	links := ExtractLinks(md)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].Href != "brain-attachment://att_x" {
		t.Fatalf("href = %q, want brain-attachment://att_x", links[0].Href)
	}
	if links[0].Type != "attachment" {
		t.Errorf("type = %q, want %q", links[0].Type, "attachment")
	}
	if links[1].Type != "markdown" {
		t.Errorf("ordinary link type = %q, want markdown", links[1].Type)
	}
}

func TestExtractLinks_SkipsImageLinks(t *testing.T) {
	md := `Here is an image ![alt text](image.png) and a [real link](target).`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link (image excluded), got %d", len(links))
	}
	if links[0].Href != "target" {
		t.Errorf("href = %q, want %q", links[0].Href, "target")
	}
}

func TestExtractLinks_MultipleLinks(t *testing.T) {
	md := `See [note1](id1) and [note2](id2) and [Google](https://google.com).`
	links := ExtractLinks(md)
	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}
	if links[0].Href != "id1" {
		t.Errorf("links[0].href = %q, want %q", links[0].Href, "id1")
	}
	if links[1].Href != "id2" {
		t.Errorf("links[1].href = %q, want %q", links[1].Href, "id2")
	}
	if links[2].Type != "url" {
		t.Errorf("links[2].type = %q, want %q", links[2].Type, "url")
	}
}

func TestExtractLinks_EmptyString(t *testing.T) {
	links := ExtractLinks("")
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}
}

func TestExtractLinks_NoLinks(t *testing.T) {
	md := `Just some plain text with no links at all.`
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}
}

func TestExtractLinks_EmptyTitle(t *testing.T) {
	md := `A link with [](empty-title) empty title.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Title != "" {
		t.Errorf("title = %q, want empty", links[0].Title)
	}
}

func TestExtractLinks_SnippetContext(t *testing.T) {
	// Build a string where the link is far from the edges
	prefix := strings.Repeat("x", 100)
	suffix := strings.Repeat("y", 100)
	md := prefix + "[link](target)" + suffix
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	snippet := links[0].Snippet
	// Snippet should be roughly ±50 chars around the match
	if len(snippet) > 150 {
		t.Errorf("snippet too long: %d chars", len(snippet))
	}
	if !strings.Contains(snippet, "[link](target)") {
		t.Errorf("snippet should contain the link itself, got %q", snippet)
	}
}

func TestExtractLinks_LinkAtStartOfLine(t *testing.T) {
	md := "[start link](target) at the beginning."
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "target" {
		t.Errorf("href = %q, want %q", links[0].Href, "target")
	}
}

func TestExtractLinks_ImageAtStartThenLink(t *testing.T) {
	md := "![img](pic.png)\n[real](target)"
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "target" {
		t.Errorf("href = %q, want %q", links[0].Href, "target")
	}
}

// ---------------------------------------------------------------------------
// Wiki-links
//
// [[title]] was documented in SKILL.md from the start and never implemented:
// the only link regex required a literal "](", which [[title]] does not
// contain, so every wiki-link in the brain was silently dropped.
// ---------------------------------------------------------------------------

func TestExtractLinks_WikiLink(t *testing.T) {
	md := `See [[My Target Note]] for details.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "My Target Note" {
		t.Errorf("href = %q, want %q", links[0].Href, "My Target Note")
	}
	if links[0].Title != "My Target Note" {
		t.Errorf("title = %q, want %q", links[0].Title, "My Target Note")
	}
	if links[0].Type != "wikilink" {
		t.Errorf("type = %q, want %q", links[0].Type, "wikilink")
	}
}

func TestExtractLinks_WikiLinkAtStartOfInput(t *testing.T) {
	md := "[[Leading Target]] then text."
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "Leading Target" {
		t.Errorf("href = %q, want %q", links[0].Href, "Leading Target")
	}
}

func TestExtractLinks_WikiLinkWithAlias(t *testing.T) {
	md := `See [[abc12def|the design doc]] for details.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "abc12def" {
		t.Errorf("href = %q, want %q", links[0].Href, "abc12def")
	}
	if links[0].Title != "the design doc" {
		t.Errorf("title = %q, want %q", links[0].Title, "the design doc")
	}
}

func TestExtractLinks_WikiLinkTrimsWhitespace(t *testing.T) {
	md := `See [[  Padded Title  ]] here.`
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Href != "Padded Title" {
		t.Errorf("href = %q, want %q", links[0].Href, "Padded Title")
	}
}

func TestExtractLinks_SkipsWikiEmbeds(t *testing.T) {
	md := `An embed ![[some-image.png]] is not a link.`
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links for an embed, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_EmptyWikiLinkIgnored(t *testing.T) {
	md := "Nothing here: [[]] or [[   ]]."
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d: %+v", len(links), links)
	}
}

// TestExtractLinks_MixedFormsInDocumentOrder is the case named in the bug
// report: markdown and wiki forms interleaved, with the wiki one dropped.
func TestExtractLinks_MixedFormsInDocumentOrder(t *testing.T) {
	md := `[a](b) and [[c]] and [d](e)`
	links := ExtractLinks(md)
	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d: %+v", len(links), links)
	}
	wantHrefs := []string{"b", "c", "e"}
	for i, want := range wantHrefs {
		if links[i].Href != want {
			t.Errorf("links[%d].Href = %q, want %q", i, links[i].Href, want)
		}
	}
	if links[1].Type != "wikilink" {
		t.Errorf("links[1].Type = %q, want %q", links[1].Type, "wikilink")
	}
}

func TestExtractLinks_WikiLinkSnippet(t *testing.T) {
	md := strings.Repeat("x", 100) + " [[Target]] " + strings.Repeat("y", 100)
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if !strings.Contains(links[0].Snippet, "[[Target]]") {
		t.Errorf("snippet should contain the link itself, got %q", links[0].Snippet)
	}
	if len(links[0].Snippet) >= len(md) {
		t.Errorf("snippet should be trimmed to ±50 chars, got %d bytes", len(links[0].Snippet))
	}
}

// ---------------------------------------------------------------------------
// Code regions
//
// Brain entries document the link syntax itself, so extracting from code
// examples filled the live graph with placeholder targets ("pattern-id",
// "entry-id", "report-id" — 85 rows between them).
// ---------------------------------------------------------------------------

func TestExtractLinks_IgnoresFencedCodeBlock(t *testing.T) {
	md := "Prose.\n\n```\nSee [example](pattern-id) and [[Example Title]]\n```\n\nMore prose."
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links inside a fence, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_IgnoresLanguageTaggedFence(t *testing.T) {
	md := "```markdown\n[Title](entry-id)\n```"
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_IgnoresTildeFence(t *testing.T) {
	md := "~~~\n[Title](report-id)\n~~~"
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_FindsLinksAroundFence(t *testing.T) {
	md := "[before](aaa11111)\n\n```\n[inside](bbb22222)\n```\n\n[after](ccc33333)"
	links := ExtractLinks(md)
	if len(links) != 2 {
		t.Fatalf("expected 2 links around the fence, got %d: %+v", len(links), links)
	}
	if links[0].Href != "aaa11111" {
		t.Errorf("links[0].Href = %q, want %q", links[0].Href, "aaa11111")
	}
	if links[1].Href != "ccc33333" {
		t.Errorf("links[1].Href = %q, want %q", links[1].Href, "ccc33333")
	}
}

func TestExtractLinks_IgnoresInlineCode(t *testing.T) {
	md := "Write it as `[Title](plan-id)` in your entry."
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links inside inline code, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_InlineCodeDoesNotHideNeighbours(t *testing.T) {
	md := "Use `[Title](plan-id)` to reach [the plan](real1234)."
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d: %+v", len(links), links)
	}
	if links[0].Href != "real1234" {
		t.Errorf("href = %q, want %q", links[0].Href, "real1234")
	}
}

// A lone backtick must not mask the rest of the line: an unclosed run has no
// closing run of equal length, so nothing is treated as code.
func TestExtractLinks_UnclosedBacktickMasksNothing(t *testing.T) {
	md := "A stray ` tick before [the note](abcd1234)."
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d: %+v", len(links), links)
	}
	if links[0].Href != "abcd1234" {
		t.Errorf("href = %q, want %q", links[0].Href, "abcd1234")
	}
}

// Adjacent links used to lose every one after the first: the "(^|[^!])" prefix
// group consumed the separating character, and FindAll does not overlap.
// The plan-template entries in the live brain keep their example links inside
// HTML comments, which is where the bulk of the placeholder targets came from
// — not code fences, as first assumed.
func TestExtractLinks_IgnoresHTMLComments(t *testing.T) {
	md := "## Patterns to Apply\n\n<!-- Link to patterns: [Pattern Name](pattern-id) -->\n\n## Based On"
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links inside an HTML comment, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_IgnoresMultiLineHTMLComment(t *testing.T) {
	md := "<!--\n[Pattern Name](pattern-id)\nSee [[Some Title]]\n-->\n[real](abcd1234)"
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d: %+v", len(links), links)
	}
	if links[0].Href != "abcd1234" {
		t.Errorf("href = %q, want %q", links[0].Href, "abcd1234")
	}
}

func TestExtractLinks_FindsLinksAroundHTMLComment(t *testing.T) {
	md := "[before](aaa11111) <!-- [hidden](bbb22222) --> [after](ccc33333)"
	links := ExtractLinks(md)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d: %+v", len(links), links)
	}
	if links[0].Href != "aaa11111" || links[1].Href != "ccc33333" {
		t.Errorf("hrefs = %q/%q, want aaa11111/ccc33333", links[0].Href, links[1].Href)
	}
}

// An unterminated "<!--" masks nothing, matching the unclosed-backtick choice.
func TestExtractLinks_UnterminatedHTMLCommentMasksNothing(t *testing.T) {
	md := "<!-- oops [the note](abcd1234)"
	links := ExtractLinks(md)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d: %+v", len(links), links)
	}
}

func TestExtractLinks_AdjacentLinks(t *testing.T) {
	tests := []struct {
		name  string
		md    string
		hrefs []string
	}{
		{"markdown", "[x](aaa11111)[y](bbb22222)", []string{"aaa11111", "bbb22222"}},
		{"wiki", "[[Alpha]][[Beta]]", []string{"Alpha", "Beta"}},
		{"mixed", "[[Alpha]][y](bbb22222)", []string{"Alpha", "bbb22222"}},
		{"image_then_link", "![img](pic.png)[y](bbb22222)", []string{"bbb22222"}},
		{"embed_then_wiki", "![[pic.png]][[Beta]]", []string{"Beta"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := ExtractLinks(tt.md)
			if len(links) != len(tt.hrefs) {
				t.Fatalf("got %d links, want %d: %+v", len(links), len(tt.hrefs), links)
			}
			for i, want := range tt.hrefs {
				if links[i].Href != want {
					t.Errorf("links[%d].Href = %q, want %q", i, links[i].Href, want)
				}
			}
		})
	}
}

func TestExtractLinks_UnclosedFenceMasksToEnd(t *testing.T) {
	md := "```\n[inside](aaa11111)\nstill inside [also](bbb22222)"
	links := ExtractLinks(md)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d: %+v", len(links), links)
	}
}

// ===========================================================================
// ComputeChecksum
// ===========================================================================

func TestComputeChecksum_KnownValue(t *testing.T) {
	// SHA-256 of "hello" is well-known
	got := ComputeChecksum("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("ComputeChecksum(%q) = %q, want %q", "hello", got, want)
	}
}

func TestComputeChecksum_EmptyString(t *testing.T) {
	got := ComputeChecksum("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("ComputeChecksum(%q) = %q, want %q", "", got, want)
	}
}

func TestComputeChecksum_DifferentInputsDifferentOutput(t *testing.T) {
	a := ComputeChecksum("hello")
	b := ComputeChecksum("world")
	if a == b {
		t.Error("different inputs should produce different checksums")
	}
}

// ===========================================================================
// CountWords
// ===========================================================================

func TestCountWords_SimpleText(t *testing.T) {
	got := CountWords("hello world foo bar")
	if got != 4 {
		t.Errorf("CountWords = %d, want 4", got)
	}
}

func TestCountWords_EmptyString(t *testing.T) {
	got := CountWords("")
	if got != 0 {
		t.Errorf("CountWords = %d, want 0", got)
	}
}

func TestCountWords_WhitespaceOnly(t *testing.T) {
	got := CountWords("   \t\n  ")
	if got != 0 {
		t.Errorf("CountWords = %d, want 0", got)
	}
}

func TestCountWords_MultipleSpaces(t *testing.T) {
	got := CountWords("hello   world")
	if got != 2 {
		t.Errorf("CountWords = %d, want 2", got)
	}
}

func TestCountWords_NewlinesAndTabs(t *testing.T) {
	got := CountWords("hello\nworld\tfoo")
	if got != 3 {
		t.Errorf("CountWords = %d, want 3", got)
	}
}

// ===========================================================================
// ExtractLead
// ===========================================================================

func TestExtractLead_SimpleParagraph(t *testing.T) {
	body := "This is the first paragraph.\n\nThis is the second."
	got := ExtractLead(body)
	if got != "This is the first paragraph." {
		t.Errorf("ExtractLead = %q, want %q", got, "This is the first paragraph.")
	}
}

func TestExtractLead_EmptyBody(t *testing.T) {
	got := ExtractLead("")
	if got != "" {
		t.Errorf("ExtractLead = %q, want empty", got)
	}
}

func TestExtractLead_StripsHeadings(t *testing.T) {
	body := "# My Heading\n\nSecond paragraph."
	got := ExtractLead(body)
	if got != "My Heading" {
		t.Errorf("ExtractLead = %q, want %q", got, "My Heading")
	}
}

func TestExtractLead_StripsBoldItalic(t *testing.T) {
	body := "This is **bold** and *italic* and ***both***."
	got := ExtractLead(body)
	if got != "This is bold and italic and both." {
		t.Errorf("ExtractLead = %q, want %q", got, "This is bold and italic and both.")
	}
}

func TestExtractLead_StripsUnderscoreEmphasis(t *testing.T) {
	body := "This is __bold__ and _italic_."
	got := ExtractLead(body)
	if got != "This is bold and italic." {
		t.Errorf("ExtractLead = %q, want %q", got, "This is bold and italic.")
	}
}

func TestExtractLead_StripsInlineCode(t *testing.T) {
	body := "Use `fmt.Println` to print."
	got := ExtractLead(body)
	if got != "Use fmt.Println to print." {
		t.Errorf("ExtractLead = %q, want %q", got, "Use fmt.Println to print.")
	}
}

func TestExtractLead_StripsImages(t *testing.T) {
	body := "Before ![alt](img.png) after."
	got := ExtractLead(body)
	if got != "Before after." {
		t.Errorf("ExtractLead = %q, want %q", got, "Before after.")
	}
}

func TestExtractLead_StripsLinksKeepsText(t *testing.T) {
	body := "See [my link](target) for details."
	got := ExtractLead(body)
	if got != "See my link for details." {
		t.Errorf("ExtractLead = %q, want %q", got, "See my link for details.")
	}
}

func TestExtractLead_StripsStrikethrough(t *testing.T) {
	body := "This is ~~deleted~~ text."
	got := ExtractLead(body)
	if got != "This is deleted text." {
		t.Errorf("ExtractLead = %q, want %q", got, "This is deleted text.")
	}
}

func TestExtractLead_TruncatesTo200Chars(t *testing.T) {
	body := strings.Repeat("word ", 100) // 500 chars
	got := ExtractLead(body)
	if len(got) > 200 {
		t.Errorf("ExtractLead length = %d, want <= 200", len(got))
	}
}

func TestExtractLead_SkipsEmptyFirstParagraph(t *testing.T) {
	body := "\n\n\nActual content here.\n\nMore stuff."
	got := ExtractLead(body)
	if got != "Actual content here." {
		t.Errorf("ExtractLead = %q, want %q", got, "Actual content here.")
	}
}

func TestExtractLead_CollapsesWhitespace(t *testing.T) {
	body := "Hello   world\n  foo   bar"
	got := ExtractLead(body)
	if got != "Hello world foo bar" {
		t.Errorf("ExtractLead = %q, want %q", got, "Hello world foo bar")
	}
}
