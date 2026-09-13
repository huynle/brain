package runner

import (
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Supervisor session-resume prompt assembly (Phase 2)
//
// Two shapes of resume prompt:
//
//   - same_session: the executor reloads the prior session, so its history is
//     already in context. The opening prompt is a short continue preamble plus
//     the supervisor-injected context. The full task body is NOT re-dumped.
//
//   - rehydrate: a fresh session is seeded with a bounded slice of the prior
//     transcript. The opening prompt carries the task assignment header, a
//     resume preamble, the (elastic, truncatable) prior transcript, the
//     authoritative brain task content, and — LAST — the supervisor-injected
//     context.
//
// Two things are authoritative and NEVER truncated: the brain task content and
// the supervisor-injected context. Only the prior transcript is elastic.
// ============================================================================

// defaultRehydrateBudgetBytes bounds the assembled prior-transcript section
// when no explicit budget is supplied (24KB ≈ 6k tokens).
const defaultRehydrateBudgetBytes = 24576

// injectedContextOpen / injectedContextClose delimit the supervisor-injected
// context block so the agent can tell where the freshest instruction begins.
const (
	injectedContextOpen  = "--- Supervisor injected context ---"
	injectedContextClose = "--- end supervisor injected context ---"
)

// transcriptSectionHeader labels the bounded prior-transcript block.
const transcriptSectionHeader = "## Prior session transcript (bounded)"

// buildInjectedContextBlock renders the delimited supervisor-injected context
// block, or "" when there is nothing to inject.
func buildInjectedContextBlock(injected string) string {
	if injected == "" {
		return ""
	}
	return injectedContextOpen + "\n" + injected + "\n" + injectedContextClose
}

// truncateTranscript bounds a transcript to budget bytes using head+tail
// truncation with a clear elision marker. When the transcript already fits, it
// is returned unchanged. The returned string never exceeds budget (assuming a
// non-degenerate budget); for pathologically small budgets it degrades to a
// bounded elision marker.
func truncateTranscript(transcript string, budget int) string {
	if budget <= 0 {
		budget = defaultRehydrateBudgetBytes
	}
	if len(transcript) <= budget {
		return transcript
	}

	// First cut with a provisional marker sized on the full elided count, then
	// recompute against the real head/tail split and trim if the marker grew a
	// digit (which would otherwise push us over budget).
	provisional := fmt.Sprintf("\n...[transcript truncated: %d bytes elided]...\n", len(transcript))
	if len(provisional) >= budget {
		return provisional[:budget]
	}

	remaining := budget - len(provisional)
	headLen := remaining / 2
	tailLen := remaining - headLen

	head := transcript[:headLen]
	tail := transcript[len(transcript)-tailLen:]

	elided := len(transcript) - headLen - tailLen
	marker := fmt.Sprintf("\n...[transcript truncated: %d bytes elided]...\n", elided)
	out := head + marker + tail
	if len(out) > budget {
		// Marker grew (more digits); trim the tail to stay within budget.
		over := len(out) - budget
		if over < len(tail) {
			tail = tail[over:]
		} else {
			tail = ""
		}
		out = head + marker + tail
	}
	return out
}

// buildRehydratePrompt assembles a bounded opening prompt for a fresh
// (rehydrate) resume: task assignment header + resume preamble + bounded prior
// context (transcript head+tail truncated to budget) + brain task content
// (never truncated) + a clearly delimited supervisor-injected-context block
// placed LAST (never truncated).
func buildRehydratePrompt(task *types.ResolvedTask, priorTranscript, injected string, budgetBytes int) string {
	if budgetBytes <= 0 {
		budgetBytes = defaultRehydrateBudgetBytes
	}

	header := buildTaskAssignmentHeader(task, true)

	preamble := "## Resume (rehydrated)\n" +
		"You were previously running this task in a session that can no longer be reloaded, " +
		"so a bounded slice of the prior conversation is provided below for context. Before continuing:\n" +
		"1. Load the brain-runner-queue skill.\n" +
		"2. Use `brain_recall` on the brain path in the header to read the authoritative task and any progress notes.\n" +
		"3. Inspect the workspace / git state for partial work.\n" +
		"4. Continue from where you left off; only restart from scratch if you cannot tell what was done.\n" +
		"5. Note in your final summary that this was a resumed task."

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	b.WriteString(preamble)

	// Prior transcript is the only elastic section. Bound the assembled
	// section (header + body) to budgetBytes.
	if priorTranscript != "" {
		sectionPrefix := "\n\n" + transcriptSectionHeader + "\n\n"
		// The invariant we care about is that the full transcript SECTION
		// (from its header to the next section) never exceeds budgetBytes.
		// Reserve the section-prefix bytes for the body budget.
		bodyBudget := budgetBytes - len(sectionPrefix)
		if bodyBudget < 0 {
			bodyBudget = 0
		}
		body := truncateTranscript(priorTranscript, bodyBudget)
		b.WriteString(sectionPrefix)
		b.WriteString(body)
	}

	// Authoritative task content — never truncated.
	taskContent := formatTaskContentForPrompt(task.Content)
	if taskContent != "" {
		b.WriteString(taskContent)
	}

	// Supervisor-injected context — LAST, never truncated.
	if block := buildInjectedContextBlock(injected); block != "" {
		b.WriteString("\n\n")
		b.WriteString(block)
	}

	return b.String()
}

// buildSameSessionResumePrompt assembles the short opening prompt for a true
// same-session resume. The prior conversation (including the task body) is
// already loaded in the reused session, so this only carries a brief continue
// preamble plus the delimited supervisor-injected context. The full task
// content is deliberately NOT re-dumped.
func buildSameSessionResumePrompt(task *types.ResolvedTask, injected string) string {
	header := buildTaskAssignmentHeader(task, true)

	preamble := "## Resume (same session)\n" +
		"You are continuing your prior session. Prior conversation history is already loaded, " +
		"so do not re-read the whole task from scratch — pick up from where you left off. " +
		"New supervisor context follows:"

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	b.WriteString(preamble)

	if block := buildInjectedContextBlock(injected); block != "" {
		b.WriteString("\n\n")
		b.WriteString(block)
	}

	return b.String()
}

// selectResumePrompt picks the opening prompt for a spawn based on the resume
// mode. sameSessionAllowed reports whether this executor can perform a true
// same-session resume (OpenCode: true; Pi: false). A same_session request on a
// non-capable executor coerces to rehydrate. An empty ResumeMode falls back to
// the legacy CommonBuildPrompt path.
func selectResumePrompt(task *types.ResolvedTask, opts SpawnOptions, sameSessionAllowed bool) string {
	switch opts.ResumeMode {
	case ResumeModeSameSession:
		if sameSessionAllowed {
			return buildSameSessionResumePrompt(task, opts.InjectedContext)
		}
		// Coerce to rehydrate on executors that cannot reuse a session.
		return buildRehydratePrompt(task, opts.PriorTranscript, opts.InjectedContext, defaultRehydrateBudgetBytes)
	case ResumeModeRehydrate:
		return buildRehydratePrompt(task, opts.PriorTranscript, opts.InjectedContext, defaultRehydrateBudgetBytes)
	default:
		return CommonBuildPrompt(task, opts.IsResume)
	}
}

// startHeadlessServerFn / createOpencodeSessionFn are indirection hooks around
// the serve-startup and session-creation steps so tests can stub them without
// spawning a real `opencode` process. Production wiring calls the real
// methods.
var startHeadlessServerFn = func(e *OpenCodeExecutor, workdir, projectID, taskID string) (int, map[string]struct{}, Process, error) {
	return e.startHeadlessServer(workdir, projectID, taskID)
}

var createOpencodeSessionFn = func(port int, title string) (string, error) {
	return createOpencodeSession(port, title)
}
