package maclawappcontract

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// WorkspaceLayoutFingerprint returns the canonical App Studio workspace layout
// fingerprint shared by Hub review, GUI install, and test fixtures.
func WorkspaceLayoutFingerprint(entryName string, layout map[string]any) string {
	if layout == nil || strings.TrimSpace(entryName) == "" {
		return ""
	}
	payload := map[string]any{
		"entry":         strings.TrimSpace(entryName),
		"template":      firstString(layout["template"]),
		"density":       firstString(layout["density"]),
		"primaryRegion": firstString(layout["primaryRegion"], layout["primary_region"]),
		"outputRegion":  firstString(layout["outputRegion"], layout["output_region"]),
		"regions":       CanonicalWorkspaceLayoutRegions(anySlice(layout["regions"])),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return fnv1aTextHash(string(data))
}

// CanonicalWorkspaceLayoutRegions returns the stable region list used for
// workspace layout fingerprints. Regions are sorted by order, then original
// position, with visible defaulting to true.
func CanonicalWorkspaceLayoutRegions(rawRegions []any) []map[string]any {
	type indexedRegion struct {
		index  int
		order  int
		region map[string]any
	}
	regions := make([]indexedRegion, 0, len(rawRegions))
	for i, raw := range rawRegions {
		region := anyMap(raw)
		if region == nil {
			continue
		}
		order := i + 1
		if value, ok := numberFromAny(region["order"]); ok && value > 0 {
			order = int(math.Floor(value))
		}
		regions = append(regions, indexedRegion{index: i, order: order, region: region})
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].order == regions[j].order {
			return regions[i].index < regions[j].index
		}
		return regions[i].order < regions[j].order
	})
	out := make([]map[string]any, 0, len(regions))
	for i, item := range regions {
		visible := true
		if value, ok := item.region["visible"].(bool); ok {
			visible = value
		}
		order := item.order
		if order <= 0 {
			order = i + 1
		}
		out = append(out, map[string]any{
			"id":        firstString(item.region["id"]),
			"role":      firstString(item.region["role"]),
			"placement": firstString(item.region["placement"]),
			"visible":   visible,
			"order":     order,
		})
	}
	return out
}

func numberFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func fnv1aTextHash(value string) string {
	var hash uint32 = 2166136261
	for _, char := range value {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return fmt.Sprintf("%08x", hash)
}
