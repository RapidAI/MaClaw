package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestRegisterKnowledgeTools(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)
	for _, name := range []string{"knowledge_search", "knowledge_explain", "knowledge_search_facets", "knowledge_topic_relevance", "knowledge_context_pack", "knowledge_fact_graph", "knowledge_fact_index", "knowledge_entity_profile", "knowledge_suggest", "knowledge_save_url", "knowledge_save_urls", "knowledge_discover_urls", "knowledge_save_text", "knowledge_import_directory", "knowledge_import_files", "knowledge_import_status", "knowledge_stats", "knowledge_doctor", "knowledge_health", "knowledge_source_quality", "knowledge_quality_maintenance_plan", "knowledge_quality_maintenance_policies", "knowledge_execute_quality_maintenance_plan", "knowledge_rebuild_quality_gaps", "knowledge_backfill_quality_labels", "knowledge_disable_quality_sensitive_sources", "knowledge_suppress_quality_duplicate_groups", "knowledge_capabilities", "knowledge_url_domain_policies", "knowledge_maintain", "knowledge_export_snapshot", "knowledge_import_snapshot", "knowledge_list_sources", "knowledge_list_source_labels", "knowledge_update_source_labels", "knowledge_backfill_source_auto_labels", "knowledge_list_import_batches", "knowledge_list_import_items", "knowledge_retry_import_batch", "knowledge_source_detail", "knowledge_source_digest", "knowledge_list_source_versions", "knowledge_list_source_links", "knowledge_source_graph", "knowledge_source_neighborhood", "knowledge_source_path", "knowledge_preview_topic_links", "knowledge_link_sources", "knowledge_unlink_sources", "knowledge_list_source_link_events", "knowledge_source_timeline", "knowledge_refresh_topic_links", "knowledge_list_duplicate_cards", "knowledge_suppress_duplicate_cards", "knowledge_suppress_duplicate_groups", "knowledge_list_suppressed_cards", "knowledge_scan_sensitive", "knowledge_disable_sensitive_sources", "knowledge_restore_suppressed_cards", "knowledge_restore_suppressed_cards_bulk", "knowledge_update_source_metadata", "knowledge_refresh_source", "knowledge_preview_source_refresh", "knowledge_preview_sources_refresh", "knowledge_preview_sources_refresh_by_filter", "knowledge_refresh_changed_sources", "knowledge_refresh_changed_sources_by_filter", "knowledge_refresh_sources", "knowledge_refresh_sources_by_filter", "knowledge_rebuild_source_derived", "knowledge_rebuild_sources_derived", "knowledge_rebuild_sources_derived_by_filter", "knowledge_disable_source", "knowledge_disable_sources_by_filter", "knowledge_enable_source", "knowledge_enable_sources_by_filter", "knowledge_delete_source"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		if tool.Handler == nil {
			t.Fatalf("expected %s to have a handler", name)
		}
	}
}

func TestKnowledgeToolSourceFilterLimit(t *testing.T) {
	t.Parallel()
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"limit": 900}, 100, 500, 5000); got != 500 {
		t.Fatalf("capped limit = %d, want 500", got)
	}
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"source_ids": []interface{}{"s1", "s2", "s3"}, "limit": 1}, 100, 500, 5000); got != 3 {
		t.Fatalf("explicit source limit = %d, want 3", got)
	}
	ids := make([]interface{}, 0, 7)
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7"} {
		ids = append(ids, id)
	}
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"ids": ids, "limit": 1}, 100, 3, 5); got != 5 {
		t.Fatalf("explicit source hard-capped limit = %d, want 5", got)
	}
}

func TestKnowledgeExecuteQualityMaintenanceSchemaDocumentsExecutableActions(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	tool, ok := registry.Get("knowledge_execute_quality_maintenance_plan")
	if !ok {
		t.Fatalf("expected knowledge_execute_quality_maintenance_plan to be registered")
	}
	actionsSchema, ok := tool.InputSchema["actions"].(map[string]interface{})
	if !ok {
		t.Fatalf("actions schema missing or malformed: %#v", tool.InputSchema["actions"])
	}
	description, _ := actionsSchema["description"].(string)
	for _, action := range []string{
		"disable_sensitive_sources",
		"refresh_or_reimport_missing_nodes",
		"rebuild_derived_gaps",
		"refresh_topic_links",
		"backfill_labels",
		"suppress_duplicate_groups",
	} {
		if !strings.Contains(description, action) {
			t.Fatalf("actions schema description missing %s: %q", action, description)
		}
	}
	items, ok := actionsSchema["items"].(map[string]string)
	if !ok || items["type"] != "string" {
		t.Fatalf("actions schema should declare string array items: %#v", actionsSchema["items"])
	}
	maxSourcesSchema, ok := tool.InputSchema["max_sources_per_action"].(map[string]string)
	if !ok {
		t.Fatalf("max_sources_per_action schema missing or malformed: %#v", tool.InputSchema["max_sources_per_action"])
	}
	maxSourcesDescription := maxSourcesSchema["description"]
	for _, phrase := range []string{"explicit source_ids", "automatically raises", "cover those sources"} {
		if !strings.Contains(maxSourcesDescription, phrase) {
			t.Fatalf("max_sources_per_action description missing %q: %q", phrase, maxSourcesDescription)
		}
	}
	limitSchema, ok := tool.InputSchema["limit"].(map[string]string)
	if !ok {
		t.Fatalf("limit schema missing or malformed: %#v", tool.InputSchema["limit"])
	}
	limitDescription := limitSchema["description"]
	for _, phrase := range []string{"explicit source_ids", "planning limit", "cover those sources"} {
		if !strings.Contains(limitDescription, phrase) {
			t.Fatalf("limit description missing %q: %q", phrase, limitDescription)
		}
	}
}

func TestKnowledgeQualityOverviewSchemasDocumentExplicitSourceLimitRaise(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	tests := map[string][]string{
		"knowledge_health":                   {"explicit source_ids", "health limit", "cover those sources", "5000"},
		"knowledge_source_quality":           {"explicit source_ids", "scoring limit", "cover those sources", "5000"},
		"knowledge_quality_maintenance_plan": {"explicit source_ids", "planning limit", "cover those sources", "5000"},
	}
	for name, phrases := range tests {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		limitSchema, ok := tool.InputSchema["limit"].(map[string]string)
		if !ok {
			t.Fatalf("%s limit schema missing or malformed: %#v", name, tool.InputSchema["limit"])
		}
		description := limitSchema["description"]
		for _, phrase := range phrases {
			if !strings.Contains(description, phrase) {
				t.Fatalf("%s limit description missing %q: %q", name, phrase, description)
			}
		}
	}
}

func TestKnowledgeQualityActionSchemasDocumentExplicitSourceLimitRaise(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{
		"knowledge_rebuild_quality_gaps",
		"knowledge_backfill_quality_labels",
		"knowledge_disable_quality_sensitive_sources",
		"knowledge_suppress_quality_duplicate_groups",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		limitSchema, ok := tool.InputSchema["limit"].(map[string]string)
		if !ok {
			t.Fatalf("%s limit schema missing or malformed: %#v", name, tool.InputSchema["limit"])
		}
		description := limitSchema["description"]
		for _, phrase := range []string{"max 5000", "explicit source_ids", "inspection limit", "cover those sources"} {
			if !strings.Contains(description, phrase) {
				t.Fatalf("%s limit description missing %q: %q", name, phrase, description)
			}
		}
	}
}

func TestKnowledgeToolQualityInspectionLimitCoversExplicitSourceIDs(t *testing.T) {
	sourceIDs := make([]interface{}, 0, 5002)
	sourceIDs = append(sourceIDs, "ksrc_0", "ksrc_0", "")
	for i := 1; i <= 5001; i++ {
		sourceIDs = append(sourceIDs, "ksrc_"+strconv.Itoa(i))
	}
	if got := knowledgeToolQualityInspectionLimit(map[string]interface{}{"source_ids": sourceIDs, "limit": 1}); got != 5000 {
		t.Fatalf("expected explicit source IDs to raise and cap quality inspection limit, got %d", got)
	}
	if got := knowledgeToolQualityInspectionLimit(map[string]interface{}{"limit": -10}); got != 100 {
		t.Fatalf("expected default quality inspection limit for invalid input, got %d", got)
	}
}

func TestKnowledgeByFilterSchemasDocumentExplicitSourceLimitRaise(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{
		"knowledge_preview_sources_refresh_by_filter",
		"knowledge_refresh_changed_sources_by_filter",
		"knowledge_refresh_sources_by_filter",
		"knowledge_rebuild_sources_derived_by_filter",
		"knowledge_disable_sources_by_filter",
		"knowledge_enable_sources_by_filter",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		limitSchema, ok := tool.InputSchema["limit"].(map[string]string)
		if !ok {
			t.Fatalf("%s limit schema missing or malformed: %#v", name, tool.InputSchema["limit"])
		}
		description := limitSchema["description"]
		for _, phrase := range []string{"explicit source_ids", "filter limit", "cover those sources", "5000"} {
			if !strings.Contains(description, phrase) {
				t.Fatalf("%s limit description missing %q: %q", name, phrase, description)
			}
		}
	}
}

func TestKnowledgeToolSourceFilterLimitCoversExplicitSourceIDs(t *testing.T) {
	sourceIDs := make([]interface{}, 0, 703)
	sourceIDs = append(sourceIDs, "ksrc_0", "ksrc_0", "")
	for i := 1; i <= 701; i++ {
		sourceIDs = append(sourceIDs, "ksrc_"+strconv.Itoa(i))
	}
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"source_ids": sourceIDs, "limit": 1}, 100, 500, 5000); got != 702 {
		t.Fatalf("expected explicit source IDs to raise source filter limit, got %d", got)
	}
	manySourceIDs := make([]interface{}, 0, 5002)
	for i := 0; i < 5002; i++ {
		manySourceIDs = append(manySourceIDs, "ksrc_many_"+strconv.Itoa(i))
	}
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"source_ids": manySourceIDs, "limit": 1}, 100, 500, 5000); got != 5000 {
		t.Fatalf("expected explicit source ID filter limit to cap at explicit max, got %d", got)
	}
	if got := knowledgeToolSourceFilterLimit(map[string]interface{}{"limit": 50000}, 100, 500, 5000); got != 500 {
		t.Fatalf("expected unscoped source filter limit to keep normal max, got %d", got)
	}
}

func TestKnowledgeSourceIDsAliasIsExposedInSchemas(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{
		"knowledge_search",
		"knowledge_explain",
		"knowledge_context_pack",
		"knowledge_search_facets",
		"knowledge_topic_relevance",
		"knowledge_fact_graph",
		"knowledge_fact_index",
		"knowledge_entity_profile",
		"knowledge_suggest",
		"knowledge_health",
		"knowledge_source_quality",
		"knowledge_quality_maintenance_plan",
		"knowledge_execute_quality_maintenance_plan",
		"knowledge_rebuild_quality_gaps",
		"knowledge_export_snapshot",
		"knowledge_list_sources",
		"knowledge_list_source_labels",
		"knowledge_update_source_labels",
		"knowledge_backfill_source_auto_labels",
		"knowledge_preview_sources_refresh",
		"knowledge_refresh_changed_sources",
		"knowledge_refresh_sources",
		"knowledge_rebuild_sources_derived",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		if _, ok := tool.InputSchema["source_ids"]; !ok {
			t.Fatalf("%s schema missing source_ids", name)
		}
		alias, ok := tool.InputSchema["ids"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s schema missing ids alias", name)
		}
		if alias["description"] != "Alias for source_ids." {
			t.Fatalf("%s ids description = %q, want alias description", name, alias["description"])
		}
	}

	for _, name := range []string{
		"knowledge_preview_sources_refresh",
		"knowledge_refresh_changed_sources",
		"knowledge_refresh_sources",
		"knowledge_rebuild_sources_derived",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		def := registeredToolToDef(*tool)
		fn := def["function"].(map[string]interface{})
		params := fn["parameters"].(map[string]interface{})
		if required, ok := params["required"].([]string); ok {
			for _, key := range required {
				if key == "source_ids" {
					t.Fatalf("%s function schema should allow ids alias without requiring source_ids", name)
				}
			}
		}
	}
}

func TestKnowledgeByFilterSchemasExposeSourceSelectors(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{
		"knowledge_preview_sources_refresh_by_filter",
		"knowledge_refresh_changed_sources_by_filter",
		"knowledge_refresh_sources_by_filter",
		"knowledge_rebuild_sources_derived_by_filter",
		"knowledge_disable_sources_by_filter",
		"knowledge_enable_sources_by_filter",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		for _, key := range []string{"label", "labels", "source_ids", "ids"} {
			if _, ok := tool.InputSchema[key]; !ok {
				t.Fatalf("%s schema missing %s", name, key)
			}
		}
	}
}

func TestKnowledgeSingleSourceSchemasExposeIDAlias(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{
		"knowledge_source_detail",
		"knowledge_list_source_links",
		"knowledge_source_neighborhood",
		"knowledge_preview_topic_links",
		"knowledge_list_source_link_events",
		"knowledge_source_timeline",
		"knowledge_source_digest",
		"knowledge_refresh_topic_links",
		"knowledge_list_source_versions",
		"knowledge_update_source_metadata",
		"knowledge_refresh_source",
		"knowledge_preview_source_refresh",
		"knowledge_rebuild_source_derived",
		"knowledge_disable_source",
		"knowledge_enable_source",
		"knowledge_delete_source",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		if _, ok := tool.InputSchema["source_id"]; !ok {
			t.Fatalf("%s schema missing source_id", name)
		}
		alias, ok := tool.InputSchema["id"].(map[string]string)
		if !ok {
			t.Fatalf("%s schema missing id alias", name)
		}
		if alias["description"] != "Alias for source_id." {
			t.Fatalf("%s id description = %q, want alias description", name, alias["description"])
		}
		def := registeredToolToDef(*tool)
		fn := def["function"].(map[string]interface{})
		params := fn["parameters"].(map[string]interface{})
		if required, ok := params["required"].([]string); ok {
			for _, key := range required {
				if key == "source_id" {
					t.Fatalf("%s function schema should allow id alias without requiring source_id", name)
				}
			}
		}
	}
}

func TestKnowledgeAliasSchemasForBatchCardAndLinkTools(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, name := range []string{"knowledge_list_import_items", "knowledge_retry_import_batch"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		if _, ok := tool.InputSchema["batch_id"]; !ok {
			t.Fatalf("%s schema missing batch_id", name)
		}
		alias, ok := tool.InputSchema["id"].(map[string]string)
		if !ok || alias["description"] != "Alias for batch_id." {
			t.Fatalf("%s schema missing batch id alias: %#v", name, tool.InputSchema["id"])
		}
	}

	restoreTool, ok := registry.Get("knowledge_restore_suppressed_cards")
	if !ok {
		t.Fatalf("expected knowledge_restore_suppressed_cards to be registered")
	}
	if _, ok := restoreTool.InputSchema["card_ids"]; !ok {
		t.Fatalf("restore schema missing card_ids")
	}
	if _, ok := restoreTool.InputSchema["ids"]; !ok {
		t.Fatalf("restore schema missing ids alias")
	}

	for _, name := range []string{"knowledge_source_path", "knowledge_link_sources", "knowledge_unlink_sources"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		for _, key := range []string{"from", "to"} {
			if _, ok := tool.InputSchema[key]; !ok {
				t.Fatalf("%s schema missing %s alias", name, key)
			}
		}
	}
}

func TestKnowledgeAlternativeInputSchemasDoNotRequirePrimaryOnlyFields(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	cases := map[string][]string{
		"knowledge_save_urls":                {"urls", "text", "url_list", "html"},
		"knowledge_discover_urls":            {"text", "html", "url_list"},
		"knowledge_entity_profile":           {"entity", "query"},
		"knowledge_import_status":            {"job_id", "id"},
		"knowledge_import_snapshot":          {"input_path", "path"},
		"knowledge_suppress_duplicate_cards": {"key", "claim"},
	}
	for name, keys := range cases {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected %s to be registered", name)
		}
		for _, key := range keys {
			if _, ok := tool.InputSchema[key]; !ok {
				t.Fatalf("%s schema missing %s", name, key)
			}
		}
		def := registeredToolToDef(*tool)
		fn := def["function"].(map[string]interface{})
		params := fn["parameters"].(map[string]interface{})
		if required, ok := params["required"].([]string); ok {
			for _, key := range required {
				for _, primary := range []string{"urls", "text", "entity", "job_id", "input_path", "key"} {
					if key == primary {
						t.Fatalf("%s function schema should allow alternative input without requiring %s", name, primary)
					}
				}
			}
		}
	}
}

func TestKnowledgeArraySchemasDeclareStringItems(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerKnowledgeTools(registry, app)

	for _, tool := range registry.List() {
		if tool.Source != "builtin:knowledge" {
			continue
		}
		for key, raw := range tool.InputSchema {
			schema, ok := raw.(map[string]interface{})
			if !ok || schema["type"] != "array" {
				continue
			}
			items, ok := schema["items"].(map[string]string)
			if !ok || items["type"] != "string" {
				t.Fatalf("%s.%s array schema should declare string items, got %#v", tool.Name, key, raw)
			}
		}
	}
}

func TestNormalizeKnowledgeSearchOptionsTrimsAndDedupesFilters(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	opts := app.normalizeKnowledgeSearchOptions(knowledge.SearchOptions{
		Query:        "  Prism Anchor  ",
		SearchScope:  " personal ",
		TopicHint:    "  Local Brain  ",
		Domain:       " Docs.Example.COM ",
		Entity:       "  Project Alpha ",
		Predicate:    " uses ",
		ContextTerms: []string{" Alpha ", "alpha", "  "},
		ResultTypes:  []string{" Card ", "card", "FACT"},
		SourceKinds:  []string{" Markdown ", "markdown"},
		SourceIDs:    []string{" KSRC_1 ", "ksrc_1"},
		Labels:       []string{" Governed ", "governed", "Docs"},
	})

	if opts.Query != "Prism Anchor" || opts.SearchScope != "personal" || opts.TopicHint != "Local Brain" || opts.Domain != "Docs.Example.COM" || opts.Entity != "Project Alpha" || opts.Predicate != "uses" {
		t.Fatalf("unexpected scalar normalization: %#v", opts)
	}
	if got, want := strings.Join(opts.ContextTerms, ","), "alpha"; got != want {
		t.Fatalf("context terms = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.ResultTypes, ","), "card,fact"; got != want {
		t.Fatalf("result types = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.SourceKinds, ","), "markdown"; got != want {
		t.Fatalf("source kinds = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.SourceIDs, ","), "ksrc_1"; got != want {
		t.Fatalf("source ids = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.Labels, ","), "governed,docs"; got != want {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestNormalizeKnowledgeListOptionsTrimsAndDedupesFilters(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	opts := app.normalizeKnowledgeListOptions(knowledge.ListSourcesOptions{
		SearchScope:    " all ",
		Query:          "  prism  ",
		Status:         " Distilled ",
		Kind:           " Markdown ",
		Domain:         " Docs.Example.COM ",
		Label:          " Governed ",
		CoverageFilter: " Missing Cards ",
		QualityGrade:   " Needs_Attention ",
		SourceIDs:      []string{" KSRC_1 ", "ksrc_1", " "},
		SourceKinds:    []string{" Markdown ", "markdown"},
		Labels:         []string{" Governed ", "governed", "Docs"},
		QualityGrades:  []string{" Poor ", "poor", "Good"},
		Limit:          -1,
	})

	if opts.SearchScope != "all" || opts.Query != "prism" || opts.Status != "distilled" || opts.Kind != "markdown" || opts.Domain != "Docs.Example.COM" || opts.Label != "Governed" || opts.CoverageFilter != "Missing Cards" || opts.QualityGrade != "needs_attention" || opts.Limit != 100 {
		t.Fatalf("unexpected list scalar normalization: %#v", opts)
	}
	if got, want := strings.Join(opts.SourceIDs, ","), "ksrc_1"; got != want {
		t.Fatalf("source ids = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.SourceKinds, ","), "markdown"; got != want {
		t.Fatalf("source kinds = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.Labels, ","), "governed,docs"; got != want {
		t.Fatalf("labels = %q, want %q", got, want)
	}
	if got, want := strings.Join(opts.QualityGrades, ","), "poor,good"; got != want {
		t.Fatalf("quality grades = %q, want %q", got, want)
	}
}

func TestKnowledgeToolListSourcesOptionsAcceptsIDsAlias(t *testing.T) {
	opts := knowledgeToolListSourcesOptions(map[string]interface{}{
		"ids": []interface{}{" KSRC_1 ", "ksrc_1", "ksrc_2"},
	}, 25)
	if got, want := strings.Join(opts.SourceIDs, ","), "KSRC_1,ksrc_1,ksrc_2"; got != want {
		t.Fatalf("source ids before app normalization = %q, want %q", got, want)
	}
	if !hasKnowledgeSourceFilterArgs(map[string]interface{}{"ids": []interface{}{"ksrc_1"}}) {
		t.Fatalf("ids alias should count as a knowledge source filter")
	}
}

func TestKnowledgeToolSourceIDsAcceptsIDsAlias(t *testing.T) {
	got := knowledgeToolSourceIDs(map[string]interface{}{"ids": []interface{}{" KSRC_1 ", "ksrc_1", "ksrc_2"}})
	if want := []string{"KSRC_1", "ksrc_1", "ksrc_2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("knowledgeToolSourceIDs = %#v, want %#v", got, want)
	}
	preferred := knowledgeToolSourceIDs(map[string]interface{}{
		"source_ids": []interface{}{"ksrc_primary"},
		"ids":        []interface{}{"ksrc_alias"},
	})
	if strings.Join(preferred, ",") != "ksrc_primary" {
		t.Fatalf("source_ids should take precedence over ids alias, got %#v", preferred)
	}
}

func TestKnowledgeToolStringSliceSplitsCommonSeparators(t *testing.T) {
	got := knowledgeToolStringSlice("alpha, beta;gamma\n delta\t epsilon\uFF0Czeta\uFF1Beta\u3001theta;alpha")
	if want := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("knowledgeToolStringSlice = %#v, want %#v", got, want)
	}
}

func TestKnowledgeToolStringSliceDedupesArrayInputs(t *testing.T) {
	got := knowledgeToolStringSlice([]interface{}{" alpha ", "beta,gamma", "alpha", "", "delta；epsilon", "gamma", "zeta、eta"})
	want := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("knowledgeToolStringSlice array = %#v, want %#v", got, want)
	}
}

func TestKnowledgeToolFilePathSlicePreservesPunctuationFileNames(t *testing.T) {
	got := knowledgeToolFilePathSlice([]interface{}{
		`D:\docs\a,b;c.md` + "\n" + `D:\docs\second.md`,
		`D:\docs\a,b;c.md`,
		`D:\docs\zh，name；part、tail.txt`,
	})
	want := []string{
		`D:\docs\a,b;c.md`,
		`D:\docs\second.md`,
		`D:\docs\zh，name；part、tail.txt`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("knowledgeToolFilePathSlice = %#v, want %#v", got, want)
	}
}

func TestKnowledgeToolURLListSplitsAndDedupesBatchText(t *testing.T) {
	got := knowledgeToolURLList("https://a.example.com, https://b.example.com；https://a.example.com\nhttps://c.example.com、https://b.example.com")
	want := []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("knowledgeToolURLList = %#v, want %#v", got, want)
	}
}

func TestKnowledgeToolFirstStringAcceptsArrayWrappedScalars(t *testing.T) {
	got := knowledgeToolFirstString(map[string]interface{}{
		"url":      []interface{}{"", " https://example.com/docs "},
		"fallback": "https://fallback.example.com",
	}, "url", "fallback")
	if got != "https://example.com/docs" {
		t.Fatalf("knowledgeToolFirstString = %q, want first array string", got)
	}
	got = knowledgeToolFirstString(map[string]interface{}{
		"input_path": []string{" ", `D:\tmp\knowledge.jsonl`},
	}, "input_path")
	if got != `D:\tmp\knowledge.jsonl` {
		t.Fatalf("knowledgeToolFirstString []string = %q", got)
	}
}

func TestKnowledgeToolScalarArgsAcceptArrayWrappedValues(t *testing.T) {
	args := map[string]interface{}{
		"limit":         []interface{}{"", json.Number("12")},
		"dry_run":       []interface{}{"", "false"},
		"auto_labels":   []string{" ", "true"},
		"source_trust":  []interface{}{"", "0.75"},
		"fallback_bool": []interface{}{"maybe"},
	}
	if got := knowledgeToolIntArg(args, "limit", 5); got != 12 {
		t.Fatalf("knowledgeToolIntArg = %d, want 12", got)
	}
	if got := knowledgeToolBoolArg(args, "dry_run", true); got {
		t.Fatalf("knowledgeToolBoolArg dry_run = true, want false")
	}
	if got := knowledgeToolBoolArg(args, "auto_labels", false); !got {
		t.Fatalf("knowledgeToolBoolArg auto_labels = false, want true")
	}
	if got := knowledgeToolBoolArg(args, "fallback_bool", true); !got {
		t.Fatalf("knowledgeToolBoolArg fallback = false, want true")
	}
	if got := knowledgeToolFloatArg(args, "source_trust", 1); got != 0.75 {
		t.Fatalf("knowledgeToolFloatArg = %v, want 0.75", got)
	}
	if got := knowledgeToolStringArg(map[string]interface{}{
		"source_id": []interface{}{"", " ksrc_array_wrapped "},
	}, "source_id"); got != "ksrc_array_wrapped" {
		t.Fatalf("knowledgeToolStringArg = %q, want trimmed array-wrapped string", got)
	}
}

func TestKnowledgeToolUniqueStringsDedupesMergedURLInputs(t *testing.T) {
	got := knowledgeToolUniqueStrings([]string{" https://a.example.com ", "https://b.example.com", "https://a.example.com", "", "https://c.example.com", "https://b.example.com"})
	want := []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("knowledgeToolUniqueStrings = %#v, want %#v", got, want)
	}
}

func TestKnowledgeToolMissingArgumentErrorsAreASCII(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for name, output := range map[string]string{
		"search":           app.toolKnowledgeSearch(map[string]interface{}{}),
		"save_url":         app.toolKnowledgeSaveURL(map[string]interface{}{}),
		"import_directory": app.toolKnowledgeImportDirectory(map[string]interface{}{}),
		"import_files":     app.toolKnowledgeImportFiles(map[string]interface{}{}),
	} {
		if !strings.Contains(output, "\"ok\": false") || !strings.Contains(output, "missing ") {
			t.Fatalf("%s missing argument output should be clear JSON error, got %s", name, output)
		}
		for _, r := range output {
			if r > 127 {
				t.Fatalf("%s missing argument output should be ASCII-stable, got %q", name, output)
			}
		}
	}
}

func TestKnowledgeToolsAreRoutedForAgentKnowledgeRequests(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	handler := &IMMessageHandler{app: app}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	registerKnowledgeTools(handler.registry, app)
	for i := 0; i < maxToolBudget; i++ {
		handler.registry.Register(RegisteredTool{
			Name:        "filler_tool_" + strconv.Itoa(i),
			Description: "Filler tool for unrelated routing pressure " + strconv.Itoa(i),
			Category:    ToolCategoryMCP,
			Status:      RegToolAvailable,
			Handler:     func(args map[string]interface{}) string { return "{}" },
		})
	}

	tools := NewDynamicToolBuilder(handler.registry).BuildAll()
	if len(tools) <= maxToolBudget {
		t.Fatalf("need more than %d tools to verify routed visibility, got %d", maxToolBudget, len(tools))
	}
	router := NewToolRouter(NewToolDefinitionGenerator(nil, tools))
	router.SetRegistry(handler.registry)
	handler.SetToolRouter(router)

	recallRouted := handler.routeTools("Use the saved local corpus to answer what the prism anchor note says.", tools)
	recallNames := knowledgeToolNames(recallRouted)
	if !recallNames["knowledge_search"] && !recallNames["knowledge_context_pack"] {
		t.Fatalf("expected a knowledge recall tool to be routed, got %v", sortedKnowledgeToolNames(recallNames))
	}

	importRouted := handler.routeTools("I approved D:\\docs; scan and import those documents into the saved local corpus.", tools)
	importNames := knowledgeToolNames(importRouted)
	if !importNames["knowledge_import_directory"] && !importNames["knowledge_import_files"] {
		t.Fatalf("expected a knowledge import tool to be routed, got %v", sortedKnowledgeToolNames(importNames))
	}
}

func knowledgeToolNames(tools []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[extractToolName(tool)] = true
	}
	return names
}

func sortedKnowledgeToolNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		if strings.HasPrefix(name, "knowledge_") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func TestKnowledgeSearchToolUsesLocalStore(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Notes\n\nProject uses prism anchor in local knowledge."), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	importResult, err := app.KnowledgeImportDirectory(knowledge.DirectoryImportRequest{
		RootPath:     root,
		SaveScope:    knowledge.SaveScopePersonal,
		Recursive:    true,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 1024,
	})
	if err != nil {
		t.Fatalf("KnowledgeImportDirectory: %v", err)
	}
	if importResult.BatchID == "" {
		t.Fatalf("expected import batch id")
	}
	out := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "prism anchor",
		"search_scope": "personal",
		"result_types": []interface{}{"card"},
	})
	if !strings.Contains(out, "\"ok\": true") || !strings.Contains(out, "prism") {
		t.Fatalf("unexpected search tool output: %s", out)
	}
	initialSources, err := app.KnowledgeListSources(knowledge.ListSourcesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("KnowledgeListSources: %v", err)
	}
	if len(initialSources) == 0 || initialSources[0].ID == "" {
		t.Fatalf("expected imported source")
	}
	sourceScopedOut := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "prism anchor",
		"search_scope": "personal",
		"source_ids":   []interface{}{initialSources[0].ID},
	})
	if !strings.Contains(sourceScopedOut, "\"ok\": true") || !strings.Contains(sourceScopedOut, initialSources[0].ID) {
		t.Fatalf("unexpected source-scoped search output: %s", sourceScopedOut)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Notes\n\nProject uses prism anchor and preview refresh anchor after a local file update."), 0o644); err != nil {
		t.Fatalf("update note: %v", err)
	}
	previewOut := app.toolKnowledgePreviewSourceRefresh(map[string]interface{}{
		"source_id": initialSources[0].ID,
	})
	if !strings.Contains(previewOut, "\"ok\": true") || !strings.Contains(previewOut, "\"changed\": true") || !strings.Contains(previewOut, "\"requires_refresh\": true") {
		t.Fatalf("unexpected refresh preview output: %s", previewOut)
	}
	previewBatchOut := app.toolKnowledgePreviewSourcesRefresh(map[string]interface{}{
		"source_ids": []interface{}{initialSources[0].ID},
	})
	if !strings.Contains(previewBatchOut, "\"ok\": true") || !strings.Contains(previewBatchOut, "\"changed\": 1") || !strings.Contains(previewBatchOut, "\"requested\": 1") {
		t.Fatalf("unexpected refresh preview batch output: %s", previewBatchOut)
	}
	previewFilterOut := app.toolKnowledgePreviewSourcesRefreshByFilter(map[string]interface{}{
		"search_scope": "personal",
		"kind":         "markdown",
		"limit":        5,
	})
	if !strings.Contains(previewFilterOut, "\"ok\": true") || !strings.Contains(previewFilterOut, "\"requested\": 1") {
		t.Fatalf("unexpected refresh preview filter output: %s", previewFilterOut)
	}
	previewFilterIDsAliasOut := app.toolKnowledgePreviewSourcesRefreshByFilter(map[string]interface{}{
		"ids":   []interface{}{initialSources[0].ID},
		"limit": 5,
	})
	if !strings.Contains(previewFilterIDsAliasOut, "\"ok\": true") || !strings.Contains(previewFilterIDsAliasOut, "\"requested\": 1") || !strings.Contains(previewFilterIDsAliasOut, initialSources[0].ID) {
		t.Fatalf("unexpected refresh preview filter ids alias output: %s", previewFilterIDsAliasOut)
	}
	refreshChangedOut := app.toolKnowledgeRefreshChangedSources(map[string]interface{}{
		"source_ids": []interface{}{initialSources[0].ID},
	})
	if !strings.Contains(refreshChangedOut, "\"ok\": true") || !strings.Contains(refreshChangedOut, "\"refreshed\": 1") || !strings.Contains(refreshChangedOut, "preview refresh anchor") {
		t.Fatalf("unexpected refresh changed output: %s", refreshChangedOut)
	}
	refreshChangedAgainOut := app.toolKnowledgeRefreshChangedSourcesByFilter(map[string]interface{}{
		"kind":  "markdown",
		"limit": 5,
	})
	if !strings.Contains(refreshChangedAgainOut, "\"ok\": true") || !strings.Contains(refreshChangedAgainOut, "\"refreshed\": 0") {
		t.Fatalf("unexpected refresh changed filter output: %s", refreshChangedAgainOut)
	}
	refreshChangedIDsAliasOut := app.toolKnowledgeRefreshChangedSourcesByFilter(map[string]interface{}{
		"ids":   []interface{}{initialSources[0].ID},
		"limit": 5,
	})
	if !strings.Contains(refreshChangedIDsAliasOut, "\"ok\": true") || !strings.Contains(refreshChangedIDsAliasOut, "\"requested\": 1") || !strings.Contains(refreshChangedIDsAliasOut, "\"refreshed\": 0") {
		t.Fatalf("unexpected refresh changed filter ids alias output: %s", refreshChangedIDsAliasOut)
	}
	explainOut := app.toolKnowledgeExplain(map[string]interface{}{
		"query":        "prism anchor",
		"search_scope": "personal",
		"limit":        3,
	})
	if !strings.Contains(explainOut, "\"ok\": true") || !strings.Contains(explainOut, "\"citations\"") || !strings.Contains(explainOut, "notes") {
		t.Fatalf("unexpected explain tool output: %s", explainOut)
	}
	facetsOut := app.toolKnowledgeSearchFacets(map[string]interface{}{
		"query":        "prism anchor",
		"search_scope": "personal",
		"limit":        10,
	})
	if !strings.Contains(facetsOut, "\"ok\": true") || !strings.Contains(facetsOut, "\"facets\"") || !strings.Contains(facetsOut, "\"entities\"") || !strings.Contains(facetsOut, "local_search_facets_no_llm") {
		t.Fatalf("unexpected search facets tool output: %s", facetsOut)
	}
	browseFacetsOut := app.toolKnowledgeSearchFacets(map[string]interface{}{
		"search_scope": "personal",
		"limit":        10,
	})
	if !strings.Contains(browseFacetsOut, "\"ok\": true") || !strings.Contains(browseFacetsOut, "browse_mode_no_query") || !strings.Contains(browseFacetsOut, "\"source_kinds\"") {
		t.Fatalf("unexpected browse facets tool output: %s", browseFacetsOut)
	}
	contextPackOut := app.toolKnowledgeContextPack(map[string]interface{}{
		"query":        "prism anchor",
		"search_scope": "personal",
		"max_items":    3,
		"max_chars":    800,
	})
	if !strings.Contains(contextPackOut, "\"ok\": true") || !strings.Contains(contextPackOut, "\"context_pack\"") || !strings.Contains(contextPackOut, "\"items\"") || !strings.Contains(contextPackOut, "prism") || !strings.Contains(contextPackOut, "local_context_pack_no_llm") {
		t.Fatalf("unexpected context pack tool output: %s", contextPackOut)
	}
	factGraphOut := app.toolKnowledgeFactGraph(map[string]interface{}{
		"search_scope": "personal",
		"entity":       "Project",
		"predicate":    "uses",
		"limit":        5,
	})
	if !strings.Contains(factGraphOut, "\"ok\": true") || !strings.Contains(factGraphOut, "\"fact_graph\"") || !strings.Contains(factGraphOut, "\"edges\"") || !strings.Contains(factGraphOut, "\"top_entities\"") || !strings.Contains(factGraphOut, "\"top_predicates\"") || !strings.Contains(factGraphOut, "local_fact_graph_no_llm") {
		t.Fatalf("unexpected fact graph tool output: %s", factGraphOut)
	}
	factIndexOut := app.toolKnowledgeFactIndex(map[string]interface{}{
		"search_scope": "personal",
		"kind":         "entity",
		"limit":        5,
	})
	if !strings.Contains(factIndexOut, "\"ok\": true") || !strings.Contains(factIndexOut, "\"fact_index\"") || !strings.Contains(factIndexOut, "\"items\"") || !strings.Contains(factIndexOut, "Project") || !strings.Contains(factIndexOut, "local_fact_index_no_llm") {
		t.Fatalf("unexpected fact index tool output: %s", factIndexOut)
	}
	entityProfileOut := app.toolKnowledgeEntityProfile(map[string]interface{}{
		"query":        []interface{}{"Project"},
		"search_scope": "personal",
		"limit":        5,
	})
	if !strings.Contains(entityProfileOut, "\"ok\": true") || !strings.Contains(entityProfileOut, "\"entity_profile\"") || !strings.Contains(entityProfileOut, "\"related_entities\"") || !strings.Contains(entityProfileOut, "local_entity_profile_no_llm") {
		t.Fatalf("unexpected entity profile tool output: %s", entityProfileOut)
	}
	suggestOut := app.toolKnowledgeSuggest(map[string]interface{}{
		"query":        "Project",
		"search_scope": "personal",
		"limit":        10,
	})
	if !strings.Contains(suggestOut, "\"ok\": true") || !strings.Contains(suggestOut, "\"suggestions\"") || !strings.Contains(suggestOut, "local_knowledge_suggest_no_llm") || !strings.Contains(suggestOut, "Project") {
		t.Fatalf("unexpected suggest tool output: %s", suggestOut)
	}
	saveTextOut := app.toolKnowledgeSaveText(map[string]interface{}{
		"text":       "Conversation memory anchor should persist as local knowledge.",
		"title":      "Conversation memory anchor",
		"kind":       "conversation",
		"save_scope": "personal",
		"topic_hint": "memory trigger",
	})
	if !strings.Contains(saveTextOut, "\"ok\": true") || !strings.Contains(saveTextOut, "Conversation memory anchor") {
		t.Fatalf("unexpected save text tool output: %s", saveTextOut)
	}
	secretOut := app.toolKnowledgeSaveText(map[string]interface{}{
		"text":         "Temporary credential password = supersecretvalue should be diagnosed.",
		"title":        "Sensitive diagnostic anchor",
		"kind":         "text",
		"save_scope":   "personal",
		"distill_mode": "rules_only",
		"labels":       []interface{}{"security-review"},
		"auto_labels":  true,
	})
	if !strings.Contains(secretOut, "\"ok\": true") || !strings.Contains(secretOut, "\"security-review\"") || !strings.Contains(secretOut, "\"kind:text\"") || !strings.Contains(secretOut, "\"scope:personal\"") {
		t.Fatalf("unexpected sensitive seed output: %s", secretOut)
	}
	sensitiveOut := app.toolKnowledgeScanSensitive(map[string]interface{}{"limit": 10})
	if !strings.Contains(sensitiveOut, "\"ok\": true") || !strings.Contains(sensitiveOut, "password_assignment") || strings.Contains(sensitiveOut, "supersecretvalue") {
		t.Fatalf("unexpected sensitive scan output: %s", sensitiveOut)
	}
	isolateOut := app.toolKnowledgeDisableSensitiveSources(map[string]interface{}{"limit": 10})
	if !strings.Contains(isolateOut, "\"ok\": true") || !strings.Contains(isolateOut, "\"updated\": 1") || strings.Contains(isolateOut, "supersecretvalue") {
		t.Fatalf("unexpected sensitive isolation output: %s", isolateOut)
	}
	saveURLsOut := app.toolKnowledgeSaveURLs(map[string]interface{}{
		"text":       "notaurl\nhttp://127.0.0.1/private",
		"save_scope": "personal",
	})
	if !strings.Contains(saveURLsOut, "\"ok\": true") || !strings.Contains(saveURLsOut, "\"failed\": 2") {
		t.Fatalf("unexpected save URLs tool output: %s", saveURLsOut)
	}
	discoverURLsOut := app.toolKnowledgeDiscoverURLs(map[string]interface{}{
		"text":             `<a href="/docs">Docs</a> https://example.com/a http://127.0.0.1/private`,
		"base_url":         "https://example.com/root",
		"same_domain_only": true,
		"limit":            10,
	})
	if !strings.Contains(discoverURLsOut, "\"ok\": true") || !strings.Contains(discoverURLsOut, "\"urls\"") || !strings.Contains(discoverURLsOut, "https://example.com/docs") || strings.Contains(discoverURLsOut, "127.0.0.1/private\",\"host") {
		t.Fatalf("unexpected discover URLs tool output: %s", discoverURLsOut)
	}
	discoverHTMLAliasOut := app.toolKnowledgeDiscoverURLs(map[string]interface{}{
		"html":             []interface{}{`<a href="/alias-docs">Docs</a>`},
		"base_url":         []interface{}{"https://example.com/root"},
		"same_domain_only": []interface{}{"true"},
		"limit":            []interface{}{"10"},
	})
	if !strings.Contains(discoverHTMLAliasOut, "\"ok\": true") || !strings.Contains(discoverHTMLAliasOut, "https://example.com/alias-docs") {
		t.Fatalf("unexpected discover URLs html alias output: %s", discoverHTMLAliasOut)
	}
	saveTextSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "memory anchor",
		"search_scope": "personal",
		"source_kinds": []interface{}{"conversation"},
	})
	if !strings.Contains(saveTextSearch, "\"ok\": true") || !strings.Contains(saveTextSearch, "memory anchor") {
		t.Fatalf("saved text was not searchable: %s", saveTextSearch)
	}
	topicRelevanceOut := app.toolKnowledgeTopicRelevance(map[string]interface{}{
		"topic_hint":   "memory trigger",
		"search_scope": "personal",
		"limit":        10,
	})
	if !strings.Contains(topicRelevanceOut, "\"ok\": true") || !strings.Contains(topicRelevanceOut, "\"topic_relevance\"") || !strings.Contains(topicRelevanceOut, "local_topic_relevance_no_llm") || !strings.Contains(topicRelevanceOut, "memory") {
		t.Fatalf("unexpected topic relevance output: %s", topicRelevanceOut)
	}
	previewLinksOut := app.toolKnowledgePreviewTopicLinks(map[string]interface{}{"source_id": initialSources[0].ID, "limit": 8})
	if !strings.Contains(previewLinksOut, "\"ok\": true") || !strings.Contains(previewLinksOut, "\"preview\"") || !strings.Contains(previewLinksOut, "local_source_topic_link_preview_no_llm") {
		t.Fatalf("unexpected preview topic links output: %s", previewLinksOut)
	}
	previewLinks, err := app.KnowledgePreviewSourceTopicLinks(initialSources[0].ID, 8)
	if err != nil {
		t.Fatalf("KnowledgePreviewSourceTopicLinks: %v", err)
	}
	if len(previewLinks.Links) > 0 {
		manualLinkOut := app.toolKnowledgeLinkSources(map[string]interface{}{
			"from":     []interface{}{initialSources[0].ID},
			"to":       []interface{}{previewLinks.Links[0].RelatedSourceID},
			"terms":    []interface{}{"manual-test"},
			"evidence": []interface{}{"tool-test"},
		})
		if !strings.Contains(manualLinkOut, "\"ok\": true") || !strings.Contains(manualLinkOut, "\"link\"") || !strings.Contains(manualLinkOut, "manual-test") {
			t.Fatalf("unexpected manual link output: %s", manualLinkOut)
		}
		unlinkOut := app.toolKnowledgeUnlinkSources(map[string]interface{}{
			"from": []interface{}{initialSources[0].ID},
			"to":   []interface{}{previewLinks.Links[0].RelatedSourceID},
		})
		if !strings.Contains(unlinkOut, "\"ok\": true") || !strings.Contains(unlinkOut, "local_source_unlink_no_llm") {
			t.Fatalf("unexpected unlink output: %s", unlinkOut)
		}
		linkEventsOut := app.toolKnowledgeListSourceLinkEvents(map[string]interface{}{"source_id": initialSources[0].ID, "limit": 10})
		if !strings.Contains(linkEventsOut, "\"ok\": true") || !strings.Contains(linkEventsOut, "\"events\"") || !strings.Contains(linkEventsOut, "\"unlink\"") {
			t.Fatalf("unexpected source link events output: %s", linkEventsOut)
		}
		eventDetailOut := app.toolKnowledgeSourceDetail(map[string]interface{}{"source_id": initialSources[0].ID, "limit": 10, "link_events_limit": 10})
		if !strings.Contains(eventDetailOut, "\"ok\": true") || !strings.Contains(eventDetailOut, "\"digest\"") || !strings.Contains(eventDetailOut, "\"link_events\"") || !strings.Contains(eventDetailOut, "\"link_event_count\"") || !strings.Contains(eventDetailOut, "\"timeline\"") || !strings.Contains(eventDetailOut, "\"unlink\"") {
			t.Fatalf("source detail should include link audit events: %s", eventDetailOut)
		}
		digestOut := app.toolKnowledgeSourceDigest(map[string]interface{}{"source_id": initialSources[0].ID, "limit": 6})
		if !strings.Contains(digestOut, "\"ok\": true") || !strings.Contains(digestOut, "\"digest\"") || !strings.Contains(digestOut, "local_source_digest_no_llm") || !strings.Contains(digestOut, "query_does_not_require_llm") {
			t.Fatalf("unexpected source digest output: %s", digestOut)
		}
		timelineOut := app.toolKnowledgeSourceTimeline(map[string]interface{}{"source_id": initialSources[0].ID, "limit": 20})
		if !strings.Contains(timelineOut, "\"ok\": true") || !strings.Contains(timelineOut, "\"timeline\"") || !strings.Contains(timelineOut, "local_source_timeline_no_llm") || !strings.Contains(timelineOut, "source_link_event") || !strings.Contains(timelineOut, "\"source_version\"") {
			t.Fatalf("unexpected source timeline output: %s", timelineOut)
		}
	}
	refreshLinksOut := app.toolKnowledgeRefreshTopicLinks(map[string]interface{}{"search_scope": "personal", "limit": 20, "limit_per_source": 8})
	if !strings.Contains(refreshLinksOut, "\"ok\": true") || !strings.Contains(refreshLinksOut, "local_source_topic_links_no_llm") {
		t.Fatalf("unexpected refresh topic links output: %s", refreshLinksOut)
	}
	sourceGraphOut := app.toolKnowledgeSourceGraph(map[string]interface{}{"search_scope": "personal", "limit": 20, "edge_limit": 50})
	if !strings.Contains(sourceGraphOut, "\"ok\": true") || !strings.Contains(sourceGraphOut, "\"graph\"") || !strings.Contains(sourceGraphOut, "local_source_graph_no_llm") || !strings.Contains(sourceGraphOut, "\"nodes\"") {
		t.Fatalf("unexpected source graph output: %s", sourceGraphOut)
	}
	neighborhoodSourceID := initialSources[0].ID
	neighborhoodOut := app.toolKnowledgeSourceNeighborhood(map[string]interface{}{"source_id": neighborhoodSourceID, "depth": 2, "limit": 20, "edge_limit": 50})
	if !strings.Contains(neighborhoodOut, "\"ok\": true") || !strings.Contains(neighborhoodOut, "\"graph\"") || !strings.Contains(neighborhoodOut, "local_source_neighborhood_no_llm") || !strings.Contains(neighborhoodOut, neighborhoodSourceID) {
		t.Fatalf("unexpected source neighborhood output: %s", neighborhoodOut)
	}
	sourceLinks, err := app.KnowledgeListSourceLinks(neighborhoodSourceID, 10)
	if err != nil {
		t.Fatalf("KnowledgeListSourceLinks: %v", err)
	}
	if len(sourceLinks) > 0 {
		pathOut := app.toolKnowledgeSourcePath(map[string]interface{}{"from_source_id": []interface{}{neighborhoodSourceID}, "to_source_id": []interface{}{sourceLinks[0].RelatedSourceID}, "max_depth": 4, "edge_limit": 50})
		if !strings.Contains(pathOut, "\"ok\": true") || !strings.Contains(pathOut, "\"path\"") || !strings.Contains(pathOut, "local_source_path_no_llm") || !strings.Contains(pathOut, "\"found\": true") || !strings.Contains(pathOut, "visited_count") {
			t.Fatalf("unexpected source path output: %s", pathOut)
		}
	}
	directPath := filepath.Join(root, "direct.md")
	if err := os.WriteFile(directPath, []byte("Direct comet signal imported from explicit file selection."), 0o644); err != nil {
		t.Fatalf("write direct note: %v", err)
	}
	scanOut := app.toolKnowledgeImportFiles(map[string]interface{}{
		"file_paths":   []interface{}{directPath},
		"action":       "scan",
		"save_scope":   "personal",
		"include_exts": []interface{}{".md"},
	})
	if !strings.Contains(scanOut, "\"ok\": true") || !strings.Contains(scanOut, "queued_files") {
		t.Fatalf("unexpected direct file scan output: %s", scanOut)
	}
	punctuatedPath := filepath.Join(root, "direct,a;b.md")
	if err := os.WriteFile(punctuatedPath, []byte("Punctuated file path should survive tool parsing."), 0o644); err != nil {
		t.Fatalf("write punctuated direct note: %v", err)
	}
	punctuatedScanOut := app.toolKnowledgeImportFiles(map[string]interface{}{
		"file_paths":   []interface{}{punctuatedPath},
		"action":       "scan",
		"save_scope":   "personal",
		"include_exts": []interface{}{".md"},
	})
	if !strings.Contains(punctuatedScanOut, "\"ok\": true") || !strings.Contains(punctuatedScanOut, "\"queued_files\": 1") {
		t.Fatalf("punctuated file path should scan as one path: %s", punctuatedScanOut)
	}
	importOut := app.toolKnowledgeImportFiles(map[string]interface{}{
		"file_paths":   []interface{}{directPath},
		"save_scope":   "personal",
		"include_exts": []interface{}{".md"},
	})
	if !strings.Contains(importOut, "\"ok\": true") || !strings.Contains(importOut, "imported_files") {
		t.Fatalf("unexpected direct file tool output: %s", importOut)
	}
	directSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "comet signal",
		"search_scope": "personal",
	})
	if !strings.Contains(directSearch, "\"ok\": true") || !strings.Contains(directSearch, "comet") {
		t.Fatalf("explicit file import was not searchable: %s", directSearch)
	}
	csvPath := filepath.Join(root, "table.csv")
	if err := os.WriteFile(csvPath, []byte("name,value\ncsvanchor,42\n"), 0o644); err != nil {
		t.Fatalf("write csv note: %v", err)
	}
	csvImportOut := app.toolKnowledgeImportFiles(map[string]interface{}{
		"file_paths":   []interface{}{csvPath},
		"save_scope":   "personal",
		"include_exts": []interface{}{".csv"},
	})
	if !strings.Contains(csvImportOut, "\"ok\": true") || !strings.Contains(csvImportOut, "imported_files") {
		t.Fatalf("unexpected csv import output: %s", csvImportOut)
	}
	csvSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "csvanchor",
		"search_scope": "personal",
		"source_kinds": []interface{}{"csv"},
	})
	if !strings.Contains(csvSearch, "\"ok\": true") || !strings.Contains(csvSearch, "csvanchor") || !strings.Contains(csvSearch, "\"row_range\"") {
		t.Fatalf("csv import was not searchable with row citation: %s", csvSearch)
	}
	gapStore, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("openKnowledgeStore for quality gap: %v", err)
	}
	gapSourceID := "ksrc_tool_quality_gap"
	if err := gapStore.SaveSource(context.Background(), knowledge.Source{
		ID:          gapSourceID,
		Kind:        knowledge.SourceKindText,
		URI:         "knowledge://text/tool-quality-gap",
		Title:       "Tool quality gap",
		ContentHash: "tool-quality-gap",
		Status:      knowledge.StatusParsed,
	}); err != nil {
		_ = gapStore.Close()
		t.Fatalf("SaveSource quality gap: %v", err)
	}
	if err := gapStore.SaveDocumentNode(context.Background(), knowledge.DocumentNode{
		SourceID:   gapSourceID,
		Type:       "document",
		Title:      "Tool quality gap node",
		Text:       "Quality gap anchor should be rebuilt into derived knowledge cards.",
		TokenCount: 16,
	}); err != nil {
		_ = gapStore.Close()
		t.Fatalf("SaveDocumentNode quality gap: %v", err)
	}
	sensitiveQualitySourceID := "ksrc_tool_quality_sensitive"
	if err := gapStore.SaveSource(context.Background(), knowledge.Source{
		ID:          sensitiveQualitySourceID,
		Kind:        knowledge.SourceKindText,
		URI:         "knowledge://text/tool-quality-sensitive",
		Title:       "Tool quality sensitive",
		ContentHash: "tool-quality-sensitive",
		Status:      knowledge.StatusParsed,
	}); err != nil {
		_ = gapStore.Close()
		t.Fatalf("SaveSource quality sensitive: %v", err)
	}
	if err := gapStore.SaveDocumentNode(context.Background(), knowledge.DocumentNode{
		SourceID:   sensitiveQualitySourceID,
		Type:       "document",
		Title:      "Tool quality sensitive node",
		Text:       "Temporary token = supersecretqualitytoolvalue should be isolated by quality governance.",
		TokenCount: 16,
	}); err != nil {
		_ = gapStore.Close()
		t.Fatalf("SaveDocumentNode quality sensitive: %v", err)
	}
	missingNodesSourceID := "ksrc_tool_quality_missing_nodes"
	if err := gapStore.SaveSource(context.Background(), knowledge.Source{
		ID:          missingNodesSourceID,
		Kind:        knowledge.SourceKindText,
		URI:         "knowledge://text/tool-quality-missing-nodes",
		Title:       "Tool quality missing nodes",
		ContentHash: "tool-quality-missing-nodes",
		Status:      knowledge.StatusDistilled,
	}); err != nil {
		_ = gapStore.Close()
		t.Fatalf("SaveSource quality missing nodes: %v", err)
	}
	_ = gapStore.Close()
	statsOut := app.toolKnowledgeStats(map[string]interface{}{})
	if !strings.Contains(statsOut, "\"ok\": true") || !strings.Contains(statsOut, "\"sources\"") || !strings.Contains(statsOut, "sources_by_kind") || !strings.Contains(statsOut, "import_items_by_status") {
		t.Fatalf("unexpected stats tool output: %s", statsOut)
	}
	healthOut := app.toolKnowledgeHealth(map[string]interface{}{"limit": []interface{}{"10"}, "include_plan": []interface{}{"true"}})
	if !strings.Contains(healthOut, "\"ok\": true") || !strings.Contains(healthOut, "\"health\"") || !strings.Contains(healthOut, "\"score\"") || !strings.Contains(healthOut, "\"quality_grades\"") || !strings.Contains(healthOut, "local_knowledge_health_no_llm") || !strings.Contains(healthOut, "\"maintenance_actions\"") || !strings.Contains(healthOut, "\"title\"") || !strings.Contains(healthOut, "\"signals\"") || !strings.Contains(healthOut, "\"executable\"") {
		t.Fatalf("unexpected health tool output: %s", healthOut)
	}
	var healthPayload map[string]interface{}
	if err := json.Unmarshal([]byte(healthOut), &healthPayload); err != nil {
		t.Fatalf("parse health output: %v\n%s", err, healthOut)
	}
	healthBody, ok := healthPayload["health"].(map[string]interface{})
	if !ok {
		t.Fatalf("health output missing health object: %s", healthOut)
	}
	actions, ok := healthBody["maintenance_actions"].([]interface{})
	if !ok {
		t.Fatalf("health output missing maintenance actions: %s", healthOut)
	}
	foundRefreshAction := false
	for _, raw := range actions {
		action, ok := raw.(map[string]interface{})
		if !ok || action["kind"] != "refresh_or_reimport_missing_nodes" {
			continue
		}
		foundRefreshAction = true
		if action["executable"] != true {
			t.Fatalf("expected missing-node refresh action to be executable: %#v", action)
		}
		if _, ok := action["manual_reason"]; ok {
			t.Fatalf("missing-node refresh action should not include manual reason: %#v", action)
		}
	}
	if !foundRefreshAction {
		t.Fatalf("health output missing missing-node refresh action: %s", healthOut)
	}
	doctorOut := app.toolKnowledgeDoctor(map[string]interface{}{})
	if !strings.Contains(doctorOut, "\"ok\": true") || !strings.Contains(doctorOut, "\"findings\"") || !strings.Contains(doctorOut, "sources_without_facts") || !strings.Contains(doctorOut, "\"filter\"") {
		t.Fatalf("unexpected doctor tool output: %s", doctorOut)
	}
	qualityOut := app.toolKnowledgeSourceQuality(map[string]interface{}{"limit": 10, "search_scope": "personal"})
	if !strings.Contains(qualityOut, "\"ok\": true") || !strings.Contains(qualityOut, "\"quality\"") || !strings.Contains(qualityOut, "local_source_quality_no_llm") || !strings.Contains(qualityOut, "\"score\"") {
		t.Fatalf("unexpected source quality tool output: %s", qualityOut)
	}
	poorQualityOut := app.toolKnowledgeSourceQuality(map[string]interface{}{"limit": 10, "search_scope": "personal", "quality_grade": "poor"})
	if !strings.Contains(poorQualityOut, "\"ok\": true") || !strings.Contains(poorQualityOut, "\"quality\"") {
		t.Fatalf("unexpected poor source quality tool output: %s", poorQualityOut)
	}
	qualityPlanOut := app.toolKnowledgeQualityMaintenancePlan(map[string]interface{}{"source_ids": []interface{}{gapSourceID, sensitiveQualitySourceID}, "limit": 10})
	if !strings.Contains(qualityPlanOut, "\"ok\": true") || !strings.Contains(qualityPlanOut, "\"plan\"") || !strings.Contains(qualityPlanOut, "local_quality_maintenance_plan_no_llm") || !strings.Contains(qualityPlanOut, "knowledge_disable_quality_sensitive_sources") || !strings.Contains(qualityPlanOut, "knowledge_rebuild_quality_gaps") {
		t.Fatalf("unexpected quality maintenance plan output: %s", qualityPlanOut)
	}
	qualityPoliciesOut := app.toolKnowledgeQualityMaintenancePolicies(map[string]interface{}{})
	if !strings.Contains(qualityPoliciesOut, "\"ok\": true") || !strings.Contains(qualityPoliciesOut, "\"policies\"") || !strings.Contains(qualityPoliciesOut, "\"enriched\"") || !strings.Contains(qualityPoliciesOut, "\"query_requires_llm\": false") || !strings.Contains(qualityPoliciesOut, "\"may_use_llm_for_structuring\": true") {
		t.Fatalf("unexpected quality maintenance policies output: %s", qualityPoliciesOut)
	}
	var qualityPoliciesPayload map[string]interface{}
	if err := json.Unmarshal([]byte(qualityPoliciesOut), &qualityPoliciesPayload); err != nil {
		t.Fatalf("parse quality policies output: %v\n%s", err, qualityPoliciesOut)
	}
	rawPolicies, ok := qualityPoliciesPayload["policies"].([]interface{})
	if !ok || len(rawPolicies) == 0 {
		t.Fatalf("quality policies output missing policies: %s", qualityPoliciesOut)
	}
	for _, raw := range rawPolicies {
		policy, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("malformed quality policy: %#v", raw)
		}
		rawActions, ok := policy["actions"].([]interface{})
		if !ok {
			t.Fatalf("quality policy missing actions: %#v", policy)
		}
		foundMissingNodeRefresh := false
		for _, action := range rawActions {
			if action == "refresh_or_reimport_missing_nodes" {
				foundMissingNodeRefresh = true
				break
			}
		}
		if !foundMissingNodeRefresh {
			t.Fatalf("quality policy %v should include missing-node refresh: %#v", policy["name"], rawActions)
		}
	}
	qualityExecutionPreviewOut := app.toolKnowledgeExecuteQualityMaintenancePlan(map[string]interface{}{"source_ids": []interface{}{gapSourceID, sensitiveQualitySourceID}, "limit": 10, "actions": []interface{}{"disable_sensitive_sources", "rebuild_derived_gaps"}, "dry_run": true})
	if !strings.Contains(qualityExecutionPreviewOut, "\"ok\": true") || !strings.Contains(qualityExecutionPreviewOut, "\"execution\"") || !strings.Contains(qualityExecutionPreviewOut, "local_quality_maintenance_execute_no_llm") || !strings.Contains(qualityExecutionPreviewOut, "\"dry_run\": true") {
		t.Fatalf("unexpected quality maintenance execution preview output: %s", qualityExecutionPreviewOut)
	}
	qualityPolicyPreviewOut := app.toolKnowledgeExecuteQualityMaintenancePlan(map[string]interface{}{"source_ids": []interface{}{gapSourceID}, "limit": 10, "policy": "enriched", "dry_run": true})
	if !strings.Contains(qualityPolicyPreviewOut, "\"ok\": true") || !strings.Contains(qualityPolicyPreviewOut, "policy_enriched") || !strings.Contains(qualityPolicyPreviewOut, "storage_may_use_llm_for_structuring") {
		t.Fatalf("unexpected quality policy preview output: %s", qualityPolicyPreviewOut)
	}
	qualityMissingPreviewOut := app.toolKnowledgeExecuteQualityMaintenancePlan(map[string]interface{}{"source_ids": []interface{}{missingNodesSourceID}, "limit": 10, "actions": []interface{}{"refresh_or_reimport_missing_nodes"}, "dry_run": true})
	var qualityMissingPreviewPayload map[string]interface{}
	if err := json.Unmarshal([]byte(qualityMissingPreviewOut), &qualityMissingPreviewPayload); err != nil {
		t.Fatalf("parse missing-node preview output: %v\n%s", err, qualityMissingPreviewOut)
	}
	previewExecution, ok := qualityMissingPreviewPayload["execution"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing-node preview output missing execution: %s", qualityMissingPreviewOut)
	}
	previewResults, ok := previewExecution["results"].([]interface{})
	if !ok || len(previewResults) != 1 {
		t.Fatalf("missing-node preview output missing action result: %s", qualityMissingPreviewOut)
	}
	previewAction, ok := previewResults[0].(map[string]interface{})
	if !ok || previewAction["kind"] != "refresh_or_reimport_missing_nodes" || previewAction["error"] != "refresh_or_reimport_preview_failed" {
		t.Fatalf("unexpected missing-node preview action result: %#v", previewResults[0])
	}
	previewResult, ok := previewAction["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing-node preview action missing nested preview result: %#v", previewAction)
	}
	previewFailures, ok := previewResult["failures"].([]interface{})
	if !ok || len(previewFailures) != 1 {
		t.Fatalf("missing-node preview result missing source failure: %#v", previewResult)
	}
	qualityMissingRefreshOut := app.toolKnowledgeExecuteQualityMaintenancePlan(map[string]interface{}{"source_ids": []interface{}{missingNodesSourceID}, "limit": 10, "actions": []interface{}{"refresh_or_reimport_missing_nodes"}, "dry_run": false})
	var qualityMissingRefreshPayload map[string]interface{}
	if err := json.Unmarshal([]byte(qualityMissingRefreshOut), &qualityMissingRefreshPayload); err != nil {
		t.Fatalf("parse missing-node refresh output: %v\n%s", err, qualityMissingRefreshOut)
	}
	execution, ok := qualityMissingRefreshPayload["execution"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing-node refresh output missing execution: %s", qualityMissingRefreshOut)
	}
	results, ok := execution["results"].([]interface{})
	if !ok || len(results) != 1 {
		t.Fatalf("missing-node refresh output missing action result: %s", qualityMissingRefreshOut)
	}
	refreshAction, ok := results[0].(map[string]interface{})
	if !ok || refreshAction["kind"] != "refresh_or_reimport_missing_nodes" || refreshAction["error"] != "refresh_or_reimport_failed" {
		t.Fatalf("unexpected missing-node refresh action result: %#v", results[0])
	}
	refreshResult, ok := refreshAction["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing-node refresh action missing nested refresh result: %#v", refreshAction)
	}
	failures, ok := refreshResult["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("missing-node refresh result missing source failure: %#v", refreshResult)
	}
	failure, ok := failures[0].(map[string]interface{})
	if !ok || failure["source_id"] != missingNodesSourceID {
		t.Fatalf("missing-node refresh failure missing source id: %#v", failures[0])
	}
	disableQualitySensitiveOut := app.toolKnowledgeDisableQualitySensitiveSources(map[string]interface{}{"source_ids": []interface{}{sensitiveQualitySourceID}, "limit": 10})
	if !strings.Contains(disableQualitySensitiveOut, "\"ok\": true") || !strings.Contains(disableQualitySensitiveOut, "\"candidate_count\": 1") || !strings.Contains(disableQualitySensitiveOut, "\"disabled\": true") || !strings.Contains(disableQualitySensitiveOut, "\"updated\": 1") || strings.Contains(disableQualitySensitiveOut, "supersecretqualitytoolvalue") {
		t.Fatalf("unexpected quality sensitive disable output: %s", disableQualitySensitiveOut)
	}
	backfillQualityLabelsOut := app.toolKnowledgeBackfillQualityLabels(map[string]interface{}{"source_ids": []interface{}{gapSourceID}, "limit": 10})
	if !strings.Contains(backfillQualityLabelsOut, "\"ok\": true") || !strings.Contains(backfillQualityLabelsOut, "\"candidate_count\": 1") || !strings.Contains(backfillQualityLabelsOut, "\"backfilled\": true") || !strings.Contains(backfillQualityLabelsOut, "\"updated\": 1") {
		t.Fatalf("unexpected quality label backfill output: %s", backfillQualityLabelsOut)
	}
	rebuildQualityGapOut := app.toolKnowledgeRebuildQualityGaps(map[string]interface{}{"source_ids": []interface{}{gapSourceID}, "limit": 10, "distill_mode": "rules_only"})
	if !strings.Contains(rebuildQualityGapOut, "\"ok\": true") || !strings.Contains(rebuildQualityGapOut, "\"candidate_count\": 1") || !strings.Contains(rebuildQualityGapOut, "\"rebuilt\": true") || !strings.Contains(rebuildQualityGapOut, "\"rebuilt\": 1") {
		t.Fatalf("unexpected quality gap rebuild output: %s", rebuildQualityGapOut)
	}
	capabilitiesOut := app.toolKnowledgeCapabilities(map[string]interface{}{})
	if !strings.Contains(capabilitiesOut, "\"ok\": true") ||
		!strings.Contains(capabilitiesOut, "\"query_requires_llm\": false") ||
		!strings.Contains(capabilitiesOut, "\"default_auto_labels\": true") ||
		(!strings.Contains(capabilitiesOut, "supported_text_extraction") &&
			!strings.Contains(capabilitiesOut, "supported_native") &&
			!strings.Contains(capabilitiesOut, "recognized_pending_converter") &&
			!strings.Contains(capabilitiesOut, "supported_with_local_converter")) {
		t.Fatalf("unexpected capabilities tool output: %s", capabilitiesOut)
	}
	if !strings.Contains(capabilitiesOut, "\"coverage_filters\"") || !strings.Contains(capabilitiesOut, "\"missing_cards\"") || !strings.Contains(capabilitiesOut, "\"pdf_ocr_needed\"") {
		t.Fatalf("capabilities output should expose source coverage filters: %s", capabilitiesOut)
	}
	if !strings.Contains(capabilitiesOut, "\"coverage_aliases\"") || !strings.Contains(capabilitiesOut, "\"rebuildcards\": \"missing_cards\"") || !strings.Contains(capabilitiesOut, "\"needsocr\": \"pdf_ocr_needed\"") {
		t.Fatalf("capabilities output should expose source coverage aliases: %s", capabilitiesOut)
	}
	policyUpdateOut := app.toolKnowledgeURLDomainPolicies(map[string]interface{}{"action": "replace", "allow_domains": []interface{}{"example.com"}, "block_domains": []interface{}{"blocked.example.com"}})
	if !strings.Contains(policyUpdateOut, "\"ok\": true") || !strings.Contains(policyUpdateOut, "blocked.example.com") {
		t.Fatalf("unexpected URL domain policy update output: %s", policyUpdateOut)
	}
	policyCheckOut := app.toolKnowledgeURLDomainPolicies(map[string]interface{}{"action": "check", "url": "https://other.example.org"})
	if !strings.Contains(policyCheckOut, "\"ok\": true") || !strings.Contains(policyCheckOut, "\"allowed\": false") || !strings.Contains(policyCheckOut, "no allow policy matched") {
		t.Fatalf("unexpected URL domain policy check output: %s", policyCheckOut)
	}
	if _, err := app.KnowledgeUpdateURLDomainPolicies(knowledge.URLDomainPolicyUpdateRequest{Replace: true}); err != nil {
		t.Fatalf("clear URL domain policies: %v", err)
	}
	maintainOut := app.toolKnowledgeMaintain(map[string]interface{}{})
	if !strings.Contains(maintainOut, "\"ok\": true") || !strings.Contains(maintainOut, "\"integrity_ok\": true") || !strings.Contains(maintainOut, "knowledge_cards_fts") {
		t.Fatalf("unexpected maintenance tool output: %s", maintainOut)
	}
	exportPath := filepath.Join(root, "knowledge-export.jsonl")
	exportOut := app.toolKnowledgeExportSnapshot(map[string]interface{}{"output_path": exportPath})
	if !strings.Contains(exportOut, "\"ok\": true") || !strings.Contains(exportOut, "\"format\": \"jsonl\"") || !strings.Contains(exportOut, "knowledge-export.jsonl") || strings.Contains(exportOut, "supersecretvalue") {
		t.Fatalf("unexpected export snapshot output: %s", exportOut)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported snapshot: %v", err)
	}
	if !strings.Contains(string(exported), "\"type\":\"source\"") || strings.Contains(string(exported), "supersecretvalue") {
		t.Fatalf("unexpected exported snapshot content: %s", string(exported))
	}
	importSnapshotOut := app.toolKnowledgeImportSnapshot(map[string]interface{}{"input_path": exportPath, "dry_run": true})
	if !strings.Contains(importSnapshotOut, "\"ok\": true") || !strings.Contains(importSnapshotOut, "\"dry_run\": true") || !strings.Contains(importSnapshotOut, "\"records\"") {
		t.Fatalf("unexpected import snapshot dry-run output: %s", importSnapshotOut)
	}
	importSnapshotPathAliasOut := app.toolKnowledgeImportSnapshot(map[string]interface{}{"path": []interface{}{exportPath}, "dry_run": []interface{}{"true"}})
	if !strings.Contains(importSnapshotPathAliasOut, "\"ok\": true") || !strings.Contains(importSnapshotPathAliasOut, "\"dry_run\": true") || !strings.Contains(importSnapshotPathAliasOut, "\"records\"") {
		t.Fatalf("unexpected import snapshot path alias dry-run output: %s", importSnapshotPathAliasOut)
	}
	overwritePreviewOut := app.toolKnowledgeImportSnapshot(map[string]interface{}{"input_path": exportPath, "dry_run": true, "overwrite": true})
	if !strings.Contains(overwritePreviewOut, "\"ok\": true") || !strings.Contains(overwritePreviewOut, "\"overwrite\": true") || !strings.Contains(overwritePreviewOut, "\"would_import\"") {
		t.Fatalf("unexpected import snapshot overwrite preview output: %s", overwritePreviewOut)
	}
	store, err := app.openKnowledgeStore()
	if err != nil {
		t.Fatalf("openKnowledgeStore: %v", err)
	}
	dupSources := []knowledge.Source{
		{ID: "ksrc_tool_dup_1", Kind: knowledge.SourceKindText, URI: "knowledge://text/tool-1", Title: "Tool duplicate one", ContentHash: "tool-dup-1", Status: knowledge.StatusDistilled},
		{ID: "ksrc_tool_dup_2", Kind: knowledge.SourceKindText, URI: "knowledge://text/tool-2", Title: "Tool duplicate two", ContentHash: "tool-dup-2", Status: knowledge.StatusDistilled},
	}
	for _, source := range dupSources {
		if err := store.SaveSource(context.Background(), source); err != nil {
			_ = store.Close()
			t.Fatalf("SaveSource duplicate: %v", err)
		}
		if err := store.SaveCard(context.Background(), knowledge.Card{SourceID: source.ID, Claim: "Repeated tool quality claim should be grouped for diagnostics", Title: source.Title}); err != nil {
			_ = store.Close()
			t.Fatalf("SaveCard duplicate: %v", err)
		}
	}
	_ = store.Close()
	duplicatesOut := app.toolKnowledgeListDuplicateCards(map[string]interface{}{"limit": 10})
	if !strings.Contains(duplicatesOut, "\"ok\": true") || !strings.Contains(duplicatesOut, "Repeated tool quality claim") || !strings.Contains(duplicatesOut, "\"count\": 1") {
		t.Fatalf("unexpected duplicate cards output: %s", duplicatesOut)
	}
	suppressOut := app.toolKnowledgeSuppressDuplicateCards(map[string]interface{}{"claim": []interface{}{"Repeated tool quality claim should be grouped for diagnostics"}})
	if !strings.Contains(suppressOut, "\"ok\": true") || !strings.Contains(suppressOut, "\"suppressed\": 1") || !strings.Contains(suppressOut, "\"kept_card_id\"") {
		t.Fatalf("unexpected suppress duplicate cards output: %s", suppressOut)
	}
	suppressedOut := app.toolKnowledgeListSuppressedCards(map[string]interface{}{"limit": 10})
	if !strings.Contains(suppressedOut, "\"ok\": true") || !strings.Contains(suppressedOut, "Repeated tool quality claim") {
		t.Fatalf("unexpected suppressed cards output: %s", suppressedOut)
	}
	suppressedCards, err := app.KnowledgeListSuppressedCards(10)
	if err != nil || len(suppressedCards) == 0 {
		t.Fatalf("KnowledgeListSuppressedCards: %v %#v", err, suppressedCards)
	}
	restoreOut := app.toolKnowledgeRestoreSuppressedCards(map[string]interface{}{"ids": []interface{}{suppressedCards[0].CardID}})
	if !strings.Contains(restoreOut, "\"ok\": true") || !strings.Contains(restoreOut, "\"restored\": 1") {
		t.Fatalf("unexpected restore suppressed cards output: %s", restoreOut)
	}
	bulkSuppressOut := app.toolKnowledgeSuppressDuplicateGroups(map[string]interface{}{"limit": 10})
	if !strings.Contains(bulkSuppressOut, "\"ok\": true") || !strings.Contains(bulkSuppressOut, "\"processed_groups\": 1") || !strings.Contains(bulkSuppressOut, "\"suppressed\": 1") {
		t.Fatalf("unexpected bulk suppress duplicate groups output: %s", bulkSuppressOut)
	}
	bulkRestoreOut := app.toolKnowledgeRestoreSuppressedCardsBulk(map[string]interface{}{"limit": 10, "reason_contains": "duplicate_card_claim_bulk"})
	if !strings.Contains(bulkRestoreOut, "\"ok\": true") || !strings.Contains(bulkRestoreOut, "\"requested\": 1") || !strings.Contains(bulkRestoreOut, "\"restored\": 1") {
		t.Fatalf("unexpected bulk restore suppressed cards output: %s", bulkRestoreOut)
	}
	qualityDuplicateSuppressOut := app.toolKnowledgeSuppressQualityDuplicateGroups(map[string]interface{}{"source_ids": []interface{}{dupSources[0].ID, dupSources[1].ID}, "limit": 10, "duplicate_limit": 10})
	if !strings.Contains(qualityDuplicateSuppressOut, "\"ok\": true") || !strings.Contains(qualityDuplicateSuppressOut, "\"candidate_count\": 2") || !strings.Contains(qualityDuplicateSuppressOut, "\"processed_groups\": 1") || !strings.Contains(qualityDuplicateSuppressOut, "\"suppressed\": 1") {
		t.Fatalf("unexpected quality duplicate suppress output: %s", qualityDuplicateSuppressOut)
	}
	listOut := app.toolKnowledgeListSources(map[string]interface{}{"limit": 10})
	if !strings.Contains(listOut, "\"ok\": true") || !strings.Contains(listOut, "\"sources\"") || !strings.Contains(listOut, "direct.md") || !strings.Contains(listOut, "\"node_count\"") || !strings.Contains(listOut, "\"card_count\"") {
		t.Fatalf("unexpected list sources tool output: %s", listOut)
	}
	filteredListOut := app.toolKnowledgeListSources(map[string]interface{}{"query": "direct", "kind": "markdown", "limit": 10})
	if !strings.Contains(filteredListOut, "\"ok\": true") || !strings.Contains(filteredListOut, "direct.md") || strings.Contains(filteredListOut, "notes.md") {
		t.Fatalf("unexpected filtered list sources tool output: %s", filteredListOut)
	}
	coverageListOut := app.toolKnowledgeListSources(map[string]interface{}{"coverage_filter": "has_cards", "limit": 10})
	if !strings.Contains(coverageListOut, "\"ok\": true") || !strings.Contains(coverageListOut, "direct.md") || !strings.Contains(coverageListOut, "\"card_count\"") {
		t.Fatalf("unexpected coverage list sources tool output: %s", coverageListOut)
	}
	coverageAliasListOut := app.toolKnowledgeListSources(map[string]interface{}{"coverage_filter": "hasCards", "limit": 10})
	if !strings.Contains(coverageAliasListOut, "\"ok\": true") || !strings.Contains(coverageAliasListOut, "direct.md") || !strings.Contains(coverageAliasListOut, "\"card_count\"") {
		t.Fatalf("unexpected coverage alias list sources tool output: %s", coverageAliasListOut)
	}
	batchesOut := app.toolKnowledgeListImportBatches(map[string]interface{}{"limit": 5})
	if !strings.Contains(batchesOut, "\"ok\": true") || !strings.Contains(batchesOut, importResult.BatchID) {
		t.Fatalf("unexpected import batches tool output: %s", batchesOut)
	}
	itemsOut := app.toolKnowledgeListImportItems(map[string]interface{}{"id": []interface{}{importResult.BatchID}, "limit": []interface{}{"10"}})
	if !strings.Contains(itemsOut, "\"ok\": true") || !strings.Contains(itemsOut, "notes.md") {
		t.Fatalf("unexpected import items tool output: %s", itemsOut)
	}
	retryPath := filepath.Join(root, "retry-tool.md")
	retrySeed, err := app.KnowledgeImportFiles(knowledge.DirectoryImportRequest{SaveScope: knowledge.SaveScopePersonal, IncludeExts: []string{".md"}, MaxFileBytes: 1024}, []string{retryPath})
	if err != nil || retrySeed.BatchID == "" || retrySeed.FailedFiles != 1 {
		t.Fatalf("seed retry batch: %v %#v", err, retrySeed)
	}
	if err := os.WriteFile(retryPath, []byte("Retry tool anchor should become searchable."), 0o644); err != nil {
		t.Fatalf("write retry tool note: %v", err)
	}
	retryOut := app.toolKnowledgeRetryImportBatch(map[string]interface{}{"id": retrySeed.BatchID})
	if !strings.Contains(retryOut, "\"ok\": true") || !strings.Contains(retryOut, "\"imported_files\": 1") {
		t.Fatalf("unexpected retry import batch output: %s", retryOut)
	}
	retrySearch := app.toolKnowledgeSearch(map[string]interface{}{"query": "Retry tool anchor", "search_scope": "personal"})
	if !strings.Contains(retrySearch, "\"ok\": true") || !strings.Contains(retrySearch, "Retry tool") {
		t.Fatalf("retry import should be searchable: %s", retrySearch)
	}
	latestSources, err := app.KnowledgeListSources(knowledge.ListSourcesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("KnowledgeListSources: %v", err)
	}
	var directSourceID string
	for _, source := range latestSources {
		if strings.Contains(source.RelativePath, "direct.md") {
			directSourceID = source.ID
			break
		}
	}
	if directSourceID == "" {
		t.Fatalf("expected direct.md source in %+v", latestSources)
	}
	scopedExportPath := filepath.Join(root, "knowledge-scoped-export.jsonl")
	scopedExportOut := app.toolKnowledgeExportSnapshot(map[string]interface{}{"output_path": scopedExportPath, "source_ids": []interface{}{directSourceID}})
	if !strings.Contains(scopedExportOut, "\"ok\": true") || !strings.Contains(scopedExportOut, "\"scoped\": true") || !strings.Contains(scopedExportOut, directSourceID) {
		t.Fatalf("unexpected scoped export snapshot output: %s", scopedExportOut)
	}
	idsAliasExportPath := filepath.Join(root, "knowledge-ids-alias-export.jsonl")
	idsAliasExportOut := app.toolKnowledgeExportSnapshot(map[string]interface{}{"output_path": idsAliasExportPath, "ids": []interface{}{directSourceID}})
	if !strings.Contains(idsAliasExportOut, "\"ok\": true") || !strings.Contains(idsAliasExportOut, "\"scoped\": true") || !strings.Contains(idsAliasExportOut, directSourceID) {
		t.Fatalf("unexpected ids alias scoped export snapshot output: %s", idsAliasExportOut)
	}
	scopedExported, err := os.ReadFile(scopedExportPath)
	if err != nil {
		t.Fatalf("read scoped exported snapshot: %v", err)
	}
	if !strings.Contains(string(scopedExported), directSourceID) || strings.Contains(string(scopedExported), "notes.md") {
		t.Fatalf("scoped export should only contain selected source: %s", string(scopedExported))
	}
	idsAliasExported, err := os.ReadFile(idsAliasExportPath)
	if err != nil {
		t.Fatalf("read ids alias scoped exported snapshot: %v", err)
	}
	if !strings.Contains(string(idsAliasExported), directSourceID) || strings.Contains(string(idsAliasExported), "notes.md") {
		t.Fatalf("ids alias scoped export should only contain selected source: %s", string(idsAliasExported))
	}
	detailOut := app.toolKnowledgeSourceDetail(map[string]interface{}{"source_id": directSourceID, "limit": 10})
	if !strings.Contains(detailOut, "\"ok\": true") || !strings.Contains(detailOut, "\"source\"") || !strings.Contains(detailOut, "\"nodes\"") || !strings.Contains(detailOut, "\"cards\"") || !strings.Contains(detailOut, "\"versions\"") || !strings.Contains(detailOut, "\"neighborhood\"") || !strings.Contains(detailOut, "local_source_neighborhood_no_llm") || !strings.Contains(detailOut, "direct.md") || !strings.Contains(detailOut, "comet") {
		t.Fatalf("unexpected source detail tool output: %s", detailOut)
	}
	detailIDAliasOut := app.toolKnowledgeSourceDetail(map[string]interface{}{"id": []interface{}{directSourceID}, "limit": []interface{}{"10"}})
	if !strings.Contains(detailIDAliasOut, "\"ok\": true") || !strings.Contains(detailIDAliasOut, directSourceID) {
		t.Fatalf("unexpected source detail id alias output: %s", detailIDAliasOut)
	}
	versionsOut := app.toolKnowledgeListSourceVersions(map[string]interface{}{"source_id": directSourceID, "limit": 10})
	if !strings.Contains(versionsOut, "\"ok\": true") || !strings.Contains(versionsOut, "\"versions\"") || !strings.Contains(versionsOut, "\"import\"") {
		t.Fatalf("unexpected source versions tool output: %s", versionsOut)
	}
	rebuildOut := app.toolKnowledgeRebuildSourceDerived(map[string]interface{}{"source_id": directSourceID, "distill_mode": "rules_only"})
	if !strings.Contains(rebuildOut, "\"ok\": true") || !strings.Contains(rebuildOut, "\"card_count\"") {
		t.Fatalf("unexpected source rebuild output: %s", rebuildOut)
	}
	rebuildFilterOut := app.toolKnowledgeRebuildSourcesDerivedByFilter(map[string]interface{}{"query": "direct", "kind": "markdown", "limit": 10, "distill_mode": "rules_only"})
	if !strings.Contains(rebuildFilterOut, "\"ok\": true") || !strings.Contains(rebuildFilterOut, "\"rebuilt\": 1") {
		t.Fatalf("unexpected filtered source rebuild output: %s", rebuildFilterOut)
	}
	updateMetaOut := app.toolKnowledgeUpdateSourceMetadata(map[string]interface{}{"source_id": []interface{}{directSourceID}, "title": []interface{}{"Direct source governed"}, "topic_hint": []interface{}{"governed comet topic"}, "source_trust": []interface{}{"0.92"}, "labels": []interface{}{"governed", "comet"}})
	if !strings.Contains(updateMetaOut, "\"ok\": true") || !strings.Contains(updateMetaOut, "Direct source governed") || !strings.Contains(updateMetaOut, "governed comet topic") || !strings.Contains(updateMetaOut, "\"source_trust\": 0.92") || !strings.Contains(updateMetaOut, "\"governed\"") {
		t.Fatalf("unexpected update source metadata output: %s", updateMetaOut)
	}
	labeledListOut := app.toolKnowledgeListSources(map[string]interface{}{"labels": []interface{}{"governed"}, "limit": 10})
	if !strings.Contains(labeledListOut, "\"ok\": true") || !strings.Contains(labeledListOut, directSourceID) || !strings.Contains(labeledListOut, "\"governed\"") {
		t.Fatalf("unexpected labeled source list output: %s", labeledListOut)
	}
	labelListOut := app.toolKnowledgeListSourceLabels(map[string]interface{}{"query": "Direct source", "limit": 10})
	if !strings.Contains(labelListOut, "\"ok\": true") || !strings.Contains(labelListOut, "\"governed\"") || !strings.Contains(labelListOut, "\"comet\"") {
		t.Fatalf("unexpected source label list output: %s", labelListOut)
	}
	labelDryRunOut := app.toolKnowledgeUpdateSourceLabels(map[string]interface{}{"query": "Direct source", "kind": "markdown", "add_labels": []interface{}{"bulk"}, "dry_run": true, "limit": 10})
	if !strings.Contains(labelDryRunOut, "\"ok\": true") || !strings.Contains(labelDryRunOut, "\"dry_run\": true") || !strings.Contains(labelDryRunOut, "\"bulk\"") {
		t.Fatalf("unexpected source label dry-run output: %s", labelDryRunOut)
	}
	labelUpdateOut := app.toolKnowledgeUpdateSourceLabels(map[string]interface{}{"source_ids": []interface{}{directSourceID}, "add_labels": []interface{}{"bulk"}, "remove_labels": []interface{}{"comet"}, "limit": 10})
	if !strings.Contains(labelUpdateOut, "\"ok\": true") || !strings.Contains(labelUpdateOut, "\"updated\": 1") || !strings.Contains(labelUpdateOut, "\"bulk\"") {
		t.Fatalf("unexpected source label update output: %s", labelUpdateOut)
	}
	labelRenameOut := app.toolKnowledgeUpdateSourceLabels(map[string]interface{}{"rename_from": "bulk", "rename_to": "curated", "limit": 10})
	if !strings.Contains(labelRenameOut, "\"ok\": true") || !strings.Contains(labelRenameOut, "\"mode\": \"rename\"") || !strings.Contains(labelRenameOut, "\"curated\"") {
		t.Fatalf("unexpected source label rename output: %s", labelRenameOut)
	}
	autoBackfillOut := app.toolKnowledgeBackfillSourceAutoLabels(map[string]interface{}{"source_ids": []interface{}{directSourceID}, "dry_run": true, "limit": 10})
	if !strings.Contains(autoBackfillOut, "\"ok\": true") || !strings.Contains(autoBackfillOut, "\"mode\": \"backfill_auto_labels\"") || !strings.Contains(autoBackfillOut, "\"kind:markdown\"") {
		t.Fatalf("unexpected source auto-label backfill output: %s", autoBackfillOut)
	}
	if err := os.WriteFile(directPath, []byte("Direct nova signal refreshed through bulk source refresh."), 0o644); err != nil {
		t.Fatalf("rewrite direct note: %v", err)
	}
	refreshManyOut := app.toolKnowledgeRefreshSources(map[string]interface{}{"source_ids": []interface{}{directSourceID}})
	if !strings.Contains(refreshManyOut, "\"ok\": true") || !strings.Contains(refreshManyOut, "\"refreshed\": 1") {
		t.Fatalf("unexpected bulk refresh output: %s", refreshManyOut)
	}
	refreshedVersionsOut := app.toolKnowledgeListSourceVersions(map[string]interface{}{"source_id": directSourceID, "limit": 10})
	if !strings.Contains(refreshedVersionsOut, "\"ok\": true") || !strings.Contains(refreshedVersionsOut, "\"refresh\"") || !strings.Contains(refreshedVersionsOut, "\"version_count\"") {
		t.Fatalf("unexpected refreshed source versions output: %s", refreshedVersionsOut)
	}
	refreshedSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "nova signal",
		"search_scope": "personal",
	})
	if !strings.Contains(refreshedSearch, "\"ok\": true") || !strings.Contains(refreshedSearch, "nova") {
		t.Fatalf("bulk refreshed source should be searchable: %s", refreshedSearch)
	}
	refreshFilteredOut := app.toolKnowledgeRefreshSourcesByFilter(map[string]interface{}{"query": "direct", "kind": "markdown", "limit": 10})
	if !strings.Contains(refreshFilteredOut, "\"ok\": true") || !strings.Contains(refreshFilteredOut, "\"requested\": 1") || !strings.Contains(refreshFilteredOut, "\"refreshed\": 1") {
		t.Fatalf("unexpected refresh by filter output: %s", refreshFilteredOut)
	}
	refreshIDsAliasOut := app.toolKnowledgeRefreshSourcesByFilter(map[string]interface{}{"ids": []interface{}{directSourceID}, "limit": 10})
	if !strings.Contains(refreshIDsAliasOut, "\"ok\": true") || !strings.Contains(refreshIDsAliasOut, "\"requested\": 1") || !strings.Contains(refreshIDsAliasOut, "\"refreshed\": 1") {
		t.Fatalf("unexpected refresh by filter ids alias output: %s", refreshIDsAliasOut)
	}
	disableFilteredOut := app.toolKnowledgeDisableSourcesByFilter(map[string]interface{}{"query": "direct", "kind": "markdown", "limit": 10})
	if !strings.Contains(disableFilteredOut, "\"ok\": true") || !strings.Contains(disableFilteredOut, "\"requested\": 1") || !strings.Contains(disableFilteredOut, "\"updated\": 1") {
		t.Fatalf("unexpected disable by filter output: %s", disableFilteredOut)
	}
	enableFilteredOut := app.toolKnowledgeEnableSourcesByFilter(map[string]interface{}{"ids": []interface{}{directSourceID}, "status": "disabled", "kind": "markdown", "limit": 10})
	if !strings.Contains(enableFilteredOut, "\"ok\": true") || !strings.Contains(enableFilteredOut, "\"requested\": 1") || !strings.Contains(enableFilteredOut, "\"updated\": 1") {
		t.Fatalf("unexpected enable by filter output: %s", enableFilteredOut)
	}
	disableOut := app.toolKnowledgeDisableSource(map[string]interface{}{"source_id": directSourceID})
	if !strings.Contains(disableOut, "\"ok\": true") || !strings.Contains(disableOut, "\"disabled\": true") || !strings.Contains(disableOut, "\"status\": \"disabled\"") {
		t.Fatalf("unexpected disable source tool output: %s", disableOut)
	}
	disabledSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "nova signal",
		"search_scope": "personal",
	})
	if !strings.Contains(disabledSearch, "\"ok\": true") || !strings.Contains(disabledSearch, "\"count\": 0") {
		t.Fatalf("disabled source should be excluded from default search: %s", disabledSearch)
	}
	includeDisabledSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":            "nova signal",
		"search_scope":     "personal",
		"include_disabled": true,
	})
	if !strings.Contains(includeDisabledSearch, "\"ok\": true") || !strings.Contains(includeDisabledSearch, "nova") {
		t.Fatalf("include disabled search should surface disabled source: %s", includeDisabledSearch)
	}
	enableOut := app.toolKnowledgeEnableSource(map[string]interface{}{"source_id": directSourceID})
	if !strings.Contains(enableOut, "\"ok\": true") || !strings.Contains(enableOut, "\"enabled\": true") || !strings.Contains(enableOut, "\"status\": \"distilled\"") {
		t.Fatalf("unexpected enable source tool output: %s", enableOut)
	}
	enabledSearch := app.toolKnowledgeSearch(map[string]interface{}{
		"query":        "nova signal",
		"search_scope": "personal",
	})
	if !strings.Contains(enabledSearch, "\"ok\": true") || !strings.Contains(enabledSearch, "nova") {
		t.Fatalf("enabled source should return to default search: %s", enabledSearch)
	}
}
