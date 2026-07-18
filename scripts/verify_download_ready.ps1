#Requires -Version 5.1
<#
.SYNOPSIS
  Verify MaClaw download workdir binding and skill demotion from live logs/config.
#>
$ErrorActionPreference = "Continue"
$log = Join-Path $env:USERPROFILE ".maclaw\logs\maclaw.log"
$cfg = Join-Path $env:USERPROFILE ".maclaw\config.json"

Write-Host "=== Process ==="
Get-Process -Name "MaClaw*" -ErrorAction SilentlyContinue |
  Select-Object Id, ProcessName, StartTime | Format-Table -AutoSize

Write-Host "=== Last workdir readiness ==="
$ready = Join-Path $env:USERPROFILE ".maclaw\logs\workdir_ready.txt"
if (Test-Path $ready) {
  Write-Host "sidecar: $ready"
  Get-Content -Path $ready -Encoding UTF8 | Select-Object -Last 3
} else {
  Write-Host "missing sidecar $ready (restart MaClaw after latest build)"
}
if (Test-Path $log) {
  Select-String -Path $log -Pattern "effective_wd=|effective_wd_set=|inject workdir|download_file=builtin|\[download_file\]|workdir_ready" |
    Select-Object -Last 12 |
    ForEach-Object { $_.Line }
} else {
  Write-Host "missing log $log"
}

Write-Host "=== Skill statuses (download-related) ==="
if (Test-Path $cfg) {
  python -c @"
import json
from pathlib import Path
c=json.loads(Path(r'$($cfg.Replace('\','\\'))').read_text(encoding='utf-8'))
for s in c.get('nl_skills',[]):
    name=s.get('name') or ''
    blob=(name+' '+str(s.get('description') or '')).lower()
    if any(k in blob for k in ['wget','curl','fetch','download','paper_pdf','translator']) or s.get('status') in ('needs_review','disabled'):
        print(f\"{name}: status={s.get('status')!r} fail={s.get('failure_count')} ok={s.get('success_count')}\")
"@
}

Write-Host "=== Workdir .maclaw-tmp ==="
try {
  $cfgObj = Get-Content $cfg -Raw -Encoding UTF8 | ConvertFrom-Json
  $wd = $cfgObj.working_directory
  Write-Host "working_directory=$wd"
  if ($wd -and (Test-Path $wd)) {
    $tmp = Join-Path $wd ".maclaw-tmp"
    Write-Host "maclaw-tmp exists=$(Test-Path $tmp)"
  }
} catch {
  Write-Host "config parse: $_"
}

Write-Host "=== Manual smoke checklist ==="
Write-Host "1) In Agent: download_file https://arxiv.org/pdf/2510.16079.pdf"
Write-Host "2) Or paper_pdf_translator arxiv 2510.16079"
Write-Host "3) Expect ~/.maclaw/logs/workdir_ready.txt + inject workdir log + files under working_directory"
Write-Host "4) scripts/smoke_download_to_workdir.ps1 for FS-level smoke"
