package schedule

import (
	"testing"
	"time"
)

func TestParseCatchUp(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "none", want: "none"},
		{in: "10m", want: "10m0s"},
		{in: "1h30m", want: "1h30m0s"},
		{in: "0s", want: "0s"},
		{in: "-5m", wantErr: true},
		{in: "soon", wantErr: true},
		{in: "None", wantErr: true},
	} {
		got, err := ParseCatchUp(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseCatchUp(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCatchUp(%q): %v", tc.in, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("ParseCatchUp(%q).String() = %q, want %q", tc.in, got.String(), tc.want)
		}
	}
}

func TestCatchUpAllows(t *testing.T) {
	slot := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	mustParse := func(s string) CatchUp {
		c, err := ParseCatchUp(s)
		if err != nil {
			t.Fatalf("ParseCatchUp(%q): %v", s, err)
		}
		return c
	}
	for _, tc := range []struct {
		name     string
		catchUp  string
		lateness time.Duration
		want     bool
	}{
		{"default on time", "", 0, true},
		{"default days late", "", 72 * time.Hour, true},
		{"default not yet", "", -time.Second, false},
		{"none on time", "none", 0, true},
		{"none within the tick", "none", 59 * time.Second, true},
		{"none one tick late", "none", time.Minute, false},
		{"cap within", "10m", 9 * time.Minute, true},
		{"cap at limit", "10m", 10 * time.Minute, false},
		{"cap beyond", "10m", time.Hour, false},
		{"cap not yet", "10m", -time.Second, false},
		{"zero cap is one tick", "0s", 30 * time.Second, true},
		{"zero cap past the tick", "0s", time.Minute, false},
		{"sub-minute cap is one tick", "10s", 45 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustParse(tc.catchUp).Allows(slot, slot.Add(tc.lateness)); got != tc.want {
				t.Errorf("Allows(lateness %v) = %v, want %v", tc.lateness, got, tc.want)
			}
		})
	}
}

func TestDue(t *testing.T) {
	at := time.Date(2026, 10, 9, 3, 17, 23, 0, time.UTC)
	slot := Slot{Base: at.Add(-17*time.Minute - 23*time.Second), At: at}
	none, err := ParseCatchUp("none")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		floor time.Time
		now   time.Time
		c     CatchUp
		want  bool
	}{
		{"zero floor on time", time.Time{}, at.Add(37 * time.Second), CatchUp{}, true},
		{"floor before slot", at.Add(-24 * time.Hour), at.Add(time.Minute), CatchUp{}, true},
		{"floor equals slot is handled", at, at.Add(time.Minute), CatchUp{}, false},
		{"floor after slot", at.Add(time.Second), at.Add(time.Minute), CatchUp{}, false},
		{"late beyond none", time.Time{}, at.Add(2 * time.Minute), none, false},
		{"not yet due", time.Time{}, at.Add(-time.Second), CatchUp{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Due(slot, tc.floor, tc.now, tc.c); got != tc.want {
				t.Errorf("Due = %v, want %v", got, tc.want)
			}
		})
	}
}
