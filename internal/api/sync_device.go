package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type syncDeviceService interface {
	SyncDevices(context.Context) ([]types.SyncDevice, error)
	SaveSyncDevice(context.Context, *types.SyncDevice, types.SyncDevice) error
	SyncEntryVersion(context.Context, string) (string, string, error)
}

func syncOwner(r *http.Request) string {
	a, _ := AuthResultFromContext(r.Context())
	b, _ := json.Marshal(a)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func syncConnection(d types.SyncDevice) string {
	t, e := time.Parse(time.RFC3339Nano, d.LastSeen)
	if e != nil || time.Since(t) > 35*time.Second {
		return "unknown"
	}
	if !d.Online {
		return "offline"
	}
	return "online"
}
func (h *Handler) syncDevice(w http.ResponseWriter, r *http.Request) (syncDeviceService, *types.SyncDevice, bool) {
	s, ok := h.brain.(syncDeviceService)
	if !ok {
		WriteError(w, 501, "Unavailable", "sync device reporting unavailable")
		return nil, nil, false
	}
	ds, err := s.SyncDevices(r.Context())
	if err != nil {
		WriteError(w, 500, "Read failed", err.Error())
		return nil, nil, false
	}
	for _, d := range ds {
		if d.ID == chi.URLParam(r, "deviceID") {
			return s, &d, true
		}
	}
	return s, nil, true
}
func (h *Handler) HandleSyncDevices(w http.ResponseWriter, r *http.Request) {
	s, ok := h.brain.(syncDeviceService)
	if !ok {
		WriteError(w, 501, "Unavailable", "sync device reporting unavailable")
		return
	}
	ds, err := s.SyncDevices(r.Context())
	if err != nil {
		WriteError(w, 500, "Read failed", err.Error())
		return
	}
	for i := range ds {
		ds[i].Owner = ""
		ds[i].Connection = syncConnection(ds[i])
		for j := range ds[i].Pending {
			ds[i].Pending[j].Raw = ""
		}
		if ds[i].Command != nil {
			c := *ds[i].Command
			c.ExpectedRaw = ""
			c.Raw = ""
			ds[i].Command = &c
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, 200, map[string]any{"devices": ds, "stale_after_seconds": 35, "note": "Last reported browser state. Unknown means disconnected, suspended, closed, or reporting failed; unsent offline edits are invisible."})
}
func (h *Handler) HandleSyncReport(w http.ResponseWriter, r *http.Request) {
	h.entrySyncMu.Lock()
	defer h.entrySyncMu.Unlock()
	s, old, ok := h.syncDevice(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "deviceID")
	if len(id) < 16 || len(id) > 128 {
		WriteError(w, 400, "Invalid device", "16–128 character device id required")
		return
	}
	var report struct {
		types.SyncDevice
		AckID      string `json:"ack_id"`
		AckOutcome string `json:"ack_outcome"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&report) != nil || len(report.Pending) > 500 {
		WriteError(w, 400, "Invalid report", "valid report with at most 500 pending edits required")
		return
	}
	owner := syncOwner(r)
	if old != nil && old.Owner != owner {
		WriteError(w, 403, "Forbidden", "device belongs to another authenticated identity")
		return
	}
	d := report.SyncDevice
	d.ID = id
	d.Owner = owner
	d.LastSeen = time.Now().UTC().Format(time.RFC3339Nano)
	d.Connection = ""
	d.Command = nil
	seen := map[string]bool{}
	for _, p := range d.Pending {
		if p.ID == "" || seen[p.ID] || p.Path == "" {
			WriteError(w, 400, "Invalid pending edits", "unique operation ids and paths required")
			return
		}
		seen[p.ID] = true
	}
	if old != nil {
		d.Command = old.Command
	}
	if d.Command != nil && report.AckID == d.Command.ID && d.Command.Outcome == "" {
		if report.AckOutcome != "applied_locally" && report.AckOutcome != "stale" {
			WriteError(w, 400, "Invalid acknowledgement", "applied_locally or stale required")
			return
		}
		c := *d.Command
		c.Outcome = report.AckOutcome
		d.Command = &c
	}
	if err := s.SaveSyncDevice(r.Context(), old, d); err != nil {
		WriteError(w, 409, "Report changed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, 200, map[string]any{"command": d.Command})
}

type syncDiff struct {
	DeviceID       string            `json:"device_id"`
	Operation      types.SyncPending `json:"operation"`
	ServerRaw      string            `json:"server_raw"`
	ServerRevision string            `json:"server_revision"`
	Snapshot       string            `json:"snapshot"`
	Diff           string            `json:"diff"`
	Connection     string            `json:"connection"`
	LastSeen       string            `json:"last_seen"`
}

func buildSyncDiff(d *types.SyncDevice, p types.SyncPending, raw, revision string) syncDiff {
	data, _ := json.Marshal([]any{d.ID, d.Epoch, p, raw, revision})
	sum := sha256.Sum256(data)
	// Full replacement hunks preserve every line and avoid truncating YAML definitions.
	diff := "--- server\n+++ browser draft\n"
	if raw != p.Raw {
		diff += "@@ full contents @@\n"
		for _, line := range strings.Split(raw, "\n") {
			diff += "-" + line + "\n"
		}
		for _, line := range strings.Split(p.Raw, "\n") {
			diff += "+" + line + "\n"
		}
	}
	return syncDiff{d.ID, p, raw, revision, hex.EncodeToString(sum[:]), diff, syncConnection(*d), d.LastSeen}
}
func (h *Handler) HandleSyncDiff(w http.ResponseWriter, r *http.Request) {
	s, d, ok := h.syncDevice(w, r)
	if !ok {
		return
	}
	if d == nil {
		WriteError(w, 404, "Not found", "device not reported")
		return
	}
	for _, p := range d.Pending {
		if p.ID == chi.URLParam(r, "operationID") {
			raw, rev, err := s.SyncEntryVersion(r.Context(), p.Path)
			if err != nil {
				WriteError(w, 500, "Read failed", err.Error())
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			WriteJSON(w, 200, buildSyncDiff(d, p, raw, rev))
			return
		}
	}
	WriteError(w, 404, "Not found", "operation no longer reported")
}
func (h *Handler) HandleSyncReconcile(w http.ResponseWriter, r *http.Request) {
	h.entrySyncMu.Lock()
	defer h.entrySyncMu.Unlock()
	s, d, ok := h.syncDevice(w, r)
	if !ok {
		return
	}
	if d == nil {
		WriteError(w, 404, "Not found", "device not reported")
		return
	}
	var q struct {
		Snapshot string `json:"snapshot"`
		Action   string `json:"action"`
		Raw      string `json:"raw"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&q) != nil || (q.Action != "discard" && q.Action != "rebase" && q.Action != "merge") {
		WriteError(w, 400, "Invalid resolution", "snapshot and action discard, rebase, or merge required")
		return
	}
	if d.Command != nil && d.Command.Outcome == "" {
		WriteError(w, 409, "Command pending", "wait for the browser to acknowledge its prior command")
		return
	}
	for _, p := range d.Pending {
		if p.ID == chi.URLParam(r, "operationID") {
			raw, rev, err := s.SyncEntryVersion(r.Context(), p.Path)
			if err != nil {
				WriteError(w, 500, "Read failed", err.Error())
				return
			}
			diff := buildSyncDiff(d, p, raw, rev)
			if q.Snapshot != diff.Snapshot {
				WriteError(w, 409, "Stale diff", "fetch a fresh diff before reconciling")
				return
			}
			if p.Error == "" || (q.Action != "discard" && (p.Method != "PATCH" || p.Failure == "uncertain" || rev == "")) {
				WriteError(w, 409, "Not reconcilable", "requires a reported failed edit; uncertain outcomes and missing entries permit discard only after review")
				return
			}
			if q.Action == "merge" && !strings.HasPrefix(q.Raw, "---\n") {
				WriteError(w, 400, "Invalid merge", "full YAML and Markdown required")
				return
			}
			var id [16]byte
			if _, err = rand.Read(id[:]); err != nil {
				WriteError(w, 500, "Command failed", err.Error())
				return
			}
			after := *d
			after.Command = &types.SyncCommand{ID: hex.EncodeToString(id[:]), OperationID: p.ID, Action: q.Action, ExpectedRaw: p.Raw, ServerRevision: rev, Raw: q.Raw}
			if err = s.SaveSyncDevice(r.Context(), d, after); err != nil {
				WriteError(w, 409, "Device changed", err.Error())
				return
			}
			WriteJSON(w, 202, map[string]any{"command_id": after.Command.ID, "status": "queued", "note": "The browser must reconnect and acknowledge. applied_locally means queued locally, not yet committed to the server."})
			return
		}
	}
	WriteError(w, 404, "Not found", "operation no longer reported")
}
