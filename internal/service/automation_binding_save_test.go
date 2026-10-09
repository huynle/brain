package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// Save-time rules for bindings: the extends tag is written and kept in sync,
// a project may hold one binding per parent, a binding needs a global parent
// and its own project, and bindings are single-tenant only.

func saveGlobalDreamParent(t *testing.T, brain *BrainServiceImpl, title string) string {
	t.Helper()
	global := true
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:   "automation",
		Title:  title,
		Global: &global,
		Status: "active",
		Trigger: &types.TriggerConfig{
			Type:     types.TriggerTypeCron,
			Schedule: "0 3 * * *",
			Filter:   map[string]string{"project": "*"},
		},
		Action: &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "Consolidate."},
	})
	if err != nil {
		t.Fatalf("save global parent: %v", err)
	}
	return resp.ID
}

func bindingRequest(parent, project string) types.CreateEntryRequest {
	return types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Dream for " + project,
		Project: project,
		Status:  "active",
		Extends: parent,
		Trigger: &types.TriggerConfig{Every: "2d", At: "01:00"},
		Action:  &types.AutomationAction{Agent: "explore", PromptAppend: "Weight decisions."},
	}
}

func entryTags(t *testing.T, brain *BrainServiceImpl, id string) []string {
	t.Helper()
	entry, err := brain.Recall(context.Background(), id)
	if err != nil {
		t.Fatalf("recall %s: %v", id, err)
	}
	return entry.Tags
}

func countTags(tags []string, want string) int {
	n := 0
	for _, tag := range tags {
		if tag == want {
			n++
		}
	}
	return n
}

func TestBinding_SaveWritesTheExtendsTag(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	resp, err := brain.Save(context.Background(), bindingRequest(parent, "hindsight"))
	if err != nil {
		t.Fatalf("save binding: %v", err)
	}
	tags := entryTags(t, brain, resp.ID)
	if n := countTags(tags, "extends:"+parent); n != 1 {
		t.Fatalf("extends tag count = %d in %v, want exactly one", n, tags)
	}
	entry, err := brain.Recall(context.Background(), resp.ID)
	if err != nil {
		t.Fatalf("recall binding: %v", err)
	}
	if entry.Extends != parent {
		t.Fatalf("extends = %q, want %q", entry.Extends, parent)
	}
}

func TestBinding_SaveReplacesACallerSuppliedExtendsTag(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	req := bindingRequest(parent, "hindsight")
	req.Tags = []string{"keep", "extends:wrong001"}
	resp, err := brain.Save(context.Background(), req)
	if err != nil {
		t.Fatalf("save binding: %v", err)
	}
	tags := entryTags(t, brain, resp.ID)
	if countTags(tags, "extends:wrong001") != 0 {
		t.Fatalf("a stale caller-supplied extends tag survived: %v", tags)
	}
	if countTags(tags, "extends:"+parent) != 1 || countTags(tags, "keep") != 1 {
		t.Fatalf("tags = %v, want keep and exactly one extends:%s", tags, parent)
	}
}

func TestBinding_UpdateRetargetsAndClearsTheExtendsTag(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	first := saveGlobalDreamParent(t, brain, "Dream A")
	second := saveGlobalDreamParent(t, brain, "Dream B")

	resp, err := brain.Save(context.Background(), bindingRequest(first, "hindsight"))
	if err != nil {
		t.Fatalf("save binding: %v", err)
	}

	retarget := second
	if _, err := brain.Update(context.Background(), resp.ID, types.UpdateEntryRequest{Extends: &retarget}); err != nil {
		t.Fatalf("retarget binding: %v", err)
	}
	tags := entryTags(t, brain, resp.ID)
	if countTags(tags, "extends:"+first) != 0 || countTags(tags, "extends:"+second) != 1 {
		t.Fatalf("after retarget tags = %v, want only extends:%s", tags, second)
	}

	cleared := ""
	if _, err := brain.Update(context.Background(), resp.ID, types.UpdateEntryRequest{Extends: &cleared}); err != nil {
		t.Fatalf("clear extends: %v", err)
	}
	for _, tag := range entryTags(t, brain, resp.ID) {
		if strings.HasPrefix(tag, "extends:") {
			t.Fatalf("clearing extends must drop every extends tag, found %q", tag)
		}
	}
}

func TestBinding_SecondBindingForTheSameProjectIsRejected(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	if _, err := brain.Save(context.Background(), bindingRequest(parent, "hindsight")); err != nil {
		t.Fatalf("first binding: %v", err)
	}
	_, err := brain.Save(context.Background(), bindingRequest(parent, "hindsight"))
	requireFieldError(t, err, "extends")
}

func TestBinding_AnotherProjectMayBindTheSameParent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	if _, err := brain.Save(context.Background(), bindingRequest(parent, "hindsight")); err != nil {
		t.Fatalf("binding for hindsight: %v", err)
	}
	if _, err := brain.Save(context.Background(), bindingRequest(parent, "orion")); err != nil {
		t.Fatalf("binding for orion must be allowed: %v", err)
	}
}

// A project-owned parent is not rejected: existing automations use one as a
// binding fixture. Its bindings are simply never evaluated (see the scheduler
// tests), so saving one succeeds.
func TestBinding_ProjectOwnedParentStillSaves(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	projectOwned, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Project-owned",
		Project: "hindsight",
		Status:  "active",
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 3 * * *"},
		Action:  &types.AutomationAction{Type: types.AutomationActionPrompt, DirectPrompt: "x"},
	})
	if err != nil {
		t.Fatalf("save project-owned automation: %v", err)
	}
	if _, err := brain.Save(context.Background(), bindingRequest(projectOwned.ID, "orion")); err != nil {
		t.Fatalf("binding of a project-owned parent must save: %v", err)
	}
}

// A binding with no project is filed under "default", so it is still a
// project-owned binding and saves.
func TestBinding_UnprojectedBindingIsFiledUnderDefault(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	req := bindingRequest(parent, "")
	resp, err := brain.Save(context.Background(), req)
	if err != nil {
		t.Fatalf("unprojected binding must save under default: %v", err)
	}
	if !strings.HasPrefix(resp.Path, "projects/default/") {
		t.Fatalf("path = %q, want projects/default/", resp.Path)
	}
}

func TestBinding_GlobalEntryCannotBeABinding(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	global := true
	req := bindingRequest(parent, "")
	req.Global = &global
	_, err := brain.Save(context.Background(), req)
	requireFieldError(t, err, "extends")
}

func TestBinding_TenantModeRejectsBindings(t *testing.T) {
	brain, _, _, db := newTestBrainServiceWithDB(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")

	// Bind the same service to a non-local tenant. Bindings are single-mode
	// only, so the save must refuse before any parent lookup.
	owner, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("storage owner: %v", err)
	}
	other, err := owner.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatalf("tenant view: %v", err)
	}
	brain.storage = other

	_, err = brain.Save(context.Background(), bindingRequest(parent, "hindsight"))
	requireFieldError(t, err, "extends")
	if err != nil && !strings.Contains(err.Error(), "single-tenant") {
		t.Fatalf("error %q should name single-tenant mode", err)
	}
}
