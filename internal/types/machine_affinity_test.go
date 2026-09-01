package types

import "testing"

func TestResolveMachineAffinity(t *testing.T) {
	tests := []struct {
		name            string
		affinity        string
		originMachineID string
		want            string
	}{
		{
			// The headline default: stamping an origin is enough to get
			// local-runner preference, with no configuration.
			name:     "unset with origin defaults to preferred",
			affinity: "", originMachineID: "machine_a", want: MachineAffinityPreferred,
		},
		{
			// Nothing to prefer, so nothing changes for pre-existing tasks.
			name:     "unset without origin defaults to none",
			affinity: "", originMachineID: "", want: MachineAffinityNone,
		},
		{name: "explicit local is kept", affinity: MachineAffinityLocal, originMachineID: "machine_a", want: MachineAffinityLocal},
		{name: "explicit none is kept", affinity: MachineAffinityNone, originMachineID: "machine_a", want: MachineAffinityNone},
		{name: "explicit preferred is kept", affinity: MachineAffinityPreferred, originMachineID: "machine_a", want: MachineAffinityPreferred},
		{
			// A typo must never resolve to something STRICTER than the
			// author asked for — that would strand the task.
			name:     "unknown value degrades to none",
			affinity: "lokal", originMachineID: "machine_a", want: MachineAffinityNone,
		},
		{
			// "local" with no origin still reports local; the scheduler
			// refuses it explicitly rather than this function guessing.
			name:     "local without origin still reports local",
			affinity: MachineAffinityLocal, originMachineID: "", want: MachineAffinityLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveMachineAffinity(tt.affinity, tt.originMachineID); got != tt.want {
				t.Errorf("ResolveMachineAffinity(%q, %q) = %q, want %q",
					tt.affinity, tt.originMachineID, got, tt.want)
			}
		})
	}
}

func TestIsValidMachineAffinity(t *testing.T) {
	for _, v := range append([]string{""}, MachineAffinities...) {
		if !IsValidMachineAffinity(v) {
			t.Errorf("IsValidMachineAffinity(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"lokal", "LOCAL", "strict", "soft"} {
		if IsValidMachineAffinity(v) {
			t.Errorf("IsValidMachineAffinity(%q) = true, want false", v)
		}
	}
}
