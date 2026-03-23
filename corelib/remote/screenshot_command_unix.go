//go:build !windows

package remote

import (
	"fmt"
	"strings"
)

// Windows stubs — these are only called via runtime.GOOS switch in
// screenshot_command.go, so they are never actually reached on non-Windows,
// but the compiler needs them to exist.

func buildWindowsScreenshotCommand() string { return "" }

func buildWindowsWindowScreenshotCommand(_ string) string { return "" }

func buildDarwinScreenshotCommand() string {
	// Permission is checked natively in Go (via CheckScreenRecordingPermission)
	// before this command is executed. No python3 permission check needed here.
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`tmpfile2=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`tmpfile3=""; ` +
		`trap "rm -f \"$tmpfile\" \"$tmpfile2\" \"$tmpfile3\"" EXIT; ` +
		`check_blank() { ` +
		`local f="$1"; ` +
		`if [ ! -f "$f" ] || [ ! -s "$f" ]; then echo "true"; return; fi; ` +
		`if command -v python3 >/dev/null 2>&1; then ` +
		`python3 -c "
import sys
try:
    from PIL import Image
    img = Image.open(sys.argv[1])
    px = img.load()
    w, h = img.size
    step = max(1, int((w * h / 2000) ** 0.5))
    for y in range(0, h, step):
        for x in range(0, w, step):
            p = px[x, y]
            if isinstance(p, tuple):
                if sum(p[:3]) > 10:
                    print('false'); sys.exit(0)
            elif p > 3:
                print('false'); sys.exit(0)
    print('true')
except:
    print('false')
" "$f" 2>/dev/null || echo "false"; ` +
		`else echo "false"; fi; }; ` +
		`is_locked=$(python3 -c "import Quartz; d=Quartz.CGSessionCopyCurrentDictionary(); print('locked' if d and d.get('CGSSessionScreenIsLocked',0) else 'unlocked')" 2>/dev/null || echo "unknown"); ` +
		`screencapture -x "$tmpfile" 2>/dev/null; ` +
		`is_blank=$(check_blank "$tmpfile"); ` +
		`if [ "$is_blank" != "true" ]; then ` +
		`base64 -i "$tmpfile"; exit 0; fi; ` +
		`screencapture -C "$tmpfile2" 2>/dev/null; ` +
		`is_blank2=$(check_blank "$tmpfile2"); ` +
		`if [ "$is_blank2" != "true" ]; then ` +
		`base64 -i "$tmpfile2"; exit 0; fi; ` +
		`tmpfile3=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`if python3 -c "
import Quartz
from Foundation import NSURL
import sys
region = Quartz.CGRectInfinite
image = Quartz.CGWindowListCreateImage(region, Quartz.kCGWindowListOptionOnScreenOnly, Quartz.kCGNullWindowID, Quartz.kCGWindowImageDefault)
if image is None:
    sys.exit(1)
w = Quartz.CGImageGetWidth(image)
h = Quartz.CGImageGetHeight(image)
if w == 0 or h == 0:
    sys.exit(1)
url = NSURL.fileURLWithPath_(sys.argv[1])
dest = Quartz.CGImageDestinationCreateWithURL(url, 'public.png', 1, None)
if dest is None:
    sys.exit(1)
Quartz.CGImageDestinationAddImage(dest, image, None)
Quartz.CGImageDestinationFinalize(dest)
" "$tmpfile3" 2>/dev/null; then ` +
		`is_blank3=$(check_blank "$tmpfile3"); ` +
		`if [ "$is_blank3" != "true" ]; then ` +
		`base64 -i "$tmpfile3"; exit 0; fi; fi; ` +
		`rm -f "$tmpfile3"; ` +
		`echo "screen is blank - session may be locked ($is_locked) or display is off" >&2; exit 1`
}

func buildLinuxScreenshotCommand() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`check_blank() { ` +
		`local f="$1"; ` +
		`if [ ! -f "$f" ] || [ ! -s "$f" ]; then echo "true"; return; fi; ` +
		`if command -v convert >/dev/null 2>&1; then ` +
		`mean=$(convert "$f" -colorspace Gray -format "%[fx:mean*255]" info: 2>/dev/null | cut -d. -f1); ` +
		`if [ -n "$mean" ] && [ "$mean" -le 3 ] 2>/dev/null; then echo "true"; else echo "false"; fi; return; fi; ` +
		`if command -v python3 >/dev/null 2>&1; then ` +
		`python3 -c "
import sys
try:
    from PIL import Image
    img = Image.open(sys.argv[1])
    px = img.load()
    w, h = img.size
    step = max(1, int((w * h / 2000) ** 0.5))
    for y in range(0, h, step):
        for x in range(0, w, step):
            p = px[x, y]
            if isinstance(p, tuple):
                if sum(p[:3]) > 10:
                    print('false'); sys.exit(0)
            elif p > 3:
                print('false'); sys.exit(0)
    print('true')
except:
    print('false')
" "$f" 2>/dev/null || echo "false"; return; fi; ` +
		`echo "false"; }; ` +
		`if command -v scrot >/dev/null 2>&1; then ` +
		`scrot "$tmpfile"; ` +
		`elif command -v gnome-screenshot >/dev/null 2>&1; then ` +
		`gnome-screenshot -f "$tmpfile"; ` +
		`elif command -v import >/dev/null 2>&1; then ` +
		`import -window root "$tmpfile"; ` +
		`elif command -v grim >/dev/null 2>&1; then ` +
		`grim "$tmpfile"; ` +
		`else ` +
		`echo "no screenshot tool found (scrot, gnome-screenshot, import, or grim required)" >&2; exit 1; ` +
		`fi; ` +
		`is_blank=$(check_blank "$tmpfile"); ` +
		`if [ "$is_blank" = "true" ]; then ` +
		`lock_info="unknown"; ` +
		`if command -v loginctl >/dev/null 2>&1; then ` +
		`lock_info=$(loginctl show-session $(loginctl --no-legend 2>/dev/null | awk "NR==1{print \$1}") -p LockedHint --value 2>/dev/null || echo "unknown"); fi; ` +
		`echo "screen is blank - session may be locked (locked=$lock_info) or display is off" >&2; exit 1; ` +
		`fi; ` +
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`
}

func buildDarwinWindowScreenshotCommand(windowTitle string) string {
	escaped := strings.ReplaceAll(windowTitle, `"`, `\"`)
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`wid=$(osascript -e 'tell application "System Events" to set wlist to every window of every process whose name of every window contains "%s"' -e 'if (count of wlist) > 0 then return id of item 1 of wlist' 2>/dev/null); `+
		`if [ -z "$wid" ]; then echo "Window not found" >&2; exit 1; fi; `+
		`screencapture -x -l "$wid" "$tmpfile" && `+
		`base64 -i "$tmpfile"`, escaped)
}

func buildLinuxWindowScreenshotCommand(windowTitle string) string {
	escaped := strings.ReplaceAll(windowTitle, `"`, `\"`)
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`wid=$(xdotool search --name "%s" 2>/dev/null | head -1); `+
		`if [ -z "$wid" ]; then echo "Window not found" >&2; exit 1; fi; `+
		`if command -v import >/dev/null 2>&1; then `+
		`import -window "$wid" "$tmpfile"; `+
		`elif command -v scrot >/dev/null 2>&1; then `+
		`scrot -u "$tmpfile"; `+
		`else echo "no screenshot tool found" >&2; exit 1; fi && `+
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`, escaped)
}
