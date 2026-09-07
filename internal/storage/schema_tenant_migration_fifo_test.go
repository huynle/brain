//go:build darwin || linux

package storage

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTenantMigrationRejectsFIFO(t *testing.T) {
	// Only the child enters the potentially blocking open. CommandContext kills
	// it at the deadline and CombinedOutput waits/reaps it (including pipe-copy
	// goroutines). The parent owns all fixtures, so SIGKILL leaks no temp trees.
	if path := os.Getenv("BRAIN_P4_FIFO_CHILD"); path != "" {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = migrateTenantSchema(ctx, db, nil)
		if err == nil || !strings.Contains(err.Error(), "CAS object not regular") {
			t.Fatalf("expected nonregular CAS rejection, got %v", err)
		}
		migrationFK(t, db)
		return
	}
	for _, published := range []bool{false, true} {
		name := "source"
		if published {
			name = "published"
		}
		t.Run(name, func(t *testing.T) {
			db, blob := completeMigrationFixture(t)
			if published {
				if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
					t.Fatal(err)
				}
			}
			before := map[string][]string{}
			for _, table := range []string{"sqlite_schema", "schema_version", "sqlite_sequence", "notes", "attachments", "tenant_roots"} {
				before[table] = relationalSnapshot(t, db, table)
			}
			if err := os.Remove(blob); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(blob, 0600); err != nil {
				t.Fatal(err)
			}
			var seq int
			var schema, path string
			if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &schema, &path); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTenantMigrationRejectsFIFO$")
			cmd.Env = append(os.Environ(), "BRAIN_P4_FIFO_CHILD="+path)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("CAS validation blocked on FIFO beyond context cancellation; child killed and reaped")
			}
			if err != nil {
				t.Fatalf("FIFO rejection child: %v\n%s", err, out)
			}
			for table, want := range before {
				if !reflect.DeepEqual(want, relationalSnapshot(t, db, table)) {
					t.Fatalf("FIFO rejection changed %s", table)
				}
			}
			info, err := os.Lstat(blob)
			if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("FIFO was mutated: %v %v", info, err)
			}
		})
	}
}
