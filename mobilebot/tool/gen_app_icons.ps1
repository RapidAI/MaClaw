# Regenerate the MaclawGo launcher icons so the badge fills the icon.
#
# Source: guiapp\build\appicon.png (1024x1024, transparent corners, white margin).
# The script crops to the visible content (alpha > 10), keeps a small margin,
# then scales up for every Android mipmap and every iOS AppIcon slot.
#
#   powershell -ExecutionPolicy Bypass -File mobilebot\tool\gen_app_icons.ps1
#   powershell -ExecutionPolicy Bypass -File mobilebot\tool\gen_app_icons.ps1 -Check
#
# -Check only verifies the existing PNGs (size + coverage) and writes nothing.
param(
    [string]$Source,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $Source) { $Source = Join-Path $repo 'guiapp\build\appicon.png' }
$androidRes = Join-Path $repo 'mobilebot\android\app\src\main\res'
$iosSet = Join-Path $repo 'mobilebot\ios\Runner\Assets.xcassets\AppIcon.appiconset'

# Android legacy launcher bitmaps: directory -> edge length in px.
$androidTargets = [ordered]@{
    'mipmap-mdpi'    = 48
    'mipmap-hdpi'    = 72
    'mipmap-xhdpi'   = 96
    'mipmap-xxhdpi'  = 144
    'mipmap-xxxhdpi' = 192
}

# iOS slots, exactly the filenames declared in AppIcon.appiconset\Contents.json.
$iosTargets = [ordered]@{
    'Icon-App-20x20@1x.png'     = 20
    'Icon-App-20x20@2x.png'     = 40
    'Icon-App-20x20@3x.png'     = 60
    'Icon-App-29x29@1x.png'     = 29
    'Icon-App-29x29@2x.png'     = 58
    'Icon-App-29x29@3x.png'     = 87
    'Icon-App-40x40@1x.png'     = 40
    'Icon-App-40x40@2x.png'     = 80
    'Icon-App-40x40@3x.png'     = 120
    'Icon-App-60x60@2x.png'     = 120
    'Icon-App-60x60@3x.png'     = 180
    'Icon-App-76x76@1x.png'     = 76
    'Icon-App-76x76@2x.png'     = 152
    'Icon-App-83.5x83.5@2x.png' = 167
    'Icon-App-1024x1024@1x.png' = 1024
}

# Margin around the badge, as a fraction of the icon edge.
# The badge keeps its own radius and a thin white rim instead of being cut into a
# hard square: launchers apply their own mask, so a square crop would double-cut
# the rounded frame. A small rim makes the launcher mask visible on the outside.
$marginRatio = 0.04
# Rounded-square shape used to clear the corners outside the white rim.
# The launcher mask sits inside this outline, so its rounded edge shows.
$radiusRatio = 0.245

function Get-VisibleBox([System.Drawing.Bitmap]$bmp) {
    $w = $bmp.Width; $h = $bmp.Height
    $rect = New-Object System.Drawing.Rectangle 0, 0, $w, $h
    $data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly,
        [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $stride = $data.Stride
        $bytes = New-Object byte[] ($stride * $h)
        [System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $bytes, 0, $bytes.Length)
        $minX = $w; $minY = $h; $maxX = -1; $maxY = -1
        for ($y = 0; $y -lt $h; $y++) {
            $row = $y * $stride
            for ($x = 0; $x -lt $w; $x++) {
                if ($bytes[$row + $x * 4 + 3] -gt 10) {
                    if ($x -lt $minX) { $minX = $x }
                    if ($x -gt $maxX) { $maxX = $x }
                    if ($y -lt $minY) { $minY = $y }
                    if ($y -gt $maxY) { $maxY = $y }
                }
            }
        }
        if ($maxX -lt 0) { throw 'source image has no visible pixels' }
        return [System.Drawing.Rectangle]::FromLTRB($minX, $minY, $maxX + 1, $maxY + 1)
    }
    finally { $bmp.UnlockBits($data) }
}

function Get-Coverage([System.Drawing.Bitmap]$bmp, [bool]$opaqueBackground) {
    # Coverage = visible content size relative to the icon edge.
    # Android: alpha > 10. iOS: any pixel that is not the white background.
    $w = $bmp.Width; $h = $bmp.Height
    $rect = New-Object System.Drawing.Rectangle 0, 0, $w, $h
    $data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly,
        [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $stride = $data.Stride
        $bytes = New-Object byte[] ($stride * $h)
        [System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $bytes, 0, $bytes.Length)
        $minX = $w; $minY = $h; $maxX = -1; $maxY = -1
        for ($y = 0; $y -lt $h; $y++) {
            $row = $y * $stride
            for ($x = 0; $x -lt $w; $x++) {
                $i = $row + $x * 4
                $b = $bytes[$i]; $g = $bytes[$i + 1]; $r = $bytes[$i + 2]; $a = $bytes[$i + 3]
                $visible = if ($opaqueBackground) {
                    ($a -gt 10) -and -not ($r -gt 246 -and $g -gt 246 -and $b -gt 246)
                } else {
                    $a -gt 10
                }
                if ($visible) {
                    if ($x -lt $minX) { $minX = $x }
                    if ($x -gt $maxX) { $maxX = $x }
                    if ($y -lt $minY) { $minY = $y }
                    if ($y -gt $maxY) { $maxY = $y }
                }
            }
        }
        if ($maxX -lt 0) { return 0.0 }
        $cw = $maxX - $minX + 1; $ch = $maxY - $minY + 1
        return [double]([Math]::Max($cw, $ch)) / [Math]::Max($w, $h)
    }
    finally { $bmp.UnlockBits($data) }
}

function New-SquareCrop([System.Drawing.Image]$img, [System.Drawing.Rectangle]$box, [double]$margin) {
    # Square box centred on the visible content, padded by $margin of its edge.
    $side = [Math]::Max($box.Width, $box.Height) * (1 + 2 * $margin)
    $cx = $box.X + $box.Width / 2.0
    $cy = $box.Y + $box.Height / 2.0
    $side = [int][Math]::Round($side)
    $left = [int][Math]::Round($cx - $side / 2.0)
    $top = [int][Math]::Round($cy - $side / 2.0)
    $crop = New-Object System.Drawing.Bitmap $side, $side, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($crop)
    try {
        $g.Clear([System.Drawing.Color]::Transparent)
        # Areas outside the source stay transparent.
        $srcRect = New-Object System.Drawing.Rectangle 0, 0, $img.Width, $img.Height
        $dstRect = New-Object System.Drawing.Rectangle (-$left), (-$top), $img.Width, $img.Height
        $g.DrawImage($img, $dstRect, $srcRect, [System.Drawing.GraphicsUnit]::Pixel)
    }
    finally { $g.Dispose() }
    return $crop
}

function New-Scaled([System.Drawing.Image]$crop, [int]$size, [bool]$roundMask, [bool]$opaqueBackground) {
    $bmp = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    try {
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
        $attr = New-Object System.Drawing.Imaging.ImageAttributes
        try {
            $attr.SetWrapMode([System.Drawing.Drawing2D.WrapMode]::TileFlipXY)
            $like = [System.Drawing.Imaging.ColorMatrix]::new()
            $attr.SetColorMatrix($like)
            $dest = New-Object System.Drawing.Rectangle 0, 0, $size, $size
            # Fill the rim with the icon's own white so the launcher shows a thin
            # white band around the rounded badge.
            $g.Clear([System.Drawing.Color]::FromArgb(255, 250, 252, 252))
            if ($roundMask) {
                $path = New-Object System.Drawing.Drawing2D.GraphicsPath
                $r = [int][Math]::Round($size * 2 * $radiusRatio)
                $d = $r * 2
                $path.AddArc(0, 0, $d, $d, 180, 90)
                $path.AddArc($size - $d, 0, $d, $d, 270, 90)
                $path.AddArc($size - $d, $size - $d, $d, $d, 0, 90)
                $path.AddArc(0, $size - $d, $d, $d, 90, 90)
                $path.CloseFigure()
                $g.SetClip($path)
                $g.DrawImage($crop, $dest, 0, 0, $crop.Width, $crop.Height, [System.Drawing.GraphicsUnit]::Pixel, $attr)
            }
            else {
                $g.DrawImage($crop, $dest, 0, 0, $crop.Width, $crop.Height, [System.Drawing.GraphicsUnit]::Pixel, $attr)
            }
        }
        finally { $attr.Dispose() }
    }
    finally { $g.Dispose() }

    if ($opaqueBackground) {
        # App Store icons must not carry alpha: flatten onto white.
        $flat = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format24bppRgb)
        $fg = [System.Drawing.Graphics]::FromImage($flat)
        try {
            $fg.Clear([System.Drawing.Color]::White)
            $fg.DrawImageUnscaled($bmp, 0, 0)
        }
        finally { $fg.Dispose() }
        $bmp.Dispose()
        return $flat
    }
    return $bmp
}

function Get-FrameCoverage([System.Drawing.Bitmap]$bmp) {
    # Edge length of the dark frame of the badge relative to the icon edge.
    # The frame is the dark teal outline; antialiased rim pixels are included.
    $w = $bmp.Width; $h = $bmp.Height
    $rect = New-Object System.Drawing.Rectangle 0, 0, $w, $h
    $data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly,
        [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $stride = $data.Stride
        $bytes = New-Object byte[] ($stride * $h)
        [System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $bytes, 0, $bytes.Length)
        $minX = $w; $minY = $h; $maxX = -1; $maxY = -1
        # Middle third of the icon: the frame's top and bottom edges pass here, so
        # the M never reaches the icon border in this band.
        $midFrom = [Math]::Floor($w / 3); $midTo = [Math]::Ceiling(2 * $w / 3)
        $midCol = [Math]::Floor($w / 2)
        for ($y = 0; $y -lt $h; $y++) {
            $row = $y * $stride
            for ($x = 0; $x -lt $w; $x++) {
                $i = $row + $x * 4
                $b = $bytes[$i]; $g = $bytes[$i + 1]; $r = $bytes[$i + 2]; $a = $bytes[$i + 3]
                # Dark teal outline: blue > green > red, all well below white.
                $frame = ($a -gt 200) -and ($r -lt 100) -and ($g -lt 140) -and ($b -lt 150) -and ($b -gt $r)
                if (-not $frame) { continue }
                if ($x -ge $midFrom -and $x -lt $midTo) {
                    if ($y -lt $minY) { $minY = $y }
                    if ($y -gt $maxY) { $maxY = $y }
                }
                if ([Math]::Abs($x - $midCol) -le [Math]::Max(1, [Math]::Floor($w / 6))) {
                    if ($x -lt $minX) { $minX = $x }
                    if ($x -gt $maxX) { $maxX = $x }
                }
            }
        }
        $cw = if ($maxX -ge 0) { $maxX - $minX + 1 } else { 0 }
        $ch = if ($maxY -ge 0) { $maxY - $minY + 1 } else { 0 }
        return [double]([Math]::Max($cw, $ch)) / [Math]::Max($w, $h)
    }
    finally { $bmp.UnlockBits($data) }
}

function Save-Png([System.Drawing.Image]$img, [string]$path) {
    $dir = Split-Path -Parent $path
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    $img.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
}

$failures = 0
$sourceBox = $null

if (-not $Check) {
    if (-not (Test-Path $Source)) { throw "source icon not found: $Source" }
    $src = [System.Drawing.Image]::FromFile((Resolve-Path $Source))
    try {
        $srcBmp = New-Object System.Drawing.Bitmap $src
        $sourceBox = Get-VisibleBox $srcBmp
        $crop = New-SquareCrop $srcBmp $sourceBox $marginRatio
        try {
            foreach ($k in $androidTargets.Keys) {
                $size = $androidTargets[$k]
                $icon = New-Scaled $crop $size $true $false
                try { Save-Png $icon (Join-Path $androidRes "$k\ic_launcher.png") } finally { $icon.Dispose() }
            }
            foreach ($k in $iosTargets.Keys) {
                $size = $iosTargets[$k]
                $icon = New-Scaled $crop $size $false $true
                try { Save-Png $icon (Join-Path $iosSet $k) } finally { $icon.Dispose() }
            }
        }
        finally { $crop.Dispose(); $srcBmp.Dispose() }
    }
    finally { $src.Dispose() }
    Write-Host ("source visible box: {0}" -f $sourceBox)
}

Write-Host ''
Write-Host 'Android (dark frame coverage 88%..98%):'
foreach ($k in $androidTargets.Keys) {
    $size = $androidTargets[$k]
    $path = Join-Path $androidRes "$k\ic_launcher.png"
    if (-not (Test-Path $path)) { Write-Host "  MISSING $path"; $failures++; continue }
    $bmp = [System.Drawing.Bitmap]::FromFile((Resolve-Path $path))
    try {
        # The dark frame of the badge must nearly touch the edge (thin white rim),
        # but not be clipped: 88%..98% of the edge.
        $cov = Get-FrameCoverage $bmp
        $hasAlpha = [System.Drawing.Image]::IsAlphaPixelFormat($bmp.PixelFormat)
        $ok = ($bmp.Width -eq $size) -and ($bmp.Height -eq $size) -and ($cov -ge 0.88) -and ($cov -le 0.98) -and $hasAlpha
        if (-not $ok) { $failures++ }
        Write-Host ("  {0,-8} {1,4}x{2,-4} frame {3,6:P1} alpha {4,-5} {5}" -f $k, $bmp.Width, $bmp.Height, $cov, $hasAlpha, $(if ($ok) { 'OK' } else { 'FAIL' }))
    }
    finally { $bmp.Dispose() }
}

Write-Host ''
Write-Host 'iOS (dark frame coverage 88%..98%, no alpha):'
foreach ($k in $iosTargets.Keys) {
    $size = $iosTargets[$k]
    $path = Join-Path $iosSet $k
    if (-not (Test-Path $path)) { Write-Host "  MISSING $path"; $failures++; continue }
    $bmp = [System.Drawing.Bitmap]::FromFile((Resolve-Path $path))
    try {
        $cov = Get-FrameCoverage $bmp
        $hasAlpha = [System.Drawing.Image]::IsAlphaPixelFormat($bmp.PixelFormat)
        $ok = ($bmp.Width -eq $size) -and ($bmp.Height -eq $size) -and ($cov -ge 0.88) -and ($cov -le 0.98) -and (-not $hasAlpha)
        if (-not $ok) { $failures++ }
        Write-Host ("  {0,-26} {1,4}x{2,-4} frame {3,6:P1} alpha {4,-5} {5}" -f $k, $bmp.Width, $bmp.Height, $cov, $hasAlpha, $(if ($ok) { 'OK' } else { 'FAIL' }))
    }
    finally { $bmp.Dispose() }
}

Write-Host ''
if ($failures -gt 0) {
    Write-Host "$failures check(s) failed"
    if ($Check) { Write-Host 'Re-run without -Check to regenerate.' }
    exit 1
}
Write-Host 'all icon checks passed'
exit 0
