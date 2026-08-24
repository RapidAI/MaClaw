[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$source = Join-Path $projectRoot 'main\meeting_recording_storage.c'
$header = Join-Path $projectRoot 'main\meeting_recording_storage.h'
$test = Join-Path $PSScriptRoot 'host_tests\test_meeting_recording_storage.c'
$meetingService = Join-Path $projectRoot 'main\services\meeting_service.c'
$meetingHeader = Join-Path $projectRoot 'main\services\meeting_service.h'
$failures = @()

foreach ($path in @($source, $header, $test, $meetingService, $meetingHeader)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if (Test-Path -LiteralPath $header) {
    $text = Get-Content -LiteralPath $header -Raw
    foreach ($api in @('meeting_recording_storage_create',
                        'meeting_recording_storage_append_pcm',
                        'meeting_recording_storage_finalize',
                        'meeting_recording_storage_open_for_upload',
                        'meeting_recording_storage_read_range',
                        'meeting_recording_storage_has_pending_audio',
                        'meeting_recording_storage_clear')) {
        if ($text -notmatch ("\b$api\s*\(")) { $failures += "storage header missing $api" }
    }
    if ($text -cmatch '\b(?:esp_|freertos/|driver/|TaskHandle_t|SemaphoreHandle_t|heap_caps|FILE)\b') {
        $failures += 'meeting storage public contract leaked platform/RTOS/allocator/VFS detail'
    }
}
if (Test-Path -LiteralPath $meetingService) {
    $text = Get-Content -LiteralPath $meetingService -Raw
    if ($text -cmatch '\b(?:FILE|fopen|fclose|fread|fwrite|fseek|stat|unlink|MEETING_WAV_PATH)\b') {
        $failures += 'meeting business service still owns retained-WAV VFS/path detail'
    }
    if ($text -notmatch 'meeting_recording_storage_open_for_upload' -or
        $text -notmatch 'meeting_recording_storage_append_pcm') {
        $failures += 'meeting business service is not routed through the recording Storage adapter'
    }
    if ($text -notmatch 'meeting_recording_storage_clear\(\)' -or
        $text -notmatch 'return save_meeting_recovery\(false, "", 0, 0\);') {
        $failures += 'meeting terminal cleanup must retain recovery metadata until the retained WAV has been deleted'
    }
    $clearIndex = $text.IndexOf('meeting_recording_storage_clear()')
    $metadataClearIndex = $text.IndexOf('return save_meeting_recovery(false, "", 0, 0);', $clearIndex)
    if ($clearIndex -lt 0 -or $metadataClearIndex -lt $clearIndex) {
        $failures += 'meeting terminal cleanup must delete WAV before clearing durable recovery metadata'
    }
}
if (Test-Path -LiteralPath $meetingHeader) {
    $text = Get-Content -LiteralPath $meetingHeader -Raw
    if ($text -cmatch '\b(?:FILE|fopen|fread|fwrite|fseek|esp_|freertos/|driver/)\b') {
        $failures += 'meeting business contract leaked VFS/platform detail'
    }
}

$cc = Get-Command gcc -ErrorAction SilentlyContinue
if (-not $cc) { $cc = Get-Command clang -ErrorAction SilentlyContinue }
if (-not $cc) {
    $failures += 'host C compiler (gcc or clang) is required for meeting recording storage test'
} elseif ($failures.Count -eq 0) {
    $outDir = Join-Path $projectRoot 'build-host-tests'
    $storageDir = Join-Path $outDir 'meeting-recording'
    New-Item -ItemType Directory -Force -Path $storageDir | Out-Null
    $exe = Join-Path $outDir 'test_meeting_recording_storage.exe'
    & $cc.Source -std=c11 -Wall -Wextra -Werror '-DMEETING_RECORDING_STORAGE_HOST_TEST' `
        "-I$(Join-Path $projectRoot 'main')" $test $source -o $exe
    if ($LASTEXITCODE -ne 0) {
        $failures += "host meeting recording storage compile failed (exit $LASTEXITCODE)"
    } else {
        Push-Location $projectRoot
        try {
            & $exe
            if ($LASTEXITCODE -ne 0) {
                $failures += "host meeting recording storage test failed (exit $LASTEXITCODE)"
            }
        } finally {
            Pop-Location
        }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("meeting recording storage check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'meeting recording storage check passed: VFS/WAV integrity is behind a value-only Storage adapter and host tests pass'
