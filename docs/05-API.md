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
  `limit` is clamped to 1–200 (default 50), never rejected; an unparseable cursor is
  `400 VALIDATION_FAILED` with `details.fields.cursor: invalid`.
- **Filtering/sorting**: explicit query params per endpoint, documented in the spec.
  Free-text search via `?q=` uses Postgres `ILIKE`/trigram; no external search engine.
- **Idempotency**: `POST /sales`, `POST /purchases/{id}/receive` and stock adjustments
  accept `Idempotency-Key`; a replay returns the original result. The fingerprint is method + path + actor + canonical body; a different body under the same key returns `409 IDEMPOTENCY_KEY_REUSED`; the resolved locale is not part of it (D-51). Ledger writes that lose a Postgres deadlock return `409 CONFLICT` with `details.reason = deadlock` and may be retried.
- **Errors** (ADR-013):

  ```json
  { "error": { "code": "STOCK_INSUFFICIENT", "details": { "variantId": "…", "available": "2.000" } } }
  ```

  HTTP status by class: `400 VALIDATION_FAILED` (with `details.fields`), `401
  UNAUTHENTICATED`, `403 FORBIDDEN`, `404 NOT_FOUND`, `409` for state conflicts
  (`STOCK_INSUFFICIENT`, `SALE_ALREADY_VOIDED`, `PURCHASE_ALREADY_RECEIVED`,
  `SALE_VOID_WINDOW_CLOSED`, `RETURN_EXCEEDS_SOLD`, `DISCOUNT_EXCEEDS_SUBTOTAL`,
  `DUPLICATE_SKU`, …), `429 RATE_LIMITED`, `500 INTERNAL`. The full enum lives in the
  spec under `components.schemas.ErrorCode`; adding a code means adding it there.
- **Validation and conflict vocabulary** (O-12): `details.fields` maps field → one of `required`, `invalid`, `too_short`, `too_long`; a uniqueness violation is `409 CONFLICT` with `details.field` naming the field (`username`, `phone`, `name`). Clients translate these words; nothing else is used.
- **List vs get asymmetry**: `GET /products` returns all fields except `description` and
  `translations` (to reduce response size); `GET /products/{id}` returns the full schema
  including translations. Same applies to variants, categories and other entities.
  `translations`, like `costPrice`/`costOverride`, is present only for a caller with the
  matching permission (`catalog.write` for translations, `cost.read` for cost fields) —
  a cashier or public caller never receives it, regardless of endpoint.
- **Promo pricing** (Q-05): `promoPrice`, `promoFrom` and `promoTo` (ISO 8601 dates) are
  independent fields on the product; a PATCH may set any subset of them. Sending an
  explicit `null` for any one of the three clears all three together (D-35's
  `ClearPromo`), since a promo without one of its parts is not valid. `promoFrom` must
  not be after `promoTo`; on a partial PATCH naming only one of the pair, the other side
  is checked against the value already stored, not against nothing. Variants have no
  promo fields — promo pricing is product-level only.
- **Role-shaped responses**: `Product` has an optional `costPrice` field and `Variant` an
  optional `costOverride` field (visible only to callers with `cost.read`, not to cashier
  or public). The same schema models all roles; permissions are enforced server-side at
  serialization, not through separate types. `ProductPublic`/`VariantPublic` remain
  separate schemas for Phase 6's public catalogue.
- **Media uploads**: `POST /media` accepts a single file part in a multipart/form-data
  request; the response includes the media id, URLs for derivatives (`_thumb`, `_card`,
  `_full`), and details (O-16). The endpoint returns `429 RATE_LIMITED` when the upload
  admission queue is full (`MEDIA_QUEUE`). **Max 8 images per product** (`image_count >= 8`
  returns `400 VALIDATION_FAILED` with `details.fields.mediaId: invalid`).
- **Soft-deleted vs inactive product visibility**: a soft-deleted product (`deleted_at`
  set) is `404 NOT_FOUND` on `GET /products/{id}` for every role, and never appears in
  `GET /products` for any role. A product with `isActive: false` (not deleted) is a
  separate case: callers with `catalog.write` see it on `GET /products/{id}`, and on
  `GET /products` only when the request passes `includeInactive=true`; a cashier gets
  `404 NOT_FOUND` on `GET /products/{id}` for an inactive product and never sees it
  listed, `includeInactive` or not.
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
| PATCH  | `/attribute-definitions/{id}`       | manager+        |
| GET/POST | `/categories`                     | any / manager+  |
| GET/PATCH/DELETE | `/categories/{id}`        | any / manager+  |
| GET/POST | `/products`                       | any / manager+  |
| GET/PATCH/DELETE | `/products/{id}`          | any / manager+  |
| GET/POST | `/products/{id}/variants`         | any / manager+  |
| PATCH/DELETE | `/variants/{id}`              | manager+        |
| POST   | `/media`                            | manager+ (multipart; returns 429 `RATE_LIMITED` when admission queue is full) |
| POST   | `/products/{id}/images`            | manager+ (max 8 per product; cap → `400 VALIDATION_FAILED`) |
| DELETE | `/products/{id}/images/{imageId}`  | manager+        |
| PATCH  | `/products/{id}/images/order`      | manager+ (reorder all)   |

### Stock, purchases, suppliers (Phase 3)

| Method | Path                            | Role       |
| ------ | ------------------------------- | ---------- |
| GET/POST | `/suppliers`                  | manager+   |
| GET/PATCH/DELETE | `/suppliers/{id}`     | manager+   |
| GET/POST | `/purchases`                  | manager+   |
| GET/PATCH | `/purchases/{id}`            | manager+ (PATCH only while draft) |
| POST   | `/purchases/{id}/receive`       | manager+   |
| POST   | `/purchases/{id}/cancel`        | manager+   |
| GET    | `/stock/levels`                 | any (D-40) |
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
