package doclayout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// EnsureModels downloads PP-DocLayoutV3 and its external weights into
// <Maclaw base>/models/layout when they are missing or fail the release pin.
// GitHub Model_Release is tried first. An empty hubURL uses MACLAW_HUB_URL.
func EnsureModels(ctx context.Context, hubURL string) error {
	base := strings.TrimSpace(maclawpath.BaseDir())
	if base == "" {
		return fmt.Errorf("doclayout: maclaw base dir unavailable")
	}
	return embedding.EnsureReleaseKind(ctx, filepath.Join(base, "models"), hubURLOrEnv(hubURL), "layout")
}

// Ready reports that both weight files match the release pin.
// A matching .verified marker skips the hash. Size alone is not enough:
// a same-size corrupt file must be downloaded again.
func Ready() bool {
	path, err := ModelPath()
	if err != nil || !ready(path) {
		return false
	}
	return verifyFiles(path) == nil
}

func verifyFiles(onnxPath string) error {
	dir := filepath.Dir(onnxPath)
	for _, name := range []string{modelFile, modelFile + ".data"} {
		spec, ok := pinSpec(name)
		if !ok {
			return fmt.Errorf("doclayout: release pin missing for %s", name)
		}
		if err := embedding.VerifyModelFile(filepath.Join(dir, name), spec); err != nil {
			return err
		}
	}
	return nil
}

func pinSpec(name string) (embedding.ModelSpec, bool) {
	for _, asset := range embedding.ReleaseAssets() {
		if asset.Kind == "layout" && asset.Filename == name {
			return asset.Spec(), true
		}
	}
	return embedding.ModelSpec{}, false
}

func hubURLOrEnv(hubURL string) string {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL != "" {
		return hubURL
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_HUB_URL")), "/")
}
