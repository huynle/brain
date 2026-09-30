package storage

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The child only accepts a database allocated by this test; never a deployment path.
func TestTenantRelationalInterruptedRestart(t *testing.T) {
	db := relationalFixture(t)
	var path string
	var seq int
	var name string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, db, "PRAGMA wal_autocheckpoint=0")
	// Close the seed connection before rehearsal; this also checkpoints its WAL.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = compatibilityDB(t, path)
	db.SetMaxOpenConns(1)
	before := map[string][]string{}
	for _, table := range append(append([]string{"sqlite_schema", "sqlite_sequence"}, relationalTenantTables...), relationalControlTables...) {
		before[table] = relationalSnapshot(t, db, table)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTenantRelationalRehearsalChild$")
	cmd.Env = append(os.Environ(), "BRAIN_P4_REHEARSAL_CHILD="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	scanner := bufio.NewScanner(out)
	ready := scanner.Scan() && scanner.Text() == "P4_STAGED_UNCOMMITTED"
	if !ready {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child did not prove staged uncommitted relational schema")
	}
	t.Logf("staged after %s; %s", time.Since(start), rehearsalSizes(t, path))
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("expected spilled WAL: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child unexpectedly exited successfully")
	}
	db = compatibilityDB(t, path)
	db.SetMaxOpenConns(1)
	for table, want := range before {
		if got := relationalSnapshot(t, db, table); !reflect.DeepEqual(got, want) {
			t.Fatalf("crash changed %s", table)
		}
	}
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("FK restoration: %d %v", fk, err)
	}
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	if v, err := GetSchemaVersion(db); err != nil || v != 28 {
		t.Fatalf("version=%d %v", v, err)
	}
	t.Logf("SIGKILL recovery: all baseline rows/catalog/sequences restored; repeat staging and rollback passed; %s", rehearsalSizes(t, path))
}

func rehearsalSizes(t testing.TB, path string) string {
	t.Helper()
	result := ""
	for _, suffix := range []string{"", "-wal", "-shm"} {
		var size int64
		info, err := os.Stat(path + suffix)
		if err == nil {
			size = info.Size()
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		result += fmt.Sprintf("%s=%d bytes ", filepath.Base(path+suffix), size)
	}
	return result
}

// Opt-in: no caller-supplied paths, network, markdown tree or actual blob bytes.
func BenchmarkTenantRelational78952(b *testing.B) {
	if os.Getenv("BRAIN_P4_SYNTHETIC_SCALE") != "1" {
		b.Skip("set BRAIN_P4_SYNTHETIC_SCALE=1 for synthetic scale rehearsal")
	}
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		path := filepath.Join(b.TempDir(), "synthetic.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			b.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		b.Cleanup(func() { _ = db.Close() })
		if err := InitSchema(db); err != nil {
			b.Fatal(err)
		}
		execSQL := func(q string) {
			if _, err := db.Exec(q); err != nil {
				b.Fatal(err)
			}
		}
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			b.Fatal(err)
		}
		execSQL("PRAGMA wal_autocheckpoint=0")
		execSQL(`
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<78952)
INSERT INTO notes(id,path,short_id,title,body,metadata,project_id,type)
SELECT x,printf('projects/p%d/note/%08d.md',x%100,x),printf('%08d',x),printf('Synthetic note %d',x),
printf('Design review %d: ownership migration recovery. %s',x,replace(hex(zeroblob(256)), '00', 'text ')), '{"status":"active","tags":["synthetic"]}',printf('p%d',x%100),'note' FROM n;
INSERT INTO tags(note_id,tag) SELECT id,'synthetic' FROM notes;
INSERT INTO links(source_id,target_path,target_id,href) SELECT id,path,id,path FROM notes WHERE id%10=0;
INSERT INTO note_embeddings(note_id,chunk_index,embedding) SELECT id,0,zeroblob(1536) FROM notes WHERE id%10=0;
INSERT INTO note_embeddings_meta(note_id,chunk_index) SELECT id,0 FROM notes WHERE id%10=0;
INSERT INTO attachments(id,digest,size) SELECT id,printf('%064x',id),4096 FROM notes WHERE id%100=0;
INSERT INTO entry_attachments(note_id,attachment_id) SELECT id,id FROM attachments;
INSERT INTO attachment_derived(attachment_id,text) SELECT id,'Synthetic extracted text' FROM attachments;
INSERT INTO entry_meta(path) VALUES('brain:system/install_claimed');
`)
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
		db, err = sql.Open("sqlite", path)
		if err != nil {
			b.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		execSQL("PRAGMA wal_autocheckpoint=0")
		execSQL("PRAGMA foreign_keys=OFF")
		b.Logf("baseline %s", rehearsalSizes(b, path))
		tx, err := db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		start := time.Now()
		if err := stageTenantRelationalSchema(tx); err != nil {
			b.Fatal(err)
		}
		elapsed := time.Since(start)
		b.StopTimer()
		b.Logf("staging including integrity/FK validation: %s; uncommitted %s", elapsed, rehearsalSizes(b, path))
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM notes WHERE tenant_id='local'").Scan(&count); err != nil || count != 78952 {
			b.Fatalf("count=%d err=%v", count, err)
		}
		if err := tx.Rollback(); err != nil {
			b.Fatal(err)
		}
		b.Logf("rollback %s", rehearsalSizes(b, path))
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
