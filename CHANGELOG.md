# Changelog

All notable changes to this project will be documented in this file.

The format is loosely based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Added

- **Hosted MCP caller headers.** `/mcp` reads `X-Brain-Host-Id`,
  `X-Brain-Client-Id`, `X-Brain-Workdir` and optional `X-Brain-Home`. Tasks
  created through it are stamped `origin_machine_id`/`origin_client_id`/
  `origin_path`, `machine_affinity: local` works when the host id is sent, and
  tools default `project` from the working folder (main repo name, worktrees
  included). Headers are validated, bounded routing hints: a malformed one is
  ignored and reported by `context_get`, never trusted for auth. See README
  "Caller headers" for the OpenCode V2 config.

- **Multiple runners on one machine.** `brain runner start -n <name>` (or
  `--new`, which assigns the next free `runner-N`) starts an additional,
  independently-registered runner on a host that already has one. The name
  selects the runner's state dir (and therefore its persisted runner id), its
  daemon pid/log files, and a `name` label shown in `brain run status` and the
  web UI's runner list. `brain runner status` now lists every runner on the
  machine; `brain runner stop -n <name>` stops one and `brain runner stop --all`
  stops them all. `-n` / `--name` / `--new` also work on `brain run start` and
  `brain start --runner`, and the name can be set with `RUNNER_NAME` or
  `runner.name` in config.yaml.

  The positional project argument is unchanged and still optional — bare
  `brain runner start` means all projects. A bare repeat is still refused rather
  than silently doubling the fleet; the error names `--new`. `--new` reuses the
  slot of a runner that died, so a crashed `runner-2` comes back with the runner
  id it had instead of leaking a new identity and an orphaned state dir.

  An unnamed runner keeps the exact paths it had before — same state dir, same
  `brain-runner.pid` — so an existing deployment keeps its runner id and its
  `brain runner stop` still works. Sharing one state dir between two runners is
  what was never supported: `ResolveRunnerID` persists the id in that directory,
  so both processes would register as the same runner and then race for every
  dispatch sent to it.

  `brain api start --runner --runner-name <name>` does the same for the API
  server's embedded runner, which otherwise shares the default state dir (and
  therefore the runner id) with a standalone runner on the same host.

### Changed

- **MCP file arguments are base64 only.** `attachment_upload` no longer
  accepts `file_path` (send `content` + `filename`), `attachment_download` no
  longer accepts `output_path` (bytes return inline as base64), and
  `plan_discover_docs` no longer globs the server's disk: pass the docs you
  found as `doc_paths` (`additional_dirs` is rejected).

- **Stdio MCP (`brain mcp`) now uses the public Go SDK's HTTP transport.**
  Tool names, DTOs and normal results are unchanged. These differences are
  intentional:
  - **Plain-http token refusal:** with `BRAIN_API_TOKEN` set, a plain `http://`
    `BRAIN_API_URL` to a non-loopback host is refused at startup
    (`insecure_transport`), so the token is never sent unencrypted off-host.
    Use `https://`, or a loopback URL (`localhost`, `127.0.0.0/8`, `::1`), e.g.
    through an SSH tunnel. Without a token, plain http is unchanged.
  - **Bad `BRAIN_API_URL`:** a malformed URL (bad scheme, userinfo, query or
    fragment) fails at startup with `invalid_configuration` instead of on first
    use.
  - **Redirects:** HTTP redirects are refused (`redirect_refused`) rather than
    followed, so a token can't be forwarded to another origin. Structural path
    traversal in request paths is refused too.
  - **Bodyless mutations:** these now send an explicit empty body.
  - **Error text:** SDK errors now read `brain: <code> (HTTP <status>)`, or
    `brain: <code>` for client-side failures (e.g. `create stdio SDK client:
    brain: invalid_configuration`). Only the stable machine code is shown; the
    server message, request ID and field details stay out of default
    formatting. The TypeScript `BrainError.message` uses the same format.

  Hosted MCP is unaffected.

### Fixed

- **`--executor`, `--pi-bin`, `--pi-model` and `--pi-thinking` reach the runner
  again.** `convertToCommandsRunnerFlags` dropped all four, so
  `brain runner start --executor pi` (and the same flags on `brain start`)
  silently fell back to the configured default executor.
- **A flag value after the project no longer becomes the project.** The
  positional pre-scan in `brain run <sub>` used `project == "all"` as its
  "not found yet" sentinel, so `brain run start all --model sonnet` parsed
  "sonnet" as the project and left `--model` empty. The scan now stops at the
  first positional and skips the value of every value-taking runner flag.

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
