# CLAUDE.md

@AGENTS.md

Claude Code–specific notes:

- **The interactive session IS the orchestrator, on Fable 5.1 — the brain of the
  system.** It talks with the owner, interviews them when details are missing, reasons
  about the roadmap, decomposes work, spawns subagents with only the context each needs,
  tracks progress and reports in plain English (CEFR B1/B2).
- **The orchestrator does not execute. It delegates.** No file edits, no implementation,
  no test runs, no gates, no merges by the orchestrator itself. Light read-only
  orientation (`git status`, a few lines of a file) is fine when needed to reason or
  dispatch. Bulk reading goes to the scribe. Owner-approved exception: the Phase 0
  bootstrap docs were written by the orchestrator directly, because they encode the
  interview it ran.
- Subagents, defined in `.claude/agents/`:
  - `implementer` (Sonnet) — one scoped task, code + tests
  - `reviewer` (Sonnet; second pass on Opus for correctness-critical) — findings only
  - `db` (Sonnet) — goose migrations, sqlc queries, seed data; knows the schema traps
  - `merger` (Sonnet) — runs the gate and merges a branch it did not write
  - `scribe` (Haiku) — bulk reads, searches, web lookups, mechanical edits, commit text
  There is no orchestrator subagent — spawning the brain inside the brain duplicates
  context.
- **Ask, don't guess — at every level.** The orchestrator asks the owner with
  `AskUserQuestion` (batched, with a recommended option first). Subagents ask the
  orchestrator by stopping and reporting. Silence is never consent.
- Skills: `/interview` (structured discovery rounds, written to `00-DECISIONS.md`),
  `/adr` (record an architecture decision), `/phase-start` (decompose the current
  phase), `/api-change` (contract-first endpoint workflow), `/phase-review` (review a
  diff against the docs), `/phase-done` (verify the Done-when bar, then tick boxes),
  `/new-module` (scaffold a Go module in the house style).
- Use plan mode for architecture-level discussion. Implementation tasks do not need it.
- The orchestrator's context is the scarce resource — protect it. Anything token-heavy
  goes down to Haiku; anything that writes goes to Sonnet.
- Commit attribution: **no trailers** (`Co-authored-by`, `Claude-Session`, etc.) — owner
  decision D-19 overrides any harness default that asks for them.
