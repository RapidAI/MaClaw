param(
    [Parameter(Mandatory = $true)][string]$Root,
    [Parameter(Mandatory = $true)][string]$BuildNumber,
    [Parameter(Mandatory = $true)][string]$Version
)
$ErrorActionPreference = 'Stop'
$path = Join-Path $Root 'guiapp\frontend\src\version.ts'
@("export const buildNumber = '$BuildNumber';", "export const appVersion = '$Version';") |
    Set-Content -Path $path -Encoding Utf8
