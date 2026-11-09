# Build the Chrome and Edge store package from the current extension source.
# The store assigns the extension id, so its manifest must not contain "key".
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root 'extensions\companion-paper'
$manifestPath = Join-Path $source 'manifest.json'
$manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
$version = [string]$manifest.version
if ([string]::IsNullOrWhiteSpace($version)) {
    throw "extension version missing: $manifestPath"
}
$zipPath = Join-Path $root "extensions\companion-paper-store-$version.zip"

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

$stream = [System.IO.File]::Open($zipPath, [System.IO.FileMode]::Create)
$archive = New-Object System.IO.Compression.ZipArchive($stream, [System.IO.Compression.ZipArchiveMode]::Create, $false)
try {
    Get-ChildItem -Path $source -Recurse -File | Where-Object {
        $_.Name -notlike '.*' -and $_.Extension -ne '.mjs' -and $_.Name -notlike '*_test*'
    } | Sort-Object FullName | ForEach-Object {
        $rel = $_.FullName.Substring($source.Length).TrimStart('\', '/').Replace('\', '/')
        $entry = $archive.CreateEntry($rel, [System.IO.Compression.CompressionLevel]::Optimal)
        $entry.LastWriteTime = [DateTimeOffset]::Parse('2026-01-01T00:00:00Z')
        if ($_.FullName -eq $manifestPath) {
            $doc = Get-Content $_.FullName -Raw | ConvertFrom-Json
            $doc.PSObject.Properties.Remove('key')
            $json = $doc | ConvertTo-Json -Depth 100 -Compress
            $bytes = [System.Text.Encoding]::UTF8.GetBytes($json + "`n")
        } else {
            $bytes = [System.IO.File]::ReadAllBytes($_.FullName)
        }
        $output = $entry.Open()
        try {
            $output.Write($bytes, 0, $bytes.Length)
        } finally {
            $output.Dispose()
        }
    }
} finally {
    $archive.Dispose()
    $stream.Dispose()
}

Write-Host "Packaged extensions/companion-paper -> extensions/companion-paper-store-$version.zip"
