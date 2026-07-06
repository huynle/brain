# Changelog

All notable changes to this project will be documented in this file.

The format is loosely based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Removed

- **`brain.ts` OpenCode plugin.** The TypeScript API-client plugin previously
  shipped at `cmd/brain/assets/plugins/opencode/brain.ts` and installed to
  `~/.config/opencode/plugin/brain.ts` has been deleted. Its tools
  (`brain_save`, `brain_recall`, `brain_search`, `brain_tasks`, etc.) are now
  exposed through the brain MCP stdio server (`brain mcp`) and registered in
  OpenCode's MCP configuration.

### Migration

Existing OpenCode installs continue to work with the old plugin file already
on disk — no functionality is removed at runtime. To complete the migration:

1. Re-run `brain install opencode`. This will no longer create
   `~/.config/opencode/plugin/brain.ts`; the companion `brain-planning.ts`
   plugin and brain skills/agents/commands still install as before.
2. Delete the old plugin: `rm ~/.config/opencode/plugin/brain.ts`.
3. Add the brain MCP server to your OpenCode config (see the "Connecting
   OpenCode" section in `README.md`):

   ```json
   {
     "mcp": {
       "brain": {
         "type": "local",
         "command": ["brain", "mcp"],
         "enabled": true,
         "environment": {
           "BRAIN_API_URL": "http://localhost:3333"
         }
       }
     }
   }
   ```

The `brain mcp` subcommand reads `mcp.api_url` / `runner.api_token` from
`~/.config/brain/config.yaml`; env vars (`BRAIN_API_URL`, `BRAIN_API_TOKEN`)
override the file. Tool names and arguments are unchanged.

### Compatibility notes

- `brain_project_context` remains registered as a name-only alias for
  `brain_context_resolve`, so existing prompts/skills/agents that call
  `brain_project_context` keep working through the cutover.
- The MCP stdio server uses a separate per-install client id stored at
  `~/.config/brain/mcp_client_id`, distinct from any legacy
  `opencode_client_id` left behind by the old plugin.
