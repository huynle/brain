package scriptexec

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// Crypto-shredding for protected script payloads (SCRIPT-DECISIONS-20261007).
// Protected results/logs/plans/digests are only ever stored sealed under a
// short-lived period key held OUTSIDE the backed-up database, so no plaintext
// reaches the database, its WAL or VACUUM INTO backups. Destroying the key
// makes every copy, including 90-day backups, undecryptable.
//
// Inactive and pure: no caller, storage, schema or key-file location is chosen
// here. Key custody — a location excluded from database backups and the WAL,
// rotation and the destruction step — is requested from DB.1/DB.6.
//
// Timing: a payload is sealed under the key for the period containing its
// DEADLINE (not its seal time). Open refuses at the exact deadline (the
// deadline and its key period are authenticated, so neither can be moved);
// the key is destroyed when its period ends. Key periods are capped at 5
// minutes, so every key is destroyed within at most 5 minutes after the
// deadlines it covers (SCRIPT-DECISIONS-20261007).
//
// The caller supplies now; a wrong clock can defeat the software refusal only
// until the key is destroyed. Key zeroing is best-effort: the AES key schedule
// inside cipher objects, garbage-collector copies and the key store's own copy
// are not wiped by this package.

var (
	errPayloadSealing      = errors.New("script payload sealing refused")
	errPayloadExpired      = errors.New("script payload expired")
	errPayloadKeyDestroyed = errors.New("script payload key destroyed")
)

const (
	payloadKeyBytes      = 32 // AES-256
	payloadNonceBytes    = 12
	payloadBindingMaxLen = 256
	payloadMinKeyPeriod  = time.Minute
	payloadMaxKeyPeriod  = 5 * time.Minute
	// Deadlines are bounded to a sane absolute range so their encoding
	// (int64 seconds + uint32 nanoseconds) can never wrap.
	payloadMinUnix    = 946684800  // 2000-01-01T00:00:00Z
	payloadMaxUnix    = 7258118400 // 2200-01-01T00:00:00Z
	payloadSealDomain = "brain-script-payload-v1"
)

// payloadKeyPeriod identifies one key period [Start, Start+Length).
type payloadKeyPeriod struct {
	Start  time.Time
	Length time.Duration
}

// payloadKeyStore holds period keys outside the backed-up database. Both
// methods must return errPayloadKeyDestroyed (wrapped or not) for a destroyed
// period, and a destroyed period must never be reissued. Returned key slices
// are copies the sealer may overwrite.
type payloadKeyStore interface {
	KeyForPeriod(period payloadKeyPeriod) (keyID string, key []byte, err error)
	Key(keyID string) ([]byte, error)
}

// payloadBinding is authenticated as associated data: a sealed payload opens
// only for the same tenant, execution and purpose.
type payloadBinding struct {
	Tenant, Execution, Purpose string
}

// sealedPayload is what may be persisted. It never contains plaintext.
type sealedPayload struct {
	KeyID       string
	PeriodStart time.Time // start of the key period containing ExpiresAt
	ExpiresAt   time.Time
	Nonce       []byte
	Ciphertext  []byte
}

// Format never prints nonce or ciphertext bytes.
func (p sealedPayload) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[sealed script payload key="+p.KeyID+" expires="+p.ExpiresAt.UTC().Format(time.RFC3339)+"]")
}

type payloadSealer struct {
	store  payloadKeyStore
	period time.Duration
}

func newPayloadSealer(store payloadKeyStore, period time.Duration) (*payloadSealer, error) {
	if store == nil || period < payloadMinKeyPeriod || period > payloadMaxKeyPeriod {
		return nil, errPayloadSealing
	}
	return &payloadSealer{store: store, period: period}, nil
}

func validBinding(b payloadBinding) bool {
	for _, v := range []string{b.Tenant, b.Execution, b.Purpose} {
		if v == "" || len(v) > payloadBindingMaxLen || !utf8.ValidString(v) {
			return false
		}
	}
	return true
}

func validDeadline(t time.Time) bool {
	u := t.Unix()
	return u >= payloadMinUnix && u < payloadMaxUnix
}

func (s *payloadSealer) periodFor(expiresAt time.Time) payloadKeyPeriod {
	return payloadKeyPeriod{Start: expiresAt.UTC().Truncate(s.period), Length: s.period}
}

// KeyDestroyAt is when the key covering this payload's deadline may be
// destroyed: the end of that period.
func (s *payloadSealer) KeyDestroyAt(p sealedPayload) time.Time {
	period := s.periodFor(p.ExpiresAt)
	return period.Start.Add(period.Length)
}

// associatedData is the documented AAD layout (pinned by an independent
// specification test): 8-byte big-endian length-prefixed domain, key ID,
// tenant, execution, purpose; then period start (int64 seconds), period
// length (int64 nanoseconds), deadline (int64 seconds) and deadline
// nanoseconds (uint32), all big-endian. Callers validate the ranges first.
func associatedData(keyID string, b payloadBinding, period payloadKeyPeriod, expiresAt time.Time) []byte {
	var ad []byte
	field := func(v string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(v)))
		ad = append(ad, n[:]...)
		ad = append(ad, v...)
	}
	field(payloadSealDomain)
	field(keyID)
	field(b.Tenant)
	field(b.Execution)
	field(b.Purpose)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(period.Start.Unix()))
	ad = append(ad, n[:]...)
	binary.BigEndian.PutUint64(n[:], uint64(period.Length))
	ad = append(ad, n[:]...)
	binary.BigEndian.PutUint64(n[:], uint64(expiresAt.Unix()))
	ad = append(ad, n[:]...)
	var ns [4]byte
	binary.BigEndian.PutUint32(ns[:], uint32(expiresAt.Nanosecond()))
	return append(ad, ns[:]...)
}

func gcmFor(key []byte) (cipher.AEAD, error) {
	if len(key) != payloadKeyBytes {
		return nil, errPayloadSealing
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errPayloadSealing
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errPayloadSealing
	}
	return aead, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Seal encrypts a protected payload whose retention ends at expiresAt, which
// must lie in (now, now+24h] (SDK-U1). Plaintext is bounded by the 256 KiB
// envelope. Errors are fixed and content-free.
func (s *payloadSealer) Seal(b payloadBinding, expiresAt, now time.Time, plaintext []byte) (sealedPayload, error) {
	if !validBinding(b) || len(plaintext) == 0 || len(plaintext) > policyEnvelopeMaxBytes {
		return sealedPayload{}, errPayloadSealing
	}
	if !validDeadline(expiresAt) || !expiresAt.After(now) || expiresAt.Sub(now) > policyProtectedRetention {
		return sealedPayload{}, errPayloadSealing
	}
	period := s.periodFor(expiresAt)
	keyID, key, err := s.store.KeyForPeriod(period)
	defer zero(key)
	if errors.Is(err, errPayloadKeyDestroyed) {
		return sealedPayload{}, errPayloadKeyDestroyed
	}
	if err != nil || keyID == "" || len(keyID) > payloadBindingMaxLen {
		return sealedPayload{}, errPayloadSealing
	}
	aead, err := gcmFor(key)
	if err != nil {
		return sealedPayload{}, err
	}
	nonce := make([]byte, payloadNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return sealedPayload{}, errPayloadSealing
	}
	expires := expiresAt.UTC()
	return sealedPayload{
		KeyID:       keyID,
		PeriodStart: period.Start,
		ExpiresAt:   expires,
		Nonce:       nonce,
		Ciphertext:  aead.Seal(nil, nonce, plaintext, associatedData(keyID, b, period, expires)),
	}, nil
}

// Open decrypts a sealed payload for the same binding before its deadline.
// At or after the deadline it refuses without touching the key; after the
// key is destroyed it refuses permanently.
func (s *payloadSealer) Open(b payloadBinding, now time.Time, p sealedPayload) ([]byte, error) {
	if !validBinding(b) || p.KeyID == "" || len(p.Nonce) != payloadNonceBytes || len(p.Ciphertext) == 0 {
		return nil, errPayloadSealing
	}
	// Range checks precede the deadline check so a forged far-future or
	// out-of-period deadline is refused, never treated as "not yet expired".
	period := s.periodFor(p.ExpiresAt)
	if !validDeadline(p.ExpiresAt) || !validDeadline(p.PeriodStart) || !p.PeriodStart.Equal(period.Start) ||
		p.ExpiresAt.Before(period.Start) || !p.ExpiresAt.Before(period.Start.Add(period.Length)) ||
		p.ExpiresAt.Sub(now) > policyProtectedRetention {
		return nil, errPayloadSealing
	}
	if !now.Before(p.ExpiresAt) {
		return nil, errPayloadExpired
	}
	key, err := s.store.Key(p.KeyID)
	defer zero(key)
	if errors.Is(err, errPayloadKeyDestroyed) {
		return nil, errPayloadKeyDestroyed
	}
	if err != nil {
		return nil, errPayloadSealing
	}
	aead, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, p.Nonce, p.Ciphertext, associatedData(p.KeyID, b, period, p.ExpiresAt))
	if err != nil {
		return nil, errPayloadSealing
	}
	return plaintext, nil
}
