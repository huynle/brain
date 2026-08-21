---
name: test-driven-development
description: Use when implementing any feature or bugfix - triages work into Route T (full TDD), Route V (tests needed, not strict TDD), or Route S (no tests needed); ensures tests verify behavior by requiring failure first for Route T work
---

# Test-Driven Development (TDD)

## Overview

Write the test first. Watch it fail. Write minimal code to pass.

**Core principle:** If you didn't watch the test fail, you don't know if it tests the right thing.

**Senior engineer principle:** Not everything needs TDD. Know the difference.

## TDD Triage — What Needs TDD?

Before writing any code, classify the work:

### Route T: Full TDD (test first, watch fail, implement)

| Indicator | Example |
|-----------|---------|
| Business logic | Validation, calculations, state machines |
| Conditional behavior | if/else, switch, error handling paths |
| Data transformations | Parsing, mapping, filtering, aggregation |
| API endpoints | Request handling, response shaping |
| Bug fixes | Must reproduce with test first |
| Edge cases exist | Null handling, boundary conditions |
| Side effects | File I/O, network calls, DB operations |

### Route V: Tests needed, not strict TDD (write code + tests together)

| Indicator | Example |
|-----------|---------|
| Integration wiring | Connecting existing tested components |
| Following established pattern | "Create endpoint like existing ones" |
| Thin adapter/wrapper | Delegates to already-tested code |
| Simple CRUD | Standard create/read/update/delete with no special logic |

### Route S: No tests needed (skip TDD entirely)

| Indicator | Example |
|-----------|---------|
| Type definitions | Interfaces, types, enums |
| Configuration | Constants, env vars, feature flags |
| Declarative markup | HTML, CSS, templates |
| Documentation | Comments, READMEs, docstrings |
| Wiring/glue code | Imports, re-exports, module registration |
| Database migrations | Schema changes (tested by integration) |
| Logging/observability | Adding log lines, metrics |
| Scaffolding | Project structure, boilerplate |

### Decision Tree

```
Has logic/conditionals/edge cases? → Yes → Route T (Full TDD)
                ↓ No
Bug fix? → Yes → Route T (Full TDD)
                ↓ No
Integration/wiring of tested components? → Yes → Route V (Tests, not strict TDD)
                ↓ No
Types/config/docs/scaffolding? → Yes → Route S (Skip TDD)
                ↓ No
Default → Route T (Full TDD)
```

**When uncertain, prefer Route T.** Better to over-test than under-test.

**Classifying as Route S to avoid testing?** Stop. If it has logic, it's Route T. That's rationalization.

## The Iron Law (Route T)

```
NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST
```

Write code before the test? Delete it. Start over.

**No exceptions for Route T work:**
- Don't keep it as "reference"
- Don't "adapt" it while writing tests
- Don't look at it
- Delete means delete

Implement fresh from tests. Period.

## Red-Green-Refactor-Verify (Route T)

```
🔴 RED → 🟢 GREEN → 🔵 REFACTOR → ✅ VERIFY → (next increment or done)
```

```dot
digraph tdd_cycle {
    rankdir=LR;
    red [label="RED\nWrite failing test", shape=box, style=filled, fillcolor="#ffcccc"];
    green [label="GREEN\nMinimal code", shape=box, style=filled, fillcolor="#ccffcc"];
    refactor [label="REFACTOR\nClean up", shape=box, style=filled, fillcolor="#ccccff"];
    verify [label="VERIFY\nFull suite", shape=box, style=filled, fillcolor="#ffffcc"];
    next [label="Next\nincrement", shape=ellipse];

    red -> green [label="test fails\ncorrectly"];
    green -> refactor [label="test\npasses"];
    refactor -> verify [label="suite\npasses"];
    verify -> next;
    next -> red;
    
    green -> red [label="test was\nwrong", style=dashed];
    refactor -> green [label="broke\ntests", style=dashed];
}
```

### RED - Write Failing Test

Write one minimal test showing what should happen.

<Good>
```typescript
test('retries failed operations 3 times', async () => {
  let attempts = 0;
  const operation = () => {
    attempts++;
    if (attempts < 3) throw new Error('fail');
    return 'success';
  };

  const result = await retryOperation(operation);

  expect(result).toBe('success');
  expect(attempts).toBe(3);
});
```
Clear name, tests real behavior, one thing
</Good>

<Bad>
```typescript
test('retry works', async () => {
  const mock = jest.fn()
    .mockRejectedValueOnce(new Error())
    .mockRejectedValueOnce(new Error())
    .mockResolvedValueOnce('success');
  await retryOperation(mock);
  expect(mock).toHaveBeenCalledTimes(3);
});
```
Vague name, tests mock not code
</Bad>

**Requirements:**
- One behavior
- Clear name
- Real code (no mocks unless unavoidable)

### Verify RED - Watch It Fail

**MANDATORY. Never skip.**

```bash
npm test path/to/test.test.ts
```

Confirm:
- Test fails (not errors)
- Failure message is expected
- Fails because feature missing (not typos)

**Test passes?** You're testing existing behavior. Fix test.

**Test errors?** Fix error, re-run until it fails correctly.

### GREEN - Minimal Code

Write simplest code to pass the test.

<Good>
```typescript
async function retryOperation<T>(fn: () => Promise<T>): Promise<T> {
  for (let i = 0; i < 3; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i === 2) throw e;
    }
  }
  throw new Error('unreachable');
}
```
Just enough to pass
</Good>

<Bad>
```typescript
async function retryOperation<T>(
  fn: () => Promise<T>,
  options?: {
    maxRetries?: number;
    backoff?: 'linear' | 'exponential';
    onRetry?: (attempt: number) => void;
  }
): Promise<T> {
  // YAGNI
}
```
Over-engineered
</Bad>

Don't add features, refactor other code, or "improve" beyond the test.

### Verify GREEN - Watch It Pass

**MANDATORY.**

```bash
npm test path/to/test.test.ts
```

Confirm:
- Test passes
- Other tests still pass
- Output pristine (no errors, warnings)

**Test fails?** Fix code, not test.

**Other tests fail?** Fix now.

### REFACTOR - Clean Up

After green only. All files editable (test + impl + docs).

- Remove duplication
- Improve names
- Extract helpers
- Update related documentation

Keep tests green. Don't add behavior.

Run full test suite after refactoring to confirm nothing broke.

### VERIFY - Full Suite Gate

Run the **full** test suite (not just the new test):

```bash
npm test  # or pytest, go test ./..., cargo test
```

Confirm:
- ALL tests pass (not just the new ones)
- No regressions introduced
- Output clean

Only after VERIFY passes: commit changes or start next increment.

### Repeat

Next failing test for next feature.

## Good Tests

| Quality | Good | Bad |
|---------|------|-----|
| **Minimal** | One thing. "and" in name? Split it. | `test('validates email and domain and whitespace')` |
| **Clear** | Name describes behavior | `test('test1')` |
| **Shows intent** | Demonstrates desired API | Obscures what code should do |

## Why Order Matters

**"I'll write tests after to verify it works"**

Tests written after code pass immediately. Passing immediately proves nothing:
- Might test wrong thing
- Might test implementation, not behavior
- Might miss edge cases you forgot
- You never saw it catch the bug

Test-first forces you to see the test fail, proving it actually tests something.

**"I already manually tested all the edge cases"**

Manual testing is ad-hoc. You think you tested everything but:
- No record of what you tested
- Can't re-run when code changes
- Easy to forget cases under pressure
- "It worked when I tried it" ≠ comprehensive

Automated tests are systematic. They run the same way every time.

**"Deleting X hours of work is wasteful"**

Sunk cost fallacy. The time is already gone. Your choice now:
- Delete and rewrite with TDD (X more hours, high confidence)
- Keep it and add tests after (30 min, low confidence, likely bugs)

The "waste" is keeping code you can't trust. Working code without real tests is technical debt.

**"TDD is dogmatic, being pragmatic means adapting"**

TDD IS pragmatic:
- Finds bugs before commit (faster than debugging after)
- Prevents regressions (tests catch breaks immediately)
- Documents behavior (tests show how to use code)
- Enables refactoring (change freely, tests catch breaks)

"Pragmatic" shortcuts = debugging in production = slower.

**"Tests after achieve the same goals - it's spirit not ritual"**

No. Tests-after answer "What does this do?" Tests-first answer "What should this do?"

Tests-after are biased by your implementation. You test what you built, not what's required. You verify remembered edge cases, not discovered ones.

Tests-first force edge case discovery before implementing. Tests-after verify you remembered everything (you didn't).

30 minutes of tests after ≠ TDD. You get coverage, lose proof tests work.

## Common Rationalizations

| Excuse | Reality |
|--------|---------|
| "Too simple to test" | Has logic? Route T. No logic? Route S. "Simple" isn't a route. |
| "I'll test after" | Tests passing immediately prove nothing. Route T = test first. |
| "Tests after achieve same goals" | Tests-after = "what does this do?" Tests-first = "what should this do?" |
| "Already manually tested" | Ad-hoc ≠ systematic. No record, can't re-run. |
| "Deleting X hours is wasteful" | Sunk cost fallacy. Keeping unverified code is technical debt. |
| "Keep as reference, write tests first" | You'll adapt it. That's testing after. Delete means delete. |
| "Need to explore first" | Fine. Throw away exploration, start with TDD. |
| "Test hard = design unclear" | Listen to test. Hard to test = hard to use. |
| "TDD will slow me down" | TDD faster than debugging. Pragmatic = test-first. |
| "Manual test faster" | Manual doesn't prove edge cases. You'll re-test every change. |
| "Existing code has no tests" | You're improving it. Add tests for existing code. |
| "It's just types/config" | Correct — that's Route S. But if it has logic, it's Route T. |
| "Route S to avoid testing" | If it has conditionals or edge cases, it's Route T. Be honest. |

## Red Flags - STOP and Start Over

- Skipping triage — classify as Route T/V/S first
- Classifying Route T work as Route S to avoid testing
- Code before test (Route T)
- Test after implementation (Route T)
- Test passes immediately
- Can't explain why test failed
- Tests added "later"
- Rationalizing "just this once"
- "I already manually tested it"
- "Tests after achieve the same purpose"
- "It's about spirit not ritual"
- "Keep as reference" or "adapt existing code"
- "Already spent X hours, deleting is wasteful"
- "TDD is dogmatic, I'm being pragmatic"
- "This is different because..."

**For Route T work, all of these mean: Delete code. Start over with TDD.**

## Example: Bug Fix

**Bug:** Empty email accepted

**RED**
```typescript
test('rejects empty email', async () => {
  const result = await submitForm({ email: '' });
  expect(result.error).toBe('Email required');
});
```

**Verify RED**
```bash
$ npm test
FAIL: expected 'Email required', got undefined
```

**GREEN**
```typescript
function submitForm(data: FormData) {
  if (!data.email?.trim()) {
    return { error: 'Email required' };
  }
  // ...
}
```

**Verify GREEN**
```bash
$ npm test
PASS
```

**REFACTOR**
Extract validation for multiple fields if needed.

## Verification Checklist

Before marking work complete:

**Route T (Full TDD):**
- [ ] Triaged as Route T (has logic/conditionals/edge cases)
- [ ] Every new function/method has a test
- [ ] Watched each test fail before implementing (RED phase)
- [ ] Each test failed for expected reason (feature missing, not typo)
- [ ] Wrote minimal code to pass each test (GREEN phase)
- [ ] Refactored without adding behavior (REFACTOR phase)
- [ ] Full test suite passes (VERIFY phase)
- [ ] Output pristine (no errors, warnings)
- [ ] Tests use real code (mocks only if unavoidable)
- [ ] Edge cases and errors covered

**Route V (Tests, not strict TDD):**
- [ ] Triaged as Route V (integration/wiring/established pattern)
- [ ] Implementation complete
- [ ] Tests written covering the integration
- [ ] All tests pass

**Route S (No tests):**
- [ ] Triaged as Route S (types/config/docs/scaffolding)
- [ ] No testable logic present (if logic found, re-triage as Route T)

Can't check all boxes for your route? Fix it before claiming complete.

## When Stuck

| Problem | Solution |
|---------|----------|
| Don't know how to test | Write wished-for API. Write assertion first. Ask your human partner. |
| Test too complicated | Design too complicated. Simplify interface. |
| Must mock everything | Code too coupled. Use dependency injection. |
| Test setup huge | Extract helpers. Still complex? Simplify design. |

## Debugging Integration

Bug found? Write failing test reproducing it. Follow TDD cycle. Test proves fix and prevents regression.

Never fix bugs without a test.

## Final Rule

```
Route T production code → test exists and failed first
Otherwise → not TDD
```

No exceptions for Route T work without your human partner's permission.

## Tool Integration (Optional)

Projects using the `tdd_gate` tool can enforce TDD discipline programmatically:

```
tdd_gate(action: "start")       → Activates enforcement, enters RED
tdd_gate(action: "transition")  → Moves between phases with evidence
tdd_gate(action: "status")      → Shows current phase and rules
tdd_gate(action: "skip")        → Emergency one-time bypass (logged)
tdd_gate(action: "exit")        → Exits TDD mode
```

**4-phase enforced cycle:** 🔴 RED → 🟢 GREEN → 🔵 REFACTOR → ✅ VERIFY

| Phase | Allowed | Blocked | Evidence to advance |
|-------|---------|---------|-------------------|
| RED | Test files, stubs | Impl edits, commits | Test failure output |
| GREEN | Impl files only | Test files, docs, commits | Test pass output |
| REFACTOR | All files | Commits | Full suite pass output |
| VERIFY | All files, commits | Nothing | N/A (terminal) |

The `tdd_gate` tool is optional — this skill stands alone as methodology guidance. The tool adds programmatic enforcement for agents that need it.
