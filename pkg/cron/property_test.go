package cron

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// Zones chosen for awkward transitions: hour-long DST both ways (New York,
// London), 30-minute DST (Lord Howe), a 45-minute offset with DST at 02:45
// (Chatham), clocks that move at midnight (Havana, Santiago), southern
// hemisphere DST (Sydney), and fixed offsets (UTC, Kolkata).
var propertyZones = []string{
	"UTC", "America/New_York", "Europe/London", "Australia/Lord_Howe",
	"Pacific/Chatham", "America/Havana", "America/Santiago",
	"Australia/Sydney", "Asia/Kolkata",
}

// randomField returns a random, valid cron field over [lo, hi].
func randomField(r *rand.Rand, lo, hi int) string {
	span := hi - lo + 1
	v := func() int { return lo + r.IntN(span) }
	switch r.IntN(8) {
	case 0, 1:
		return "*"
	case 2:
		return fmt.Sprintf("*/%d", 1+r.IntN(span))
	case 3:
		return fmt.Sprint(v())
	case 4:
		a, b := v(), v()
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d-%d", a, b)
	case 5:
		a, b := v(), v()
		if a > b {
			a, b = b, a
		}
		return fmt.Sprintf("%d-%d/%d", a, b, 1+r.IntN(4))
	case 6:
		return fmt.Sprintf("%d,%d,%d", v(), v(), v())
	default:
		return fmt.Sprintf("%d/%d", v(), 1+r.IntN(span))
	}
}

func randomExpr(r *rand.Rand) string {
	return strings.Join([]string{
		randomField(r, 0, 59),
		randomField(r, 0, 23),
		randomField(r, 1, 31),
		randomField(r, 1, 12),
		randomField(r, 0, 7),
	}, " ")
}

// randomInstant picks an instant in 2020-2030, half the time within three
// hours of one of the zone's offset transitions, with random seconds.
func randomInstant(r *rand.Rand, loc *time.Location) time.Time {
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t := base.Add(time.Duration(r.Int64N(int64(11 * 365 * 24 * time.Hour))))
	if r.IntN(2) == 0 {
		if start, _ := t.In(loc).ZoneBounds(); !start.IsZero() {
			t = start.Add(time.Duration(r.Int64N(int64(6*time.Hour))) - 3*time.Hour)
		}
	}
	return t.Add(time.Duration(r.Int64N(int64(time.Minute)))).In(loc)
}

func isWholeMinute(t time.Time) bool { return t.Second() == 0 && t.Nanosecond() == 0 }

// For many random expressions, zones and instants, NextAfter and
// PrevAtOrBefore must describe the same set of firing instants:
//
//	n = NextAfter(t):      n > t, Matches(n), PrevAtOrBefore(n) == n
//	p = PrevAtOrBefore(t): p <= t, Matches(p), NextAfter(p - 1m) == p
//	both defined:          NextAfter(p) == n   (nothing fires strictly between)
func TestProperty_NextAfterAndPrevAtOrBeforeAgree(t *testing.T) {
	r := rand.New(rand.NewPCG(20261008, 7))
	zones := make([]*time.Location, len(propertyZones))
	for i, z := range propertyZones {
		zones[i] = mustZone(t, z)
	}

	const cases = 3000
	defined := 0
	for i := 0; i < cases; i++ {
		expr := randomExpr(r)
		s := mustParse(t, expr)
		loc := zones[r.IntN(len(zones))]
		at := randomInstant(r, loc)
		ctx := fmt.Sprintf("case %d: %q in %s at %v", i, expr, loc, at)

		n := s.NextAfter(at)
		p := s.PrevAtOrBefore(at)

		if !n.IsZero() {
			if !n.After(at) || !s.Matches(n) || !isWholeMinute(n) || n.Location() != loc {
				t.Fatalf("%s: NextAfter = %v: must be a later, matching, whole minute in %s", ctx, n, loc)
			}
			if back := s.PrevAtOrBefore(n); !back.Equal(n) {
				t.Fatalf("%s: PrevAtOrBefore(NextAfter = %v) = %v, want %v", ctx, n, back, n)
			}
		}
		if !p.IsZero() {
			if p.After(at) || !s.Matches(p) || !isWholeMinute(p) || p.Location() != loc {
				t.Fatalf("%s: PrevAtOrBefore = %v: must be an earlier-or-equal, matching, whole minute in %s", ctx, p, loc)
			}
			if fwd := s.NextAfter(p.Add(-time.Minute)); !fwd.Equal(p) {
				t.Fatalf("%s: NextAfter(PrevAtOrBefore - 1m = %v) = %v, want %v", ctx, p.Add(-time.Minute), fwd, p)
			}
		}
		if !n.IsZero() && !p.IsZero() {
			defined++
			if fwd := s.NextAfter(p); !fwd.Equal(n) {
				t.Fatalf("%s: NextAfter(p = %v) = %v, but NextAfter(t) = %v: a run between p and n was missed", ctx, p, fwd, n)
			}
		}
	}
	// Guard against a generator that only produces impossible expressions.
	if defined < cases/2 {
		t.Fatalf("only %d of %d cases had both directions defined; generator too sparse", defined, cases)
	}
}

// Brute-force oracle: walk real instants minute by minute and ask Matches.
// The first hit in each direction must be exactly what the smart searches
// return; with no hit inside the window, the answer must lie beyond it.
func TestProperty_AgreesWithMinuteWalk(t *testing.T) {
	r := rand.New(rand.NewPCG(20261009, 11))
	zones := make([]*time.Location, len(propertyZones))
	for i, z := range propertyZones {
		zones[i] = mustZone(t, z)
	}

	const cases = 250
	const window = 3 * 24 * 60 // minutes each way
	for i := 0; i < cases; i++ {
		// Bias toward frequently firing schedules so the window usually
		// contains hits: keep day and month fields wide.
		expr := strings.Join([]string{
			randomField(r, 0, 59), randomField(r, 0, 23), "*", "*", randomField(r, 0, 7),
		}, " ")
		if r.IntN(3) == 0 {
			expr = randomExpr(r)
		}
		s := mustParse(t, expr)
		loc := zones[r.IntN(len(zones))]
		at := randomInstant(r, loc)
		ctx := fmt.Sprintf("case %d: %q in %s at %v", i, expr, loc, at)
		floor := at.Truncate(time.Minute) // every zone here has whole-minute offsets

		wantPrev := time.Time{}
		for k := 0; k <= window; k++ {
			if c := floor.Add(-time.Duration(k) * time.Minute); s.Matches(c) {
				wantPrev = c
				break
			}
		}
		gotPrev := s.PrevAtOrBefore(at)
		if !wantPrev.IsZero() && !gotPrev.Equal(wantPrev) {
			t.Fatalf("%s: PrevAtOrBefore = %v, minute walk found %v", ctx, gotPrev, wantPrev)
		}
		if wantPrev.IsZero() && !gotPrev.IsZero() && !gotPrev.Before(floor.Add(-window*time.Minute)) {
			t.Fatalf("%s: PrevAtOrBefore = %v inside a window where the minute walk found nothing", ctx, gotPrev)
		}

		wantNext := time.Time{}
		for k := 1; k <= window; k++ {
			if c := floor.Add(time.Duration(k) * time.Minute); s.Matches(c) {
				wantNext = c
				break
			}
		}
		gotNext := s.NextAfter(at)
		if !wantNext.IsZero() && !gotNext.Equal(wantNext) {
			t.Fatalf("%s: NextAfter = %v, minute walk found %v", ctx, gotNext, wantNext)
		}
		if wantNext.IsZero() && !gotNext.IsZero() && !gotNext.After(floor.Add(window*time.Minute)) {
			t.Fatalf("%s: NextAfter = %v inside a window where the minute walk found nothing", ctx, gotNext)
		}
	}
}
