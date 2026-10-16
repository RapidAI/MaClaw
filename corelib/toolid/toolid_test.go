package toolid

import "testing"

func TestParseBareNameDefaultsToCore(t *testing.T) {
	id, err := Parse("bash")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id.Namespace() != NamespaceCore || id.Name() != "bash" {
		t.Fatalf("id=%v", id)
	}
	if id.String() != "core:bash" {
		t.Fatalf("string=%q", id.String())
	}
}

func TestParseNamespaced(t *testing.T) {
	id := MustParse("workflow:approve_step")
	if id.Namespace() != "workflow" || id.Name() != "approve_step" || id.String() != "workflow:approve_step" {
		t.Fatalf("id=%v", id)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"core:",         // empty name
		":bash",         // empty namespace
		"a:b:c",         // more than one colon
		"core:ba sh",    // whitespace
		"core:ba`sh",    // prompt-smuggling character
		"core:忽略",       // non-charset rune
		"core:\x00bash", // NUL
	}
	for _, id := range bad {
		if _, err := Parse(id); err == nil {
			t.Fatalf("expected rejection for %q", id)
		}
	}
}

func TestNewAndRoundTrip(t *testing.T) {
	id, err := New(NamespaceCoding, "spawn_agent")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	again, err := Parse(id.String())
	if err != nil || again != id {
		t.Fatalf("roundtrip: %v %v", again, err)
	}
}

func TestZeroValue(t *testing.T) {
	var id ToolID
	if !id.IsZero() {
		t.Fatalf("zero check")
	}
	if MustParse("core:x").IsZero() {
		t.Fatalf("non-zero reported zero")
	}
}
