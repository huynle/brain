package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/huynle/brain-api/cmd/brain/assets"
	brainconfig "github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
)

// AttachmentDigestCheck is the attachment metadata doctor needs to verify that
// on-disk blobs still match database rows.
type AttachmentDigestCheck struct {
	Digest string
	Size   int64
}

// checkBrainDirectory checks if the brain directory exists and is writable.
func checkBrainDirectory(brainDir string) Check {
	check := Check{
		Name:    "brain-directory",
		Fixable: true,
	}

	// Check if directory exists
	info, err := os.Stat(brainDir)
	if err != nil {
		if os.IsNotExist(err) {
			check.Status = CheckStatusFail
			check.Message = fmt.Sprintf("Brain directory does not exist: %s", brainDir)
			return check
		}
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Cannot access brain directory: %v", err)
		return check
	}

	if !info.IsDir() {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Path exists but is not a directory: %s", brainDir)
		return check
	}

	// Check if writable by attempting to create a temp file
	testFile := filepath.Join(brainDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Brain directory is not writable: %v", err)
		return check
	}
	os.Remove(testFile)

	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("Brain directory exists and is writable: %s", brainDir)
	return check
}

// checkTemplates checks if all required templates are present.
func checkTemplates(brainDir string) Check {
	check := Check{
		Name:    "templates",
		Fixable: true,
	}

	templatesDir := filepath.Join(brainDir, brainconfig.DataDir, "templates")

	// Check if templates directory exists
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		check.Status = CheckStatusFail
		check.Message = "Templates directory does not exist"
		return check
	}

	// Get expected templates from assets
	expectedTemplates := assets.ListTemplates()
	if len(expectedTemplates) == 0 {
		check.Status = CheckStatusWarn
		check.Message = "Cannot verify templates: asset list empty"
		return check
	}

	// Check each template
	missing := []string{}
	for _, tmpl := range expectedTemplates {
		path := filepath.Join(templatesDir, tmpl)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			missing = append(missing, tmpl)
		}
	}

	if len(missing) > 0 {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Missing %d templates: %v", len(missing), missing)
		return check
	}

	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("All %d templates present", len(expectedTemplates))
	return check
}

// checkConfig checks if config.toml exists and is valid.
func checkConfig(brainDir string) Check {
	check := Check{
		Name:    "config",
		Fixable: true,
	}

	configPath := filepath.Join(brainDir, brainconfig.DataDir, "config.toml")

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		check.Status = CheckStatusFail
		check.Message = "Config file does not exist: " + brainconfig.DataDir + "/config.toml"
		return check
	}

	// Try to parse as TOML
	var config map[string]interface{}
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Config file is not valid TOML: %v", err)
		return check
	}

	check.Status = CheckStatusPass
	check.Message = "Config file exists and is valid TOML"
	return check
}

// checkDatabase checks if the database file exists and is accessible.
func checkDatabase(brainDir string) Check {
	check := Check{
		Name:    "database",
		Fixable: false, // Database is created automatically on first use
	}

	dbPath := filepath.Join(brainDir, "brain.db")

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		check.Status = CheckStatusWarn
		check.Message = "Database file does not exist (will be created on first use)"
		return check
	}

	check.Status = CheckStatusPass
	check.Message = "Database file exists"
	return check
}

// checkAttachmentStorageRoot verifies that the configured attachment blob root
// exists, is writable, and clearly states backup expectations.
func checkAttachmentStorageRoot(brainDir, storageRoot string) Check {
	check := Check{Name: "attachment-storage", Fixable: false}
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" {
		check.Status = CheckStatusFail
		check.Message = "Attachment storage root is not configured"
		return check
	}

	info, err := os.Stat(storageRoot)
	if err != nil {
		if os.IsNotExist(err) {
			check.Status = CheckStatusFail
			check.Message = fmt.Sprintf("Attachment storage root does not exist: %s", storageRoot)
			return check
		}
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Cannot access attachment storage root %s: %v", storageRoot, err)
		return check
	}
	if !info.IsDir() {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Attachment storage root is not a directory: %s", storageRoot)
		return check
	}

	testFile := filepath.Join(storageRoot, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Attachment storage root is not writable: %v", err)
		return check
	}
	_ = os.Remove(testFile)

	if pathWithin(brainDir, storageRoot) {
		check.Status = CheckStatusPass
		check.Message = fmt.Sprintf("Attachment storage root exists and is writable: %s; included with brain directory backups", storageRoot)
		return check
	}

	check.Status = CheckStatusWarn
	check.Message = fmt.Sprintf("Attachment storage root exists and is writable but is outside brain directory: %s; backups/exports must include both %s and brain.db", storageRoot, storageRoot)
	return check
}

func checkAttachmentUploadLimits(maxUploadSizeBytes int64) Check {
	check := Check{Name: "attachment-upload-limits", Fixable: false}
	if maxUploadSizeBytes <= 0 {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Attachment max upload size must be positive, got %d", maxUploadSizeBytes)
		return check
	}
	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("Attachment max upload size is configured: %d bytes", maxUploadSizeBytes)
	return check
}

// checkAttachmentBlobIntegrity verifies that database attachment digests have
// matching blobs and that blob files are not orphaned from the database.
func checkAttachmentBlobIntegrity(storageRoot string, expected []AttachmentDigestCheck) Check {
	check := Check{Name: "attachment-integrity", Fixable: false}
	expectedByDigest := make(map[string]AttachmentDigestCheck, len(expected))
	for _, row := range expected {
		digest := strings.TrimSpace(strings.ToLower(row.Digest))
		if digest != "" {
			expectedByDigest[digest] = AttachmentDigestCheck{Digest: digest, Size: row.Size}
		}
	}

	seen := map[string]struct{}{}
	missing := 0
	mismatches := 0
	for digest, row := range expectedByDigest {
		path := attachmentBlobPath(storageRoot, digest)
		actualDigest, size, err := digestFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				missing++
				continue
			}
			check.Status = CheckStatusFail
			check.Message = fmt.Sprintf("Attachment integrity check failed reading %s: %v", path, err)
			return check
		}
		seen[digest] = struct{}{}
		if actualDigest != digest || (row.Size >= 0 && size != row.Size) {
			mismatches++
		}
	}

	orphans := make([]string, 0)
	if err := filepath.WalkDir(storageRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !isHexSHA256(d.Name()) {
			return nil
		}
		if _, ok := expectedByDigest[d.Name()]; !ok {
			orphans = append(orphans, d.Name())
		}
		return nil
	}); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Attachment integrity check failed walking storage root: %v", err)
		return check
	}

	if missing > 0 || mismatches > 0 || len(orphans) > 0 {
		check.Status = CheckStatusFail
		sample := ""
		if len(orphans) > 0 {
			sample = fmt.Sprintf("; orphan sample: %s", orphans[0][:12])
		}
		check.Message = fmt.Sprintf("Attachment integrity failures: missing blobs: %d, digest mismatches: %d, orphan blobs: %d%s", missing, mismatches, len(orphans), sample)
		return check
	}

	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("Attachment integrity OK: %d database blobs verified, 0 orphan blobs", len(seen))
	return check
}

func loadAttachmentDigestChecksFromDatabase(brainDir string) ([]AttachmentDigestCheck, error) {
	dbPath := filepath.Join(brainDir, "brain.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	store, err := storage.New(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	rows, err := store.ListAttachments(context.Background())
	if err != nil {
		return nil, err
	}
	checks := make([]AttachmentDigestCheck, 0, len(rows))
	for _, row := range rows {
		checks = append(checks, AttachmentDigestCheck{Digest: row.Digest, Size: row.Size})
	}
	return checks, nil
}

func pathWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func attachmentBlobPath(root, digest string) string {
	if len(digest) < 4 {
		return filepath.Join(root, digest)
	}
	return filepath.Join(root, digest[:2], digest[2:4], digest)
}

func digestFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", size, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func isHexSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}
