package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
)

// validVerifierRegex matches RFC 7636 unreserved characters: [A-Za-z0-9-._~]
// Code verifier must be 43-128 characters long.
var validVerifierRegex = regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`)

// validBase64URLRegex matches base64url characters without padding.
var validBase64URLRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// GenerateCodeChallenge creates an S256 code challenge from a verifier.
// Uses SHA-256 hash with base64url encoding without padding (RFC 4648 Section 5).
func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// ValidateCodeVerifier checks that the verifier matches the stored challenge.
// Uses constant-time comparison to prevent timing attacks.
func ValidateCodeVerifier(verifier, storedChallenge string) error {
	if !validVerifierRegex.MatchString(verifier) {
		return fmt.Errorf("invalid code verifier format")
	}

	computed := GenerateCodeChallenge(verifier)

	// Constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(computed), []byte(storedChallenge)) != 1 {
		return fmt.Errorf("code verifier does not match challenge")
	}
	return nil
}

// ValidateCodeChallenge checks that a code challenge has valid format.
// S256 challenges are exactly 43 base64url characters (256 bits / 6 bits per char = 43 chars).
func ValidateCodeChallenge(challenge string) error {
	if len(challenge) != 43 {
		return fmt.Errorf("invalid code challenge length: %d (expected 43)", len(challenge))
	}
	if !validBase64URLRegex.MatchString(challenge) {
		return fmt.Errorf("invalid code challenge: contains non-base64url characters")
	}
	return nil
}
