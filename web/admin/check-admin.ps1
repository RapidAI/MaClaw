$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
Push-Location $repo
try {
  node 'hub/web/admin/validate-admin-modules.js'
  if ($LASTEXITCODE -ne 0) { throw 'Admin module validation failed.' }
  go test ./hub/internal/httpapi ./hub/internal/im ./hub/internal/llmservice
  if ($LASTEXITCODE -ne 0) { throw 'Go admin-related tests failed.' }
  Write-Host 'Admin checks passed.' -ForegroundColor Green
} finally {
  Pop-Location
}
