package auth

import (
	"fmt"
	"log/slog"
	"os"
)

// AuthenticateLocalDatabaseOwner is the audited host-ownership seam for
// unattended composition and offline CLI commands. The OS authenticates this
// process; successful read/write access to the EXISTING regular database is its
// authorization evidence. It neither creates a file nor changes permissions.
// This is not a remote credential verifier or a sandbox against hostile code in
// the same process. Only the exact owner call sites in the storage ownership CI
// policy may use it. Never pass request-supplied paths here or retain the result
// in HTTP handlers. Password-authenticated operator capabilities remain separate.
func AuthenticateLocalDatabaseOwner(path string) (DeploymentOperator, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return DeploymentOperator{}, fmt.Errorf("%w: local database access: %v", ErrOperatorRequired, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return DeploymentOperator{}, fmt.Errorf("%w: database must be a regular file", ErrOperatorRequired)
	}
	slog.Info("authenticated local deployment owner", "source", "OS database read/write authorization", "database", path)
	return DeploymentOperator{authenticated: true}, nil
}
