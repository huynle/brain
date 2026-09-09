package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticateOperator(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("operator-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvUsername, "deployment-owner")
	t.Setenv(EnvPasswordHash, string(hash))
	configured := NewVerifierFromEnv()
	for _, tt := range []struct {
		name               string
		verifier           *Verifier
		username, password string
		want               bool
	}{
		{"authenticated", configured, "deployment-owner", "operator-password", true},
		{"wrong password", configured, "deployment-owner", "wrong", false},
		{"wrong username", configured, "admin", "operator-password", false},
		{"admin scope is not authority", configured, "admin:*", "operator-password", false},
		{"local is not authority", configured, "local", "operator-password", false},
		{"disabled", &Verifier{}, "deployment-owner", "operator-password", false},
		{"nil verifier", nil, "deployment-owner", "operator-password", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cap, err := tt.verifier.AuthenticateOperator(tt.username, tt.password)
			if cap.Valid() != tt.want || (err == nil) != tt.want {
				t.Fatalf("capability valid=%v error=%v, want success=%v", cap.Valid(), err, tt.want)
			}
			if (DeploymentOperator{}).Valid() {
				t.Fatal("zero capability must deny")
			}
		})
	}
}
