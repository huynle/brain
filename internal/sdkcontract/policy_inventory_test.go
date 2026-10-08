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
	if len(seen) != 146 || len(policy.Operations) != len(seen) {
		t.Fatalf("contract coverage=%d policy=%d", len(seen), len(policy.Operations))
	}
	matrix, err := os.ReadFile("../../docs/sdk-operation-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]string{"Public": "public", "R": "read", "W": "write", "A": "admin", "Auth": "authenticated_handler_policy", "C": "control"}
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
	if matched != 146 {
		t.Fatalf("policy/matrix coverage=%d", matched)
	}
	// Guard read-shaped effects and legacy scopes: scope is not an effect class.
	for _, id := range []string{"attention.read", "attention.dismiss", "attention.resolve"} {
		if policy.Operations[id][0] != "read" || policy.Operations[id][3] != "state" {
			t.Fatalf("read scope must not imply effect-free: %s", id)
		}
	}
	// Runner-host control and dispatch dials are never read-shaped, and remote
	// control keeps its own legacy scope (control:*, not admin-implied grants).
	for id, row := range policy.Operations {
		switch {
		case strings.HasPrefix(id, "control."):
			if row[0] != "control" || !strings.HasPrefix(row[3], "remote_") {
				t.Errorf("%s must be control-scoped with a remote effect: %v", id, row)
			}
		case strings.HasPrefix(id, "dispatch."):
			if row[0] != "admin" || !strings.Contains(row[3], "dispatch_state") {
				t.Errorf("%s must be an admin dispatch-state write: %v", id, row)
			}
		}
	}
	// Step 3: operations that run work elsewhere are admin-scoped with a work
	// effect and name the executor; the other operator writes are admin-scoped
	// and never read-shaped.
	for _, id := range []string{"supervision.submitOperation", "monitors.create"} {
		row := policy.Operations[id]
		if row[0] != "admin" || !strings.Contains(row[3], "work") || !strings.Contains(row[4], "downstream_executor") {
			t.Errorf("%s must be an admin work operation that names its executor: %v", id, row)
		}
	}
	for _, id := range []string{"monitors.deleteByScope", "sync.devices", "sync.diff", "sync.reconcile", "supervision.getOperation", "supervision.updateCheckpoint", "supervision.updateBudget"} {
		if row := policy.Operations[id]; row[0] != "admin" {
			t.Errorf("%s must keep its admin-only legacy scope: %v", id, row)
		}
	}
	for _, id := range []string{"sync.reconcile", "supervision.updateCheckpoint", "supervision.updateBudget", "clientContext.resolve"} {
		if row := policy.Operations[id]; row[3] == "read" {
			t.Errorf("%s writes state and must not be read-shaped: %v", id, row)
		}
	}
	// Full per-row provider pinning lives in policy_provider_test.go.
	if !strings.Contains(policy.Operations["attachments.extract"][4], "extraction_provider") || policy.Operations["search.query"][4] != "embedding_for_semantic_hybrid" {
		t.Fatal("provider-shaped reads/actions lost classification")
	}
	if policy.Operations["entries.move"][2] != "project_only_no_revision" {
		t.Fatal("invented move revision precondition")
	}
}

// hostedMCPOperation reports the operations added for the hosted MCP (steps 2
// and 3). They drive runners and agents, run or gate work, write server-wide
// or registry state, queue browser commands or read runner hosts, so none of
// them may become script-reachable (reads included).
func hostedMCPOperation(id string) bool {
	group, _, _ := strings.Cut(id, ".")
	switch group {
	case "runners", "dispatch", "control", "scheduler", "monitors", "clientContext", "sync", "supervision":
		return true
	}
	switch id {
	case "tasks.dispatchLease", "tasks.placementReasons", "tasks.runnerCandidates", "tasks.proposedRunnerCandidates", "features.runnerCandidates":
		return true
	}
	return false
}

// Runner-host control, dispatch dials, runner/scheduler reads and the step-3
// operator surfaces are SDK operations for the hosted MCP only: no script
// facade mapping may name them, and their contract rows stay script-off.
func TestRunnerControlOperationsStayOutOfScriptFacade(t *testing.T) {
	facade, err := os.ReadFile("../scriptexec/testdata/facade-normalization.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../api/operation-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Operations map[string][]string
	}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	n := 0
	for id := range policy.Operations {
		if !hostedMCPOperation(id) {
			continue
		}
		n++
		if strings.Contains(string(facade), id) {
			t.Errorf("%s appears in the script facade fixture", id)
		}
	}
	// 21 runner/dispatch/control/scheduler (step 2) + 20 step 3.
	if n != 41 {
		t.Fatalf("hosted-MCP operations=%d, want 41", n)
	}
}
