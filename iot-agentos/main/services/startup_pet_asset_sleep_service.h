#pragma once

/*
 * Reversible System Sleep coordinator for the startup-pet domain.
 *
 * Coordinates only value-level participant order: retained descriptor state,
 * download worker, retry callback and optional cache worker. Each physical
 * owner remains a host callback; this public contract contains no timer,
 * task, Storage, HTTP, renderer or board handle.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    int64_t (*monotonic_time_us)(void *context);
    device_status_t (*prepare_state)(void *context);
    device_status_t (*prepare_worker)(uint32_t timeout_ms, void *context);
    device_status_t (*prepare_retry)(uint32_t timeout_ms, void *context);
    device_status_t (*prepare_cache)(uint32_t timeout_ms, void *context);
    bool (*abort_state)(bool *out_restored_audio_preemption, void *context);
    void (*abort_worker)(void *context);
    void (*abort_retry)(void *context);
    void (*abort_cache)(void *context);
    bool (*server_audio_lease_active)(void *context);
    bool (*take_audio_preemption)(void *context);
    void (*rearm_preempted)(void *context);
    void *context;
} startup_pet_asset_sleep_service_host_t;

/* PREPARE is deliberately fail-closed: after state admission closes, any
 * failed/timed-out descendant remains closed until the root's common reverse
 * rollback explicitly calls ABORT. */
device_status_t startup_pet_asset_sleep_service_prepare(
    const startup_pet_asset_sleep_service_host_t *host, uint32_t timeout_ms);

/* Reopens participants in reverse dependency order only when the retained
 * state participant reports a successful prior PREPARE. An audio preemption
 * restored by that state is consumed only after the physical lease is gone. */
void startup_pet_asset_sleep_service_abort(
    const startup_pet_asset_sleep_service_host_t *host);
