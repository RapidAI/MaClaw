<#
.SYNOPSIS
  Upload a prebuilt Linux maclaw-hub binary to one or more hosts and restart via start.sh.

.EXAMPLE
  .\Deploy-HubBinary.ps1 -Hosts hubs.mypapers.top,hubs.maclaw.top -Password (Read-Host -AsSecureString)

.EXAMPLE
  $env:MACLAW_DEPLOY_PASSWORD = '***'
  .\Deploy-HubBinary.ps1 -Hosts hubs.mypapers.top -BinaryPath D:\workprj\aicoder\build\deploy-hub-bin\maclaw-hub
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string[]]$Hosts,

  [string]$BinaryPath = '',

  [string]$RemoteDir = '/data/soft/hub',

  [string]$User = 'root',

  [string]$Password = $env:MACLAW_DEPLOY_PASSWORD,

  [hashtable]$HostKeys = @{
    'hubs.mypapers.top' = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
    'hubs.maclaw.top'   = 'SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE'
  },

  [string]$Plink = 'C:\Program Files\PuTTY\plink.exe',
  [string]$Pscp  = 'C:\Program Files\PuTTY\pscp.exe'
)

$ErrorActionPreference = 'Stop'

if (-not $BinaryPath) {
  $BinaryPath = Join-Path $PSScriptRoot '..\..\build\deploy-hub-bin\maclaw-hub'
}
$BinaryPath = [System.IO.Path]::GetFullPath($BinaryPath)

if (-not $Password) {
  throw 'Password required via -Password or MACLAW_DEPLOY_PASSWORD'
}
if (-not (Test-Path -LiteralPath $BinaryPath)) {
  throw "Binary not found: $BinaryPath"
}
if (-not (Test-Path -LiteralPath $Plink)) { throw "plink not found: $Plink" }
if (-not (Test-Path -LiteralPath $Pscp))  { throw "pscp not found: $Pscp" }

$results = @()
foreach ($h in $Hosts) {
  $item = [ordered]@{ Host = $h; Ok = $false; Detail = '' }
  try {
    $hk = $HostKeys[$h]
    if (-not $hk) {
      # Discover host key from plink error output
      $probe = & $Plink -batch -pw $Password "${User}@${h}" 'echo ok' 2>&1 | Out-String
      if ($probe -match 'SHA256:([A-Za-z0-9+/=]+)') {
        $hk = "SHA256:$($Matches[1])"
        Write-Host "[$h] discovered hostkey $hk"
      } else {
        throw "No hostkey for $h and discovery failed: $probe"
      }
    }

    Write-Host "[$h] uploading..."
    & $Pscp -batch -hostkey $hk -pw $Password $BinaryPath "${User}@${h}:/tmp/maclaw-hub.new"
    if ($LASTEXITCODE -ne 0) { throw "pscp failed exit=$LASTEXITCODE" }

    $remote = @"
set -eu
REMOTE_DIR='$RemoteDir'
NEW=/tmp/maclaw-hub.new
BIN="`$REMOTE_DIR/maclaw-hub"
test -f "`$NEW"
chmod +x "`$NEW"
ts=`$(date +%Y%m%d%H%M%S)
if [ -f "`$BIN" ]; then cp -f "`$BIN" "`$BIN.bak.`$ts"; fi
mv -f "`$NEW" "`$BIN"
chmod +x "`$BIN"
cd "`$REMOTE_DIR"
./start.sh
sleep 2
pgrep -a maclaw-hub || true
for m in mobile.digital_employee_task TASK_ALREADY_FINISHED LLM_ENDPOINT_USER_RATE_LIMIT_WAIT_CANCELED; do
  strings "`$BIN" | grep -q "`$m" && echo marker_ok_`$m || echo marker_MISSING_`$m
done
code=`$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap 2>/dev/null || echo fail)
echo bootstrap_9399=`$code
echo DEPLOY_OK
"@
    Write-Host "[$h] installing + restart..."
    $out = & $Plink -batch -hostkey $hk -pw $Password "${User}@${h}" $remote 2>&1 | Out-String
    Write-Host $out
    if ($out -notmatch 'DEPLOY_OK') { throw "remote deploy missing DEPLOY_OK" }
    $item.Ok = $true
    $item.Detail = ($out -split "`n" | Select-Object -Last 8) -join ' | '
  } catch {
    $item.Detail = "$_"
    Write-Warning "[$h] FAILED: $_"
  }
  $results += [pscustomobject]$item
}

$results | Format-Table -AutoSize
$failed = @($results | Where-Object { -not $_.Ok })
if ($failed.Count -gt 0) {
  exit 1
}
