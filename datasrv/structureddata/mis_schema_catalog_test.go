package structureddata

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedMISCatalogHasHeaderLineAndSharedMasters(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_mis", UserID: "user_1", Role: "data_admin"}

	catalog, err := svc.ListTemplates(context.Background(), p)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(catalog) != len(datasetTemplates) {
		t.Fatalf("ListTemplates returned %d items, shipped catalog has %d", len(catalog), len(datasetTemplates))
	}
	byID := map[string]DatasetTemplate{}
	for _, tmpl := range catalog {
		if _, dup := byID[tmpl.ID]; dup {
			t.Fatalf("duplicate shipped template id %s", tmpl.ID)
		}
		byID[tmpl.ID] = tmpl
	}
	roleByDataset := map[string]string{}
	seenRoles := map[string]string{}
	for _, def := range businessObjectDefinitions {
		if def.ObjectRole == "" {
			t.Fatal("business object definition missing object role")
		}
		if prev, dup := seenRoles[def.ObjectRole]; dup {
			t.Fatalf("duplicate object role %s on %s and %s", def.ObjectRole, prev, def.DatasetID)
		}
		seenRoles[def.ObjectRole] = def.DatasetID
		if def.DatasetID == "" {
			continue
		}
		if def.TemplateID != def.DatasetID {
			t.Fatalf("object role %s template %s must equal dataset %s", def.ObjectRole, def.TemplateID, def.DatasetID)
		}
		if _, ok := byID[def.DatasetID]; !ok {
			t.Fatalf("object role %s maps to missing template %s", def.ObjectRole, def.DatasetID)
		}
		if def.Domain != byID[def.DatasetID].Domain {
			t.Fatalf("object role %s domain %s does not match template domain %s", def.ObjectRole, def.Domain, byID[def.DatasetID].Domain)
		}
		roleByDataset[def.DatasetID] = def.ObjectRole
	}
	for id := range byID {
		if _, ok := roleByDataset[id]; !ok {
			t.Fatalf("shipped template %s has no object-role mapping", id)
		}
	}
	for _, id := range shippedPreviouslyPublishedTemplateIDs() {
		if _, ok := byID[id]; !ok {
			t.Fatalf("existing public template %s missing from shipped catalog", id)
		}
	}
	ticket, ok := byID["sales.service_tickets"]
	if !ok {
		t.Fatal("sales.service_tickets missing")
	}
	if ref, ok := templateFieldByKey(ticket, "customer_ref"); !ok || templateRefDataset(ref) != "sales.customers" {
		t.Fatal("service ticket must record_ref sales.customers")
	}
	if ref, ok := templateFieldByKey(ticket, "order_ref"); !ok || templateRefDataset(ref) != "sales.orders" || ref.Required {
		t.Fatal("service ticket must optionally record_ref sales.orders")
	}
	if _, ok := byID["finance.bank_accounts"]; !ok {
		t.Fatal("finance.bank_accounts missing")
	}
	if _, ok := byID["company.projects"]; !ok {
		t.Fatal("company.projects missing")
	}
	invoice, ok := byID["finance.invoices"]
	if !ok {
		t.Fatal("finance.invoices missing")
	}
	if ref, ok := templateFieldByKey(invoice, "order_ref"); !ok || templateRefDataset(ref) != "sales.orders" || ref.Required {
		t.Fatal("invoice must optionally record_ref sales.orders")
	}
	if ref, ok := templateFieldByKey(invoice, "purchase_order_ref"); !ok || templateRefDataset(ref) != "procurement.purchase_orders" || ref.Required {
		t.Fatal("invoice must optionally record_ref procurement.purchase_orders")
	}
	if _, ok := templateFieldByKey(invoice, "due_date"); !ok {
		t.Fatal("invoice must have due_date")
	}
	asset, ok := byID["assets.fixed_assets"]
	if !ok {
		t.Fatal("assets.fixed_assets missing")
	}
	if _, ok := templateFieldByKey(asset, "useful_life_months"); !ok {
		t.Fatal("fixed asset register must store useful_life_months for depreciation")
	}
	if _, ok := templateFieldByKey(asset, "residual_value"); !ok {
		t.Fatal("fixed asset register must store residual_value for depreciation")
	}
	if ref, ok := templateFieldByKey(byID["procurement.return_lines"], "warehouse_ref"); !ok || templateRefDataset(ref) != "inventory.warehouses" {
		t.Fatal("purchase return lines must record_ref inventory.warehouses")
	}
	lineCovered := map[string]bool{}
	headerCovered := map[string]bool{}
	for _, spec := range shippedMISHeaderLineSpecs() {
		if headerCovered[spec.HeaderTemplateID] {
			t.Fatalf("duplicate header-line spec for %s", spec.HeaderTemplateID)
		}
		if lineCovered[spec.LineTemplateID] {
			t.Fatalf("duplicate header-line spec for line %s", spec.LineTemplateID)
		}
		headerCovered[spec.HeaderTemplateID] = true
		lineCovered[spec.LineTemplateID] = true
	}
	for _, tmpl := range catalog {
		if strings.HasSuffix(tmpl.ID, "_lines") || strings.HasSuffix(tmpl.ID, "_components") {
			if !lineCovered[tmpl.ID] {
				t.Fatalf("line template %s is not registered in shippedMISHeaderLineSpecs", tmpl.ID)
			}
		}
		seenKeys := map[string]bool{}
		for _, field := range tmpl.Fields {
			if field.Key == "" {
				t.Fatalf("%s has a field with empty key", tmpl.ID)
			}
			if seenKeys[field.Key] {
				t.Fatalf("%s has duplicate field key %s", tmpl.ID, field.Key)
			}
			seenKeys[field.Key] = true
			if field.Type != "record_ref" {
				continue
			}
			target := templateRefDataset(field)
			if target == "" {
				if field.Key == "source_ref" {
					continue
				}
				t.Fatalf("%s.%s is record_ref without ref_dataset", tmpl.ID, field.Key)
			}
			if _, ok := byID[target]; !ok {
				t.Fatalf("%s.%s refs unknown dataset %s", tmpl.ID, field.Key, target)
			}
			switch {
			case strings.HasSuffix(target, ".customers") && target != "sales.customers":
				t.Fatalf("%s.%s must not introduce a second customer table %s", tmpl.ID, field.Key, target)
			case strings.HasSuffix(target, ".suppliers") && target != "procurement.suppliers":
				t.Fatalf("%s.%s must not introduce a second supplier table %s", tmpl.ID, field.Key, target)
			case strings.HasSuffix(target, ".items") && target != "inventory.items":
				t.Fatalf("%s.%s must not introduce a second item table %s", tmpl.ID, field.Key, target)
			case strings.HasSuffix(target, ".warehouses") && target != "inventory.warehouses":
				t.Fatalf("%s.%s must not introduce a second warehouse table %s", tmpl.ID, field.Key, target)
			case strings.HasSuffix(target, ".fixed_assets") && target != "assets.fixed_assets":
				t.Fatalf("%s.%s must not introduce a second asset table %s", tmpl.ID, field.Key, target)
			}
		}
		for i, row := range tmpl.SampleData {
			for key := range row {
				if !seenKeys[key] {
					t.Fatalf("%s sample data has unknown field %s", tmpl.ID, key)
				}
			}
			for _, field := range tmpl.Fields {
				if !field.Required {
					continue
				}
				if _, ok := row[field.Key]; !ok {
					t.Fatalf("%s sample[%d] missing required field %s", tmpl.ID, i, field.Key)
				}
			}
		}
	}

	for _, spec := range shippedMISHeaderLineSpecs() {
		header, ok := byID[spec.HeaderTemplateID]
		if !ok {
			t.Fatalf("header template %s missing from shipped ListTemplates catalog", spec.HeaderTemplateID)
		}
		line, ok := byID[spec.LineTemplateID]
		if !ok {
			t.Fatalf("line template %s missing for header %s", spec.LineTemplateID, spec.HeaderTemplateID)
		}
		ref, ok := templateFieldByKey(line, spec.HeaderRefField)
		if !ok || ref.Type != "record_ref" || templateRefDataset(ref) != spec.HeaderTemplateID {
			t.Fatalf("%s.%s must record_ref %s, got type=%s ref=%s", spec.LineTemplateID, spec.HeaderRefField, spec.HeaderTemplateID, ref.Type, templateRefDataset(ref))
		}
		for _, key := range spec.LineQtyAmountKeys {
			if _, ok := templateFieldByKey(line, key); !ok {
				t.Fatalf("%s missing required line field %s", spec.LineTemplateID, key)
			}
		}
		if spec.HeaderTemplateID == "finance.vouchers" {
			if _, ok := templateFieldByKey(line, "account_ref"); !ok {
				t.Fatal("voucher lines must have account_ref")
			}
			if _, ok := templateFieldByKey(header, "lines"); ok {
				if _, ok := byID["finance.voucher_lines"]; !ok {
					t.Fatal("voucher header array must not be the only line store")
				}
			}
		}
		_ = header
	}

	roleToDataset := map[string]map[string]struct{}{}
	for _, def := range businessObjectDefinitions {
		roleToDataset[def.ObjectRole] = map[string]struct{}{}
	}
	for _, def := range businessObjectDefinitions {
		roleToDataset[def.ObjectRole][def.DatasetID] = struct{}{}
	}
	for _, role := range shippedMISSharedMasterRoles() {
		ids := roleToDataset[role]
		if len(ids) != 1 {
			t.Fatalf("shared master role %s must map to exactly one dataset, got %#v", role, ids)
		}
	}

	items, ok := byID["inventory.items"]
	if !ok {
		t.Fatal("inventory.items missing")
	}
	if _, hasQty := templateFieldByKey(items, "quantity"); hasQty {
		t.Fatal("inventory item master must not store warehouse quantity as the only stock truth")
	}
	movements, ok := byID["inventory.movements"]
	if !ok {
		t.Fatal("inventory.movements missing")
	}
	balances, ok := byID["inventory.stock_balances"]
	if !ok {
		t.Fatal("inventory.stock_balances missing")
	}
	for _, stock := range []DatasetTemplate{movements, balances} {
		qty, ok := templateFieldByKey(stock, "quantity")
		if !ok || qty.Type != "number" {
			t.Fatalf("%s must carry numeric quantity", stock.ID)
		}
		itemRef, ok := templateFieldByKey(stock, "item_ref")
		if !ok || templateRefDataset(itemRef) != "inventory.items" {
			t.Fatalf("%s must record_ref inventory.items", stock.ID)
		}
	}
	wh, ok := templateFieldByKey(balances, "warehouse_ref")
	if !ok || templateRefDataset(wh) != "inventory.warehouses" {
		t.Fatal("stock balances must record_ref inventory.warehouses")
	}

	actions := map[string]bool{}
	actionDataset := map[string]string{}
	actionDomain := map[string]string{}
	for _, action := range businessActions {
		actions[action.ID] = true
		actionDataset[action.ID] = action.DatasetID
		actionDomain[action.ID] = action.Domain
	}
	views := map[string]bool{}
	viewDomain := map[string]string{}
	for _, view := range businessViews {
		views[view.ID] = true
		viewDomain[view.ID] = view.Domain
	}
	reports := map[string]bool{}
	reportDomain := map[string]string{}
	for _, report := range reportDefinitions {
		reports[report.ID] = true
		reportDomain[report.ID] = report.Domain
	}
	dashboards := map[string]bool{}
	dashboardDomain := map[string]string{}
	for _, dashboard := range dashboardDefinitions {
		dashboards[dashboard.ID] = true
		dashboardDomain[dashboard.ID] = dashboard.Domain
	}
	assertPreferred := func(kind, owner, id string, known map[string]bool) {
		t.Helper()
		if id == "" {
			return
		}
		if !known[id] {
			t.Fatalf("%s %s references missing %s %s", owner, kind, kind, id)
		}
	}
	for _, def := range businessObjectDefinitions {
		owner := "object role " + def.ObjectRole
		assertPreferred("action", owner, def.PreferredAction, actions)
		assertPreferred("view", owner, def.PreferredView, views)
		assertPreferred("report", owner, def.PreferredReport, reports)
		assertPreferred("dashboard", owner, def.PreferredDashboard, dashboards)
		if def.PreferredAction != "" && actionDataset[def.PreferredAction] != def.DatasetID {
			t.Fatalf("object role %s preferred action %s writes %s, not %s", def.ObjectRole, def.PreferredAction, actionDataset[def.PreferredAction], def.DatasetID)
		}
	}
	for _, domain := range []string{"sales", "finance", "hr", "legal", "procurement", "inventory", "manufacturing", "assets", "company"} {
		for _, useCase := range businessDomainUseCases(domain) {
			owner := "use case " + useCase.ID
			assertPreferred("action", owner, useCase.PreferredAction, actions)
			assertPreferred("view", owner, useCase.PreferredView, views)
			assertPreferred("report", owner, useCase.PreferredReport, reports)
			assertPreferred("dashboard", owner, useCase.PreferredDashboard, dashboards)
			if useCase.PreferredAction != "" && actionDomain[useCase.PreferredAction] != domain {
				t.Fatalf("use case %s preferred action %s is domain %s, not %s", useCase.ID, useCase.PreferredAction, actionDomain[useCase.PreferredAction], domain)
			}
			if useCase.PreferredView != "" && viewDomain[useCase.PreferredView] != domain {
				t.Fatalf("use case %s preferred view %s is domain %s, not %s", useCase.ID, useCase.PreferredView, viewDomain[useCase.PreferredView], domain)
			}
			if useCase.PreferredReport != "" && reportDomain[useCase.PreferredReport] != domain {
				t.Fatalf("use case %s preferred report %s is domain %s, not %s", useCase.ID, useCase.PreferredReport, reportDomain[useCase.PreferredReport], domain)
			}
			if useCase.PreferredDashboard != "" && dashboardDomain[useCase.PreferredDashboard] != domain {
				t.Fatalf("use case %s preferred dashboard %s is domain %s, not %s", useCase.ID, useCase.PreferredDashboard, dashboardDomain[useCase.PreferredDashboard], domain)
			}
		}
	}

	seenActionIDs := map[string]bool{}
	for _, action := range businessActions {
		if action.ID == "" {
			t.Fatal("business action missing id")
		}
		if seenActionIDs[action.ID] {
			t.Fatalf("duplicate business action id %s", action.ID)
		}
		seenActionIDs[action.ID] = true
		if action.DatasetID == "" {
			t.Fatalf("business action %s missing dataset id", action.ID)
		}
		tmpl, ok := byID[action.DatasetID]
		if !ok {
			t.Fatalf("business action %s maps to missing template %s", action.ID, action.DatasetID)
		}
		if action.Domain != tmpl.Domain {
			t.Fatalf("business action %s domain %s does not match template domain %s", action.ID, action.Domain, tmpl.Domain)
		}
		inputKeys := map[string]bool{}
		for _, field := range action.InputFields {
			inputKeys[field.Key] = true
		}
		for _, key := range action.RequiredFields {
			if !inputKeys[key] {
				if _, onTmpl := templateFieldByKey(tmpl, key); !onTmpl {
					t.Fatalf("business action %s required field %s is missing from input fields and template %s", action.ID, key, action.DatasetID)
				}
			}
		}
	}
	seenViewIDs := map[string]bool{}
	for _, view := range businessViews {
		if view.ID == "" {
			t.Fatal("business view missing id")
		}
		if seenViewIDs[view.ID] {
			t.Fatalf("duplicate business view id %s", view.ID)
		}
		seenViewIDs[view.ID] = true
		tmpl, ok := byID[view.DatasetID]
		if !ok {
			t.Fatalf("business view %s maps to missing template %s", view.ID, view.DatasetID)
		}
		if view.Domain != tmpl.Domain {
			t.Fatalf("business view %s domain %s does not match template domain %s", view.ID, view.Domain, tmpl.Domain)
		}
		for _, key := range view.Fields {
			if _, ok := templateFieldByKey(tmpl, key); !ok {
				t.Fatalf("business view %s field %s is missing from template %s", view.ID, key, view.DatasetID)
			}
		}
		for _, sortSpec := range view.DefaultSort {
			if _, ok := templateFieldByKey(tmpl, sortSpec.Field); !ok {
				t.Fatalf("business view %s default sort field %s is missing from template %s", view.ID, sortSpec.Field, view.DatasetID)
			}
		}
	}
	seenReportIDs := map[string]bool{}
	for _, report := range reportDefinitions {
		if report.ID == "" {
			t.Fatal("report missing id")
		}
		if seenReportIDs[report.ID] {
			t.Fatalf("duplicate report id %s", report.ID)
		}
		seenReportIDs[report.ID] = true
		tmpl, ok := byID[report.DatasetID]
		if !ok {
			t.Fatalf("report %s maps to missing template %s", report.ID, report.DatasetID)
		}
		if report.Domain != tmpl.Domain {
			t.Fatalf("report %s domain %s does not match template domain %s", report.ID, report.Domain, tmpl.Domain)
		}
		for _, key := range report.Aggregate.GroupBy {
			if _, ok := templateFieldByKey(tmpl, key); !ok {
				t.Fatalf("report %s group_by %s is missing from template %s", report.ID, key, report.DatasetID)
			}
		}
		for _, metric := range report.Aggregate.Metrics {
			if metric.Field == "" {
				continue
			}
			if _, ok := templateFieldByKey(tmpl, metric.Field); !ok {
				t.Fatalf("report %s metric field %s is missing from template %s", report.ID, metric.Field, report.DatasetID)
			}
		}
		if report.Aggregate.Filter != nil {
			if field, _ := report.Aggregate.Filter["field"].(string); field != "" {
				if _, ok := templateFieldByKey(tmpl, field); !ok {
					t.Fatalf("report %s filter field %s is missing from template %s", report.ID, field, report.DatasetID)
				}
			}
		}
	}
	seenDashboardIDs := map[string]bool{}
	for _, dashboard := range dashboardDefinitions {
		if dashboard.ID == "" {
			t.Fatal("dashboard missing id")
		}
		if seenDashboardIDs[dashboard.ID] {
			t.Fatalf("duplicate dashboard id %s", dashboard.ID)
		}
		seenDashboardIDs[dashboard.ID] = true
		for _, reportID := range dashboard.ReportIDs {
			if !seenReportIDs[reportID] {
				t.Fatalf("dashboard %s references missing report %s", dashboard.ID, reportID)
			}
		}
	}
}

func TestBootstrapCreatesExpandedMISCatalogThenSkips(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_mis", UserID: "user_1", Role: "data_admin"}
	ctx := context.Background()

	preview, err := svc.BootstrapTemplates(ctx, p, BootstrapTemplatesInput{DryRun: true, SkipExisting: true})
	if err != nil {
		t.Fatalf("dry-run BootstrapTemplates: %v", err)
	}
	if len(preview.WouldCreate) != len(datasetTemplates) || len(preview.Created) != 0 || len(preview.Errors) != 0 {
		t.Fatalf("dry-run want %d would_create, got created=%d would=%d errors=%v", len(datasetTemplates), len(preview.Created), len(preview.WouldCreate), preview.Errors)
	}

	first, err := svc.BootstrapTemplates(ctx, p, BootstrapTemplatesInput{SkipExisting: true})
	if err != nil {
		t.Fatalf("first BootstrapTemplates: %v", err)
	}
	if len(first.Created) != len(datasetTemplates) || len(first.Errors) != 0 {
		t.Fatalf("first apply want %d created, got created=%d skipped=%d errors=%v", len(datasetTemplates), len(first.Created), len(first.Skipped), first.Errors)
	}

	second, err := svc.BootstrapTemplates(ctx, p, BootstrapTemplatesInput{SkipExisting: true})
	if err != nil {
		t.Fatalf("second BootstrapTemplates: %v", err)
	}
	if len(second.Created) != 0 || len(second.Skipped) != len(datasetTemplates) || len(second.Errors) != 0 {
		t.Fatalf("second apply want skip %d, got created=%d skipped=%d errors=%v", len(datasetTemplates), len(second.Created), len(second.Skipped), second.Errors)
	}
}

func TestBootstrappedMISRelationshipsLinkHeadersLinesAndMasters(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc := NewService(store, "sqlite")
	p := Principal{TenantID: "tenant_mis", UserID: "user_1", Role: "data_admin"}
	ctx := context.Background()
	if _, err := svc.BootstrapTemplates(ctx, p, BootstrapTemplatesInput{SkipExisting: true}); err != nil {
		t.Fatalf("BootstrapTemplates: %v", err)
	}

	rels, err := svc.ListRelationships(ctx, p, QueryRelationshipsInput{Limit: 500})
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}

	want := [][3]string{
		{"sales.orders", "customer_ref", "sales.customers"},
		{"procurement.purchase_orders", "supplier_ref", "procurement.suppliers"},
		{"procurement.purchase_orders", "department_ref", "company.departments"},
		{"finance.invoices", "customer_ref", "sales.customers"},
		{"finance.payments", "bank_account_ref", "finance.bank_accounts"},
		{"finance.expenses", "department_ref", "company.departments"},
		{"inventory.stock_balances", "item_ref", "inventory.items"},
		{"inventory.stock_balances", "warehouse_ref", "inventory.warehouses"},
		{"inventory.movements", "item_ref", "inventory.items"},
		{"hr.employees", "department_ref", "company.departments"},
		{"hr.payroll", "department_ref", "company.departments"},
		{"sales.service_tickets", "customer_ref", "sales.customers"},
		{"sales.service_tickets", "order_ref", "sales.orders"},
		{"assets.maintenance_orders", "asset_ref", "assets.fixed_assets"},
		{"assets.fixed_assets", "department_ref", "company.departments"},
		{"assets.fixed_assets", "supplier_ref", "procurement.suppliers"},
		{"finance.invoices", "order_ref", "sales.orders"},
		{"finance.invoices", "purchase_order_ref", "procurement.purchase_orders"},
		{"finance.payments", "purchase_order_ref", "procurement.purchase_orders"},
		{"sales.orders", "project_ref", "company.projects"},
		{"sales.service_tickets", "item_ref", "inventory.items"},
		{"sales.service_tickets", "asset_ref", "assets.fixed_assets"},
		{"procurement.return_lines", "warehouse_ref", "inventory.warehouses"},
		{"hr.leave_requests", "approver_ref", "hr.employees"},
		{"finance.vouchers", "department_ref", "company.departments"},
		{"manufacturing.production_orders", "sales_order_ref", "sales.orders"},
		{"finance.invoice_lines", "order_line_ref", "sales.order_lines"},
		{"finance.invoice_lines", "purchase_order_line_ref", "procurement.purchase_order_lines"},
		{"sales.return_lines", "order_line_ref", "sales.order_lines"},
		{"sales.return_lines", "shipment_line_ref", "sales.shipment_lines"},
		{"procurement.return_lines", "receipt_line_ref", "procurement.receipt_lines"},
		{"finance.payments", "voucher_ref", "finance.vouchers"},
		{"finance.invoices", "project_ref", "company.projects"},
		{"sales.contacts", "owner_ref", "hr.employees"},
		{"sales.opportunities", "owner_ref", "hr.employees"},
		{"sales.order_lines", "quote_line_ref", "sales.quote_lines"},
	}
	for _, spec := range shippedMISHeaderLineSpecs() {
		want = append(want, [3]string{spec.LineTemplateID, spec.HeaderRefField, spec.HeaderTemplateID})
		if containsString(spec.LineQtyAmountKeys, "item_ref") {
			want = append(want, [3]string{spec.LineTemplateID, "item_ref", "inventory.items"})
		}
		if containsString(spec.LineQtyAmountKeys, "asset_ref") {
			want = append(want, [3]string{spec.LineTemplateID, "asset_ref", "assets.fixed_assets"})
		}
		if containsString(spec.LineQtyAmountKeys, "account_ref") {
			want = append(want, [3]string{spec.LineTemplateID, "account_ref", "finance.accounts"})
		}
	}
	for _, row := range want {
		if !containsRelationship(rels, row[0], row[1], row[2]) {
			t.Fatalf("missing relationship %s.%s -> %s in %#v", row[0], row[1], row[2], rels)
		}
	}
}
