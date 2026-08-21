/**
 * TDD Enforcement Plugin
 *
 * Enforces Test-Driven Development discipline by:
 * 1. Tracking current TDD phase (red/green/refactor/verify)
 * 2. Blocking file modifications that violate phase rules
 * 3. Requiring evidence for phase transitions
 * 4. Blocking commits until verification complete
 * 5. Providing audit trail for session analysis
 *
 * 4-PHASE CYCLE: RED → GREEN → REFACTOR → VERIFY
 *
 * ENFORCEMENT IS OPT-IN: Only active when tdd_gate(action: "start") is called.
 * Non-TDD sessions are completely unaffected.
 * When idle (tdd-dev agent): doc/config files allowed, source code blocked.
 */

import type { Plugin } from "@opencode-ai/plugin"
import { tool } from "@opencode-ai/plugin"
import { Bun } from "../tool/lib/bun-shim.ts"

// =============================================================================
// TYPES
// =============================================================================

type TDDPhase = "idle" | "red" | "green" | "refactor" | "verify"

type ProjectType = "node" | "go" | "python" | "rust" | "dotnet" | "unknown"

interface ProjectConfig {
  type: ProjectType
  testPatterns: RegExp[]
  testCommands: string[]
}

interface TDDState {
  phase: TDDPhase
  testFile?: string
  implFile?: string
  config: ProjectConfig
  evidence: Array<{ phase: TDDPhase; content: string; timestamp: number }>
  testsPassing: boolean
  incrementCount: number
  startedAt: number
  skipActive: boolean // One-time bypass flag
  stubbedFiles: Set<string> // Files that have been stubbed during RED phase
}

interface AuditEvent {
  type: "start" | "transition" | "block" | "skip" | "exit" | "test_result" | "stub_allowed"
  sessionID: string
  timestamp: number
  phase?: TDDPhase
  targetPhase?: TDDPhase
  evidence?: string
  reason?: string
  blockedTool?: string
  blockedFile?: string
  testsPassing?: boolean
}

// =============================================================================
// PROJECT AUTO-DETECTION
// =============================================================================

const PROJECT_CONFIGS: Record<ProjectType, Omit<ProjectConfig, "type">> = {
  node: {
    testPatterns: [
      /\.test\.(ts|tsx|js|jsx|mjs|cjs)$/,
      /\.spec\.(ts|tsx|js|jsx|mjs|cjs)$/,
      /__tests__\//,
      /\/tests?\//,
    ],
    testCommands: ["npm test", "yarn test", "pnpm test", "bun test", "vitest", "jest"],
  },
  go: {
    testPatterns: [/_test\.go$/],
    testCommands: ["go test"],
  },
  python: {
    testPatterns: [/test_.*\.py$/, /.*_test\.py$/, /tests?\/.*\.py$/, /pytest/],
    testCommands: ["pytest", "python -m pytest", "python -m unittest"],
  },
  rust: {
    testPatterns: [/tests?\.rs$/, /tests\/.*\.rs$/],
    testCommands: ["cargo test"],
  },
  dotnet: {
    testPatterns: [/Tests?\.cs$/, /\.Tests?\//],
    testCommands: ["dotnet test"],
  },
  unknown: {
    testPatterns: [/test/i, /spec/i],
    testCommands: [],
  },
}

async function detectProjectType(directory: string): Promise<ProjectConfig> {
  const checks: Array<{ file: string; type: ProjectType }> = [
    { file: "package.json", type: "node" },
    { file: "go.mod", type: "go" },
    { file: "pyproject.toml", type: "python" },
    { file: "setup.py", type: "python" },
    { file: "requirements.txt", type: "python" },
    { file: "Cargo.toml", type: "rust" },
  ]

  for (const check of checks) {
    try {
      const exists = await Bun.file(`${directory}/${check.file}`).exists()
      if (exists) {
        return {
          type: check.type,
          ...PROJECT_CONFIGS[check.type],
        }
      }
    } catch {
      // Continue checking
    }
  }

  // Check for .csproj files (dotnet)
  try {
    const result = await Bun.$`find ${directory} -maxdepth 2 -name "*.csproj" 2>/dev/null`.quiet()
    if (result.stdout.toString().trim()) {
      return { type: "dotnet", ...PROJECT_CONFIGS.dotnet }
    }
  } catch {
    // Continue
  }

  return { type: "unknown", ...PROJECT_CONFIGS.unknown }
}

function isTestFile(filePath: string, config: ProjectConfig, explicitTestFile?: string): boolean {
  // Explicit test file always matches
  if (explicitTestFile && filePath.includes(explicitTestFile)) {
    return true
  }

  // Check against patterns
  return config.testPatterns.some((pattern) => pattern.test(filePath))
}

function isImplFile(filePath: string, config: ProjectConfig, explicitImplFile?: string, explicitTestFile?: string): boolean {
  // Explicit impl file always matches
  if (explicitImplFile && filePath.includes(explicitImplFile)) {
    return true
  }

  // If it's a test file, it's not an impl file
  if (isTestFile(filePath, config, explicitTestFile)) {
    return false
  }

  // Otherwise, assume it's an impl file
  return true
}

/**
 * Check if a file is a documentation or configuration file (non-code).
 * These are allowed when the tdd-dev agent is idle (outside TDD mode)
 * because they don't contain testable logic.
 */
function isDocOrConfigFile(filePath: string): boolean {
  const docConfigExtensions = [
    /\.md$/i,
    /\.txt$/i,
    /\.rst$/i,
    /\.yaml$/i,
    /\.yml$/i,
    /\.json$/i,
    /\.toml$/i,
    /\.ini$/i,
    /\.cfg$/i,
    /\.conf$/i,
    /\.env$/i,
    /\.env\..+$/i,
    /\.gitignore$/i,
    /\.gitattributes$/i,
    /\.editorconfig$/i,
    /\.prettierrc$/i,
    /\.eslintrc$/i,
    /Makefile$/i,
    /Dockerfile$/i,
    /\.dockerignore$/i,
    /LICENSE$/i,
    /CHANGELOG$/i,
  ]
  return docConfigExtensions.some((pattern) => pattern.test(filePath))
}

// =============================================================================
// TEST RESULT PARSING
// =============================================================================

function parseTestResult(output: string): { passing: boolean; summary: string } {
  const outputLower = output.toLowerCase()

  // Failure patterns (check first)
  const failPatterns = [
    /(\d+)\s*(failed|failures|failing)/i,
    /FAIL/,
    /FAILED/,
    /AssertionError/i,
    /Error:/,
    /ERRORS?:/i,
  ]

  // Pass patterns
  const passPatterns = [
    /(\d+)\s*(passed|passing|ok|success)/i,
    /PASS/,
    /OK\s*\(/,
    /All tests passed/i,
    /Tests:\s*\d+\s*passed/i,
  ]

  const hasFail = failPatterns.some((p) => p.test(output))
  const hasPass = passPatterns.some((p) => p.test(output))

  // Extract summary
  const summaryPatterns = [
    /(\d+)\s*passed.*?(\d+)\s*failed/i,
    /(\d+)\/(\d+)\s*(tests?|specs?)/i,
    /Tests?:\s*(\d+)\s*passed/i,
    /(\d+)\s*(passed|ok)/i,
  ]

  let summary = "unknown"
  for (const pattern of summaryPatterns) {
    const match = output.match(pattern)
    if (match) {
      summary = match[0]
      break
    }
  }

  // Passing = has pass indicators AND no fail indicators
  const passing = hasPass && !hasFail

  return { passing, summary }
}

function isGitCommit(command: string): boolean {
  return /git\s+(commit|push)/.test(command)
}

/**
 * Detect if file content is a stub (placeholder implementation).
 * Stubs are allowed during RED phase to enable proper assertion failures
 * instead of import/compilation errors.
 * 
 * A stub typically:
 * - Returns a hardcoded wrong value (0, null, undefined, "", false, [])
 * - Throws "not implemented" error
 * - Has minimal/no logic
 */
function isStubContent(content: string): boolean {
  // Normalize content
  const normalized = content.replace(/\/\/.*$/gm, "").replace(/\/\*[\s\S]*?\*\//g, "").trim()
  
  // Stub patterns - returns placeholder values
  const stubPatterns = [
    /return\s+0\s*;/,                    // return 0;
    /return\s+null\s*;/,                 // return null;
    /return\s+undefined\s*;/,            // return undefined;
    /return\s+""\s*;/,                   // return "";
    /return\s+''\s*;/,                   // return '';
    /return\s+false\s*;/,                // return false;
    /return\s+true\s*;/,                 // return true; (sometimes used as stub)
    /return\s+\[\]\s*;/,                 // return [];
    /return\s+\{\}\s*;/,                 // return {};
    /throw\s+.*not\s*implement/i,        // throw "not implemented"
    /throw\s+.*todo/i,                   // throw "TODO"
    /panic!\s*\("not implemented"\)/,    // Rust panic
    /raise\s+NotImplementedError/,       // Python
    /pass\s*$/m,                         // Python pass statement
  ]
  
  // Check if content matches stub patterns
  const hasStubReturn = stubPatterns.some(p => p.test(normalized))
  
  // Also check if it's a very short file (likely just exports with stubs)
  const isShort = normalized.split('\n').filter(l => l.trim()).length <= 10
  
  return hasStubReturn && isShort
}

function isTestCommand(command: string, config: ProjectConfig): boolean {
  // Check explicit test commands
  if (config.testCommands.some((tc) => command.includes(tc))) {
    return true
  }

  // Generic test detection
  const genericTestPatterns = [/\btest\b/i, /\bspec\b/i, /\bcheck\b/i]
  return genericTestPatterns.some((p) => p.test(command))
}

// =============================================================================
// PLUGIN
// =============================================================================

export const TDDEnforcementPlugin: Plugin = async ({ client, project, directory }) => {
  // Session-scoped state
  const sessions = new Map<string, TDDState>()
  const sessionAgents = new Map<string, string>()

  // Audit log (standalone, exportable)
  const auditLog: AuditEvent[] = []

  // Helper to get state
  const getState = (sessionID: string): TDDState | null => {
    return sessions.get(sessionID) || null
  }

  // Helper to log audit event
  const logAudit = (event: Omit<AuditEvent, "timestamp">) => {
    auditLog.push({ ...event, timestamp: Date.now() })
  }

  // Phase info for display
  const phaseInfo: Record<
    TDDPhase,
    { emoji: string; name: string; allowed: string; blocked: string; next: string }
  > = {
    idle: {
      emoji: "⚪",
      name: "IDLE",
      allowed: "all",
      blocked: "none",
      next: 'Call tdd_gate(action: "start") to begin',
    },
    red: {
      emoji: "🔴",
      name: "RED",
      allowed: "test files, stub files (one per impl)",
      blocked: "implementation edits, git commit",
      next: "Write failing test, run it, then transition to GREEN",
    },
    green: {
      emoji: "🟢",
      name: "GREEN",
      allowed: "implementation files only",
      blocked: "test files, doc/config files, git commit",
      next: "Make test pass, then transition to REFACTOR",
    },
    refactor: {
      emoji: "🔵",
      name: "REFACTOR",
      allowed: "all files (test + impl + docs)",
      blocked: "git commit",
      next: "Clean up code, run full suite, then transition to VERIFY",
    },
    verify: {
      emoji: "✅",
      name: "VERIFY",
      allowed: "all files, git commit",
      blocked: "none",
      next: "Commit changes, or start new increment with RED",
    },
  }

  // Valid phase transitions
    const validTransitions: Record<TDDPhase, TDDPhase[]> = {
      idle: ["red"],
      red: ["green"],
      green: ["refactor", "red"], // Refactor after pass, or back to red if test was wrong
      refactor: ["verify", "green"], // Verify after refactor, or back to green if refactor broke something
      verify: ["red", "idle"], // New increment or done
    }

  return {
    // Track active agent
    "chat.message": async (input) => {
      if (input.agent) {
        sessionAgents.set(input.sessionID, input.agent)
      }
    },

    // =========================================================================
    // HOOK: tool.execute.before - ENFORCEMENT
    // =========================================================================
    "tool.execute.before": async (input, output) => {
      const state = getState(input.sessionID)
      const { tool: toolName } = input
      const args = output.args || {}

      // STRICT ENFORCEMENT for tdd-dev agent
      // If using tdd-dev agent, MUST start TDD before any SOURCE CODE changes
      // Doc/config files and git operations are allowed when idle (e.g., step 8 doc updates)
      const agent = sessionAgents.get(input.sessionID)
      if (agent && agent.includes("tdd-dev") && (!state || state.phase === "idle")) {
        const isFileModification = toolName === "write" || toolName === "edit"
        
        if (isFileModification) {
          const filePath: string = args.filePath || args.target_file || ""
          
          // Allow doc/config files when idle (documentation updates, config changes)
          if (filePath && isDocOrConfigFile(filePath)) {
            // Permitted — doc/config files don't need TDD
            return
          }
          
          throw new Error(
            `⛔ TDD ENFORCEMENT: You are using the TDD Developer agent.\n\n` +
            `You MUST start TDD mode before modifying source code files:\n` +
            `-> call tdd_gate(action: "start") first.\n\n` +
            `Allowed without TDD mode:\n` +
            `  ✅ Doc/config files (.md, .yaml, .json, .toml, etc.)\n` +
            `  ✅ Git operations (commit, add, push)\n\n` +
            `If this file doesn't need TDD (Route S triage), use:\n` +
            `  tdd_gate(action: "skip", reason: "Route S: <reason>")`
          )
        }
      }

      // Skip if TDD mode not active
      if (!state || state.phase === "idle") {
        return
      }

      // Check for one-time skip
      if (state.skipActive) {
        state.skipActive = false // Reset after one use
        return
      }

      const info = phaseInfo[state.phase]

      // === RED Phase Enforcement ===
      if (state.phase === "red") {
        if (toolName === "write" || toolName === "edit") {
          const filePath: string = args.filePath || args.target_file || ""
          if (filePath && isImplFile(filePath, state.config, state.implFile, state.testFile)) {
            const content: string = args.content || ""
            
            // Allow ONE stub creation per impl file during RED phase
            // This enables proper assertion failures instead of import errors
            if (toolName === "write" && !state.stubbedFiles.has(filePath)) {
              // Check if content looks like a stub
              if (isStubContent(content)) {
                // Allow stub creation, track it
                state.stubbedFiles.add(filePath)
                logAudit({
                  type: "stub_allowed",
                  sessionID: input.sessionID,
                  phase: state.phase,
                  blockedFile: filePath,
                  reason: "Stub file created during RED phase (allowed for proper test failures)",
                })
                return // Allow this write
              }
            }
            
            // Block non-stub writes and edits to already-stubbed files
            const reason = state.stubbedFiles.has(filePath)
              ? "File already stubbed - no further edits during RED phase"
              : "Cannot modify implementation file during RED phase"
            
            logAudit({
              type: "block",
              sessionID: input.sessionID,
              phase: state.phase,
              blockedTool: toolName,
              blockedFile: filePath,
              reason,
            })

            throw new Error(
              `${info.emoji} TDD BLOCKED: Cannot modify "${filePath}" during RED phase\n\n` +
                `In RED phase, you can ONLY modify test files.\n` +
                (state.stubbedFiles.has(filePath) 
                  ? `This file was already stubbed. Transition to GREEN to implement.\n`
                  : `You can create a STUB file (returns 0/null/undefined) to get proper test failures.\n`) +
                `Then call:\n` +
                `  tdd_gate(action: "transition", to: "green", evidence: "<test failure output>")\n\n` +
                `Or use tdd_gate(action: "skip", reason: "...") for emergency bypass.`
            )
          }
        }

        if (toolName === "bash" && isGitCommit(args.command || "")) {
          logAudit({
            type: "block",
            sessionID: input.sessionID,
            phase: state.phase,
            blockedTool: "git commit",
            reason: "Cannot commit during RED phase",
          })

          throw new Error(
            `${info.emoji} TDD BLOCKED: Cannot commit during RED phase\n\n` +
              `Complete the RED → GREEN → REFACTOR → VERIFY cycle first.`
          )
        }
      }

      // === GREEN Phase Enforcement ===
      // Only implementation files allowed — test files, doc/config files, and commits blocked
      if (state.phase === "green") {
        if (toolName === "write" || toolName === "edit") {
          const filePath: string = args.filePath || args.target_file || ""
          if (filePath && isTestFile(filePath, state.config, state.testFile)) {
            logAudit({
              type: "block",
              sessionID: input.sessionID,
              phase: state.phase,
              blockedTool: toolName,
              blockedFile: filePath,
              reason: "Cannot modify test file during GREEN phase",
            })

            throw new Error(
              `${info.emoji} TDD BLOCKED: Cannot modify test "${filePath}" during GREEN phase\n\n` +
                `Focus on making the existing test pass.\n` +
                `If the test is wrong, transition back to RED:\n` +
                `  tdd_gate(action: "transition", to: "red", evidence: "test incorrect because...")`
            )
          }
          
          // Block doc/config files during GREEN — focus on implementation only
          if (filePath && isDocOrConfigFile(filePath)) {
            logAudit({
              type: "block",
              sessionID: input.sessionID,
              phase: state.phase,
              blockedTool: toolName,
              blockedFile: filePath,
              reason: "Cannot modify doc/config files during GREEN phase — focus on implementation",
            })

            throw new Error(
              `${info.emoji} TDD BLOCKED: Cannot modify "${filePath}" during GREEN phase\n\n` +
                `GREEN phase is for implementation only. Doc/config updates belong in REFACTOR or after exit.\n` +
                `Make the test pass first, then transition to REFACTOR:\n` +
                `  tdd_gate(action: "transition", to: "refactor", evidence: "<test pass output>")`
            )
          }
        }

        if (toolName === "bash" && isGitCommit(args.command || "")) {
          logAudit({
            type: "block",
            sessionID: input.sessionID,
            phase: state.phase,
            blockedTool: "git commit",
            reason: "Cannot commit during GREEN phase",
          })

          throw new Error(
            `${info.emoji} TDD BLOCKED: Cannot commit during GREEN phase\n\n` +
              `Make the test pass first, then transition to REFACTOR:\n` +
              `  tdd_gate(action: "transition", to: "refactor", evidence: "<test pass output>")`
          )
        }
      }

      // === REFACTOR Phase Enforcement ===
      // All files editable (test + impl + docs), but commits blocked
      if (state.phase === "refactor") {
        if (toolName === "bash" && isGitCommit(args.command || "")) {
          logAudit({
            type: "block",
            sessionID: input.sessionID,
            phase: state.phase,
            blockedTool: "git commit",
            reason: "Cannot commit during REFACTOR phase — verify full suite first",
          })

          throw new Error(
            `🔵 TDD BLOCKED: Cannot commit during REFACTOR phase\n\n` +
              `Run the full test suite and transition to VERIFY first:\n` +
              `  tdd_gate(action: "transition", to: "verify", evidence: "<full suite pass output>")`
          )
        }
      }

      // === VERIFY Phase - No blocks, commits allowed ===
    },

    // =========================================================================
    // HOOK: tool.execute.after - TRACK TEST RESULTS
    // =========================================================================
    "tool.execute.after": async (input, output) => {
      const state = getState(input.sessionID)
      if (!state || state.phase === "idle") return

      // Detect test runs and parse results
      if (input.tool === "bash") {
        const command: string = output.args?.command || ""
        const stdout: string = output.metadata?.stdout || ""

        if (isTestCommand(command, state.config)) {
          const { passing, summary } = parseTestResult(stdout)
          state.testsPassing = passing

          logAudit({
            type: "test_result",
            sessionID: input.sessionID,
            phase: state.phase,
            testsPassing: passing,
            evidence: summary,
          })
        }
      }
    },

    // =========================================================================
    // TOOLS
    // =========================================================================
    tool: {
      // =====================================================================
      // tdd_gate - Main control tool
      // =====================================================================
      tdd_gate: tool({
        description: `TDD phase gate - enforces Test-Driven Development discipline.

ACTIONS:
- start: Begin TDD mode (enters RED phase). Activates enforcement.
- transition: Move to next phase (requires evidence from test output).
- status: Check current phase and state.
- skip: Emergency one-time bypass (logged for audit).
- exit: Exit TDD mode (removes enforcement). Git ops and doc edits allowed after exit.

PHASES (4-phase cycle):
- RED: Write failing test. Only test files can be modified (stubs allowed).
- GREEN: Make test pass. Only implementation files can be modified.
- REFACTOR: Clean up code. All files editable, but commits blocked.
- VERIFY: Run full suite. All files editable, commits allowed.

EVIDENCE REQUIREMENTS:
- RED → GREEN: Must show test FAILING (assertion failure, not error). Min 20 chars.
- GREEN → REFACTOR: Must show test PASSING. Min 20 chars.
- REFACTOR → VERIFY: Must show full suite PASSING. Min 20 chars.

ENFORCEMENT:
- Violations throw errors that block the tool execution
- Skip provides one-time bypass for emergencies (logged)
- All events are logged for audit via tdd_audit tool
- When idle: doc/config files and git ops allowed, source code blocked
- Use TDD triage (Route T/V/S) to decide if work needs full TDD`,

        args: {
          action: tool.schema
            .enum(["start", "transition", "status", "skip", "exit"])
            .describe("Action to perform"),
          to: tool.schema
            .enum(["red", "green", "refactor", "verify"])
            .optional()
            .describe("Target phase (required for transition)"),
          testFile: tool.schema.string().optional().describe("Test file path (for start)"),
          implFile: tool.schema.string().optional().describe("Implementation file path (for start)"),
          evidence: tool.schema
            .string()
            .optional()
            .describe("Evidence for transition (test output required)"),
          reason: tool.schema.string().optional().describe("Reason for skip (required for skip action)"),
        },

        async execute(args, ctx): Promise<string> {
          const sessionID = ctx.sessionID
          const state = getState(sessionID)

          // =================================================================
          // ACTION: start
          // =================================================================
          if (args.action === "start") {
            if (state && state.phase !== "idle") {
              return (
                `❌ TDD mode already active (phase: ${state.phase})\n\n` +
                `Use tdd_gate(action: "exit") first, or tdd_gate(action: "status") to check state.`
              )
            }

            // Auto-detect project type
            const config = await detectProjectType(directory)

            const newState: TDDState = {
              phase: "red",
              testFile: args.testFile,
              implFile: args.implFile,
              config,
              evidence: [],
              testsPassing: false,
              incrementCount: 1,
              startedAt: Date.now(),
              skipActive: false,
              stubbedFiles: new Set<string>(),
            }
            sessions.set(sessionID, newState)

            logAudit({
              type: "start",
              sessionID,
              phase: "red",
              evidence: `testFile: ${args.testFile || "auto"}, implFile: ${args.implFile || "auto"}`,
            })

            const info = phaseInfo.red
            return (
              `${info.emoji} TDD MODE STARTED - Phase: ${info.name}\n\n` +
              `**Project Type:** ${config.type}\n` +
              `**Test File:** ${args.testFile || "(auto-detect from patterns)"}\n` +
              `**Impl File:** ${args.implFile || "(auto-detect from patterns)"}\n\n` +
              `**CYCLE:** 🔴 RED → 🟢 GREEN → 🔵 REFACTOR → ✅ VERIFY\n\n` +
              `**ENFORCEMENT ACTIVE:**\n` +
              `  ✅ Allowed: ${info.allowed}\n` +
              `  ❌ Blocked: ${info.blocked}\n\n` +
              `**NEXT STEP:**\n` +
              `1. Write a failing test\n` +
              `2. Run the test, confirm it FAILS (not errors)\n` +
              `3. Call: tdd_gate(action: "transition", to: "green", evidence: "<paste test failure>")`
            )
          }

          // =================================================================
          // ACTION: transition
          // =================================================================
          if (args.action === "transition") {
            if (!state || state.phase === "idle") {
              return `❌ TDD mode not active\n\nCall tdd_gate(action: "start") first.`
            }

            if (!args.to) {
              return (
                `❌ Missing 'to' parameter\n\n` +
                `Specify target phase: tdd_gate(action: "transition", to: "green", evidence: "...")`
              )
            }

            if (!args.evidence) {
              return (
                `❌ Missing 'evidence' parameter\n\n` +
                `You must provide evidence (test output) to transition phases.\n` +
                `Run your tests and paste the relevant output.`
              )
            }

            // Validate transition
            if (!validTransitions[state.phase]?.includes(args.to)) {
              const valid = validTransitions[state.phase]?.map((p) => p.toUpperCase()).join(", ") || "none"
              return (
                `❌ Invalid transition: ${state.phase.toUpperCase()} → ${args.to.toUpperCase()}\n\n` +
                `Valid transitions from ${state.phase.toUpperCase()}: ${valid}`
              )
            }

            // Validate evidence content
            const evidenceLower = args.evidence.toLowerCase()
            
            // Minimum evidence length — prevent trivial/gaming evidence
            if (args.evidence.trim().length < 20) {
              return (
                `❌ Evidence too short (${args.evidence.trim().length} chars, minimum 20)\n\n` +
                `Paste actual test runner output, not a summary.\n` +
                `Example: "FAIL src/auth.test.ts > validates email > rejects empty string"`
              )
            }

            if (state.phase === "red" && args.to === "green") {
              const hasFailure =
                evidenceLower.includes("fail") ||
                evidenceLower.includes("error") ||
                evidenceLower.includes("assertion") ||
                evidenceLower.includes("expected") ||
                evidenceLower.includes("not equal") ||
                evidenceLower.includes("rejected") ||
                evidenceLower.includes("threw")
              
              // Reject contradictory evidence (has both pass and fail — ambiguous)
              const hasPass = evidenceLower.includes("pass") || evidenceLower.includes("success")
              const hasFail = evidenceLower.includes("fail") || evidenceLower.includes("error")
              const isContradictory = hasPass && hasFail && !evidenceLower.includes("1 failed")
              
              if (!hasFailure) {
                return (
                  `❌ Evidence does not show test failure\n\n` +
                  `To transition RED → GREEN, evidence must show the test FAILING.\n` +
                  `Run your test and paste the failure output.\n\n` +
                  `Example: "FAIL: expected 'foo' but got undefined"`
                )
              }
              
              if (isContradictory) {
                return (
                  `⚠️ Evidence is ambiguous — contains both pass and fail indicators\n\n` +
                  `Paste ONLY the relevant test failure output for the test you just wrote.\n` +
                  `If other tests pass, that's fine — focus on the NEW test's failure.`
                )
              }
            }

            if (state.phase === "green" && args.to === "refactor") {
              const hasPassing =
                evidenceLower.includes("pass") ||
                evidenceLower.includes("ok") ||
                evidenceLower.includes("success") ||
                /\d+\/\d+/.test(args.evidence) // e.g., "47/47"
              
              // Reject if evidence shows failures
              const hasFailure = evidenceLower.includes("fail") && !evidenceLower.includes("0 failed")
              
              if (!hasPassing || hasFailure) {
                return (
                  `❌ Evidence does not show tests passing\n\n` +
                  `To transition GREEN → REFACTOR, your new test must PASS.\n` +
                  `Run your tests and paste the success output.\n\n` +
                  `Example: "PASS: 47/47 tests passed, 0 failed"`
                )
              }
            }

            if (state.phase === "refactor" && args.to === "verify") {
              const hasPassing =
                evidenceLower.includes("pass") ||
                evidenceLower.includes("ok") ||
                evidenceLower.includes("success") ||
                /\d+\/\d+/.test(args.evidence)
              
              // Reject if evidence shows failures
              const hasFailure = evidenceLower.includes("fail") && !evidenceLower.includes("0 failed")
              
              if (!hasPassing || hasFailure) {
                return (
                  `❌ Evidence does not show full suite passing\n\n` +
                  `To transition REFACTOR → VERIFY, the FULL test suite must pass.\n` +
                  `Run ALL tests (not just the new one) and paste the output.\n\n` +
                  `Example: "PASS: 142/142 tests passed, 0 failed"`
                )
              }
            }

            // Record evidence and transition
            const previousPhase = state.phase
            state.evidence.push({
              phase: previousPhase,
              content: args.evidence.slice(0, 500),
              timestamp: Date.now(),
            })
            state.phase = args.to

            // Track new increment if going back to red
            if (args.to === "red" && previousPhase !== "idle") {
              state.incrementCount++
              state.stubbedFiles.clear() // Reset stubbed files for new increment
            }

            logAudit({
              type: "transition",
              sessionID,
              phase: previousPhase,
              targetPhase: args.to,
              evidence: args.evidence.slice(0, 200),
            })

            const info = phaseInfo[args.to]
            return (
              `${info.emoji} TRANSITION: ${previousPhase.toUpperCase()} → ${args.to.toUpperCase()}\n\n` +
              `**Evidence recorded** ✓\n\n` +
              `**ENFORCEMENT:**\n` +
              `  ✅ Allowed: ${info.allowed}\n` +
              `  ❌ Blocked: ${info.blocked}\n\n` +
              `**NEXT:** ${info.next}`
            )
          }

          // =================================================================
          // ACTION: status
          // =================================================================
          if (args.action === "status") {
            if (!state || state.phase === "idle") {
              return (
                `📊 TDD STATUS: Not active\n\n` +
                `Call tdd_gate(action: "start", testFile: "path/to/test") to begin.`
              )
            }

            const info = phaseInfo[state.phase]
            const duration = Math.round((Date.now() - state.startedAt) / 1000 / 60)
            const recentEvidence = state.evidence
              .slice(-2)
              .map((e) => `  [${e.phase.toUpperCase()}] ${e.content.slice(0, 60)}...`)
              .join("\n")

            return (
              `📊 TDD STATUS\n\n` +
              `**Phase:** ${info.emoji} ${info.name}\n` +
              `**Project Type:** ${state.config.type}\n` +
              `**Test File:** ${state.testFile || "(auto-detect)"}\n` +
              `**Impl File:** ${state.implFile || "(auto-detect)"}\n` +
              `**Tests Passing:** ${state.testsPassing ? "✅ Yes" : "❌ No"}\n` +
              `**Increment:** #${state.incrementCount}\n` +
              `**Duration:** ${duration} min\n` +
              `**Evidence Count:** ${state.evidence.length}\n\n` +
              `**Enforcement:**\n` +
              `  ✅ Allowed: ${info.allowed}\n` +
              `  ❌ Blocked: ${info.blocked}\n\n` +
              `**Recent Evidence:**\n${recentEvidence || "  (none)"}\n\n` +
              `**Next:** ${info.next}`
            )
          }

          // =================================================================
          // ACTION: skip
          // =================================================================
          if (args.action === "skip") {
            if (!state || state.phase === "idle") {
              return `❌ TDD mode not active - nothing to skip.`
            }

            if (!args.reason) {
              return (
                `❌ Missing 'reason' parameter\n\n` +
                `You must provide a reason for the skip:\n` +
                `  tdd_gate(action: "skip", reason: "hotfix for production outage")`
              )
            }

            // Enable one-time skip
            state.skipActive = true

            logAudit({
              type: "skip",
              sessionID,
              phase: state.phase,
              reason: args.reason,
            })

            return (
              `⚠️ TDD ENFORCEMENT SKIPPED\n\n` +
              `**Phase:** ${state.phase.toUpperCase()} → BYPASSED (one action)\n` +
              `**Reason:** "${args.reason}"\n` +
              `**Logged for audit:** ✓\n\n` +
              `**WARNING:** This is an exception. TDD discipline ensures code quality.\n` +
              `Enforcement will resume after the next tool execution.\n\n` +
              `Resume normal TDD flow as soon as possible.`
            )
          }

          // =================================================================
          // ACTION: exit
          // =================================================================
          if (args.action === "exit") {
            if (!state || state.phase === "idle") {
              return `✅ TDD mode was not active.`
            }

            const duration = Math.round((Date.now() - state.startedAt) / 1000 / 60)
            const summary = {
              duration,
              increments: state.incrementCount,
              transitions: state.evidence.length,
              finalPhase: state.phase,
            }

            logAudit({
              type: "exit",
              sessionID,
              phase: state.phase,
              evidence: JSON.stringify(summary),
            })

            sessions.delete(sessionID)

            return (
              `✅ TDD MODE EXITED\n\n` +
              `**Session Summary:**\n` +
              `  Duration: ${duration} min\n` +
              `  Increments: ${summary.increments}\n` +
              `  Transitions: ${summary.transitions}\n` +
              `  Final Phase: ${summary.finalPhase.toUpperCase()}\n\n` +
              `Enforcement disabled. You can now modify any files freely.\n\n` +
              `To restart: tdd_gate(action: "start", testFile: "path/to/test")`
            )
          }

          return `❌ Unknown action: ${args.action}`
        },
      }),

      // =====================================================================
      // tdd_audit - Query audit log
      // =====================================================================
      tdd_audit: tool({
        description: `Query TDD enforcement audit log for session analysis.

Returns events logged during TDD sessions:
- start: TDD mode activated
- transition: Phase changes with evidence (RED→GREEN→REFACTOR→VERIFY)
- block: Enforcement blocked a tool
- skip: Emergency bypass used
- exit: TDD mode deactivated
- test_result: Test execution detected

Use for post-session analysis to check TDD compliance.`,

        args: {
          sessionID: tool.schema.string().optional().describe("Filter by session ID (default: current)"),
          type: tool.schema
            .enum(["all", "transition", "block", "skip"])
            .optional()
            .describe("Filter by event type (default: all)"),
          format: tool.schema
            .enum(["summary", "detailed", "json"])
            .optional()
            .describe("Output format (default: summary)"),
        },

        async execute(args, ctx): Promise<string> {
          const targetSession = args.sessionID || ctx.sessionID
          const eventType = args.type || "all"
          const format = args.format || "summary"

          // Filter events
          let events = auditLog.filter((e) => e.sessionID === targetSession || args.sessionID === undefined)

          if (eventType !== "all") {
            events = events.filter((e) => e.type === eventType)
          }

          if (events.length === 0) {
            return (
              `📊 TDD AUDIT: No events found\n\n` +
              `No TDD enforcement events logged${targetSession ? ` for session ${targetSession.slice(0, 8)}...` : ""}.\n\n` +
              `Events are logged when:\n` +
              `- tdd_gate(action: "start") is called\n` +
              `- Phase transitions occur\n` +
              `- Enforcement blocks a tool\n` +
              `- Skip bypass is used`
            )
          }

          // Calculate summary stats
          const stats = {
            total: events.length,
            starts: events.filter((e) => e.type === "start").length,
            transitions: events.filter((e) => e.type === "transition").length,
            blocks: events.filter((e) => e.type === "block").length,
            skips: events.filter((e) => e.type === "skip").length,
            stubsAllowed: events.filter((e) => e.type === "stub_allowed").length,
            exits: events.filter((e) => e.type === "exit").length,
            testResults: events.filter((e) => e.type === "test_result").length,
          }

          // Calculate compliance (stubs don't count against compliance - they're expected TDD behavior)
          const totalActions = stats.transitions + stats.skips
          const compliance = totalActions > 0 ? Math.round((stats.transitions / totalActions) * 100) : 100

          if (format === "json") {
            return JSON.stringify({ stats, compliance, events }, null, 2)
          }

          const lines: string[] = [
            `📊 TDD AUDIT REPORT`,
            ``,
            `**Sessions:** ${new Set(events.map((e) => e.sessionID)).size}`,
            `**Total Events:** ${stats.total}`,
            `**Compliance:** ${compliance}%`,
            ``,
            `### Event Summary`,
            ``,
            `| Type | Count |`,
            `|------|-------|`,
            `| Starts | ${stats.starts} |`,
            `| Transitions | ${stats.transitions} |`,
            `| Stubs Allowed | ${stats.stubsAllowed} |`,
            `| Blocks | ${stats.blocks} |`,
            `| Skips | ${stats.skips} |`,
            `| Exits | ${stats.exits} |`,
            `| Test Results | ${stats.testResults} |`,
            ``,
          ]

          if (stats.stubsAllowed > 0) {
            lines.push(`### ✅ Stubs Created (Expected TDD Behavior)`)
            lines.push(``)
            lines.push(`Stub files were created during RED phase to enable proper assertion failures.`)
            lines.push(`This is normal TDD workflow - stubs don't count against compliance.`)
            lines.push(``)
            events
              .filter((e) => e.type === "stub_allowed")
              .forEach((e) => {
                const time = new Date(e.timestamp).toLocaleTimeString()
                lines.push(`- [${time}] \`${e.blockedFile}\``)
              })
            lines.push(``)
          }

          if (stats.blocks > 0) {
            lines.push(`### ❌ Enforcement Blocks`)
            lines.push(``)
            events
              .filter((e) => e.type === "block")
              .forEach((e) => {
                const time = new Date(e.timestamp).toLocaleTimeString()
                lines.push(`- [${time}] **${e.phase?.toUpperCase()}**: ${e.reason}`)
                if (e.blockedFile) {
                  lines.push(`  File: \`${e.blockedFile}\``)
                }
              })
            lines.push(``)
          }

          if (stats.skips > 0) {
            lines.push(`### ⚠️ Emergency Skips`)
            lines.push(``)
            events
              .filter((e) => e.type === "skip")
              .forEach((e) => {
                const time = new Date(e.timestamp).toLocaleTimeString()
                lines.push(`- [${time}] **${e.phase?.toUpperCase()}**: "${e.reason}"`)
              })
            lines.push(``)
          }

          if (format === "detailed") {
            lines.push(`### All Events`)
            lines.push(``)
            events.forEach((e) => {
              const time = new Date(e.timestamp).toLocaleTimeString()
              const phase = e.phase ? `[${e.phase.toUpperCase()}]` : ""
              const target = e.targetPhase ? ` → ${e.targetPhase.toUpperCase()}` : ""
              lines.push(`- [${time}] **${e.type}** ${phase}${target}`)
              if (e.evidence) {
                lines.push(`  Evidence: ${e.evidence.slice(0, 80)}...`)
              }
              if (e.reason) {
                lines.push(`  Reason: ${e.reason}`)
              }
            })
            lines.push(``)
          }

          lines.push(`---`)
          lines.push(``)
          lines.push(`*Compliance = transitions / (transitions + skips)*`)

          return lines.join("\n")
        },
      }),
    },
  }
}

export default TDDEnforcementPlugin
