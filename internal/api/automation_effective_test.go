package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

// effectiveFakeService is the AutomationRunService the effective route reads.
// It records what it was asked and returns a canned view or error.
type effectiveFakeService struct {
	gotPath    string
	gotProject string
	calls      int
	view       *types.AutomationEffective
	err        error
}

func (f *effectiveFakeService) RunAutomationNow(ctx context.Context, pathOrID, project string) ([]string, error) {
	return nil, errors.New("RunAutomationNow not expected in effective tests")
}

func (f *effectiveFakeService) EffectiveAutomation(ctx context.Context, pathOrID, project string) (*types.AutomationEffective, error) {
	f.calls++
	f.gotPath, f.gotProject = pathOrID, project
	return f.view, f.err
}

func effectiveRouter(svc *effectiveFakeService) http.Handler {
	opts := []HandlerOption{}
	if svc != nil {
		opts = append(opts, WithAutomationRunService(svc))
	}
	return NewRouter(config.Config{}, WithHandler(NewHandler(&mockBrainService{}, opts...)))
}

func effectiveGet(t *testing.T, router http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestAutomationEffective_ReturnsTheViewForTheProject(t *testing.T) {
	svc := &effectiveFakeService{view: &types.AutomationEffective{
		ID:            "parent01",
		Project:       "p1",
		Fields:        map[string]string{"action.agent": types.EffectiveFieldOverridden},
		BindingID:     "bind0001",
		BindingStatus: "active",
		Targeted:      true,
	}}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/parent01/effective?project=p1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotPath != "parent01" || svc.gotProject != "p1" {
		t.Fatalf("service got path=%q project=%q, want parent01 and p1", svc.gotPath, svc.gotProject)
	}
	got := decodeJSON[types.AutomationEffective](t, rec.Result())
	if got.ID != "parent01" || got.Project != "p1" || got.BindingID != "bind0001" || !got.Targeted {
		t.Fatalf("decoded view = %+v", got)
	}
	if got.Fields["action.agent"] != types.EffectiveFieldOverridden {
		t.Fatalf("fields = %v, want action.agent overridden", got.Fields)
	}
	if got.Broken {
		t.Fatal("a healthy view is reported broken")
	}
}

func TestAutomationEffective_BrokenViewIsStillA200(t *testing.T) {
	svc := &effectiveFakeService{view: &types.AutomationEffective{
		ID:           "parent01",
		Project:      "p1",
		Fields:       map[string]string{},
		Broken:       true,
		BrokenReason: types.EffectiveBrokenDuplicateBinding,
	}}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/parent01/effective?project=p1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a broken view is data, not an error)", rec.Code)
	}
	got := decodeJSON[types.AutomationEffective](t, rec.Result())
	if !got.Broken || got.BrokenReason != types.EffectiveBrokenDuplicateBinding {
		t.Fatalf("broken=%v reason=%q, want duplicate_binding", got.Broken, got.BrokenReason)
	}
}

func TestAutomationEffective_MissingOrEmptyProjectIs400(t *testing.T) {
	for _, target := range []string{
		"/api/v1/automations/parent01/effective",
		"/api/v1/automations/parent01/effective?project=",
	} {
		svc := &effectiveFakeService{view: &types.AutomationEffective{}}
		rec := effectiveGet(t, effectiveRouter(svc), target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
		if svc.calls != 0 {
			t.Errorf("%s: the service was called %d times for a request with no project", target, svc.calls)
		}
	}
}

func TestAutomationEffective_UnknownAutomationIs404(t *testing.T) {
	svc := &effectiveFakeService{err: ErrNotFound}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/nope/effective?project=p1")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	// A missing route also answers 404, so the proof is that the service was asked.
	if svc.calls != 1 || svc.gotPath != "nope" {
		t.Fatalf("service calls=%d path=%q, want one call for nope", svc.calls, svc.gotPath)
	}
}

func TestAutomationEffective_InvalidProjectIs400(t *testing.T) {
	svc := &effectiveFakeService{err: ErrInvalidInput}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/parent01/effective?project=a%2Fb")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAutomationEffective_UnexpectedFailureIs500(t *testing.T) {
	svc := &effectiveFakeService{err: errors.New("store unavailable")}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/parent01/effective?project=p1")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (an internal failure must not read as a bad request)", rec.Code)
	}
}

func TestAutomationEffective_WithoutTheServiceIs501(t *testing.T) {
	rec := effectiveGet(t, effectiveRouter(nil), "/api/v1/automations/parent01/effective?project=p1")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestAutomationEffective_EncodedSlashPathReachesTheService(t *testing.T) {
	svc := &effectiveFakeService{view: &types.AutomationEffective{ID: "global/automation/dream.md", Project: "p1"}}

	rec := effectiveGet(t, effectiveRouter(svc), "/api/v1/automations/global%2Fautomation%2Fdream.md/effective?project=p1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotPath != "global/automation/dream.md" {
		t.Fatalf("service path = %q, want the decoded path global/automation/dream.md", svc.gotPath)
	}
}
