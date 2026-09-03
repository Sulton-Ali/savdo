package auth

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Permission is a machine-readable capability string (ADR-010). GetMe
// hands the authenticated user's set to the UI so it can show/hide
// actions; the API itself remains the enforcement point via Require — the
// UI hiding a button is a courtesy, not a boundary.
type Permission string

// The permission vocabulary, transcribed from docs/04-DATA-MODEL.md § 7
// (the permission matrix). A capability the matrix grants to every
// authenticated role uniformly (reading products/prices, seeing stock
// quantities) has no Permission here — there is nothing to gate once a
// request is authenticated at all; Require only exists for capabilities at
// least one staff role lacks.
const (
	// PermStaffManage gates staff CRUD, role assignment and password
	// resets. Owner only.
	PermStaffManage Permission = "staff.manage"

	// PermShopSettings gates PATCH /shop. Owner only.
	PermShopSettings Permission = "shop.settings"

	// PermLocationsManage gates creating/editing locations. Owner only.
	PermLocationsManage Permission = "locations.manage"

	// PermCatalogWrite gates creating/editing products, categories and
	// media. Owner and manager.
	PermCatalogWrite Permission = "catalog.write"

	// PermStockWrite gates purchases, adjustments and transfers. Owner
	// and manager.
	PermStockWrite Permission = "stock.write"

	// PermSalesCreate gates creating a sale and attaching a customer to
	// one. Owner, manager and cashier.
	PermSalesCreate Permission = "sales.create"

	// PermSalesVoid gates voiding a sale or recording a return. Owner and
	// manager.
	PermSalesVoid Permission = "sales.void"

	// PermCustomersWrite gates full customer CRUD (update/delete). Owner
	// and manager — a cashier's narrower create/read ability is a route
	// (not a permission-string) concern for the customers module (Phase
	// 4) to enforce, since it is a subset rather than an on/off gate.
	PermCustomersWrite Permission = "customers.write"

	// PermSuppliersManage gates supplier CRUD. Owner and manager.
	PermSuppliersManage Permission = "suppliers.manage"

	// PermDiscountsManage gates discount/promo CRUD. Owner and manager.
	PermDiscountsManage Permission = "discounts.manage"

	// PermReportsRead gates full sales/stock reports. Owner and manager.
	PermReportsRead Permission = "reports.read"

	// PermReportsOwnDay gates a cashier's narrower view: their own day's
	// sales only. Cashier only — owner/manager already have the broader
	// PermReportsRead.
	PermReportsOwnDay Permission = "reports.own_day"

	// PermContentManage gates editing landing content blocks. Owner and
	// manager.
	PermContentManage Permission = "content.manage"

	// PermBotRead gates reading bot conversations. Owner and manager.
	PermBotRead Permission = "bot.read"

	// PermCostRead gates seeing cost_price, unit_cost and margins (hard
	// rule 5 / ADR-010). Owner and manager.
	PermCostRead Permission = "cost.read"
)

// rolePermissions is the matrix itself: docs/04-DATA-MODEL.md § 7, one row
// per role. Keep this the single place the matrix is encoded — Permissions
// and Require both read it, so a matrix change is a one-line diff here.
var rolePermissions = map[db.UserRole][]Permission{
	db.UserRoleOwner: {
		PermStaffManage, PermShopSettings, PermLocationsManage,
		PermCatalogWrite, PermStockWrite, PermSalesCreate, PermSalesVoid,
		PermCustomersWrite, PermSuppliersManage, PermDiscountsManage,
		PermReportsRead, PermContentManage, PermBotRead, PermCostRead,
	},
	db.UserRoleManager: {
		PermCatalogWrite, PermStockWrite, PermSalesCreate, PermSalesVoid,
		PermCustomersWrite, PermSuppliersManage, PermDiscountsManage,
		PermReportsRead, PermContentManage, PermBotRead, PermCostRead,
	},
	db.UserRoleCashier: {
		PermSalesCreate, PermReportsOwnDay,
	},
}

// Permissions returns role's capability strings, for GetMe to hand the
// client (ADR-010: "machine-readable capability strings the UI uses to
// show/hide actions"). An unrecognized role — never expected, since the
// database enum only has three values — returns an empty, non-nil slice
// rather than nil, so callers can range over the result unconditionally.
func Permissions(role db.UserRole) []string {
	perms := rolePermissions[role]
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// Require reports whether the request authenticated in ctx (via
// FromContext) has perm, returning an *apierr.Error the caller can return
// unwrapped otherwise: Forbidden if the role lacks it, Unauthenticated if
// ctx carries no auth Context at all (a caller invoking Require on a path
// Middleware never authenticated — a bug in that caller, not a client
// error, but still safer to answer 401 than to panic on a zero-value
// Context).
func Require(ctx context.Context, perm Permission) error {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return apierr.Unauthenticated()
	}

	for _, p := range rolePermissions[authCtx.Role] {
		if p == perm {
			return nil
		}
	}
	return apierr.Forbidden()
}
