[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$transportH = Join-Path $projectRoot 'main\boards\fangtang_4g\fangtang_ml307_transport.h'
$transportC = Join-Path $projectRoot 'main\boards\fangtang_4g\fangtang_ml307_transport.cpp'
$uartH = Join-Path $projectRoot 'managed_components\78__esp-ml307\include\at_uart.h'
$uartC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\at_uart.cc'
$modemH = Join-Path $projectRoot 'managed_components\78__esp-ml307\include\at_modem.h'
$modemC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\at_modem.cc'
$httpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_http.cc'
$mqttC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_mqtt.cc'
$tcpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_tcp.cc'
$udpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_udp.cc'
$ecMqttC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_mqtt.cc'
$ecTcpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_tcp.cc'
$ecUdpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_udp.cc'
$ecSslC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_ssl.cc'
$failures = @()
foreach ($path in @($transportH,$transportC,$uartH,$uartC,$modemH,$modemC,$httpC,$mqttC,$tcpC,$udpC,$ecMqttC,$ecTcpC,$ecUdpC,$ecSslC)) {
    if (-not (Test-Path -LiteralPath $path)) { $failures += "missing $path" }
}
if ($failures.Count -eq 0) {
    $transport = Get-Content -LiteralPath $transportC -Raw
    $transportHeader = Get-Content -LiteralPath $transportH -Raw
    $uart = Get-Content -LiteralPath $uartC -Raw
    $uartHeader = Get-Content -LiteralPath $uartH -Raw
    $modem = Get-Content -LiteralPath $modemC -Raw
    $modemHeader = Get-Content -LiteralPath $modemH -Raw
    $http = Get-Content -LiteralPath $httpC -Raw
    $mqtt = Get-Content -LiteralPath $mqttC -Raw
    $tcp = Get-Content -LiteralPath $tcpC -Raw
    $udp = Get-Content -LiteralPath $udpC -Raw
    $ecMqtt = Get-Content -LiteralPath $ecMqttC -Raw
    $ecTcp = Get-Content -LiteralPath $ecTcpC -Raw
    $ecUdp = Get-Content -LiteralPath $ecUdpC -Raw
    $ecSsl = Get-Content -LiteralPath $ecSslC -Raw
    foreach ($pair in @(
        @($transportHeader,'ml307_transport_deinit\s*\('),
        @($transportHeader,'ml307_transport_reinitialize\s*\('),
        @($transport,'close_transport_and_drain\s*\('),
        @($transport,'s_modem\.reset\s*\(\)'),
        @($transport,'ml307_transport_start\s*\('),
        @($uartHeader,'bool\s+Shutdown\s*\('),
        @($uart,'uart_uhci_\.StopReceive\s*\(\)'),
        @($uart,'receive_task_stopped_'),
        @($uart,'event_task_stopped_'),
        @($uart,'shutdown_requested_'),
        @($uart,'item\.empty\(\)'),
        @($uart,'arguments\.empty\(\)'),
        @($uartHeader,'bool\s+DecodeHexAppend\s*\('),
        @($modemHeader,'bool\s+Shutdown\s*\('),
        @($http,'Malformed MHTTPURC'),
        @($http,'Malformed MHTTPCREATE'),
        @($http,'parse_content_length'),
        @($http,'awaiting_create_'),
        @($modemHeader,'bool\s+Shutdown\s*\('),
        @($modem,'UnregisterUrcCallback\s*\('),
        @($modem,'at_uart_->Shutdown\s*\('))) {
        if ($pair[0] -notmatch $pair[1]) { $failures += "ML307 lifecycle requirement missing: $($pair[1])" }
    }
    if ($transport -notmatch '(?s)close_transport_and_drain\s*\(timeout_ms\).*?s_modem\.reset\s*\(\)') {
        $failures += 'ML307 deinit must drain borrowers before destroying modem/UART generation'
    }
    if ($uart -notmatch '(?s)StopReceive\s*\(\).*?xSemaphoreTake\(receive_task_stopped_') {
        $failures += 'AtUart shutdown must stop DMA before joining receive task'
    }
    $shutdownBodyMatch = [regex]::Match($uart, '(?s)bool\s+AtUart::Shutdown\s*\([^\{]+\{(.*?)\n\}')
    $shutdownBody = if ($shutdownBodyMatch.Success) { $shutdownBodyMatch.Groups[1].Value } else { '' }
    $shutdownPos = $shutdownBody.IndexOf('shutdown_requested_.store(true)')
    $waitMatch = [regex]::Match($shutdownBody, 'xSemaphoreTake\(\s*receive_task_stopped_')
    $waitPos = if ($waitMatch.Success) { $waitMatch.Index } else { -1 }
    if ($shutdownPos -lt 0 -or $waitPos -lt 0 -or $shutdownPos -gt $waitPos) {
        $failures += 'AtUart shutdown flag must be published before waiting for worker termination'
    }
    if ($shutdownBody -notmatch '(?s)shutdown_requested_\.store\(true\).*?xEventGroupSetBits\(event_group_handle_') {
        $failures += 'AtUart shutdown must explicitly wake EventTask via event-group bit'
    }
    if ($uart -notmatch '(?s)Shutdown\(1000\).*?xSemaphoreTake\(receive_task_stopped_.*?portMAX_DELAY') {
        $failures += 'AtUart destructor must not release resources while workers may still be alive'
    }
    if ($modem -notmatch '(?s)UnregisterUrcCallback\s*\(urc_callback_\).*?at_uart_->Shutdown') {
        $failures += 'AtModem shutdown must unregister URC callback before UART shutdown'
    }
    if ($uart -notmatch '(?s)item\.empty\(\).*?argument\.string_value\.clear\(\)') {
        $failures += 'AtUart parser must handle empty URC fields without indexing the token'
    }
    if ($uart -notmatch '(?s)command == "CME ERROR".*?arguments\.empty\(\)') {
        $failures += 'AtUart CME ERROR handling must reject malformed empty argument lists'
    }
    if ($http -notmatch '(?s)command == "MHTTPURC".*?arguments\.size\(\) < 2u') {
        $failures += 'Ml307Http must validate MHTTPURC minimum arity before indexing'
    }
    if ($http -notmatch '(?s)type == "content".*?arguments\.size\(\) < 5u') {
        $failures += 'Ml307Http content URC must reject truncated metadata'
    }
    if ($uart -notmatch '(?s)DecodeHexAppend\s*\(.*?\(length & 1u\).*?CharToHex') {
        $failures += 'AtUart hex decoder must reject odd-length or non-hex data before decoding'
    }
    if ($uart -notmatch '(?s)kInd\[\].*?MHTTPURC: .*?ind' -or
        $uart -notmatch '(?s)rx_buffer_\.size\(\) == kIndLength.*?rx_buffer_\.append' -or
        $uart -notmatch '(?s)rx_buffer_\[kIndLength\] == .\+.*?rx_buffer_\.insert\(kIndLength' -or
        $uart -notmatch '(?s)else \{\s*return false;\s*\}\s*end_pos = kIndLength') {
        $failures += 'AtUart must synthesize missing CRLF only for an exact MHTTPURC ind token or a precisely bounded next-URC boundary'
    }
    if ($http -notmatch '(?s)Malformed encoded HTTP headers.*?ML307_HTTP_EVENT_ERROR' -or
        $http -notmatch '(?s)Malformed encoded HTTP content.*?ML307_HTTP_EVENT_ERROR') {
        $failures += 'Ml307Http must fail closed on malformed encoded headers or body data'
    }
    if ($http -notmatch '(?s)decoded_data\.size\(\).*?current_length') {
        $failures += 'Ml307Http must verify decoded body length against modem-reported chunk length'
    }
    if ($http -notmatch 'arguments\.size\(\) != 1u' -or
        $http -notmatch 'created_id\s*>=\s*4') {
        $failures += 'Ml307Http must constrain MHTTPCREATE to one valid modem slot id'
    }
    if ($http -notmatch '(?s)parse_content_length.*?from_chars') {
        $failures += 'Ml307Http Content-Length parsing must be bounded and non-throwing'
    }
    if ($http -notmatch '(?s)s_http_create_mutex.*?awaiting_create_\s*=\s*true') {
        $failures += 'Ml307Http MHTTPCREATE claim must be serialized with an explicit await gate'
    }
    if ($http -notmatch '(?s)create_lock\.unlock\(\)') {
        $failures += 'Ml307Http must release create serialization after slot claim'
    }
    if ($http -notmatch '(?s)protocol_ != "http".*?protocol_ != "https"') {
        $failures += 'Ml307Http URL parser must restrict schemes to HTTP/HTTPS'
    }
    if ($http -notmatch '(?s)timeout_seconds.*?MHTTPCFG=.*?timeout') {
        $failures += 'Ml307Http must apply a non-zero modem timeout derived from caller timeout'
    }
    if ($http -notmatch '(?s)keep_alive_.*?Connection.*?keep-alive') {
        $failures += 'Ml307Http keep-alive must map to a protocol Connection header'
    }
    if ($http -notmatch '(?s)if \(urc_http_id == http_id_\).*?type == "header"') {
        $failures += 'Ml307Http must route malformed-response handling only to the matching HTTP slot'
    }
    if ($http -notmatch '(?s)error_code_ >= 0.*?return -1') {
        $failures += 'Ml307Http Read must fail closed after an attributed error event'
    }
    if ($mqtt -notmatch 'DecodeHexAppend' -or $mqtt -notmatch 'arguments\[6\]\.type') {
        $failures += 'Ml307Mqtt publish URC must type-check and validate hex payloads'
    }
    if ($tcp -notmatch 'DecodeHexAppend' -or $tcp -notmatch 'arguments\[3\]\.type') {
        $failures += 'Ml307Tcp receive URC must type-check and validate hex payloads'
    }
    if ($udp -notmatch 'DecodeHexAppend' -or $udp -notmatch 'arguments\[3\]\.type') {
        $failures += 'Ml307Udp receive URC must type-check and validate hex payloads'
    }
    foreach ($pair in @(
        @($ecMqtt,'DecodeHexAppend'), @($ecMqtt,'arguments\[0\]\.type'),
        @($ecTcp,'DecodeHexAppend'), @($ecTcp,'arguments\[0\]\.type'),
        @($ecUdp,'DecodeHexAppend'), @($ecUdp,'arguments\[0\]\.type'),
        @($ecSsl,'DecodeHexAppend'), @($ecSsl,'arguments\[0\]\.type'))) {
        if ($pair[0] -notmatch $pair[1]) { $failures += "EC801E URC safety requirement missing: $($pair[1])" }
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Fangtang ML307 lifecycle check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Fangtang ML307 lifecycle check passed: borrower drain, callback retirement, DMA/task join, and explicit reinitialize are fenced'
