<#
.SYNOPSIS
  Lightweight smoke checks for Hub mobile DE progress deployment.

.EXAMPLE
  $env:MACLAW_DEPLOY_PASSWORD = '***'
  .\Smoke-HubProgress.ps1 -Hosts hubs.mypapers.top,hubs.maclaw.top
#>
[CmdletBinding()]
param(
  [string[]]$Hosts = @('hubs.mypapers.top', 'hubs.maclaw.top', 'hubs2.maclaw.top'),

  [string[]]$PublicBases = @(
    'https://hub.mypapers.top',
    'https://hub.maclaw.top'
  ),

  [string]$User = 'root',
  [string]$Password = $env:MACLAW_DEPLOY_PASSWORD,

  [hashtable]$HostKeys = @{
    'hubs.mypapers.top' = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
    'hubs.maclaw.top'   = 'SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE'
  },

  [string]$Plink = 'C:\Program Files\PuTTY\plink.exe'
)

$ErrorActionPreference = 'Continue'
$fail = 0

Write-Host '=== public API ==='
foreach ($base in $PublicBases) {
  $base = $base.Trim().TrimEnd('/')
  if ($base -notmatch '^https?://') {
    Write-Host "SKIP bad PublicBase (need absolute https URL): $base"
    $fail++
    continue
  }
  foreach ($path in @('/api/mobile/bootstrap', '/api/llm/v1/models')) {
    $url = "$base$path"
    try {
      $null = Invoke-WebRequest -Uri $url -Method GET -TimeoutSec 10 -ErrorAction Stop
      Write-Host "UNEXPECTED_2xx $url"
      $fail++
    } catch {
      $code = $null
      if ($_.Exception.Response) {
        $code = [int]$_.Exception.Response.StatusCode
      }
      if ($code -eq 401) {
        Write-Host "OK $url -> 401"
      } else {
        Write-Host "FAIL $url -> code=$code err=$($_.Exception.Message)"
        $fail++
      }
    }
  }
}

if ($Password -and (Test-Path -LiteralPath $Plink)) {
  Write-Host '=== remote binary markers ==='
  foreach ($h in $Hosts) {
    $hk = $HostKeys[$h]
    if (-not $hk) {
      Write-Host "SKIP $h (no hostkey configured; try Deploy-HubBinary discovery)"
      continue
    }
    $cmd = 'set -eu; pgrep -a maclaw-hub | head -2 || true; for m in mobile.digital_employee_task TASK_ALREADY_FINISHED LLM_ENDPOINT_USER_RATE_LIMIT_WAIT_CANCELED; do strings /data/soft/hub/maclaw-hub | grep -q "$m" && echo marker_ok_$m || echo marker_MISSING_$m; done; code=$(curl -sS -o /dev/null -w "%{http_code}" --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap 2>/dev/null || echo fail); echo bootstrap_9399=$code'
    try {
      Write-Host "--- $h ---"
      $out = & $Plink -batch -hostkey $hk -pw $Password "${User}@${h}" $cmd 2>&1 | Out-String
      Write-Host $out
      if ($out -match 'marker_MISSING' -or $out -notmatch 'marker_ok_mobile') {
        $fail++
      }
      if ($out -match 'bootstrap_9399=' -and $out -notmatch 'bootstrap_9399=401') {
        # 401 expected without token; other codes are suspicious
        if ($out -notmatch 'bootstrap_9399=401') { $fail++ }
      }
    } catch {
      Write-Host "FAIL $h $_"
      $fail++
    }
  }
} else {
  Write-Host 'Remote SSH smoke skipped (set MACLAW_DEPLOY_PASSWORD to enable).'
}

if ($fail -gt 0) {
  Write-Host "SMOKE_FAILED count=$fail"
  exit 1
}
Write-Host 'SMOKE_OK'
