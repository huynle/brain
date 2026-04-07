package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEventsTestRouter creates a minimal router with just the events endpoint.
func newEventsTestRouter(bus events.Bus) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithEventBus(bus))
	r := chi.NewRouter()
	r.Post("/events/emit", h.HandleEmitEvent)
	return r
}

func TestHandleEmitEvent(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
		checkEvent func(t *testing.T, bus *capturingBus)
	}{
		{
			name:       "valid event with all fields",
			body:       map[string]any{"type": "runner.started", "payload": map[string]any{"runner_id": "r1"}, "dedup_key": "start-r1"},
			wantStatus: http.StatusAccepted,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
				assert.Equal(t, "accepted", body["status"])
			},
			checkEvent: func(t *testing.T, bus *capturingBus) {
				require.Len(t, bus.events, 1)
				e := bus.events[0]
				assert.Equal(t, events.EventType("runner.started"), e.Type)
				assert.Equal(t, "r1", e.Payload["runner_id"])
				assert.Equal(t, "start-r1", e.DedupKey)
				assert.Equal(t, "external", e.Source)
			},
		},
		{
			name:       "valid event with type only",
			body:       map[string]any{"type": "webhook.received"},
			wantStatus: http.StatusAccepted,
			checkEvent: func(t *testing.T, bus *capturingBus) {
				require.Len(t, bus.events, 1)
				e := bus.events[0]
				assert.Equal(t, events.EventType("webhook.received"), e.Type)
				assert.Equal(t, "external", e.Source)
				assert.NotZero(t, e.Timestamp)
			},
		},
		{
			name:       "missing type returns 400",
			body:       map[string]any{"payload": map[string]any{"foo": "bar"}},
			wantStatus: http.StatusBadRequest,
			checkEvent: func(t *testing.T, bus *capturingBus) {
				assert.Empty(t, bus.events)
			},
		},
		{
			name:       "empty type returns 400",
			body:       map[string]any{"type": ""},
			wantStatus: http.StatusBadRequest,
			checkEvent: func(t *testing.T, bus *capturingBus) {
				assert.Empty(t, bus.events)
			},
		},
		{
			name:       "whitespace-only type returns 400",
			body:       map[string]any{"type": "   "},
			wantStatus: http.StatusBadRequest,
			checkEvent: func(t *testing.T, bus *capturingBus) {
				assert.Empty(t, bus.events)
			},
		},
		{
			name:       "invalid JSON body returns 400",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
			checkEvent: func(t *testing.T, bus *capturingBus) {
				assert.Empty(t, bus.events)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := &capturingBus{}
			router := newEventsTestRouter(bus)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var bodyBytes []byte
			switch v := tt.body.(type) {
			case string:
				bodyBytes = []byte(v)
			default:
				var err error
				bodyBytes, err = json.Marshal(v)
				require.NoError(t, err)
			}

			resp, err := http.Post(srv.URL+"/events/emit", "application/json", bytes.NewReader(bodyBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
			if tt.checkEvent != nil {
				tt.checkEvent(t, bus)
			}
		})
	}
}

func TestHandleEmitEvent_NoBus(t *testing.T) {
	// When no event bus is configured, the endpoint should return 501
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Post("/events/emit", h.HandleEmitEvent)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body := map[string]any{"type": "test.event"}
	bodyBytes, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/events/emit", "application/json", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHandleEmitEvent_SetsExternalSource(t *testing.T) {
	// Even if the caller provides a source, it should be overridden to "external"
	bus := &capturingBus{}
	router := newEventsTestRouter(bus)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := map[string]any{"type": "test.event", "payload": map[string]any{"source": "sneaky"}}
	bodyBytes, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/events/emit", "application/json", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Len(t, bus.events, 1)
	assert.Equal(t, "external", bus.events[0].Source, "source must always be 'external' for API-emitted events")
}

// capturingBus is a simple in-memory bus that captures published events for assertions.
type capturingBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (b *capturingBus) Publish(event events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	b.events = append(b.events, event)
}

func (b *capturingBus) Subscribe(eventType events.EventType, handler events.Handler) events.Subscription {
	return &noopSub{}
}

func (b *capturingBus) SubscribePattern(pattern string, handler events.Handler) events.Subscription {
	return &noopSub{}
}

func (b *capturingBus) Close() {}

type noopSub struct{}

func (s *noopSub) Unsubscribe() {}
