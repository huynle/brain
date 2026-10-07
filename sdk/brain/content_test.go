package brain_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestContentOperationRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if strings.HasSuffix(r.URL.Path, "/backlinks") || strings.HasSuffix(r.URL.Path, "/outlinks") || strings.HasSuffix(r.URL.Path, "/related") {
			_, _ = w.Write([]byte(`[{"id":"linked"}]`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	_, e1 := c.Entries().Move(ctx, "a", brain.MoveEntryRequest{Project: "next"}, brain.RequestOptions{})
	_, e2 := c.Entries().BulkUpdate(ctx, brain.BulkUpdateRequest{}, brain.RequestOptions{})
	_, e3 := c.Entries().BulkDelete(ctx, brain.BulkDeleteRequest{}, brain.RequestOptions{})
	_, e4 := c.Sections().List(ctx, "p/a.md")
	_, e5 := c.Sections().Get(ctx, "p/a.md", "Hello world", true)
	b, e6 := c.Graph().Backlinks(ctx, "a")
	_, e7 := c.Graph().Outlinks(ctx, "a")
	_, e8 := c.Graph().Related(ctx, "a", 5)
	for _, err := range []error{e1, e2, e3, e4, e5, e6, e7, e8} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*b) != 1 || (*b)[0].Id != "linked" {
		t.Fatalf("graph decode: %+v", b)
	}
	want := []string{"POST /api/v1/entries/a/move", "POST /api/v1/entries/bulk-update", "POST /api/v1/entries/bulk-delete", "GET /api/v1/entries/p%2Fa.md/sections", "GET /api/v1/entries/p%2Fa.md/sections/Hello%20world?includeSubsections=true", "GET /api/v1/entries/a/backlinks", "GET /api/v1/entries/a/outlinks", "GET /api/v1/entries/a/related?limit=5"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes=%q", got)
	}
}
