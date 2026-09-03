# 01 — Overview

## Pitch

Savdo is a light ERP + CRM for small shops in Uzbekistan. A seller manages products,
stock across locations, purchases from suppliers, quick sales and customers — from a
web admin or a phone. The shop gets a modern public landing that shows its catalogue,
address and hours, and a Telegram bot that answers customers' questions using the shop's
own data.

It deliberately takes the small useful core of ERP (catalogue, inventory ledger,
purchases, sales) and CRM (customers, suppliers, promotions) and leaves out everything a
five-person shop never uses.

**First client:** the owner's family clothing shop. Sizes, colours, seasons, several
racks and a storeroom. If the product works for them, it works for the segment.

## Personas

| Persona      | Who                                                       | Wants                                                                                                                | Uses                          |
| ------------ | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| **Owner**    | Runs the shop, buys stock, decides prices                 | Know what is in stock and where, what sold today, what to reorder, margins; manage staff; keep the landing current    | Admin web + mobile            |
| **Manager**  | Trusted staff, runs the shop when the owner is away       | Everything the owner does except staff management and shop settings                                                  | Admin web + mobile            |
| **Cashier**  | Sells at the counter                                      | Make a sale in under 30 seconds on a phone, find a product fast, look up a customer. Must not see cost price/margins  | Mobile (mainly)               |
| **Customer** | Walks in, or finds the shop online / via Telegram         | See what the shop sells, prices, whether a size is available, where the shop is, when it is open; ask a quick question | Landing + Telegram bot        |

## User stories (MVP)

**Catalogue**

- As an owner I create categories and products with photos, prices (sale and cost),
  units, and variants (size/colour) so my stock is described exactly.
- As an owner I translate product names/descriptions to uz/ru/en so the landing serves
  every customer.

**Stock**

- As an owner I receive a purchase from a supplier into a location and stock levels
  update per variant.
- As a manager I move stock between the shop floor and the storeroom.
- As a manager I adjust stock after a count and the reason is recorded.
- As an owner I see low-stock variants and the full movement history of any variant.

**Sales**

- As a cashier I make a quick sale: pick variants, quantities, optional discount,
  payment method; stock decreases; I see the total.
- As a manager I void a wrong sale or record a return; stock comes back.
- As an owner I see today's and this period's sales totals and top products.

**CRM**

- As a cashier I attach a customer (by phone) to a sale and see their history.
- As an owner I keep a supplier directory with contact details and purchase history.
- As an owner I set a promo price on a product for a date range, and it shows on the
  landing and applies at the counter.

**Landing**

- As a customer I browse categories and products, see prices, photos and whether a
  variant is available, in my language.
- As a customer I find the address, hours, phone and Telegram link.

**Bot**

- As a customer I ask the shop's Telegram bot "do you have this jacket in L?" and get an
  answer grounded in real stock availability, hours, and contacts — never invented.
- As an owner I see what customers asked so I learn what they want.

**Admin & auth**

- As an owner I log in with username + password (later also via Telegram), create staff
  accounts and give them roles.
- As a staff member I use the same features on my phone as on the web.

## Landing (draft sections — Q-08)

Hero with shop name and tagline · category grid · featured/promo products · product
pages with variant availability · about the shop · address with map link · hours ·
contacts (phone, Telegram) · language switcher (uz/ru/en). SEO: SSR, per-locale meta,
Open Graph images, sitemap.

## Non-goals (MVP)

- Online cart, checkout or payment (D-03)
- Multi-tenant registration, billing, plans (D-02)
- Debt/credit ledger (D-14), loyalty, coupons
- Barcode scanning (D-05), receipt printing, fiscal integration
- Accounting, tax reports, payroll
- Offline mode on mobile
- iOS build
- Any LLM feature outside the Telegram bot

Anything here needs an owner decision before an agent touches it.

## Definition of done (product)

The MVP is done when the family clothing shop runs a full week on Savdo:

1. Its full catalogue with variants and photos is in the system and on the landing.
2. Every incoming delivery is received as a purchase and every sale goes through quick
   sale, on phone or web, by owner and staff.
3. Stock levels on screen match a physical count at week's end, with every difference
   explained by a recorded adjustment.
4. The landing is live on a real domain in three languages.
5. The bot answers at least ten real customer questions correctly without exposing
   internal data.
6. A database restore drill has been performed on the VPS.
