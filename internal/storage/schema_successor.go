package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// A version is meaningful only within its family. In particular main/29 is
// NOT private-tenant/29. These private descriptors confer no runtime admission.
type schemaFormat struct {
	family  string
	version int
}

type schemaSource struct {
	profile string
	format  schemaFormat
}

type successorSchemaPlan struct {
	source schemaSource
	target schemaFormat
}

const successorSchemaVersion = 31

// selectSuccessorSchema is a dormant, read-only planning seam. The caller owns
// the transaction and fences schema writers before classification, and must call
// this BEFORE staging or TEMP snapshots. It neither runs nor authorizes migration.
// Even a database claiming tenant/31 is refused: its complete catalog validator
// and atomic publisher do not exist until phase 4. Never feed these source versions
// into the old v28->private29 owner or relabel them to reuse its staging helpers.
func selectSuccessorSchema(ctx context.Context, tx *sql.Tx) (successorSchemaPlan, error) {
	profile, err := classifySchemaSource(ctx, tx)
	if err != nil {
		return successorSchemaPlan{}, err
	}
	source, err := describeSchemaSource(profile)
	if err != nil {
		return successorSchemaPlan{}, err
	}
	return successorSchemaPlan{source, schemaFormat{"tenant", successorSchemaVersion}}, nil
}

func describeSchemaSource(profile string) (schemaSource, error) {
	var format schemaFormat
	switch profile {
	case "main28":
		format = schemaFormat{"main", 28}
	case "main29":
		format = schemaFormat{"main", 29}
	case "main30-pre-sync", "main30-initial-sync", "main30-devices":
		format = schemaFormat{"main", 30}
	case "private29":
		format = schemaFormat{"private-tenant", 29}
	default:
		return schemaSource{}, fmt.Errorf("unreviewed source profile %q", profile)
	}
	return schemaSource{profile, format}, nil
}

// Exact CONTROL SUBSET only, not the successor's full DDL manifest. In particular
// this does not approve any workload/FTS/ledger/sync objects. schema_version keeps
// its existing definition and history; provenance is a new immutable singleton.
// Literals are independent of mutable legacy/private29 target DDL. No initializer
// calls this function, and no publication helper is provided in phase 2.
func successorControlDefinitions() map[string]string {
	return map[string]string{
		"schema_version": `CREATE TABLE schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
)`,
		"schema_provenance": `CREATE TABLE schema_provenance (
  singleton INTEGER NOT NULL PRIMARY KEY CHECK (singleton = 1),
  source_profile TEXT NOT NULL,
  source_family TEXT NOT NULL,
  source_version INTEGER NOT NULL,
  target_family TEXT NOT NULL CHECK (target_family = 'tenant'),
  target_version INTEGER NOT NULL CHECK (target_version = 31),
  CHECK (
    (source_profile = 'main28' AND source_family = 'main' AND source_version = 28) OR
    (source_profile = 'main29' AND source_family = 'main' AND source_version = 29) OR
    (source_profile IN ('main30-pre-sync','main30-initial-sync','main30-devices') AND source_family = 'main' AND source_version = 30) OR
    (source_profile = 'private29' AND source_family = 'private-tenant' AND source_version = 29)
  )
) STRICT`,
		"schema_provenance_no_update":  `CREATE TRIGGER schema_provenance_no_update BEFORE UPDATE ON schema_provenance BEGIN SELECT RAISE(ABORT, 'schema provenance is immutable'); END`,
		"schema_provenance_no_delete":  `CREATE TRIGGER schema_provenance_no_delete BEFORE DELETE ON schema_provenance BEGIN SELECT RAISE(ABORT, 'schema provenance is immutable'); END`,
		"schema_provenance_no_replace": `CREATE TRIGGER schema_provenance_no_replace BEFORE INSERT ON schema_provenance WHEN EXISTS (SELECT 1 FROM schema_provenance) BEGIN SELECT RAISE(ABORT, 'schema provenance already exists'); END`,
	}
}

// readSuccessorProvenanceRecord validates ONLY the control record and version
// history of a purportedly published successor. Success does NOT establish a
// complete successor schema, original row preservation, integrity, authorization,
// roots/CAS readiness or runtime admission. A fabricated record is not evidence of
// migration: phase 4 must validate the entire catalog and preservation snapshots
// before publication. Committed repeat must validate everything, never repair.
// This helper is deliberately unused by constructors and receivers.
func readSuccessorProvenanceRecord(ctx context.Context, tx *sql.Tx) (schemaSource, error) {
	if ctx == nil || tx == nil {
		return schemaSource{}, fmt.Errorf("provenance inspection requires context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return schemaSource{}, err
	}
	var temporary int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_temp_schema").Scan(&temporary); err != nil {
		return schemaSource{}, err
	}
	if temporary != 0 {
		return schemaSource{}, fmt.Errorf("provenance inspection refuses temporary schema objects")
	}
	want := successorControlDefinitions()
	rows, err := tx.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM main.sqlite_schema
WHERE tbl_name IN ('schema_version','schema_provenance') OR name = 'schema_version'
OR lower(name) GLOB 'schema_provenance*'`)
	if err != nil {
		return schemaSource{}, err
	}
	for rows.Next() {
		var kind, name, table, ddl string
		if err := rows.Scan(&kind, &name, &table, &ddl); err != nil {
			_ = rows.Close()
			return schemaSource{}, err
		}
		expectedKind, expectedTable := "trigger", "schema_provenance"
		if name == "schema_version" || name == "schema_provenance" {
			expectedKind, expectedTable = "table", name
		}
		expected, ok := want[name]
		if !ok || kind != expectedKind || table != expectedTable || ddl != expected {
			_ = rows.Close()
			return schemaSource{}, fmt.Errorf("unreviewed successor control definition %s", name)
		}
		delete(want, name)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return schemaSource{}, err
	}
	if len(want) != 0 {
		return schemaSource{}, fmt.Errorf("missing successor control definitions")
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM main.schema_provenance").Scan(&count); err != nil {
		return schemaSource{}, err
	}
	if count != 1 {
		return schemaSource{}, fmt.Errorf("provenance requires exactly one record")
	}
	var singleton int
	var source schemaSource
	var target schemaFormat
	if err := tx.QueryRowContext(ctx, "SELECT singleton,source_profile,source_family,source_version,target_family,target_version FROM main.schema_provenance").Scan(
		&singleton, &source.profile, &source.format.family, &source.format.version, &target.family, &target.version); err != nil {
		return schemaSource{}, err
	}
	expected, err := describeSchemaSource(source.profile)
	if err != nil {
		return schemaSource{}, err
	}
	if singleton != 1 || source != expected || target != (schemaFormat{"tenant", successorSchemaVersion}) {
		return schemaSource{}, fmt.Errorf("inconsistent source/target provenance")
	}
	rows, err = tx.QueryContext(ctx, "SELECT version,typeof(version) FROM main.schema_version ORDER BY version")
	if err != nil {
		return schemaSource{}, err
	}
	defer rows.Close()
	prior, published := 0, false
	for rows.Next() {
		var version int
		var kind string
		if err := rows.Scan(&version, &kind); err != nil {
			return schemaSource{}, err
		}
		if kind != "integer" || version < 1 || (version > source.format.version && version != successorSchemaVersion) {
			return schemaSource{}, fmt.Errorf("unreviewed provenance version history")
		}
		if version == successorSchemaVersion {
			published = true
		} else {
			prior = version
		}
	}
	if err := rows.Err(); err != nil {
		return schemaSource{}, err
	}
	if !published || prior != source.format.version {
		return schemaSource{}, fmt.Errorf("source/publication version history mismatch")
	}
	return source, nil
}
