package service

import (
	"context"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func (s *BrainServiceImpl) EntryChanges(ctx context.Context, epoch string, after int64, limit int) (*types.EntryChanges, error) {
	p, err := s.storage.ReadEntryChanges(ctx, epoch, after, limit)
	if err != nil {
		return nil, err
	}
	out := &types.EntryChanges{Epoch: p.Epoch, Cursor: p.Cursor, More: p.More, Changes: []types.EntryChange{}}
	for _, r := range p.Rows {
		c := types.EntryChange{Path: r.Path, Deleted: r.Note == nil}
		if r.Note != nil {
			e := NoteRowToBrainEntry(r.Note)
			if r.Note.RawContent != nil {
				c.Raw = *r.Note.RawContent
			}
			e.Revision = indexedEntryRevision(r.Note, c.Raw)
			c.Entry = &e
		}
		out.Changes = append(out.Changes, c)
	}
	return out, nil
}
func (s *BrainServiceImpl) ReserveSyncOperation(ctx context.Context, id, hash string) (*storage.SyncReceipt, error) {
	return s.storage.ReserveSyncOperation(ctx, id, hash)
}
func (s *BrainServiceImpl) CompleteSyncOperation(ctx context.Context, id string, status int, body string) error {
	return s.storage.CompleteSyncOperation(ctx, id, status, body)
}

func (s *BrainServiceImpl) SyncDevices(ctx context.Context) ([]types.SyncDevice, error) {
	return s.storage.SyncDevices(ctx)
}
func (s *BrainServiceImpl) SaveSyncDevice(ctx context.Context, before *types.SyncDevice, after types.SyncDevice) error {
	return s.storage.SaveSyncDevice(ctx, before, after)
}
func (s *BrainServiceImpl) SyncEntryVersion(ctx context.Context, path string) (string, string, error) {
	n, err := s.storage.SyncNote(ctx, path)
	if err != nil {
		return "", "", err
	}
	if n == nil {
		return "", "", nil
	}
	raw := ""
	if n.RawContent != nil {
		raw = *n.RawContent
	}
	return raw, indexedEntryRevision(n, raw), nil
}
