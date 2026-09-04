# 04 — Data model

PostgreSQL is the system of record (ADR-003). This document is the specification for
`api/db/migrations/`. Column lists are the intended v1; the `db` agent turns them into
migrations phase by phase and updates this doc when the owner changes a decision.

Conventions for every table unless stated: `id uuid primary key` (v7, app-generated),
`shop_id uuid not null references shops(id)`, `created_at timestamptz not null default
now()`, `updated_at timestamptz not null default now()`. Soft delete only where noted
(`deleted_at`). All unique constraints include `shop_id` (ADR-004).

## 1. Tenancy, users, auth (`auth`, `shop`)

**shops** — `id`, `slug text unique`, `name text`, `currency char(3) default 'UZS'`,
`timezone text default 'Asia/Tashkent'`, `default_locale text default 'uz'`,
`allow_negative_stock bool default false`, `update_cost_on_purchase bool default true`,
`next_sale_number bigint default 1`, `ai_daily_token_budget int`.
Exactly one row in MVP. No `shop_id` column on itself.

**users** — `username citext`, `password_hash text`, `full_name`, `phone text`,
`role user_role` (enum `owner|manager|cashier`), `locale`, `is_active bool`,
`last_login_at`. Unique `(shop_id, username)`. Unique `(shop_id, phone)` where not null.

**sessions** — `user_id`, `token_hash bytea unique`, `client session_client` (enum
`web|mobile`), `user_agent`, `ip inet`, `expires_at`, `last_seen_at`, `revoked_at`.

**telegram_accounts** — `user_id unique`, `telegram_user_id bigint unique`,
`telegram_username`, `linked_at`.

**otp_codes** — `user_id`, `purpose otp_purpose` (`password_reset|link_telegram|
confirm_action`), `code_hash bytea`, `expires_at`, `used_at`, `attempts int`.

**locations** — `name`, `kind location_kind` (`store|warehouse`), `is_default bool`,
`is_active`. Unique `(shop_id, name)`.

## 2. Catalogue (`catalog`, `media`)

**units** — `code text` (`pcs`, `kg`, `m`, …), `name_*` via translations, `precision
smallint` (0 for pcs, 3 for kg). Unique `(shop_id, code)`. Seeded per shop.

**categories** — `parent_id`, `slug`, `sort_order`, `is_active`, `image_id`, `deleted_at` (soft delete, O-14).
Unique `(shop_id, slug)`. Depth ≤ 3 enforced in service.

**category_translations** — `category_id`, `locale`, `name`, `description`.
PK `(category_id, locale)`.

**attribute_definitions** — `code` (`size`, `color`), `sort_order`, plus translations.
Unique `(shop_id, code)`. Seeded with size + colour for the clothing shop (Q-03).

**products** — `category_id`, `slug`, `sku text`, `unit_id`, `base_price numeric(14,2)`,
`cost_price numeric(14,2)`, `promo_price numeric(14,2)`, `promo_from`, `promo_to`,
`is_active`, `is_featured`, `has_variants bool`, `deleted_at`.
Unique `(shop_id, slug)`, unique `(shop_id, sku)` where not null.

**product_translations** — `product_id`, `locale`, `name`, `description`. PK
`(product_id, locale)`.

**product_variants** — `product_id`, `sku`, `barcode text`, `attributes jsonb` (e.g.
`{"size":"L","color":"blue"}`), `price_override numeric(14,2)`, `is_active`,
`deleted_at`. Unique `(shop_id, sku)` where not null, unique `(shop_id, barcode)` where
not null, unique `(product_id, attributes)`. **Every product has at least one variant**
— a product without options gets one variant with `attributes = '{}'`. Stock and sales
always reference a variant, never a product.

**media_files** — `storage_key`, `mime`, `size_bytes`, `width`, `height`, `sha256`,
`uploaded_by`. Variants (thumb/card/full) derived by key suffix.

**product_images** — `product_id`, `variant_id null`, `media_id`, `sort_order`,
`is_cover`. Unique `(product_id, media_id)`. At most one cover per product (partial unique index); removing or un-flagging the only cover promotes the next image by `sort_order` (D-51).

## 3. Stock (`stock`) — correctness-critical

**stock_movements** (append-only; no `updated_at`) — `variant_id`, `location_id`,
`kind stock_movement_kind` (`purchase_in|sale_out|sale_void_in|return_in|adjustment|
transfer_out|transfer_in`), `qty numeric(12,3)` (signed: positive in, negative out),
`unit_cost numeric(14,2)`, `ref_type text`, `ref_id uuid`, `reason text`, `adjustment_reason adjustment_reason null`,
`created_by uuid`, `created_at`. Index `(shop_id, variant_id, location_id, created_at)`.
**No UPDATE or DELETE is ever issued on this table** — enforce with a trigger that raises. Adjustments carry `adjustment_reason` (enum `count_correction`, `damaged`, `lost`, `found`, `other`; D-46, not null iff kind = `adjustment`); the existing `reason text` column holds the optional free-text note.

**stock_levels** — `variant_id`, `location_id`, `qty numeric(12,3) not null default 0`,
`updated_at`. PK `(shop_id, variant_id, location_id)`. Maintained only by
`stock.Service.Move` inside the movement's transaction with `FOR UPDATE`. Rebuildable:
`savdo stock rebuild`.

**Rules (D-41, D-42, D-44):** a level never goes below zero unless `shops.allow_negative_stock` is on — otherwise any movement that would do so fails with `STOCK_INSUFFICIENT` inside `stock.Service.Move` (D-41, D-48). When `shops.update_cost_on_purchase` is on, receiving a purchase sets each received variant's `cost_override` to the line's `unit_cost` (D-42, D-48). Low stock: `shops.low_stock_threshold` default with optional `products.low_stock_threshold` override; a variant is low when its total quantity across locations is at or below the effective threshold (D-44). Only variants stocked at least once on active products and variants count (D-50).

**purchases** — `supplier_id`, `location_id`, `number text`, `status purchase_status`
(`draft|received|cancelled`), `received_at`, `note`, `total_cost numeric(14,2)`,
`created_by`, `supplier_invoice_no text null`. Unique `(shop_id, number)`. `number` is system-generated per shop (`P-000001` style, from `shops.next_purchase_number` under row lock; D-45).

**purchase_items** — `purchase_id`, `variant_id`, `qty numeric(12,3)`, `unit_cost`.

Receiving a purchase (`draft → received`) writes one `purchase_in` movement per item in
the same transaction. A received purchase is immutable; cancel writes reversing
movements. (kind `purchase_in`, negative qty, `ref_type = purchase_cancel`, D-51). A cancelled purchase keeps its `total_cost`; purchase reports must filter `status = received`. `line_total` is stored per item but not returned by the API; clients reconcile `totalCost` against the stored rounding (2 dp, half-up).

**idempotency_keys** — `key text`, `request_hash text`, `response_status int`, `response_body jsonb`, `created_at`. PK `(shop_id, key)`. Backs the `Idempotency-Key` header (`05-API.md` § Conventions) for purchase receive and stock adjustments in Phase 3 and sales in Phase 4; a replay with the same key and hash returns the stored response, a different hash returns `409 IDEMPOTENCY_KEY_REUSED`. Rows older than 24 h may be pruned.

## 4. Sales (`sales`) — correctness-critical

**sales** — `number bigint` (per shop, from `shops.next_sale_number` under row lock),
`kind sale_kind` (`sale|return`), `status sale_status` (`completed|voided`),
`location_id`, `customer_id null`, `cashier_id`, `original_sale_id null` (for returns),
`subtotal numeric(14,2)`, `discount_amount numeric(14,2)`, `discount_reason`,
`total numeric(14,2)`, `note`, `completed_at`, `voided_at`, `voided_by`, `void_reason`.
Unique `(shop_id, number)`. **Never updated after insert except the void columns.**

**sale_items** — `sale_id`, `variant_id`, `qty numeric(12,3)`, `unit_price
numeric(14,2)` (price at sale time, after promo), `unit_cost numeric(14,2)` (frozen for
margin reports; **never returned to cashier or public**), `line_total`.

**sale_payments** — `sale_id`, `method payment_method` (`cash|card|transfer`),
`amount numeric(14,2)`. One row per sale in MVP (Q-07); the table shape allows splits.

**discounts** — promotions beyond per-product promo price: `name`, `kind`
(`percent|fixed`), `value`, `applies_to` (`sale|category|product`), `target_id`,
`starts_at`, `ends_at`, `is_active`. Scope per Q-05.

## 5. CRM (`crm`)

**customers** — `full_name`, `phone`, `telegram_username`, `note`, `tags text[]`,
`deleted_at`. Unique `(shop_id, phone)` where not null. Purchase history is a query on
`sales`. **No balance column in MVP** (D-14).

**suppliers** — `name`, `contact_name`, `phone`, `telegram_username`, `note`,
`deleted_at`. Unique `(shop_id, name)`.

## 6. Content and bot (`content`, `bot`)

**content_blocks** — `key text` (`hero|about|hours|contacts|social|seo`), `locale`,
`data jsonb`, `updated_by`. PK `(shop_id, key, locale)`. Shapes validated against
JSON Schemas defined in the OpenAPI contract.

**bot_conversations** — `telegram_chat_id bigint`, `telegram_user_id`, `customer_id
null`, `mode` (`customer`), `message_count`, `last_message_at`. Unique `(shop_id,
telegram_chat_id)`.

**bot_messages** — `conversation_id`, `role` (`user|assistant|tool`), `content text`,
`tool_calls jsonb`, `provider`, `model`, `input_tokens`, `output_tokens`, `latency_ms`,
`created_at`. Append-only.

**audit_log** — `actor_id`, `action text`, `entity_type`, `entity_id`, `before jsonb`,
`after jsonb`, `created_at`. Written by services for: price changes, stock adjustments,
voids, role changes, settings changes (created in Phase 3, D-47).

## 7. Permissions (ADR-010)

| Capability                              | owner | manager | cashier | public |
| --------------------------------------- | :---: | :-----: | :-----: | :----: |
| Read products, variants, prices         |   ✓   |    ✓    |    ✓    | ✓ (active only) |
| See `cost_price`, `unit_cost`, margins  |   ✓   |    ✓    |    ✗    |   ✗    |
| See stock quantities                    |   ✓   |    ✓    |  ✓ (D-40) | ✗ (availability only) |
| Create/edit products, categories, media |   ✓   |    ✓    |    ✗    |   ✗    |
| Purchases, adjustments, transfers       |   ✓   |    ✓    |    ✗    |   ✗    |
| Create sale, attach customer            |   ✓   |    ✓    |    ✓    |   ✗    |
| Void sale / return                      |   ✓   |    ✓    |    ✗    |   ✗    |
| Customers CRUD                          |   ✓   |    ✓    |  ✓ (create/read) | ✗ |
| Suppliers CRUD                          |   ✓   |    ✓    |    ✗    |   ✗    |
| Discounts/promos                        |   ✓   |    ✓    |    ✗    |   ✗    |
| Reports                                 |   ✓   |    ✓    | own-day sales only | ✗ |
| Landing content                         |   ✓   |    ✓    |    ✗    |   ✗    |
| Bot conversations (read)                |   ✓   |    ✓    |    ✗    |   ✗    |
| Staff CRUD, roles, password reset       |   ✓   |    ✗    |    ✗    |   ✗    |
| Shop settings, locations                |   ✓   |    ✗    |    ✗    |   ✗    |

## 8. Rules for agents

1. **`shop_id` on every business table and in every query's WHERE**, bound from the
   auth context (ADR-004). A query without it fails review.
2. **`stock_movements` is append-only; `stock_levels` is written only by
   `stock.Service.Move`** (ADR-006). Add the raise-trigger in the same migration that
   creates the table.
3. **Money `NUMERIC(14,2)`, quantity `NUMERIC(12,3)`.** No `float`, `real`, `double`.
4. **Real enum types.** Adding a value is additive (`ALTER TYPE … ADD VALUE`); removing
   one is destructive and needs owner approval.
5. **Migrations are goose SQL, one concern each, never edited after merge.** Name
   `NNNN_<slug>.sql`, `-- +goose Up` / `-- +goose Down`. Down must be real, not a
   comment, except for data backfills where the owner approved "no down".
6. **sqlc output is committed and must be fresh** — `make verify` runs `sqlc diff`.
7. **Soft delete** categories, products, variants, customers, suppliers (`deleted_at`); hard delete
   join rows (`product_images`). Never hard-delete a variant referenced by a movement
   or a sale item — the FK prevents it; do not weaken the FK.
8. **`sale_items.unit_cost` and `products.cost_price` never appear in a query used by a
   cashier-role or public/bot code path.** Keep separate sqlc queries
   (`…ForStaff` vs `…Public`) rather than filtering in Go.
9. **Every timestamp `timestamptz`, UTC.** Shop timezone is applied in reports only.
10. **Index every FK** and every `(shop_id, <lookup>)` pair a list endpoint filters on.
