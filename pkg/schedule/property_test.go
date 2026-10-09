package schedule

import (
	"context"
	"math/rand"
	"testing"
	"time"
)

// LatestSlot and NextSlot must describe the same sequence of slots: from any
// instant, the next slot after the latest one is the following slot, nothing
// lies strictly between them, and each is its own latest slot.
func TestLatestNextSlotAgree(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 1, 5, 9, 0, 0, 0, ny)
	weekdays := DayFilterFunc(func(_ context.Context, day time.Time) (bool, error) {
		wd := day.Weekday()
		return wd != time.Saturday && wd != time.Sunday, nil
	})
	specs := map[string]Spec{
		"cron with timezone":     {Cron: "*/7 1-4 * * *", Timezone: "America/New_York"},
		"cron dom or dow":        {Cron: "30 2 1,15 * 1", Timezone: "Europe/London"},
		"every 4d at 03:00":      {Every: "4d", At: "03:00", Timezone: "America/New_York", Anchor: anchor},
		"every 2w":               {Every: "2w", At: "02:30", Timezone: "America/New_York", Anchor: anchor},
		"every 90m":              {Every: "90m", Anchor: anchor},
		"weekday filtered cron":  {Cron: "0 3 * * *", Timezone: "America/New_York", DayFilters: []DayFilter{weekdays}},
		"weekday filtered every": {Every: "1d", At: "02:30", Timezone: "America/New_York", Anchor: anchor, DayFilters: []DayFilter{weekdays}},
	}
	ctx := context.Background()
	rng := rand.New(rand.NewSource(20261009))
	start := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	span := int64(400 * 24 * time.Hour)

	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			sched, err := Compile(spec)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			for i := 0; i < 300; i++ {
				now := start.Add(time.Duration(rng.Int63n(span)))
				offset := StaggerOffset("auto", string(rune('a'+i%26)), 2*time.Hour)

				latest, ok, err := sched.LatestSlot(ctx, now, offset)
				if err != nil || !ok {
					t.Fatalf("LatestSlot(%v): ok=%v err=%v", now, ok, err)
				}
				if latest.At.After(now) {
					t.Fatalf("LatestSlot(%v).At = %v is after now", now, latest.At)
				}
				if got := latest.At.Sub(latest.Base); got != offset {
					t.Fatalf("slot offset = %v, want %v", got, offset)
				}

				next, ok, err := sched.NextSlot(ctx, latest.At, offset)
				if err != nil || !ok {
					t.Fatalf("NextSlot(%v): ok=%v err=%v", latest.At, ok, err)
				}
				if !next.At.After(now) {
					t.Fatalf("NextSlot(LatestSlot(%v)) = %v, not after now: a slot was skipped", now, next.At)
				}

				if again, _, _ := sched.LatestSlot(ctx, next.At, offset); !again.At.Equal(next.At) {
					t.Fatalf("LatestSlot(%v) = %v, want the slot itself", next.At, again.At)
				}
				if before, _, _ := sched.LatestSlot(ctx, next.At.Add(-time.Nanosecond), offset); !before.At.Equal(latest.At) {
					t.Fatalf("LatestSlot just before %v = %v, want %v (nothing in between)", next.At, before.At, latest.At)
				}
			}
		})
	}
}
