package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type BulkJobService interface {
	Create(context.Context, types.BulkJobRequest, string) (*types.BulkJob, error)
	List(context.Context) ([]types.BulkJob, error)
	Get(context.Context, string) (*types.BulkJob, error)
	Items(context.Context, string, int, int) ([]types.BulkJobItem, error)
	ControlJob(context.Context, string, string) (*types.BulkJob, error)
}

func WithBulkJobService(s BulkJobService) HandlerOption { return func(h *Handler) { h.bulkJobs = s } }
func bulkJobReply(w http.ResponseWriter, status int, value any, err error) {
	if err == nil {
		WriteJSON(w, status, value)
		return
	}
	code := http.StatusInternalServerError
	if errors.Is(err, ErrInvalidInput) {
		code = http.StatusBadRequest
	}
	if errors.Is(err, ErrNotFound) {
		code = http.StatusNotFound
	}
	if errors.Is(err, ErrConflict) {
		code = http.StatusConflict
	}
	WriteError(w, code, http.StatusText(code), err.Error())
}
func decodeBulkJob(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	d.DisallowUnknownFields()
	err := d.Decode(v)
	if err == nil {
		var tail any
		if d.Decode(&tail) != io.EOF {
			err = errors.New("expected exactly one JSON object")
		}
	}
	if err != nil {
		WriteError(w, 400, "Bad Request", err.Error())
		return false
	}
	return true
}
func (h *Handler) HandleCreateBulkJob(w http.ResponseWriter, r *http.Request) {
	var req types.BulkJobRequest
	if !decodeBulkJob(w, r, &req) {
		return
	}
	j, e := h.bulkJobs.Create(r.Context(), req, limiterKey(r))
	bulkJobReply(w, 202, j, e)
}
func (h *Handler) HandleListBulkJobs(w http.ResponseWriter, r *http.Request) {
	j, e := h.bulkJobs.List(r.Context())
	bulkJobReply(w, 200, j, e)
}
func (h *Handler) HandleGetBulkJob(w http.ResponseWriter, r *http.Request) {
	j, e := h.bulkJobs.Get(r.Context(), chi.URLParam(r, "jobID"))
	bulkJobReply(w, 200, j, e)
}
func (h *Handler) HandleBulkJobItems(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	j, e := h.bulkJobs.Items(r.Context(), chi.URLParam(r, "jobID"), offset, limit)
	bulkJobReply(w, 200, j, e)
}
func (h *Handler) HandleControlBulkJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !decodeBulkJob(w, r, &req) {
		return
	}
	j, e := h.bulkJobs.ControlJob(r.Context(), chi.URLParam(r, "jobID"), req.Action)
	bulkJobReply(w, 200, j, e)
}
