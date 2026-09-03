---
name: adr
description: Record, amend or reverse an Architecture Decision Record in docs/03-ARCHITECTURE.md and cross-link it in docs/00-DECISIONS.md. Use whenever a technical decision is made or overruled by the owner.
---

# adr

## Procedure

1. **Confirm it is a decision, not a task.** An ADR changes how things are built across
   modules. A one-off implementation choice is not an ADR.
2. **Check for an existing ADR** on the topic in `docs/03-ARCHITECTURE.md`. Amend it
   (append "Amended <date>: …") rather than creating a duplicate; reversing one needs
   the owner's explicit words quoted.
3. **Write it** with the next number, format **Context → Decision → Consequences**, five
   to fifteen lines. Name the alternatives considered in one line each.
4. **Cross-link**: add an `O-xx` (orchestrator) or `D-xx` (owner) row in
   `docs/00-DECISIONS.md` referencing the ADR; update `AGENTS.md` § Hard rules or
   § Stack only if the ADR adds a rule agents must obey.
5. **Update affected docs** (`04-DATA-MODEL.md`, `05-API.md`, `07-DEVOPS.md`) in the same
   branch so the spec stays consistent.
6. Commit as `docs(adr): ADR-0xx <title>` on a `docs/` branch. No trailers.

Owner approval is required before an ADR is written when it touches: auth, roles, money,
ledger rules, dependencies, hosting, or anything in `01-OVERVIEW.md` § Non-goals.
