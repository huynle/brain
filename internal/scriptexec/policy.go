package scriptexec

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"
)

// Pure enforcement of the user-APPROVED product policy SCRIPT-DECISIONS-20261006
// (Brain plan qfcda7ct, task vggevclc). Inactive: there is no caller, storage,
// route, capability grant or schema. Approval of these rules does NOT allocate
// DB1 tables/profile, grant script:execute, supply S09 fences or select a launcher;
// a future persistence owner must call these checks, not reimplement them.

var errPolicyRefused = errors.New("script policy refused")

// U1 bounds and retention windows. The result bound equals outputQuarantine's.
const (
	policyResultMaxBytes     = 64 << 10
	policyLogMaxBytes        = 16 << 10
	policyLogMaxRecords      = 32
	policyEnvelopeMaxBytes   = 256 << 10
	policyProtectedRetention = 24 * time.Hour
	policyAuditRetention     = 90 * 24 * time.Hour
	policyMACKeyMinBytes     = 32
)

// protectedEnvelope is the only protected material U1 allows to persist, and
// only for policyProtectedRetention. It deliberately has no source field: raw
// submitted source is never stored. Digests are lowercase hex SHA-256 values.
type protectedEnvelope struct {
	Result  []byte
	Logs    [][]byte
	Plan    []byte
	Digests []string
}

type auditOutcome string

const (
	auditSucceeded auditOutcome = "succeeded"
	auditFailed    auditOutcome = "failed"
	auditCancelled auditOutcome = "cancelled"
	auditTimedOut  auditOutcome = "timed_out"
	auditRejected  auditOutcome = "rejected"
)

func (o auditOutcome) valid() bool {
	switch o {
	case auditSucceeded, auditFailed, auditCancelled, auditTimedOut, auditRejected:
		return true
	}
	return false
}

// contentFreeAudit is the 90-day record: actor, times, limits, closed outcome
// and counts only. No digest, source, result, log text or target identifier.
type contentFreeAudit struct {
	Principal              string
	AdmittedAt, TerminalAt time.Time
	TimeoutMS              int
	MaxOperations          int
	Outcome                auditOutcome
	Operations, Logs       int
}

// checkProtectedEnvelope enforces the approved U1 size bounds. Refusal is a
// fixed content-free error; the caller must not retain a partially valid copy.
func checkProtectedEnvelope(e protectedEnvelope) error {
	if len(e.Result) == 0 || len(e.Result) > policyResultMaxBytes || !json.Valid(e.Result) {
		return errPolicyRefused
	}
	if len(e.Logs) > policyLogMaxRecords {
		return errPolicyRefused
	}
	total, logBytes := len(e.Result), 0
	for _, log := range e.Logs {
		if len(log) == 0 || len(log) > policyLogMaxBytes-logBytes || !json.Valid(log) {
			return errPolicyRefused
		}
		logBytes += len(log)
	}
	total += logBytes
	if len(e.Plan) > 0 && !json.Valid(e.Plan) {
		return errPolicyRefused
	}
	total += len(e.Plan)
	for _, digest := range e.Digests {
		if !lowerHexSHA256(digest) {
			return errPolicyRefused
		}
		total += len(digest)
		if total > policyEnvelopeMaxBytes {
			return errPolicyRefused
		}
	}
	if total > policyEnvelopeMaxBytes {
		return errPolicyRefused
	}
	return nil
}

func lowerHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// protectedExpiry returns the fixed U1 expiry. Terminal artifacts expire 24h
// after terminal state; interrupted ones 24h after the admission deadline, which
// is also the ceiling for a terminal recorded late. It takes no access time, so
// replay cannot slide retention forward.
func protectedExpiry(terminalAt, admissionDeadline time.Time, terminal bool) (time.Time, error) {
	if admissionDeadline.IsZero() {
		return time.Time{}, errPolicyRefused
	}
	ceiling := admissionDeadline.Add(policyProtectedRetention)
	if !terminal {
		return ceiling, nil
	}
	if terminalAt.IsZero() {
		return time.Time{}, errPolicyRefused
	}
	if expiry := terminalAt.Add(policyProtectedRetention); expiry.Before(ceiling) {
		return expiry, nil
	}
	return ceiling, nil
}

func auditExpiry(admittedAt time.Time) (time.Time, error) {
	if admittedAt.IsZero() {
		return time.Time{}, errPolicyRefused
	}
	return admittedAt.Add(policyAuditRetention), nil
}

// keyNamespace is the admission namespace of an idempotency key.
type keyNamespace struct {
	Tenant, Principal, Endpoint string
	AdmissionEpoch              uint64
}

// consumedKeyTombstone is the U2 detached residue: no execution, source or
// target identifiers, content hashes or payload. Retained until its namespace
// is irreversibly retired.
type consumedKeyTombstone struct {
	Tenant, Principal, Endpoint string
	AdmissionEpoch              uint64
	KeyMAC                      [sha256.Size]byte
}

// consumedKeyMAC is HMAC-SHA256 under a server-held key over a domain-separated,
// length-prefixed namespace and key, so the raw key is never retained.
func consumedKeyMAC(secret []byte, ns keyNamespace, key string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	if len(secret) < policyMACKeyMinBytes || key == "" || ns.Tenant == "" || ns.Principal == "" || ns.Endpoint == "" || ns.AdmissionEpoch == 0 {
		return out, errPolicyRefused
	}
	mac := hmac.New(sha256.New, secret)
	field := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		mac.Write(n[:])
		mac.Write([]byte(s))
	}
	field("brain-script-consumed-key-v1")
	field(ns.Tenant)
	field(ns.Principal)
	field(ns.Endpoint)
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], ns.AdmissionEpoch)
	mac.Write(epoch[:])
	field(key)
	copy(out[:], mac.Sum(nil))
	return out, nil
}

func newConsumedKeyTombstone(secret []byte, ns keyNamespace, key string) (consumedKeyTombstone, error) {
	sum, err := consumedKeyMAC(secret, ns, key)
	if err != nil {
		return consumedKeyTombstone{}, err
	}
	return consumedKeyTombstone{ns.Tenant, ns.Principal, ns.Endpoint, ns.AdmissionEpoch, sum}, nil
}

// tombstonePurgeable is true only when the tombstone's exact namespace has been
// irreversibly retired. Payload expiry or erasure never purges a tombstone.
func tombstonePurgeable(t consumedKeyTombstone, retired map[keyNamespace]bool) bool {
	return retired[keyNamespace{t.Tenant, t.Principal, t.Endpoint, t.AdmissionEpoch}]
}

type replayState int

const (
	replayInProgress replayState = iota + 1
	replayPayloadLive
	replayTombstoned
	replayErased
)

type replayRecord struct {
	State       replayState
	Fingerprint string
	ExpiresAt   time.Time
}

type replayAction int

const (
	replayRefused replayAction = iota
	replayAdmit
	replayReturnStored
	replayConflict
	replayRetired
)

type replayDecision struct {
	Action     replayAction
	Code       string
	HTTPStatus int
}

// decideReplay applies U2. A consumed key is never run again: once its payload
// has expired, been erased or tombstoned (or its state is unknown) it returns a
// content-free 409 idempotency_key_retired regardless of fingerprint. Before
// expiry, a matching fingerprint may return the stored result only after the
// caller separately authorizes output release; a mismatch conflicts.
// Codes approved by SCRIPT-DECISIONS-20261006/20261006b, all HTTP 409 and
// content-free: idempotency_key_retired, idempotency_key_conflict,
// idempotency_key_in_progress.
func decideReplay(existing *replayRecord, fingerprint string, now time.Time) replayDecision {
	if fingerprint == "" {
		return replayDecision{Action: replayRefused}
	}
	if existing == nil {
		return replayDecision{Action: replayAdmit}
	}
	switch existing.State {
	case replayInProgress:
		if !now.Before(existing.ExpiresAt) {
			break
		}
		return replayDecision{replayConflict, "idempotency_key_in_progress", 409}
	case replayPayloadLive:
		if !now.Before(existing.ExpiresAt) {
			break
		}
		if !hmac.Equal([]byte(existing.Fingerprint), []byte(fingerprint)) {
			return replayDecision{replayConflict, "idempotency_key_conflict", 409}
		}
		return replayDecision{Action: replayReturnStored}
	}
	return replayDecision{replayRetired, "idempotency_key_retired", 409}
}

// submitCredential is the verified caller description a future P6 adapter must
// supply. Role is the server-verified project role, compared exactly.
type submitCredential struct {
	AuthEnabled bool
	Verified    bool
	Human       bool
	Role        string
	ScriptOptIn bool
}

// checkSubmitEligibility applies U3: only explicitly opted-in, verified
// owner/admin human credentials. Auth-disabled mode cannot submit scripts
// (credential-free submission deferred); ordinary auth-off REST is unaffected.
// This is eligibility only, NOT a script:execute grant.
func checkSubmitEligibility(c submitCredential) error {
	if !c.AuthEnabled || !c.Verified || !c.Human || !c.ScriptOptIn {
		return errPolicyRefused
	}
	if c.Role != "owner" && c.Role != "admin" {
		return errPolicyRefused
	}
	return nil
}
