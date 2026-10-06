package brain

import (
	"strings"
	"testing"
)

// Error() carries only the stable machine code and HTTP status: never the
// server message, request ID or field details (redaction stays default).
func TestErrorFormatIncludesStableCodeOnly(t *testing.T) {
	cases := []struct {
		err  *Error
		want string
	}{
		{&Error{Code: "invalid_configuration"}, "brain: invalid_configuration"},
		{&Error{Code: "redirect_refused"}, "brain: redirect_refused"},
		{&Error{Code: "not_found", Status: 404, Message: "SECRET-BODY", RequestID: "req-SECRET", Details: []FieldViolation{{"f", "SECRET-FIELD"}}}, "brain: not_found (HTTP 404)"},
		// Not a machine code (public struct): never echo it.
		{&Error{Code: "Bad Code SECRET\n", Status: 500}, "brain: request failed (HTTP 500)"},
		{&Error{Status: 502}, "brain: request failed (HTTP 502)"},
		{&Error{}, "brain: request failed"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want || strings.Contains(got, "SECRET") {
			t.Errorf("Error()=%q want %q", got, c.want)
		}
	}
	if _, err := New(Config{BaseURL: "ftp://example.com"}); err == nil || err.Error() != "brain: invalid_configuration" {
		t.Fatalf("bad base URL error text: %v", err)
	}
}
