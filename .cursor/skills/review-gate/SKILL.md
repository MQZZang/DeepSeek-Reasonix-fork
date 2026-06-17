---
name: review-gate
description: >-
  当用户需要检查需求、计划、代码、QA 结果、风险、遗漏、幻觉或实现质量时使用。
  Evidence-backed review across assumptions, context, plan, code, and QA.
---

# Review Gate

## Purpose

Structured, evidence-based review to catch hallucinations, missing context, plan gaps, code issues, and false QA confidence before merge or sign-off.

## Review Types

| Type | Focus |
|------|--------|
| Assumption Review | Hidden or risky assumptions |
| Context Review | Repo reality vs agent understanding |
| Plan Review | Completeness, scope, verifiability |
| Code Review | Correctness, references, minimalism |
| QA Review | Evidence vs claims |

Run only the types relevant to the artifact under review.

## Assumption Review

- List assumptions the author relied on.
- Mark each: validated / unvalidated / contradicted.
- For each issue: 问题 · 证据 · 风险 · 修正建议

## Context Review

- Tech stack, directories, conventions — match `AGENTS.md` and `.ai/knowledge/project-context.md` when filled.
- Flag invented paths, commands, or dependencies.
- Output per finding: 问题 · 证据 · 风险 · 修正建议

## Plan Review

Check against the plan template in `ask-plan-code-qa`:

- Missing non-goals, files, risks, acceptance criteria, or QA plan?
- Steps ordered and testable?
- Pause conditions for destructive work?

Output per finding: 问题 · 证据 · 风险 · 修正建议

## Code Review

- Read actual diffs and related files — do not review from memory.
- Cross-file contract changes: grep evidence required.
- Root cause vs symptom patch; scope creep; over-engineering.

Output per finding: 问题 · 证据 · 风险 · 修正建议

## QA Review

- Does Passing Evidence support the Summary?
- Is Not Verified complete and honest?
- Failures hidden or minimized?

Output per finding: 问题 · 证据 · 风险 · 修正建议

## Required Output Format

```markdown
## Review Scope
<What was reviewed>

## Assumption Review
### Findings
- **问题:** ...
  **证据:** ...
  **风险:** ...
  **修正建议:** ...

## Context Review
...

## Plan Review
...

## Code Review
...

## QA Review
...

## Verdict
Proceed | Proceed with fixes | Block

## What Was Checked (even if clean)
<Bullet list of artifacts and commands reviewed>
```

If a section has zero issues, still state what was examined.

## Must Not Behavior

- Do not say "looks good" without listing checked evidence.
- Do not invent file contents, test passes, or user intent.
- Do not approve QA that lacks Passing Evidence for critical claims.
- Do not skip Assumption Review on ambiguous requirements.

## Evaluation Cases

### Happy path

Reviewer reads plan + diff + test log; lists checks; verdict Proceed with minor suggestions.

### Ambiguous case

QA says "tests pass" but no command output — QA Review flags Failing Evidence gap; verdict Block.

### Boundary / failure

Plan omits rollback for migration — Plan Review cites missing Pause Conditions; verdict Proceed with fixes.
