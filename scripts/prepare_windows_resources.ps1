param(
    [Parameter(Mandatory = $true)][string]$Root,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$AppName
)
$ErrorActionPreference = 'Stop'
$cfg = Get-Content (Join-Path $Root 'wails.json') -Raw | ConvertFrom-Json
$parts = $Version.Split('.')
if ($parts.Length -ne 4) { throw 'Version must contain 4 numeric parts for Windows resources.' }
$safeName = ($cfg.name -replace '[^a-zA-Z0-9._-]', '')
if (-not $safeName) { $safeName = $AppName }
$build = [Math]::Min([int]$parts[3], 65534)
$manifestVer = "$($parts[0]).$($parts[1]).$($parts[2]).$build"
$dir = Join-Path $Root 'build\windows'
$manifest = Get-Content (Join-Path $dir 'wails.exe.manifest') -Raw
$manifest = $manifest.Replace('{{.Name}}', $safeName).Replace('{{.Info.ProductVersion}}', $manifestVer)
[IO.File]::WriteAllText((Join-Path $dir 'wails.exe.manifest.tmp'), $manifest, [Text.UTF8Encoding]::new($false))
$info = @{ FixedFileInfo = @{ FileVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $build }; ProductVersion = @{ Major = [int]$parts[0]; Minor = [int]$parts[1]; Patch = [int]$parts[2]; Build = $build } }; StringFileInfo = @{ Comments = $cfg.info.comments; CompanyName = $cfg.info.companyName; FileDescription = $cfg.info.productName; FileVersion = $Version; InternalName = $cfg.info.productName; LegalCopyright = $cfg.info.copyright; OriginalFilename = "$AppName.exe"; ProductName = $cfg.info.productName; ProductVersion = $Version }; VarFileInfo = @{ Translation = @{ LangID = '0409'; CharsetID = '04B0' } } }
$json = $info | ConvertTo-Json -Depth 6
[IO.File]::WriteAllText((Join-Path $dir 'versioninfo.json.tmp'), $json, [Text.UTF8Encoding]::new($false))
