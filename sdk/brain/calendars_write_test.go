package brain_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

const sdkSecretFeed = "https://calendar.example.test/private/s3cret-token-sdk/basic.ics"

func TestCalendarsCreateRouteAndBody(t *testing.T) {
	var routes []string
	var body map[string]any
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.Method+" "+r.URL.RequestURI())
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"personal","kind":"ics","origin":"runtime","poll":"5m","event_count":2}`))
	}, brain.Config{})

	poll := "10m"
	got, err := c.Calendars().Create(context.Background(), brain.CreateCalendarRequest{Name: "personal", Url: sdkSecretFeed, Poll: &poll})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"POST /api/v1/calendars"}; !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
	if body["name"] != "personal" || body["url"] != sdkSecretFeed || body["poll"] != "10m" {
		t.Fatalf("request body = %v", body)
	}
	if got.Name != "personal" || got.Origin == nil || *got.Origin != brain.OriginRuntime {
		t.Fatalf("decoded status = %+v", got)
	}
}

func TestCalendarsUpdateRouteSendsOnlyTheSetFields(t *testing.T) {
	var route string
	var body map[string]any
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		route = r.Method + " " + r.URL.RequestURI()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"name":"personal","kind":"ics","origin":"runtime","poll":"10m"}`))
	}, brain.Config{})

	poll := "10m"
	if _, err := c.Calendars().Update(context.Background(), "personal", brain.UpdateCalendarRequest{Poll: &poll}); err != nil {
		t.Fatal(err)
	}
	if route != "PUT /api/v1/calendars/personal" {
		t.Fatalf("route = %q", route)
	}
	if _, has := body["url"]; has {
		t.Fatalf("poll-only update sent an address: %v", body)
	}
	if body["poll"] != "10m" {
		t.Fatalf("body = %v", body)
	}
}

func TestCalendarsDeleteForceAndReferences(t *testing.T) {
	var routes []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"name":"personal","removed":true,"references":[{"id":"a1","path":"projects/p/automation/a1.md","title":"Morning","field":"trigger.skip_if_event"}]}`))
	}, brain.Config{})

	plain, err := c.Calendars().Delete(context.Background(), "personal", false)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := c.Calendars().Delete(context.Background(), "personal", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DELETE /api/v1/calendars/personal", "DELETE /api/v1/calendars/personal?force=true"}; !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
	if !forced.Removed || len(forced.References) != 1 || forced.References[0].Id != "a1" || forced.References[0].Field != brain.TriggerSkipIfEvent {
		t.Fatalf("decoded removal = %+v", forced)
	}
	if plain.Name != "personal" {
		t.Fatalf("plain removal name = %q", plain.Name)
	}
}

func TestCalendarsWriteErrorsNeverEchoTheAddress(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"Unprocessable Entity","message":"calendar address rejected: feed host resolves to a non-public address"}`))
	}, brain.Config{})

	_, err := c.Calendars().Create(context.Background(), brain.CreateCalendarRequest{Name: "personal", Url: sdkSecretFeed})
	if err == nil {
		t.Fatal("rejected address returned no error")
	}
	if strings.Contains(err.Error(), "s3cret-token-sdk") {
		t.Fatalf("SDK error echoed the address: %v", err)
	}
}
