package brain_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestIteratorIgnoresPageLocalTotalAndSnapshotsFilters(t *testing.T) {
	var offsets []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("project") != "original" {
			t.Error("caller mutation changed cursor filter")
		}
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		entries := `[]`
		if offset == "0" {
			entries = `[{"id":"a"}]`
		}
		if offset == "1" {
			entries = `[{"id":"b"}]`
		}
		_, _ = fmt.Fprintf(w, `{"entries":%s,"total":1,"limit":1,"offset":%s}`, entries, offset)
	}, brain.Config{})
	project := "original"
	limit := 1
	p := brain.EntriesListParams{Project: &project, Limit: &limit}
	seq := c.Entries().Iterate(context.Background(), &p)
	project = "changed"
	var ids []string
	for e, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.Id)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b"}) || !reflect.DeepEqual(offsets, []string{"0", "1", "2"}) {
		t.Fatalf("ids=%v offsets=%v", ids, offsets)
	}
}

func TestIteratorRefusesTruncationOrWrongOffset(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{`{"entries":[],"truncated":true,"offset":0,"limit":100}`, "pagination_incomplete"},
		{`{"entries":[{"id":"a"}],"offset":20,"limit":100}`, "invalid_pagination"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			c := client(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }, brain.Config{})
			var got error
			for _, err := range c.Entries().Iterate(context.Background(), nil) {
				got = err
				break
			}
			var e *brain.Error
			if !errors.As(got, &e) || e.Code != tc.code {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestIteratorStopsWhenConsumerBreaks(t *testing.T) {
	calls := 0
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"entries":[{"id":"a"}],"offset":0,"limit":100}`))
	}, brain.Config{})
	for _, err := range c.Entries().Iterate(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestIteratorWithholdsBufferedEntriesAfterBindingRetired(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[{"id":"a"},{"id":"b"}],"offset":0,"limit":100}`))
	}, brain.Config{})
	var ids []string
	var last error
	for e, err := range c.Entries().Iterate(context.Background(), nil) {
		if err != nil {
			last = err
			break
		}
		ids = append(ids, e.Id)
		c.Close()
	}
	if !reflect.DeepEqual(ids, []string{"a"}) || !errors.Is(last, context.Canceled) {
		t.Fatalf("old binding yielded ids=%v error=%v", ids, last)
	}
}
