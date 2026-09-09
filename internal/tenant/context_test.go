package tenant

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestContextScope(t *testing.T) {
	type siblingKey struct{}
	a, b := MustParse("tenant-a"), MustParse("tenant-b")
	bare := context.Background()
	parent := Into(bare, a)
	child := Into(parent, b)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want ID
		ok   bool
	}{
		{"bare", bare, ID{}, false},
		{"parent-isolated", parent, a, true},
		{"child", child, b, true},
		{"sibling-inherits-parent", context.WithValue(parent, siblingKey{}, "value"), a, true},
		{"explicit-local", Into(bare, Local), Local, true},
		{"zero", Into(bare, ID{}), ID{}, false},
		{"invalid", Into(bare, ID{v: "INVALID"}), ID{}, false},
		{"zero-masks-parent", Into(parent, ID{}), ID{}, false},
		{"invalid-masks-parent", Into(parent, ID{v: "INVALID"}), ID{}, false},
		{"reserved-masks-parent", Into(parent, ID{v: "system"}), ID{}, false},
		{"wrong-type-masks-parent", context.WithValue(parent, ctxKey{}, "tenant-b"), ID{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := From(tc.ctx)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("From = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestContextPrivateKey(t *testing.T) {
	// Even an identically named type in a different scope is a distinct key.
	type ctxKey struct{}
	a, b := MustParse("tenant-a"), MustParse("tenant-b")
	for _, key := range []any{ctxKey{}, struct{}{}, "tenant", "ctxKey"} {
		t.Run(fmt.Sprintf("%T/%v", key, key), func(t *testing.T) {
			bare := context.WithValue(context.Background(), key, b)
			if got, ok := From(bare); ok || got != (ID{}) {
				t.Fatalf("foreign key created scope: (%q, %v)", got, ok)
			}
			ctx := context.WithValue(Into(bare, a), key, b)
			if got, ok := From(ctx); !ok || got != a {
				t.Fatalf("foreign key replaced scope: (%q, %v)", got, ok)
			}
		})
	}
}

func TestContextPreservesParentState(t *testing.T) {
	type valueKey struct{}
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), valueKey{}, "keep"), deadline)
	defer cancel()
	ctx := Into(parent, MustParse("tenant-a"))
	if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("deadline = %v, %v; want %v", got, ok, deadline)
	}
	if ctx.Value(valueKey{}) != "keep" {
		t.Fatal("unrelated value lost")
	}
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("parent cancellation lost")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("Err = %v, want context.Canceled", ctx.Err())
	}
}

func TestContextMissingSentinel(t *testing.T) {
	if ErrNoTenant == nil || ErrNoTenant.Error() != "tenant: no tenant in context" {
		t.Fatalf("unexpected sentinel: %v", ErrNoTenant)
	}
	if !errors.Is(fmt.Errorf("scope required: %w", ErrNoTenant), ErrNoTenant) {
		t.Fatal("wrapped sentinel not recognized")
	}
}
