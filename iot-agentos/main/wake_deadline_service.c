#include "wake_deadline_service.h"

#include <stdbool.h>
#include <string.h>
#include <sys/time.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#define WAKE_DEADLINE_MAX_SLOTS 8u
#define WAKE_DEADLINE_MIN_TIMER_US 1000u
#define WAKE_DEADLINE_TRUSTED_EPOCH_MS 1672531200000LL /* 2023-01-01 UTC */

typedef struct {
    uint16_t generation;
    bool registered;
    bool armed;
    int64_t epoch_ms;
    wake_deadline_callback_t callback;
    void *arg;
} deadline_slot_t;

static const char *TAG = "wake_deadline";
static SemaphoreHandle_t s_lock;
static TaskHandle_t s_task;
static esp_timer_handle_t s_timer;
static SemaphoreHandle_t s_stopped;
static deadline_slot_t s_slots[WAKE_DEADLINE_MAX_SLOTS];
static volatile bool s_initialized;
static volatile bool s_stop_requested;

static int64_t current_epoch_ms(void) {
    struct timeval tv = {0};
    gettimeofday(&tv, NULL);
    return (int64_t)tv.tv_sec * 1000LL + tv.tv_usec / 1000;
}

static bool clock_is_trusted(int64_t epoch_ms) {
    return epoch_ms >= WAKE_DEADLINE_TRUSTED_EPOCH_MS;
}

static bool valid_handle(wake_deadline_handle_t handle, size_t *out_index) {
    if (!handle) return false;
    const size_t index = (size_t)((handle & 0xffu) - 1u);
    const uint16_t generation = (uint16_t)(handle >> 8);
    if (index >= WAKE_DEADLINE_MAX_SLOTS || !generation) return false;
    if (!s_slots[index].registered || s_slots[index].generation != generation) return false;
    if (out_index) *out_index = index;
    return true;
}

/* s_lock is held. */
static void rearm_timer_locked(int64_t now_ms) {
    if (!s_timer || s_stop_requested) return;
    (void)esp_timer_stop(s_timer);
    if (!clock_is_trusted(now_ms)) return;

    int64_t earliest = 0;
    for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
        const deadline_slot_t *slot = &s_slots[i];
        if (!slot->registered || !slot->armed) continue;
        if (!earliest || slot->epoch_ms < earliest) earliest = slot->epoch_ms;
    }
    if (!earliest) return;
    int64_t delay_ms = earliest - now_ms;
    uint64_t delay_us = delay_ms <= 0 ? WAKE_DEADLINE_MIN_TIMER_US
                                      : (uint64_t)delay_ms * 1000u;
    if (delay_us < WAKE_DEADLINE_MIN_TIMER_US) delay_us = WAKE_DEADLINE_MIN_TIMER_US;
    esp_err_t err = esp_timer_start_once(s_timer, delay_us);
    if (err != ESP_OK) ESP_LOGW(TAG, "cannot arm earliest deadline: %s", esp_err_to_name(err));
}

static void timer_callback(void *arg) {
    (void)arg;
    if (s_task && !s_stop_requested) xTaskNotifyGive(s_task);
}

static void deadline_task(void *arg) {
    (void)arg;
    for (;;) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        if (s_stop_requested) break;
        wake_deadline_callback_t callbacks[WAKE_DEADLINE_MAX_SLOTS] = {0};
        void *callback_args[WAKE_DEADLINE_MAX_SLOTS] = {0};
        size_t callback_count = 0;
        int64_t now_ms = current_epoch_ms();
        if (xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
            if (clock_is_trusted(now_ms)) {
                for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
                    deadline_slot_t *slot = &s_slots[i];
                    if (!slot->registered || !slot->armed || slot->epoch_ms > now_ms) continue;
                    slot->armed = false; /* callbacks explicitly re-arm repeating policy. */
                    callbacks[callback_count] = slot->callback;
                    callback_args[callback_count++] = slot->arg;
                }
            }
            rearm_timer_locked(now_ms);
            xSemaphoreGive(s_lock);
        }
        for (size_t i = 0; i < callback_count; ++i) {
            if (callbacks[i]) callbacks[i](callback_args[i]);
        }
    }
    s_task = NULL;
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

esp_err_t wake_deadline_service_init(void) {
    if (s_initialized) return ESP_OK;
    s_lock = xSemaphoreCreateMutex();
    if (!s_lock) return ESP_ERR_NO_MEM;
    s_stopped = xSemaphoreCreateBinary();
    if (!s_stopped) {
        vSemaphoreDelete(s_lock);
        s_lock = NULL;
        return ESP_ERR_NO_MEM;
    }
    esp_timer_create_args_t timer_args = {
        .callback = timer_callback,
        .name = "maclaw_deadline",
    };
    esp_err_t err = esp_timer_create(&timer_args, &s_timer);
    if (err != ESP_OK) {
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_lock);
        s_stopped = NULL;
        s_lock = NULL;
        return err;
    }
    if (xTaskCreate(deadline_task, "maclaw_deadline", 3072, NULL, 6, &s_task) != pdPASS) {
        esp_timer_delete(s_timer);
        s_timer = NULL;
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_lock);
        s_stopped = NULL;
        s_lock = NULL;
        return ESP_ERR_NO_MEM;
    }
    s_initialized = true;
    ESP_LOGI(TAG, "service ready: slots=%u", WAKE_DEADLINE_MAX_SLOTS);
    return ESP_OK;
}

esp_err_t wake_deadline_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_initialized) return ESP_OK;
    if (s_task && xTaskGetCurrentTaskHandle() == s_task) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) return ESP_ERR_TIMEOUT;
    s_stop_requested = true;
    if (s_timer) (void)esp_timer_stop(s_timer);
    TaskHandle_t task = s_task;
    xSemaphoreGive(s_lock);
    if (task) {
        xTaskNotifyGive(task);
        if (xSemaphoreTake(s_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) return ESP_ERR_TIMEOUT;
    }
    if (s_timer) {
        esp_err_t timer_err = esp_timer_delete(s_timer);
        if (timer_err != ESP_OK) return timer_err;
        s_timer = NULL;
    }
    memset(s_slots, 0, sizeof(s_slots));
    vSemaphoreDelete(s_stopped);
    vSemaphoreDelete(s_lock);
    s_stopped = NULL;
    s_lock = NULL;
    s_initialized = false;
    s_stop_requested = false;
    ESP_LOGI(TAG, "service stopped");
    return ESP_OK;
}

esp_err_t wake_deadline_service_register(wake_deadline_callback_t callback, void *arg,
                                         wake_deadline_handle_t *out_handle) {
    if (!callback || !out_handle || !s_initialized || s_stop_requested) return ESP_ERR_INVALID_ARG;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return ESP_ERR_TIMEOUT;
    esp_err_t result = ESP_ERR_NO_MEM;
    for (size_t i = 0; i < WAKE_DEADLINE_MAX_SLOTS; ++i) {
        deadline_slot_t *slot = &s_slots[i];
        if (slot->registered) continue;
        uint16_t generation = (uint16_t)(slot->generation + 1u);
        if (!generation) generation = 1;
        *slot = (deadline_slot_t){
            .generation = generation,
            .registered = true,
            .callback = callback,
            .arg = arg,
        };
        *out_handle = ((uint32_t)generation << 8) | (uint32_t)(i + 1u);
        result = ESP_OK;
        break;
    }
    xSemaphoreGive(s_lock);
    return result;
}

esp_err_t wake_deadline_service_arm(wake_deadline_handle_t handle, int64_t epoch_ms) {
    if (!s_initialized || s_stop_requested || epoch_ms <= 0) return ESP_ERR_INVALID_ARG;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return ESP_ERR_TIMEOUT;
    size_t index = 0;
    if (!valid_handle(handle, &index)) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_NOT_FOUND;
    }
    s_slots[index].epoch_ms = epoch_ms;
    s_slots[index].armed = true;
    rearm_timer_locked(current_epoch_ms());
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

void wake_deadline_service_cancel(wake_deadline_handle_t handle) {
    if (!s_initialized || s_stop_requested ||
        xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    size_t index = 0;
    if (valid_handle(handle, &index)) {
        s_slots[index].armed = false;
        s_slots[index].epoch_ms = 0;
        rearm_timer_locked(current_epoch_ms());
    }
    xSemaphoreGive(s_lock);
}

void wake_deadline_service_unregister(wake_deadline_handle_t handle) {
    if (!s_initialized || s_stop_requested ||
        xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    size_t index = 0;
    if (valid_handle(handle, &index)) {
        deadline_slot_t *slot = &s_slots[index];
        slot->registered = false;
        slot->armed = false;
        slot->epoch_ms = 0;
        slot->callback = NULL;
        slot->arg = NULL;
        rearm_timer_locked(current_epoch_ms());
    }
    xSemaphoreGive(s_lock);
}

void wake_deadline_service_on_wall_clock_updated(void) {
    if (s_task && !s_stop_requested) xTaskNotifyGive(s_task);
}
