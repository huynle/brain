# Conversation coordinator and Go workers

Enable `BRAIN_ASSISTANT_JOBS=true` on an Assistant-enabled, single-tenant API
deployment. The API starts the lightweight Go worker automatically and registers
`brain-conversation-worker` in the normal runner registry. No phone-side process
or manually launched coding agent is required. The worker is an embedded service
in the API process, not a separate Pi/OpenCode process.

The coordinator exposes only `list_jobs`, `inspect_job`, `start_job`, `update_job`,
`resume_job`, and `cancel_job`. Tool enforcement uses a separate registry, not
just a prompt. Every Brain operation, including quick counts, is delegated.
Workers use the existing Go agent loop and the caller-authenticated Brain MCP
adapter. Claims, renewal, and release use the authenticated task REST API.

Each job creates a normal task under `assistant-jobs`, with executor `assistant`,
no feature checkout/delivery, and links to its conversation and persistent worker
ID. The embedded worker only takes jobs from its private queue; it does not run
arbitrary user-created tasks with that executor. Pi and OpenCode runners remain
unchanged. Runner pause, project/global pause, and configured concurrency are
honored (default three workers, maximum eight). Running work is independent of
the foreground HTTP request and browser lifetime.

Private runtime state lives in `.brain-data/assistant-jobs/jobs.db` (outside the
content index). The directory is private and the database file is mode 0600. It
contains delegated credentials, worker messages, pending context, job state, and
server conversation history. Never export it as a note or log its records.
Access to job APIs is admin-scoped, tenant-bound, and additionally partitioned by
authenticated identity and conversation. Password-login identity survives access
token refresh; API token identities follow their token names.

Context updates are persisted immediately and consumed between model/tool
steps. If context arrives while a model is deciding on tools, the worker
replans before executing that stale decision. A running tool cannot be undone.
Resuming keeps the same job, Brain task, and saved worker history. A tool budget
of at least 30 steps bounds each worker run; hitting it pauses the job.

Completion, failure, cancellation, and pause become pending inbox updates. The
browser requests a concise coordinator summary while the selected conversation
is idle; it does not interrupt detected speech or audio playback. The summary
and inbox acknowledgment are committed together. A fresh browser can restore
saved conversations, including results delivered before it disconnected.
Interrupting speech stops the foreground response, not its delegated jobs.

Closing the phone stops microphone capture, but server jobs continue. A server
restart is different: queued jobs survive, while jobs interrupted mid-execution
pause with an uncertainty warning. They are not automatically replayed because a
write may already have taken effect. The coordinator can inspect and resume
them. Expired/revoked credentials are not elevated or silently replaced; access
failures require signing in and resuming with valid authority.

Validation includes coordinator tool isolation, ownership and conversation
isolation, durable inbox delivery, context injection during execution, foreground
responsiveness, cancellation and same-job resume, and a real browser test that
closes during a live model-backed job and restores the result in a fresh browser
context. Browser microphone tests use simulated audio; Android/car Bluetooth
behavior still requires a phone check.
