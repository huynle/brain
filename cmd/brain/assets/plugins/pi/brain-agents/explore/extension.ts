/**
 * Read-Only Enforcement Extension for Pi
 *
 * Blocks all file write operations to enforce the explore agent's
 * read-only contract.
 */

// Intercept tool calls to enforce read-only mode
pi.on("tool_call", (event: { tool: string; args: Record<string, unknown> }) => {
  const tool = event.tool;

  // Block all write operations
  const writeTools = ["edit", "write", "create", "delete", "move", "rename"];

  if (writeTools.includes(tool)) {
    return {
      block: true,
      reason: `Explore agent is read-only. Cannot use '${tool}' tool. Use a different agent for modifications.`,
    };
  }

  // Block potentially destructive bash commands
  if (tool === "bash") {
    const command = ((event.args.command || "") as string).trim();

    const destructivePatterns = [
      /^\s*rm\s/,
      /^\s*mv\s/,
      /^\s*cp\s.*>/, // cp with redirect
      /^\s*echo\s.*>/, // echo redirect to file
      /^\s*cat\s.*>/, // cat redirect to file
      /^\s*tee\s/,
      /^\s*dd\s/,
      />\s*[^|]/, // output redirect (but not pipe)
      /^\s*git\s+(push|commit|merge|rebase|reset|checkout\s+-b)/,
      /^\s*npm\s+(publish|link)/,
    ];

    for (const pattern of destructivePatterns) {
      if (pattern.test(command)) {
        return {
          block: true,
          reason: `Explore agent is read-only. Blocked destructive command: ${command.substring(0, 80)}`,
        };
      }
    }
  }
});
