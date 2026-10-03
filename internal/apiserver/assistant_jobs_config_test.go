package apiserver

import "testing"

func TestAssistantJobsEnabledUsesLegacyEnvOnlyWhenConfigIsUnset(t *testing.T) {
	truth, falsity := true, false
	tests := []struct {
		name   string
		value  *bool
		legacy string
		want   bool
	}{
		{name: "legacy enabled", legacy: "true", want: true},
		{name: "unset disabled", legacy: "false", want: false},
		{name: "explicit config enables", value: &truth, want: true},
		{name: "explicit config disables despite legacy env", value: &falsity, legacy: "true", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := assistantJobsEnabled(tt.value, tt.legacy); got != tt.want {
				t.Fatalf("assistantJobsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
