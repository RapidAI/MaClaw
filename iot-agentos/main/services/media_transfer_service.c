#include "services/media_transfer_service.h"

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "esp_timer.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static portMUX_TYPE s_priority_lock = portMUX_INITIALIZER_UNLOCKED;
static media_transfer_service_host_t s_host;
static SemaphoreHandle_t s_lane_mutex;
/* A FreeRTOS mutex must only be returned by the task that acquired it. Keep
 * the owner in the service so a stale/duplicate composition-root cleanup
 * cannot give the lane from another task and let two transfers overlap. */
static TaskHandle_t s_lane_owner;
static uint32_t s_wake_memory_lease_count;
#define MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES 8u
/* Optional leases may be nested by more than one worker. Keep one slot per
 * successful acquisition so a late cleanup from another task cannot consume
 * a lease it did not acquire. */
static TaskHandle_t s_optional_wake_lease_owner[
    MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES];
static bool s_server_audio_wake_lease_active;
static TaskHandle_t s_server_audio_wake_lease_owner;
static bool s_audio_download_active;
static bool s_system_sleep_preparing;
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
        s_lane_owner = NULL;
        s_wake_memory_lease_count = 0;
        for (size_t i = 0; i < MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES; ++i) {
            s_optional_wake_lease_owner[i] = NULL;
        }
        s_server_audio_wake_lease_active = false;
        s_server_audio_wake_lease_owner = NULL;
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
    if (s_initialized && !s_system_sleep_preparing && !s_server_audio_wake_lease_active) {
        s_server_audio_wake_lease_active = true;
        s_server_audio_wake_lease_owner = xTaskGetCurrentTaskHandle();
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
    if (s_initialized && s_server_audio_wake_lease_active &&
        s_server_audio_wake_lease_owner == xTaskGetCurrentTaskHandle()) {
        s_server_audio_wake_lease_active = false;
        s_server_audio_wake_lease_owner = NULL;
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

bool media_transfer_service_begin_optional_wake_lease(const char *source) {
    media_transfer_service_host_t host = {0};
    bool stop_wake = false;
    bool acquired = false;
    TaskHandle_t current = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    size_t free_slot = MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES;
    if (s_initialized && !s_system_sleep_preparing) {
        for (size_t i = 0; i < MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES; ++i) {
            if (s_optional_wake_lease_owner[i] == NULL) {
                free_slot = i;
                break;
            }
        }
    }
    if (free_slot < MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES) {
        s_optional_wake_lease_owner[free_slot] = current;
        stop_wake = s_wake_memory_lease_count == 0;
        ++s_wake_memory_lease_count;
        host = s_host;
        acquired = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (stop_wake) host.stop_wake_word_for_media(source, host.context);
    return acquired;
}

bool media_transfer_service_finish_optional_wake_lease(void) {
    media_transfer_service_host_t host = {0};
    bool final_owner = false;
    bool completed = false;
    TaskHandle_t current = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    size_t owned_slot = MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES;
    if (s_initialized) {
        for (size_t i = 0; i < MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES; ++i) {
            if (s_optional_wake_lease_owner[i] == current) {
                owned_slot = i;
                break;
            }
        }
    }
    if (owned_slot < MEDIA_TRANSFER_MAX_OPTIONAL_WAKE_LEASES &&
        s_wake_memory_lease_count > 0) {
        s_optional_wake_lease_owner[owned_slot] = NULL;
        --s_wake_memory_lease_count;
        final_owner = s_wake_memory_lease_count == 0;
        host = s_host;
        completed = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!completed) return false;
    if (final_owner) host.schedule_wake_restart(host.context);
    return final_owner;
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
    lane = (s_initialized && !s_system_sleep_preparing) ? s_lane_mutex : NULL;
    taskEXIT_CRITICAL(&s_lock);
    if (!lane) return DEVICE_STATUS_UNAVAILABLE;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    if (xSemaphoreTake(lane, ticks) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    TaskHandle_t current = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_lock);
    /* The mutex serializes successful takers; still fail closed if lifecycle
     * state changed between the snapshot above and ownership publication. */
    if (!s_initialized || s_lane_mutex != lane || s_lane_owner != NULL) {
        taskEXIT_CRITICAL(&s_lock);
        (void)xSemaphoreGive(lane);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    s_lane_owner = current;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t media_transfer_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return !s_initialized ? DEVICE_STATUS_UNAVAILABLE : DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lock);
    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        const bool drained = s_lane_owner == NULL &&
                             !s_server_audio_wake_lease_active &&
                             s_wake_memory_lease_count == 0;
        taskEXIT_CRITICAL(&s_lock);
        if (drained) return DEVICE_STATUS_OK;
        if (esp_timer_get_time() >= deadline_us) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(1);
    }
}

void media_transfer_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lock);
}

void media_transfer_service_release_lane(void) {
    SemaphoreHandle_t lane = NULL;
    TaskHandle_t owner = NULL;
    taskENTER_CRITICAL(&s_lock);
    lane = s_initialized ? s_lane_mutex : NULL;
    owner = s_lane_owner;
    if (lane && owner == xTaskGetCurrentTaskHandle()) {
        s_lane_owner = NULL;
    } else {
        lane = NULL;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (lane) xSemaphoreGive(lane);
}
