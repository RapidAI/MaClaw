package guiapp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// wireOfficeImagePDFOCR connects PDF reads that have pictures but no text
// layer to the same process-wide PP-OCRv6 engine used by knowledge import
// and computer use. corelib/agent cannot import the browser OCR provider.
func wireOfficeImagePDFOCR() {
	agent.SetOfficeImagePDFOCR(officeImagePDFOCRReady, officeImagePDFOCRText)
}

func officeImagePDFOCRReady() bool {
	return ocrConfiguredEnabled() && sharedNativeOCRProvider().IsAvailable()
}

func officeImagePDFOCRText(imageBase64 string) (string, error) {
	if !ocrConfiguredEnabled() {
		return "", fmt.Errorf("OCR disabled")
	}
	provider := sharedNativeOCRProvider()
	if provider == nil || !provider.IsAvailable() {
		return "", fmt.Errorf("OCR unavailable")
	}
	results, err := provider.Recognize(imageBase64)
	if err != nil {
		return "", err
	}
	return joinBrowserOCRText(results), nil
}

// joinBrowserOCRText keeps recognized lines in reading order and drops the
// screenshot-style boxes. Document injection wants the words, not coordinates.
func joinBrowserOCRText(results []browser.OCRResult) string {
	if len(results) == 0 {
		return ""
	}
	ordered := append([]browser.OCRResult(nil), results...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].BBox[1] != ordered[j].BBox[1] {
			return ordered[i].BBox[1] < ordered[j].BBox[1]
		}
		return ordered[i].BBox[0] < ordered[j].BBox[0]
	})
	lines := make([]string, 0, len(ordered))
	for _, result := range ordered {
		if text := strings.TrimSpace(result.Text); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}
