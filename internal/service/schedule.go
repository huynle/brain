package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CronField represents a single parsed cron field (minute, hour, etc.).
type CronField struct {
	Any    bool  // true if the field matches all values in its range
	Values []int // sorted list of matching values
}

// CronSchedule represents a parsed 5-field cron expression.
type CronSchedule struct {
	Minute     CronField
	Hour       CronField
	DayOfMonth CronField
	Month      CronField
	DayOfWeek  CronField
}

// cronFieldSpec defines the valid range for a cron field.
type cronFieldSpec struct {
	min  int
	max  int
	name string
}

var cronSpecs = map[string]cronFieldSpec{
	"minute":     {min: 0, max: 59, name: "minute"},
	"hour":       {min: 0, max: 23, name: "hour"},
	"dayOfMonth": {min: 1, max: 31, name: "dayOfMonth"},
	"month":      {min: 1, max: 12, name: "month"},
	"dayOfWeek":  {min: 0, max: 6, name: "dayOfWeek"},
}

// ParseCronExpression parses a 5-field cron expression into a CronSchedule.
// Fields: minute hour dayOfMonth month dayOfWeek
// Supports: *, ranges (1-5), steps (*/15), comma-separated (1,3,5), and combinations.
func ParseCronExpression(expr string) (*CronSchedule, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("invalid cron expression: expected 5 fields, got %d", len(fields))
	}

	minute, err := parseCronField(fields[0], cronSpecs["minute"])
	if err != nil {
		return nil, err
	}
	hour, err := parseCronField(fields[1], cronSpecs["hour"])
	if err != nil {
		return nil, err
	}
	dom, err := parseCronField(fields[2], cronSpecs["dayOfMonth"])
	if err != nil {
		return nil, err
	}
	month, err := parseCronField(fields[3], cronSpecs["month"])
	if err != nil {
		return nil, err
	}
	dow, err := parseCronField(fields[4], cronSpecs["dayOfWeek"])
	if err != nil {
		return nil, err
	}

	return &CronSchedule{
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dom,
		Month:      month,
		DayOfWeek:  dow,
	}, nil
}

// GetNextRun finds the next time matching the schedule after the given time.
// All calculations are in UTC.
func GetNextRun(schedule *CronSchedule, after time.Time) time.Time {
	// Start from the next minute boundary
	probe := after.UTC().Truncate(time.Minute).Add(time.Minute)

	// Search up to 5 years of minutes
	maxIterations := 60 * 24 * 366 * 5
	for i := 0; i < maxIterations; i++ {
		if matchesSchedule(schedule, probe) {
			return probe
		}
		probe = probe.Add(time.Minute)
	}

	// Should not happen for valid schedules
	return time.Time{}
}

// GenerateRunID creates a run ID in the format YYYYMMDD-HHmm-<6-char-random>.
func GenerateRunID(triggerTime time.Time) string {
	t := triggerTime.UTC()
	b := make([]byte, 3) // 3 bytes = 6 hex chars
	_, _ = rand.Read(b)
	suffix := hex.EncodeToString(b)
	return fmt.Sprintf("%04d%02d%02d-%02d%02d-%s",
		t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), suffix)
}

// matchesSchedule checks if a time matches the cron schedule.
func matchesSchedule(schedule *CronSchedule, t time.Time) bool {
	if !matchesField(schedule.Minute, t.Minute()) {
		return false
	}
	if !matchesField(schedule.Hour, t.Hour()) {
		return false
	}
	if !matchesField(schedule.Month, int(t.Month())) {
		return false
	}

	domMatch := matchesField(schedule.DayOfMonth, t.Day())
	dowMatch := matchesField(schedule.DayOfWeek, int(t.Weekday()))

	// Standard cron behavior for day fields:
	// If both are *, match any day.
	// If only one is restricted, use that one.
	// If both are restricted, match if EITHER matches (OR logic).
	if schedule.DayOfMonth.Any && schedule.DayOfWeek.Any {
		return true
	}
	if schedule.DayOfMonth.Any {
		return dowMatch
	}
	if schedule.DayOfWeek.Any {
		return domMatch
	}
	return domMatch || dowMatch
}

// matchesField checks if a value matches a cron field.
func matchesField(field CronField, value int) bool {
	if field.Any {
		return true
	}
	for _, v := range field.Values {
		if v == value {
			return true
		}
	}
	return false
}

// parseCronField parses a single cron field string.
func parseCronField(raw string, spec cronFieldSpec) (CronField, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return CronField{}, fmt.Errorf("empty %s field", spec.name)
	}

	values := make(map[int]bool)
	parts := strings.Split(trimmed, ",")

	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return CronField{}, fmt.Errorf("invalid %s token: %s", spec.name, raw)
		}

		segments := strings.SplitN(token, "/", 2)

		base := segments[0]
		step := 1
		if len(segments) == 2 {
			s, err := strconv.Atoi(segments[1])
			if err != nil {
				return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, segments[1])
			}
			step = s
		}

		if base == "*" {
			if err := addRange(values, spec.min, spec.max, step, spec, token); err != nil {
				return CronField{}, err
			}
			continue
		}

		if strings.Contains(base, "-") {
			rangeParts := strings.SplitN(base, "-", 2)
			if len(rangeParts) != 2 {
				return CronField{}, fmt.Errorf("invalid %s token: %s", spec.name, token)
			}
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, rangeParts[0])
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, rangeParts[1])
			}
			if err := addRange(values, start, end, step, spec, token); err != nil {
				return CronField{}, err
			}
			continue
		}

		// Number with step: e.g., "5/15" means starting at 5, every 15
		if len(segments) == 2 {
			start, err := strconv.Atoi(base)
			if err != nil {
				return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, base)
			}
			if err := addRange(values, start, spec.max, step, spec, token); err != nil {
				return CronField{}, err
			}
			continue
		}

		// Single value
		value, err := strconv.Atoi(base)
		if err != nil {
			return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, base)
		}
		if value < spec.min || value > spec.max {
			return CronField{}, fmt.Errorf("invalid %s value: %s", spec.name, token)
		}
		values[value] = true
	}

	sorted := sortedKeys(values)
	isAny := len(sorted) == (spec.max - spec.min + 1)

	return CronField{
		Any:    isAny,
		Values: sorted,
	}, nil
}

// addRange adds values from start to end (inclusive) with the given step.
func addRange(values map[int]bool, start, end, step int, spec cronFieldSpec, rawToken string) error {
	if start < spec.min || end > spec.max || start > end {
		return fmt.Errorf("invalid %s token: %s", spec.name, rawToken)
	}
	if step <= 0 {
		return fmt.Errorf("invalid %s step: %s", spec.name, rawToken)
	}
	for v := start; v <= end; v += step {
		values[v] = true
	}
	return nil
}

// sortedKeys returns the sorted keys of a map[int]bool.
func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
