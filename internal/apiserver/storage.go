package apiserver

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// storageViews is composition-only. No raw pool owner or operator capability
// survives in a handler/service. Roots is used only to construct bound consumers.
type storageViews struct {
	tenant   *storage.TenantStore
	identity *storage.ControlStore
	tokens   *storage.SingleModeTokenStore
	roots    *tenantfs.Resolver
	close    func()
}

// openSingleModeStorage is the exact audited lifetime/host-ownership seam.
// Mode is deployment configuration, never a request parameter. Reject BEFORE
// opening the DB or authenticating host ownership. Only this local composition
// path delegates registry capability, and never to an ordinary request handler.
func openSingleModeStorage(ctx context.Context, mode tenant.Mode, dbPath, brainDir string, passwordConfigured bool) (*storageViews, error) {
	if _, err := backgroundTenantContext(ctx, mode); err != nil {
		return nil, err
	}
	owner, err := storage.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open shared database: %w", err)
	}
	closeOwner := func() { _ = owner.Close() }
	ok := false
	defer func() {
		if !ok {
			closeOwner()
		}
	}()
	view, err := owner.ForTenant(tenant.Local)
	if err != nil {
		return nil, err
	}
	control, err := owner.Control()
	if err != nil {
		return nil, err
	}
	operator, err := auth.AuthenticateLocalDatabaseOwner(dbPath)
	if err != nil {
		return nil, err
	}
	if passwordConfigured {
		if err := control.MarkInstallClaimed(ctx, operator); err != nil {
			return nil, fmt.Errorf("failed to persist configured installation claim: %w", err)
		}
	}
	registry, err := control.TenantRegistry(operator)
	if err != nil {
		return nil, err
	}
	roots, err := tenantfs.New(registry, brainDir)
	if err != nil {
		return nil, err
	}
	// The guard above normalizes unset deployment mode to single. Do not move
	// this literal to a caller-controlled bypass or hand out TokenAdmin instead.
	tokens, err := owner.SingleModeTokens(tenant.ModeSingle)
	if err != nil {
		return nil, err
	}
	ok = true
	return &storageViews{tenant: view, identity: control, tokens: tokens, roots: roots, close: closeOwner}, nil
}
