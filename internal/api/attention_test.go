package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// fakeAttention is an in-memory AttentionService double that records the caller
// and recipient so the handler's recipient-scoping can be asserted.
type fakeAttention struct {
	lastCaller    string
	items         map[string]*types.Attention
	lastListFilt  types.AttentionListFilter
}

func newFakeAttention() *fakeAttention {
	return &fakeAttention{items: map[string]*types.Attention{}}
}

func (f *fakeAttention) CreateAttention(_ context.Context, caller string, req types.CreateAttentionRequest) (*types.Attention, error) {
	f.lastCaller = caller
	recipient := req.Recipient
	if recipient == "" {
		recipient = caller
	}
	a := &types.Attention{ID: "attn_1", Recipient: recipient, Kind: req.Kind, Title: req.Title,
		Severity: types.AttentionSeverityInfo, State: types.AttentionStateUnread, Revision: 1}
	f.items[a.ID] = a
	return a, nil
}

func (f *fakeAttention) ListAttention(_ context.Context, filt types.AttentionListFilter) ([]types.Attention, error) {
	f.lastListFilt = filt
	var out []types.Attention
	for _, a := range f.items {
		if a.Recipient == filt.Recipient {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakeAttention) GetAttention(_ context.Context, recipient, id string) (*types.Attention, error) {
	a := f.items[id]
	if a == nil || a.Recipient != recipient {
		return nil, nil
	}
	return a, nil
}

func (f *fakeAttention) AttentionCounts(_ context.Context, recipient string) (types.AttentionCounts, error) {
	c := types.AttentionCounts{}
	for _, a := range f.items {
		if a.Recipient == recipient {
			c.Total++
			if a.State == types.AttentionStateUnread {
				c.Unread++
			}
		}
	}
	return c, nil
}

func (f *fakeAttention) SetAttentionState(_ context.Context, recipient, id, state, _ string) (*types.Attention, error) {
	a := f.items[id]
	if a == nil || a.Recipient != recipient {
		return nil, context.Canceled // any error; handler maps to 400
	}
	a.State = state
	a.Revision++
	return a, nil
}

func (f *fakeAttention) SavePushSubscription(_ context.Context, _ types.PushSubscription) error {
	return nil
}

func (f *fakeAttention) DeletePushSubscription(_ context.Context, _, _ string) error {
	return nil
}

func attnRouter(fa AttentionService) http.Handler {
	h := NewHandler(nil, WithAttentionService(fa))
	r := chi.NewRouter()
	// Inject an authenticated principal named "alice" so recipient scoping is
	// exercised exactly as the real auth middleware would populate it.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), ctxAuthResult, &AuthResult{Name: "alice", Scope: "admin:*"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/attention", h.HandleListAttention)
	r.Get("/attention/counts", h.HandleAttentionCounts)
	r.Get("/attention/{id}", h.HandleGetAttention)
	r.Post("/attention", h.HandleCreateAttention)
	r.Post("/attention/{id}/read", h.HandleReadAttention)
	r.Post("/attention/{id}/resolve", h.HandleResolveAttention)
	return r
}

func TestHandleCreateAttention_DefaultsRecipientToPrincipal(t *testing.T) {
	fa := newFakeAttention()
	r := attnRouter(fa)
	req := httptest.NewRequest("POST", "/attention", strings.NewReader(`{"kind":"task_blocked","title":"Blocked"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if fa.lastCaller != "alice" {
		t.Fatalf("caller = %q, want alice", fa.lastCaller)
	}
	var got types.Attention
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Recipient != "alice" {
		t.Fatalf("recipient = %q, want alice", got.Recipient)
	}
}

func TestHandleListAttention_ScopesToPrincipal(t *testing.T) {
	fa := newFakeAttention()
	fa.items["x"] = &types.Attention{ID: "x", Recipient: "bob", Kind: "k", Title: "bobs", State: "unread"}
	r := attnRouter(fa)
	req := httptest.NewRequest("GET", "/attention", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if fa.lastListFilt.Recipient != "alice" {
		t.Fatalf("list recipient = %q, want alice", fa.lastListFilt.Recipient)
	}
	var resp struct {
		Attention []types.Attention `json:"attention"`
		Count     int               `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 0 {
		t.Fatalf("alice must not see bob's items, got %d", resp.Count)
	}
}

func TestHandleResolveAttention_Transitions(t *testing.T) {
	fa := newFakeAttention()
	fa.items["attn_1"] = &types.Attention{ID: "attn_1", Recipient: "alice", Kind: "k", Title: "t", State: "unread", Revision: 1}
	r := attnRouter(fa)
	req := httptest.NewRequest("POST", "/attention/attn_1/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if fa.items["attn_1"].State != types.AttentionStateResolved {
		t.Fatalf("state = %q, want resolved", fa.items["attn_1"].State)
	}
}

func TestHandleAttention_NotConfiguredReturns501(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest("GET", "/attention", nil)
	w := httptest.NewRecorder()
	h.HandleListAttention(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}
}
