param(
    [string]$HubCenterUrl = 'http://127.0.0.1:9388',
    [string]$HubUrl = 'http://127.0.0.1:9399'
)

$ErrorActionPreference = 'Stop'

$rootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$hubCenterDir = Join-Path $rootDir 'hubcenter'
$hubDir = Join-Path $rootDir 'hub'

function Test-Healthy {
    param([string]$Url)

    try {
        $resp = Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 3
        return ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300)
    } catch {
        return $false
    }
}

function Stop-PortOwner {
    param([int]$Port)

    $pids = @(
        Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue |
            Select-Object -ExpandProperty OwningProcess -Unique
    ) | Where-Object { $_ }

    foreach ($pid in $pids) {
        Write-Host ("[INFO] Port {0} occupied by PID {1}. Stopping..." -f $Port, $pid)
        taskkill /PID $pid /T /F *> $null
    }
}

function Ensure-ConfigFile {
    param([string]$ServiceDir)

    $configDir = Join-Path $ServiceDir 'configs'
    $configPath = Join-Path $configDir 'config.yaml'
    $examplePath = Join-Path $configDir 'config.example.yaml'
    if (-not (Test-Path -LiteralPath $configPath) -and (Test-Path -LiteralPath $examplePath)) {
        Copy-Item -LiteralPath $examplePath -Destination $configPath -Force
    }
}

function Start-ServiceProcess {
    param(
        [string]$Label,
        [string]$ServiceDir,
        [string]$Command
    )

    Ensure-ConfigFile $ServiceDir

    $script = @"
Set-Location '$ServiceDir'
\$env:GOCACHE = '$ServiceDir\.gocache'
\$env:GOMODCACHE = '$ServiceDir\.gomodcache'
$Command
"@

    Write-Host ("[INFO] Starting {0}..." -f $Label)
    Start-Process -FilePath 'powershell.exe' `
        -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', $script) `
        -WindowStyle Minimized | Out-Null
}

function Ensure-Service {
    param(
        [int]$Port,
        [string]$Label,
        [string]$HealthUrl,
        [string]$ServiceDir,
        [string]$Command
    )

    if (Test-Healthy $HealthUrl) {
        Write-Host ("[OK] {0} already healthy on port {1}." -f $Label, $Port)
        return
    }

    Stop-PortOwner $Port
    Start-ServiceProcess -Label $Label -ServiceDir $ServiceDir -Command $Command
}

Ensure-Service -Port 9388 -Label 'MaClaw Hub Center' -HealthUrl ($HubCenterUrl.TrimEnd('/') + '/healthz') -ServiceDir $hubCenterDir -Command 'go run .\cmd\hubcenter --config .\configs\config.yaml'
Start-Sleep -Seconds 2
Ensure-Service -Port 9399 -Label 'MaClaw Hub' -HealthUrl ($HubUrl.TrimEnd('/') + '/healthz') -ServiceDir $hubDir -Command 'go run .\cmd\hub --config .\configs\config.yaml'

Write-Host ''
Write-Host 'MaClaw remote stack launch requested.'
Write-Host ('Hub Center:   ' + $HubCenterUrl)
Write-Host ('Hub:          ' + $HubUrl)
Write-Host ('Hub PWA:      ' + $HubUrl.TrimEnd('/') + '/app')
Write-Host ('Hub Admin:    ' + $HubUrl.TrimEnd('/') + '/admin')
Write-Host ('Center Admin: ' + $HubCenterUrl.TrimEnd('/') + '/admin')
Write-Host ''
Write-Host 'Run check_remote_stack.cmd after a few seconds to verify both services.'
