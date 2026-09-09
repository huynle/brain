package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

type githubTestTransport func(*http.Request) (*http.Response, error)

func (f githubTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitHubDeliveryUsesExactHeadAndActualMerge(t *testing.T) {
	head := strings.Repeat("a", 40)
	merge := strings.Repeat("b", 40)
	d := &types.DeliveryVerification{Required: "merged", Repository: "owner/repo", PullRequest: 1, Head: head, Target: "main", RequiredChecks: []string{"test"}}
	client := &http.Client{Transport: githubTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "api.github.com" {
			t.Fatal("unexpected provider mutation or host", r.URL)
		}
		body := ""
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			body = `{"merged":true,"merge_commit_sha":"` + merge + `","head":{"sha":"` + head + `"},"base":{"ref":"main"}}`
		case strings.HasSuffix(r.URL.Path, "/reviews"):
			body = `[{"id":1,"state":"APPROVED","commit_id":"` + head + `","user":{"login":"reviewer"}}]`
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			if !strings.Contains(r.URL.Path, head) {
				t.Fatal("wrong check head")
			}
			body = `{"total_count":1,"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"success"}]}`
		default:
			t.Fatal("unexpected request", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	e, err := verifyGitHubWithClient(context.Background(), d, "admin:test", client, "")
	if err != nil {
		t.Fatal(err)
	}
	d.Evidence = e
	if len(d.Unmet()) != 0 {
		t.Fatal(d.Unmet())
	}
	d.Head = strings.Repeat("c", 40)
	if len(d.Unmet()) == 0 {
		t.Fatal("new head reused old verification")
	}
	d.Head = head
	e.Merged = false
	if len(d.Unmet()) == 0 {
		t.Fatal("open PR satisfies delivery")
	}
}
func TestDeliveryGatesTaskAndFeatureDependenciesWithoutChangingStatus(t *testing.T) {
	gate := &types.DeliveryVerification{Required: "integrated", Repository: "o/r", PullRequest: 1, Head: "h", Target: "main", Evidence: &types.DeliveryEvidence{Head: "h", Target: "main", Merged: true, MergeCommit: "squash", ReviewAccepted: true, Verifier: "admin", Source: "github", VerifiedAt: time.Now()}}
	tasks := []types.BrainEntry{{ID: "a", Status: "completed", DeliveryVerification: gate}, {ID: "b", Status: "pending", DependsOn: []string{"a"}}}
	result := ResolveDependencies(tasks)
	if result.Tasks[0].Status != "completed" || result.Tasks[1].Classification != "waiting" {
		t.Fatal(result.Tasks)
	}
	if ComputeFeatureStatus(result.Tasks[:1]) != "blocked" {
		t.Fatal("feature released before integration")
	}
	gate.Evidence.IntegrationPassed = true
	gate.Evidence.IntegrationArtifact = "squash"
	gate.Evidence.IntegrationVerifier = "operator"
	gate.Evidence.IntegrationReference = "test-report"
	result = ResolveDependencies(tasks)
	if result.Tasks[1].Classification != "ready" || ComputeFeatureStatus(result.Tasks[:1]) != "completed" {
		t.Fatal(result.Tasks)
	}
	gate.Evidence.Checks = map[string]string{"test": "failure"}
	gate.RequiredChecks = []string{"test"}
	if ResolveDependencies(tasks).Tasks[1].Classification != "waiting" {
		t.Fatal("failed check released downstream")
	}
	tasks[0].DeliveryVerification = nil
	if ResolveDependencies(tasks).Tasks[1].Classification != "ready" {
		t.Fatal("legacy behavior changed")
	}
}
func TestDeliveryPolicyCASAndReindexPreservation(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := tenant.Into(context.Background(), tenant.Local)
	insertTaskNote(t, store, "a", "A", "completed", "medium", "p", nil)
	raw, _ := json.Marshal(DeliveryCommand{Action: "configure", Policy: &types.DeliveryVerification{Required: "merged", Repository: "o/r", Head: strings.Repeat("a", 40), Target: "main", PullRequest: 1}})
	first, err := svc.UpdateDeliveryVerification(ctx, "p", "a", raw, "admin:test")
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || len(first.Unmet()) == 0 {
		t.Fatal(first)
	}
	if _, err = svc.UpdateDeliveryVerification(ctx, "p", "a", raw, "admin:test"); err == nil {
		t.Fatal("stale revision accepted")
	}
	// Indexing unrelated file changes cannot erase the opt-in gate.
	if _, err = store.UpdateNote(ctx, "projects/p/task/a.md", map[string]interface{}{"metadata": "{}"}); err != nil {
		t.Fatal(err)
	}
	task, err := svc.GetTask(ctx, "p", "a")
	if err != nil || task.DeliveryVerification == nil || task.DeliveryVerification.Revision != 1 {
		t.Fatal(task, err)
	}
}

func TestDeliveryGateCannotBeBypassedByDirectClaim(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := tenant.Into(context.Background(), tenant.Local)
	insertRunnerForTaskSelectionTest(t, store, "runner-1", nil, nil)
	insertTaskNote(t, store, "upstream", "Upstream", "completed", "medium", "p", map[string]interface{}{"delivery_verification": map[string]interface{}{"required": "merged", "revision": 1}})
	insertTaskNote(t, store, "downstream", "Downstream", "pending", "medium", "p", map[string]interface{}{"depends_on": []string{"upstream"}})
	if _, err := svc.ClaimTask(ctx, "p", "downstream", "runner-1"); err == nil {
		t.Fatal("direct claim bypassed unmet delivery gate")
	}
	claim, err := svc.GetClaimStatus(ctx, "p", "downstream")
	if err != nil {
		t.Fatal(err)
	}
	if claim.Claimed {
		t.Fatal("refused claim acquired ownership")
	}
}
