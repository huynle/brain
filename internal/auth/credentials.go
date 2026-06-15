// Package auth provides username/password credential verification for the
// brain-api human login flow (the PWA password login and the OAuth consent
// page). Credentials are configured via environment for a single operator
// account:
//
//	BRAIN_AUTH_USERNAME       - login username (defaults to "admin" when a hash is set)
//	BRAIN_AUTH_PASSWORD_HASH  - bcrypt hash of the password (generate with `brain auth hash`)
//
// Password login is enabled only when BRAIN_AUTH_PASSWORD_HASH is set.
package auth

import (
	"crypto/subtle"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Environment variable names for the operator credentials.
const (
	EnvUsername     = "BRAIN_AUTH_USERNAME"
	EnvPasswordHash = "BRAIN_AUTH_PASSWORD_HASH"
	defaultUsername = "admin"
)

// Verifier checks username/password credentials against the configured
// single-operator account.
type Verifier struct {
	username     string
	passwordHash string
}

// NewVerifierFromEnv builds a Verifier from environment variables. If
// BRAIN_AUTH_USERNAME is unset it defaults to "admin"; password login stays
// disabled until BRAIN_AUTH_PASSWORD_HASH is set.
func NewVerifierFromEnv() *Verifier {
	u := strings.TrimSpace(os.Getenv(EnvUsername))
	if u == "" {
		u = defaultUsername
	}
	return &Verifier{
		username:     u,
		passwordHash: strings.TrimSpace(os.Getenv(EnvPasswordHash)),
	}
}

// Configured reports whether password login is enabled (a password hash is set).
func (v *Verifier) Configured() bool {
	return v != nil && v.passwordHash != ""
}

// Username returns the configured login username.
func (v *Verifier) Username() string {
	if v == nil {
		return defaultUsername
	}
	return v.username
}

// Verify reports whether the supplied username and password are valid. It always
// performs a bcrypt comparison (even on username mismatch) so request timing
// does not reveal whether the username was correct.
func (v *Verifier) Verify(username, password string) bool {
	if !v.Configured() {
		return false
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(v.username)) == 1
	passOK := bcrypt.CompareHashAndPassword([]byte(v.passwordHash), []byte(password)) == nil
	return userOK && passOK
}

// HashPassword returns a bcrypt hash of the password, suitable for the
// BRAIN_AUTH_PASSWORD_HASH environment variable.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
