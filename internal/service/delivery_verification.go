package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

type DeliveryCommand struct {
	Action            string                      `json:"action"`
	ExpectedRevision  int                         `json:"expected_revision"`
	Policy            *types.DeliveryVerification `json:"policy,omitempty"`
	Artifact          string                      `json:"artifact,omitempty"`
	Passed            bool                        `json:"passed,omitempty"`
	EvidenceReference string                      `json:"evidence_reference,omitempty"`
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var commitPattern = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)

func (s *TaskServiceImpl) UpdateDeliveryVerification(ctx context.Context, project, taskID string, raw json.RawMessage, actor string) (*types.DeliveryVerification, error) {
	var command DeliveryCommand
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&command); err != nil {
		return nil, fmt.Errorf("%w: %v", api.ErrInvalidInput, err)
	}
	task, err := s.GetTask(ctx, project, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, api.ErrNotFound
	}
	current := task.DeliveryVerification
	if current == nil {
		current = &types.DeliveryVerification{}
	}
	if current.Revision != command.ExpectedRevision {
		return nil, fmt.Errorf("%w: delivery revision changed", api.ErrConflict)
	}
	next := *current
	switch command.Action {
	case "configure":
		if command.Policy == nil {
			return nil, fmt.Errorf("%w: policy required", api.ErrInvalidInput)
		}
		next = *command.Policy
		next.Evidence = nil
		if next.Required != "none" && next.Required != "merged" && next.Required != "integrated" {
			return nil, fmt.Errorf("%w: unknown delivery policy", api.ErrInvalidInput)
		}
		if next.Required != "none" && (!repositoryPattern.MatchString(next.Repository) || strings.Contains(next.Repository, "..") || !commitPattern.MatchString(next.Head) || next.PullRequest <= 0 || next.Target == "" || len(next.Target) > 256 || len(next.RequiredChecks) > 100) {
			return nil, fmt.Errorf("%w: exact GitHub repository, PR, head and target required", api.ErrInvalidInput)
		}
		for _, check := range next.RequiredChecks {
			if check == "" || len(check) > 128 {
				return nil, fmt.Errorf("%w: invalid required check", api.ErrInvalidInput)
			}
		}
	case "verify":
		if next.Required == "" || next.Required == "none" {
			return nil, fmt.Errorf("%w: configure a delivery policy first", api.ErrInvalidInput)
		}
		evidence, verifyErr := verifyGitHubDelivery(ctx, &next, actor)
		if verifyErr != nil {
			next.Evidence = nil
			next.VerificationError = "provider verification unavailable; previous evidence invalidated"
		} else {
			next.VerificationError = ""
			next.Evidence = evidence
		}
	case "integration":
		if next.Evidence == nil || !next.Evidence.Merged || command.Artifact != next.Evidence.MergeCommit || !commitPattern.MatchString(command.Artifact) || command.EvidenceReference == "" || len(command.EvidenceReference) > 1024 {
			return nil, fmt.Errorf("%w: integration evidence must name the verified merge commit and evidence reference", api.ErrInvalidInput)
		}
		evidence := *next.Evidence
		evidence.IntegrationPassed = command.Passed
		evidence.IntegrationArtifact = command.Artifact
		evidence.IntegrationVerifier = actor
		evidence.IntegrationReference = command.EvidenceReference
		next.Evidence = &evidence
	default:
		return nil, fmt.Errorf("%w: action must be configure, verify or integration", api.ErrInvalidInput)
	}
	next.Revision = current.Revision + 1
	ok, err := s.storage.CompareDeliveryVerification(ctx, task.Path, current.Revision, &next)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, api.ErrConflict
	}
	return &next, nil
}

// GitHub is the first supported verifier. There is deliberately no arbitrary
// provider URL and no merge/deploy operation. Missing access fails the gate.
func verifyGitHubDelivery(ctx context.Context, d *types.DeliveryVerification, actor string) (*types.DeliveryEvidence, error) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return verifyGitHubWithClient(ctx, d, actor, client, os.Getenv("GITHUB_TOKEN"))
}
func verifyGitHubWithClient(ctx context.Context, d *types.DeliveryVerification, actor string, client *http.Client, token string) (*types.DeliveryEvidence, error) {
	if !repositoryPattern.MatchString(d.Repository) || strings.Contains(d.Repository, "..") {
		return nil, fmt.Errorf("unsupported repository")
	}
	get := func(path string, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+d.Repository+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("GitHub read returned %d", resp.StatusCode)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}
	var pr struct {
		Merged      bool   `json:"merged"`
		MergeCommit string `json:"merge_commit_sha"`
		Head        struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	path := "/pulls/" + strconv.Itoa(d.PullRequest)
	if err := get(path, &pr); err != nil {
		return nil, err
	}
	e := &types.DeliveryEvidence{Head: pr.Head.SHA, Target: pr.Base.Ref, Merged: pr.Merged, MergeCommit: pr.MergeCommit, Checks: map[string]string{}, Verifier: actor, Source: "github", VerifiedAt: time.Now().UTC()}
	type review struct {
		ID       int64  `json:"id"`
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	latest := map[string]review{}
	for page := 1; page <= 10; page++ {
		var reviews []review
		if err := get(path+"/reviews?per_page=100&page="+strconv.Itoa(page), &reviews); err != nil {
			return nil, err
		}
		for _, r := range reviews {
			if r.State == "COMMENTED" {
				continue
			}
			if old := latest[r.User.Login]; r.ID > old.ID {
				latest[r.User.Login] = r
			}
		}
		if len(reviews) < 100 {
			break
		}
		if page == 10 {
			return nil, fmt.Errorf("review history exceeds verification bound")
		}
	}
	approved, rejected := false, false
	for _, r := range latest {
		if r.State == "CHANGES_REQUESTED" {
			rejected = true
		}
		if r.State == "APPROVED" && r.CommitID == pr.Head.SHA {
			approved = true
		}
	}
	e.ReviewAccepted = approved && !rejected
	seen := map[string]int64{}
	for page := 1; page <= 10; page++ {
		var checks struct {
			Total int `json:"total_count"`
			Runs  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if err := get("/commits/"+pr.Head.SHA+"/check-runs?per_page=100&page="+strconv.Itoa(page), &checks); err != nil {
			return nil, err
		}
		for _, c := range checks.Runs {
			if c.ID > seen[c.Name] {
				seen[c.Name] = c.ID
				e.Checks[c.Name] = c.Conclusion
				if c.Status != "completed" {
					e.Checks[c.Name] = "pending"
				}
			}
		}
		if page*100 >= checks.Total {
			break
		}
		if page == 10 {
			return nil, fmt.Errorf("check history exceeds verification bound")
		}
	}
	return e, nil
}
