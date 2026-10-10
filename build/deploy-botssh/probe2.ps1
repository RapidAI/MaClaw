$ErrorActionPreference = 'Continue'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$pscp = 'C:\Program Files\PuTTY\pscp.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$local = 'D:\workprj\aicoder\build\deploy-botssh\probe2.sh'
& $pscp -batch -P 22 -hostkey $hk -pw $env:REMOTE_PASS $local "${target}:/tmp/maclaw-botssh/probe2.sh" | Out-Null
$remote = "sed -i 's/\r$//' /tmp/maclaw-botssh/probe2.sh; sh /tmp/maclaw-botssh/probe2.sh"
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $remote 2>&1 | ForEach-Object { "$_" } | Out-String
$raw = $raw.Replace($env:REMOTE_PASS, 'REDACTED')
$raw -split "`r?`n" | ForEach-Object {
    if ($_ -match 'MACLAW_DESKTOP_API_TOKEN|DESKTOPD_TOKEN|REMOTE_PASS|(?i)password\s*=|(?i)authorization:|Bearer ') { 'REDACTED_LINE' } else { $_ }
}
