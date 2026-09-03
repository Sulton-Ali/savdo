# Savdo

Light ERP + CRM for small shops in Uzbekistan. One Go API, a public landing
(TanStack Start), an admin panel (Vite + React), a mobile admin app (Expo) and a
Telegram bot with AI answers over the shop's own data.

First client: a family clothing shop. Built single-shop, designed tenant-ready.

**This repository is built AI-native.** The owner is the Product Owner and writes no
code. An orchestrator session (Claude, Fable 5.1) decomposes the roadmap and delegates
to implementer, reviewer, scribe and db agents. See `AGENTS.md` and
`docs/08-AI-WORKFLOW.md`.

## Docs

| Doc                        | What it holds                                              |
| -------------------------- | ---------------------------------------------------------- |
| `docs/00-DECISIONS.md`     | Interview log: every owner decision and every open question |
| `docs/01-OVERVIEW.md`      | Product pitch, personas, user stories, non-goals            |
| `docs/02-TECH-STACK.md`    | Pinned versions and why                                     |
| `docs/03-ARCHITECTURE.md`  | Monorepo layout, module map, key flows, ADRs                |
| `docs/04-DATA-MODEL.md`    | Schema, ledgers, permissions, rules for agents              |
| `docs/05-API.md`           | REST contract conventions and endpoint catalogue            |
| `docs/06-ROADMAP.md`       | Phases with Done-when bars — the living state               |
| `docs/07-DEVOPS.md`        | Local stack, the `make verify` gate, branch protocol, deploy |
| `docs/08-AI-WORKFLOW.md`   | The agent fleet, the loop, context discipline               |

## Status

Phase 0 — bootstrap. Scaffolds merged for the Go API (healthz), contract + generated clients, admin (Ant Design), landing (TanStack Start SSR), mobile (Expo) and shared packages; CI in progress; the phase closes with `/phase-done`.
