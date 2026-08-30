[CmdletBinding()]
param(
    [switch]$SkipHostTest
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$mainC = Join-Path $projectRoot 'main\main.c'
$failures = @()

function Assert-FileLacks([string]$Path, [string]$Pattern, [string]$Why) {
    $hits = Select-String -Path $Path -Pattern $Pattern
    if ($hits) {
        $script:failures += "${Why}: found /$Pattern/ in $Path ($($hits.Count) hit(s))"
    }
}

foreach ($ident in @(
        's_setup_server',
        's_dns_task',
        's_setup_restart_task',
        'setup_save_handler',
        'dns_server_task',
        'start_setup_portal_locked',
        'httpd_start\s*\('
    )) {
    Assert-FileLacks $mainC $ident "composition-root dual-write leftover"
}

Assert-FileLacks $mainC '#include\s+"esp_http_server.h"' "httpd include must leave main.c"

$svc = Join-Path $projectRoot 'main\services\provisioning_service.c'
$entropySvc = Join-Path $projectRoot 'main\services\entropy_service.c'
$entropyHdr = Join-Path $projectRoot 'main\services\entropy_service.h'
$qrSvc = Join-Path $projectRoot 'main\services\provisioning_qr_service.c'
$hdr = Join-Path $projectRoot 'main\services\provisioning_service.h'
$defaults = Join-Path $projectRoot 'sdkconfig.defaults'
if (-not (Test-Path -LiteralPath $svc)) { $failures += 'missing services/provisioning_service.c' }
if (-not (Test-Path -LiteralPath $hdr)) { $failures += 'missing services/provisioning_service.h' }
if (-not (Test-Path -LiteralPath $entropySvc)) { $failures += 'missing services/entropy_service.c' }
if (-not (Test-Path -LiteralPath $entropyHdr)) { $failures += 'missing services/entropy_service.h' }
if (-not (Test-Path -LiteralPath $qrSvc)) { $failures += 'missing services/provisioning_qr_service.c' }

Assert-FileLacks $mainC '\besp_fill_random\s*\(' 'composition root must use Entropy Service'
if (Test-Path -LiteralPath $svc) { Assert-FileLacks $svc '\besp_fill_random\s*\(' 'Provisioning Service must use Entropy Service' }
if (Test-Path -LiteralPath $entropyHdr) {
    Assert-FileLacks $entropyHdr '#include\s*[<"](?:esp_|freertos/)' 'Entropy public header leaked SDK/RTOS detail'
}
if (Test-Path -LiteralPath $entropySvc) {
    $entropyText = Get-Content -LiteralPath $entropySvc -Raw
    if ($entropyText -notmatch 'esp_fill_random\s*\(') { $failures += 'Entropy Service lacks hardware RNG source owner' }
}
if (Test-Path -LiteralPath $qrSvc) {
    $qrText = Get-Content -LiteralPath $qrSvc -Raw
    foreach ($required in @(
            '#include\s+"mbedtls/platform_util.h"',
            'char payload\[128\]',
            'mbedtls_platform_zeroize\(payload, sizeof\(payload\)\)')) {
        if ($qrText -notmatch $required) {
            $failures += "QR service lacks credential payload cleanup (${required})"
        }
    }
}

foreach ($path in @($svc, $hdr)) {
    if (Test-Path -LiteralPath $path) {
        Assert-FileLacks $path 'board_port_' "new increment must not call board_port"
        Assert-FileLacks $path 'CONFIG_MACLAW_BOARD_' "new increment must not branch on board type"
    }
}

$cmake = Get-Content -LiteralPath (Join-Path $projectRoot 'main\CMakeLists.txt') -Raw
if ($cmake -notlike '*services/provisioning_service.c*') {
    $failures += 'CMakeLists.txt missing services/provisioning_service.c'
}

# Public header stays Device-API value types only.
if (Test-Path -LiteralPath $hdr) {
    Assert-FileLacks $hdr '#include\s*[<"](?:esp_|freertos/|httpd)' "public header must not include ESP-IDF/httpd"
    Assert-FileLacks $hdr '\besp_err_t\b' "public header must not expose esp_err_t"
    Assert-FileLacks $hdr '\bsave_full_config\b|\bprovisioning_save_request_t\b|\bsave_pairing_code\b' "Provisioning Service must not own composition-root configuration persistence callbacks"
}

# Production provisioning must not publish an open AP: it carries Wi-Fi/EAP
# and Hub credentials. The per-portal WPA2 passphrase stays ephemeral and is
# passed only to the composition-root radio/QR adapters.
if (Test-Path -LiteralPath $svc) {
    $svcText = Get-Content -LiteralPath $svc -Raw
    foreach ($required in @(
            'configuration_provisioning_request_t',
            'configuration_service_stage_provisioning\s*\(',
            'configuration_service_valid_pairing_code\s*\(',
            'configuration_service_set_pairing_code\s*\(',
            'generate_setup_ap_passphrase\s*\(',
            'wifi_configure_protected_ap\s*\(',
            'show_qr\s*\(\s*ap_ssid\s*,\s*ap_secret\.passphrase\s*\)')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks protected-AP contract (${required})"
        }
    }
    foreach ($required in @(
            'generate_setup_csrf_token\s*\(',
            'setup_portal_csrf_valid\s*\(',
            'setup rejected save with missing or stale CSRF token',
            'setup rejected delete with missing or stale CSRF token',
            'mbedtls_ct_memcmp',
            'mbedtls_platform_zeroize\(s_setup_csrf_token',
            'cleanup_setup_portal_form_scope',
            'cleanup_setup_portal_save_secret_scope',
            'cleanup_setup_portal_delete_secret_scope',
            'cleanup_setup_portal_ap_secret_scope',
            'mbedtls_platform_zeroize\(scope, sizeof\(\*scope\)\)')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks CSRF generation/validation lifecycle (${required})"
        }
    }
    foreach ($required in @(
            'setup_portal_form_scope_t\s+form_scope',
            'setup_portal_save_secret_scope_t\s+secrets',
            'setup_portal_delete_secret_scope_t\s+secrets',
            'setup_portal_ap_secret_scope_t\s+ap_secret',
            '__attribute__\(\(cleanup\(cleanup_setup_portal_form_scope\)\)\)',
            '__attribute__\(\(cleanup\(cleanup_setup_portal_save_secret_scope\)\)\)',
            '__attribute__\(\(cleanup\(cleanup_setup_portal_delete_secret_scope\)\)\)',
            '__attribute__\(\(cleanup\(cleanup_setup_portal_ap_secret_scope\)\)\)')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks per-request/per-portal secret cleanup (${required})"
        }
    }
    # A credential-bearing setup portal is a bounded generation.  Keep its
    # expiry worker lifecycle explicit so a later extraction cannot turn the
    # protected AP into an indefinitely live management surface.
    foreach ($required in @(
            'SETUP_PORTAL_TTL_MS',
            'setup_portal_ttl_task\s*\(',
            'start_setup_portal_ttl_task\s*\(',
            'stop_setup_portal_ttl_task\s*\(',
            'name\s*=\s*"setup_ttl"',
            'setup portal expired after',
            's_setup_ttl_starting',
            's_setup_ttl_admission_open')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks bounded portal-TTL lifecycle (${required})"
        }
    }
    foreach ($required in @(
            'SETUP_PORTAL_RATE_CLIENT_CAPACITY',
            'SETUP_PORTAL_MUTATION_LIMIT',
            'SETUP_PORTAL_REFRESH_LIMIT',
            'setup_portal_rate_limit_allows\s*\(',
            'setup_portal_rate_limit_exceeded\s*\(',
            '429 Too Many Requests',
            'Retry-After',
            'SETUP_PORTAL_RATE_MUTATION',
            'SETUP_PORTAL_RATE_REFRESH',
            'memset\(s_setup_rate_clients')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks bounded portal request-rate limit (${required})"
        }
    }
    # ESP HTTP Server has one request worker. Bound slow/incomplete clients at
    # both the accepted socket and the credential body levels, while retaining
    # the existing LRU eviction for captive-probe churn.
    foreach ($required in @(
            'SETUP_PORTAL_HTTP_MAX_OPEN_SOCKETS',
            'SETUP_PORTAL_HTTP_BACKLOG_CONNECTIONS',
            'SETUP_PORTAL_HTTP_RECV_WAIT_TIMEOUT_SECONDS',
            'SETUP_PORTAL_HTTP_SEND_WAIT_TIMEOUT_SECONDS',
            'SETUP_PORTAL_FORM_RECEIVE_DEADLINE_MS',
            'receive_setup_form_body\s*\(',
            'HTTPD_408_REQ_TIMEOUT',
            'server_config\.max_open_sockets\s*=',
            'server_config\.backlog_conn\s*=',
            'server_config\.recv_wait_timeout\s*=',
            'server_config\.send_wait_timeout\s*=',
            'server_config\.lru_purge_enable\s*=\s*true',
            'Connection", "close"')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks bounded HTTP connection/body lifecycle (${required})"
        }
    }
}
$mainText = Get-Content -LiteralPath $mainC -Raw
Assert-FileLacks $mainC '\bsave_device_config\b|\bprovisioning_host_save_full_config\b|\bconfiguration_service_save_provisioning\b' "main.c must not assemble or persist provisioning snapshots outside Configuration Service"
Assert-FileLacks $mainC '\bsave_pairing_code_only\b|\bprovisioning_host_save_pairing_code\b|\bis_six_digit_pair_code\b' "main.c must not mirror reuse-network pairing-code validation or persistence"
if ($mainText -match 'WIFI_AUTH_OPEN' -or $mainText -match 'WIFI:T:nopass') {
    $failures += 'production provisioning must not configure or advertise an open setup AP'
}
foreach ($required in @(
        'provisioning_host_wifi_configure_protected_ap\s*\(',
        'provisioning_qr_service_show\s*\(',
        '\.wifi_configure_protected_ap\s*=')) {
    if ($mainText -notmatch $required) {
        $failures += "composition root lacks protected-AP implementation (${required})"
    }
}
$wifiDriverOwner = Join-Path $projectRoot 'main\services\connectivity_wifi_driver_owner.c'
if (-not (Test-Path -LiteralPath $wifiDriverOwner)) {
    $failures += 'missing Wi-Fi driver physical owner for protected setup AP'
} else {
    $wifiDriverOwnerText = Get-Content -LiteralPath $wifiDriverOwner -Raw
    foreach ($required in @(
            'connectivity_wifi_driver_owner_configure_protected_ap\s*\(',
            'WIFI_AUTH_WPA2_PSK',
            'strlcpy\(\(char \*\)ap\.ap\.password, passphrase',
            'mbedtls_platform_zeroize\(&ap, sizeof\(ap\)\)')) {
        if ($wifiDriverOwnerText -notmatch $required) {
            $failures += "Wi-Fi driver owner lacks protected-AP implementation (${required})"
        }
    }
}

# APSTA pairing recovery may retain the device's own station connection, but
# the temporary credential-bearing SoftAP must never become a routed/NATed
# path to that station or to the Hub.  The service owns the fail-closed policy
# decision; the composition root owns ESP-NETIF/lwIP details and performs the
# runtime NAPT disable against the concrete AP netif.
foreach ($required in @(
        'provisioning_host_verify_ap_client_isolation\s*\(',
        '\.verify_ap_client_isolation\s*=')) {
    if ($mainText -notmatch $required) {
        $failures += "composition root lacks AP client isolation enforcement (${required})"
    }
}
Assert-FileLacks $mainC 'esp_netif_napt_enable\s*\(' "setup AP must not enable NAPT"

$provisioningNetworkOwner = Join-Path $projectRoot 'main\services\provisioning_network_owner.c'
if (-not (Test-Path -LiteralPath $provisioningNetworkOwner)) {
    $failures += 'missing Provisioning AP network physical owner for client isolation'
} else {
    $provisioningNetworkOwnerText = Get-Content -LiteralPath $provisioningNetworkOwner -Raw
    foreach ($required in @(
            'esp_netif_napt_disable\s*\(',
            'setup AP isolation unavailable')) {
        if ($provisioningNetworkOwnerText -notmatch $required) {
            $failures += "Provisioning network owner lacks AP client isolation enforcement (${required})"
        }
    }
}

if (Test-Path -LiteralPath $svc) {
    $svcText = Get-Content -LiteralPath $svc -Raw
    foreach ($required in @(
            'verify_ap_client_isolation\s*\(',
            'cannot verify setup AP client isolation')) {
        if ($svcText -notmatch $required) {
            $failures += "provisioning service lacks fail-closed AP isolation gate (${required})"
        }
    }
}

if (-not (Test-Path -LiteralPath $defaults)) {
    $failures += 'missing sdkconfig.defaults for provisioning routing policy'
} else {
    $defaultsText = Get-Content -LiteralPath $defaults -Raw
    foreach ($required in @(
            '#\s*CONFIG_LWIP_IP_FORWARD is not set',
            '#\s*CONFIG_LWIP_IPV4_NAPT is not set',
            '#\s*CONFIG_LWIP_IPV4_NAPT_PORTMAP is not set')) {
        if ($defaultsText -notmatch $required) {
            $failures += "sdkconfig.defaults must explicitly disable provisioning route/NAPT policy (${required})"
        }
    }
}

# One-time pairing codes arrive through this portal and must not be re-emitted
# into serial diagnostics by the later Gateway pairing request.  Preserve
# operational visibility (presence/status) without making exported logs a
# second credential channel.
$gatewayTransport = Join-Path $projectRoot 'main\services\gateway_transport.c'
$gatewayTransportHeader = Join-Path $projectRoot 'main\services\gateway_transport.h'
if (-not (Test-Path -LiteralPath $gatewayTransport)) {
    $failures += 'missing services/gateway_transport.c for pairing-code log policy'
} else {
    $gatewayText = Get-Content -LiteralPath $gatewayTransport -Raw
    if ($gatewayText -match 'pairing request:\s*url=%s\s+client=%s\s+code=%s') {
        $failures += 'gateway pairing request must not log one-time pairing code'
    }
    if ($gatewayText -notmatch 'pairing request:\s*url=%s\s+client=%s\s+code_present=yes') {
        $failures += 'gateway pairing request lacks non-secret pairing diagnostic'
    }
}

# A portal save is a candidate stage, not an irreversible replacement of the
# active device owner.  Keep the Configuration Service's confirmed/staged
# storage and the two independent failure exits (no uplink and Hub rejection)
# structurally visible until dedicated fault-injection coverage is added.
$configurationService = Join-Path $projectRoot 'main\configuration_service.c'
$configurationHeader = Join-Path $projectRoot 'main\configuration_service.h'
$configurationTransaction = Join-Path $projectRoot 'main\configuration_transaction.c'
$configurationTransactionHeader = Join-Path $projectRoot 'main\configuration_transaction.h'
foreach ($path in @($configurationService, $configurationHeader)) {
    if (-not (Test-Path -LiteralPath $path)) {
        $failures += "missing configuration transaction source ($path)"
    }
}
if ((Test-Path -LiteralPath $configurationService) -and
    (Test-Path -LiteralPath $configurationHeader) -and
    (Test-Path -LiteralPath $configurationTransaction) -and
    (Test-Path -LiteralPath $configurationTransactionHeader)) {
    $configurationText = Get-Content -LiteralPath $configurationService -Raw
    $configurationHeaderText = Get-Content -LiteralPath $configurationHeader -Raw
    $configurationTransactionText = Get-Content -LiteralPath $configurationTransaction -Raw
    $configurationTransactionHeaderText = Get-Content -LiteralPath $configurationTransactionHeader -Raw
    foreach ($required in @(
            'configuration_provisioning_transaction_t',
            'configuration_service_stage_provisioning_legacy\s*\(',
            'configuration_service_stage_provisioning\s*\(',
            'configuration_transaction_stage_provisioning_request\s*\(',
            'configuration_transaction_commit_gateway_pairing_token\s*\(',
            'configuration_transaction_boot_snapshot\s*\(',
            'configuration_transaction_begin_staged_boot\s*\(',
            'configuration_transaction_rollback\s*\(',
            'configuration_service_load_boot_candidate',
            'configuration_service_rollback_staged_provisioning',
            'Configuration owns the complete durable pairing transaction')) {
        if ($configurationText -notmatch $required) {
            $failures += "configuration service lacks staged provisioning transaction (${required})"
        }
    }
    foreach ($required in @(
            'configuration_provisioning_transaction_t',
            'configuration_transaction_stage_provisioning_request\s*\(',
            'configuration_transaction_apply_confirmed_policy\s*\(',
            'configuration_transaction_boot_snapshot\s*\(',
            'configuration_transaction_begin_staged_boot\s*\(',
            'configuration_transaction_commit_gateway_pairing_token\s*\(',
            'configuration_transaction_rollback\s*\(')) {
        if ($configurationTransactionHeaderText -notmatch $required -or
            $configurationTransactionText -notmatch $required) {
            $failures += "configuration transaction value contract lacks ${required}"
        }
    }
    foreach ($retired in @(
            'configuration_transaction_stage\s*\(',
            'configuration_transaction_confirm\s*\(')) {
        if ($configurationTransactionHeaderText -match $retired -or
            $configurationTransactionText -match $retired) {
            $failures += "configuration transaction retained bypassable candidate mutation (${retired})"
        }
    }
    foreach ($required in @(
            'configuration_provisioning_request_t',
            'configuration_service_stage_provisioning\s*\(',
            'configuration_service_commit_gateway_pairing_token\s*\(',
            'configuration_service_load_boot_candidate',
            'configuration_service_begin_staged_provisioning_boot',
            'configuration_service_rollback_staged_provisioning')) {
        if ($configurationHeaderText -notmatch $required) {
            $failures += "configuration public contract lacks staged provisioning operation (${required})"
        }
    }
}
foreach ($required in @(
            'configuration_service_load_boot_candidate\s*\(',
            'configuration_service_begin_staged_provisioning_boot\s*\(',
            'configuration_service_commit_gateway_pairing_token\s*\(',
            'candidate retry budget expired',
            'configuration_service_rollback_staged_provisioning\s*\(',
            'startup_runtime_state_service_capture_staged_provisioning\s*\(',
            'startup_runtime_state_service_staged_provisioning_pending\s*\(',
            'unconfirmed provisioning candidate has no uplink')) {
    if ($mainText -notmatch $required) {
        $failures += "composition root lacks staged provisioning connectivity rollback (${required})"
    }
}
if (Test-Path -LiteralPath $gatewayTransport) {
    $gatewayText = Get-Content -LiteralPath $gatewayTransport -Raw
    foreach ($required in @(
            'mbedtls_platform_zeroize\(destination, capacity\)',
            'replace_secret\(s_gateway_token',
            'replace_secret\(s_pair_code',
            'mbedtls_platform_zeroize\(authorization, sizeof\(authorization\)\)',
            'mbedtls_platform_zeroize\(response->data, response->capacity\)')) {
        if ($gatewayText -notmatch $required) {
            $failures += "gateway transport lacks credential zeroization fence (${required})"
        }
    }
    Assert-FileLacks $gatewayTransport '#include\s+"configuration_service\.h"' "gateway transport must not depend directly on Configuration Service"
    Assert-FileLacks $gatewayTransport 'configuration_service_' "gateway transport must receive Configuration evidence through its host contract"
    foreach ($required in @(
            'staged_provisioning_pending\s*\(',
            'rollback_staged_provisioning',
            'GATEWAY_STAGED_PROVISIONING_CONFIRM_DEADLINE_MS',
            'confirmation deadline expired',
            'Pair code rejection is authoritative confirmation')) {
        if ($gatewayText -notmatch $required) {
            $failures += "gateway transport lacks staged provisioning Hub rollback (${required})"
        }
    }
}
Assert-FileLacks $mainC 'configuration_service_set_gateway_token\s*\(' "Gateway token confirmation must use Configuration's staged-candidate operation"
Assert-FileLacks $mainC 'configuration_service_has_staged_provisioning\s*\(' "candidate evidence must be captured during the authoritative boot snapshot load"
Assert-FileLacks $mainC '\bs_pair_code\b' "composition root must not own a pairing-code runtime mirror"
if ($mainText -notmatch 'transport_host_staged_provisioning_pending\s*\(' -or
    $mainText -notmatch 'return\s+startup_runtime_state_service_staged_provisioning_pending\s*\(\s*\)\s*;' -or
    $mainText -notmatch '\.staged_provisioning_pending\s*=\s*transport_host_staged_provisioning_pending') {
    $failures += 'composition root must wire Configuration candidate evidence into Gateway Transport host contract'
}
if ((Test-Path -LiteralPath $gatewayTransportHeader) -and
    ((Get-Content -LiteralPath $gatewayTransportHeader -Raw) -match '\(\*pair_code\)\s*\(' -or
     (Get-Content -LiteralPath $gatewayTransportHeader -Raw) -match '\(\*clear_pair_code\)\s*\(')) {
    $failures += 'Gateway Transport must own only its boot-local pairing-code copy, not request Configuration through host callbacks'
}
if ($gatewayText -and
    ($gatewayText -notmatch 'gateway_transport_pairing_pending\s*\(' -or
     $gatewayText -notmatch 'Configuration remains its durable owner' -or
     $gatewayText -notmatch 'Hub token persistence is the candidate confirmation point' -or
     $gatewayText -notmatch '!paired && staged_confirmation_deadline_us')) {
    $failures += 'Gateway Transport must expose only a non-secret boot-local pairing pending state'
}

if ($failures.Count -gt 0) {
    Write-Error ("provisioning extraction check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output "provisioning extraction check passed: portal HTTP/DNS/restart left main.c, no board_port in new service, AP clients fail closed without forwarding/NAPT"
exit 0
