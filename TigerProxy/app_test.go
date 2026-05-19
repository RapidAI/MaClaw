package main

import "testing"

func TestNormalizeListenAddressDefaultsToAllInterfaces(t *testing.T) {
	cases := map[string]string{
		"":            "0.0.0.0:18086",
		":18090":      "0.0.0.0:18090",
		"18091":       "0.0.0.0:18091",
		"localhost:7": "0.0.0.0:7",
		"*:18092":     "0.0.0.0:18092",
	}

	for in, want := range cases {
		got, err := normalizeListenAddress(in)
		if err != nil {
			t.Fatalf("normalizeListenAddress(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("normalizeListenAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeModelOptionsStripsProviderPrefixAndDeduplicates(t *testing.T) {
	models := normalizeModelOptions([]ModelOption{
		{ID: "qax-codegen/Qwen-Flash", Name: "Qwen Flash", ContextWindow: 1000},
		{ID: "Qwen-Flash", Name: "duplicate", ContextWindow: 2000},
		{ID: " qax-codegen/Claude-Sonnet ", Name: "", ContextWindow: 3000},
		{ID: "", Name: "ignored"},
	})

	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "Qwen-Flash" || models[0].Name != "Qwen Flash" || models[0].ContextWindow != 1000 {
		t.Fatalf("first model = %+v, want normalized Qwen-Flash", models[0])
	}
	if models[1].ID != "Claude-Sonnet" || models[1].Name != "Claude-Sonnet" || models[1].ContextWindow != 3000 {
		t.Fatalf("second model = %+v, want fallback name Claude-Sonnet", models[1])
	}
}
