#pragma once

/*
 * Cold-start pet-asset admission coordinator.
 *
 * Owns only value-level eligibility decisions around an already retained
 * descriptor: display adaptation, optional-capacity recovery, bounded retry
 * accounting, worker admission, and server-audio re-arm. Timers, workers,
 * Gateway state, Display, Storage and physical media cancellation remain
 * host-owned callbacks.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/pet_asset_service.h"
#include "services/startup_pet_asset_state_service.h"

typedef enum {
    STARTUP_PET_ASSET_ADMISSION_NO_ACTION = 0,
    STARTUP_PET_ASSET_ADMISSION_RETRY_SCHEDULED,
    STARTUP_PET_ASSET_ADMISSION_STARTED,
    STARTUP_PET_ASSET_ADMISSION_FINISHED,
    STARTUP_PET_ASSET_ADMISSION_REARMED,
} startup_pet_asset_admission_result_t;

typedef struct {
    uint32_t struct_size;
    bool (*snapshot)(startup_pet_asset_state_snapshot_t *out_state, void *context);
    bool (*stop_requested)(void *context);
    bool (*system_sleep_preparing)(void *context);
    bool (*prepare_for_display)(const pet_asset_descriptor_t *source,
                                pet_asset_descriptor_t *out_display,
                                void *context);
    bool (*capacity_available)(const pet_asset_descriptor_t *descriptor,
                               void *context);
    bool (*drop_stale_cache)(const pet_asset_descriptor_t *descriptor,
                             void *context);
    bool (*take_capacity_retry)(uint32_t generation, uint32_t retry_limit,
                                uint32_t *out_attempt, void *context);
    void (*return_capacity_retry)(uint32_t generation, void *context);
    bool (*schedule_retry)(void *context);
    void (*finish_generation)(uint32_t generation, void *context);
    bool (*worker_active)(void *context);
    bool (*gateway_operational)(void *context);
    device_status_t (*start_worker)(void *context);
    bool (*revision_installed)(const pet_asset_descriptor_t *descriptor,
                               void *context);
    void (*set_pending)(bool pending, void *context);
    void *context;
} startup_pet_asset_admission_service_host_t;

/* Decides whether the pending startup descriptor may begin its asynchronous
 * transaction. On capacity pressure, consumes at most `retry_limit` retries.
 * A failed timer arm returns that reservation before terminal completion. */
startup_pet_asset_admission_result_t
startup_pet_asset_admission_service_admit_pending(
    const startup_pet_asset_admission_service_host_t *host,
    uint32_t retry_limit, uint32_t *out_retry_attempt,
    device_status_t *out_start_status);

/* Reopens an audio-preempted descriptor only if it is still present, has not
 * been replaced or installed, and the host can either start a worker now or
 * safely schedule the hand-off retry while an old worker unwinds. */
startup_pet_asset_admission_result_t
startup_pet_asset_admission_service_rearm_preempted(
    const startup_pet_asset_admission_service_host_t *host);
