// Package calendar parses iCalendar (RFC 5545) feeds and expands their
// events into concrete occurrences over a time window.
//
// It is pure: no network, configuration, or service code. Fetching and
// caching feeds is the poller's job; this package only turns bytes into
// []Occurrence.
//
// Decoding is delegated to github.com/emersion/go-ical and recurrence rules
// to github.com/teambition/rrule-go. The wrapper adds what those libraries
// leave out: input-size and nesting guards, panic recovery around the
// decoder, TZID → IANA mapping (including Windows zone names), RECURRENCE-ID
// overrides, cancellation, all-day date semantics, and runaway-rule caps.
package calendar

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// MaxFeedBytes is the largest feed Parse accepts. It matches the ICS fetch
// cap (design addendum, decision 15).
const MaxFeedBytes = 10 << 20

const (
	// maxNestingDepth bounds BEGIN/END nesting. Real feeds need 3
	// (VCALENDAR > VEVENT > VALARM); the decoder recurses once per level.
	maxNestingDepth = 8
	// maxWarnings bounds Feed.Warnings so a hostile feed cannot grow it
	// without limit. Further warnings are counted in a final summary line.
	maxWarnings = 100
)

var (
	// ErrFeedTooLarge is returned when the input exceeds MaxFeedBytes.
	ErrFeedTooLarge = errors.New("calendar: feed exceeds size limit")
	// ErrMalformedFeed wraps every structural decoding failure.
	ErrMalformedFeed = errors.New("calendar: malformed feed")
)

// Feed is a parsed calendar. It is immutable after Parse and safe for
// concurrent Expand calls.
type Feed struct {
	// Warnings lists per-event problems that did not fail the whole feed:
	// events skipped for a missing or invalid DTSTART, recurrence rules
	// ignored as invalid or unsafe, unresolvable TZIDs treated as floating
	// time, and unparsable EXDATE/RDATE values. They name event UIDs, never
	// feed URLs.
	Warnings []string

	groups []*eventGroup
}

// eventGroup is every VEVENT sharing one UID: at most one master plus its
// RECURRENCE-ID overrides.
type eventGroup struct {
	uid       string
	master    *vevent
	overrides []*vevent
}

// icsTime is a parsed DATE or DATE-TIME value.
type icsTime struct {
	// t is the absolute instant, or — when floating — the wall clock
	// stored in UTC, re-anchored by in().
	t        time.Time
	floating bool // no zone: resolved in the caller's default location
	date     bool // VALUE=DATE: an all-day date, always floating
}

// in returns the value as an instant, placing floating values in def.
func (x icsTime) in(def *time.Location) time.Time {
	if !x.floating {
		return x.t
	}
	y, m, d := x.t.Date()
	hh, mm, ss := x.t.Clock()
	return time.Date(y, m, d, hh, mm, ss, 0, def)
}

// vevent is one parsed VEVENT component.
type vevent struct {
	uid, summary, description, location string

	cancelled bool
	sequence  int
	order     int // position in the feed, for deterministic tie-breaks

	start        icsTime
	end          *icsTime
	duration     *time.Duration
	rrule        string // validated RRULE value; empty when not recurring
	rdates       []icsTime
	exdates      []icsTime
	recurrenceID *icsTime
}

// Parse reads and decodes an iCalendar feed. Structural problems (not a
// VCALENDAR, truncated input, bad content lines, excessive nesting) return
// an error wrapping ErrMalformedFeed; input over MaxFeedBytes returns
// ErrFeedTooLarge. Problems confined to one event skip or degrade that
// event and are reported in Feed.Warnings.
func Parse(r io.Reader) (*Feed, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("calendar: read feed: %w", err)
	}
	if len(data) > MaxFeedBytes {
		return nil, ErrFeedTooLarge
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 BOM (Outlook)

	if err := checkNesting(data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedFeed, err)
	}
	cal, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedFeed, err)
	}

	p := &parser{
		feed:     &Feed{},
		tz:       newTZResolver(cal),
		byUID:    map[string]*eventGroup{},
		tzWarned: map[string]bool{},
	}
	for _, child := range cal.Children {
		if child.Name == ical.CompEvent {
			p.addEvent(child)
		}
	}
	p.flushWarnings()
	return p.feed, nil
}

// decode runs the go-ical decoder, converting its panics into errors: some
// malformed property parameters make it index past the end of the line.
func decode(data []byte) (cal *ical.Calendar, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			cal, err = nil, fmt.Errorf("decoder panic: %v", rec)
		}
	}()
	return ical.NewDecoder(bytes.NewReader(data)).Decode()
}

// checkNesting rejects feeds nested deeper than maxNestingDepth before the
// recursive decoder sees them. Lines are unfolded first so a BEGIN split
// across a fold cannot evade the check.
func checkNesting(data []byte) error {
	depth := 0
	var head []byte // first bytes of the current logical (unfolded) line
	flush := func() error {
		switch {
		case hasPrefixFold(head, "BEGIN:"):
			depth++
			if depth > maxNestingDepth {
				return fmt.Errorf("components nested deeper than %d", maxNestingDepth)
			}
		case hasPrefixFold(head, "END:"):
			depth--
		}
		head = head[:0]
		return nil
	}
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			line = line[1:] // continuation of the previous logical line
		} else if err := flush(); err != nil {
			return err
		}
		if room := len("BEGIN:") - len(head); room > 0 {
			head = append(head, line[:min(room, len(line))]...)
		}
	}
	return flush()
}

func hasPrefixFold(b []byte, prefix string) bool {
	return len(b) >= len(prefix) && strings.EqualFold(string(b[:len(prefix)]), prefix)
}

type parser struct {
	feed       *Feed
	tz         *tzResolver
	byUID      map[string]*eventGroup
	order      int
	suppressed int
	tzWarned   map[string]bool // uid + "\x00" + tzid already warned about
}

func (p *parser) warnf(uid, format string, args ...any) {
	if len(p.feed.Warnings) >= maxWarnings {
		p.suppressed++
		return
	}
	p.feed.Warnings = append(p.feed.Warnings, fmt.Sprintf("event %q: ", truncate(uid, 80))+fmt.Sprintf(format, args...))
}

func (p *parser) flushWarnings() {
	if p.suppressed > 0 {
		p.feed.Warnings = append(p.feed.Warnings, fmt.Sprintf("%d more warnings suppressed", p.suppressed))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// addEvent parses one VEVENT and files it under its UID group.
func (p *parser) addEvent(c *ical.Component) {
	ev := &vevent{
		uid:         propText(c, ical.PropUID),
		summary:     propText(c, ical.PropSummary),
		description: propText(c, ical.PropDescription),
		location:    propText(c, ical.PropLocation),
		cancelled:   strings.EqualFold(propText(c, ical.PropStatus), "CANCELLED"),
		order:       p.order,
	}
	p.order++
	if seq := c.Props.Get(ical.PropSequence); seq != nil {
		ev.sequence, _ = strconv.Atoi(strings.TrimSpace(seq.Value))
	}

	startProp := c.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		p.warnf(ev.uid, "skipped: no DTSTART")
		return
	}
	start, err := p.parseTime(ev.uid, startProp, startProp.Value, nil)
	if err != nil {
		p.warnf(ev.uid, "skipped: invalid DTSTART: %v", err)
		return
	}
	ev.start = start
	startLoc := zoneOf(start)

	if endProp := c.Props.Get(ical.PropDateTimeEnd); endProp != nil {
		if end, err := p.parseTime(ev.uid, endProp, endProp.Value, startLoc); err != nil {
			p.warnf(ev.uid, "ignored invalid DTEND: %v", err)
		} else {
			ev.end = &end
		}
	} else if durProp := c.Props.Get(ical.PropDuration); durProp != nil {
		if d, err := durProp.Duration(); err != nil {
			p.warnf(ev.uid, "ignored invalid DURATION: %v", err)
		} else {
			ev.duration = &d
		}
	}

	if ridProp := c.Props.Get(ical.PropRecurrenceID); ridProp != nil {
		rid, err := p.parseTime(ev.uid, ridProp, ridProp.Value, startLoc)
		if err != nil {
			p.warnf(ev.uid, "skipped: invalid RECURRENCE-ID: %v", err)
			return
		}
		ev.recurrenceID = &rid
	} else {
		p.parseRecurrence(c, ev, startLoc)
	}

	p.file(ev)
}

// parseRecurrence fills the master's RRULE, RDATE, and EXDATE fields.
func (p *parser) parseRecurrence(c *ical.Component, ev *vevent, startLoc *time.Location) {
	if ruleProp := c.Props.Get(ical.PropRecurrenceRule); ruleProp != nil {
		raw := strings.ToUpper(strings.TrimSpace(ruleProp.Value))
		if _, err := buildRule(raw, ev.start.in(time.UTC), ev.start.date); err != nil {
			p.warnf(ev.uid, "recurrence ignored: %v", err)
		} else {
			ev.rrule = raw
		}
	}
	ev.rdates = p.parseTimeList(ev.uid, c.Props.Values(ical.PropRecurrenceDates), startLoc)
	ev.exdates = p.parseTimeList(ev.uid, c.Props.Values(ical.PropExceptionDates), startLoc)
}

// parseTimeList parses comma-separated RDATE/EXDATE values. RDATE PERIOD
// values contribute their start only.
func (p *parser) parseTimeList(uid string, props []ical.Prop, inherit *time.Location) []icsTime {
	var out []icsTime
	for i := range props {
		for _, v := range strings.Split(props[i].Value, ",") {
			v, _, _ = strings.Cut(strings.TrimSpace(v), "/")
			if v == "" {
				continue
			}
			t, err := p.parseTime(uid, &props[i], v, inherit)
			if err != nil {
				p.warnf(uid, "ignored invalid %s value: %v", props[i].Name, err)
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// file adds ev to its UID group. Events without a UID cannot be overridden
// and always form their own group.
func (p *parser) file(ev *vevent) {
	g := p.byUID[ev.uid]
	if g == nil || ev.uid == "" {
		g = &eventGroup{uid: ev.uid}
		p.feed.groups = append(p.feed.groups, g)
		if ev.uid != "" {
			p.byUID[ev.uid] = g
		}
	}
	if ev.recurrenceID != nil {
		g.overrides = append(g.overrides, ev)
		return
	}
	// Duplicate masters: the highest SEQUENCE wins, then the later one.
	if g.master == nil || ev.sequence >= g.master.sequence {
		g.master = ev
	}
}

const (
	dateLayout        = "20060102"
	dateTimeLayout    = "20060102T150405"
	dateTimeUTCLayout = "20060102T150405Z"
)

// parseTime parses a DATE or DATE-TIME value of prop. A zone-less
// DATE-TIME uses inherit when non-nil (EXDATE/RDATE/RECURRENCE-ID follow
// their event's DTSTART zone), else it is floating.
func (p *parser) parseTime(uid string, prop *ical.Prop, value string, inherit *time.Location) (icsTime, error) {
	value = strings.TrimSpace(value)
	isDate := strings.EqualFold(prop.Params.Get(ical.ParamValue), string(ical.ValueDate)) ||
		len(value) == len(dateLayout)
	if isDate {
		t, err := time.Parse(dateLayout, value)
		if err != nil {
			return icsTime{}, err
		}
		return icsTime{t: t, floating: true, date: true}, nil
	}
	if strings.HasSuffix(value, "Z") || strings.HasSuffix(value, "z") {
		t, err := time.Parse(dateTimeUTCLayout, strings.ToUpper(value))
		if err != nil {
			return icsTime{}, err
		}
		return icsTime{t: t}, nil
	}
	loc := inherit
	if tzid := prop.Params.Get(ical.PropTimezoneID); tzid != "" {
		loc = p.tz.resolve(tzid)
		if key := uid + "\x00" + tzid; loc == nil && !p.tzWarned[key] {
			p.tzWarned[key] = true
			p.warnf(uid, "unknown TZID %q, using floating time", truncate(tzid, 80))
		}
	}
	if loc == nil {
		t, err := time.Parse(dateTimeLayout, value)
		if err != nil {
			return icsTime{}, err
		}
		return icsTime{t: t, floating: true}, nil
	}
	t, err := time.ParseInLocation(dateTimeLayout, value, loc)
	if err != nil {
		return icsTime{}, err
	}
	return icsTime{t: t}, nil
}

// zoneOf is the location zone-less sibling values inherit from DTSTART:
// nil for floating values (they stay floating).
func zoneOf(x icsTime) *time.Location {
	if x.floating {
		return nil
	}
	return x.t.Location()
}

// buildRule parses an RRULE value anchored at dtstart. A DATE-valued UNTIL
// on a timed event is read as the end of that day, matching common clients.
func buildRule(raw string, dtstart time.Time, allDay bool) (*rrule.RRule, error) {
	opt, err := rrule.StrToROptionInLocation(raw, dtstart.Location())
	if err != nil {
		return nil, err
	}
	opt.Dtstart = dtstart
	if !allDay && untilIsDate(raw) {
		opt.Until = opt.Until.AddDate(0, 0, 1).Add(-time.Second)
	}
	return rrule.NewRRule(*opt)
}

func untilIsDate(raw string) bool {
	for _, part := range strings.Split(raw, ";") {
		if v, ok := strings.CutPrefix(part, "UNTIL="); ok {
			return len(v) == len(dateLayout)
		}
	}
	return false
}

// propText returns a TEXT property value, unescaped. Unlike go-ical's
// Prop.Text it keeps unescaped commas (go-ical truncates at the first one)
// and tolerates unknown escapes instead of failing.
func propText(c *ical.Component, name string) string {
	prop := c.Props.Get(name)
	if prop == nil {
		return ""
	}
	return unescapeText(prop.Value)
}

func unescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		default: // \\ \; \, and, leniently, any other escaped byte
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
