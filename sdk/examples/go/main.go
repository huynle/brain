// An external-style application: only the public SDK and the standard library.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/huynle/brain-api/sdk/brain"
)

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
	if _, err := c.Health(ctx); err != nil {
		return err
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
	if _, err := c.Tasks().List(ctx, project); err != nil {
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
