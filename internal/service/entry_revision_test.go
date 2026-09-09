package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

func TestEntryRevisionConcurrentWritersAndAtomicAppend(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	saved, err := svc.Save(ctx, types.CreateEntryRequest{Type: "note", Title: "Conditional", Content: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.Recall(ctx, saved.ID)
	if err != nil || first.Revision == "" {
		t.Fatal(first, err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, body := range []string{"writer A", "writer B"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			_, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{ExpectedRevision: first.Revision, Content: &body})
			results <- err
		}(body)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, api.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	for _, text := range []string{"append A", "append B"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			if _, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{Append: &text}); err != nil {
				t.Error(err)
			}
		}(text)
	}
	wg.Wait()
	final, err := svc.Recall(ctx, saved.ID)
	if err != nil || !strings.Contains(final.Content, "append A") || !strings.Contains(final.Content, "append B") {
		t.Fatal(final, err)
	}
	if _, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"expected_revision": first.Revision, "status": "completed"}); !errors.Is(err, api.ErrConflict) {
		t.Fatal("stale metadata accepted", err)
	}
}

func TestConditionalGraphRejectsMissingReferencesAndCycles(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	a, err := svc.Save(ctx, types.CreateEntryRequest{Type: "task", Project: "p", Title: "A", Content: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Save(ctx, types.CreateEntryRequest{Type: "task", Project: "p", Title: "B", Content: "b", DependsOn: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := svc.Recall(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]string{{"missing-task"}, {b.ID}} {
		if _, err := svc.Update(ctx, a.ID, types.UpdateEntryRequest{ExpectedRevision: current.Revision, DependsOn: &refs}); !errors.Is(err, api.ErrInvalidInput) {
			t.Fatal("invalid graph accepted", refs, err)
		}
		after, err := svc.Recall(ctx, a.ID)
		if err != nil || after.Revision != current.Revision {
			t.Fatal("failed edit changed entry", after, err)
		}
	}
	status := "completed"
	if _, err := svc.Update(ctx, a.ID, types.UpdateEntryRequest{ExpectedRevision: current.Revision, Status: &status}); err != nil {
		t.Fatal(err)
	}
	status = "pending"
	if _, err := svc.Update(ctx, a.ID, types.UpdateEntryRequest{ExpectedRevision: current.Revision, Status: &status}); !errors.Is(err, api.ErrConflict) {
		t.Fatal("stale status accepted", err)
	}
}
