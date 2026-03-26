// Package frontmatter provides YAML frontmatter parsing and serialization for
// brain markdown files. This is a reusable package that can be imported by
// external projects.
//
// Parsing uses gopkg.in/yaml.v3 to unmarshal the YAML section between ---
// delimiters. Serialization uses manual string building to control field order
// and style, matching the TypeScript canonical field order.
package frontmatter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SessionInfo holds session traceability data.
type SessionInfo struct {
	Timestamp string `yaml:"timestamp" json:"timestamp"`
	CronID    string `yaml:"cron_id,omitempty" json:"cron_id,omitempty"`
	RunID     string `yaml:"run_id,omitempty" json:"run_id,omitempty"`
}

// CronRun represents a single cron execution run.
type CronRun struct {
	RunID      string `yaml:"run_id" json:"run_id"`
	Status     string `yaml:"status" json:"status"`
	Started    string `yaml:"started" json:"started"`
	Completed  string `yaml:"completed" json:"completed"`
	Duration   string `yaml:"duration" json:"duration"`
	Tasks      string `yaml:"tasks" json:"tasks"`
	FailedTask string `yaml:"failed_task" json:"failed_task"`
	SkipReason string `yaml:"skip_reason" json:"skip_reason"`
}

// RunFinalization records the finalization state of a run.
type RunFinalization struct {
	Status      string `yaml:"status" json:"status"`
	FinalizedAt string `yaml:"finalized_at" json:"finalized_at"`
	SessionID   string `yaml:"session_id,omitempty" json:"session_id,omitempty"`
}

// Frontmatter holds all known brain entry frontmatter fields.
// Boolean fields use *bool so nil (absent) is distinguishable from false.
// Integer fields use *int for the same reason.
// Unknown YAML fields are captured in Extra.
type Frontmatter struct {
	Title  string `yaml:"title" json:"title"`
	Type   string `yaml:"type" json:"type"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Status string `yaml:"status" json:"status"`

	Tags     []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Priority string   `yaml:"priority,omitempty" json:"priority,omitempty"`
	Created  string   `yaml:"created,omitempty" json:"created,omitempty"`

	// Scheduling
	Schedule        string    `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	ScheduleEnabled *bool     `yaml:"schedule_enabled,omitempty" json:"schedule_enabled,omitempty"`
	NextRun         string    `yaml:"next_run,omitempty" json:"next_run,omitempty"`
	MaxRuns         *int      `yaml:"max_runs,omitempty" json:"max_runs,omitempty"`
	StartsAt        string    `yaml:"starts_at,omitempty" json:"starts_at,omitempty"`
	ExpiresAt       string    `yaml:"expires_at,omitempty" json:"expires_at,omitempty"`
	Runs            []CronRun `yaml:"runs,omitempty" json:"runs,omitempty"`

	// Hierarchy / dependencies
	ParentID         string   `yaml:"parent_id,omitempty" json:"parent_id,omitempty"`
	ProjectID        string   `yaml:"projectId,omitempty" json:"projectId,omitempty"`
	DependsOn        []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	FeatureID        string   `yaml:"feature_id,omitempty" json:"feature_id,omitempty"`
	FeaturePriority  string   `yaml:"feature_priority,omitempty" json:"feature_priority,omitempty"`
	FeatureDependsOn []string `yaml:"feature_depends_on,omitempty" json:"feature_depends_on,omitempty"`

	// Execution context
	Workdir            string `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	GitRemote          string `yaml:"git_remote,omitempty" json:"git_remote,omitempty"`
	GitBranch          string `yaml:"git_branch,omitempty" json:"git_branch,omitempty"`
	MergeTargetBranch  string `yaml:"merge_target_branch,omitempty" json:"merge_target_branch,omitempty"`
	MergePolicy        string `yaml:"merge_policy,omitempty" json:"merge_policy,omitempty"`
	MergeStrategy      string `yaml:"merge_strategy,omitempty" json:"merge_strategy,omitempty"`
	RemoteBranchPolicy string `yaml:"remote_branch_policy,omitempty" json:"remote_branch_policy,omitempty"`
	OpenPRBeforeMerge  *bool  `yaml:"open_pr_before_merge,omitempty" json:"open_pr_before_merge,omitempty"`
	ExecutionMode      string `yaml:"execution_mode,omitempty" json:"execution_mode,omitempty"`
	CompleteOnIdle     *bool  `yaml:"complete_on_idle,omitempty" json:"complete_on_idle,omitempty"`
	TargetWorkdir      string `yaml:"target_workdir,omitempty" json:"target_workdir,omitempty"`

	// User intent / prompts
	UserOriginalRequest string `yaml:"user_original_request,omitempty" json:"user_original_request,omitempty"`
	DirectPrompt        string `yaml:"direct_prompt,omitempty" json:"direct_prompt,omitempty"`
	Agent               string `yaml:"agent,omitempty" json:"agent,omitempty"`
	Model               string `yaml:"model,omitempty" json:"model,omitempty"`

	// Generated task metadata
	Generated     *bool  `yaml:"generated,omitempty" json:"generated,omitempty"`
	GeneratedKind string `yaml:"generated_kind,omitempty" json:"generated_kind,omitempty"`
	GeneratedKey  string `yaml:"generated_key,omitempty" json:"generated_key,omitempty"`
	GeneratedBy   string `yaml:"generated_by,omitempty" json:"generated_by,omitempty"`

	// Session traceability
	Sessions         map[string]SessionInfo     `yaml:"sessions,omitempty" json:"sessions,omitempty"`
	RunFinalizations map[string]RunFinalization `yaml:"run_finalizations,omitempty" json:"run_finalizations,omitempty"`

	// Unknown fields captured during parsing
	Extra map[string]interface{} `yaml:"-" json:"-"`
}

// Document is the result of parsing a markdown file with frontmatter.
type Document struct {
	Frontmatter Frontmatter
	Body        string
}

// GenerateOptions configures frontmatter generation for a new entry.
type GenerateOptions struct {
	Title  string
	Type   string
	Name   string
	Status string
	Tags   []string

	Schedule        string
	ScheduleEnabled *bool
	NextRun         string
	MaxRuns         *int
	StartsAt        string
	ExpiresAt       string
	Runs            []CronRun

	Priority  string
	Created   string
	ProjectID string

	DependsOn        []string
	FeatureID        string
	FeaturePriority  string
	FeatureDependsOn []string

	Workdir            string
	GitRemote          string
	GitBranch          string
	MergeTargetBranch  string
	MergePolicy        string
	MergeStrategy      string
	RemoteBranchPolicy string
	OpenPRBeforeMerge  *bool
	ExecutionMode      string
	CompleteOnIdle     *bool
	TargetWorkdir      string

	UserOriginalRequest string
	DirectPrompt        string
	Agent               string
	Model               string

	Generated     *bool
	GeneratedKind string
	GeneratedKey  string
	GeneratedBy   string

	Sessions         map[string]SessionInfo
	RunFinalizations map[string]RunFinalization
}

// ---------------------------------------------------------------------------
// Regex patterns
// ---------------------------------------------------------------------------

// needsQuotingRe matches values that require double-quoting in YAML.
var needsQuotingRe = regexp.MustCompile(
	`[\n\r\t]` +
		`|[:\#\[\]\{\}\|\>\<\!\&\*\?\x60\'\"\,\@\%]` +
		`|^\s|\s$` +
		`|^---` +
		`|^\.\.\.`,
)

// ---------------------------------------------------------------------------
// Parse
// ---------------------------------------------------------------------------

// rawFrontmatter is an intermediate struct used during YAML unmarshalling.
// It captures all known fields plus legacy session fields.
type rawFrontmatter struct {
	Title               string                     `yaml:"title"`
	Type                string                     `yaml:"type"`
	Name                string                     `yaml:"name"`
	Status              string                     `yaml:"status"`
	Tags                []string                   `yaml:"tags"`
	Priority            string                     `yaml:"priority"`
	Created             string                     `yaml:"created"`
	Schedule            string                     `yaml:"schedule"`
	ScheduleEnabled     *bool                      `yaml:"schedule_enabled"`
	NextRun             string                     `yaml:"next_run"`
	MaxRuns             *int                       `yaml:"max_runs"`
	StartsAt            string                     `yaml:"starts_at"`
	ExpiresAt           string                     `yaml:"expires_at"`
	Runs                []CronRun                  `yaml:"runs"`
	ParentID            string                     `yaml:"parent_id"`
	ProjectID           string                     `yaml:"projectId"`
	DependsOn           []string                   `yaml:"depends_on"`
	FeatureID           string                     `yaml:"feature_id"`
	FeaturePriority     string                     `yaml:"feature_priority"`
	FeatureDependsOn    []string                   `yaml:"feature_depends_on"`
	Workdir             string                     `yaml:"workdir"`
	GitRemote           string                     `yaml:"git_remote"`
	GitBranch           string                     `yaml:"git_branch"`
	MergeTargetBranch   string                     `yaml:"merge_target_branch"`
	MergePolicy         string                     `yaml:"merge_policy"`
	MergeStrategy       string                     `yaml:"merge_strategy"`
	RemoteBranchPolicy  string                     `yaml:"remote_branch_policy"`
	OpenPRBeforeMerge   *bool                      `yaml:"open_pr_before_merge"`
	ExecutionMode       string                     `yaml:"execution_mode"`
	CompleteOnIdle      *bool                      `yaml:"complete_on_idle"`
	TargetWorkdir       string                     `yaml:"target_workdir"`
	UserOriginalRequest string                     `yaml:"user_original_request"`
	DirectPrompt        string                     `yaml:"direct_prompt"`
	Agent               string                     `yaml:"agent"`
	Model               string                     `yaml:"model"`
	Generated           *bool                      `yaml:"generated"`
	GeneratedKind       string                     `yaml:"generated_kind"`
	GeneratedKey        string                     `yaml:"generated_key"`
	GeneratedBy         string                     `yaml:"generated_by"`
	Sessions            map[string]SessionInfo     `yaml:"sessions"`
	RunFinalizations    map[string]RunFinalization `yaml:"run_finalizations"`

	// Legacy session fields (normalized into Sessions during parsing)
	SessionIDs        []string          `yaml:"session_ids"`
	SessionTimestamps map[string]string `yaml:"session_timestamps"`
}

// knownFields is the set of YAML keys handled by rawFrontmatter.
// Anything not in this set goes into Extra.
var knownFields = map[string]bool{
	"title": true, "type": true, "name": true, "status": true,
	"tags": true, "priority": true, "created": true,
	"schedule": true, "schedule_enabled": true, "next_run": true,
	"max_runs": true, "starts_at": true, "expires_at": true, "runs": true,
	"parent_id": true, "projectId": true,
	"depends_on": true, "feature_id": true, "feature_priority": true,
	"feature_depends_on": true,
	"workdir":            true, "git_remote": true, "git_branch": true,
	"merge_target_branch": true, "merge_policy": true, "merge_strategy": true,
	"remote_branch_policy": true, "open_pr_before_merge": true,
	"execution_mode": true, "complete_on_idle": true, "target_workdir": true,
	"user_original_request": true, "direct_prompt": true,
	"agent": true, "model": true,
	"generated": true, "generated_kind": true, "generated_key": true,
	"generated_by": true,
	"sessions":     true, "run_finalizations": true,
	// Legacy fields (consumed during normalization, not emitted)
	"session_ids": true, "session_timestamps": true,
	// Legacy cron_ids (ignored)
	"cron_ids": true,
}

// Parse splits markdown content into frontmatter and body.
// If no frontmatter delimiters are found the entire content is returned as body.
func Parse(content string) (*Document, error) {
	// Match ^---\n...\n---\n? with the rest as body
	idx := strings.Index(content, "---\n")
	if idx != 0 {
		return &Document{Body: content}, nil
	}

	// Find closing ---
	rest := content[4:] // skip opening "---\n"

	var yamlStr string
	var bodyRaw string

	// Handle empty frontmatter: ---\n---\n (rest starts with "---\n")
	if strings.HasPrefix(rest, "---\n") {
		yamlStr = ""
		bodyRaw = rest[4:]
	} else if strings.HasPrefix(rest, "---") && len(rest) == 3 {
		// ---\n--- (no trailing newline, no body)
		yamlStr = ""
		bodyRaw = ""
	} else {
		closeIdx := strings.Index(rest, "\n---\n")
		if closeIdx == -1 {
			// Try \n--- at end of string (no trailing newline)
			if strings.HasSuffix(rest, "\n---") {
				closeIdx = len(rest) - 4
			} else {
				return &Document{Body: content}, nil
			}
		}
		yamlStr = rest[:closeIdx]
		bodyRaw = rest[closeIdx+5:] // skip "\n---\n"
	}

	body := strings.TrimSpace(bodyRaw)

	if strings.TrimSpace(yamlStr) == "" {
		return &Document{Body: strings.TrimSpace(body)}, nil
	}

	// --- Unmarshal known fields ---
	var raw rawFrontmatter
	if err := yaml.Unmarshal([]byte(yamlStr), &raw); err != nil {
		return nil, fmt.Errorf("frontmatter: yaml parse error: %w", err)
	}

	// --- Capture unknown fields into Extra ---
	var allFields map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &allFields); err != nil {
		// Non-fatal: we already have the typed parse
		allFields = nil
	}

	extra := make(map[string]interface{})
	for k, v := range allFields {
		if !knownFields[k] {
			extra[k] = v
		}
	}

	// --- Normalize legacy sessions ---
	sessions := raw.Sessions
	if len(sessions) == 0 && (len(raw.SessionIDs) > 0 || len(raw.SessionTimestamps) > 0) {
		sessions = make(map[string]SessionInfo)
		for _, sid := range raw.SessionIDs {
			ts := ""
			if raw.SessionTimestamps != nil {
				ts = raw.SessionTimestamps[sid]
			}
			sessions[sid] = SessionInfo{Timestamp: ts}
		}
		// Also include any timestamps not in session_ids
		for sid, ts := range raw.SessionTimestamps {
			if _, ok := sessions[sid]; !ok {
				sessions[sid] = SessionInfo{Timestamp: ts}
			}
		}
	}

	fm := Frontmatter{
		Title:               raw.Title,
		Type:                raw.Type,
		Name:                raw.Name,
		Status:              raw.Status,
		Tags:                raw.Tags,
		Priority:            raw.Priority,
		Created:             raw.Created,
		Schedule:            raw.Schedule,
		ScheduleEnabled:     raw.ScheduleEnabled,
		NextRun:             raw.NextRun,
		MaxRuns:             raw.MaxRuns,
		StartsAt:            raw.StartsAt,
		ExpiresAt:           raw.ExpiresAt,
		Runs:                raw.Runs,
		ParentID:            raw.ParentID,
		ProjectID:           raw.ProjectID,
		DependsOn:           raw.DependsOn,
		FeatureID:           raw.FeatureID,
		FeaturePriority:     raw.FeaturePriority,
		FeatureDependsOn:    raw.FeatureDependsOn,
		Workdir:             raw.Workdir,
		GitRemote:           raw.GitRemote,
		GitBranch:           raw.GitBranch,
		MergeTargetBranch:   raw.MergeTargetBranch,
		MergePolicy:         raw.MergePolicy,
		MergeStrategy:       raw.MergeStrategy,
		RemoteBranchPolicy:  raw.RemoteBranchPolicy,
		OpenPRBeforeMerge:   raw.OpenPRBeforeMerge,
		ExecutionMode:       raw.ExecutionMode,
		CompleteOnIdle:      raw.CompleteOnIdle,
		TargetWorkdir:       raw.TargetWorkdir,
		UserOriginalRequest: raw.UserOriginalRequest,
		DirectPrompt:        raw.DirectPrompt,
		Agent:               raw.Agent,
		Model:               raw.Model,
		Generated:           raw.Generated,
		GeneratedKind:       raw.GeneratedKind,
		GeneratedKey:        raw.GeneratedKey,
		GeneratedBy:         raw.GeneratedBy,
		Sessions:            sessions,
		RunFinalizations:    raw.RunFinalizations,
	}

	if len(extra) > 0 {
		fm.Extra = extra
	}

	// Trim trailing newline from block scalar fields (yaml.v3 keeps it)
	fm.UserOriginalRequest = strings.TrimRight(fm.UserOriginalRequest, "\n")
	fm.DirectPrompt = strings.TrimRight(fm.DirectPrompt, "\n")

	return &Document{Frontmatter: fm, Body: body}, nil
}

// ---------------------------------------------------------------------------
// EscapeYamlValue
// ---------------------------------------------------------------------------

// EscapeYamlValue wraps a string in double quotes if it contains characters
// that are special in YAML. Internal double-quotes, backslashes, and control
// characters are escaped.
func EscapeYamlValue(value string) string {
	if !needsQuotingRe.MatchString(value) {
		return value
	}

	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ---------------------------------------------------------------------------
// FormatMultilineValue
// ---------------------------------------------------------------------------

// FormatMultilineValue formats a key-value pair for YAML. If the value
// contains newlines or special YAML characters it uses the literal block
// scalar (|) style with 2-space indentation.
func FormatMultilineValue(key, value string) string {
	hasNewlines := strings.Contains(value, "\n")
	hasSpecial := needsQuotingRe.MatchString(value)

	if !hasNewlines && !hasSpecial {
		return key + ": " + value
	}

	var b strings.Builder
	b.WriteString(key)
	b.WriteString(": |\n")
	for i, line := range strings.Split(value, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("  ")
		b.WriteString(line)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Serialize
// ---------------------------------------------------------------------------

// formatRunValue formats a CronRun field value: empty strings become "",
// otherwise the value is escaped.
func formatRunValue(v string) string {
	if v == "" {
		return `""`
	}
	return EscapeYamlValue(v)
}

// escapeDependsOnEntry escapes a depends_on entry for serialization.
// Entries are always double-quoted with backslash/quote escaping.
func escapeDependsOnEntry(dep string) string {
	escaped := strings.ReplaceAll(dep, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// Serialize converts a Frontmatter struct to a YAML string (without the ---
// delimiters). Fields are emitted in canonical order; empty/nil fields are
// omitted.
func Serialize(fm *Frontmatter) string {
	var lines []string

	// Helper to append a simple key: value line if value is non-empty.
	emit := func(key, value string) {
		if value != "" {
			lines = append(lines, key+": "+EscapeYamlValue(value))
		}
	}
	// Helper for plain (unescaped) values like enum fields.
	emitPlain := func(key, value string) {
		if value != "" {
			lines = append(lines, key+": "+value)
		}
	}

	// --- Canonical field order (matches TypeScript serializeFrontmatter) ---

	emit("title", fm.Title)
	emitPlain("type", fm.Type)
	emit("name", fm.Name)

	// Tags
	if len(fm.Tags) > 0 {
		lines = append(lines, "tags:")
		for _, tag := range fm.Tags {
			lines = append(lines, "  - "+EscapeYamlValue(tag))
		}
	}

	emitPlain("status", fm.Status)
	emit("schedule", fm.Schedule)

	if fm.ScheduleEnabled != nil {
		lines = append(lines, fmt.Sprintf("schedule_enabled: %v", *fm.ScheduleEnabled))
	}

	emit("next_run", fm.NextRun)

	if fm.MaxRuns != nil {
		lines = append(lines, fmt.Sprintf("max_runs: %d", *fm.MaxRuns))
	}

	emit("starts_at", fm.StartsAt)
	emit("expires_at", fm.ExpiresAt)

	// Runs
	if len(fm.Runs) > 0 {
		lines = append(lines, "runs:")
		for _, run := range fm.Runs {
			lines = append(lines, "  - run_id: "+formatRunValue(run.RunID))
			lines = append(lines, "    status: "+formatRunValue(run.Status))
			lines = append(lines, "    started: "+formatRunValue(run.Started))
			lines = append(lines, "    completed: "+formatRunValue(run.Completed))
			lines = append(lines, "    duration: "+formatRunValue(run.Duration))
			lines = append(lines, "    tasks: "+formatRunValue(run.Tasks))
			lines = append(lines, "    failed_task: "+formatRunValue(run.FailedTask))
			lines = append(lines, "    skip_reason: "+formatRunValue(run.SkipReason))
		}
	}

	emitPlain("created", fm.Created)
	emitPlain("priority", fm.Priority)
	emitPlain("parent_id", fm.ParentID)
	emit("projectId", fm.ProjectID)

	// depends_on (always double-quoted)
	if len(fm.DependsOn) > 0 {
		lines = append(lines, "depends_on:")
		for _, dep := range fm.DependsOn {
			lines = append(lines, "  - "+escapeDependsOnEntry(dep))
		}
	}

	emit("feature_id", fm.FeatureID)
	emitPlain("feature_priority", fm.FeaturePriority)

	// feature_depends_on (always double-quoted)
	if len(fm.FeatureDependsOn) > 0 {
		lines = append(lines, "feature_depends_on:")
		for _, dep := range fm.FeatureDependsOn {
			lines = append(lines, "  - "+escapeDependsOnEntry(dep))
		}
	}

	emit("workdir", fm.Workdir)
	emit("git_remote", fm.GitRemote)
	emit("git_branch", fm.GitBranch)
	emit("merge_target_branch", fm.MergeTargetBranch)
	emitPlain("merge_policy", fm.MergePolicy)
	emitPlain("merge_strategy", fm.MergeStrategy)
	emitPlain("remote_branch_policy", fm.RemoteBranchPolicy)

	if fm.OpenPRBeforeMerge != nil {
		lines = append(lines, fmt.Sprintf("open_pr_before_merge: %v", *fm.OpenPRBeforeMerge))
	}

	emitPlain("execution_mode", fm.ExecutionMode)

	if fm.CompleteOnIdle != nil {
		lines = append(lines, fmt.Sprintf("complete_on_idle: %v", *fm.CompleteOnIdle))
	}

	emit("target_workdir", fm.TargetWorkdir)

	// Multiline fields
	if fm.UserOriginalRequest != "" {
		lines = append(lines, FormatMultilineValue("user_original_request", fm.UserOriginalRequest))
	}
	if fm.DirectPrompt != "" {
		lines = append(lines, FormatMultilineValue("direct_prompt", fm.DirectPrompt))
	}

	emit("agent", fm.Agent)
	emit("model", fm.Model)

	if fm.Generated != nil {
		lines = append(lines, fmt.Sprintf("generated: %v", *fm.Generated))
	}
	emitPlain("generated_kind", fm.GeneratedKind)
	emit("generated_key", fm.GeneratedKey)
	emit("generated_by", fm.GeneratedBy)

	// Sessions map
	if len(fm.Sessions) > 0 {
		lines = append(lines, "sessions:")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(fm.Sessions))
		for k := range fm.Sessions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, sid := range keys {
			s := fm.Sessions[sid]
			lines = append(lines, "  "+EscapeYamlValue(sid)+":")
			lines = append(lines, "    timestamp: "+EscapeYamlValue(s.Timestamp))
			if s.CronID != "" {
				lines = append(lines, "    cron_id: "+EscapeYamlValue(s.CronID))
			}
			if s.RunID != "" {
				lines = append(lines, "    run_id: "+EscapeYamlValue(s.RunID))
			}
		}
	}

	// Run finalizations map
	if len(fm.RunFinalizations) > 0 {
		lines = append(lines, "run_finalizations:")
		keys := make([]string, 0, len(fm.RunFinalizations))
		for k := range fm.RunFinalizations {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, rid := range keys {
			f := fm.RunFinalizations[rid]
			lines = append(lines, "  "+EscapeYamlValue(rid)+":")
			status := f.Status
			if status == "" {
				status = "completed"
			}
			lines = append(lines, "    status: "+EscapeYamlValue(status))
			lines = append(lines, "    finalized_at: "+EscapeYamlValue(f.FinalizedAt))
			if f.SessionID != "" {
				lines = append(lines, "    session_id: "+EscapeYamlValue(f.SessionID))
			}
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// ---------------------------------------------------------------------------
// Generate
// ---------------------------------------------------------------------------

// Generate creates YAML frontmatter for a new brain entry from options.
// The entry type is automatically added to tags.
// Default status is "active" if not specified.
func Generate(opts *GenerateOptions) string {
	status := opts.Status
	if status == "" {
		status = "active"
	}

	// Build tag set — type is always included
	tagSet := make(map[string]struct{})
	for _, t := range opts.Tags {
		tagSet[t] = struct{}{}
	}
	tagSet[opts.Type] = struct{}{}

	// Deterministic tag order: user-provided tags first (in order), then type if not already present
	var tags []string
	seen := make(map[string]bool)
	for _, t := range opts.Tags {
		if !seen[t] {
			tags = append(tags, t)
			seen[t] = true
		}
	}
	if !seen[opts.Type] {
		tags = append(tags, opts.Type)
	}

	// Build a Frontmatter and delegate to Serialize
	fm := &Frontmatter{
		Title:               opts.Title,
		Type:                opts.Type,
		Name:                opts.Name,
		Status:              status,
		Tags:                tags,
		Priority:            opts.Priority,
		Created:             opts.Created,
		Schedule:            opts.Schedule,
		ScheduleEnabled:     opts.ScheduleEnabled,
		NextRun:             opts.NextRun,
		MaxRuns:             opts.MaxRuns,
		StartsAt:            opts.StartsAt,
		ExpiresAt:           opts.ExpiresAt,
		Runs:                opts.Runs,
		ProjectID:           opts.ProjectID,
		DependsOn:           opts.DependsOn,
		FeatureID:           opts.FeatureID,
		FeaturePriority:     opts.FeaturePriority,
		FeatureDependsOn:    opts.FeatureDependsOn,
		Workdir:             opts.Workdir,
		GitRemote:           opts.GitRemote,
		GitBranch:           opts.GitBranch,
		MergeTargetBranch:   opts.MergeTargetBranch,
		MergePolicy:         opts.MergePolicy,
		MergeStrategy:       opts.MergeStrategy,
		RemoteBranchPolicy:  opts.RemoteBranchPolicy,
		OpenPRBeforeMerge:   opts.OpenPRBeforeMerge,
		ExecutionMode:       opts.ExecutionMode,
		CompleteOnIdle:      opts.CompleteOnIdle,
		TargetWorkdir:       opts.TargetWorkdir,
		UserOriginalRequest: opts.UserOriginalRequest,
		DirectPrompt:        opts.DirectPrompt,
		Agent:               opts.Agent,
		Model:               opts.Model,
		Generated:           opts.Generated,
		GeneratedKind:       opts.GeneratedKind,
		GeneratedKey:        opts.GeneratedKey,
		GeneratedBy:         opts.GeneratedBy,
		Sessions:            opts.Sessions,
		RunFinalizations:    opts.RunFinalizations,
	}

	return Serialize(fm)
}

// ---------------------------------------------------------------------------
// Sanitization helpers
// ---------------------------------------------------------------------------

// controlCharRe matches C0 control characters except common whitespace.
// \x00-\x08, \x0B, \x0C, \x0E-\x1F, \x7F
var controlCharRe = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

// allControlCharRe matches ALL C0 control characters including \n \r \t.
var allControlCharRe = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// multiSpaceRe collapses runs of whitespace.
var multiSpaceRe = regexp.MustCompile(`\s+`)

// NormalizeTitle normalizes a title for user-friendly display:
//   - Strips control characters (except spaces)
//   - Replaces newlines/carriage returns/tabs with spaces
//   - Collapses multiple spaces to single space
//   - Trims whitespace
//   - Truncates to 200 characters
func NormalizeTitle(title string) string {
	// Strip control characters (except common whitespace \n \r \t and space)
	result := controlCharRe.ReplaceAllString(title, "")
	// Replace newlines, carriage returns, and tabs with spaces
	result = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(result)
	// Trim
	result = strings.TrimSpace(result)
	// Collapse whitespace
	result = multiSpaceRe.ReplaceAllString(result, " ")
	// Truncate
	runes := []rune(result)
	if len(runes) > 200 {
		result = string(runes[:200])
	}
	return result
}

// SanitizeTitle sanitizes a title for safe use in YAML frontmatter
// (double-quoted string). It first normalizes the title, then escapes
// backslashes and double quotes.
func SanitizeTitle(title string) string {
	normalized := NormalizeTitle(title)
	// Escape backslashes first, then double quotes
	normalized = strings.ReplaceAll(normalized, `\`, `\\`)
	normalized = strings.ReplaceAll(normalized, `"`, `\"`)
	return normalized
}

// SanitizeTag sanitizes a tag for safe use in YAML frontmatter.
// Returns ("", false) for empty tags or tags containing ": " (colon+space).
// Bare colons without trailing space are safe.
func SanitizeTag(tag string) (string, bool) {
	// Strip all control characters including \n, \r, \t, \0
	result := allControlCharRe.ReplaceAllString(tag, "")
	result = strings.TrimSpace(result)

	if result == "" {
		return "", false
	}
	// Reject tags with ": " (colon+space) which YAML parses as key-value
	if strings.Contains(result, ": ") {
		return "", false
	}
	return result, true
}

// SanitizeSimpleValue sanitizes a simple value (workdir, git_remote, etc.):
//   - Strips null bytes
//   - Replaces newlines/carriage returns with spaces
//   - Collapses multiple spaces
//   - Trims whitespace
func SanitizeSimpleValue(value string) string {
	// Strip null bytes
	result := strings.Map(func(r rune) rune {
		if r == 0 {
			return -1
		}
		return r
	}, value)
	// Replace newlines and carriage returns with spaces
	result = strings.NewReplacer("\n", " ", "\r", " ").Replace(result)
	// Collapse whitespace
	result = multiSpaceRe.ReplaceAllString(result, " ")
	return strings.TrimSpace(result)
}

// SanitizeDependsOnEntry sanitizes a depends_on entry:
//   - Strips control characters
//   - Trims whitespace
func SanitizeDependsOnEntry(dep string) string {
	result := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, dep)
	return strings.TrimSpace(result)
}
