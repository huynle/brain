package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineAttachmentChecksRejectMulti(t *testing.T) {
	t.Setenv("BRAIN_TENANT_MODE", "multi")
	dir := t.TempDir()
	if _, err := loadAttachmentDigestChecksFromDatabase(dir); err == nil {
		t.Fatal("multi offline check accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "brain.db")); !os.IsNotExist(err) {
		t.Fatalf("multi check opened database: %v", err)
	}
}
