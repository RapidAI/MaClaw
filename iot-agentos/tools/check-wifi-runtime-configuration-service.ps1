[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$header = Join-Path $projectRoot 'main\services\wifi_runtime_configuration_service.h'
$source = Join-Path $projectRoot 'main\services\wifi_runtime_configuration_service.c'
$main = Join-Path $projectRoot 'main\main.c'
$testSource = Join-Path $PSScriptRoot 'host_tests\test_wifi_runtime_configuration_service.c'
$failures = @()

foreach ($path in @($header, $source, $main, $testSource)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $headerText = Get-Content -LiteralPath $header -Raw
    $sourceText = Get-Content -LiteralPath $source -Raw
    $mainText = Get-Content -LiteralPath $main -Raw
    foreach ($api in @('wifi_runtime_configuration_service_init',
                         'wifi_runtime_configuration_service_capture_boot_snapshot',
                         'wifi_runtime_configuration_service_get_snapshot',
                         'wifi_runtime_configuration_service_select_saved_network',
                         'wifi_runtime_configuration_service_sync_saved_networks_after_delete')) {
        if ($headerText -notmatch ("\b$api\b")) {
            $failures += "Wi-Fi runtime configuration public contract missing $api"
        }
    }
    if ($headerText -match '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|heap_caps|esp_wifi|esp_netif|httpd|board_)\b') {
        $failures += 'Wi-Fi runtime configuration public contract leaked SDK/RTOS/HTTP/allocator/Wi-Fi/netif/board detail'
    }
    if ($sourceText -match '\b(?:esp_wifi|esp_netif|esp_http|heap_caps|xTask|SemaphoreHandle_t|gateway_transport|board_port|CONFIG_MACLAW_BOARD_)\b') {
        $failures += 'Wi-Fi runtime configuration state owner absorbed physical SDK/HTTP/allocator/RTOS/transport/board ownership'
    }
    if ($sourceText -notmatch 'strcmp\(s_snapshot\.security, "enterprise"\) != 0') {
        $failures += 'deleted active personal Wi-Fi must clear runtime credentials while enterprise credentials remain active'
    }
    if ($mainText -match '\bs_wifi_(?:ssid|password|networks|network_count|security|eap_method|identity|username|ttls_phase2|ca_mode|server_domain)\b') {
        $failures += 'main.c still owns Wi-Fi runtime configuration mirrors'
    }
    foreach ($rootRequirement in @('wifi_runtime_configuration_service_init\s*\(',
                                    'wifi_runtime_configuration_service_capture_boot_snapshot\s*\(',
                                    'wifi_runtime_configuration_service_get_snapshot\s*\(',
                                    'wifi_runtime_configuration_service_select_saved_network\s*\(',
                                    'wifi_runtime_configuration_service_sync_saved_networks_after_delete\s*\(')) {
        if ($mainText -notmatch $rootRequirement) {
            $failures += "main Wi-Fi runtime configuration wiring missing $rootRequirement"
        }
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Wi-Fi runtime configuration test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_wifi_runtime_configuration_service.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" `
        $testSource $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Wi-Fi runtime configuration compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Wi-Fi runtime configuration test failed (exit $LASTEXITCODE)"
        }
        if ($failures.Count -eq 0) {
            & $exe enterprise
            if ($LASTEXITCODE -ne 0) {
                $failures += "host enterprise Wi-Fi runtime configuration test failed (exit $LASTEXITCODE)"
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Wi-Fi runtime configuration check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Wi-Fi runtime configuration check passed: boot snapshot, fallback selection, and portal deletion remain value-only and host-tested'
