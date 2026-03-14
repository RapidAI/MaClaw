param(
    [string]$HubUrl = "http://127.0.0.1:9399",
    [string]$ProgressFile = "D:\workprj\aicoder\.last_remote_demo.json",
    [string]$InputText = "",
    [switch]$Interrupt,
    [switch]$Kill
)

$ErrorActionPreference = 'Stop'

function Fail($message) {
    Write-Host "[FAIL] $message" -ForegroundColor Red
    exit 1
}

function Read-JsonFile($path) {
    if (-not (Test-Path -LiteralPath $path)) {
        Fail "Progress file not found: $path"
    }
    return Get-Content -Raw -LiteralPath $path | ConvertFrom-Json
}

function Extract-ConfirmUrl($message) {
    if ([string]::IsNullOrWhiteSpace($message)) {
        return $null
    }
    $match = [regex]::Match($message, 'https?://\S+')
    if ($match.Success) {
        return $match.Value.TrimEnd('.', ',', ';')
    }
    return $null
}

function Invoke-JsonRequest($method, $url, $body, $token) {
    $headers = @{}
    if (-not [string]::IsNullOrWhiteSpace($token)) {
        $headers["Authorization"] = "Bearer $token"
    }

    if ($null -ne $body) {
        return Invoke-RestMethod -Method $method -Uri $url -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json -Depth 8)
    }

    return Invoke-RestMethod -Method $method -Uri $url -Headers $headers
}

function Wait-ForMachineAndSession($hubUrl, $viewerToken, $machineId, $sessionId, $attempts, $delaySeconds) {
    for ($i = 0; $i -lt $attempts; $i++) {
        try {
            $machines = Invoke-JsonRequest "GET" "$hubUrl/api/machines" $null $viewerToken
            $machine = $null
            foreach ($item in $machines.machines) {
                if ([string]$item.id -eq $machineId -or [string]$item.machine_id -eq $machineId) {
                    $machine = $item
                    break
                }
            }

            if ($null -ne $machine) {
                $sessionListUrl = "$hubUrl/api/sessions?machine_id=$([uri]::EscapeDataString($machineId))"
                $sessions = Invoke-JsonRequest "GET" $sessionListUrl $null $viewerToken
                $session = $null
                foreach ($item in $sessions.sessions) {
                    if ([string]$item.session_id -eq $sessionId -or [string]$item.id -eq $sessionId) {
                        $session = $item
                        break
                    }
                }

                if ($null -ne $session) {
                    return @{
                        machine = $machine
                        session = $session
                    }
                }
            }
        } catch {
        }

        Start-Sleep -Seconds $delaySeconds
    }

    return $null
}

$report = Read-JsonFile $ProgressFile
$email = [string]$report.activation.email
$machineId = [string]$report.activation.machine_id
$sessionId = [string]$report.started_session.id

if ([string]::IsNullOrWhiteSpace($email)) {
    Fail "Activation email missing from progress file"
}
if ([string]::IsNullOrWhiteSpace($machineId)) {
    Fail "Machine ID missing from progress file"
}
if ([string]::IsNullOrWhiteSpace($sessionId)) {
    Fail "Session ID missing from progress file"
}

$hubUrl = $HubUrl.TrimEnd('/')

Write-Host ""
Write-Host "Verifying MaClaw remote controls..." -ForegroundColor Cyan
Write-Host "Hub:          $hubUrl"
Write-Host "Email:        $email"
Write-Host "Machine ID:   $machineId"
Write-Host "Session ID:   $sessionId"
Write-Host "Progress:     $ProgressFile"
Write-Host ""

$emailRequest = Invoke-JsonRequest "POST" "$hubUrl/api/auth/email-request" @{ email = $email } $null
if ($emailRequest.status -ne "pending_email_confirmation") {
    Fail "Unexpected email login status: $($emailRequest.status)"
}

$confirmUrl = Extract-ConfirmUrl([string]$emailRequest.message)
if ([string]::IsNullOrWhiteSpace($confirmUrl)) {
    Fail "Development confirm URL was not returned. Enable SMTP or use local dev mode."
}

$tokenParam = [System.Web.HttpUtility]::ParseQueryString(([System.Uri]$confirmUrl).Query).Get("token")
if ([string]::IsNullOrWhiteSpace($tokenParam)) {
    Fail "Could not extract login token from confirm URL"
}

$loginConfirm = Invoke-JsonRequest "POST" "$hubUrl/api/auth/email-confirm" @{ token = $tokenParam } $null
$viewerToken = [string]$loginConfirm.access_token
if ([string]::IsNullOrWhiteSpace($viewerToken)) {
    Fail "Viewer token was not returned"
}

$lookup = Wait-ForMachineAndSession $hubUrl $viewerToken $machineId $sessionId 20 1
if ($null -eq $lookup) {
    Fail "Target machine/session not visible yet in Hub APIs"
}

$machine = $lookup.machine
$session = $lookup.session

$snapshotUrl = "$hubUrl/api/session?machine_id=$([uri]::EscapeDataString($machineId))&session_id=$([uri]::EscapeDataString($sessionId))"
$snapshot = Invoke-JsonRequest "GET" $snapshotUrl $null $viewerToken

if (-not [string]::IsNullOrWhiteSpace($InputText)) {
    $inputResp = Invoke-JsonRequest "POST" "$hubUrl/api/session/input" @{
        machine_id = $machineId
        session_id = $sessionId
        text       = $InputText
    } $viewerToken
    if (-not $inputResp.ok) {
        Fail "session.input did not return ok=true"
    }
}

if ($Interrupt) {
    $interruptResp = Invoke-JsonRequest "POST" "$hubUrl/api/session/interrupt" @{
        machine_id = $machineId
        session_id = $sessionId
    } $viewerToken
    if (-not $interruptResp.ok) {
        Fail "session.interrupt did not return ok=true"
    }
}

if ($Kill) {
    $killResp = Invoke-JsonRequest "POST" "$hubUrl/api/session/kill" @{
        machine_id = $machineId
        session_id = $sessionId
    } $viewerToken
    if (-not $killResp.ok) {
        Fail "session.kill did not return ok=true"
    }
}

$summary = $snapshot.summary
$preview = $snapshot.preview
$machineName = ""
if ($null -ne $machine.name) {
    $machineName = [string]$machine.name
} elseif ($null -ne $machine.machine_name) {
    $machineName = [string]$machine.machine_name
}
if ([string]::IsNullOrWhiteSpace($machineName)) {
    $machineName = "unknown"
}

Write-Host "[OK] Viewer login and control path verified." -ForegroundColor Green
Write-Host ""
Write-Host ("Machine Name:     {0}" -f $machineName)
Write-Host ("Machine Status:   {0}" -f ([string]$machine.status))
Write-Host ("Session Status:   {0}" -f ([string]$summary.status))
Write-Host ("Current Task:     {0}" -f ([string]$summary.current_task))
Write-Host ("Last Result:      {0}" -f ([string]$summary.last_result))
Write-Host ("Preview Seq:      {0}" -f ([string]$preview.output_seq))
Write-Host ("Input Sent:       {0}" -f ([bool](-not [string]::IsNullOrWhiteSpace($InputText))))
Write-Host ("Interrupt Sent:   {0}" -f ([bool]$Interrupt))
Write-Host ("Kill Sent:        {0}" -f ([bool]$Kill))
