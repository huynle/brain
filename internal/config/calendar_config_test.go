package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// calendarTestConfig is a valid default configuration carrying the given
// calendar sources, so each case differs from valid only in its calendars.
func calendarTestConfig(t *testing.T, calendars map[string]CalendarConfig) *UnifiedConfig {
	t.Helper()
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
	cfg.Server.Calendars = calendars
	return &cfg
}

func TestCalendarConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		calendars map[string]CalendarConfig
		wantErr   string // substring of the error; "" means valid
	}{
		{name: "no calendars is valid"},
		{
			name:      "builtin XNYS",
			calendars: map[string]CalendarConfig{"xnys": {Type: "builtin", Market: "XNYS"}},
		},
		{
			name: "builtin XNYS with valid extras",
			calendars: map[string]CalendarConfig{"xnys": {
				Type: "builtin", Market: "XNYS",
				ExtraClosed: []string{"2026-11-27"}, ExtraOpen: []string{"2026-12-24"},
			}},
		},
		{
			name:      "builtin without market",
			calendars: map[string]CalendarConfig{"xnys": {Type: "builtin"}},
			wantErr:   "server.calendars.xnys.market",
		},
		{
			name:      "builtin with an unsupported market",
			calendars: map[string]CalendarConfig{"lse": {Type: "builtin", Market: "XLON"}},
			wantErr:   "server.calendars.lse.market",
		},
		{
			name:      "builtin extra_closed with an impossible date",
			calendars: map[string]CalendarConfig{"xnys": {Type: "builtin", Market: "XNYS", ExtraClosed: []string{"2026-02-30"}}},
			wantErr:   "extra_closed",
		},
		{
			name:      "builtin extra_open in the wrong format",
			calendars: map[string]CalendarConfig{"xnys": {Type: "builtin", Market: "XNYS", ExtraOpen: []string{"12/24/2026"}}},
			wantErr:   "extra_open",
		},
		{
			name: "builtin date listed as both extra closed and extra open",
			calendars: map[string]CalendarConfig{"xnys": {
				Type: "builtin", Market: "XNYS",
				ExtraClosed: []string{"2026-12-24"}, ExtraOpen: []string{"2026-12-24"},
			}},
			wantErr: "extra_open",
		},
		{
			name:      "builtin must not carry an ics url",
			calendars: map[string]CalendarConfig{"xnys": {Type: "builtin", Market: "XNYS", URLEnv: "CAL_URL"}},
			wantErr:   "server.calendars.xnys.url_env",
		},
		{
			name:      "ics with url_env and default poll",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "TEAM_CAL_URL"}},
		},
		{
			name:      "ics with url_file and a poll of one minute",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLFile: "/etc/brain/team.ics", Poll: "1m"}},
		},
		{
			name:      "ics with neither url_env nor url_file",
			calendars: map[string]CalendarConfig{"team": {Type: "ics"}},
			wantErr:   "server.calendars.team.url_env",
		},
		{
			name:      "ics with both url_env and url_file",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "A", URLFile: "/b.ics"}},
			wantErr:   "server.calendars.team.url_env",
		},
		{
			name:      "ics poll below one minute",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "A", Poll: "30s"}},
			wantErr:   "server.calendars.team.poll",
		},
		{
			name:      "ics poll that is not a duration",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "A", Poll: "soon"}},
			wantErr:   "server.calendars.team.poll",
		},
		{
			name:      "ics must not carry a market",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "A", Market: "XNYS"}},
			wantErr:   "server.calendars.team.market",
		},
		{
			name:      "ics must not carry extra dates",
			calendars: map[string]CalendarConfig{"team": {Type: "ics", URLEnv: "A", ExtraClosed: []string{"2026-11-27"}}},
			wantErr:   "server.calendars.team.extra_closed",
		},
		{
			name:      "unknown calendar type",
			calendars: map[string]CalendarConfig{"x": {Type: "google", URLEnv: "A"}},
			wantErr:   "server.calendars.x.type",
		},
		{
			name:      "name starting with an uppercase letter",
			calendars: map[string]CalendarConfig{"XNYS": {Type: "builtin", Market: "XNYS"}},
			wantErr:   "server.calendars",
		},
		{
			name:      "name starting with a dash",
			calendars: map[string]CalendarConfig{"-xnys": {Type: "builtin", Market: "XNYS"}},
			wantErr:   "server.calendars",
		},
		{
			name:      "name containing a space",
			calendars: map[string]CalendarConfig{"my cal": {Type: "builtin", Market: "XNYS"}},
			wantErr:   "server.calendars",
		},
		{
			name:      "name with digits, dashes and underscores is valid",
			calendars: map[string]CalendarConfig{"us-hol_1": {Type: "builtin", Market: "XNYS"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := calendarTestConfig(t, tt.calendars).Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %q, want it to name %q", err, tt.wantErr)
			}
		})
	}
}

// A calendar's URL source is secret-adjacent: the error must name the field,
// never echo the environment variable's value or the file path.
func TestCalendarConfigErrorsNeverEchoURLSource(t *testing.T) {
	const path = "/secret/tenant/private-cal.ics"
	err := calendarTestConfig(t, map[string]CalendarConfig{
		"team": {Type: "ics", URLFile: path, Poll: "soon"},
	}).Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want a poll error")
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("error %q echoes the url_file path", err)
	}
}

func TestCalendarPollInterval(t *testing.T) {
	tests := []struct {
		poll string
		want time.Duration
	}{
		{poll: "", want: 5 * time.Minute},
		{poll: "2m", want: 2 * time.Minute},
		{poll: "1h", want: time.Hour},
	}
	for _, tt := range tests {
		got := CalendarConfig{Type: "ics", URLEnv: "A", Poll: tt.poll}.PollInterval()
		if got != tt.want {
			t.Errorf("PollInterval(%q) = %s, want %s", tt.poll, got, tt.want)
		}
	}
}

// The calendars block is read from the unified config file and reaches the
// flat Config that the API server builds its services from.
func TestCalendarsLoadFromUnifiedConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "brain", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = `server:
  calendars:
    xnys:
      type: builtin
      market: XNYS
      extra_closed: ["2026-11-27"]
    team:
      type: ics
      url_env: TEAM_CAL_URL
      poll: 10m
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load()
	if err := cfg.Err(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	xnys, ok := cfg.Calendars["xnys"]
	if !ok || xnys.Type != "builtin" || xnys.Market != "XNYS" || len(xnys.ExtraClosed) != 1 {
		t.Fatalf("xnys = %+v, ok=%v", xnys, ok)
	}
	team, ok := cfg.Calendars["team"]
	if !ok || team.URLEnv != "TEAM_CAL_URL" || team.PollInterval() != 10*time.Minute {
		t.Fatalf("team = %+v, ok=%v", team, ok)
	}
}
