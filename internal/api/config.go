package api

import (
	"net/http"

	"github.com/huynle/brain-api/internal/config"
)

// taskDefaultsResponse is the JSON response for GET /config/task-defaults.
// It uses explicit pointer fields for booleans so that false/nil are distinguishable
// (false serialises as false, nil as null).
type taskDefaultsResponse struct {
	Agent              string `json:"agent"`
	Model              string `json:"model"`
	ExecutionMode      string `json:"execution_mode"`
	CompleteOnIdle     *bool  `json:"complete_on_idle"`
	MergePolicy        string `json:"merge_policy"`
	MergeStrategy      string `json:"merge_strategy"`
	MergeTargetBranch  string `json:"merge_target_branch"`
	RemoteBranchPolicy string `json:"remote_branch_policy"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge"`
	TargetWorkdir      string `json:"target_workdir"`
}

// TaskDefaultsHandler returns the handler for GET /api/v1/config/task-defaults.
// It returns the current server-side task defaults configuration as JSON.
func TaskDefaultsHandler(td config.TaskDefaultsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := taskDefaultsResponse{
			Agent:              td.Agent,
			Model:              td.Model,
			ExecutionMode:      td.ExecutionMode,
			CompleteOnIdle:     td.CompleteOnIdle,
			MergePolicy:        td.MergePolicy,
			MergeStrategy:      td.MergeStrategy,
			MergeTargetBranch:  td.MergeTargetBranch,
			RemoteBranchPolicy: td.RemoteBranchPolicy,
			OpenPRBeforeMerge:  td.OpenPRBeforeMerge,
			TargetWorkdir:      td.TargetWorkdir,
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}
