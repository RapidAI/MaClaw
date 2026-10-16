$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$remote = @'
echo PROBE_BEGIN
ps -eo pid=,args= | awk '/install.sh|maclaw-hubcenter/ && !/awk/ {print}'
systemctl is-active maclawsrv.service maclaw-desktopd.service || true
systemctl show maclawsrv.service maclaw-desktopd.service -p Id,ActiveState,MainPID,SubState --no-page
curl -fsS --max-time 5 http://127.0.0.1:18080/version || echo VERSION_FAIL
echo
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap || echo fail)
echo "bootstrap_9399=$code"
echo "HUBCENTER=$(ps -eo pid=,args= | awk '/maclaw-hubcenter/ && !/awk/ {print $1; exit}')"
echo DESKTOPS
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}' || true
sha256sum /data/soft/maclaw_srv/bin/maclawsrv /data/soft/hub/maclaw-hub /data/soft/maclaw_desktopd/bin/desktopd
ls -l /data/soft/maclaw_srv/bin/maclawsrv.bak-botssh-20261010 /data/soft/hub/maclaw-hub.bak-botssh-20261010 /data/soft/maclaw_desktopd/bin/desktopd.bak-botssh-20261010
SPID=$(systemctl show -p MainPID --value maclawsrv.service)
if [ -r "/proc/$SPID/environ" ]; then
  tr '\0' '\n' < "/proc/$SPID/environ" | awk -F= '
    $1=="MACLAW_DESKTOP_API_TOKEN" { print "SRV_TOKEN_LEN=" length($2) }
    $1=="MACLAW_HUB_URL" { print "SRV_HUB_URL_LEN=" length($2) }
  '
fi
HPID=$(ps -eo pid=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print $1; exit}')
echo "HUB_PID=$HPID"
if [ -n "$HPID" ] && [ -r "/proc/$HPID/environ" ]; then
  tr '\0' '\n' < "/proc/$HPID/environ" | awk -F= '$1=="MACLAW_DESKTOP_API_TOKEN" { print "HUB_TOKEN_LEN=" length($2) }'
fi
echo PROBE_END
'@
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $remote 2>&1 | ForEach-Object { "$_" } | Out-String
$raw = $raw.Replace($env:REMOTE_PASS, 'REDACTED')
$raw -split "`r?`n" | ForEach-Object {
    if ($_ -match 'MACLAW_DESKTOP_API_TOKEN|DESKTOPD_TOKEN|REMOTE_PASS|(?i)password\s*=|(?i)authorization:') { 'REDACTED_LINE' } else { $_ }
}
