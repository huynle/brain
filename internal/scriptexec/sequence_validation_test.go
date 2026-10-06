package scriptexec

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func planFixture() []plannedStep {
	return []plannedStep{
		{Index: 1, Call: OperationCall{"entries.create", json.RawMessage(`{"request":{"content":"fixture"}}`)}},
		{Index: 2, Call: OperationCall{"entries.update", json.RawMessage(`{"id":"existing","request":{"content":"fixture"}}`)}, ExpectedRevision: "revision"},
	}
}

func TestPlanShapeOrderingAndDependencies(t *testing.T) {
	limits := ProtocolLimits{2, 1024, 2048}
	if err := validatePlanShape(planFixture(), limits); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func([]plannedStep) []plannedStep
		want   error
	}{
		{"zero index", func(p []plannedStep) []plannedStep { p[0].Index = 0; return p }, errPlanShape},
		{"gap", func(p []plannedStep) []plannedStep { p[1].Index = 3; return p }, errPlanShape},
		{"duplicate", func(p []plannedStep) []plannedStep { p[1].Index = 1; return p }, errPlanShape},
		{"malformed operation", func(p []plannedStep) []plannedStep { p[0].Call.Operation = "raw.http.escape"; return p }, errPlanShape},
		{"nonobject", func(p []plannedStep) []plannedStep { p[0].Call.Arguments = json.RawMessage(`[]`); return p }, errPlanShape},
		{"duplicate keys", func(p []plannedStep) []plannedStep {
			p[0].Call.Arguments = json.RawMessage(`{"id":1,"\u0069d":2}`)
			return p
		}, errPlanShape},
		{"trailing JSON", func(p []plannedStep) []plannedStep { p[0].Call.Arguments = json.RawMessage(`{} {}`); return p }, errPlanShape},
		{"invalid UTF8", func(p []plannedStep) []plannedStep {
			p[0].Call.Arguments = []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}
			return p
		}, errPlanShape},
		{"long revision", func(p []plannedStep) []plannedStep { p[0].ExpectedRevision = strings.Repeat("x", 257); return p }, errPlanShape},
		{"self dependency", func(p []plannedStep) []plannedStep { p[1].ProvisionalDependencies = []int{2}; return p }, errPlanShape},
		{"forward dependency", func(p []plannedStep) []plannedStep { p[0].ProvisionalDependencies = []int{2}; return p }, errPlanShape},
		{"missing dependency", func(p []plannedStep) []plannedStep { p[1].ProvisionalDependencies = []int{0}; return p }, errPlanShape},
		{"repeated dependency", func(p []plannedStep) []plannedStep { p[1].ProvisionalDependencies = []int{1, 1}; return p }, errPlanShape},
		{"valid reference remains unsupported", func(p []plannedStep) []plannedStep { p[1].ProvisionalDependencies = []int{1}; return p }, errPlanDependency},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.mutate(planFixture())
			before, _ := json.Marshal(p)
			err := validatePlanShape(p, limits)
			after, _ := json.Marshal(p)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if string(before) != string(after) {
				t.Fatal("validator mutated plan")
			}
		})
	}
}

func TestPlanShapeBudgets(t *testing.T) {
	p := planFixture()
	total := 0
	maxPayload := 0
	for _, step := range p {
		n := len(step.Call.Arguments)
		total += n
		maxPayload = max(maxPayload, n)
	}
	if err := validatePlanShape(p, ProtocolLimits{2, maxPayload, total}); err != nil {
		t.Fatal(err)
	}
	for _, limits := range []ProtocolLimits{{1, maxPayload, total}, {2, maxPayload - 1, total}, {2, maxPayload, total - 1}, {0, 1, 1}, {10001, 1, 1}, {2, MaxFrameBytes + 1, MaxFrameBytes + 1}, {2, 1, 17 * MaxFrameBytes}} {
		if err := validatePlanShape(p, limits); !errors.Is(err, errPlanShape) {
			t.Fatalf("budget %+v got %v", limits, err)
		}
	}
	if err := validatePlanShape(nil, ProtocolLimits{1, 1, 1}); err != nil {
		t.Fatal("empty plan must be allowed for read-only script")
	}
}

func TestPlanShapeDoesNotAttestAuthorityOrRegistry(t *testing.T) {
	p := planFixture()[:1]
	p[0].Call.Operation = "unknown.operation"
	p[0].ExpectedRevision = "unverified-revision"
	if err := validatePlanShape(p, ProtocolLimits{1, 1024, 1024}); err != nil {
		t.Fatal("pure syntax is not an operation registry or revision checker")
	}
}

func FuzzPlanShape(f *testing.F) {
	f.Add([]byte(`{"request":{"content":"fixture"}}`))
	f.Add([]byte(`{"id":1,"id":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 8192 {
			return
		}
		before := string(data)
		p := planFixture()[:1]
		p[0].Call.Arguments = data
		err := validatePlanShape(p, ProtocolLimits{1, 4096, 4096})
		if string(data) != before {
			t.Fatal("validator mutated input")
		}
		if err == nil && (!json.Valid(data) || len(data) > 4096) {
			t.Fatal("accepted malformed or over-budget plan")
		}
	})
}

func TestMutationOutcomeSequenceStopsWithoutInventingReceipts(t *testing.T) {
	for _, tc := range []struct {
		dry    bool
		states []string
		want   outcomeSummary
	}{
		{false, nil, outcomeSummary{}},
		{false, []string{"committed", "committed"}, outcomeSummary{committed: 2}},
		{false, []string{"committed", "failed"}, outcomeSummary{committed: 1, failed: 1}},
		{false, []string{"committed", "outcome_unknown"}, outcomeSummary{committed: 1, unknown: 1}},
		{true, []string{"planned", "planned", "failed"}, outcomeSummary{planned: 2, failed: 1}},
	} {
		got, err := summarizeMutationOutcomes(tc.dry, tc.states, 3)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("dry=%v states=%v got=%+v err=%v", tc.dry, tc.states, got, err)
		}
	}
	for _, tc := range []struct {
		dry    bool
		states []string
		max    int
	}{
		{false, []string{"failed", "committed"}, 3}, {false, []string{"outcome_unknown", "failed"}, 3},
		{false, []string{"failed", "failed"}, 3}, {false, []string{"planned"}, 3},
		{true, []string{"committed"}, 3}, {true, []string{"outcome_unknown"}, 3},
		{false, []string{"private error content"}, 3}, {false, []string{""}, 3},
		{false, []string{"committed", "committed"}, 1}, {false, nil, 0}, {false, nil, 10001},
	} {
		got, err := summarizeMutationOutcomes(tc.dry, tc.states, tc.max)
		if !errors.Is(err, errOutcomeSequence) || got != (outcomeSummary{}) {
			t.Fatalf("dry=%v states=%v: got=%+v err=%v", tc.dry, tc.states, got, err)
		}
	}
}
