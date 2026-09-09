// Package tenant defines opaque tenant identifiers, not authorization grants.
// A zero or invalid ID must MATCH NOTHING, never mean "no constraint". Consumers
// must reject it or return no results; never omit a tenant filter because it is empty.
package tenant

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
)

// ID is an opaque identifier. Its zero value is invalid and must match nothing,
// never mean an unconstrained scope. Parsing does not establish ownership.
type ID struct{ v string }

// LocalID names the reserved single-tenant bootstrap scope.
const LocalID = "local"

// Local is the reserved single-tenant bootstrap scope. Do not reassign it.
// Its exact canonical spelling round-trips through JSON/SQL; serialization syntax
// is not authorization. Parse still reserves it against public provisioning.
var Local = ID{v: LocalID}

var syntax = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// String returns the exact identifier, or an empty string for the zero value.
func (id ID) String() string { return id.v }

// Valid includes the bootstrap sentinel; it does not imply authorization.
func (id ID) Valid() bool {
	if id.v == LocalID {
		return true
	}
	_, err := Parse(id.v)
	return err == nil
}

// Parse validates syntax and reserved names, never normalizes input and never
// authorizes access. Production call sites require explicit policy review.
func Parse(s string) (ID, error) {
	if !syntax.MatchString(s) {
		return ID{}, fmt.Errorf("invalid tenant ID %q", s)
	}
	switch s {
	case LocalID, "system", "tenants", "global", "projects", "attachments", "api", "mcp", "health", "admin", "brain-data":
		return ID{}, fmt.Errorf("reserved tenant ID %q", s)
	}
	return ID{v: s}, nil
}

// MustParse is for tests and reviewed bootstrap constants only, never requests.
func MustParse(s string) ID {
	id, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

// decodeCanonical validates serialized IDs, including the exact bootstrap scope.
// It does not authorize access or relax provisioning reservations in Parse.
func decodeCanonical(s string) (ID, error) {
	if s == LocalID {
		return ID{v: LocalID}, nil
	}
	return Parse(s)
}

// MarshalJSON accepts canonical IDs, including local, and rejects invalid IDs.
func (id ID) MarshalJSON() ([]byte, error) {
	parsed, err := decodeCanonical(id.v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(parsed.v)
}

// UnmarshalJSON accepts canonical ID strings, including local, not access grants.
// Errors clear the receiver when
// this method is invoked; callers must still honor errors from encoding/json
// itself (which can reject malformed JSON before invoking this method).
func (id *ID) UnmarshalJSON(b []byte) error {
	if id == nil {
		return fmt.Errorf("nil tenant ID receiver")
	}
	*id = ID{}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := decodeCanonical(s)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// Value accepts canonical IDs, including local, and rejects zero or invalid IDs.
func (id ID) Value() (driver.Value, error) {
	parsed, err := decodeCanonical(id.v)
	if err != nil {
		return nil, err
	}
	return parsed.v, nil
}

// Scan accepts SQL text only, rejecting NULL and clearing the receiver on error.
func (id *ID) Scan(src any) error {
	if id == nil {
		return fmt.Errorf("nil tenant ID receiver")
	}
	*id = ID{}
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan tenant ID from %T", src)
	}
	parsed, err := decodeCanonical(s)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
