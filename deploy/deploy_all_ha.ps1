param(
    [ValidateSet('full', 'hubcenter-only', 'hub-only')]
    [string]$Scope = 'full',

    [ValidateSet('rapidai', 'tigerclaw')]
    [string]$Brand = 'rapidai',

    [string]$SkipTargets = '',

    [switch]$CleanHubCenterDB,

    [switch]$NoCheck
)

$ErrorActionPreference = 'Stop'

function Get-EnvOrDefault {
    param(
        [string]$Name,
        [string]$DefaultValue = ''
    )

    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $DefaultValue
    }
    return $value
}

function Get-TargetSetting {
    param(
        [string]$BaseName,
        [string]$NodeName,
        [string]$DefaultValue = ''
    )

    $suffix = ($NodeName -replace '[^A-Za-z0-9]', '_').ToUpperInvariant()
    $scoped = Get-EnvOrDefault ("{0}_{1}" -f $BaseName, $suffix) ''
    if (-not [string]::IsNullOrWhiteSpace($scoped)) {
        return $scoped
    }
    return Get-EnvOrDefault $BaseName $DefaultValue
}

function Require-Tool {
    param([string]$Name)

    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($null -ne $cmd) {
        return $cmd.Source
    }

    # PuTTY's installer does not always add its directory to PATH.  The
    # deployment entry point should still work with the standard installation.
    $knownToolPaths = @()
    if ($Name -in @('plink.exe', 'pscp.exe')) {
        $knownToolPaths = @(
            (Join-Path $env:ProgramFiles ("PuTTY\\{0}" -f $Name)),
            (Join-Path ${env:ProgramFiles(x86)} ("PuTTY\\{0}" -f $Name))
        )
    }
    foreach ($path in $knownToolPaths) {
        if (-not [string]::IsNullOrWhiteSpace($path) -and (Test-Path -LiteralPath $path -PathType Leaf)) {
            return $path
        }
    }
    throw "Required tool not found: $Name"
}

function Escape-Psd1String {
    param([string]$Value)
    return ($Value -replace "'", "''")
}

function Quote-ShellEnvValue {
    param([string]$Value)

    if ($null -eq $Value) {
        return "''"
    }
    return "'" + ($Value -replace "'", "'\''") + "'"
}

function Get-ExistingClusterSecret {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $Path)) {
        return ''
    }

    try {
        $data = Import-PowerShellDataFile -LiteralPath $Path
        $secret = [string]$data.ClusterSecret
        if ([string]::IsNullOrWhiteSpace($secret)) {
            return ''
        }
        return $secret.Trim()
    }
    catch {
        Write-Warning ("Failed to read existing cluster secret from {0}: {1}" -f $Path, $_.Exception.Message)
        return ''
    }
}
function New-ClusterSecret {
    $bytes = New-Object byte[] 48
    $rng = New-Object System.Security.Cryptography.RNGCryptoServiceProvider
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return [Convert]::ToBase64String($bytes).TrimEnd('=')
}

function Prompt-PasswordIfNeeded {
    param(
        [string]$PromptScript,
        [string]$PowerShellExe,
        [string]$HostLabel,
        [string]$PasswordFile
    )

    $existing = [Environment]::GetEnvironmentVariable('REMOTE_PASS')
    if (-not [string]::IsNullOrWhiteSpace($existing)) {
        return $existing
    }

    if (-not (Test-Path $PromptScript)) {
        throw "Missing password prompt helper: $PromptScript"
    }

    Write-Host "Please enter SSH password for $HostLabel." -ForegroundColor Yellow
    if (Test-Path $PasswordFile) {
        Remove-Item -LiteralPath $PasswordFile -Force -ErrorAction SilentlyContinue
    }

    & $PowerShellExe -NoProfile -ExecutionPolicy Bypass -File $PromptScript -Prompt 'Password' -OutputPath $PasswordFile
    if (-not (Test-Path $PasswordFile)) {
        throw 'Password input was cancelled.'
    }

    $password = (Get-Content -LiteralPath $PasswordFile -Raw).Trim()
    Remove-Item -LiteralPath $PasswordFile -Force -ErrorAction SilentlyContinue
    if ([string]::IsNullOrWhiteSpace($password)) {
        throw 'Password input was empty.'
    }
    return $password
}

function New-CleanDirectory {
    param([string]$Path)

    if (Test-Path $Path) {
        Remove-Item -LiteralPath $Path -Recurse -Force
    }
    New-Item -ItemType Directory -Path $Path -Force | Out-Null
}

function Assert-DeployFileExists {
    param(
        [string]$Path,
        [string]$Label
    )

    if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw ("Missing deploy payload: {0} ({1})" -f $Label, $Path)
    }
}

function Assert-DeployDirectoryHasFiles {
    param(
        [string]$Path,
        [string]$Label
    )

    if ([string]::IsNullOrWhiteSpace($Path) -or -not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw ("Missing deploy directory: {0} ({1})" -f $Label, $Path)
    }
    $file = Get-ChildItem -LiteralPath $Path -File -Recurse -Force | Select-Object -First 1
    if ($null -eq $file) {
        throw ("Deploy directory is empty: {0} ({1})" -f $Label, $Path)
    }
}

function Test-StageExcludedPath {
    param(
        [string]$RelativePath,
        [string[]]$ExcludePaths
    )

    if ([string]::IsNullOrWhiteSpace($RelativePath)) {
        return $false
    }

    $normalized = ($RelativePath -replace '/', '\').Trim('\')
    foreach ($exclude in $ExcludePaths) {
        if ([string]::IsNullOrWhiteSpace($exclude)) {
            continue
        }
        $excludeNormalized = ($exclude -replace '/', '\').Trim('\')
        if ($normalized.Equals($excludeNormalized, [StringComparison]::OrdinalIgnoreCase) -or
            $normalized.StartsWith($excludeNormalized + '\', [StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }
    return $false
}

function Copy-StageDirectory {
    param(
        [string]$SourceRoot,
        [string]$DestinationRoot,
        [string[]]$ExcludePaths = @(),
        [string[]]$ExcludeFilePatterns = @()
    )

    if (-not (Test-Path -LiteralPath $SourceRoot)) {
        return
    }

    New-Item -ItemType Directory -Path $DestinationRoot -Force | Out-Null
    $sourceFullPath = (Resolve-Path -LiteralPath $SourceRoot).Path.TrimEnd('\')

    function Copy-StageDirectoryChildren {
        param([string]$CurrentSource)

        Get-ChildItem -LiteralPath $CurrentSource -Force | ForEach-Object {
            $relative = $_.FullName.Substring($sourceFullPath.Length).TrimStart('\')
            if (Test-StageExcludedPath -RelativePath $relative -ExcludePaths $ExcludePaths) {
                return
            }

            $destination = Join-Path $DestinationRoot $relative
            if ($_.PSIsContainer) {
                New-Item -ItemType Directory -Path $destination -Force | Out-Null
                Copy-StageDirectoryChildren -CurrentSource $_.FullName
                return
            }

            foreach ($pattern in $ExcludeFilePatterns) {
                if ($_.Name -like $pattern) {
                    return
                }
            }

            $destinationParent = Split-Path -Parent $destination
            if (-not [string]::IsNullOrWhiteSpace($destinationParent)) {
                New-Item -ItemType Directory -Path $destinationParent -Force | Out-Null
            }
            Copy-Item -LiteralPath $_.FullName -Destination $destination -Force
        }
    }

    Copy-StageDirectoryChildren -CurrentSource $SourceRoot
}

function Stage-SourceTree {
    param(
        [string]$SourceRoot,
        [string]$StageRoot
    )

    $includeRapidSpeech = (Get-EnvOrDefault 'DEPLOY_INCLUDE_RAPIDSPEECH' '0') -eq '1'

    $copyDirs = @(
        [pscustomobject]@{
            Path = 'corelib'
            ExcludePaths = @('tts', 'yolo\weights', '.gocache', '.gomodcache', 'bin', 'package', 'data')
        },
        [pscustomobject]@{
            Path = 'hub'
            ExcludePaths = @('bin', 'package', 'data', '.gocache', '.gomodcache')
        },
        [pscustomobject]@{
            Path = 'hubcenter'
            ExcludePaths = @('bin', 'package', 'data', '.gocache', '.gomodcache')
        },
        [pscustomobject]@{
            Path = 'datasrv'
            ExcludePaths = @(
                '.gocache',
                '.gomodcache',
                '.tmp-go-cache-datasrv-bool-cmd',
                '.tmp-go-cache-datasrv-cmd-syncstate',
                '.tmp-go-tmp-datasrv-bool-cmd',
                '.tmp-go-tmp-datasrv-cmd-syncstate',
                'structureddata\.tmp-go-cache-datasrv-bool-review',
                'structureddata\.tmp-go-cache-datasrv-export-review',
                'structureddata\.tmp-go-cache-datasrv-review-syncstate',
                'structureddata\.tmp-go-tmp-datasrv-bool-review',
                'structureddata\.tmp-go-tmp-datasrv-export-review',
                'structureddata\.tmp-go-tmp-datasrv-review-syncstate',
                'bin',
                'package',
                'data'
            )
        },
        [pscustomobject]@{
            Path = 'openclaw-bridge'
            ExcludePaths = @('node_modules', 'dist')
        },
        [pscustomobject]@{
            Path = 'gui\internal\systray'
            ExcludePaths = @()
        }
    )

    if ($includeRapidSpeech) {
        $copyDirs += [pscustomobject]@{
            Path = 'RapidSpeech.cpp'
            ExcludePaths = @('build', 'models', '.git', 'ggml\.git', 'test', 'server', 'examples', 'assets')
        }
    }
    $copyFiles = @('go.mod', 'go.sum')

    foreach ($file in $copyFiles) {
        $src = Join-Path $SourceRoot $file
        if (Test-Path $src) {
            Copy-Item -LiteralPath $src -Destination $StageRoot -Force
        }
    }

    foreach ($dir in $copyDirs) {
        $src = Join-Path $SourceRoot $dir.Path
        if (Test-Path $src) {
            $dst = Join-Path $StageRoot $dir.Path
            $dstParent = Split-Path -Parent $dst
            if (-not [string]::IsNullOrWhiteSpace($dstParent)) {
                New-Item -ItemType Directory -Path $dstParent -Force | Out-Null
            }
            Copy-StageDirectory -SourceRoot $src -DestinationRoot $dst -ExcludePaths $dir.ExcludePaths -ExcludeFilePatterns @('*.exe', '*.exe~')
        }
    }

    if (-not $includeRapidSpeech) {
        Write-Host '  - RapidSpeech source skipped for fast deploy (set DEPLOY_INCLUDE_RAPIDSPEECH=1 to include it).'
    }
}

function Stage-DeployAssets {
    param(
        [string]$SourceRoot,
        [string]$StageRoot
    )

    $assetDirs = @(
        [pscustomobject]@{ Path = 'hubcenter'; ExcludePaths = @('bin', 'package', 'data', '.gocache', '.gomodcache', 'cmd', 'internal') },
        [pscustomobject]@{ Path = 'hub'; ExcludePaths = @('bin', 'package', 'data', '.gocache', '.gomodcache', 'cmd', 'internal') },
        [pscustomobject]@{ Path = 'openclaw-bridge'; ExcludePaths = @('node_modules', 'dist') }
    )

    foreach ($dir in $assetDirs) {
        $src = Join-Path $SourceRoot $dir.Path
        if (Test-Path $src) {
            $dst = Join-Path $StageRoot $dir.Path
            $dstParent = Split-Path -Parent $dst
            if (-not [string]::IsNullOrWhiteSpace($dstParent)) {
                New-Item -ItemType Directory -Path $dstParent -Force | Out-Null
            }
            Copy-StageDirectory -SourceRoot $src -DestinationRoot $dst -ExcludePaths $dir.ExcludePaths -ExcludeFilePatterns @('*.exe', '*.exe~')
        }
    }

    Assert-DeployDirectoryHasFiles -Path (Join-Path $StageRoot 'hubcenter\web\admin') -Label 'hubcenter admin web assets'
    Assert-DeployFileExists -Path (Join-Path $StageRoot 'hubcenter\web\admin\assets\js\admin-core.js') -Label 'hubcenter admin core script'
    Assert-DeployFileExists -Path (Join-Path $StageRoot 'hubcenter\web\admin\assets\js\problem-reports-tab.js') -Label 'hubcenter problem reports admin script'
    $hubCenterAdminIndex = Get-Content -LiteralPath (Join-Path $StageRoot 'hubcenter\web\admin\index.html') -Raw
    if ($hubCenterAdminIndex -notmatch '/admin/assets/js/problem-reports-tab\.js') {
        throw 'HubCenter admin page does not reference the problem reports script.'
    }
    $hubCenterProblemReportsScript = Get-Content -LiteralPath (Join-Path $StageRoot 'hubcenter\web\admin\assets\js\problem-reports-tab.js') -Raw
    if ($hubCenterProblemReportsScript -notmatch 'URL\.createObjectURL' -or $hubCenterProblemReportsScript -notmatch "Authorization: 'Bearer '") {
        throw 'HubCenter problem reports script is missing authenticated local attachment download support.'
    }
    Assert-DeployDirectoryHasFiles -Path (Join-Path $StageRoot 'hub\web\admin') -Label 'hub admin web assets'
    Assert-DeployDirectoryHasFiles -Path (Join-Path $StageRoot 'hub\web\dist') -Label 'hub pwa web dist'
    Assert-DeployDirectoryHasFiles -Path (Join-Path $StageRoot 'hub\web\card_store') -Label 'hub card store web assets'
    Assert-DeployFileExists -Path (Join-Path $StageRoot 'hub\web\card_store\index.html') -Label 'hub card store index'
    Assert-DeployFileExists -Path (Join-Path $StageRoot 'hub\web\card_store\professional.css') -Label 'hub card store stylesheet'

    # Keep a content manifest in the archive.  The remote script validates the
    # deployed web trees against it after copying them, so a partial/stale copy
    # can never be reported as a successful rollout.
    $manifestRoot = Join-Path $StageRoot 'deploy-manifest'
    New-Item -ItemType Directory -Path $manifestRoot -Force | Out-Null
    foreach ($webTree in @(
            [pscustomobject]@{ Name = 'hubcenter-web'; Root = (Join-Path $StageRoot 'hubcenter\web') },
            [pscustomobject]@{ Name = 'hub-web'; Root = (Join-Path $StageRoot 'hub\web') }
        )) {
        $manifestLines = @(
            Get-ChildItem -LiteralPath $webTree.Root -File -Recurse -Force |
                Sort-Object FullName |
                ForEach-Object {
                    $relative = $_.FullName.Substring($webTree.Root.Length).TrimStart('\\') -replace '\\', '/'
                    "{0}  {1}" -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $relative
                }
        )
        if ($manifestLines.Count -eq 0) {
            throw ("Cannot create empty deploy manifest for {0}" -f $webTree.Name)
        }
        # Force LF: GNU sha256sum treats CRLF as part of each filename.
        [System.IO.File]::WriteAllText((Join-Path $manifestRoot ($webTree.Name + '.sha256')), ($manifestLines -join "`n") + "`n", [System.Text.Encoding]::ASCII)
    }
}

function Get-HubCenterProblemReportsScriptSrc {
    param([string]$AdminIndexPath)

    $html = Get-Content -LiteralPath $AdminIndexPath -Raw
    $match = [regex]::Match($html, '<script\s+src="(?<src>/admin/assets/js/problem-reports-tab\.js[^\"]*)"')
    if (-not $match.Success) {
        throw "HubCenter admin page does not provide a problem reports script URL: $AdminIndexPath"
    }
    return $match.Groups['src'].Value
}

function Build-LocalBinaries {
    param(
        [string]$SourceRoot,
        [string]$OutputRoot,
        [string]$HubBinaryName,
        [string]$HubCenterBinaryName,
        [string]$MeetingASRWorkerBinaryName,
        [string]$BrandBuildTag,
        [bool]$BuildHub,
        [bool]$BuildHubCenter
    )

    $goExe = Require-Tool 'go.exe'
    $goos = Get-EnvOrDefault 'DEPLOY_GOOS' 'linux'
    $goarch = Get-EnvOrDefault 'DEPLOY_GOARCH' 'amd64'
    $cgo = Get-EnvOrDefault 'CGO_ENABLED' '0'
    $goproxyValue = Get-EnvOrDefault 'GOPROXY' 'https://goproxy.cn,direct'
    $goBuildParallelism = Get-EnvOrDefault 'DEPLOY_GO_BUILD_P' '1'
    $tags = $BrandBuildTag.Trim()
    $binDir = Join-Path $OutputRoot 'bin'
    $binDirForGo = (New-Item -ItemType Directory -Path $binDir -Force).FullName
    $goCacheRoot = Split-Path -Parent $OutputRoot
    $goCacheDir = (New-Item -ItemType Directory -Path (Join-Path $goCacheRoot '.gocache') -Force).FullName

    function Quote-CmdArg {
        param([string]$Value)
        return '"' + ($Value -replace '"', '\"') + '"'
    }

    function Invoke-GoBuildCmd {
        param(
            [string]$OutputPath,
            [string]$PackagePath,
            [string]$Label
        )

        $buildArgs = @('build', '-p', $goBuildParallelism)
        if (-not [string]::IsNullOrWhiteSpace($tags)) {
            $buildArgs += @('-tags', (Quote-CmdArg $tags))
        }
        $buildArgs += @('-o', (Quote-CmdArg $OutputPath), $PackagePath)
        $commandParts = @(
            ('set "GOOS={0}"' -f $goos),
            ('set "GOARCH={0}"' -f $goarch),
            ('set "CGO_ENABLED={0}"' -f $cgo),
            ('set "GOPROXY={0}"' -f $goproxyValue),
            ('set "GOCACHE={0}"' -f $goCacheDir),
            ('cd /d {0}' -f (Quote-CmdArg $SourceRoot)),
            ((Quote-CmdArg $goExe) + ' ' + ($buildArgs -join ' '))
        )
        $cmdText = $commandParts -join ' && '
        $maxAttempts = [int](Get-EnvOrDefault 'DEPLOY_GO_BUILD_RETRIES' '3')
        for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
            if ($attempt -gt 1) {
                Write-Host ("  - retrying {0} build ({1}/{2})..." -f $Label, $attempt, $maxAttempts) -ForegroundColor Yellow
                Start-Sleep -Seconds 3
            }
            & cmd.exe /d /c $cmdText
            if ($LASTEXITCODE -eq 0) {
                return
            }
        }
        throw ("Local {0} build failed." -f $Label)
    }

    Write-Host ("  - target: {0}/{1}, CGO_ENABLED={2}" -f $goos, $goarch, $cgo)
    if ($BuildHubCenter) {
        Write-Host '  - building hubcenter locally...'
        Invoke-GoBuildCmd -OutputPath (Join-Path $binDirForGo $HubCenterBinaryName) -PackagePath './hubcenter/cmd/hubcenter' -Label 'hubcenter'
    }

    if ($BuildHub) {
        Write-Host '  - building hub locally...'
        Invoke-GoBuildCmd -OutputPath (Join-Path $binDirForGo $HubBinaryName) -PackagePath './hub/cmd/hub' -Label 'hub'
        Write-Host '  - building mobile meeting ASR worker locally...'
        Invoke-GoBuildCmd -OutputPath (Join-Path $binDirForGo $MeetingASRWorkerBinaryName) -PackagePath './hub/cmd/meeting_asr_worker' -Label 'mobile meeting ASR worker'
    }
}
function Write-InventoryFile {
    param(
        [string]$Path,
        [string]$ClusterSecret
    )

    $secret = Escape-Psd1String $ClusterSecret
    $content = @"
@{
    ClusterSecret = '$secret'

    HubCenters = @(
        @{
            NodeID        = 'hc-1'
            NodeName      = 'hubcenter-1'
            PublicBaseURL = 'https://hubs.mypapers.top'
            AdvertiseURL  = 'http://hub.mypapers.top:9388'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        },
        @{
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hubs.maclaw.top'
            AdvertiseURL  = 'http://107.172.86.131:9388'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        },
        @{
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hubs2.maclaw.top'
            AdvertiseURL  = 'http://66.154.113.63:9388'
            DatabaseDSN   = './data/codeclaw-hubcenter.db'
        }
    )

    Hubs = @(
        @{
            FileName              = 'hub-mypapers.yaml'
            PublicBaseURL         = 'https://hub.mypapers.top'
            PrimaryCenterBaseURL  = 'https://hubs.mypapers.top'
            CenterBaseURLs        = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = 'rapidai.tech'
            CorporateEmailDomains = @('rapidai.tech', 'qianxin.com')
            AcceptPublicSignup    = `$false
        },
        @{
            FileName              = 'hub-maclaw.yaml'
            PublicBaseURL         = 'https://hub.maclaw.top'
            PrimaryCenterBaseURL  = 'https://hubs.maclaw.top'
            CenterBaseURLs        = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = ''
            CorporateEmailDomains = @()
            AcceptPublicSignup    = `$true
        },
        @{
            FileName              = 'hub2-maclaw.yaml'
            PublicBaseURL         = 'https://hub2.maclaw.top'
            PrimaryCenterBaseURL  = 'https://hubs2.maclaw.top'
            CenterBaseURLs        = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN           = './data/codeclaw-hub.db'
            Visibility            = 'shared'
            CorporateEmailDomain  = ''
            CorporateEmailDomains = @()
            AcceptPublicSignup    = `$true
        }
    )
}
"@
    Set-Content -LiteralPath $Path -Value $content -Encoding UTF8
}

function Write-RemoteScript {
    param([string]$Path)

    $lines = @(
        '#!/bin/sh',
        'set -eu',
        '',
        ': "${REMOTE_TMP_DIR:=/tmp/aicoder_deploy}"',
        ': "${REMOTE_HUB_DIR:=/data/soft/hub}"',
        ': "${REMOTE_HUBCENTER_DIR:=/data/soft/hubcenter}"',
        ': "${DEPLOY_HUBCENTER:=1}"',
        ': "${DEPLOY_HUB:=0}"',
        ': "${ENSURE_HUB_MODELS:=0}"',
        ': "${HUB_MODEL_BASE_URL:=https://github.com/RapidAI/MaClaw/releases/download/Model_Release}"',
        ': "${HUB_MODEL_FILES:=embeddinggemma-300M-Q8_0.gguf sensevoice-small-q8.gguf omniparser-v2.yolow kokoro-v1_0.koro kokoro_82m_selected_voices_koro.zip}"',
        ': "${HUB_CONFIG_BASENAME:=}"',
        ': "${HUBCENTER_CONFIG_BASENAME:=hubcenter-config.yaml}"',
        ': "${HUB_BINARY_NAME:=maclaw-hub}"',
        ': "${HUBCENTER_BINARY_NAME:=maclaw-hubcenter}"',
        ': "${MEETING_ASR_WORKER_BINARY_NAME:=meeting_asr_worker}"',
        ': "${CLEAN_HUBCENTER_DB:=0}"',
        ': "${HUBCENTER_DB_PATH:=./data/codeclaw-hubcenter.db}"',
        '',
        'PATH="$PATH:/usr/local/go/bin:/root/go/bin"',
        'export PATH',
        '',
        'SRC_ROOT="$REMOTE_TMP_DIR/src"',
        'ARCHIVE_PATH="$REMOTE_TMP_DIR/maclaw-deploy.tar.gz"',
        'HUB_DATA_DIR="$REMOTE_HUB_DIR/data"',
        'HUB_MODELS_DIR="$HUB_DATA_DIR/models"',
        'HOME_MODELS_DIR="$HOME/.maclaw/models"',
        'MODEL_SENTINEL="$HUB_MODELS_DIR/.models-initialized"',
        'MODEL_LOCK="$HUB_MODELS_DIR/.models-downloading"',
        'MODEL_SCRIPT="$HUB_DATA_DIR/download-models.sh"',
        'MODEL_LOG="$HUB_DATA_DIR/logs/model-download.log"',
        '',
        'rm -rf "$SRC_ROOT"',
        'mkdir -p "$SRC_ROOT"',
        'tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"',
        'cd "$SRC_ROOT"',
        '',
        'echo "[remote] Using uploaded local binaries."',
        '',
        'backup_and_write_config() {',
        '  target_path="$1"',
        '  source_path="$2"',
        '  if [ ! -f "$source_path" ]; then',
        '    echo "[ERROR] Missing config payload: $source_path" >&2',
        '    exit 1',
        '  fi',
        '  mkdir -p "$(dirname "$target_path")"',
        '  if [ -f "$target_path" ]; then',
        '    cp -f "$target_path" "$target_path.bak"',
        '  fi',
        '  cp -f "$source_path" "$target_path"',
        '}',
        '',
        'verify_web_manifest() {',
        '  verify_target_dir="$1"',
        '  verify_manifest="$2"',
        '  verify_label="$3"',
        '  if ! command -v sha256sum >/dev/null 2>&1; then',
        '    echo "[ERROR] sha256sum is required to verify $verify_label assets" >&2',
        '    exit 1',
        '  fi',
        '  if [ ! -f "$verify_manifest" ]; then',
        '    echo "[ERROR] Missing $verify_label asset manifest: $verify_manifest" >&2',
        '    exit 1',
        '  fi',
        '  echo "[remote] Verifying $verify_label static assets..."',
        '  (cd "$verify_target_dir" && sha256sum -c "$verify_manifest")',
        '}',
        '',
        'replace_web_tree() {',
        '  source_dir="$1"',
        '  target_dir="$2"',
        '  manifest="$3"',
        '  label="$4"',
        '  parent_dir=$(dirname "$target_dir")',
        '  deploy_nonce=$(date +%s).$$',
        '  staging_dir="${target_dir}.deploying.${deploy_nonce}"',
        '  if [ ! -d "$source_dir" ]; then',
        '    echo "[ERROR] Missing $label source assets: $source_dir" >&2',
        '    exit 1',
        '  fi',
        '  # Stage beside the live directory: the final rename is atomic and',
        '  # a failed copy leaves the currently served files untouched.',
        '  mkdir -p "$parent_dir"',
        '  rm -rf "$staging_dir"',
        '  mkdir -p "$staging_dir"',
        '  cp -R "$source_dir"/. "$staging_dir"/',
        '  verify_web_manifest "$staging_dir" "$manifest" "$label"',
        '  rm -rf "$target_dir"',
        '  mv "$staging_dir" "$target_dir"',
        '}',
        '',
        'stop_hubcenter_process() {',
		'  if command -v systemctl >/dev/null 2>&1 && systemctl cat "$HUBCENTER_BINARY_NAME.service" >/dev/null 2>&1; then',
		'    echo "[remote] Stopping systemd service: $HUBCENTER_BINARY_NAME.service"',
		'    systemctl stop "$HUBCENTER_BINARY_NAME.service"',
		'  fi',
        '  pid_file="$REMOTE_HUBCENTER_DIR/data/$HUBCENTER_BINARY_NAME.pid"',
        '  legacy_pid_file="$REMOTE_HUBCENTER_DIR/data/maclaw-hubcenter.pid"',
        '  for file in "$pid_file" "$legacy_pid_file"; do',
        '    if [ -f "$file" ]; then',
        '      old_pid=$(cat "$file" 2>/dev/null || true)',
        '      if [ -n "${old_pid:-}" ] && kill -0 "$old_pid" 2>/dev/null; then',
        '        echo "[remote] Stopping hubcenter process before DB rebuild: $old_pid"',
        '        kill "$old_pid" 2>/dev/null || true',
        '        sleep 2',
        '        if kill -0 "$old_pid" 2>/dev/null; then',
        '          kill -9 "$old_pid" 2>/dev/null || true',
        '        fi',
        '      fi',
        '      rm -f "$file"',
        '    fi',
        '  done',
        '  ps -eo pid=,args= | awk -v cmd="$REMOTE_HUBCENTER_DIR/$HUBCENTER_BINARY_NAME" ''$2 == cmd { print $1 }'' | while read -r pid; do',
        '    if [ -n "${pid:-}" ]; then',
        '      echo "[remote] Stopping stale hubcenter process before DB rebuild: $pid"',
        '      kill "$pid" 2>/dev/null || true',
        '      sleep 1',
        '      if kill -0 "$pid" 2>/dev/null; then',
        '        kill -9 "$pid" 2>/dev/null || true',
        '      fi',
        '    fi',
        '  done',
        '  ps -eo pid=,args= | awk -v dir="$REMOTE_HUBCENTER_DIR/" ''index($0, dir) && ($0 ~ /maclaw-hubcenter/ || $0 ~ /tigerclaw-hubcenter/) { print $1 }'' | while read -r pid; do',
        '    if [ -n "${pid:-}" ] && [ "$pid" != "$$" ]; then',
        '      echo "[remote] Stopping hubcenter process from deploy dir before update: $pid"',
        '      kill "$pid" 2>/dev/null || true',
        '      sleep 1',
        '      if kill -0 "$pid" 2>/dev/null; then',
        '        kill -9 "$pid" 2>/dev/null || true',
        '      fi',
        '    fi',
        '  done',
        '  ps -eo pid=,args= | awk ''$0 ~ /(^|[\/ ])(maclaw-hubcenter|tigerclaw-hubcenter)( |$)/ { print $1 }'' | while read -r pid; do',
        '    if [ -n "${pid:-}" ] && [ "$pid" != "$$" ]; then',
        '      echo "[remote] Stopping hubcenter process by binary name before update: $pid"',
        '      kill "$pid" 2>/dev/null || true',
        '      sleep 1',
        '      if kill -0 "$pid" 2>/dev/null; then',
        '        kill -9 "$pid" 2>/dev/null || true',
        '      fi',
        '    fi',
        '  done',
        '}',
        '',
        'clean_hubcenter_db_if_requested() {',
        '  if [ "$CLEAN_HUBCENTER_DB" != "1" ]; then',
        '    return 0',
        '  fi',
        '  if ! command -v sqlite3 >/dev/null 2>&1; then',
        '    echo "[ERROR] sqlite3 is required for --clean-hubcenter-db" >&2',
        '    exit 1',
        '  fi',
        '  if [ -z "$HUBCENTER_DB_PATH" ]; then',
        '    echo "[ERROR] HUBCENTER_DB_PATH is empty" >&2',
        '    exit 1',
        '  fi',
        '  case "$HUBCENTER_DB_PATH" in',
        '    /*) db_path="$HUBCENTER_DB_PATH" ;;',
        '    *) db_path="$REMOTE_HUBCENTER_DIR/$HUBCENTER_DB_PATH" ;;',
        '  esac',
        '  if [ ! -f "$db_path" ]; then',
        '    echo "[remote] HubCenter DB not found, skip rebuild: $db_path"',
        '    return 0',
        '  fi',
        '  db_dir=$(dirname "$db_path")',
        '  ts=$(date +%Y%m%d_%H%M%S)',
        '  backup_path="$db_path.bak.$ts"',
        '  dump_path="$db_dir/hubcenter_clean.$ts.sql"',
        '  new_path="$db_path.clean.$ts"',
        '  echo "[remote] Rebuilding HubCenter DB without HA history: $db_path"',
        '  stop_hubcenter_process',
        '  cp -f "$db_path" "$backup_path"',
        '  sqlite3 "$db_path" ''.dump'' | grep -v -E ''ha_sync_ops|ha_applied_ops'' > "$dump_path"',
        '  sqlite3 "$new_path" < "$dump_path"',
        '  mv -f "$db_path" "$db_path.old.$ts"',
        '  mv -f "$new_path" "$db_path"',
        '  rm -f "$dump_path" "$db_path-shm" "$db_path-wal"',
        '  echo "[remote] HubCenter DB rebuilt. Backup: $backup_path"',
        '}',
        '',
        'is_allowed_model_file() {',
        '  case "$1" in',
        '    ""|*/*|*\\*|*..*) return 1 ;;',
        '    *.gguf|*.yolow|*.koro|*.zip) return 0 ;;',
        '    *) return 1 ;;',
        '  esac',
        '}',
        '',
        'seed_home_models() {',
        '  mkdir -p "$HOME_MODELS_DIR"',
        '  for name in $HUB_MODEL_FILES; do',
        '    if ! is_allowed_model_file "$name"; then',
        '      echo "[remote] Skip unsafe model filename: $name" >&2',
        '      continue',
        '    fi',
        '    src="$HUB_MODELS_DIR/$name"',
        '    if [ -f "$src" ] && [ ! -f "$HOME_MODELS_DIR/$name" ]; then',
        '      cp -f "$src" "$HOME_MODELS_DIR/$name"',
        '    fi',
        '  done',
        '}',
        '',
        'write_model_download_script() {',
        '  mkdir -p "$HUB_MODELS_DIR" "$HUB_DATA_DIR/logs"',
        '  cat > "$MODEL_SCRIPT" <<''MODELEOF''',
        '#!/bin/sh',
        '# maclaw-model-download-v2',
        'set -eu',
        'BASE_URL="$1"',
        'TARGET_DIR="$2"',
        'HOME_DIR="$3"',
        'SENTINEL="$4"',
        'LOCK_FILE="$5"',
        'shift 5',
        'mkdir -p "$TARGET_DIR" "$HOME_DIR"',
        'cleanup() { rm -f "$LOCK_FILE"; }',
        'trap cleanup EXIT INT TERM',
        'is_allowed_model_file() {',
        '  case "$1" in',
        '    ""|*/*|*\\*|*..*) return 1 ;;',
        '    *.gguf|*.yolow|*.koro|*.zip) return 0 ;;',
        '    *) return 1 ;;',
        '  esac',
        '}',
        'download_one() {',
        '  name="$1"',
        '  url="$BASE_URL/$name"',
        '  target="$TARGET_DIR/$name"',
        '  tmp="$target.part"',
        '  if [ -f "$target" ]; then',
        '    cp -f "$target" "$HOME_DIR/$name"',
        '    return 0',
        '  fi',
        '  rm -f "$tmp"',
        '  if command -v curl >/dev/null 2>&1; then',
        '    if ! curl -L --fail --retry 3 --retry-delay 2 --connect-timeout 15 -o "$tmp" "$url"; then',
        '      rm -f "$tmp"',
        '      return 1',
        '    fi',
        '  elif command -v wget >/dev/null 2>&1; then',
        '    if ! wget --tries=3 --timeout=30 -O "$tmp" "$url"; then',
        '      rm -f "$tmp"',
        '      return 1',
        '    fi',
        '  else',
        '    echo "[ERROR] Neither curl nor wget is available" >&2',
        '    exit 1',
        '  fi',
        '  mv -f "$tmp" "$target"',
        '  cp -f "$target" "$HOME_DIR/$name"',
        '}',
        'for file in "$@"; do',
        '  if ! is_allowed_model_file "$file"; then',
        '    echo "[WARN] Skip unsafe model filename: $file" >&2',
        '    continue',
        '  fi',
        '  download_one "$file"',
        'done',
        'touch "$SENTINEL"',
        'MODELEOF',
        '  chmod +x "$MODEL_SCRIPT"',
        '}',
        '',
        'hub_models_missing() {',
        '  valid_model_count=0',
        '  for name in $HUB_MODEL_FILES; do',
        '    if ! is_allowed_model_file "$name"; then',
        '      echo "[remote] Skip unsafe model filename: $name" >&2',
        '      continue',
        '    fi',
        '    valid_model_count=$((valid_model_count + 1))',
        '    if [ ! -f "$HUB_MODELS_DIR/$name" ]; then',
        '      return 0',
        '    fi',
        '  done',
        '  if [ "$valid_model_count" -eq 0 ]; then',
        '    return 0',
        '  fi',
        '  return 1',
        '}',
        '',
        'ensure_hub_models() {',
        '  mkdir -p "$HUB_MODELS_DIR" "$HUB_DATA_DIR/logs"',
        '  if ! hub_models_missing; then',
        '    touch "$MODEL_SENTINEL"',
        '    seed_home_models',
        '    return 0',
        '  fi',
        '  rm -f "$MODEL_SENTINEL"',
        '  filtered_model_files=""',
        '  for name in $HUB_MODEL_FILES; do',
        '    if is_allowed_model_file "$name"; then',
        '      filtered_model_files="$filtered_model_files $name"',
        '    else',
        '      echo "[remote] Skip unsafe model filename: $name" >&2',
        '    fi',
        '  done',
        '  filtered_model_files=$(printf "%s" "$filtered_model_files" | xargs)',
        '  if [ -z "$filtered_model_files" ]; then',
        '    echo "[remote] No valid hub model files configured." >&2',
        '    return 1',
        '  fi',
        '  if [ -f "$MODEL_LOCK" ]; then',
        '    echo "[remote] Hub model download already in progress."',
        '    return 0',
        '  fi',
        '  write_model_download_script',
        '  touch "$MODEL_LOCK"',
        '  nohup "$MODEL_SCRIPT" "$HUB_MODEL_BASE_URL" "$HUB_MODELS_DIR" "$HOME_MODELS_DIR" "$MODEL_SENTINEL" "$MODEL_LOCK" $filtered_model_files >> "$MODEL_LOG" 2>&1 &',
        '  echo "[remote] Hub model download started in background: $MODEL_LOG"',
        '}',
        '',
        'deploy_hubcenter() {',
        '  mkdir -p "$REMOTE_HUBCENTER_DIR" "$REMOTE_HUBCENTER_DIR/configs" "$REMOTE_HUBCENTER_DIR/data" "$REMOTE_HUBCENTER_DIR/data/logs"',
        '  if [ ! -f "$SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" ]; then',
        '    echo "[ERROR] Missing hubcenter binary: $SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" >&2',
        '    exit 1',
        '  fi',
        '  stop_hubcenter_process',
        '  cp -f "$SRC_ROOT/bin/$HUBCENTER_BINARY_NAME" "$REMOTE_HUBCENTER_DIR/$HUBCENTER_BINARY_NAME"',
        '  chmod +x "$REMOTE_HUBCENTER_DIR/$HUBCENTER_BINARY_NAME"',
        '  if [ -f "$SRC_ROOT/hubcenter/start.sh" ]; then',
        '    cp -f "$SRC_ROOT/hubcenter/start.sh" "$REMOTE_HUBCENTER_DIR/start.sh"',
        '    sed -i ''s/\r$//'' "$REMOTE_HUBCENTER_DIR/start.sh"',
        '    sed -i "s/maclaw-hubcenter/$HUBCENTER_BINARY_NAME/g" "$REMOTE_HUBCENTER_DIR/start.sh"',
        '    chmod +x "$REMOTE_HUBCENTER_DIR/start.sh"',
        '  fi',
        '  if [ -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" ]; then',
        '    cp -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" "$REMOTE_HUBCENTER_DIR/configs/config.example.yaml"',
        '  fi',
        '  replace_web_tree "$SRC_ROOT/hubcenter/web" "$REMOTE_HUBCENTER_DIR/web" "$SRC_ROOT/deploy-manifest/hubcenter-web.sha256" "hubcenter"',
        '  backup_and_write_config "$REMOTE_HUBCENTER_DIR/configs/config.yaml" "$REMOTE_TMP_DIR/$HUBCENTER_CONFIG_BASENAME"',
        '}',
        '',
        'deploy_hub() {',
        '  mkdir -p "$REMOTE_HUB_DIR" "$REMOTE_HUB_DIR/configs" "$REMOTE_HUB_DIR/data" "$REMOTE_HUB_DIR/data/logs"',
        '  if [ ! -f "$SRC_ROOT/bin/$HUB_BINARY_NAME" ]; then',
        '    echo "[ERROR] Missing hub binary: $SRC_ROOT/bin/$HUB_BINARY_NAME" >&2',
        '    exit 1',
        '  fi',
        '  cp -f "$SRC_ROOT/bin/$HUB_BINARY_NAME" "$REMOTE_HUB_DIR/$HUB_BINARY_NAME"',
        '  chmod +x "$REMOTE_HUB_DIR/$HUB_BINARY_NAME"',
        '  if [ ! -f "$SRC_ROOT/bin/$MEETING_ASR_WORKER_BINARY_NAME" ]; then',
        '    echo "[ERROR] Missing mobile meeting ASR worker: $SRC_ROOT/bin/$MEETING_ASR_WORKER_BINARY_NAME" >&2',
        '    exit 1',
        '  fi',
        '  cp -f "$SRC_ROOT/bin/$MEETING_ASR_WORKER_BINARY_NAME" "$REMOTE_HUB_DIR/meeting_asr_worker"',
        '  chmod +x "$REMOTE_HUB_DIR/meeting_asr_worker"',
        '  if [ -f "$SRC_ROOT/hub/start.sh" ]; then',
        '    cp -f "$SRC_ROOT/hub/start.sh" "$REMOTE_HUB_DIR/start.sh"',
        '    sed -i ''s/\r$//'' "$REMOTE_HUB_DIR/start.sh"',
        '    sed -i "s/maclaw-hub/$HUB_BINARY_NAME/g" "$REMOTE_HUB_DIR/start.sh"',
        '    chmod +x "$REMOTE_HUB_DIR/start.sh"',
        '  fi',
        '  if [ -f "$SRC_ROOT/hub/configs/config.example.yaml" ]; then',
        '    cp -f "$SRC_ROOT/hub/configs/config.example.yaml" "$REMOTE_HUB_DIR/configs/config.example.yaml"',
        '  fi',
        '  replace_web_tree "$SRC_ROOT/hub/web" "$REMOTE_HUB_DIR/web" "$SRC_ROOT/deploy-manifest/hub-web.sha256" "hub"',
        '  backup_and_write_config "$REMOTE_HUB_DIR/configs/config.yaml" "$REMOTE_TMP_DIR/$HUB_CONFIG_BASENAME"',
        '  BRIDGE_SRC="$SRC_ROOT/openclaw-bridge"',
        '  BRIDGE_DST="$REMOTE_HUB_DIR/openclaw-bridge"',
        '  if [ -d "$BRIDGE_SRC" ] && [ -f "$BRIDGE_SRC/package.json" ]; then',
        '    echo "[remote] Deploying openclaw-bridge..."',
        '    mkdir -p "$BRIDGE_DST"',
        '    cp -f "$BRIDGE_SRC/package.json" "$BRIDGE_DST/package.json"',
        '    cp -f "$BRIDGE_SRC/tsconfig.json" "$BRIDGE_DST/tsconfig.json" 2>/dev/null || true',
        '    rm -rf "$BRIDGE_DST/src" "$BRIDGE_DST/dist"',
        '    cp -Rf "$BRIDGE_SRC/src" "$BRIDGE_DST/src"',
        '    if [ -f "$BRIDGE_SRC/config.example.json" ]; then',
        '      cp -f "$BRIDGE_SRC/config.example.json" "$BRIDGE_DST/config.example.json"',
        '    fi',
        '    if command -v npm >/dev/null 2>&1; then',
        '      echo "[remote] Running npm install in openclaw-bridge..."',
        '      cd "$BRIDGE_DST" && npm install 2>&1 || echo "[WARN] npm install failed for openclaw-bridge"',
        '      echo "[remote] Building openclaw-bridge..."',
        '      npx tsc 2>&1 || echo "[WARN] tsc build failed for openclaw-bridge"',
        '      echo "[remote] Pruning dev dependencies..."',
        '      npm prune --production 2>&1 || true',
        '      cd "$SRC_ROOT"',
        '    else',
        '      echo "[WARN] npm not found on remote host, skipping openclaw-bridge dependencies"',
        '    fi',
        '  fi',
        '}',
        '',
        'if [ "$DEPLOY_HUBCENTER" = "1" ]; then',
        '  echo "[remote] Deploying hubcenter files..."',
        '  deploy_hubcenter',
        '  clean_hubcenter_db_if_requested',
        'fi',
        '',
        'if [ "$DEPLOY_HUB" = "1" ]; then',
        '  echo "[remote] Deploying hub files..."',
        '  deploy_hub',
        '  if [ "$ENSURE_HUB_MODELS" = "1" ]; then',
        '    ensure_hub_models',
        '  fi',
        'fi',
        '',
        'if [ "$DEPLOY_HUBCENTER" = "1" ]; then',
        '  echo "[remote] Restarting hubcenter..."',
        '  if [ -x "$REMOTE_HUBCENTER_DIR/start.sh" ]; then',
        '    cd "$REMOTE_HUBCENTER_DIR"',
        '    ./start.sh',
        '  fi',
        'fi',
        '',
        'if [ "$DEPLOY_HUB" = "1" ]; then',
        '  echo "[remote] Restarting hub..."',
        '  if [ -x "$REMOTE_HUB_DIR/start.sh" ]; then',
        '    cd "$REMOTE_HUB_DIR"',
        '    ./start.sh',
        '  fi',
        'fi',
        '',
        'rm -rf "$SRC_ROOT"',
        'rm -f "$ARCHIVE_PATH" "$REMOTE_TMP_DIR/remote_deploy.sh"',
        'if [ -n "$HUBCENTER_CONFIG_BASENAME" ]; then',
        '  rm -f "$REMOTE_TMP_DIR/$HUBCENTER_CONFIG_BASENAME"',
        'fi',
        'if [ -n "$HUB_CONFIG_BASENAME" ]; then',
        '  rm -f "$REMOTE_TMP_DIR/$HUB_CONFIG_BASENAME"',
        'fi',
        '',
        'echo "Remote build and deploy finished."'
    )
    Set-Content -LiteralPath $Path -Value ($lines -join "`r`n") -Encoding ASCII
}

function Get-ConnectionArgs {
    param(
        [string]$UserName,
        [string]$HostName,
        [int]$Port,
        [string]$Password,
        [string]$HostKey
    )

    $connArgs = @('-batch')
    if (-not [string]::IsNullOrWhiteSpace($HostKey)) {
        $connArgs += @('-hostkey', $HostKey)
    }
    $connArgs += @('-P', [string]$Port, '-pw', $Password, "$UserName@$HostName")
    return ,$connArgs
}

function Invoke-Plink {
    param(
        [string]$PlinkExe,
        [string[]]$ConnectionArgs,
        [string]$CommandText
    )

    & $PlinkExe @ConnectionArgs $CommandText
    if ($LASTEXITCODE -ne 0) {
        throw "Remote command failed: $CommandText"
    }
}

function Invoke-PlinkCapture {
    param(
        [string]$PlinkExe,
        [string[]]$ConnectionArgs,
        [string]$CommandText
    )

    $output = & $PlinkExe @ConnectionArgs $CommandText
    if ($LASTEXITCODE -ne 0) {
        throw "Remote command failed: $CommandText"
    }
    return ($output | Out-String).Trim()
}

function Invoke-PscpUpload {
    param(
        [string]$PscpExe,
        [string[]]$ConnectionArgs,
        [string]$LocalPath,
        [string]$RemotePath
    )

    $scpArgs = $ConnectionArgs[0..($ConnectionArgs.Length - 2)]
    $target = $ConnectionArgs[-1]
    & $PscpExe @scpArgs $LocalPath "$($target):$RemotePath"
    if ($LASTEXITCODE -ne 0) {
        throw "Upload failed: $LocalPath -> $RemotePath"
    }
}

function Invoke-RemoteNginxHubProxyFix {
    param(
        [string]$PlinkExe,
        [string[]]$ConnectionArgs,
        [pscustomobject]$Target
    )

    if (-not $Target.DeployHub) {
        return
    }
    if (-not ($Target.PSObject.Properties.Name -contains 'NginxHubProxyPort')) {
        return
    }

    $configPath = if ($Target.PSObject.Properties.Name -contains 'NginxConfigPath') { [string]$Target.NginxConfigPath } else { '' }
    $serverName = if ($Target.PSObject.Properties.Name -contains 'NginxHubServerName') { [string]$Target.NginxHubServerName } else { '' }
    $proxyPort = [string]$Target.NginxHubProxyPort
    if ([string]::IsNullOrWhiteSpace($configPath) -or [string]::IsNullOrWhiteSpace($serverName) -or [string]::IsNullOrWhiteSpace($proxyPort)) {
        return
    }

    $serverPattern = ($serverName -replace '\.', '\.')
    $command = @"
set -e
if [ -f '$configPath' ]; then
  cp -a '$configPath' '$configPath.bak.codex-nginx-proxy'
  sed -i '/server_name[[:space:]]\+$serverPattern;/,/^}/s#proxy_pass http://127\.0\.0\.1:[0-9]\+;#proxy_pass http://127.0.0.1:$proxyPort;#' '$configPath'
  nginx -t
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet nginx; then
    systemctl reload nginx
  else
    nginx -s reload
  fi
fi
"@

    Write-Host ("Ensuring nginx proxy for {0} -> 127.0.0.1:{1} on {2}..." -f $serverName, $proxyPort, $Target.Host) -ForegroundColor Cyan
    try {
        Invoke-Plink -PlinkExe $PlinkExe -ConnectionArgs $ConnectionArgs -CommandText $command
    } catch {
        Write-Host ("  [WARN] nginx proxy fix skipped (non-fatal): {0}" -f $_.Exception.Message) -ForegroundColor Yellow
    }
}

function Invoke-UrlStatusCheck {
    param(
        [string]$Url,
        [string]$Method = 'Get',
        [string]$Body = $null,
        [string]$ContentType = 'application/json',
        [int]$TimeoutSec = 10
    )

    $curlExe = Get-Command 'curl.exe' -ErrorAction SilentlyContinue
    if ($null -ne $curlExe) {
        $curlArgs = @('--silent', '--show-error', '--output', 'NUL', '--write-out', '%{http_code}', '--max-time', [string]$TimeoutSec)
        # HTTPS verification is intentionally relaxed only for post-deploy
        # probes: a peer with a bad certificate must not mask a completed rollout.
        if ($Url -match '^https://') {
            $curlArgs += '--insecure'
        }
        $curlArgs += @('--request', $Method)
        if (-not [string]::IsNullOrEmpty($Body) -and $Method -notin @('Get', 'Head')) {
            $curlArgs += @('--header', ("Content-Type: {0}" -f $ContentType), '--data', $Body)
        }
        $curlArgs += $Url
        $output = & $curlExe.Source @curlArgs
        if ($LASTEXITCODE -eq 0 -and $output -match '^\d{3}$') {
            return [int]$output
        }
        throw ("curl smoke check failed (exit {0}): {1}" -f $LASTEXITCODE, ($output | Out-String).Trim())
    }

    try {
        $args = @{
            Uri = $Url
            Method = $Method
            UseBasicParsing = $true
            TimeoutSec = $TimeoutSec
        }
        if (-not [string]::IsNullOrEmpty($Body) -and $Method -notin @('Get', 'Head')) {
            $args.Body = $Body
            $args.ContentType = $ContentType
        }
        $response = Invoke-WebRequest @args
        return [int]$response.StatusCode
    }
    catch {
        $resp = $null
        if ($null -ne $_.Exception) {
            $resp = $_.Exception.Response
        }
        if ($null -ne $resp) {
            try {
                return [int]$resp.StatusCode
            }
            catch {
                try {
                    return [int]$resp.StatusCode.value__
                }
                catch {
                }
            }
        }
        throw
    }
}

function Assert-HubCenterProblemReportAdminAsset {
    param(
        [string]$BaseUrl,
        [string]$ExpectedScriptSrc,
        [int]$TimeoutSec = 10
    )

    $baseUrl = $BaseUrl.TrimEnd('/')
    $pagePath = Join-Path ([System.IO.Path]::GetTempPath()) ("hubcenter-admin-{0}.html" -f [guid]::NewGuid().ToString('N'))
    $scriptPath = Join-Path ([System.IO.Path]::GetTempPath()) ("hubcenter-problem-reports-{0}.js" -f [guid]::NewGuid().ToString('N'))
    try {
        $curlExe = Get-Command 'curl.exe' -ErrorAction SilentlyContinue
        if ($null -eq $curlExe) {
            throw 'curl.exe is required for the problem reports admin asset check'
        }
        & $curlExe.Source --silent --show-error --fail --insecure --max-time $TimeoutSec --output $pagePath ($baseUrl + '/admin')
        if ($LASTEXITCODE -ne 0) { throw 'could not fetch admin HTML' }
        $adminPage = Get-Content -LiteralPath $pagePath -Raw
        if ($adminPage -notmatch '/admin/assets/js/problem-reports-tab\.js') {
            throw 'admin HTML does not reference the problem reports script'
        }
        $scriptMatch = [regex]::Match($adminPage, '<script\s+src="(?<src>/admin/assets/js/problem-reports-tab\.js[^\"]*)"')
        if (-not $scriptMatch.Success) {
            throw 'admin HTML does not provide a problem reports script URL'
        }
        $actualScriptSrc = $scriptMatch.Groups['src'].Value
        if (-not [string]::IsNullOrWhiteSpace($ExpectedScriptSrc) -and $actualScriptSrc -ne $ExpectedScriptSrc) {
            throw ("admin HTML references stale problem reports script: expected {0}, got {1}" -f $ExpectedScriptSrc, $actualScriptSrc)
        }
        $scriptURL = $baseUrl + $actualScriptSrc
        $separator = if ($scriptURL.Contains('?')) { '&' } else { '?' }
        # Verify the exact cache-versioned script referenced by the page.  The
        # nonce also prevents an intermediary cache from making a stale asset
        # appear healthy immediately after a deployment.
        $scriptURL += ($separator + 'deploy_check=' + [guid]::NewGuid().ToString('N'))
        & $curlExe.Source --silent --show-error --fail --insecure --max-time $TimeoutSec --header 'Cache-Control: no-cache' --output $scriptPath $scriptURL
        if ($LASTEXITCODE -ne 0) { throw 'could not fetch the problem reports script' }
        $script = Get-Content -LiteralPath $scriptPath -Raw
        if ($script -notmatch 'installWhenAdminShellReady') {
            throw 'problem reports script is missing or is an obsolete version'
        }
        if ($script -notmatch 'URL\.createObjectURL' -or $script -notmatch "Authorization: 'Bearer '") {
            throw 'problem reports script is missing authenticated local attachment download support'
        }
    }
    catch {
        throw ("problem reports admin asset check failed: {0}" -f $_.Exception.Message)
    }
    finally {
        Remove-Item -LiteralPath $pagePath, $scriptPath -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-PostDeploySmokeCheck {
    param(
        [object[]]$Targets,
        [string]$ExpectedHubCenterProblemReportsScriptSrc,
        [int]$TimeoutSec = 10
    )

    $failures = @()
    foreach ($target in $Targets) {
        $checks = @()
        if ($target.DeployHubCenter) {
            $checks += [pscustomobject]@{ Label = 'hubcenter healthz'; Url = ("https://{0}/healthz" -f $target.Host); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hubcenter admin'; Url = ("https://{0}/admin" -f $target.Host); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hubcenter hub action route'; Url = ("https://{0}/api/admin/hubs/registration-policy" -f $target.Host); Method = 'Post'; Body = '{"hub_id":"smoke"}'; Want = 401 }
        }
        if ($target.DeployHub -and -not [string]::IsNullOrWhiteSpace($target.HubPublicUrl)) {
            $checks += [pscustomobject]@{ Label = 'hub healthz'; Url = ("{0}/healthz" -f $target.HubPublicUrl.TrimEnd('/')); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hub admin'; Url = ("{0}/admin" -f $target.HubPublicUrl.TrimEnd('/')); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hub card store page'; Url = ("{0}/card_store?tenant_id=tenant_default" -f $target.HubPublicUrl.TrimEnd('/')); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hub card store public api'; Url = ("{0}/api/card-store/products?tenant_id=tenant_default" -f $target.HubPublicUrl.TrimEnd('/')); Want = 200 }
            $checks += [pscustomobject]@{ Label = 'hub card store admin api'; Url = ("{0}/api/admin/card-store/config" -f $target.HubPublicUrl.TrimEnd('/')); Want = 401 }
        }

        if ($target.DeployHubCenter) {
            try {
                Assert-HubCenterProblemReportAdminAsset -BaseUrl ("https://{0}" -f $target.Host) -ExpectedScriptSrc $ExpectedHubCenterProblemReportsScriptSrc -TimeoutSec $TimeoutSec
                Write-Host ("  - {0}: hubcenter problem reports admin asset -> OK" -f $target.Host)
            }
            catch {
                Write-Host ("  - {0}: hubcenter problem reports admin asset -> ERROR" -f $target.Host)
                $failures += ("{0}: {1}" -f $target.Host, $_.Exception.Message)
            }
        }

        foreach ($check in $checks) {
            try {
                $method = if ($check.PSObject.Properties.Name -contains 'Method') { $check.Method } else { 'Get' }
                $body = if (($check.PSObject.Properties.Name -contains 'Body') -and -not [string]::IsNullOrEmpty($check.Body)) { $check.Body } else { $null }
                $status = Invoke-UrlStatusCheck -Url $check.Url -Method $method -Body $body -TimeoutSec $TimeoutSec
                Write-Host ("  - {0}: {1} -> {2}" -f $target.Host, $check.Label, $status)
                if ($status -ne $check.Want) {
                    $failures += ("{0}: {1} expected {2}, got {3} ({4})" -f $target.Host, $check.Label, $check.Want, $status, $check.Url)
                }
            }
            catch {
                $message = (($_ | Out-String).Trim())
                Write-Host ("  - {0}: {1} -> ERROR" -f $target.Host, $check.Label)
                $failures += ("{0}: {1} failed: {2} ({3})" -f $target.Host, $check.Label, $message, $check.Url)
            }
        }
    }

    if ($failures.Count -gt 0) {
        throw ("Post-deploy smoke check failed:`n - " + ($failures -join "`n - "))
    }
}
function Invoke-RemotePrecheck {
    param(
        [string]$PlinkExe,
        [string[]]$ConnectionArgs,
        [pscustomobject]$Target
    )

    $mkdirDirs = @($Target.RemoteTmpDir)
    if ($Target.DeployHub) {
        $mkdirDirs += $Target.RemoteHubDir
    }
    if ($Target.DeployHubCenter) {
        $mkdirDirs += $Target.RemoteHubCenterDir
    }
    $mkdirArgs = ($mkdirDirs | ForEach-Object { '"{0}"' -f $_ }) -join ' '

    $checks = @(
        'PATH="$PATH:/usr/local/go/bin:/root/go/bin"; export PATH',
        '[ -n "$(command -v sh 2>/dev/null)" ] || { echo "missing:sh"; exit 1; }',
        '[ -n "$(command -v tar 2>/dev/null)" ] || { echo "missing:tar"; exit 1; }',
        '[ -n "$(command -v sha256sum 2>/dev/null)" ] || { echo "missing:sha256sum"; exit 1; }',
        ('mkdir -p {0} >/dev/null 2>&1 || {{ echo "mkdir-failed"; exit 1; }}' -f $mkdirArgs),
        ('[ -w "{0}" ] || {{ echo "not-writable:{0}"; exit 1; }}' -f $Target.RemoteTmpDir)
    )
    if ($Target.DeployHubCenter) {
        $checks += ('[ -w "{0}" ] || {{ echo "not-writable:{0}"; exit 1; }}' -f $Target.RemoteHubCenterDir)
    }
    if ($Target.DeployHub) {
        $checks += ('[ -w "{0}" ] || {{ echo "not-writable:{0}"; exit 1; }}' -f $Target.RemoteHubDir)
        $checks += '(command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1) || { echo "missing:curl-or-wget"; exit 1; }'
    }
    $checks += 'echo "precheck:ok"'
    $command = ($checks -join ' && ')
    $output = Invoke-PlinkCapture -PlinkExe $PlinkExe -ConnectionArgs $ConnectionArgs -CommandText $command
    if ($output -notmatch 'precheck:ok') {
        throw ("Remote precheck failed on {0}: {1}" -f $Target.Host, $output)
    }
    return $output
}

$rootDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$powerShellExe = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$promptScript = Join-Path $rootDir 'prompt_password.ps1'
$renderScript = Join-Path $rootDir 'deploy\render-hubcenter-ha-configs.ps1'

if (-not (Test-Path (Join-Path $rootDir 'go.mod'))) { throw 'Missing go.mod' }
if (-not (Test-Path (Join-Path $rootDir 'go.sum'))) { throw 'Missing go.sum' }
if (-not (Test-Path (Join-Path $rootDir 'hub\cmd\hub'))) { throw 'Missing hub source.' }
if (-not (Test-Path (Join-Path $rootDir 'hubcenter\cmd\hubcenter'))) { throw 'Missing hubcenter source.' }
if (-not (Test-Path $renderScript)) { throw 'Missing HA config render script.' }

$plinkExe = Require-Tool 'plink.exe'
$pscpExe = Require-Tool 'pscp.exe'
$tarExe = Require-Tool 'tar.exe'

$sshUser = Get-EnvOrDefault 'REMOTE_USER' 'root'
$sshPort = [int](Get-EnvOrDefault 'REMOTE_PORT' '22')
$hubModelBaseUrl = Get-EnvOrDefault 'HUB_MODEL_BASE_URL' 'https://github.com/RapidAI/MaClaw/releases/download/Model_Release'
$hubModelFiles = Get-EnvOrDefault 'HUB_MODEL_FILES' 'embeddinggemma-300M-Q8_0.gguf sensevoice-small-q8.gguf omniparser-v2.yolow kokoro-v1_0.koro kokoro_82m_selected_voices_koro.zip'

$brandKey = $Brand.ToLowerInvariant()
$brandBuildTag = ''
$hubBinaryName = 'maclaw-hub'
$hubCenterBinaryName = 'maclaw-hubcenter'
$meetingASRWorkerBinaryName = 'meeting_asr_worker'
if ($brandKey -eq 'tigerclaw') {
    $brandBuildTag = 'oem_qianxin'
    $hubBinaryName = 'tigerclaw-hub'
    $hubCenterBinaryName = 'tigerclaw-hubcenter'
}

$targets = @(
    [pscustomobject]@{
        Name = 'hc-1'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-1' 'hubs.mypapers.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC1' 'ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-1' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-1' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-1' '/data/soft/hubcenter'
        HubCenterDBPath = './data/codeclaw-hubcenter.db'
        DeployHubCenter = $true
        HubCenterConfig = 'hubcenter-hc-1.yaml'
        DeployHub = $true
        HubConfig = 'hub-mypapers.yaml'
        HubPublicUrl = 'https://hub.mypapers.top'
    },
    [pscustomobject]@{
        Name = 'hc-2'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-2' 'hubs.maclaw.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC2' 'ssh-ed25519 255 SHA256:yoyEXbuT2kezyG9Y8cJDZplBMZgaPAN7+sureAkVRVE'
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-2' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-2' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-2' '/data/soft/hubcenter'
        HubCenterDBPath = './data/codeclaw-hubcenter.db'
        DeployHubCenter = $true
        HubCenterConfig = 'hubcenter-hc-2.yaml'
        DeployHub = $true
        HubConfig = 'hub-maclaw.yaml'
        HubPublicUrl = 'https://hub.maclaw.top'
        NginxConfigPath = '/etc/nginx/conf.d/maclaw.top.conf'
        NginxHubServerName = 'hub.maclaw.top'
        NginxHubProxyPort = '9399'
    },
    [pscustomobject]@{
        Name = 'hc-3'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-3' 'hubs2.maclaw.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC3' 'ssh-ed25519 255 SHA256:e4P6+FeRZk+ERuReXtR+bE95uZCm1v2Ebei97bdJ5s4'
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-3' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-3' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-3' '/data/soft/hubcenter'
        HubCenterDBPath = './data/codeclaw-hubcenter.db'
        DeployHubCenter = $true
        HubCenterConfig = 'hubcenter-hc-3.yaml'
        DeployHub = $true
        HubConfig = 'hub2-maclaw.yaml'
        HubPublicUrl = 'https://hub2.maclaw.top'
    }
)

$skipTargetsValue = $SkipTargets
if ([string]::IsNullOrWhiteSpace($skipTargetsValue)) {
    $skipTargetsValue = Get-EnvOrDefault 'DEPLOY_SKIP_TARGETS' ''
}
if (-not [string]::IsNullOrWhiteSpace($skipTargetsValue)) {
    $skipSet = @{}
    $skipTargetsValue -split '[,;\s]+' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object {
        $skipSet[$_.Trim().ToLowerInvariant()] = $true
    }
    $targets = @($targets | Where-Object {
        -not ($skipSet.ContainsKey($_.Name.ToLowerInvariant()) -or
            $skipSet.ContainsKey($_.Host.ToLowerInvariant()) -or
            (-not [string]::IsNullOrWhiteSpace($_.HubPublicUrl) -and $skipSet.ContainsKey(([uri]$_.HubPublicUrl).Host.ToLowerInvariant())))
    })
    if ($targets.Count -eq 0) {
        throw 'DEPLOY_SKIP_TARGETS excluded all deployment targets.'
    }
}

if ($Scope -eq 'hubcenter-only') {
    foreach ($target in $targets) {
        $target.DeployHub = $false
        $target.HubConfig = ''
        $target.HubPublicUrl = ''
    }
}
elseif ($Scope -eq 'hub-only') {
    foreach ($target in $targets) {
        $target.DeployHubCenter = $false
        $target.HubCenterConfig = ''
    }
    if ($CleanHubCenterDB) {
        throw '--clean-hubcenter-db cannot be used with hub-only.'
    }
}

$passwordFile = Join-Path $env:TEMP ("deploy_all_password_{0}_{1}.txt" -f (Get-Random), (Get-Random))
$buildBaseRoot = Join-Path $rootDir 'build\deploy-ha'
$runStamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$buildRoot = Join-Path $buildBaseRoot ('run-{0}-{1}' -f $runStamp, $PID)
$stageRoot = Join-Path $buildRoot 'stage'
$renderedDir = Join-Path $rootDir 'deploy\rendered-configs-temp'
$archivePath = Join-Path $buildRoot 'maclaw-deploy.tar.gz'
$remoteScriptPath = Join-Path $buildRoot 'remote_deploy.sh'
$inventoryPath = Join-Path $rootDir 'deploy\hubcenter-ha.inventory.generated.psd1'
$modelStatuses = @()

try {
    $password = Prompt-PasswordIfNeeded -PromptScript $promptScript -PowerShellExe $powerShellExe -HostLabel ("$sshUser@{0}" -f $targets[0].Host) -PasswordFile $passwordFile

    $clusterSecret = Get-EnvOrDefault 'CLUSTER_SECRET' ''
    $secretSource = 'environment'
    if ([string]::IsNullOrWhiteSpace($clusterSecret)) {
        $clusterSecret = Get-ExistingClusterSecret -Path $inventoryPath
        if (-not [string]::IsNullOrWhiteSpace($clusterSecret)) {
            $secretSource = 'existing-inventory'
        }
        else {
            $clusterSecret = New-ClusterSecret
            $secretSource = 'generated'
        }
    }

    Write-Host ''
    Write-Host '[1/9] Deployment topology' -ForegroundColor Cyan
    foreach ($target in $targets) {
        if ($target.DeployHubCenter -and $target.DeployHub) {
            Write-Host ("  - {0}: hubcenter[{1}] + hub[{2}]" -f $target.Host, $target.RemoteHubCenterDir, $target.RemoteHubDir)
        }
        elseif ($target.DeployHubCenter) {
            Write-Host ("  - {0}: hubcenter[{1}] only" -f $target.Host, $target.RemoteHubCenterDir)
        }
        else {
            Write-Host ("  - {0}: hub[{1}] only" -f $target.Host, $target.RemoteHubDir)
        }
    }
    Write-Host ("  Shared cluster secret: {0}" -f $secretSource)
    Write-Host ''

    Write-Host '[2/9] Running remote prechecks...' -ForegroundColor Cyan
    foreach ($target in $targets) {
        $connectionArgs = Get-ConnectionArgs -UserName $sshUser -HostName $target.Host -Port $sshPort -Password $password -HostKey $target.HostKey
        Write-Host ("  - checking {0}" -f $target.Host)
        [void](Invoke-RemotePrecheck -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -Target $target)
    }

    Write-Host '[3/9] Preparing build workspace...' -ForegroundColor Cyan
    New-CleanDirectory -Path $buildRoot
    New-Item -ItemType Directory -Path $stageRoot -Force | Out-Null
    New-CleanDirectory -Path $renderedDir

    Write-Host '[4/9] Rendering hubcenter/hub configs...' -ForegroundColor Cyan
    Write-InventoryFile -Path $inventoryPath -ClusterSecret $clusterSecret
    & $powerShellExe -NoProfile -ExecutionPolicy Bypass -File $renderScript -InventoryPath $inventoryPath -OutputDir $renderedDir
    if ($LASTEXITCODE -ne 0) {
        throw 'HA config rendering failed.'
    }

    Write-Host '[5/9] Building local Linux binaries and staging deploy assets...' -ForegroundColor Cyan
    $shouldBuildHub = @($targets | Where-Object { $_.DeployHub }).Count -gt 0
    $shouldBuildHubCenter = @($targets | Where-Object { $_.DeployHubCenter }).Count -gt 0
    Build-LocalBinaries -SourceRoot $rootDir -OutputRoot $stageRoot -HubBinaryName $hubBinaryName -HubCenterBinaryName $hubCenterBinaryName -MeetingASRWorkerBinaryName $meetingASRWorkerBinaryName -BrandBuildTag $brandBuildTag -BuildHub $shouldBuildHub -BuildHubCenter $shouldBuildHubCenter
    Stage-DeployAssets -SourceRoot $rootDir -StageRoot $stageRoot
    $expectedHubCenterProblemReportsScriptSrc = Get-HubCenterProblemReportsScriptSrc -AdminIndexPath (Join-Path $stageRoot 'hubcenter\web\admin\index.html')
    Write-Host ("  - HubCenter problem reports script: {0}" -f $expectedHubCenterProblemReportsScriptSrc)

    Write-Host '[6/9] Creating deploy archive...' -ForegroundColor Cyan
    & $tarExe -czf $archivePath -C $stageRoot .
    if ($LASTEXITCODE -ne 0) {
        throw 'Failed to create deploy archive.'
    }

    Write-Host '[7/9] Writing remote deployment script...' -ForegroundColor Cyan
    Write-RemoteScript -Path $remoteScriptPath

    $targetIndex = 0
    foreach ($target in $targets) {
        $targetIndex++
        $connectionArgs = Get-ConnectionArgs -UserName $sshUser -HostName $target.Host -Port $sshPort -Password $password -HostKey $target.HostKey
        $hubCenterConfigPath = if ($target.DeployHubCenter) { Join-Path $renderedDir $target.HubCenterConfig } else { '' }
        $hubConfigPath = if ($target.DeployHub) { Join-Path $renderedDir $target.HubConfig } else { '' }
        $ensureHubModels = '0'

        if ($target.DeployHubCenter) {
            Assert-DeployFileExists -Path $hubCenterConfigPath -Label ("hubcenter config for {0}" -f $target.Name)
        }
        if ($target.DeployHub) {
            Assert-DeployFileExists -Path $hubConfigPath -Label ("hub config for {0}" -f $target.Name)
        }

        Write-Host ''
        Write-Host ("[8/9][{0}/{1}] Uploading artifacts to {2}..." -f $targetIndex, $targets.Count, $target.Host) -ForegroundColor Cyan
        Invoke-Plink -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText "mkdir -p $($target.RemoteTmpDir)"
        Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $archivePath -RemotePath "$($target.RemoteTmpDir)/maclaw-deploy.tar.gz"
        Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $remoteScriptPath -RemotePath "$($target.RemoteTmpDir)/remote_deploy.sh"
        if ($target.DeployHubCenter) {
            Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $hubCenterConfigPath -RemotePath "$($target.RemoteTmpDir)/$($target.HubCenterConfig)"
        }
        if ($target.DeployHub) {
            Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $hubConfigPath -RemotePath "$($target.RemoteTmpDir)/$($target.HubConfig)"
            $remoteModelSentinel = "$($target.RemoteHubDir)/data/models/.models-initialized"
            $remoteModelLock = "$($target.RemoteHubDir)/data/models/.models-downloading"
            $remoteModelDir = "$($target.RemoteHubDir)/data/models"
            $remoteModelFilesList = ($hubModelFiles -split '\s+' | Where-Object { $_ }) -join ' '
            $modelStateScript = "missing=0; for name in $remoteModelFilesList; do [ -f '$remoteModelDir/`$name' ] || missing=1; done; if [ -f '$remoteModelLock' ]; then echo downloading; elif [ `$missing -eq 0 ]; then echo ready; else echo missing; fi"
            $modelState = Invoke-PlinkCapture -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText $modelStateScript
            if ($modelState -eq 'ready') {
                $modelStatuses += ("{0}: existing models kept in {1}/data/models" -f $target.Host, $target.RemoteHubDir)
            }
            elseif ($modelState -eq 'downloading') {
                $modelStatuses += ("{0}: model download already running in background" -f $target.Host)
            }
            else {
                $ensureHubModels = '1'
                $modelStatuses += ("{0}: model download will be started in background to {1}/data/models" -f $target.Host, $target.RemoteHubDir)
            }
        }

        $deployHubFlag = if ($target.DeployHub) { '1' } else { '0' }
        $deployHubCenterFlag = if ($target.DeployHubCenter) { '1' } else { '0' }
        $cleanHubCenterDBFlag = if ($CleanHubCenterDB) { '1' } else { '0' }
        $envParts = @(
            ("export REMOTE_TMP_DIR={0}" -f (Quote-ShellEnvValue $target.RemoteTmpDir)),
            ("export REMOTE_HUB_DIR={0}" -f (Quote-ShellEnvValue $target.RemoteHubDir)),
            ("export REMOTE_HUBCENTER_DIR={0}" -f (Quote-ShellEnvValue $target.RemoteHubCenterDir)),
            ("export DEPLOY_HUBCENTER={0}" -f (Quote-ShellEnvValue $deployHubCenterFlag)),
            ("export DEPLOY_HUB={0}" -f (Quote-ShellEnvValue $deployHubFlag)),
            ("export CLEAN_HUBCENTER_DB={0}" -f (Quote-ShellEnvValue $cleanHubCenterDBFlag)),
            ("export HUBCENTER_DB_PATH={0}" -f (Quote-ShellEnvValue $target.HubCenterDBPath)),
            ("export ENSURE_HUB_MODELS={0}" -f (Quote-ShellEnvValue $ensureHubModels)),
            ("export HUB_MODEL_BASE_URL={0}" -f (Quote-ShellEnvValue $hubModelBaseUrl)),
            ("export HUB_MODEL_FILES={0}" -f (Quote-ShellEnvValue $hubModelFiles)),
            ("export HUB_BINARY_NAME={0}" -f (Quote-ShellEnvValue $hubBinaryName)),
            ("export HUBCENTER_BINARY_NAME={0}" -f (Quote-ShellEnvValue $hubCenterBinaryName)),
            ("export MEETING_ASR_WORKER_BINARY_NAME={0}" -f (Quote-ShellEnvValue $meetingASRWorkerBinaryName))
        )
        if ($target.DeployHubCenter) {
            $envParts += ("export HUBCENTER_CONFIG_BASENAME={0}" -f (Quote-ShellEnvValue $target.HubCenterConfig))
        }
        if ($target.DeployHub) {
            $envParts += ("export HUB_CONFIG_BASENAME={0}" -f (Quote-ShellEnvValue $target.HubConfig))
        }

        $remoteCommand = "sed -i 's/\r$//' $($target.RemoteTmpDir)/remote_deploy.sh && chmod +x $($target.RemoteTmpDir)/remote_deploy.sh && {0} && $($target.RemoteTmpDir)/remote_deploy.sh" -f ($envParts -join ' && ')

        Write-Host ("[9/9][{0}/{1}] Deploying uploaded binaries on {2}..." -f $targetIndex, $targets.Count, $target.Host) -ForegroundColor Cyan
        Invoke-Plink -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText $remoteCommand
        Invoke-RemoteNginxHubProxyFix -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -Target $target
    }

    Write-Host ''
    if ($NoCheck) {
        Write-Host 'Post-deploy smoke check skipped (--no-check).' -ForegroundColor Yellow
    }
    else {
        Write-Host 'Running post-deploy smoke checks...' -ForegroundColor Cyan
        Start-Sleep -Seconds 3
        Invoke-PostDeploySmokeCheck -Targets $targets -ExpectedHubCenterProblemReportsScriptSrc $expectedHubCenterProblemReportsScriptSrc
    }

    Write-Host 'Deployment completed successfully.' -ForegroundColor Green
    Write-Host ("Rendered configs: {0}" -f $renderedDir)
    Write-Host 'Services deployed:'
    foreach ($target in $targets) {
        if ($target.DeployHubCenter -and $target.DeployHub) {
            Write-Host ("  - {0}: hubcenter + {1}" -f $target.Host, $target.HubPublicUrl)
        }
        elseif ($target.DeployHubCenter) {
            Write-Host ("  - {0}: hubcenter" -f $target.Host)
        }
        else {
            Write-Host ("  - {0}: {1}" -f $target.Host, $target.HubPublicUrl)
        }
    }
    if ($modelStatuses.Count -gt 0) {
        Write-Host 'Hub model status:'
        foreach ($status in $modelStatuses) {
            Write-Host ("  - {0}" -f $status)
        }
        Write-Host 'Model download log path:'
        foreach ($target in $targets | Where-Object { $_.DeployHub }) {
            Write-Host ("  - {0}: {1}/data/logs/model-download.log" -f $target.Host, $target.RemoteHubDir)
        }
    }
    if ($secretSource -eq 'generated') {
        Write-Host ''
        Write-Host 'Generated cluster secret for this rollout:' -ForegroundColor Yellow
        Write-Host $clusterSecret
    }
}
finally {
    if (Test-Path $passwordFile) {
        Remove-Item -LiteralPath $passwordFile -Force -ErrorAction SilentlyContinue
    }
}















