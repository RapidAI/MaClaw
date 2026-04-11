$ErrorActionPreference = 'Stop'
$exe = 'C:\Program Files (x86)\NSIS\makensis.exe'
$args = @(
  '/V4',
  '/DINFO_PROJECTNAME=MaClaw',
  '/DPRODUCT_EXECUTABLE=MaClaw.exe',
  '"/DINFO_PRODUCTNAME=码卡龙·涅槃 (MaClaw)"',
  '/DINFO_COMPANYNAME=RapidAI',
  '"/DINFO_COPYRIGHT=Copyright (C) 2026 RapidAI"',
  '/DINFO_PRODUCTVERSION=5.6.0.10084',
  '/DARG_WAILS_AMD64_BINARY=D:\workprj\aicoder\dist\MaClaw_amd64.exe',
  '/DARG_WAILS_ARM64_BINARY=D:\workprj\aicoder\dist\MaClaw_arm64.exe',
  '/DMUI_ICON_PATH=D:\workprj\aicoder\build\windows\icon.ico',
  'D:\workprj\aicoder\build\windows\installer\multiarch.nsi'
)
$p = Start-Process -FilePath $exe -ArgumentList $args -WorkingDirectory 'D:\workprj\aicoder' -Wait -PassThru -NoNewWindow -RedirectStandardOutput 'D:\workprj\aicoder\nsis_stdout.log' -RedirectStandardError 'D:\workprj\aicoder\nsis_stderr.log'
Write-Output ("EXIT_CODE=$($p.ExitCode)")
