package api

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// entryPathParam reads a chi URL param that may carry a percent-encoded entry
// path.
//
// Entry-scoped routes match a SINGLE path segment (/entries/{id}/...), so a
// client addressing an entry by its full path — "projects/x/task/abc.md" — must
// percent-encode the slashes, and the MCP client does exactly that with
// url.PathEscape. chi does not unescape URL params, so handlers received
// "projects%2Fx%2Ftask%2Fabc.md" and nothing ever resolved it. The path form
// advertised by these tools' own schemas has therefore never worked; it failed
// silently, as an empty result or a not-found.
//
// The failure is not confined to the id. Go's URL parsing keeps RawPath
// populated whenever the escaped and unescaped forms differ, and chi routes on
// RawPath, so ONE escaped slash anywhere in the URL makes EVERY param on that
// route arrive escaped. On /entries/{id}/sections/{title} a full-path id left
// the title as "JWT%20Middleware" too, which then matched no heading.
//
// A bare short ID contains nothing to unescape and is returned unchanged, so the
// form that has always worked is untouched. A malformed escape sequence falls
// back to the raw value rather than erroring — that input was already going to
// fail to resolve, and it should fail as "not found", not as a 500.
func entryPathParam(r *http.Request, name string) string {
	raw := chi.URLParam(r, name)
	if unescaped, err := url.PathUnescape(raw); err == nil {
		return unescaped
	}
	return raw
}

// entryWildcardParam reads the /entries/* locator. The public SDKs send a
// legacy entry path as ONE percent-encoded segment ("projects%2Fx%2Fa.md"),
// which keeps r.URL.RawPath set, and chi then routes on, and returns params
// from, the raw path: decode it once. When RawPath is empty the param already
// came from the decoded path (slashes sent literally), so it is returned as is
// and a literal "%41" in a path is never decoded a second time.
func entryWildcardParam(r *http.Request) string {
	raw := chi.URLParam(r, "*")
	if r.URL.RawPath == "" {
		return raw
	}
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}
