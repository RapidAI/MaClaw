package mcp

import "testing"

func TestRejectSchemaFailsClosed(t *testing.T) {
	if err := RejectSchema(nil); err == nil {
		t.Fatal("nil schema")
	}
	if err := RejectSchema(map[string]interface{}{}); err == nil {
		t.Fatal("empty schema")
	}
	if err := RejectSchema(map[string]interface{}{"type": "string"}); err == nil {
		t.Fatal("non-object")
	}
	err := RejectSchema(map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"q"},
	})
	if err == nil {
		t.Fatal("required without properties")
	}
	ok := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"q": map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"q"},
	}
	if err := RejectSchema(ok); err != nil {
		t.Fatal(err)
	}
	if err := RejectUnknownArgs(ok, map[string]interface{}{"other": "x"}); err == nil {
		t.Fatal("unknown argument")
	}
	if errs := ValidateArgs(ok, map[string]interface{}{"q": 1}); len(errs) == 0 {
		t.Fatal("number passed for a string")
	}
}
