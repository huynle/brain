package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/huynle/brain-api/internal/types"
)

// HandleDeleteProject handles DELETE /tasks/{projectId} — erase a project.
//
// The confirmation is the project's own name, echoed back as ?confirm=<id>.
// Every other delete on this API takes confirm=true, which is a formality a
// client sets once and forgets; this one cannot be satisfied without naming
// the exact thing being destroyed, so a mis-routed request against the wrong
// project fails instead of wiping it. The PWA's type-to-confirm dialog is the
// same guard at the other end of the wire — but the guard has to exist here,
// because the API is reachable without it.
func (h *Handler) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Missing project id")
		return
	}

	confirm := r.URL.Query().Get("confirm")
	if confirm != projectID {
		WriteError(w, http.StatusBadRequest, "Bad Request", fmt.Sprintf(
			"Missing or mismatched confirmation: pass confirm=%s to delete project %q",
			projectID, projectID))
		return
	}

	force := r.URL.Query().Get("force") == "true"

	// Live-claim guard, matching the entry and bulk delete paths: work an
	// online runner is currently executing must not have its task deleted
	// out from under it. Fails the whole request rather than skipping the
	// running task — a project wipe that silently leaves one task behind is
	// worse than one that refuses and names what to abort.
	if !force {
		if blocked, taskID, runnerID := h.projectHasLiveClaim(r, projectID); blocked {
			WriteError(w, http.StatusConflict, "Conflict", fmt.Sprintf(
				"task %q in project %q is being executed by online runner %q; abort that runner or retry with force=true",
				taskID, projectID, runnerID))
			return
		}
	}

	resp, err := h.brain.DeleteProject(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found",
				fmt.Sprintf("Project not found: %s", projectID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Tell SSE clients the project changed before it disappears from the
	// listing. PublishProjectDirty alone is right here — the snapshot helper
	// used elsewhere would re-read tasks for a project that no longer exists.
	if h.hub != nil {
		h.hub.PublishProjectDirty(projectID)
	}

	evt := types.NewEvent(types.EventEntryDeleted, types.EventSourceAPI)
	evt.ProjectID = projectID
	evt.Metadata = map[string]string{
		"project": projectID,
		"scope":   "project",
		"deleted": fmt.Sprintf("%d", resp.Deleted),
		"failed":  fmt.Sprintf("%d", resp.Failed),
	}
	h.emitEvent(r.Context(), evt)

	WriteJSON(w, http.StatusOK, resp)
}

// projectHasLiveClaim reports whether any task in the project is currently
// being executed by an online runner, naming the first one found.
//
// Only in_progress tasks are checked: a claim is what makes a task
// in_progress, so scanning the rest would be one registry round trip per
// task for an answer that is structurally "no".
//
// Fails open in every uncertain case, exactly as taskHasLiveClaim does — a
// guard that blocks deletion whenever it cannot reach the registry is worse
// than one that occasionally lets a racing delete through.
func (h *Handler) projectHasLiveClaim(r *http.Request, projectID string) (bool, string, string) {
	if h.tasks == nil {
		return false, "", ""
	}
	resp, err := h.tasks.GetTasks(r.Context(), projectID)
	if err != nil || resp == nil {
		return false, "", ""
	}
	for i := range resp.Tasks {
		task := &resp.Tasks[i]
		if task.Status != "in_progress" {
			continue
		}
		claim, cerr := h.tasks.GetLiveClaim(r.Context(), projectID, task.ID)
		if cerr != nil || claim == nil || !claim.Live {
			continue
		}
		return true, task.ID, claim.RunnerID
	}
	return false, "", ""
}
