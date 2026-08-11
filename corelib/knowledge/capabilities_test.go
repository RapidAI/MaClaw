package knowledge

import (
	"strings"
	"testing"
)

func TestCapabilitiesExposeLocalQueryAndAutoLabelDefaults(t *testing.T) {
	caps := Capabilities()
	if caps.QueryRequiresLLM {
		t.Fatalf("query should not require LLM: %#v", caps)
	}
	if !caps.WriteLLMOptional {
		t.Fatalf("write-time LLM should be optional: %#v", caps)
	}
	if !caps.DefaultAutoLabels {
		t.Fatalf("auto labels should default on for product entry points: %#v", caps)
	}
	if !caps.ImageRetrieval.TextToImage || caps.ImageRetrieval.ImageToImage {
		t.Fatalf("unexpected image retrieval capabilities: %#v", caps.ImageRetrieval)
	}
	if caps.ImageRetrieval.SearchEndpoint != "/api/v1/knowledge/images/search" || caps.ImageRetrieval.AgentTool != "knowledge_image_search" {
		t.Fatalf("image search surface missing: %#v", caps.ImageRetrieval)
	}
	if !stringSliceContains(caps.ImageRetrieval.IndexEvidence, "ocr") || !stringSliceContains(caps.ImageRetrieval.IndexEvidence, "vision_caption") || caps.ImageRetrieval.ImageToImageReason == "" {
		t.Fatalf("image retrieval evidence/limit missing: %#v", caps.ImageRetrieval)
	}
	if !stringSliceContains(caps.AutoLabelRules, "kind:*") || !stringSliceContains(caps.AutoLabelRules, "scope:*") {
		t.Fatalf("expected core auto-label rules: %#v", caps.AutoLabelRules)
	}
	if !stringSliceContains(caps.DefaultIncludeExts, ".pdf") || !stringSliceContains(caps.DefaultIncludeExts, ".docx") {
		t.Fatalf("expected document formats in capabilities: %#v", caps.DefaultIncludeExts)
	}
	if !stringSliceContains(caps.DefaultIncludeExts, ".ppt") {
		t.Fatalf("expected legacy PowerPoint in capabilities: %#v", caps.DefaultIncludeExts)
	}
	if !stringSliceContains(caps.CoverageFilters, "missing_cards") || !stringSliceContains(caps.CoverageFilters, "pdf_ocr_needed") {
		t.Fatalf("expected source coverage filters in capabilities: %#v", caps.CoverageFilters)
	}
	if len(caps.CoverageFilters) != len(canonicalCoverageFilters) {
		t.Fatalf("capabilities coverage filters should match canonical list: %#v", caps.CoverageFilters)
	}
	seenCoverageFilters := make(map[string]struct{}, len(canonicalCoverageFilters))
	for i, filter := range canonicalCoverageFilters {
		if filter == "" {
			t.Fatalf("canonical coverage filter at %d is empty", i)
		}
		if normalizeCoverageFilterKey(filter) != filter {
			t.Fatalf("canonical coverage filter %q should be stored in normalized form", filter)
		}
		if _, ok := seenCoverageFilters[filter]; ok {
			t.Fatalf("duplicate canonical coverage filter %q in %#v", filter, canonicalCoverageFilters)
		}
		seenCoverageFilters[filter] = struct{}{}
		if caps.CoverageFilters[i] != filter {
			t.Fatalf("capabilities coverage filter order mismatch at %d: got %q want %q", i, caps.CoverageFilters[i], filter)
		}
		if !stringSliceContains(caps.CoverageFilters, filter) {
			t.Fatalf("capabilities missing canonical coverage filter %q: %#v", filter, caps.CoverageFilters)
		}
		if normalizeCoverageFilter(filter) != filter {
			t.Fatalf("canonical coverage filter %q should normalize to itself", filter)
		}
		if _, ok := coverageFilterAliases[filter]; ok {
			t.Fatalf("canonical coverage filter %q should not also be an alias", filter)
		}
	}
	if caps.CoverageAliases["rebuild_cards"] != "missing_cards" || caps.CoverageAliases["facts_rebuildable"] != "missing_facts" {
		t.Fatalf("expected rebuild coverage aliases in capabilities: %#v", caps.CoverageAliases)
	}
	if caps.CoverageAliases["rebuildcards"] != "missing_cards" || caps.CoverageAliases["needsocr"] != "pdf_ocr_needed" || caps.CoverageAliases["haslinks"] != "has_links" {
		t.Fatalf("expected compact coverage aliases in capabilities: %#v", caps.CoverageAliases)
	}
	if normalizeCoverageFilter("rebuild_cards") != caps.CoverageAliases["rebuild_cards"] || normalizeCoverageFilter("facts_rebuildable") != caps.CoverageAliases["facts_rebuildable"] {
		t.Fatalf("coverage aliases should match normalizer: %#v", caps.CoverageAliases)
	}
	if len(caps.CoverageAliases) != len(coverageFilterAliases) {
		t.Fatalf("capabilities coverage aliases should match canonical alias list: %#v", caps.CoverageAliases)
	}
	for alias, canonical := range coverageFilterAliases {
		if alias == "" || canonical == "" {
			t.Fatalf("coverage alias entries must be non-empty: %q -> %q", alias, canonical)
		}
		if normalizeCoverageFilterKey(alias) != alias {
			t.Fatalf("coverage alias key %q should be stored in normalized form", alias)
		}
		if caps.CoverageAliases[alias] != canonical {
			t.Fatalf("capabilities alias %q = %q, want %q", alias, caps.CoverageAliases[alias], canonical)
		}
		if normalizeCoverageFilter(alias) != canonical {
			t.Fatalf("normalizer alias %q = %q, want %q", alias, normalizeCoverageFilter(alias), canonical)
		}
		if !stringSliceContains(canonicalCoverageFilters, canonical) {
			t.Fatalf("coverage alias %q points at unknown canonical filter %q", alias, canonical)
		}
	}
	caps.CoverageFilters[0] = "mutated"
	caps.CoverageAliases["rebuild_cards"] = "mutated"
	next := Capabilities()
	if next.CoverageFilters[0] == "mutated" || next.CoverageAliases["rebuild_cards"] == "mutated" {
		t.Fatalf("capabilities should return defensive coverage copies: %#v", next)
	}
}

func TestCapabilitiesDescribeKnowledgeOfficeReadAndNativeFallbackBoundaries(t *testing.T) {
	caps := Capabilities()
	byKind := make(map[string]FormatCapability, len(caps.Formats))
	for _, format := range caps.Formats {
		byKind[format.Kind] = format
	}

	for _, kind := range []string{SourceKindDOC, SourceKindDOCX, SourceKindPPT, SourceKindPPTX, SourceKindXLS, SourceKindXLSX} {
		format, ok := byKind[kind]
		if !ok {
			t.Fatalf("missing Office capability for %q: %#v", kind, caps.Formats)
		}
		if !strings.Contains(format.Parser, "officeread_structured_markdown_opt_in") {
			t.Fatalf("%s parser must disclose the OfficeRead rich-content opt-in: %#v", kind, format)
		}
	}

	if got := byKind[SourceKindPPT]; got.Status != "staged_opt_in" || !strings.Contains(got.Notes, "no native PPT parser") || !strings.Contains(got.Notes, "Chat/read_document") {
		t.Fatalf("PPT capability must distinguish knowledge opt-in from chat text extraction: %#v", got)
	}
	for _, kind := range []string{SourceKindDOC, SourceKindXLS} {
		if got := byKind[kind]; got.Status != "supported_native" || !strings.Contains(got.Notes, "fallback") {
			t.Fatalf("%s capability must retain its native knowledge fallback: %#v", kind, got)
		}
	}
}

func TestNormalizeCoverageFilterCanonicalAndAliases(t *testing.T) {
	for _, filter := range canonicalCoverageFilters {
		if got := normalizeCoverageFilter("  " + filter + "  "); got != filter {
			t.Fatalf("canonical filter %q normalized to %q", filter, got)
		}
	}
	cases := map[string]string{
		" Rebuild_Cards ":     "missing_cards",
		"rebuild-cards":       "missing_cards",
		"rebuild__cards":      "missing_cards",
		"rebuild.cards":       "missing_cards",
		"rebuild/cards":       "missing_cards",
		"rebuild cards":       "missing_cards",
		"rebuild\tcards":      "missing_cards",
		"rebuild\ncards":      "missing_cards",
		"rebuildCards":        "missing_cards",
		"cards_rebuildable":   "missing_cards",
		"cards rebuildable":   "missing_cards",
		"cardsRebuildable":    "missing_cards",
		"facts_rebuildable":   "missing_facts",
		"facts-rebuildable":   "missing_facts",
		"factsRebuildable":    "missing_facts",
		"WITHOUT_LABELS":      "missing_labels",
		"without labels":      "missing_labels",
		"missingLabels":       "missing_labels",
		"needs_ocr":           "pdf_ocr_needed",
		"needs ocr":           "pdf_ocr_needed",
		"needs   ocr":         "pdf_ocr_needed",
		"needsOCR":            "pdf_ocr_needed",
		"pdfOCRNeeded":        "pdf_ocr_needed",
		"linked":              "has_links",
		"hasLinks":            "has_links",
		"unknown_filter_name": "",
	}
	for input, want := range cases {
		if got := normalizeCoverageFilter(input); got != want {
			t.Fatalf("normalizeCoverageFilter(%q) = %q, want %q", input, got, want)
		}
	}
}
