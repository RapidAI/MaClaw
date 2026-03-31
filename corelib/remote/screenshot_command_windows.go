package remote

import (
	"fmt"
	"strings"
)

// Darwin/Linux stubs — never reached on Windows, but the compiler needs them.

func buildDarwinScreenshotCommand() string              { return "" }
func buildLinuxScreenshotCommand() string               { return "" }
func buildDarwinWindowScreenshotCommand(_ string) string { return "" }
func buildLinuxWindowScreenshotCommand(_ string) string  { return "" }

func buildWindowsScreenshotCommand() string {
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
		`  public const int GWL_EXSTYLE = -20;` + "\n" +
		`  public const int WS_EX_TOOLWINDOW = 0x00000080;` + "\n" +
		`  public const int WS_EX_NOACTIVATE = 0x08000000;` + "\n" +
		`}` + "\n" +
		`'@;` +
		`[ScreenUtil]::SetProcessDPIAware() | Out-Null; ` +
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
		`if (-not $isLocked) { ` +
		`$bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`try { $g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size) } catch { }; ` +
		`$g.Dispose(); ` +
		`if (-not (Test-BlankBitmap $bmp)) { ` +
		`$b64 = ConvertTo-Base64Png $bmp; $bmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`$bmp.Dispose(); ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); ` +
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
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); ` +
		`$tsconOk = $false; ` +
		`try { ` +
		`$sid = (Get-Process -Id $PID).SessionId; ` +
		`$tsconResult = Start-Process -FilePath 'tscon.exe' -ArgumentList "$sid /dest:console" ` +
		`-NoNewWindow -Wait -PassThru -ErrorAction SilentlyContinue; ` +
		`if ($tsconResult -and $tsconResult.ExitCode -eq 0) { ` +
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
		`}; ` +
		`[System.GC]::Collect(); [System.GC]::WaitForPendingFinalizers(); ` +
		buildWindowsPrintWindowComposite() +
		buildWindowsDXGICapture() +
		`Write-Error "screen is blank - all 5 capture methods failed"; exit 1`
}

func buildWindowsPrintWindowComposite() string {
	return `$composite = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height); ` +
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
		`$destX = $win.Left - $bounds.X; $destY = $win.Top - $bounds.Y; ` +
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

func buildWindowsDXGICapture() string {
	return `try { ` +
		`Add-Type @'` + "\n" +
		`using System;` + "\n" +
		`using System.Drawing;` + "\n" +
		`using System.Drawing.Imaging;` + "\n" +
		`using System.Runtime.InteropServices;` + "\n" +
		`public class DxgiCapture {` + "\n" +
		`  [DllImport("d3d11.dll")]` + "\n" +
		`  static extern int D3D11CreateDevice(IntPtr pAdapter, int DriverType, IntPtr Software,` + "\n" +
		`    uint Flags, IntPtr pFeatureLevels, uint FeatureLevels, uint SDKVersion,` + "\n" +
		`    out IntPtr ppDevice, IntPtr pFeatureLevel, out IntPtr ppImmediateContext);` + "\n" +
		`  [DllImport("dxgi.dll")]` + "\n" +
		`  static extern int CreateDXGIFactory1(ref Guid riid, out IntPtr ppFactory);` + "\n" +
		`  static IntPtr VF(IntPtr obj, int slot) {` + "\n" +
		`    IntPtr vt = Marshal.ReadIntPtr(obj);` + "\n" +
		`    return Marshal.ReadIntPtr(vt, slot * IntPtr.Size);` + "\n" +
		`  }` + "\n" +
		`  delegate int EnumAdapters1D(IntPtr s, uint i, out IntPtr a);` + "\n" +
		`  delegate int EnumOutputsD(IntPtr s, uint i, out IntPtr o);` + "\n" +
		`  delegate int QID(IntPtr s, ref Guid g, out IntPtr p);` + "\n" +
		`  delegate int DupOutputD(IntPtr s, IntPtr dev, out IntPtr d);` + "\n" +
		`  delegate int AcquireD(IntPtr s, uint ms, IntPtr info, out IntPtr res);` + "\n" +
		`  delegate int ReleaseFrameD(IntPtr s);` + "\n" +
		`  delegate int ReleaseD(IntPtr s);` + "\n" +
		`  delegate int GetDescD(IntPtr s, IntPtr descBuf);` + "\n" +
		`  [StructLayout(LayoutKind.Sequential)]` + "\n" +
		`  struct TexDesc {` + "\n" +
		`    public uint Width, Height, MipLevels, ArraySize;` + "\n" +
		`    public uint Format, SampleCount, SampleQuality, Usage;` + "\n" +
		`    public uint BindFlags, CPUAccessFlags, MiscFlags;` + "\n" +
		`  }` + "\n" +
		`  delegate int CreateTex2DD(IntPtr s, ref TexDesc d, IntPtr init, out IntPtr tex);` + "\n" +
		`  delegate void CopyResD(IntPtr s, IntPtr dst, IntPtr src);` + "\n" +
		`  [StructLayout(LayoutKind.Sequential)]` + "\n" +
		`  struct MappedSub { public IntPtr pData; public uint RowPitch; public uint DepthPitch; }` + "\n" +
		`  delegate int MapD(IntPtr s, IntPtr res, uint sub, uint mt, uint fl, out MappedSub m);` + "\n" +
		`  delegate void UnmapD(IntPtr s, IntPtr res, uint sub);` + "\n" +
		`  public static Bitmap Capture() {` + "\n" +
		`    Guid fGuid = new Guid("770aae78-f26f-4dba-a829-253c83d1b387");` + "\n" +
		`    IntPtr factory; int hr = CreateDXGIFactory1(ref fGuid, out factory);` + "\n" +
		`    if (hr != 0) throw new Exception("DXGI factory: 0x" + hr.ToString("X"));` + "\n" +
		`    IntPtr adapter; hr = Marshal.GetDelegateForFunctionPointer<EnumAdapters1D>(VF(factory,12))(factory,0,out adapter);` + "\n" +
		`    if (hr != 0) throw new Exception("enum adapter: 0x" + hr.ToString("X"));` + "\n" +
		`    IntPtr output; hr = Marshal.GetDelegateForFunctionPointer<EnumOutputsD>(VF(adapter,7))(adapter,0,out output);` + "\n" +
		`    if (hr != 0) throw new Exception("enum output: 0x" + hr.ToString("X"));` + "\n" +
		`    Guid o1g = new Guid("00cddea8-939b-4b83-a340-a685226666cc");` + "\n" +
		`    IntPtr output1; hr = Marshal.GetDelegateForFunctionPointer<QID>(VF(output,0))(output,ref o1g,out output1);` + "\n" +
		`    if (hr != 0) throw new Exception("QI output1: 0x" + hr.ToString("X"));` + "\n" +
		`    IntPtr dev, ctx;` + "\n" +
		`    hr = D3D11CreateDevice(adapter,0,IntPtr.Zero,0,IntPtr.Zero,0,7,out dev,IntPtr.Zero,out ctx);` + "\n" +
		`    if (hr != 0) throw new Exception("D3D11 device: 0x" + hr.ToString("X"));` + "\n" +
		`    IntPtr dup; hr = Marshal.GetDelegateForFunctionPointer<DupOutputD>(VF(output1,22))(output1,dev,out dup);` + "\n" +
		`    if (hr != 0) throw new Exception("dup output: 0x" + hr.ToString("X"));` + "\n" +
		`    IntPtr descBuf = Marshal.AllocHGlobal(128);` + "\n" +
		`    Marshal.GetDelegateForFunctionPointer<GetDescD>(VF(dup,7))(dup, descBuf);` + "\n" +
		`    int w = Marshal.ReadInt32(descBuf, 0);` + "\n" +
		`    int h = Marshal.ReadInt32(descBuf, 4);` + "\n" +
		`    Marshal.FreeHGlobal(descBuf);` + "\n" +
		`    if (w <= 0 || h <= 0) throw new Exception("bad DXGI size: " + w + "x" + h);` + "\n" +
		`    IntPtr fi = Marshal.AllocHGlobal(48);` + "\n" +
		`    IntPtr resource; hr = Marshal.GetDelegateForFunctionPointer<AcquireD>(VF(dup,8))(dup,500,fi,out resource);` + "\n" +
		`    Marshal.FreeHGlobal(fi);` + "\n" +
		`    if (hr != 0) throw new Exception("acquire frame: 0x" + hr.ToString("X"));` + "\n" +
		`    Guid t2g = new Guid("6f15aaf2-d208-4e89-9ab4-489535d34f9c");` + "\n" +
		`    IntPtr srcTex; Marshal.GetDelegateForFunctionPointer<QID>(VF(resource,0))(resource,ref t2g,out srcTex);` + "\n" +
		`    TexDesc td = new TexDesc();` + "\n" +
		`    td.Width=(uint)w; td.Height=(uint)h; td.MipLevels=1; td.ArraySize=1;` + "\n" +
		`    td.Format=87; td.SampleCount=1; td.SampleQuality=0;` + "\n" +
		`    td.Usage=3; td.BindFlags=0; td.CPUAccessFlags=0x20000; td.MiscFlags=0;` + "\n" +
		`    IntPtr staging; Marshal.GetDelegateForFunctionPointer<CreateTex2DD>(VF(dev,5))(dev,ref td,IntPtr.Zero,out staging);` + "\n" +
		`    Marshal.GetDelegateForFunctionPointer<CopyResD>(VF(ctx,47))(ctx,staging,srcTex);` + "\n" +
		`    MappedSub mapped; Marshal.GetDelegateForFunctionPointer<MapD>(VF(ctx,14))(ctx,staging,0,1,0,out mapped);` + "\n" +
		`    Bitmap bmp = new Bitmap(w, h, PixelFormat.Format32bppArgb);` + "\n" +
		`    BitmapData bd = bmp.LockBits(new Rectangle(0,0,w,h), ImageLockMode.WriteOnly, PixelFormat.Format32bppArgb);` + "\n" +
		`    int rowBytes = w * 4;` + "\n" +
		`    byte[] rowBuf = new byte[rowBytes];` + "\n" +
		`    for (int r = 0; r < h; r++) {` + "\n" +
		`      Marshal.Copy(IntPtr.Add(mapped.pData, (int)(r * mapped.RowPitch)), rowBuf, 0, rowBytes);` + "\n" +
		`      Marshal.Copy(rowBuf, 0, IntPtr.Add(bd.Scan0, r * bd.Stride), rowBytes);` + "\n" +
		`    }` + "\n" +
		`    bmp.UnlockBits(bd);` + "\n" +
		`    Marshal.GetDelegateForFunctionPointer<UnmapD>(VF(ctx,15))(ctx,staging,0);` + "\n" +
		`    Marshal.GetDelegateForFunctionPointer<ReleaseFrameD>(VF(dup,11))(dup);` + "\n" +
		`    var rel = new Action<IntPtr>(p => { try { Marshal.GetDelegateForFunctionPointer<ReleaseD>(VF(p,2))(p); } catch {} });` + "\n" +
		`    rel(staging); rel(srcTex); rel(resource); rel(dup);` + "\n" +
		`    rel(output1); rel(output); rel(adapter); rel(factory); rel(dev); rel(ctx);` + "\n" +
		`    return bmp;` + "\n" +
		`  }` + "\n" +
		`}` + "\n" +
		`'@ -ReferencedAssemblies System.Drawing; ` +
		`$dxgiBmp = [DxgiCapture]::Capture(); ` +
		`if ($dxgiBmp -and -not (Test-BlankBitmap $dxgiBmp)) { ` +
		`$b64 = ConvertTo-Base64Png $dxgiBmp; $dxgiBmp.Dispose(); [Console]::Out.Write($b64); exit 0 }; ` +
		`if ($dxgiBmp) { $dxgiBmp.Dispose() } ` +
		`} catch { }; `
}

func buildWindowsWindowScreenshotCommand(windowTitle string) string {
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
			`function Test-BlankBitmap($bmp) { `+
			`$step = [Math]::Max(1, [Math]::Floor([Math]::Sqrt($bmp.Width * $bmp.Height / 2000))); `+
			`for ($y = 0; $y -lt $bmp.Height; $y += $step) { `+
			`for ($x = 0; $x -lt $bmp.Width; $x += $step) { `+
			`$px = $bmp.GetPixel($x, $y); `+
			`if (($px.R + $px.G + $px.B) -gt 10) { return $false } `+
			`} } return $true }; `+
			`function ConvertTo-Base64Png($bmp) { `+
			`$ms = New-Object IO.MemoryStream; `+
			`$bmp.Save($ms, [Drawing.Imaging.ImageFormat]::Png); `+
			`$b64 = [Convert]::ToBase64String($ms.ToArray()); `+
			`$ms.Dispose(); return $b64 }; `+
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
			`$pwFlags = @(2, 3, 0); `+
			`foreach ($fl in $pwFlags) { `+
			`$bmpPW = New-Object Drawing.Bitmap($w, $h); `+
			`$gPW = [Drawing.Graphics]::FromImage($bmpPW); `+
			`$hdcPW = $gPW.GetHdc(); `+
			`$ok = [WinAPI]::PrintWindow($found, $hdcPW, $fl); `+
			`$gPW.ReleaseHdc($hdcPW); $gPW.Dispose(); `+
			`if ($ok -and -not (Test-BlankBitmap $bmpPW)) { `+
			`$b64 = ConvertTo-Base64Png $bmpPW; $bmpPW.Dispose(); [Console]::Out.Write($b64); exit 0 }; `+
			`$bmpPW.Dispose() }; `+
			`$bmp2 = New-Object Drawing.Bitmap($w, $h); `+
			`$g2 = [Drawing.Graphics]::FromImage($bmp2); `+
			`try { $g2.CopyFromScreen($r.Left, $r.Top, 0, 0, (New-Object Drawing.Size($w,$h))) } catch { }; `+
			`$g2.Dispose(); `+
			`if (-not (Test-BlankBitmap $bmp2)) { `+
			`$b64 = ConvertTo-Base64Png $bmp2; $bmp2.Dispose(); [Console]::Out.Write($b64); exit 0 }; `+
			`$bmp2.Dispose(); `+
			`Write-Error 'Window screenshot is blank'; exit 1`, escaped)
}
