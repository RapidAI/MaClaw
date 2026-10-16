[CmdletBinding()]
param(
    [switch]$SkipHostTest
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$failures = @()
$hostTestStatus = 'skipped'

function Assert-FileLacks([string]$Path, [string]$Pattern, [string]$Why) {
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $hits = Select-String -Path $Path -Pattern $Pattern
    if ($hits) {
        $script:failures += "${Why}: found /$Pattern/ in $Path ($($hits.Count) hit(s))"
    }
}

function Assert-FileHas([string]$Path, [string]$Pattern, [string]$Why) {
    if (-not (Test-Path -LiteralPath $Path)) {
        $script:failures += "${Why}: missing $Path"
        return
    }
    if (-not (Select-String -Path $Path -Pattern $Pattern -Quiet)) {
        $script:failures += "${Why}: /$Pattern/ not found in $Path"
    }
}

$src = Join-Path $projectRoot 'main\services\event_presentation_policy.c'
$hdr = Join-Path $projectRoot 'main\services\event_presentation_policy.h'
if (-not (Test-Path -LiteralPath $src)) { $failures += 'missing services/event_presentation_policy.c' }
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/event_presentation_policy.h' }

# The public header is the host-tested surface: it must stay free of ESP-IDF,
# otherwise the admission matrix stops being verifiable without hardware.
if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' 'public header must not include ESP-IDF'
    Assert-FileLacks $hdr '\besp_err_t\b' 'public header must not expose esp_err_t'
    Assert-FileLacks $hdr '\bcJSON\b' 'public header must not depend on cJSON'

    $hdrText = Get-Content -LiteralPath $hdr -Raw
    foreach ($required in @(
            'event_severity_parse',
            'event_severity_name',
            'event_device_state_dominant',
            'event_presentation_decide',
            'event_presentation_decide_from_flags',
            'event_presentation_speech_text',
            'event_action_name',
            'event_action_is_audible',
            'event_action_preempts_playback',
            'event_action_preempts_capture',
            'event_action_should_display',
            'event_action_should_buzz'
        )) {
        if ($hdrText -notlike "*$required*") {
            $failures += "event_presentation_policy.h must expose the pure helper $required"
        }
    }

    # Severity is a closed set mirroring the wire contract.  A missing level
    # would silently drop a whole row of the admission matrix.
    foreach ($severity in @('EVENT_SEVERITY_SILENT', 'EVENT_SEVERITY_SOFT', 'EVENT_SEVERITY_INTERRUPT')) {
        if ($hdrText -notlike "*$severity*") {
            $failures += "event_presentation_policy.h missing $severity"
        }
    }
    # Every column of section 4.2 must have a named state.
    foreach ($state in @(
            'EVENT_STATE_STANDBY', 'EVENT_STATE_SPEAKING', 'EVENT_STATE_LISTENING',
            'EVENT_STATE_QUIET_HOURS', 'EVENT_STATE_MEETING_RECORDING'
        )) {
        if ($hdrText -notlike "*$state*") {
            $failures += "event_presentation_policy.h missing $state"
        }
    }
    # Every outcome of the matrix must be nameable for the logs.
    foreach ($action in @(
            'EVENT_ACTION_DROP', 'EVENT_ACTION_DISPLAY', 'EVENT_ACTION_DISPLAY_AND_BUZZ',
            'EVENT_ACTION_SPEAK', 'EVENT_ACTION_QUEUE_SPEAK',
            'EVENT_ACTION_INTERRUPT_SPEAK', 'EVENT_ACTION_INTERRUPT_LISTEN'
        )) {
        if ($hdrText -notlike "*$action*") {
            $failures += "event_presentation_policy.h missing $action"
        }
    }
}

# The glue is the only place allowed to reach for device services.
if (Test-Path -LiteralPath $src) {
    Assert-FileHas $src 'event_presentation_current_flags' `
        'the glue must expose the live-state snapshot'
    Assert-FileHas $src 'foreground_coordinator_current' `
        'speaking/listening must come from the foreground lease, not from pixels'
    Assert-FileHas $src 'meeting_service_is_active' `
        'the recorder outlives the foreground lease and must be asked directly'
    Assert-FileHas $src 'sleep_schedule_service_get_status' `
        'quiet hours must come from the sleep schedule status snapshot'
    # A manual wake means the user is awake: the override must be honoured or
    # a just-woken user gets treated as asleep and loses their events.
    Assert-FileHas $src 'override_active' `
        'the manual wake override must be honoured before declaring quiet hours'
}

$cmake = Get-Content -LiteralPath (Join-Path $projectRoot 'main\CMakeLists.txt') -Raw
if ($cmake -notlike '*services/event_presentation_policy.c*') {
    $failures += 'CMakeLists.txt missing services/event_presentation_policy.c'
}

if (-not $SkipHostTest) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host event presentation policy test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_event_presentation_policy.exe'
        $testC = Join-Path $PSScriptRoot 'host_tests\test_event_presentation_policy.c'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host event presentation policy compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host event presentation policy test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("event presentation policy check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "event presentation policy check passed: closed-set severity, section 4.2 matrix, host test $hostTestStatus"
exit 0
