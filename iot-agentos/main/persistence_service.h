#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

/*
 * Single owner for serialized NVS transactions used by domain services.
 * Callers own their schema and migration, but never a shared NVS mutex or an
 * open handle.  Blob replacement is one NVS commit; callers publish runtime
 * state only after this function returns ESP_OK.
 */

/* Uses the application's pre-existing NVS transaction mutex during migration,
 * so legacy domains and newly migrated domains cannot commit concurrently. */
esp_err_t persistence_service_init(SemaphoreHandle_t transaction_mutex);
/* Stops the internal-stack flash worker and closes new routed requests.  It
 * does not delete the transaction mutex supplied by the composition root. */
esp_err_t persistence_service_deinit(uint32_t timeout_ms);
bool persistence_service_is_initialized(void);

/* Reads exactly the stored blob into caller-provided storage.  `inout_size`
 * is the capacity on input and actual stored size on success. */
esp_err_t persistence_service_read_blob(const char *name_space, const char *key,
                                        void *out_value, size_t *inout_size);
esp_err_t persistence_service_write_blob(const char *name_space, const char *key,
                                         const void *value, size_t size);

/* Transitional typed reads let a domain import a pre-service NVS layout
 * without receiving an NVS handle.  New domain state must be written as a
 * versioned blob through the transactional write API above. */
esp_err_t persistence_service_read_i64(const char *name_space, const char *key,
                                       int64_t *out_value);
esp_err_t persistence_service_read_i32(const char *name_space, const char *key,
                                       int32_t *out_value);
esp_err_t persistence_service_read_u8(const char *name_space, const char *key,
                                      uint8_t *out_value);
/* A small standalone scalar with no cross-field schema (for example a single
 * brightness level) may use this typed write instead of a one-field versioned
 * blob.  Anything with related fields still belongs in a versioned blob. */
esp_err_t persistence_service_write_u8(const char *name_space, const char *key,
                                       uint8_t value);
esp_err_t persistence_service_read_string(const char *name_space, const char *key,
                                          char *out_value, size_t *inout_size);
