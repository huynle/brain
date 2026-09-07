package tokens

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineTokensRejectMultiWithoutOpeningDatabase(t *testing.T) {
	t.Setenv("BRAIN_TENANT_MODE", "multi")
	dir := t.TempDir()
	if _, err := CreateTokenDirect(dir, "first", "admin:*"); err == nil {
		t.Fatal("multi create accepted")
	}
	if _, err := ListTokensDirect(dir); err == nil {
		t.Fatal("multi list accepted")
	}
	if err := RevokeTokenDirect(dir, "first"); err == nil {
		t.Fatal("multi revoke accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".brain-data", "brain.db")); !os.IsNotExist(err) {
		t.Fatalf("multi command opened database: %v", err)
	}
}
