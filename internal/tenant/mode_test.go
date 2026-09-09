package tenant

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Mode
	}{
		{"", ModeSingle}, {"single", ModeSingle}, {"multi", ModeMulti},
	} {
		got, err := ParseMode(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"invalid", "MULTI", " single ", " "} {
		_, err := ParseMode(input)
		if err == nil || !strings.Contains(err.Error(), "single") || !strings.Contains(err.Error(), "multi") {
			t.Errorf("ParseMode(%q) error = %v; want valid values", input, err)
		}
	}
}
