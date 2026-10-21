// Package grounding turns a planner's description of a click target ("the
// blue Submit button under the form") into screen coordinates with a second,
// grounding-specialised vision model (UI-TARS, Qwen2.5/3-VL, GTA1, Holo, …).
//
// The split follows cua's composed-grounded agent loop
// (https://github.com/trycua/cua, libs/python/agent/cua_agent/loops/
// composed_grounded.py, MIT): a planner model decides what to do and names
// the target in words; a grounding model only answers where it is. No code
// is copied. When no grounder is configured, or it fails, callers keep the
// single-model behaviour (the planner gives x,y or an element ref itself).
package grounding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// Mark is one numbered element the image already shows (SoM / a11y box).
type Mark struct {
	Ref  string // e.g. "e3" or "3"
	Name string
	Role string
	X, Y int // centre, image pixels
}

// Result is where the target is, in pixels of the image that was grounded.
// Ref is set when the grounder picked a numbered mark instead.
type Result struct {
	X, Y int
	Ref  string
	Raw  string
}

// MaxTokens bounds the grounder's answer. Coordinates are short.
const MaxTokens = 120

// CallTimeout bounds one grounding request.
const CallTimeout = 25 * time.Second

// Prompt is the grounding instruction. It asks for pixel JSON but Parse also
// accepts the native output of common grounding models.
func Prompt(target string, width, height int, marks []Mark) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a GUI grounding model. The screenshot is %dx%d pixels (origin top-left).\n", width, height)
	fmt.Fprintf(&b, "Target to click: %s\n", strings.TrimSpace(target))
	if len(marks) > 0 {
		b.WriteString("Numbered elements already boxed on the image:\n")
		n := 0
		for _, m := range marks {
			if n >= 60 {
				break
			}
			label := strings.TrimSpace(m.Name)
			if label == "" {
				label = "(no label)"
			}
			fmt.Fprintf(&b, "- %s %s %q at (%d,%d)\n", m.Ref, strings.TrimSpace(m.Role), label, m.X, m.Y)
			n++
		}
		b.WriteString("If one of them is the target, you may answer {\"ref\":\"<ref>\"}.\n")
	}
	b.WriteString("Answer with JSON only: {\"x\":<int>,\"y\":<int>} for the centre of the target in image pixels. If the target is not visible, answer {\"none\":true}.")
	return b.String()
}

var (
	reBox      = regexp.MustCompile(`<\|box_start\|>\s*\(?\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*\)?(?:\s*,?\s*\(?\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*\)?)?\s*<\|box_end\|>`)
	reStartBox = regexp.MustCompile(`start_box\s*=\s*['"]?\(?\[?\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)(?:\s*,\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?))?`)
	rePoint    = regexp.MustCompile(`<point>\s*(-?\d+(?:\.\d+)?)[\s,]+(-?\d+(?:\.\d+)?)\s*</point>`)
	reBracket4 = regexp.MustCompile(`\[\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*\]`)
	rePair     = regexp.MustCompile(`[\(\[]\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*[\)\]]`)
	reJSONObj  = regexp.MustCompile(`(?s)\{.*\}`)
)

// Scale says how a model reports coordinates.
type Scale int

const (
	// ScaleAuto guesses: values <= 1 are fractions, values beyond the image
	// but <= 1000 are the 0..1000 grid, anything else is pixels.
	ScaleAuto Scale = iota
	// ScalePixels is absolute image pixels (Qwen2.5-VL, Qwen3-VL, most JSON).
	ScalePixels
	// ScaleThousand is the 0..1000 grid (UI-TARS, Qwen2-VL, CogAgent).
	ScaleThousand
)

// ScaleForModel picks the coordinate convention from the model id.
func ScaleForModel(model string) Scale {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "ui-tars"), strings.Contains(m, "uitars"), strings.Contains(m, "qwen2-vl"), strings.Contains(m, "cogagent"):
		return ScaleThousand
	}
	return ScaleAuto
}

// Parse reads a grounder answer into image pixels.
func Parse(raw string, width, height int, scale Scale) (Result, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Result{}, fmt.Errorf("grounder returned nothing")
	}
	res := Result{Raw: text}
	if obj := reJSONObj.FindString(text); obj != "" {
		var m map[string]any
		if json.Unmarshal([]byte(obj), &m) == nil {
			if none, _ := m["none"].(bool); none {
				return res, fmt.Errorf("grounder did not find the target")
			}
			if ref := refValue(m["ref"]); ref != "" {
				res.Ref = ref
				return res, nil
			}
			if ref := refValue(m["mark"]); ref != "" {
				res.Ref = ref
				return res, nil
			}
			x, xok := num(m["x"])
			y, yok := num(m["y"])
			if xok && yok {
				return finish(res, x, y, width, height, scale)
			}
			for _, key := range []string{"point", "coordinate", "click", "position"} {
				if arr, ok := m[key].([]any); ok && len(arr) >= 2 {
					x, xok := num(arr[0])
					y, yok := num(arr[1])
					if xok && yok {
						return finish(res, x, y, width, height, scale)
					}
				}
			}
			for _, key := range []string{"bbox", "bbox_2d", "box"} {
				if arr, ok := m[key].([]any); ok && len(arr) >= 4 {
					x1, _ := num(arr[0])
					y1, _ := num(arr[1])
					x2, _ := num(arr[2])
					y2, _ := num(arr[3])
					return finish(res, (x1+x2)/2, (y1+y2)/2, width, height, scale)
				}
			}
		}
	}
	for _, re := range []*regexp.Regexp{reBox, reStartBox} {
		if g := re.FindStringSubmatch(text); g != nil {
			x, y := atof(g[1]), atof(g[2])
			if g[3] != "" && g[4] != "" {
				x, y = (x+atof(g[3]))/2, (y+atof(g[4]))/2
			}
			if scale == ScaleAuto {
				scale = ScaleThousand
			}
			return finish(res, x, y, width, height, scale)
		}
	}
	if g := rePoint.FindStringSubmatch(text); g != nil {
		return finish(res, atof(g[1]), atof(g[2]), width, height, scale)
	}
	if g := reBracket4.FindStringSubmatch(text); g != nil {
		return finish(res, (atof(g[1])+atof(g[3]))/2, (atof(g[2])+atof(g[4]))/2, width, height, scale)
	}
	if g := rePair.FindStringSubmatch(text); g != nil {
		return finish(res, atof(g[1]), atof(g[2]), width, height, scale)
	}
	return res, fmt.Errorf("grounder answer has no coordinates")
}

func finish(res Result, x, y float64, width, height int, scale Scale) (Result, error) {
	if math.IsNaN(x) || math.IsNaN(y) || x < 0 || y < 0 {
		return res, fmt.Errorf("grounder coordinates are invalid")
	}
	switch scale {
	case ScaleThousand:
		x, y = x*float64(width)/1000, y*float64(height)/1000
	case ScaleAuto:
		switch {
		case x <= 1 && y <= 1:
			x, y = x*float64(width), y*float64(height)
		case (x > float64(width) || y > float64(height)) && x <= 1000 && y <= 1000:
			x, y = x*float64(width)/1000, y*float64(height)/1000
		}
	}
	px, py := int(math.Round(x)), int(math.Round(y))
	if width > 0 && height > 0 && (px >= width || py >= height) {
		return res, fmt.Errorf("grounder point (%d,%d) is outside the %dx%d image", px, py, width, height)
	}
	res.X, res.Y = px, py
	return res, nil
}

func refValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t > 0 && t == math.Trunc(t) {
			return strconv.Itoa(int(t))
		}
	}
	return ""
}

func num(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// Configured reports whether cfg names a usable grounding model.
func Configured(cfg corelib.MaclawLLMConfig) bool {
	return strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Model) != ""
}

// Ground asks the grounder where target is on the image.
func Ground(ctx context.Context, cfg corelib.MaclawLLMConfig, client *http.Client, imageB64, mime string, width, height int, target string, marks []Mark) (Result, error) {
	if !Configured(cfg) {
		return Result{}, fmt.Errorf("no grounder model")
	}
	if strings.TrimSpace(target) == "" {
		return Result{}, fmt.Errorf("target is empty")
	}
	if strings.TrimSpace(mime) == "" {
		mime = "image/png"
	}
	raw, err := call(ctx, cfg, client, Prompt(target, width, height, marks), imageB64, mime)
	if err != nil {
		return Result{}, err
	}
	return Parse(raw, width, height, ScaleForModel(cfg.Model))
}

func usesAnthropic(cfg corelib.MaclawLLMConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") || corelib.IsAnthropicWireAPI(cfg.WireAPI)
}

func call(ctx context.Context, cfg corelib.MaclawLLMConfig, client *http.Client, prompt, imageB64, mime string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: CallTimeout}
	}
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	atts := []agent.MessageAttachment{{MimeType: mime, Data: imageB64}}
	var content interface{}
	if usesAnthropic(cfg) {
		content = agent.BuildAnthropicVisionContent(prompt, atts)
	} else {
		content = agent.BuildOpenAIVisionContent(prompt, atts)
	}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": content}}
	if usesAnthropic(cfg) {
		resp, err := llm.DoAnthropicRequestWithOptions(callCtx, cfg, messages, nil, client, llm.AnthropicMessagesRequestOptions{MaxTokens: MaxTokens})
		return responseText(resp, err)
	}
	extra := map[string]interface{}{"max_tokens": MaxTokens, "temperature": 0}
	if cfg.IsResponsesAPI() {
		req, _, _, err := llm.NewResponsesAPIRequest(callCtx, cfg, messages, llm.ResponsesAPIRequestOptions{Stream: false, ExtraBody: map[string]interface{}{"max_output_tokens": MaxTokens}})
		if err != nil {
			return "", err
		}
		return do(client, req, llm.ParseNonStreamResponsesAPIBody)
	}
	req, _, _, err := llm.NewOpenAIChatRequest(callCtx, cfg, messages, llm.OpenAIChatRequestOptions{Stream: false, ExtraBody: extra})
	if err != nil {
		return "", err
	}
	return do(client, req, llm.ParseNonStreamOpenAIResponseBody)
}

func do(client *http.Client, req *http.Request, parse func([]byte) (*llm.Response, error)) (string, error) {
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &llm.HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}
	parsed, err := parse(body)
	return responseText(parsed, err)
}

func responseText(resp *llm.Response, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("grounder returned no choices")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	if out == "" {
		out = strings.TrimSpace(resp.Choices[0].Message.ReasoningContent)
	}
	return agent.StripThinkingTags(out), nil
}
