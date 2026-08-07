$ErrorActionPreference = 'Stop'

$projectDir = Split-Path -Parent $PSScriptRoot
$firmware = Join-Path $projectDir 'build-gateway-fix\maclaw_esp32s3_client.bin'
$python = 'C:\Espressif\tools\python\v6.0.2\venv\Scripts\python.exe'
$flashLog = Join-Path $projectDir 'flash-settings-com3.log'
$serialLog = Join-Path $projectDir 'serial-settings-com3.log'
$expectedSha256 = '22E1935F3626AA5773C663A32B3AEB1D9C01EA5A2CD2AAC77FD7328182E890D2'
$deadline = (Get-Date).AddHours(24)

function Write-FlashLog([string]$message) {
    Add-Content -LiteralPath $flashLog -Encoding UTF8 -Value ('[{0:O}] {1}' -f (Get-Date), $message)
}

function Test-EspressifCom3 {
    if (-not ([System.IO.Ports.SerialPort]::GetPortNames() -contains 'COM3')) {
        return $false
    }
    # ESP32-S3's native USB serial/JTAG function is exposed in two valid PnP
    # forms depending on driver/boot mode: VID/PID directly, or a composite
    # child carrying &MI_00. Resolve the current COM assignment first, then
    # validate the immutable Espressif VID/PID so COM1 can never be flashed.
    $device = Get-CimInstance Win32_SerialPort -ErrorAction SilentlyContinue |
        Where-Object {
            $_.DeviceID -eq 'COM3' -and
            $_.PNPDeviceID -match '^USB\\VID_303A&PID_1001(?:&MI_00)?\\'
        } |
        Select-Object -First 1
    return $null -ne $device
}

Set-Content -LiteralPath $flashLog -Encoding UTF8 -Value ('[{0:O}] waiting for Espressif COM3' -f (Get-Date))

if (-not (Test-Path -LiteralPath $firmware)) {
    Write-FlashLog "firmware missing: $firmware"
    exit 2
}
if (-not (Test-Path -LiteralPath $python)) {
    Write-FlashLog "ESP-IDF Python missing: $python"
    exit 3
}
$actualSha256 = (Get-FileHash -LiteralPath $firmware -Algorithm SHA256).Hash
if ($actualSha256 -ne $expectedSha256) {
    Write-FlashLog "firmware hash changed: expected=$expectedSha256 actual=$actualSha256"
    exit 4
}

while ((Get-Date) -lt $deadline -and -not (Test-EspressifCom3)) {
    Start-Sleep -Milliseconds 500
}
if (-not (Test-EspressifCom3)) {
    Write-FlashLog 'timed out waiting for Espressif COM3'
    exit 5
}

Write-FlashLog "device present; flashing App only, sha256=$actualSha256"
$arguments = @(
    '-m', 'esptool', '--chip', 'esp32s3', '-p', 'COM3', '-b', '460800',
    '--before', 'default-reset', '--after', 'hard-reset',
    'write-flash', '0x10000', $firmware
)
& $python @arguments *>> $flashLog
if ($LASTEXITCODE -ne 0) {
    Write-FlashLog "esptool failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}
Write-FlashLog 'App flash completed successfully'

$serialDeadline = (Get-Date).AddSeconds(30)
while ((Get-Date) -lt $serialDeadline -and
       -not ([System.IO.Ports.SerialPort]::GetPortNames() -contains 'COM3')) {
    Start-Sleep -Milliseconds 250
}
if (-not ([System.IO.Ports.SerialPort]::GetPortNames() -contains 'COM3')) {
    Write-FlashLog 'COM3 did not return for startup log capture'
    exit 0
}

$serial = [System.IO.Ports.SerialPort]::new('COM3', 115200, 'None', 8, 'One')
$serial.DtrEnable = $false
$serial.RtsEnable = $false
$serial.ReadTimeout = 250
$serial.Open()
try {
    Set-Content -LiteralPath $serialLog -Encoding UTF8 -Value ('[{0:O}] serial capture started' -f (Get-Date))
    $captureUntil = (Get-Date).AddSeconds(90)
    while ((Get-Date) -lt $captureUntil) {
        try {
            $chunk = $serial.ReadExisting()
            if ($chunk) {
                Add-Content -LiteralPath $serialLog -Encoding UTF8 -NoNewline -Value $chunk
            }
        } catch [System.TimeoutException] {
        }
        Start-Sleep -Milliseconds 50
    }
} finally {
    if ($serial.IsOpen) { $serial.Close() }
    $serial.Dispose()
}
Write-FlashLog 'startup serial capture completed'


