package remote

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Stubs for non-Windows platforms — the compiler needs these on Windows
// where the _unix.go file is excluded.

func enumDisplaysDarwin() ([]DisplayInfo, error) {
	return nil, fmt.Errorf("enumDisplaysDarwin: not available on windows")
}

func enumDisplaysLinux() ([]DisplayInfo, error) {
	return nil, fmt.Errorf("enumDisplaysLinux: not available on windows")
}

func buildMultiMonitorScreenshotDarwin() string       { return "" }
func buildMultiMonitorScreenshotLinux() string        { return "" }
func buildSingleMonitorScreenshotDarwin(_ int) string { return "" }
func buildSingleMonitorScreenshotLinux(_ int) string  { return "" }

// enumDisplaysWindows enumerates all connected displays by running a
// PowerShell one-liner that queries [System.Windows.Forms.Screen]::AllScreens
// and outputs JSON.
func enumDisplaysWindows() ([]DisplayInfo, error) {
	psScript := `Add-Type -AssemblyName System.Windows.Forms;` +
		`Add-Type -AssemblyName System.Drawing;` +
		`$screens = [System.Windows.Forms.Screen]::AllScreens;` +
		`$result = @();` +
		`$i = 0;` +
		`foreach ($s in $screens) {` +
		`  $result += @{` +
		`    index = $i;` +
		`    name = $s.DeviceName;` +
		`    x = $s.Bounds.X;` +
		`    y = $s.Bounds.Y;` +
		`    width = $s.Bounds.Width;` +
		`    height = $s.Bounds.Height;` +
		`    scale = [Math]::Round(96.0 / 96.0, 2);` +
		`    primary = $s.Primary` +
		`  };` +
		`  $i++` +
		`};` +
		`$result | ConvertTo-Json -Compress`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("enumDisplaysWindows: powershell failed: %w", err)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return nil, fmt.Errorf("enumDisplaysWindows: empty output from powershell")
	}

	// PowerShell outputs a single object (not array) when there's only one screen.
	// Normalize to always be an array.
	if !strings.HasPrefix(outStr, "[") {
		outStr = "[" + outStr + "]"
	}

	var raw []struct {
		Index   int     `json:"index"`
		Name    string  `json:"name"`
		X       int     `json:"x"`
		Y       int     `json:"y"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Scale   float64 `json:"scale"`
		Primary bool    `json:"primary"`
	}
	if err := json.Unmarshal([]byte(outStr), &raw); err != nil {
		return nil, fmt.Errorf("enumDisplaysWindows: json parse failed: %w (output: %s)", err, outStr)
	}

	displays := make([]DisplayInfo, len(raw))
	for i, r := range raw {
		displays[i] = DisplayInfo{
			Index:   r.Index,
			Name:    r.Name,
			X:       r.X,
			Y:       r.Y,
			Width:   r.Width,
			Height:  r.Height,
			Scale:   r.Scale,
			Primary: r.Primary,
		}
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("enumDisplaysWindows: no displays found")
	}
	return displays, nil
}

// buildMultiMonitorScreenshotWindows generates a PowerShell command that
// captures ALL monitors into one stitched image using virtual desktop
// coordinates (SM_XVIRTUALSCREEN, SM_YVIRTUALSCREEN, SM_CXVIRTUALSCREEN,
// SM_CYVIRTUALSCREEN) instead of PrimaryScreen.Bounds.
//
// It preserves the existing 5-level degradation chain:
//   1. CopyFromScreen (virtual desktop)
//   2. BitBlt (virtual desktop)
//   3. tscon + CopyFromScreen (virtual desktop)
//   4. PrintWindow composite (virtual desktop)
//   5. DXGI capture
//
// Each level uses virtual desktop coordinates so all monitors are captured.
func buildMultiMonitorScreenshotWindows() string {
	return buildMultiMonVirtualDesktopPreamble() +
		buildMultiMonLevel1CopyFromScreen() +
		buildMultiMonLevel2BitBlt() +
		buildMultiMonLevel3Tscon() +
		buildMultiMonLevel4PrintWindow() +
		buildMultiMonLevel5DXGI() +
		`Write-Error "screen is blank - all 5 multi-monitor capture methods failed"; exit 1`
}

// buildMultiMonVirtualDesktopPreamble returns the PowerShell preamble that
// loads assemblies, defines native helpers, sets DPI awareness, and computes
// virtual desktop bounds ($vx, $vy, $vw, $vh) covering all monitors.
func buildMultiMonVirtualDesktopPreamble() string {
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
		`  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint nFlags);` + "\n" +
		`  public struct RECT { public int Left, Top, Right, Bottom; }` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);` + "\n" +
		`  public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc proc, IntPtr lParam);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);` + "\n" +
		`  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hWnd);` + "\n" +
		`  [DllImport("user32.dll", CharSet=CharSet.Auto)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder sb, int count);` + "\n" +
		`  [DllImport("user32.dll")] public static extern int GetWindowTextLength(IntPtr hWnd);` + "\n" +
		`  [DllImport("dwmapi.dll")] public static extern int DwmIsCompositionEnabled(out bool enabled);` + "\n" +
		`  [DllImport("user32.dll")] public static extern int GetWindowLong(IntPtr hWnd, int nIndex);` + "\n" +
		`  [DllImport("user32.dll")] public static extern int GetSystemMetrics(int nIndex);` + "\n" +
		`  public const int GWL_EXSTYLE = -20;` + "\n" +
		`  public const int WS_EX_TOOLWINDOW = 0x00000080;` + "\n" +
		`  public const int WS_EX_NOACTIVATE = 0x08000000;` + "\n" +
		`  public const int SM_XVIRTUALSCREEN = 76;` + "\n" +
		`  public const int SM_YVIRTUALSCREEN = 77;` + "\n" +
		`  public const int SM_CXVIRTUALSCREEN = 78;` + "\n" +
		`  public const int SM_CYVIRTUALSCREEN = 79;` + "\n" +
		`}` + "\n" +
		`'@;` +
		`[ScreenUtil]::SetProcessDPIAware() | Out-Null; ` +
		// Helper: Test-BlankBitmap
		`function Test-BlankBitmap($bmp) { ` +
		`$step = [Math]::Max(1, [Math]::Floor([Math]::Sqrt($bmp.Width * $bmp.Height / 2000))); ` +
		`for ($y = 0; $y -lt $bmp.Height; $y += $step) { ` +
		`for ($x = 0; $x -lt $bmp.Width; $x += $step) { ` +
		`$px = $bmp.GetPixel($x, $y); ` +
		`if (($px.R + $px.G + $px.B) -gt 10) { return $false } ` +
		`} } return $true }; ` +
		// Helper: ConvertTo-Base64Png
		`function ConvertTo-Base64Png($bmp) { ` +
		`$ms = New-Object System.IO.MemoryStream; ` +
		`$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); ` +
		`$b64 = [Convert]::ToBase64String($ms.ToArray()); ` +
		`$ms.Dispose(); return $b64 }; ` +
		// Compute virtual desktop bounds covering all monitors
		`$vx = [ScreenUtil]::GetSystemMetrics([ScreenUtil]::SM_XVIRTUALSCREEN); ` +
		`$vy = [ScreenUtil]::GetSystemMetrics([ScreenUtil]::SM_YVIRTUALSCREEN); ` +
		`$vw = [ScreenUtil]::GetSystemMetrics([ScreenUtil]::SM_CXVIRTUALSCREEN); ` +
		`$vh = [ScreenUtil]::GetSystemMetrics([ScreenUtil]::SM_CYVIRTUALSCREEN); ` +
		`if ($vw -le 0 -or $vh -le 0) { ` +
		`$vx = 0; $vy = 0; ` +
		`$vw = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width; ` +
		`$vh = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height }; ` +
		// Lock check
		`$isLocked = $false; ` +
		`try { ` +
		`Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; ` +
		`public class LockCheck { ` +
		`[DllImport("user32.dll")] static extern IntPtr OpenInputDesktop(uint dwFlags, bool fInherit, uint dwDesiredAccess); ` +
		`[DllImport("user32.dll")] static extern bool CloseDesktop(IntPtr hDesktop); ` +
		`public static bool IsLocked() { IntPtr h = OpenInputDesktop(0, false, 0x0001); if (h == IntPtr.Zero) return true; CloseDesktop(h); return false; } ` +
		`}' -ErrorAction SilentlyContinue; ` +
		`$isLocked = [LockCheck]::IsLocked() ` +
		`} catch { }; ` +
		`if ($isLocked) { ` +
		`}; ` +
		`if (-not $isLocked) { `
}

// buildMultiMonLevel1CopyFromScreen returns the PowerShell fragment for
// Level 1: CopyFromScreen using virtual desktop coordinates.
func buildMultiMonLevel1CopyFromScreen() string {
	return `$bmp = New-Object System.Drawing.Bitmap($vw, $vh); ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`try { $g.CopyFromScreen($vx, $vy, 0, 0, (New-Object System.Drawing.Size($vw, $vh))) } catch { }; ` +
		`$g.Dispose(); ` +
		`if (-not (Test-BlankBitmap $bmp)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp; $bmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); `
}

// buildMultiMonLevel2BitBlt returns the PowerShell fragment for
// Level 2: BitBlt from desktop DC using virtual desktop coordinates.
func buildMultiMonLevel2BitBlt() string {
	return `$hDesktop = [ScreenUtil]::GetDesktopWindow(); ` +
		`$hDC = [ScreenUtil]::GetWindowDC($hDesktop); ` +
		`$memDC = [ScreenUtil]::CreateCompatibleDC($hDC); ` +
		`$hBmp = [ScreenUtil]::CreateCompatibleBitmap($hDC, $vw, $vh); ` +
		`$old = [ScreenUtil]::SelectObject($memDC, $hBmp); ` +
		`[ScreenUtil]::BitBlt($memDC, 0, 0, $vw, $vh, $hDC, $vx, $vy, 0x00CC0020 -bor 0x40000000) | Out-Null; ` +
		`[ScreenUtil]::SelectObject($memDC, $old) | Out-Null; ` +
		`$bmp2 = [System.Drawing.Image]::FromHbitmap($hBmp); ` +
		`[ScreenUtil]::DeleteDC($memDC) | Out-Null; ` +
		`[ScreenUtil]::ReleaseDC($hDesktop, $hDC) | Out-Null; ` +
		`[ScreenUtil]::DeleteObject($hBmp) | Out-Null; ` +
		`if (-not (Test-BlankBitmap $bmp2)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp2; $bmp2.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp2.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); `
}

// buildMultiMonLevel3Tscon returns the PowerShell fragment for
// Level 3: tscon session reconnect + CopyFromScreen using virtual desktop coordinates.
func buildMultiMonLevel3Tscon() string {
	return `$tsconOk = $false; ` +
		`try { ` +
		`$sid = (Get-Process -Id $PID).SessionId; ` +
		`$tsconResult = Start-Process -FilePath 'tscon.exe' -ArgumentList "$sid /dest:console" ` +
		`-NoNewWindow -Wait -PassThru -ErrorAction SilentlyContinue; ` +
		`if ($tsconResult -and $tsconResult.ExitCode -eq 0) { ` +
		`Start-Sleep -Milliseconds 1500; ` +
		`$tsconOk = $true } ` +
		`} catch { }; ` +
		`if ($tsconOk) { ` +
		`$bmp3 = New-Object System.Drawing.Bitmap($vw, $vh); ` +
		`$g3 = [System.Drawing.Graphics]::FromImage($bmp3); ` +
		`try { $g3.CopyFromScreen($vx, $vy, 0, 0, (New-Object System.Drawing.Size($vw, $vh))) } catch { }; ` +
		`$g3.Dispose(); ` +
		`if (-not (Test-BlankBitmap $bmp3)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp3; $bmp3.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp3.Dispose() }; ` +
		`}; ` + // closes the if (-not $isLocked) block
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); `
}

// buildMultiMonLevel4PrintWindow returns the PowerShell fragment for
// Level 4: PrintWindow composite using virtual desktop coordinates.
func buildMultiMonLevel4PrintWindow() string {
	return `$composite = New-Object System.Drawing.Bitmap($vw, $vh); ` +
		`$cg = [System.Drawing.Graphics]::FromImage($composite); ` +
		`$cg.Clear([System.Drawing.Color]::FromArgb(30, 30, 30)); ` +
		`$windows = New-Object 'System.Collections.Generic.List[object]'; ` +
		`[ScreenUtil]::EnumWindows({ param($hwnd, $lp); ` +
		`if ([ScreenUtil]::IsWindowVisible($hwnd) -and -not [ScreenUtil]::IsIconic($hwnd)) { ` +
		`$exStyle = [ScreenUtil]::GetWindowLong($hwnd, [ScreenUtil]::GWL_EXSTYLE); ` +
		`if (($exStyle -band [ScreenUtil]::WS_EX_TOOLWINDOW) -eq 0) { ` +
		`$wr = New-Object ScreenUtil+RECT; ` +
		`[ScreenUtil]::GetWindowRect($hwnd, [ref]$wr) | Out-Null; ` +
		`$ww = $wr.Right - $wr.Left; $wh = $wr.Bottom - $wr.Top; ` +
		`if ($ww -gt 0 -and $wh -gt 0) { ` +
		`$windows.Add(@{Handle=$hwnd; Left=$wr.Left; Top=$wr.Top; Width=$ww; Height=$wh}) } } } ` +
		`return $true }, [IntPtr]::Zero) | Out-Null; ` +
		`$windows.Reverse(); ` +
		`$capturedAny = $false; ` +
		`foreach ($win in $windows) { ` +
		`try { ` +
		`$pwFlags = @(2, 3, 0); ` +
		`$gotWin = $false; ` +
		`foreach ($fl in $pwFlags) { ` +
		`$wBmpT = New-Object System.Drawing.Bitmap($win.Width, $win.Height); ` +
		`$wgT = [System.Drawing.Graphics]::FromImage($wBmpT); ` +
		`$whdcT = $wgT.GetHdc(); ` +
		`$res = [ScreenUtil]::PrintWindow($win.Handle, $whdcT, $fl); ` +
		`$wgT.ReleaseHdc($whdcT); $wgT.Dispose(); ` +
		`if ($res) { ` +
		`$sp = $wBmpT.GetPixel([Math]::Min(10, $win.Width-1), [Math]::Min(10, $win.Height-1)); ` +
		`$mp = $wBmpT.GetPixel([int]($win.Width/2), [int]($win.Height/2)); ` +
		`if (($sp.R + $sp.G + $sp.B + $mp.R + $mp.G + $mp.B) -gt 5) { ` +
		// Use virtual desktop offset: window position relative to virtual origin
		`$destX = $win.Left - $vx; $destY = $win.Top - $vy; ` +
		`$cg.DrawImage($wBmpT, $destX, $destY, $win.Width, $win.Height); ` +
		`$capturedAny = $true; $gotWin = $true; $wBmpT.Dispose(); break } }; ` +
		`$wBmpT.Dispose() }; ` +
		`} catch { } }; ` +
		`$cg.Dispose(); ` +
		`if ($capturedAny -and -not (Test-BlankBitmap $composite)) { ` +
		`$b64 = ConvertTo-Base64Png $composite; $composite.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$composite.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); `
}

// buildMultiMonLevel5DXGI returns the PowerShell fragment for
// Level 5: DXGI Desktop Duplication capture.
// Note: DXGI DuplicateOutput captures the primary adapter's output by default.
// For true multi-monitor DXGI, we would need to enumerate all adapters/outputs,
// but this serves as the final fallback.
func buildMultiMonLevel5DXGI() string {
	return buildWindowsDXGICapture()
}

// buildSingleMonitorScreenshotWindows generates a PowerShell command that
// captures only the specified monitor by index. It first captures the full
// virtual desktop, then crops to the target monitor's region.
//
// The monitor region is determined at runtime by querying
// [System.Windows.Forms.Screen]::AllScreens[screenIndex].Bounds.
func buildSingleMonitorScreenshotWindows(screenIndex int) string {
	// Use the same preamble as multi-monitor (includes ScreenUtil, helpers, lock check)
	// then crop to the target monitor with BitBlt fallback for blank images.
	return buildMultiMonVirtualDesktopPreamble() +
		fmt.Sprintf(
			// Validate screen index
			`$screens = [System.Windows.Forms.Screen]::AllScreens; `+
				`$idx = %d; `+
				`if ($idx -lt 0 -or $idx -ge $screens.Length) { `+
				`Write-Error "screen index $idx out of range: $($screens.Length) display(s) available"; exit 1 }; `+
				`$target = $screens[$idx].Bounds; `+
				`$cropX = $target.X - $vx; `+
				`$cropY = $target.Y - $vy; `+
				`$cropRect = New-Object System.Drawing.Rectangle($cropX, $cropY, $target.Width, $target.Height); `,
			screenIndex) +
		// Level 1: CopyFromScreen (same as multi-mon but crop to target)
		`$fullBmp = New-Object System.Drawing.Bitmap($vw, $vh); ` +
		`$g = [System.Drawing.Graphics]::FromImage($fullBmp); ` +
		`try { $g.CopyFromScreen($vx, $vy, 0, 0, (New-Object System.Drawing.Size($vw, $vh))) } catch { }; ` +
		`$g.Dispose(); ` +
		`$monBmp = $fullBmp.Clone($cropRect, $fullBmp.PixelFormat); ` +
		`$fullBmp.Dispose(); ` +
		`if (-not (Test-BlankBitmap $monBmp)) { ` +
		`$b64 = ConvertTo-Base64Png $monBmp; $monBmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$monBmp.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); ` +
		// Level 2: BitBlt+CAPTUREBLT on full virtual desktop, then crop
		`$hDesktop = [ScreenUtil]::GetDesktopWindow(); ` +
		`$hDC = [ScreenUtil]::GetWindowDC($hDesktop); ` +
		`$memDC = [ScreenUtil]::CreateCompatibleDC($hDC); ` +
		`$hBmp = [ScreenUtil]::CreateCompatibleBitmap($hDC, $vw, $vh); ` +
		`$old = [ScreenUtil]::SelectObject($memDC, $hBmp); ` +
		`[ScreenUtil]::BitBlt($memDC, 0, 0, $vw, $vh, $hDC, $vx, $vy, 0x00CC0020 -bor 0x40000000) | Out-Null; ` +
		`[ScreenUtil]::SelectObject($memDC, $old) | Out-Null; ` +
		`$bmp2 = [System.Drawing.Image]::FromHbitmap($hBmp); ` +
		`[ScreenUtil]::DeleteDC($memDC) | Out-Null; ` +
		`[ScreenUtil]::ReleaseDC($hDesktop, $hDC) | Out-Null; ` +
		`[ScreenUtil]::DeleteObject($hBmp) | Out-Null; ` +
		`$monBmp2 = $bmp2.Clone($cropRect, $bmp2.PixelFormat); ` +
		`$bmp2.Dispose(); ` +
		`if (-not (Test-BlankBitmap $monBmp2)) { ` +
		`$b64 = ConvertTo-Base64Png $monBmp2; $monBmp2.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$monBmp2.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); ` +
		// Close the if(-not $isLocked) block opened by the preamble
		`}; ` +
		`Write-Error "single monitor screenshot is blank - all capture methods failed"; exit 1`
}
