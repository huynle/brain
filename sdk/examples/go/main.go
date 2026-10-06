// An external-style application: only the public SDK and the standard library.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	if err := exerciseTaskActions(ctx, c); err != nil {
		return err
	}
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

// The integration fixture registers a runner record, never an executor. All
// scheduler dispatches must be refused/undelivered; no external work can start.
func exerciseTaskActions(ctx context.Context, c *brain.Client) error {
	if os.Getenv("BRAIN_SDK_ACTION_FIXTURE") != "1" {
		return nil
	}
	if err := exerciseRemainingHTTP(ctx, c); err != nil {
		return err
	}
	project, feature, status := "sdk-go-actions", "action-feature", "pending"
	opts := brain.RequestOptions{}
	initial, e := c.Projects().GetPlacement(ctx, project)
	if e != nil || initial == nil || initial.ProjectId != project || initial.Affinity != "soft" {
		return fmt.Errorf("initial placement: %+v %w", initial, e)
	}
	policy, e := c.Projects().SetPlacement(ctx, project, brain.ProjectPlacement{ProjectId: "ignored", Affinity: "soft"}, opts)
	if e != nil || policy.ProjectId != project || policy.Affinity != "soft" {
		return fmt.Errorf("placement write: %w", e)
	}
	policy, e = c.Projects().GetPlacement(ctx, project)
	if e != nil || policy.Affinity != "soft" {
		return fmt.Errorf("placement read: %w", e)
	}
	_, e = c.Projects().SetPlacement(ctx, project, brain.ProjectPlacement{ProjectId: project, Affinity: "invalid"}, opts)
	var invalid *brain.Error
	if !errors.As(e, &invalid) || invalid.Status != 400 || len(invalid.Details) != 1 {
		return fmt.Errorf("placement validation absent: %v", e)
	}
	standalone, e := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Standalone action", Content: "fixture", Project: &project, Status: &status}, opts)
	if e != nil {
		return e
	}
	defer func() { _ = c.Entries().Delete(context.Background(), standalone.Id, false) }()
	assigned, e := c.Tasks().Assign(ctx, project, standalone.Id, brain.TaskAssignmentRequest{RunnerId: "sdk-fixture-runner"}, opts)
	if e != nil || assigned.RunnerId == nil || *assigned.RunnerId != "sdk-fixture-runner" || assigned.Scope != "task" {
		return fmt.Errorf("task assign: %w", e)
	}
	cleared, e := c.Tasks().ClearAssignment(ctx, project, standalone.Id, brain.ClearFeatureAssignmentRequest{Intent: "clear"}, opts)
	if e != nil || cleared.Status != "cleared" {
		return fmt.Errorf("task clear: %w", e)
	}
	created, e := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Feature action", Content: "fixture", Project: &project, FeatureId: &feature, Status: &status}, opts)
	if e != nil {
		return e
	}
	defer func() { _ = c.Entries().Delete(context.Background(), created.Id, false) }()
	fa, e := c.Features().Assign(ctx, project, feature, brain.FeatureAssignmentRequest{RunnerId: "sdk-fixture-runner"}, opts)
	if e != nil || fa.RunnerId == nil || *fa.RunnerId != "sdk-fixture-runner" {
		return fmt.Errorf("feature assign: %w", e)
	}
	fc, e := c.Features().ClearAssignment(ctx, project, feature, brain.ClearFeatureAssignmentRequest{Intent: "clear"}, opts)
	if e != nil || fc.Status != "cleared" {
		return fmt.Errorf("feature clear: %w", e)
	}
	_, e = c.Tasks().Assign(ctx, project, created.Id, brain.TaskAssignmentRequest{RunnerId: "sdk-fixture-runner"}, opts)
	var conflict *brain.Error
	if !errors.As(e, &conflict) || conflict.Status != 409 {
		return fmt.Errorf("feature task assignment guard absent: %v", e)
	}
	logs, e := c.Tasks().Logs(ctx, project, created.Id, nil)
	if e != nil || logs.Total != 0 || logs.Lines == nil || len(*logs.Lines) != 0 {
		return fmt.Errorf("empty log history: %w", e)
	}
	triggered, e := c.Tasks().Trigger(ctx, project, created.Id, opts)
	if e != nil || !triggered.Success {
		return fmt.Errorf("trigger: %w", e)
	}
	resumed, e := c.Tasks().Resume(ctx, project, created.Id, brain.ResumeTaskOptions{}, opts)
	if e != nil || resumed.Resumed || resumed.Reason == nil {
		return fmt.Errorf("pending resume guard: %w", e)
	}
	inProgress := "in_progress"
	if _, e = c.Entries().Update(ctx, created.Id, brain.UpdateEntryRequest{Status: &inProgress}, opts); e != nil {
		return e
	}
	resumed, e = c.Tasks().Resume(ctx, project, created.Id, brain.ResumeTaskOptions{}, opts)
	if e != nil || !resumed.Resumed {
		return fmt.Errorf("abandoned resume: %w", e)
	}
	batch, e := c.Features().Resume(ctx, project, feature, brain.ResumeTaskOptions{}, opts)
	if e != nil || batch.TotalSkipped != 1 || batch.Results == nil {
		return fmt.Errorf("feature resume idempotency: %w", e)
	}
	if _, e = c.Entries().Update(ctx, created.Id, brain.UpdateEntryRequest{Status: &inProgress}, opts); e != nil {
		return e
	}
	contextual, e := c.Tasks().ResumeWithContext(ctx, project, created.Id, brain.ResumeWithContextOptions{InjectedContext: "continue fixture"}, opts)
	if e != nil || !contextual.Resumed || contextual.ResumeMode != "rehydrate" {
		return fmt.Errorf("context resume: %+v %w", contextual, e)
	}
	cb, e := c.Features().ResumeWithContext(ctx, project, feature, brain.ResumeWithContextOptions{InjectedContext: "continue fixture"}, opts)
	if e != nil || cb.TotalSkipped != 1 || cb.Results == nil {
		return fmt.Errorf("feature context no-op: %w", e)
	}
	_, e = c.Tasks().ResumeWithContext(ctx, project, created.Id, brain.ResumeWithContextOptions{}, opts)
	if !errors.As(e, &invalid) || invalid.Status != 400 {
		return fmt.Errorf("context validation absent: %v", e)
	}
	run, e := c.Tasks().Run(ctx, project, created.Id, brain.RunTaskRequest{}, opts)
	if e != nil || run.Dispatched || run.Reason == nil {
		return fmt.Errorf("unexpected task dispatch: %+v %w", run, e)
	}
	include := true
	fr, e := c.Features().Run(ctx, project, feature, brain.RunFeatureRequest{IncludeDependents: &include}, opts)
	if e != nil || fr.Dispatched {
		return fmt.Errorf("unexpected feature dispatch: %+v %w", fr, e)
	}
	pr, e := c.Projects().Run(ctx, project, brain.RunProjectRequest{}, opts)
	if e != nil || pr.TotalTasksDispatched != 0 {
		return fmt.Errorf("unexpected project dispatch: %w", e)
	}
	chains, e := c.Features().Chains(ctx, project)
	if e != nil || chains.Chains == nil {
		return fmt.Errorf("chains: %w", e)
	}
	cancelled, e := c.Features().Cancel(ctx, project, feature, opts)
	if e != nil || !cancelled.Success {
		return fmt.Errorf("cancel chain: %w", e)
	}
	cancelled, e = c.Features().Cancel(ctx, project, feature, opts)
	if e != nil || cancelled.Cancelled {
		return fmt.Errorf("cancel chain no-op: %w", e)
	}
	_, e = c.Tasks().Dispatch(ctx, project, created.Id, brain.DispatchRequest{TargetRunnerId: "not-registered"}, opts)
	var denied *brain.Error
	if !errors.As(e, &denied) || denied.Status != 403 {
		return fmt.Errorf("dispatch registration guard absent: %v", e)
	}
	claim, e := c.Tasks().ClaimStatus(ctx, project, created.Id)
	if e != nil || claim.Claimed {
		return fmt.Errorf("denied dispatch left claim: %w", e)
	}
	completed := "completed"
	if _, e = c.Entries().Update(ctx, created.Id, brain.UpdateEntryRequest{Status: &completed}, opts); e != nil {
		return e
	}
	promptOnly, none := "prompt_only", "none"
	checkout, e := c.Features().Checkout(ctx, project, feature, brain.FeatureCheckoutOptions{MergePolicy: &promptOnly, DeliveryMode: &none}, opts)
	if e != nil || !checkout.Created || checkout.Task == nil {
		return fmt.Errorf("checkout task creation: %w", e)
	}
	defer func() { _ = c.Entries().Delete(context.Background(), checkout.Task.Id, false) }()
	indexed, e := c.Entries().Get(ctx, checkout.Task.Id)
	if e != nil || indexed.Type != "task" {
		return fmt.Errorf("checkout indexing: %w", e)
	}
	return nil
}

func exerciseRemainingHTTP(ctx context.Context, c *brain.Client) error {
	p, status := "sdk-go-http", "pending"
	opts := brain.RequestOptions{}
	task, e := c.Entries().Create(ctx, brain.CreateEntryRequest{Type: "task", Title: "Quasar context", Content: "Quasar unique source", Project: &p, Status: &status}, opts)
	if e != nil {
		return e
	}
	defer func() { _, _ = c.Projects().Delete(context.Background(), p, p, false, opts) }()
	entry, e := c.Entries().Get(ctx, task.Id)
	if e != nil || entry.Revision == nil {
		return fmt.Errorf("revision absent: %w", e)
	}
	title := "Quasar revised"
	empty := []string{}
	off := false
	updated, e := c.Entries().UpdateMetadata(ctx, task.Id, brain.MetadataUpdateRequest{ExpectedRevision: entry.Revision, Title: &title, Tags: &empty, ScheduleEnabled: &off}, opts)
	if e != nil || updated.Title != title {
		return fmt.Errorf("metadata: %w", e)
	}
	_, e = c.Entries().UpdateMetadata(ctx, task.Id, brain.MetadataUpdateRequest{ExpectedRevision: entry.Revision, Title: &title}, opts)
	var conflict *brain.Error
	if !errors.As(e, &conflict) || conflict.Status != 409 {
		return fmt.Errorf("metadata CAS missing: %v", e)
	}
	injected, e := c.Inject(ctx, brain.InjectRequest{Query: "Quasar", Project: &p})
	if e != nil || injected.Total < 1 || injected.Context == "" {
		return fmt.Errorf("inject lost context: %w", e)
	}
	d, e := c.Tasks().Delivery(ctx, p, task.Id)
	if e != nil || d.TaskId != task.Id || d.Delivery != nil || d.Unmet != nil {
		return fmt.Errorf("default delivery: %w", e)
	}
	configured, e := c.Tasks().VerifyDelivery(ctx, p, task.Id, brain.DeliveryCommand{Action: brain.Configure, ExpectedRevision: 0, Policy: &brain.DeliveryVerification{Required: "none"}}, opts)
	if e != nil || configured.Delivery == nil || configured.Delivery.Revision != 1 {
		return fmt.Errorf("delivery configure: %w", e)
	}
	d, e = c.Tasks().Delivery(ctx, p, task.Id)
	if e != nil || d.Delivery == nil || d.Delivery.Revision != 1 {
		return fmt.Errorf("delivery persisted: %w", e)
	}
	_, e = c.Tasks().VerifyDelivery(ctx, p, task.Id, brain.DeliveryCommand{Action: brain.Configure, ExpectedRevision: 0, Policy: &brain.DeliveryVerification{Required: "none"}}, opts)
	if !errors.As(e, &conflict) || conflict.Status != 409 {
		return fmt.Errorf("delivery CAS missing: %v", e)
	}
	recent, e := c.Events().Recent(ctx, url.Values{"project_id": {p}, "type": {"entry.*"}})
	if e != nil || recent.Count < 1 || recent.Events == nil || recent.Coverage.Buffered < recent.Count {
		return fmt.Errorf("recent events: %w", e)
	}
	for _, event := range *recent.Events {
		if event.ProjectId == nil || *event.ProjectId != p {
			return fmt.Errorf("event filter escaped")
		}
	}
	anchor := (*recent.Events)[len(*recent.Events)-1].Id
	streamTitle := "Quasar stream"
	if _, e = c.Entries().Update(ctx, task.Id, brain.UpdateEntryRequest{Title: &streamTitle}, opts); e != nil {
		return e
	}
	stopStream := errors.New("fixture received replay")
	streamCount := 0
	streamCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	e = c.Events().Stream(streamCtx, url.Values{"project_id": {p}}, anchor, func(event brain.Event) error {
		streamCount++
		if event.ProjectId == nil || *event.ProjectId != p || event.Id == anchor {
			return fmt.Errorf("stream scope/replay mismatch")
		}
		return stopStream
	}, opts)
	stop()
	if !errors.Is(e, stopStream) || streamCount != 1 {
		return fmt.Errorf("stream replay: %w", e)
	}
	page, e := c.Events().Wait(ctx, url.Values{"project_id": {p}, "timeout_ms": {"0"}, "limit": {"1"}})
	if e != nil || len(page.Events) != 1 || page.NextCursor == "" {
		return fmt.Errorf("event page: %w", e)
	}
	_, e = c.Events().Wait(ctx, url.Values{"project_id": {"different"}, "timeout_ms": {"0"}, "after": {page.NextCursor}})
	var bad *brain.Error
	if !errors.As(e, &bad) || bad.Status != 400 {
		return fmt.Errorf("cursor scope accepted: %v", e)
	}
	timed, e := c.Events().Wait(ctx, url.Values{"project_id": {"no-such-events"}, "timeout_ms": {"0"}})
	if e != nil || !timed.TimedOut || len(timed.Events) != 0 {
		return fmt.Errorf("event timeout: %w", e)
	}
	health, e := c.Events().ResourceHealth(ctx, url.Values{"project_id": {p}})
	if e != nil || len(health.Samples) != 0 || health.Availability == "" {
		return fmt.Errorf("health absence: %w", e)
	}
	timeline, e := c.Observability().Timeline(ctx, url.Values{"project": {p}, "from": {time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, "to": {time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
	if e != nil || timeline.Items == nil || len(*timeline.Items) == 0 {
		return fmt.Errorf("timeline: %w", e)
	}
	for _, item := range *timeline.Items {
		if item.ProjectId == nil || *item.ProjectId != p {
			return fmt.Errorf("timeline filter escaped")
		}
	}
	_, e = c.Projects().Delete(ctx, p, "true", false, opts)
	if !errors.As(e, &bad) || bad.Status != 400 {
		return fmt.Errorf("project confirmation accepted: %v", e)
	}
	removed, e := c.Projects().Delete(ctx, p, p, false, opts)
	if e != nil || removed.Deleted != 1 || removed.Failed != 0 {
		return fmt.Errorf("project deletion: %+v %w", removed, e)
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
	manifest, err := c.Capabilities(ctx)
	if err != nil {
		return err
	}
	if manifest.ContractVersion != brain.ContractVersion || manifest.Scripts.Available {
		return fmt.Errorf("unexpected discovery contract or script availability")
	}
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
