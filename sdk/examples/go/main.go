// An external-style application: only the public SDK and the standard library.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/huynle/brain-api/sdk/brain"
)

func exerciseTaskSelection(ctx context.Context, c *brain.Client, project, id string) error {
	pending := "pending"
	deps := []string{id}
	dependent, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Go selection fixture", Content: "Dependency selection", Project: &project, Status: &pending, DependsOn: &deps}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Entries().Delete(context.Background(), dependent.Id, false) }()
	if _, err := c.Entries().Update(ctx, id, brain.UpdateEntryRequest{Status: &pending}, brain.RequestOptions{}); err != nil {
		return err
	}
	features := brain.TaskFeatureFilter{"", "not-present"}
	executors := "opencode"
	ready, err := c.Tasks().Ready(ctx, project, &brain.TasksReadyParams{FeatureId: &features, Executors: &executors})
	if err != nil {
		return err
	}
	if ready.Tasks == nil || len(*ready.Tasks) != 1 || (*ready.Tasks)[0].Id != id {
		return fmt.Errorf("ready dependency selection mismatch")
	}
	next, err := c.Tasks().Next(ctx, project, &brain.TasksNextParams{FeatureId: &features, Executors: &executors})
	if err != nil || next.Id != id {
		return fmt.Errorf("next dependency selection mismatch: %w", err)
	}
	waiting, err := c.Tasks().Waiting(ctx, project)
	if err != nil || waiting.Tasks == nil || len(*waiting.Tasks) != 1 || (*waiting.Tasks)[0].Id != dependent.Id {
		return fmt.Errorf("waiting dependency selection mismatch: %w", err)
	}
	cancelled := "cancelled"
	if _, err := c.Entries().Update(ctx, id, brain.UpdateEntryRequest{Status: &cancelled}, brain.RequestOptions{}); err != nil {
		return err
	}
	blocked, err := c.Tasks().Blocked(ctx, project)
	if err != nil || blocked.Tasks == nil || len(*blocked.Tasks) != 1 || (*blocked.Tasks)[0].Id != dependent.Id {
		return fmt.Errorf("blocked dependency selection mismatch: %w", err)
	}
	next, err = c.Tasks().Next(ctx, project, nil)
	if err != nil || next != nil {
		return fmt.Errorf("no ready tasks did not return null: %w", err)
	}
	_, err = c.Entries().Update(ctx, id, brain.UpdateEntryRequest{Status: &pending}, brain.RequestOptions{})
	return err
}

func exerciseTaskReads(ctx context.Context, c *brain.Client) error {
	project, feature, pending := "sdk-go-reads", "read-feature", "pending"
	created, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Read fixture", Content: "Private fixture content", Project: &project, FeatureId: &feature, Status: &pending}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Entries().Delete(context.Background(), created.Id, false) }()
	ids := []string{created.Id, "missing1"}
	status, err := c.Tasks().Status(ctx, project, brain.MultiTaskStatusRequest{TaskIds: &ids})
	if err != nil || status.AllCompleted || status.Tasks == nil || len(*status.Tasks) != 1 || status.NotFound == nil || len(*status.NotFound) != 1 || (*status.NotFound)[0] != "missing1" {
		return fmt.Errorf("mixed status mismatch: %w", err)
	}
	_, err = c.Tasks().Status(ctx, project, brain.MultiTaskStatusRequest{})
	var invalid *brain.Error
	if !errors.As(err, &invalid) || invalid.Status != 400 || len(invalid.Details) != 1 || invalid.Details[0].Field != "taskIds" {
		return fmt.Errorf("empty status validation absent")
	}
	metadata, err := c.Tasks().Metadata(ctx, project, created.Id)
	if err != nil || metadata.Path != created.Path || metadata.FeatureId != feature || metadata.Status != "pending" {
		return fmt.Errorf("metadata mismatch: %w", err)
	}
	claim, err := c.Tasks().ClaimStatus(ctx, project, created.Id)
	if err != nil || claim.TaskId != created.Id || claim.Claimed || claim.IsStale {
		return fmt.Errorf("unexpected claim: %w", err)
	}
	for _, call := range []func(context.Context, string) (*brain.FeatureListResponse, error){c.Features().List, c.Features().Ready} {
		out, err := call(ctx, project)
		if err != nil || out.Features == nil || len(*out.Features) != 1 || (*out.Features)[0].FeatureId != feature || !(*out.Features)[0].Ready {
			return fmt.Errorf("feature list mismatch: %w", err)
		}
	}
	group, err := c.Features().Get(ctx, project, feature)
	if err != nil || group.Feature.FeatureId != feature || group.Feature.Tasks == nil || len(*group.Feature.Tasks) != 1 || (*group.Feature.Tasks)[0].Id != created.Id {
		return fmt.Errorf("feature get mismatch: %w", err)
	}
	_, err = c.Features().Get(ctx, project, "missing1")
	var missing *brain.Error
	if !errors.As(err, &missing) || missing.Status != 404 {
		return fmt.Errorf("missing feature was not 404")
	}
	completed := "completed"
	if _, err := c.Entries().Update(ctx, created.Id, brain.UpdateEntryRequest{Status: &completed}, brain.RequestOptions{}); err != nil {
		return err
	}
	ids = []string{created.Id}
	status, err = c.Tasks().Status(ctx, project, brain.MultiTaskStatusRequest{TaskIds: &ids})
	if err != nil || !status.AllCompleted {
		return fmt.Errorf("completed task not reported: %w", err)
	}
	ready, err := c.Features().Ready(ctx, project)
	if err != nil || (ready.Features != nil && len(*ready.Features) != 0) {
		return fmt.Errorf("completed feature still ready: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	c, err := brain.New(brain.Config{BaseURL: os.Getenv("BRAIN_API_URL"), Token: os.Getenv("BRAIN_API_TOKEN")})
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	project := "sdk-example"
	if err := exerciseTaskReads(ctx, c); err != nil {
		return err
	}
	_, invalidErr := c.Entries().Create(ctx, brain.CreateEntryRequest{}, brain.RequestOptions{})
	var validation *brain.Error
	if !errors.As(invalidErr, &validation) || validation.Status != 400 || len(validation.Details) == 0 {
		return fmt.Errorf("missing field validation details")
	}
	if _, err := c.Health(ctx); err != nil {
		return err
	}
	if os.Getenv("BRAIN_SDK_EXTRACTION_FIXTURE") == "1" {
		pixels, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
		if err != nil {
			return err
		}
		image, err := c.Attachments().Upload(ctx, "sdk-go-extraction", brain.UploadRequest{Filename: "pixel.png", ContentType: "image/png", Content: pixels}, brain.RequestOptions{})
		if err != nil {
			return err
		}
		defer func() {
			_, _ = c.Attachments().Delete(context.Background(), "sdk-go-extraction", image.Attachment.Id, brain.RequestOptions{})
		}()
		if _, err := c.Attachments().Extract(ctx, "sdk-go-extraction", image.Attachment.Id, brain.AttachmentExtractionRequest{}, brain.RequestOptions{}); err != nil {
			return err
		}
		text, err := c.Attachments().Text(ctx, "sdk-go-extraction", image.Attachment.Id)
		if err != nil || text != "fixture extracted text" {
			return fmt.Errorf("stored extraction text mismatch: %w", err)
		}
		if repeated, err := c.Attachments().Text(ctx, "sdk-go-extraction", image.Attachment.Id); err != nil || repeated != text {
			return fmt.Errorf("repeated stored text mismatch: %w", err)
		}
	}
	created, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "SDK contract example", Content: "## Details\nCreated through the public SDK", Project: &project}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = c.Entries().Delete(context.Background(), created.Id, false) }()
	entry, err := c.Entries().Get(ctx, created.Id)
	if err != nil {
		return err
	}
	if entry.Revision == nil {
		return fmt.Errorf("missing revision")
	}
	title := "SDK updated example"
	updated, err := c.Entries().Update(ctx, created.Id, brain.UpdateEntryRequest{Title: &title, ExpectedRevision: entry.Revision}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	if updated.Title != title {
		return fmt.Errorf("update not visible")
	}
	task, err := c.Tasks().Get(ctx, project, created.Id)
	if err != nil {
		return err
	}
	if task.Id != created.Id {
		return fmt.Errorf("task identity mismatch")
	}
	projects, err := c.Projects().List(ctx)
	if err != nil {
		return err
	}
	foundProject := false
	if projects.Projects != nil {
		for _, name := range *projects.Projects {
			if name == project {
				foundProject = true
			}
		}
	}
	if !foundProject {
		return fmt.Errorf("missing project catalog entry")
	}
	stats, err := c.Observability().Stats(ctx, project, "", false)
	if err != nil || stats.ProjectEntries < 1 {
		return fmt.Errorf("missing scoped statistics: %w", err)
	}
	orphans, err := c.Graph().Orphans(ctx, project, "task", 100)
	if err != nil {
		return err
	}
	foundOrphan := false
	for _, e := range *orphans {
		if e.Id == created.Id {
			foundOrphan = true
		}
	}
	if !foundOrphan {
		return fmt.Errorf("unlinked task omitted from orphans")
	}
	stale, err := c.Observability().Stale(ctx, project, "task", 30, 100)
	if err != nil {
		return err
	}
	foundStale := false
	for _, e := range *stale {
		if e.Id == created.Id {
			foundStale = true
		}
	}
	if !foundStale {
		return fmt.Errorf("unverified task omitted from stale results")
	}
	if _, err := c.Tasks().List(ctx, project); err != nil {
		return err
	}
	if err := exerciseTaskSelection(ctx, c, project, created.Id); err != nil {
		return err
	}
	if _, err := c.Entries().List(ctx, &brain.EntriesListParams{Project: &project}); err != nil {
		return err
	}
	pageSize := 1
	found := false
	for e, err := range c.Entries().Iterate(ctx, &brain.EntriesListParams{Project: &project, Limit: &pageSize}) {
		if err != nil {
			return err
		}
		if e.Id == created.Id {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("iterator did not find created entry")
	}
	strategy := "fts"
	if _, err := c.Search(ctx, brain.SearchRequest{Query: title, Strategy: &strategy}); err != nil {
		return err
	}
	if _, err := c.Sections().List(ctx, created.Id); err != nil {
		return err
	}
	if _, err := c.Sections().Get(ctx, created.Id, "Details", true); err != nil {
		return err
	}
	if _, err := c.Graph().Backlinks(ctx, created.Id); err != nil {
		return err
	}
	if _, err := c.Graph().Outlinks(ctx, created.Id); err != nil {
		return err
	}
	if _, err := c.Graph().Related(ctx, created.Id, 5); err != nil {
		return err
	}
	file, err := c.Attachments().Upload(ctx, project, brain.UploadRequest{Filename: "example.txt", Content: []byte("sdk attachment"), ContentType: "text/plain"}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	attachmentID := file.Attachment.Id
	if _, err := c.Attachments().Get(ctx, project, attachmentID); err != nil {
		return err
	}
	if _, err := c.Attachments().List(ctx, project); err != nil {
		return err
	}
	if b, err := c.Attachments().Download(ctx, project, attachmentID); err != nil || string(b) != "sdk attachment" {
		return fmt.Errorf("attachment bytes mismatch: %v", err)
	}
	role := "source"
	if _, err := c.Attachments().Attach(ctx, project, created.Id, brain.AttachEntryAttachmentRequest{Attachment: brain.AttachmentReference{Id: attachmentID, Role: &role}}, brain.RequestOptions{}); err != nil {
		return err
	}
	if _, err := c.Attachments().ForEntry(ctx, project, created.Id); err != nil {
		return err
	}
	if _, err := c.Attachments().Detach(ctx, project, created.Id, attachmentID, role, brain.RequestOptions{}); err != nil {
		return err
	}
	if _, err := c.Attachments().Delete(ctx, project, attachmentID, brain.RequestOptions{}); err != nil {
		return err
	}
	goal, err := c.Goals().Create(ctx, brain.CreateGoalRequest{Project: project, Title: "SDK goal example", Config: brain.GoalConfig{TaskId: &created.Id}, Action: brain.AutomationAction{Type: "create_task"}}, brain.RequestOptions{})
	if err != nil {
		return err
	}
	if _, err := c.Goals().Update(ctx, goal.GoalId, brain.UpdateGoalRequest{Title: &title}, brain.RequestOptions{}); err != nil {
		return err
	}
	if _, err := c.Goals().List(ctx, &brain.GoalsListParams{Project: &project}); err != nil {
		return err
	}
	if _, err := c.Goals().Progress(ctx, goal.GoalId); err != nil {
		return err
	}
	if _, err := c.Goals().Audit(ctx, goal.GoalId, 10); err != nil {
		return err
	}
	if _, err := c.Goals().Delete(ctx, goal.GoalId, brain.RequestOptions{}); err != nil {
		return err
	}
	dry := true
	entries := []brain.BulkUpdateEntry{{Path: updated.Path, Updates: brain.UpdateEntryRequest{Title: &title}}}
	if _, err := c.Entries().BulkUpdate(ctx, brain.BulkUpdateRequest{Entries: &entries, DryRun: &dry}, brain.RequestOptions{}); err != nil {
		return err
	}
	paths := []string{updated.Path}
	if _, err := c.Entries().BulkDelete(ctx, brain.BulkDeleteRequest{Paths: &paths, DryRun: &dry}, brain.RequestOptions{}); err != nil {
		return err
	}
	if _, err := c.Entries().Move(ctx, created.Id, brain.MoveEntryRequest{Project: "sdk-example-moved"}, brain.RequestOptions{}); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "client": "go", "id": created.Id, "task_title": task.Title})
}
