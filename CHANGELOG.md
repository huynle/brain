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
  included) when it is under `X-Brain-Home`; home itself or anything outside
  it gives no project. Header-bearing calls never fall back to the API
  server's own working-directory project. Headers are validated, bounded routing hints: a malformed one is
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

- **`brain run stop` renamed to `brain run pause-all`.** It called
  `POST /tasks/runner/pause`, pausing every project on the server while leaving
  the local runner running. `brain run stop` now exits 2 with a rename notice.
  `pause-all` and the new `resume-all` show what they affect, ask y/N, accept
  `--yes`/`-y`, and refuse without a terminal unless `--yes` is given;
  `resume-all` warns that individually paused projects resume too. New
  `brain run pause|resume <project>` use the per-project endpoints.
- **CLI review follow-ups.** `resume-all` reports the pause scope from the
  server's paused-project list (the server's `paused` flag is true when ANY
  project is paused) and only says "All N projects are paused" when every
  known project is. `pause-all`/`resume-all` reject positional arguments.
  `brain run features` adds a WAITING column. `brain run config` shows the
  underlying error. `brain run ready` without a project, `brain help <unknown>`,
  `brain api <word>` and stray words after `api`/`dev` exit 2.
  `brain run <sub> --help` prints per-subcommand help. `brain runner` accepts
  every `brain run` subcommand (start/stop/status stay daemon commands).
  `brain dev` now actually runs the API server in the foreground at debug log
  level instead of silently doing nothing.

- **Unknown commands fail.** `brain <unknown>` and unknown `brain run` /
  `brain runner` subcommands print `brain: unknown command "<x>"` (or
  `unknown subcommand`) and a pointer to `brain help` on stderr, nothing on
  stdout, and exit 2. Bare `brain`, `-h`/`--help` and bare `brain run` still
  show help and exit 0.
- **`brain run features|logs|config` implemented.** `features <project>` and
  `config` print tables from `GET /tasks/<project>/features` and
  `GET /config/task-defaults`; `logs <project> <taskId> [--limit N]` prints the
  latest lines from `GET /tasks/<project>/<taskId>/logs` (one-shot: the endpoint
  does not stream, so `-f` prints a notice). `brain run ready` without a
  project now errors instead of querying a project named `all`.

- **MCP file arguments are base64 only.** `attachment_upload` no longer
  accepts `file_path` (send `content` + `filename`), `attachment_download` no
  longer accepts `output_path` (bytes return inline as base64), and
  `plan_discover_docs` no longer globs the server's disk: pass the docs you
  found as `doc_paths` (`additional_dirs` is rejected).

- **SDK error text:** public Go SDK errors read `brain: <code> (HTTP <status>)`,
  or `brain: <code>` for client-side failures. Only the stable machine code is
  shown; the server message, request ID and field details stay out of default
  formatting. The TypeScript `BrainError.message` uses the same format.

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

- **`brain goal` alias.** Use `brain automation goal`; `brain goal` is now an
  unknown command whose error says so.

- **`brain mcp` (stdio MCP server).** MCP is served only from the API's
  `/mcp` endpoint (e.g. https://brain.huynle.com/mcp), so tool changes ship
  with each deploy. `brain mcp` and the `brain-mcp` argv0 alias are now
  unknown commands; `internal/mcpserver`, the stdio SDK client, the NDJSON
  stdio loop, `WithLocalFilesystem`, and the stdio-only machine/client id
  resolution (`internal/identity`, `mcp_client_id`) are gone. Point OpenCode at
  the remote endpoint with the `X-Brain-*` headers (README "Connecting
  OpenCode"). The public SDK (`sdk/brain`) is unchanged.

- **`brain.ts` OpenCode plugin.** The TypeScript API-client plugin previously
  shipped at `cmd/brain/assets/plugins/opencode/brain.ts` and installed to
  `~/.config/opencode/plugin/brain.ts` has been deleted. Its tools
  (`brain_save`, `brain_recall`, `brain_search`, `brain_tasks`, etc.) are now
  exposed through the hosted brain MCP endpoint (`/mcp`) and registered in
  OpenCode's MCP configuration.

### Migration

Existing OpenCode installs continue to work with the old plugin file already
on disk — no functionality is removed at runtime. To complete the migration:

1. Re-run `brain install opencode`. This will no longer create
   `~/.config/opencode/plugin/brain.ts`; the companion `brain-planning.ts`
   plugin and brain skills/agents/commands still install as before.
2. Delete the old plugin: `rm ~/.config/opencode/plugin/brain.ts`.
3. Add the hosted brain MCP endpoint to your OpenCode config as a remote
   server with the `X-Brain-*` caller headers (see "Connecting OpenCode" in
   `README.md`). Tool names are unchanged.

### Compatibility notes

- `brain_project_context` remains registered as a name-only alias for
  `brain_context_resolve`, so existing prompts/skills/agents that call
  `brain_project_context` keep working through the cutover.
- The client id stamped on tasks is whatever the client sends in
  `X-Brain-Client-Id`; leftover `~/.config/brain/mcp_client_id` and
  `opencode_client_id` files are no longer read.
