package doctor

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func TestCheckBrainDirectory(t *testing.T) {
	t.Run("directory exists and writable", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkBrainDirectory(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "brain-directory" {
			t.Errorf("checkBrainDirectory() name = %q, want %q", check.Name, "brain-directory")
		}
		if !check.Fixable {
			t.Error("checkBrainDirectory() should be fixable")
		}
	})

	t.Run("directory does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "nonexistent")

		check := checkBrainDirectory(nonExistent)

		if check.Status != CheckStatusFail {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkBrainDirectory() should be fixable when directory missing")
		}
	})

	t.Run("directory not writable", func(t *testing.T) {
		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")
		os.Mkdir(readOnlyDir, 0444)
		defer os.Chmod(readOnlyDir, 0755) // cleanup

		check := checkBrainDirectory(readOnlyDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusFail)
		}
	})
}

func TestCheckTemplates(t *testing.T) {
	t.Run("all templates present", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".brain-data", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create all expected templates
		expectedTemplates := []string{
			"decision.md", "execution.md", "exploration.md",
			"idea.md", "learning.md", "pattern.md",
			"plan.md", "report.md", "scratch.md",
			"summary.md", "task.md", "walkthrough.md",
			"default.md",
		}
		for _, tmpl := range expectedTemplates {
			os.WriteFile(filepath.Join(templatesDir, tmpl), []byte("test"), 0644)
		}

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "templates" {
			t.Errorf("checkTemplates() name = %q, want %q", check.Name, "templates")
		}
	})

	t.Run("missing templates", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".brain-data", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create only some templates
		os.WriteFile(filepath.Join(templatesDir, "task.md"), []byte("test"), 0644)

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkTemplates() should be fixable when templates missing")
		}
	})

	t.Run("templates directory missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkTemplates() should be fixable when directory missing")
		}
	})
}

func TestCheckConfig(t *testing.T) {
	t.Run("config exists and valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".brain-data")
		os.MkdirAll(zkDir, 0755)

		configPath := filepath.Join(zkDir, "config.toml")
		validConfig := `[note]
id_length = 8
`
		os.WriteFile(configPath, []byte(validConfig), 0644)

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "config" {
			t.Errorf("checkConfig() name = %q, want %q", check.Name, "config")
		}
	})

	t.Run("config missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkConfig() should be fixable when config missing")
		}
	})

	t.Run("config invalid TOML", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".brain-data")
		os.MkdirAll(zkDir, 0755)

		configPath := filepath.Join(zkDir, "config.toml")
		invalidConfig := `[note
this is not valid TOML
`
		os.WriteFile(configPath, []byte(invalidConfig), 0644)

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkConfig() should be fixable when config invalid")
		}
	})
}

func TestCheckDatabase(t *testing.T) {
	t.Run("database exists and accessible", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "brain.db")

		// Create empty file to simulate database
		os.WriteFile(dbPath, []byte{}, 0644)

		check := checkDatabase(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkDatabase() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "database" {
			t.Errorf("checkDatabase() name = %q, want %q", check.Name, "database")
		}
	})

	t.Run("database missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkDatabase(tmpDir)

		if check.Status != CheckStatusWarn {
			t.Errorf("checkDatabase() status = %v, want %v", check.Status, CheckStatusWarn)
		}
		if check.Fixable {
			t.Error("checkDatabase() should not be fixable - database created on first use")
		}
	})
}

func TestCheckAttachmentStorageRoot(t *testing.T) {
	t.Run("storage root exists writable and backup-covered under brain dir", func(t *testing.T) {
		brainDir := t.TempDir()
		root := filepath.Join(brainDir, "attachments")
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}

		check := checkAttachmentStorageRoot(brainDir, root)

		if check.Status != CheckStatusPass {
			t.Fatalf("status = %v, want pass; message=%s", check.Status, check.Message)
		}
		if check.Name != "attachment-storage" {
			t.Fatalf("name = %q, want attachment-storage", check.Name)
		}
		if !strings.Contains(check.Message, "included with brain directory backups") {
			t.Fatalf("message = %q, want backup coverage guidance", check.Message)
		}
	})

	t.Run("outside brain dir warns backup must include both paths", func(t *testing.T) {
		brainDir := t.TempDir()
		root := filepath.Join(t.TempDir(), "attachment-blobs")
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}

		check := checkAttachmentStorageRoot(brainDir, root)

		if check.Status != CheckStatusWarn {
			t.Fatalf("status = %v, want warn; message=%s", check.Status, check.Message)
		}
		for _, want := range []string{"outside brain directory", "brain.db", root} {
			if !strings.Contains(check.Message, want) {
				t.Fatalf("message = %q, want %q", check.Message, want)
			}
		}
	})

	t.Run("missing storage root fails", func(t *testing.T) {
		brainDir := t.TempDir()
		root := filepath.Join(brainDir, "missing-attachments")

		check := checkAttachmentStorageRoot(brainDir, root)

		if check.Status != CheckStatusFail {
			t.Fatalf("status = %v, want fail; message=%s", check.Status, check.Message)
		}
		if check.Fixable {
			t.Fatal("attachment storage root should not be marked fixable by doctor")
		}
	})
}

func TestCheckAttachmentUploadLimits(t *testing.T) {
	tests := []struct {
		name  string
		limit int64
		want  CheckStatus
	}{
		{name: "positive explicit limit passes", limit: 1024, want: CheckStatusPass},
		{name: "zero limit fails", limit: 0, want: CheckStatusFail},
		{name: "negative limit fails", limit: -1, want: CheckStatusFail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := checkAttachmentUploadLimits(tt.limit)
			if check.Status != tt.want {
				t.Fatalf("status = %v, want %v; message=%s", check.Status, tt.want, check.Message)
			}
			if check.Name != "attachment-upload-limits" {
				t.Fatalf("name = %q", check.Name)
			}
		})
	}
}

func TestCheckAttachmentBlobIntegrity(t *testing.T) {
	t.Run("detects missing blob digest mismatch and orphan blob", func(t *testing.T) {
		root := t.TempDir()
		goodData := []byte("good attachment")
		goodDigest := writeBlobForDoctorTest(t, root, goodData)
		missingDigest := strings.Repeat("a", 64)
		mismatchDigest := strings.Repeat("b", 64)
		writeBlobAtDigestForDoctorTest(t, root, mismatchDigest, []byte("wrong data"))
		orphanDigest := writeBlobForDoctorTest(t, root, []byte("orphan data"))

		check := checkAttachmentBlobIntegrity(root, []AttachmentDigestCheck{
			{Digest: goodDigest, Size: int64(len(goodData))},
			{Digest: missingDigest, Size: 12},
			{Digest: mismatchDigest, Size: int64(len("expected data"))},
		})

		if check.Status != CheckStatusFail {
			t.Fatalf("status = %v, want fail; message=%s", check.Status, check.Message)
		}
		for _, want := range []string{"missing blobs: 1", "digest mismatches: 1", "orphan blobs: 1", orphanDigest[:12]} {
			if !strings.Contains(check.Message, want) {
				t.Fatalf("message = %q, want %q", check.Message, want)
			}
		}
	})

	t.Run("passes when every blob matches database digests", func(t *testing.T) {
		root := t.TempDir()
		data := []byte("consistent")
		digest := writeBlobForDoctorTest(t, root, data)

		check := checkAttachmentBlobIntegrity(root, []AttachmentDigestCheck{{Digest: digest, Size: int64(len(data))}})

		if check.Status != CheckStatusPass {
			t.Fatalf("status = %v, want pass; message=%s", check.Status, check.Message)
		}
	})
}

func TestLoadAttachmentDigestChecksFromDatabase(t *testing.T) {
	brainDir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(brainDir, "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE attachments (id INTEGER PRIMARY KEY, digest TEXT NOT NULL, size INTEGER NOT NULL, media_type TEXT, metadata TEXT, created_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO attachments (digest, size, media_type, metadata, created_at) VALUES ('` + strings.Repeat("d", 64) + `', 42, 'text/plain', '{}', 'now')`); err != nil {
		t.Fatal(err)
	}

	checks, err := loadAttachmentDigestChecksFromDatabase(brainDir)
	if err != nil {
		t.Fatalf("loadAttachmentDigestChecksFromDatabase returned error: %v", err)
	}
	if len(checks) != 1 || checks[0].Digest != strings.Repeat("d", 64) || checks[0].Size != 42 {
		t.Fatalf("checks = %#v, want one digest/size row", checks)
	}

	missing, err := loadAttachmentDigestChecksFromDatabase(filepath.Join(t.TempDir(), "missing-brain"))
	if err != nil {
		t.Fatalf("missing database returned error: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing checks = %#v, want nil", missing)
	}
}

func writeBlobForDoctorTest(t *testing.T, root string, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	writeBlobAtDigestForDoctorTest(t, root, digest, data)
	return digest
}

func writeBlobAtDigestForDoctorTest(t *testing.T, root, digest string, data []byte) {
	t.Helper()
	path := filepath.Join(root, digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
