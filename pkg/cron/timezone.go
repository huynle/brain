package cron

import (
	"log/slog"
	"time"

	// Embed the IANA timezone database so tz lookups work even in minimal
	// container images that lack /usr/share/zoneinfo.
	_ "time/tzdata"
)

// LoadTimezone loads an IANA timezone location.
//
// Empty timezone returns time.UTC silently. Invalid timezone strings return
// time.UTC and emit a warn log so misconfiguration is visible without
// breaking automation scheduling.
//
// This helper is the single source of truth for timezone resolution across
// both task-level scheduled tasks (internal/runner/schedule.go) and
// automation-level cron triggers (internal/service/automation_service.go).
func LoadTimezone(timezone string) *time.Location {
	if timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		slog.Warn("cron: invalid timezone, defaulting to UTC",
			"timezone", timezone,
			"error", err,
		)
		return time.UTC
	}
	return loc
}
