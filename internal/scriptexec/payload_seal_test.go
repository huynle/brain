package scriptexec

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// memoryPayloadKeyStore is a TEST-ONLY key store. Production key custody
// (a location excluded from database backups and the WAL, rotation and
// destruction) belongs to DB.1/DB.6 (SCRIPT-DECISIONS-20261007).
type memoryPayloadKeyStore struct {
	mu        sync.Mutex
	keys      map[string][]byte
	destroyed map[string]bool
	badLength bool
}

func newMemoryPayloadKeyStore() *memoryPayloadKeyStore {
	return &memoryPayloadKeyStore{keys: map[string][]byte{}, destroyed: map[string]bool{}}
}

func (m *memoryPayloadKeyStore) KeyForPeriod(period payloadKeyPeriod) (string, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("p%d", period.Start.Unix())
	if m.destroyed[id] {
		return "", nil, errPayloadKeyDestroyed
	}
	key, ok := m.keys[id]
	if !ok {
		n := 32
		if m.badLength {
			n = 16
		}
		key = make([]byte, n)
		if _, err := rand.Read(key); err != nil {
			return "", nil, err
		}
		m.keys[id] = key
	}
	return id, bytes.Clone(key), nil
}

func (m *memoryPayloadKeyStore) Key(id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.keys[id]
	if !ok || m.destroyed[id] {
		return nil, errPayloadKeyDestroyed
	}
	return bytes.Clone(key), nil
}

// destroyThrough crypto-shreds every key whose period ended at or before now:
// the bytes are overwritten and the ID can never be reissued.
func (m *memoryPayloadKeyStore) destroyThrough(now time.Time, period time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, key := range m.keys {
		var start int64
		fmt.Sscanf(id, "p%d", &start)
		if !time.Unix(start, 0).Add(period).After(now) {
			for i := range key {
				key[i] = 0
			}
			delete(m.keys, id)
			m.destroyed[id] = true
		}
	}
}

var sealT0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func testSealer(t *testing.T) (*payloadSealer, *memoryPayloadKeyStore) {
	t.Helper()
	store := newMemoryPayloadKeyStore()
	s, err := newPayloadSealer(store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, store
}

func TestPayloadSealRoundTripAndBinding(t *testing.T) {
	s, _ := testSealer(t)
	plain := []byte(`{"result":"SECRET-PAYLOAD"}`)
	b := payloadBinding{Tenant: "t1", Execution: "e1", Purpose: "result"}
	expires := sealT0.Add(24 * time.Hour)
	sealed, err := s.Seal(b, expires, sealT0, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Ciphertext, []byte("SECRET-PAYLOAD")) || sealed.KeyID == "" || !sealed.ExpiresAt.Equal(expires) {
		t.Fatalf("sealed payload leaks plaintext or lacks binding: %+v", sealed)
	}
	got, err := s.Open(b, sealT0.Add(time.Hour), sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open=%q err=%v", got, err)
	}
	// Every bound field is authenticated: a different tenant, execution or
	// purpose, or tampered expiry, key ID, nonce or ciphertext, fails closed.
	for name, mutate := range map[string]func(*payloadBinding, *sealedPayload){
		"tenant":     func(b *payloadBinding, _ *sealedPayload) { b.Tenant = "t2" },
		"execution":  func(b *payloadBinding, _ *sealedPayload) { b.Execution = "e2" },
		"purpose":    func(b *payloadBinding, _ *sealedPayload) { b.Purpose = "logs" },
		"expiry":     func(_ *payloadBinding, p *sealedPayload) { p.ExpiresAt = p.ExpiresAt.Add(time.Hour) },
		"nonce":      func(_ *payloadBinding, p *sealedPayload) { p.Nonce = bytes.Clone(p.Nonce); p.Nonce[0] ^= 1 },
		"ciphertext": func(_ *payloadBinding, p *sealedPayload) { p.Ciphertext = bytes.Clone(p.Ciphertext); p.Ciphertext[0] ^= 1 },
	} {
		bb, pp := b, sealed
		mutate(&bb, &pp)
		if _, err := s.Open(bb, sealT0.Add(time.Hour), pp); !errors.Is(err, errPayloadSealing) {
			t.Errorf("%s mismatch: err=%v, want errPayloadSealing", name, err)
		}
	}
	// A key ID from another period cannot open it either.
	other, err := s.Seal(b, sealT0.Add(23*time.Hour), sealT0, plain)
	if err != nil || other.KeyID == sealed.KeyID {
		t.Fatalf("different expiry period must use a different key: %v", err)
	}
	swapped := sealed
	swapped.KeyID = other.KeyID
	if _, err := s.Open(b, sealT0.Add(time.Hour), swapped); !errors.Is(err, errPayloadSealing) {
		t.Fatalf("swapped key ID: err=%v", err)
	}
	// Fresh nonce per seal: identical inputs never produce identical bytes.
	again, _ := s.Seal(b, expires, sealT0, plain)
	if bytes.Equal(again.Ciphertext, sealed.Ciphertext) || bytes.Equal(again.Nonce, sealed.Nonce) {
		t.Fatal("nonce reuse")
	}
}

// SCRIPT-DECISIONS-20261007: the payload is unreadable at its deadline even if
// the period key still exists, and permanently after the key is destroyed.
func TestPayloadSealRefusesAtDeadlineAndAfterKeyDestruction(t *testing.T) {
	s, store := testSealer(t)
	b := payloadBinding{Tenant: "t1", Execution: "e1", Purpose: "logs"}
	expires := sealT0.Add(90 * time.Minute)
	sealed, err := s.Seal(b, expires, sealT0, []byte(`["SECRET-LOG"]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(b, expires.Add(-time.Nanosecond), sealed); err != nil {
		t.Fatalf("open just before deadline: %v", err)
	}
	if _, err := s.Open(b, expires, sealed); !errors.Is(err, errPayloadExpired) {
		t.Fatalf("open at deadline with key present: err=%v, want errPayloadExpired", err)
	}
	// The key covering that deadline is destroyed when its period ends, at
	// most one period after the deadline.
	destroyAt := s.KeyDestroyAt(sealed)
	if destroyAt.Before(expires) || destroyAt.Sub(expires) > time.Hour {
		t.Fatalf("key destroy time %v not within one period after deadline %v", destroyAt, expires)
	}
	store.destroyThrough(destroyAt, time.Hour)
	// Even a caller that ignores the deadline (clock skew, bug) cannot decrypt.
	if _, err := s.Open(b, sealT0, sealed); !errors.Is(err, errPayloadKeyDestroyed) {
		t.Fatalf("open after key destruction: err=%v, want errPayloadKeyDestroyed", err)
	}
	// A destroyed period key is never reissued for new seals.
	if _, err := s.Seal(b, expires, sealT0, []byte(`1`)); !errors.Is(err, errPayloadKeyDestroyed) {
		t.Fatalf("seal into destroyed period: err=%v", err)
	}
}

func TestPayloadSealRefusalsAreBoundedAndContentFree(t *testing.T) {
	s, _ := testSealer(t)
	secret := "SECRET-CONTENT-MARKER"
	ok := payloadBinding{Tenant: "t1", Execution: "e1", Purpose: "result"}
	expires := sealT0.Add(time.Hour)
	cases := map[string]struct {
		b       payloadBinding
		expires time.Time
		plain   []byte
	}{
		"empty plaintext":       {ok, expires, nil},
		"over envelope bound":   {ok, expires, bytes.Repeat([]byte("x"), policyEnvelopeMaxBytes+1)},
		"empty tenant":          {payloadBinding{"", "e1", "result"}, expires, []byte(secret)},
		"empty execution":       {payloadBinding{"t1", "", "result"}, expires, []byte(secret)},
		"empty purpose":         {payloadBinding{"t1", "e1", ""}, expires, []byte(secret)},
		"expiry not in future":  {ok, sealT0, []byte(secret)},
		"expiry beyond 24h":     {ok, sealT0.Add(policyProtectedRetention + time.Second), []byte(secret)},
		"invalid utf8 tenant":   {payloadBinding{"t\xff", "e1", "result"}, expires, []byte(secret)},
		"oversized binding key": {payloadBinding{strings.Repeat("t", 257), "e1", "result"}, expires, []byte(secret)},
	}
	for name, c := range cases {
		_, err := s.Seal(c.b, c.expires, sealT0, c.plain)
		if !errors.Is(err, errPayloadSealing) || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	// Exactly 256 KiB and exactly 24h are allowed.
	if _, err := s.Seal(ok, sealT0.Add(policyProtectedRetention), sealT0, bytes.Repeat([]byte("x"), policyEnvelopeMaxBytes)); err != nil {
		t.Fatalf("boundary payload refused: %v", err)
	}
	// Wrong key length from the store is refused, not truncated or padded.
	fresh, _ := newPayloadSealer(newMemoryPayloadKeyStore(), time.Hour)
	fresh.store.(*memoryPayloadKeyStore).badLength = true
	if _, err := fresh.Seal(ok, expires, sealT0, []byte(secret)); !errors.Is(err, errPayloadSealing) {
		t.Fatalf("short key accepted: %v", err)
	}
	// Errors from Open never include plaintext, ciphertext or binding values.
	sealed, err := s.Seal(ok, expires, sealT0, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Open(payloadBinding{"t-other", "e1", "result"}, sealT0, sealed)
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "t-other") {
		t.Fatalf("open error leaks content: %v", err)
	}
	// Formatting a sealed payload never prints its bytes.
	if out := fmt.Sprintf("%v %+v", sealed, sealed); strings.Contains(out, string(sealed.Ciphertext[:4])) {
		t.Fatalf("sealed payload formatting leaks bytes: %q", out)
	}
}

func TestPayloadSealerConfiguration(t *testing.T) {
	if _, err := newPayloadSealer(nil, time.Hour); !errors.Is(err, errPayloadSealing) {
		t.Fatal("nil key store accepted")
	}
	for _, period := range []time.Duration{0, -time.Hour, time.Second * 30, policyProtectedRetention + time.Hour} {
		if _, err := newPayloadSealer(newMemoryPayloadKeyStore(), period); !errors.Is(err, errPayloadSealing) {
			t.Errorf("period %v accepted", period)
		}
	}
}
