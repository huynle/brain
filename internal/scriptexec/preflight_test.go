package scriptexec

import (
	"strings"
	"testing"
)

func testRequestPolicy() RequestPolicy {
	return RequestPolicy{ContractVersion: "v1", MaxSourceBytes: 1024, DefaultTimeoutMS: 1000, MaxTimeoutMS: 5000, DefaultOperations: 10, MaxOperations: 100}
}

func TestPrepareRequestNormalizesWithoutExecuting(t *testing.T) {
	policy := testRequestPolicy()
	request := ScriptRequest{Script: "return 42", DryRun: true}
	got, err := PrepareRequest(request, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeoutMS != 1000 || got.MaxOperations != 10 || !got.DryRun || len(got.SourceSHA256) != 64 || len(got.Fingerprint) != 64 {
		t.Fatalf("incomplete normalized request: %+v", got)
	}
	explicit := request
	explicit.TimeoutMS = 1000
	explicit.MaxOperations = 10
	again, err := PrepareRequest(explicit, policy)
	if err != nil || again.Fingerprint != got.Fingerprint {
		t.Fatalf("normalized defaults differ: %+v %v", again, err)
	}
	for name, other := range map[string]ScriptRequest{"source": {Script: "return 43", DryRun: true}, "mode": {Script: "return 42"}, "timeout": {Script: "return 42", DryRun: true, TimeoutMS: 999}, "operations": {Script: "return 42", DryRun: true, MaxOperations: 9}} {
		different, err := PrepareRequest(other, policy)
		if err != nil || different.Fingerprint == got.Fingerprint {
			t.Errorf("fingerprint failed to bind %s: %+v %v", name, different, err)
		}
	}
	policy.ContractVersion = "v2"
	different, err := PrepareRequest(request, policy)
	if err != nil || different.Fingerprint == got.Fingerprint {
		t.Fatal("fingerprint omitted contract version")
	}
}

func TestPrepareRequestRejectsInvalidRequestsAndPolicies(t *testing.T) {
	policy := testRequestPolicy()
	for name, r := range map[string]ScriptRequest{"empty": {}, "whitespace": {Script: " \n\t"}, "invalid_utf8": {Script: string([]byte{0xff})}, "source_bytes": {Script: strings.Repeat("é", 513)}, "negative_timeout": {Script: "0", TimeoutMS: -1}, "excess_timeout": {Script: "0", TimeoutMS: 5001}, "negative_operations": {Script: "0", MaxOperations: -1}, "excess_operations": {Script: "0", MaxOperations: 101}} {
		t.Run(name, func(t *testing.T) {
			if out, err := PrepareRequest(r, policy); err == nil || out != (PreparedRequest{}) {
				t.Fatalf("invalid request accepted/partial result: %+v %v", out, err)
			}
		})
	}
	for _, change := range []func(*RequestPolicy){func(p *RequestPolicy) { p.ContractVersion = "" }, func(p *RequestPolicy) { p.MaxSourceBytes = 0 }, func(p *RequestPolicy) { p.DefaultTimeoutMS = 0 }, func(p *RequestPolicy) { p.MaxTimeoutMS = 999 }, func(p *RequestPolicy) { p.DefaultOperations = 0 }, func(p *RequestPolicy) { p.MaxOperations = 9 }} {
		p := policy
		change(&p)
		if _, err := PrepareRequest(ScriptRequest{Script: "0"}, p); err == nil {
			t.Errorf("invalid policy accepted: %+v", p)
		}
	}
	if _, err := PrepareRequest(ScriptRequest{Script: strings.Repeat("é", 512), TimeoutMS: 5000, MaxOperations: 100}, policy); err != nil {
		t.Fatalf("exact bounds refused: %v", err)
	}
}
