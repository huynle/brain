package doctor

import (
	"testing"
)

func TestCheckStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status CheckStatus
		want   string
	}{
		{"pass status", CheckStatusPass, "pass"},
		{"warn status", CheckStatusWarn, "warn"},
		{"fail status", CheckStatusFail, "fail"},
		{"skip status", CheckStatusSkip, "skip"},
		{"unknown status", CheckStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("CheckStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckStatus_Symbol(t *testing.T) {
	tests := []struct {
		name   string
		status CheckStatus
		want   string
	}{
		{"pass symbol", CheckStatusPass, "✅"},
		{"warn symbol", CheckStatusWarn, "⚠️"},
		{"fail symbol", CheckStatusFail, "❌"},
		{"skip symbol", CheckStatusSkip, "⏭"},
		{"unknown symbol", CheckStatus(99), "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.Symbol()
			if got != tt.want {
				t.Errorf("CheckStatus.Symbol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheck_Creation(t *testing.T) {
	check := Check{
		Name:    "test-check",
		Status:  CheckStatusPass,
		Message: "Test message",
		Fixable: true,
	}

	if check.Name != "test-check" {
		t.Errorf("Check.Name = %q, want %q", check.Name, "test-check")
	}
	if check.Status != CheckStatusPass {
		t.Errorf("Check.Status = %v, want %v", check.Status, CheckStatusPass)
	}
	if check.Message != "Test message" {
		t.Errorf("Check.Message = %q, want %q", check.Message, "Test message")
	}
	if !check.Fixable {
		t.Errorf("Check.Fixable = %v, want true", check.Fixable)
	}
}

func TestDoctorOptions_Defaults(t *testing.T) {
	opts := DoctorOptions{}

	if opts.Fix {
		t.Error("DoctorOptions.Fix should default to false")
	}
	if opts.Force {
		t.Error("DoctorOptions.Force should default to false")
	}
	if opts.DryRun {
		t.Error("DoctorOptions.DryRun should default to false")
	}
	if opts.Verbose {
		t.Error("DoctorOptions.Verbose should default to false")
	}
	if opts.SkipVersionCheck {
		t.Error("DoctorOptions.SkipVersionCheck should default to false")
	}
}

func TestDoctorResult_Creation(t *testing.T) {
	checks := []Check{
		{Name: "check1", Status: CheckStatusPass},
		{Name: "check2", Status: CheckStatusFail, Fixable: true},
	}

	result := DoctorResult{
		Checks:  checks,
		Summary: "Test summary",
	}

	if len(result.Checks) != 2 {
		t.Errorf("DoctorResult.Checks length = %d, want 2", len(result.Checks))
	}
	if result.Summary != "Test summary" {
		t.Errorf("DoctorResult.Summary = %q, want %q", result.Summary, "Test summary")
	}
}
