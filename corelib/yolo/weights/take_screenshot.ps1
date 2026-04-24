Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$ms = New-Object System.IO.MemoryStream
$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
$b64 = [Convert]::ToBase64String($ms.ToArray())
[System.IO.File]::WriteAllText("corelib/yolo/weights/test_screenshot.b64", $b64)
Write-Host "Screenshot: $($bounds.Width)x$($bounds.Height), b64 length: $($b64.Length)"
$g.Dispose()
$bmp.Dispose()
$ms.Dispose()
