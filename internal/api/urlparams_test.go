package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestEntryPathParam_UnescapesEveryParamOnTheRoute documents the interaction
// that made this bug wider than it looked.
//
// Go keeps URL.RawPath populated whenever the escaped and unescaped forms of a
// path differ, and chi routes on RawPath — so a SINGLE percent-encoded slash
// anywhere in the URL leaves EVERY param on that route escaped. On
// /entries/{id}/sections/{title}, addressing the entry by full path also left
// the section title as "JWT%20Middleware", which then matched no heading.
func TestEntryPathParam_UnescapesEveryParamOnTheRoute(t *testing.T) {
	const fullPath = "projects/demo/plan/p8j9ydoc.md"
	const title = "JWT Middleware"

	var gotID, gotTitle string
	var rawID, rawTitle string
	r := chi.NewRouter()
	r.Get("/entries/{id}/sections/{title}", func(w http.ResponseWriter, req *http.Request) {
		rawID, rawTitle = chi.URLParam(req, "id"), chi.URLParam(req, "title")
		gotID, gotTitle = entryPathParam(req, "id"), entryPathParam(req, "title")
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/entries/" + url.PathEscape(fullPath) + "/sections/" + url.PathEscape(title))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Establish the premise rather than assuming it: chi really does hand back
	// escaped values here, for BOTH params.
	if rawID == fullPath {
		t.Fatalf("premise broken: chi unescaped {id} on its own (%q) — this test no longer tests anything", rawID)
	}
	if rawTitle == title {
		t.Fatalf("premise broken: chi unescaped {title} on its own (%q)", rawTitle)
	}

	if gotID != fullPath {
		t.Errorf("id = %q, want %q", gotID, fullPath)
	}
	if gotTitle != title {
		t.Errorf("title = %q, want %q", gotTitle, title)
	}
}

// TestEntryPathParam_LeavesOrdinaryValuesAlone — the short-ID form is what the
// PWA sends and what every example in the shipped skill uses. It must survive
// untouched, including values carrying a literal percent that is not a valid
// escape (PathUnescape errors there, and the raw value is the right answer).
func TestEntryPathParam_LeavesOrdinaryValuesAlone(t *testing.T) {
	for _, id := range []string{"p8j9ydoc", "100% Done", "plain-title"} {
		t.Run(id, func(t *testing.T) {
			var got string
			r := chi.NewRouter()
			r.Get("/entries/{id}/sections", func(w http.ResponseWriter, req *http.Request) {
				got = entryPathParam(req, "id")
			})
			req := httptest.NewRequest("GET", "/entries/"+url.PathEscape(id)+"/sections", nil)
			r.ServeHTTP(httptest.NewRecorder(), req)

			if got != id {
				t.Errorf("entryPathParam = %q, want %q", got, id)
			}
		})
	}
}
