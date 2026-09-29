package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type cancelEmbedding struct {
	started chan struct{}
	exited  chan struct{}
	calls   atomic.Int32
}

func (c *cancelEmbedding) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	c.calls.Add(1)
	close(c.started)
	<-ctx.Done()
	close(c.exited)
	return nil, ctx.Err()
}

func TestBrainGraphCloseCancelsAndJoinsEmbeddings(t *testing.T) {
	client := &cancelEmbedding{started: make(chan struct{}), exited: make(chan struct{})}
	svc, store, _ := newTestBrainServiceWithEmbedding(t, nil)
	closer, ok := any(svc).(interface{ Close() })
	if !ok {
		t.Fatal("BrainService must expose Close to cancel and join graph-owned embeddings")
	}
	entry, err := svc.Save(context.Background(), types.CreateEntryRequest{Type: "note", Title: "Lifecycle", Content: "before", Project: "default"})
	if err != nil {
		t.Fatal(err)
	}
	svc.embeddingClient = client
	svc.scheduleEmbeddingRefresh(entry.Path)
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("embedding did not start")
	}
	done := make(chan struct{})
	go func() { closer.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not cancel and join embedding")
	}
	select {
	case <-client.exited:
	default:
		t.Fatal("Close returned before embedding exited")
	}
	closer.Close()
	svc.scheduleEmbeddingRefresh(entry.Path)
	svc.scheduleEmbeddingMetadataSync(entry.Path)
	svc.WaitForPendingEmbeddings()
	if client.calls.Load() != 1 {
		t.Fatal("embedding admitted after Close")
	}
	if _, err := store.GetNoteByPath(context.Background(), entry.Path); err != nil {
		t.Fatalf("graph closed borrowed store: %v", err)
	}
}

type cancelTransport struct{ started, exited chan struct{} }

func (c *cancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(c.started)
	<-r.Context().Done()
	close(c.exited)
	return nil, r.Context().Err()
}

func TestWebhookGraphCloseCancelsAndJoinsDelivery(t *testing.T) {
	_, store, _ := newTestBrainServiceWithEmbedding(t, nil)
	transport := &cancelTransport{make(chan struct{}), make(chan struct{})}
	svc := NewWebhookServiceWithClient(store, &http.Client{Transport: transport})
	closer, ok := any(svc).(interface{ Close() })
	if !ok {
		t.Fatal("WebhookService must expose Close to cancel and join graph-owned deliveries")
	}
	_, err := svc.Create(context.Background(), types.CreateWebhookRequest{Name: "test", URL: "https://example.test", Events: []string{"task.completed"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Deliver(context.Background(), types.Event{Type: "task.completed"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-transport.started:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not start")
	}
	done := make(chan struct{})
	go func() { closer.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not cancel and join delivery")
	}
	select {
	case <-transport.exited:
	default:
		t.Fatal("Close returned before delivery exited")
	}
	closer.Close()
	if _, err = svc.List(context.Background(), true); err != nil {
		t.Fatalf("graph closed borrowed store: %v", err)
	}
}
