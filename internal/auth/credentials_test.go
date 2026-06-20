package auth

import "testing"

func TestVerifier_Verify(t *testing.T) {
	hash, err := HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	v := &Verifier{username: "alice", passwordHash: hash}

	tests := []struct {
		name string
		user string
		pass string
		want bool
	}{
		{"correct", "alice", "s3cret-pw", true},
		{"wrong password", "alice", "nope", false},
		{"wrong username", "bob", "s3cret-pw", false},
		{"both wrong", "bob", "nope", false},
		{"empty password", "alice", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := v.Verify(tt.user, tt.pass); got != tt.want {
				t.Errorf("Verify(%q,%q) = %v, want %v", tt.user, tt.pass, got, tt.want)
			}
		})
	}
}

func TestVerifier_NotConfigured(t *testing.T) {
	v := &Verifier{username: "alice", passwordHash: ""}
	if v.Configured() {
		t.Fatal("Configured() = true, want false when no hash set")
	}
	if v.Verify("alice", "anything") {
		t.Error("Verify() = true, want false when not configured")
	}
}

func TestNewVerifierFromEnv_Defaults(t *testing.T) {
	t.Setenv(EnvUsername, "")
	t.Setenv(EnvPasswordHash, "")
	v := NewVerifierFromEnv()
	if v.Username() != "admin" {
		t.Errorf("default username = %q, want admin", v.Username())
	}
	if v.Configured() {
		t.Error("Configured() = true, want false with no hash")
	}
}

func TestNewVerifierFromEnv_Configured(t *testing.T) {
	hash, _ := HashPassword("pw")
	t.Setenv(EnvUsername, "operator")
	t.Setenv(EnvPasswordHash, hash)
	v := NewVerifierFromEnv()
	if v.Username() != "operator" {
		t.Errorf("username = %q, want operator", v.Username())
	}
	if !v.Configured() {
		t.Fatal("Configured() = false, want true")
	}
	if !v.Verify("operator", "pw") {
		t.Error("Verify() = false for correct credentials")
	}
}
