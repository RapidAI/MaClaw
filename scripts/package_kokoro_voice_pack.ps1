param(
    [Parameter(Mandatory = $true)]
    [string]$VoiceDirectory,

    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

# Recommended OutputPath filename: kokoro_82m_selected_voices_koro_v2.zip.
# The v2 cache key prevents existing four-voice packs from masking this pack.

$ErrorActionPreference = 'Stop'
$voiceIds = @('zm_yunxi', 'zm_yunyang', 'zf_xiaoxiao', 'zf_xiaoyi', 'am_adam', 'af_heart')
$resolvedVoiceDirectory = (Resolve-Path -LiteralPath $VoiceDirectory).Path
$resolvedOutputDirectory = [System.IO.Path]::GetDirectoryName([System.IO.Path]::GetFullPath($OutputPath))

if (-not (Test-Path -LiteralPath $resolvedOutputDirectory -PathType Container)) {
    New-Item -ItemType Directory -Path $resolvedOutputDirectory | Out-Null
}

$sourceFiles = foreach ($voiceId in $voiceIds) {
    $path = Join-Path $resolvedVoiceDirectory ($voiceId + '.koro')
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing required Kokoro voice: $path"
    }
    Get-Item -LiteralPath $path
}

$resolvedOutputPath = [System.IO.Path]::GetFullPath($OutputPath)
if ($sourceFiles.FullName -contains $resolvedOutputPath) {
    throw 'Output ZIP cannot overwrite a source voice file.'
}

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$temporaryPath = $resolvedOutputPath + '.tmp'
if (Test-Path -LiteralPath $temporaryPath) {
    Remove-Item -LiteralPath $temporaryPath -Force
}

try {
    $stream = [System.IO.File]::Open($temporaryPath, [System.IO.FileMode]::CreateNew)
    try {
        $archive = [System.IO.Compression.ZipArchive]::new($stream, [System.IO.Compression.ZipArchiveMode]::Create, $false)
        try {
            foreach ($sourceFile in $sourceFiles) {
                [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                    $archive,
                    $sourceFile.FullName,
                    $sourceFile.Name,
                    [System.IO.Compression.CompressionLevel]::Optimal
                ) | Out-Null
            }
        } finally {
            $archive.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
    Move-Item -LiteralPath $temporaryPath -Destination $resolvedOutputPath -Force
} catch {
    if (Test-Path -LiteralPath $temporaryPath) {
        Remove-Item -LiteralPath $temporaryPath -Force
    }
    throw
}

Write-Output "Created $resolvedOutputPath with $($voiceIds.Count) voices: $($voiceIds -join ', ')"
