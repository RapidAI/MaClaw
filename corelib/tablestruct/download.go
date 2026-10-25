package tablestruct

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// EnsureModels downloads the wired table UNET into <Maclaw base>/models/table
// when it is missing or fails the release pin. GitHub Model_Release is tried
// first. An empty hubURL uses MACLAW_HUB_URL.
func EnsureModels(ctx context.Context, hubURL string) error {
	base := strings.TrimSpace(maclawpath.BaseDir())
	if base == "" {
		return fmt.Errorf("tablestruct: maclaw base dir unavailable")
	}
	return embedding.EnsureReleaseKind(ctx, filepath.Join(base, "models"), hubURLOrEnv(hubURL), "table")
}

// Ready reports that unet.onnx matches the release pin.
// A matching .verified marker skips the hash.
func Ready() bool {
	path, err := ModelPath()
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() != modelBytes {
		return false
	}
	return verifyFile(path) == nil
}

func verifyFile(path string) error {
	spec, ok := pinSpec(filepath.Base(path))
	if !ok {
		return fmt.Errorf("tablestruct: release pin missing for %s", filepath.Base(path))
	}
	return embedding.VerifyModelFile(path, spec)
}

func pinSpec(name string) (embedding.ModelSpec, bool) {
	for _, asset := range embedding.ReleaseAssets() {
		if asset.Kind == "table" && asset.Filename == name {
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
