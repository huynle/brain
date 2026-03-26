package markdown

import (
	"regexp"
	"testing"
)

// ===========================================================================
// ExtractIDFromPath
// ===========================================================================

func TestExtractIDFromPath_FullPath(t *testing.T) {
	got := ExtractIDFromPath("global/plan/abc12def.md")
	if got != "abc12def" {
		t.Errorf("ExtractIDFromPath = %q, want %q", got, "abc12def")
	}
}

func TestExtractIDFromPath_NestedPath(t *testing.T) {
	got := ExtractIDFromPath("projects/test/task/xyz98765.md")
	if got != "xyz98765" {
		t.Errorf("ExtractIDFromPath = %q, want %q", got, "xyz98765")
	}
}

func TestExtractIDFromPath_JustFilename(t *testing.T) {
	got := ExtractIDFromPath("abc12def.md")
	if got != "abc12def" {
		t.Errorf("ExtractIDFromPath = %q, want %q", got, "abc12def")
	}
}

func TestExtractIDFromPath_NoExtension(t *testing.T) {
	got := ExtractIDFromPath("abc12def")
	if got != "abc12def" {
		t.Errorf("ExtractIDFromPath = %q, want %q", got, "abc12def")
	}
}

func TestExtractIDFromPath_EmptyString(t *testing.T) {
	got := ExtractIDFromPath("")
	if got != "" {
		t.Errorf("ExtractIDFromPath = %q, want empty", got)
	}
}

// ===========================================================================
// GenerateShortID
// ===========================================================================

func TestGenerateShortID_Length(t *testing.T) {
	id := GenerateShortID()
	if len(id) != 8 {
		t.Errorf("GenerateShortID length = %d, want 8", len(id))
	}
}

func TestGenerateShortID_AlphanumericOnly(t *testing.T) {
	id := GenerateShortID()
	matched, _ := regexp.MatchString(`^[a-z0-9]{8}$`, id)
	if !matched {
		t.Errorf("GenerateShortID = %q, want [a-z0-9]{8}", id)
	}
}

func TestGenerateShortID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateShortID()
		if ids[id] {
			t.Fatalf("GenerateShortID produced duplicate: %q", id)
		}
		ids[id] = true
	}
}

// ===========================================================================
// GenerateMarkdownLink
// ===========================================================================

func TestGenerateMarkdownLink_WithTitle(t *testing.T) {
	got := GenerateMarkdownLink("abc12def", "My Note")
	want := "[My Note](abc12def)"
	if got != want {
		t.Errorf("GenerateMarkdownLink = %q, want %q", got, want)
	}
}

func TestGenerateMarkdownLink_EmptyTitle(t *testing.T) {
	got := GenerateMarkdownLink("abc12def", "")
	want := "[abc12def](abc12def)"
	if got != want {
		t.Errorf("GenerateMarkdownLink = %q, want %q", got, want)
	}
}

// ===========================================================================
// Slugify
// ===========================================================================

func TestSlugify_BasicText(t *testing.T) {
	got := Slugify("Hello World")
	if got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
}

func TestSlugify_SpecialChars(t *testing.T) {
	got := Slugify("Hello, World! @#$%")
	if got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
}

func TestSlugify_MultipleSpaces(t *testing.T) {
	got := Slugify("hello   world")
	if got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
}

func TestSlugify_LeadingTrailingHyphens(t *testing.T) {
	got := Slugify("  -hello world-  ")
	if got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
}

func TestSlugify_MaxLength(t *testing.T) {
	long := "this is a very long title that should be truncated to sixty four characters maximum length"
	got := Slugify(long)
	if len(got) > 64 {
		t.Errorf("Slugify length = %d, want <= 64", len(got))
	}
}

func TestSlugify_EmptyString(t *testing.T) {
	got := Slugify("")
	if got != "" {
		t.Errorf("Slugify = %q, want empty", got)
	}
}

func TestSlugify_OnlySpecialChars(t *testing.T) {
	got := Slugify("@#$%^&*()")
	if got != "" {
		t.Errorf("Slugify = %q, want empty", got)
	}
}

func TestSlugify_NumbersPreserved(t *testing.T) {
	got := Slugify("version 2.0")
	if got != "version-20" {
		t.Errorf("Slugify = %q, want %q", got, "version-20")
	}
}

func TestSlugify_ConsecutiveHyphensCollapsed(t *testing.T) {
	got := Slugify("hello---world")
	if got != "hello-world" {
		t.Errorf("Slugify = %q, want %q", got, "hello-world")
	}
}

// ===========================================================================
// MatchesFilenamePattern
// ===========================================================================

func TestMatchesFilenamePattern_ExactMatch(t *testing.T) {
	if !MatchesFilenamePattern("abc12def", "abc12def") {
		t.Error("exact match should return true")
	}
}

func TestMatchesFilenamePattern_ExactNoMatch(t *testing.T) {
	if MatchesFilenamePattern("abc12def", "xyz98765") {
		t.Error("different IDs should not match")
	}
}

func TestMatchesFilenamePattern_PrefixWildcard(t *testing.T) {
	if !MatchesFilenamePattern("abc12def", "abc*") {
		t.Error("prefix wildcard should match")
	}
}

func TestMatchesFilenamePattern_SuffixWildcard(t *testing.T) {
	if !MatchesFilenamePattern("abc12def", "*def") {
		t.Error("suffix wildcard should match")
	}
}

func TestMatchesFilenamePattern_MiddleWildcard(t *testing.T) {
	if !MatchesFilenamePattern("abc12def", "abc*def") {
		t.Error("middle wildcard should match")
	}
}

func TestMatchesFilenamePattern_WildcardNoMatch(t *testing.T) {
	if MatchesFilenamePattern("abc12def", "xyz*") {
		t.Error("non-matching prefix wildcard should return false")
	}
}

func TestMatchesFilenamePattern_WithMdExtension(t *testing.T) {
	if !MatchesFilenamePattern("abc12def.md", "abc12def") {
		t.Error("should strip .md extension before matching")
	}
}

func TestMatchesFilenamePattern_PatternWithMdExtension(t *testing.T) {
	if !MatchesFilenamePattern("abc12def", "abc12def.md") {
		t.Error("should strip .md from pattern too")
	}
}

func TestMatchesFilenamePattern_CaseInsensitive(t *testing.T) {
	if !MatchesFilenamePattern("ABC12DEF", "abc*") {
		t.Error("matching should be case-insensitive")
	}
}

func TestMatchesFilenamePattern_AllWildcard(t *testing.T) {
	if !MatchesFilenamePattern("anything", "*") {
		t.Error("* should match anything")
	}
}
