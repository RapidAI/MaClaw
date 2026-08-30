#include "services/media_transfer_service.h"

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static portMUX_TYPE s_priority_lock = portMUX_INITIALIZER_UNLOCKED;
static media_transfer_service_host_t s_host;
static SemaphoreHandle_t s_lane_mutex;
static uint32_t s_wake_memory_lease_count;
static bool s_server_audio_wake_lease_active;
static bool s_audio_download_active;
static bool s_initialized;

static bool host_valid(const media_transfer_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->stop_wake_word_for_media &&
           host->cancel_startup_pet_for_server_audio &&
           host->take_startup_pet_audio_preemption &&
           host->rearm_preempted_startup_pet && host->schedule_wake_restart;
}

device_status_t media_transfer_service_init(
    const media_transfer_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);

    SemaphoreHandle_t lane = xSemaphoreCreateMutex();
    if (!lane) return DEVICE_STATUS_RESOURCE_EXHAUSTED;

    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        s_host = *host;
        s_lane_mutex = lane;
        s_wake_memory_lease_count = 0;
        s_server_audio_wake_lease_active = false;
        s_initialized = true;
        taskEXIT_CRITICAL(&s_lock);
        taskENTER_CRITICAL(&s_priority_lock);
        s_audio_download_active = false;
        taskEXIT_CRITICAL(&s_priority_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);
    vSemaphoreDelete(lane);
    return DEVICE_STATUS_OK;
}

bool media_transfer_service_begin_server_audio_wake_lease(const char *source) {
    media_transfer_service_host_t host = {0};
    bool acquired = false;
    bool stop_wake = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized && !s_server_audio_wake_lease_active) {
        s_server_audio_wake_lease_active = true;
        stop_wake = s_wake_memory_lease_count == 0;
        ++s_wake_memory_lease_count;
        host = s_host;
        acquired = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!acquired) return false;

    /* Prevent a cold-start frame request from taking the lane while the audio
     * request is waiting for recognizer teardown. This host action owns the
     * startup descriptor state and runs outside the service lock. */
    host.cancel_startup_pet_for_server_audio(host.context);
    if (stop_wake) host.stop_wake_word_for_media(source, host.context);
    return true;
}

bool media_transfer_service_finish_server_audio_wake_lease(void) {
    media_transfer_service_host_t host = {0};
    bool final_owner = false;
    bool completed = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized && s_server_audio_wake_lease_active) {
        s_server_audio_wake_lease_active = false;
        if (s_wake_memory_lease_count > 0) {
            --s_wake_memory_lease_count;
            final_owner = s_wake_memory_lease_count == 0;
        }
        host = s_host;
        completed = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!completed) return false;

    /* The descriptor state has its own lock. Never call it while the media
     * lock is held, otherwise the renderer/network callbacks could invert
     * lock order with a future media request. */
    if (host.take_startup_pet_audio_preemption(host.context)) {
        host.rearm_preempted_startup_pet(host.context);
    }
    return final_owner;
}

void media_transfer_service_begin_optional_wake_lease(const char *source) {
    media_transfer_service_host_t host = {0};
    bool stop_wake = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        stop_wake = s_wake_memory_lease_count == 0;
        ++s_wake_memory_lease_count;
        host = s_host;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (stop_wake) host.stop_wake_word_for_media(source, host.context);
}

void media_transfer_service_finish_optional_wake_lease(void) {
    media_transfer_service_host_t host = {0};
    bool final_owner = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized && s_wake_memory_lease_count > 0) {
        --s_wake_memory_lease_count;
        final_owner = s_wake_memory_lease_count == 0;
        host = s_host;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (final_owner) host.schedule_wake_restart(host.context);
}

bool media_transfer_service_server_audio_wake_lease_active(void) {
    bool active = false;
    taskENTER_CRITICAL(&s_lock);
    active = s_initialized && s_server_audio_wake_lease_active;
    taskEXIT_CRITICAL(&s_lock);
    return active;
}

void media_transfer_service_set_audio_download_active(bool active) {
    taskENTER_CRITICAL(&s_priority_lock);
    if (s_initialized) s_audio_download_active = active;
    taskEXIT_CRITICAL(&s_priority_lock);
}

bool media_transfer_service_audio_download_active(void) {
    bool active = false;
    taskENTER_CRITICAL(&s_priority_lock);
    active = s_initialized && s_audio_download_active;
    taskEXIT_CRITICAL(&s_priority_lock);
    return active;
}

device_status_t media_transfer_service_take_lane(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    SemaphoreHandle_t lane = NULL;
    taskENTER_CRITICAL(&s_lock);
    lane = s_initialized ? s_lane_mutex : NULL;
    taskEXIT_CRITICAL(&s_lock);
    if (!lane) return DEVICE_STATUS_UNAVAILABLE;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    return xSemaphoreTake(lane, ticks) == pdTRUE
               ? DEVICE_STATUS_OK
               : DEVICE_STATUS_TIMEOUT;
}

void media_transfer_service_release_lane(void) {
    SemaphoreHandle_t lane = NULL;
    taskENTER_CRITICAL(&s_lock);
    lane = s_initialized ? s_lane_mutex : NULL;
    taskEXIT_CRITICAL(&s_lock);
    if (lane) xSemaphoreGive(lane);
}
