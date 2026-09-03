---
name: phase-done
description: Verify a roadmap phase's Done-when bar actually holds by running it, then tick its checkboxes and append to the phase log. The ONLY thing permitted to tick a checkbox in docs/06-ROADMAP.md.
---

# phase-done

The only procedure allowed to tick a checkbox in `docs/06-ROADMAP.md`.

Executed by a **dispatched Sonnet agent** with the full skill text as its brief — never by the orchestrator itself (D-27). The orchestrator reads the report and relays it to the owner.

**Verification first, ticking second.** A box ticked because the code exists makes the
roadmap lie, and later agents read the roadmap as truth.

## Procedure

1. **Identify the phase** — named, or the first with unchecked boxes.
2. **Read its Done-when bar.** That is the target, not the task list.
3. **Exercise the bar. Actually run it.**
   - `make dev-infra`, `make migrate`, `make seed`, `make api` (and `make bot` for
     Phase 7); start the web apps the bar mentions.
   - Walk the scenario end to end: `curl`, the browser via the playwright MCP, or the
     Expo app. Record commands and observed output.
   - Run `make verify` and `pnpm -r test:e2e`.
   - For Phases 3, 4 and 7 confirm the correctness-critical tests exist, are unskipped,
     and pass. For Phase 8 the restore drill counts only if a real dump was restored
     into a scratch container and the API booted against it.
4. **Check every task box individually** from the code and your run. List what is not
   done.
5. **Decide.** Bar holds and all tasks done → tick and go to 6. Bar holds but tasks
   remain → tick nothing, report which remain (owner decides to finish or drop). Bar
   fails → tick nothing, report exactly what failed with output.
6. **Append to the phase log** in `06-ROADMAP.md`: phase, date, "Verified by" = what
   you **did**, "Deferred" = what moved where.
7. **Update `08-AI-WORKFLOW.md`**: metrics for the phase and any new known failure
   mode a review caught.
8. **Report to the owner** in plain English: verified, ticked, deferred, next phase.
   The owner accepts or rejects.

## Never

Tick a box you did not verify · tick speculatively · close a phase with a failing or
skipped test or an unexplained deferral · edit boxes outside the phase being closed.
