package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// Server request log: an in-memory ring buffer of recent HTTP requests handled
// by the Brain API, annotated with who made them (the authenticated actor).
// Surfaced via GET /server/requests/recent so the PWA/TUI Logs tab can show a
// live, global view of traffic in and out of the server — runners, clients,
// and browsers alike.

// RequestRecord is one recorded HTTP request (alias of the shared type so the
// runner client and TUI decode the same shape).
type RequestRecord = types.ServerRequestRecord

// requestRing is a fixed-capacity ring buffer of the most recent requests.
type requestRing struct {
	mu  sync.Mutex
	buf []RequestRecord
	n   int   // number of valid entries (<= cap)
	at  int   // next write index
	seq int64 // monotonic sequence
}

func newRequestRing(capacity int) *requestRing {
	return &requestRing{buf: make([]RequestRecord, capacity)}
}

func (r *requestRing) add(rec RequestRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	rec.Seq = r.seq
	r.buf[r.at] = rec
	r.at = (r.at + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// recent returns up to `limit` of the newest records, oldest-first. When
// sinceSeq > 0, only records with a greater sequence are returned (for polling
// without duplicates).
func (r *requestRing) recent(limit int, sinceSeq int64) []RequestRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RequestRecord, 0, r.n)
	start := (r.at - r.n + len(r.buf)) % len(r.buf)
	for i := 0; i < r.n; i++ {
		rec := r.buf[(start+i)%len(r.buf)]
		if rec.Seq > sinceSeq {
			out = append(out, rec)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// globalRequestRing holds the recent-request history. 2000 entries is plenty
// for a live tail and bounded in memory.
var globalRequestRing = newRequestRing(2000)

// RequestRecorder records each request (with its authenticated actor) into the
// ring buffer. It must be installed AFTER the Auth middleware so the actor is
// present in the request context. Requests to the request-log endpoints
// themselves are skipped to avoid self-generated noise.
func RequestRecorder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/server/requests") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		actorType, actorName := "anonymous", ""
		if auth, ok := AuthResultFromContext(r.Context()); ok {
			actorType = auth.Type
			actorName = auth.Name
			if actorName == "" {
				actorName = auth.ClientID
			}
		}

		globalRequestRing.add(RequestRecord{
			Time:       start.UnixMilli(),
			Method:     r.Method,
			Path:       r.URL.Path,
			Status:     sw.status,
			DurationMs: time.Since(start).Milliseconds(),
			ActorType:  actorType,
			ActorName:  actorName,
			RequestID:  r.Header.Get("X-Request-ID"),
		})
	})
}

// reqLogQueryInt parses an integer query param with a default.
func reqLogQueryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// HandleRecentRequests handles GET /server/requests/recent?limit=N&since=SEQ —
// the recent server-request log, oldest-first.
func (h *Handler) HandleRecentRequests(w http.ResponseWriter, r *http.Request) {
	limit := reqLogQueryInt(r, "limit", 500)
	if limit > 2000 {
		limit = 2000
	}
	since := int64(reqLogQueryInt(r, "since", 0))
	records := globalRequestRing.recent(limit, since)
	WriteJSON(w, http.StatusOK, map[string]any{
		"requests": records,
		"total":    len(records),
	})
}
