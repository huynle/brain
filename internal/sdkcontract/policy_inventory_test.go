package sdkcontract

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOperationPolicyInventoryIsCompleteAndUnavailable(t *testing.T) {
	data, err := os.ReadFile("../../api/operation-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Version    int
		Profile    string
		Script     bool   `yaml:"script_exposure"`
		DryRun     string `yaml:"dry_run_validator"`
		Telemetry  string `yaml:"inherited_request_telemetry"`
		Operations map[string][]string
	}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Version != 1 || policy.Profile != "single" || policy.Script || policy.DryRun != "unimplemented" || policy.Telemetry != "unreviewed" {
		t.Fatal("descriptive metadata must not enable scripts or invent dry-run proofs")
	}
	data, err = os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Paths map[string]map[string]yaml.Node
	}
	if err := yaml.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range contract.Paths {
		for method, node := range path {
			if method == "parameters" {
				continue
			}
			var op struct {
				ID     string `yaml:"operationId"`
				Script bool   `yaml:"x-brain-script"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatal(err)
			}
			row, ok := policy.Operations[op.ID]
			if !ok || len(row) != 5 || op.Script {
				t.Errorf("operation %s lacks complete sealed policy row", op.ID)
				continue
			}
			seen[op.ID] = true
			for _, v := range row {
				if strings.TrimSpace(v) == "" {
					t.Errorf("empty policy field for %s", op.ID)
				}
			}
		}
	}
	if len(seen) != 104 || len(policy.Operations) != len(seen) {
		t.Fatalf("contract coverage=%d policy=%d", len(seen), len(policy.Operations))
	}
	matrix, err := os.ReadFile("../../docs/sdk-operation-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]string{"Public": "public", "R": "read", "W": "write", "A": "admin", "Auth": "authenticated_handler_policy"}
	matched := 0
	for _, line := range strings.Split(string(matrix), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 5 {
			continue
		}
		id := strings.TrimSpace(fields[1])
		row, ok := policy.Operations[id]
		if !ok {
			continue
		}
		matched++
		if got, want := row[0], scopes[strings.TrimSpace(fields[3])]; got != want {
			t.Errorf("legacy scope differs from reviewed matrix for %s: %s != %s", id, got, want)
		}
	}
	if matched != 104 {
		t.Fatalf("policy/matrix coverage=%d", matched)
	}
	// Guard read-shaped effects and legacy scopes: scope is not an effect class.
	for _, id := range []string{"attention.read", "attention.dismiss", "attention.resolve"} {
		if policy.Operations[id][0] != "read" || policy.Operations[id][3] != "state" {
			t.Fatalf("read scope must not imply effect-free: %s", id)
		}
	}
	if policy.Operations["attachments.extract"][4] != "extraction_provider" || policy.Operations["search.query"][4] != "embedding_for_semantic_hybrid" {
		t.Fatal("provider-shaped reads/actions lost classification")
	}
	if policy.Operations["entries.move"][2] != "project_only_no_revision" {
		t.Fatal("invented move revision precondition")
	}
}
