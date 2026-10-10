$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$remote = 'sha256sum /tmp/maclaw-botssh/maclawsrv /tmp/maclaw-botssh/maclaw-hub /tmp/maclaw-botssh/desktopd /data/soft/maclaw_srv/bin/maclawsrv /data/soft/hub/maclaw-hub /data/soft/maclaw_desktopd/bin/desktopd; wc -c /tmp/maclaw-botssh/maclaw-hub /data/soft/hub/maclaw-hub /data/soft/hub/maclaw-hub.bak-botssh-20261010'
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $remote 2>&1 | ForEach-Object { "$_" } | Out-String
$raw.Replace($env:REMOTE_PASS, 'REDACTED')
