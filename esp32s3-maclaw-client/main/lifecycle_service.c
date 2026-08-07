#include "lifecycle_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"

static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static device_runtime_snapshot_t s_snapshot = {
    .struct_size = sizeof(device_runtime_snapshot_t),
    .abi_version = DEVICE_RUNTIME_ABI_VERSION,
    .phase = DEVICE_RUNTIME_PHASE_BOOTING,
    .first_failure_phase = DEVICE_RUNTIME_PHASE_BOOTING,
    .first_failure_status = DEVICE_STATUS_OK,
};

void lifecycle_service_begin(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_snapshot = (device_runtime_snapshot_t){
        .struct_size = sizeof(device_runtime_snapshot_t),
        .abi_version = DEVICE_RUNTIME_ABI_VERSION,
        .phase = DEVICE_RUNTIME_PHASE_BOOTING,
        .first_failure_phase = DEVICE_RUNTIME_PHASE_BOOTING,
        .first_failure_status = DEVICE_STATUS_OK,
        .local_services_allowed = false,
    };
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

void lifecycle_service_reach(device_runtime_phase_t phase) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_snapshot.phase != DEVICE_RUNTIME_PHASE_DEGRADED) {
        s_snapshot.phase = phase;
        s_snapshot.local_services_allowed = phase == DEVICE_RUNTIME_PHASE_LOCAL_READY;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

void lifecycle_service_degrade(device_runtime_phase_t failed_phase,
                               device_status_t failure_status) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_snapshot.phase != DEVICE_RUNTIME_PHASE_DEGRADED) {
        s_snapshot.first_failure_phase = failed_phase;
        s_snapshot.first_failure_status = failure_status;
    }
    s_snapshot.phase = DEVICE_RUNTIME_PHASE_DEGRADED;
    s_snapshot.local_services_allowed = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

bool lifecycle_service_get_snapshot(device_runtime_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    *out_snapshot = s_snapshot;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return true;
}
