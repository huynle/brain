package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateAttentionSystemRecipients(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recipients []string
		wantErr    string
	}{
		{name: "absent is valid"},
		{name: "distinct names are valid", recipients: []string{"alice", "bob"}},
		{
			name:       "blank name rejected",
			recipients: []string{"alice", "   "},
			wantErr:    "server.attention.system_recipients[1] must not be blank",
		},
		{
			name:       "duplicate after trimming rejected",
			recipients: []string{"alice", " alice "},
			wantErr:    `server.attention.system_recipients lists "alice" more than once`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.Attention.SystemRecipients = tc.recipients
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadAttentionSystemRecipientsFromConfigFile(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "brain")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("server:\n  attention:\n    system_recipients:\n      - alice\n      - bob\n")
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load()
	if err := cfg.Err(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"alice", "bob"}
	if !reflect.DeepEqual(cfg.Attention.SystemRecipients, want) {
		t.Fatalf("Attention.SystemRecipients = %v, want %v", cfg.Attention.SystemRecipients, want)
	}
}
