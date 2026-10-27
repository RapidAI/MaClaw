<#
.SYNOPSIS
    Backs up the current app partition and flashes the MaClaw Buddy firmware.

.DESCRIPTION
    Only the factory app partition at 0x10000 is written, so NVS and the
    storage partition (meeting recordings, weather cache, pairing data) are
    left untouched. The previous app image is saved first; use
    restore-voice-firmware.ps1 with that file to switch back.

    The serial port is exclusive on Windows: stop the MaClaw GUI (or disable
    the companion link in Settings > 硬件) before running this script.

.EXAMPLE
    .\flash-buddy.ps1 -Port COM3

.EXAMPLE
    .\flash-buddy.ps1 -Port COM3 -NoBackup -Firmware ..\firmware\build\buddy_firmware.bin
#>
[CmdletBinding()]
param(
    [string] $Port = "COM3",
    [string] $Firmware = "",
    [string] $Esptool = "python",
    [switch] $NoBackup
)

$ErrorActionPreference = "Stop"

# $PSScriptRoot can be empty under some PowerShell hosts; fall back to the
# command path so relative defaults still resolve.
$ScriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
if (-not $Firmware) {
    $Firmware = Join-Path $ScriptDir "..\firmware\build\buddy_firmware.bin"
}

$AppOffset = "0x10000"
$AppSizeBytes = 0x3a0000

if (-not (Test-Path $Firmware)) {
    throw "firmware not found: $Firmware (build it with firmware\build.cmd -B build build)"
}

$firmwareItem = Get-Item $Firmware
if ($firmwareItem.Length -ge $AppSizeBytes) {
    throw ("firmware {0} bytes does not fit the {1} byte app partition" -f $firmwareItem.Length, $AppSizeBytes)
}

$device = Get-PnpDevice -PresentOnly -ErrorAction SilentlyContinue |
    Where-Object { $_.InstanceId -match "VID_303A&PID_1001" -and $_.FriendlyName -match [regex]::Escape("($Port)") }
if (-not $device) {
    throw "no Espressif USB-Serial/JTAG device on $Port; check the cable and the port name"
}

$sha = (Get-FileHash -Algorithm SHA256 $Firmware).Hash
Write-Host ("[buddy] firmware {0} bytes sha256={1}" -f $firmwareItem.Length, $sha.ToLowerInvariant())

if (-not $NoBackup) {
    $backupDir = Join-Path $ScriptDir "backup"
    New-Item -ItemType Directory -Force -Path $backupDir | Out-Null
    $backup = Join-Path $backupDir ("voice-app-" + (Get-Date -Format "yyyyMMdd-HHmmss") + ".bin")
    Write-Host "[buddy] backing up app partition to $backup"
    # This board's USB-Serial/JTAG link wedges after ~100-450 KiB of continuous
    # reads, and a wedged session cannot be recovered in-process. The session
    # helper therefore reads a bounded batch per process, leaves the stub alive,
    # and resumes from the output size; loop it until the partition is complete.
    $sessionHelper = Join-Path $ScriptDir "backup_app_session.py"
    $round = 0
    while ($round -lt 60) {
        & $Esptool $sessionHelper --port $Port --out $backup
        $size = if (Test-Path $backup) { (Get-Item $backup).Length } else { 0 }
        if ($size -eq $AppSizeBytes) { break }
        if ($size -gt $AppSizeBytes) { throw "backup grew past the partition size" }
        $round++
        if ($LASTEXITCODE -ne 0) {
            Start-Sleep -Seconds 15
        } else {
            Start-Sleep -Seconds 1
        }
    }
    if (-not (Test-Path $backup) -or (Get-Item $backup).Length -ne $AppSizeBytes) {
        throw "app partition backup did not complete; refusing to flash"
    }
    Write-Host "[buddy] backup ok ($AppSizeBytes bytes)"
}

Write-Host "[buddy] flashing buddy app at $AppOffset"
$flashed = $false
for ($attempt = 1; $attempt -le 3; $attempt++) {
    & $Esptool -m esptool --chip esp32s3 -p $Port -b 460800 --before default-reset --after hard-reset `
        write-flash $AppOffset $Firmware
    if ($LASTEXITCODE -eq 0) {
        $flashed = $true
        break
    }
    Write-Warning "[buddy] flash attempt $attempt failed; retrying"
    Start-Sleep -Seconds 2
}
if (-not $flashed) { throw "flash failed after 3 attempts" }

Write-Host "[buddy] done. Start the MaClaw GUI (or buddy-host) to connect."
