package agentruntime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// DesktopCaptureAllDisplays asks the host to stitch every monitor into one
// image. GUI uses this when the model omits display; hosts that can only
// capture the primary display may treat it as display 0.
const DesktopCaptureAllDisplays = -1

// DesktopCaptureRequest is the typed capture request shared by builtin
// screenshot and host adapters. Display is 0-based; a negative value means
// all displays.
type DesktopCaptureRequest struct {
	Display int
}

// ParseDesktopDisplayIndex accepts the JSON shapes models emit for the
// screenshot display argument. GUI and the compiled builtin module must use
// this parser so a float, int, or numeric string cannot take different paths.
func ParseDesktopDisplayIndex(raw any) (int, error) {
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case float32:
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid display: %v", raw)
		}
		return int(n), nil
	case string:
		trimmed := strings.TrimSpace(v)
		displayIndex, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, fmt.Errorf("invalid display: %s", v)
		}
		return displayIndex, nil
	default:
		return 0, fmt.Errorf("invalid display type: %T", raw)
	}
}
