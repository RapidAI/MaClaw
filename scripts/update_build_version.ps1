param(
    [Parameter(Mandatory = $true)][string]$Root,
    [Parameter(Mandatory = $true)][string]$AppName
)
$ErrorActionPreference = 'Stop'
$numberPath = Join-Path $Root 'build_number'
$n = if (Test-Path $numberPath) { [int](Get-Content $numberPath) + 1 } else { 1 }
Set-Content -Path $numberPath -Value $n -NoNewline
$cfg = Get-Content (Join-Path $Root 'wails.json') -Raw | ConvertFrom-Json
$parts = @([string]$cfg.info.productVersion -split '\.')
if ($parts.Count -eq 3) { $parts += '0' }
if ($parts.Count -ne 4) { throw 'productVersion must contain 3 or 4 numeric parts.' }
$parts[3] = [string]$n
$version = $parts -join '.'
Set-Content (Join-Path $Root 'temp_VERSION.txt') $version -NoNewline
Set-Content (Join-Path $Root 'temp_BUILD_NUM.txt') ([string]$n) -NoNewline
Set-Content (Join-Path $Root 'temp_PRODUCT_NAME.txt') $AppName -NoNewline
Set-Content (Join-Path $Root 'temp_COMPANY_NAME.txt') ([string]$cfg.info.companyName) -NoNewline
Set-Content (Join-Path $Root 'temp_COPYRIGHT.txt') ([string]$cfg.info.copyright) -NoNewline
