[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# These headers are the shared, hardware-neutral contracts.  Board ports and
# profile-private adapters intentionally sit below this list, so they may use
# ESP-IDF while translating the contract to real hardware.
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$headers = @(
    'main/device_api.h',
    'main/board_profile.h',
    'main/app_ui.h',
    'main/platform_audio.h',
    'main/platform_connectivity.h',
    'main/platform_display.h',
    'main/platform_input.h',
    'main/platform_lifecycle.h',
    'main/platform_power.h',
    'main/platform_sensor.h',
    'main/platform_storage.h'
)

# Keep the check deliberately structural: it guards SDK/RTOS/driver object
# leakage, not ordinary words in comments.  Add a new hardware-neutral value
# type to device_api.h instead of allowing a profile implementation type here.
$forbidden = @(
    @{ Name = 'ESP-IDF/FreeRTOS/driver include'; Pattern = '#include\s*[<\"](?:esp_|freertos/|driver/|lwip/|soc/|hal/)' },
    @{ Name = 'ESP-IDF error type'; Pattern = '\besp_err_t\b' },
    @{ Name = 'FreeRTOS handle'; Pattern = '\b(?:Task|Queue|Semaphore|EventGroup)Handle_t\b' },
    @{ Name = 'ESP timer handle'; Pattern = '\besp_timer_handle_t\b' },
    @{ Name = 'NVS handle'; Pattern = '\bnvs_handle_t\b' },
    @{ Name = 'JSON object pointer'; Pattern = '\bcJSON\s*\*' },
    @{ Name = 'driver port/pin type'; Pattern = '\b(?:gpio_num_t|i2c_port_t|i2s_port_t|uart_port_t)\b' }
)

$violations = @()
foreach ($relativePath in $headers) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $violations += "${relativePath}: required HAL boundary header is missing"
        continue
    }
    $lineNumber = 0
    foreach ($line in Get-Content -LiteralPath $path) {
        $lineNumber++
        foreach ($rule in $forbidden) {
            if ($line -match $rule.Pattern) {
                $violations += "${relativePath}:${lineNumber}: $($rule.Name): $($line.Trim())"
            }
        }
    }
}

if ($violations.Count -ne 0) {
    Write-Error ("HAL boundary check failed:`n" + ($violations -join "`n"))
    exit 1
}

Write-Output 'HAL boundary check passed: shared Device/Platform headers expose no SDK, RTOS, JSON, or driver object types.'
