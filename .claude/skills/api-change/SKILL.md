---
name: api-change
description: Contract-first workflow for adding or changing an endpoint — edit contracts/openapi.yaml, regenerate Go and TypeScript, then implement. Use for every API change; hand-declared shapes are a review finding (ADR-002).
---

# api-change

## Procedure (for the dispatched implementer)

1. **Read** `docs/05-API.md` § Conventions and the catalogue row for the endpoint. If
   the row does not exist, stop: the orchestrator adds it to the doc first (owner may
   need to decide).
2. **Edit `contracts/openapi.yaml`**: path, operationId (`<module>.<verb><Noun>`),
   request/response schemas under `components.schemas`, error codes added to the
   `ErrorCode` enum, role-shaped variants (`…Staff` / `…Cashier` / `…Public`) where
   fields differ by role, cursor pagination envelope for collections, money and
   quantity as `string` with `format: decimal`.
3. **`make generate`.** Commit the regenerated `api/gen`, `packages/api-client`.
4. **Implement the Go handler** against the generated interface; the compiler enforces
   the shape. Service and sqlc queries per the task.
5. **Update clients** only if the task's file scope includes them.
6. **Tests**: handler test for each status code the spec declares; integration test for
   the behaviour.
7. **Doc**: if you added an error code or changed a convention, update `05-API.md` in
   the same branch.

## Rules

- One branch edits the contract at a time — the orchestrator serializes.
- Never `fetch` by hand in TypeScript; always the generated client.
- Additive within `/v1`; a breaking change is an owner decision.
