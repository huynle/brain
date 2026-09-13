package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

type entrySyncService interface {
	EntryChanges(context.Context, string, int64, int) (*types.EntryChanges, error)
	ReserveSyncOperation(context.Context, string, string) (*storage.SyncReceipt, error)
	CompleteSyncOperation(context.Context, string, int, string) error
}

// Stable authenticated namespace; never accept an account identity from the client.
func (h *Handler) HandleEntrySyncIdentity(w http.ResponseWriter, r *http.Request) {
	auth, _ := AuthResultFromContext(r.Context())
	b, _ := json.Marshal(auth)
	sum := sha256.Sum256(b)
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, 200, map[string]string{"scope": hex.EncodeToString(sum[:])})
}

func (h *Handler) HandleEntryChanges(w http.ResponseWriter, r *http.Request) {
	s, ok := h.brain.(entrySyncService)
	if !ok {
		WriteError(w, 501, "Unavailable", "entry sync unavailable")
		return
	}
	after := int64(0)
	var err error
	if v := r.URL.Query().Get("cursor"); v != "" {
		after, err = strconv.ParseInt(v, 10, 64)
	}
	if err != nil || after < 0 {
		WriteError(w, 400, "Invalid cursor", "cursor must be a nonnegative integer")
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
	}
	if err != nil || limit < 1 || limit > 500 {
		WriteError(w, 400, "Invalid limit", "limit must be 1–500")
		return
	}
	if after > 0 && r.URL.Query().Get("epoch") == "" {
		WriteError(w, 400, "Missing epoch", "epoch required with cursor")
		return
	}
	p, err := s.EntryChanges(r.Context(), r.URL.Query().Get("epoch"), after, limit)
	if errors.Is(err, storage.ErrSyncReset) {
		WriteError(w, 410, "Reset required", err.Error())
		return
	}
	if err != nil {
		WriteError(w, 500, "Sync failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, 200, p)
}

type entrySyncMutation struct {
	ID       string          `json:"id"`
	Path     string          `json:"path"`
	Method   string          `json:"method"`
	Body     json.RawMessage `json:"body"`
	Raw      *string         `json:"raw"`
	Revision string          `json:"revision"`
}

// Uses the normal entry handlers so sync cannot bypass frontmatter validation,
// task dependency checks, git remote admission, or automation definition rules.
func (h *Handler) HandleEntrySyncMutation(w http.ResponseWriter, r *http.Request) {
	s, ok := h.brain.(entrySyncService)
	if !ok {
		WriteError(w, 501, "Unavailable", "entry sync unavailable")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		WriteError(w, 413, "Too large", err.Error())
		return
	}
	var m entrySyncMutation
	if json.Unmarshal(data, &m) != nil || len(m.ID) < 16 || len(m.ID) > 128 || (m.Method != "PATCH" && m.Method != "POST") {
		WriteError(w, 400, "Invalid mutation", "id and PATCH or POST required")
		return
	}
	if m.Method == "PATCH" && (m.Path == "" || m.Revision == "") {
		WriteError(w, 400, "Missing revision", "updates require a path and base revision")
		return
	}
	if m.Method == "POST" && (m.Raw != nil || m.Path != "") {
		WriteError(w, 400, "Invalid create", "create requires JSON body and no path")
		return
	}
	// Bind receipts to the authenticated identity, not a client-supplied account.
	auth, _ := AuthResultFromContext(r.Context())
	identity, _ := json.Marshal(auth)
	sum := sha256.Sum256(append(identity, data...))
	hash := hex.EncodeToString(sum[:])
	keySum := sha256.Sum256(append(identity, []byte(m.ID)...))
	key := hex.EncodeToString(keySum[:])
	h.entrySyncMu.Lock()
	defer h.entrySyncMu.Unlock()
	receipt, err := s.ReserveSyncOperation(r.Context(), key, hash)
	if err != nil {
		WriteError(w, 409, "Operation conflict", err.Error())
		return
	}
	if receipt != nil {
		if receipt.Status == 0 {
			WriteError(w, 409, "Outcome uncertain", "An interrupted sync may have applied this edit. Compare with the server before retrying.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(receipt.Status)
		_, _ = w.Write([]byte(receipt.Body))
		return
	}
	body := []byte(m.Body)
	ct := "application/json"
	if m.Raw != nil {
		body = []byte(*m.Raw)
		ct = "text/x-brain-full"
	}
	req := r.Clone(r.Context())
	req.Method = m.Method
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Brain-Expected-Revision", m.Revision)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("*", m.Path)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	if m.Method == "POST" {
		h.HandleCreateEntry(rec, req)
	} else {
		h.HandleUpdateEntry(rec, req)
	}
	// Finish even if the caller disconnected after the write committed.
	if err = s.CompleteSyncOperation(context.WithoutCancel(r.Context()), key, rec.Code, rec.Body.String()); err != nil {
		WriteError(w, 500, "Receipt failed", "Edit may have applied; retry with the same operation ID")
		return
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// Header is used by full-file edits; JSON callers can keep expected_revision.
func applySyncRevision(r *http.Request, req *types.UpdateEntryRequest) {
	if revision := strings.TrimSpace(r.Header.Get("X-Brain-Expected-Revision")); revision != "" {
		req.ExpectedRevision = revision
	}
}

// A read-only POST avoids putting a bounded set of long entry paths in the URL.
func (h *Handler) HandleSelectedEntryChanges(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Entries map[string]string `json:"entries"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil || len(req.Entries) > 250 {
		WriteError(w, 400, "Invalid selection", "Provide at most 250 entry paths and revisions")
		return
	}
	for path := range req.Entries {
		if path == "" || len(path) > 4096 || strings.HasPrefix(path, "/") || strings.Contains("/"+path+"/", "/../") || strings.Contains(path, "\\") {
			WriteError(w, 400, "Invalid path", "Expected an entry-relative path")
			return
		}
	}
	s, ok := h.brain.(interface {
		SelectedEntryChanges(context.Context, map[string]string) (*types.EntryChanges, error)
	})
	if !ok {
		WriteError(w, 501, "Unavailable", "selected sync unavailable")
		return
	}
	result, err := s.SelectedEntryChanges(r.Context(), req.Entries)
	if err != nil {
		WriteError(w, 500, "Sync failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, 200, result)
}
