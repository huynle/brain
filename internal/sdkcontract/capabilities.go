package sdkcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

// Inactive client-side experiment for the PROPOSED discovery manifest. No route,
// public SDK method, production caller, or authority decision uses this decoder.
// Wire allocation is still required; do not mistake decoded booleans for grants.

var (
	errCapabilityUnsupported = errors.New("unsupported_server")
	errCapabilityAuth        = errors.New("capability_auth_required")
	errCapabilityUnavailable = errors.New("capability_discovery_unavailable")
	errCapabilityInvalid     = errors.New("invalid_capability_manifest")
	errCapabilityVersion     = errors.New("incompatible_contract_version")
)

type capabilityManifest struct {
	ContractVersion string          `json:"contract_version"`
	Operations      []string        `json:"operations"`
	Scripts         capabilityState `json:"scripts"`
}
type capabilityState struct {
	Compiled            bool `json:"compiled"`
	Configured          bool `json:"configured"`
	DeploymentAvailable bool `json:"deployment_available"`
	CallerAuthorized    bool `json:"caller_authorized"`
	Available           bool `json:"available"`
}

var capabilityOperation = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}\.[a-z][a-zA-Z0-9]{0,63}$`)

func decodeCapabilityManifest(status int, body []byte, expected string) (capabilityManifest, error) {
	bad := func(err error) (capabilityManifest, error) { return capabilityManifest{}, err }
	switch status {
	case 404, 501:
		return bad(errCapabilityUnsupported)
	case 401, 403:
		return bad(errCapabilityAuth)
	case 200:
	default:
		return bad(errCapabilityUnavailable)
	}
	if len(body) == 0 || len(body) > 65536 || !utf8.Valid(body) || expected == "" || len(expected) > 64 || !utf8.ValidString(expected) {
		return bad(errCapabilityInvalid)
	}
	fields, ok := capabilityObject(body, 3)
	if !ok || fields["contract_version"] == nil || fields["operations"] == nil || fields["scripts"] == nil {
		return bad(errCapabilityInvalid)
	}
	var result capabilityManifest
	if json.Unmarshal(fields["contract_version"], &result.ContractVersion) != nil || len(result.ContractVersion) == 0 || len(result.ContractVersion) > 64 {
		return bad(errCapabilityInvalid)
	}
	if json.Unmarshal(fields["operations"], &result.Operations) != nil || result.Operations == nil || len(result.Operations) > 10000 {
		return bad(errCapabilityInvalid)
	}
	seen := make(map[string]bool, len(result.Operations))
	for _, op := range result.Operations {
		if !capabilityOperation.MatchString(op) || seen[op] {
			return bad(errCapabilityInvalid)
		}
		seen[op] = true
	}
	flags, ok := capabilityObject(fields["scripts"], 5)
	if !ok {
		return bad(errCapabilityInvalid)
	}
	for _, key := range []string{"compiled", "configured", "deployment_available", "caller_authorized", "available"} {
		literal := string(bytes.TrimSpace(flags[key]))
		if literal != "true" && literal != "false" {
			return bad(errCapabilityInvalid)
		}
	}
	if json.Unmarshal(fields["scripts"], &result.Scripts) != nil {
		return bad(errCapabilityInvalid)
	}
	s := result.Scripts
	if s.Available != (s.Compiled && s.Configured && s.DeploymentAvailable && s.CallerAuthorized) {
		return bad(errCapabilityInvalid)
	}
	if result.ContractVersion != expected {
		return bad(errCapabilityVersion)
	}
	return result, nil
}

// Reject duplicate decoded field names before ordinary struct decoding can pick
// a last value. Both manifest objects have exact fields; no resource metadata.
func capabilityObject(body []byte, count int) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(body))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage, count)
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || fields[name] != nil || len(fields) >= count {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		fields[name] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(fields) != count {
		return nil, false
	}
	_, err = d.Token()
	return fields, err == io.EOF
}
