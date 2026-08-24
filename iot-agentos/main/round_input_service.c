#include "round_input_service.h"

#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "round_input_profile_service.h"
#include "round_peripheral_service.h"

static const char *TAG = "maclaw_round_input";

static round_input_service_publish_cb_t s_on_button;
static void *s_on_press_arg;
static TaskHandle_t s_button_task;
static SemaphoreHandle_t s_button_task_stopped;
static bool s_button_task_stop_requested;
/* System Sleep retains this boot-lifetime scanner, but no I2C touch or GPIO
 * sample may overlap a future profile electrical prepare. */
static SemaphoreHandle_t s_button_task_system_sleep_quiesced;
static bool s_button_task_system_sleep_preparing;
static portMUX_TYPE s_button_task_system_sleep_lock = portMUX_INITIALIZER_UNLOCKED;
static volatile bool s_command_cancel_enabled;
static volatile uint32_t s_command_gesture_revision;
/* Once a double tap is emitted, discard residual native/raw touch reports
 * until the panel has been continuously released. */
static volatile bool s_touch_gesture_consumed;
static int64_t s_touch_gesture_released_at_us;

static void emit_button_input(device_input_action_t action,
                              device_input_source_t source) {
    if (round_input_profile_service_consume_boot_gesture(action, source)) return;
    if (s_on_button) s_on_button(action, source, s_on_press_arg);
}

static void button_task(void *arg) {
    (void)arg;
    const round_input_profile_t *input_profile = round_input_profile_service_profile();
    bool button_pressed = round_input_profile_service_activate_key_pressed();
    bool panel_pressed = false;
    round_peripheral_service_touch_read(&panel_pressed, NULL);
    bool pressed = button_pressed || panel_pressed;
    device_input_source_t gesture_source = round_input_profile_service_resolve_source(
        button_pressed, panel_pressed);
    device_input_source_t pending_source = DEVICE_INPUT_SOURCE_UNKNOWN;
    int64_t pressed_at_us = pressed ? esp_timer_get_time() : 0;
    int64_t released_at_us = 0;
    bool long_sent = false;
    bool short_pending = false;
    bool native_double_sent = false;
    uint32_t command_gesture_revision = s_command_gesture_revision;
    ESP_LOGI(TAG, "interaction monitor ready: idle_pressed=%s touch=%s",
             button_pressed ? "yes" : "no",
             round_peripheral_service_touch_ready() ? "yes" : "no");
    while (true) {
        taskENTER_CRITICAL(&s_button_task_system_sleep_lock);
        const bool system_sleep_preparing = s_button_task_system_sleep_preparing;
        taskEXIT_CRITICAL(&s_button_task_system_sleep_lock);
        if (system_sleep_preparing) {
            /* PREPARE raises this marker before waking us. Reaching this
             * branch proves the previous scan has completed and prevents a
             * new touch/I2C or activate-key read until ABORT. */
            if (s_button_task_system_sleep_quiesced) {
                xSemaphoreGive(s_button_task_system_sleep_quiesced);
            }
            for (;;) {
                (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
                taskENTER_CRITICAL(&s_button_task_system_sleep_lock);
                const bool still_preparing = s_button_task_system_sleep_preparing;
                taskEXIT_CRITICAL(&s_button_task_system_sleep_lock);
                if (s_button_task_stop_requested) goto stopped;
                if (!still_preparing) break;
            }
            continue;
        }
        bool now_button_pressed = round_input_profile_service_activate_key_pressed();
        bool now_panel_pressed = false;
        uint8_t now_touch_gesture = 0;
        round_peripheral_service_touch_read(&now_panel_pressed, &now_touch_gesture);
        bool now_pressed = now_button_pressed || now_panel_pressed;
        int64_t now_us = esp_timer_get_time();
        if (command_gesture_revision != s_command_gesture_revision) {
            command_gesture_revision = s_command_gesture_revision;
            pressed = now_pressed;
            gesture_source = round_input_profile_service_resolve_source(
                now_button_pressed, now_panel_pressed);
            pending_source = DEVICE_INPUT_SOURCE_UNKNOWN;
            pressed_at_us = now_pressed ? now_us : 0;
            released_at_us = 0;
            long_sent = false;
            short_pending = false;
            native_double_sent = false;
            s_touch_gesture_consumed = false;
            s_touch_gesture_released_at_us = 0;
            ESP_LOGI(TAG, "fresh command-cancel gesture window armed");
            vTaskDelay(pdMS_TO_TICKS(input_profile->scan_poll_ms));
            continue;
        }
        if (s_touch_gesture_consumed) {
            short_pending = false;
            native_double_sent = true;
            if (now_panel_pressed) {
                s_touch_gesture_released_at_us = 0;
            } else if (s_touch_gesture_released_at_us == 0) {
                s_touch_gesture_released_at_us = now_us;
            } else if (now_us - s_touch_gesture_released_at_us >=
                       (int64_t)input_profile->touch_release_drain_ms * 1000) {
                s_touch_gesture_consumed = false;
                s_touch_gesture_released_at_us = 0;
                native_double_sent = false;
                ESP_LOGD(TAG, "touch gesture drain complete");
            }
        }
        if (now_panel_pressed &&
            round_peripheral_service_touch_is_native_double_tap(now_touch_gesture) &&
            !native_double_sent) {
            short_pending = false;
            native_double_sent = true;
            s_touch_gesture_consumed = true;
            s_touch_gesture_released_at_us = 0;
            ESP_LOGI(TAG, "button gesture: native touch double");
            if (s_on_button) {
                s_on_button(DEVICE_INPUT_CONTACT_DOWN, DEVICE_INPUT_SOURCE_TOUCH,
                            s_on_press_arg);
                s_on_button(DEVICE_INPUT_SECONDARY, DEVICE_INPUT_SOURCE_TOUCH,
                            s_on_press_arg);
            }
        }
        if (now_pressed != pressed) {
            vTaskDelay(pdMS_TO_TICKS(input_profile->debounce_ms));
            now_button_pressed = round_input_profile_service_activate_key_pressed();
            now_touch_gesture = 0;
            round_peripheral_service_touch_read(&now_panel_pressed, &now_touch_gesture);
            now_pressed = now_button_pressed || now_panel_pressed;
            if (now_pressed != pressed) {
                now_us = esp_timer_get_time();
                pressed = now_pressed;
                if (pressed) {
                    pressed_at_us = now_us;
                    long_sent = false;
                    gesture_source = round_input_profile_service_resolve_source(
                        now_button_pressed, now_panel_pressed);
                    if (!round_peripheral_service_touch_is_native_double_tap(now_touch_gesture)) {
                        native_double_sent = false;
                    }
                    ESP_LOGI(TAG, "button/touch down");
                    if (!round_peripheral_service_touch_is_native_double_tap(now_touch_gesture)) {
                        emit_button_input(DEVICE_INPUT_CONTACT_DOWN, gesture_source);
                    }
                } else {
                    uint32_t held_ms = pressed_at_us > 0
                                           ? (uint32_t)((now_us - pressed_at_us) / 1000)
                                           : 0;
                    ESP_LOGI(TAG, "button/touch up: held=%lu ms", (unsigned long)held_ms);
                    uint32_t minimum_tap_ms =
                        (gesture_source == DEVICE_INPUT_SOURCE_TOUCH &&
                         s_command_cancel_enabled)
                            ? input_profile->touch_cancel_min_tap_ms
                            : input_profile->touch_regular_min_tap_ms;
                    if (!long_sent &&
                        input_profile->local_volume_decrease_hold_ms > 0 &&
                        held_ms >= input_profile->local_volume_decrease_hold_ms) {
                        /* Round touch/key hardware maps its profile-declared
                         * alternate control here. Business policy receives the
                         * same normalized volume intent as Bread's side key;
                         * it never learns a controller, coordinate, or hold
                         * interval. The longer configuration hold was already
                         * emitted while pressed and remains unchanged. */
                        short_pending = false;
                        pending_source = DEVICE_INPUT_SOURCE_UNKNOWN;
                        ESP_LOGI(TAG, "local volume decrease hold: %s",
                                 gesture_source == DEVICE_INPUT_SOURCE_TOUCH ?
                                     "touch" : "button");
                        emit_button_input(DEVICE_INPUT_VOLUME_DOWN, gesture_source);
                    } else if (!long_sent &&
                               input_profile->local_volume_increase_hold_ms > 0 &&
                               held_ms >= input_profile->local_volume_increase_hold_ms) {
                        short_pending = false;
                        pending_source = DEVICE_INPUT_SOURCE_UNKNOWN;
                        ESP_LOGI(TAG, "local volume increase hold: %s",
                                 gesture_source == DEVICE_INPUT_SOURCE_TOUCH ?
                                     "touch" : "button");
                        emit_button_input(DEVICE_INPUT_VOLUME_UP, gesture_source);
                    } else if (!long_sent && held_ms >= minimum_tap_ms) {
                        int64_t since_previous_us = now_us - released_at_us;
                        int64_t double_window_us =
                            (int64_t)input_profile->double_tap_window_ms * 1000;
                        if (!native_double_sent && short_pending &&
                            pending_source == gesture_source &&
                            ((gesture_source != DEVICE_INPUT_SOURCE_TOUCH &&
                              since_previous_us <= double_window_us) ||
                             (gesture_source == DEVICE_INPUT_SOURCE_TOUCH &&
                              since_previous_us >=
                                  (int64_t)input_profile->touch_double_min_gap_ms * 1000 &&
                              since_previous_us <= double_window_us))) {
                            short_pending = false;
                            native_double_sent = true;
                            if (gesture_source == DEVICE_INPUT_SOURCE_TOUCH) {
                                s_touch_gesture_consumed = true;
                                s_touch_gesture_released_at_us = 0;
                            }
                            ESP_LOGI(TAG, "button gesture: double (%s timing gap=%lld ms)",
                                     gesture_source == DEVICE_INPUT_SOURCE_TOUCH ? "touch" : "button",
                                     (long long)(since_previous_us / 1000));
                            emit_button_input(DEVICE_INPUT_SECONDARY, gesture_source);
                        } else if (gesture_source == DEVICE_INPUT_SOURCE_TOUCH &&
                                   s_command_cancel_enabled && short_pending &&
                                   since_previous_us <
                                       (int64_t)input_profile->touch_double_min_gap_ms * 1000) {
                            ESP_LOGD(TAG, "ignored touch duplicate contact: gap=%lld ms",
                                     (long long)(since_previous_us / 1000));
                        } else if (!native_double_sent && !s_touch_gesture_consumed) {
                            short_pending = true;
                            pending_source = gesture_source;
                            released_at_us = now_us;
                        }
                    }
                }
            }
        }
        if (pressed && !long_sent && pressed_at_us > 0 &&
            now_us - pressed_at_us >= (int64_t)input_profile->long_hold_ms * 1000) {
            long_sent = true;
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: long");
            emit_button_input(DEVICE_INPUT_CONFIGURE, gesture_source);
        }
        int64_t pending_window_us =
            (int64_t)input_profile->double_tap_window_ms * 1000;
        if (!pressed && short_pending && !s_touch_gesture_consumed &&
            now_us - released_at_us > pending_window_us) {
            short_pending = false;
            ESP_LOGI(TAG, "button gesture: short");
            emit_button_input(DEVICE_INPUT_PRIMARY, pending_source);
            pending_source = DEVICE_INPUT_SOURCE_UNKNOWN;
        }
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(input_profile->scan_poll_ms)) != 0) break;
    }

stopped:
    s_on_button = NULL;
    s_on_press_arg = NULL;
    if (s_button_task_stopped) xSemaphoreGive(s_button_task_stopped);
    vTaskDelete(NULL);
}

esp_err_t round_input_service_start(round_input_service_publish_cb_t on_button, void *arg) {
    if (s_button_task || s_button_task_stopped) return ESP_ERR_INVALID_STATE;
    const round_input_profile_t *input_profile = round_input_profile_service_profile();
    if (!input_profile) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(round_input_profile_service_initialize_activate_key(), TAG,
                        "round activation key init failed");
    s_on_button = on_button;
    s_on_press_arg = arg;
    s_button_task_stopped = xSemaphoreCreateBinary();
    s_button_task_system_sleep_quiesced = xSemaphoreCreateBinary();
    if (!s_button_task_stopped || !s_button_task_system_sleep_quiesced) {
        if (s_button_task_stopped) vSemaphoreDelete(s_button_task_stopped);
        if (s_button_task_system_sleep_quiesced) {
            vSemaphoreDelete(s_button_task_system_sleep_quiesced);
        }
        s_button_task_stopped = NULL;
        s_button_task_system_sleep_quiesced = NULL;
        s_on_button = NULL;
        s_on_press_arg = NULL;
        return ESP_ERR_NO_MEM;
    }
    s_button_task_stop_requested = false;
    s_button_task_system_sleep_preparing = false;
    if (round_input_profile_service_start_scan_task(button_task, &s_button_task) != pdPASS) {
        ESP_LOGE(TAG, "cannot start button task");
        vSemaphoreDelete(s_button_task_stopped);
        vSemaphoreDelete(s_button_task_system_sleep_quiesced);
        s_button_task_stopped = NULL;
        s_button_task_system_sleep_quiesced = NULL;
        s_on_button = NULL;
        s_on_press_arg = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

esp_err_t round_input_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_button_task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == s_button_task) return ESP_ERR_INVALID_STATE;
    if (!s_button_task_stop_requested) {
        s_button_task_stop_requested = true;
        xTaskNotifyGive(s_button_task);
    }
    if (!s_button_task_stopped ||
        xSemaphoreTake(s_button_task_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        ESP_LOGW(TAG, "timed out stopping board input scanner");
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_button_task_stopped);
    vSemaphoreDelete(s_button_task_system_sleep_quiesced);
    s_button_task_stopped = NULL;
    s_button_task_system_sleep_quiesced = NULL;
    s_button_task = NULL;
    s_button_task_stop_requested = false;
    s_button_task_system_sleep_preparing = false;
    ESP_LOGI(TAG, "board input scanner stopped");
    return ESP_OK;
}

esp_err_t round_input_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_button_task_system_sleep_lock);
    if (!s_button_task || s_button_task_stop_requested ||
        s_button_task_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_button_task_system_sleep_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_button_task_system_sleep_preparing = true;
    task = s_button_task;
    taskEXIT_CRITICAL(&s_button_task_system_sleep_lock);

    /* An earlier aborted generation may have left its acknowledgement queued.
     * Drain it before notifying this generation, so a successful take always
     * proves the scanner observed the marker above. */
    while (xSemaphoreTake(s_button_task_system_sleep_quiesced, 0) == pdTRUE) {}
    xTaskNotifyGive(task);
    if (xSemaphoreTake(s_button_task_system_sleep_quiesced,
                       pdMS_TO_TICKS(timeout_ms)) == pdTRUE) {
        return ESP_OK;
    }
    /* A timeout is still a closed generation.  Only the owning Power
     * transaction may ABORT it after reverse-order rollback; reopening here
     * could resume touch/I2C or activate-key reads while another private
     * participant remains parked. */
    return ESP_ERR_TIMEOUT;
}

void round_input_service_abort_system_sleep_prepare(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_button_task_system_sleep_lock);
    s_button_task_system_sleep_preparing = false;
    task = s_button_task;
    taskEXIT_CRITICAL(&s_button_task_system_sleep_lock);
    if (task) xTaskNotifyGive(task);
}

void round_input_service_set_command_cancel_enabled(bool enabled) {
    if (enabled && !s_command_cancel_enabled) ++s_command_gesture_revision;
    s_command_cancel_enabled = enabled;
}
