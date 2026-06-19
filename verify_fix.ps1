$path = 'd:\workprj\aicoder\gui\frontend\src\components\pages\AppsPage.tsx'
try {
    $text = [System.IO.File]::ReadAllText($path, [System.Text.Encoding]::UTF8)
    Write-Host "File length: $($text.Length)"
    Write-Host "UTF-8 decode: OK"
    if ($text.Contains([char]0xFFFD)) {
        Write-Host "WARNING: Contains replacement character U+FFFD"
    } else {
        Write-Host "No U+FFFD found: OK"
    }
} catch {
    Write-Host "ERROR: $_"
}
