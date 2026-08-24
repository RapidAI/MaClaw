[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# Hardware input must have exactly one shared route into business policy:
#
#   selected key/touch adapter -> Platform Input -> Input Service
#       -> Device Input -> App Intent -> Input Binding -> business services
#
# This is deliberately a source-level regression rather than a board-specific
# test.  It protects a future hardware profile from gaining a convenient
# callback into UI or business code while preserving the profile-private
# gesture recognizers below Platform Input.
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$violations = @()

function Read-Required([string]$relativePath) {
    $path = Join-Path $projectRoot $relativePath
    if (-not (Test-Path -LiteralPath $path)) {
        $script:violations += "${relativePath}: required Input HAL route source is missing"
        return ''
    }
    return Get-Content -LiteralPath $path -Raw
}

function Require-Pattern([string]$relativePath, [string]$text,
                         [string]$pattern, [string]$message) {
    if ($text -notmatch $pattern) {
        $script:violations += "${relativePath}: ${message}"
    }
}

$inputService = Read-Required 'main/input_service.c'
Require-Pattern 'main/input_service.c' $inputService `
    'platform_input_start\s*\(\s*input_service_publish_from_board\s*,\s*NULL\s*\)' `
    'must install its queueing publisher as the sole Platform Input callback'
Require-Pattern 'main/input_service.c' $inputService `
    's_input_service\.consumer\s*\(\s*&event\.input\s*,\s*s_input_service\.consumer_context\s*\)' `
    'must dispatch normalized Device Input only through its installed consumer'
if ($inputService -match '\b(?:app_ui_|input_binding_handle_event|interaction_service_|meeting_service_|alarm_manager_)') {
    $violations += 'main/input_service.c: shared Input Service must not dispatch UI or business policy directly'
}

$deviceApi = Read-Required 'main/device_api.c'
Require-Pattern 'main/device_api.c' $deviceApi `
    'device_input_start\s*\([^)]*\)\s*\{\s*return\s+input_service_start\s*\(' `
    'Device Input facade must route startup only to Input Service'
if ($deviceApi -match '\b(?:platform_input_|app_ui_|input_binding_handle_event|interaction_service_|meeting_service_)') {
    $violations += 'main/device_api.c: Device Input facade must not bypass Input Service into Platform/UI/business code'
}

$intentService = Read-Required 'main/app_intent_service.c'
Require-Pattern 'main/app_intent_service.c' $intentService `
    'device_input_start\s*\(\s*on_device_input\s*,\s*NULL\s*\)' `
    'must subscribe to Device Input through on_device_input'
Require-Pattern 'main/app_intent_service.c' $intentService `
    's_service\.consumer\s*\(\s*&event\.intent\s*,\s*s_service\.consumer_context\s*\)' `
    'must dispatch App Intent only through its installed consumer'
foreach ($requiredIntent in @(
        'DEVICE_INPUT_PRIMARY', 'APP_INTENT_PRIMARY_ACTIVATE',
        'DEVICE_INPUT_SECONDARY', 'APP_INTENT_SECONDARY_ACTIVATE',
        'DEVICE_INPUT_CONFIGURE', 'APP_INTENT_OPEN_CONFIGURATION',
        'DEVICE_INPUT_VOLUME_UP', 'APP_INTENT_INCREASE_VOLUME',
        'DEVICE_INPUT_VOLUME_DOWN', 'APP_INTENT_DECREASE_VOLUME')) {
    Require-Pattern 'main/app_intent_service.c' $intentService $requiredIntent `
        "must own the shared normalized action mapping (${requiredIntent})"
}
if ($intentService -match '\b(?:app_ui_|input_binding_handle_event|interaction_service_|meeting_service_|alarm_manager_|board_port|CONFIG_MACLAW_BOARD_|\bgpio_|\bi2c_)') {
    $violations += 'main/app_intent_service.c: App Intent must remain profile/UI/business implementation neutral'
}

$binding = Read-Required 'main/presentation/input_binding.c'
Require-Pattern 'main/presentation/input_binding.c' $binding `
    'void\s+input_binding_handle_event\s*\(\s*const\s+app_intent_event_t\s*\*event\s*\)' `
    'must provide the single abstract business dispatch entry'
if ($binding -match '\b(?:board_port|platform_input|compact_input|round_input|CONFIG_MACLAW_BOARD_|\bgpio_|\bi2c_)') {
    $violations += 'main/presentation/input_binding.c: business dispatch must not select hardware, scanner, GPIO or touch/I2C detail'
}

$mainSource = Read-Required 'main/main.c'
Require-Pattern 'main/main.c' $mainSource `
    'static\s+void\s+on_app_intent\s*\([^)]*\)\s*\{[\s\S]{0,450}?input_binding_handle_event\s*\(\s*event\s*\)' `
    'composition callback must delegate abstract intents to Input Binding'
Require-Pattern 'main/main.c' $mainSource `
    'app_intent_service_start\s*\(\s*on_app_intent\s*,\s*NULL\s*\)' `
    'must start the shared App Intent service with the composition callback'

$platformInput = Read-Required 'main/platform_input.c'
Require-Pattern 'main/platform_input.c' $platformInput `
    'return\s+platform_input_profile_start\s*\(\s*on_input\s*,\s*context\s*\)' `
    'Platform Input facade must delegate publisher installation only to the selected profile'
if ($platformInput -match '\b(?:board_port|app_ui_|app_intent_service_|input_binding_handle_event|interaction_service_|meeting_service_)') {
    $violations += 'main/platform_input.c: Platform Input facade must not use compatibility board/UI/business paths'
}

$hardwareInputSources = @(
    'main/platform_input_compact.c',
    'main/platform_input_round.c',
    'main/compact_input_service.c',
    'main/round_input_service.c',
    'main/round_input_profile_service.c',
    'main/boards/compact_input_adapter.h',
    'main/boards/bread_compact/bread_input_adapter.h',
    'main/boards/fangtang_4g/fangtang_input_adapter.h',
    'main/boards/round_input_profile_adapter.h',
    'main/boards/echoear_2st/echoear_input_adapter.h',
    'main/boards/waveshare_amoled_1_75c/waveshare_input_adapter.h'
)
$hardwareBypassPattern = '\b(?:app_ui_|app_intent_service_|input_service_|input_binding_handle_event|interaction_service_|meeting_service_|alarm_manager_|sleep_schedule_service_)'
foreach ($relativePath in $hardwareInputSources) {
    $text = Read-Required $relativePath
    if ($text -match $hardwareBypassPattern) {
        $violations += "${relativePath}: hardware Input implementation must publish normalized input only; direct UI/App Intent/business dependency is forbidden"
    }
}

# `input_binding_handle_event` is intentionally narrow: a declaration, its
# definition, and the composition-root callback.  A second caller is a direct
# business bypass around the queueing/lifecycle boundary.
$allCFiles = Get-ChildItem -Path (Join-Path $projectRoot 'main') -Recurse -File -Include '*.c','*.h'
$bindingReferences = @($allCFiles | Where-Object {
    Select-String -Path $_.FullName -Pattern '\binput_binding_handle_event\s*\(' -Quiet
} | ForEach-Object {
    $_.FullName.Substring($projectRoot.Length + 1).Replace('\', '/')
} | Sort-Object)
$expectedBindingReferences = @(
    'main/main.c',
    'main/presentation/input_binding.c',
    'main/presentation/input_binding.h'
)
if (($bindingReferences -join '|') -ne ($expectedBindingReferences -join '|')) {
    $violations += "input_binding_handle_event: only composition root and binding contract may reference it; found: $($bindingReferences -join ', ')"
}

if ($violations.Count -gt 0) {
    $violations | ForEach-Object { Write-Error $_ }
    exit 1
}

Write-Host 'Input HAL routing regression passed.'
