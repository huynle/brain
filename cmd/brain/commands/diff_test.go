package commands

import (
	"reflect"
	"strings"
	"testing"
)

func TestDiffLines_IdenticalReturnsNil(t *testing.T) {
	if got := diffLines("a\nb\nc\n", "a\nb\nc\n"); got != nil {
		t.Fatalf("identical inputs should return nil, got: %v", got)
	}
	if got := diffLines("", ""); got != nil {
		t.Fatalf("empty inputs should return nil, got: %v", got)
	}
}

func TestDiffLines_AdditionsAndRemovals(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want []string
	}{
		{
			name: "single changed line",
			old:  "host: prod\nport: 1\n",
			new:  "host: local\nport: 1\n",
			want: []string{"- host: prod", "+ host: local"},
		},
		{
			name: "pure addition",
			old:  "a\n",
			new:  "a\nb\n",
			want: []string{"+ b"},
		},
		{
			name: "pure removal",
			old:  "a\nb\n",
			new:  "a\n",
			want: []string{"- b"},
		},
		{
			name: "empty old (whole file added)",
			old:  "",
			new:  "x\ny\n",
			want: []string{"+ x", "+ y"},
		},
		{
			name: "empty new (whole file removed)",
			old:  "x\ny\n",
			new:  "",
			want: []string{"- x", "- y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffLines(tt.old, tt.new)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("diffLines()\n got: %v\nwant: %v", got, tt.want)
			}
		})
	}
}

// TestDiffLines_TrailingNewlineInsensitive verifies that a trailing newline
// difference alone is not reported as a change (config writers may or may not
// terminate the file with a newline).
func TestDiffLines_TrailingNewlineInsensitive(t *testing.T) {
	if got := diffLines("a\nb", "a\nb\n"); got != nil {
		t.Fatalf("trailing-newline-only difference should be nil, got: %v", got)
	}
}

// TestDiffLines_PreservesRemovedContent guards the security-relevant property
// that a forced overwrite diff surfaces the values about to be lost (e.g. an
// api_token line) so the user can react before it is gone.
func TestDiffLines_PreservesRemovedContent(t *testing.T) {
	old := "runner:\n  api_token: secret-abc\n"
	new := "runner:\n  api_token: ''\n"
	got := diffLines(old, new)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "- "+"  api_token: secret-abc") {
		t.Fatalf("diff should surface the removed token line, got:\n%s", joined)
	}
}
