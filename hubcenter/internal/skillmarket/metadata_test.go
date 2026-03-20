package skillmarket

import (
	"reflect"
	"testing"
)

// ── Task 2.2: SkillMetadata round-trip 属性测试 ─────────────────────────

func TestMetadata_RoundTrip(t *testing.T) {
	original := &SkillMetadata{
		Name:        "test-skill",
		Description: "A test skill for unit testing",
		Tags:        []string{"test", "automation"},
		Triggers:    []string{"run test", "execute test"},
		Version:     "1.0.0",
		Author:      "dev@example.com",
		Price:       10,
		Permissions: []string{"network_access"},
		RequiredEnv: []string{"API_KEY"},
		PricingMode: "auto",
		Extra:       map[string]any{"custom_field": "custom_value", "priority": 5},
	}

	// Format → Parse → 比较
	data, err := FormatSkillYAML(original)
	if err != nil {
		t.Fatalf("FormatSkillYAML: %v", err)
	}

	parsed, err := ParseSkillYAML(data)
	if err != nil {
		t.Fatalf("ParseSkillYAML: %v", err)
	}

	if parsed.Name != original.Name {
		t.Errorf("Name: got %q, want %q", parsed.Name, original.Name)
	}
	if parsed.Description != original.Description {
		t.Errorf("Description mismatch")
	}
	if !reflect.DeepEqual(parsed.Tags, original.Tags) {
		t.Errorf("Tags: got %v, want %v", parsed.Tags, original.Tags)
	}
	if !reflect.DeepEqual(parsed.Triggers, original.Triggers) {
		t.Errorf("Triggers mismatch")
	}
	if parsed.Price != original.Price {
		t.Errorf("Price: got %d, want %d", parsed.Price, original.Price)
	}
	if !reflect.DeepEqual(parsed.Permissions, original.Permissions) {
		t.Errorf("Permissions mismatch")
	}
	if !reflect.DeepEqual(parsed.RequiredEnv, original.RequiredEnv) {
		t.Errorf("RequiredEnv mismatch")
	}
	// Extra 字段保留
	if parsed.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if parsed.Extra["custom_field"] != "custom_value" {
		t.Errorf("Extra[custom_field]: got %v, want custom_value", parsed.Extra["custom_field"])
	}
}

func TestMetadata_RoundTrip_MinimalFields(t *testing.T) {
	original := &SkillMetadata{
		Name:        "minimal",
		Description: "minimal skill",
	}

	data, err := FormatSkillYAML(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSkillYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "minimal" || parsed.Description != "minimal skill" {
		t.Errorf("minimal round-trip failed: name=%q desc=%q", parsed.Name, parsed.Description)
	}
}

// ── Task 2.4: 验证器单元测试 ────────────────────────────────────────────

func TestValidateMetadata_Valid(t *testing.T) {
	meta := &SkillMetadata{Name: "test", Description: "desc", Price: 10}
	errs := ValidateMetadata(meta)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateMetadata_MissingName(t *testing.T) {
	meta := &SkillMetadata{Description: "desc"}
	errs := ValidateMetadata(meta)
	if len(errs) == 0 {
		t.Error("expected error for missing name")
	}
}

func TestValidateMetadata_MissingDescription(t *testing.T) {
	meta := &SkillMetadata{Name: "test"}
	errs := ValidateMetadata(meta)
	if len(errs) == 0 {
		t.Error("expected error for missing description")
	}
}

func TestValidateMetadata_NegativePrice(t *testing.T) {
	meta := &SkillMetadata{Name: "test", Description: "desc", Price: -1}
	errs := ValidateMetadata(meta)
	found := false
	for _, e := range errs {
		if e == "price must be non-negative" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for negative price")
	}
}

func TestParseSkillYAML_Invalid(t *testing.T) {
	_, err := ParseSkillYAML([]byte(":::invalid yaml"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseSkillYAML_Empty(t *testing.T) {
	_, err := ParseSkillYAML([]byte(""))
	if err == nil {
		t.Error("expected error for empty document")
	}
}
