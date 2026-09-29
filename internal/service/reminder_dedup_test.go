package service

import (
	"fmt"
	"testing"
)

func TestReminderDedupCompositeConstraint(t *testing.T) {
	// SQLite's v29 partial unique index reports both columns. The private
	// migrated storage tests exercise the actual database error; service
	// fixtures deliberately do not expose/activate the dormant migration.
	for _, columns := range []string{"event_log.dedup_key", "event_log.tenant_id, event_log.dedup_key"} {
		err := fmt.Errorf("insert event: %w", fmt.Errorf("constraint failed: UNIQUE constraint failed: %s (2067)", columns))
		if !isDuplicateDedupKey(err) {
			t.Fatalf("claim duplicate not recognized: %v", err)
		}
	}
	if isDuplicateDedupKey(nil) || isDuplicateDedupKey(fmt.Errorf("database is locked")) {
		t.Fatal("non-duplicate recognized as claim")
	}
}
