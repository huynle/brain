package service

import "time"

// runAuditScheduledFor returns the audit slot as RFC3339 UTC, or "" for the
// zero time, so unscheduled runs write no scheduled_for value.
func runAuditScheduledFor(slot time.Time) string {
	if slot.IsZero() {
		return ""
	}
	return slot.UTC().Format(time.RFC3339)
}

// runAuditBindingTag returns the "binding:<id>" tag, or "" when no binding
// applied.
func runAuditBindingTag(binding string) string {
	if binding == "" {
		return ""
	}
	return "binding:" + binding
}
