package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantHome bool
	}{
		{
			name:     "tilde only",
			path:     "~",
			wantHome: true,
		},
		{
			name:     "tilde with slash",
			path:     "~/",
			wantHome: true,
		},
		{
			name:     "tilde with subdirectory",
			path:     "~/.config/opencode",
			wantHome: true,
		},
		{
			name:     "absolute path",
			path:     "/usr/local/bin",
			wantHome: false,
		},
		{
			name:     "relative path",
			path:     "./relative/path",
			wantHome: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPath(tt.path)
			if tt.wantHome {
				home, _ := os.UserHomeDir()
				if got == tt.path || !filepath.IsAbs(got) {
					t.Errorf("expandPath(%q) = %q, want absolute path with home directory", tt.path, got)
				}
				if got != home && !filepath.HasPrefix(got, home) {
					t.Errorf("expandPath(%q) = %q, want path starting with %q", tt.path, got, home)
				}
			} else {
				if got != tt.path {
					t.Errorf("expandPath(%q) = %q, want %q", tt.path, got, tt.path)
				}
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "create single directory",
			path:    filepath.Join(tmpDir, "testdir"),
			wantErr: false,
		},
		{
			name:    "create nested directories",
			path:    filepath.Join(tmpDir, "nested", "deep", "path"),
			wantErr: false,
		},
		{
			name:    "directory already exists",
			path:    tmpDir,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureDir(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				info, err := os.Stat(tt.path)
				if err != nil {
					t.Errorf("ensureDir() created path but stat failed: %v", err)
					return
				}
				if !info.IsDir() {
					t.Errorf("ensureDir() created %q but it's not a directory", tt.path)
				}
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file with specific permissions
	srcPath := filepath.Join(tmpDir, "source.txt")
	srcContent := []byte("test content")
	srcMode := os.FileMode(0755)
	if err := os.WriteFile(srcPath, srcContent, srcMode); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	tests := []struct {
		name    string
		src     string
		dst     string
		mode    os.FileMode
		wantErr bool
		checkFn func(t *testing.T, dst string)
	}{
		{
			name:    "copy file with permissions",
			src:     srcPath,
			dst:     filepath.Join(tmpDir, "dest.txt"),
			mode:    0755,
			wantErr: false,
			checkFn: func(t *testing.T, dst string) {
				// Read content
				got, err := os.ReadFile(dst)
				if err != nil {
					t.Errorf("failed to read destination: %v", err)
					return
				}
				if string(got) != string(srcContent) {
					t.Errorf("content = %q, want %q", got, srcContent)
				}

				// Check permissions
				info, err := os.Stat(dst)
				if err != nil {
					t.Errorf("failed to stat destination: %v", err)
					return
				}
				if info.Mode().Perm() != 0755 {
					t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0755)
				}
			},
		},
		{
			name:    "copy file with different permissions",
			src:     srcPath,
			dst:     filepath.Join(tmpDir, "dest2.txt"),
			mode:    0644,
			wantErr: false,
			checkFn: func(t *testing.T, dst string) {
				info, err := os.Stat(dst)
				if err != nil {
					t.Errorf("failed to stat destination: %v", err)
					return
				}
				if info.Mode().Perm() != 0644 {
					t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0644)
				}
			},
		},
		{
			name:    "source file does not exist",
			src:     filepath.Join(tmpDir, "nonexistent.txt"),
			dst:     filepath.Join(tmpDir, "dest3.txt"),
			mode:    0644,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := copyFile(tt.src, tt.dst, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("copyFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFn != nil {
				tt.checkFn(t, tt.dst)
			}
		})
	}
}

func TestInstallPlugin(t *testing.T) {
	tests := []struct {
		name     string
		targetID string
		opts     InstallOptions
		wantErr  bool
	}{
		{
			name:     "unknown target returns error",
			targetID: "nonexistent",
			opts:     InstallOptions{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InstallPlugin(tt.targetID, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("InstallPlugin() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUninstallPlugin(t *testing.T) {
	tests := []struct {
		name     string
		targetID string
		wantErr  bool
	}{
		{
			name:     "unknown target returns error",
			targetID: "nonexistent",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UninstallPlugin(tt.targetID)
			if (err != nil) != tt.wantErr {
				t.Errorf("UninstallPlugin() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
