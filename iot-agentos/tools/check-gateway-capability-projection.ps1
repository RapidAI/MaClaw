[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\services\gateway_capability_projection.c'
$header = Join-Path $projectRoot 'main\services\gateway_capability_projection.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_gateway_capability_projection.c'
$cmake = Join-Path $projectRoot 'main\CMakeLists.txt'
$failures = @()

foreach ($path in @($source, $header, $test, $cmake)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

foreach ($path in @($source, $header)) {
    if (Test-Path -LiteralPath $path) {
        $text = Get-Content -LiteralPath $path -Raw
        if ($text -cmatch '\b(?:esp_|freertos/|driver/|cJSON|TaskHandle_t|SemaphoreHandle_t|board_|platform_)\b') {
            $failures += "Gateway capability projection leaked platform, parser, RTOS, or board detail ($path)"
        }
    }
}

if ((Test-Path -LiteralPath $header) -and
    ((Get-Content -LiteralPath $header -Raw) -notmatch 'effective_capabilities' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'accepted_capabilities' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'negotiated_capabilities' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'operational_capabilities' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY')) {
    $failures += 'Gateway capability projection contract is missing effective/accepted/negotiated/health hysteresis evidence'
}

if ((Test-Path -LiteralPath $header) -and
    ((Get-Content -LiteralPath $header -Raw) -notmatch 'gateway_capability_lease_t' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'GATEWAY_CAPABILITY_LEASE_ABI_VERSION' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'gateway_capability_projection_capture_lease' -or
     (Get-Content -LiteralPath $header -Raw) -notmatch 'gateway_capability_projection_lease_current')) {
    $failures += 'Gateway capability projection contract is missing the value-only lease API'
}

if ((Test-Path -LiteralPath $source) -and
    ((Get-Content -LiteralPath $source -Raw) -notmatch 'accepted_capabilities\s*&\s*~projection->effective_capabilities' -or
     (Get-Content -LiteralPath $source -Raw) -notmatch 'consecutive_failures\s*>=\s*GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE' -or
     (Get-Content -LiteralPath $source -Raw) -notmatch 'consecutive_successes\s*>=\s*GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY')) {
    $failures += 'Gateway capability projection does not fail closed on malformed acceptance or retain health hysteresis'
}

if ((Test-Path -LiteralPath $source) -and
    ((Get-Content -LiteralPath $source -Raw) -notmatch 'lease->generation' -or
     (Get-Content -LiteralPath $source -Raw) -notmatch 'lease->required_capabilities' -or
     (Get-Content -LiteralPath $source -Raw) -notmatch 'gateway_capability_projection_capture_lease' -or
     (Get-Content -LiteralPath $source -Raw) -notmatch 'gateway_capability_projection_lease_current')) {
    $failures += 'Gateway capability projection does not validate generation-bound capability leases'
}

$transportSource = Join-Path $projectRoot 'main\services\gateway_transport.c'
$transportHeader = Join-Path $projectRoot 'main\services\gateway_transport.h'
foreach ($path in @($transportSource, $transportHeader)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $transportSource) -and
    ((Get-Content -LiteralPath $transportSource -Raw) -notmatch 'parse_accepted_gateway_capabilities' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'json_array_is_string_list' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'if \(!cJSON_IsObject\(output\)\) return false;' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'if \(!json_array_is_string_list\(output_modalities\)\) return false;' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'gateway_capability_projection_observe_accepted' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'gateway_capability_projection_observe_transport_result' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'gateway_capability_projection_set_effective')) {
    $failures += 'Gateway transport is not wired to the capability projection owner or fails to validate the mandatory accepted output schema'
}

$hubGatewayTest = Join-Path (Split-Path $projectRoot -Parent) 'hub\internal\im\device_gateway_test.go'
if (-not (Test-Path -LiteralPath $hubGatewayTest)) {
    $failures += "missing $hubGatewayTest"
} else {
    $hubGatewayTestText = Get-Content -LiteralPath $hubGatewayTest -Raw
    if ($hubGatewayTestText -notmatch 'TestDeviceGatewayHandshakeCapabilitiesAcceptedWireContract' -or
        $hubGatewayTestText -notmatch 'TestDeviceGatewayLegacyHandshakeCapabilitiesAcceptedWireContract' -or
        $hubGatewayTestText -notmatch 'futureUnknownFeature' -or
        $hubGatewayTestText -notmatch 'petAssetMaxFrames was not bounded') {
        $failures += 'Hub lacks full-surface and legacy capabilitiesAccepted wire-contract regression coverage'
    }
}

$dispatcherSource = Join-Path $projectRoot 'main\services\gateway_dispatcher.c'
$dispatcherHeader = Join-Path $projectRoot 'main\services\gateway_dispatcher.h'
foreach ($path in @($dispatcherSource, $dispatcherHeader)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $transportHeader) -and
    ((Get-Content -LiteralPath $transportHeader -Raw) -notmatch 'gateway_transport_capabilities_operational' -or
     (Get-Content -LiteralPath $transportHeader -Raw) -notmatch 'gateway_transport_observe_capability_control_plane_success')) {
    $failures += 'Gateway transport lacks the value-only operational capability consumer seam'
}
if ((Test-Path -LiteralPath $transportHeader) -and
    ((Get-Content -LiteralPath $transportHeader -Raw) -notmatch 'gateway_transport_capture_capability_lease' -or
     (Get-Content -LiteralPath $transportHeader -Raw) -notmatch 'gateway_transport_capability_lease_current')) {
    $failures += 'Gateway transport lacks the value-only capability lease seam'
}
if ((Test-Path -LiteralPath $transportSource) -and
    ((Get-Content -LiteralPath $transportSource -Raw) -notmatch 'gateway_capability_projection_capture_lease' -or
     (Get-Content -LiteralPath $transportSource -Raw) -notmatch 'gateway_capability_projection_lease_current')) {
    $failures += 'Gateway transport does not synchronize capability lease capture/current checks'
}
if ((Test-Path -LiteralPath $dispatcherSource) -and
    ((Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'gateway_message_capability_allowed' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_OUTPUT_TEXT' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_OUTPUT_AUDIO' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_OUTPUT_IMAGE' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_AMBIENT_DISPLAY' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_PET_STATE' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_PET_ASSET' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_VOLUME_CONTROL' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'gateway_transport_observe_capability_control_plane_success')) {
    $failures += 'Gateway Dispatcher must gate reply, ambient, pet, and hardware-config messages through operational capability projection and record only valid poll control-plane success'
}
if ((Test-Path -LiteralPath $dispatcherSource) -and
    ((Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'gateway_reply_audio_lease_current' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'GATEWAY_CAPABILITY_OUTPUT_AUDIO,\s*&audio_lease' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'before inline playback' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'before audio download' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'after audio download' -or
     (Get-Content -LiteralPath $dispatcherSource -Raw) -notmatch 'before audio acknowledgement')) {
    $failures += 'Reply audio is not generation-bound at decode/download/playback/acknowledgement boundaries'
}

$meetingSource = Join-Path $projectRoot 'main\services\meeting_service.c'
if (-not (Test-Path -LiteralPath $meetingSource)) {
    $failures += "missing $meetingSource"
} else {
    $meetingText = Get-Content -LiteralPath $meetingSource -Raw
    if ($meetingText -notmatch 'GATEWAY_CAPABILITY_MEETING_RECORDER' -or
        $meetingText -notmatch 'gateway_transport_capture_capability_lease' -or
        $meetingText -notmatch 'meeting_gateway_lease_current' -or
        $meetingText -notmatch 'gateway_capability_lease_t' -or
        $meetingText -notmatch 'recording_post_action') {
        $failures += 'Meeting worker is not generation-bound to an operational meeting-recorder capability lease'
    }
    if ($meetingText -notmatch '网关能力变更后可续传') {
        $failures += 'Meeting capability withdrawal is still presented as a generic network failure'
    }
}

$rootSource = Join-Path $projectRoot 'main\main.c'
$configurationSource = Join-Path $projectRoot 'main\configuration_reconcile_service.c'
$configurationHeader = Join-Path $projectRoot 'main\configuration_reconcile_service.h'
if (-not (Test-Path -LiteralPath $rootSource)) {
    $failures += "missing $rootSource"
} else {
    $rootText = Get-Content -LiteralPath $rootSource -Raw
    if ($rootText -notmatch 'GATEWAY_CAPABILITY_PET_ASSET' -or
        $rootText -notmatch 'gateway_transport_capture_capability_lease' -or
        $rootText -notmatch 'pet_asset_gateway_lease_current' -or
        $rootText -notmatch 'download_pet_asset_frames' -or
        $rootText -notmatch 'cache_pet_asset_in_background') {
        $failures += 'Pet asset download/cache/install path is not generation-bound to an operational pet-asset capability lease'
    }
    if ($rootText -notmatch 'gateway_configuration_authorization_from_lease' -or
        $rootText -notmatch 'configuration_reconcile_service_reconcile_authorized' -or
        $rootText -notmatch 'before persistence: capability lease changed' -or
        $rootText -notmatch 'before reconcile: capability lease changed' -or
        $rootText -notmatch 'before acknowledgement: capability lease changed') {
        $failures += 'Hardware-config persistence/reconcile/ack boundaries are not fenced by the Gateway generation lease'
    }
}

foreach ($path in @($configurationSource, $configurationHeader)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ((Test-Path -LiteralPath $configurationSource) -and
    ((Get-Content -LiteralPath $configurationSource -Raw) -cmatch
        '\b(?:gateway_transport|gateway_capability|cJSON|esp_http|board_|platform_)\b')) {
    $failures += 'Configuration authorization implementation leaked Gateway, parser, HTTP, board, or platform detail'
}
if ((Test-Path -LiteralPath $configurationHeader) -and
    ((Get-Content -LiteralPath $configurationHeader -Raw) -cmatch
        '\b(?:gateway_transport|gateway_capability|cJSON|esp_http|board_|platform_)\b')) {
    $failures += 'Configuration authorization contract leaked Gateway, parser, HTTP, board, or platform detail'
}
if ((Test-Path -LiteralPath $configurationSource) -and
    ((Get-Content -LiteralPath $configurationSource -Raw) -notmatch
        'configuration_reconcile_service_reconcile_authorized' -or
     (Get-Content -LiteralPath $configurationSource -Raw) -notmatch
        's_retry_authorization_valid' -or
     (Get-Content -LiteralPath $configurationSource -Raw) -notmatch
        'authorization_current\(authorization\)' -or
     (Get-Content -LiteralPath $configurationSource -Raw) -notmatch
        'reconcile_internal\(reason, retry_authorization_valid')) {
    $failures += 'Configuration reconcile is not generation-fenced across consumers and retained retry work'
}

if ((Test-Path -LiteralPath $cmake) -and
    ((Get-Content -LiteralPath $cmake -Raw) -notmatch '"services/gateway_capability_projection\.c"')) {
    $failures += 'Gateway capability projection source is not compiled by the main component'
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for Gateway capability projection test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $exe = Join-Path $outDir 'test_gateway_capability_projection.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host Gateway capability projection compile failed (exit $LASTEXITCODE)"
    } else {
        & $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host Gateway capability projection test failed (exit $LASTEXITCODE)"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("Gateway capability projection check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Gateway capability projection check passed: effective, accepted, negotiated, health-hysteresis, and generation-bound leases remain value-only and fail closed.'
