package service

import (
	"context"
	"errors"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/types"
)

// BrainLister lists entries. BrainServiceImpl implements it.
type BrainLister interface {
	List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)
}

// calendarReferencePageSize bounds one automation page, like the other
// automation scans in this package.
const calendarReferencePageSize = 200

// NewCalendarReferenceFinder returns the calendar.ReferenceFinder that reports
// every automation field naming a calendar source. It scans automations of
// every status, bindings included, by paging through List without a status
// filter. It makes no storage call of its own and never calls the calendar
// manager, so removal can run it while holding the manager's lock.
func NewCalendarReferenceFinder(lister BrainLister) calendar.ReferenceFinder {
	return func(ctx context.Context, name string) ([]calendar.Reference, error) {
		if lister == nil {
			return nil, errors.New("calendar reference finder has no entry lister")
		}
		var refs []calendar.Reference
		for offset := 0; ; offset += calendarReferencePageSize {
			resp, err := lister.List(ctx, types.ListEntriesRequest{
				Type:   "automation",
				Limit:  calendarReferencePageSize,
				Offset: offset,
			})
			if err != nil {
				return nil, err
			}
			if resp == nil {
				return refs, nil
			}
			for _, entry := range resp.Entries {
				refs = appendCalendarReferences(refs, entry, name)
			}
			if len(resp.Entries) < calendarReferencePageSize {
				return refs, nil
			}
		}
	}
}

// appendCalendarReferences adds one reference per trigger field of entry that
// names the calendar source.
func appendCalendarReferences(refs []calendar.Reference, entry types.BrainEntry, name string) []calendar.Reference {
	tc := entry.Trigger
	if tc == nil {
		return refs
	}
	add := func(field string) {
		refs = append(refs, calendar.Reference{
			ID:    entry.ID,
			Path:  entry.Path,
			Title: entry.Title,
			Field: field,
		})
	}
	if tc.Calendar == name {
		add("trigger.calendar")
	}
	if tc.SkipIfEvent != nil && tc.SkipIfEvent.Calendar == name {
		add("trigger.skip_if_event")
	}
	if tc.OnlyIfEvent != nil && tc.OnlyIfEvent.Calendar == name {
		add("trigger.only_if_event")
	}
	return refs
}
