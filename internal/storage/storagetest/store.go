// Package storagetest supplies local tenant fixtures. Production imports are
// forbidden by TestProductionStorageOwnership; storage implementation tests may
// continue to use raw owners directly.
package storagetest

import (
	"database/sql"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

func New(path string) (*storage.TenantStore, error) {
	owner, err := storage.New(path)
	if err != nil {
		return nil, err
	}
	return owner.ForTenant(tenant.Local)
}

func NewWithDB(db *sql.DB) (*storage.TenantStore, error) {
	owner, err := storage.NewWithDB(db)
	if err != nil {
		return nil, err
	}
	return owner.ForTenant(tenant.Local)
}
