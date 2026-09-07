// Package gitremote defines the credential-free remote admission contract shared
// by runners and (in Phase 2) the service. Hosts are exact HTTPS authorities,
// including non-default ports; there are no wildcard or suffix matches.
package gitremote

import (
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Authority canonicalizes a DNS name or bracketed IP with an optional port.
// DNS case and :443 normalize; ambiguous spellings, zones and trailing dots fail.
func Authority(raw string) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("invalid git remote host %q", raw) }
	if raw == "" || strings.ContainsAny(raw, "/@?#\\% \t\r\n") {
		return bad()
	}
	u, err := url.Parse("https://" + raw)
	if err != nil || u.Host != raw || u.User != nil {
		return bad()
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return bad()
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return bad()
		}
		host = ip.String()
		if ip.Is6() {
			host = "[" + host + "]"
		} else if strings.Contains(raw, "[") {
			return bad()
		}
	} else {
		if len(host) > 253 || strings.ContainsAny(raw, "[]") {
			return bad()
		}
		// libcurl accepts historical inet_aton spellings that net/url treats
		// as DNS. Refuse those rather than authorize a different IP spelling.
		numeric := true
		for _, label := range strings.Split(host, ".") {
			decimal := label != "" && strings.Trim(label, "0123456789") == ""
			hex := strings.HasPrefix(label, "0x") && len(label) > 2 && strings.Trim(label[2:], "0123456789abcdef") == ""
			numeric = numeric && (decimal || hex)
		}
		if numeric {
			return bad()
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return bad()
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return bad()
				}
			}
		}
	}
	port := u.Port()
	if strings.HasSuffix(raw, ":") {
		return bad()
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return bad()
		}
		if n != 443 {
			host += ":" + port
		}
	}
	return host, nil
}

// Parse validates an HTTPS remote without consulting credentials.
func Parse(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("git_remote must be a valid HTTPS URL; SSH remotes are not allowed")
	}
	if u.User != nil {
		return nil, fmt.Errorf("git_remote must not contain embedded credentials")
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("git_remote must use HTTPS")
	}
	host, err := Authority(u.Host)
	if err != nil {
		return nil, err
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" || u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return nil, fmt.Errorf("git_remote must have a repository path without query or fragment")
	}
	for _, c := range u.Path {
		if c <= 32 || c == 127 || c == '\\' {
			return nil, fmt.Errorf("invalid git_remote path")
		}
	}
	u.Host = host
	return u, nil
}

// Validate authorizes against effective (credentialed or explicitly anonymous)
// authorities supplied by the caller. It does not expand subdomains or ports.
func Validate(raw string, hosts []string) (*url.URL, error) {
	u, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	for _, host := range hosts {
		canonical, err := Authority(host)
		if err != nil {
			return nil, err
		}
		if canonical == u.Host {
			return u, nil
		}
	}
	return nil, fmt.Errorf("git remote host %q is not allowed or has no git token configured", u.Host)
}

// Hosts returns sorted, canonical, deduplicated authorities.
func Hosts(hosts []string) ([]string, error) {
	seen := map[string]bool{}
	for _, host := range hosts {
		h, err := Authority(host)
		if err != nil {
			return nil, err
		}
		seen[h] = true
	}
	result := make([]string, 0, len(seen))
	for host := range seen {
		result = append(result, host)
	}
	sort.Strings(result)
	return result, nil
}
