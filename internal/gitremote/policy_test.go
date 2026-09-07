package gitremote

import (
	"reflect"
	"testing"
)

func TestAuthority(t *testing.T) {
	for raw, want := range map[string]string{"GitHub.COM:443": "github.com", "git.example:8443": "git.example:8443", "127.0.0.1:8080": "127.0.0.1:8080", "[2001:0db8::1]:443": "[2001:db8::1]"} {
		got, err := Authority(raw)
		if err != nil || got != want {
			t.Errorf("Authority(%q)=%q,%v want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "*.example.com", "example.com.", "example.com:", "example.com:0443", "example.com:65536", "example.com:0", "example.com/a", "example.com@evil.com", "https://example.com", "[fe80::1%en0]", "bad_host", "éxample.com", "127.1", "2130706433", "0177.0.0.1", "0x7f000001", "0x7f.0.0.1"} {
		if got, err := Authority(raw); err == nil {
			t.Errorf("accepted ambiguous authority %q as %q", raw, got)
		}
	}
}

func TestValidateExactAuthority(t *testing.T) {
	hosts := []string{"github.com", "git.example:8443"}
	for _, raw := range []string{"https://GITHUB.COM:443/o/r.git", "https://git.example:8443/o/r"} {
		if _, err := Validate(raw, hosts); err != nil {
			t.Error(err)
		}
	}
	for _, raw := range []string{"https://github.com.evil/o/r", "https://sub.github.com/o/r", "https://github.com:8443/o/r", "https://git.example/o/r", "https://github.com@evil/o/r", "http://github.com/o/r", "git@github.com:o/r", "https://github.com/r?", "https://github.com/r#", "https://github.com/r%00"} {
		if _, err := Validate(raw, hosts); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	got, err := Hosts([]string{"B.example", "a.example:443", "b.example"})
	if err != nil || !reflect.DeepEqual(got, []string{"a.example", "b.example"}) {
		t.Errorf("Hosts=%v,%v", got, err)
	}
	if _, err := Validate("https://github.com/r", nil); err == nil {
		t.Fatal("empty hosts must deny")
	}
}

func TestParseRefusesAnonymousHTTP(t *testing.T) {
	for _, raw := range []string{"http://git.example/repo", "http://git.example:443/repo", "http://git.example:80/repo"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted non-HTTPS remote %q", raw)
		}
	}
}
