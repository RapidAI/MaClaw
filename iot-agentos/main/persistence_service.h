#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

/*
 * Single owner for serialized NVS transactions used by domain services.
 * Callers own their schema and migration, but never a shared NVS mutex or an
 * open handle.  Blob replacement is one NVS commit; callers publish runtime
 * state only after this function returns DEVICE_STATUS_OK.
 */

/* Platform NVS owns initialization and physical transaction serialization.
 * Persistence Service owns only routed request admission and cache-safe stack
 * placement, so schemas never receive a mutex or an open NVS handle. */
device_status_t persistence_service_init(void);
/* Stops the internal-stack flash worker and closes new routed requests. */
device_status_t persistence_service_deinit(uint32_t timeout_ms);
bool persistence_service_is_initialized(void);

/*
 * Internal System Sleep participant. PREPARE closes new persistence requests
 * and waits for every already-admitted NVS transaction to finish, so a later
 * Power commit has a durable checkpoint boundary without exposing NVS handles
 * or flash primitives to Power Service. It deliberately retains the worker:
 * abort is idempotent and restores normal request admission when any later
 * participant rejects the transaction. Neither operation enters MCU sleep.
 */
device_status_t persistence_service_prepare_system_sleep(uint32_t timeout_ms);
void persistence_service_abort_system_sleep_prepare(void);

/* Reads exactly the stored blob into caller-provided storage.  `inout_size`
 * is the capacity on input and actual stored size on success. */
device_status_t persistence_service_read_blob(const char *name_space, const char *key,
                                        void *out_value, size_t *inout_size);
device_status_t persistence_service_write_blob(const char *name_space, const char *key,
                                         const void *value, size_t size);
/* Schema recovery may discard one invalid record, but never receives a raw
 * NVS handle or a namespace-wide erase capability. */
device_status_t persistence_service_erase_key(const char *name_space, const char *key);

/* Transitional typed reads let a domain import a pre-service NVS layout
 * without receiving an NVS handle.  New domain state must be written as a
 * versioned blob through the transactional write API above. */
device_status_t persistence_service_read_i64(const char *name_space, const char *key,
                                       int64_t *out_value);
device_status_t persistence_service_read_i32(const char *name_space, const char *key,
                                       int32_t *out_value);
device_status_t persistence_service_read_u8(const char *name_space, const char *key,
                                      uint8_t *out_value);
/* A small standalone scalar with no cross-field schema (for example a single
 * brightness level) may use this typed write instead of a one-field versioned
 * blob.  Anything with related fields still belongs in a versioned blob. */
device_status_t persistence_service_write_u8(const char *name_space, const char *key,
                                       uint8_t value);
device_status_t persistence_service_read_string(const char *name_space, const char *key,
                                          char *out_value, size_t *inout_size);
