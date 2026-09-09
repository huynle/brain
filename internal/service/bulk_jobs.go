package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

const MaxBulkJobTargets = 10000

// BulkJobService owns a serial mutation queue. Request contexts are used only
// for admission/read/control; execution uses the server's authorized lifecycle.
// The disk and SQLite cannot commit atomically. Unknown mutation outcomes are
// durable 'uncertain' items, never automatically repeated.
type BulkJobService struct {
	brain       *BrainServiceImpl
	store       *storage.TenantStore
	tasks       api.TaskService
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	ready       <-chan struct{}
	changed     func(string)
	dirty       map[string]bool
	lastPublish time.Time
	events      api.EventService
	completions map[string]bulkCompletion
}

type bulkCompletion struct{ project, feature, task string }

func NewBulkJobService(brain *BrainServiceImpl, store *storage.TenantStore, tasks api.TaskService, changed func(string)) *BulkJobService {
	return &BulkJobService{brain: brain, store: store, tasks: tasks, changed: changed, dirty: map[string]bool{}, completions: map[string]bulkCompletion{}}
}
func (s *BulkJobService) SetEventService(events api.EventService) { s.events = events }
func (s *BulkJobService) publish(ctx context.Context, force bool) {
	if !force && time.Since(s.lastPublish) < time.Second {
		return
	}
	for project := range s.dirty {
		if project != "" && s.changed != nil {
			s.changed(project)
		}
		delete(s.dirty, project)
	}
	for key, completion := range s.completions {
		if s.events != nil {
			s.events.CheckFeatureCompletion(ctx, completion.project, completion.feature, completion.task)
		}
		delete(s.completions, key)
	}
	s.lastPublish = time.Now()
}
func (s *BulkJobService) Start(ctx context.Context, ready <-chan struct{}) {
	s.ready = ready
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		if err := s.store.RecoverBulkJobs(ctx); err != nil {
			slog.Error("bulk recovery failed", "error", err)
			return
		}
		for ctx.Err() == nil {
			worked, err := s.ProcessOne(ctx)
			if err != nil {
				slog.Error("bulk worker failed", "error", err)
			}
			if worked && err == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
}
func (s *BulkJobService) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func requestDigest(req types.BulkJobRequest) string {
	raw, _ := json.Marshal(req)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *BulkJobService) fingerprint(ctx context.Context, path string) (string, error) {
	abs, err := s.brain.filesystemPath(ctx, path, true)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *BulkJobService) Create(ctx context.Context, req types.BulkJobRequest, actor string) (*types.BulkJob, error) {
	if s.ready != nil {
		select {
		case <-s.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(req.Label) > 200 {
		return nil, fmt.Errorf("%w: label exceeds 200 characters", api.ErrInvalidInput)
	}
	if len(req.RequestID) < 8 || len(req.RequestID) > 128 {
		return nil, fmt.Errorf("%w: request_id must be 8..128 characters", api.ErrInvalidInput)
	}
	digest := requestDigest(req)
	old, err := s.store.BulkJobByRequest(ctx, req.RequestID)
	if err != nil {
		return nil, err
	}
	if old != nil {
		if old.RequestHash != digest {
			return nil, fmt.Errorf("%w: request_id already belongs to a different request", api.ErrConflict)
		}
		return old, nil
	}
	switch req.Operation {
	case "archive", "delete":
		if req.Status != "" || req.TargetProject != "" {
			return nil, fmt.Errorf("%w: unexpected status or target_project", api.ErrInvalidInput)
		}
	case "set_status":
		if !validBulkStatus(req.Status) || req.TargetProject != "" {
			return nil, fmt.Errorf("%w: invalid target status", api.ErrInvalidInput)
		}
	case "move":
		if req.Status != "" {
			return nil, fmt.Errorf("%w: unexpected status", api.ErrInvalidInput)
		}
		if err := validateProjectID(req.TargetProject); err != nil {
			return nil, fmt.Errorf("%w: %v", api.ErrInvalidInput, err)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported operation", api.ErrInvalidInput)
	}
	if len(req.Paths)+len(req.Filters) == 0 || len(req.Paths) > MaxBulkJobTargets || len(req.Filters) > 512 {
		return nil, fmt.Errorf("%w: supply 1..10000 paths and/or up to 512 constrained filters", api.ErrInvalidInput)
	}
	targets := map[string]types.BrainEntry{}
	for _, path := range req.Paths {
		entry, err := s.brain.Recall(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("%w: cannot resolve target %q: %v", api.ErrInvalidInput, path, err)
		}
		targets[entry.Path] = *entry
	}
	for _, filter := range req.Filters {
		if !filter.IsEffective() {
			return nil, fmt.Errorf("%w: unconstrained filter", api.ErrInvalidInput)
		}
		list := types.ListEntriesRequest{Limit: MaxBulkJobTargets + 1}
		if filter.Project != nil {
			list.Project = *filter.Project
		}
		if filter.Type != nil {
			list.Type = *filter.Type
		}
		if filter.Status != nil {
			list.Status = *filter.Status
		}
		if filter.FeatureID != nil {
			list.FeatureID = *filter.FeatureID
		}
		if filter.Priority != nil {
			list.Priority = *filter.Priority
		}
		list.Tags = strings.Join(filter.Tags, ",")
		result, err := s.brain.List(ctx, list)
		if err != nil {
			return nil, err
		}
		if len(result.Entries) > MaxBulkJobTargets {
			return nil, fmt.Errorf("%w: filter exceeds 10000 candidates; narrow the selection", api.ErrInvalidInput)
		}
		for _, e := range result.Entries {
			if matchesJobFilter(e, filter) {
				targets[e.Path] = e
			}
		}
		if len(targets) > MaxBulkJobTargets {
			return nil, fmt.Errorf("%w: selection exceeds 10000 entries", api.ErrInvalidInput)
		}
	}
	paths := make([]string, 0, len(targets))
	for path := range targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	items := make([]types.BulkJobItem, 0, len(paths))
	for n, path := range paths {
		entry := targets[path]
		fp, err := s.fingerprint(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("%w: snapshot %q: %v", api.ErrInvalidInput, path, err)
		}
		item := types.BulkJobItem{Sequence: n, Path: path, EntryID: entry.ID, Title: entry.Title, Fingerprint: fp, State: "pending"}
		items = append(items, item)
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	job := &types.BulkJob{ID: hex.EncodeToString(id), RequestID: req.RequestID, RequestHash: digest, Request: req, Operation: req.Operation, State: "queued", SubmittedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err = s.store.InsertBulkJob(ctx, job, items); err != nil {
		return nil, err
	}
	return s.store.GetBulkJob(ctx, job.ID)
}
func validBulkStatus(status string) bool { return types.IsValidEntryStatus(status) }
func matchesJobFilter(e types.BrainEntry, f types.BulkUpdateFilter) bool {
	return (f.GeneratedBy == nil || e.GeneratedBy == *f.GeneratedBy) && (f.GeneratedKey == nil || e.GeneratedKey == *f.GeneratedKey) && (f.Agent == nil || e.Agent == *f.Agent) && (f.Executor == nil || e.Executor == *f.Executor) && (f.ExecutionMode == nil || e.ExecutionMode == *f.ExecutionMode)
}
func (s *BulkJobService) List(ctx context.Context) ([]types.BulkJob, error) {
	return s.store.ListBulkJobs(ctx)
}
func (s *BulkJobService) Get(ctx context.Context, id string) (*types.BulkJob, error) {
	j, e := s.store.GetBulkJob(ctx, id)
	if e == nil && j == nil {
		return nil, api.ErrNotFound
	}
	return j, e
}
func (s *BulkJobService) Items(ctx context.Context, id string, offset, limit int) ([]types.BulkJobItem, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.store.BulkJobItems(ctx, id, offset, limit)
}
func (s *BulkJobService) ControlJob(ctx context.Context, id, action string) (*types.BulkJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch action {
	case "pause":
		s.publish(ctx, true)
		if j.State == "queued" || j.State == "running" {
			err = s.store.TransitionBulkJob(ctx, id, j.State, "paused")
		}
	case "resume":
		if j.State == "paused" {
			err = s.store.TransitionBulkJob(ctx, id, "paused", "queued")
		}
	case "retry":
		if j.State == "needs_attention" {
			err = s.store.RetryBulkJob(ctx, id)
		}
	default:
		return nil, fmt.Errorf("%w: action must be pause, resume, or retry", api.ErrInvalidInput)
	}
	if err != nil {
		return nil, err
	}
	return s.store.GetBulkJob(ctx, id)
}

// ProcessOne commits a claim before touching the file, and commits its outcome
// before taking another item. An interrupted claim is intentionally quarantined.
func (s *BulkJobService) ProcessOne(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.NextRunnableBulkJob(ctx)
	if err != nil || j == nil {
		return false, err
	}
	if j.State == "queued" {
		if err = s.store.TransitionBulkJob(ctx, j.ID, "queued", "running"); err != nil {
			return false, err
		}
	}
	item, err := s.store.ClaimBulkJobItem(ctx, j.ID)
	if err != nil {
		return false, err
	}
	if item == nil {
		s.publish(ctx, true)
		if err = s.store.QuarantineBulkJobItems(ctx, j.ID); err != nil {
			return false, err
		}
		j, err = s.store.GetBulkJob(ctx, j.ID)
		if err != nil {
			return false, err
		}
		state := "completed"
		if j.Failed+j.Uncertain > 0 {
			state = "needs_attention"
		}
		return true, s.store.TransitionBulkJob(ctx, j.ID, "running", state)
	}
	s.apply(ctx, j, item)
	// On shutdown the write may already have happened. Persist its result with
	// a short, uncancelled context when possible; otherwise recovery flags it.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = s.store.FinishBulkJobItem(saveCtx, j.ID, *item); err != nil {
		return false, err
	}
	if s.changed != nil && (item.State == "succeeded" || item.State == "uncertain") {
		s.dirty[extractProjectFromPath(item.Path)] = true
		if item.Destination != "" {
			s.dirty[extractProjectFromPath(item.Destination)] = true
		}
	}
	s.publish(ctx, false)
	return true, nil
}
func (s *BulkJobService) apply(ctx context.Context, j *types.BulkJob, item *types.BulkJobItem) {
	fail := func(err error) { item.State = "failed"; item.Error = err.Error() }
	entry, err := s.brain.Recall(ctx, item.Path)
	if err != nil {
		fail(err)
		return
	}
	fp, err := s.fingerprint(ctx, item.Path)
	if err != nil {
		fail(err)
		return
	}
	if entry.ID != item.EntryID || fp != item.Fingerprint {
		fail(errors.New("entry changed since this job was submitted; review it and submit a new selection"))
		return
	}
	if entry.Type == "task" && !j.Request.Force {
		if s.tasks == nil {
			fail(errors.New("cannot verify live task claims"))
			return
		}
		claim, err := s.tasks.GetLiveClaim(ctx, extractProjectFromPath(item.Path), entry.ID)
		if err != nil {
			fail(fmt.Errorf("cannot verify live task claim: %w", err))
			return
		}
		if claim != nil && claim.Live {
			fail(fmt.Errorf("task is claimed by online runner %s; stop it before retrying", claim.RunnerID))
			return
		}
	}
	status := j.Request.Status
	switch j.Operation {
	case "archive":
		if entry.Type != "task" {
			fail(errors.New("only tasks can be archived by this operation"))
			return
		}
		switch entry.Status {
		case "completed", "validated", "cancelled", "superseded", "archived":
		default:
			fail(errors.New("only settled tasks can be archived"))
			return
		}
		status = "archived"
	case "move":
		if entry.Status == "in_progress" {
			fail(errors.New("cannot move an in-progress task"))
			return
		}
	}
	if (j.Operation == "archive" || j.Operation == "set_status") && entry.Status == status {
		item.State = "skipped"
		return
	}
	// Known admission errors are checked before entering the mutation boundary,
	// so they are safe to retry. Other errors after entry may be partial writes.
	if j.Operation == "archive" || j.Operation == "set_status" {
		req := types.UpdateEntryRequest{Status: &status}
		if !retirementUpdate(req) && entry.Type == "task" {
			if err = validateConfiguredGitRemote(ctx, s.store, entry.GitRemote); err != nil {
				fail(err)
				return
			}
		}
		_, err = s.brain.Update(ctx, item.Path, req)
	} else if j.Operation == "delete" {
		err = s.brain.Delete(ctx, item.Path)
	} else {
		var moved *types.MoveResult
		moved, err = s.brain.Move(ctx, item.Path, j.Request.TargetProject)
		if moved != nil {
			item.Destination = moved.NewPath
		}
	}
	if err != nil {
		item.State = "uncertain"
		item.Error = "Mutation did not return a confirmed success; inspect this entry before taking further action: " + err.Error()
		return
	}
	if s.events != nil && entry.Type == "task" && entry.FeatureID != "" && (j.Operation == "archive" || j.Operation == "set_status") && (status == "completed" || status == "validated") {
		project := extractProjectFromPath(item.Path)
		s.completions[project+"\x00"+entry.FeatureID] = bulkCompletion{project, entry.FeatureID, entry.ID}
	}
	item.State = "succeeded"
	item.Error = ""
}
