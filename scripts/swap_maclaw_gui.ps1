#Requires -Version 5.1
<#
.SYNOPSIS
  Replace the running MaClaw GUI binary with dist\MaClaw_amd64.new.exe (or a source path).

.DESCRIPTION
  Stops MaClaw_amd64 / MaClaw processes that lock the dist binary, backs up the old
  exe, swaps in the new build, optionally starts MaClaw again.

.PARAMETER Source
  Path to the new binary. Default: <repo>\dist\MaClaw_amd64.new.exe

.PARAMETER Start
  Launch the swapped MaClaw_amd64.exe after a successful swap.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\swap_maclaw_gui.ps1 -Start
#>
[CmdletBinding()]
param(
    [string]$Source = "",
    [switch]$Start
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not (Test-Path (Join-Path $root "gui"))) {
    # Fallback: script may live at repo root
    $root = Split-Path -Parent $MyInvocation.MyCommand.Path
    if (-not (Test-Path (Join-Path $root "dist"))) {
        $root = "D:\workprj\aicoder"
    }
}

if ([string]::IsNullOrWhiteSpace($Source)) {
    $Source = Join-Path $root "dist\MaClaw_amd64.new.exe"
}
$target = Join-Path $root "dist\MaClaw_amd64.exe"
$alias = Join-Path $root "dist\MaClaw.exe"
$backup = Join-Path $root ("dist\MaClaw_amd64.exe.bak_{0:yyyyMMdd_HHmmss}" -f (Get-Date))

if (-not (Test-Path $Source)) {
    throw "Source binary not found: $Source`nBuild first (e.g. go build -tags desktop,production -o dist\MaClaw_amd64.new.exe ./gui/)"
}

Write-Host "Source : $Source"
Write-Host "Target : $target"

$names = @("MaClaw_amd64", "MaClaw")
$stopped = @()
foreach ($n in $names) {
    Get-Process -Name $n -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Host "Stopping PID $($_.Id) $($_.ProcessName) ..."
        Stop-Process -Id $_.Id -Force
        $stopped += $_.Id
    }
}
if ($stopped.Count -gt 0) {
    Start-Sleep -Seconds 1
}

if (Test-Path $target) {
    Copy-Item -Force $target $backup
    Write-Host "Backup : $backup"
}

Copy-Item -Force $Source $target
if (Test-Path (Split-Path $alias -Parent)) {
    Copy-Item -Force $target $alias
}

# Verify new binary still has download/workdir markers
$bytes = [IO.File]::ReadAllBytes($target)
$text = [Text.Encoding]::ASCII.GetString($bytes)
$need = @("inject workdir", "MACLAW_WORKDIR", "download_file")
foreach ($s in $need) {
    if (-not $text.Contains($s)) {
        Write-Warning "Marker missing in swapped binary: $s"
    } else {
        Write-Host "OK marker: $s"
    }
}

Write-Host "Swap complete: $target"
if ($Start) {
    Write-Host "Starting MaClaw..."
    Start-Process -FilePath $target
    Write-Host "Started."
} else {
    Write-Host "Run with -Start to launch, or start dist\MaClaw_amd64.exe manually."
}
