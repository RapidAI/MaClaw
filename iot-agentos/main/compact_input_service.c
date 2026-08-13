#include "compact_input_service.h"

/* Exactly one source owner includes the selected compact input adapter. */
#include "boards/compact_input_adapter.h"

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

static bool s_startup_selector_latched;
static compact_input_publish_cb_t s_publish;
static void *s_publish_context;
static TaskHandle_t s_scanner_task;
static SemaphoreHandle_t s_scanner_stopped;
static bool s_scanner_stop_requested;

esp_err_t compact_input_service_initialize(void) { return compact_input_adapter_init(); }
void compact_input_service_read_raw(compact_input_raw_state_t *out_state) {
    compact_input_adapter_read_raw(out_state);
}
bool compact_input_service_has_volume_keys(void) {
    return compact_input_adapter_has_volume_keys();
}
int64_t compact_input_service_activate_debounce_us(void) {
    return compact_input_adapter_activate_debounce_us();
}
int64_t compact_input_service_volume_debounce_us(void) {
    return compact_input_adapter_volume_debounce_us();
}
int64_t compact_input_service_long_press_us(void) {
    return compact_input_adapter_long_press_us();
}
int64_t compact_input_service_double_click_us(void) {
    return compact_input_adapter_double_click_us();
}
const char *compact_input_service_name(void) { return compact_input_adapter_name(); }

static void compact_input_service_publish(device_input_action_t action,
                                          device_input_source_t source) {
    if (s_publish) s_publish(action, source, s_publish_context);
}

static void compact_input_service_scanner_task(void *arg) {
    (void)arg;
    compact_input_raw_state_t raw_state = {0};
    compact_input_service_read_raw(&raw_state);
    bool previous = raw_state.activate_released;
    bool activate_raw = previous;
    int64_t activate_changed_at = 0;
    const bool has_volume_keys = compact_input_service_has_volume_keys();
    bool volume_up_raw = raw_state.volume_up_released;
    bool volume_down_raw = raw_state.volume_down_released;
    bool volume_up_stable = volume_up_raw;
    bool volume_down_stable = volume_down_raw;
    int64_t volume_up_changed_at = 0;
    int64_t volume_down_changed_at = 0;
    int64_t pressed_at = 0;
    int64_t short_pending_at = 0;
    bool long_sent = false;
    while (true) {
        int64_t now = esp_timer_get_time();
        compact_input_service_read_raw(&raw_state);
        bool activate_level = raw_state.activate_released;
        if (activate_level != activate_raw) {
            activate_raw = activate_level;
            activate_changed_at = now;
        }
        /* The activate key has no hardware debounce. Raw contact bounce fired
         * same-millisecond phantom repeats and could split one click into a
         * double. Accept a new level only after it holds for the selected
         * profile's qualified debounce interval. */
        bool released = previous;
        if (activate_raw != previous &&
            now - activate_changed_at >= compact_input_service_activate_debounce_us()) {
            released = activate_raw;
        }
        if (previous && !released) {
            pressed_at = now;
            long_sent = false;
            compact_input_service_publish(DEVICE_INPUT_CONTACT_DOWN,
                                          DEVICE_INPUT_SOURCE_PRIMARY_CONTROL);
        }
        if (!released && pressed_at && !long_sent &&
            now - pressed_at >= compact_input_service_long_press_us()) {
            long_sent = true;
            short_pending_at = 0;
            ESP_LOGI("compact_input", "activate long hold detected");
            compact_input_service_publish(DEVICE_INPUT_CONFIGURE,
                                          DEVICE_INPUT_SOURCE_PRIMARY_CONTROL);
        }
        if (!previous && released && pressed_at) {
            int64_t duration = now - pressed_at;
            if (long_sent || duration >= compact_input_service_long_press_us()) {
                short_pending_at = 0;
            } else if (short_pending_at &&
                       now - short_pending_at <= compact_input_service_double_click_us()) {
                short_pending_at = 0;
                compact_input_service_publish(DEVICE_INPUT_SECONDARY,
                                              DEVICE_INPUT_SOURCE_PRIMARY_CONTROL);
            } else {
                short_pending_at = now;
            }
            pressed_at = 0;
        }
        previous = released;
        if (short_pending_at &&
            now - short_pending_at > compact_input_service_double_click_us()) {
            short_pending_at = 0;
            compact_input_service_publish(DEVICE_INPUT_PRIMARY,
                                          DEVICE_INPUT_SOURCE_PRIMARY_CONTROL);
        }

        if (has_volume_keys) {
            bool volume_up_released = raw_state.volume_up_released;
            if (volume_up_released != volume_up_raw) {
                volume_up_raw = volume_up_released;
                volume_up_changed_at = now;
            }
            if (volume_up_stable != volume_up_raw && volume_up_changed_at &&
                now - volume_up_changed_at >= compact_input_service_volume_debounce_us()) {
                volume_up_stable = volume_up_raw;
                ESP_LOGI("compact_input", "volume up control level=%d",
                         volume_up_stable ? 1 : 0);
                if (volume_up_stable) {
                    compact_input_service_publish(DEVICE_INPUT_VOLUME_UP,
                                                  DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL);
                }
            }
            bool volume_down_released = raw_state.volume_down_released;
            if (volume_down_released != volume_down_raw) {
                volume_down_raw = volume_down_released;
                volume_down_changed_at = now;
            }
            if (volume_down_stable != volume_down_raw && volume_down_changed_at &&
                now - volume_down_changed_at >= compact_input_service_volume_debounce_us()) {
                volume_down_stable = volume_down_raw;
                ESP_LOGI("compact_input", "volume down control level=%d",
                         volume_down_stable ? 1 : 0);
                if (volume_down_stable) {
                    compact_input_service_publish(DEVICE_INPUT_VOLUME_DOWN,
                                                  DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL);
                }
            }
        }
        /* Keep the established 20 ms scanner cadence; profile debounce and
         * double-click windows retain their existing externally observed
         * timing even though task ownership moved below the Input HAL. */
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(20)) != 0) break;
    }
    s_publish = NULL;
    s_publish_context = NULL;
    if (s_scanner_stopped) xSemaphoreGive(s_scanner_stopped);
    vTaskDelete(NULL);
}

esp_err_t compact_input_service_prepare_scanner(void) {
    if (s_scanner_task || s_scanner_stopped) return ESP_ERR_INVALID_STATE;
    s_scanner_stopped = xSemaphoreCreateBinary();
    return s_scanner_stopped ? ESP_OK : ESP_ERR_NO_MEM;
}

esp_err_t compact_input_service_start_scanner(compact_input_publish_cb_t publish,
                                              void *context) {
    if (!publish || !s_scanner_stopped || s_scanner_task) return ESP_ERR_INVALID_STATE;
    s_publish = publish;
    s_publish_context = context;
    s_scanner_stop_requested = false;
    if (compact_input_adapter_start_scan_task(compact_input_service_scanner_task,
                                              &s_scanner_task) != pdPASS) {
        s_publish = NULL;
        s_publish_context = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

esp_err_t compact_input_service_stop_scanner(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_scanner_task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == s_scanner_task) return ESP_ERR_INVALID_STATE;
    if (!s_scanner_stop_requested) {
        s_scanner_stop_requested = true;
        xTaskNotifyGive(s_scanner_task);
    }
    if (!s_scanner_stopped ||
        xSemaphoreTake(s_scanner_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        ESP_LOGW("compact_input", "timed out stopping board input scanner");
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_scanner_stopped);
    s_scanner_stopped = NULL;
    s_scanner_task = NULL;
    s_scanner_stop_requested = false;
    ESP_LOGI("compact_input", "board input scanner stopped");
    return ESP_OK;
}

void compact_input_service_discard_unpublished_scanner_state(void) {
    if (s_scanner_task) return;
    if (s_scanner_stopped) vSemaphoreDelete(s_scanner_stopped);
    s_scanner_stopped = NULL;
    s_scanner_stop_requested = false;
    s_publish = NULL;
    s_publish_context = NULL;
}

/* A profile can reserve its activate key during a bounded pre-scanner
 * transport-selection gesture.  The selected adapter supplies only raw
 * contact/policy facts; this service retains the generic gesture state. */
void compact_input_service_run_startup_selector(void) {
    const uint32_t window_ms = compact_input_adapter_startup_selector_window_ms();
    s_startup_selector_latched = false;
    if (window_ms == 0) return;
    bool released = compact_input_adapter_activate_is_released_now();
    bool previous_released = released;
    int64_t pressed_at = 0;
    int64_t first_release_at = 0;
    const int64_t deadline = esp_timer_get_time() + (int64_t)window_ms * 1000;
    ESP_LOGI("compact_input", "startup selector active for %u ms", (unsigned)window_ms);
    while (esp_timer_get_time() < deadline && !s_startup_selector_latched) {
        const int64_t now = esp_timer_get_time();
        released = compact_input_adapter_activate_is_released_now();
        if (previous_released && !released) {
            pressed_at = now;
        } else if (!previous_released && released && pressed_at) {
            const int64_t duration = now - pressed_at;
            pressed_at = 0;
            if (duration < compact_input_adapter_long_press_us()) {
                if (first_release_at &&
                    now - first_release_at <= compact_input_adapter_double_click_us()) {
                    s_startup_selector_latched = true;
                    break;
                }
                first_release_at = now;
            } else {
                first_release_at = 0;
            }
        }
        if (first_release_at &&
            now - first_release_at > compact_input_adapter_double_click_us()) {
            first_release_at = 0;
        }
        previous_released = released;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    ESP_LOGI("compact_input", "startup selector closed: %s",
             s_startup_selector_latched ? "toggle" : "unchanged");
}

bool compact_input_service_consume_startup_selector_result(uint32_t window_ms) {
    (void)window_ms;
    const bool requested = s_startup_selector_latched;
    s_startup_selector_latched = false;
    return requested;
}
bool compact_input_service_response_paging_uses_volume_keys(void) {
    return compact_input_adapter_response_paging_uses_volume_keys();
}
