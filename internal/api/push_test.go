package api

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

type pushReminders struct {
	ReminderService
	rows []types.ReminderSummary
}

func (s pushReminders) ListReminders(context.Context, string, string) ([]types.ReminderSummary, error) {
	return s.rows, nil
}
func TestPushReminderAndJobRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.db")
	p, e := storage.OpenPhonePush(path)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	var d storage.PushDevice
	d.Endpoint = "https://fcm.googleapis.com/fcm/send/test"
	d.Keys.P256dh = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	d.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	d.Jobs = true
	d.Reminders = true
	owner := "local:api_token:alice"
	if e = p.Save(owner, d); e != nil {
		t.Fatal(e)
	}
	jobs, e := storage.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer jobs.Close()
	for _, o := range []string{owner, "local:api_token:bob"} {
		e = jobs.Create(storage.Record{Owner: o, Job: storage.Job{ID: o, State: "completed", Revision: 1, Conversation: "seeded"}})
		if e != nil {
			t.Fatal(e)
		}
	}
	h := &Handler{push: p, reminders: pushReminders{rows: []types.ReminderSummary{{ReminderID: "seeded", Title: "A seeded reminder", FiredAt: time.Now().UTC().Format(time.RFC3339)}}}, assistant: &AssistantService{jobs: &conversationJobs{store: jobs}}}
	h.collectPush(context.Background())
	h.collectPush(context.Background())
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow("SELECT count(*) FROM deliveries").Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 2 {
		t.Fatalf("want one reminder and only Alice's job, got %d", count)
	}
	var body string
	if e = db.QueryRow("SELECT payload FROM deliveries WHERE event LIKE 'reminder:%'").Scan(&body); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(body, "A seeded reminder") {
		t.Fatal(body)
	}
}

func TestPushRoutesRequireAdministratorAndLocalIdentity(t *testing.T) {
	p, e := storage.OpenPhonePush(filepath.Join(t.TempDir(), "push.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	h := &Handler{push: p}
	for _, scope := range []string{"read:*", "admin:*"} {
		ctx := context.WithValue(tenant.Into(context.Background(), tenant.Local), ctxAuthResult, &AuthResult{Type: "api_token", Name: "alice", Scope: scope})
		router := chi.NewRouter()
		router.Use(RequireScope("admin:*"))
		router.Get("/push", h.HandlePush)
		r := httptest.NewRequest("GET", "/push", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if scope == "read:*" {
			if w.Code != http.StatusForbidden {
				t.Fatal(w.Code)
			}
		} else {
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var data map[string]string
			json.Unmarshal(w.Body.Bytes(), &data)
			if data["public_key"] == "" || strings.Contains(w.Body.String(), "private") {
				t.Fatal("wrong key response")
			}
		}
	}
}
