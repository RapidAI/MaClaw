#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

esp_err_t ml307_transport_start(int tx_gpio, int rx_gpio, int baud_rate,
                                int timeout_ms, const char *apn);
bool ml307_transport_is_ready(void);
bool ml307_transport_cancel_foreground(void);
bool ml307_transport_cancel_requests_for_owner(const void *owner);
/* Closes new start/HTTP admission and joins only transport-owned background
 * coordination. It never destroys the modem/UART while a request borrower
 * might still hold it; a timeout leaves the transport intact and returns an
 * error rather than pretending the cellular service is fully stopped. */
esp_err_t ml307_transport_quiesce(uint32_t timeout_ms);
/* Terminally drains and destroys the modem/UART generation.  No GPIO power
 * rail is changed here; the Fangtang profile owns that electrical action.
 * The call is idempotent when no modem generation exists. */
esp_err_t ml307_transport_deinit(uint32_t timeout_ms);
/* Recreates a fresh modem/UART generation after a successful deinit.  This is
 * intentionally one bounded operation so callers cannot reopen admission
 * while the previous generation is still retiring. */
esp_err_t ml307_transport_reinitialize(int tx_gpio, int rx_gpio, int baud_rate,
                                       int timeout_ms, const char *apn);
/* Future System Sleep needs the same bounded probe/HTTP safe point as a
 * shutdown, but must be able to return to the exact pre-PREPARE generation
 * when a later participant rejects the transaction.  These functions never
 * deinitialize the modem or UART and remain private to the Fangtang adapter. */
esp_err_t ml307_transport_prepare_system_sleep(uint32_t timeout_ms);
void ml307_transport_abort_system_sleep_prepare(void);

typedef esp_err_t (*ml307_transport_body_reader_t)(
    void *context, void *buffer, size_t requested, size_t *read_bytes);

esp_err_t ml307_transport_http_request(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, const void *body, size_t body_len,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    const void *cancellation_owner, bool foreground);

esp_err_t ml307_transport_http_request_stream(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, size_t body_len,
    ml307_transport_body_reader_t body_reader, void *body_reader_context,
    void *stream_buffer, size_t stream_buffer_size,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    const void *cancellation_owner, bool foreground);

#ifdef __cplusplus
}
#endif
