package storage

import (
	"context"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
	"path/filepath"
	"testing"
)

func TestSyncDevicePersistenceAndCAS(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "brain.db")
	base, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := base.ForTenant(tenant.Local)
	d := types.SyncDevice{ID: "device", Owner: "browser"}
	if err = s.SaveSyncDevice(ctx, nil, d); err != nil {
		t.Fatal(err)
	}
	changed := d
	changed.Command = &types.SyncCommand{ID: "command", Action: "discard"}
	if err = s.SaveSyncDevice(ctx, &d, changed); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSyncDevice(ctx, &d, d); err == nil {
		t.Fatal("stale report overwrote command")
	}
	_ = base.Close()
	base, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	s, _ = base.ForTenant(tenant.Local)
	all, err := s.SyncDevices(ctx)
	if err != nil || len(all) != 1 || all[0].Command.ID != "command" {
		t.Fatal(all, err)
	}
	other, _ := base.ForTenant(tenant.MustParse("other"))
	if _, err = other.SyncDevices(ctx); err == nil {
		t.Fatal("tenant boundary bypassed")
	}
}
