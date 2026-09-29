package storage

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// SearchNotes intentionally exposes neither total count nor BM25. Probe its
// existing snapshot matcher and the migration ranking helper without changing
// the public result shape or acquiring another pool connection inside the tx.
func tenantReceiverSearchProbe(t *testing.T, ctx context.Context, s *TenantStore, query string, limit int) ([]*NoteRow, []tenantFTSHit, int) {
	t.Helper()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	name, err := tenantSearchTable(ctx, tx, s.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tenantSearchSnapshot{tx: tx, owner: s.TenantID().String(), table: name}
	rows, count, err := snapshot.match(ctx, query, limit, nil)
	if err != nil {
		t.Fatal(err)
	}
	hits, hitCount, err := queryTenantFTS(ctx, tx, s.TenantID(), query, limit)
	if err != nil || hitCount != count || len(hits) != len(rows) {
		t.Fatalf("ranking/count probe: hits=%+v count=%d rows=%+v count=%d err=%v", hits, hitCount, rows, count, err)
	}
	for i := range rows {
		if hits[i].ID != rows[i].ID || hits[i].Score >= 0 {
			t.Fatalf("ranking/hydration mismatch at %d: hit=%+v row=%+v", i, hits[i], rows[i])
		}
	}
	return rows, hits, count
}

func TestTenantSearchPublicStrategyFallback(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, s := range []*TenantStore{a, b} {
		entry, err := s.InsertNote(ctx, sampleNote("projects/shared/strategy-entry.md", "strategy", "strategyneedle"))
		if err != nil {
			t.Fatal(err)
		}
		const attachmentPath = "projects/shared/strategy-attachment.md"
		addTenantSearchAttachment(t, owner, s, attachmentPath, "strategyneedle", "ready")
		attachment, err := s.GetNoteByPath(ctx, attachmentPath)
		if err != nil || attachment == nil {
			t.Fatalf("attachment note: %+v %v", attachment, err)
		}
		entry.MatchSource, attachment.MatchSource = "entry", "attachment"
		want := []*NoteRow{entry, attachment}
		for _, strategy := range []string{"fts", "match", "words", "attachment", "unknown"} {
			t.Run(s.TenantID().String()+"/"+strategy, func(t *testing.T) {
				opts := &SearchOptions{Strategy: strategy, Limit: 10}
				got, err := s.SearchNotes(ctx, "strategyneedle", opts)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) {
					t.Fatalf("strategy %q returned %d rows; want 2 (entry + attachment-only)", strategy, len(got))
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("strategy %q changed full rows/order/match sources: got %+v want %+v", strategy, got, want)
				}
				if opts.Strategy != strategy {
					t.Fatalf("mutated caller strategy: %q", opts.Strategy)
				}
			})
		}
	}
}

func TestTenantSearchReceiverSnapshot(t *testing.T) {
	owner, a, _ := migratedNoteStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var mode string
	if err := owner.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %q %v", mode, err)
	}
	var seq int
	var name, path string
	if err := owner.db.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	relationalExec(t, writer, "PRAGMA foreign_keys=ON")
	entry, err := a.InsertNote(ctx, sampleNote("projects/shared/snapshot.md", "snapshot", "snapshotneedle"))
	if err != nil {
		t.Fatal(err)
	}
	_, attachmentID := addTenantSearchAttachment(t, owner, a, "projects/shared/snapshot-attachment.md", "snapshotneedle", "ready")
	tx, err := owner.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	table, err := tenantSearchTable(ctx, tx, a.TenantID())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tenantSearchSnapshot{tx: tx, owner: a.TenantID().String(), table: table}
	before, count, err := snapshot.match(ctx, "snapshotneedle", 10, nil)
	if err != nil || count != 1 || !reflect.DeepEqual(before, []*NoteRow{entry}) {
		t.Fatalf("snapshot baseline: %+v count=%d err=%v", before, count, err)
	}
	attachments, err := snapshot.attachments(ctx, "snapshotneedle", 10, nil)
	if err != nil || len(attachments) != 1 {
		t.Fatalf("attachment baseline: %+v %v", attachments, err)
	}
	// The read snapshot is already established. Commit a real writer before
	// repeating count/hydration/attachment reads; no timing race or note trigger.
	wtx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer wtx.Rollback()
	if _, err := wtx.ExecContext(ctx, "UPDATE notes SET title='committedneedle',body='committed body' WHERE id=?", entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := wtx.ExecContext(ctx, "UPDATE attachment_derived SET text='committedneedle' WHERE attachment_id=?", attachmentID); err != nil {
		t.Fatal(err)
	}
	if err := wtx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, n, err := snapshot.match(ctx, "snapshotneedle", 10, nil)
	if err != nil || n != count || !reflect.DeepEqual(rows, before) {
		t.Fatalf("mixed count/hydration snapshot: %+v count=%d err=%v", rows, n, err)
	}
	gotAttachments, err := snapshot.attachments(ctx, "snapshotneedle", 10, nil)
	if err != nil || !reflect.DeepEqual(gotAttachments, attachments) {
		t.Fatalf("mixed attachment snapshot: %+v %v", gotAttachments, err)
	}
	if rows, n, err := snapshot.match(ctx, "committedneedle", 10, nil); err != nil || n != 0 || len(rows) != 0 {
		t.Fatalf("old snapshot saw new postings: %+v count=%d err=%v", rows, n, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if stale, err := a.SearchNotes(ctx, "snapshotneedle", nil); err != nil || len(stale) != 0 {
		t.Fatalf("fresh receiver saw stale postings/attachment: %+v %v", stale, err)
	}
	current, err := a.SearchNotes(ctx, "committedneedle", nil)
	if err != nil || len(current) != 2 || current[0].ID != entry.ID || current[0].Title != "committedneedle" || current[0].MatchSource != "entry" || current[1].MatchSource != "attachment" {
		t.Fatalf("fresh receiver missed committed entry/attachment: %+v %v", current, err)
	}
}

func TestTenantSearchReceiverForeignCorpusStability(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			owner, a, b := migratedNoteStores(t)
			if reverse {
				a, b = b, a
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var expected []*NoteRow
			for i, title := range []string{"stabilityneedle", "ordinary"} {
				n := sampleNote(fmt.Sprintf("projects/shared/stable%d.md", i), fmt.Sprintf("stable%02d", i), title)
				body := "stabilityneedle"
				n.Body = &body
				row, err := a.InsertNote(ctx, n)
				if err != nil {
					t.Fatal(err)
				}
				expected = append(expected, row)
			}
			baseline, scores, count := tenantReceiverSearchProbe(t, ctx, a, "stabilityneedle", 10)
			if count != 2 || !reflect.DeepEqual(baseline, expected) {
				t.Fatalf("baseline hydration/order: %+v want %+v count=%d", baseline, expected, count)
			}
			for _, row := range expected {
				row.MatchSource = "entry"
			}
			check := func() {
				t.Helper()
				for _, limit := range []int{1, 10} {
					rows, hits, n := tenantReceiverSearchProbe(t, ctx, a, "stabilityneedle", limit)
					end := min(limit, 2)
					if n != count || !reflect.DeepEqual(rows, baseline[:end]) || !reflect.DeepEqual(hits, scores[:end]) {
						t.Fatalf("foreign corpus changed rows/count/scores: rows=%+v count=%d hits=%+v", rows, n, hits)
					}
					for _, strategy := range []string{"fts", "unknown", "exact", "like"} {
						got, err := a.SearchNotes(ctx, "stabilityneedle", &SearchOptions{Strategy: strategy, Limit: limit})
						if err != nil || !reflect.DeepEqual(got, expected[:end]) {
							t.Fatalf("%s limit=%d full receiver rows=%+v want=%+v err=%v", strategy, limit, got, expected[:end], err)
						}
					}
				}
			}
			check()
			for _, stmt := range []string{
				`WITH RECURSIVE c(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM c WHERE x<100)
				 INSERT INTO notes(tenant_id,path,short_id,title,body)
				 SELECT ?, 'projects/shared/stable'||(x-1)||'.md','foreign','stabilityneedle',replace(hex(zeroblob(1000)),'0','stabilityneedle ') FROM c`,
				`UPDATE notes SET title='absent',body='unrelated',path=path||'-changed' WHERE tenant_id=?`,
				`DELETE FROM notes WHERE tenant_id=?`,
			} {
				if _, err := owner.db.ExecContext(ctx, stmt, b.TenantID().String()); err != nil {
					t.Fatal(err)
				}
				check()
			}
		})
	}
}

func TestTenantSearchReceiverConflictMaintenance(t *testing.T) {
	for _, recursive := range []string{"OFF", "ON"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("recursive=%s/reverse=%t", recursive, reverse), func(t *testing.T) {
				owner, a, b := migratedNoteStores(t)
				if reverse {
					a, b = b, a
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				relationalExec(t, owner.db, "PRAGMA recursive_triggers="+recursive)
				const path = "projects/shared/conflict.md"
				foreign, err := b.InsertNote(ctx, sampleNote(path, "foreign1", "originalneedle"))
				if err != nil {
					t.Fatal(err)
				}
				initial, err := a.InsertNote(ctx, sampleNote(path, "initial1", "originalneedle"))
				if err != nil {
					t.Fatal(err)
				}
				foreign.MatchSource = "entry"
				oldID := initial.ID
				oldTerm := "originalneedle"
				for _, step := range []struct {
					name, sql, term string
					changesID       bool
				}{
					{"upsert", `INSERT INTO notes(tenant_id,path,short_id,title,body,metadata) VALUES(?,?,'updated1','upsertneedle','upsertneedle body','{"changed":true}') ON CONFLICT(tenant_id,path) DO UPDATE SET short_id=excluded.short_id,title=excluded.title,body=excluded.body,metadata=excluded.metadata`, "upsertneedle", false},
					{"do-nothing", `INSERT INTO notes(tenant_id,path,short_id,title) VALUES(?,?,'ignored1','ignoredneedle') ON CONFLICT DO NOTHING`, "upsertneedle", false},
					{"replace-auto", `INSERT OR REPLACE INTO notes(tenant_id,path,short_id,title,body) VALUES(?,?,'replace1','replaceneedle','replaceneedle body')`, "replaceneedle", true},
					{"replace-explicit-negative", `INSERT OR REPLACE INTO notes(tenant_id,path,id,short_id,title,body) VALUES(?,?,-31,'negative','negativeneedle','negativeneedle body')`, "negativeneedle", true},
					{"update-id", `UPDATE notes SET id=-32,title='identityneedle',body='identityneedle body' WHERE tenant_id=? AND path=?`, "identityneedle", true},
					{"update-rowid", `UPDATE notes SET rowid=900001,title='rowidneedle',body='rowidneedle body' WHERE tenant_id=? AND path=?`, "rowidneedle", true},
				} {
					t.Run(step.name, func(t *testing.T) {
						if _, err := owner.db.ExecContext(ctx, step.sql, a.TenantID().String(), path); err != nil {
							t.Fatal(err)
						}
						tx, err := owner.db.BeginTx(ctx, nil)
						if err != nil {
							t.Fatal(err)
						}
						catalogErr := checkFinalTenantSearchSchema(tx)
						_ = tx.Rollback()
						if catalogErr != nil {
							t.Fatalf("legitimate mutation damaged catalog/postings: %v", catalogErr)
						}
						want, err := a.GetNoteByPath(ctx, path)
						if err != nil || want == nil {
							t.Fatalf("hydrate: %+v %v", want, err)
						}
						if want.Title != step.term || (want.ID != oldID) != step.changesID {
							t.Fatalf("mutation identity/title: %+v oldID=%d", want, oldID)
						}
						byID, err := a.GetNoteByShortID(ctx, want.ShortID)
						if err != nil || !reflect.DeepEqual(byID, want) {
							t.Fatalf("ID hydration: %+v want %+v err=%v", byID, want, err)
						}
						if step.changesID {
							var stale int
							if err := owner.db.QueryRowContext(ctx, "SELECT count(*) FROM notes WHERE id=?", oldID).Scan(&stale); err != nil || stale != 0 {
								t.Fatalf("old ID remains: %d %v", stale, err)
							}
						}
						rows, _, count := tenantReceiverSearchProbe(t, ctx, a, step.term, 10)
						if count != 1 || !reflect.DeepEqual(rows, []*NoteRow{want}) {
							t.Fatalf("snapshot hydration: %+v count=%d want %+v", rows, count, want)
						}
						want.MatchSource = "entry"
						for _, strategy := range []string{"fts", "exact", "like"} {
							got, err := a.SearchNotes(ctx, step.term, &SearchOptions{Strategy: strategy})
							if err != nil || !reflect.DeepEqual(got, []*NoteRow{want}) {
								t.Fatalf("%s hydration: %+v want %+v err=%v", strategy, got, want, err)
							}
							if oldTerm != step.term {
								if stale, err := a.SearchNotes(ctx, oldTerm, &SearchOptions{Strategy: strategy}); err != nil || len(stale) != 0 {
									t.Fatalf("stale %s term: %+v %v", strategy, stale, err)
								}
							}
						}
						got, err := b.SearchNotes(ctx, "originalneedle", nil)
						if err != nil || !reflect.DeepEqual(got, []*NoteRow{foreign}) {
							t.Fatalf("foreign same-path row changed: %+v %v", got, err)
						}
						if ignored, err := a.SearchNotes(ctx, "ignoredneedle", nil); err != nil || len(ignored) != 0 {
							t.Fatalf("DO NOTHING indexed excluded title: %+v %v", ignored, err)
						}
						oldID, oldTerm = want.ID, step.term
					})
					if t.Failed() {
						t.FailNow()
					}
				}
			})
		}
	}
}
