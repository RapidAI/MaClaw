param(
    [string]$Root = (Split-Path $PSScriptRoot -Parent),
    [string]$AppName = '',
    [string]$Version = '',
    [string]$OutputDir = ''
)
$ErrorActionPreference = 'Stop'
$cfg = Get-Content (Join-Path $Root 'wails.json') -Raw | ConvertFrom-Json
$AppName = if ($AppName) { $AppName } else { [string]$cfg.name }
$OutputDir = if ($OutputDir) { $OutputDir } else { Join-Path $Root 'dist' }
$Version = if ($Version) { $Version } else { "$($cfg.info.productVersion).$([int](Get-Content (Join-Path $Root 'build_number')) )" }
$ProductName = $cfg.info.productName
$CompanyName = $cfg.info.companyName
$Copyright = $cfg.info.copyright
$lines = @(
    "!define INFO_PROJECTNAME `"$AppName`""
    "!define PRODUCT_EXECUTABLE `"$AppName.exe`""
    "!define INFO_PRODUCTNAME `"$ProductName`""
    "!define INFO_COMPANYNAME `"$CompanyName`""
    "!define INFO_COPYRIGHT `"$Copyright`""
    "!define INFO_PRODUCTVERSION `"$Version`""
    "!define ARG_WAILS_AMD64_BINARY `"$OutputDir\$AppName`_amd64.exe`""
    "!define ARG_WAILS_ARM64_BINARY `"$OutputDir\$AppName`_arm64.exe`""
    "!define ARG_MACLAWCLI_AMD64_BINARY `"$OutputDir\maclaw-cli`_amd64.exe`""
    "!define ARG_MACLAWCLI_ARM64_BINARY `"$OutputDir\maclaw-cli`_arm64.exe`""
    "!define ARG_ACPBRIDGE_AMD64_BINARY `"$OutputDir\maclaw-acp-bridge`_amd64.exe`""
    "!define ARG_ACPBRIDGE_ARM64_BINARY `"$OutputDir\maclaw-acp-bridge`_arm64.exe`""
)
$out = Join-Path $Root 'build\windows\installer\build_params.nsh.tmp'
[IO.File]::WriteAllText($out, ($lines -join [Environment]::NewLine), [Text.UTF8Encoding]::new($false))
