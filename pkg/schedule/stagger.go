package schedule

import "time"

// FNV-1a 64-bit parameters (the same constants hash/fnv uses).
const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

// StaggerOffset returns the stable offset of one (automation, project) pair
// within a stagger window: FNV-1a 64 over automationID + "\x00" + project,
// modulo the window counted in whole seconds (addendum decision #8).
//
// The offset depends on nothing but its two arguments, so a project's runs
// never move when other projects are added or removed, and it is identical
// across processes and restarts — unlike hash/maphash, which is seeded per
// process and must never be used here.
//
// Offsets are whole seconds so every slot instant survives an RFC 3339
// round trip (run audits record scheduled_for at second precision; a
// sub-second slot would compare newer than its own audit and look due
// again). A window shorter than one second, zero or negative yields 0.
func StaggerOffset(automationID, project string, window time.Duration) time.Duration {
	seconds := uint64(window / time.Second)
	if window <= 0 || seconds == 0 {
		return 0
	}
	h := fnvOffset64
	h = fnv1a(h, automationID)
	h = fnv1a(h, "\x00")
	h = fnv1a(h, project)
	return time.Duration(h%seconds) * time.Second
}

// fnv1a folds s into an FNV-1a 64 running hash.
func fnv1a(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}
