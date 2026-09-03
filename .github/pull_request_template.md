## Task

Phase / task id: `phase-<n>/t<id>`
Goal (one line):

## Checklist (the merger verifies every line)

- [ ] `make verify` green locally (run by the merger, not only the author)
- [ ] `/phase-review` clean — no unresolved CRITICAL/MAJOR (no MINOR for `auth`, `stock`, `sales`, `ai`/`bot`)
- [ ] Diff stays inside the declared file scope; one concern
- [ ] Tests ship in this change and exercise the behaviour
- [ ] Contract-first: any endpoint change started in `contracts/openapi.yaml`, generated code committed and fresh
- [ ] Migrations: new files only, numbered after `main`, real `Down`; destructive changes quote owner approval
- [ ] `shop_id` filtered in every query; no `costPrice`/`unitCost` in cashier or public paths
- [ ] No hand-declared DTOs; no `float` money; no direct `stock_levels` writes
- [ ] No secrets, tokens, or `.env` content anywhere in the diff
- [ ] No roadmap checkbox ticked; no attribution trailers in commits
- [ ] Correctness-critical: two reviews on different models recorded in the merge body

## Notes for the reviewer

What you looked at, what you deliberately left alone (out of scope), open questions.
