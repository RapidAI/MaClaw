param(
    [string]$HubCenterUrl = 'http://127.0.0.1:9388',
    [string]$HubUrl = 'http://127.0.0.1:9399',
    [string]$HubCenterAdminUser = '',
    [string]$HubCenterAdminPass = '',
    [string]$HubCenterAdminEmail = '',
    [string]$HubAdminUser = '',
    [string]$HubAdminPass = '',
    [string]$HubAdminEmail = ''
)

$ErrorActionPreference = 'Stop'

$rootDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$runStackCmd = Join-Path $rootDir 'run_remote_stack.cmd'
$cleanStackCmd = Join-Path $rootDir 'clean_remote_stack.cmd'
$healthUrls = @(
    ($HubCenterUrl.TrimEnd('/') + '/healthz'),
    ($HubUrl.TrimEnd('/') + '/healthz')
)

if ([string]::IsNullOrWhiteSpace($HubCenterAdminUser)) {
    $HubCenterAdminUser = if ($env:HUBCENTER_ADMIN_USER) { $env:HUBCENTER_ADMIN_USER } else { 'admin' }
}
if ([string]::IsNullOrWhiteSpace($HubCenterAdminPass)) {
    $HubCenterAdminPass = if ($env:HUBCENTER_ADMIN_PASS) { $env:HUBCENTER_ADMIN_PASS } else { 'MaClaw123!' }
}
if ([string]::IsNullOrWhiteSpace($HubCenterAdminEmail)) {
    $HubCenterAdminEmail = if ($env:HUBCENTER_ADMIN_EMAIL) { $env:HUBCENTER_ADMIN_EMAIL } else { 'admin@local.maclaw' }
}

if ([string]::IsNullOrWhiteSpace($HubAdminUser)) {
    $HubAdminUser = if ($env:HUB_ADMIN_USER) { $env:HUB_ADMIN_USER } else { $HubCenterAdminUser }
}
if ([string]::IsNullOrWhiteSpace($HubAdminPass)) {
    $HubAdminPass = if ($env:HUB_ADMIN_PASS) { $env:HUB_ADMIN_PASS } else { $HubCenterAdminPass }
}
if ([string]::IsNullOrWhiteSpace($HubAdminEmail)) {
    $HubAdminEmail = if ($env:HUB_ADMIN_EMAIL) { $env:HUB_ADMIN_EMAIL } else { $HubCenterAdminEmail }
}

function Invoke-Json {
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Url,
        [AllowNull()]$Body,
        [hashtable]$Headers
    )

    if ($null -eq $Headers) {
        $Headers = @{}
    }

    if ($null -ne $Body -and $Body -ne '') {
        return Invoke-RestMethod -Method $Method -Uri $Url -ContentType 'application/json' -Body $Body -Headers $Headers -TimeoutSec 15
    }

    return Invoke-RestMethod -Method $Method -Uri $Url -Headers $Headers -TimeoutSec 15
}

function Wait-Healthy {
    param(
        [string[]]$Urls,
        [int]$Attempts,
        [int]$DelaySeconds
    )

    foreach ($url in $Urls) {
        $ok = $false
        for ($i = 0; $i -lt $Attempts; $i++) {
            try {
                $resp = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 5
                if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) {
                    Write-Host ('[OK] health ' + $url + ' -> ' + $resp.StatusCode)
                    $ok = $true
                    break
                }
            } catch {
            }
            Start-Sleep -Seconds $DelaySeconds
        }

        if (-not $ok) {
            return $false
        }
    }

    return $true
}

function Ensure-Admin {
    param(
        [string]$BaseUrl,
        [string]$Username,
        [string]$Password,
        [string]$Email,
        [string]$Label
    )

    $setupBody = @{
        username = $Username
        password = $Password
        email    = $Email
    } | ConvertTo-Json

    $loginBody = @{
        username = $Username
        password = $Password
    } | ConvertTo-Json

    try {
        $null = Invoke-Json -Method 'POST' -Url ($BaseUrl.TrimEnd('/') + '/api/admin/setup') -Body $setupBody -Headers $null
        Write-Host ('[OK] ' + $Label + ' admin initialized')
    } catch {
        $statusCode = $null
        if ($_.Exception.Response) {
            $statusCode = [int]$_.Exception.Response.StatusCode
        }

        if ($statusCode -eq 409) {
            Write-Host ('[OK] ' + $Label + ' admin already initialized')
        } else {
            throw
        }
    }

    try {
        $login = Invoke-Json -Method 'POST' -Url ($BaseUrl.TrimEnd('/') + '/api/admin/login') -Body $loginBody -Headers $null
    } catch {
        $statusCode = $null
        if ($_.Exception.Response) {
            $statusCode = [int]$_.Exception.Response.StatusCode
        }
        if ($statusCode -eq 401) {
            throw ('LOGIN_FAILED:' + $Label)
        }
        throw
    }

    Write-Host ('[OK] ' + $Label + ' admin login')
    return $login.access_token
}

function Test-LocalUrl {
    param([string]$Url)

    try {
        $uri = [Uri]$Url
        return @('127.0.0.1', 'localhost').Contains($uri.Host)
    } catch {
        return $false
    }
}

function Test-DefaultAdminCreds {
    param(
        [string]$Username,
        [string]$Password,
        [string]$Email
    )

    return (
        $Username -eq 'admin' -and
        $Password -eq 'MaClaw123!' -and
        $Email -eq 'admin@local.maclaw'
    )
}

function Initialize-RemoteStack {
    Write-Host ''
    Write-Host 'Initializing MaClaw remote stack...'
    Write-Host ('Hub Center: ' + $HubCenterUrl)
    Write-Host ('Hub:        ' + $HubUrl)
    Write-Host ''

    if (-not (Wait-Healthy -Urls $healthUrls -Attempts 3 -DelaySeconds 1)) {
        if (Test-Path $runStackCmd) {
            Write-Host '[INFO] Services are not healthy yet, launching run_remote_stack.cmd and retrying...'
            Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', $runStackCmd | Out-Null
            Start-Sleep -Seconds 3
        }
    }

    if (-not (Wait-Healthy -Urls $healthUrls -Attempts 90 -DelaySeconds 1)) {
        throw ('HEALTH_TIMEOUT:' + ($healthUrls -join ', '))
    }

    $hubCenterToken = Ensure-Admin -BaseUrl $HubCenterUrl -Username $HubCenterAdminUser -Password $HubCenterAdminPass -Email $HubCenterAdminEmail -Label 'Hub Center'
    $hubToken = Ensure-Admin -BaseUrl $HubUrl -Username $HubAdminUser -Password $HubAdminPass -Email $HubAdminEmail -Label 'Hub'

    $hubHeaders = @{
        Authorization = ('Bearer ' + $hubToken)
    }

    $centerConfigBody = @{
        base_url = $HubCenterUrl
    } | ConvertTo-Json

    $null = Invoke-Json -Method 'POST' -Url ($HubUrl.TrimEnd('/') + '/api/admin/center/config') -Body $centerConfigBody -Headers $hubHeaders
    Write-Host '[OK] Hub Center URL configured on Hub'

    $status = Invoke-Json -Method 'GET' -Url ($HubUrl.TrimEnd('/') + '/api/admin/center/status') -Body $null -Headers $hubHeaders
    if (-not $status.registered) {
        $status = Invoke-Json -Method 'POST' -Url ($HubUrl.TrimEnd('/') + '/api/admin/center/register') -Body '' -Headers $hubHeaders
        Write-Host '[OK] Hub registered to Hub Center'
    } else {
        Write-Host '[OK] Hub already registered to Hub Center'
    }

    Write-Host ''
    Write-Host ('Hub Center registered state: ' + $status.registered)
    Write-Host ('Advertised host: ' + $status.host + ':' + $status.port)
    Write-Host ('Advertised base URL: ' + $status.advertised_base_url)
    Write-Host ''
    Write-Host 'Ready:'
    Write-Host ('  Hub Center admin: ' + $HubCenterUrl.TrimEnd('/') + '/admin')
    Write-Host ('  Hub admin:        ' + $HubUrl.TrimEnd('/') + '/admin')
    Write-Host ('  Hub PWA:          ' + $HubUrl.TrimEnd('/') + '/app')
}

try {
    Initialize-RemoteStack
} catch {
    $message = $_.Exception.Message
    $canAutoRepair = (
        $message -like 'LOGIN_FAILED:*' -and
        (Test-Path $cleanStackCmd) -and
        (Test-LocalUrl $HubCenterUrl) -and
        (Test-LocalUrl $HubUrl) -and
        (Test-DefaultAdminCreds -Username $HubCenterAdminUser -Password $HubCenterAdminPass -Email $HubCenterAdminEmail) -and
        (Test-DefaultAdminCreds -Username $HubAdminUser -Password $HubAdminPass -Email $HubAdminEmail)
    )

    if (-not $canAutoRepair) {
        throw
    }

    Write-Host ''
    Write-Host '[WARN] Admin login failed with the default local credentials.'
    Write-Host '[INFO] Attempting automatic local reset and retry...'
    & cmd.exe /c $cleanStackCmd
    if ($LASTEXITCODE -ne 0) {
        throw 'AUTO_REPAIR_CLEAN_FAILED'
    }
    Start-Sleep -Seconds 1
    & cmd.exe /c $runStackCmd
    if ($LASTEXITCODE -ne 0) {
        throw 'AUTO_REPAIR_RUN_FAILED'
    }
    Start-Sleep -Seconds 3
    Initialize-RemoteStack
}
