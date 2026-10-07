package sdkcontract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const capabilityFixture = `{"contract_version":"0.1.0","operations":["health.get","entries.get"],"scripts":{"compiled":false,"configured":false,"deployment_available":false,"caller_authorized":false,"available":false}}`

func TestCapabilityDecoderAvailabilityConjunction(t *testing.T) {
	for bits := 0; bits < 32; bits++ {
		state := capabilityState{bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0, bits&16 != 0}
		in := capabilityManifest{"0.1.0", []string{"health.get", "entries.get"}, state}
		body, _ := json.Marshal(in)
		got, err := decodeCapabilityManifest(200, body, "0.1.0")
		consistent := state.Available == (state.Compiled && state.Configured && state.DeploymentAvailable && state.CallerAuthorized)
		if !consistent {
			if !errors.Is(err, errCapabilityInvalid) {
				t.Fatalf("bits%d accepted inconsistent availability: %v", bits, err)
			}
			continue
		}
		if err != nil || got.ContractVersion != in.ContractVersion || len(got.Operations) != 2 || got.Scripts != state {
			t.Fatalf("bits%d got=%+v err=%v", bits, got, err)
		}
	}
}

func TestCapabilityDecoderRefusesOldUnauthorizedAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		status        int
		body, version string
		want          error
	}{
		{404, `private diagnostic`, "0.1.0", errCapabilityUnsupported}, {501, `private diagnostic`, "0.1.0", errCapabilityUnsupported},
		{401, capabilityFixture, "0.1.0", errCapabilityAuth}, {403, capabilityFixture, "0.1.0", errCapabilityAuth},
		{500, capabilityFixture, "0.1.0", errCapabilityUnavailable}, {204, "", "0.1.0", errCapabilityUnavailable},
		{200, capabilityFixture, "2.0.0", errCapabilityVersion}, {200, capabilityFixture, "", errCapabilityInvalid},
		{200, `{}`, "0.1.0", errCapabilityInvalid}, {200, `null`, "0.1.0", errCapabilityInvalid},
		{200, capabilityFixture + `{}`, "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"contract_version":"0.1.0"`, `"contract_version":"0.1.0","contract_version":"0.1.0"`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"compiled":false`, `"compiled":false,"\u0063ompiled":false`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"compiled":false,`, "", 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"compiled":false`, `"compiled":null`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"compiled":false`, `"compiled":"false"`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"entries.get"`, `"health.get"`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"entries.get"`, `"arbitrary HTTP"`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"operations":["health.get","entries.get"]`, `"operations":null`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Replace(capabilityFixture, `"contract_version"`, `"resources":[],"contract_version"`, 1), "0.1.0", errCapabilityInvalid},
		{200, strings.Repeat(" ", 65537) + capabilityFixture, "0.1.0", errCapabilityInvalid},
		{200, string([]byte{0xff}), "0.1.0", errCapabilityInvalid},
	} {
		got, err := decodeCapabilityManifest(tc.status, []byte(tc.body), tc.version)
		if !errors.Is(err, tc.want) || got.ContractVersion != "" || got.Operations != nil {
			t.Fatalf("status%d got=%+v err=%v want=%v", tc.status, got, err, tc.want)
		}
	}
}

func FuzzCapabilityDecoder(f *testing.F) {
	f.Add([]byte(capabilityFixture))
	f.Add([]byte(`{"contract_version":"0.1.0","operations":[],"scripts":{}}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 70000 {
			return
		}
		before := string(body)
		got, err := decodeCapabilityManifest(200, body, "0.1.0")
		if string(body) != before {
			t.Fatal("decoder modified source")
		}
		if err == nil && (got.ContractVersion != "0.1.0" || !json.Valid(body) || len(body) > 65536) {
			t.Fatal("accepted incompatible or unbounded manifest")
		}
	})
}
