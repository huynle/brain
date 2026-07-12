package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeTarget_ID(t *testing.T) {
	target := &ClaudeTarget{}
	if got := target.ID(); got != "claude" {
		t.Errorf("ID() = %q, want %q", got, "claude")
	}
}

func TestClaudeTarget_Name(t *testing.T) {
	target := &ClaudeTarget{}
	if got := target.Name(); got != "Claude Code" {
		t.Errorf("Name() = %q, want %q", got, "Claude Code")
	}
}

func TestClaudeTarget_Description(t *testing.T) {
	target := &ClaudeTarget{}
	desc := target.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
	if !strings.Contains(desc, "Claude") {
		t.Errorf("Description() = %q, should mention Claude", desc)
	}
}

func TestClaudeTarget_Exists_ReturnsFalseWhenConfigMissing(t *testing.T) {
	tmpDir := t.TempDir()

	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if target.Exists() {
		t.Error("Exists() = true, want false when config directory missing")
	}
}

func TestClaudeTarget_Exists_ReturnsTrueWhenConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("Failed to create test config dir: %v", err)
	}

	target := &ClaudeTarget{
		configPath: configPath,
	}

	if !target.Exists() {
		t.Error("Exists() = false, want true when config directory exists")
	}
}

func TestClaudeTarget_Install_CopiesSkills(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	// Representative skills that ship with brain
	expectedFiles := []string{
		"skills/using-brain/SKILL.md",
		"skills/brain-project-planning/SKILL.md",
		"skills/brain-memory/SKILL.md",
	}

	for _, relPath := range expectedFiles {
		fullPath := filepath.Join(tmpDir, ".claude", relPath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Install() did not create %s", relPath)
		}
	}
}

func TestClaudeTarget_Install_SkillStartsWithFrontmatterAndHasHeader(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	skillPath := filepath.Join(tmpDir, ".claude", "skills", "using-brain", "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("Failed to read installed skill: %v", err)
	}

	// Claude Code requires SKILL.md to start with YAML frontmatter
	if !strings.HasPrefix(string(content), "---\n") {
		t.Errorf("Installed skill should start with YAML frontmatter, got: %q", string(content[:20]))
	}

	if !strings.Contains(string(content), "AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY") {
		t.Error("Installed skill missing auto-generated header")
	}
	if !strings.Contains(string(content), "brain install claude") {
		t.Error("Installed skill header should reference 'brain install claude'")
	}
}

func TestClaudeTarget_Install_DryRunDoesNotCreateFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".claude")
	target := &ClaudeTarget{configPath: configPath}

	if err := target.Install(InstallOptions{DryRun: true}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(configPath, "skills")); !os.IsNotExist(err) {
		t.Error("DryRun install should not create the skills directory")
	}
}

func TestClaudeTarget_Install_WithoutForceSkipsExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	skillPath := filepath.Join(tmpDir, ".claude", "skills", "using-brain", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}
	custom := []byte("user-modified content")
	if err := os.WriteFile(skillPath, custom, 0644); err != nil {
		t.Fatalf("Failed to write existing skill: %v", err)
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("Failed to read skill: %v", err)
	}
	if string(content) != string(custom) {
		t.Error("Install() without --force should not overwrite existing files")
	}
}

func TestClaudeTarget_Install_ForceOverwritesExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	skillPath := filepath.Join(tmpDir, ".claude", "skills", "using-brain", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("stale content"), 0644); err != nil {
		t.Fatalf("Failed to write existing skill: %v", err)
	}

	output := capturePluginOutput(t, func() {
		if err := target.Install(InstallOptions{Force: true}); err != nil {
			t.Errorf("Install() failed: %v", err)
		}
	})

	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("Failed to read skill: %v", err)
	}
	if string(content) == "stale content" {
		t.Error("Install() with --force should overwrite existing files")
	}
	if !strings.Contains(output, "Updated: skills/using-brain/SKILL.md") {
		t.Errorf("Install() with --force should report updated file, got output:\n%s", output)
	}
}

func TestClaudeTarget_Install_ForceIsIdempotentAcrossRuns(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Install(InstallOptions{Force: true}); err != nil {
		t.Fatalf("first Install() failed: %v", err)
	}

	output := capturePluginOutput(t, func() {
		if err := target.Install(InstallOptions{Force: true}); err != nil {
			t.Errorf("second Install() failed: %v", err)
		}
	})

	if !strings.Contains(output, "Identical: skills/using-brain/SKILL.md (left untouched)") {
		t.Errorf("second install should report identical files, got output:\n%s", output)
	}
	if strings.Contains(output, "Updated: skills/using-brain/SKILL.md") {
		t.Errorf("second install should not rewrite identical files, got output:\n%s", output)
	}
}

func TestClaudeTarget_Uninstall_RemovesSkills(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if err := target.Uninstall(); err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	skillPath := filepath.Join(tmpDir, ".claude", "skills", "using-brain", "SKILL.md")
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("Uninstall() should remove installed skill files")
	}
	if _, err := os.Stat(filepath.Dir(skillPath)); !os.IsNotExist(err) {
		t.Error("Uninstall() should remove empty skill directories")
	}
}

func TestClaudeTarget_Uninstall_LeavesForeignSkillsAlone(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	// A user-authored skill unrelated to brain
	foreignPath := filepath.Join(tmpDir, ".claude", "skills", "my-own-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreignPath), 0755); err != nil {
		t.Fatalf("Failed to create foreign skill dir: %v", err)
	}
	if err := os.WriteFile(foreignPath, []byte("mine"), 0644); err != nil {
		t.Fatalf("Failed to write foreign skill: %v", err)
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	if err := target.Uninstall(); err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	if _, err := os.Stat(foreignPath); os.IsNotExist(err) {
		t.Error("Uninstall() must not remove user-authored skills")
	}
}

func TestClaudeTarget_Validate_DetectsMissingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Validate(); err == nil {
		t.Error("Validate() should fail when no skills are installed")
	}
}

func TestClaudeTarget_Validate_PassesWhenFilesExist(t *testing.T) {
	tmpDir := t.TempDir()
	target := &ClaudeTarget{
		configPath: filepath.Join(tmpDir, ".claude"),
	}

	if err := target.Install(InstallOptions{}); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if err := target.Validate(); err != nil {
		t.Errorf("Validate() should pass after successful install, got: %v", err)
	}
}

func TestGetAvailableTargets_ReturnsClaudeTarget(t *testing.T) {
	targets := GetAvailableTargets()

	found := false
	for _, target := range targets {
		if target.ID() == "claude" {
			found = true
			break
		}
	}

	if !found {
		t.Error("GetAvailableTargets() should include claude target")
	}
}

func TestGetTarget_ResolvesClaudeCodeAlias(t *testing.T) {
	target := getTarget("claude-code")
	if target == nil {
		t.Fatal("getTarget(\"claude-code\") returned nil, want claude target")
	}
	if target.ID() != "claude" {
		t.Errorf("getTarget(\"claude-code\").ID() = %q, want %q", target.ID(), "claude")
	}
}
