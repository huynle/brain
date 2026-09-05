// Package api — config schema description.
//
// ConfigSchema returns a flat list of every editable field with the
// metadata the frontend needs to build a form: dotted path, type,
// whether the change requires a server restart, whether it's a
// secret, and any enumerated allowed values.
//
// The list is hand-maintained because reflection on UnifiedConfig
// can't answer domain questions like "does this require restart"
// or "is this an enum". Every new field added to UnifiedConfig
// SHOULD be added here — that's what surfaces it in the UI.

package api

// ConfigField describes one editable field in the config.
type ConfigField struct {
	// Path is a dotted yaml path, e.g. "server.port" or
	// "runner.opencode.bin". Matches how the frontend addresses
	// nested fields.
	Path string `json:"path"`

	// Kind is one of: "string", "int", "bool", "duration_ms",
	// "string_array", "enum", "secret", "path", "url".
	Kind string `json:"kind"`

	// Section groups fields for the UI. Options: "server",
	// "task_defaults", "embedding", "attachments",
	// "attachment_extraction", "assistant", "feature_checkout",
	// "runner", "runner.opencode", "runner.script", "mcp",
	// "plugins".
	Section string `json:"section"`

	// Label is the human-readable field name shown in the UI.
	Label string `json:"label"`

	// Help is a short (< 120 char) tooltip explaining the field.
	Help string `json:"help,omitempty"`

	// Enum values (only when Kind == "enum").
	Enum []string `json:"enum,omitempty"`

	// RequiresRestart is true when a change here cannot be applied
	// without bouncing the API server process. The UI shows a badge
	// and the response after PUT lists these fields.
	RequiresRestart bool `json:"requires_restart,omitempty"`

	// Secret marks values that are redacted on GET and interpreted
	// as "leave unchanged" on PUT when equal to sentinelUnchanged.
	Secret bool `json:"secret,omitempty"`

	// Required marks fields that must not be empty when submitted.
	// Empty here means blank string or nil.
	Required bool `json:"required,omitempty"`
}

// ConfigSchema returns the full schema list.
//
// Ordering within each section matters — it's what the UI uses to
// render fields top-to-bottom.
func ConfigSchema() []ConfigField {
	return []ConfigField{
		// ─── server ────────────────────────────────────────────
		{Path: "server.port", Kind: "int", Section: "server", Label: "Port", Help: "TCP port the API server listens on.", RequiresRestart: true, Required: true},
		{Path: "server.host", Kind: "string", Section: "server", Label: "Host", Help: "Bind address. 'localhost' or '0.0.0.0'.", RequiresRestart: true, Required: true},
		{Path: "server.brain_dir", Kind: "path", Section: "server", Label: "Brain directory", Help: "Filesystem root for entries, attachments, and the SQLite database.", RequiresRestart: true, Required: true},
		{Path: "server.enable_auth", Kind: "bool", Section: "server", Label: "Enable authentication", Help: "Require OAuth PKCE / JWT bearer tokens for API calls.", RequiresRestart: true},
		{Path: "server.cors_origin", Kind: "string", Section: "server", Label: "CORS origin", Help: "Allow-list for cross-origin requests. '*' allows any origin."},
		{Path: "server.log_level", Kind: "enum", Section: "server", Label: "Log level", Enum: []string{"debug", "info", "warn", "error"}, Help: "Minimum log level for API server."},
		{Path: "server.oauth_pin", Kind: "secret", Section: "server", Label: "OAuth PIN", Help: "Optional PIN required on the consent page.", Secret: true},
		{Path: "server.jwt_secret", Kind: "secret", Section: "server", Label: "JWT secret", Help: "HMAC secret for HS256 bearer tokens.", Secret: true, RequiresRestart: true},
		{Path: "server.tls_cert", Kind: "path", Section: "server", Label: "TLS cert", Help: "Path to TLS certificate PEM. Blank = HTTP.", RequiresRestart: true},
		{Path: "server.tls_key", Kind: "path", Section: "server", Label: "TLS key", Help: "Path to TLS private key PEM.", RequiresRestart: true},
		{Path: "server.pid_file", Kind: "path", Section: "server", Label: "PID file", RequiresRestart: true},
		{Path: "server.log_file", Kind: "path", Section: "server", Label: "Log file", RequiresRestart: true},
		{Path: "server.log_max_size_mb", Kind: "int", Section: "server", Label: "Log rotate size (MB)", Help: "Rotate log file when it grows past this size.", RequiresRestart: true},
		{Path: "server.log_max_backups", Kind: "int", Section: "server", Label: "Rotated backups to keep", RequiresRestart: true},

		// ─── task_defaults ─────────────────────────────────────
		{Path: "server.task_defaults.agent", Kind: "string", Section: "task_defaults", Label: "Default agent", Help: "Agent name applied to tasks with no explicit agent."},
		{Path: "server.task_defaults.model", Kind: "string", Section: "task_defaults", Label: "Default model"},
		{Path: "server.task_defaults.executor", Kind: "enum", Section: "task_defaults", Label: "Default executor", Enum: []string{"", "opencode", "pi", "script"}, Help: "Executor backend used when a task doesn't specify one."},
		{Path: "server.task_defaults.extensions", Kind: "string_array", Section: "task_defaults", Label: "Extensions", Help: "Extension names loaded for every task."},
		{Path: "server.task_defaults.execution_mode", Kind: "enum", Section: "task_defaults", Label: "Execution mode", Enum: []string{"worktree", "current_branch"}},
		{Path: "server.task_defaults.complete_on_idle", Kind: "bool", Section: "task_defaults", Label: "Complete on idle"},
		{Path: "server.task_defaults.merge_policy", Kind: "enum", Section: "task_defaults", Label: "Merge policy", Enum: []string{"prompt_only", "auto_pr", "auto_merge"}},
		{Path: "server.task_defaults.merge_strategy", Kind: "enum", Section: "task_defaults", Label: "Merge strategy", Enum: []string{"squash", "merge", "rebase"}},
		{Path: "server.task_defaults.merge_target_branch", Kind: "string", Section: "task_defaults", Label: "Merge target branch"},
		{Path: "server.task_defaults.remote_branch_policy", Kind: "enum", Section: "task_defaults", Label: "Remote branch after merge", Enum: []string{"keep", "delete"}},
		{Path: "server.task_defaults.open_pr_before_merge", Kind: "bool", Section: "task_defaults", Label: "Open PR before merge"},
		{Path: "server.task_defaults.target_workdir", Kind: "path", Section: "task_defaults", Label: "Target workdir"},

		// ─── embedding ─────────────────────────────────────────
		{Path: "server.embedding.enabled", Kind: "bool", Section: "embedding", Label: "Enable embedding", Help: "Compute semantic embeddings for entries; enables semantic search.", RequiresRestart: true},
		{Path: "server.embedding.provider", Kind: "string", Section: "embedding", Label: "Provider", RequiresRestart: true},
		{Path: "server.embedding.base_url", Kind: "url", Section: "embedding", Label: "Base URL", RequiresRestart: true},
		{Path: "server.embedding.api_key_env", Kind: "string", Section: "embedding", Label: "API key env var", Help: "Environment variable name that holds the API key.", RequiresRestart: true},
		{Path: "server.embedding.model", Kind: "string", Section: "embedding", Label: "Model", RequiresRestart: true},
		{Path: "server.embedding.dim", Kind: "int", Section: "embedding", Label: "Embedding dimensions", RequiresRestart: true},
		{Path: "server.embedding.batch_size", Kind: "int", Section: "embedding", Label: "Batch size", RequiresRestart: true},
		{Path: "server.embedding.timeout_ms", Kind: "int", Section: "embedding", Label: "Request timeout (ms)", RequiresRestart: true},

		// ─── assistant ─────────────────────────────────────────
		{Path: "server.assistant.enabled", Kind: "bool", Section: "assistant", Label: "Enable assistant"},
		{Path: "server.assistant.provider", Kind: "string", Section: "assistant", Label: "Provider"},
		{Path: "server.assistant.base_url", Kind: "url", Section: "assistant", Label: "Base URL"},
		{Path: "server.assistant.api_key_env", Kind: "string", Section: "assistant", Label: "API key env var"},
		{Path: "server.assistant.model", Kind: "string", Section: "assistant", Label: "Model"},
		{Path: "server.assistant.timeout_ms", Kind: "int", Section: "assistant", Label: "Request timeout (ms)"},

		// ─── attachments ───────────────────────────────────────
		{Path: "server.attachments.storage_root", Kind: "path", Section: "attachments", Label: "Storage root", RequiresRestart: true},
		{Path: "server.attachments.max_upload_size_bytes", Kind: "int", Section: "attachments", Label: "Max upload size (bytes)"},
		{Path: "server.attachments.allowed_mime_types", Kind: "string_array", Section: "attachments", Label: "Allowed MIME types"},
		{Path: "server.attachments.blocked_mime_types", Kind: "string_array", Section: "attachments", Label: "Blocked MIME types"},

		// ─── attachment_extraction ─────────────────────────────
		{Path: "server.attachment_extraction.enabled", Kind: "bool", Section: "attachment_extraction", Label: "Enable extraction"},
		{Path: "server.attachment_extraction.provider", Kind: "string", Section: "attachment_extraction", Label: "Provider"},
		{Path: "server.attachment_extraction.base_url", Kind: "url", Section: "attachment_extraction", Label: "Base URL"},
		{Path: "server.attachment_extraction.api_key_env", Kind: "string", Section: "attachment_extraction", Label: "API key env var"},
		{Path: "server.attachment_extraction.model", Kind: "string", Section: "attachment_extraction", Label: "Model"},
		{Path: "server.attachment_extraction.timeout_ms", Kind: "int", Section: "attachment_extraction", Label: "Request timeout (ms)"},
		{Path: "server.attachment_extraction.max_size_bytes", Kind: "int", Section: "attachment_extraction", Label: "Max size (bytes)"},
		{Path: "server.attachment_extraction.supported_mime_types", Kind: "string_array", Section: "attachment_extraction", Label: "Supported MIME types"},
		{Path: "server.attachment_extraction.max_derived_text_chars", Kind: "int", Section: "attachment_extraction", Label: "Max derived text chars"},

		// ─── feature_checkout ──────────────────────────────────
		{Path: "server.feature_checkout.enabled", Kind: "bool", Section: "feature_checkout", Label: "Enable feature checkout automation", Help: "Registers the built-in feature-checkout automations at startup, so a completed feature is merged automatically. Takes effect when the API server restarts.", RequiresRestart: true},

		// ─── runner ────────────────────────────────────────────
		{Path: "runner.brain_api_url", Kind: "url", Section: "runner", Label: "Brain API URL", Help: "URL the runner uses to reach the API server.", RequiresRestart: true, Required: true},
		{Path: "runner.api_token", Kind: "secret", Section: "runner", Label: "API token", Help: "Bearer token for authenticating with the API server. Prefer api_token_env in production.", Secret: true, RequiresRestart: true},
		{Path: "runner.api_token_env", Kind: "string", Section: "runner", Label: "API token env var", RequiresRestart: true},
		{Path: "runner.max_parallel", Kind: "int", Section: "runner", Label: "Max parallel tasks", Help: "Upper bound on simultaneously-executing tasks."},
		{Path: "runner.poll_interval", Kind: "int", Section: "runner", Label: "Poll interval (s)", Help: "Seconds between API polls when SSE is unavailable."},
		{Path: "runner.task_poll_interval", Kind: "int", Section: "runner", Label: "Task poll interval (s)"},
		{Path: "runner.work_dir", Kind: "path", Section: "runner", Label: "Work directory", Help: "Root under which task worktrees are created.", RequiresRestart: true},
		{Path: "runner.state_dir", Kind: "path", Section: "runner", Label: "State directory", RequiresRestart: true},
		{Path: "runner.log_dir", Kind: "path", Section: "runner", Label: "Log directory", RequiresRestart: true},
		{Path: "runner.api_timeout", Kind: "int", Section: "runner", Label: "API timeout (ms)"},
		{Path: "runner.task_timeout", Kind: "int", Section: "runner", Label: "Task timeout (ms)", Help: "Kill running tasks that exceed this. 0 = no limit."},
		{Path: "runner.idle_detection_threshold", Kind: "int", Section: "runner", Label: "Idle detection threshold (ms)"},
		{Path: "runner.memory_threshold_percent", Kind: "int", Section: "runner", Label: "Memory threshold %", Help: "Refuse to start tasks while the host has less than this % of memory available. 0 = off."},
		{Path: "runner.task_memory_limit_mb", Kind: "int", Section: "runner", Label: "Task memory limit (MB)", Help: "Kill a task whose whole process tree (agent + OpenCode server + children) exceeds this, and park it in blocked. 0 = off."},
		{Path: "runner.opencode_db_max_gb", Kind: "int", Section: "runner", Label: "OpenCode DB max (GB)", Help: "Refuse OpenCode tasks while ~/.local/share/opencode/opencode.db is larger than this. 0 = off."},
		{Path: "runner.exclude_projects", Kind: "string_array", Section: "runner", Label: "Exclude projects", Help: "Project IDs the runner will never claim tasks for."},
		{Path: "runner.include_projects", Kind: "string_array", Section: "runner", Label: "Include projects only", Help: "If set, runner ONLY claims tasks in these projects."},
		{Path: "runner.auto_monitors", Kind: "bool", Section: "runner", Label: "Auto-start monitors"},

		// ─── runner.opencode ───────────────────────────────────
		{Path: "runner.opencode.bin", Kind: "path", Section: "runner.opencode", Label: "OpenCode binary", RequiresRestart: true},
		{Path: "runner.opencode.agent", Kind: "string", Section: "runner.opencode", Label: "Default agent"},
		{Path: "runner.opencode.model", Kind: "string", Section: "runner.opencode", Label: "Default model"},

		// ─── mcp ───────────────────────────────────────────────
		{Path: "mcp.api_url", Kind: "url", Section: "mcp", Label: "MCP API URL", RequiresRestart: true},

		// ─── plugins ───────────────────────────────────────────
		{Path: "plugins.opencode_path", Kind: "path", Section: "plugins", Label: "OpenCode plugins path"},
		{Path: "plugins.claude_code_path", Kind: "path", Section: "plugins", Label: "Claude Code plugins path"},
	}
}

// allFieldPaths returns every path from the schema — used as the
// fallback "requires restart" list when the hot-reloader isn't wired
// (e.g. in tests).
func allFieldPaths() []string {
	fields := ConfigSchema()
	paths := make([]string, len(fields))
	for i, f := range fields {
		paths[i] = f.Path
	}
	return paths
}
