$ErrorActionPreference = 'Stop'
$exe = 'C:\Program Files (x86)\NSIS\makensis.exe'
$root = 'D:\workprj\aicoder'
$cfg = Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json
$buildNumber = (Get-Content (Join-Path $root 'build_number') -Raw).Trim()
$version = "$($cfg.info.productVersion).$buildNumber"

# multiarch.nsi includes build_params.nsh.tmp.  Passing the same !defines on
# the command line makes NSIS fail with "already defined" when a previous
# build left that file behind.  Generate one authoritative parameter file and
# let the installer script consume it.
& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root 'scripts\write_installer_params.ps1') `
  -Root $root -AppName ([string]$cfg.name) -Version $version -OutputDir (Join-Path $root 'dist')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$args = @(
  '/V4',
  (Join-Path $root 'build\windows\installer\multiarch.nsi')
)
$p = Start-Process -FilePath $exe -ArgumentList $args -WorkingDirectory (Join-Path $root 'build\windows\installer') -Wait -PassThru -NoNewWindow -RedirectStandardOutput (Join-Path $root 'nsis_stdout.log') -RedirectStandardError (Join-Path $root 'nsis_stderr.log')
Write-Output ("EXIT_CODE=$($p.ExitCode)")
if ($p.ExitCode -ne 0) { exit $p.ExitCode }
