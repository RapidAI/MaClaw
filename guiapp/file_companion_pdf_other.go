//go:build !darwin

package guiapp

import "context"

// fileCompanionQuartzRenderPages is the macOS batch renderer. Other platforms
// leave the per-page loop in paperRenderNativePages to run.
func fileCompanionQuartzRenderPages(context.Context, string, string, []int, int) (bool, error) {
	return false, nil
}
