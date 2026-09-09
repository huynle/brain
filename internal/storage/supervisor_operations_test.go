package storage

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestSupervisorOperationConcurrentAdmissionAndScope(t *testing.T) {
	owner, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.Into(context.Background(), tenant.Local)
	var wg sync.WaitGroup
	results := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := store.BeginSupervisorOperation(ctx, "actor", "operation-1", "prompt", "digest")
			if err != nil {
				t.Error(err)
			}
			results <- created
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for created := range results {
		if created {
			count++
		}
	}
	if count != 1 {
		t.Fatal("delivery owners", count)
	}
	if err := store.FinishSupervisorOperation(ctx, "actor", "operation-1", "delivered", "accepted, not completed"); err != nil {
		t.Fatal(err)
	}
	receipt, digest, err := store.SupervisorOperation(ctx, "actor", "operation-1")
	if err != nil || receipt.State != "delivered" || digest != "digest" {
		t.Fatal(receipt, err)
	}
	hidden, _, err := store.SupervisorOperation(ctx, "another-actor", "operation-1")
	if err != nil || hidden != nil {
		t.Fatal("cross-principal receipt", hidden, err)
	}
	if _, err := store.BeginSupervisorOperation(context.Background(), "actor", "operation-2", "prompt", "digest"); err == nil {
		t.Fatal("unscoped admission accepted")
	}
}

func TestSupervisorSchemaUpgradeFrom29(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	owner, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"budget_reservations", "execution_budgets", "supervisor_checkpoint_versions", "supervisor_checkpoints", "supervisor_operations"} {
		if _, err := owner.db.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.db.Exec("DELETE FROM schema_version WHERE version > 29"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("INSERT INTO schema_version(version) SELECT 29 WHERE NOT EXISTS (SELECT 1 FROM schema_version WHERE version=29)"); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, _ := owner.ForTenant(tenant.Local)
	ctx := tenant.Into(context.Background(), tenant.Local)
	if ok, err := store.BeginSupervisorOperation(ctx, "actor", "after-upgrade", "prompt", "digest"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if rows, err := store.SupervisorCheckpointVersions(ctx, "p", "missing"); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
