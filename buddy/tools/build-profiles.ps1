# Builds the two Buddy panels into separate directories.
# EchoEar stays in firmware/build. The 240 module uses firmware/build-nv3023
# and does not rewrite firmware/sdkconfig.
$ErrorActionPreference = "Stop"
$firmware = Join-Path (Split-Path $PSScriptRoot -Parent) "firmware"
$merged = Join-Path $env:TEMP "buddy-sdkconfig-240.defaults"
Get-Content (Join-Path $firmware "sdkconfig.defaults"), (Join-Path $firmware "sdkconfig.defaults.240") |
    Set-Content -Encoding ascii $merged

Push-Location $firmware
try {
    cmd /c "build.cmd -B build -DSDKCONFIG_DEFAULTS=sdkconfig.defaults build"
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    cmd /c "build.cmd -B build-nv3023 -DSDKCONFIG=build-nv3023/sdkconfig -DSDKCONFIG_DEFAULTS=$merged build"
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
