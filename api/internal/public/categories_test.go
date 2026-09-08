package public_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestListPublicCategories_activeOnlyWithProductCounts(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	activeCat := seedCategory(ctx, t, q, shopRow.ID, "shirts", "Shirts", true)
	inactiveCat := seedCategory(ctx, t, q, shopRow.ID, "old", "Old", false)

	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "shirt-1", Name: "Shirt 1", BasePrice: "100000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "shirt-2", Name: "Shirt 2", BasePrice: "100000.00", IsActive: false, // inactive: not counted
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &inactiveCat.ID, Slug: "old-1", Name: "Old 1", BasePrice: "100000.00", IsActive: true,
	})

	resp, err := h.ListPublicCategories(ctxWithAcceptLanguage("uz"), gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	list := resp.(gen.ListPublicCategories200JSONResponse)
	if len(list.Items) != 1 {
		t.Fatalf("Items = %+v, want exactly the active category", list.Items)
	}
	if list.Items[0].Slug != "shirts" {
		t.Errorf("Items[0].Slug = %q, want shirts", list.Items[0].Slug)
	}
	if list.Items[0].ProductCount != 1 {
		t.Errorf("Items[0].ProductCount = %d, want 1 (only the active product)", list.Items[0].ProductCount)
	}
	if !list.Items[0].ParentSlug.IsNull() {
		t.Errorf("Items[0].ParentSlug = %+v, want null for a root category", list.Items[0].ParentSlug)
	}
	if !list.Items[0].ParentName.IsNull() {
		t.Errorf("Items[0].ParentName = %+v, want null for a root category", list.Items[0].ParentName)
	}
}

// TestListPublicCategories_parentChildHierarchy pins T7: a parent's
// productCount sums its own direct products AND every active descendant's
// (here "Erkaklar" has none of its own but reports its child's total), a
// child carries its immediate parent's slug/name, and rows sort with each
// parent immediately before its own children.
func TestListPublicCategories_parentChildHierarchy(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	men := seedCategory(ctx, t, q, shopRow.ID, "erkaklar", "Erkaklar", true)
	women := seedCategory(ctx, t, q, shopRow.ID, "ayollar", "Ayollar", true)
	menShirts := seedSubcategory(ctx, t, q, shopRow.ID, men.ID, "erkaklar-koylaklar", "Ko‘ylaklar", true)
	womenDresses := seedSubcategory(ctx, t, q, shopRow.ID, women.ID, "ayollar-koylaklar", "Ko‘ylaklar", true)

	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &menShirts.ID, Slug: "mens-shirt-1", Name: "Mens Shirt 1", BasePrice: "100000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &menShirts.ID, Slug: "mens-shirt-2", Name: "Mens Shirt 2", BasePrice: "100000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &womenDresses.ID, Slug: "womens-dress-1", Name: "Womens Dress 1", BasePrice: "150000.00", IsActive: true,
	})

	resp, err := h.ListPublicCategories(ctxWithAcceptLanguage("uz"), gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	list := resp.(gen.ListPublicCategories200JSONResponse)
	if len(list.Items) != 4 {
		t.Fatalf("Items = %+v, want 4 (2 parents + 2 children)", list.Items)
	}

	bySlug := make(map[string]gen.PublicCategory, len(list.Items))
	order := make([]string, len(list.Items))
	for i, item := range list.Items {
		bySlug[item.Slug] = item
		order[i] = item.Slug
	}

	menRow, womenRow := bySlug["erkaklar"], bySlug["ayollar"]
	if !menRow.ParentSlug.IsNull() {
		t.Errorf("Erkaklar ParentSlug = %+v, want null (root)", menRow.ParentSlug)
	}
	if menRow.ProductCount != 2 {
		t.Errorf("Erkaklar ProductCount = %d, want 2 (its child's products)", menRow.ProductCount)
	}
	if womenRow.ProductCount != 1 {
		t.Errorf("Ayollar ProductCount = %d, want 1 (its child's products)", womenRow.ProductCount)
	}

	menShirtsRow, womenDressesRow := bySlug["erkaklar-koylaklar"], bySlug["ayollar-koylaklar"]
	if menShirtsRow.ParentSlug.MustGet() != "erkaklar" || menShirtsRow.ParentName.MustGet() != "Erkaklar" {
		t.Errorf("erkaklar-koylaklar parent = %+v/%+v, want erkaklar/Erkaklar", menShirtsRow.ParentSlug, menShirtsRow.ParentName)
	}
	if womenDressesRow.ParentSlug.MustGet() != "ayollar" || womenDressesRow.ParentName.MustGet() != "Ayollar" {
		t.Errorf("ayollar-koylaklar parent = %+v/%+v, want ayollar/Ayollar", womenDressesRow.ParentSlug, womenDressesRow.ParentName)
	}
	if menShirtsRow.ProductCount != 2 {
		t.Errorf("erkaklar-koylaklar ProductCount = %d, want 2", menShirtsRow.ProductCount)
	}
	if womenDressesRow.Name != "Ko‘ylaklar" || menShirtsRow.Name != "Ko‘ylaklar" {
		t.Errorf("both children named Ko‘ylaklar, got %q and %q — disambiguated only by parentSlug", menShirtsRow.Name, womenDressesRow.Name)
	}

	// Sort order: each parent immediately before its own children.
	menIdx, menShirtsIdx := indexOf(order, "erkaklar"), indexOf(order, "erkaklar-koylaklar")
	womenIdx, womenDressesIdx := indexOf(order, "ayollar"), indexOf(order, "ayollar-koylaklar")
	if menShirtsIdx != menIdx+1 {
		t.Errorf("order = %v, want erkaklar-koylaklar immediately after erkaklar", order)
	}
	if womenDressesIdx != womenIdx+1 {
		t.Errorf("order = %v, want ayollar-koylaklar immediately after ayollar", order)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestListPublicCategories_parentNameFallsBackToUz pins T7's "same
// fallback as name" for parentName: the parent has only a uz translation,
// a ru request still resolves both the child's own name and its
// parentName to the uz value (ADR-012/D-104-style fallback).
func TestListPublicCategories_parentNameFallsBackToUz(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	parent := seedCategory(ctx, t, q, shopRow.ID, "erkaklar", "Erkaklar", true)
	seedSubcategory(ctx, t, q, shopRow.ID, parent.ID, "erkaklar-koylaklar", "Ko‘ylaklar", true)

	resp, err := h.ListPublicCategories(ctxWithAcceptLanguage("ru"), gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	list := resp.(gen.ListPublicCategories200JSONResponse)

	var child gen.PublicCategory
	for _, item := range list.Items {
		if item.Slug == "erkaklar-koylaklar" {
			child = item
		}
	}
	if child.ParentName.MustGet() != "Erkaklar" {
		t.Errorf("ParentName = %+v, want the uz fallback \"Erkaklar\"", child.ParentName)
	}
}

// TestListPublicCategories_inactiveChildExcludedFromParentCountAndListing
// pins T7: an inactive child neither appears as its own row nor
// contributes to its (active) parent's productCount.
func TestListPublicCategories_inactiveChildExcludedFromParentCountAndListing(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	parent := seedCategory(ctx, t, q, shopRow.ID, "erkaklar", "Erkaklar", true)
	activeChild := seedSubcategory(ctx, t, q, shopRow.ID, parent.ID, "erkaklar-koylaklar", "Ko‘ylaklar", true)
	inactiveChild := seedSubcategory(ctx, t, q, shopRow.ID, parent.ID, "erkaklar-shim", "Shim", false)

	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeChild.ID, Slug: "active-child-product", Name: "Active Child Product", BasePrice: "100000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &inactiveChild.ID, Slug: "inactive-child-product", Name: "Inactive Child Product", BasePrice: "100000.00", IsActive: true,
	})

	resp, err := h.ListPublicCategories(ctxWithAcceptLanguage("uz"), gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	list := resp.(gen.ListPublicCategories200JSONResponse)
	if len(list.Items) != 2 {
		t.Fatalf("Items = %+v, want 2 (erkaklar + its active child only)", list.Items)
	}
	for _, item := range list.Items {
		if item.Slug == "erkaklar-shim" {
			t.Fatalf("Items = %+v, want the inactive child excluded", list.Items)
		}
		if item.Slug == "erkaklar" && item.ProductCount != 1 {
			t.Errorf("erkaklar ProductCount = %d, want 1 (only the active child's product)", item.ProductCount)
		}
	}
}
