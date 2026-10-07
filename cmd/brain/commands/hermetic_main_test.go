package commands

import (
	"os"
	"testing"
)

// TestMain keeps every test in this package away from the developer's real
// Brain config, which may point at a production API. Tests that need a
// server start an httptest one and pass its URL explicitly.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "brain-cli-test-home-")
	if err != nil {
		panic(err)
	}
	for k, v := range map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": dir + "/.config",
		"XDG_STATE_HOME":  dir + "/.local/state",
		"BRAIN_API_URL":   "http://127.0.0.1:1",
		"BRAIN_API_TOKEN": "",
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
