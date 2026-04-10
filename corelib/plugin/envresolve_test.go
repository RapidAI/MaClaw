package plugin

import (
	"os"
	"testing"
)

// Task 3.2: 为环境变量解析编写单元测试

func TestResolveEnvVars_BasicReplace(t *testing.T) {
	os.Setenv("TEST_PLUGIN_KEY", "secret123")
	defer os.Unsetenv("TEST_PLUGIN_KEY")

	settings := map[string]interface{}{
		"api_key": "${TEST_PLUGIN_KEY}",
		"name":    "static",
	}
	got := ResolveEnvVars(settings)
	if got["api_key"] != "secret123" {
		t.Errorf("api_key = %v, want %q", got["api_key"], "secret123")
	}
	if got["name"] != "static" {
		t.Errorf("name = %v, want %q", got["name"], "static")
	}
}

func TestResolveEnvVars_NestedMap(t *testing.T) {
	os.Setenv("TEST_NESTED_VAR", "nested_value")
	defer os.Unsetenv("TEST_NESTED_VAR")

	settings := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": "${TEST_NESTED_VAR}",
		},
	}
	got := ResolveEnvVars(settings)
	outer := got["outer"].(map[string]interface{})
	if outer["inner"] != "nested_value" {
		t.Errorf("inner = %v, want %q", outer["inner"], "nested_value")
	}
}

func TestResolveEnvVars_SliceReplace(t *testing.T) {
	os.Setenv("TEST_SLICE_VAR", "replaced")
	defer os.Unsetenv("TEST_SLICE_VAR")

	settings := map[string]interface{}{
		"items": []interface{}{"static", "${TEST_SLICE_VAR}"},
	}
	got := ResolveEnvVars(settings)
	items := got["items"].([]interface{})
	if items[0] != "static" || items[1] != "replaced" {
		t.Errorf("items = %v", items)
	}
}

func TestResolveEnvVars_UnsetVarKeptUnchanged(t *testing.T) {
	os.Unsetenv("NONEXISTENT_PLUGIN_VAR")

	settings := map[string]interface{}{
		"key": "${NONEXISTENT_PLUGIN_VAR}",
	}
	got := ResolveEnvVars(settings)
	if got["key"] != "${NONEXISTENT_PLUGIN_VAR}" {
		t.Errorf("key = %v, want original placeholder", got["key"])
	}
}

func TestResolveEnvVars_NilInput(t *testing.T) {
	got := ResolveEnvVars(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestResolveEnvVars_NonStringValues(t *testing.T) {
	settings := map[string]interface{}{
		"count":   42,
		"enabled": true,
		"ratio":   3.14,
	}
	got := ResolveEnvVars(settings)
	if got["count"] != 42 || got["enabled"] != true {
		t.Errorf("non-string values changed: %v", got)
	}
}

func TestResolveEnvVars_DoesNotMutateInput(t *testing.T) {
	os.Setenv("TEST_MUTATE_VAR", "new")
	defer os.Unsetenv("TEST_MUTATE_VAR")

	original := map[string]interface{}{
		"key": "${TEST_MUTATE_VAR}",
	}
	_ = ResolveEnvVars(original)
	if original["key"] != "${TEST_MUTATE_VAR}" {
		t.Error("input map was mutated")
	}
}

func TestResolveEnvVars_MultipleVarsInOneString(t *testing.T) {
	os.Setenv("TEST_HOST", "localhost")
	os.Setenv("TEST_PORT", "8080")
	defer os.Unsetenv("TEST_HOST")
	defer os.Unsetenv("TEST_PORT")

	settings := map[string]interface{}{
		"url": "http://${TEST_HOST}:${TEST_PORT}/api",
	}
	got := ResolveEnvVars(settings)
	if got["url"] != "http://localhost:8080/api" {
		t.Errorf("url = %v", got["url"])
	}
}
