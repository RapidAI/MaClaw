package httpapi

// Tests for the administrator-curated AI Expert Market category taxonomy: the
// seeded defaults, the CRUD + reorder endpoints, the share-time validation,
// the per-listing admin override, and the HA full-list snapshot convergence.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func expertMarketCategoryNames(t *testing.T, h *SkillMarketHandlers) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/expert-market/categories", nil)
	rec := httptest.NewRecorder()
	h.ListExpertMarketCategories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public categories=%d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Categories []expertMarketCategory `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(payload.Categories))
	for _, item := range payload.Categories {
		names = append(names, item.Name)
	}
	return names
}

func TestExpertMarketCategoriesSeededDefaults(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	names := expertMarketCategoryNames(t, h)
	want := []string{"科研", "办公", "其它"}
	if len(names) != len(want) {
		t.Fatalf("seeded categories=%v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("seeded categories=%v, want %v", names, want)
		}
	}
	// The seed marker must survive a re-ensure so an administrator-deleted
	// taxonomy is not resurrected on restart.
	h.ensureExpertMarketSchema()
	if _, err := h.store.DB().Exec(`DELETE FROM sm_expert_market_categories`); err != nil {
		t.Fatal(err)
	}
	h.ensureExpertMarketSchema()
	if again := expertMarketCategoryNames(t, h); len(again) != 0 {
		t.Fatalf("categories after delete+re-ensure=%v, want empty", again)
	}
}

func TestExpertMarketCategoriesAdminCRUDReorderAndRenameFollowsListings(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	h.ensureExpertMarketSchema()

	// Create: duplicate and invalid names are rejected.
	create := func(name string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/categories", strings.NewReader(`{"name":`+jsonQuote(name)+`}`))
		h.AdminCreateExpertMarketCategory(rec, req)
		return rec
	}
	if rec := create("开发"); rec.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := create("开发"); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := create("   "); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank create=%d %s", rec.Code, rec.Body.String())
	}
	if rec := create(strings.Repeat("长", expertMarketCategoryMaxRunes+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong create=%d %s", rec.Code, rec.Body.String())
	}

	// A listing carrying the old name follows a rename.
	now := "2026-11-19T00:00:00Z"
	_, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?,?)`,
		"cat-rename-listing", "owner-1", "owner@example.test", "pkgexp-cat-rename", "Rename target", "", "", "1", 0, "public", "listed", "", 0, now, now, "科研")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := h.expertMarketCategoryRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var researchID string
	for _, item := range rows {
		if item.Name == "科研" {
			researchID = item.ID
		}
	}
	if researchID == "" {
		t.Fatalf("seeded 科研 missing: %+v", rows)
	}
	rename := httptest.NewRecorder()
	renameReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/expert-market/categories/"+researchID, strings.NewReader(`{"name":"学术科研"}`))
	renameReq.SetPathValue("id", researchID)
	h.AdminUpdateExpertMarketCategory(rename, renameReq)
	if rename.Code != http.StatusOK {
		t.Fatalf("rename=%d %s", rename.Code, rename.Body.String())
	}
	var listingCategory string
	if err := h.store.DB().QueryRowContext(ctx, `SELECT category FROM sm_expert_market_listings WHERE id='cat-rename-listing'`).Scan(&listingCategory); err != nil {
		t.Fatal(err)
	}
	if listingCategory != "学术科研" {
		t.Fatalf("listing category after rename=%q", listingCategory)
	}
	for _, name := range expertMarketCategoryNames(t, h) {
		if name == "科研" {
			t.Fatalf("old name still in taxonomy: %v", expertMarketCategoryNames(t, h))
		}
	}

	// Reorder: send the full ordered list; the response is the canonical order.
	// Rows are re-read after the rename so the reordered list uses live ids.
	rows, err = h.expertMarketCategoryRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reordered := make([]string, 0)
	for _, name := range []string{"学术科研", "开发", "办公", "其它"} {
		for _, item := range rows {
			if item.Name == name {
				reordered = append(reordered, item.ID)
			}
		}
	}
	if len(reordered) != len(rows) {
		t.Fatalf("reorder list incomplete: %v of %v", reordered, rows)
	}
	payload, err := json.Marshal(map[string]any{"ids": reordered})
	if err != nil {
		t.Fatal(err)
	}
	reorder := httptest.NewRecorder()
	reorderReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/categories/reorder", bytes.NewReader(payload))
	h.AdminReorderExpertMarketCategories(reorder, reorderReq)
	if reorder.Code != http.StatusOK {
		t.Fatalf("reorder=%d %s", reorder.Code, reorder.Body.String())
	}
	names := expertMarketCategoryNames(t, h)
	if len(names) != 4 || names[0] != "学术科研" || names[1] != "开发" || names[2] != "办公" || names[3] != "其它" {
		t.Fatalf("order after reorder=%v", names)
	}

	// Delete: the name leaves the taxonomy and the listing keeps its text.
	var deleteID string
	for _, item := range rows {
		if item.Name == "学术科研" {
			deleteID = item.ID
		}
	}
	if deleteID == "" {
		t.Fatalf("renamed category missing from rows: %+v", rows)
	}
	del := httptest.NewRecorder()
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/categories/"+deleteID, nil)
	delReq.SetPathValue("id", deleteID)
	h.AdminDeleteExpertMarketCategory(del, delReq)
	if del.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", del.Code, del.Body.String())
	}
	if err := h.store.DB().QueryRowContext(ctx, `SELECT category FROM sm_expert_market_listings WHERE id='cat-rename-listing'`).Scan(&listingCategory); err != nil {
		t.Fatal(err)
	}
	if listingCategory != "学术科研" {
		t.Fatalf("delete must not rewrite listings, got %q", listingCategory)
	}
	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/expert-market/categories/"+deleteID, nil)
	missingReq.SetPathValue("id", deleteID)
	h.AdminDeleteExpertMarketCategory(missing, missingReq)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("second delete=%d", missing.Code)
	}
}

func TestExpertMarketSubmitValidatesCategoryAgainstTaxonomy(t *testing.T) {
	h, users, auth, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	seller, err := users.EnsureAccountWithID(ctx, "category-seller", "category-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, _ := auth.CreateSessionForUser(ctx, seller.ID, seller.Email)

	submit := func(category string) (*httptest.ResponseRecorder, string) {
		var form bytes.Buffer
		mw := multipart.NewWriter(&form)
		_ = mw.WriteField("version", "1.0.0")
		_ = mw.WriteField("price", "0")
		_ = mw.WriteField("visibility", "public")
		if category != "" {
			_ = mw.WriteField("category", category)
		}
		part, ferr := mw.CreateFormFile("package", "expert.zip")
		if ferr != nil {
			t.Fatal(ferr)
		}
		_, _ = part.Write(expertMarketArchive(t, "pkgexp-cat-"+fmtInt(int64(len(category))), "Category Expert"))
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/expert-market/experts", &form)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		h.SubmitExpertMarketListing(rec, req)
		return rec, mw.FormDataContentType()
	}

	rec, _ := submit("科研")
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid category submit=%d %s", rec.Code, rec.Body.String())
	}
	var created expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Category != "科研" {
		t.Fatalf("created listing category=%q", created.Category)
	}
	var stored string
	if err := h.store.DB().QueryRowContext(ctx, `SELECT category FROM sm_expert_market_listings WHERE id=?`, created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "科研" {
		t.Fatalf("stored category=%q", stored)
	}

	rec, _ = submit("不存在的分类")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown category submit=%d %s", rec.Code, rec.Body.String())
	}

	// An empty category stays legal for clients that predate the picker.
	rec, _ = submit("")
	if rec.Code != http.StatusCreated {
		t.Fatalf("empty category submit=%d %s", rec.Code, rec.Body.String())
	}
	var untagged expertMarketAdminListing
	if err := json.Unmarshal(rec.Body.Bytes(), &untagged); err != nil {
		t.Fatal(err)
	}
	if untagged.Category != "" {
		t.Fatalf("untagged listing category=%q", untagged.Category)
	}
}

func TestExpertMarketAdminSetListingCategory(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	ctx := context.Background()
	h.ensureExpertMarketSchema()
	now := "2026-11-19T00:00:00Z"
	_, err := h.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`, platform_distribution) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,'',?,?,?,0)`,
		"cat-set-listing", "owner-1", "owner@example.test", "pkgexp-cat-set", "Set target", "", "", "1", 0, "public", "pending_review", "", 0, now, now, "")
	if err != nil {
		t.Fatal(err)
	}
	set := httptest.NewRecorder()
	setReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/cat-set-listing/category", strings.NewReader(`{"category":"办公"}`))
	setReq.SetPathValue("id", "cat-set-listing")
	h.AdminSetExpertMarketListingCategory(set, setReq)
	if set.Code != http.StatusOK {
		t.Fatalf("set=%d %s", set.Code, set.Body.String())
	}
	var stored string
	if err := h.store.DB().QueryRowContext(ctx, `SELECT category FROM sm_expert_market_listings WHERE id='cat-set-listing'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "办公" {
		t.Fatalf("stored category=%q", stored)
	}
	var events int
	if err := h.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_expert_market_events WHERE listing_id='cat-set-listing' AND action='category_set'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("category_set events=%d", events)
	}
	invalid := httptest.NewRecorder()
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/cat-set-listing/category", strings.NewReader(`{"category":"乱写的"}`))
	invalidReq.SetPathValue("id", "cat-set-listing")
	h.AdminSetExpertMarketListingCategory(invalid, invalidReq)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid set=%d %s", invalid.Code, invalid.Body.String())
	}
	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/expert-market/experts/cat-missing/category", strings.NewReader(`{"category":"办公"}`))
	missingReq.SetPathValue("id", "cat-missing")
	h.AdminSetExpertMarketListingCategory(missing, missingReq)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing listing set=%d", missing.Code)
	}
}

func TestExpertMarketCategoriesSnapshotConvergesPeers(t *testing.T) {
	h, _, _, _ := newExpertMarketTestHandler(t)
	h.ensureExpertMarketSchema()
	snapshot, err := json.Marshal(expertMarketCategoriesSnapshot{
		ID: "categories",
		Categories: []expertMarketCategory{
			{ID: "expert_cat_a", Name: "科研", SortOrder: 2},
			{ID: "expert_cat_b", Name: "办公", SortOrder: 1},
			{ID: "expert_cat_bad", Name: "   ", SortOrder: 3},
			{ID: "expert_cat_dup", Name: "科研", SortOrder: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ApplyExpertMarketCategoriesSnapshot(context.Background(), "upsert", snapshot); err != nil {
		t.Fatal(err)
	}
	names := expertMarketCategoryNames(t, h)
	if len(names) != 2 || names[0] != "办公" || names[1] != "科研" {
		t.Fatalf("taxonomy after snapshot=%v, want [办公 科研]", names)
	}
	var seeded string
	if err := h.store.DB().QueryRowContext(context.Background(), `SELECT value FROM sm_expert_market_meta WHERE key='expert_categories_seeded'`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != "1" {
		t.Fatalf("seed marker=%q", seeded)
	}
	// An invalid op type is a no-op, never a table wipe.
	if err := h.ApplyExpertMarketCategoriesSnapshot(context.Background(), "delete", snapshot); err != nil {
		t.Fatalf("delete op must be ignored: %v", err)
	}
	if again := expertMarketCategoryNames(t, h); len(again) != 2 {
		t.Fatalf("taxonomy after ignored delete op=%v", again)
	}
}

func jsonQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
