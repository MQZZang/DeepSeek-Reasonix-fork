# Prompt & Agent Design Patterns

This directory stores **public, generic, non-proprietary** prompt and agent design patterns for reuse across tools (Cursor, Claude, Codex, etc.).

## Policy

- **Allowed:** Abstract workflows, output contracts, review checklists, verification habits.
- **Forbidden:** Storing or reproducing leaked system prompt text, proprietary vendor prompts, or copy-paste of internal model instructions.

When adding content, write original summaries — link to public docs if needed, do not transcribe secrets.

---

## Patterns

### Progressive disclosure

Load minimal context first; pull detailed references only when the task requires them. Skills use short `SKILL.md` + optional `reference.md` instead of one giant prompt.

### Evidence-based progress reporting

Claims require proof: command output, file paths, diffs, or explicit **Not Verified**. Ban "should work" without checks.

### Explicit pause conditions

Define when the agent must stop: destructive ops, production, secrets, ambiguous scope, missing credentials. Pause beats guessing.

### Root-cause-first debugging

Reproduce → localize → identify cause → fix at source. Avoid masking symptoms in downstream layers.

### Smallest sufficient change

One problem, one minimal diff. No unrelated refactors, abstractions, or "while I'm here" edits.

### Assumption disclosure

When requirements are multi-interpretable, list assumptions before acting. Update plan if assumptions change.

### Review gates

Separate phases for assumption, context, plan, code, and QA review. Each finding needs 问题 / 证据 / 风险 / 修正建议 — not blanket approval.

### Eval-driven skill refinement

Every skill ships with ≥3 eval cases (happy, ambiguous, boundary/failure). Update cases when real sessions expose new failure modes; record durable lessons in `lessons.md`.
