# Mirror extensions/companion-paper into guiapp/extension_asset/companion-paper.
# The GUI binary embeds that copy, so an installed build can always unpack the
# extension folder even though extensions/ is not shipped next to the exe.
# TestCompanionExtensionAssetMatchesSource fails when the copy drifts.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root 'extensions\companion-paper'
$dest = Join-Path $root 'guiapp\extension_asset\companion-paper'

if (-not (Test-Path (Join-Path $source 'manifest.json'))) {
    throw "extension source missing: $source"
}

if (Test-Path $dest) {
    Remove-Item $dest -Recurse -Force
}
New-Item -ItemType Directory -Path $dest | Out-Null

Get-ChildItem -Path $source -Recurse -File | Where-Object {
    $_.Extension -ne '.mjs' -and $_.Name -notlike '*_test*'
} | ForEach-Object {
    $rel = $_.FullName.Substring($source.Length).TrimStart('\', '/')
    $target = Join-Path $dest $rel
    $parent = Split-Path -Parent $target
    if (-not (Test-Path $parent)) {
        New-Item -ItemType Directory -Path $parent | Out-Null
    }
    Copy-Item -LiteralPath $_.FullName -Destination $target -Force
}

Write-Host "Synced extensions/companion-paper -> guiapp/extension_asset/companion-paper"
