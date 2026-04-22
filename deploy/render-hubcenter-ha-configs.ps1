param(
    [Parameter(Mandatory = $true)]
    [string]$InventoryPath,

    [string]$OutputDir = './out-ha-configs'
)

$ErrorActionPreference = 'Stop'

function Normalize-Url {
    param([string]$Value)
    return $Value.Trim().TrimEnd('/')
}

function Quote-YamlString {
    param([string]$Value)
    if ($null -eq $Value) {
        return "''"
    }
    return "'" + ($Value -replace "'", "''") + "'"
}

function Render-HubCenterConfig {
    param(
        [hashtable]$Center,
        [array]$Peers,
        [string]$ClusterSecret
    )

    $peerLines = foreach ($peer in $Peers) {
@"
    - node_id: $($peer.NodeID)
      name: $($peer.NodeName)
      base_url: $(Normalize-Url $peer.AdvertiseURL)
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
  node_id: $($Center.NodeID)
  node_name: $($Center.NodeName)
  advertise_url: $(Normalize-Url $Center.AdvertiseURL)
  cluster_secret: $ClusterSecret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  peers:
$($peerLines -join "`r`n")

database:
  driver: sqlite
  dsn: $(Quote-YamlString $Center.DatabaseDSN)
  wal: true
  busy_timeout_ms: 5000
  max_read_open_conns: 8
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
  name: MaClaw Hub
  description: Self-hosted MaClaw remote hub
  visibility: private

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

$inventory = Import-PowerShellDataFile -Path $InventoryPath
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
    $peers = @($centers | Where-Object { $_.NodeID -ne $center.NodeID })
    $content = Render-HubCenterConfig -Center $center -Peers $peers -ClusterSecret $inventory.ClusterSecret
    $target = Join-Path $OutputDir ("hubcenter-{0}.yaml" -f $center.NodeID)
    Set-Content -Path $target -Value $content -Encoding UTF8
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
    Set-Content -Path $hubTarget -Value $hubContent -Encoding UTF8
    Write-Host "Wrote $hubTarget" -ForegroundColor Green
}
