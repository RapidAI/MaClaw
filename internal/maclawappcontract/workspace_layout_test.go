package maclawappcontract

import "testing"

func TestWorkspaceLayoutFingerprintCanonicalizesRegions(t *testing.T) {
	layout := map[string]any{
		"template":       "left_nav",
		"density":        "compact",
		"primary_region": "center",
		"output_region":  "bottom",
		"regions": []any{
			map[string]any{"id": "result", "role": "output", "placement": "bottom", "order": 3},
			map[string]any{"id": "request", "role": "input", "placement": "center", "order": 1},
			map[string]any{"id": "detail", "role": "detail", "placement": "right", "visible": false, "order": 2},
		},
	}

	got := WorkspaceLayoutFingerprint("approval_workspace", layout)
	if got == "" {
		t.Fatalf("WorkspaceLayoutFingerprint() returned empty")
	}
	reordered := map[string]any{
		"template":      "left_nav",
		"density":       "compact",
		"primaryRegion": "center",
		"outputRegion":  "bottom",
		"regions": []any{
			map[string]any{"id": "request", "role": "input", "placement": "center", "visible": true, "order": 1},
			map[string]any{"id": "detail", "role": "detail", "placement": "right", "visible": false, "order": 2},
			map[string]any{"id": "result", "role": "output", "placement": "bottom", "visible": true, "order": 3},
		},
	}
	if want := WorkspaceLayoutFingerprint("approval_workspace", reordered); got != want {
		t.Fatalf("fingerprint should be stable after canonicalization: got %q want %q", got, want)
	}
}

func TestCanonicalWorkspaceLayoutRegionsDefaultsVisibleAndOrder(t *testing.T) {
	regions := CanonicalWorkspaceLayoutRegions([]any{
		map[string]any{"id": "b", "role": "output", "placement": "right", "order": 2},
		map[string]any{"id": "a", "role": "input", "placement": "left"},
	})
	if len(regions) != 2 {
		t.Fatalf("regions=%#v", regions)
	}
	if regions[0]["id"] != "b" || regions[0]["order"] != 2 || regions[0]["visible"] != true {
		t.Fatalf("first canonical region mismatch: %#v", regions[0])
	}
	if regions[1]["id"] != "a" || regions[1]["order"] != 2 || regions[1]["visible"] != true {
		t.Fatalf("second canonical region mismatch: %#v", regions[1])
	}
}
