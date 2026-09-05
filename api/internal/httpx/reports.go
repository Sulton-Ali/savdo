package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The two methods below satisfy gen.StrictServerInterface's sales-report
// operations by forwarding to server.reports (*reports.Handler) — named,
// not embedded, for the same reason server.shop/server.catalog/server.crm
// are (healthz.go's doc comment on the server struct). Replaces what
// unimplemented.go stubbed out before the reports module existed
// (docs/06-ROADMAP.md Phase 4 T5).

// GetSalesSummaryReport reports sales totals for a period (cashier+, D-55).
func (s server) GetSalesSummaryReport(ctx context.Context, req gen.GetSalesSummaryReportRequestObject) (gen.GetSalesSummaryReportResponseObject, error) {
	return s.reports.GetSalesSummaryReport(ctx, req)
}

// ListSalesByProduct reports sales by product for a period (manager+, D-55).
func (s server) ListSalesByProduct(ctx context.Context, req gen.ListSalesByProductRequestObject) (gen.ListSalesByProductResponseObject, error) {
	return s.reports.ListSalesByProduct(ctx, req)
}
