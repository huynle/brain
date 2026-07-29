package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// DefaultHeartbeatInterval is the default interval for SSE heartbeat events.
// Must be well below the server's WriteTimeout (30s) to prevent the server
// from killing idle SSE connections before a heartbeat can keep them alive.
var DefaultHeartbeatInterval = 15 * time.Second

// multiStreamMaxProjects caps ?projects= list length so a malicious or
// runaway client can't ask the hub to spawn thousands of subscriptions
// on one connection.
const multiStreamMaxProjects = 200

// projectIDRe matches the same charset used elsewhere for project IDs
// (see validatePathParam). Keeps the ?projects= parser strict.
var projectIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

// HandleSSEStream handles GET /tasks/{projectId}/stream — SSE event stream.
func (h *Handler) HandleSSEStream(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")

	// Check that the ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe to hub for this project
	ch, unsub := h.hub.Subscribe(projectId)
	defer unsub()

	// Also subscribe to global runner lifecycle events so TUI clients
	// receive runner_registered and runner_offline events.
	runnerCh, runnerUnsub := h.hub.Subscribe(realtime.RunnerLifecycleTopic)
	defer runnerUnsub()

	now := types.TimeNowUTC().Format(time.RFC3339)

	// Send connected event
	writeSSEEvent(w, "connected", types.SSEConnectedData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventConnected,
			Transport: "sse",
			Timestamp: now,
			ProjectID: projectId,
		},
	})
	flusher.Flush()

	// Send initial task snapshot
	if h.tasks != nil {
		resp, err := h.tasks.GetTasks(r.Context(), projectId)
		if err == nil {
			writeSSEEvent(w, "tasks_snapshot", types.SSETasksSnapshotData{
				SSEEventData: types.SSEEventData{
					Type:      types.SSEEventTasksSnapshot,
					Transport: "sse",
					Timestamp: types.TimeNowUTC().Format(time.RFC3339),
					ProjectID: projectId,
				},
				Tasks:  resp.Tasks,
				Count:  resp.Count,
				Stats:  resp.Stats,
				Cycles: resp.Cycles,
			})
			flusher.Flush()
		}
	}

	// Start heartbeat ticker
	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	// Event loop
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return

		case msg, ok := <-ch:
			if !ok {
				// Channel closed
				return
			}
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()

		case msg, ok := <-runnerCh:
			if !ok {
				// Runner lifecycle channel closed
				return
			}
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()

		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", types.SSEEventData{
				Type:      types.SSEEventHeartbeat,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				ProjectID: projectId,
			})
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a single SSE event to the writer.
func writeSSEEvent(w http.ResponseWriter, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
}

// HandleMultiSSEStream handles GET /tasks/stream?projects=a,b,c — a
// single multiplexed SSE stream fanning events from N project topics +
// the global runner-lifecycle topic into ONE connection.
//
// Why: Chrome caps HTTP/1.1 at 6 concurrent connections per origin. With
// one stream per project on a dashboard listing 30+ projects, the browser
// stalls all further /api/* fetches (automations, entries, everything)
// forever, queued behind long-lived SSE streams that never release the
// socket. This endpoint collapses that fanout to a single socket.
//
// Special value: ?projects=all subscribes to every project ListProjects
// returns. Comma-separated lists are validated per-id and deduped.
func (h *Handler) HandleMultiSSEStream(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("projects"))
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "?projects=a,b,c required (or ?projects=all)")
		return
	}

	var projects []string
	if raw == "all" {
		if h.tasks == nil {
			WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "task service unavailable")
			return
		}
		list, err := h.tasks.ListProjects(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", "failed to list projects")
			return
		}
		if len(list) > multiStreamMaxProjects {
			list = list[:multiStreamMaxProjects]
		}
		projects = list
	} else {
		seen := make(map[string]struct{})
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !projectIDRe.MatchString(p) {
				WriteError(w, http.StatusBadRequest, "Bad Request", "invalid project id in list")
				return
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			projects = append(projects, p)
			if len(projects) > multiStreamMaxProjects {
				WriteError(w, http.StatusBadRequest, "Bad Request", fmt.Sprintf("too many projects (max %d)", multiStreamMaxProjects))
				return
			}
		}
		if len(projects) == 0 {
			WriteError(w, http.StatusBadRequest, "Bad Request", "no valid projects")
			return
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe to every requested project + the global runner-lifecycle
	// topic. All unsubs are collected and deferred so a mid-loop return
	// (context cancel, closed channel) still tears the subscriptions
	// down cleanly.
	channels := make([]<-chan realtime.SSEMessage, 0, len(projects)+1)
	unsubs := make([]func(), 0, len(projects)+1)
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()
	for _, p := range projects {
		ch, unsub := h.hub.Subscribe(p)
		channels = append(channels, ch)
		unsubs = append(unsubs, unsub)
	}
	runnerCh, runnerUnsub := h.hub.Subscribe(realtime.RunnerLifecycleTopic)
	channels = append(channels, runnerCh)
	unsubs = append(unsubs, runnerUnsub)

	now := types.TimeNowUTC().Format(time.RFC3339)

	// Emit one "connected" event per project so the frontend can flip
	// each project's ProjectLive.connected flag independently.
	for _, p := range projects {
		writeSSEEvent(w, "connected", types.SSEConnectedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventConnected,
				Transport: "sse",
				Timestamp: now,
				ProjectID: p,
			},
		})
	}
	flusher.Flush()

	// Initial task snapshot per project. If any snapshot fetch errors
	// out we skip that project's snapshot but keep the stream alive —
	// clients will still receive live task events over the subscription.
	if h.tasks != nil {
		for _, p := range projects {
			resp, err := h.tasks.GetTasks(r.Context(), p)
			if err != nil {
				continue
			}
			writeSSEEvent(w, "tasks_snapshot", types.SSETasksSnapshotData{
				SSEEventData: types.SSEEventData{
					Type:      types.SSEEventTasksSnapshot,
					Transport: "sse",
					Timestamp: types.TimeNowUTC().Format(time.RFC3339),
					ProjectID: p,
				},
				Tasks:  resp.Tasks,
				Count:  resp.Count,
				Stats:  resp.Stats,
				Cycles: resp.Cycles,
			})
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	// reflect.Select lets us wait on a variable-length set of channels
	// (N project channels + runner-lifecycle + ctx.Done + heartbeat).
	// A closed subscriber channel gets its Chan cleared, which makes
	// that case never fire again — the connection stays healthy for the
	// remaining subscriptions.
	const (
		caseCtxDone   = 0
		caseHeartbeat = 1
		caseFirstSub  = 2
	)
	cases := make([]reflect.SelectCase, 0, len(channels)+caseFirstSub)
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(r.Context().Done())})
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(heartbeat.C)})
	for _, ch := range channels {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)})
	}

	for {
		idx, val, ok := reflect.Select(cases)
		switch idx {
		case caseCtxDone:
			return
		case caseHeartbeat:
			writeSSEEvent(w, "heartbeat", types.SSEEventData{
				Type:      types.SSEEventHeartbeat,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
			})
			flusher.Flush()
		default:
			if !ok {
				// Subscriber channel closed (hub tore it down). Blank the
				// case's Chan so this branch never re-fires; other subs
				// stay live.
				cases[idx].Chan = reflect.Value{}
				continue
			}
			msg := val.Interface().(realtime.SSEMessage)
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()
		}
	}
}
