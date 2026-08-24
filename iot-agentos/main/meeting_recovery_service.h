#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define MEETING_RECOVERY_RECORDING_ID_CAPACITY 96u

typedef struct {
    bool pending;
    int32_t next_chunk;
    int32_t phase;
    char recording_id[MEETING_RECOVERY_RECORDING_ID_CAPACITY];
} meeting_recovery_snapshot_t;

device_status_t meeting_recovery_service_init(void);
/* Closes recovery metadata admission and drains the synchronous Persistence
 * calls already in flight. It neither owns the meeting worker nor SPIFFS WAV. */
device_status_t meeting_recovery_service_deinit(uint32_t timeout_ms);
bool meeting_recovery_service_is_initialized(void);
/* Internal System Sleep participant. It closes new recovery metadata
 * load/save admission and drains calls already routed to Persistence. ABORT
 * restores the same service generation without creating or resuming a
 * meeting worker. */
device_status_t meeting_recovery_service_prepare_system_sleep(uint32_t timeout_ms);
void meeting_recovery_service_abort_system_sleep_prepare(void);
device_status_t meeting_recovery_service_load(meeting_recovery_snapshot_t *out_snapshot);
device_status_t meeting_recovery_service_save(const meeting_recovery_snapshot_t *snapshot);
