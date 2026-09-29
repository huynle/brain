package apiserver

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

// AST source attestations pin the inherited callers and scoped delegates. This
// checks the actual dynamic implementations supplied by production composition,
// without adding accessors to production types just for tests.
func phase6GraphReceivers(g *tenantGraph, view *storage.TenantStore) error {
	for name, holder := range map[string]interface{}{"tasks": g.tasks, "runners": g.runners} {
		field := reflect.ValueOf(holder).Elem().FieldByName("storage")
		if field.Type() != reflect.TypeOf(view) || field.Pointer() != reflect.ValueOf(view).Pointer() {
			return fmt.Errorf("%s does not retain supplied tenant view", name)
		}
	}
	registry := reflect.ValueOf(g.runners).Pointer()
	scheduled := reflect.ValueOf(g.scheduler).Elem().FieldByName("runners").Elem()
	// Registry ListRunners returns the API envelope; scheduler consumes a slice
	// through the established response adapter (not directly through the registry).
	if scheduled.Type().PkgPath() != "github.com/huynle/brain-api/internal/service" || scheduled.Type().Name() != "runnerListResponseAdapter" {
		return fmt.Errorf("unexpected scheduler registry adapter: %s", scheduled.Type())
	}
	scheduled = scheduled.FieldByName("source").Elem()
	if scheduled.Type() != reflect.TypeOf(g.runners) || scheduled.Pointer() != registry {
		return fmt.Errorf("scheduler does not use graph tenant registry")
	}
	injected := reflect.ValueOf(g.tasks).Elem().FieldByName("liveInjector").Elem()
	if injected.Type() != reflect.TypeOf((*bridgeLiveInjector)(nil)) {
		return fmt.Errorf("unexpected live injector implementation")
	}
	instances := injected.Elem().FieldByName("instances").Elem()
	if instances.Type() != reflect.TypeOf(g.runners) || instances.Pointer() != registry {
		return fmt.Errorf("injector does not use graph tenant registry")
	}
	return nil
}

func TestPhase6InheritedReceiverComposition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err = views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "attachments")); err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(ctx, views.tenant, views.roots, config.Config{}, graphIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err := phase6GraphReceivers(g, views.tenant); err != nil {
		t.Fatal(err)
	}
	// Negative control: a different registry retaining even the SAME view is not
	// the graph's registry. The scheduler/injector must not silently drift apart.
	original := g.runners
	g.runners = service.NewRunnerRegistryService(views.tenant)
	if err := phase6GraphReceivers(g, views.tenant); err == nil {
		t.Fatal("detached registry accepted")
	}
	g.runners = original
	if err := phase6GraphReceivers(g, &storage.TenantStore{}); err == nil {
		t.Fatal("wrong tenant receiver accepted")
	}
}
