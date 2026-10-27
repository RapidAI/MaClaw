<#
.SYNOPSIS
    Restores an app image backed up by flash-buddy.ps1.

.DESCRIPTION
    Writes only the factory app partition at 0x10000, so NVS and storage are
    preserved. Use this to switch a test board back to the iot-agentos voice
    firmware.

.EXAMPLE
    .\restore-voice-firmware.ps1 -Backup .\backup\voice-app-20261027-081500.bin -Port COM3
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $Backup,
    [string] $Port = "COM3",
    [string] $Esptool = "python"
)

$ErrorActionPreference = "Stop"

$AppOffset = "0x10000"
$AppSizeBytes = 0x3a0000

if (-not (Test-Path $Backup)) { throw "backup not found: $Backup" }
$item = Get-Item $Backup
if ($item.Length -ne $AppSizeBytes) {
    throw ("backup is {0} bytes; expected the full app partition ({1} bytes)" -f $item.Length, $AppSizeBytes)
}

$device = Get-PnpDevice -PresentOnly -ErrorAction SilentlyContinue |
    Where-Object { $_.InstanceId -match "VID_303A&PID_1001" -and $_.FriendlyName -match [regex]::Escape("($Port)") }
if (-not $device) {
    throw "no Espressif USB-Serial/JTAG device on $Port"
}

Write-Host "[buddy] restoring $Backup"
$restored = $false
for ($attempt = 1; $attempt -le 3; $attempt++) {
    & $Esptool -m esptool --chip esp32s3 -p $Port -b 460800 --before default-reset --after hard-reset `
        write-flash $AppOffset $Backup
    if ($LASTEXITCODE -eq 0) {
        $restored = $true
        break
    }
    Write-Warning "[buddy] restore attempt $attempt failed; retrying"
    Start-Sleep -Seconds 2
}
if (-not $restored) { throw "restore failed after 3 attempts" }
Write-Host "[buddy] restore complete"
