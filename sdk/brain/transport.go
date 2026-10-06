package brain

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPTransport is the shared HTTP send policy used by the typed Go SDK and
// compatibility clients such as stdio MCP. It fixes the API origin/path prefix,
// rejects structural traversal and redirects, and prevents ambiguous mutation
// replay by net/http. It makes exactly one call to the underlying RoundTripper.
// It does not interpret DTOs, grant authority, retry, buffer response bodies or
// choose credentials. The owning client supplies headers/context/timeouts and
// owns response decoding/limits. Never expose this trusted transport to scripts.
type HTTPTransport struct {
	next                 http.RoundTripper
	scheme, host, prefix string
}

// NewHTTPTransport accepts an optional trusted underlying transport. The returned
// binding is immutable; a different origin requires a new transport.
func NewHTTPTransport(baseURL string, next http.RoundTripper) (*HTTPTransport, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, &Error{Code: "invalid_configuration"}
	}
	if next == nil {
		next = http.DefaultTransport
	}
	return &HTTPTransport{next: next, scheme: u.Scheme, host: u.Host, prefix: strings.TrimRight(u.EscapedPath(), "/") + "/api/v1/"}, nil
}

func (t *HTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	refuse := func(code string) (*http.Response, error) {
		if r != nil && r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, &Error{Code: code}
	}
	if r == nil || r.URL == nil || r.URL.Scheme != t.scheme || r.URL.Host != t.host || r.URL.User != nil || r.URL.Fragment != "" || (r.Host != "" && r.Host != t.host) || !strings.HasPrefix(r.URL.EscapedPath(), t.prefix) {
		return refuse("invalid_request")
	}
	for decoded := r.URL.EscapedPath(); ; {
		for _, segment := range strings.FieldsFunc(decoded, func(r rune) bool { return r == '/' || r == '\\' }) {
			if segment == "." || segment == ".." {
				return refuse("invalid_request")
			}
		}
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = next
	}
	if err := r.Context().Err(); err != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	request := r.Clone(r.Context())
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		// Idempotency-Key does not give legacy endpoints server deduplication.
		request.GetBody = nil
		if request.Body == nil || request.Body == http.NoBody {
			request.Body = io.NopCloser(strings.NewReader(""))
			request.ContentLength = -1
		}
	}
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		_ = response.Body.Close()
		return nil, &Error{Code: "redirect_refused", Status: response.StatusCode, RequestID: response.Header.Get("X-Request-ID")}
	}
	return response, nil
}
