param(
    [string]$ServiceName = "MaClawSrv",
    [string]$DataRoot
)

$ErrorActionPreference = "Stop"

function New-SecretHex {
    param([int]$Bytes)

    $buffer = [byte[]]::new($Bytes)
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return (($buffer | ForEach-Object { $_.ToString("x2") }) -join "")
}

if ([string]::IsNullOrWhiteSpace($DataRoot)) {
    $DataRoot = Join-Path $env:ProgramData "RapidAI\MaClawSrv"
}

New-Item -ItemType Directory -Force -Path $DataRoot | Out-Null

$serviceKey = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"
if (-not (Test-Path $serviceKey)) {
    throw "Windows service '$ServiceName' does not exist."
}

$existing = @{}
$raw = (Get-ItemProperty -Path $serviceKey -Name Environment -ErrorAction SilentlyContinue).Environment
foreach ($entry in @($raw)) {
    if ($entry -match "^([^=]+)=(.*)$") {
        $existing[$matches[1]] = $matches[2]
    }
}

if (-not $existing.ContainsKey("MACLAW_ADMIN_SECRET") -or [string]::IsNullOrWhiteSpace($existing["MACLAW_ADMIN_SECRET"])) {
    $existing["MACLAW_ADMIN_SECRET"] = New-SecretHex -Bytes 24
}
if (-not $existing.ContainsKey("MACLAW_TOKEN_SECRET") -or [string]::IsNullOrWhiteSpace($existing["MACLAW_TOKEN_SECRET"])) {
    $existing["MACLAW_TOKEN_SECRET"] = New-SecretHex -Bytes 32
}
if (-not $existing.ContainsKey("MACLAW_DATA_ROOT") -or [string]::IsNullOrWhiteSpace($existing["MACLAW_DATA_ROOT"])) {
    $existing["MACLAW_DATA_ROOT"] = $DataRoot
}

$entries = @(
    "MACLAW_ADMIN_SECRET=$($existing["MACLAW_ADMIN_SECRET"])",
    "MACLAW_TOKEN_SECRET=$($existing["MACLAW_TOKEN_SECRET"])",
    "MACLAW_DATA_ROOT=$($existing["MACLAW_DATA_ROOT"])"
)

New-ItemProperty -Path $serviceKey -Name Environment -PropertyType MultiString -Value ([string[]]$entries) -Force | Out-Null
