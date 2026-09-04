# 05 — API

The REST contract is `contracts/openapi.yaml` (ADR-002). This document explains the
conventions the spec follows and lists the endpoint catalogue per phase so the
orchestrator can scope tasks. When this doc and the spec disagree, the spec is a bug —
fix the spec through `/api-change` and this doc in the same branch.

Base path `/v1`. JSON only. Server: Go, `api/cmd/api`, port 8080 behind Caddy.

## Conventions

- **Auth**: `Authorization: Bearer <token>` (mobile) or session cookie `savdo_session`
  (web admin). Public endpoints under `/v1/public/*` need no auth. CSRF: cookie is
  `SameSite=Lax` and mutating requests require the `X-Requested-With: savdo` header.
  Behind the production proxy the API trusts only the **last** `X-Forwarded-For` hop (Caddy must set `trusted_proxies`); in dev it uses the socket address.
- **Locale**: `Accept-Language` (`uz`, `ru`, `en`); responses include `locale` and
  `translationFallback: true` when a fallback was used.
- **IDs**: UUID strings. **Money**: decimal strings (`"125000.00"`). **Quantities**:
  decimal strings. **Times**: RFC 3339 UTC.
- **Pagination**: cursor-based on every collection: `?limit=50&cursor=…` →
  `{ items: [...], nextCursor: string | null }`. Never `?page=` / `?offset=`.
  `limit` is 1–200 (default 50); an unparseable cursor is `400 VALIDATION_FAILED` with `fields.cursor: invalid`.
- **Filtering/sorting**: explicit query params per endpoint, documented in the spec.
  Free-text search via `?q=` uses Postgres `ILIKE`/trigram; no external search engine.
- **Idempotency**: `POST /sales`, `POST /purchases/{id}/receive` and stock adjustments
  accept `Idempotency-Key`; a replay returns the original result.
- **Errors** (ADR-013):

  ```json
  { "error": { "code": "STOCK_INSUFFICIENT", "details": { "variantId": "…", "available": "2.000" } } }
  ```

  HTTP status by class: `400 VALIDATION_FAILED` (with `details.fields`), `401
  UNAUTHENTICATED`, `403 FORBIDDEN`, `404 NOT_FOUND`, `409` for state conflicts
  (`STOCK_INSUFFICIENT`, `SALE_ALREADY_VOIDED`, `PURCHASE_ALREADY_RECEIVED`,
  `DUPLICATE_SKU`, …), `429 RATE_LIMITED`, `500 INTERNAL`. The full enum lives in the
  spec under `components.schemas.ErrorCode`; adding a code means adding it there.
- **Validation and conflict vocabulary** (O-12): `details.fields` maps field → one of `required`, `invalid`, `too_short`, `too_long`; a uniqueness violation is `409 CONFLICT` with `details.field` naming the field (`username`, `phone`, `name`). Clients translate these words; nothing else is used.
- **Role-shaped responses**: the same endpoint returns fewer fields for `cashier`
  (`costPrice`, `unitCost`, margin fields absent, not null). The spec models this with
  `ProductStaff` / `ProductCashier` / `ProductPublic` schemas.
- **Versioning**: additive changes only within `/v1`. A breaking change is `/v2` and an
  owner decision.

## Endpoint catalogue

Phase numbers refer to `06-ROADMAP.md`.

### Auth (Phase 1; Telegram parts Phase 7)

| Method | Path                         | Role   | Notes                                   |
| ------ | ---------------------------- | ------ | --------------------------------------- |
| POST   | `/auth/login`                | —      | username + password → session           |
| POST   | `/auth/logout`               | any    | revoke current session                  |
| GET    | `/auth/me`                   | any    | user, role, shop summary, permissions   |
| GET    | `/auth/sessions`             | any    | own sessions                            |
| DELETE | `/auth/sessions/{id}`        | any    | revoke                                  |
| POST   | `/auth/telegram`             | —      | Telegram Login payload → session (P7)   |
| POST   | `/auth/otp/request`          | —      | purpose + username → code via bot (P7)  |
| POST   | `/auth/otp/verify`           | —      | code → action token (P7)                |
| POST   | `/auth/password/reset`       | —      | action token + new password (P7)        |

### Shop, locations, staff (Phase 1)

| Method | Path                     | Role     |
| ------ | ------------------------ | -------- |
| GET    | `/shop`                  | any      |
| PATCH  | `/shop`                  | owner    |
| GET    | `/locations`             | any      |
| POST   | `/locations`             | owner    |
| PATCH  | `/locations/{id}`        | owner    |
| GET    | `/staff`                 | owner    |
| POST   | `/staff`                 | owner    |
| PATCH  | `/staff/{id}`            | owner    |
| POST   | `/staff/{id}/password`   | owner    |

### Catalogue and media (Phase 2)

| Method | Path                                | Role            |
| ------ | ----------------------------------- | --------------- |
| GET    | `/units`                            | any             |
| GET/POST | `/attribute-definitions`          | manager+        |
| GET/POST | `/categories`                     | any / manager+  |
| GET/PATCH/DELETE | `/categories/{id}`        | any / manager+  |
| GET/POST | `/products`                       | any / manager+  |
| GET/PATCH/DELETE | `/products/{id}`          | any / manager+  |
| GET/POST | `/products/{id}/variants`         | any / manager+  |
| PATCH/DELETE | `/variants/{id}`              | manager+        |
| POST   | `/media`                            | manager+ (multipart) |
| POST/DELETE | `/products/{id}/images`        | manager+        |
| PATCH  | `/products/{id}/images/order`       | manager+        |

### Stock, purchases, suppliers (Phase 3)

| Method | Path                            | Role       |
| ------ | ------------------------------- | ---------- |
| GET/POST | `/suppliers`                  | manager+   |
| GET/PATCH/DELETE | `/suppliers/{id}`     | manager+   |
| GET/POST | `/purchases`                  | manager+   |
| GET/PATCH | `/purchases/{id}`            | manager+ (PATCH only while draft) |
| POST   | `/purchases/{id}/receive`       | manager+   |
| POST   | `/purchases/{id}/cancel`        | manager+   |
| GET    | `/stock/levels`                 | any (cashier per Q-02) |
| GET    | `/stock/movements`              | manager+   |
| POST   | `/stock/adjustments`            | manager+   |
| POST   | `/stock/transfers`              | manager+   |
| GET    | `/stock/low`                    | manager+   |

### Sales, customers, discounts, reports (Phase 4)

| Method | Path                         | Role                 |
| ------ | ---------------------------- | -------------------- |
| GET/POST | `/customers`               | cashier+             |
| GET/PATCH/DELETE | `/customers/{id}`  | cashier+ / manager+  |
| GET/POST | `/sales`                   | cashier+             |
| GET    | `/sales/{id}`                | cashier+             |
| POST   | `/sales/{id}/void`           | manager+             |
| POST   | `/sales/{id}/return`         | manager+             |
| GET/POST | `/discounts`               | manager+             |
| PATCH/DELETE | `/discounts/{id}`      | manager+             |
| GET    | `/reports/sales/summary`     | manager+ (cashier: own day) |
| GET    | `/reports/sales/by-product`  | manager+             |
| GET    | `/reports/stock/low`         | manager+             |

### Content and public (Phase 6)

| Method | Path                            | Role      |
| ------ | ------------------------------- | --------- |
| GET/PUT | `/content/{key}`               | manager+  |
| GET    | `/public/shop`                  | public    |
| GET    | `/public/categories`            | public    |
| GET    | `/public/products`              | public    |
| GET    | `/public/products/{slug}`       | public    |

### Bot (Phase 7)

| Method | Path                                | Role      |
| ------ | ----------------------------------- | --------- |
| POST   | `/bot/webhook/{secret}`             | Telegram  |
| GET    | `/bot/conversations`                | manager+  |
| GET    | `/bot/conversations/{id}/messages`  | manager+  |

### Ops

`GET /healthz`, `GET /readyz`, `GET /metrics` (Prometheus, internal network only).

## Rules for agents

1. Change the spec first (`/api-change`), regenerate, then implement. A handler whose
   shape differs from the spec does not compile — that is the point.
2. Every new error code goes into `ErrorCode` in the spec **and** into this doc's error
   list if it is a new class.
3. Never return `costPrice`/`unitCost`/margins on a cashier or public schema.
4. Public endpoints return `availability`, never quantities.
5. Collections are cursor-paginated. No exceptions.
