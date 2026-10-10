package brain_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestCalendarsListRoute(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"calendars":[{"name":"team","kind":"ics","event_count":2,"stale":false}]}`))
	}, brain.Config{})

	resp, err := c.Calendars().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GET /api/v1/calendars"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
	if len(resp.Calendars) != 1 || resp.Calendars[0].Name != "team" || resp.Calendars[0].Kind != brain.Ics {
		t.Fatalf("decoded calendars = %+v", resp.Calendars)
	}
	if resp.Calendars[0].EventCount == nil || *resp.Calendars[0].EventCount != 2 {
		t.Fatalf("event_count not decoded: %+v", resp.Calendars[0])
	}
}
