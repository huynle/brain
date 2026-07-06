// Package webui embeds the compiled Brain PWA (a Vite/React single-page app)
// and serves it from the same binary as the API. The built assets live in
// ./dist and are produced by `just web-build` (which runs the Vite build with
// its output directory pointed here).
//
// The handler returned by [Handler] sits in front of the API router. It serves
// static assets and the SPA shell for browser navigations, while transparently
// delegating API, OAuth, MCP, and well-known routes to the wrapped API handler.
package webui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// distFS holds the compiled SPA. `all:` ensures dotfiles (e.g. the committed
// .gitkeep placeholder) are embedded too, so the package builds even before the
// frontend has been compiled.
//
//go:embed all:dist
var distFS embed.FS

// apiPrefixes are request-path prefixes that must always be handled by the API
// router rather than the SPA, regardless of HTTP method.
var apiPrefixes = []string{
	"/api/",
	"/mcp",
	"/.well-known/",
	"/oauth/",
	"/authorize",
	"/token",
	"/register",
	"/health",
}

// isAPIPath reports whether a request should be routed to the API handler
// instead of the embedded SPA.
func isAPIPath(p string) bool {
	if p == "/mcp" {
		return true
	}
	for _, prefix := range apiPrefixes {
		if strings.HasSuffix(prefix, "/") {
			if strings.HasPrefix(p, prefix) {
				return true
			}
		} else if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// Handler wraps the API handler with the embedded SPA. Behaviour:
//
//   - API/OAuth/MCP/well-known paths → always delegated to api.
//   - Non-GET/HEAD requests to any other path → delegated to api (this preserves
//     the MCP-at-root POST/DELETE handlers).
//   - GET/HEAD for an existing embedded file → served directly with caching.
//   - GET/HEAD for anything else → the SPA shell (index.html) so client-side
//     routing works on deep links and OAuth callbacks.
//
// If the frontend has not been built (no dist/index.html), browser navigations
// receive a friendly placeholder instead of a confusing API error.
func Handler(api http.Handler) http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Should never happen; fall back to API-only.
		return api
	}

	built := fileExists(sub, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always let the API own its routes.
		if isAPIPath(r.URL.Path) {
			api.ServeHTTP(w, r)
			return
		}

		// Only GET/HEAD are SPA navigations; everything else (e.g. MCP POST at
		// root) belongs to the API.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			api.ServeHTTP(w, r)
			return
		}

		if !built {
			servePlaceholder(w, r)
			return
		}

		serveSPA(w, r, sub)
	})
}

// serveSPA serves a static asset if it exists, otherwise the SPA shell.
func serveSPA(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	upath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if upath == "" {
		upath = "index.html"
	}

	if f, err := sub.Open(upath); err == nil {
		defer f.Close() //nolint:errcheck
		if info, statErr := f.Stat(); statErr == nil && !info.IsDir() {
			setCacheHeaders(w, upath)
			rs, ok := f.(io.ReadSeeker)
			if !ok {
				// Embedded files implement io.ReadSeeker, but guard anyway.
				w.Header().Set("Content-Type", contentTypeFor(upath))
				_, _ = io.Copy(w, f)
				return
			}
			http.ServeContent(w, r, upath, info.ModTime(), rs)
			return
		}
	}

	// SPA fallback — serve index.html with no-cache so deploys are picked up.
	serveIndex(w, r, sub)
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		servePlaceholder(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setCacheHeaders(w http.ResponseWriter, upath string) {
	w.Header().Set("Content-Type", contentTypeFor(upath))
	switch {
	case strings.HasPrefix(upath, "assets/"):
		// Vite emits content-hashed filenames under assets/ — safe to cache hard.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case upath == "index.html" ||
		strings.HasSuffix(upath, "manifest.webmanifest") ||
		strings.HasSuffix(upath, "sw.js") ||
		strings.HasSuffix(upath, "registerSW.js"):
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

// contentTypeFor returns a content type for paths the stdlib mime table may
// miss (notably .webmanifest), falling back to extension detection elsewhere.
func contentTypeFor(upath string) string {
	switch {
	case strings.HasSuffix(upath, ".webmanifest"):
		return "application/manifest+json"
	case strings.HasSuffix(upath, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(upath, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(upath, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(upath, ".json"):
		return "application/json"
	case strings.HasSuffix(upath, ".html"):
		return "text/html; charset=utf-8"
	default:
		return ""
	}
}

func fileExists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsBuilt reports whether the SPA assets are embedded in this binary.
func IsBuilt() bool {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return false
	}
	return fileExists(sub, "index.html")
}

var placeholderModTime = time.Now()

const placeholderHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Brain</title>
<style>
  :root { color-scheme: dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font-family: ui-monospace, SFMono-Regular, Menlo, monospace; background:#0b0e14; color:#c7d0e0; }
  .card { max-width: 32rem; padding: 2rem; text-align:center; }
  h1 { font-size: 1.5rem; margin:0 0 .5rem; color:#7aa2f7; }
  code { background:#161b26; padding:.15rem .4rem; border-radius:.3rem; color:#9ece6a; }
  p { line-height:1.6; }
</style>
</head>
<body>
  <div class="card">
    <h1>Brain</h1>
    <p>The web UI has not been built into this binary yet.</p>
    <p>Run <code>just web-build</code> and rebuild the server, or use the API directly at <code>/api/v1</code>.</p>
  </div>
</body>
</html>`

func servePlaceholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", placeholderModTime, strings.NewReader(placeholderHTML))
}
