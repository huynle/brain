package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/huynle/brain-api/internal/auth"
)

// AuthCommand implements the Command interface for the auth command, which
// helps configure password login for the PWA and OAuth consent page.
type AuthCommand struct {
	Subcommand string
	Args       []string
}

// Type returns the command type identifier.
func (c *AuthCommand) Type() string { return "auth" }

// Execute runs the auth command.
func (c *AuthCommand) Execute() error {
	switch c.Subcommand {
	case "hash":
		return c.hashPassword()
	default:
		return fmt.Errorf("unknown auth subcommand: %q (try: brain auth hash)", c.Subcommand)
	}
}

// hashPassword reads a password (from the first argument, or stdin) and prints a
// bcrypt hash suitable for BRAIN_AUTH_PASSWORD_HASH.
//
//	brain auth hash 'my-password'
//	printf '%s' 'my-password' | brain auth hash
func (c *AuthCommand) hashPassword() error {
	var pw string
	if len(c.Args) > 0 && c.Args[0] != "" {
		pw = c.Args[0]
	} else {
		fmt.Fprintln(os.Stderr, "Reading password from stdin (e.g. pipe it in, or pass as an argument)…")
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read password from stdin: %w", err)
		}
		pw = string(b)
	}
	pw = strings.TrimRight(pw, "\r\n")
	if pw == "" {
		return fmt.Errorf("password must not be empty")
	}

	h, err := auth.HashPassword(pw)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// The hash goes to stdout (pipeable); guidance goes to stderr.
	fmt.Println(h)
	// bcrypt hashes contain '$', which Docker Compose interpolates in .env files
	// (it silently mangles the value). Offer a pre-escaped variant for that case.
	escaped := strings.ReplaceAll(h, "$", "$$")
	fmt.Fprintf(os.Stderr, "\nAdd to the server environment:\n  %s=admin\n  %s=%s\n",
		auth.EnvUsername, auth.EnvPasswordHash, h)
	fmt.Fprintf(os.Stderr, "\n⚠ In a Docker Compose .env file, escape the $ as $$ (otherwise the\n"+
		"  hash is interpolated and login fails). Use this line instead:\n  %s=%s\n",
		auth.EnvPasswordHash, escaped)
	return nil
}
