package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// RFC 7636 Appendix B test vector
const (
	// The RFC 7636 example code verifier
	rfcVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// The expected S256 challenge for the above verifier
	rfcChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

func TestGenerateCodeChallenge(t *testing.T) {
	t.Run("RFC 7636 test vector", func(t *testing.T) {
		challenge := GenerateCodeChallenge(rfcVerifier)
		if challenge != rfcChallenge {
			t.Errorf("GenerateCodeChallenge(%q) = %q, want %q", rfcVerifier, challenge, rfcChallenge)
		}
	})

	t.Run("produces base64url without padding", func(t *testing.T) {
		challenge := GenerateCodeChallenge("abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
		if strings.Contains(challenge, "=") {
			t.Errorf("challenge contains padding: %q", challenge)
		}
		if strings.Contains(challenge, "+") || strings.Contains(challenge, "/") {
			t.Errorf("challenge contains standard base64 chars (+/): %q", challenge)
		}
	})

	t.Run("produces exactly 43 characters", func(t *testing.T) {
		// SHA-256 produces 32 bytes, base64url encodes to ceil(32*4/3) = 43 chars (no padding)
		challenge := GenerateCodeChallenge("abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
		if len(challenge) != 43 {
			t.Errorf("challenge length = %d, want 43", len(challenge))
		}
	})

	t.Run("different verifiers produce different challenges", func(t *testing.T) {
		c1 := GenerateCodeChallenge("abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
		c2 := GenerateCodeChallenge("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqr")
		if c1 == c2 {
			t.Error("different verifiers produced same challenge")
		}
	})

	t.Run("uses SHA-256", func(t *testing.T) {
		verifier := "test-verifier-that-is-long-enough-to-pass-validation"
		expected := sha256.Sum256([]byte(verifier))
		expectedStr := base64.RawURLEncoding.EncodeToString(expected[:])
		got := GenerateCodeChallenge(verifier)
		if got != expectedStr {
			t.Errorf("challenge does not match manual SHA-256 computation: got %q, want %q", got, expectedStr)
		}
	})
}

func TestValidateCodeVerifier(t *testing.T) {
	t.Run("valid verifier and matching challenge", func(t *testing.T) {
		challenge := GenerateCodeChallenge(rfcVerifier)
		err := ValidateCodeVerifier(rfcVerifier, challenge)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid verifier but wrong challenge", func(t *testing.T) {
		err := ValidateCodeVerifier(rfcVerifier, "wrong-challenge-value-that-is-43-chars-long")
		if err == nil {
			t.Error("expected error for mismatched challenge")
		}
		if !strings.Contains(err.Error(), "does not match") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("verifier too short", func(t *testing.T) {
		err := ValidateCodeVerifier("tooshort", "some-challenge")
		if err == nil {
			t.Error("expected error for short verifier")
		}
		if !strings.Contains(err.Error(), "invalid code verifier format") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("verifier too long", func(t *testing.T) {
		// 129 characters - exceeds max of 128
		longVerifier := strings.Repeat("a", 129)
		err := ValidateCodeVerifier(longVerifier, "some-challenge")
		if err == nil {
			t.Error("expected error for long verifier")
		}
	})

	t.Run("verifier with invalid characters", func(t *testing.T) {
		// Contains spaces and special chars not in unreserved set
		badVerifier := "this has spaces and !@#$ invalid chars padding"
		err := ValidateCodeVerifier(badVerifier, "some-challenge")
		if err == nil {
			t.Error("expected error for verifier with invalid chars")
		}
	})

	t.Run("verifier exactly 43 chars (minimum)", func(t *testing.T) {
		verifier := strings.Repeat("a", 43)
		challenge := GenerateCodeChallenge(verifier)
		err := ValidateCodeVerifier(verifier, challenge)
		if err != nil {
			t.Errorf("unexpected error for 43-char verifier: %v", err)
		}
	})

	t.Run("verifier exactly 128 chars (maximum)", func(t *testing.T) {
		verifier := strings.Repeat("b", 128)
		challenge := GenerateCodeChallenge(verifier)
		err := ValidateCodeVerifier(verifier, challenge)
		if err != nil {
			t.Errorf("unexpected error for 128-char verifier: %v", err)
		}
	})

	t.Run("verifier with all valid special chars", func(t *testing.T) {
		// Test verifier using all allowed special chars: - . _ ~
		verifier := "abcde-fghij.klmno_pqrst~uvwxyz0123456789ABCD"
		challenge := GenerateCodeChallenge(verifier)
		err := ValidateCodeVerifier(verifier, challenge)
		if err != nil {
			t.Errorf("unexpected error for verifier with special chars: %v", err)
		}
	})

	t.Run("constant-time comparison used", func(t *testing.T) {
		// This test verifies the function rejects a near-match.
		// While we can't directly test constant-time behavior,
		// we verify the function correctly rejects similar-but-different values.
		verifier := rfcVerifier
		challenge := GenerateCodeChallenge(verifier)
		// Flip one character in the challenge
		wrongChallenge := "X" + challenge[1:]
		err := ValidateCodeVerifier(verifier, wrongChallenge)
		if err == nil {
			t.Error("expected error for near-match challenge")
		}
	})
}

func TestValidateCodeChallenge(t *testing.T) {
	t.Run("valid challenge", func(t *testing.T) {
		err := ValidateCodeChallenge(rfcChallenge)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("challenge too short", func(t *testing.T) {
		err := ValidateCodeChallenge("tooshort")
		if err == nil {
			t.Error("expected error for short challenge")
		}
		if !strings.Contains(err.Error(), "invalid code challenge length") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("challenge too long", func(t *testing.T) {
		err := ValidateCodeChallenge(strings.Repeat("a", 44))
		if err == nil {
			t.Error("expected error for long challenge")
		}
	})

	t.Run("challenge with invalid base64url characters", func(t *testing.T) {
		// 43 chars but contains '=' (padding) and '+' (standard base64)
		err := ValidateCodeChallenge("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSst+=cM")
		if err == nil {
			t.Error("expected error for challenge with invalid chars")
		}
		if !strings.Contains(err.Error(), "non-base64url") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("generated challenge is valid", func(t *testing.T) {
		challenge := GenerateCodeChallenge(rfcVerifier)
		err := ValidateCodeChallenge(challenge)
		if err != nil {
			t.Errorf("generated challenge should be valid: %v", err)
		}
	})
}
