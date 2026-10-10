$ErrorActionPreference = 'Stop'
$plink = 'C:\Program Files\PuTTY\plink.exe'
$pscp = 'C:\Program Files\PuTTY\pscp.exe'
$probe = Get-Content 'D:\workprj\aicoder\.tmp-deploy-probe.ps1' -Raw
if ($probe -notmatch "REMOTE_PASS = '([^']+)'") { throw 'deploy credential missing' }
$env:REMOTE_PASS = $Matches[1]
$hk = 'SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
$target = 'root@maclawsrv.mypapers.top'
$local = 'D:\workprj\aicoder\build\deploy-outcome'
$log = Join-Path $local 'deploy-log.txt'

function Redact([string]$text) {
    $out = New-Object System.Collections.Generic.List[string]
    foreach ($line in ($text -split "`r?`n")) {
        if ($line -match 'MACLAW_DESKTOP_API_TOKEN|DESKTOPD_TOKEN|REMOTE_PASS|(?i)password\s*=|(?i)authorization:') {
            $out.Add('REDACTED_LINE')
        } else {
            $out.Add($line)
        }
    }
    return ($out -join "`n")
}

if (-not (Test-Path "$local\maclawsrv")) { throw 'maclawsrv binary missing' }
& $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target 'mkdir -p /tmp/maclaw-outcome'
if ($LASTEXITCODE -ne 0) { throw "mkdir failed: $LASTEXITCODE" }
& $pscp -batch -P 22 -hostkey $hk -pw $env:REMOTE_PASS `
    "$local\maclawsrv" "$local\install.sh" `
    "${target}:/tmp/maclaw-outcome/"
if ($LASTEXITCODE -ne 0) { throw "upload failed: $LASTEXITCODE" }
$remote = "sed -i 's/\r$//' /tmp/maclaw-outcome/install.sh; sh /tmp/maclaw-outcome/install.sh"
# The remote script prints curl's first connection-refused line on stderr while
# maclawsrv is still opening its port. Stop must not turn that line into a
# terminating error and drop the session before the retry finishes.
$previous = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
$raw = & $plink -batch -no-antispoof -P 22 -hostkey $hk -pw $env:REMOTE_PASS $target $remote 2>&1 | Out-String
$ErrorActionPreference = $previous
$raw = $raw.Replace($env:REMOTE_PASS, 'REDACTED')
$clean = Redact $raw
Set-Content -Path $log -Value $clean -Encoding utf8
Write-Output $clean
if ($clean -notmatch 'DEPLOY_OK') { exit 1 }
