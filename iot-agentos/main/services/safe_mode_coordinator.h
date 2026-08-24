#pragma once

/*
 * Private, value-only coordinator for the minimum SAFE_MODE service set.
 *
 * This object owns neither a board resource nor a normal-startup rollback.
 * The composition root may enter it only after it has independently proven
 * that durable alarm state, Wake Deadline, Display semantic publication,
 * Power/Audio feedback, and their required boot-lifetime adapters are ready.
 * Earlier boot failures therefore remain diagnostics-only DEGRADED failures.
 *
 * Once entry starts, a failed stage is terminal.  In particular this API has
 * no rollback/abort callback: resurrecting a normal worker after a failed
 * safe recovery could let it use a partially-quiesced fault-domain generation.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define SAFE_MODE_COORDINATOR_ABI_VERSION 1u

typedef enum {
    SAFE_MODE_STAGE_IDLE = 0,
    /* Must close and join every non-essential worker which could otherwise
     * publish foreground work, use a retired network root, or contend for
     * display/audio feedback. */
    SAFE_MODE_STAGE_QUIESCE_NONESSENTIAL,
    /* Establishes only the local clock cadence and feedback path; it must not
     * open Gateway, provisioning, media, or general interaction admission. */
    SAFE_MODE_STAGE_INITIALIZE_CLOCK_FEEDBACK,
    /* Starts the durable local alarm scheduler after its persistence/deadline
     * dependencies are confirmed by the composition root. */
    SAFE_MODE_STAGE_INITIALIZE_ALARM,
    SAFE_MODE_STAGE_PUBLISH_DIAGNOSTIC_SURFACE,
    SAFE_MODE_STAGE_COMPLETE,
    SAFE_MODE_STAGE_FAILED,
} safe_mode_stage_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    device_runtime_phase_t failed_phase;
    device_status_t failure_status;
} safe_mode_entry_t;

typedef struct {
    /* Monotonic value only.  All callbacks consume the same parent deadline. */
    uint64_t (*now_ms)(void *context);
    /* Each callback must return OK only when its own bounded operation has
     * completed.  It receives no SDK, RTOS, board or resource handle. */
    device_status_t (*quiesce_nonessential)(void *context, uint32_t timeout_ms);
    device_status_t (*initialize_clock_feedback)(void *context, uint32_t timeout_ms);
    device_status_t (*initialize_alarm)(void *context, uint32_t timeout_ms);
    device_status_t (*publish_diagnostic_surface)(void *context,
                                                   const safe_mode_entry_t *entry,
                                                   uint32_t timeout_ms);
    void *context;
} safe_mode_coordinator_host_t;

typedef struct {
    safe_mode_coordinator_host_t host;
    safe_mode_stage_t stage;
    device_status_t terminal_status;
    bool initialized;
    bool in_progress;
} safe_mode_coordinator_t;

device_status_t safe_mode_coordinator_init(
    safe_mode_coordinator_t *coordinator,
    const safe_mode_coordinator_host_t *host);

/* Runs exactly one fail-closed SAFE_MODE entry transaction.  COMPLETE and
 * FAILED instances require explicit reinitialization against a new proven
 * dependency generation; callers cannot reuse old callback admission. */
device_status_t safe_mode_coordinator_enter(safe_mode_coordinator_t *coordinator,
                                            const safe_mode_entry_t *entry,
                                            uint32_t timeout_ms);

bool safe_mode_coordinator_get_snapshot(const safe_mode_coordinator_t *coordinator,
                                        safe_mode_stage_t *out_stage,
                                        device_status_t *out_terminal_status);
