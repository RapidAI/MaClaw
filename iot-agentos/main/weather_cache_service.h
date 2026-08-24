#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define WEATHER_CACHE_SUMMARY_CAPACITY 24u
#define WEATHER_CACHE_LOCATION_CAPACITY 24u

typedef struct {
    char summary[WEATHER_CACHE_SUMMARY_CAPACITY];
    char location[WEATHER_CACHE_LOCATION_CAPACITY];
    int32_t temperature_c;
    int64_t expires_at_ms;
    bool valid;
} weather_cache_snapshot_t;

/* The cache is advisory UI state, never a source of weather truth.  It owns
 * schema/versioning and persistence; callers only exchange snapshots. */
device_status_t weather_cache_service_init(void);
/* Closes cache read/write admission and drains the synchronous Persistence
 * calls already in flight. The cache owns no NVS handle or worker. */
device_status_t weather_cache_service_deinit(uint32_t timeout_ms);
bool weather_cache_service_is_initialized(void);
/* Internal System Sleep participant. It closes advisory weather-cache
 * load/save admission and drains calls already routed to Persistence. ABORT
 * restores the same service generation and does not invent a weather update
 * or a display submission. */
device_status_t weather_cache_service_prepare_system_sleep(uint32_t timeout_ms);
void weather_cache_service_abort_system_sleep_prepare(void);
device_status_t weather_cache_service_load(weather_cache_snapshot_t *out_snapshot);
device_status_t weather_cache_service_save(const weather_cache_snapshot_t *snapshot);
