<#
.SYNOPSIS
    Closes the running MaClaw gracefully, flashes the latest buddy firmware,
    and starts the sound-enabled GUI build.

.DESCRIPTION
    The serial port is exclusive, so the old MaClaw must release it before
    flashing. This helper asks every MaClaw window to close (WM_CLOSE, no
    force-kill), waits for COM3, runs flash-buddy.ps1 -NoBackup, then starts
    build\bin\MaClaw-sound.exe.

    Run it from a normal PowerShell window when you are ready to restart the
    desktop app.

.EXAMPLE
    .\flash-and-restart-gui.ps1 -Port COM3
#>
[CmdletBinding()]
param(
    [string] $Port = "COM3",
    [string] $Gui = (Join-Path $PSScriptRoot "..\..\build\bin\MaClaw-pet.exe")
)

$ErrorActionPreference = "Continue"
$ScriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }

Write-Host "[buddy] asking MaClaw to close (graceful, no force-kill)"
$running = @(Get-Process | Where-Object { $_.ProcessName -like 'MaClaw*' })
foreach ($proc in $running) {
    try { $null = $proc.CloseMainWindow() } catch {}
}
for ($i = 0; $i -lt 20; $i++) {
    $still = @(Get-Process | Where-Object { $_.ProcessName -like 'MaClaw*' })
    if ($still.Count -eq 0) { break }
    Start-Sleep -Seconds 1
}
$stillRunning = @(Get-Process | Where-Object { $_.ProcessName -like 'MaClaw*' })
if ($stillRunning.Count -gt 0) {
    Write-Warning "[buddy] MaClaw is still running (tray mode?). Quit it manually, then rerun."
    exit 1
}

Write-Host "[buddy] waiting for $Port and flashing"
& (Join-Path $ScriptDir "flash-when-free.ps1") -Port $Port -TimeoutSec 90
if ($LASTEXITCODE -ne 0) { throw "flash failed" }

if (Test-Path $Gui) {
    Write-Host "[buddy] starting $Gui"
    Start-Process -FilePath $Gui
} else {
    Write-Warning "[buddy] new GUI not found: $Gui (build it with build_win.bat)"
}
