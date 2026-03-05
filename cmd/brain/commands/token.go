package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/huynle/brain-api/internal/tokens"
)

// TokenCommand implements the Command interface for the token command.
type TokenCommand struct {
	Subcommand string
	Name       string
	Config     *UnifiedConfig
	Flags      *TokenFlags
}

// TokenFlags holds token command flags.
type TokenFlags struct {
	Name string
}

// Type returns the command type identifier.
func (c *TokenCommand) Type() string {
	return "token"
}

// Execute runs the token command.
func (c *TokenCommand) Execute() error {
	brainDir := expandPath(c.Config.Server.BrainDir)

	switch c.Subcommand {
	case "create":
		return c.createToken(brainDir)
	case "list":
		return c.listTokens(brainDir)
	case "revoke":
		return c.revokeToken(brainDir)
	default:
		return fmt.Errorf("unknown subcommand: %s", c.Subcommand)
	}
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if path == "~" {
		homeDir, _ := os.UserHomeDir()
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// createToken creates a new API token.
func (c *TokenCommand) createToken(brainDir string) error {
	if c.Name == "" {
		return fmt.Errorf("token name is required (use --name)")
	}

	token, err := tokens.CreateTokenDirect(brainDir, c.Name)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	fmt.Printf("✓ Token created successfully\n")
	fmt.Printf("  Name:  %s\n", token.Name)
	fmt.Printf("  Token: %s\n", token.Token)

	return nil
}

// listTokens lists all API tokens.
func (c *TokenCommand) listTokens(brainDir string) error {
	tokenList, err := tokens.ListTokensDirect(brainDir)
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}

	if len(tokenList) == 0 {
		fmt.Println("No tokens found")
		return nil
	}

	fmt.Println("API Tokens")
	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("%-20s %-45s %s\n", "Name", "Token", "Created")
	fmt.Println(strings.Repeat("─", 80))

	for _, token := range tokenList {
		maskedToken := maskToken(token.Token)
		fmt.Printf("%-20s %-45s %s\n", token.Name, maskedToken, token.CreatedAt)
	}

	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("Total: %d tokens\n", len(tokenList))

	return nil
}

// revokeToken revokes an API token.
func (c *TokenCommand) revokeToken(brainDir string) error {
	if c.Name == "" {
		return fmt.Errorf("token name is required")
	}

	if err := tokens.RevokeTokenDirect(brainDir, c.Name); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	fmt.Printf("✓ Token '%s' revoked successfully\n", c.Name)

	return nil
}

// maskToken masks a token for display: shows first 8 chars + "..." + last 4 chars.
func maskToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:8] + "..." + token[len(token)-4:]
}
