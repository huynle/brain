package api

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// TestMapFrontmatterToUpdateRequest_OriginFields: a full-file PUT
// (Content-Type: text/x-brain-full) rewrites the entry from the markdown it
// carries. Any field this mapper forgets is silently STRIPPED from a task
// whose file happened to contain it.
func TestMapFrontmatterToUpdateRequest_OriginFields(t *testing.T) {
	req := mapFrontmatterToUpdateRequest(frontmatter.Frontmatter{
		Title:           "Pinned task",
		Status:          "pending",
		OriginMachineID: "machine_a1b2",
		OriginClientID:  "mcp-c3d4",
		OriginPath:      "/Users/huy/projects/brain-api",
		MachineAffinity: types.MachineAffinityLocal,
	}, "Body")

	for _, tc := range []struct {
		field string
		got   *string
		want  string
	}{
		{"origin_machine_id", req.OriginMachineID, "machine_a1b2"},
		{"origin_client_id", req.OriginClientID, "mcp-c3d4"},
		{"origin_path", req.OriginPath, "/Users/huy/projects/brain-api"},
		{"machine_affinity", req.MachineAffinity, types.MachineAffinityLocal},
	} {
		if tc.got == nil {
			t.Errorf("%s is nil — a full-file PUT would strip it", tc.field)
			continue
		}
		if *tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.field, *tc.got, tc.want)
		}
	}
}

// TestValidateUpdateEnums_MachineAffinity covers the bulk_update path.
// Validating create and update but not this one lets an invalid value in
// through a bulk edit.
func TestValidateUpdateEnums_MachineAffinity(t *testing.T) {
	bad := "lokal"
	details := validateUpdateEnums("updates", &types.UpdateEntryRequest{MachineAffinity: &bad})
	if len(details) == 0 {
		t.Fatal("invalid machine_affinity accepted by validateUpdateEnums")
	}
	found := false
	for _, d := range details {
		if d.Field == "updates.machine_affinity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("details = %#v, want one for updates.machine_affinity", details)
	}

	for _, ok := range types.MachineAffinities {
		v := ok
		if d := validateUpdateEnums("updates", &types.UpdateEntryRequest{MachineAffinity: &v}); len(d) != 0 {
			t.Errorf("valid machine_affinity %q rejected: %#v", v, d)
		}
	}
}
