package calendar

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture returns the raw bytes of a testdata feed.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// mustParse parses an inline feed body wrapped in a VCALENDAR envelope.
func mustParse(t *testing.T, body string) *Feed {
	t.Helper()
	f, err := Parse(strings.NewReader(wrapCalendar(body)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func wrapCalendar(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
		strings.TrimSpace(body) + "\r\nEND:VCALENDAR\r\n"
}

func TestParse_FixturesSucceedWithoutWarnings(t *testing.T) {
	for _, name := range []string{
		"google_weekly.ics",
		"allday.ics",
		"cancelled.ics",
		"outlook_windows_tz.ics",
	} {
		t.Run(name, func(t *testing.T) {
			f, err := Parse(bytes.NewReader(loadFixture(t, name)))
			if err != nil {
				t.Fatalf("Parse(%s): %v", name, err)
			}
			if f == nil {
				t.Fatal("Parse returned nil feed without error")
			}
			if len(f.Warnings) != 0 {
				t.Fatalf("unexpected warnings: %q", f.Warnings)
			}
		})
	}
}

func TestParse_MalformedFeedsReturnErrMalformedFeed(t *testing.T) {
	cases := map[string]string{
		"truncated fixture": string(loadFixture(t, "malformed_truncated.ics")),
		"empty input":       "",
		"html login page":   "<!DOCTYPE html>\n<html><body>Sign in</body></html>\n",
		"not a calendar":    "BEGIN:VEVENT\r\nUID:x\r\nEND:VEVENT\r\n",
		"mismatched END":    "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
		// Both shapes make the underlying decoder panic (index out of
		// range / unexpected character); Parse must turn that into an error.
		"param without colon": "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART;TZID=America/New_York\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"junk after quoted":   "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nX-A;P=\"q\"junk:v\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"nesting too deep": "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEGIN:X\r\n", 100) +
			strings.Repeat("END:X\r\n", 100) + "END:VCALENDAR\r\n",
		// Folding a BEGIN line must not let deep nesting slip past the guard.
		"folded deep nesting": "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEG\r\n IN:X\r\n", 100) +
			strings.Repeat("END:X\r\n", 100) + "END:VCALENDAR\r\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := Parse(strings.NewReader(input))
			if err == nil {
				t.Fatalf("expected error, got feed %+v", f)
			}
			if !errors.Is(err, ErrMalformedFeed) {
				t.Fatalf("error %v does not wrap ErrMalformedFeed", err)
			}
		})
	}
}

func TestParse_SizeGuard(t *testing.T) {
	envelope := wrapCalendar("X-PAD:")
	pad := MaxFeedBytes - len(envelope)

	atLimit := strings.Replace(envelope, "X-PAD:", "X-PAD:"+strings.Repeat("a", pad), 1)
	if len(atLimit) != MaxFeedBytes {
		t.Fatalf("test setup: got %d bytes, want %d", len(atLimit), MaxFeedBytes)
	}
	if _, err := Parse(strings.NewReader(atLimit)); err != nil {
		t.Fatalf("feed of exactly MaxFeedBytes should parse: %v", err)
	}

	overLimit := strings.Replace(envelope, "X-PAD:", "X-PAD:"+strings.Repeat("a", pad+1), 1)
	_, err := Parse(strings.NewReader(overLimit))
	if !errors.Is(err, ErrFeedTooLarge) {
		t.Fatalf("want ErrFeedTooLarge, got %v", err)
	}
}

func TestParse_AcceptsUTF8BOM(t *testing.T) {
	in := "\ufeff" + wrapCalendar("BEGIN:VEVENT\r\nUID:bom\r\nDTSTART:20251015T170000Z\r\nEND:VEVENT")
	f, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse with BOM: %v", err)
	}
	if len(f.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %q", f.Warnings)
	}
}

func TestParse_PerEventProblemsBecomeWarnings(t *testing.T) {
	f := mustParse(t, `
BEGIN:VEVENT
UID:no-start
SUMMARY:Missing DTSTART
END:VEVENT
BEGIN:VEVENT
UID:bad-start
DTSTART:2025-10-15
END:VEVENT
BEGIN:VEVENT
UID:bad-rule
DTSTART:20251015T170000Z
RRULE:FREQ=FORTNIGHTLY
END:VEVENT
BEGIN:VEVENT
UID:unknown-zone
DTSTART;TZID=Mars/Olympus_Mons:20251015T090000
END:VEVENT
BEGIN:VEVENT
UID:fine
DTSTART:20251015T170000Z
END:VEVENT`)

	want := []string{"no-start", "bad-start", "bad-rule", "unknown-zone"}
	if len(f.Warnings) != len(want) {
		t.Fatalf("got %d warnings %q, want %d", len(f.Warnings), f.Warnings, len(want))
	}
	for i, uid := range want {
		if !strings.Contains(f.Warnings[i], uid) {
			t.Errorf("warning %d = %q, want it to mention %q", i, f.Warnings[i], uid)
		}
	}
}
