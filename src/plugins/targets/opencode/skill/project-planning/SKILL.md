---
name: project-planning
description: "Use when creating implementation plans for features - discovers project docs (PRD/architecture), captures user intent, checks design against architecture, proposes doc updates, and saves plan to brain"
---

# Project-Aware Planning

Create implementation plans that are grounded in existing project documentation (PRD, architecture) and maintain documentation as the source of truth.

**Announce at start:** "I'm using the project-planning skill to create a plan grounded in your project's PRD and architecture."

## Phase Overview

```
IDLE → INIT → UNDERSTAND → DESIGN → APPROVE → (complete)
       │         │            │         │
       ▼         ▼            ▼         ▼
    Load docs  Capture     Arch check  Doc updates
    PRD/arch   user intent (subagent)  + plan save
```

## Checklist

### Phase 1: INIT - Load Project Documentation

- [ ] Step 1: Start planning session
  ```
  plan_phase(action: "start", objective: "{user's goal}")
  ```

- [ ] Step 2: Discover project documentation
  ```
  plan_discover_docs()
  ```

- [ ] Step 3: Handle discovery results:
  - **If docs found:** Summarize key points to user, note existing requirements/decisions
  - **If NO docs found:** Propose creating `docs/prd.md` and `docs/architecture.md`

- [ ] Step 4: Transition to UNDERSTAND
  ```
  plan_phase(action: "transition", to: "understand")
  ```

### Phase 2: UNDERSTAND - Capture User Intent

- [ ] Step 1: Ask clarifying questions in these categories:

  **Goal Clarity:**
  - "What problem are you trying to solve?"
  - "What does success look like?"

  **Scope Definition:**
  - "What's the minimum viable version?"
  - "What's explicitly OUT of scope?"

  **Constraints:**
  - "Are there existing systems to integrate with?"
  - "What's the timeline pressure?"

  **Risk Probing:**
  - "What's the riskiest assumption?"
  - "What could cause this to fail?"

- [ ] Step 2: Push back if needed:
  - Scope too large → "Can we identify the core piece first?"
  - Requirements vague → "When you say X, do you mean A or B?"
  - Approach risky → "I see a potential issue: [issue]. Have you considered [alternative]?"

- [ ] Step 3: Capture intent after clarification
  ```
  plan_capture_intent(
    problem: "User wants to...",
    success_criteria: ["criterion 1", "criterion 2"],
    scope_in: ["feature A", "feature B"],
    scope_out: ["feature C - deferred"],
    constraints: ["must integrate with X", "timeline: 2 weeks"]
  )
  ```

- [ ] Step 4: Transition to DESIGN
  ```
  plan_phase(action: "transition", to: "design")
  ```

### Phase 3: DESIGN - Draft Approach + Architecture Check

- [ ] Step 1: Draft implementation approach based on:
  - User's stated goal (from captured intent)
  - Existing PRD requirements
  - Existing architecture patterns

- [ ] Step 2: Dispatch explore subagent for architecture check
  ```
  Task(
    subagent_type: "explore",
    prompt: """
    Analyze this proposed design against project architecture:

    PROPOSED DESIGN:
    {your_draft_design}

    Check against:
    1. docs/architecture.md - established patterns & decisions
    2. docs/prd.md - existing requirements & scope
    3. Codebase patterns - how similar features are implemented

    Return:
    - ALIGNED: What follows existing patterns
    - CONFLICTS: Anti-patterns or violations detected
    - GAPS: New decisions/requirements needed
    - RECOMMENDATIONS: How to resolve conflicts
    """
  )
  ```

- [ ] Step 3: Record architecture check result
  ```
  plan_record_arch_check(
    aligned: ["Using existing service layer pattern", "Follows auth flow"],
    conflicts: ["Bypassing cache layer - violates ARCH-DEC-003"],
    gaps: ["Need decision on new data model"],
    recommendations: ["Add caching strategy", "Create new service"]
  )
  ```

- [ ] Step 4: Present IMPACT ANALYSIS to user
  ```
  ## Architecture & PRD Impact Analysis

  **Aligned With:**
  - [PRD-REQ-003] User authentication - reusing existing flow
  - [ARCH-DEC-002] Service layer pattern - following convention

  **Potential Issues Detected:**
  - Bypassing service layer for direct DB access
    → Violates [ARCH-DEC-001]: All data access through services
    → Recommendation: Create UserService.getProfile() method

  **Documentation Updates Required:**

  PRD (docs/prd.md):
  - NEW: Add requirement for user profile feature

  Architecture (docs/architecture.md):
  - NEW: Decision on profile data caching strategy

  Proceed with these changes? [Y/n/revise]
  ```

- [ ] Step 5: Get user approval, then transition
  ```
  plan_phase(action: "transition", to: "approve")
  ```

### Phase 4: APPROVE - Doc Updates + Plan Save

- [ ] Step 1: Propose documentation updates
  ```
  plan_propose_doc_updates(
    prd_section_title: "User Profile Management",
    prd_requirements: [
      "Users can view their profile",
      "Users can edit display name and bio",
      "Changes reflect immediately"
    ],
    prd_rationale: "Users need to manage their profile information",
    arch_decision_title: "Profile Service Architecture",
    arch_context: "Need to add profile management capability",
    arch_decision: "Create ProfileService following existing service layer pattern",
    arch_consequences: "Consistent with existing architecture, adds ~200 lines"
  )
  ```

- [ ] Step 2: Present diff-style changes to user for approval

- [ ] Step 3: If approved, dispatch subagent to write docs
  ```
  Task(
    subagent_type: "general",
    prompt: """
    Update project documentation with approved changes:

    FILE: docs/prd.md
    ACTION: Add new requirement section
    CONTENT: {approved_prd_content}
    LOCATION: Under ## Requirements section

    FILE: docs/architecture.md
    ACTION: Add new decision
    CONTENT: {approved_arch_content}
    LOCATION: Under ## Decisions Log section

    Follow existing format in each file.
    Return the new requirement/decision IDs created.
    """
  )
  ```

- [ ] Step 4: Create implementation plan with requirement references
  ```
  brain_save(
    type: "plan",
    title: "{Feature} - Implementation Plan",
    content: """
    # {Feature} - Implementation Plan

    ## Overview
    {Brief description}

    **Requirements:** PRD-REQ-{id} (docs/prd.md)
    **Architecture:** ARCH-DEC-{id} (docs/architecture.md)

    ## Tasks

    ### 1. {Task Name}
    **Implements:** PRD-REQ-{id}.1
    **Follows:** ARCH-DEC-{id}
    **Files:** src/services/...
    ...
    """,
    tags: ["{project}", "{feature}", "PRD-REQ-{id}"]
  )
  ```

- [ ] Step 5: Provide handoff information
  ```
  ## Ready for Execution

  **Plan:**
  - Brain ID: {planBrainId}
  - Title: "{Feature} - Implementation Plan"
  - Tasks: {N}

  **Requirements Reference:**
  - PRD: docs/prd.md → PRD-REQ-{id}
  - Architecture: docs/architecture.md → ARCH-DEC-{id}

  Execute with do-work:
    do-work graph <project>    # Review task dependencies
    do-work start <project> --foreground --tui
  ```

- [ ] Step 6: Complete session
  ```
  plan_phase(action: "transition", to: "idle")
  ```

## Document Templates

### docs/prd.md Template

```markdown
# Product Requirements Document

## Overview
{Project description}

## Goals
1. {Goal 1}
2. {Goal 2}

## Requirements

### PRD-REQ-001: {Feature Name}
**Added:** {date}
**Rationale:** {why}

- Requirement 1
- Requirement 2

## Scope

**In Scope:**
- ...

**Out of Scope:**
- ...

## Changelog
| Date | Change | Rationale |
|------|--------|-----------|
| {date} | Added {feature} | {why} |
```

### docs/architecture.md Template

```markdown
# Architecture

## Overview
{High-level architecture description}

## Patterns
- {Pattern 1}: {description}
- {Pattern 2}: {description}

## Components
### {Component Name}
- Purpose: ...
- Interfaces: ...

## Decisions Log

### ARCH-DEC-001: {Decision Title}
**Date:** {date}
**Status:** Accepted
**Context:** {why this decision was needed}
**Decision:** {what was decided}
**Consequences:** {tradeoffs}

## Constraints
- {Constraint 1}
- {Constraint 2}
```

## Anti-Patterns (NEVER Do These)

- **Skip INIT phase** - Always load project docs first
- **Skip clarifying questions** - Don't assume you understand the goal
- **Ignore architecture check** - Always validate against existing patterns
- **Write docs without approval** - Present changes to user first
- **Create plan without requirement refs** - Link to PRD/architecture IDs
- **Bypass phase enforcement** - Follow the workflow (use skip only for emergencies)

## Tool Quick Reference

| Tool | Phase | Purpose |
|------|-------|---------|
| `plan_phase(action: "start")` | IDLE→INIT | Begin planning session |
| `plan_discover_docs()` | INIT | Load PRD and architecture |
| `plan_capture_intent(...)` | UNDERSTAND | Store user requirements |
| `plan_record_arch_check(...)` | DESIGN | Record subagent analysis |
| `plan_propose_doc_updates(...)` | APPROVE | Format doc change proposal |
| `plan_phase(action: "status")` | Any | Check current phase |
| `plan_phase(action: "skip")` | Any | Emergency bypass |

## Integration with do-work

When handing off to execution:

1. **Plan is in brain** - Tasks are created with `brain_save(type: "task", depends_on: [...])`
2. **Requirements in docs** - Tasks reference `docs/prd.md` for acceptance criteria
3. **Architecture in docs** - Tasks reference `docs/architecture.md` for pattern compliance
4. **Parallel execution** - `do-work start <project>` handles dependency resolution and parallel execution
