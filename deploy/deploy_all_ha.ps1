param()

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
    if ($null -eq $cmd) {
        throw "Required tool not found: $Name"
    }
    return $cmd.Source
}

function Escape-Psd1String {
    param([string]$Value)
    return ($Value -replace "'", "''")
}

function New-ClusterSecret {
    $bytes = New-Object byte[] 48
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
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

function Stage-SourceTree {
    param(
        [string]$SourceRoot,
        [string]$StageRoot
    )

    $copyDirs = @(
        'corelib',
        'hub',
        'hubcenter',
        'openclaw-bridge',
        'RapidSpeech.cpp',
        'gui\internal\systray'
    )
    $copyFiles = @('go.mod', 'go.sum')

    foreach ($file in $copyFiles) {
        $src = Join-Path $SourceRoot $file
        if (Test-Path $src) {
            Copy-Item -LiteralPath $src -Destination $StageRoot -Force
        }
    }

    foreach ($dir in $copyDirs) {
        $src = Join-Path $SourceRoot $dir
        if (Test-Path $src) {
            $dstParent = Split-Path -Parent (Join-Path $StageRoot $dir)
            if (-not [string]::IsNullOrWhiteSpace($dstParent)) {
                New-Item -ItemType Directory -Path $dstParent -Force | Out-Null
            }
            Copy-Item -LiteralPath $src -Destination (Join-Path $StageRoot $dir) -Recurse -Force
        }
    }

    $buildSrc = Join-Path $SourceRoot 'build'
    $buildDst = Join-Path $StageRoot 'build'
    if (Test-Path $buildSrc) {
        New-Item -ItemType Directory -Path $buildDst -Force | Out-Null
        Get-ChildItem -LiteralPath $buildSrc | Where-Object { $_.Name -ne 'deploy' } | ForEach-Object {
            Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $buildDst $_.Name) -Recurse -Force
        }
    }

    $removePaths = @(
        'hub\bin',
        'hub\package',
        'hub\data',
        'hub\.gocache',
        'hub\.gomodcache',
        'hubcenter\bin',
        'hubcenter\package',
        'hubcenter\data',
        'hubcenter\.gocache',
        'hubcenter\.gomodcache',
        'openclaw-bridge\node_modules',
        'openclaw-bridge\dist',
        'RapidSpeech.cpp\build',
        'RapidSpeech.cpp\models'
    )

    foreach ($rel in $removePaths) {
        $target = Join-Path $StageRoot $rel
        if (Test-Path $target) {
            Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    Get-ChildItem -LiteralPath $StageRoot -Recurse -File -Force | Where-Object {
        $_.Extension -ieq '.exe' -or $_.Name -like '*.exe~'
    } | Remove-Item -Force -ErrorAction SilentlyContinue
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
            AdvertiseURL  = 'https://hubs.mypapers.top'
            DatabaseDSN   = './data/hubcenter-hc-1.db'
        },
        @{
            NodeID        = 'hc-2'
            NodeName      = 'hubcenter-2'
            PublicBaseURL = 'https://hubs.maclaw.top'
            AdvertiseURL  = 'https://hubs.maclaw.top'
            DatabaseDSN   = './data/hubcenter-hc-2.db'
        },
        @{
            NodeID        = 'hc-3'
            NodeName      = 'hubcenter-3'
            PublicBaseURL = 'https://hubs2.maclaw.top'
            AdvertiseURL  = 'https://hubs2.maclaw.top'
            DatabaseDSN   = './data/hubcenter-hc-3.db'
        }
    )

    Hubs = @(
        @{
            FileName             = 'hub-mypapers.yaml'
            PublicBaseURL        = 'https://hub.mypapers.top'
            PrimaryCenterBaseURL = 'https://hubs.mypapers.top'
            CenterBaseURLs       = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN = './data/maclaw-hub-mypapers.db'
        },
        @{
            FileName             = 'hub-maclaw.yaml'
            PublicBaseURL        = 'https://hub.maclaw.top'
            PrimaryCenterBaseURL = 'https://hubs.maclaw.top'
            CenterBaseURLs       = @(
                'https://hubs.mypapers.top',
                'https://hubs.maclaw.top',
                'https://hubs2.maclaw.top'
            )
            DatabaseDSN = './data/maclaw-hub-maclaw.db'
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
        ': "${CGO_ENABLED:=0}"',
        ': "${GOPROXY:=https://goproxy.cn,direct}"',
        ': "${DEPLOY_HUB:=0}"',
        ': "${ENSURE_HUB_MODELS:=0}"',
        ': "${HUB_MODEL_BASE_URL:=https://github.com/RapidAI/MaClaw/releases/download/Model_Release}"',
        ': "${HUB_MODEL_FILES:=embeddinggemma-300M-Q8_0.gguf moonshine-base-zh.gguf}"',
        ': "${HUB_CONFIG_BASENAME:=}"',
        ': "${HUBCENTER_CONFIG_BASENAME:=hubcenter-config.yaml}"',
        '',
        'if ! command -v go >/dev/null 2>&1; then',
        '  echo "[ERROR] go is not installed on remote host" >&2',
        '  exit 1',
        'fi',
        '',
        'SRC_ROOT="$REMOTE_TMP_DIR/src"',
        'BUILD_ROOT="$REMOTE_TMP_DIR/build"',
        'ARCHIVE_PATH="$REMOTE_TMP_DIR/maclaw-src.tar.gz"',
        'HUB_DATA_DIR="$REMOTE_HUB_DIR/data"',
        'HUB_MODELS_DIR="$HUB_DATA_DIR/models"',
        'HOME_MODELS_DIR="$HOME/.maclaw/models"',
        'MODEL_SENTINEL="$HUB_MODELS_DIR/.models-initialized"',
        'MODEL_LOCK="$HUB_MODELS_DIR/.models-downloading"',
        'MODEL_SCRIPT="$HUB_DATA_DIR/download-models.sh"',
        'MODEL_LOG="$HUB_DATA_DIR/logs/model-download.log"',
        '',
        'rm -rf "$SRC_ROOT" "$BUILD_ROOT"',
        'mkdir -p "$SRC_ROOT" "$BUILD_ROOT"',
        'tar -xzf "$ARCHIVE_PATH" -C "$SRC_ROOT"',
        'cd "$SRC_ROOT"',
        '',
        'echo "[remote] Downloading dependencies..."',
        'GOPROXY="$GOPROXY" go mod download',
        '',
        'RS_BUILD_SCRIPT="$SRC_ROOT/build/build_rapidspeech.sh"',
        'RS_LIB="$SRC_ROOT/RapidSpeech.cpp/build/librapidspeech_static.a"',
        'EXTRA_TAGS=""',
        'if [ -f "$RS_BUILD_SCRIPT" ]; then',
        '  echo "[remote] Building RapidSpeech static library (optional)..."',
        '  chmod +x "$RS_BUILD_SCRIPT"',
        '  if "$RS_BUILD_SCRIPT" && [ -f "$RS_LIB" ]; then',
        '    echo "[remote] RapidSpeech built. Enabling cgo_embedding."',
        '    CGO_ENABLED=1',
        '    EXTRA_TAGS="cgo_embedding"',
        '  else',
        '    echo "[remote] RapidSpeech build skipped or failed. Continuing without cgo_embedding."',
        '  fi',
        'fi',
        '',
        'echo "[remote] Building hubcenter..."',
        'GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -tags "$EXTRA_TAGS" -o "$BUILD_ROOT/maclaw-hubcenter" ./hubcenter/cmd/hubcenter',
        '',
        'if [ "$DEPLOY_HUB" = "1" ]; then',
        '  echo "[remote] Building hub..."',
        '  GOPROXY="$GOPROXY" CGO_ENABLED="$CGO_ENABLED" go build -tags "$EXTRA_TAGS" -o "$BUILD_ROOT/maclaw-hub" ./hub/cmd/hub',
        'fi',
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
        'seed_home_models() {',
        '  mkdir -p "$HOME_MODELS_DIR"',
        '  for name in $HUB_MODEL_FILES; do',
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
        'download_one() {',
        '  name="$1"',
        '  url="$BASE_URL/$name"',
        '  target="$TARGET_DIR/$name"',
        '  tmp="$target.part"',
        '  if [ -f "$target" ]; then',
        '    return 0',
        '  fi',
        '  if command -v curl >/dev/null 2>&1; then',
        '    curl -L --fail --retry 3 --connect-timeout 15 -o "$tmp" "$url"',
        '  elif command -v wget >/dev/null 2>&1; then',
        '    wget -O "$tmp" "$url"',
        '  else',
        '    echo "[ERROR] Neither curl nor wget is available" >&2',
        '    exit 1',
        '  fi',
        '  mv -f "$tmp" "$target"',
        '  cp -f "$target" "$HOME_DIR/$name"',
        '}',
        'for file in "$@"; do',
        '  download_one "$file"',
        'done',
        'touch "$SENTINEL"',
        'MODELEOF',
        '  chmod +x "$MODEL_SCRIPT"',
        '}',
        '',
        'ensure_hub_models() {',
        '  if [ -f "$MODEL_SENTINEL" ]; then',
        '    seed_home_models',
        '    return 0',
        '  fi',
        '  mkdir -p "$HUB_MODELS_DIR" "$HUB_DATA_DIR/logs"',
        '  if [ -f "$MODEL_LOCK" ]; then',
        '    echo "[remote] Hub model download already in progress."',
        '    return 0',
        '  fi',
        '  write_model_download_script',
        '  touch "$MODEL_LOCK"',
        '  nohup "$MODEL_SCRIPT" "$HUB_MODEL_BASE_URL" "$HUB_MODELS_DIR" "$HOME_MODELS_DIR" "$MODEL_SENTINEL" "$MODEL_LOCK" $HUB_MODEL_FILES >> "$MODEL_LOG" 2>&1 &',
        '  echo "[remote] Hub model download started in background: $MODEL_LOG"',
        '}',
        '',
        'deploy_hubcenter() {',
        '  mkdir -p "$REMOTE_HUBCENTER_DIR" "$REMOTE_HUBCENTER_DIR/configs" "$REMOTE_HUBCENTER_DIR/data" "$REMOTE_HUBCENTER_DIR/data/logs"',
        '  cp -f "$BUILD_ROOT/maclaw-hubcenter" "$REMOTE_HUBCENTER_DIR/maclaw-hubcenter"',
        '  chmod +x "$REMOTE_HUBCENTER_DIR/maclaw-hubcenter"',
        '  if [ -f "$SRC_ROOT/hubcenter/start.sh" ]; then',
        '    cp -f "$SRC_ROOT/hubcenter/start.sh" "$REMOTE_HUBCENTER_DIR/start.sh"',
        '    sed -i ''s/\r$//'' "$REMOTE_HUBCENTER_DIR/start.sh"',
        '    chmod +x "$REMOTE_HUBCENTER_DIR/start.sh"',
        '  fi',
        '  if [ -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" ]; then',
        '    cp -f "$SRC_ROOT/hubcenter/configs/config.example.yaml" "$REMOTE_HUBCENTER_DIR/configs/config.example.yaml"',
        '  fi',
        '  if [ -d "$SRC_ROOT/hubcenter/web" ]; then',
        '    rm -rf "$REMOTE_HUBCENTER_DIR/web"',
        '    cp -R "$SRC_ROOT/hubcenter/web" "$REMOTE_HUBCENTER_DIR/web"',
        '  fi',
        '  backup_and_write_config "$REMOTE_HUBCENTER_DIR/configs/config.yaml" "$REMOTE_TMP_DIR/$HUBCENTER_CONFIG_BASENAME"',
        '}',
        '',
        'deploy_hub() {',
        '  mkdir -p "$REMOTE_HUB_DIR" "$REMOTE_HUB_DIR/configs" "$REMOTE_HUB_DIR/data" "$REMOTE_HUB_DIR/data/logs"',
        '  cp -f "$BUILD_ROOT/maclaw-hub" "$REMOTE_HUB_DIR/maclaw-hub"',
        '  chmod +x "$REMOTE_HUB_DIR/maclaw-hub"',
        '  if [ -f "$SRC_ROOT/hub/start.sh" ]; then',
        '    cp -f "$SRC_ROOT/hub/start.sh" "$REMOTE_HUB_DIR/start.sh"',
        '    sed -i ''s/\r$//'' "$REMOTE_HUB_DIR/start.sh"',
        '    chmod +x "$REMOTE_HUB_DIR/start.sh"',
        '  fi',
        '  if [ -f "$SRC_ROOT/hub/configs/config.example.yaml" ]; then',
        '    cp -f "$SRC_ROOT/hub/configs/config.example.yaml" "$REMOTE_HUB_DIR/configs/config.example.yaml"',
        '  fi',
        '  if [ -d "$SRC_ROOT/hub/web" ]; then',
        '    rm -rf "$REMOTE_HUB_DIR/web"',
        '    cp -R "$SRC_ROOT/hub/web" "$REMOTE_HUB_DIR/web"',
        '  fi',
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
        'echo "[remote] Deploying hubcenter files..."',
        'deploy_hubcenter',
        '',
        'if [ "$DEPLOY_HUB" = "1" ]; then',
        '  echo "[remote] Deploying hub files..."',
        '  deploy_hub',
        '  if [ "$ENSURE_HUB_MODELS" = "1" ]; then',
        '    ensure_hub_models',
        '  fi',
        'fi',
        '',
        'echo "[remote] Restarting hubcenter..."',
        'if [ -x "$REMOTE_HUBCENTER_DIR/start.sh" ]; then',
        '  cd "$REMOTE_HUBCENTER_DIR"',
        '  ./start.sh',
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
        'rm -rf "$SRC_ROOT" "$BUILD_ROOT"',
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
        [string]$User,
        [string]$Host,
        [int]$Port,
        [string]$Password,
        [string]$HostKey
    )

    $args = @('-batch')
    if (-not [string]::IsNullOrWhiteSpace($HostKey)) {
        $args += @('-hostkey', $HostKey)
    }
    $args += @('-P', [string]$Port, '-pw', $Password, "$User@$Host")
    return ,$args
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

function Invoke-RemotePrecheck {
    param(
        [string]$PlinkExe,
        [string[]]$ConnectionArgs,
        [pscustomobject]$Target
    )

    $checks = @(
        '[ -n "$(command -v sh 2>/dev/null)" ] || { echo "missing:sh"; exit 1; }',
        '[ -n "$(command -v tar 2>/dev/null)" ] || { echo "missing:tar"; exit 1; }',
        '[ -n "$(command -v go 2>/dev/null)" ] || { echo "missing:go"; exit 1; }',
        ('mkdir -p "{0}" "{1}" "{2}" >/dev/null 2>&1 || {{ echo "mkdir-failed"; exit 1; }}' -f $Target.RemoteTmpDir, $Target.RemoteHubDir, $Target.RemoteHubCenterDir),
        ('[ -w "{0}" ] || {{ echo "not-writable:{0}"; exit 1; }}' -f $Target.RemoteTmpDir),
        ('[ -w "{0}" ] || {{ echo "not-writable:{0}"; exit 1; }}' -f $Target.RemoteHubCenterDir)
    )
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
$cgoEnabled = Get-EnvOrDefault 'CGO_ENABLED' '0'
$goproxy = Get-EnvOrDefault 'GOPROXY' 'https://goproxy.cn,direct'
$hubModelBaseUrl = Get-EnvOrDefault 'HUB_MODEL_BASE_URL' 'https://github.com/RapidAI/MaClaw/releases/download/Model_Release'
$hubModelFiles = Get-EnvOrDefault 'HUB_MODEL_FILES' 'embeddinggemma-300M-Q8_0.gguf moonshine-base-zh.gguf'

$targets = @(
    [pscustomobject]@{
        Name = 'hc-1'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-1' 'hubs.mypapers.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC1' 'ssh-ed25519 255 SHA256:i4dErlVhnE3VDG7s6lOJ/cg3wfyqf1bgRXSqIddwuog'
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-1' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-1' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-1' '/data/soft/hubcenter'
        HubCenterConfig = 'hubcenter-hc-1.yaml'
        DeployHub = $true
        HubConfig = 'hub-mypapers.yaml'
        HubPublicUrl = 'https://hub.mypapers.top'
    },
    [pscustomobject]@{
        Name = 'hc-2'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-2' 'hubs.maclaw.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC2' ''
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-2' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-2' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-2' '/data/soft/hubcenter'
        HubCenterConfig = 'hubcenter-hc-2.yaml'
        DeployHub = $true
        HubConfig = 'hub-maclaw.yaml'
        HubPublicUrl = 'https://hub.maclaw.top'
    },
    [pscustomobject]@{
        Name = 'hc-3'
        Host = Get-TargetSetting 'DEPLOY_HOST' 'hc-3' 'hubs2.maclaw.top'
        HostKey = Get-EnvOrDefault 'DEPLOY_HOSTKEY_HC3' ''
        RemoteTmpDir = Get-TargetSetting 'REMOTE_TMP_DIR' 'hc-3' '/tmp/aicoder_deploy'
        RemoteHubDir = Get-TargetSetting 'REMOTE_HUB_DIR' 'hc-3' '/data/soft/hub'
        RemoteHubCenterDir = Get-TargetSetting 'REMOTE_HUBCENTER_DIR' 'hc-3' '/data/soft/hubcenter'
        HubCenterConfig = 'hubcenter-hc-3.yaml'
        DeployHub = $false
        HubConfig = ''
        HubPublicUrl = ''
    }
)

$passwordFile = Join-Path $env:TEMP ("deploy_all_password_{0}_{1}.txt" -f (Get-Random), (Get-Random))
$buildRoot = Join-Path $rootDir 'build\deploy-ha'
$stageRoot = Join-Path $buildRoot 'stage'
$renderedDir = Join-Path $buildRoot 'rendered-configs'
$archivePath = Join-Path $buildRoot 'maclaw-src.tar.gz'
$remoteScriptPath = Join-Path $buildRoot 'remote_deploy.sh'
$inventoryPath = Join-Path $buildRoot 'hubcenter-ha.inventory.generated.psd1'
$modelStatuses = @()

try {
    $password = Prompt-PasswordIfNeeded -PromptScript $promptScript -PowerShellExe $powerShellExe -HostLabel ("$sshUser@{0}" -f $targets[0].Host) -PasswordFile $passwordFile

    $clusterSecret = Get-EnvOrDefault 'CLUSTER_SECRET' ''
    $secretSource = 'environment'
    if ([string]::IsNullOrWhiteSpace($clusterSecret)) {
        $clusterSecret = New-ClusterSecret
        $secretSource = 'generated'
    }

    Write-Host ''
    Write-Host '[1/9] Deployment topology' -ForegroundColor Cyan
    foreach ($target in $targets) {
        if ($target.DeployHub) {
            Write-Host ("  - {0}: hubcenter[{1}] + hub[{2}]" -f $target.Host, $target.RemoteHubCenterDir, $target.RemoteHubDir)
        }
        else {
            Write-Host ("  - {0}: hubcenter[{1}] only" -f $target.Host, $target.RemoteHubCenterDir)
        }
    }
    Write-Host ("  Shared cluster secret: {0}" -f $secretSource)
    Write-Host ''

    Write-Host '[2/9] Running remote prechecks...' -ForegroundColor Cyan
    foreach ($target in $targets) {
        $connectionArgs = Get-ConnectionArgs -User $sshUser -Host $target.Host -Port $sshPort -Password $password -HostKey $target.HostKey
        Write-Host ("  - checking {0}" -f $target.Host)
        [void](Invoke-RemotePrecheck -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -Target $target)
    }

    Write-Host '[3/9] Preparing build workspace...' -ForegroundColor Cyan
    New-CleanDirectory -Path $buildRoot
    New-Item -ItemType Directory -Path $stageRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $renderedDir -Force | Out-Null

    Write-Host '[4/9] Rendering hubcenter/hub configs...' -ForegroundColor Cyan
    Write-InventoryFile -Path $inventoryPath -ClusterSecret $clusterSecret
    & $powerShellExe -NoProfile -ExecutionPolicy Bypass -File $renderScript -InventoryPath $inventoryPath -OutputDir $renderedDir
    if ($LASTEXITCODE -ne 0) {
        throw 'HA config rendering failed.'
    }

    Write-Host '[5/9] Staging source tree...' -ForegroundColor Cyan
    Stage-SourceTree -SourceRoot $rootDir -StageRoot $stageRoot

    Write-Host '[6/9] Creating source archive...' -ForegroundColor Cyan
    & $tarExe -czf $archivePath -C $stageRoot .
    if ($LASTEXITCODE -ne 0) {
        throw 'Failed to create source archive.'
    }

    Write-Host '[7/9] Writing remote deployment script...' -ForegroundColor Cyan
    Write-RemoteScript -Path $remoteScriptPath

    $targetIndex = 0
    foreach ($target in $targets) {
        $targetIndex++
        $connectionArgs = Get-ConnectionArgs -User $sshUser -Host $target.Host -Port $sshPort -Password $password -HostKey $target.HostKey
        $hubCenterConfigPath = Join-Path $renderedDir $target.HubCenterConfig
        $hubConfigPath = if ($target.DeployHub) { Join-Path $renderedDir $target.HubConfig } else { '' }
        $ensureHubModels = '0'

        Write-Host ''
        Write-Host ("[8/9][{0}/{1}] Uploading artifacts to {2}..." -f $targetIndex, $targets.Count, $target.Host) -ForegroundColor Cyan
        Invoke-Plink -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText "mkdir -p $($target.RemoteTmpDir)"
        Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $archivePath -RemotePath "$($target.RemoteTmpDir)/maclaw-src.tar.gz"
        Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $remoteScriptPath -RemotePath "$($target.RemoteTmpDir)/remote_deploy.sh"
        Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $hubCenterConfigPath -RemotePath "$($target.RemoteTmpDir)/$($target.HubCenterConfig)"
        if ($target.DeployHub) {
            Invoke-PscpUpload -PscpExe $pscpExe -ConnectionArgs $connectionArgs -LocalPath $hubConfigPath -RemotePath "$($target.RemoteTmpDir)/$($target.HubConfig)"
            $remoteModelSentinel = "$($target.RemoteHubDir)/data/models/.models-initialized"
            $remoteModelLock = "$($target.RemoteHubDir)/data/models/.models-downloading"
            $modelState = Invoke-PlinkCapture -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText "if [ -f '$remoteModelSentinel' ]; then echo ready; elif [ -f '$remoteModelLock' ]; then echo downloading; else echo missing; fi"
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
        $envParts = @(
            "CGO_ENABLED=$cgoEnabled",
            "GOPROXY=$goproxy",
            "REMOTE_TMP_DIR=$($target.RemoteTmpDir)",
            "REMOTE_HUB_DIR=$($target.RemoteHubDir)",
            "REMOTE_HUBCENTER_DIR=$($target.RemoteHubCenterDir)",
            "DEPLOY_HUB=$deployHubFlag",
            "ENSURE_HUB_MODELS=$ensureHubModels",
            "HUB_MODEL_BASE_URL=$hubModelBaseUrl",
            "HUB_MODEL_FILES=$hubModelFiles",
            "HUBCENTER_CONFIG_BASENAME=$($target.HubCenterConfig)"
        )
        if ($target.DeployHub) {
            $envParts += "HUB_CONFIG_BASENAME=$($target.HubConfig)"
        }

        $remoteCommand = "sed -i 's/\r$//' $($target.RemoteTmpDir)/remote_deploy.sh && chmod +x $($target.RemoteTmpDir)/remote_deploy.sh && {0} $($target.RemoteTmpDir)/remote_deploy.sh" -f ($envParts -join ' ')

        Write-Host ("[9/9][{0}/{1}] Building and deploying on {2}..." -f $targetIndex, $targets.Count, $target.Host) -ForegroundColor Cyan
        Invoke-Plink -PlinkExe $plinkExe -ConnectionArgs $connectionArgs -CommandText $remoteCommand
    }

    Write-Host ''
    Write-Host 'Deployment completed successfully.' -ForegroundColor Green
    Write-Host ("Rendered configs: {0}" -f $renderedDir)
    Write-Host 'Services deployed:'
    foreach ($target in $targets) {
        if ($target.DeployHub) {
            Write-Host ("  - {0}: hubcenter + {1}" -f $target.Host, $target.HubPublicUrl)
        }
        else {
            Write-Host ("  - {0}: hubcenter" -f $target.Host)
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

