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

func TestDiffRestartFieldsIncludesAssistantJobs(t *testing.T) {
	prev := config.DefaultConfig()
	next := prev
	enabled := true
	next.Server.Assistant.Jobs.Enabled = &enabled
	next.Server.Assistant.Jobs.MaxParallel = 5

	got := diffRestartFields(&prev, &next)
	want := map[string]bool{
		"server.assistant.jobs.enabled":      false,
		"server.assistant.jobs.max_parallel": false,
	}
	for _, field := range got {
		if _, ok := want[field]; ok {
			want[field] = true
		}
	}
	for field, found := range want {
		if !found {
			t.Errorf("%s change should require restart; got %v", field, got)
		}
	}
}
