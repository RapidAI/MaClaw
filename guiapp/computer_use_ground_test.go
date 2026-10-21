package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func groundTestPNG(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func withGroundState(t *testing.T, cfg corelib.MaclawLLMConfig, ok bool, img string) {
	t.Helper()
	prevFn := computerUseGrounderFn
	globalComputerUse.mu.Lock()
	prevImg := globalComputerUse.lastModelImage
	globalComputerUse.lastModelImage = img
	globalComputerUse.mu.Unlock()
	computerUseGrounderFn = func() (corelib.MaclawLLMConfig, bool) { return cfg, ok }
	t.Cleanup(func() {
		computerUseGrounderFn = prevFn
		globalComputerUse.mu.Lock()
		globalComputerUse.lastModelImage = prevImg
		globalComputerUse.mu.Unlock()
	})
}

func TestCuGroundTargetWithoutGrounderAsksForCoordinates(t *testing.T) {
	withGroundState(t, corelib.MaclawLLMConfig{}, false, groundTestPNG(t, 4, 4))
	if _, _, note := cuGroundTarget("OK"); !strings.Contains(note, "x,y") {
		t.Fatalf("note=%q", note)
	}
}

func TestCuGroundTargetUsesLastModelImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"x\":40,\"y\":30}"}}]}`))
	}))
	defer srv.Close()
	withGroundState(t, corelib.MaclawLLMConfig{URL: srv.URL, Model: "ui-grounder", Protocol: "openai"}, true, groundTestPNG(t, 80, 60))
	x, y, note := cuGroundTarget("Send")
	if note != "" || x != 40 || y != 30 {
		t.Fatalf("x=%d y=%d note=%q", x, y, note)
	}
}

func TestSetPendingModelImageKeepsLastForGrounder(t *testing.T) {
	withGroundState(t, corelib.MaclawLLMConfig{}, false, "")
	setPendingComputerUseModelImage("abc")
	_, _ = takePendingComputerUseModelImage()
	globalComputerUse.mu.Lock()
	last := globalComputerUse.lastModelImage
	globalComputerUse.mu.Unlock()
	if last != "abc" {
		t.Fatalf("last=%q", last)
	}
}

func TestGrounderRoleIsUnusedUntilPinned(t *testing.T) {
	a := &App{}
	_, err := a.resolveMaclawLLMGrounderFrom(corelib.MaclawLLMProfiles{}, nil)
	if !errors.Is(err, errGrounderNotConfigured) {
		t.Fatalf("err=%v", err)
	}
	inherit := true
	_, err = a.resolveMaclawLLMGrounderFrom(corelib.MaclawLLMProfiles{Grounder: corelib.MaclawLLMRoleAssignment{Model: "x", InheritHub: &inherit}}, nil)
	if !errors.Is(err, errGrounderNotConfigured) {
		t.Fatalf("inherit must mean not used: %v", err)
	}
	if err := validateMaclawLLMRoleAssignment("grounder", corelib.MaclawLLMRoleAssignment{InheritHub: new(bool)}); err == nil {
		t.Fatal("a pinned grounder needs a model")
	}
}
