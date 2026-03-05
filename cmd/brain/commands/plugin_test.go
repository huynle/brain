package commands

import (
	"testing"
)

// TestPluginCommand_Type verifies Type() returns correct identifiers
func TestPluginCommand_Type(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		want       string
	}{
		{
			name:       "install subcommand",
			subcommand: "install",
			want:       "plugin-install",
		},
		{
			name:       "uninstall subcommand",
			subcommand: "uninstall",
			want:       "plugin-uninstall",
		},
		{
			name:       "status subcommand",
			subcommand: "status",
			want:       "plugin-status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &PluginCommand{
				Subcommand: tt.subcommand,
			}
			got := cmd.Type()
			if got != tt.want {
				t.Errorf("Type() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPluginCommand_Execute_UnknownSubcommand verifies error on unknown subcommand
func TestPluginCommand_Execute_UnknownSubcommand(t *testing.T) {
	cmd := &PluginCommand{
		Subcommand: "unknown",
		Config:     &UnifiedConfig{},
		Flags:      &PluginFlags{},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() expected error for unknown subcommand, got nil")
	}

	want := "unknown subcommand: unknown"
	if err.Error() != want {
		t.Errorf("Execute() error = %v, want %v", err.Error(), want)
	}
}

// TestPluginCommand_Execute_Status verifies status command lists targets
func TestPluginCommand_Execute_Status(t *testing.T) {
	// This test verifies the status command runs without error
	// We can't mock plugin targets easily, so we just verify no panic/error
	cmd := &PluginCommand{
		Subcommand: "status",
		Config:     &UnifiedConfig{},
		Flags:      &PluginFlags{},
	}

	// Status should not return error (it prints to stdout)
	err := cmd.Execute()
	if err != nil {
		t.Errorf("Execute() status command returned error: %v", err)
	}
}

// TestPluginFlags_DefaultValues verifies PluginFlags defaults
func TestPluginFlags_DefaultValues(t *testing.T) {
	flags := &PluginFlags{}

	if flags.Force {
		t.Error("Force should default to false")
	}
	if flags.DryRun {
		t.Error("DryRun should default to false")
	}
	if flags.APIURL != "" {
		t.Error("APIURL should default to empty string")
	}
}
