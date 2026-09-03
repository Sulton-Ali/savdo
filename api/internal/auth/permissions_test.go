package auth

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func TestPermissionsPerRole(t *testing.T) {
	tests := []struct {
		role db.UserRole
		want []Permission
		lack []Permission
	}{
		{
			role: db.UserRoleOwner,
			want: []Permission{PermStaffManage, PermShopSettings, PermLocationsManage, PermCostRead},
		},
		{
			role: db.UserRoleManager,
			want: []Permission{PermCatalogWrite, PermStockWrite, PermCostRead, PermSalesVoid},
			lack: []Permission{PermStaffManage, PermShopSettings, PermLocationsManage},
		},
		{
			role: db.UserRoleCashier,
			want: []Permission{PermSalesCreate, PermReportsOwnDay},
			lack: []Permission{PermCostRead, PermCatalogWrite, PermSalesVoid, PermReportsRead, PermCustomersWrite},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := Permissions(tt.role)
			for _, p := range tt.want {
				if !slices.Contains(got, string(p)) {
					t.Errorf("Permissions(%s) = %v, want it to contain %s", tt.role, got, p)
				}
			}
			for _, p := range tt.lack {
				if slices.Contains(got, string(p)) {
					t.Errorf("Permissions(%s) = %v, want it to NOT contain %s", tt.role, got, p)
				}
			}
		})
	}
}

func TestPermissionsUnknownRoleIsEmptyNotNil(t *testing.T) {
	got := Permissions(db.UserRole("bogus"))
	if got == nil {
		t.Fatal("Permissions(unknown role) = nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("Permissions(unknown role) = %v, want empty", got)
	}
}

func TestRequireGrantsAndDenies(t *testing.T) {
	ownerCtx := WithContext(context.Background(), Context{UserID: uuid.New(), Role: db.UserRoleOwner})
	cashierCtx := WithContext(context.Background(), Context{UserID: uuid.New(), Role: db.UserRoleCashier})

	if err := Require(ownerCtx, PermStaffManage); err != nil {
		t.Fatalf("Require(owner, staff.manage) error = %v, want nil", err)
	}

	err := Require(cashierCtx, PermStaffManage)
	if err == nil {
		t.Fatal("Require(cashier, staff.manage) error = nil, want Forbidden")
	}
	if got, want := errStatus(t, err), 403; got != want {
		t.Fatalf("Require(cashier, staff.manage) status = %d, want %d", got, want)
	}
}

func TestRequireWithNoAuthContextIsUnauthenticated(t *testing.T) {
	err := Require(context.Background(), PermSalesCreate)
	if err == nil {
		t.Fatal("Require() with no auth Context error = nil, want Unauthenticated")
	}
	if got, want := errStatus(t, err), 401; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
