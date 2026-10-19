package computeruse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopBotStructureCases(t *testing.T) {
	for _, id := range []string{"browser_no_pixel", "dialog_probe_first", "captcha_handoff"} {
		t.Run(id, func(t *testing.T) {
			runStructureCase(t, readStructureCase(t, id))
		})
	}
}

type structureCase struct {
	ID     string `json:"id"`
	Window string `json:"window"`
	Page   string `json:"page"`
	Secret string `json:"secret"`
	Expect string `json:"expect"`
	Steps  []any  `json:"steps"`
}

func readStructureCase(t *testing.T, id string) structureCase {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c structureCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runStructureCase(t *testing.T, c structureCase) {
	t.Helper()
	steps, err := ParseSteps(c.Steps)
	if err != nil {
		t.Fatal(err)
	}
	got := PixelRefusal(c.Window, c.Page, steps)
	if got == nil || got.Error() != c.Expect {
		t.Fatalf("refusal = %v", got)
	}
	if c.Secret != "" && strings.Contains(got.Error(), c.Secret) {
		t.Fatalf("refusal contains the secret: %v", got)
	}
	switch c.ID {
	case "dialog_probe_first":
		withID, err := ParseSteps([]any{map[string]any{"action": "click", "id": 3.0}})
		if err != nil {
			t.Fatal(err)
		}
		if err := PixelRefusal(c.Window, "", withID); err != nil {
			t.Fatalf("an element id is a structure step: %v", err)
		}
	case "captcha_handoff":
		withID, err := ParseSteps([]any{map[string]any{"action": "click", "id": 3.0}})
		if err != nil {
			t.Fatal(err)
		}
		if err := PixelRefusal(c.Window, c.Page, withID); err == nil || err.Error() != c.Expect || strings.Contains(err.Error(), c.Secret) {
			t.Fatalf("captcha id refusal = %v", err)
		}
	case "browser_no_pixel":
		shot, err := ParseSteps([]any{map[string]any{"action": "screenshot"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := PixelRefusal(c.Window, "", shot); err != nil {
			t.Fatalf("a screenshot is not a pixel click: %v", err)
		}
	}
}
