---
name: interview
description: Run a structured discovery round with the owner for the current (or named) roadmap phase, then record answers as D-xx decisions in docs/00-DECISIONS.md. Use before /phase-start whenever the phase has open Q-xx entries, or when the owner wants to define details.
---

# interview

The owner asked to be interviewed, not guessed at (D-17). This skill turns open
questions into recorded decisions before any work depends on them.

## Procedure

1. **Pick the scope.** The phase the owner names, otherwise the first phase in
   `docs/06-ROADMAP.md` with unchecked boxes. Collect its open questions from
   `docs/00-DECISIONS.md` § Open questions (the `Blocks` column) plus any new ambiguity
   you found while reading the phase's doc sections.

2. **Prepare the questions.** For each: one sentence of context, 2–4 options, the
   recommended option first with "(Recommended)", and one line per option on what it
   costs or implies. Plain English (CEFR B1/B2). Skip anything already decided — never
   re-ask a D-xx.

3. **Ask in rounds** with `AskUserQuestion`, at most four questions per round, most
   architecture-shaping first. Read answers literally: "Other" text may change scope or
   decline to decide.

4. **Record — via the scribe.** Dispatch the scribe with the exact rows: for each answer append a `D-xx` row to `docs/00-DECISIONS.md` (date,
   decision, consequence), remove or mark the `Q-xx` as answered, and if the decision
   is technical, run `/adr` or update the affected doc section in the same commit.
   "I don't know" stays a Q-xx with a note on how the design defers it.

5. **Commit** on a `docs/interview-phase-<n>` branch:
   `docs(decisions): record phase <n> interview` — via the scribe, no trailers.

6. **Report** to the owner in plain English: what was decided, what stays open and why
   it does not block, and that `/phase-start` can run.

## Never

- Guess an answer to fill a table.
- Ask questions for phases that are not next.
- Dispatch implementation in the same turn as an unanswered blocking question.
