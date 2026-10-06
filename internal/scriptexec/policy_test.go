package scriptexec

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApprovedPolicyConstants(t *testing.T) {
	if policyResultMaxBytes != 64<<10 || policyLogMaxBytes != 16<<10 || policyLogMaxRecords != 32 || policyEnvelopeMaxBytes != 256<<10 {
		t.Fatal("approved SCRIPT-DECISIONS-20261006 size bounds changed")
	}
	if policyProtectedRetention != 24*time.Hour || policyAuditRetention != 90*24*time.Hour {
		t.Fatal("approved retention windows changed")
	}
	if policyResultMaxBytes != 65536 {
		t.Fatal("result bound must match the inactive quarantine")
	}
}

func repeatJSONString(n int) []byte {
	// A JSON string literal whose encoded length is exactly n bytes (n >= 2).
	return append(append([]byte{'"'}, bytes.Repeat([]byte{'a'}, n-2)...), '"')
}

func TestCheckProtectedEnvelopeBounds(t *testing.T) {
	log := func(n int) []byte { return repeatJSONString(n) }
	ok := protectedEnvelope{Result: repeatJSONString(policyResultMaxBytes), Logs: [][]byte{log(policyLogMaxBytes)}}
	if err := checkProtectedEnvelope(ok); err != nil {
		t.Fatalf("exact result/log bounds refused: %v", err)
	}
	thirtyTwo := make([][]byte, policyLogMaxRecords)
	for i := range thirtyTwo {
		thirtyTwo[i] = log(16)
	}
	if err := checkProtectedEnvelope(protectedEnvelope{Result: []byte("1"), Logs: thirtyTwo}); err != nil {
		t.Fatalf("exact 32 records refused: %v", err)
	}
	plan := repeatJSONString(policyEnvelopeMaxBytes - policyResultMaxBytes - policyLogMaxBytes)
	if err := checkProtectedEnvelope(protectedEnvelope{Result: ok.Result, Logs: ok.Logs, Plan: plan}); err != nil {
		t.Fatalf("exact 256KiB envelope refused: %v", err)
	}
	cases := map[string]protectedEnvelope{
		"result over":   {Result: repeatJSONString(policyResultMaxBytes + 1)},
		"logs over":     {Result: []byte("1"), Logs: [][]byte{log(policyLogMaxBytes/2 + 1), log(policyLogMaxBytes / 2)}},
		"records over":  {Result: []byte("1"), Logs: append(thirtyTwo, log(4))},
		"envelope over": {Result: ok.Result, Logs: ok.Logs, Plan: append(plan[:len(plan):len(plan)], ' ')},
		"invalid json":  {Result: []byte("{")},
		"empty result":  {},
		"empty log":     {Result: []byte("1"), Logs: [][]byte{{}}},
		"invalid log":   {Result: []byte("1"), Logs: [][]byte{[]byte(`{"level":`)}},
		"invalid plan":  {Result: []byte("1"), Plan: []byte(`[1,`)},
		"digest format": {Result: []byte("1"), Digests: []string{"not-hex"}},
	}
	for name, envelope := range cases {
		if err := checkProtectedEnvelope(envelope); !errors.Is(err, errPolicyRefused) {
			t.Errorf("%s: err=%v, want errPolicyRefused", name, err)
		}
	}
	// Digests count toward the 256KiB envelope as their encoded hex bytes.
	digests := make([]string, 0)
	for i := 0; i < (policyEnvelopeMaxBytes/64)+1; i++ {
		digests = append(digests, strings.Repeat("a", 64))
	}
	if err := checkProtectedEnvelope(protectedEnvelope{Result: []byte("1"), Digests: digests}); !errors.Is(err, errPolicyRefused) {
		t.Errorf("oversized digests accepted: %v", err)
	}
}

// Approved U1: raw submitted source is never persisted. Every persistable
// policy type is checked structurally so a future field cannot smuggle it in.
func TestPersistablePolicyTypesCarryNoSource(t *testing.T) {
	allowed := map[string]map[string]bool{
		"protectedEnvelope":    {"Result": true, "Logs": true, "Plan": true, "Digests": true},
		"contentFreeAudit":     {"Principal": true, "AdmittedAt": true, "TerminalAt": true, "TimeoutMS": true, "MaxOperations": true, "Outcome": true, "Operations": true, "Logs": true},
		"consumedKeyTombstone": {"Tenant": true, "Principal": true, "Endpoint": true, "AdmissionEpoch": true, "KeyMAC": true},
	}
	for _, v := range []any{protectedEnvelope{}, contentFreeAudit{}, consumedKeyTombstone{}} {
		typ := reflect.TypeOf(v)
		want := allowed[typ.Name()]
		if typ.NumField() != len(want) {
			t.Errorf("%s has %d fields, want exact allowlist %d", typ.Name(), typ.NumField(), len(want))
		}
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			lower := strings.ToLower(name)
			if !want[name] || strings.Contains(lower, "source") || strings.Contains(lower, "script") {
				t.Errorf("%s.%s is not an approved persistable field", typ.Name(), name)
			}
		}
	}
	// Audit outcome is a closed set; count fields are integers, not content.
	if _, ok := reflect.TypeOf(contentFreeAudit{}).FieldByName("Outcome"); !ok {
		t.Fatal("audit outcome missing")
	}
	for _, outcome := range []auditOutcome{auditSucceeded, auditFailed, auditCancelled, auditTimedOut, auditRejected} {
		if !outcome.valid() {
			t.Errorf("%q should be valid", outcome)
		}
	}
	if auditOutcome("anything else").valid() {
		t.Fatal("open audit outcome accepted")
	}
}

func TestRetentionExpiry(t *testing.T) {
	admitted := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	deadline := admitted.Add(30 * time.Second)
	terminal := admitted.Add(5 * time.Second)
	got, err := protectedExpiry(terminal, deadline, true)
	if err != nil || !got.Equal(terminal.Add(24*time.Hour)) {
		t.Fatalf("terminal expiry=%v err=%v", got, err)
	}
	got, err = protectedExpiry(time.Time{}, deadline, false)
	if err != nil || !got.Equal(deadline.Add(24*time.Hour)) {
		t.Fatalf("interrupted expiry=%v err=%v", got, err)
	}
	// A terminal state recorded after the admission deadline cannot extend
	// retention beyond the interrupted ceiling.
	got, err = protectedExpiry(deadline.Add(time.Hour), deadline, true)
	if err != nil || !got.Equal(deadline.Add(24*time.Hour)) {
		t.Fatalf("late terminal expiry=%v err=%v", got, err)
	}
	if _, err := protectedExpiry(terminal, time.Time{}, true); !errors.Is(err, errPolicyRefused) {
		t.Fatal("missing admission deadline accepted")
	}
	if _, err := protectedExpiry(time.Time{}, deadline, true); !errors.Is(err, errPolicyRefused) {
		t.Fatal("terminal without terminal time accepted")
	}
	audit, err := auditExpiry(admitted)
	if err != nil || !audit.Equal(admitted.Add(90*24*time.Hour)) {
		t.Fatalf("audit expiry=%v err=%v", audit, err)
	}
	if _, err := auditExpiry(time.Time{}); !errors.Is(err, errPolicyRefused) {
		t.Fatal("zero audit time accepted")
	}
}

func testMACKey() []byte { return bytes.Repeat([]byte{7}, 32) }

func TestConsumedKeyMAC(t *testing.T) {
	base := keyNamespace{Tenant: "t", Principal: "p", Endpoint: "script.execute", AdmissionEpoch: 1}
	a, err := consumedKeyMAC(testMACKey(), base, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := consumedKeyMAC(testMACKey(), base, "key-1")
	if a != again {
		t.Fatal("MAC not deterministic")
	}
	variants := []struct {
		ns  keyNamespace
		key string
	}{
		{keyNamespace{"t2", "p", "script.execute", 1}, "key-1"},
		{keyNamespace{"t", "p2", "script.execute", 1}, "key-1"},
		{keyNamespace{"t", "p", "other", 1}, "key-1"},
		{keyNamespace{"t", "p", "script.execute", 2}, "key-1"},
		{base, "key-2"},
	}
	for _, v := range variants {
		other, err := consumedKeyMAC(testMACKey(), v.ns, v.key)
		if err == nil && other == a {
			t.Errorf("MAC collision for %+v %q", v.ns, v.key)
		}
	}
	// Length prefixes: both namespaces are valid (non-empty fields) and their
	// naive concatenations are identical, so only the prefixes separate them.
	left, errL := consumedKeyMAC(testMACKey(), keyNamespace{"t", "pX", "e", 1}, "k")
	right, errR := consumedKeyMAC(testMACKey(), keyNamespace{"tp", "X", "e", 1}, "k")
	if errL != nil || errR != nil || left == right {
		t.Fatalf("length-prefix ambiguity: %v %v equal=%v", errL, errR, left == right)
	}
	keyShift, _ := consumedKeyMAC(testMACKey(), keyNamespace{"t", "p", "ek", 1}, "ey")
	keyBase, _ := consumedKeyMAC(testMACKey(), keyNamespace{"t", "p", "e", 1}, "key")
	if keyShift == keyBase {
		t.Fatal("endpoint/key boundary ambiguity")
	}
	// Golden value pins the exact construction (domain label
	// "brain-script-consumed-key-v1", 8-byte big-endian length prefixes,
	// big-endian epoch): dropping or changing any part changes it.
	if got := hex.EncodeToString(a[:]); got != goldenConsumedKeyMAC {
		t.Fatalf("consumed-key MAC construction changed: %s", got)
	}
	if _, err := consumedKeyMAC(make([]byte, 31), base, "key-1"); !errors.Is(err, errPolicyRefused) {
		t.Fatal("short MAC key accepted")
	}
	if _, err := consumedKeyMAC(testMACKey(), base, ""); !errors.Is(err, errPolicyRefused) {
		t.Fatal("empty idempotency key accepted")
	}
	if _, err := consumedKeyMAC(testMACKey(), keyNamespace{Tenant: "t", Principal: "p", Endpoint: "e"}, "k"); !errors.Is(err, errPolicyRefused) {
		t.Fatal("zero admission epoch accepted")
	}
	tomb, err := newConsumedKeyTombstone(testMACKey(), base, "key-1")
	if err != nil || tomb.KeyMAC != a || tomb.Tenant != "t" || tomb.AdmissionEpoch != 1 {
		t.Fatalf("tombstone=%+v err=%v", tomb, err)
	}
}

func TestReplayDecision(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	live := replayRecord{State: replayPayloadLive, Fingerprint: "f1", ExpiresAt: now.Add(time.Hour)}
	cases := []struct {
		name   string
		record *replayRecord
		fp     string
		want   replayAction
		code   string
	}{
		{"fresh key admits", nil, "f1", replayAdmit, ""},
		{"live match returns stored", &live, "f1", replayReturnStored, ""},
		{"live mismatch conflicts", &live, "f2", replayConflict, "idempotency_key_conflict"},
		{"in progress never reruns", &replayRecord{State: replayInProgress, Fingerprint: "f1", ExpiresAt: now.Add(time.Hour)}, "f1", replayConflict, "idempotency_key_in_progress"},
		// An abandoned claim past its deadline-anchored expiry is retired: it is
		// neither re-admitted nor reported as still in progress.
		{"expired in progress is retired", &replayRecord{State: replayInProgress, Fingerprint: "f1", ExpiresAt: now}, "f1", replayRetired, "idempotency_key_retired"},
		{"long-expired in progress is retired", &replayRecord{State: replayInProgress, Fingerprint: "f1", ExpiresAt: now.Add(-48 * time.Hour)}, "f1", replayRetired, "idempotency_key_retired"},
		{"exactly expired is retired", &replayRecord{State: replayPayloadLive, Fingerprint: "f1", ExpiresAt: now}, "f1", replayRetired, "idempotency_key_retired"},
		{"tombstone retired same fp", &replayRecord{State: replayTombstoned}, "f1", replayRetired, "idempotency_key_retired"},
		{"tombstone retired other fp", &replayRecord{State: replayTombstoned}, "zzz", replayRetired, "idempotency_key_retired"},
		{"erased retired", &replayRecord{State: replayErased, Fingerprint: "f1", ExpiresAt: now.Add(time.Hour)}, "f1", replayRetired, "idempotency_key_retired"},
		{"unknown state retires", &replayRecord{State: replayState(99), Fingerprint: "f1", ExpiresAt: now.Add(time.Hour)}, "f1", replayRetired, "idempotency_key_retired"},
	}
	for _, c := range cases {
		got := decideReplay(c.record, c.fp, now)
		if got.Action != c.want || got.Code != c.code {
			t.Errorf("%s: %+v, want %v %q", c.name, got, c.want, c.code)
		}
		// SCRIPT-DECISIONS-20261006(b): all three approved codes are HTTP 409.
		if (got.Action == replayRetired || got.Action == replayConflict) && got.HTTPStatus != 409 {
			t.Errorf("%s: approved code %q must be 409, got %d", c.name, got.Code, got.HTTPStatus)
		}
	}
	if decideReplay(nil, "", now).Action != replayRefused {
		t.Fatal("empty fingerprint admitted")
	}
	// Replay reads never extend retention: decideReplay does not return or
	// mutate an expiry. The record is passed by pointer; check it is unchanged.
	before := live
	_ = decideReplay(&live, "f1", now)
	if live != before {
		t.Fatal("replay decision mutated record (sliding extension)")
	}
}

func TestTombstonePurgeOnlyAfterNamespaceRetirement(t *testing.T) {
	tomb := consumedKeyTombstone{Tenant: "t", Principal: "p", Endpoint: "e", AdmissionEpoch: 3}
	if tombstonePurgeable(tomb, nil) {
		t.Fatal("purgeable with no retirement")
	}
	if tombstonePurgeable(tomb, map[keyNamespace]bool{{"t", "p", "e", 2}: true, {"t", "p", "e2", 3}: true}) {
		t.Fatal("purgeable from a different namespace retirement")
	}
	if !tombstonePurgeable(tomb, map[keyNamespace]bool{{"t", "p", "e", 3}: true}) {
		t.Fatal("irreversibly retired namespace still retains tombstone")
	}
}

func TestSubmitEligibility(t *testing.T) {
	good := submitCredential{AuthEnabled: true, Verified: true, Human: true, Role: "owner", ScriptOptIn: true}
	if err := checkSubmitEligibility(good); err != nil {
		t.Fatalf("owner refused: %v", err)
	}
	admin := good
	admin.Role = "admin"
	if err := checkSubmitEligibility(admin); err != nil {
		t.Fatalf("admin refused: %v", err)
	}
	mutate := map[string]func(*submitCredential){
		"auth disabled (credential-free deferred)": func(c *submitCredential) { c.AuthEnabled = false },
		"unverified":                     func(c *submitCredential) { c.Verified = false },
		"service/runner/OAuth non-human": func(c *submitCredential) { c.Human = false },
		"editor role":                    func(c *submitCredential) { c.Role = "editor" },
		"member role":                    func(c *submitCredential) { c.Role = "member" },
		"wildcard role":                  func(c *submitCredential) { c.Role = "admin:*" },
		"case-folded role":               func(c *submitCredential) { c.Role = "Owner" },
		"no explicit opt-in":             func(c *submitCredential) { c.ScriptOptIn = false },
	}
	for name, m := range mutate {
		c := good
		m(&c)
		if err := checkSubmitEligibility(c); !errors.Is(err, errPolicyRefused) {
			t.Errorf("%s: err=%v, want refusal", name, err)
		}
	}
}

// Refusal errors are fixed and content-free.
func TestPolicyRefusalIsContentFree(t *testing.T) {
	secret := "SECRET-SOURCE-MARKER"
	err := checkProtectedEnvelope(protectedEnvelope{Result: []byte(`"` + secret)})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("refusal leaked content: %v", err)
	}
	d := decideReplay(&replayRecord{State: replayTombstoned, Fingerprint: secret}, secret, time.Now())
	if strings.Contains(d.Code, secret) {
		t.Fatal("replay decision leaked content")
	}
}

const goldenConsumedKeyMAC = "7ab40b0a87d306f2bee405c3665cc2b879b6df7986e9a0e1d7fa23411e49adf5"
