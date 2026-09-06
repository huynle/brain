package indexer

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/pkg/markdown"
)

// BenchmarkBootAttachmentTree measures a restart boot with an up-to-date index,
// not just discovery. Both arms query checksums, parse every note, compare them,
// and check for deletions. Setup, DB seeding and cleanup are outside timing.
// Run: go test ./internal/indexer -run '^$' -bench '^BenchmarkBootAttachmentTree$' -benchtime=3x -count=5
// These are warm-filesystem measurements, not cold OS-cache measurements.
func BenchmarkBootAttachmentTree(b *testing.B) {
	root := b.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	// 128 first-level shards, 128 second-level shards each, one hash blob each.
	for a := 0; a < 128; a++ {
		for c := 0; c < 128; c++ {
			write(fmt.Sprintf("attachments/%02x/%02x/%064x", a, c, a*128+c), "attachment payload")
		}
	}
	const notes = 256
	for n := 0; n < notes; n++ {
		prefix := "projects/bench/note"
		if n%2 == 0 {
			prefix = "global/note"
		}
		write(fmt.Sprintf("%s/%08x.md", prefix, n), noteContent(fmt.Sprintf("Note %d", n), "bench"))
	}
	var dirs, files int
	if err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	b.Logf("identical fixture: %d directories (including root), %d files (%d markdown, 16384 blobs)", dirs, files, notes)
	for _, old := range []bool{true, false} {
		name := "AllowlistedIndexChanged"
		if old {
			name = "OldFullTreeBoot"
		}
		b.Run(name, func(b *testing.B) {
			b.StopTimer()
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				b.Fatal(err)
			}
			store, err := storage.NewWithDB(db)
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			idx := NewIndexer(root, store)
			if r, err := idx.IndexChanged(); err != nil || r.Added != notes || len(r.Errors) != 0 {
				b.Fatalf("seed: %+v, %v", r, err)
			}
			b.ResetTimer()
			b.StartTimer()
			for i := 0; i < b.N; i++ {
				var r *IndexResult
				if old {
					r, err = oldUnchangedBoot(idx)
				} else {
					r, err = idx.IndexChanged()
				}
				if err != nil || r.Skipped != notes || r.Added != 0 || r.Updated != 0 || r.Deleted != 0 || len(r.Errors) != 0 {
					b.Fatalf("boot: %+v, %v", r, err)
				}
			}
			b.StopTimer()
		})
	}
}

// oldUnchangedBoot emulates pre-allowlist IndexChanged on an unchanged fixture.
// It deliberately fails on changed files rather than silently omitting indexing
// work. Do not use it to benchmark first-time/modified-index boots. No production
// hook or instrumentation is needed to restore the old full-tree discovery.
func oldUnchangedBoot(idx *Indexer) (*IndexResult, error) {
	var files []string
	err := filepath.WalkDir(idx.brainDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(idx.brainDir, path)
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(rel, ".md") {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	diskSet := make(map[string]bool, len(files))
	for _, f := range files {
		diskSet[f] = true
	}
	rows, err := idx.storage.DB().Query("SELECT path, checksum FROM notes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dbMap := make(map[string]*string)
	for rows.Next() {
		var path string
		var checksum *string
		if err := rows.Scan(&path, &checksum); err != nil {
			return nil, err
		}
		dbMap[path] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	r := &IndexResult{}
	for _, file := range files {
		pf, err := markdown.ParseFile(file, idx.brainDir)
		if err != nil {
			return nil, err
		}
		checksum, exists := dbMap[file]
		if !exists || checksum == nil || *checksum != pf.Checksum {
			return nil, fmt.Errorf("fixture changed: %s", file)
		}
		r.Skipped++
	}
	for path := range dbMap {
		if !diskSet[path] {
			return nil, fmt.Errorf("fixture deleted: %s", path)
		}
	}
	return r, nil
}
