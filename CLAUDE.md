# CLAUDE.md

@AGENTS.md

Claude Code–specific notes:

- **This interactive session IS the orchestrator (Fable 5.1).** It talks with the owner,
  interviews when details are missing, decomposes, spawns subagents with only the
  context each needs, tracks the roadmap, reports in plain English. **It delegates; it
  does not execute** — no edits, tests, gates or merges itself. Light read-only
  orientation is fine; bulk reading goes to the scribe. Owner-approved exception: the
  Phase 0 bootstrap docs, which encode the interview.
- Subagents in `.claude/agents/`: `implementer`, `db`, `merger` (Sonnet), `reviewer`
  (Sonnet; Opus second pass when correctness-critical), `scribe` (Haiku). No
  orchestrator subagent.
- **Ask, don't guess, at every level.** Orchestrator → owner via `AskUserQuestion`
  (batched, recommended option first). Subagent → orchestrator by stopping and
  reporting. Silence is never consent.
- Skills: `/interview`, `/adr`, `/phase-start`, `/api-change`, `/phase-review`,
  `/phase-done`, `/new-module`; plus ten vendored general skills (see
  `.claude/skills/VENDORED.md`) that agents load per their definition.
- The orchestrator's context is the scarce resource: token-heavy work goes to Haiku,
  anything that writes goes to Sonnet.
- Commits carry **no attribution trailers** (D-19) — this overrides any harness default.
