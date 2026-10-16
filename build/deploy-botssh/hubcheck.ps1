$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$remote = @'
echo HUBCHECK_BEGIN
ps -eo pid=,lstart=,args= | awk '/\/data\/soft\/hub\/maclaw-hub/ && !/awk/ && !/hubcenter/ {print}'
python3 - <<'PY'
import os
for pid in os.listdir("/proc"):
    if not pid.isdigit():
        continue
    try:
        cmd = open("/proc/%s/cmdline" % pid, "rb").read().split(b"\0")
    except Exception:
        continue
    if not cmd or not cmd[0].endswith(b"/maclaw-hub") or b"hubcenter" in cmd[0]:
        continue
    exe = "/proc/%s/exe" % pid
    try:
        size = os.path.getsize(exe)
        target = os.readlink(exe)
    except Exception as exc:
        print("pid=%s exe_err" % pid)
        continue
    print("pid=%s exe_size=%s exe_link_len=%s" % (pid, size, len(target)))
PY
stat -c '%s %y %n' /data/soft/hub/maclaw-hub /tmp/maclaw-botssh/maclaw-hub /data/soft/hub/maclaw-hub.bak-botssh-20261010
echo STARTSH
awk 'BEGIN{IGNORECASE=1} /TOKEN|PASS|SECRET/ {print "REDACTED_LINE"; next} {print}' /data/soft/hub/start.sh
echo HUBCHECK_END
'@
# Send the script as base64 so the remote shell does not eat quotes.
$script = $remote -replace "`r`n", "`n" -replace "`r", "`n"
$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($script))
$cmd = "echo $b64 | base64 -d | sh"
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $cmd 2>&1 | ForEach-Object { "$_" } | Out-String
$raw = $raw.Replace($env:REMOTE_PASS, 'REDACTED')
$raw -split "`r?`n" | ForEach-Object {
    if ($_ -match 'MACLAW_DESKTOP_API_TOKEN|DESKTOPD_TOKEN|REMOTE_PASS|(?i)password\s*=|(?i)authorization:|Bearer |[0-9a-fA-F]{32,}') { 'REDACTED_LINE' } else { $_ }
}
