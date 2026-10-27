<#
.SYNOPSIS
    Waits until the buddy serial port is free, then flashes the app image.

.DESCRIPTION
    The MaClaw GUI holds COM3 while the companion link is enabled (the port is
    exclusive). This helper probes the port until it can be opened, then runs
    flash-buddy.ps1 -NoBackup. Run it, then uncheck 启用伴侣屏连接 in
    设置 → 硬件 (or close MaClaw); the new firmware is written automatically.

.EXAMPLE
    .\flash-when-free.ps1 -Port COM3 -TimeoutSec 1800
#>
[CmdletBinding()]
param(
    [string] $Port = "COM3",
    [int] $TimeoutSec = 1800,
    [string] $Esptool = "python"
)

# Native-command stderr must not terminate the probe loop.
$ErrorActionPreference = "Continue"
$ScriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$Firmware = Join-Path $ScriptDir "..\firmware\build\buddy_firmware.bin"
if (-not (Test-Path $Firmware)) { throw "firmware not found: $Firmware" }

Write-Host "[buddy] waiting for $Port (release it in MaClaw 设置 → 硬件)"
$deadline = (Get-Date).AddSeconds($TimeoutSec)
while ((Get-Date) -lt $deadline) {
    # chip-id both tests exclusivity and confirms the Espressif device; each
    # successful probe leaves the chip in the ROM loader, which the flash
    # script resets anyway.
    & $Esptool -m esptool --chip esp32s3 -p $Port chip-id 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[buddy] $Port is free; flashing"
        & (Join-Path $ScriptDir "flash-buddy.ps1") -Port $Port -NoBackup
        if ($LASTEXITCODE -eq 0) {
            exit 0
        }
        Write-Warning "[buddy] flash attempt lost the port; re-waiting"
    }
    Start-Sleep -Seconds 3
}
Write-Warning "[buddy] timed out waiting for $Port"
exit 1
