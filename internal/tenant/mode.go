package tenant

import "fmt"

// Mode selects single-tenant or multi-tenant operation.
type Mode string

const (
	ModeSingle Mode = "single"
	ModeMulti  Mode = "multi"
)

// ParseMode parses a deployment mode, defaulting empty input to single.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "", ModeSingle:
		return ModeSingle, nil
	case ModeMulti:
		return ModeMulti, nil
	default:
		return "", fmt.Errorf("invalid tenant mode %q: valid values are single, multi", s)
	}
}
