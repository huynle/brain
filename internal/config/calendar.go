package config

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/huynle/brain-api/pkg/marketcal"
)

// defaultCalendarPoll is how often an ics calendar is re-read when poll is unset.
const defaultCalendarPoll = 5 * time.Minute

// minCalendarPoll is the shortest poll interval an ics calendar may request.
const minCalendarPoll = time.Minute

// supportedMarket is the only market a builtin calendar may name.
const supportedMarket = "XNYS"

// calendarNamePattern is the shape of a calendar name, as used in config and
// in trigger.calendar and skip/only_if_event.calendar.
var calendarNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// PollInterval returns how often an ics calendar is re-read: poll, or five
// minutes when it is unset. Validate rejects an unparseable poll, so the
// fallback only guards callers that skip validation.
func (c CalendarConfig) PollInterval() time.Duration {
	if c.Poll == "" {
		return defaultCalendarPoll
	}
	d, err := time.ParseDuration(c.Poll)
	if err != nil {
		return defaultCalendarPoll
	}
	return d
}

// validateCalendars checks every configured calendar source, in name order,
// and returns one message per problem. Messages name the field and never the
// value of a URL source, which is secret-adjacent.
func validateCalendars(calendars map[string]CalendarConfig) []string {
	names := make([]string, 0, len(calendars))
	for name := range calendars {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		errs = append(errs, validateCalendar(name, calendars[name])...)
	}
	return errs
}

func validateCalendar(name string, c CalendarConfig) []string {
	if !calendarNamePattern.MatchString(name) {
		return []string{fmt.Sprintf("server.calendars %q: name must match ^[a-z0-9][a-z0-9_-]*$", name)}
	}
	prefix := "server.calendars." + name
	switch c.Type {
	case "builtin":
		return validateBuiltinCalendar(prefix, c)
	case "ics":
		return validateICSCalendar(prefix, c)
	default:
		return []string{fmt.Sprintf("%s.type must be builtin|ics (got %q)", prefix, c.Type)}
	}
}

// validateBuiltinCalendar checks a market day calendar: its market and its
// extra dates, and no fields that only an ics feed uses.
func validateBuiltinCalendar(prefix string, c CalendarConfig) []string {
	var errs []string
	if c.Market != supportedMarket {
		errs = append(errs, fmt.Sprintf("%s.market must be %s (the only supported market)", prefix, supportedMarket))
	}
	if c.URLEnv != "" {
		errs = append(errs, prefix+".url_env only applies to ics calendars")
	}
	if c.URLFile != "" {
		errs = append(errs, prefix+".url_file only applies to ics calendars")
	}
	if c.Poll != "" {
		errs = append(errs, prefix+".poll only applies to ics calendars")
	}
	closed, closedErrs := parseExtraDates(prefix+".extra_closed", c.ExtraClosed)
	open, openErrs := parseExtraDates(prefix+".extra_open", c.ExtraOpen)
	errs = append(errs, closedErrs...)
	errs = append(errs, openErrs...)
	for d := range open {
		if closed[d] {
			errs = append(errs, fmt.Sprintf("%s.extra_open: %s is also listed in extra_closed", prefix, d))
		}
	}
	return errs
}

// validateICSCalendar checks an iCalendar feed: exactly one URL source, a poll
// of at least a minute, and no fields that only builtin calendars use.
func validateICSCalendar(prefix string, c CalendarConfig) []string {
	var errs []string
	if c.Market != "" {
		errs = append(errs, prefix+".market only applies to builtin calendars")
	}
	if len(c.ExtraClosed) > 0 {
		errs = append(errs, prefix+".extra_closed only applies to builtin calendars")
	}
	if len(c.ExtraOpen) > 0 {
		errs = append(errs, prefix+".extra_open only applies to builtin calendars")
	}
	if (c.URLEnv == "") == (c.URLFile == "") {
		errs = append(errs, prefix+".url_env: set exactly one of url_env or url_file")
	}
	if c.Poll != "" {
		d, err := time.ParseDuration(c.Poll)
		switch {
		case err != nil:
			errs = append(errs, prefix+`.poll must be a duration such as "5m"`)
		case d < minCalendarPoll:
			errs = append(errs, prefix+".poll must be at least 1m")
		}
	}
	return errs
}

// parseExtraDates parses a list of strict YYYY-MM-DD dates and returns them as
// a set. Each bad entry gets its own message naming its index.
func parseExtraDates(field string, values []string) (map[marketcal.Date]bool, []string) {
	set := make(map[marketcal.Date]bool, len(values))
	var errs []string
	for i, s := range values {
		d, err := marketcal.ParseDate(s)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s[%d]: %v", field, i, err))
			continue
		}
		set[d] = true
	}
	return set, errs
}
