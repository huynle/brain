package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/huynle/brain-api/internal/attentionstore"
	"github.com/huynle/brain-api/internal/types"
)

// attentionEventIngester is the narrow slice of the event pipeline the
// attention service needs. Declared locally so the service can be constructed
// in tests without a full event hub.
type attentionEventIngester interface {
	Ingest(ctx context.Context, events []types.Event) error
}

// AttentionService owns the durable, per-user attention inbox: creation with
// dedup, revision-safe state transitions, listing, and counts. It emits
// attention.created / attention.updated so delivery providers, webhooks, and
// the PWA can react. The inbox lives in a self-contained SQLite datastore
// (attentionstore), NOT the main brain catalog: that catalog is frozen by a
// reviewed provenance pin and the tenant/v31 successor schema is dormant.
type AttentionService struct {
	store  *attentionstore.Store
	events attentionEventIngester
	now    func() time.Time
	// notifier holds the system notifier once wired. It is set after the
	// background workers start, so readers must tolerate it being unset.
	notifier atomic.Pointer[systemNotifierBox]
}

type systemNotifierBox struct{ n SystemNotifier }

// SetSystemNotifier publishes the system notifier to later consumers. It is
// nil-safe: calling it on a nil service does nothing.
func (s *AttentionService) SetSystemNotifier(n SystemNotifier) {
	if s == nil {
		return
	}
	s.notifier.Store(&systemNotifierBox{n: n})
}

// SystemNotifier returns the published notifier, or nil when none is set yet
// (or the service is nil). Callers must treat nil as "no notifier".
func (s *AttentionService) SystemNotifier() SystemNotifier {
	if s == nil {
		return nil
	}
	box := s.notifier.Load()
	if box == nil {
		return nil
	}
	return box.n
}

// ListAttentionRecipients returns every recipient that owns an attention item.
func (s *AttentionService) ListAttentionRecipients(ctx context.Context) ([]string, error) {
	return s.store.ListRecipients(ctx)
}

// AttentionServiceOption configures an AttentionService.
type AttentionServiceOption func(*AttentionService)

// WithAttentionEventIngester supplies the event sink for attention.* events.
func WithAttentionEventIngester(e attentionEventIngester) AttentionServiceOption {
	return func(s *AttentionService) { s.events = e }
}

// WithAttentionClock overrides the clock, for tests.
func WithAttentionClock(now func() time.Time) AttentionServiceOption {
	return func(s *AttentionService) { s.now = now }
}

// NewAttentionService builds the service over a self-contained attention store.
func NewAttentionService(store *attentionstore.Store, opts ...AttentionServiceOption) *AttentionService {
	s := &AttentionService{store: store, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// CreateAttention creates an item for the requested recipient, defaulting to
// caller when the request omits one. Creation is idempotent per
// (recipient, dedup_key): a duplicate returns the existing item unchanged.
func (s *AttentionService) CreateAttention(ctx context.Context, caller string, req types.CreateAttentionRequest) (*types.Attention, error) {
	recipient := req.Recipient
	if recipient == "" {
		recipient = caller
	}
	if recipient == "" {
		return nil, fmt.Errorf("attention requires a recipient")
	}
	if req.Kind == "" || req.Title == "" {
		return nil, fmt.Errorf("attention requires kind and title")
	}
	severity := req.Severity
	if severity == "" {
		severity = types.AttentionSeverityInfo
	}
	if !types.ValidAttentionSeverity(severity) {
		return nil, fmt.Errorf("invalid severity %q", severity)
	}
	item := &types.Attention{
		ID: newAttentionID(), Recipient: recipient, Kind: req.Kind, Severity: severity,
		Title: req.Title, Body: req.Body, Project: req.Project, TaskID: req.TaskID,
		FeatureID: req.FeatureID, SessionID: req.SessionID, RunnerID: req.RunnerID,
		InstanceID: req.InstanceID, SourceType: req.SourceType, SourceID: req.SourceID,
		DedupKey: req.DedupKey, State: types.AttentionStateUnread, Actions: req.Actions,
	}
	created, err := s.store.CreateAttention(ctx, item, s.now())
	if err != nil {
		return nil, err
	}
	if !created {
		// Dedup hit: return the pre-existing item so callers are idempotent.
		if req.DedupKey != "" {
			if existing, err := s.findByDedup(ctx, recipient, req.DedupKey); err == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("attention not created")
	}
	s.emit(ctx, types.EventAttentionCreated, item)
	return item, nil
}

// SetAttentionState transitions an item to a new state, reading the current
// revision first so callers do not have to track it. snoozedUntil is honoured
// only for the snoozed state.
func (s *AttentionService) SetAttentionState(ctx context.Context, recipient, id, state, snoozedUntil string) (*types.Attention, error) {
	if !types.ValidAttentionState(state) {
		return nil, fmt.Errorf("invalid state %q", state)
	}
	current, err := s.store.GetAttention(ctx, recipient, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("attention %q not found", id)
	}
	ok, err := s.store.TransitionAttention(ctx, recipient, id, state, snoozedUntil, current.Revision, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("attention %q changed concurrently, retry", id)
	}
	updated, err := s.store.GetAttention(ctx, recipient, id)
	if err != nil {
		return nil, err
	}
	s.emit(ctx, types.EventAttentionUpdated, updated)
	return updated, nil
}

// ListAttention returns a recipient's inbox.
func (s *AttentionService) ListAttention(ctx context.Context, f types.AttentionListFilter) ([]types.Attention, error) {
	return s.store.ListAttention(ctx, f)
}

// GetAttention returns one item for a recipient, or nil when absent.
func (s *AttentionService) GetAttention(ctx context.Context, recipient, id string) (*types.Attention, error) {
	return s.store.GetAttention(ctx, recipient, id)
}

// AttentionCounts returns the bell-badge summary for a recipient.
func (s *AttentionService) AttentionCounts(ctx context.Context, recipient string) (types.AttentionCounts, error) {
	return s.store.AttentionCounts(ctx, recipient)
}

func (s *AttentionService) findByDedup(ctx context.Context, recipient, dedupKey string) (*types.Attention, error) {
	items, err := s.store.ListAttention(ctx, types.AttentionListFilter{Recipient: recipient, IncludeSnoozed: true})
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].DedupKey == dedupKey {
			return &items[i], nil
		}
	}
	return nil, nil
}

func (s *AttentionService) emit(ctx context.Context, eventType string, a *types.Attention) {
	if s.events == nil {
		return
	}
	ev := types.NewEvent(eventType, "api")
	ev.ProjectID = a.Project
	ev.TaskID = a.TaskID
	ev.FeatureID = a.FeatureID
	ev.Metadata = map[string]string{
		"attention_id": a.ID,
		"recipient":    a.Recipient,
		"kind":         a.Kind,
		"severity":     a.Severity,
		"state":        a.State,
	}
	// Best-effort: a durable item already exists; a dropped announcement is
	// recoverable by polling and must not fail the write.
	_ = s.events.Ingest(ctx, []types.Event{ev})
}

func newAttentionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is catastrophic and vanishingly rare; fall back
		// to a time-derived value so we never emit an empty id.
		return fmt.Sprintf("attn%d", time.Now().UnixNano())
	}
	return "attn_" + hex.EncodeToString(b)
}
