package tenant

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

var _ driver.Valuer = ID{}
var _ sql.Scanner = (*ID)(nil)

func TestIDContract(t *testing.T) {
	good := []string{"a", "0", "ab", "a-b", "a--b", "team-123", "550e8400-e29b-41d4-a716-446655440000", strings.Repeat("a", 40)}
	bad := []string{"", ".", "..", "/", "-", "-a", "a-", "A", "aB", " a", "a ", "a\n", "a\t", "a_b", "a.b", "a/b", "../a", "a\\b", "a%2fb", "a\x00b", "é", "Ａ", string([]byte{0xff}), strings.Repeat("a", 41), "local", "system", "tenants", "global", "projects", "attachments", "api", "mcp", "health", "admin", "brain-data"}
	for _, s := range good {
		t.Run("valid/"+s, func(t *testing.T) {
			id, err := Parse(s)
			if err != nil || !id.Valid() || id.String() != s {
				t.Fatalf("Parse(%q) = %#v, %v; want unchanged valid ID", s, id, err)
			}
			if MustParse(s) != id {
				t.Fatal("MustParse disagrees")
			}
			b, err := json.Marshal(id)
			if err != nil || string(b) != `"`+s+`"` {
				t.Fatalf("marshal = %s, %v", b, err)
			}
			var decoded ID
			if err := json.Unmarshal(b, &decoded); err != nil || decoded != id {
				t.Fatalf("JSON round trip = %#v, %v", decoded, err)
			}
			v, err := id.Value()
			if err != nil || v != s {
				t.Fatalf("Value = %v, %v", v, err)
			}
			for _, src := range []any{v, []byte(s)} {
				if err := decoded.Scan(src); err != nil || decoded != id {
					t.Fatalf("SQL round trip = %#v, %v", decoded, err)
				}
			}
		})
	}
	for _, s := range bad {
		t.Run("invalid/"+s, func(t *testing.T) {
			id, err := Parse(s)
			if err == nil || id != (ID{}) {
				t.Fatalf("Parse(%q) = %#v, %v; want zero and error", s, id, err)
			}
			defer func() {
				if recover() == nil {
					t.Error("MustParse must panic")
				}
			}()
			MustParse(s)
		})
	}
	if (ID{}).Valid() || (ID{}).String() != "" {
		t.Fatal("zero must be invalid")
	}
	if !Local.Valid() || Local.String() != LocalID {
		t.Fatal("bootstrap sentinel must be valid")
	}
	for _, s := range append(bad, "UPPER") {
		if s == LocalID {
			continue // Reserved for provisioning, but canonical in serialization.
		}
		corrupt := ID{v: s}
		if corrupt.Valid() {
			t.Errorf("corrupt ID %q valid", s)
		}
		if _, err := corrupt.MarshalJSON(); err == nil {
			t.Errorf("marshal corrupt/reserved %q succeeded", s)
		}
		if _, err := corrupt.Value(); err == nil {
			t.Errorf("value corrupt/reserved %q succeeded", s)
		}
		b, _ := json.Marshal(s)
		for _, initial := range []ID{Local, MustParse("existing")} {
			dst := initial
			if err := dst.UnmarshalJSON(b); err == nil || dst != (ID{}) {
				t.Errorf("decoded invalid %q: %#v, %v", s, dst, err)
			}
			for _, src := range []any{s, []byte(s)} {
				dst = initial
				if err := dst.Scan(src); err == nil || dst != (ID{}) {
					t.Errorf("scanned invalid %q: %#v, %v", s, dst, err)
				}
			}
		}
	}
}

func TestDecodeErrorsClearReceiver(t *testing.T) {
	for _, initial := range []ID{Local, MustParse("existing")} {
		for _, b := range []string{"null", "123", "true", "{}", "[]", "garbage", `"ok" garbage`, `"unterminated`, `"Local"`, `" local"`, `"local "`} {
			t.Run(b, func(t *testing.T) {
				id := initial
				if err := id.UnmarshalJSON([]byte(b)); err == nil || id != (ID{}) {
					t.Fatalf("decode = %#v, %v; want error and cleared ID", id, err)
				}
			})
		}
		for _, src := range []any{nil, 123, int64(1), true, 1.2, ID{v: "valid"}, []byte(nil), "Local", " local", "local "} {
			id := initial
			if err := id.Scan(src); err == nil || id != (ID{}) {
				t.Errorf("Scan(%T) = %#v, %v", src, id, err)
			}
		}
	}
	var nilID *ID
	if err := nilID.Scan("valid"); err == nil {
		t.Error("nil Scan must error")
	}
	if err := nilID.UnmarshalJSON([]byte(`"valid"`)); err == nil {
		t.Error("nil Unmarshal must error")
	}
}

func TestIDLocalJSONRoundTrip(t *testing.T) {
	b, err := json.Marshal(Local)
	if err != nil || string(b) != `"local"` {
		t.Errorf("marshal Local = %s, %v; want exact canonical string", b, err)
	}
	var decoded ID
	if err := json.Unmarshal([]byte(`"local"`), &decoded); err != nil || decoded != Local {
		t.Errorf("decode local = %#v, %v", decoded, err)
	}
	for _, src := range []any{LocalID, []byte(LocalID)} {
		if err := decoded.Scan(src); err != nil || decoded != Local {
			t.Errorf("Scan(%T) = %#v, %v", src, decoded, err)
		}
	}
}

func TestIDSQLiteRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec("CREATE TABLE tenant_ids (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []ID{Local, MustParse("team-123")} {
		t.Run(id.String(), func(t *testing.T) {
			result, err := db.Exec("INSERT INTO tenant_ids (id) VALUES (?)", id)
			if err != nil {
				t.Fatal(err)
			}
			rowID, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			var stored string
			var decoded ID
			if err := db.QueryRow("SELECT id, id FROM tenant_ids WHERE rowid = ?", rowID).Scan(&stored, &decoded); err != nil {
				t.Fatal(err)
			}
			if stored != id.String() || decoded != id {
				t.Fatalf("stored = %q, decoded = %#v; want %#v", stored, decoded, id)
			}
		})
	}
	for _, s := range []string{"", "Local", " local", "local ", "local\n", "../local", "system", "tenants", "global", "projects", "attachments", "api", "mcp", "health", "admin", "brain-data"} {
		t.Run("invalid/"+s, func(t *testing.T) {
			invalid := ID{v: s}
			if b, err := json.Marshal(invalid); err == nil || b != nil {
				t.Errorf("marshal invalid = %s, %v", b, err)
			}
			if v, err := invalid.Value(); err == nil || v != nil {
				t.Errorf("invalid Value = %v, %v", v, err)
			}
			if _, err := db.Exec("INSERT INTO tenant_ids (id) VALUES (?)", invalid); err == nil {
				t.Fatal("bound invalid ID inserted successfully")
			}
			// Bypass Value to simulate corrupt persisted text, then exercise Scanner.
			result, err := db.Exec("INSERT INTO tenant_ids (id) VALUES (?)", s)
			if err != nil {
				t.Fatal(err)
			}
			rowID, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			for _, initial := range []ID{Local, MustParse("existing")} {
				decoded := initial
				if err := db.QueryRow("SELECT id FROM tenant_ids WHERE rowid = ?", rowID).Scan(&decoded); err == nil || decoded != (ID{}) {
					t.Errorf("scan invalid = %#v, %v; want zero and error", decoded, err)
				}
			}
		})
	}
}
