#pragma once

#include <stdbool.h>
#include <stddef.h>

#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

esp_err_t ml307_transport_start(int tx_gpio, int rx_gpio, int baud_rate,
                                int timeout_ms, const char *apn);
bool ml307_transport_is_ready(void);
bool ml307_transport_cancel_foreground(void);

typedef esp_err_t (*ml307_transport_body_reader_t)(
    void *context, void *buffer, size_t requested, size_t *read_bytes);

esp_err_t ml307_transport_http_request(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, const void *body, size_t body_len,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    bool foreground);

esp_err_t ml307_transport_http_request_stream(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, size_t body_len,
    ml307_transport_body_reader_t body_reader, void *body_reader_context,
    void *stream_buffer, size_t stream_buffer_size,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    bool foreground);

#ifdef __cplusplus
}
#endif
