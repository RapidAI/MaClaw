[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$main = Join-Path $projectRoot 'main\main.c'
$alarmManager = Join-Path $projectRoot 'main\alarm_manager.c'
$appUi = Join-Path $projectRoot 'main\app_ui.c'
$failures = @()

foreach ($path in @($main, $alarmManager, $appUi)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}

if ($failures.Count -eq 0) {
    $mainText = Get-Content -LiteralPath $main -Raw
    $alarmManagerText = Get-Content -LiteralPath $alarmManager -Raw
    $appUiText = Get-Content -LiteralPath $appUi -Raw

    # These values are published and reconciled by Configuration. Keeping a
    # write-only root mirror would create a second, stale authority with no
    # reader, especially after a failed persistence/reconcile transaction.
    if ($mainText -match '\bs_configured_(?:output_volume|display_brightness|screen_sleep_seconds)(?:_saved)?\b') {
        $failures += 'main.c still owns obsolete write-only Configuration display/output caches'
    }

    # Alarm Service is the lifecycle authority. The root must ask its facade
    # rather than retain a duplicate bit that can drift after deinitialization.
    if ($mainText -match '\bs_alarm_manager_started\b') {
        $failures += 'main.c still owns a duplicate alarm-manager lifecycle flag'
    }
    if ($mainText -notmatch '(?s)static\s+bool\s+ensure_alarm_manager_started\s*\(\s*void\s*\)\s*\{\s*if\s*\(\s*alarm_manager_is_initialized\s*\(\s*\)\s*\)\s*return\s+true;') {
        $failures += 'main.c alarm startup seam must query alarm_manager_is_initialized before init'
    }
    if ($alarmManagerText -notmatch '(?s)bool\s+alarm_manager_is_initialized\s*\(\s*void\s*\)\s*\{\s*return\s+alarm_service_is_ready\s*\(\s*\)\s*;\s*\}') {
        $failures += 'alarm manager readiness facade must remain backed by Alarm Service'
    }

    # UI initialization belongs to the shared UI model owner. Root only needs
    # the value query before publishing a terminal diagnostic scene.
    if ($mainText -match '\bs_startup_ui_initialized\b') {
        $failures += 'main.c still owns a duplicate App UI initialization flag'
    }
    if ($mainText -notmatch '\bapp_ui_is_initialized\s*\(') {
        $failures += 'main.c terminal diagnostic guards must query App UI readiness'
    }
    if ($appUiText -notmatch '(?s)bool\s+app_ui_is_initialized\s*\(\s*void\s*\).*?s_initialized') {
        $failures += 'App UI readiness query must remain owned by the UI model'
    }

    # Storage Service owns VFS admission. A root mirror can become stale if a
    # mount/unmount outcome is uncertain, which would let a late optional
    # cache operation reason about a volume that the lifecycle owner closed.
    if ($mainText -match '\bs_storage_mounted\b') {
        $failures += 'main.c still owns a duplicate Storage lifecycle flag'
    }
    foreach ($storageRequirement in @(
            '\bstorage_service_is_available\s*\(',
            '\bdevice_storage_allows_optional_flash_work\s*\(')) {
        if ($mainText -notmatch $storageRequirement) {
            $failures += "main.c Storage composition wiring missing $storageRequirement"
        }
    }

    # Configuration returns staged-candidate evidence only while it owns the
    # durable boot snapshot lock. Keep that value immutable in the startup
    # state owner; a later retry/read may be unavailable and must not make the
    # candidate look confirmed to Wi-Fi or Gateway.
    if ($mainText -match '\bs_boot_provisioning_staged\b') {
        $failures += 'main.c still owns the staged provisioning boot fact'
    }
    foreach ($candidateRequirement in @(
            '\bstartup_runtime_state_service_capture_staged_provisioning\s*\(',
            '\bstartup_runtime_state_service_staged_provisioning_pending\s*\(')) {
        if ($mainText -notmatch $candidateRequirement) {
            $failures += "main.c staged provisioning composition wiring missing $candidateRequirement"
        }
    }

    # Gateway's cold-start correlation ID is a boot-lifetime value, not a
    # composition-root buffer. The startup state owner publishes the immutable
    # value after entropy generation and serves both request formatting and
    # Welcome correlation.
    if ($mainText -match '\bs_boot_session_id\b') {
        $failures += 'main.c still owns the Gateway boot-session correlation buffer'
    }
    foreach ($sessionRequirement in @(
            '\bstartup_runtime_state_service_capture_boot_session_id\s*\(',
            '\bstartup_runtime_state_service_boot_session_id\s*\(',
            '\bstartup_runtime_state_service_matches_boot_session_id\s*\(')) {
        if ($mainText -notmatch $sessionRequirement) {
            $failures += "main.c boot-session composition wiring missing $sessionRequirement"
        }
    }

    # Wi-Fi's boot candidate is mutable only for the cold-start saved-network
    # fallback and a committed portal delete. That lifecycle belongs to its
    # dedicated value-state owner, not to a root collection of credential
    # mirrors that can diverge from Configuration.
    if ($mainText -match '\bs_wifi_(?:ssid|password|networks|network_count|security|eap_method|identity|username|ttls_phase2|ca_mode|server_domain)\b') {
        $failures += 'main.c still owns Wi-Fi runtime configuration mirrors'
    }
    foreach ($wifiRequirement in @(
            '\bwifi_runtime_configuration_service_init\s*\(',
            '\bwifi_runtime_configuration_service_capture_boot_snapshot\s*\(',
            '\bwifi_runtime_configuration_service_get_snapshot\s*\(',
            '\bwifi_runtime_configuration_service_select_saved_network\s*\(',
            '\bwifi_runtime_configuration_service_sync_saved_networks_after_delete\s*\(')) {
        if ($mainText -notmatch $wifiRequirement) {
            $failures += "main Wi-Fi runtime configuration wiring missing $wifiRequirement"
        }
    }

    # SAFE_MODE's terminal transaction is single-use and owns its own state.
    # Retaining an instance in main would let the composition root accidentally
    # reuse a coordinator against a partially quiesced generation.
    if ($mainText -match '\bs_safe_mode_coordinator\b') {
        $failures += 'main.c still owns SAFE_MODE coordinator transaction state'
    }
    foreach ($safeModeRequirement in @(
            '\bsafe_mode_coordinator_configure_host\s*\(',
            '\bsafe_mode_coordinator_enter\s*\(')) {
        if ($mainText -notmatch $safeModeRequirement) {
            $failures += "main SAFE_MODE composition wiring missing $safeModeRequirement"
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("main composition-root state check failed:`n" + ($failures -join "`n"))
    exit 1
}

Write-Output 'main composition-root state check passed: Configuration, Wi-Fi runtime state, SAFE_MODE, Alarm, App UI, Storage, and startup candidate evidence retain their respective state authority'
