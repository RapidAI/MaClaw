#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "cJSON.h"
#include "esp_err.h"

// Device-local alarm engine. Epoch milliseconds are authoritative; labels are
// display-only UTF-8 and are truncated to a bounded persisted size.
typedef void (*alarm_manager_ring_callback_t)(void *arg);

// Invoked once immediately before each ringing attempt takes display/audio
// focus. The application callback must remain non-blocking.
void alarm_manager_set_ring_callback(alarm_manager_ring_callback_t callback,
                                     void *arg);
esp_err_t alarm_manager_init(void);
esp_err_t alarm_manager_execute_tool(const char *name, cJSON *arguments,
                                     const char *idempotency_key,
                                     cJSON **out_result, char *error, size_t error_size);
bool alarm_manager_is_ringing(void);
void alarm_manager_dismiss(void);
