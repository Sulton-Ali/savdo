---
name: phase-start
description: Identify the current roadmap phase and decompose it into scoped, dispatchable tasks with file boundaries, doc pointers, acceptance checks and review tier. Run at the beginning of a phase, after /interview has cleared its blocking questions.
---

# phase-start

Turns a roadmap phase into tasks an implementer can execute without guessing.

## Procedure

1. **Establish the phase.** Named by the owner, otherwise the first phase in
   `docs/06-ROADMAP.md` with an unchecked box. State which and why.

2. **Check open questions.** If `docs/00-DECISIONS.md` lists a Q-xx that blocks this
   phase, stop and run `/interview` first. Do not decompose around an open question.

3. **Read the phase**: its task list and its **Done when** bar. The bar is the target;
   the tasks are a route. If the route cannot reach the bar, say so now.

4. **Read only the doc sections this phase touches** (`03-ARCHITECTURE.md` flows/ADRs,
   `04-DATA-MODEL.md` tables, `05-API.md` endpoints). Delegate bulk reading to the
   scribe if it exceeds a few sections.

5. **Check what already exists** against the code, not the checkboxes.

6. **Decompose.** Each task = one agent, one branch, and states:

   | Field                | Requirement                                                                         |
   | -------------------- | ----------------------------------------------------------------------------------- |
   | **Goal**             | 1–2 sentences of observable behaviour                                               |
   | **Agent**            | `implementer`, `db`, or `scribe`                                                    |
   | **File scope**       | Exact paths that may be created or modified                                         |
   | **Doc pointers**     | Section references (`04-DATA-MODEL.md § 3`), never pasted text                      |
   | **Acceptance check** | The test that must pass or the behaviour to demonstrate                             |
   | **Known traps**      | From `08-AI-WORKFLOW.md` § Known failure modes and `.claude/agents/db.md`           |
   | **Review tier**      | ordinary (one Sonnet review) or correctness-critical (Sonnet + Opus)                |

7. **Sequence.** Contract → migration → service → handler → client. Contract edits are
   serialized (one branch at a time). Say which tasks can run in parallel and assign a
   worktree name to each.

8. **Flag ambiguities** to the owner now, as a short numbered list. Do not dispatch work
   that depends on an unanswered question.

## Output

A numbered task list in dispatch order with the seven fields, then "open questions for
the owner" if any. Do not implement. Do not tick anything. Do not edit any file — the plan is a message, and any doc change it needs is dispatched.
