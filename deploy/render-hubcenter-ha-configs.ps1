param(
    [Parameter(Mandatory = $true)]
    [string]$InventoryPath,

    [string]$OutputDir = './out-ha-configs'
)

$ErrorActionPreference = 'Stop'

function Import-InventoryDataFile {
    param([string]$Path)

    $importDataFile = Get-Command Import-PowerShellDataFile -ErrorAction SilentlyContinue
    if ($null -ne $importDataFile) {
        return Import-PowerShellDataFile -LiteralPath $Path
    }

    # Windows PowerShell 5.1 lacks Import-PowerShellDataFile; generated
    # deployment inventories are local PSD1 hashtables.
    $resolvedPath = (Resolve-Path -LiteralPath $Path -ErrorAction Stop).Path
    return & {
        param([string]$InventoryFile)
        Invoke-Expression ([System.IO.File]::ReadAllText($InventoryFile))
    } $resolvedPath
}

function Normalize-Url {
    param([string]$Value)
    return $Value.Trim().TrimEnd('/')
}

function Get-Fqdn {
    param([hashtable]$Center)

    foreach ($candidate in @($Center.FQDN, $Center.PublicBaseURL, $Center.AdvertiseURL)) {
        if ([string]::IsNullOrWhiteSpace([string]$candidate)) {
            continue
        }
        $value = ([string]$candidate).Trim()
        if ($value -match '^[a-zA-Z][a-zA-Z0-9+.-]*://') {
            try {
                return ([uri]$value).Host.ToLowerInvariant()
            }
            catch {
            }
        }
        return $value.Trim().Trim('/').ToLowerInvariant()
    }

    throw "HubCenter entry is missing FQDN/PublicBaseURL/AdvertiseURL"
}

function Quote-YamlString {
    param([string]$Value)
    if ($null -eq $Value) {
        return "''"
    }
    return "'" + ($Value -replace "'", "''") + "'"
}

function Write-Utf8NoBomFile {
    param(
        [string]$Path,
        [string]$Content
    )

    if (-not $Content.EndsWith("`n")) {
        $Content += "`r`n"
    }
    $encoding = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function Render-HubCenterConfig {
    param(
        [hashtable]$Center,
        [array]$Centers,
        [string]$ClusterSecret
    )

    $nodeLines = foreach ($node in $Centers) {
@"
    - fqdn: $(Get-Fqdn $node)
      node_id: $($node.NodeID)
      node_name: $($node.NodeName)
      advertise_url: $(Normalize-Url $node.AdvertiseURL)
      public_url: $(Normalize-Url $node.PublicBaseURL)
      enabled: true
"@
    }

@"
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: $(Normalize-Url $Center.PublicBaseURL)

ha:
  enabled: true
  self_fqdn: $(Get-Fqdn $Center)
  private_key_path: ./data/ha_node_key.pem
  cluster_secret: $ClusterSecret
  sync_interval_seconds: 5
  push_debounce_seconds: 5
  pull_batch_size: 1000
  heartbeat_sync_min_interval_seconds: 600
  history_retention_days: 0.5
  history_max_retained_ops: 50000
  history_prune_interval_minutes: 10
  history_prune_batch_size: 20000
  nodes:
$($nodeLines -join "`r`n")

database:
  driver: sqlite
  dsn: $(Quote-YamlString $Center.DatabaseDSN)
  wal: true
  busy_timeout_ms: 10000
  max_read_open_conns: 4
  max_read_idle_conns: 4
  max_write_open_conns: 1
  max_write_idle_conns: 1

mail:
  enabled: false
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: MaClaw Hub Center
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
"@
}

function Render-HubConfig {
    param([hashtable]$Hub)

    $centerLines = foreach ($url in $Hub.CenterBaseURLs) {
        "    - $(Normalize-Url $url)"
    }
    $hubName = if ([string]::IsNullOrWhiteSpace([string]$Hub.Name)) { 'MaClaw Hub' } else { [string]$Hub.Name }
    $hubDescription = if ([string]::IsNullOrWhiteSpace([string]$Hub.Description)) { 'Self-hosted MaClaw remote hub' } else { [string]$Hub.Description }
    $hubVisibility = if ([string]::IsNullOrWhiteSpace([string]$Hub.Visibility)) { 'private' } else { ([string]$Hub.Visibility).Trim().ToLowerInvariant() }
    $corpDomain = ''
    if ($null -ne $Hub.CorporateEmailDomain) {
        $corpDomain = ([string]$Hub.CorporateEmailDomain).Trim()
    }
    $corpDomains = @()
    if ($null -ne $Hub.CorporateEmailDomains) {
        $corpDomains = @($Hub.CorporateEmailDomains | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) } | ForEach-Object { ([string]$_).Trim() })
    }
    $acceptPublicSignup = $false
    if ($null -ne $Hub.AcceptPublicSignup) {
        $acceptPublicSignup = [bool]$Hub.AcceptPublicSignup
    }
    $corpDomainLines = @()
    if ($corpDomains.Count -gt 0) {
        $corpDomainLines += '  corporate_email_domains:'
        foreach ($domain in $corpDomains) {
            $corpDomainLines += ('    - {0}' -f (Quote-YamlString $domain))
        }
    }
    $corpDomainBlock = ''
    if ($corpDomainLines.Count -gt 0) {
        $corpDomainBlock = "`r`n" + ($corpDomainLines -join "`r`n")
    }

@"
server:
  listen_host: 0.0.0.0
  listen_port: 9399
  public_base_url: $(Normalize-Url $Hub.PublicBaseURL)

database:
  driver: sqlite
  dsn: $(Quote-YamlString $Hub.DatabaseDSN)
  wal: true
  busy_timeout_ms: 5000
  max_read_open_conns: 8
  max_read_idle_conns: 4
  max_write_open_conns: 1
  max_write_idle_conns: 1

identity:
  enrollment_mode: open
  allow_self_enroll: true

pwa:
  static_dir: ./web/dist
  route_prefix: /app

center:
  enabled: true
  base_url: $(Normalize-Url $Hub.PrimaryCenterBaseURL)
  base_urls:
$($centerLines -join "`r`n")
  register_on_startup: true
  heartbeat_interval_sec: 30

hub:
  name: $(Quote-YamlString $hubName)
  description: $(Quote-YamlString $hubDescription)
  visibility: $(Quote-YamlString $hubVisibility)
  corporate_email_domain: $(Quote-YamlString $corpDomain)$corpDomainBlock
  accept_public_signup: $($acceptPublicSignup.ToString().ToLowerInvariant())

mail:
  enabled: false
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: MaClaw Hub
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
"@
}
$inventory = Import-InventoryDataFile -Path $InventoryPath
if ($null -eq $inventory) {
    throw "Failed to load inventory from $InventoryPath"
}

$centers = @($inventory.HubCenters)
if ($centers.Count -ne 3) {
    throw "Inventory must contain exactly 3 HubCenters entries."
}
if ([string]::IsNullOrWhiteSpace($inventory.ClusterSecret)) {
    throw 'Inventory ClusterSecret is required.'
}
$hubs = @()
if ($null -ne $inventory.Hubs) {
    $hubs = @($inventory.Hubs)
}
elseif ($null -ne $inventory.Hub) {
    $hubs = @($inventory.Hub)
}
if ($hubs.Count -eq 0) {
    throw 'Inventory Hub or Hubs section is required.'
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

foreach ($center in $centers) {
    $content = Render-HubCenterConfig -Center $center -Centers $centers -ClusterSecret $inventory.ClusterSecret
    $target = Join-Path $OutputDir ("hubcenter-{0}.yaml" -f $center.NodeID)
    Write-Utf8NoBomFile -Path $target -Content $content
    Write-Host "Wrote $target" -ForegroundColor Green
}

for ($i = 0; $i -lt $hubs.Count; $i++) {
    $hub = $hubs[$i]
    $hubContent = Render-HubConfig -Hub $hub
    $hubFileName = if ($null -ne $hub.FileName -and -not [string]::IsNullOrWhiteSpace([string]$hub.FileName)) {
        [string]$hub.FileName
    }
    elseif ($hubs.Count -eq 1) {
        'hub.yaml'
    }
    else {
        'hub-{0}.yaml' -f ($i + 1)
    }
    $hubTarget = Join-Path $OutputDir $hubFileName
    Write-Utf8NoBomFile -Path $hubTarget -Content $hubContent
    Write-Host "Wrote $hubTarget" -ForegroundColor Green
}

