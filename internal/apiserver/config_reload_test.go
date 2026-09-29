package apiserver

import (
	"testing"

	"github.com/huynle/brain-api/internal/config"
)

func TestDiffRestartFieldsIncludesPasswordSessionTTL(t *testing.T) {
	prev := config.DefaultConfig()
	next := prev
	next.Server.PasswordSessionTTLDays = 0

	for _, field := range diffRestartFields(&prev, &next) {
		if field == "server.password_session_ttl_days" {
			return
		}
	}
	t.Fatal("password session TTL change should require restart")
}
