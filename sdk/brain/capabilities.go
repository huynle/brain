package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

const ContractVersion = "1.0.0"

type capabilityBody []byte

// Capabilities negotiates this SDK's contract on its immutable authenticated
// binding. The manifest describes support, not resource access or a grant.
// No caching, alternate origin, anonymous retry or old-server fallback is used.
func (c *Client) Capabilities(ctx context.Context) (*CapabilityManifest, error) {
	bounded := *c
	bounded.limit = min(c.limit, 65536)
	var body capabilityBody
	if err := bounded.request(ctx, "GET", "/capabilities", nil, nil, RequestOptions{}, &body); err != nil {
		var e *Error
		if !errors.As(err, &e) {
			return nil, err
		}
		code := "capability_discovery_unavailable"
		switch {
		case e.Status == 404 || e.Status == 501:
			code = "unsupported_server"
		case e.Status == 401 || e.Status == 403:
			code = "capability_auth_required"
		case e.Code == "response_too_large":
			code = "invalid_capability_manifest"
		}
		return nil, &Error{Code: code, Status: e.Status, RequestID: e.RequestID}
	}
	return decodeCapabilities(body)
}

var capabilityOperation = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}\.[a-z][a-zA-Z0-9]{0,63}$`)

func decodeCapabilities(body []byte) (*CapabilityManifest, error) {
	bad := func() (*CapabilityManifest, error) { return nil, &Error{Code: "invalid_capability_manifest"} }
	if len(body) == 0 || len(body) > 65536 || !utf8.Valid(body) {
		return bad()
	}
	f, ok := capabilityObject(body, 3)
	if !ok || f["contract_version"] == nil || f["operations"] == nil || f["scripts"] == nil {
		return bad()
	}
	var m CapabilityManifest
	if json.Unmarshal(f["contract_version"], &m.ContractVersion) != nil || len(m.ContractVersion) == 0 || len(m.ContractVersion) > 64 {
		return bad()
	}
	if json.Unmarshal(f["operations"], &m.Operations) != nil || m.Operations == nil || len(m.Operations) > 10000 {
		return bad()
	}
	seen := map[string]bool{}
	for _, op := range m.Operations {
		if !capabilityOperation.MatchString(op) || seen[op] {
			return bad()
		}
		seen[op] = true
	}
	flags, ok := capabilityObject(f["scripts"], 5)
	if !ok {
		return bad()
	}
	for _, key := range []string{"compiled", "configured", "deployment_available", "caller_authorized", "available"} {
		v := string(bytes.TrimSpace(flags[key]))
		if v != "true" && v != "false" {
			return bad()
		}
	}
	if json.Unmarshal(f["scripts"], &m.Scripts) != nil {
		return bad()
	}
	s := m.Scripts
	if s.Available != (s.Compiled && s.Configured && s.DeploymentAvailable && s.CallerAuthorized) {
		return bad()
	}
	if m.ContractVersion != ContractVersion {
		return nil, &Error{Code: "incompatible_contract_version"}
	}
	return &m, nil
}

func capabilityObject(body []byte, count int) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(body))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	f := make(map[string]json.RawMessage, count)
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || f[name] != nil || len(f) >= count {
			return nil, false
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, false
		}
		f[name] = v
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(f) != count {
		return nil, false
	}
	_, err = d.Token()
	return f, err == io.EOF
}
