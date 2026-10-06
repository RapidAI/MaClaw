[CmdletBinding()]
param(
    [switch]$SkipHostTest
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$repoRoot = (Resolve-Path (Join-Path $projectRoot '..')).Path
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

$hdr = Join-Path $projectRoot 'main\services\event_decision.h'
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/event_decision.h' }

# The header is the host-tested surface: it must stay free of ESP-IDF and
# cJSON, otherwise the decision rules stop being verifiable without hardware.
if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' 'public header must not include ESP-IDF'
    Assert-FileLacks $hdr '\besp_err_t\b' 'public header must not expose esp_err_t'
    # Match real usage, not the prose that explains the rule: this file's own
    # comment says it stays free of cJSON, and that word must not fail the check.
    Assert-FileLacks $hdr '#include\s*[<"][^">]*cJSON|\bcJSON_[A-Za-z_]' 'public header must not depend on cJSON'

    # The gesture-to-meaning mapping belongs to the presentation layer's input
    # binding.  If this header ever learns about app_intent_service it stops
    # being compilable on the host and the safety rules lose their tests.
    # Again: match the calls, not the comment naming the thing being excluded.
    Assert-FileLacks $hdr '#include\s*[<"][^">]*app_intent_service|\bapp_intent_service_[a-z_]' 'gesture mapping must stay in the input binding'
    Assert-FileLacks $hdr '#include\s*[<"][^">]*scene_presenter|\bscene_presenter_[a-z_]' 'presentation must stay out of the decision rules'

    $hdrText = Get-Content -LiteralPath $hdr -Raw
    foreach ($required in @(
            'event_decision_reset',
            'event_decision_action_is_affirmative',
            'event_decision_affirmative_index',
            'event_decision_decline_index',
            'event_decision_busy',
            'event_decision_active',
            'event_decision_begin',
            'event_decision_apply_input',
            'event_decision_note_window',
            'event_decision_next_ack',
            'event_decision_ack_status',
            'event_decision_ack_action_id',
            'event_decision_ack_decided_by',
            'event_decision_mark_ack_delivered',
            'event_decision_settled',
            'event_decision_outcome'
        )) {
        if ($hdrText -notlike "*$required*") {
            $failures += "event_decision.h must expose the pure helper $required"
        }
    }

    # The outcome set is closed and a caller switches on it; a missing row would
    # silently collapse "timed out" into whatever the default branch says.
    foreach ($outcome in @('EVENT_DECISION_OUTCOME_NONE', 'EVENT_DECISION_OUTCOME_APPROVED',
            'EVENT_DECISION_OUTCOME_DECLINED', 'EVENT_DECISION_OUTCOME_TIMED_OUT')) {
        if ($hdrText -notlike "*$outcome*") {
            $failures += "event_decision.h missing $outcome"
        }
    }

    # Every device-side pure module in this tree is ASCII-only: the wording of
    # what the user sees is device copy and lives in the glue, so a header that
    # starts carrying UI text is a sign the split has drifted.
    $nonAscii = (Select-String -Path $hdr -Pattern '[^\x00-\x7F]').Count
    if ($nonAscii -gt 0) {
        $failures += "event_decision.h must stay ASCII-only (found $nonAscii non-ASCII line(s))"
    }

    # The ack status set is closed and mirrors the wire contract; a missing row
    # would silently drop a whole class of answer.
    foreach ($status in @('EVENT_DECISION_STATUS_RECEIVED', 'EVENT_DECISION_STATUS_APPROVED',
            'EVENT_DECISION_STATUS_REJECTED', 'EVENT_DECISION_STATUS_EXPIRED')) {
        if ($hdrText -notlike "*$status*") {
            $failures += "event_decision.h missing $status"
        }
    }

    # D5 ("timed out means not approved") is a safety property, so the rules
    # that carry it must be visible in the header, not merely implied by a test.
    foreach ($guard in @('EVENT_DECISION_DECIDED_BY_TIMEOUT', 'timed_out', 'expires_at_ms')) {
        if ($hdrText -notlike "*$guard*") {
            $failures += "event_decision.h must reference $guard to keep the timeout contract explicit"
        }
    }
}

# Drift guard: the device repeats the wire's closed-set strings because it does
# not link the Go contract.  Compare them against the single source of truth so
# a rename on one side is caught here instead of on a device.
$wire = Join-Path $repoRoot 'corelib\im\device_event.go'
if (-not (Test-Path -LiteralPath $wire)) {
    $failures += "missing wire contract $wire (drift guard cannot run)"
} elseif (Test-Path -LiteralPath $hdr) {
    $wireText = Get-Content -LiteralPath $wire -Raw
    $deviceText = Get-Content -LiteralPath $hdr -Raw
    foreach ($value in @('received', 'approved', 'rejected', 'expired',
            'button', 'timeout')) {
        if ($wireText -notlike "*`"$value`"*") {
            $failures += "wire contract no longer declares `"$value`""
        }
        if ($deviceText -notlike "*`"$value`"*") {
            $failures += "event_decision.h does not declare the wire value `"$value`""
        }
    }

    # The bounds are duplicated as literals; assert the numbers, not the names.
    foreach ($bound in @(
            @{ Pattern = 'DeviceEventMaxIDLen\s*=\s*(\d+)'; Device = 'EVENT_DECISION_MAX_EVENT_ID_LEN (\d+)'; What = 'event id length' },
            @{ Pattern = 'DeviceEventMaxActionIDLen\s*=\s*(\d+)'; Device = 'EVENT_DECISION_MAX_ACTION_ID_LEN (\d+)'; What = 'action id length' },
            @{ Pattern = 'DeviceEventMaxActions\s*=\s*(\d+)'; Device = 'EVENT_INGEST_MAX_ACTIONS (\d+)'; What = 'action count' }
        )) {
        $wireMatch = [regex]::Match($wireText, $bound.Pattern)
        if (-not $wireMatch.Success) {
            $failures += "wire contract no longer declares the $($bound.What) bound"
            continue
        }
        # The action-count bound is inherited from event_ingest.h, which the
        # device already keeps in step with the wire via check-event-ingest.ps1.
        $deviceSource = if ($bound.What -eq 'action count') {
            Join-Path $projectRoot 'main\services\event_ingest.h'
        } else {
            $hdr
        }
        $deviceText2 = Get-Content -LiteralPath $deviceSource -Raw
        $deviceMatch = [regex]::Match($deviceText2, $bound.Device)
        if (-not $deviceMatch.Success) {
            $failures += "device headers no longer declare the $($bound.What) bound"
            continue
        }
        if ($wireMatch.Groups[1].Value -ne $deviceMatch.Groups[1].Value) {
            $failures += "wire/device $($bound.What) bound drifted: wire=$($wireMatch.Groups[1].Value) device=$($deviceMatch.Groups[1].Value)"
        }
    }
}

if (-not $SkipHostTest) {
    $testC = Join-Path $PSScriptRoot 'host_tests\test_event_decision.c'
    Assert-FileHas $testC 'event_decision_apply_input' 'host test must exercise the gesture rules'
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host event decision test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_event_decision.exe'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host event decision compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host event decision test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("event decision check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "event decision check passed: closed-set ack status, timeout contract, wire drift guard, host test $hostTestStatus"
exit 0
