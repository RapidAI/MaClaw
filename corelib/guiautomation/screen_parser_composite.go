package guiautomation

import "github.com/RapidAI/CodeClaw/corelib/taskengine"

// CompositeScreenParser merges results from multiple ScreenParsers.
// Unlike CompositeOCRProvider (first-success), this merges all available
// results because different parsers detect different element types:
// accessibility has Value, YOLO has Interactable, OCR has text.
type CompositeScreenParser struct {
	parsers []taskengine.ScreenParser
}

// NewCompositeScreenParser creates a composite parser.
// Parsers are tried in order; all available results are merged.
func NewCompositeScreenParser(parsers ...taskengine.ScreenParser) *CompositeScreenParser {
	return &CompositeScreenParser{parsers: parsers}
}

// Parse implements taskengine.ScreenParser.
func (c *CompositeScreenParser) Parse(pngBase64 string) ([]taskengine.UIElement, error) {
	var all []taskengine.UIElement
	for _, p := range c.parsers {
		if p == nil || !p.IsAvailable() {
			continue
		}
		elements, err := p.Parse(pngBase64)
		if err != nil {
			continue // try next parser
		}
		all = append(all, elements...)
	}
	return deduplicateByBBox(all), nil
}

// IsAvailable implements taskengine.ScreenParser.
func (c *CompositeScreenParser) IsAvailable() bool {
	for _, p := range c.parsers {
		if p != nil && p.IsAvailable() {
			return true
		}
	}
	return false
}

// deduplicateByBBox merges elements with overlapping bounding boxes (>80% IoU).
// When two elements overlap, keeps the one with higher confidence but merges
// information from both (e.g. accessibility Value + YOLO Interactable).
func deduplicateByBBox(elements []taskengine.UIElement) []taskengine.UIElement {
	if len(elements) <= 1 {
		return elements
	}

	keep := make([]bool, len(elements))
	for i := range keep {
		keep[i] = true
	}

	for i := 0; i < len(elements); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(elements); j++ {
			if !keep[j] {
				continue
			}
			if bboxIoU(elements[i].BBox, elements[j].BBox) > 0.8 {
				// Merge: keep higher confidence, combine info
				if elements[j].Confidence > elements[i].Confidence {
					merged := mergeElements(elements[j], elements[i])
					elements[i] = merged
				} else {
					merged := mergeElements(elements[i], elements[j])
					elements[i] = merged
				}
				keep[j] = false
			}
		}
	}

	var result []taskengine.UIElement
	for i, el := range elements {
		if keep[i] {
			result = append(result, el)
		}
	}
	return result
}

// mergeElements merges two overlapping elements. primary has higher confidence.
func mergeElements(primary, secondary taskengine.UIElement) taskengine.UIElement {
	result := primary
	// Take Value from whichever has it (accessibility provides Value, YOLO doesn't)
	if result.Value == "" && secondary.Value != "" {
		result.Value = secondary.Value
	}
	// Take Name from whichever has a more descriptive one
	if result.Name == "" || (len(secondary.Name) > len(result.Name) && secondary.Name != "") {
		result.Name = secondary.Name
	}
	// Interactable: true if either says true
	if secondary.Interactable {
		result.Interactable = true
	}
	return result
}

// bboxIoU computes Intersection over Union for two [x, y, w, h] bounding boxes.
func bboxIoU(a, b [4]int) float64 {
	x1 := maxInt(a[0], b[0])
	y1 := maxInt(a[1], b[1])
	x2 := minInt(a[0]+a[2], b[0]+b[2])
	y2 := minInt(a[1]+a[3], b[1]+b[3])

	if x2 <= x1 || y2 <= y1 {
		return 0
	}

	inter := float64((x2 - x1) * (y2 - y1))
	areaA := float64(a[2] * a[3])
	areaB := float64(b[2] * b[3])
	if areaA+areaB-inter <= 0 {
		return 0
	}
	return inter / (areaA + areaB - inter)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Compile-time interface check.
var _ taskengine.ScreenParser = (*CompositeScreenParser)(nil)
