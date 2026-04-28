/**
 * TDD Enforcement Extension for Pi
 *
 * Enforces the RED-GREEN-REFACTOR-VERIFY cycle by intercepting tool calls.
 * Blocks file edits that violate the current TDD phase.
 */

// TDD Phase state machine
type Phase = "idle" | "red" | "green" | "refactor" | "verify";

interface TDDState {
  phase: Phase;
  testFile: string | null;
  implFile: string | null;
  evidence: string[];
  skipUsed: boolean;
  auditLog: AuditEntry[];
}

interface AuditEntry {
  timestamp: string;
  event: string;
  phase: Phase;
  detail: string;
}

const state: TDDState = {
  phase: "idle",
  testFile: null,
  implFile: null,
  evidence: [],
  skipUsed: false,
  auditLog: [],
};

function log(event: string, detail: string) {
  state.auditLog.push({
    timestamp: new Date().toISOString(),
    event,
    phase: state.phase,
    detail,
  });
}

function isTestFile(path: string): boolean {
  return (
    path.includes("_test.") ||
    path.includes(".test.") ||
    path.includes(".spec.") ||
    path.includes("__tests__/")
  );
}

function isImplFile(path: string): boolean {
  const ext = path.split(".").pop() || "";
  const codeExts = ["ts", "js", "go", "py", "rs", "java", "rb", "cpp", "c", "cs"];
  return codeExts.includes(ext) && !isTestFile(path);
}

// Register TDD gate tool
pi.registerTool("tdd_gate", {
  description: "Control TDD enforcement phases: start, transition, status, skip, exit",
  parameters: {
    type: "object",
    properties: {
      action: {
        type: "string",
        enum: ["start", "transition", "status", "skip", "exit"],
        description: "The TDD action to perform",
      },
      to: {
        type: "string",
        enum: ["red", "green", "refactor", "verify"],
        description: "Target phase for transition",
      },
      evidence: {
        type: "string",
        description: "Test output evidence for phase transition",
      },
      testFile: {
        type: "string",
        description: "Test file being worked on",
      },
      implFile: {
        type: "string",
        description: "Implementation file being worked on",
      },
      reason: {
        type: "string",
        description: "Reason for skip action",
      },
    },
    required: ["action"],
  },
  handler: async (params: Record<string, unknown>) => {
    const action = params.action as string;

    switch (action) {
      case "start":
        state.phase = "red";
        state.testFile = (params.testFile as string) || null;
        state.implFile = (params.implFile as string) || null;
        state.evidence = [];
        state.skipUsed = false;
        log("start", "TDD mode activated");
        return { phase: "red", message: "TDD mode started. Write a failing test." };

      case "transition": {
        const to = params.to as Phase;
        const evidence = params.evidence as string;

        if (!to) return { error: "Missing 'to' phase" };

        // Validate transitions
        const validTransitions: Record<Phase, Phase[]> = {
          idle: ["red"],
          red: ["green"],
          green: ["refactor"],
          refactor: ["verify"],
          verify: ["red", "idle"],
        };

        if (!validTransitions[state.phase]?.includes(to)) {
          return {
            error: `Cannot transition from ${state.phase} to ${to}`,
            validTargets: validTransitions[state.phase],
          };
        }

        // Require evidence for key transitions
        if (
          (state.phase === "red" && to === "green") ||
          (state.phase === "green" && to === "refactor") ||
          (state.phase === "refactor" && to === "verify")
        ) {
          if (!evidence || evidence.length < 20) {
            return {
              error: `Evidence required for ${state.phase} -> ${to} transition (min 20 chars)`,
            };
          }
          state.evidence.push(evidence);
        }

        const oldPhase = state.phase;
        state.phase = to;
        log("transition", `${oldPhase} -> ${to}`);
        return { phase: to, message: `Transitioned to ${to}` };
      }

      case "status":
        return {
          phase: state.phase,
          testFile: state.testFile,
          implFile: state.implFile,
          evidenceCount: state.evidence.length,
          skipUsed: state.skipUsed,
        };

      case "skip":
        if (state.skipUsed) {
          return { error: "Skip already used in this session" };
        }
        state.skipUsed = true;
        const reason = (params.reason as string) || "no reason given";
        log("skip", reason);
        return { message: "One-time skip granted", reason };

      case "exit":
        const summary = {
          phase: state.phase,
          evidenceCount: state.evidence.length,
          auditEntries: state.auditLog.length,
        };
        state.phase = "idle";
        log("exit", "TDD mode deactivated");
        return { message: "TDD mode exited", summary };

      default:
        return { error: `Unknown action: ${action}` };
    }
  },
});

// Register audit tool
pi.registerTool("tdd_audit", {
  description: "View TDD enforcement audit log",
  parameters: {
    type: "object",
    properties: {
      format: {
        type: "string",
        enum: ["summary", "detailed", "json"],
        description: "Output format",
      },
    },
  },
  handler: async (params: Record<string, unknown>) => {
    const format = (params.format as string) || "summary";

    if (format === "json") {
      return { auditLog: state.auditLog };
    }

    if (format === "detailed") {
      return {
        entries: state.auditLog.map(
          (e) => `[${e.timestamp}] ${e.event} (${e.phase}): ${e.detail}`
        ),
      };
    }

    // Summary
    const counts: Record<string, number> = {};
    for (const entry of state.auditLog) {
      counts[entry.event] = (counts[entry.event] || 0) + 1;
    }
    return { totalEvents: state.auditLog.length, eventCounts: counts };
  },
});

// Intercept tool calls to enforce TDD phases
pi.on("tool_call", (event: { tool: string; args: Record<string, unknown> }) => {
  if (state.phase === "idle") return; // No enforcement when idle

  const tool = event.tool;
  const filePath = (event.args.path || event.args.file || "") as string;

  // Allow TDD control tools always
  if (tool === "tdd_gate" || tool === "tdd_audit") return;

  // Allow read tools always
  if (["read", "grep", "glob", "find", "ls"].includes(tool)) return;

  // Allow bash always (needed for running tests)
  if (tool === "bash") return;

  // Phase-specific enforcement for write operations
  if (["edit", "write"].includes(tool) && filePath) {
    switch (state.phase) {
      case "red":
        // Only test files allowed in RED
        if (!isTestFile(filePath)) {
          // Allow stub files (minimal type definitions)
          if (state.skipUsed) return;
          return {
            block: true,
            reason: `RED phase: only test files can be modified. Use tdd_gate to transition to GREEN first. File: ${filePath}`,
          };
        }
        break;

      case "green":
        // Only implementation files allowed in GREEN
        if (isTestFile(filePath)) {
          return {
            block: true,
            reason: `GREEN phase: only implementation files can be modified. File: ${filePath}`,
          };
        }
        break;

      case "refactor":
        // All code files allowed in REFACTOR
        break;

      case "verify":
        // All files allowed in VERIFY
        break;
    }
  }
});
