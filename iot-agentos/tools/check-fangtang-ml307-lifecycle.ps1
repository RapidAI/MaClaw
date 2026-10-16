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
$genericHttpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\http_client.cc'
$genericHttpH = Join-Path $projectRoot 'managed_components\78__esp-ml307\include\http_client.h'
$mqttC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_mqtt.cc'
$tcpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_tcp.cc'
$udpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_udp.cc'
$ecMqttC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_mqtt.cc'
$ecTcpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_tcp.cc'
$ecUdpC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_udp.cc'
$ecSslC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_ssl.cc'
$espMqttC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\esp\esp_mqtt.cc'
$mlAtModemC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ml307\ml307_at_modem.cc'
$ecAtModemC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\ec801e\ec801e_at_modem.cc'
$webSocketC = Join-Path $projectRoot 'managed_components\78__esp-ml307\src\web_socket.cc'
$webSocketH = Join-Path $projectRoot 'managed_components\78__esp-ml307\include\web_socket.h'
$failures = @()
foreach ($path in @($transportH,$transportC,$uartH,$uartC,$modemH,$modemC,$httpC,$genericHttpC,$genericHttpH,$mqttC,$tcpC,$udpC,$ecMqttC,$ecTcpC,$ecUdpC,$ecSslC,$espMqttC,$mlAtModemC,$ecAtModemC,$webSocketC,$webSocketH)) {
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
    $genericHttp = Get-Content -LiteralPath $genericHttpC -Raw
    $genericHttpHeader = Get-Content -LiteralPath $genericHttpH -Raw
    $mqtt = Get-Content -LiteralPath $mqttC -Raw
    $tcp = Get-Content -LiteralPath $tcpC -Raw
    $udp = Get-Content -LiteralPath $udpC -Raw
    $ecMqtt = Get-Content -LiteralPath $ecMqttC -Raw
    $ecTcp = Get-Content -LiteralPath $ecTcpC -Raw
    $ecUdp = Get-Content -LiteralPath $ecUdpC -Raw
    $ecSsl = Get-Content -LiteralPath $ecSslC -Raw
    $espMqtt = Get-Content -LiteralPath $espMqttC -Raw
    $mlAtModem = Get-Content -LiteralPath $mlAtModemC -Raw
    $ecAtModem = Get-Content -LiteralPath $ecAtModemC -Raw
    $webSocket = Get-Content -LiteralPath $webSocketC -Raw
    $webSocketHeader = Get-Content -LiteralPath $webSocketH -Raw
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
    if ($shutdownBody -notmatch '(?s)esp_timer_get_time\(\).*?remaining_us.*?xSemaphoreTake\(\s*receive_task_stopped_.*?remaining_ticks\(\).*?xSemaphoreTake\(\s*event_task_stopped_.*?remaining_ticks\(\)') {
        $failures += 'AtUart shutdown must use one monotonic parent deadline for both worker joins'
    }
    if ($uart -notmatch '(?s)DmaRxCallback.*?shutdown_requested_\.load\(\).*?ReturnBuffer\(data\.buffer\)') {
        $failures += 'AtUart DMA callback must return late buffers without touching retired queues'
    }
    if ($uart -notmatch '(?s)DmaOverflowCallback.*?shutdown_requested_\.load\(\).*?event_group_handle_') {
        $failures += 'AtUart overflow ISR must not signal a retired event group during shutdown'
    }
    if ($uartHeader -notmatch 'recursive_mutex\s+urc_mutex_' -or
        $uart -notmatch '(?s)void AtUart::HandleUrc.*?recursive_mutex.*?\+\+urc_dispatch_depth_.*?\(\*iterator\)\(command, arguments\)' -or
        $uart -notmatch '(?s)void AtUart::UnregisterUrcCallback.*?\*iterator\s*=\s*UrcCallback\{\}') {
        $failures += 'AtUart URC registry must serialize callbacks and defer removal safely during dispatch'
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
    if ($uartHeader -notmatch 'AT_UART_RX_FRAME_MAX' -or
        $uart -notmatch '(?s)rx_frame_overflowed_.*?AT_UART_RX_FRAME_MAX' -or
        $uart -notmatch '(?s)rx_frame_overflowed_\.exchange') {
        $failures += 'AtUart RX reassembly must enforce a bounded frame and explicit overflow error'
    }
    if ($uart -notmatch '(?s)split_at_arguments\s*\(.*?quoted.*?tokens' -or
        $uart -notmatch '(?s)split_at_arguments\(values,\s*&tokens\)') {
        $failures += 'AtUart URC parser must preserve quoted comma fields and reject unterminated quotes'
    }
    if ($uartHeader -notmatch 'AT_UART_MAX_ARGUMENTS' -or
        $uart -notmatch '(?s)AT_UART_MAX_ARGUMENT_LENGTH.*?AT_UART_MAX_ARGUMENTS') {
        $failures += 'AtUart URC parser must cap argument count and token length'
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
    if ($transport -notmatch '(?s)MHTTPCREATE.*?arguments\.size\(\) != 1u.*?kHttpSlotCount') {
        $failures += 'Fangtang cellular HTTP create URC must enforce one valid slot id'
    }
    if ($transport -notmatch '(?s)MHTTPURC.*?arguments\.size\(\) < 2u.*?arguments\.size\(\) > 6u') {
        $failures += 'Fangtang cellular HTTP URC must reject malformed arity before indexing'
    }
    if ($transport -notmatch '(?s)arguments\.size\(\) != 5u.*?status_code_') {
        $failures += 'Fangtang cellular HTTP header URC must require exact metadata arity'
    }
    if ($transport -notmatch '(?s)body_offset_.*?current_length.*?received_length') {
        $failures += 'Fangtang cellular HTTP content URC must enforce cumulative body length'
    }
    if ($transport -notmatch '(?s)current_length > 0\).*?arguments\.size\(\) != 5u') {
        $failures += 'Fangtang cellular HTTP zero-length content URC must reject an extra payload field'
    }
    if ($transport -notmatch '(?s)http_id_\.store\(-1\)') {
        $failures += 'Fangtang cellular HTTP close must retire the slot id before reuse'
    }
    if ($transport -notmatch '(?s)c < 0x20u \|\| c == 0x7fu') {
        $failures += 'Fangtang cellular HTTP URL parser must reject authority control characters'
    }
    if ($transport -notmatch '(?s)body_len > 0 && !body && !body_reader') {
        $failures += 'Fangtang cellular HTTP must reject non-empty requests without a body source'
    }
    if ($transport -notmatch '(?s)header_value_safe.*?\\r.*?\\n.*?"') {
        $failures += 'Fangtang cellular HTTP headers must reject AT command delimiter injection'
    }
    if ($transport -notmatch '(?s)parse_http_header_framing.*?from_chars' -or
        $transport -notmatch '(?s)parse_http_header_framing\(decoded') {
        $failures += 'Fangtang cellular HTTP response headers must use bounded framing parser'
    }
    if ($transport -notmatch '(?s)token_count != 1u.*?chunked') {
        $failures += 'Fangtang cellular HTTP Transfer-Encoding must reject coding chains and unknown tokens'
    }
    if ($transport -notmatch '(?s)body_forbidden_\s*=\s*false' -or
        $transport -notmatch '(?s)body_forbidden_\s*=\s*method_\s*==\s*"HEAD"' -or
        $transport -notmatch '(?s)body_forbidden_.*?received_length != 0') {
        $failures += 'Fangtang cellular HTTP must fence body-forbidden responses and content URCs'
    }
    if ($transport -notmatch '(?s)content_length_seen.*?from_chars' -or
        $transport -notmatch '(?s)content_length_seen.*?length != content_length_value') {
        $failures += 'Fangtang cellular HTTP must reject conflicting Content-Length headers'
    }
    if ($transport -notmatch '(?s)header_name_safe.*?[:\\?]') {
        $failures += 'Fangtang cellular HTTP header names must reject delimiter characters'
    }
    if ($transport -notmatch '(?s)authority\.find_first_of\("@\\\\"\)') {
        $failures += 'Fangtang cellular HTTP URL authority must reject userinfo and backslash ambiguity'
    }
    if ($http -notmatch '(?s)ReadAll\(\).*?error_code_ = 255.*?Close\(\)') {
        $failures += 'Ml307Http ReadAll timeout/error must close the owned modem slot'
    }
    if ($http -notmatch '(?s)body_forbidden_\s*=\s*method_\s*==\s*"HEAD"' -or
        $http -notmatch '(?s)status_code == 101' -or
        $http -notmatch '(?s)interim_response_') {
        $failures += 'Ml307Http must fence body-forbidden statuses and interim responses'
    }
    if ($http -notmatch '(?s)!headers_received.*?body_forbidden') {
        $failures += 'Ml307Http content URC must require final headers and reject bodyless payloads'
    }
    if ($http -notmatch '(?s)token_count != 1u.*?chunked' -or
        $http -notmatch '(?s)content_length_seen.*?content_length_value') {
        $failures += 'Ml307Http response framing must reject coding chains and conflicting lengths'
    }
    if ($http -notmatch '(?s)buffer_size == 0u\) return 0') {
        $failures += 'Ml307Http zero-length writes must not inject synthetic CRLF payloads'
    }
    if ($genericHttp -notmatch '(?s)protocol_ != "http".*?protocol_ != "https"' -or
        $genericHttp -notmatch '(?s)ParseHeaderLine.*?http_header_name_safe' -or
        $genericHttp -notmatch '(?s)parse_http_size\(cl_it->second.value' -or
        $genericHttp -notmatch '(?s)CHUNK_DATA_CRLF.*?ParseHeaderLine' -or
        $genericHttp -notmatch '(?s)buffer_size > 0u && !buffer') {
        $failures += 'Generic EC801E HTTP client must enforce URL/header/length/chunk and buffer boundaries'
    }
    if ($genericHttpHeader -notmatch 'CHUNK_DATA_CRLF' -or
        $genericHttpHeader -notmatch 'content_length_present_' -or
        $genericHttpHeader -notmatch 'body_forbidden_' -or
        $genericHttpHeader -notmatch 'interim_response_') {
        $failures += 'Generic EC801E HTTP client state must retain explicit chunk CRLF and Content-Length presence'
    }
    if ($genericHttp -notmatch '(?s)status == 101' -or
        $genericHttp -notmatch '(?s)status == 204' -or
        $genericHttp -notmatch '(?s)status == 304' -or
        $genericHttp -notmatch '(?s)http_transfer_encoding_is_chunked') {
        $failures += 'Generic EC801E HTTP client must fence body-forbidden statuses and Transfer-Encoding tokens'
    }
    if ($genericHttpHeader -notmatch 'HTTP_RX_BUFFER_MAX' -or
        $genericHttp -notmatch '(?s)OnTcpData\s*\([^\)]*const std::string& data[^\)]*\).*?HTTP_RX_BUFFER_MAX.*?SetError\(\)') {
        $failures += 'Generic HTTP parser must bound undecoded TCP reassembly before append'
    }
    if ($genericHttpHeader -notmatch '(?s)bool\s+ParseRegularBody\s*\(' -or
        $genericHttpHeader -notmatch '(?s)bool\s+AddBodyData\s*\(') {
        $failures += 'Generic EC801E HTTP body queue helpers must report backpressure failure explicitly'
    }
    $bodyQueueStart = $genericHttp.IndexOf('bool HttpClient::AddBodyData(')
    if ($bodyQueueStart -ge 0) {
        $bodyQueueEnd = $genericHttp.IndexOf('std::string HttpClient::ReadAll()', $bodyQueueStart)
        if ($bodyQueueEnd -lt 0) { $bodyQueueEnd = $genericHttp.Length }
        $bodyQueue = $genericHttp.Substring($bodyQueueStart, $bodyQueueEnd - $bodyQueueStart)
        if ($bodyQueue -match 'SetError\s*\(') {
            $failures += 'Generic EC801E body queue helpers must not mutate terminal error state while applying backpressure'
        }
    } else {
        $failures += 'Generic EC801E body queue helpers must be implemented as bool-returning functions'
    }
    if ($genericHttp -notmatch '(?s)if\s*\(!?queued\).*?SetError\s*\(' -or
        $genericHttp -notmatch '(?s)if\s*\(!ParseRegularBody\(rx_buffer_,\s*state_lock\).*?SetError\s*\(') {
        $failures += 'Generic EC801E body parsers must handle queue backpressure failure at the state-machine boundary'
    }
    $onTcpDataMatch = [regex]::Match($genericHttp, '(?s)void\s+HttpClient::OnTcpData\s*\([^\{]+\{(.*?)\n\}')
    if ($onTcpDataMatch.Success -and $onTcpDataMatch.Groups[1].Value -match 'lock_guard\s*<\s*std::mutex\s*>\s+lock\s*\(mutex_\)') {
        $failures += 'Generic EC801E TCP callback must not hold client mutex while applying body backpressure'
    }
    if ($genericHttpHeader -notmatch 'parse_mutex_' -or
        $genericHttp -notmatch '(?s)OnTcpData\s*\([^\{]+\{.*?parse_mutex_') {
        $failures += 'Generic EC801E parser callbacks must have an independent serialization lock'
    }
    if ($genericHttp -notmatch '(?s)OnTcpDisconnected\s*\([^\{]+\{.*?connected_\s*=\s*false;.*?write_cv_\.notify_all') {
        $failures += 'Generic EC801E disconnect must wake body producers before parser-lock drain'
    }
    if ($genericHttp -notmatch '(?s)std::string HttpClient::ReadAll\(\).*?Read\(chunk\.data\(\),\s*chunk\.size\(\)\)') {
        $failures += 'Generic EC801E ReadAll must drain the bounded body queue incrementally'
    }
    if ($genericHttp -notmatch '(?s)void HttpClient::ResetRequestState\(\).*?parse_mutex_.*?mutex_') {
        $failures += 'Generic EC801E request reset must serialize parser and response state locks'
    }
    if ($genericHttp -notmatch '(?s)HttpClient::~HttpClient\(\).*?parse_mutex_.*?tcp_->OnStream\(\{\}\).*?tcp_->OnDisconnected\(\{\}\)') {
        $failures += 'Generic EC801E destructor must retire parser state before detaching TCP callbacks'
    }
    if ($genericHttpHeader -notmatch 'callback_admission_' -or
        $genericHttp -notmatch '(?s)HttpClient::~HttpClient\(\).*?callback_admission_.*?store\(false' -or
        $genericHttp -notmatch '(?s)tcp_->OnStream\(\[this[^\]]*\].*?callback_admission_.*?load') {
        $failures += 'Generic EC801E HTTP callbacks must have an explicit retirement admission fence'
    }
    if ($genericHttpHeader -notmatch 'event_group_valid_' -or
        $genericHttp -notmatch '(?s)HttpClient::HttpClient.*?event_group_valid_(?:\s*=\s*event_group_handle_ != nullptr|\.store\(event_group_handle_ != nullptr)' -or
        $genericHttp -notmatch '(?s)Open\(const std::string& method, const std::string& url\).*?event_group_valid_') {
        $failures += 'Generic HTTP client must fence EventGroup allocation and readiness before Open'
    }
    if ($genericHttp -notmatch '(?s)content_length_present_\s*&&\s*content_length_\s*==\s*0.*?Unexpected bytes after zero-length response') {
        $failures += 'Generic EC801E zero-length responses must reject trailing payload bytes'
    }
    if ($mqtt -notmatch 'DecodeHexAppend' -or $mqtt -notmatch 'arguments\[6\]\.type') {
        $failures += 'Ml307Mqtt publish URC must type-check and validate hex payloads'
    }
    if ($mqtt -notmatch '(?s)xEventGroupCreate\(\).*?Failed to allocate MQTT event group' -or
        $mqtt -notmatch 'valid_mqtt_text' -or $mqtt -notmatch 'urc_callback_registered_') {
        $failures += 'Ml307Mqtt must fence event-group allocation, AT text inputs, and callback retirement'
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
    if ($ecMqtt -notmatch '(?s)xEventGroupCreate\(\).*?Failed to allocate MQTT event group' -or
        $ecMqtt -notmatch 'valid_mqtt_text' -or $ecMqtt -notmatch 'urc_callback_registered_') {
        $failures += 'Ec801EMqtt must fence event-group allocation, AT text inputs, and callback retirement'
    }
    if ($espMqtt -notmatch 'event_group_valid_' -or
        $espMqtt -notmatch '(?s)esp_mqtt_client_init.*?== nullptr' -or
        $espMqtt -notmatch '(?s)esp_mqtt_client_register_event.*?!= ESP_OK' -or
        $espMqtt -notmatch '(?s)esp_mqtt_client_start.*?!= ESP_OK') {
        $failures += 'ESP MQTT must fence EventGroup/client/event registration/start failures'
    }
    if ($mlAtModem -notmatch '(?s)CreateTcp.*?event_group_valid_.*?IsInitialized' -or
        $ecAtModem -notmatch '(?s)CreateTcp.*?event_group_valid_.*?IsInitialized') {
        $failures += 'Modem transport factories must reject unready UART/lifecycle owners'
    }
    if ($webSocket -notmatch 'handshake_event_group_valid_' -or
        $webSocket -notmatch '(?s)Connect\(const char\* uri\).*?uri \|\| !\*uri' -or
        $webSocket -notmatch '(?s)rfind\("HTTP/1\.1 101 ' -or
        $webSocket -notmatch '(?s)payload_length > 125u' -or
        $webSocket -notmatch '(?s)!connected_.*?!tcp_.*?!data') {
        $failures += 'WebSocket must fence event-group/URI/handshake/frame and payload ownership boundaries'
    }
}
if ($failures.Count -gt 0) {
    Write-Error ("Fangtang ML307 lifecycle check failed:`n" + ($failures -join "`n"))
    exit 1
}
Write-Output 'Fangtang ML307 lifecycle check passed: borrower drain, callback retirement, DMA/task join, and explicit reinitialize are fenced'
