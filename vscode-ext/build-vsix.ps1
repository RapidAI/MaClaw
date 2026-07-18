# Build + package the MaClaw VS Code extension and refresh the embedded asset
# under gui/vscode_ext_asset/. Best-effort: callers (release scripts) may skip
# this when Node is unavailable — the committed VSIX keeps builds working.
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$extDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $extDir
try {
    $npm = Get-Command npm.cmd -ErrorAction SilentlyContinue
    if (-not $npm) {
        $candidate = Join-Path ${env:ProgramFiles} "nodejs\npm.cmd"
        if (Test-Path $candidate) {
            $npm = $candidate
        }
    }
    if (-not $npm) {
        Write-Warning "npm not found; keeping the committed VSIX under gui/vscode_ext_asset/ unchanged."
        exit 0
    }
    $npmCmd = if ($npm -is [string]) { $npm } else { $npm.Source }

    & $npmCmd install --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw "npm install failed ($LASTEXITCODE)" }

    & $npmCmd run package
    if ($LASTEXITCODE -ne 0) { throw "npm run package failed ($LASTEXITCODE)" }

    Write-Host "VSIX refreshed under gui/vscode_ext_asset/"
}
finally {
    Pop-Location
}
