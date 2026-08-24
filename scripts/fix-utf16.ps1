# Some editing tools in this workspace write Go sources as UTF-16LE, which the
# Go toolchain rejects with "unexpected NUL in input". Converting blindly is
# worse than the problem: reading an already-UTF-8 file as UTF-16 corrupts it.
# So detect a NUL byte first and only convert files that actually have one.
param([string[]]$Paths)

foreach ($p in $Paths) {
    if (-not (Test-Path $p)) { Write-Output "missing $p"; continue }
    $bytes = [System.IO.File]::ReadAllBytes($p)
    if ($bytes.Length -lt 2 -or ($bytes -notcontains 0)) {
        Write-Output "ok $p"
        continue
    }
    $text = [System.Text.Encoding]::Unicode.GetString($bytes)
    [System.IO.File]::WriteAllText($p, $text, (New-Object System.Text.UTF8Encoding $false))
    Write-Output "converted $p"
}
