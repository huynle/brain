package scriptexec

import (
	"bytes"
	"errors"
	"unicode/utf8"
)

var (
	errPlanShape       = errors.New("invalid plan shape")
	errPlanDependency  = errors.New("dry_run_dependency_unsupported")
	errOutcomeSequence = errors.New("invalid outcome sequence")
)

// Descriptive, inactive preparation only. No broker, service, storage or authority.
type plannedStep struct {
	Index                   int
	Call                    OperationCall
	ExpectedRevision        string
	ProvisionalDependencies []int
}

type outcomeSummary struct{ planned, committed, failed, unknown int }

// validatePlanShape only checks ordered syntax and argument byte budgets. It
// does NOT attest registry membership, target existence, current revision,
// permissions, source provenance, publication or side-effect-free preflight.
// No provisional dependency is supported until service owners allocate it.
func validatePlanShape(plan []plannedStep, limits ProtocolLimits) error {
	budget, err := NewProtocolSession(limits)
	if err != nil || len(plan) > limits.MaxOperations {
		return errPlanShape
	}
	dependent := false
	for i, step := range plan {
		args := bytes.TrimSpace(step.Call.Arguments)
		if step.Index != i+1 || !operationName.MatchString(step.Call.Operation) || len(args) == 0 || args[0] != '{' || !budget.consume(step.Call.Arguments) || len(step.ExpectedRevision) > 256 || !utf8.ValidString(step.ExpectedRevision) {
			return errPlanShape
		}
		if len(step.ProvisionalDependencies) > i {
			return errPlanShape
		}
		seen := make(map[int]bool, len(step.ProvisionalDependencies))
		for _, index := range step.ProvisionalDependencies {
			if index < 1 || index >= step.Index || seen[index] {
				return errPlanShape
			}
			seen[index] = true
			dependent = true
		}
	}
	if dependent {
		return errPlanDependency
	}
	return nil
}

// summarizeMutationOutcomes validates descriptive MUTATION outcomes only, not
// a durable journal, state transition or receipt. Reads are not represented in
// this sequence. "committed" remains an unverified caller claim; this helper
// must never justify replay, retry, output release, or audit persistence.
func summarizeMutationOutcomes(dryRun bool, states []string, maximum int) (outcomeSummary, error) {
	bad := func() (outcomeSummary, error) { return outcomeSummary{}, errOutcomeSequence }
	if maximum < 1 || maximum > 10000 || len(states) > maximum {
		return bad()
	}
	var summary outcomeSummary
	for i, state := range states {
		switch state {
		case "planned":
			if !dryRun {
				return bad()
			}
			summary.planned++
		case "committed":
			if dryRun {
				return bad()
			}
			summary.committed++
		case "failed":
			if i != len(states)-1 {
				return bad()
			}
			summary.failed++
		case "outcome_unknown":
			if dryRun || i != len(states)-1 {
				return bad()
			}
			summary.unknown++
		default:
			return bad()
		}
	}
	return summary, nil
}
