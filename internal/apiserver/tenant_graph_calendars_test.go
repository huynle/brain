package apiserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// calendarGraphConfig is a tenant graph configuration carrying calendar sources.
func calendarGraphConfig(t *testing.T, calendars map[string]config.CalendarConfig) config.Config {
	t.Helper()
	return config.Config{
		BrainDir:     t.TempDir(),
		Attachments:  config.AttachmentConfig{StorageRoot: t.TempDir()},
		TaskDefaults: config.TaskDefaultsConfig{},
		Calendars:    calendars,
	}
}

// The graph's brain and automation service share one calendar registry built
// from server.calendars: a builtin name saves, an unknown name is rejected on
// the trigger.calendar field.
func TestTenantGraphWiresCalendarRegistryIntoAutomationSave(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "custom-blobs")); err != nil {
		t.Fatal(err)
	}
	cfg := calendarGraphConfig(t, map[string]config.CalendarConfig{
		"xnys": {Type: "builtin", Market: "XNYS"},
		"team": {Type: "ics", URLEnv: "TEAM_CAL_URL"},
	})
	g, err := newTenantGraph(ctx, views.tenant, views.roots, cfg, graphIdentity{})
	if err != nil {
		t.Fatalf("construct tenant graph: %v", err)
	}
	defer g.Close()

	save := func(calendar string) error {
		_, err := g.brain.Save(ctx, types.CreateEntryRequest{
			Type: "automation", Title: "gate", Content: "gate", Status: "active", Project: "demo",
			Trigger: &types.TriggerConfig{Schedule: "0 9 * * *", Calendar: calendar},
			Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "run"},
		})
		return err
	}
	if err := save("xnys"); err != nil {
		t.Fatalf("save with builtin calendar: %v", err)
	}
	err = save("nope")
	if !errors.Is(err, api.ErrInvalidInput) || !strings.Contains(err.Error(), "trigger.calendar") {
		t.Fatalf("save with unknown calendar: %v, want invalid input naming trigger.calendar", err)
	}
}

// Calendar sources are copied into the graph like the other reference-bearing
// config: changing the caller's source after construction does not reach it.
func TestTenantGraphCopiesCalendarSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "custom-blobs")); err != nil {
		t.Fatal(err)
	}
	cfg := calendarGraphConfig(t, map[string]config.CalendarConfig{
		"xnys": {Type: "builtin", Market: "XNYS", ExtraClosed: []string{"2026-11-27"}},
	})
	g, err := newTenantGraph(ctx, views.tenant, views.roots, cfg, graphIdentity{})
	if err != nil {
		t.Fatalf("construct tenant graph: %v", err)
	}
	defer g.Close()

	cfg.Calendars["xnys"].ExtraClosed[0] = "mutated"
	cfg.Calendars["added"] = config.CalendarConfig{Type: "ics", URLEnv: "A"}
	if got := g.config.Calendars["xnys"].ExtraClosed; len(got) != 1 || got[0] != "2026-11-27" {
		t.Fatalf("graph calendar extras = %v, aliases the caller's config", got)
	}
	if _, ok := g.config.Calendars["added"]; ok {
		t.Fatal("graph calendar map aliases the caller's map")
	}
}

// A source that cannot be built fails graph construction rather than starting
// a server whose calendar gates silently fail.
func TestTenantGraphRejectsUnbuildableCalendar(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	views, err := openSingleModeStorage(ctx, tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer views.close()
	if _, err := views.roots.ProvisionLocal(ctx, root, filepath.Join(root, "custom-blobs")); err != nil {
		t.Fatal(err)
	}
	cfg := calendarGraphConfig(t, map[string]config.CalendarConfig{
		"xnys": {Type: "builtin", Market: "XNYS", ExtraClosed: []string{"11/27/2026"}},
	})
	if g, err := newTenantGraph(ctx, views.tenant, views.roots, cfg, graphIdentity{}); err == nil {
		g.Close()
		t.Fatal("newTenantGraph accepted an unbuildable calendar source")
	}
}
