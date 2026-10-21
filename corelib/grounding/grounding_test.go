package grounding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestParseFormats(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		scale Scale
		x, y  int
		ref   string
	}{
		{"json pixels", `{"x": 640, "y": 360}`, ScaleAuto, 640, 360, ""},
		{"json in prose", "Sure.\n```json\n{\"x\":10,\"y\":20}\n```", ScaleAuto, 10, 20, ""},
		{"json ref", `{"ref":"e7"}`, ScaleAuto, 0, 0, "e7"},
		{"json mark number", `{"mark":3}`, ScaleAuto, 0, 0, "3"},
		{"json bbox_2d", `{"bbox_2d":[100,100,200,300]}`, ScalePixels, 150, 200, ""},
		{"ui-tars box", `click(start_box='<|box_start|>(500,500)<|box_end|>')`, ScaleAuto, 640, 360, ""},
		{"ui-tars start_box", `Action: click(start_box='(250,750)')`, ScaleThousand, 320, 540, ""},
		{"point tag", `<point>100 50</point>`, ScaleAuto, 100, 50, ""},
		{"bracket bbox", `[10, 20, 30, 40]`, ScaleAuto, 20, 30, ""},
		{"pair", `The button is at (1200, 700).`, ScaleAuto, 1200, 700, ""},
		{"fraction", `{"x":0.5,"y":0.25}`, ScaleAuto, 640, 180, ""},
		{"thousand grid beyond image", `(900, 950)`, ScaleAuto, 0, 0, ""},
	}
	for _, c := range cases {
		res, err := Parse(c.raw, 1280, 720, c.scale)
		if c.name == "thousand grid beyond image" {
			// 900,950 fits a 1280 wide image in x but not 720 in y, so the
			// auto rule reads it as the 0..1000 grid.
			if err != nil || res.X != 1152 || res.Y != 684 {
				t.Fatalf("%s: res=%+v err=%v", c.name, res, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Ref != c.ref || (c.ref == "" && (res.X != c.x || res.Y != c.y)) {
			t.Fatalf("%s: got %+v want (%d,%d) ref=%q", c.name, res, c.x, c.y, c.ref)
		}
	}
}

func TestParseRefusals(t *testing.T) {
	for _, raw := range []string{"", `{"none":true}`, "I cannot see it", `{"x":5000,"y":10}`} {
		if _, err := Parse(raw, 1280, 720, ScalePixels); err == nil {
			t.Fatalf("%q must fail", raw)
		}
	}
}

func TestScaleForModel(t *testing.T) {
	if ScaleForModel("ByteDance-Seed/UI-TARS-1.5-7B") != ScaleThousand {
		t.Fatal("ui-tars uses the 0..1000 grid")
	}
	if ScaleForModel("qwen3-vl-plus") != ScaleAuto {
		t.Fatal("qwen3-vl reports pixels")
	}
}

func TestPromptListsMarks(t *testing.T) {
	p := Prompt("the Submit button", 800, 600, []Mark{{Ref: "e1", Name: "Submit", Role: "button", X: 10, Y: 20}})
	for _, want := range []string{"800x600", "the Submit button", `e1 button "Submit" at (10,20)`, `{"x":<int>,"y":<int>}`} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt misses %q:\n%s", want, p)
		}
	}
}

func TestGroundCallsOpenAICompatibleModel(t *testing.T) {
	var sawImage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		blob, _ := json.Marshal(body)
		sawImage = strings.Contains(string(blob), "data:image/png;base64,QUJD")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"x\":12,\"y\":34}"}}]}`))
	}))
	defer srv.Close()
	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen3-vl", Protocol: "openai"}
	res, err := Ground(context.Background(), cfg, srv.Client(), "QUJD", "image/png", 100, 100, "the OK button", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.X != 12 || res.Y != 34 {
		t.Fatalf("res=%+v", res)
	}
	if !sawImage {
		t.Fatal("the screenshot must be sent to the grounder")
	}
}

func TestGroundNeedsConfig(t *testing.T) {
	if _, err := Ground(context.Background(), corelib.MaclawLLMConfig{}, nil, "x", "", 1, 1, "t", nil); err == nil {
		t.Fatal("unconfigured grounder must fail so callers fall back")
	}
}
