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

$src = Join-Path $projectRoot 'main\services\latency_trace.c'
$hdr = Join-Path $projectRoot 'main\services\latency_trace.h'
if (-not (Test-Path -LiteralPath $src)) { $failures += 'missing services/latency_trace.c' }
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/latency_trace.h' }

# The public header is the host-tested surface: it must stay free of ESP-IDF and
# of any timer dependency, otherwise the milestone state machine stops being
# verifiable without hardware.
if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' 'public header must not include ESP-IDF'
    Assert-FileLacks $hdr '\besp_err_t\b' 'public header must not expose esp_err_t'
    Assert-FileLacks $hdr 'esp_timer' 'public header must not reach for the device clock'

    $hdrText = Get-Content -LiteralPath $hdr -Raw
    foreach ($required in @(
            'latency_trace_stamp',
            'latency_trace_offset_ms',
            'latency_trace_response_ms',
            'latency_trace_worth_logging',
            'latency_trace_format',
            'latency_trace_reset'
        )) {
        if ($hdrText -notlike "*$required*") {
            $failures += "latency_trace.h must expose the pure helper $required"
        }
    }
    # Every mark must be reachable from the public enum: a missing milestone
    # silently drops a latency segment from the log.
    foreach ($mark in @(
            'LATENCY_MARK_RELEASE', 'LATENCY_MARK_SENT', 'LATENCY_MARK_ACK',
            'LATENCY_MARK_TEXT', 'LATENCY_MARK_DONE', 'LATENCY_MARK_TTS',
            'LATENCY_MARK_DECODE', 'LATENCY_MARK_AUDIO'
        )) {
        if ($hdrText -notlike "*$mark*") { $failures += "latency_trace.h missing $mark" }
    }
}

# Only the device glue may touch the monotonic clock.
if (Test-Path -LiteralPath $src) {
    $srcText = Get-Content -LiteralPath $src -Raw
    if ($srcText -notlike '*esp_timer_get_time*') {
        $failures += 'latency_trace.c must stamp with esp_timer_get_time'
    }
    if ($srcText -notlike '*latency_trace_flush*') {
        $failures += 'latency_trace.c must implement the one-line flush'
    }
}

$cmake = Get-Content -LiteralPath (Join-Path $projectRoot 'main\CMakeLists.txt') -Raw
if ($cmake -notlike '*services/latency_trace.c*') {
    $failures += 'CMakeLists.txt missing services/latency_trace.c'
}

# The framework is only useful if it is actually wired into the turn.  Assert
# one producer per milestone so a future refactor cannot silently drop a
# segment of the latency line.
Assert-FileHas (Join-Path $projectRoot 'main\services\interaction_service.c') 'latency_trace_begin' `
    'turn start must open a latency turn'
Assert-FileHas (Join-Path $projectRoot 'main\services\interaction_service.c') 'LATENCY_MARK_RELEASE' `
    'capture completion must stamp the latency origin'
Assert-FileHas (Join-Path $projectRoot 'main\services\interaction_service.c') 'LATENCY_MARK_SENT' `
    'accepted uplink must stamp the sent milestone'
Assert-FileHas (Join-Path $projectRoot 'main\services\gateway_dispatcher.c') 'LATENCY_MARK_ACK' `
    'the first correlated reply frame must stamp the ack milestone'
Assert-FileHas (Join-Path $projectRoot 'main\services\gateway_dispatcher.c') 'LATENCY_MARK_TEXT' `
    'the terminal text reply must stamp the text milestone'
Assert-FileHas (Join-Path $projectRoot 'main\services\gateway_dispatcher.c') 'latency_close_turn' `
    'the dispatcher must close the turn so cancelled turns are dropped'
Assert-FileHas (Join-Path $projectRoot 'main\main.c') 'LATENCY_MARK_TTS' `
    'the server-audio composition root must stamp the first-audio-byte milestone'
Assert-FileHas (Join-Path $projectRoot 'main\mp3_player.c') 'LATENCY_MARK_DECODE' `
    'the decoder must stamp the decode milestone'
Assert-FileHas (Join-Path $projectRoot 'main\mp3_player.c') 'LATENCY_MARK_AUDIO' `
    'the renderer must stamp the rendered-frame milestone'

if (-not $SkipHostTest) {
    $cc = Get-Command gcc -ErrorAction SilentlyContinue
    if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
    if (-not $cc) {
        $hostTestStatus = 'skipped (no gcc/clang)'
        Write-Output 'host latency_trace test skipped: no gcc/clang on PATH'
    } else {
        $outDir = Join-Path $projectRoot 'build-host-tests'
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
        $exe = Join-Path $outDir 'test_latency_trace.exe'
        $testC = Join-Path $PSScriptRoot 'host_tests\test_latency_trace.c'
        $inc = Join-Path $projectRoot 'main'
        & $cc.Source -std=c11 -Wall -Wextra -Werror -I $inc $testC -o $exe
        if ($LASTEXITCODE -ne 0) {
            $failures += "host latency_trace compile failed (exit $LASTEXITCODE)"
            $hostTestStatus = 'compile-failed'
        } else {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host latency_trace test failed (exit $LASTEXITCODE)"
                $hostTestStatus = 'failed'
            } else {
                $hostTestStatus = 'PASS'
            }
        }
    }
}

if ($failures.Count -gt 0) {
    Write-Error ("latency trace check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "latency trace check passed: pure milestone state machine, one-line flush, host test $hostTestStatus"
exit 0
