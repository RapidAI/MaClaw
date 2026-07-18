#Requires -Version 5.1
<#
.SYNOPSIS
  End-to-end smoke: download a small file into MaClaw working_directory.
  Mirrors what download_file / web_fetch(save_path=...) should do.
#>
$ErrorActionPreference = "Stop"
$cfgPath = Join-Path $env:USERPROFILE ".maclaw\config.json"
if (-not (Test-Path $cfgPath)) { throw "missing $cfgPath" }

# Prefer a standalone Python file so PowerShell quoting never mangles the script.
$script = Join-Path $PSScriptRoot "_smoke_download_py.py"
if (-not (Test-Path $script)) {
    throw "missing $script"
}
python $script
