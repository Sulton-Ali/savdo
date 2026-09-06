// `/sale/{id}` — the Quick sale tab's own read-only sale detail (post-Pay
// round trip, a customer's purchase history, or the "view original" link
// on a return). Same component as `/sales/{id}`
// (`(tabs)/sales/[id].tsx`) — see `features/sales/SaleDetail.tsx`'s own
// doc comment for why this is a thin re-export rather than two copies.
export { default } from "@/features/sales/SaleDetail";
