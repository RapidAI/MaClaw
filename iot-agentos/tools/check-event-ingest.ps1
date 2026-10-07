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

$hdr = Join-Path $projectRoot 'main\services\event_ingest.h'
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/event_ingest.h' }

# The header is the host-tested surface: it must stay free of ESP-IDF and
# cJSON, otherwise the ingest rules stop being verifiable without hardware.
if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' 'public header must not include ESP-IDF'
    Assert-FileLacks $hdr '\besp_err_t\b' 'public header must not expose esp_err_t'
    Assert-FileLacks $hdr '\bcJSON\b' 'public header must not depend on cJSON'

    $hdrText = Get-Content -LiteralPath $hdr -Raw
    foreach ($required in @(
            'event_category_parse',
            'event_category_name',
            'event_ingest_validate',
            'event_ingest_decide',
            'event_ingest_dedupe_key',
            'event_ingest_display_title',
            'event_ingest_display_body',
            'event_ingest_defers_while_busy',
            'event_ingest_dedupe_seen',
            'event_ingest_dedupe_reset',
            'event_ingest_action_kind_valid',
            'event_ingest_action_risk_valid'
        )) {
        if ($hdrText -notlike "*$required*") {
            $failures += "event_ingest.h must expose the pure helper $required"
        }
    }

    # The category set is closed and mirrors the wire contract; a missing row
    # would silently drop a whole class of event.
    foreach ($category in @('EVENT_CATEGORY_APPROVAL', 'EVENT_CATEGORY_TASK_DONE',
            'EVENT_CATEGORY_SCHEDULE', 'EVENT_CATEGORY_VE', 'EVENT_CATEGORY_SYSTEM')) {
        if ($hdrText -notlike "*$category*") {
            $failures += "event_ingest.h missing $category"
        }
    }

    # The approval audit contract must be enforced here, not merely trusted to
    # the Hub.  Without this assertion a future refactor could quietly drop it.
    foreach ($guard in @('EVENT_CATEGORY_APPROVAL', 'requires_ack', 'persist', 'ttl_sec')) {
        if ($hdrText -notlike "*$guard*") {
            $failures += "event_ingest.h must reference $guard in the approval audit contract"
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
    # Category lives in event_ingest.h; severity lives in the presentation
    # policy header it includes.  Search both so the guard covers the whole
    # closed set the device repeats from the wire contract.
    $policyHdr = Join-Path $projectRoot 'main\services\event_presentation_policy.h'
    $deviceText = (Get-Content -LiteralPath $hdr -Raw) + "`n" +
        (Get-Content -LiteralPath $policyHdr -Raw)
    foreach ($value in @('approval', 'task_done', 'schedule', 've', 'system',
            'silent', 'soft', 'interrupt')) {
        if ($wireText -notlike "*`"$value`"*") {
            $failures += "wire contract no longer declares `"$value`""
        }
        if ($deviceText -notlike "*`"$value`"*") {
            $failures += "device headers do not declare the wire value `"$value`""
        }
    }
}

if (-not $SkipHostTest) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host event ingest test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_event_ingest.exe'
        $testC = Join-Path $PSScriptRoot 'host_tests\test_event_ingest.c'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host event ingest compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host event ingest test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("event ingest check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "event ingest check passed: closed-set category, approval audit contract, wire drift guard, host test $hostTestStatus"
exit 0
