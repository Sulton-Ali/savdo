// `/sales/{id}` — the Sales list tab's own read-only sale detail, pushed
// from `sales/index.tsx` so the native back button and Android back return
// here. Same component as `/sale/{id}` (`(tabs)/sale/[id].tsx`) — see
// `features/sales/SaleDetail.tsx`'s own doc comment for why this is a thin
// re-export rather than two copies.
export { default } from "@/features/sales/SaleDetail";
