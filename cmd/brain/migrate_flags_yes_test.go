package main

import "testing"

// --yes confirms the migrate stagger offer without a terminal; -y is its short form.
func TestParseMigrateFlags_Yes(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"-y"}, {"--dry-run", "--yes"}} {
		flags, err := ParseMigrateFlags(args)
		if err != nil {
			t.Fatalf("ParseMigrateFlags(%v): %v", args, err)
		}
		if !flags.Yes {
			t.Errorf("ParseMigrateFlags(%v).Yes = false, want true", args)
		}
	}
	flags, err := ParseMigrateFlags([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("ParseMigrateFlags: %v", err)
	}
	if flags.Yes {
		t.Error("Yes defaulted to true")
	}
}
