#include "services/startup_pet_asset_state_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static SemaphoreHandle_t s_mutex;
static portMUX_TYPE s_init_lock = portMUX_INITIALIZER_UNLOCKED;
static startup_pet_asset_state_snapshot_t s_state;
static uint32_t s_capacity_retry_count;
static bool s_system_sleep_preparing;
static bool s_system_sleep_was_pending;
static bool s_system_sleep_was_preempted_by_audio;

static bool lock_state(void) {
    return s_mutex && xSemaphoreTake(s_mutex, portMAX_DELAY) == pdTRUE;
}

static void next_generation(void) {
    ++s_state.generation;
    if (s_state.generation == 0) ++s_state.generation;
}

device_status_t startup_pet_asset_state_service_init(void) {
    if (s_mutex) return DEVICE_STATUS_OK;
    SemaphoreHandle_t mutex = xSemaphoreCreateMutex();
    if (!mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    taskENTER_CRITICAL(&s_init_lock);
    if (!s_mutex) {
        s_mutex = mutex;
        mutex = NULL;
        memset(&s_state, 0, sizeof(s_state));
        s_capacity_retry_count = 0;
        s_system_sleep_preparing = false;
        s_system_sleep_was_pending = false;
        s_system_sleep_was_preempted_by_audio = false;
        next_generation();
    }
    taskEXIT_CRITICAL(&s_init_lock);
    if (mutex) vSemaphoreDelete(mutex);
    return DEVICE_STATUS_OK;
}

device_status_t startup_pet_asset_state_service_record(
    const pet_asset_descriptor_t *descriptor, bool present, const char *skin) {
    if (!s_mutex || (present && !descriptor) || !lock_state()) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const uint32_t previous_generation = s_state.generation;
    memset(&s_state, 0, sizeof(s_state));
    s_capacity_retry_count = 0;
    s_state.generation = previous_generation;
    next_generation();
    s_state.pending = true;
    s_state.present = present;
    if (present) s_state.descriptor = *descriptor;
    if (skin) strlcpy(s_state.skin, skin, sizeof(s_state.skin));
    xSemaphoreGive(s_mutex);
    return DEVICE_STATUS_OK;
}

bool startup_pet_asset_state_service_snapshot(
    startup_pet_asset_state_snapshot_t *out_snapshot) {
    if (!out_snapshot || !lock_state()) return false;
    *out_snapshot = s_state;
    xSemaphoreGive(s_mutex);
    return true;
}

bool startup_pet_asset_state_service_pending(void) {
    if (!lock_state()) return false;
    const bool pending = s_state.pending;
    xSemaphoreGive(s_mutex);
    return pending;
}

bool startup_pet_asset_state_service_pending_generation(uint32_t generation) {
    if (!generation || !lock_state()) return false;
    const bool admitted = s_state.pending && s_state.generation == generation;
    xSemaphoreGive(s_mutex);
    return admitted;
}

void startup_pet_asset_state_service_set_pending(bool pending) {
    if (!lock_state()) return;
    if (s_state.pending != pending) {
        s_state.pending = pending;
        next_generation();
    }
    xSemaphoreGive(s_mutex);
}

bool startup_pet_asset_state_service_finish_generation(uint32_t generation) {
    if (!generation || !lock_state()) return false;
    const bool finished = s_state.pending && s_state.generation == generation;
    if (finished) {
        s_state.pending = false;
        next_generation();
    }
    xSemaphoreGive(s_mutex);
    return finished;
}

bool startup_pet_asset_state_service_take_capacity_retry(uint32_t generation,
                                                          uint32_t retry_limit,
                                                          uint32_t *out_attempt) {
    if (out_attempt) *out_attempt = 0;
    if (!generation || !retry_limit || !lock_state()) return false;
    const bool accepted = s_state.pending && s_state.generation == generation &&
                          s_capacity_retry_count < retry_limit;
    if (accepted) {
        ++s_capacity_retry_count;
        if (out_attempt) *out_attempt = s_capacity_retry_count;
    }
    xSemaphoreGive(s_mutex);
    return accepted;
}

void startup_pet_asset_state_service_return_capacity_retry(uint32_t generation) {
    if (!generation || !lock_state()) return;
    if (s_state.pending && s_state.generation == generation && s_capacity_retry_count > 0) {
        --s_capacity_retry_count;
    }
    xSemaphoreGive(s_mutex);
}

device_status_t startup_pet_asset_state_service_prepare_system_sleep(void) {
    if (!lock_state()) return DEVICE_STATUS_BUSY;
    if (s_system_sleep_preparing) {
        xSemaphoreGive(s_mutex);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_system_sleep_was_pending = s_state.pending;
    s_system_sleep_was_preempted_by_audio = s_state.preempted_by_audio;
    xSemaphoreGive(s_mutex);
    return DEVICE_STATUS_OK;
}

bool startup_pet_asset_state_service_abort_system_sleep_prepare(
    bool *out_restored_audio_preemption) {
    if (out_restored_audio_preemption) *out_restored_audio_preemption = false;
    if (!lock_state()) return false;
    if (!s_system_sleep_preparing) {
        xSemaphoreGive(s_mutex);
        return false;
    }
    const bool restore_pending = s_system_sleep_was_pending;
    const bool restore_preempted = s_system_sleep_was_preempted_by_audio;
    s_system_sleep_preparing = false;
    s_system_sleep_was_pending = false;
    s_system_sleep_was_preempted_by_audio = false;
    if (s_state.pending != restore_pending ||
        s_state.preempted_by_audio != restore_preempted) {
        s_state.pending = restore_pending;
        s_state.preempted_by_audio = restore_preempted;
        next_generation();
    }
    xSemaphoreGive(s_mutex);
    if (out_restored_audio_preemption) *out_restored_audio_preemption = restore_preempted;
    return true;
}

bool startup_pet_asset_state_service_system_sleep_preparing(void) {
    if (!lock_state()) return false;
    const bool preparing = s_system_sleep_preparing;
    xSemaphoreGive(s_mutex);
    return preparing;
}

bool startup_pet_asset_state_service_preempt_for_audio(bool worker_stopping) {
    if (worker_stopping || !lock_state()) return false;
    const bool preempted = s_state.pending;
    if (preempted) {
        s_state.pending = false;
        s_state.preempted_by_audio = true;
        next_generation();
    }
    xSemaphoreGive(s_mutex);
    return preempted;
}

bool startup_pet_asset_state_service_take_audio_preemption(void) {
    if (!lock_state()) return false;
    const bool preempted = s_state.preempted_by_audio;
    s_state.preempted_by_audio = false;
    xSemaphoreGive(s_mutex);
    return preempted;
}

void startup_pet_asset_state_service_restore(bool pending,
                                             bool preempted_by_audio) {
    if (!lock_state()) return;
    if (s_state.pending != pending ||
        s_state.preempted_by_audio != preempted_by_audio) {
        s_state.pending = pending;
        s_state.preempted_by_audio = preempted_by_audio;
        next_generation();
    }
    xSemaphoreGive(s_mutex);
}

bool startup_pet_asset_state_service_matches_profile(const char *revision,
                                                      const char *skin) {
    if (!revision || !lock_state()) return false;
    const bool matches = s_state.pending && s_state.present &&
                         strcmp(s_state.descriptor.revision, revision) == 0 &&
                         (!skin || strcmp(s_state.skin, skin) == 0);
    xSemaphoreGive(s_mutex);
    return matches;
}
