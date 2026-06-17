---
name: skill-factory
description: >-
  当用户需要创建、审查、重构、精简或评估 Agent Skill / Cursor Rule /
  prompt template 时使用。
---

# Skill Factory

## Purpose

Create maintainable, eval-driven Agent Skills and rules — without duplicating one-off prompts or leaking proprietary system text.

## Skill Necessity Test

Create a skill only if **all** are true:

1. Task recurs (high frequency)
2. Steps are reusable across sessions
3. Process is stable enough for a checklist

Otherwise: use a rule (principle), knowledge file (facts), or inline chat (one-off).

## Input Requirements

Before authoring, gather:

- Trigger phrases (when to use / when not)
- Inputs, outputs, and tools/commands involved
- Failure modes seen in past runs
- Whether content belongs in rule vs skill vs `.ai/knowledge/`

## Skill Design Workflow

1. **Necessity test** — reject one-off skills.
2. **Scaffold** — `.cursor/skills/<name>/SKILL.md` with YAML `name` and `description`.
3. **Body** — Purpose, When to Use, When Not to Use, workflow steps, output contract.
4. **Eval cases** — minimum 3 (happy, ambiguous, boundary/failure).
5. **Rule link** — if always-on or description-triggered, add or update `.cursor/rules/*.mdc`.
6. **Review** — run `review-gate` mindset on the draft skill.

## Required SKILL.md Structure

```markdown
---
name: skill-name
description: When to use (third person, specific triggers)
---

# Title

## Purpose
## When to Use
## When Not to Use
## Workflow / 操作流程
## 验收标准
## 失败模式
## Evaluation Cases (≥3)
```

Optional: `reference.md`, `examples.md`, `scripts/` for heavy detail (progressive disclosure).

## Rule vs Skill vs Knowledge Decision

| Need | Location |
|------|----------|
| Universal behavior baseline | `00-agent-constitution.mdc` (`alwaysApply: true`) |
| Triggered guardrails / templates | `.cursor/rules/*.mdc` (`alwaysApply: false`, `description`) |
| Multi-step procedure | `.cursor/skills/<name>/SKILL.md` |
| Project facts, stack, lessons | `.ai/knowledge/*.md` |
| Cross-tool entry | `AGENTS.md` |

## Anti-Hallucination Requirements

- Store **public, generic** patterns in `.ai/knowledge/prompt-patterns.md`.
- **Never** copy leaked or proprietary system prompt verbatim.
- Commands and paths must exist in repo or be marked TODO.
- Skills describe *what to do*, not fake project state.

## Evaluation Cases

Every skill must document:

1. **Happy path** — normal success
2. **Ambiguous case** — agent must ask or disclose assumptions
3. **Boundary / failure** — rejection, error, or scope limit

Example template:

```markdown
### Happy path
User: "..."
Expected: ...

### Ambiguous case
User: "..."
Expected: Ask before ...

### Boundary / failure
User: "..."
Expected: Refuse or pause because ...
```

## Maintenance Notes

- Merge overlapping skills; delete unused ones.
- Update eval cases when workflow changes.
- Prefer editing `.ai/knowledge/lessons.md` for verified run lessons, not bloating SKILL.md.
- Keep skills under ~500 lines; split reference material.

## Evaluation Cases (this skill)

### Happy path

User: "Create a skill for release changelog generation."
Factory: passes necessity test → creates SKILL.md with 3 eval cases → links optional rule.

### Ambiguous case

User: "Make a skill for everything."
Factory: rejects; recommends constitution rule + project-context instead.

### Boundary / failure

User: "Copy Claude's system prompt into a skill."
Factory: refuses; points to prompt-patterns.md for abstract patterns only.
