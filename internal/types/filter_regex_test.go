package types

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp/syntax"
	"strings"
	"sync"
	"testing"
)

// The "re:" filter form (design addendum #12): RE2 via Go regexp, pattern
// ≤ 512 characters, matched input truncated at 4 KiB, bounded compile
// cache (256), and an invalid pattern at runtime matches nothing and warns.

func TestMatchFilterValue_Regex(t *testing.T) {
	tests := []struct {
		name       string
		actual     string
		filterExpr string
		want       bool
	}{
		// Case-insensitive flag + start anchor (the acceptance example).
		{"(?i) lower", "standup sync", "re:(?i)^standup", true},
		{"(?i) title case", "Standup Sync", "re:(?i)^standup", true},
		{"(?i) upper", "STANDUP", "re:(?i)^standup", true},
		{"start anchor rejects mid-string", "daily standup", "re:(?i)^standup", false},
		{"no (?i) is case-sensitive", "Standup", "re:^standup", false},

		// Anchors.
		{"both anchors exact", "standup", "re:^standup$", true},
		{"end anchor rejects suffix", "standup sync", "re:^standup$", false},
		{"end anchor alone", "daily sync", "re:sync$", true},
		{"unanchored finds substring", "daily sync meeting", "re:sync", true},
		{"unanchored miss", "daily meeting", "re:sync", false},

		// Empty actual: the regex decides (unlike "*", which needs non-empty).
		{"^$ matches empty actual", "", "re:^$", true},
		{"literal does not match empty actual", "", "re:foo", false},

		// Named capture groups compile (calendar triggers rely on them later).
		{"named capture group", "1:1 Alice", "re:(?i)^1:1 (?P<person>.+)$", true},

		// The pattern is taken verbatim: whitespace is significant in a regex.
		{"leading space is part of the pattern (miss)", "foo", "re: foo", false},
		{"leading space is part of the pattern (hit)", " foo", "re: foo", true},

		// Invalid patterns match nothing, never panic.
		{"unclosed group", "(", "re:(", false},
		{"unclosed class", "[a-", "re:[a-", false},
		{"perl lookahead unsupported by RE2", "x", "re:(?=x)", false},
		{"backreference unsupported by RE2", "aa", `re:(a)\1`, false},

		// Empty pattern fails CLOSED like "has:" — it would otherwise match
		// every value, including the empty one.
		{"empty pattern fails closed", "anything", "re:", false},
		{"empty pattern fails closed on empty actual", "", "re:", false},

		// The prefix is case-sensitive; anything else is still an exact match.
		{"RE: is an exact literal", "RE:foo", "RE:foo", true},
		{"RE: is not a regex", "foo", "RE:foo", false},
		{"re prefix without colon is exact", "re", "re", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchFilterValue(tt.actual, tt.filterExpr)
			if got != tt.want {
				t.Errorf("MatchFilterValue(%q, %q) = %v, want %v", tt.actual, tt.filterExpr, got, tt.want)
			}
		})
	}
}

func TestMatchFilterValue_RegexInputTruncatedAt4KiB(t *testing.T) {
	tests := []struct {
		name       string
		actual     string
		filterExpr string
		want       bool
	}{
		{"needle ending exactly at 4096 bytes is seen", strings.Repeat("a", 4090) + "needle", "re:needle", true},
		{"needle crossing 4096 bytes is cut", strings.Repeat("a", 4091) + "needle", "re:needle", false},
		{"needle far past 4096 bytes is not seen", strings.Repeat("a", 5000) + "needle", "re:needle", false},
		{"bytes past the limit are dropped before $", strings.Repeat("a", 4096) + "b", "re:^a+$", true},
		// A multi-byte rune straddling the limit is dropped whole, never
		// split into an invalid sequence (which RE2 would read as U+FFFD).
		{"straddling rune dropped whole", strings.Repeat("a", 4095) + "é", "re:^a+$", true},
		{"straddling rune not split into U+FFFD", strings.Repeat("a", 4095) + "é", `re:\x{FFFD}`, false},
		{"short input untouched", "é", "re:^é$", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchFilterValue(tt.actual, tt.filterExpr)
			if got != tt.want {
				t.Errorf("MatchFilterValue(len %d, %q) = %v, want %v", len(tt.actual), tt.filterExpr, got, tt.want)
			}
		})
	}
}

func TestMatchFilterValue_RegexPatternLengthLimit(t *testing.T) {
	ascii512 := strings.Repeat("a", 512)
	if !MatchFilterValue(ascii512, "re:"+ascii512) {
		t.Error("512-character pattern should be accepted and match")
	}

	ascii513 := strings.Repeat("a", 513)
	if MatchFilterValue(ascii513, "re:"+ascii513) {
		t.Error("513-character pattern should be rejected (match nothing)")
	}

	// The limit counts characters, not bytes: 512 two-byte runes is 1024
	// bytes and still within the limit.
	multi512 := strings.Repeat("é", 512)
	if !MatchFilterValue(multi512, "re:"+multi512) {
		t.Error("512-character multi-byte pattern should be accepted and match")
	}
	multi513 := strings.Repeat("é", 513)
	if MatchFilterValue(multi513, "re:"+multi513) {
		t.Error("513-character multi-byte pattern should be rejected")
	}
}

func TestValidateFilterValue(t *testing.T) {
	valid := []string{
		// Every non-"re:" form is always valid, whatever its content.
		"", "*", "completed", "in:a,b", "in:", "has:x", "has:",
		"RE:(", "re", "regex:(",
		// Valid "re:" forms.
		"re:(?i)^standup", "re:^standup$", "re:^$", "re:(?P<person>.+)",
		"re:" + strings.Repeat("a", 512),
		"re:" + strings.Repeat("é", 512),
	}
	for _, expr := range valid {
		if err := ValidateFilterValue(expr); err != nil {
			t.Errorf("ValidateFilterValue(%.40q) = %v, want nil", expr, err)
		}
	}

	invalid := []struct {
		name    string
		expr    string
		wantSub string
	}{
		{"empty pattern", "re:", "empty"},
		{"too long", "re:" + strings.Repeat("a", 513), "512"},
		{"too long multi-byte", "re:" + strings.Repeat("é", 513), "512"},
		{"unclosed group", "re:(", "missing closing )"},
		{"perl lookahead", "re:(?=x)", "invalid or unsupported Perl syntax"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilterValue(tt.expr)
			if err == nil {
				t.Fatalf("ValidateFilterValue(%.40q) = nil, want error", tt.expr)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not mention %q", err, tt.wantSub)
			}
			if !strings.Contains(err.Error(), "re:") {
				t.Errorf("error %q does not name the re: filter form", err)
			}
		})
	}

	// Compile failures wrap the regexp/syntax error so callers can inspect it.
	var synErr *syntax.Error
	if err := ValidateFilterValue("re:("); !errors.As(err, &synErr) {
		t.Errorf("compile error %v does not wrap *syntax.Error", err)
	}
}

// The validator and the runtime matcher must agree: anything the validator
// accepts compiles at runtime, anything it rejects matches nothing.
func TestValidateFilterValue_AgreesWithRuntime(t *testing.T) {
	for _, expr := range []string{"re:(", "re:", "re:(?=x)", "re:" + strings.Repeat("a", 513)} {
		if ValidateFilterValue(expr) == nil {
			t.Fatalf("expected %.40q to be invalid", expr)
		}
		for _, actual := range []string{"", "x", "(", strings.Repeat("a", 513)} {
			if MatchFilterValue(actual, expr) {
				t.Errorf("invalid %.40q matched %.40q", expr, actual)
			}
		}
	}
}

func TestTriggerConfig_MatchesFilters_Regex(t *testing.T) {
	fields := map[string]string{"project_id": "brain-api", "title": "Standup: Q4"}
	getField := func(key string) string { return fields[key] }

	tc := TriggerConfig{Filter: map[string]string{"project_id": "re:^brain-", "title": "re:(?i)^standup"}}
	if !tc.MatchesFilters(getField) {
		t.Error("MatchesFilters with matching re: filters = false, want true")
	}
	tc = TriggerConfig{Filter: map[string]string{"project_id": "re:^brain-", "title": "re:^standup"}}
	if tc.MatchesFilters(getField) {
		t.Error("MatchesFilters with one failing re: filter = true, want false")
	}
}

func TestFilterRegexCache_DefaultCapacityIs256(t *testing.T) {
	if got := defaultFilterRegexCache.capacity; got != 256 {
		t.Errorf("default filter regex cache capacity = %d, want 256", got)
	}
}

func TestFilterRegexCache_Bounded(t *testing.T) {
	c := newFilterRegexCache(256, discardLogger())

	for i := 0; i < 300; i++ {
		pattern := fmt.Sprintf("^p%d$", i)
		if !c.match(pattern, fmt.Sprintf("p%d", i)) {
			t.Fatalf("pattern %q did not match its own literal", pattern)
		}
	}
	if got := c.len(); got != 256 {
		t.Errorf("cache len after 300 distinct patterns = %d, want 256", got)
	}

	// Invalid and oversized patterns occupy slots too and stay bounded.
	for i := 0; i < 300; i++ {
		c.match(fmt.Sprintf("(%d", i), "x")
		c.match(strings.Repeat("b", 513)+fmt.Sprint(i), "x")
	}
	if got := c.len(); got > 256 {
		t.Errorf("cache len after invalid/oversized patterns = %d, want <= 256", got)
	}

	// An evicted pattern is recompiled correctly on next use.
	if !c.match("^p0$", "p0") || c.match("^p0$", "p1") {
		t.Error("evicted pattern ^p0$ did not recompile with correct semantics")
	}
}

func TestFilterRegexCache_RecentlyUsedSurvivesEviction(t *testing.T) {
	c := newFilterRegexCache(4, discardLogger())
	for _, p := range []string{"a", "b", "c", "d"} {
		c.match(p, p)
	}
	c.match("a", "a") // touch "a" so "b" is now least recently used
	c.match("e", "e") // evicts "b"

	if !c.contains("a") {
		t.Error("recently used pattern \"a\" was evicted")
	}
	if c.contains("b") {
		t.Error("least recently used pattern \"b\" was not evicted")
	}
	if got := c.len(); got != 4 {
		t.Errorf("len = %d, want 4", got)
	}
}

func TestFilterRegexCache_InvalidPatternWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	c := newFilterRegexCache(16, slog.New(slog.NewTextHandler(&buf, nil)))

	for i := 0; i < 3; i++ {
		if c.match("(", "(") {
			t.Fatal("invalid pattern matched")
		}
	}
	if got := strings.Count(buf.String(), "level=WARN"); got != 1 {
		t.Fatalf("warnings after 3 uses of one invalid pattern = %d, want 1\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "missing closing )") {
		t.Errorf("warning does not carry the compile error: %s", buf.String())
	}

	// A second, different invalid pattern gets its own warning.
	c.match("[a-", "x")
	if got := strings.Count(buf.String(), "level=WARN"); got != 2 {
		t.Errorf("warnings after a second invalid pattern = %d, want 2", got)
	}
}

func TestFilterRegexCache_OversizedPatternWarnsOnceWithoutLoggingIt(t *testing.T) {
	var buf bytes.Buffer
	c := newFilterRegexCache(16, slog.New(slog.NewTextHandler(&buf, nil)))
	huge := strings.Repeat("z", 100_000)

	for i := 0; i < 3; i++ {
		if c.match(huge, huge) {
			t.Fatal("oversized pattern matched")
		}
	}
	out := buf.String()
	if got := strings.Count(out, "level=WARN"); got != 1 {
		t.Fatalf("warnings after 3 uses of one oversized pattern = %d, want 1\n%.500s", got, out)
	}
	if len(out) > 2048 {
		t.Errorf("warning is %d bytes; an oversized pattern must not be logged in full", len(out))
	}
	if !strings.Contains(out, "512") {
		t.Errorf("warning does not state the limit: %s", out)
	}

	// A distinct oversized pattern is a distinct cache entry and warns again.
	c.match(strings.Repeat("y", 100_000), "y")
	if got := strings.Count(buf.String(), "level=WARN"); got != 2 {
		t.Errorf("warnings after a second oversized pattern = %d, want 2", got)
	}
}

func TestFilterRegexCache_ValidPatternDoesNotWarn(t *testing.T) {
	var buf bytes.Buffer
	c := newFilterRegexCache(16, slog.New(slog.NewTextHandler(&buf, nil)))
	c.match("^ok$", "ok")
	if buf.Len() != 0 {
		t.Errorf("valid pattern logged: %s", buf.String())
	}
}

func TestFilterRegexCache_ConcurrentUse(t *testing.T) {
	// Small capacity forces eviction under contention; run with -race.
	c := newFilterRegexCache(8, discardLogger())
	patterns := make([]string, 40)
	for i := range patterns {
		if i%5 == 0 {
			patterns[i] = fmt.Sprintf("(%d", i) // invalid
		} else {
			patterns[i] = fmt.Sprintf("^c%d$", i)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				i := (g*7 + n) % len(patterns)
				want := i%5 != 0
				if got := c.match(patterns[i], fmt.Sprintf("c%d", i)); got != want {
					select {
					case errs <- fmt.Sprintf("pattern %q: got %v want %v", patterns[i], got, want):
					default:
					}
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if got := c.len(); got > 8 {
		t.Errorf("cache len under concurrency = %d, want <= 8", got)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
