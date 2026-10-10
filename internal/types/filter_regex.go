package types

// This file implements the "re:<pattern>" filter form used by
// MatchFilterValue (design: docs/plans/2026-10-08-automation-scheduling-design.md,
// addendum decision #12).
//
// Patterns are RE2 (Go regexp), so matching is linear in the input and a
// hostile pattern cannot backtrack catastrophically. The remaining costs are
// bounded explicitly:
//
//   - a pattern may be at most maxFilterRegexPatternChars characters;
//   - only the first maxFilterRegexInputBytes bytes of the actual value are
//     matched;
//   - compiled patterns live in a bounded LRU cache.
//
// At save time callers reject bad patterns with ValidateFilterValue. At run
// time a pattern that is invalid or oversized (saved before validation
// existed, or written out of band) matches nothing and logs one warning per
// cache entry, so a bad filter fails closed without flooding the log.

import (
	"container/list"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// regexFilterPrefix marks a filter expression as an RE2 pattern.
	regexFilterPrefix = "re:"
	// maxFilterRegexPatternChars caps a pattern's length in characters
	// (runes), excluding the "re:" prefix.
	maxFilterRegexPatternChars = 512
	// maxFilterRegexInputBytes caps how much of the actual value a pattern
	// sees; bytes past it are ignored, so "$" anchors at the cut.
	maxFilterRegexInputBytes = 4 << 10
	// filterRegexCacheCapacity bounds the shared compile cache.
	filterRegexCacheCapacity = 256
	// oversizedPatternPreviewChars is how much of an oversized pattern a
	// warning quotes.
	oversizedPatternPreviewChars = 64
)

// ValidateFilterValue reports whether a filter expression is usable. It
// returns nil for every form other than "re:" ("*", "in:", "has:", exact),
// because those accept any string. For "re:<pattern>" it returns an error
// naming the problem when the pattern is empty, longer than 512 characters,
// or not valid RE2 syntax; compile errors wrap the *regexp/syntax.Error.
//
// Use it at save time; MatchFilterValue applies the same rules at run time
// and treats a rejected pattern as matching nothing.
func ValidateFilterValue(expr string) error {
	pattern, ok := parseRegexFilter(expr)
	if !ok {
		return nil
	}
	_, err := compileFilterRegex(pattern)
	return err
}

// MatchFilterCaptures reports the same match as MatchFilterValue and, when the
// expression is a "re:" pattern that matches, the named capture groups of that
// match (name -> captured text; a group that did not participate is ""). It
// shares the compile cache and input limits with MatchFilterValue. For every
// other form, and for a rejected or non-matching pattern, the captures are nil.
//
// Only named groups are returned. Unnamed groups never appear, so a pattern's
// capture names are the only keys a caller can rely on.
func MatchFilterCaptures(actual, filterExpr string) (bool, map[string]string) {
	if pattern, ok := parseRegexFilter(filterExpr); ok {
		return defaultFilterRegexCache.matchCaptures(pattern, actual)
	}
	return MatchFilterValue(actual, filterExpr), nil
}

// parseRegexFilter returns the pattern of a "re:<pattern>" expression. The
// pattern is taken verbatim (no trimming): whitespace is significant in a
// regular expression. The second return value is false for any other form.
func parseRegexFilter(filterExpr string) (string, bool) {
	if !strings.HasPrefix(filterExpr, regexFilterPrefix) {
		return "", false
	}
	return filterExpr[len(regexFilterPrefix):], true
}

// compileFilterRegex applies the "re:" rules to a pattern and compiles it.
// An empty pattern is rejected so the form fails closed, like "has:" with no
// operand, instead of matching every value.
func compileFilterRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, errors.New("re: filter pattern is empty")
	}
	if filterPatternTooLong(pattern) {
		return nil, fmt.Errorf("re: filter pattern is %d characters; the limit is %d",
			utf8.RuneCountInString(pattern), maxFilterRegexPatternChars)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re: filter pattern does not compile: %w", err)
	}
	return re, nil
}

// filterPatternTooLong reports whether pattern exceeds the character limit.
// A string's byte length bounds its rune count, so short patterns skip the
// count.
func filterPatternTooLong(pattern string) bool {
	return len(pattern) > maxFilterRegexPatternChars &&
		utf8.RuneCountInString(pattern) > maxFilterRegexPatternChars
}

// truncateFilterInput returns at most the first maxFilterRegexInputBytes
// bytes of s. A rune straddling the limit is dropped whole rather than split
// into an invalid sequence, which RE2 would read as U+FFFD.
func truncateFilterInput(s string) string {
	limit := maxFilterRegexInputBytes
	if len(s) <= limit {
		return s
	}
	// A valid rune spans at most utf8.UTFMax bytes, so its start is within
	// that distance of the cut. If none is found the input is not valid
	// UTF-8 and the byte cut stands.
	for i := limit; i > limit-utf8.UTFMax && i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i]
		}
	}
	return s[:limit]
}

// defaultFilterRegexCache serves MatchFilterValue.
var defaultFilterRegexCache = newFilterRegexCache(filterRegexCacheCapacity, nil)

// filterRegexKey identifies a cached pattern. Oversized patterns are keyed by
// their SHA-256 digest so the cache's memory stays bounded however long they
// are; the flag keeps a digest from ever colliding with a real pattern.
type filterRegexKey struct {
	oversized bool
	text      string
}

func filterRegexKeyFor(pattern string) filterRegexKey {
	if filterPatternTooLong(pattern) {
		sum := sha256.Sum256([]byte(pattern))
		return filterRegexKey{oversized: true, text: string(sum[:])}
	}
	return filterRegexKey{text: pattern}
}

// filterRegexEntry is a cached compile result; re is nil when the pattern
// was rejected, so failures are cached (and warned about) once too.
type filterRegexEntry struct {
	key filterRegexKey
	re  *regexp.Regexp
}

// filterRegexCache is a concurrency-safe LRU cache of compiled "re:"
// patterns holding at most capacity entries.
type filterRegexCache struct {
	capacity int
	logger   *slog.Logger // nil means slog.Default() at log time

	mu    sync.Mutex
	order *list.List // of *filterRegexEntry, most recently used first
	items map[filterRegexKey]*list.Element
}

func newFilterRegexCache(capacity int, logger *slog.Logger) *filterRegexCache {
	return &filterRegexCache{
		capacity: capacity,
		logger:   logger,
		order:    list.New(),
		items:    make(map[filterRegexKey]*list.Element),
	}
}

// match reports whether pattern matches actual (truncated to the input
// limit). A rejected pattern matches nothing.
func (c *filterRegexCache) match(pattern, actual string) bool {
	re := c.get(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(truncateFilterInput(actual))
}

// matchCaptures is match that also returns the named capture groups of the
// first match. A rejected pattern, or one that does not match, returns nil.
func (c *filterRegexCache) matchCaptures(pattern, actual string) (bool, map[string]string) {
	re := c.get(pattern)
	if re == nil {
		return false, nil
	}
	found := re.FindStringSubmatch(truncateFilterInput(actual))
	if found == nil {
		return false, nil
	}
	captures := make(map[string]string)
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		captures[name] = found[i]
	}
	return true, captures
}

// get returns the compiled pattern, or nil if it was rejected. Compilation
// runs outside the lock so a cold pattern does not stall other matchers; if
// two goroutines race on the same pattern, the first insert wins and only
// that insert logs, so each cache entry warns at most once.
func (c *filterRegexCache) get(pattern string) *regexp.Regexp {
	key := filterRegexKeyFor(pattern)
	if re, ok := c.lookup(key); ok {
		return re
	}
	re, err := compileFilterRegex(pattern)
	if cached, inserted := c.insert(key, re); !inserted {
		return cached
	}
	if err != nil {
		c.warn(key, pattern, err)
	}
	return re
}

// lookup returns a cached result and marks it most recently used.
func (c *filterRegexCache) lookup(key filterRegexKey) (*regexp.Regexp, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*filterRegexEntry).re, true
}

// insert caches re under key, evicting least recently used entries beyond
// capacity. If key is already cached (a concurrent insert won), it returns
// that result and false instead.
func (c *filterRegexCache) insert(key filterRegexKey, re *regexp.Regexp) (*regexp.Regexp, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*filterRegexEntry).re, false
	}
	c.items[key] = c.order.PushFront(&filterRegexEntry{key: key, re: re})
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*filterRegexEntry).key)
	}
	return re, true
}

// warn logs a rejected pattern. An oversized pattern is quoted only as a
// short preview; its error already states the length and the limit.
func (c *filterRegexCache) warn(key filterRegexKey, pattern string, err error) {
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	if key.oversized {
		pattern = previewRunes(pattern, oversizedPatternPreviewChars) + "…"
	}
	logger.Warn("re: filter pattern rejected; it matches nothing",
		"pattern", pattern,
		"error", err)
}

// previewRunes returns the first n runes of s.
func previewRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// len returns the number of cached patterns.
func (c *filterRegexCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// contains reports whether pattern is cached, without touching its recency.
func (c *filterRegexCache) contains(pattern string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.items[filterRegexKeyFor(pattern)]
	return ok
}
