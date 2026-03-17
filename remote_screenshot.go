package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// pngMagicBytes is the 8-byte PNG file header signature.
var pngMagicBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// ParseScreenshotOutput extracts and validates base64-encoded PNG data from
// the screenshot command's stdout output. It strips whitespace, BOM markers,
// null bytes, and other non-base64 characters, then validates the encoding
// and confirms the decoded data starts with PNG magic bytes.
//
// The function tries standard base64 first, then falls back to raw (no-padding)
// base64 to handle platform differences in the `base64` command output.
func ParseScreenshotOutput(stdout string) (string, error) {
	// Strip UTF-8 BOM if present.
	cleaned := strings.TrimPrefix(stdout, "\xEF\xBB\xBF")
	cleaned = strings.TrimSpace(cleaned)

	// Remove all whitespace (newlines, spaces, tabs, carriage returns).
	cleaned = strings.Join(strings.Fields(cleaned), "")

	// Strip any remaining non-base64 characters (null bytes, zero-width
	// spaces, control characters, etc.) that shells or terminal emulators
	// may inject.
	cleaned = stripNonBase64(cleaned)

	if cleaned == "" {
		return "", fmt.Errorf("screenshot command produced no output")
	}

	// Try standard base64 (with padding) first.
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		// Fallback: try raw base64 (no padding) — some `base64` implementations
		// omit trailing '=' characters.
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "="))
		if err != nil {
			// Provide diagnostic info: show the first 80 chars of the cleaned
			// output so the log reveals what went wrong.
			preview := cleaned
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			return "", fmt.Errorf("invalid base64 encoding (len=%d, preview=%s)", len(cleaned), preview)
		}
	}

	if len(decoded) < len(pngMagicBytes) || !bytes.Equal(decoded[:len(pngMagicBytes)], pngMagicBytes) {
		return "", fmt.Errorf("output is not PNG (decoded %d bytes, header=%x)", len(decoded), safeHeader(decoded, 8))
	}

	// Re-encode to canonical standard base64 so downstream consumers always
	// receive a well-formed string regardless of the original encoding.
	canonical := base64.StdEncoding.EncodeToString(decoded)
	return canonical, nil
}

// stripNonBase64 removes any character that is not part of the standard base64
// alphabet (A-Z, a-z, 0-9, +, /, =). This handles BOM remnants, null bytes,
// zero-width spaces, and other invisible characters that may be injected by
// shells, terminal emulators, or PowerShell.
func stripNonBase64(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// safeHeader returns up to n bytes from data for diagnostic logging.
func safeHeader(data []byte, n int) []byte {
	if len(data) < n {
		return data
	}
	return data[:n]
}

// blankImageThreshold is the maximum average pixel brightness (0–255) below
// which a screenshot is considered blank/black. A fully black image has an
// average of 0; we allow a small margin for compression artifacts and minor
// noise (e.g. a cursor blinking on a lock screen).
const blankImageThreshold = 3

// isBlankImage decodes a base64-encoded PNG and returns true if the image is
// effectively blank (all or nearly all black pixels). This detects the common
// case where a locked/disconnected session produces a solid black screenshot.
//
// The check samples pixels adaptively for performance on large screenshots.
// Returns false (not blank) if the image cannot be decoded, so callers never
// discard a potentially valid screenshot due to a decode error.
func isBlankImage(base64Data string) bool {
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return false
	}

	img, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		return false // can't decode → assume not blank
	}

	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return true
	}

	return isImageBlank(img, blankImageThreshold)
}

// isImageBlank checks whether the given image is effectively blank by sampling
// pixels and computing the average brightness. The step parameter controls
// sampling density (every Nth pixel in each dimension).
func isImageBlank(img image.Image, threshold uint32) bool {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Adaptive step: sample ~10000 pixels max for performance.
	step := 1
	totalPixels := w * h
	if totalPixels > 10000 {
		step = int(isqrt(uint64(totalPixels / 10000)))
		if step < 1 {
			step = 1
		}
	}

	var totalBrightness uint64
	var count uint64

	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			// RGBA() returns 16-bit values; scale to 8-bit.
			brightness := (r>>8 + g>>8 + b>>8) / 3
			totalBrightness += uint64(brightness)
			count++
		}
	}

	if count == 0 {
		return true
	}

	avg := totalBrightness / count
	return avg <= uint64(threshold)
}

// isqrt returns the integer square root of n.
func isqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// BuildScreenshotCommand returns a platform-specific shell command string that
// captures a screenshot and outputs the result as raw base64-encoded PNG data
// to stdout. Temporary files are cleaned up on macOS and Linux regardless of
// success or failure.
func BuildScreenshotCommand() string {
	switch runtime.GOOS {
	case "windows":
		return buildWindowsScreenshotCommand()
	case "darwin":
		return buildDarwinScreenshotCommand()
	case "linux":
		return buildLinuxScreenshotCommand()
	default:
		return ""
	}
}

func buildWindowsScreenshotCommand() string {
	// Returns a pure PowerShell script block (without the powershell.exe prefix).
	// The caller (captureAndSend) invokes this via powershell -Command directly,
	// avoiding cmd.exe quote mangling that corrupts base64 output.
	//
	// Enhanced strategy for locked/disconnected sessions (5 attempts):
	// 1. Standard CopyFromScreen (BitBlt) — fastest, works when unlocked.
	// 2. BitBlt with CAPTUREBLT flag — captures layered windows.
	// 3. tscon reconnect — reconnects RDP session to console to restore
	//    desktop composition, then retries CopyFromScreen.
	// 4. PrintWindow composite — enumerates all visible top-level windows,
	//    captures each via PrintWindow (WM_PRINT), composites them onto a
	//    single bitmap. Works even without desktop composition because each
	//    window paints itself independently.
	// 5. If all fail, return a clear error message.
	return `Add-Type -AssemblyName System.Drawing; ` +
		`Add-Type -AssemblyName System.Windows.Forms; ` +
		`Add-Type @'` + "\n" +
		`using System; using System.Collections.Generic; using System.Drawing;` + "\n" +
		`using System.Runtime.InteropServices; using System.Text;` + "\n" +
		`public class ScreenUtil {` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();` + "\n" +
		`  [DllImport("user32.dll")] public static extern IntPtr GetDesktopWindow();` + "\n" +
		`  [DllImport("user32.dll")] public static extern IntPtr GetWindowDC(IntPtr hWnd);` + "\n" +
		`  [DllImport("user32.dll")] public static extern int ReleaseDC(IntPtr hWnd, IntPtr hDC);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern IntPtr CreateCompatibleDC(IntPtr hdc);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern IntPtr CreateCompatibleBitmap(IntPtr hdc, int w, int h);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern IntPtr SelectObject(IntPtr hdc, IntPtr obj);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern bool BitBlt(IntPtr hdcDest, int x1, int y1, int w, int h, IntPtr hdcSrc, int x2, int y2, uint rop);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern bool DeleteDC(IntPtr hdc);` + "\n" +
		`  [DllImport("gdi32.dll")] public static extern bool DeleteObject(IntPtr obj);` + "\n" +
		// PrintWindow for per-window capture.
		`  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint nFlags);` + "\n" +
		// Window enumeration for composite capture.
		`  public struct RECT { public int Left, Top, Right, Bottom; }` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);` + "\n" +
		`  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc proc, IntPtr lParam);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hWnd);` + "\n" +
		`  [DllImport("user32.dll", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int count);` + "\n" +
		`  [DllImport("user32.dll")] public static extern int GetWindowTextLength(IntPtr hWnd);` + "\n" +
		// DWM thumbnail for locked-session window capture.
		`  [DllImport("dwmapi.dll")] public static extern int DwmIsCompositionEnabled(out bool enabled);` + "\n" +
		// Window style for filtering.
		`  [DllImport("user32.dll")] public static extern int GetWindowLong(IntPtr hWnd, int nIndex);` + "\n" +
		`  public const int GWL_EXSTYLE = -20;` + "\n" +
		`  public const int WS_EX_TOOLWINDOW = 0x00000080;` + "\n" +
		`  public const int WS_EX_NOACTIVATE = 0x08000000;` + "\n" +
		`}` + "\n" +
		`'@;` +
		`[ScreenUtil]::SetProcessDPIAware() | Out-Null; ` +
		// --- Helper functions ---
		`function Test-BlankBitmap($bmp) { ` +
		`$step = [Math]::Max(1, [Math]::Floor([Math]::Sqrt($bmp.Width * $bmp.Height / 2000))); ` +
		`for ($y = 0; $y -lt $bmp.Height; $y += $step) { ` +
		`for ($x = 0; $x -lt $bmp.Width; $x += $step) { ` +
		`$px = $bmp.GetPixel($x, $y); ` +
		`if (($px.R + $px.G + $px.B) -gt 10) { return $false } ` +
		`} } return $true }; ` +
		`function ConvertTo-Base64Png($bmp) { ` +
		`$ms = New-Object System.IO.MemoryStream; ` +
		`$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); ` +
		`$b64 = [Convert]::ToBase64String($ms.ToArray()); ` +
		`$ms.Dispose(); return $b64 }; ` +
		`$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; ` +
		// ========== Attempt 1: standard CopyFromScreen ==========
		`$bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`try { $g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size) } catch { }; ` +
		`$g.Dispose(); ` +
		`if (-not (Test-BlankBitmap $bmp)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp; $bmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp.Dispose(); ` +
		// ========== Attempt 2: BitBlt with CAPTUREBLT ==========
		`$hDesktop = [ScreenUtil]::GetDesktopWindow(); ` +
		`$hDC = [ScreenUtil]::GetWindowDC($hDesktop); ` +
		`$memDC = [ScreenUtil]::CreateCompatibleDC($hDC); ` +
		`$hBmp = [ScreenUtil]::CreateCompatibleBitmap($hDC, $bounds.Width, $bounds.Height); ` +
		`$old = [ScreenUtil]::SelectObject($memDC, $hBmp); ` +
		`[ScreenUtil]::BitBlt($memDC, 0, 0, $bounds.Width, $bounds.Height, $hDC, 0, 0, 0x00CC0020 -bor 0x40000000) | Out-Null; ` +
		`[ScreenUtil]::SelectObject($memDC, $old) | Out-Null; ` +
		`$bmp2 = [System.Drawing.Image]::FromHbitmap($hBmp); ` +
		`[ScreenUtil]::DeleteDC($memDC) | Out-Null; ` +
		`[ScreenUtil]::ReleaseDC($hDesktop, $hDC) | Out-Null; ` +
		`[ScreenUtil]::DeleteObject($hBmp) | Out-Null; ` +
		`if (-not (Test-BlankBitmap $bmp2)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp2; $bmp2.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp2.Dispose(); ` +
		// ========== Attempt 3: tscon reconnect + retry ==========
		// When an RDP session is disconnected, the desktop compositor (DWM)
		// is suspended and all screen captures return black. Reconnecting
		// the session to the console via tscon restores the desktop.
		// This requires admin privileges and is best-effort.
		`$tsconOk = $false; ` +
		`try { ` +
		// Get current session ID from the process.
		`$sid = (Get-Process -Id $PID).SessionId; ` +
		// Try tscon to reconnect to console. This will fail without admin
		// privileges, which is fine — we just move to the next attempt.
		`$tsconResult = Start-Process -FilePath 'tscon.exe' -ArgumentList "$sid /dest:console" ` +
		`-NoNewWindow -Wait -PassThru -ErrorAction SilentlyContinue; ` +
		`if ($tsconResult -and $tsconResult.ExitCode -eq 0) { ` +
		// Give DWM a moment to reinitialize the desktop composition.
		`Start-Sleep -Milliseconds 1500; ` +
		`$tsconOk = $true } ` +
		`} catch { }; ` +
		`if ($tsconOk) { ` +
		`$bmp3 = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$g3 = [System.Drawing.Graphics]::FromImage($bmp3); ` +
		`try { $g3.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size) } catch { }; ` +
		`$g3.Dispose(); ` +
		`if (-not (Test-BlankBitmap $bmp3)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp3; $bmp3.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp3.Dispose() }; ` +
		// ========== Attempt 4: PrintWindow composite ==========
		// Enumerate all visible top-level windows and capture each using
		// PrintWindow (which sends WM_PRINT to the window, making it paint
		// itself into our DC). This works even when the desktop compositor
		// is inactive because each window renders independently.
		// We composite the results in Z-order (bottom to top) onto a single
		// bitmap the size of the screen.
		`$composite = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$cg = [System.Drawing.Graphics]::FromImage($composite); ` +
		`$cg.Clear([System.Drawing.Color]::FromArgb(30, 30, 30)); ` +
		`$windows = New-Object 'System.Collections.Generic.List[object]'; ` +
		`[ScreenUtil]::EnumWindows({ param($hwnd, $lp); ` +
		`if ([ScreenUtil]::IsWindowVisible($hwnd) -and -not [ScreenUtil]::IsIconic($hwnd)) { ` +
		// Filter out tool windows and zero-size windows.
		`$exStyle = [ScreenUtil]::GetWindowLong($hwnd, [ScreenUtil]::GWL_EXSTYLE); ` +
		`if (($exStyle -band [ScreenUtil]::WS_EX_TOOLWINDOW) -eq 0) { ` +
		`$wr = New-Object ScreenUtil+RECT; ` +
		`[ScreenUtil]::GetWindowRect($hwnd, [ref]$wr) | Out-Null; ` +
		`$ww = $wr.Right - $wr.Left; $wh = $wr.Bottom - $wr.Top; ` +
		`if ($ww -gt 0 -and $wh -gt 0) { ` +
		`$windows.Add(@{Handle=$hwnd; Left=$wr.Left; Top=$wr.Top; Width=$ww; Height=$wh}) } } } ` +
		`return $true }, [IntPtr]::Zero) | Out-Null; ` +
		// Reverse so we draw bottom windows first (EnumWindows returns top-to-bottom Z-order).
		`$windows.Reverse(); ` +
		`$capturedAny = $false; ` +
		`foreach ($win in $windows) { ` +
		`try { ` +
		`$wBmp = New-Object System.Drawing.Bitmap($win.Width, $win.Height); ` +
		`$wg = [System.Drawing.Graphics]::FromImage($wBmp); ` +
		`$whdc = $wg.GetHdc(); ` +
		// PW_RENDERFULLCONTENT = 0x2 for DWM-aware capture.
		`$pwOk = [ScreenUtil]::PrintWindow($win.Handle, $whdc, 2); ` +
		`$wg.ReleaseHdc($whdc); $wg.Dispose(); ` +
		`if ($pwOk) { ` +
		// Quick check: is this window capture non-blank?
		`$samplePx = $wBmp.GetPixel([Math]::Min(10, $win.Width-1), [Math]::Min(10, $win.Height-1)); ` +
		`$midPx = $wBmp.GetPixel([int]($win.Width/2), [int]($win.Height/2)); ` +
		`if (($samplePx.R + $samplePx.G + $samplePx.B + $midPx.R + $midPx.G + $midPx.B) -gt 5) { ` +
		// Draw this window onto the composite at its screen position.
		`$destX = $win.Left - $bounds.X; $destY = $win.Top - $bounds.Y; ` +
		`$cg.DrawImage($wBmp, $destX, $destY, $win.Width, $win.Height); ` +
		`$capturedAny = $true } }; ` +
		`$wBmp.Dispose() ` +
		`} catch { } }; ` +
		`$cg.Dispose(); ` +
		`if ($capturedAny -and -not (Test-BlankBitmap $composite)) { ` +
		`$b64 = ConvertTo-Base64Png $composite; $composite.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$composite.Dispose(); ` +
		// ========== All attempts failed ==========
		`Write-Error "screen is blank - session may be locked or disconnected (no active desktop). Tried: CopyFromScreen, BitBlt+CAPTUREBLT, tscon reconnect, PrintWindow composite"; exit 1`
}

func buildDarwinScreenshotCommand() string {
	// screencapture -x captures silently (no shutter sound).
	// On locked screens, screencapture may produce a black image.
	//
	// Strategy:
	// 1. Try screencapture -x (standard approach).
	// 2. Check if the image is all-black using python3 PIL pixel sampling.
	// 3. If blank, try screencapture -C (include cursor, sometimes helps).
	// 4. If still blank, report the error with lock status.
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`tmpfile2=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\" \"$tmpfile2\"" EXIT; ` +
		// Define a reusable blank-check function to avoid code duplication.
		// Takes a file path as $1, prints "true" if blank, "false" otherwise.
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
		// Check if screen is locked via CGSession (best-effort).
		`is_locked=$(python3 -c "import Quartz; d=Quartz.CGSessionCopyCurrentDictionary(); print('locked' if d and d.get('CGSSessionScreenIsLocked',0) else 'unlocked')" 2>/dev/null || echo "unknown"); ` +
		// Attempt 1: standard screencapture.
		`screencapture -x "$tmpfile" 2>/dev/null; ` +
		`is_blank=$(check_blank "$tmpfile"); ` +
		`if [ "$is_blank" != "true" ]; then ` +
		`base64 -i "$tmpfile"; exit 0; fi; ` +
		// Attempt 2: screencapture with -C flag (cursor capture mode).
		`screencapture -C "$tmpfile2" 2>/dev/null; ` +
		`is_blank2=$(check_blank "$tmpfile2"); ` +
		`if [ "$is_blank2" != "true" ]; then ` +
		`base64 -i "$tmpfile2"; exit 0; fi; ` +
		// Both attempts blank — report error with lock status.
		`echo "screen is blank - session may be locked ($is_locked) or display is off" >&2; exit 1`
}

func buildLinuxScreenshotCommand() string {
	// On Linux, locked screens or headless sessions (e.g. VNC disconnected)
	// can produce blank images. Strategy:
	// 1. Capture with available tool (scrot, gnome-screenshot, import, grim).
	// 2. Validate the image is not all-black using ImageMagick or python3 PIL.
	// 3. If blank, report with session lock state info.
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		// Define reusable blank-check function.
		`check_blank() { ` +
		`local f="$1"; ` +
		`if [ ! -f "$f" ] || [ ! -s "$f" ]; then echo "true"; return; fi; ` +
		// Prefer ImageMagick: get the overall mean brightness across all channels.
		`if command -v convert >/dev/null 2>&1; then ` +
		`mean=$(convert "$f" -colorspace Gray -format "%[fx:mean*255]" info: 2>/dev/null | cut -d. -f1); ` +
		`if [ -n "$mean" ] && [ "$mean" -le 3 ] 2>/dev/null; then echo "true"; else echo "false"; fi; return; fi; ` +
		// Fallback: python3 PIL.
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
		// No check tool available — assume not blank.
		`echo "false"; }; ` +
		// Capture screenshot.
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
		// Validate.
		`is_blank=$(check_blank "$tmpfile"); ` +
		`if [ "$is_blank" = "true" ]; then ` +
		`lock_info="unknown"; ` +
		`if command -v loginctl >/dev/null 2>&1; then ` +
		`lock_info=$(loginctl show-session $(loginctl --no-legend 2>/dev/null | awk "NR==1{print \$1}") -p LockedHint --value 2>/dev/null || echo "unknown"); fi; ` +
		`echo "screen is blank - session may be locked (locked=$lock_info) or display is off" >&2; exit 1; ` +
		`fi; ` +
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`
}

// sanitizeWindowTitle strips characters that could be used for shell injection
// in the window title parameter. Only alphanumeric, spaces, hyphens, underscores,
// dots, and common CJK characters are allowed.
func sanitizeWindowTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '(' || r == ')':
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			b.WriteRune(r)
		case r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
			b.WriteRune(r)
		case r >= 0xAC00 && r <= 0xD7AF: // Hangul
			b.WriteRune(r)
		default:
			// Skip potentially dangerous characters
		}
	}
	return b.String()
}

// BuildWindowScreenshotCommand returns a platform-specific shell command that
// captures a screenshot of a specific window by title and outputs base64 PNG
// to stdout. If the window is not found, the command should fail with a
// non-zero exit code.
func BuildWindowScreenshotCommand(windowTitle string) string {
	// Sanitize the title to prevent shell injection.
	windowTitle = sanitizeWindowTitle(windowTitle)
	if windowTitle == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return buildWindowsWindowScreenshotCommand(windowTitle)
	case "darwin":
		return buildDarwinWindowScreenshotCommand(windowTitle)
	case "linux":
		return buildLinuxWindowScreenshotCommand(windowTitle)
	default:
		return ""
	}
}

func buildWindowsWindowScreenshotCommand(windowTitle string) string {
	// Returns a pure PowerShell script block (without the powershell.exe prefix).
	// The caller (captureAndSend) invokes this via powershell -Command directly.
	// SetProcessDPIAware ensures correct coordinates on high-DPI displays.
	//
	// For window-specific screenshots, we use PrintWindow API as the primary
	// method. PrintWindow sends WM_PRINT to the target window, which renders
	// the window content into our DC — this works even when the session is
	// locked or the window is occluded, because it asks the window to paint
	// itself rather than reading from the screen buffer.
	//
	// Fallback: CopyFromScreen for the window rect (works when unlocked).
	// Escape single quotes in the title for PowerShell.
	escaped := strings.ReplaceAll(windowTitle, "'", "''")
	return fmt.Sprintf(
		`Add-Type -AssemblyName System.Drawing; `+
			`Add-Type -AssemblyName System.Windows.Forms; `+
			`Add-Type @'`+"\n"+
			`using System; using System.Runtime.InteropServices; using System.Text;`+"\n"+
			`public class WinAPI {`+"\n"+
			`  public struct RECT { public int Left, Top, Right, Bottom; }`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);`+"\n"+
			`  [DllImport("user32.dll")] public static extern IntPtr FindWindow(string cls, string title);`+"\n"+
			`  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc proc, IntPtr lParam);`+"\n"+
			`  [DllImport("user32.dll", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int count);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();`+"\n"+
			`  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint nFlags);`+"\n"+
			`  [DllImport("user32.dll")] public static extern IntPtr GetWindowDC(IntPtr hWnd);`+"\n"+
			`  [DllImport("user32.dll")] public static extern int ReleaseDC(IntPtr hWnd, IntPtr hDC);`+"\n"+
			`}`+"\n"+
			`'@;`+
			`[WinAPI]::SetProcessDPIAware() | Out-Null; `+
			// Helper: check if bitmap is blank.
			`function Test-BlankBitmap($bmp) { `+
			`$step = [Math]::Max(1, [Math]::Floor([Math]::Sqrt($bmp.Width * $bmp.Height / 2000))); `+
			`for ($y = 0; $y -lt $bmp.Height; $y += $step) { `+
			`for ($x = 0; $x -lt $bmp.Width; $x += $step) { `+
			`$px = $bmp.GetPixel($x, $y); `+
			`if (($px.R + $px.G + $px.B) -gt 10) { return $false } `+
			`} } return $true }; `+
			// Helper: convert bitmap to base64 PNG.
			`function ConvertTo-Base64Png($bmp) { `+
			`$ms = New-Object IO.MemoryStream; `+
			`$bmp.Save($ms, [Drawing.Imaging.ImageFormat]::Png); `+
			`$b64 = [Convert]::ToBase64String($ms.ToArray()); `+
			`$ms.Dispose(); return $b64 }; `+
			// Find the target window.
			`$target = '%s'; `+
			`$found = $null; `+
			`[WinAPI]::EnumWindows({ param($h,$l); `+
			`if ([WinAPI]::IsWindowVisible($h)) { `+
			`$sb = New-Object Text.StringBuilder 256; `+
			`[WinAPI]::GetWindowText($h, $sb, 256) | Out-Null; `+
			`$t = $sb.ToString(); `+
			`if ($t -like ('*' + $target + '*')) { $script:found = $h } `+
			`} return $true }, [IntPtr]::Zero) | Out-Null; `+
			`if (-not $found) { Write-Error 'Window not found'; exit 1 }; `+
			`$r = New-Object WinAPI+RECT; `+
			`[WinAPI]::GetWindowRect($found, [ref]$r) | Out-Null; `+
			`$w = $r.Right - $r.Left; $h = $r.Bottom - $r.Top; `+
			`if ($w -le 0 -or $h -le 0) { Write-Error 'Invalid window size'; exit 1 }; `+
			// Attempt 1: PrintWindow — works even on locked/occluded windows
			// because it asks the window to paint itself (WM_PRINT).
			// Flag 0x2 = PW_RENDERFULLCONTENT for better DWM capture.
			`$bmp = New-Object Drawing.Bitmap($w, $h); `+
			`$g = [Drawing.Graphics]::FromImage($bmp); `+
			`$hdc = $g.GetHdc(); `+
			`$ok = [WinAPI]::PrintWindow($found, $hdc, 2); `+
			`$g.ReleaseHdc($hdc); $g.Dispose(); `+
			`if ($ok -and -not (Test-BlankBitmap $bmp)) { `+
			`$b64 = ConvertTo-Base64Png $bmp; $bmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; `+
			`$bmp.Dispose(); `+
			// Attempt 2: CopyFromScreen for the window rect (works when unlocked).
			`$bmp2 = New-Object Drawing.Bitmap($w, $h); `+
			`$g2 = [Drawing.Graphics]::FromImage($bmp2); `+
			`try { $g2.CopyFromScreen($r.Left, $r.Top, 0, 0, (New-Object Drawing.Size($w,$h))) } catch { }; `+
			`$g2.Dispose(); `+
			`if (-not (Test-BlankBitmap $bmp2)) { `+
			`$b64 = ConvertTo-Base64Png $bmp2; $bmp2.Dispose(); [Console]::Out.Write($b64); exit 0 }; `+
			`$bmp2.Dispose(); `+
			`Write-Error 'Window screenshot is blank - session may be locked or disconnected'; exit 1`, escaped)
}

func buildDarwinWindowScreenshotCommand(windowTitle string) string {
	// Use osascript to find the window ID, then screencapture -l <windowID>
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

// DetectDisplayServer checks whether a graphical display environment is
// available on the current platform.
// Returns (available, reason) where reason is non-empty when available is false.
//   - Windows: always returns true (desktop app necessarily has display)
//   - macOS: always returns true (Quartz display server is available for desktop apps)
//   - Linux: checks DISPLAY or WAYLAND_DISPLAY environment variables
func DetectDisplayServer() (bool, string) {
	switch runtime.GOOS {
	case "windows":
		return true, ""
	case "darwin":
		return true, ""
	case "linux":
		if display := os.Getenv("DISPLAY"); display != "" {
			return true, ""
		}
		if waylandDisplay := os.Getenv("WAYLAND_DISPLAY"); waylandDisplay != "" {
			return true, ""
		}
		return false, "no display server detected: neither DISPLAY nor WAYLAND_DISPLAY environment variable is set"
	default:
		return false, fmt.Sprintf("unsupported platform for display detection: %s", runtime.GOOS)
	}
}

// CaptureScreenshot executes the full screenshot capture flow for the given
// session: detect display → build command → execute → parse output → send image.
// Only SDK-mode sessions are supported; PTY sessions return an error.
func (m *RemoteSessionManager) CaptureScreenshot(sessionID string) error {
	cmdStr := BuildScreenshotCommand()
	if cmdStr == "" {
		return fmt.Errorf("screenshot capture is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, "", cmdStr)
}

// CaptureWindowScreenshot captures a screenshot of a specific window by title
// and sends it through the image transfer pipeline. The windowTitle is matched
// as a substring against visible window titles.
func (m *RemoteSessionManager) CaptureWindowScreenshot(sessionID, windowTitle string) error {
	if strings.TrimSpace(windowTitle) == "" {
		return fmt.Errorf("window title must not be empty")
	}
	cmdStr := BuildWindowScreenshotCommand(windowTitle)
	if cmdStr == "" {
		return fmt.Errorf("window screenshot is not supported on %s", runtime.GOOS)
	}
	return m.captureAndSend(sessionID, windowTitle, cmdStr)
}

// captureAndSend is the shared implementation for CaptureScreenshot and
// CaptureWindowScreenshot. It validates the session, executes the shell
// command, parses the base64 output, and sends the image via the hub.
func (m *RemoteSessionManager) captureAndSend(sessionID, label, cmdStr string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	// Screenshot capture works for SDK-mode and Gemini ACP sessions.
	// The capture runs outside the CLI process (platform-native commands),
	// so it doesn't depend on the CLI tool's own image support.
	switch s.Exec.(type) {
	case *SDKExecutionHandle, *GeminiACPExecutionHandle:
		// supported
	default:
		return fmt.Errorf("screenshot capture is only supported in SDK and ACP mode sessions")
	}

	available, reason := DetectDisplayServer()
	if !available {
		return fmt.Errorf("screenshot requires a graphical display environment: %s", reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		// Use PowerShell directly to avoid cmd.exe quote mangling that
		// corrupts base64 output. The Windows screenshot commands return
		// pure PowerShell script blocks (no powershell.exe prefix).
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logLabel := "fullscreen"
	if label != "" {
		logLabel = fmt.Sprintf("window %q", label)
	}
	m.app.log(fmt.Sprintf("[screenshot] capturing %s for session=%s", logLabel, sessionID))

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("screenshot command timed out after 45s")
		}
		m.app.log(fmt.Sprintf("[screenshot] capture failed for session=%s: %v, stderr: %s", sessionID, err, stderr.String()))
		return fmt.Errorf("screenshot command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	rawOut := stdout.String()
	base64Data, err := ParseScreenshotOutput(rawOut)
	if err != nil {
		// Log diagnostic info: raw output length and first 120 chars to help
		// identify what the screenshot command actually produced.
		preview := rawOut
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		m.app.log(fmt.Sprintf("[screenshot] failed to parse output for session=%s: %v (stdout_len=%d, stderr=%q, preview=%q)",
			sessionID, err, len(rawOut), strings.TrimSpace(stderr.String()), preview))
		return fmt.Errorf("screenshot output parse error: %w", err)
	}

	// Server-side blank image detection as a safety net. Even if the
	// platform-specific command didn't detect a blank image (e.g. the
	// blank-check tools weren't available), we validate here before
	// sending a useless black image to the client.
	if isBlankImage(base64Data) {
		m.app.log(fmt.Sprintf("[screenshot] captured image is blank/black for session=%s — session may be locked or display is off", sessionID))
		return fmt.Errorf("screenshot is blank (all black) — the session may be locked, the display may be off, or the remote desktop is disconnected")
	}

	msg := NewImageTransferMessage(sessionID, "image/png", base64Data)
	if err := ValidateImageTransferMessage(msg, ImageOutputSizeLimit); err != nil {
		m.app.log(fmt.Sprintf("[screenshot] image exceeds size limit for session=%s: %v", sessionID, err))
		return err
	}

	if m.hubClient != nil {
		if err := m.hubClient.SendSessionImage(msg); err != nil {
			m.app.log(fmt.Sprintf("[screenshot] failed to send image for session=%s: %v", sessionID, err))
			return err
		}
	}

	m.app.log(fmt.Sprintf("[screenshot] successfully captured %s for session=%s", logLabel, sessionID))
	return nil
}
