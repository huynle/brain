package runner

import "testing"

// TestPiExecutor_CanResumeSession asserts Pi can never same-session resume and
// always reports the pi-specific reason.
func TestPiExecutor_CanResumeSession(t *testing.T) {
	e := NewPiExecutor(RunnerConfig{})

	for _, sessionID := range []string{"", "ses_abc123", "anything"} {
		cap := e.CanResumeSession(sessionID)
		if cap.SameSession {
			t.Fatalf("PiExecutor.CanResumeSession(%q).SameSession = true, want false", sessionID)
		}
		if cap.Reason != "pi has no session continuation" {
			t.Fatalf("PiExecutor.CanResumeSession(%q).Reason = %q, want %q",
				sessionID, cap.Reason, "pi has no session continuation")
		}
	}
}

// TestOpenCodeExecutor_CanResumeSession_EmptyID asserts an empty session id is
// rejected with the no-prior-session reason before any disk probe.
func TestOpenCodeExecutor_CanResumeSession_EmptyID(t *testing.T) {
	e := NewExecutor(RunnerConfig{})

	cap := e.CanResumeSession("")
	if cap.SameSession {
		t.Fatalf("CanResumeSession(\"\").SameSession = true, want false")
	}
	if cap.Reason != "no prior session id" {
		t.Fatalf("CanResumeSession(\"\").Reason = %q, want %q", cap.Reason, "no prior session id")
	}
}

// TestOpenCodeExecutor_CanResumeSession_NotFound asserts that a session id with
// no history on disk (the normal case in a test env with no opencode.db) is
// reported as not resumable, not a same-session candidate.
func TestOpenCodeExecutor_CanResumeSession_NotFound(t *testing.T) {
	e := NewExecutor(RunnerConfig{})

	cap := e.CanResumeSession("ses_does_not_exist_phase1_test")
	if cap.SameSession {
		t.Fatalf("CanResumeSession(unknown).SameSession = true, want false")
	}
	if cap.Reason != "session history not found on disk" {
		t.Fatalf("CanResumeSession(unknown).Reason = %q, want %q",
			cap.Reason, "session history not found on disk")
	}
}

// TestSpawnOptions_ResumeFields is a trivial compile+set check that the Phase 1
// plumbing fields exist and carry values through the struct.
func TestSpawnOptions_ResumeFields(t *testing.T) {
	opts := SpawnOptions{
		ResumeSessionID: "ses_abc",
		ResumeMode:      ResumeModeSameSession,
		InjectedContext: "supervisor context",
		PriorTranscript: "[]",
	}
	if opts.ResumeSessionID != "ses_abc" {
		t.Fatalf("ResumeSessionID = %q, want %q", opts.ResumeSessionID, "ses_abc")
	}
	if opts.ResumeMode != ResumeModeSameSession {
		t.Fatalf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeSameSession)
	}
	if ResumeModeRehydrate != "rehydrate" {
		t.Fatalf("ResumeModeRehydrate = %q, want %q", ResumeModeRehydrate, "rehydrate")
	}
	if opts.InjectedContext != "supervisor context" {
		t.Fatalf("InjectedContext = %q, want %q", opts.InjectedContext, "supervisor context")
	}
	if opts.PriorTranscript != "[]" {
		t.Fatalf("PriorTranscript = %q, want %q", opts.PriorTranscript, "[]")
	}
}
