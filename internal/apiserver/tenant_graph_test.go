package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

func TestTenantGraphAuthoritativeBindingAndCopiedConfig(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	mapping, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "custom-blobs"))
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	cfg := config.Config{BrainDir: t.TempDir(), FeatureCheckout: config.FeatureCheckoutConfig{Enabled: true},
		TaskDefaults:         config.TaskDefaultsConfig{Extensions: []string{"original"}, CompleteOnIdle: &enabled, OpenPRBeforeMerge: &enabled},
		Attachments:          config.AttachmentConfig{StorageRoot: t.TempDir(), AllowedMIMETypes: []string{"text/plain"}, BlockedMIMETypes: []string{"application/x-test"}},
		AttachmentExtraction: config.AttachmentExtractionConfig{SupportedMIMETypes: []string{"text/plain"}}}
	g, err := newTenantGraph(ctx, views.tenant, views.roots, cfg, graphIdentity{})
	if err != nil {
		t.Fatalf("construct tenant graph: %v", err)
	}
	defer g.Close()
	cfg.TaskDefaults.Extensions[0] = "mutated"
	enabled = false
	cfg.Attachments.AllowedMIMETypes[0] = "mutated"
	cfg.Attachments.BlockedMIMETypes[0] = "mutated"
	cfg.AttachmentExtraction.SupportedMIMETypes[0] = "mutated"
	if g.config.BrainDir != mapping.BrainAbsolute || g.config.Attachments.StorageRoot != mapping.BlobAbsolute {
		t.Fatal("graph used caller paths instead of authoritative mapping")
	}
	if g.config.TaskDefaults.Extensions[0] != "original" || !*g.config.TaskDefaults.CompleteOnIdle || !*g.config.TaskDefaults.OpenPRBeforeMerge || g.config.Attachments.AllowedMIMETypes[0] != "text/plain" || g.config.Attachments.BlockedMIMETypes[0] != "application/x-test" || g.config.AttachmentExtraction.SupportedMIMETypes[0] != "text/plain" {
		t.Fatal("graph config aliases caller memory")
	}
	entry, err := g.brain.Save(ctx, types.CreateEntryRequest{Type: "note", Title: "Graph binding", Content: "bound content", Project: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(mapping.BrainAbsolute, entry.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(cfg.BrainDir, entry.Path)); !os.IsNotExist(err) {
		t.Fatalf("write escaped bound root: %v", err)
	}
	g.Close()
	if _, err = views.tenant.GetNoteByPath(ctx, entry.Path); err != nil {
		t.Fatalf("graph closed shared DB: %v", err)
	}
}

func TestTenantGraphWorkersAreExplicitAndJoinBeforeOwnerClose(t *testing.T) {
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
	stop := startSingleGraphWorkers(ctx, g)
	defer stop()
	// Synchronize on actual subscription/first-sweep state, not a guessed sleep.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for g.eventHub.SubscriberCount() != 5 || g.scheduler.Status().TotalTicks == 0 {
		select {
		case <-deadline.C:
			t.Fatalf("workers not ready: subscribers=%d status=%+v", g.eventHub.SubscriberCount(), g.scheduler.Status())
		case <-tick.C:
		}
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker stop did not join")
	}
	if n := g.eventHub.SubscriberCount(); n != 0 {
		t.Fatalf("%d worker subscriptions survive stop", n)
	}
	if g.scheduler.Status().Running {
		t.Fatal("scheduler survives stop")
	}
	stop()
	g.Close()
	if _, err = views.tenant.ListRunners(ctx); err != nil {
		t.Fatalf("worker stop/graph close closed owner: %v", err)
	}
}

func TestTenantGraphPrivateRealtimeAndMissingBinding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err = newTenantGraph(ctx, views.tenant, views.roots, config.Config{BrainDir: root}, graphIdentity{}); err == nil {
		t.Fatal("unregistered root accepted")
	}
	if _, err = newTenantGraph(ctx, nil, views.roots, config.Config{}, graphIdentity{}); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err = newTenantGraph(ctx, views.tenant, nil, config.Config{}, graphIdentity{}); err == nil {
		t.Fatal("nil resolver accepted")
	}
	if _, err = views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "attachments")); err != nil {
		t.Fatal(err)
	}
	a, err := newTenantGraph(ctx, views.tenant, views.roots, config.Config{}, graphIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := newTenantGraph(ctx, views.tenant, views.roots, config.Config{}, graphIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.hub == b.hub || a.eventHub == b.eventHub || a.handler == b.handler {
		t.Fatal("graphs share mutable realtime/handler state")
	}
	ach, unsubA := a.eventHub.Subscribe(realtime.EventFilter{})
	defer unsubA()
	bch, unsubB := b.eventHub.Subscribe(realtime.EventFilter{})
	defer unsubB()
	a.eventHub.Publish(types.Event{Type: "graph.test"})
	select {
	case <-ach:
	default:
		t.Fatal("graph event missing")
	}
	select {
	case <-bch:
		t.Fatal("event crossed graph boundary")
	default:
	}
}

func TestTenantGraphConstructionDoesNotInstallScanOrStartWorkers(t *testing.T) {
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
	if err = os.MkdirAll(filepath.Join(root, "projects/demo/note"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "projects/demo/note/12345678.md"), []byte("---\ntitle: Unscanned\ntype: note\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(ctx, views.tenant, views.roots, config.Config{FeatureCheckout: config.FeatureCheckoutConfig{Enabled: true}}, graphIdentity{})
	if err != nil {
		t.Fatalf("construct tenant graph: %v", err)
	}
	defer g.Close()
	list, err := g.brain.List(ctx, types.ListEntriesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 0 {
		t.Fatalf("construction scanned or installed entries: %v", list.Entries)
	}
	if status := g.scheduler.Status(); status.Started || status.Running || status.TotalTicks != 0 {
		t.Fatalf("construction started scheduler: %+v", status)
	}
}
