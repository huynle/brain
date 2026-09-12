package storage

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type phonePushTransport func(*http.Request) (*http.Response, error)

func (f phonePushTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func phonePushDevice(t *testing.T, suffix string) PushDevice {
	t.Helper()
	k, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	d := PushDevice{Reminders: true, Jobs: true}
	d.Endpoint = "https://fcm.googleapis.com/fcm/send/" + suffix
	d.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	d.Keys.P256dh = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	return d
}
func phonePushOpen(t *testing.T) *PhonePush {
	t.Helper()
	s, e := OpenPhonePush(filepath.Join(t.TempDir(), "push.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func phonePushCount(t *testing.T, s *PhonePush, query string) int {
	t.Helper()
	var n int
	if e := s.db.QueryRow(query).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestPersistenceDedupeOwnershipAndPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.db")
	s, e := OpenPhonePush(path)
	if e != nil {
		t.Fatal(e)
	}
	d := phonePushDevice(t, "one")
	if e = s.Save("alice", d); e != nil {
		t.Fatal(e)
	}
	key := s.PublicKey
	if e = s.Save("bob", d); e == nil {
		t.Fatal("subscription transferred across owners")
	}
	m := PushMessage{Title: "Brain", URL: "/"}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if e = s.Enqueue("alice", "job", "job:one", now, m); e != nil {
			t.Fatal(e)
		}
	}
	s.Enqueue("bob", "job", "job:other", now, m)
	s.Enqueue("", "reminder", "reminder:old", now.Add(-time.Hour), m)
	if phonePushCount(t, s, "SELECT count(*) FROM deliveries") != 1 {
		t.Fatal("dedupe/owner/time filter failed")
	}
	s.Close()
	s, e = OpenPhonePush(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.PublicKey != key || phonePushCount(t, s, "SELECT count(*) FROM deliveries") != 1 {
		t.Fatal("restart lost state")
	}
	d.Jobs = false
	s.Save("alice", d)
	s.Enqueue("alice", "job", "job:disabled", now, m)
	if phonePushCount(t, s, "SELECT count(*) FROM deliveries") != 1 {
		t.Fatal("disabled job enqueued")
	}
	if e = s.Remove("bob", d.Endpoint); e != nil {
		t.Fatal(e)
	}
	if phonePushCount(t, s, "SELECT count(*) FROM devices") != 1 {
		t.Fatal("cross-owner removal")
	}
	if e = s.Remove("alice", d.Endpoint); e != nil {
		t.Fatal(e)
	}
	if phonePushCount(t, s, "SELECT count(*) FROM deliveries") != 0 {
		t.Fatal("unsubscribe left queued messages")
	}
}
func TestEncryptedDeliveryRetriesAndExpiry(t *testing.T) {
	s := phonePushOpen(t)
	d := phonePushDevice(t, "one")
	if e := s.Save("alice", d); e != nil {
		t.Fatal(e)
	}
	if e := s.Enqueue("", "reminder", "reminder:one", time.Now(), PushMessage{Title: "secret reminder", URL: "/"}); e != nil {
		t.Fatal(e)
	}
	calls := 0
	status := 503
	s.client = &http.Client{Transport: phonePushTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "secret reminder") || r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Error("missing encrypted Web Push envelope")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	if e := s.Deliver(context.Background()); e != nil {
		t.Fatal(e)
	}
	s.Deliver(context.Background())
	if calls != 1 {
		t.Fatal("retry backoff ignored")
	}
	s.db.Exec("UPDATE deliveries SET next=0")
	status = 201
	s.Deliver(context.Background())
	s.Deliver(context.Background())
	if calls != 2 || phonePushCount(t, s, "SELECT count(*) FROM deliveries WHERE state='sent'") != 1 {
		t.Fatal("success wasn't settled")
	}
	s.Enqueue("", "reminder", "reminder:two", time.Now(), PushMessage{Title: "next", URL: "/"})
	status = 410
	s.Deliver(context.Background())
	if phonePushCount(t, s, "SELECT count(*) FROM devices") != 0 {
		t.Fatal("expired phonePushDevice retained")
	}
}
func TestEndpointAndKeysValidation(t *testing.T) {
	for _, endpoint := range []string{"http://fcm.googleapis.com/send/x", "https://localhost/send/x", "https://127.0.0.1/", "https://fcm.googleapis.com.evil.test/", "https://user@fcm.googleapis.com/", "https://fcm.googleapis.com:8443/"} {
		d := phonePushDevice(t, "one")
		d.Endpoint = endpoint
		if ValidatePushDevice(d) == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	d := phonePushDevice(t, "one")
	d.Keys.P256dh = "bad"
	if ValidatePushDevice(d) == nil {
		t.Fatal("bad key accepted")
	}
}
