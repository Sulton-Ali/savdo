package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The ten methods below satisfy gen.StrictServerInterface's suppliers and
// customers operations by forwarding to server.crm (*crm.Handler) —
// named, not embedded, for the same reason server.shop/server.catalog are
// (healthz.go's doc comment on the server struct). Replaces what
// unimplemented.go stubbed out before the crm module existed
// (docs/06-ROADMAP.md Phase 3 T4 for suppliers, Phase 4 T2 for
// customers).

// ListSuppliers lists the shop's suppliers.
func (s server) ListSuppliers(ctx context.Context, req gen.ListSuppliersRequestObject) (gen.ListSuppliersResponseObject, error) {
	return s.crm.ListSuppliers(ctx, req)
}

// CreateSupplier creates a supplier.
func (s server) CreateSupplier(ctx context.Context, req gen.CreateSupplierRequestObject) (gen.CreateSupplierResponseObject, error) {
	return s.crm.CreateSupplier(ctx, req)
}

// GetSupplier gets a supplier.
func (s server) GetSupplier(ctx context.Context, req gen.GetSupplierRequestObject) (gen.GetSupplierResponseObject, error) {
	return s.crm.GetSupplier(ctx, req)
}

// UpdateSupplier updates a supplier.
func (s server) UpdateSupplier(ctx context.Context, req gen.UpdateSupplierRequestObject) (gen.UpdateSupplierResponseObject, error) {
	return s.crm.UpdateSupplier(ctx, req)
}

// DeleteSupplier soft-deletes a supplier.
func (s server) DeleteSupplier(ctx context.Context, req gen.DeleteSupplierRequestObject) (gen.DeleteSupplierResponseObject, error) {
	return s.crm.DeleteSupplier(ctx, req)
}

// ListCustomers lists the shop's customers.
func (s server) ListCustomers(ctx context.Context, req gen.ListCustomersRequestObject) (gen.ListCustomersResponseObject, error) {
	return s.crm.ListCustomers(ctx, req)
}

// CreateCustomer creates a customer.
func (s server) CreateCustomer(ctx context.Context, req gen.CreateCustomerRequestObject) (gen.CreateCustomerResponseObject, error) {
	return s.crm.CreateCustomer(ctx, req)
}

// GetCustomer gets a customer.
func (s server) GetCustomer(ctx context.Context, req gen.GetCustomerRequestObject) (gen.GetCustomerResponseObject, error) {
	return s.crm.GetCustomer(ctx, req)
}

// UpdateCustomer updates a customer.
func (s server) UpdateCustomer(ctx context.Context, req gen.UpdateCustomerRequestObject) (gen.UpdateCustomerResponseObject, error) {
	return s.crm.UpdateCustomer(ctx, req)
}

// DeleteCustomer soft-deletes a customer.
func (s server) DeleteCustomer(ctx context.Context, req gen.DeleteCustomerRequestObject) (gen.DeleteCustomerResponseObject, error) {
	return s.crm.DeleteCustomer(ctx, req)
}
