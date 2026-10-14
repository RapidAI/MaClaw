package desktop

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Desktop control operations run by desktop_supervisor.py ctl inside the
// container: clipboard, window management, screen info, waiting, the
// person-viewer status, and the accessibility (AT-SPI) element tree.
const (
	CtlClipboardGet = "clipboard_get"
	CtlClipboardSet = "clipboard_set"
	CtlWindows      = "windows"
	CtlWindow       = "window"
	CtlScreen       = "screen"
	CtlWait         = "wait"
	CtlStatus       = "status"
	CtlA11yProbe    = "a11y_probe"
	CtlA11yAct      = "a11y_act"
)

// CtlOps lists every operation; CtlReadOnlyOps the ones that change nothing.
var (
	CtlOps         = []string{CtlClipboardGet, CtlClipboardSet, CtlWindows, CtlWindow, CtlScreen, CtlWait, CtlStatus, CtlA11yProbe, CtlA11yAct}
	CtlReadOnlyOps = []string{CtlClipboardGet, CtlWindows, CtlScreen, CtlWait, CtlStatus, CtlA11yProbe}
)

// CtlRequestMax bounds one encoded request. It travels as one argv string,
// which Linux caps at 128 KiB.
const CtlRequestMax = 96 << 10

// CtlTimeoutSeconds is the longest wait a ctl request may ask for.
const CtlTimeoutSeconds = 60

// ValidCtlOp reports whether op is a known control operation.
// Bot display control ops. They act on one isolated bot display; the
// Docker service fills in that display's key, a client never names it.
const (
	CtlBotState      = "bot_state"
	CtlTakeoverBegin = "takeover_begin"
	CtlTakeoverEnd   = "takeover_end"
)

// CtlBotOps are accepted only for a bot desktop (desktop_mode=isolated).
var CtlBotOps = []string{CtlBotState, CtlTakeoverBegin, CtlTakeoverEnd}

// IsCtlBotOp reports whether op is one of CtlBotOps.
func IsCtlBotOp(op string) bool {
	for _, known := range CtlBotOps {
		if op == known {
			return true
		}
	}
	return false
}

func ValidCtlOp(op string) bool {
	if IsCtlBotOp(op) {
		return true
	}
	for _, known := range CtlOps {
		if op == known {
			return true
		}
	}
	return false
}

// CtlReadOnly reports whether op only reads the desktop.
func CtlReadOnly(op string) bool {
	for _, known := range CtlReadOnlyOps {
		if op == known {
			return true
		}
	}
	return false
}

// CtlArgs builds the docker exec tail (after the container name) that runs
// one control request. The supervisor prints one JSON object; the command
// always exits 0 so the caller gets that object, ok or not.
func CtlArgs(request map[string]any) ([]string, error) {
	op, _ := request["op"].(string)
	op = strings.TrimSpace(op)
	if !ValidCtlOp(op) {
		return nil, fmt.Errorf("%w: desktop control op must be one of %s", ErrInvalid, strings.Join(CtlOps, ", "))
	}
	request["op"] = op
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("%w: desktop control request is not JSON", ErrInvalid)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) > CtlRequestMax {
		return nil, fmt.Errorf("%w: desktop control request is larger than %d bytes", ErrInvalid, CtlRequestMax)
	}
	return []string{"sh", "-c", `python3 /desktop_supervisor.py ctl "$1" 2>/dev/null; exit 0`, "maclaw-ctl", encoded}, nil
}

// DecodeCtl reads the supervisor's JSON answer. An empty or non-JSON answer
// means the container's supervisor predates ctl.
func DecodeCtl(out string) (map[string]any, error) {
	text := strings.TrimSpace(out)
	if i := strings.LastIndex(text, "\n{"); i >= 0 {
		text = text[i+1:]
	}
	var result map[string]any
	if text == "" || json.Unmarshal([]byte(text), &result) != nil {
		return nil, fmt.Errorf("desktop control is not available in this desktop; open it again to update the supervisor")
	}
	return result, nil
}
