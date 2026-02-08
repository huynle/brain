// @ts-nocheck - This file is installed to OpenCode, not compiled by brain-api
/**
 * Brain Planning Enforcement Plugin
 *
 * Enforces project-aware planning discipline with phase-based workflow:
 *
 * PHASES:
 * - idle: No active planning session
 * - init: Loading project docs (PRD, architecture)
 * - understand: Capturing user intent through clarifying questions
 * - design: Drafting approach + architecture check (subagent)
 * - approve: User approves doc updates, plan saved
 *
 * FEATURES:
 * 1. Phase state machine with enforcement
 * 2. Project documentation discovery (docs/prd.md, docs/architecture.md)
 * 3. User intent capture and storage
 * 4. Doc update proposal formatting
 * 5. Agent-scoped enforcement (only for brain-planner agent)
 * 6. Audit logging for compliance
 */

import type { Plugin } from "@opencode-ai/plugin"
import { tool } from "@opencode-ai/plugin"
import { spawn } from "child_process"
import { existsSync } from "fs"
import { join } from "path"
import { homedir } from "os"

// Brain directory for zk notebook
const BRAIN_DIR = join(homedir(), ".opencode", "brain")
const ZK_DIR = join(BRAIN_DIR, ".zk")

// Check if zk notebook exists
const zkNotebookExists = existsSync(ZK_DIR)

interface ExecResult {
  stdout: string
  stderr: string
  exitCode: number
}

// Execute zk command
async function execZk(args: string[], timeout = 30000): Promise<ExecResult> {
  return new Promise((resolve, reject) => {
    const fullArgs = ["--notebook-dir", BRAIN_DIR, "--no-input", ...args]
    
    const proc = spawn("zk", fullArgs, {
      timeout,
      env: { ...process.env },
      cwd: BRAIN_DIR,
    })

    let stdout = ""
    let stderr = ""

    proc.stdout.on("data", (data) => { stdout += data.toString() })
    proc.stderr.on("data", (data) => { stderr += data.toString() })
    proc.on("error", (error) => reject(new Error(`Failed to spawn zk: ${error.message}`)))
    proc.on("close", (code) => resolve({ stdout, stderr, exitCode: code ?? 1 }))
  })
}

// Check if zk CLI is available
async function isZkAvailable(): Promise<boolean> {
  try {
    const result = await execZk(["--version"])
    return result.exitCode === 0
  } catch {
    return false
  }
}

// Parse zk JSON output
interface ZkNote {
  path: string
  title: string
  lead?: string
  body?: string
  tags?: string[]
  metadata?: Record<string, unknown>
  created?: string
  modified?: string
}

function parseZkJsonOutput(output: string): ZkNote[] {
  const trimmed = output.trim()
  if (!trimmed) return []

  try {
    const parsed = JSON.parse(trimmed)
    if (Array.isArray(parsed)) {
      return parsed.map((raw: any) => ({
        path: raw.path,
        title: raw.title,
        lead: raw.lead || undefined,
        body: raw.body || undefined,
        tags: raw.tags || [],
        metadata: raw.metadata || {},
        created: raw.created || undefined,
        modified: raw.modified || undefined,
      }))
    }
    return [parsed]
  } catch {
    const lines = trimmed.split("\n").filter(Boolean)
    const notes: ZkNote[] = []
    for (const line of lines) {
      try {
        const raw = JSON.parse(line)
        notes.push({ path: raw.path, title: raw.title, tags: raw.tags || [], metadata: raw.metadata || {} })
      } catch { continue }
    }
    return notes
  }
}

// Extract ID from zk note path (e.g., "projects/abc/plan/x1y2z3a4.md" -> "x1y2z3a4")
function extractIdFromPath(path: string): string {
  const filename = path.split("/").pop() || path
  return filename.replace(/\.md$/, "")
}

// Format relative time (e.g., "2d ago", "1w ago")
function formatRelativeTime(dateStr: string | undefined): string {
  if (!dateStr) return "unknown"
  const date = new Date(dateStr)
  const now = Date.now()
  const diffMs = now - date.getTime()
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  
  if (diffDays === 0) return "today"
  if (diffDays === 1) return "1d ago"
  if (diffDays < 7) return `${diffDays}d ago`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`
  if (diffDays < 365) return `${Math.floor(diffDays / 30)}mo ago`
  return `${Math.floor(diffDays / 365)}y ago`
}

// Extract status from note tags
function extractStatus(note: ZkNote): string {
  const statuses = ["draft", "active", "in_progress", "blocked", "completed", "superseded", "archived"]
  for (const status of statuses) {
    if (note.tags?.includes(status)) return status
  }
  return "active"
}

// =============================================================================
// TYPES
// =============================================================================

const PHASES = ["idle", "init", "understand", "design", "approve"] as const
type Phase = (typeof PHASES)[number]

type EnforcementLevel = "advisory" | "strict"

interface UserIntent {
  problem: string
  success_criteria: string[]
  scope: { in: string[]; out: string[] }
  constraints: string[]
  captured_at: number
}

interface ProjectDocs {
  prd: { found: boolean; path: string; content: string; summary: string }
  arch: { found: boolean; path: string; content: string; summary: string }
  other: string[]
  discovered_at: number
}

// Discovery results from plan_discover_docs
interface DocCandidate {
  index: number
  path: string
  modified: string
  summary: string
  content: string
}

interface BrainPlanCandidate {
  index: number
  id: string
  title: string
  status: string
  age: string
  lead?: string
}

interface BrainExplorationCandidate {
  index: number
  id: string
  title: string
  age: string
  lead?: string
}

interface DiscoveryResults {
  // File-based docs
  prdCandidates: DocCandidate[]
  archCandidates: DocCandidate[]
  otherDocs: string[]

  // Brain entries
  matchingPlans: BrainPlanCandidate[]
  relatedExplorations: BrainExplorationCandidate[]

  discoveredAt: number
}

// Confirmed selections from plan_confirm_docs
interface ConfirmedDocs {
  prdPath: string | null              // null = create new during APPROVE
  archPath: string | null             // null = create new during APPROVE
  prdContent?: string                 // Cached content if path selected
  archContent?: string                // Cached content if path selected

  existingPlanId: string | null       // Brain ID if updating existing
  existingPlanTitle: string | null
  isUpdatingExistingPlan: boolean

  includedExplorationIds: string[]
  includedOtherDocs: string[]

  confirmedAt: number
}

interface PlanningState {
  // Phase tracking
  phase: Phase
  phaseStartedAt: number

  // Legacy compatibility
  brainChecked: boolean
  planSaved: boolean
  currentPlanId?: string
  currentPlanTitle?: string
  objective?: string
  searchQueries: string[]
  savedPlans: Array<{ id: string; title: string; timestamp: number }>
  enforcementLevel: EnforcementLevel
  startedAt: number

  // New: Project-aware planning
  projectDocs?: ProjectDocs
  userIntent?: UserIntent
  archCheckResult?: string
  proposedDocUpdates?: {
    prd: string
    arch: string
    approved: boolean
  }
  newRequirementIds: string[]
  skipActive: boolean

  // Interactive discovery (new)
  discoveryResults?: DiscoveryResults
  confirmedDocs?: ConfirmedDocs
  
  // Track discovery to enforce user confirmation
  discoveryMessageId?: string  // Message ID when discovery was called
  awaitingUserConfirmation: boolean  // True after discovery, false after user responds
}

interface AuditEvent {
  type:
    | "session_start"
    | "phase_transition"
    | "brain_check"
    | "plan_save"
    | "plan_update"
    | "compliance_check"
    | "block"
    | "reminder"
    | "skip"
    | "docs_discovered"
    | "intent_captured"
    | "arch_check"
    | "doc_update_proposed"
    | "doc_update_approved"
  sessionID: string
  timestamp: number
  phase?: Phase
  targetPhase?: Phase
  details?: string
}

// =============================================================================
// CONSTANTS
// =============================================================================

const VALID_TRANSITIONS: Record<Phase, Phase[]> = {
  idle: ["init"],
  init: ["understand"],
  understand: ["design"],
  design: ["approve", "understand"], // Can go back to refine intent
  approve: ["idle", "design"], // Complete or revise approach
}

const PHASE_INFO: Record<Phase, { emoji: string; name: string; description: string; next: string }> = {
  idle: {
    emoji: "⚪",
    name: "IDLE",
    description: "No active planning session",
    next: 'Call plan_phase(action: "start", objective: "...") to begin',
  },
  init: {
    emoji: "📂",
    name: "INIT",
    description: "Loading project documentation",
    next: "Call plan_discover_docs() to scan for PRD and architecture docs",
  },
  understand: {
    emoji: "🎯",
    name: "UNDERSTAND",
    description: "Capturing user intent through clarifying questions",
    next: "Ask clarifying questions, then call plan_capture_intent(...)",
  },
  design: {
    emoji: "✏️",
    name: "DESIGN",
    description: "Drafting approach and checking against architecture",
    next: "Draft design, dispatch explore subagent for arch check, present impact analysis",
  },
  approve: {
    emoji: "✅",
    name: "APPROVE",
    description: "User approves doc updates and plan is saved",
    next: "Present doc changes, get approval, save plan to brain",
  },
}

// =============================================================================
// PLUGIN
// =============================================================================

export const BrainPlanningPlugin: Plugin = async ({ directory }) => {
  // Session-scoped state
  const sessions = new Map<string, PlanningState>()
  const sessionAgents = new Map<string, string>()

  // Audit log
  const auditLog: AuditEvent[] = []

  // Helper to get or create state
  const getOrCreateState = (sessionID: string): PlanningState => {
    if (!sessions.has(sessionID)) {
      sessions.set(sessionID, {
        phase: "idle",
        phaseStartedAt: Date.now(),
        brainChecked: false,
        planSaved: false,
        searchQueries: [],
        savedPlans: [],
        enforcementLevel: "advisory",
        startedAt: Date.now(),
        newRequirementIds: [],
        skipActive: false,
        awaitingUserConfirmation: false,
      })
    }
    return sessions.get(sessionID)!
  }

  // Helper to log audit event
  const logAudit = (event: Omit<AuditEvent, "timestamp">) => {
    auditLog.push({ ...event, timestamp: Date.now() })
  }

  // Check if agent is the brain-planner (strict check for phase enforcement)
  const isBrainPlannerAgent = (agent: string | undefined): boolean => {
    if (!agent) return false
    return agent.includes("brain-planner")
  }

  // Check if agent is plan-related (looser check for tracking)
  const isPlanAgent = (agent: string | undefined): boolean => {
    if (!agent) return false
    return agent.includes("plan")
  }

  // Discover project documentation
  const discoverDocs = async (): Promise<ProjectDocs> => {
    const result: ProjectDocs = {
      prd: { found: false, path: "", content: "", summary: "" },
      arch: { found: false, path: "", content: "", summary: "" },
      other: [],
      discovered_at: Date.now(),
    }

    // Common paths to check for PRD
    const prdPaths = ["docs/prd.md", "docs/PRD.md", "PRD.md", "docs/requirements.md", "REQUIREMENTS.md"]

    // Common paths to check for architecture
    const archPaths = [
      "docs/architecture.md",
      "docs/ARCHITECTURE.md",
      "ARCHITECTURE.md",
      "docs/design.md",
      "docs/technical-design.md",
    ]

    // Check PRD paths
    for (const path of prdPaths) {
      try {
        const fullPath = `${directory}/${path}`
        const file = Bun.file(fullPath)
        if (await file.exists()) {
          const content = await file.text()
          result.prd = {
            found: true,
            path,
            content,
            summary: extractDocSummary(content, "PRD"),
          }
          break
        }
      } catch {
        // Continue checking
      }
    }

    // Check architecture paths
    for (const path of archPaths) {
      try {
        const fullPath = `${directory}/${path}`
        const file = Bun.file(fullPath)
        if (await file.exists()) {
          const content = await file.text()
          result.arch = {
            found: true,
            path,
            content,
            summary: extractDocSummary(content, "Architecture"),
          }
          break
        }
      } catch {
        // Continue checking
      }
    }

    // Scan docs/ folder for other files
    try {
      const docsDir = `${directory}/docs`
      const glob = new Bun.Glob("**/*.md")
      for await (const file of glob.scan({ cwd: docsDir, onlyFiles: true })) {
        const fullPath = `docs/${file}`
        if (fullPath !== result.prd.path && fullPath !== result.arch.path) {
          result.other.push(fullPath)
        }
      }
    } catch {
      // docs/ folder may not exist
    }

    return result
  }

  // Extract summary from document content
  const extractDocSummary = (content: string, docType: string): string => {
    const lines = content.split("\n")
    const summaryLines: string[] = []

    // Get title
    const titleMatch = content.match(/^#\s+(.+)$/m)
    if (titleMatch) {
      summaryLines.push(`**Title:** ${titleMatch[1]}`)
    }

    // Count sections
    const sections = content.match(/^##\s+.+$/gm) || []
    summaryLines.push(`**Sections:** ${sections.length}`)

    // For PRD, count requirements
    if (docType === "PRD") {
      const reqMatches = content.match(/PRD-REQ-\d+|REQ-\d+|\*\*REQ\d+\*\*/gi) || []
      if (reqMatches.length > 0) {
        summaryLines.push(`**Requirements:** ${reqMatches.length} identified`)
      }
    }

    // For Architecture, count decisions
    if (docType === "Architecture") {
      const decMatches = content.match(/ARCH-DEC-\d+|ADR-\d+|Decision\s*#?\d+/gi) || []
      if (decMatches.length > 0) {
        summaryLines.push(`**Decisions:** ${decMatches.length} documented`)
      }
    }

    // Get first paragraph as overview (skip title)
    let inOverview = false
    let overviewLines: string[] = []
    for (const line of lines) {
      if (line.startsWith("# ")) {
        inOverview = true
        continue
      }
      if (inOverview && line.startsWith("## ")) {
        break
      }
      if (inOverview && line.trim()) {
        overviewLines.push(line.trim())
        if (overviewLines.length >= 3) break
      }
    }
    if (overviewLines.length > 0) {
      summaryLines.push(`**Overview:** ${overviewLines.join(" ").slice(0, 200)}...`)
    }

    return summaryLines.join("\n")
  }

  return {
    // =========================================================================
    // HOOK: Track active agent and user messages
    // =========================================================================
    "chat.message": async (input) => {
      if (input.agent) {
        sessionAgents.set(input.sessionID, input.agent)

        // Initialize state for plan agents
        if (isPlanAgent(input.agent)) {
          const state = getOrCreateState(input.sessionID)
          if (!state.objective) {
            logAudit({
              type: "session_start",
              sessionID: input.sessionID,
              phase: state.phase,
              details: `Plan agent session started: ${input.agent}`,
            })
          }
        }
      }
      
      // Track user messages to clear awaiting confirmation flag
      // When user sends a message, they've had a chance to respond to discovery
      if (input.role === "user") {
        const state = sessions.get(input.sessionID)
        if (state && state.awaitingUserConfirmation) {
          state.awaitingUserConfirmation = false
          logAudit({
            type: "compliance_check",
            sessionID: input.sessionID,
            phase: state.phase,
            details: "User responded after discovery - confirmation now allowed",
          })
        }
      }
    },

    // =========================================================================
    // HOOK: Inject context during compaction
    // =========================================================================
    "experimental.session.compacting": async (input, output) => {
      const agent = sessionAgents.get(input.sessionID)
      if (!isPlanAgent(agent)) return

      const state = sessions.get(input.sessionID)
      if (!state) return

      // Build context reminder
      const contextLines: string[] = ["## 🧠 Brain Planning Context"]
      const info = PHASE_INFO[state.phase]

      contextLines.push("")
      contextLines.push(`**Current Phase:** ${info.emoji} ${info.name}`)
      contextLines.push(`**Description:** ${info.description}`)

      if (state.objective) {
        contextLines.push(`**Objective:** ${state.objective}`)
      }

      if (state.projectDocs) {
        contextLines.push("")
        contextLines.push("**Project Docs:**")
        contextLines.push(`- PRD: ${state.projectDocs.prd.found ? state.projectDocs.prd.path : "Not found"}`)
        contextLines.push(`- Architecture: ${state.projectDocs.arch.found ? state.projectDocs.arch.path : "Not found"}`)
      }

      if (state.userIntent) {
        contextLines.push("")
        contextLines.push("**Captured Intent:**")
        contextLines.push(`- Problem: ${state.userIntent.problem.slice(0, 100)}...`)
        contextLines.push(`- Criteria: ${state.userIntent.success_criteria.length} items`)
      }

      if (state.currentPlanId) {
        contextLines.push("")
        contextLines.push(`**Active Plan:** "${state.currentPlanTitle || "Untitled"}"`)
        contextLines.push(`**Brain ID:** \`${state.currentPlanId}\``)
      }

      contextLines.push("")
      contextLines.push(`**Next Step:** ${info.next}`)

      // Inject reminder
      output.context.push(contextLines.join("\n"))

      logAudit({
        type: "reminder",
        sessionID: input.sessionID,
        phase: state.phase,
        details: "Context injected during compaction",
      })
    },

    // =========================================================================
    // HOOK: Phase-based enforcement (tool.execute.before)
    // =========================================================================
    "tool.execute.before": async (input, output) => {
      const agent = sessionAgents.get(input.sessionID)
      const state = getOrCreateState(input.sessionID)
      const { tool: toolName } = input

      // Check for one-time skip
      if (state.skipActive) {
        state.skipActive = false
        return
      }

      // Enforce user confirmation before plan_confirm_docs (for all agents)
      if (toolName === "plan_confirm_docs" && state.awaitingUserConfirmation) {
        logAudit({
          type: "block",
          sessionID: input.sessionID,
          phase: state.phase,
          details: "Blocked plan_confirm_docs - awaiting user confirmation after discovery",
        })

        throw new Error(
          `⛔ HARD STOP: Cannot call plan_confirm_docs yet!\n\n` +
          `You MUST wait for the user to respond with their selections after plan_discover_docs.\n\n` +
          `**What to do:**\n` +
          `1. Present the discovery results to the user\n` +
          `2. Ask them to confirm their selections (PRD, architecture, existing plan)\n` +
          `3. WAIT for their response\n` +
          `4. THEN call plan_confirm_docs with their choices\n\n` +
          `This is a critical rule: @confirm_docs - ALWAYS ask user to confirm doc/plan selections.`
        )
      }

      // Only enforce phase-based blocking for brain-planner agent in strict mode
      if (!isBrainPlannerAgent(agent)) return
      if (state.enforcementLevel !== "strict") return

      // Phase-based blocking rules
      const blockRules: Record<Phase, { tools: string[]; message: string }> = {
        idle: { tools: [], message: "" },
        init: {
          tools: ["brain_save", "brain_execution_start", "brain_task_add"],
          message: "Complete INIT phase first. Call plan_discover_docs() to load project documentation.",
        },
        understand: {
          tools: ["brain_save", "brain_execution_start", "brain_task_add"],
          message: "Complete UNDERSTAND phase first. Capture user intent with plan_capture_intent().",
        },
        design: {
          tools: ["brain_save"],
          message: "Complete DESIGN phase first. Run architecture check and get user approval.",
        },
        approve: {
          tools: [],
          message: "",
        },
      }

      const rule = blockRules[state.phase]
      if (rule.tools.includes(toolName)) {
        logAudit({
          type: "block",
          sessionID: input.sessionID,
          phase: state.phase,
          details: `Blocked ${toolName} in ${state.phase} phase`,
        })

        throw new Error(
          `${PHASE_INFO[state.phase].emoji} PHASE ENFORCEMENT: Cannot use ${toolName} during ${state.phase.toUpperCase()} phase.\n\n` +
            `${rule.message}\n\n` +
            `Current phase: ${PHASE_INFO[state.phase].name}\n` +
            `Next step: ${PHASE_INFO[state.phase].next}\n\n` +
            `Use plan_phase(action: "skip", reason: "...") for emergency bypass.`
        )
      }

      // Legacy: Strict enforcement for brain checking
      if (state.enforcementLevel === "strict" && !state.brainChecked) {
        const blockedTools = ["brain_execution_start", "brain_task_add"]

        if (blockedTools.includes(toolName) && state.phase === "idle") {
          logAudit({
            type: "block",
            sessionID: input.sessionID,
            details: `Blocked ${toolName} - brain not checked`,
          })

          throw new Error(
            `⛔ BRAIN PLANNING ENFORCEMENT: You MUST check brain for existing plans first.\n\n` +
              `Call one of these before ${toolName}:\n` +
              `  brain_inject(query: "<objective>", type: "plan")\n` +
              `  brain_search(query: "<topic>", type: "plan")\n\n` +
              `This ensures you don't recreate existing work.\n\n` +
              `To disable strict mode: plan_gate(action: "set_enforcement", level: "advisory")`
          )
        }
      }
    },

    // =========================================================================
    // HOOK: Track brain tool results
    // =========================================================================
    "tool.execute.after": async (input, output) => {
      const agent = sessionAgents.get(input.sessionID)
      if (!isPlanAgent(agent)) return

      const state = getOrCreateState(input.sessionID)
      const { tool: toolName } = input
      const args = output.args || {}
      const result = output.result || ""

      // Track brain search/inject usage
      if (toolName === "brain_inject" || toolName === "brain_search") {
        state.brainChecked = true
        if (args.query) {
          state.searchQueries.push(args.query as string)
        }

        logAudit({
          type: "brain_check",
          sessionID: input.sessionID,
          phase: state.phase,
          details: `${toolName}(query: "${args.query}", type: "${args.type || "any"}")`,
        })
      }

      // Track brain_recall (also counts as checking)
      if (toolName === "brain_recall") {
        state.brainChecked = true
        // path can be either a full path or an 8-char zk ID
        if (args.path && typeof args.path === "string") {
          state.currentPlanId = args.path
        }

        logAudit({
          type: "brain_check",
          sessionID: input.sessionID,
          phase: state.phase,
          details: `brain_recall(path: "${args.path || ""}", title: "${args.title || ""}")`,
        })
      }

      // Track brain_save for plans
      if (toolName === "brain_save" && args.type === "plan") {
        state.planSaved = true

        const idMatch = result.match(/ID:\s*`([^`]+)`/)
        if (idMatch) {
          const savedPlan = {
            id: idMatch[1],
            title: (args.title as string) || "Untitled Plan",
            timestamp: Date.now(),
          }
          state.savedPlans.push(savedPlan)
          state.currentPlanId = savedPlan.id
          state.currentPlanTitle = savedPlan.title

          logAudit({
            type: "plan_save",
            sessionID: input.sessionID,
            phase: state.phase,
            details: `Saved "${savedPlan.title}" (ID: ${savedPlan.id})`,
          })
        }
      }
    },

    // =========================================================================
    // TOOLS
    // =========================================================================
    tool: {
      // =====================================================================
      // plan_phase - Phase state machine control
      // =====================================================================
      plan_phase: tool({
        description: `Control the planning phase state machine.

PHASES (in order):
1. IDLE → No active session
2. INIT → Loading project docs (PRD, architecture)
3. UNDERSTAND → Capturing user intent
4. DESIGN → Drafting approach + architecture check
5. APPROVE → User approves doc updates, plan saved

ACTIONS:
- start: Begin planning session (IDLE → INIT)
- transition: Move to next phase (requires completing current phase)
- status: Check current phase and requirements
- skip: Emergency one-time bypass (logged for audit)
- reset: Clear session and return to IDLE

TRANSITIONS:
- init → understand (after docs loaded)
- understand → design (after intent captured)
- design → approve (after arch check + user approval)
- design → understand (to refine intent)
- approve → idle (complete)
- approve → design (to revise approach)`,

        args: {
          action: tool.schema.enum(["start", "transition", "status", "skip", "reset"]).describe("Action to perform"),
          objective: tool.schema.string().optional().describe("Planning objective (for start action)"),
          to: tool.schema.enum(["init", "understand", "design", "approve", "idle"]).optional().describe("Target phase (for transition)"),
          reason: tool.schema.string().optional().describe("Reason for skip (required for skip action)"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          // =================================================================
          // ACTION: start
          // =================================================================
          if (args.action === "start") {
            if (state.phase !== "idle") {
              return (
                `❌ Planning session already active (phase: ${state.phase})\n\n` +
                `Use plan_phase(action: "reset") first, or plan_phase(action: "status") to check state.`
              )
            }

            state.phase = "init"
            state.phaseStartedAt = Date.now()
            state.objective = args.objective
            state.enforcementLevel = "strict" // Enable strict mode for new sessions

            logAudit({
              type: "phase_transition",
              sessionID: ctx.sessionID,
              phase: "idle",
              targetPhase: "init",
              details: `Planning started: "${args.objective}"`,
            })

            const info = PHASE_INFO.init
            return (
              `${info.emoji} PLANNING SESSION STARTED\n\n` +
              `**Objective:** ${args.objective || "(not specified)"}\n` +
              `**Phase:** ${info.name}\n` +
              `**Enforcement:** strict\n\n` +
              `---\n\n` +
              `## Next Step: Discover Project Documentation\n\n` +
              `Call \`plan_discover_docs()\` to scan for:\n` +
              `- docs/prd.md (Product Requirements)\n` +
              `- docs/architecture.md (Architecture Decisions)\n` +
              `- Other docs/*.md files\n\n` +
              `If no docs are found, you'll be prompted to create them.`
            )
          }

          // =================================================================
          // ACTION: transition
          // =================================================================
          if (args.action === "transition") {
            if (!args.to) {
              return `❌ Missing 'to' parameter\n\nUsage: plan_phase(action: "transition", to: "understand")`
            }

            // Validate transition
            if (!VALID_TRANSITIONS[state.phase]?.includes(args.to)) {
              const valid = VALID_TRANSITIONS[state.phase]?.map((p) => p.toUpperCase()).join(", ") || "none"
              return (
                `❌ Invalid transition: ${state.phase.toUpperCase()} → ${args.to.toUpperCase()}\n\n` +
                `Valid transitions from ${state.phase.toUpperCase()}: ${valid}`
              )
            }

            // Check phase completion requirements
            const requirements: Record<Phase, () => { met: boolean; message: string }> = {
              idle: () => ({ met: true, message: "" }),
              init: () => ({
                met: !!state.projectDocs,
                message: "Call plan_discover_docs() first to load project documentation.",
              }),
              understand: () => ({
                met: !!state.userIntent,
                message: "Call plan_capture_intent() first to capture user requirements.",
              }),
              design: () => ({
                met: !!state.archCheckResult,
                message: "Complete architecture check first (dispatch explore subagent).",
              }),
              approve: () => ({
                met: state.planSaved,
                message: "Save plan to brain before completing.",
              }),
            }

            const req = requirements[state.phase]()
            if (!req.met && args.to !== "understand" && args.to !== "design") {
              // Allow going back without completing
              return (
                `❌ Cannot transition: ${state.phase.toUpperCase()} phase not complete\n\n` +
                `${req.message}\n\n` +
                `Use plan_phase(action: "skip", reason: "...") for emergency bypass.`
              )
            }

            const previousPhase = state.phase
            state.phase = args.to
            state.phaseStartedAt = Date.now()

            // Reset state if going back
            if (args.to === "understand" && previousPhase === "design") {
              state.archCheckResult = undefined
            }
            if (args.to === "design" && previousPhase === "approve") {
              state.proposedDocUpdates = undefined
            }

            logAudit({
              type: "phase_transition",
              sessionID: ctx.sessionID,
              phase: previousPhase,
              targetPhase: args.to,
              details: `Transitioned from ${previousPhase} to ${args.to}`,
            })

            const info = PHASE_INFO[args.to]
            return (
              `${info.emoji} PHASE TRANSITION: ${previousPhase.toUpperCase()} → ${args.to.toUpperCase()}\n\n` +
              `**Phase:** ${info.name}\n` +
              `**Description:** ${info.description}\n\n` +
              `**Next Step:** ${info.next}`
            )
          }

          // =================================================================
          // ACTION: status
          // =================================================================
          if (args.action === "status") {
            const info = PHASE_INFO[state.phase]
            const duration = Math.round((Date.now() - state.phaseStartedAt) / 1000 / 60)
            const totalDuration = Math.round((Date.now() - state.startedAt) / 1000 / 60)

            const lines: string[] = [
              `## ${info.emoji} Planning Status`,
              "",
              `**Phase:** ${info.name}`,
              `**Description:** ${info.description}`,
              `**Phase Duration:** ${duration} min`,
              `**Total Duration:** ${totalDuration} min`,
              `**Enforcement:** ${state.enforcementLevel}`,
              "",
            ]

            if (state.objective) {
              lines.push(`**Objective:** ${state.objective}`)
              lines.push("")
            }

            // Phase completion status
            lines.push("### Phase Completion")
            lines.push("")
            lines.push("| Phase | Status |")
            lines.push("|-------|--------|")
            lines.push(`| INIT (docs loaded) | ${state.projectDocs ? "✅" : "⚪"} |`)
            lines.push(`| UNDERSTAND (intent captured) | ${state.userIntent ? "✅" : "⚪"} |`)
            lines.push(`| DESIGN (arch check done) | ${state.archCheckResult ? "✅" : "⚪"} |`)
            lines.push(`| APPROVE (plan saved) | ${state.planSaved ? "✅" : "⚪"} |`)
            lines.push("")

            // Project docs summary
            if (state.projectDocs) {
              lines.push("### Project Documentation")
              lines.push("")
              lines.push(`- **PRD:** ${state.projectDocs.prd.found ? state.projectDocs.prd.path : "❌ Not found"}`)
              lines.push(`- **Architecture:** ${state.projectDocs.arch.found ? state.projectDocs.arch.path : "❌ Not found"}`)
              if (state.projectDocs.other.length > 0) {
                lines.push(`- **Other docs:** ${state.projectDocs.other.join(", ")}`)
              }
              lines.push("")
            }

            // User intent summary
            if (state.userIntent) {
              lines.push("### Captured Intent")
              lines.push("")
              lines.push(`- **Problem:** ${state.userIntent.problem.slice(0, 100)}...`)
              lines.push(`- **Success Criteria:** ${state.userIntent.success_criteria.length} items`)
              lines.push(`- **In Scope:** ${state.userIntent.scope.in.length} items`)
              lines.push(`- **Out of Scope:** ${state.userIntent.scope.out.length} items`)
              lines.push(`- **Constraints:** ${state.userIntent.constraints.length} items`)
              lines.push("")
            }

            // Current plan
            if (state.currentPlanId) {
              lines.push("### Active Plan")
              lines.push("")
              lines.push(`- **Title:** ${state.currentPlanTitle || "Untitled"}`)
              lines.push(`- **Brain ID:** \`${state.currentPlanId}\``)
              lines.push("")
            }

            lines.push("### Next Step")
            lines.push("")
            lines.push(info.next)

            return lines.join("\n")
          }

          // =================================================================
          // ACTION: skip
          // =================================================================
          if (args.action === "skip") {
            if (!args.reason) {
              return `❌ Missing 'reason' parameter\n\nUsage: plan_phase(action: "skip", reason: "emergency hotfix")`
            }

            state.skipActive = true

            logAudit({
              type: "skip",
              sessionID: ctx.sessionID,
              phase: state.phase,
              details: args.reason,
            })

            return (
              `⚠️ PHASE ENFORCEMENT SKIPPED\n\n` +
              `**Phase:** ${state.phase.toUpperCase()} → BYPASSED (one action)\n` +
              `**Reason:** "${args.reason}"\n` +
              `**Logged for audit:** ✓\n\n` +
              `Enforcement will resume after the next tool execution.`
            )
          }

          // =================================================================
          // ACTION: reset
          // =================================================================
          if (args.action === "reset") {
            const previousPhase = state.phase
            sessions.delete(ctx.sessionID)

            logAudit({
              type: "phase_transition",
              sessionID: ctx.sessionID,
              phase: previousPhase,
              targetPhase: "idle",
              details: "Session reset",
            })

            return (
              `## Planning Session Reset\n\n` +
              `Previous phase: ${previousPhase.toUpperCase()}\n` +
              `All state cleared.\n\n` +
              `To start a new session:\n` +
              `\`\`\`\n` +
              `plan_phase(action: "start", objective: "...")\n` +
              `\`\`\``
            )
          }

          return `❌ Unknown action: ${args.action}`
        },
      }),

      // =====================================================================
      // plan_discover_docs - Scan for project documentation AND brain plans
      // =====================================================================
      plan_discover_docs: tool({
        description: `Discover project documentation AND existing brain plans.

This tool performs interactive discovery:
1. Scans for PRD and architecture documents in common locations
2. Searches brain for existing plans (fuzzy match against objective)
3. Searches brain for related explorations
4. Returns numbered options for user to select

After discovery, use plan_confirm_docs() to confirm selections.

Arguments:
- prdPath: Custom PRD path (skips auto-discovery for PRD)
- archPath: Custom architecture doc path (skips auto-discovery for arch)
- additionalDirs: Additional directories to scan for docs
- planQuery: Search query for existing plans (uses objective if not provided)
- planId: Specific brain plan ID to load directly

Call this during INIT phase to load project context.`,

        args: {
          prdPath: tool.schema.string().optional()
            .describe("Custom PRD path (skips auto-discovery for PRD)"),
          archPath: tool.schema.string().optional()
            .describe("Custom architecture doc path (skips auto-discovery for arch)"),
          additionalDirs: tool.schema.array(tool.schema.string()).optional()
            .describe("Additional directories to scan for documentation"),
          planQuery: tool.schema.string().optional()
            .describe("Search query for existing plans in brain (uses objective if not provided)"),
          planId: tool.schema.string().optional()
            .describe("Specific brain plan ID to load directly"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          // Initialize or merge with existing discovery results
          const existingResults = state.discoveryResults
          const results: DiscoveryResults = {
            prdCandidates: existingResults?.prdCandidates || [],
            archCandidates: existingResults?.archCandidates || [],
            otherDocs: existingResults?.otherDocs || [],
            matchingPlans: existingResults?.matchingPlans || [],
            relatedExplorations: existingResults?.relatedExplorations || [],
            discoveredAt: Date.now(),
          }

          // Track seen paths to avoid duplicates when merging
          const seenPrdPaths = new Set(results.prdCandidates.map(c => c.path))
          const seenArchPaths = new Set(results.archCandidates.map(c => c.path))
          const seenOtherDocs = new Set(results.otherDocs)
          const seenPlanIds = new Set(results.matchingPlans.map(p => p.id))
          const seenExplorationIds = new Set(results.relatedExplorations.map(e => e.id))

          // ========================================
          // 1. FILE-BASED DOCUMENTATION DISCOVERY
          // ========================================

          // If custom PRD path provided, use it directly
          if (args.prdPath) {
            const fullPath = `${directory}/${args.prdPath}`
            try {
              const file = Bun.file(fullPath)
              if (await file.exists()) {
                const content = await file.text()
                const stat = await file.stat()
                if (!seenPrdPaths.has(args.prdPath)) {
                  results.prdCandidates.push({
                    index: results.prdCandidates.length + 1,
                    path: args.prdPath,
                    modified: formatRelativeTime(new Date(stat.mtime).toISOString()),
                    summary: extractDocSummary(content, "PRD"),
                    content,
                  })
                  seenPrdPaths.add(args.prdPath)
                }
              }
            } catch { /* ignore */ }
          }

          // If custom arch path provided, use it directly
          if (args.archPath) {
            const fullPath = `${directory}/${args.archPath}`
            try {
              const file = Bun.file(fullPath)
              if (await file.exists()) {
                const content = await file.text()
                const stat = await file.stat()
                if (!seenArchPaths.has(args.archPath)) {
                  results.archCandidates.push({
                    index: results.archCandidates.length + 1,
                    path: args.archPath,
                    modified: formatRelativeTime(new Date(stat.mtime).toISOString()),
                    summary: extractDocSummary(content, "Architecture"),
                    content,
                  })
                  seenArchPaths.add(args.archPath)
                }
              }
            } catch { /* ignore */ }
          }

          // Auto-discover PRD candidates (if not only using custom path)
          if (!args.prdPath || results.prdCandidates.length === 0) {
            const prdPaths = ["docs/prd.md", "docs/PRD.md", "PRD.md", "docs/requirements.md", "REQUIREMENTS.md"]
            for (const path of prdPaths) {
              if (seenPrdPaths.has(path)) continue
              try {
                const fullPath = `${directory}/${path}`
                const file = Bun.file(fullPath)
                if (await file.exists()) {
                  const content = await file.text()
                  const stat = await file.stat()
                  results.prdCandidates.push({
                    index: results.prdCandidates.length + 1,
                    path,
                    modified: formatRelativeTime(new Date(stat.mtime).toISOString()),
                    summary: extractDocSummary(content, "PRD"),
                    content,
                  })
                  seenPrdPaths.add(path)
                }
              } catch { /* continue */ }
            }
          }

          // Auto-discover architecture candidates
          if (!args.archPath || results.archCandidates.length === 0) {
            const archPaths = [
              "docs/architecture.md", "docs/ARCHITECTURE.md", "ARCHITECTURE.md",
              "docs/design.md", "docs/technical-design.md",
            ]
            for (const path of archPaths) {
              if (seenArchPaths.has(path)) continue
              try {
                const fullPath = `${directory}/${path}`
                const file = Bun.file(fullPath)
                if (await file.exists()) {
                  const content = await file.text()
                  const stat = await file.stat()
                  results.archCandidates.push({
                    index: results.archCandidates.length + 1,
                    path,
                    modified: formatRelativeTime(new Date(stat.mtime).toISOString()),
                    summary: extractDocSummary(content, "Architecture"),
                    content,
                  })
                  seenArchPaths.add(path)
                }
              } catch { /* continue */ }
            }
          }

          // Scan additional directories
          const dirsToScan = ["docs", ...(args.additionalDirs || [])]
          for (const dir of dirsToScan) {
            try {
              const docsDir = `${directory}/${dir}`
              const glob = new Bun.Glob("**/*.md")
              for await (const file of glob.scan({ cwd: docsDir, onlyFiles: true })) {
                const fullPath = `${dir}/${file}`
                if (!seenPrdPaths.has(fullPath) && !seenArchPaths.has(fullPath) && !seenOtherDocs.has(fullPath)) {
                  results.otherDocs.push(fullPath)
                  seenOtherDocs.add(fullPath)
                }
              }
            } catch { /* directory may not exist */ }
          }

          // Re-index candidates after merging
          results.prdCandidates.forEach((c, i) => c.index = i + 1)
          results.archCandidates.forEach((c, i) => c.index = i + 1)

          // ========================================
          // 2. BRAIN SEARCH FOR EXISTING PLANS
          // ========================================

          const zkAvailable = zkNotebookExists && await isZkAvailable()

          if (zkAvailable) {
            // If specific planId provided, load it directly
            if (args.planId) {
              try {
                const zkArgs = ["list", "--format", "json", "--quiet", "--match", args.planId, "--limit", "1"]
                const result = await execZk(zkArgs)
                if (result.exitCode === 0 && result.stdout.trim()) {
                  const notes = parseZkJsonOutput(result.stdout)
                  for (const note of notes) {
                    const id = extractIdFromPath(note.path)
                    if (!seenPlanIds.has(id)) {
                      results.matchingPlans.push({
                        index: results.matchingPlans.length + 1,
                        id,
                        title: note.title,
                        status: extractStatus(note),
                        age: formatRelativeTime(note.modified),
                        lead: note.lead,
                      })
                      seenPlanIds.add(id)
                    }
                  }
                }
              } catch { /* ignore */ }
            }

            // Search for plans matching objective or query
            const searchQuery = args.planQuery || state.objective
            if (searchQuery) {
              try {
                // Search for plans with matching query
                const zkArgs = ["list", "--format", "json", "--quiet", "--match", searchQuery, "--tag", "plan", "--limit", "10"]
                const result = await execZk(zkArgs)
                if (result.exitCode === 0 && result.stdout.trim()) {
                  const notes = parseZkJsonOutput(result.stdout)
                  for (const note of notes) {
                    const id = extractIdFromPath(note.path)
                    const status = extractStatus(note)
                    // Only include non-completed, non-archived plans
                    if (!seenPlanIds.has(id) && !["completed", "archived", "superseded"].includes(status)) {
                      results.matchingPlans.push({
                        index: results.matchingPlans.length + 1,
                        id,
                        title: note.title,
                        status,
                        age: formatRelativeTime(note.modified),
                        lead: note.lead,
                      })
                      seenPlanIds.add(id)
                    }
                  }
                }
              } catch { /* ignore */ }

              // Search for related explorations
              try {
                const zkArgs = ["list", "--format", "json", "--quiet", "--match", searchQuery, "--tag", "exploration", "--limit", "5"]
                const result = await execZk(zkArgs)
                if (result.exitCode === 0 && result.stdout.trim()) {
                  const notes = parseZkJsonOutput(result.stdout)
                  for (const note of notes) {
                    const id = extractIdFromPath(note.path)
                    if (!seenExplorationIds.has(id)) {
                      results.relatedExplorations.push({
                        index: results.relatedExplorations.length + 1,
                        id,
                        title: note.title,
                        age: formatRelativeTime(note.modified),
                        lead: note.lead,
                      })
                      seenExplorationIds.add(id)
                    }
                  }
                }
              } catch { /* ignore */ }
            }

            // Re-index brain entries after merging
            results.matchingPlans.forEach((p, i) => p.index = i + 1)
            results.relatedExplorations.forEach((e, i) => e.index = i + 1)
          }

          // Store results in state
          state.discoveryResults = results

          // Also update legacy projectDocs for backward compatibility
          if (results.prdCandidates.length > 0) {
            const prd = results.prdCandidates[0]
            state.projectDocs = state.projectDocs || { prd: { found: false, path: "", content: "", summary: "" }, arch: { found: false, path: "", content: "", summary: "" }, other: [], discovered_at: Date.now() }
            state.projectDocs.prd = { found: true, path: prd.path, content: prd.content, summary: prd.summary }
          }
          if (results.archCandidates.length > 0) {
            const arch = results.archCandidates[0]
            state.projectDocs = state.projectDocs || { prd: { found: false, path: "", content: "", summary: "" }, arch: { found: false, path: "", content: "", summary: "" }, other: [], discovered_at: Date.now() }
            state.projectDocs.arch = { found: true, path: arch.path, content: arch.content, summary: arch.summary }
          }

          // Set flag to require user confirmation before plan_confirm_docs
          state.awaitingUserConfirmation = true

          logAudit({
            type: "docs_discovered",
            sessionID: ctx.sessionID,
            phase: state.phase,
            details: `PRD: ${results.prdCandidates.length}, Arch: ${results.archCandidates.length}, Plans: ${results.matchingPlans.length}, Explorations: ${results.relatedExplorations.length} - AWAITING USER CONFIRMATION`,
          })

          // ========================================
          // 3. FORMAT OUTPUT
          // ========================================

          const lines: string[] = ["## 📂 Discovery Results", ""]

          // Brain plans section
          if (zkAvailable) {
            lines.push("### 🧠 Existing Brain Plans")
            lines.push("")
            
            if (results.matchingPlans.length > 0) {
              const query = args.planQuery || state.objective || "(no query)"
              lines.push(`**Matching Plans** (search: "${query.slice(0, 50)}${query.length > 50 ? '...' : ''}")`)
              lines.push("")
              lines.push("| # | Title | Status | Age | ID |")
              lines.push("|---|-------|--------|-----|-----|")
              for (const plan of results.matchingPlans) {
                lines.push(`| ${plan.index} | ${plan.title.slice(0, 40)}${plan.title.length > 40 ? '...' : ''} | ${plan.status} | ${plan.age} | \`${plan.id}\` |`)
              }
              lines.push("")
            } else {
              lines.push("No matching plans found in brain.")
              lines.push("")
            }

            if (results.relatedExplorations.length > 0) {
              lines.push("**Related Explorations:**")
              lines.push("")
              lines.push("| # | Title | Age | ID |")
              lines.push("|---|-------|-----|-----|")
              for (const exp of results.relatedExplorations) {
                lines.push(`| ${exp.index} | ${exp.title.slice(0, 50)}${exp.title.length > 50 ? '...' : ''} | ${exp.age} | \`${exp.id}\` |`)
              }
              lines.push("")
            }

            lines.push("> Select a plan number to update it, or choose `0` for a new plan.")
            lines.push("")
            lines.push("---")
            lines.push("")
          }

          // File-based docs section
          lines.push("### 📄 File-Based Documentation")
          lines.push("")

          // PRD candidates
          lines.push("**PRD Candidates:**")
          lines.push("")
          if (results.prdCandidates.length > 0) {
            lines.push("| # | Path | Modified | Summary |")
            lines.push("|---|------|----------|---------|")
            for (const prd of results.prdCandidates) {
              lines.push(`| ${prd.index} | \`${prd.path}\` | ${prd.modified} | ${prd.summary.slice(0, 50)}${prd.summary.length > 50 ? '...' : ''} |`)
            }
            lines.push("")
          } else {
            lines.push("No PRD documents found.")
            lines.push("")
          }
          lines.push("> Select a number as primary PRD, or `0` to create new during APPROVE phase.")
          lines.push("")

          // Architecture candidates
          lines.push("**Architecture Candidates:**")
          lines.push("")
          if (results.archCandidates.length > 0) {
            lines.push("| # | Path | Modified | Summary |")
            lines.push("|---|------|----------|---------|")
            for (const arch of results.archCandidates) {
              lines.push(`| ${arch.index} | \`${arch.path}\` | ${arch.modified} | ${arch.summary.slice(0, 50)}${arch.summary.length > 50 ? '...' : ''} |`)
            }
            lines.push("")
          } else {
            lines.push("No architecture documents found.")
            lines.push("")
          }
          lines.push("> Select a number as primary architecture doc, or `0` to create new during APPROVE phase.")
          lines.push("")

          // Other docs
          if (results.otherDocs.length > 0) {
            lines.push("**Other Documentation Found:**")
            lines.push("")
            for (const doc of results.otherDocs.slice(0, 10)) {
              lines.push(`- \`${doc}\``)
            }
            if (results.otherDocs.length > 10) {
              lines.push(`- ... and ${results.otherDocs.length - 10} more`)
            }
            lines.push("")
          }

          // Next step - HARD STOP instruction
          lines.push("---")
          lines.push("")
          lines.push("### ⛔ HARD STOP - WAIT FOR USER RESPONSE")
          lines.push("")
          lines.push("**DO NOT call plan_confirm_docs yet!**")
          lines.push("")
          lines.push("You MUST:")
          lines.push("1. Present these discovery results to the user")
          lines.push("2. Ask them to confirm their selections")
          lines.push("3. **WAIT for their response**")
          lines.push("4. THEN call plan_confirm_docs with their choices")
          lines.push("")
          lines.push("Example questions to ask:")
          lines.push("- \"Which PRD should I use? (number or 0 for new)\"")
          lines.push("- \"Which architecture doc? (number or 0 for new)\"")
          lines.push("- \"Should I update an existing plan or create new?\"")
          lines.push("")
          lines.push("After user responds, call:")
          lines.push("```")
          lines.push("plan_confirm_docs({")
          lines.push(`  prdSelection: <user's choice>,`)
          lines.push(`  archSelection: <user's choice>,`)
          lines.push(`  existingPlan: <user's choice>,`)
          lines.push("})")
          lines.push("```")

          return lines.join("\n")
        },
      }),

      // =====================================================================
      // plan_confirm_docs - Confirm documentation selections
      // =====================================================================
      plan_confirm_docs: tool({
        description: `Confirm documentation selections from plan_discover_docs.

Use this after plan_discover_docs to lock in user selections:
- PRD document (number from list, 0 for create new, or custom path)
- Architecture document (number from list, 0 for create new, or custom path)
- Existing plan to update (number from list, 0 for new plan, or brain ID)
- Explorations to include as context

This stores the confirmed selections and marks INIT phase as ready for transition.`,

        args: {
          prdSelection: tool.schema.union([
            tool.schema.number(),
            tool.schema.string()
          ]).describe("PRD selection: number from discovery list (0=create new), or custom path string"),
          archSelection: tool.schema.union([
            tool.schema.number(),
            tool.schema.string()
          ]).describe("Architecture doc selection: number from list (0=create new), or custom path string"),
          existingPlan: tool.schema.union([
            tool.schema.number(),
            tool.schema.string()
          ]).default(0).describe("Existing plan: number from list (0=new plan), or brain ID string"),
          includeExplorations: tool.schema.array(tool.schema.string()).optional()
            .describe("Brain IDs of explorations to include as context"),
          includeOtherDocs: tool.schema.array(tool.schema.string()).optional()
            .describe("Paths of other docs to include as context"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)
          const discovery = state.discoveryResults

          if (!discovery) {
            return `❌ No discovery results found.\n\nCall \`plan_discover_docs()\` first to discover available documentation.`
          }

          const confirmed: ConfirmedDocs = {
            prdPath: null,
            archPath: null,
            existingPlanId: null,
            existingPlanTitle: null,
            isUpdatingExistingPlan: false,
            includedExplorationIds: [],
            includedOtherDocs: [],
            confirmedAt: Date.now(),
          }

          // ========================================
          // Resolve PRD selection
          // ========================================
          if (typeof args.prdSelection === "number") {
            if (args.prdSelection === 0) {
              // Create new during APPROVE
              confirmed.prdPath = null
            } else {
              // Lookup from discovery
              const prd = discovery.prdCandidates.find(c => c.index === args.prdSelection)
              if (prd) {
                confirmed.prdPath = prd.path
                confirmed.prdContent = prd.content
              } else {
                return `❌ Invalid PRD selection: ${args.prdSelection}\n\nAvailable: ${discovery.prdCandidates.map(c => c.index).join(", ") || "none (use 0)"}`
              }
            }
          } else if (typeof args.prdSelection === "string") {
            // Custom path
            const fullPath = `${directory}/${args.prdSelection}`
            try {
              const file = Bun.file(fullPath)
              if (await file.exists()) {
                confirmed.prdPath = args.prdSelection
                confirmed.prdContent = await file.text()
              } else {
                return `❌ Custom PRD path not found: ${args.prdSelection}`
              }
            } catch (e) {
              return `❌ Error reading custom PRD: ${e instanceof Error ? e.message : String(e)}`
            }
          }

          // ========================================
          // Resolve Architecture selection
          // ========================================
          if (typeof args.archSelection === "number") {
            if (args.archSelection === 0) {
              // Create new during APPROVE
              confirmed.archPath = null
            } else {
              // Lookup from discovery
              const arch = discovery.archCandidates.find(c => c.index === args.archSelection)
              if (arch) {
                confirmed.archPath = arch.path
                confirmed.archContent = arch.content
              } else {
                return `❌ Invalid architecture selection: ${args.archSelection}\n\nAvailable: ${discovery.archCandidates.map(c => c.index).join(", ") || "none (use 0)"}`
              }
            }
          } else if (typeof args.archSelection === "string") {
            // Custom path
            const fullPath = `${directory}/${args.archSelection}`
            try {
              const file = Bun.file(fullPath)
              if (await file.exists()) {
                confirmed.archPath = args.archSelection
                confirmed.archContent = await file.text()
              } else {
                return `❌ Custom architecture path not found: ${args.archSelection}`
              }
            } catch (e) {
              return `❌ Error reading custom architecture doc: ${e instanceof Error ? e.message : String(e)}`
            }
          }

          // ========================================
          // Resolve existing plan selection
          // ========================================
          if (typeof args.existingPlan === "number") {
            if (args.existingPlan === 0) {
              // New plan
              confirmed.isUpdatingExistingPlan = false
            } else {
              // Lookup from discovery
              const plan = discovery.matchingPlans.find(p => p.index === args.existingPlan)
              if (plan) {
                confirmed.existingPlanId = plan.id
                confirmed.existingPlanTitle = plan.title
                confirmed.isUpdatingExistingPlan = true
              } else {
                return `❌ Invalid plan selection: ${args.existingPlan}\n\nAvailable: ${discovery.matchingPlans.map(p => p.index).join(", ") || "none (use 0 for new)"}`
              }
            }
          } else if (typeof args.existingPlan === "string") {
            // Direct brain ID
            confirmed.existingPlanId = args.existingPlan
            confirmed.isUpdatingExistingPlan = true
            // Try to find title from discovery
            const plan = discovery.matchingPlans.find(p => p.id === args.existingPlan)
            confirmed.existingPlanTitle = plan?.title || "Unknown Plan"
          }

          // ========================================
          // Include explorations
          // ========================================
          if (args.includeExplorations && args.includeExplorations.length > 0) {
            // Validate all IDs exist in discovery
            for (const id of args.includeExplorations) {
              const exp = discovery.relatedExplorations.find(e => e.id === id)
              if (exp) {
                confirmed.includedExplorationIds.push(id)
              }
              // Silently skip invalid IDs (might be manually specified)
            }
            // Also accept manually specified IDs not in discovery
            confirmed.includedExplorationIds = args.includeExplorations
          }

          // ========================================
          // Include other docs
          // ========================================
          if (args.includeOtherDocs && args.includeOtherDocs.length > 0) {
            confirmed.includedOtherDocs = args.includeOtherDocs
          }

          // Store confirmed selections
          state.confirmedDocs = confirmed
          
          // Clear the awaiting flag since confirmation is complete
          state.awaitingUserConfirmation = false

          // Also update legacy state for backward compatibility
          if (confirmed.existingPlanId) {
            state.currentPlanId = confirmed.existingPlanId
            state.currentPlanTitle = confirmed.existingPlanTitle || undefined
          }

          logAudit({
            type: "docs_discovered",
            sessionID: ctx.sessionID,
            phase: state.phase,
            details: `Confirmed: PRD=${confirmed.prdPath || "create"}, Arch=${confirmed.archPath || "create"}, Plan=${confirmed.isUpdatingExistingPlan ? confirmed.existingPlanId : "new"}`,
          })

          // ========================================
          // Format confirmation output
          // ========================================
          const lines: string[] = ["## ✅ Documentation Confirmed", ""]

          lines.push("**Source of Truth:**")
          if (confirmed.prdPath) {
            lines.push(`- PRD: \`${confirmed.prdPath}\``)
          } else {
            lines.push(`- PRD: Will create \`docs/prd.md\` during APPROVE phase`)
          }
          if (confirmed.archPath) {
            lines.push(`- Architecture: \`${confirmed.archPath}\``)
          } else {
            lines.push(`- Architecture: Will create \`docs/architecture.md\` during APPROVE phase`)
          }
          lines.push("")

          lines.push("**Working Mode:**")
          if (confirmed.isUpdatingExistingPlan) {
            lines.push(`- Updating existing plan: "${confirmed.existingPlanTitle}" (\`${confirmed.existingPlanId}\`)`)
          } else {
            lines.push(`- Creating new plan`)
          }
          lines.push("")

          if (confirmed.includedExplorationIds.length > 0) {
            lines.push("**Additional Context:**")
            for (const id of confirmed.includedExplorationIds) {
              const exp = discovery.relatedExplorations.find(e => e.id === id)
              lines.push(`- Exploration: "${exp?.title || id}" (\`${id}\`)`)
            }
            lines.push("")
          }

          if (confirmed.includedOtherDocs.length > 0) {
            lines.push("**Other Docs:**")
            for (const doc of confirmed.includedOtherDocs) {
              lines.push(`- \`${doc}\``)
            }
            lines.push("")
          }

          lines.push("---")
          lines.push("")
          lines.push("Ready to proceed. Transition to UNDERSTAND phase:")
          lines.push("```")
          lines.push('plan_phase(action: "transition", to: "understand")')
          lines.push("```")

          return lines.join("\n")
        },
      }),

      // =====================================================================
      // plan_capture_intent - Capture user intent
      // =====================================================================
      plan_capture_intent: tool({
        description: `Capture the user's intent after clarifying questions.

Store the understood requirements for later validation:
- Problem statement
- Success criteria
- Scope (in/out)
- Constraints

Call this during UNDERSTAND phase after asking clarifying questions.
This enables checking the final plan against original intent.`,

        args: {
          problem: tool.schema.string().describe("Problem statement - what the user wants to solve"),
          success_criteria: tool.schema.array(tool.schema.string()).describe("List of success criteria"),
          scope_in: tool.schema.array(tool.schema.string()).describe("What's in scope"),
          scope_out: tool.schema.array(tool.schema.string()).describe("What's explicitly out of scope"),
          constraints: tool.schema.array(tool.schema.string()).describe("Technical or business constraints"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          state.userIntent = {
            problem: args.problem,
            success_criteria: args.success_criteria,
            scope: {
              in: args.scope_in,
              out: args.scope_out,
            },
            constraints: args.constraints,
            captured_at: Date.now(),
          }

          logAudit({
            type: "intent_captured",
            sessionID: ctx.sessionID,
            phase: state.phase,
            details: `Problem: ${args.problem.slice(0, 50)}..., Criteria: ${args.success_criteria.length}`,
          })

          const lines: string[] = [
            "## 🎯 User Intent Captured",
            "",
            "### Problem Statement",
            args.problem,
            "",
            "### Success Criteria",
            ...args.success_criteria.map((c, i) => `${i + 1}. ${c}`),
            "",
            "### Scope",
            "",
            "**In Scope:**",
            ...args.scope_in.map((s) => `- ${s}`),
            "",
            "**Out of Scope:**",
            ...args.scope_out.map((s) => `- ${s}`),
            "",
            "### Constraints",
            ...args.constraints.map((c) => `- ${c}`),
            "",
            "---",
            "",
            "### Next Step",
            "",
            "Intent captured. Transition to DESIGN phase:",
            "```",
            'plan_phase(action: "transition", to: "design")',
            "```",
            "",
            "Then draft your implementation approach and dispatch an explore subagent",
            "to check it against the architecture.",
          ]

          return lines.join("\n")
        },
      }),

      // =====================================================================
      // plan_record_arch_check - Record architecture check result
      // =====================================================================
      plan_record_arch_check: tool({
        description: `Record the result of an architecture check.

Call this after dispatching an explore subagent to analyze the design
against existing PRD and architecture. Store the analysis result.

The agent should dispatch the subagent and then call this tool with the result.`,

        args: {
          aligned: tool.schema.array(tool.schema.string()).describe("What aligns with existing patterns"),
          conflicts: tool.schema.array(tool.schema.string()).describe("Potential conflicts or anti-patterns"),
          gaps: tool.schema.array(tool.schema.string()).describe("New decisions or requirements needed"),
          recommendations: tool.schema.array(tool.schema.string()).describe("Recommendations to resolve issues"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          const result = [
            "## Aligned With:",
            ...args.aligned.map((a) => `- ✅ ${a}`),
            "",
            "## Potential Issues:",
            ...args.conflicts.map((c) => `- ⚠️ ${c}`),
            "",
            "## Gaps (New Decisions Needed):",
            ...args.gaps.map((g) => `- 📝 ${g}`),
            "",
            "## Recommendations:",
            ...args.recommendations.map((r) => `- 💡 ${r}`),
          ].join("\n")

          state.archCheckResult = result

          logAudit({
            type: "arch_check",
            sessionID: ctx.sessionID,
            phase: state.phase,
            details: `Aligned: ${args.aligned.length}, Conflicts: ${args.conflicts.length}, Gaps: ${args.gaps.length}`,
          })

          const lines: string[] = [
            "## ✏️ Architecture Check Recorded",
            "",
            result,
            "",
            "---",
            "",
            "### Next Step",
            "",
            "Present this impact analysis to the user.",
            "If they approve, transition to APPROVE phase:",
            "```",
            'plan_phase(action: "transition", to: "approve")',
            "```",
          ]

          return lines.join("\n")
        },
      }),

      // =====================================================================
      // plan_propose_doc_updates - Format doc update proposal
      // =====================================================================
      plan_propose_doc_updates: tool({
        description: `Format proposed documentation updates for user approval.

Creates a diff-style proposal showing:
- New PRD requirements to add
- New architecture decisions to add

Call this during APPROVE phase to present changes before writing.`,

        args: {
          prd_section_title: tool.schema.string().describe("Title for new PRD section (e.g., 'User Profile Management')"),
          prd_requirements: tool.schema.array(tool.schema.string()).describe("New requirements to add"),
          prd_rationale: tool.schema.string().describe("Why these requirements are needed"),
          arch_decision_title: tool.schema.string().describe("Title for new architecture decision"),
          arch_context: tool.schema.string().describe("Context for the decision"),
          arch_decision: tool.schema.string().describe("The decision made"),
          arch_consequences: tool.schema.string().describe("Consequences/tradeoffs"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          // Generate requirement ID
          const reqId = `PRD-REQ-${String(Date.now()).slice(-4)}`
          const decId = `ARCH-DEC-${String(Date.now()).slice(-4)}`

          const prdDiff = [
            "```diff",
            "## Requirements",
            "",
            `+ ### ${reqId}: ${args.prd_section_title}`,
            `+ **Added:** ${new Date().toISOString().split("T")[0]}`,
            `+ **Rationale:** ${args.prd_rationale}`,
            "+",
            ...args.prd_requirements.map((r) => `+ - ${r}`),
            "```",
          ].join("\n")

          const archDiff = [
            "```diff",
            "## Decisions Log",
            "",
            `+ ### ${decId}: ${args.arch_decision_title}`,
            `+ **Date:** ${new Date().toISOString().split("T")[0]}`,
            "+ **Status:** Accepted",
            `+ **Context:** ${args.arch_context}`,
            `+ **Decision:** ${args.arch_decision}`,
            `+ **Consequences:** ${args.arch_consequences}`,
            "```",
          ].join("\n")

          state.proposedDocUpdates = {
            prd: prdDiff,
            arch: archDiff,
            approved: false,
          }
          state.newRequirementIds = [reqId, decId]

          logAudit({
            type: "doc_update_proposed",
            sessionID: ctx.sessionID,
            phase: state.phase,
            details: `PRD: ${reqId}, Arch: ${decId}`,
          })

          const lines: string[] = [
            "## 📝 Proposed Documentation Updates",
            "",
            "### docs/prd.md",
            "",
            prdDiff,
            "",
            "### docs/architecture.md",
            "",
            archDiff,
            "",
            "---",
            "",
            "### New Requirement IDs",
            "",
            `- **PRD:** ${reqId}`,
            `- **Architecture:** ${decId}`,
            "",
            "These IDs will be referenced in the implementation plan.",
            "",
            "---",
            "",
            "**Approve these documentation updates?** [Y/n/edit]",
            "",
            "If approved, dispatch a subagent to write the updates:",
            "```",
            'Task(subagent_type: "general", prompt: "Update docs/prd.md with: ...")',
            "```",
          ]

          return lines.join("\n")
        },
      }),

      // =====================================================================
      // plan_gate - Legacy compatibility (kept for backward compat)
      // =====================================================================
      plan_gate: tool({
        description: `[LEGACY] Control brain planning enforcement. Use plan_phase instead for new workflow.

ACTIONS:
- status: Check current planning session state
- set_enforcement: Set enforcement level ("advisory" or "strict")
- set_objective: Set the current planning objective
- reset: Clear session state`,

        args: {
          action: tool.schema.enum(["status", "set_enforcement", "set_objective", "reset"]).describe("Action to perform"),
          level: tool.schema.enum(["advisory", "strict"]).optional().describe("Enforcement level"),
          objective: tool.schema.string().optional().describe("Planning objective"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)

          if (args.action === "status") {
            const duration = Math.round((Date.now() - state.startedAt) / 1000 / 60)
            const agent = sessionAgents.get(ctx.sessionID) || "unknown"
            const info = PHASE_INFO[state.phase]

            return (
              `## 🧠 Brain Planning Status\n\n` +
              `**Agent:** ${agent}\n` +
              `**Phase:** ${info.emoji} ${info.name}\n` +
              `**Duration:** ${duration} min\n` +
              `**Enforcement:** ${state.enforcementLevel}\n\n` +
              `| Check | Status |\n` +
              `|-------|--------|\n` +
              `| Brain checked | ${state.brainChecked ? "✅" : "❌"} |\n` +
              `| Plan saved | ${state.planSaved ? "✅" : "⚪"} |\n\n` +
              `**Next:** ${info.next}`
            )
          }

          if (args.action === "set_enforcement") {
            if (!args.level) {
              return `❌ Missing 'level' parameter`
            }
            state.enforcementLevel = args.level
            return `Enforcement set to: ${args.level}`
          }

          if (args.action === "set_objective") {
            if (!args.objective) {
              return `❌ Missing 'objective' parameter`
            }
            state.objective = args.objective
            return `Objective set to: ${args.objective}`
          }

          if (args.action === "reset") {
            sessions.delete(ctx.sessionID)
            return `Session reset.`
          }

          return `❌ Unknown action: ${args.action}`
        },
      }),

      // =====================================================================
      // plan_start - Legacy compatibility (redirects to plan_phase)
      // =====================================================================
      plan_start: tool({
        description: `[LEGACY] Start a planning session. Use plan_phase(action: "start") instead.`,

        args: {
          objective: tool.schema.string().describe("The planning objective"),
          enforcement: tool.schema.enum(["advisory", "strict"]).optional().describe("Enforcement level"),
        },

        async execute(args, ctx): Promise<string> {
          const state = getOrCreateState(ctx.sessionID)
          state.phase = "init"
          state.phaseStartedAt = Date.now()
          state.objective = args.objective
          state.enforcementLevel = args.enforcement || "strict"

          logAudit({
            type: "phase_transition",
            sessionID: ctx.sessionID,
            phase: "idle",
            targetPhase: "init",
            details: `Legacy plan_start: "${args.objective}"`,
          })

          return (
            `## 🧠 Planning Session Started\n\n` +
            `**Objective:** ${args.objective}\n` +
            `**Phase:** INIT\n` +
            `**Enforcement:** ${state.enforcementLevel}\n\n` +
            `**Next:** Call plan_discover_docs() to load project documentation.`
          )
        },
      }),

      // =====================================================================
      // plan_compliance_report - Generate compliance report
      // =====================================================================
      plan_compliance_report: tool({
        description: `Generate a compliance report for planning sessions.

Returns audit events showing:
- Phase transitions
- Brain checks performed
- Plans saved
- Enforcement blocks
- Skips used`,

        args: {
          sessionID: tool.schema.string().optional().describe("Filter by session ID"),
          format: tool.schema.enum(["summary", "detailed", "json"]).optional().describe("Output format"),
        },

        async execute(args, ctx): Promise<string> {
          const targetSession = args.sessionID || ctx.sessionID
          const format = args.format || "summary"

          const events = auditLog.filter((e) => e.sessionID === targetSession || !args.sessionID)

          if (events.length === 0) {
            return `## 📊 Planning Compliance: No events logged`
          }

          const stats = {
            total: events.length,
            phaseTransitions: events.filter((e) => e.type === "phase_transition").length,
            brainChecks: events.filter((e) => e.type === "brain_check").length,
            planSaves: events.filter((e) => e.type === "plan_save").length,
            blocks: events.filter((e) => e.type === "block").length,
            skips: events.filter((e) => e.type === "skip").length,
          }

          if (format === "json") {
            return JSON.stringify({ stats, events }, null, 2)
          }

          const lines: string[] = [
            "## 📊 Planning Compliance Report",
            "",
            `**Events:** ${stats.total}`,
            `**Phase Transitions:** ${stats.phaseTransitions}`,
            `**Brain Checks:** ${stats.brainChecks}`,
            `**Plans Saved:** ${stats.planSaves}`,
            `**Blocks:** ${stats.blocks}`,
            `**Skips:** ${stats.skips}`,
          ]

          if (format === "detailed") {
            lines.push("")
            lines.push("### All Events")
            lines.push("")
            events.forEach((e) => {
              const time = new Date(e.timestamp).toLocaleTimeString()
              lines.push(`- [${time}] **${e.type}** ${e.phase ? `(${e.phase})` : ""}: ${e.details || ""}`)
            })
          }

          return lines.join("\n")
        },
      }),
    },
  }
}

export default BrainPlanningPlugin
