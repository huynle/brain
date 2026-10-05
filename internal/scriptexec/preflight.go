package scriptexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrPreflight = errors.New("invalid script request or policy")

// RequestPolicy is server-owned input to a pure validation prototype. It is
// not an authorization grant, runtime configuration, or executable profile.
type RequestPolicy struct {
	ContractVersion                  string
	MaxSourceBytes                   int
	DefaultTimeoutMS, MaxTimeoutMS   int
	DefaultOperations, MaxOperations int
}

type ScriptRequest struct {
	Script        string
	DryRun        bool
	TimeoutMS     int
	MaxOperations int
}

type PreparedRequest struct {
	SourceSHA256  string
	Fingerprint   string
	TimeoutMS     int
	MaxOperations int
	DryRun        bool
}

// PrepareRequest validates only source encoding/size and requested limits. It
// does not compile, authorize, reserve quota, validate operations or execute.
// The fingerprint is not an idempotency receipt: a future owner must scope it
// by verified tenant/principal/endpoint and authorize every replay/output.
func PrepareRequest(request ScriptRequest, policy RequestPolicy) (PreparedRequest, error) {
	if policy.ContractVersion == "" || policy.MaxSourceBytes <= 0 || policy.DefaultTimeoutMS <= 0 || policy.MaxTimeoutMS < policy.DefaultTimeoutMS || policy.DefaultOperations <= 0 || policy.MaxOperations < policy.DefaultOperations {
		return PreparedRequest{}, ErrPreflight
	}
	if len(request.Script) > policy.MaxSourceBytes || !utf8.ValidString(request.Script) || strings.TrimSpace(request.Script) == "" || request.TimeoutMS < 0 || request.TimeoutMS > policy.MaxTimeoutMS || request.MaxOperations < 0 || request.MaxOperations > policy.MaxOperations {
		return PreparedRequest{}, ErrPreflight
	}
	if request.TimeoutMS == 0 {
		request.TimeoutMS = policy.DefaultTimeoutMS
	}
	if request.MaxOperations == 0 {
		request.MaxOperations = policy.DefaultOperations
	}
	source := sha256.Sum256([]byte(request.Script))
	prepared := PreparedRequest{SourceSHA256: hex.EncodeToString(source[:]), TimeoutMS: request.TimeoutMS, MaxOperations: request.MaxOperations, DryRun: request.DryRun}
	// Named, fixed fields prevent concatenation ambiguity; source bytes are not
	// retained in the prepared result or an error. Keep v1 domain separation.
	data, err := json.Marshal(struct {
		Format     string `json:"format"`
		Contract   string `json:"contract"`
		Source     string `json:"source_sha256"`
		TimeoutMS  int    `json:"timeout_ms"`
		Operations int    `json:"max_operations"`
		DryRun     bool   `json:"dry_run"`
	}{"brain-script-request-v1", policy.ContractVersion, prepared.SourceSHA256, prepared.TimeoutMS, prepared.MaxOperations, prepared.DryRun})
	if err != nil {
		return PreparedRequest{}, ErrPreflight
	}
	fingerprint := sha256.Sum256(data)
	prepared.Fingerprint = hex.EncodeToString(fingerprint[:])
	return prepared, nil
}
