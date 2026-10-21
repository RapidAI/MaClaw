package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/grounding"
)

// computerUseGrounderFn returns the optional grounding model (model settings
// → 定位模型). Unset keeps computer_click on the planner's own ref or x,y.
var computerUseGrounderFn = func() (corelib.MaclawLLMConfig, bool) { return corelib.MaclawLLMConfig{}, false }

var computerUseGroundHTTP = &http.Client{Timeout: grounding.CallTimeout}

func cuHasArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	return ok && v != nil
}

// cuGroundTarget places target on the last screenshot the model saw. The
// point is in that image's pixels, the same space as a vision model's own
// x,y, so the caller maps it with MapVisionClick/MapCaptureClick. A non-empty
// note means grounding was not possible and tells the model how to continue
// on its own.
func cuGroundTarget(target string) (int, int, string) {
	cfg, ok := computerUseGrounderFn()
	if !ok {
		return 0, 0, "target needs a grounding model, and none is set; pass x,y from the screenshot or ref=eN from computer_observe"
	}
	globalComputerUse.mu.Lock()
	img := globalComputerUse.lastModelImage
	globalComputerUse.mu.Unlock()
	if img == "" {
		return 0, 0, "no screenshot to ground target on yet; call computer_observe first, then click by target, x,y or ref"
	}
	raw, err := base64.StdEncoding.DecodeString(img)
	if err != nil {
		return 0, 0, "could not read the last screenshot; pass x,y or ref"
	}
	conf, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || conf.Width <= 0 || conf.Height <= 0 {
		return 0, 0, "could not read the last screenshot's size; pass x,y or ref"
	}
	mime := "image/png"
	if format == "jpeg" {
		mime = "image/jpeg"
	}
	ctx, cancel := context.WithTimeout(context.Background(), grounding.CallTimeout)
	defer cancel()
	started := time.Now()
	res, err := grounding.Ground(ctx, cfg, computerUseGroundHTTP, img, mime, conf.Width, conf.Height, target, nil)
	log.Printf("[computer-use] grounder model=%s took=%s ok=%v", cfg.Model, time.Since(started).Round(10*time.Millisecond), err == nil && res.Ref == "")
	if err != nil {
		return 0, 0, fmt.Sprintf("the grounding model could not place %q (%v); pass x,y from the screenshot or ref=eN", target, err)
	}
	if strings.TrimSpace(res.Ref) != "" {
		return 0, 0, fmt.Sprintf("the grounding model answered element %s; pass ref=%s", res.Ref, res.Ref)
	}
	return res.X, res.Y, ""
}
