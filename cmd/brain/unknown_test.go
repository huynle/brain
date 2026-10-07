package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/huynle/brain-api/cmd/brain/commands"
)

// usageErrorOf routes args, executes the command, and returns the usage error
// it produced, failing the test when there is none.
func usageErrorOf(t *testing.T, args ...string) *commands.UsageError {
	t.Helper()
	cmd, err := route(args)
	if err == nil {
		err = cmd.Execute()
	}
	var ue *commands.UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("brain %s: err = %v, want a usage error", strings.Join(args, " "), err)
	}
	return ue
}

func TestUnknownTopLevelCommandIsAUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"mcp"},
		{"mcp", "--api-url", "http://localhost:3333"},
		{"bogus"},
		{"all"},
		{"my-project"},
		{"--bogus"},
	} {
		ue := usageErrorOf(t, args...)
		want := `brain: unknown command "` + args[0] + `"`
		if !strings.Contains(ue.Message, want) || !strings.Contains(ue.Message, "brain help") {
			t.Errorf("brain %v message = %q, want %q and a pointer to 'brain help'", args, ue.Message, want)
		}
	}
}

func TestUnknownRunSubcommandIsAUsageError(t *testing.T) {
	ue := usageErrorOf(t, "run", "bogus", "my-project")
	if !strings.Contains(ue.Message, `brain run: unknown subcommand "bogus"`) || !strings.Contains(ue.Message, "brain help run") {
		t.Errorf("message = %q", ue.Message)
	}
	ue = usageErrorOf(t, "runner", "bogus")
	if !strings.Contains(ue.Message, `brain runner: unknown subcommand "bogus"`) || !strings.Contains(ue.Message, "brain help runner") {
		t.Errorf("message = %q", ue.Message)
	}
}

func TestRunCLIExitCodes(t *testing.T) {
	cases := []struct {
		args       []string
		wantCode   int
		wantStderr string
	}{
		{nil, 0, ""},
		{[]string{"--help"}, 0, ""},
		{[]string{"help"}, 0, ""},
		{[]string{"mcp"}, 2, `brain: unknown command "mcp"`},
		{[]string{"run", "bogus"}, 2, `brain run: unknown subcommand "bogus"`},
	}
	for _, tc := range cases {
		var stderr bytes.Buffer
		var code int
		captureOutput(func() { code = runCLI(tc.args, &stderr) })
		if code != tc.wantCode {
			t.Errorf("brain %v exit = %d, want %d (stderr %q)", tc.args, code, tc.wantCode, stderr.String())
		}
		if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
			t.Errorf("brain %v stderr = %q, want %q", tc.args, stderr.String(), tc.wantStderr)
		}
		if tc.wantCode == 0 && stderr.Len() != 0 {
			t.Errorf("brain %v wrote to stderr: %q", tc.args, stderr.String())
		}
	}
}

func TestUnknownCommandPrintsNothingOnStdout(t *testing.T) {
	var stderr bytes.Buffer
	out := captureOutput(func() { runCLI([]string{"mcp"}, &stderr) })
	if out != "" {
		t.Errorf("unknown command printed to stdout (would corrupt an MCP stdio client): %q", out)
	}
}
