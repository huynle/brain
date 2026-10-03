# Settings Navigation and Conversation Worker Configuration

## Goal

Turn the dashboard Settings modal from one long scrolling form into a focused,
standard settings experience and make the embedded conversation worker's
startup and concurrency configuration editable there.

## Conversation worker configuration

Add a nested configuration block:

```yaml
server:
  assistant:
    jobs:
      enabled: true
      max_parallel: 3
```

`enabled` controls whether the API starts the embedded
`brain-conversation-worker`. `max_parallel` controls its registered capacity and
must be between 1 and 8. Both values are boot-owned and require a server restart
after editing. The default remains disabled with concurrency 3.

`BRAIN_ASSISTANT_JOBS=true` remains a deprecated compatibility fallback for
deployments that have not moved the setting into YAML. Production should write
the YAML setting and remove the environment variable so Settings is
authoritative. The worker registration and scheduler must use the configured
capacity instead of the current hard-coded value.

The schema advertises both controls in the Assistant section with restart
badges and explanatory help. Config validation rejects an enabled assistant
worker outside the supported concurrency range before the file is saved.

## Settings information architecture

The modal uses a fixed navigation rail and one independently scrolling content
pane. It has five categories:

- **General**: phone notifications and browser-local workspace preferences.
- **Assistant**: assistant model/provider, speech, and conversation worker.
- **Tasks & Automation**: task defaults and feature automation.
- **Runner**: shared runner and OpenCode executor settings.
- **Advanced**: server/auth, embeddings, attachments, extraction, MCP, and
  plugins.

Schema sections remain the rendering source of truth. A category mapping groups
those sections without duplicating field definitions. Switching categories
keeps the edited config in memory. Each category has a title and short
description; section cards organize related controls within the content pane.

The desktop rail is approximately 190 pixels wide and provides selected,
hover, and keyboard-focus states. On narrow/mobile layouts it becomes a
horizontally scrollable tab strip above the content. The active item uses proper
tab semantics and the content pane is associated with it. The visual direction
is restrained industrial/utilitarian, matching Brain's existing monospace dark
interface while improving hierarchy, spacing, and focus visibility.

The footer remains fixed. Save and Close are always available; Reset workspace
is shown only on General because it does not affect server configuration.
Restart and save-result notices sit inside the active content header rather
than above the entire long form. The config file path is shown in Advanced.

## Error handling and compatibility

Loading failures stay visible in the content pane and retain retry actions.
Invalid worker concurrency is rejected by server validation and returned through
the existing save-error banner. Existing unknown or temporarily empty schema
sections do not break navigation; tabs render from the known category mapping,
and unmapped server sections fall back to Advanced.

No browser preference is moved into server configuration. No secret handling,
redaction behavior, atomic config writes, backups, or assistant job recovery
behavior changes.

## Verification

- Config tests cover defaults, YAML round-trip, valid bounds, and invalid
  conversation-worker concurrency.
- API schema tests prove both fields are exposed in the Assistant section and
  require restart.
- Assistant job tests prove configured concurrency is used for registration.
- Frontend unit tests cover section-to-category grouping and category-level
  dirty detection as pure helpers.
- Typecheck, frontend tests, Go tests, build, and vet must pass.
- Browser verification covers desktop rail navigation, mobile tab layout,
  keyboard focus, persisted edits while switching tabs, and save/restart
  messaging.
