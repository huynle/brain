package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
)

// classifySchemaSource recognizes reviewed source catalogs only. Constructors and
// migrations do not call it. The private execution-ledger preflight uses it to
// distinguish genuine local main29/30 from private29. Recognition is NOT runtime
// admission, authorization, data integrity, root/CAS readiness or permission to
// migrate. The caller owns a stable transaction and must fence schema writers
// before using a result for a future migration. It performs no writes or PRAGMAs.
//
// Main pins are SHA-256 of JSON [][4]string (type,name,tbl_name,coalesce(sql,”)),
// ordered by type,name over the COMPLETE sqlite_schema. Root pages and rows are
// not catalog provenance. SQL is byte-exact: no whitespace/literal normalization,
// object-name exclusions, optional additive tables or mutable target DDL. See the
// archived InitSchema fixtures for the immutable source revisions and limitations.
func classifySchemaSource(ctx context.Context, tx *sql.Tx) (string, error) {
	if ctx == nil || tx == nil {
		return "", fmt.Errorf("source classification requires context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var temporary int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_temp_schema").Scan(&temporary); err != nil {
		return "", err
	}
	if temporary != 0 {
		return "", fmt.Errorf("source classification refuses temporary schema objects")
	}
	rows, err := tx.QueryContext(ctx, "SELECT version,typeof(version) FROM main.schema_version ORDER BY version")
	if err != nil {
		return "", err
	}
	version := 0
	for rows.Next() {
		var v int
		var kind string
		if err := rows.Scan(&v, &kind); err != nil {
			_ = rows.Close()
			return "", err
		}
		if kind != "integer" || v < 1 || v > 30 {
			_ = rows.Close()
			return "", fmt.Errorf("unreviewed source version history")
		}
		version = v
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return "", err
	}
	rows, err = tx.QueryContext(ctx, "SELECT type,name,tbl_name,coalesce(sql,'') FROM main.sqlite_schema ORDER BY type,name")
	if err != nil {
		return "", err
	}
	var catalog [][4]string
	private := false
	for rows.Next() {
		var row [4]string
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3]); err != nil {
			_ = rows.Close()
			return "", err
		}
		catalog = append(catalog, row)
		private = private || (row[0] == "table" && row[1] == "tenants")
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return "", err
	}
	if version == 29 && private {
		// Private29 has server-generated per-tenant FTS names. Retain the
		// existing exact validators, including mapping and control definitions;
		// never treat the numeric collision with main29 as sufficient provenance.
		if err := checkMigrationControlSchema(tx); err != nil {
			return "", err
		}
		if _, err := checkTenantSearchCatalog(tx); err != nil {
			return "", err
		}
		return "private29", nil
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	for _, pin := range []struct {
		version         int
		profile, digest string
	}{
		{28, "main28", "cb702fa33fbe1231a6c1347384aaeee874ebdf35aa308c068dcf5923e942d2c3"},
		{29, "main29", "269268c4e9ed0cc16a05a5ce9c9e2b23cd86d63372d1aec48e6c7af8c9888a23"},
		{30, "main30-pre-sync", "d43f48233533d3476d92ffeec25b1f874cb91dc55e01ee7d09e81a7880a17776"},
		{30, "main30-initial-sync", "72fc3207492e15819c48b58df76f4faf79eefd7f1b035f007140eee95227941d"},
		{30, "main30-devices", "2104f3eac47abaa1b5cc8e35e7265e7ca7b7856cd0e1335df03d33f01bb65a66"},
	} {
		if version == pin.version && digest == pin.digest {
			return pin.profile, nil
		}
	}
	return "", fmt.Errorf("unreviewed source catalog at version %d (sha256 %s)", version, digest)
}
