$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$remote = "echo WHO_BEGIN; file /data/soft/hub/maclaw-hub /tmp/maclaw-botssh/maclaw-hub; sha256sum /proc/2237/exe; find /data/soft/hub -maxdepth 2 -type f -newermt '2026-10-10 13:07:50' ! -newermt '2026-10-10 13:09:30' -printf '%s %TY-%Tm-%Td %TH:%TM:%TS %p\n'; echo WHO_END"
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $remote 2>&1 | ForEach-Object { "$_" } | Out-String
$raw.Replace($env:REMOTE_PASS, 'REDACTED')
