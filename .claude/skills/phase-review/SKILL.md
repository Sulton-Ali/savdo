---
name: phase-review
description: Review a branch or the current roadmap phase's code against the docs, ADRs and Done-when bar. Findings only — never fixes, never edits. Dispatch on Sonnet for ordinary work, and additionally on Opus with a split scope for auth, stock, sales and the ai/bot boundary.
---

# phase-review

Reviews Savdo code against its specification. **Produces findings. Touches no file.**

If you want to fix something, that urge is the point of the gate. Report it.

## Procedure

1. **Scope**: a branch diff (`git diff main...<branch>`), a phase, or named paths.
2. **Bar**: the phase's task list and Done-when line from `docs/06-ROADMAP.md`.
3. **Docs**: only the sections the diff implements.
4. **Review the code, not its existence.** A file being present is not behaviour being
   correct. Trace queries; do not assume a layer handled it.

## Checklist (say which groups you checked, even the clean ones)

Use the checklist in `.claude/agents/reviewer.md` — scope, contract, data,
correctness-critical, security, versions.

## Output

Findings grouped **CRITICAL / MAJOR / MINOR**, each `file:line` · why it matters ·
pointer to the fix. Then a one-line verdict: does the phase's Done-when bar hold, not
hold, or not yet testable. If it holds, say the orchestrator may run `/phase-done` —
do not run it yourself.
