#pragma once

/*
 * Internal physical NVS port.
 *
 * This is the only owner of ESP-IDF NVS initialization, transaction handles
 * and the serializing lock.  Persistence Service owns request admission and
 * the cache-safe worker; domain services own schemas only.  In particular,
 * an NVS format/version fault is reported fail-closed and is never repaired
 * by erasing user data implicitly.
 */

#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

device_status_t platform_nvs_init(void);
/* Call only after Persistence Service has closed request admission and joined
 * its worker.  No physical erase/format is performed during deinitialization. */
device_status_t platform_nvs_deinit(void);
bool platform_nvs_is_initialized(void);

device_status_t platform_nvs_read_blob(const char *name_space, const char *key,
                                       void *out_value, size_t *inout_size);
device_status_t platform_nvs_write_blob(const char *name_space, const char *key,
                                        const void *value, size_t size);
/* Removes one schema record without exposing an NVS handle to callers. */
device_status_t platform_nvs_erase_key(const char *name_space, const char *key);
device_status_t platform_nvs_read_i64(const char *name_space, const char *key,
                                      int64_t *out_value);
device_status_t platform_nvs_read_i32(const char *name_space, const char *key,
                                      int32_t *out_value);
device_status_t platform_nvs_read_u8(const char *name_space, const char *key,
                                     uint8_t *out_value);
device_status_t platform_nvs_write_u8(const char *name_space, const char *key,
                                      uint8_t value);
device_status_t platform_nvs_read_string(const char *name_space, const char *key,
                                         char *out_value, size_t *inout_size);
