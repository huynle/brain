package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestBindAuthorized(t *testing.T) {
	a, b := MustParse("tenant-a"), MustParse("tenant-b")
	for _, tc := range []struct {
		name                  string
		inherited, authorized ID
		fails                 bool
	}{
		{"new", ID{}, a, false}, {"same", a, a, false},
		{"conflict", b, a, true}, {"zero", a, ID{}, true},
		{"invalid", a, ID{v: "INVALID"}, true}, {"local", ID{}, Local, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(Into(context.Background(), tc.inherited))
			defer cancel()
			ctx, err := BindAuthorized(parent, tc.authorized)
			if (err != nil) != tc.fails {
				t.Fatalf("error = %v, want failure %v", err, tc.fails)
			}
			if tc.fails {
				if ctx != nil {
					t.Error("failure returned usable context")
				}
				if !tc.authorized.Valid() && !errors.Is(err, ErrNoTenant) {
					t.Errorf("error = %v; want ErrNoTenant", err)
				}
				return
			}
			if id, ok := From(ctx); !ok || id != tc.authorized {
				t.Errorf("tenant = %v, %v", id, ok)
			}
			cancel()
			if ctx.Err() != context.Canceled {
				t.Error("lost parent cancellation")
			}
		})
	}
}
