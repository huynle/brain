package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// Shipped automations are the content installed with the brain. Save-time
// validation must never reject them, or upgrades would fail to load them.
func TestShippedAutomationsValidate(t *testing.T) {
	paths, err := filepath.Glob("../../cmd/brain/assets/automations/*.md")
	if err != nil {
		t.Fatalf("glob shipped automations: %v", err)
	}
	if len(paths) < 3 {
		t.Fatalf("found %d shipped automations, want at least 3; the glob is wrong", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			doc, err := frontmatter.Parse(string(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if doc.Frontmatter.Type != "automation" {
				t.Fatalf("type = %q, want automation", doc.Frontmatter.Type)
			}
			if err := validateAutomationDefinition(context.Background(), &doc.Frontmatter, "", noParents); err != nil {
				t.Fatalf("shipped automation rejected: %v", err)
			}
		})
	}
}

// The built-in feature checkout and delivery automations are created and
// migrated through the real Save and Update paths. Each second registration
// uses a new merge target, which changes the generated prompt and script, so
// the migration Update is validated too.
func TestBuiltInAutomationsPassValidation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for _, target := range []string{"main", "release"} {
		if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
			Enabled:           true,
			MergeTargetBranch: target,
			MergeStrategy:     "squash",
			TargetWorkdir:     "/repo/brain",
		}); err != nil {
			t.Fatalf("EnsureBuiltInFeatureCheckoutAutomation(%s): %v", target, err)
		}
		if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
			Enabled:            true,
			MergeTargetBranch:  target,
			RemoteBranchPolicy: "delete",
		}); err != nil {
			t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation(%s): %v", target, err)
		}
		if err := EnsureBuiltInFeatureDeliveryAutomation(ctx, brain, BuiltInFeatureDeliveryConfig{
			Enabled:            true,
			MergeTargetBranch:  target,
			MergeStrategy:      "squash",
			RemoteBranchPolicy: "delete",
			TargetWorkdir:      "/repo/brain",
		}); err != nil {
			t.Fatalf("EnsureBuiltInFeatureDeliveryAutomation(%s): %v", target, err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		t.Fatalf("List automations: %v", err)
	}
	counts := map[string]int{}
	for _, e := range resp.Entries {
		counts[e.GeneratedBy]++
	}
	for _, generatedBy := range []string{
		BuiltInFeatureCheckoutGeneratedBy,
		BuiltInFeatureCheckoutSimpleGeneratedBy,
		BuiltInFeatureDeliveryGeneratedBy,
	} {
		if counts[generatedBy] != 1 {
			t.Errorf("%s: %d entries, want exactly 1", generatedBy, counts[generatedBy])
		}
	}
}
