#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

#define MEETING_RECOVERY_RECORDING_ID_CAPACITY 96u

typedef struct {
    bool pending;
    int32_t next_chunk;
    int32_t phase;
    char recording_id[MEETING_RECOVERY_RECORDING_ID_CAPACITY];
} meeting_recovery_snapshot_t;

esp_err_t meeting_recovery_service_init(void);
esp_err_t meeting_recovery_service_load(meeting_recovery_snapshot_t *out_snapshot);
esp_err_t meeting_recovery_service_save(const meeting_recovery_snapshot_t *snapshot);
