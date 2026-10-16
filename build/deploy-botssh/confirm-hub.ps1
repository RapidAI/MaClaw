$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$remote = @'
echo CONFIRM_BEGIN
sha256sum /data/soft/hub/maclaw-hub /data/soft/maclaw_srv/bin/maclawsrv /data/soft/maclaw_desktopd/bin/desktopd
stat -c '%s %y %n' /data/soft/hub/maclaw-hub
ps -eo pid=,lstart=,args= | awk '/\/data\/soft\/hub\/maclaw-hub / && !/awk/ {print}'
echo "HUBCENTER=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo "SRV_PID=$(systemctl show -p MainPID --value maclawsrv.service)"
echo "DSK_PID=$(systemctl show -p MainPID --value maclaw-desktopd.service)"
curl -fsS --max-time 3 http://127.0.0.1:18080/version || echo VERSION_FAIL
echo
curl -sS -o /dev/null -w 'bootstrap=%{http_code}\n' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap || echo bootstrap=fail
sha256sum /data/soft/hub/maclaw-hub.bak-botssh-20261010 /data/soft/maclaw_srv/bin/maclawsrv.bak-botssh-20261010 /data/soft/maclaw_desktopd/bin/desktopd.bak-botssh-20261010
echo CONFIRM_END
'@
$script = $remote -replace "`r`n", "`n" -replace "`r", "`n"
$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($script))
$cmd = "echo $b64 | base64 -d | sh"
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $cmd 2>&1 | ForEach-Object { "$_" } | Out-String
$raw.Replace($env:REMOTE_PASS, 'REDACTED')
