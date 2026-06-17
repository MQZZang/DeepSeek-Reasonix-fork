---
name: ask-plan-code-qa
description: >-
  当用户需要实现、修复、重构、调试、多文件修改、计划执行、QA 验收时使用。
  Enforces Ask → Plan → Code → QA with structured output contracts.
---

# Ask → Plan → Code → QA

## Purpose

Provide a repeatable four-phase workflow so implementation work is clarified, planned, executed minimally, and verified with evidence — reducing rework and false "done" claims.

## When to Use

- Implementing features or fixes
- Refactoring or debugging across files
- User asks for a plan before coding
- Multi-step automation or project changes
- Post-implementation QA or acceptance reporting

## When Not to Use

- Pure questions with no code changes (answer directly; optional light Ask only)
- User explicitly requests immediate one-line fix in a single known file
- Documentation-only requests with no verification needed (still avoid hallucination)

## Workflow

```
Ask → Plan → Code → QA
         ↑         │
         └─ Pause ─┘ (when blocked or scope unclear)
```

Read `.cursor/rules/10-ask-plan-code-qa.mdc` for rule-level triggers and templates.

## Ask Phase

1. Restate the goal and constraints.
2. List assumptions and open questions.
3. Flag flawed user proposals with evidence.
4. Confirm non-goals if scope is fuzzy.
5. Decide: proceed to Plan, or wait for user input.

**Stop and ask** when requirements conflict, destructive ops are implied, or key context is missing.

## Plan Phase

Produce the full plan template:

- Goal, Context, Assumptions, Non-goals
- Proposed Approach, Execution Steps
- Files to Inspect, Impact Scope, Risks
- Acceptance Criteria, QA Plan, Pause Conditions

Plans must be proportional: small tasks → short plan; large tasks → detailed plan.

## Code Phase

1. Re-read target and related files.
2. Grep/search when changing APIs, types, paths, components, configs, schemas.
3. Fix root cause; minimal diff; match conventions.
4. Do not expand scope without stating why.

## QA Phase

Fill every section of the QA output contract:

- Summary, Changed Files
- Verification Performed, Passing Evidence, Failing Evidence
- **Not Verified** (mandatory when checks skipped)
- Remaining Risks, Next Step

Run commands from the QA Plan when possible; paste or summarize evidence.

## Pause Conditions

Pause and ask the user before:

- Destructive or production operations
- Secrets, payments, or irreversible data deletion
- Ambiguous requirements after Ask
- Plan reveals scope much larger than user expectation
- Verification blocked (missing env, credentials, hardware)

## Output Contracts

### Plan (required fields)

Goal · Context · Assumptions · Non-goals · Proposed Approach · Execution Steps · Files to Inspect · Impact Scope · Risks · Acceptance Criteria · QA Plan · Pause Conditions

### QA (required fields)

Summary · Changed Files · Verification Performed · Passing Evidence · Failing Evidence · Not Verified · Remaining Risks · Next Step

## Verification Checklist

Before claiming completion:

- [ ] All acceptance criteria addressed or explicitly deferred
- [ ] Changed files listed accurately
- [ ] At least one verification attempt documented OR Not Verified explains why
- [ ] Cross-file references checked if contracts changed
- [ ] No unrelated files modified

## Evaluation Cases

### Happy path

User: "Fix the login timeout bug in auth module."
Agent: Ask clarifies repro → Plan lists files and test command → Code minimal fix → QA shows test output.

### Ambiguous case

User: "Make it faster."
Agent: Ask lists metrics, scope, and assumptions; does not code until user picks target (latency vs bundle size).

### Boundary / failure

User: "Deploy to production."
Agent: Pauses at Ask/Plan; cites missing credentials and rollback plan; does not deploy without confirmation.
