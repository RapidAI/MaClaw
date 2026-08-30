#include "services/command_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "app_ui.h"
#include "presentation/scene_presenter.h"
#include "operation_context.h"
#include "services/foreground_coordinator.h"
#include "services/gateway_transport.h"
#include "services/interaction_service.h"
#include "services/reply_service.h"
#include "task_registry.h"

/* Keep the log tag identical to the original main.c owner so existing voice /
 * cancel trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define COMMAND_CANCEL_WORKER_TIMEOUT_MS 13000
#define COMMAND_CANCEL_ACKNOWLEDGEMENT_MS 450

static portMUX_TYPE s_command_state_lock = portMUX_INITIALIZER_UNLOCKED;

static volatile bool s_command_display_locked;
static volatile bool s_command_cancel_requested;
static volatile bool s_command_cancel_enabled;
static bool s_command_cancel_ui_shown;
// The activation down edge is intentionally useful while recording: it stops
// capture immediately instead of waiting for the 500 ms single/double gesture
// decision. Consume the completed gesture from that same physical contact so
// its delayed SHORT can never dismiss the new thinking/result surface or start
// another command. A fresh down edge disarms this one-contact barrier.  The
// scanner is allowed to lose a completion during controller recovery, so the
// barrier has a bounded lifetime; it must never consume a later, independent
// press in the next command.
#define INPUT_GESTURE_DRAIN_WINDOW_US 1500000ULL
static bool s_command_capture_stop_gesture_pending;
static device_input_source_t s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
static uint64_t s_command_capture_stop_gesture_deadline_us;
static int64_t s_ignore_command_input_until_us;
static uint32_t s_cancel_requested_generation;
static uint32_t s_cancel_ui_ready_generation;
static int64_t s_command_timing_started_us;
static int64_t s_command_timing_capture_done_us;
static int64_t s_command_timing_upload_done_us;
static int64_t s_command_timing_accepted_us;
static int64_t s_command_timing_first_progress_us;
static SemaphoreHandle_t s_command_cancel_ui_ready;
static TaskHandle_t s_command_cancel_task;
static SemaphoreHandle_t s_command_cancel_stopped;
static bool s_command_cancel_stop_requested;
/* A stopped task is not retired until its immutable Interaction Registry
 * identity is gone. Preserve the terminal result and keep admission closed on
 * failure so a future lifecycle sweep cannot address a replacement worker. */
static bool s_command_cancel_retiring;
static esp_err_t s_command_cancel_exit_status = ESP_OK;
static bool s_command_cancel_registry_retirement_failed;
/* System Sleep must never stop/recreate the permanent cancellation worker:
 * that would make an already accepted cancellation lose its local/remote
 * terminal semantics.  PREPARE instead closes new admission and returns BUSY
 * if a cancellation has crossed into the worker. */
static bool s_system_sleep_preparing;
static bool s_command_cancel_worker_active;

static command_service_host_t s_host;
static bool s_host_installed;

static uint32_t elapsed_ms_between(int64_t started_us, int64_t finished_us) {
    return started_us > 0 && finished_us >= started_us
               ? (uint32_t)((finished_us - started_us) / 1000)
               : 0;
}

/* Rollback children receive only what remains of the parent's monotonic
 * deadline.  Round up a non-zero remainder so a child can observe and return
 * a real timeout instead of treating a sub-millisecond budget as invalid. */
static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

static void host_notify_interaction_task(void) {
    interaction_service_notify_worker();
}

bool command_service_cancel_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_command_state_lock);
    requested = s_command_cancel_requested;
    taskEXIT_CRITICAL(&s_command_state_lock);
    return requested;
}

bool command_service_cancel_requested_for(uint32_t generation) {
    bool operation_cancelled = operation_context_cancel_requested(generation);
    bool requested;
    taskENTER_CRITICAL(&s_command_state_lock);
    requested = s_command_cancel_requested &&
                s_cancel_requested_generation == generation &&
                operation_cancelled;
    taskEXIT_CRITICAL(&s_command_state_lock);
    return requested;
}

bool command_service_current_task_is_cancel_worker(void) {
    bool matches;
    taskENTER_CRITICAL(&s_command_state_lock);
    matches = s_command_cancel_task != NULL &&
              s_command_cancel_task == xTaskGetCurrentTaskHandle();
    taskEXIT_CRITICAL(&s_command_state_lock);
    return matches;
}

static void show_cancelled_command(uint32_t generation) {
    interaction_service_snapshot_t snapshot;
    interaction_service_snapshot(&snapshot);
    taskENTER_CRITICAL(&s_command_state_lock);
    bool cancellation_still_active = s_command_cancel_requested &&
                                     s_cancel_requested_generation == generation &&
                                     snapshot.generation == generation;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (!cancellation_still_active) return;
    reply_service_remember_cancelled();
    scene_presenter_publish_command_cancel_enabled(false);
    bool should_draw = false;
    taskENTER_CRITICAL(&s_command_state_lock);
    // Let CST816 finish reporting the second contact before a SHORT gesture is
    // allowed to start another recording. This guard complements the board
    // driver's raw-event drain and also covers the physical BOOT button.
    s_ignore_command_input_until_us = esp_timer_get_time() + 1200000;
    if (!s_command_cancel_ui_shown) {
        s_command_cancel_ui_shown = true;
        should_draw = true;
    }
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (should_draw) {
        scene_presenter_publish_message("已取消", "本次操作已停止");
        ESP_LOGI(TAG, "voice command cancelled by double tap");
        foreground_coordinator_observe_acquire(FOREGROUND_OWNER_COMMAND_VOICE,
                                               FOREGROUND_PRIORITY_CAPTURE,
                                               FOREGROUND_SCENE_COMMAND_MESSAGE);
    }
}

void command_service_finish_cancelled(uint32_t generation) {
    // The high-priority cancellation worker owns LCD rendering so the touch
    // scanner never blocks on a full display transfer. Wait briefly for that
    // final state before releasing the interaction token; this also prevents a
    // delayed cancellation frame from overwriting the next command screen.
    if (s_command_cancel_ui_ready) {
        TickType_t started = xTaskGetTickCount();
        bool worker_finished = false;
        while ((xTaskGetTickCount() - started) <
               pdMS_TO_TICKS(COMMAND_CANCEL_WORKER_TIMEOUT_MS)) {
            if (xSemaphoreTake(s_command_cancel_ui_ready, pdMS_TO_TICKS(50)) == pdTRUE) {
                taskENTER_CRITICAL(&s_command_state_lock);
                bool ready_for_this_command = s_cancel_ui_ready_generation == generation;
                taskEXIT_CRITICAL(&s_command_state_lock);
                if (ready_for_this_command) {
                    worker_finished = true;
                    break;
                }
            }
        }
        if (!worker_finished) {
            ESP_LOGW(TAG, "command cancellation worker timed out: generation=%lu",
                     (unsigned long)generation);
        }
    }
    if (command_service_cancel_requested_for(generation)) show_cancelled_command(generation);
    // Keep the acknowledgement long enough to be perceived, then perform one
    // explicit cancel -> idle transition. The gesture input guard remains in
    // force, so the second contact cannot immediately start another command.
    vTaskDelay(pdMS_TO_TICKS(COMMAND_CANCEL_ACKNOWLEDGEMENT_MS));
    interaction_service_finish_with_surface(generation, true);
}

static void command_cancel_worker(void *arg) {
    (void)arg;
    while (true) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);

        taskENTER_CRITICAL(&s_command_state_lock);
        bool stop_requested = s_command_cancel_stop_requested;
        taskEXIT_CRITICAL(&s_command_state_lock);
        if (stop_requested) break;

        uint32_t cancel_generation = 0;
        taskENTER_CRITICAL(&s_command_state_lock);
        cancel_generation = s_cancel_requested_generation;
        taskEXIT_CRITICAL(&s_command_state_lock);
        if (!cancel_generation) continue;

        interaction_service_snapshot_t snapshot;
        interaction_service_snapshot(&snapshot);
        bool cancellation_still_active;
        taskENTER_CRITICAL(&s_command_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    snapshot.generation == cancel_generation &&
                                    !s_system_sleep_preparing;
        if (cancellation_still_active) s_command_cancel_worker_active = true;
        taskEXIT_CRITICAL(&s_command_state_lock);
        if (!cancellation_still_active) continue;

        show_cancelled_command(cancel_generation);

        // Hold the pointer guard for the entire cancel call. The request task
        // must acquire the same guard before clearing/cleaning the handle, so
        // this can never race esp_http_client_cleanup() or dereference a stale
        // client pointer.
        if (s_host_installed && s_host.cancel_foreground_http) {
            s_host.cancel_foreground_http();
        }
        /* A request admitted on cellular can outlive a selected-uplink
         * transition until this cancellation reaches its bounded return.
         * Device API checks the request-level cellular fact and is a no-op
         * for Wi-Fi-only or idle generations. */
        if (device_connectivity_cancel_cellular_foreground_request()) {
            ESP_LOGI(TAG, "foreground ML307 HTTP request cancelled");
        }

        // Local cancellation stops waiting immediately, but the server-side
        // agent may already be executing after accepting the voice event. Send
        // the protocol's normal /cancel command before releasing the local
        // interaction token so it cannot accidentally target a newer command.
        interaction_service_snapshot(&snapshot);
        taskENTER_CRITICAL(&s_command_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    snapshot.generation == cancel_generation;
        taskEXIT_CRITICAL(&s_command_state_lock);
        if (gateway_transport_is_paired() && cancellation_still_active) {
            char cancelled_reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY] = {0};
            reply_service_copy_active_reply_to(cancelled_reply_to,
                                               sizeof(cancelled_reply_to));
            if (s_host.send_server_cancel) {
                int32_t server_cancel_err = s_host.send_server_cancel(cancelled_reply_to);
                if (server_cancel_err != 0) {
                    ESP_LOGW(TAG, "server command cancel failed: %s",
                             esp_err_to_name((esp_err_t)server_cancel_err));
                } else {
                    ESP_LOGI(TAG, "server command cancel accepted");
                }
            }
        }

        bool notify_waiter = false;
        taskENTER_CRITICAL(&s_command_state_lock);
        s_cancel_ui_ready_generation = cancel_generation;
        if (s_command_cancel_requested &&
            s_cancel_requested_generation == cancel_generation) {
            notify_waiter = true;
        }
        s_command_cancel_worker_active = false;
        taskEXIT_CRITICAL(&s_command_state_lock);
        if (s_command_cancel_ui_ready) xSemaphoreGive(s_command_cancel_ui_ready);
        if (notify_waiter) host_notify_interaction_task();
        // High-water mark after a full cancel cycle (CJK frame + transport
        // cancel + /cancel POST) so real-device runs can confirm the stack
        // margin on both the Wi-Fi/TLS and the ML307 paths.
        ESP_LOGI(TAG, "command cancel handled: generation=%lu stack_hwm=%u",
                 (unsigned long)cancel_generation,
                 (unsigned)uxTaskGetStackHighWaterMark(NULL));
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_retiring = true;
    taskEXIT_CRITICAL(&s_command_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_INTERACTION, (void *)self, 10);
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_exit_status = registry_err;
    if (s_command_cancel_task == self) s_command_cancel_task = NULL;
    s_command_cancel_retiring = false;
    if (registry_err != ESP_OK) {
        s_command_cancel_stop_requested = true;
        s_command_cancel_registry_retirement_failed = true;
    }
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (s_command_cancel_stopped) xSemaphoreGive(s_command_cancel_stopped);
    vTaskDelete(NULL);
}

/* The cancellation coordinator owns display/network cancellation side effects,
 * but no driver lifetime.  Stop only at its notification wait boundary and
 * keep its completion semaphore alive on timeout: a still-running worker may
 * still reference the UI and foreground HTTP guards. */
static esp_err_t stop_command_cancel_worker(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_stop_requested = true;
    task = s_command_cancel_task;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_command_state_lock);
        const esp_err_t exit_status = s_command_cancel_exit_status;
        taskEXIT_CRITICAL(&s_command_state_lock);
        return exit_status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_command_cancel_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_command_cancel_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_command_state_lock);
    const esp_err_t exit_status = s_command_cancel_exit_status;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (exit_status != ESP_OK) return exit_status;
    ESP_LOGI(TAG, "command cancel worker stopped");
    return ESP_OK;
}

static esp_err_t stop_command_cancel_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_command_state_lock);
    task = s_command_cancel_task;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_command_cancel_worker(timeout_ms);
}

bool command_service_request_cancel(void) {
    interaction_service_snapshot_t snapshot;
    interaction_service_snapshot(&snapshot);
    uint32_t generation = 0;
    taskENTER_CRITICAL(&s_command_state_lock);
    // Cancellation belongs strictly to the thinking phase. Once the poller has
    // accepted a result it clears this flag before drawing the answer, so a
    // late double tap cannot replace a completed command with “已取消”.
    if (!s_system_sleep_preparing && snapshot.task_active &&
        s_command_cancel_enabled && !s_command_cancel_requested) {
        s_command_cancel_requested = true;
        s_cancel_requested_generation = snapshot.generation;
        generation = snapshot.generation;
        s_command_cancel_enabled = false;
    }
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (!generation) return false;
    if (!operation_context_request_cancel(generation)) {
        /* A terminal reply won while the gesture was being classified.  Do
         * not let the stale gesture revive a cancellation side effect. */
        taskENTER_CRITICAL(&s_command_state_lock);
        if (s_cancel_requested_generation == generation) {
            s_command_cancel_requested = false;
            s_cancel_requested_generation = 0;
        }
        taskEXIT_CRITICAL(&s_command_state_lock);
        return false;
    }
    scene_presenter_publish_command_cancel_enabled(false);
    // Keep the touch task responsive: a dedicated internal-RAM worker renders
    // the final frame and interrupts any in-flight HTTP operation safely.
    if (s_command_cancel_task) {
        xTaskNotifyGive(s_command_cancel_task);
    } else {
        // Startup treats creation failure as fatal, but retain a cooperative
        // fallback so a partially initialized device cannot wait for 90 s.
        host_notify_interaction_task();
    }
    ESP_LOGI(TAG, "voice command cancel requested by double tap");
    return true;
}

void command_service_set_cancel_enabled(bool enabled) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_enabled = enabled && !s_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_reset_cancel_state(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_requested = false;
    s_command_cancel_enabled = false;
    s_command_cancel_ui_shown = false;
    s_cancel_requested_generation = 0;
    s_cancel_ui_ready_generation = 0;
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_clear_cancel_state(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_enabled = false;
    s_command_cancel_requested = false;
    s_cancel_requested_generation = 0;
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_drain_cancel_ui_ready(void) {
    if (s_command_cancel_ui_ready) {
        while (xSemaphoreTake(s_command_cancel_ui_ready, 0) == pdTRUE) {}
    }
}

void command_service_set_display_locked(bool locked) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_display_locked = locked;
    taskEXIT_CRITICAL(&s_command_state_lock);
    foreground_coordinator_observe_display_lock(locked);
}

// A foreground command owns the LCD from the end of capture until a final
// answer or explicit error is displayed. Background updates may refresh data,
// but must not replace that flow with the ambient/weather screen.
bool command_service_display_active(void) {
    interaction_service_snapshot_t snapshot;
    interaction_service_snapshot(&snapshot);
    bool active;
    taskENTER_CRITICAL(&s_command_state_lock);
    active = snapshot.task_active || s_command_display_locked;
    taskEXIT_CRITICAL(&s_command_state_lock);
    return active;
}

void command_service_arm_capture_stop_gesture(device_input_source_t source) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_capture_stop_gesture_pending = true;
    s_command_capture_stop_source = source;
    s_command_capture_stop_gesture_deadline_us =
        (uint64_t)esp_timer_get_time() + INPUT_GESTURE_DRAIN_WINDOW_US;
    taskEXIT_CRITICAL(&s_command_state_lock);
    ESP_LOGI(TAG, "command recording stop requested by input source=%d",
             (int)source);
}

bool command_service_consume_capture_stop_gesture(device_input_source_t source,
                                                  bool contact_down) {
    bool consumed = false;
    taskENTER_CRITICAL(&s_command_state_lock);
    if (s_command_capture_stop_gesture_pending &&
        esp_timer_get_time() >= (int64_t)s_command_capture_stop_gesture_deadline_us) {
        /* A malformed/aborted physical contact must not leave the old
         * completion barrier armed indefinitely.  The next activation is a
         * new user gesture and must retain its normal command semantics. */
        s_command_capture_stop_gesture_pending = false;
        s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
        s_command_capture_stop_gesture_deadline_us = 0;
        taskEXIT_CRITICAL(&s_command_state_lock);
        ESP_LOGW(TAG, "command-capture stop gesture barrier expired");
        taskENTER_CRITICAL(&s_command_state_lock);
    }
    if (s_command_capture_stop_gesture_pending &&
        source == s_command_capture_stop_source) {
        if (contact_down) {
            // This is a genuinely new contact, not completion of the stop
            // contact. Admit it normally after retiring the old barrier.
            s_command_capture_stop_gesture_pending = false;
            s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
            s_command_capture_stop_gesture_deadline_us = 0;
        } else {
            consumed = true;
        }
    }
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (consumed) {
        ESP_LOGI(TAG, "completed command-capture stop gesture consumed");
    }
    return consumed;
}

bool command_service_input_guarded(void) {
    bool guarded;
    taskENTER_CRITICAL(&s_command_state_lock);
    guarded = esp_timer_get_time() < s_ignore_command_input_until_us;
    taskEXIT_CRITICAL(&s_command_state_lock);
    return guarded;
}

void command_service_timing_begin(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_timing_started_us = esp_timer_get_time();
    s_command_timing_capture_done_us = 0;
    s_command_timing_upload_done_us = 0;
    s_command_timing_accepted_us = 0;
    s_command_timing_first_progress_us = 0;
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_timing_capture_done(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_timing_capture_done_us = esp_timer_get_time();
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_timing_upload_done(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_timing_upload_done_us = esp_timer_get_time();
    taskEXIT_CRITICAL(&s_command_state_lock);
}

void command_service_timing_accepted(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_timing_accepted_us = esp_timer_get_time();
    taskEXIT_CRITICAL(&s_command_state_lock);
}

bool command_service_timing_mark_first_progress(void) {
    bool marked = false;
    taskENTER_CRITICAL(&s_command_state_lock);
    if (!s_command_timing_first_progress_us) {
        s_command_timing_first_progress_us = esp_timer_get_time();
        marked = true;
    }
    taskEXIT_CRITICAL(&s_command_state_lock);
    return marked;
}

uint32_t command_service_timing_accepted_to_first_progress_ms(void) {
    uint32_t elapsed_ms;
    taskENTER_CRITICAL(&s_command_state_lock);
    elapsed_ms = elapsed_ms_between(s_command_timing_accepted_us,
                                    s_command_timing_first_progress_us);
    taskEXIT_CRITICAL(&s_command_state_lock);
    return elapsed_ms;
}

void command_service_log_timing(const char *terminal) {
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_command_state_lock);
    int64_t started_us = s_command_timing_started_us;
    int64_t capture_done_us = s_command_timing_capture_done_us;
    int64_t upload_done_us = s_command_timing_upload_done_us;
    int64_t accepted_us = s_command_timing_accepted_us;
    int64_t first_progress_us = s_command_timing_first_progress_us;
    taskEXIT_CRITICAL(&s_command_state_lock);
    ESP_LOGI(TAG,
             "command timing: terminal=%s capture=%ums upload=%ums submit=%ums firstProgress=%ums total=%ums",
             terminal ? terminal : "unknown",
             (unsigned)elapsed_ms_between(started_us, capture_done_us),
             (unsigned)elapsed_ms_between(capture_done_us, upload_done_us),
             (unsigned)elapsed_ms_between(upload_done_us, accepted_us),
             (unsigned)elapsed_ms_between(accepted_us, first_progress_us),
             (unsigned)elapsed_ms_between(started_us, now_us));
}

bool command_service_voice_upload_should_retry(int32_t err, int status) {
    switch ((esp_err_t)err) {
        case ESP_ERR_TIMEOUT:
        case ESP_ERR_HTTP_CONNECT:
        case ESP_ERR_HTTP_WRITE_DATA:
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_CONNECTING:
        case ESP_ERR_HTTP_EAGAIN:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return true;
        default:
            break;
    }
    return err == 0 &&
           (status == 408 || status == 425 || status == 429 || status >= 500);
}

void command_service_voice_upload_retry_delay(unsigned attempt) {
    vTaskDelay(pdMS_TO_TICKS(250u << (attempt - 1u)));
}

const char *command_service_submit_error_detail(int32_t err) {
    switch ((esp_err_t)err) {
        case ESP_ERR_TIMEOUT: return "网关响应超时，请稍后重试";
        case ESP_ERR_HTTP_CONNECT:
            return device_connectivity_is_active_cellular() ? "网络连接失败，请检查 4G"
                                           : "网络连接失败，请检查 Wi-Fi";
        case ESP_ERR_HTTP_WRITE_DATA: return "语音发送中断，请重新尝试";
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return "网关连接不稳定，请重新尝试";
        case ESP_ERR_NO_MEM: return "设备内存不足，请重启后重试";
        case ESP_ERR_INVALID_RESPONSE: return "网关响应格式不兼容";
        case ESP_ERR_INVALID_STATE: return "请求已取消或网络状态异常";
        case ESP_FAIL: return "网关拒绝请求或服务异常";
        default: return esp_err_to_name((esp_err_t)err);
    }
}

device_status_t command_service_init(const command_service_host_t *host) {
    if (!host || !host->send_server_cancel || !host->cancel_foreground_http) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    s_host = *host;
    s_host_installed = true;
    taskENTER_CRITICAL(&s_command_state_lock);
    s_system_sleep_preparing = false;
    s_command_cancel_worker_active = false;
    s_command_cancel_retiring = false;
    s_command_cancel_exit_status = ESP_OK;
    s_command_cancel_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_command_state_lock);
    s_command_cancel_ui_ready = xSemaphoreCreateBinary();
    if (!s_command_cancel_ui_ready) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_command_cancel_stopped = xSemaphoreCreateBinary();
    if (!s_command_cancel_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    return DEVICE_STATUS_OK;
}

device_status_t command_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_command_state_lock);
    if (!s_host_installed || !s_command_cancel_task) {
        taskEXIT_CRITICAL(&s_command_state_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    /* A cancellation owns an ordered UI → foreground transport abort →
     * server /cancel transaction. It must complete or be rejected before a
     * later Power participant can fence display/connectivity; ABORT cannot
     * faithfully manufacture that terminal protocol sequence. */
    if (s_system_sleep_preparing || s_command_cancel_requested ||
        s_command_cancel_worker_active) {
        taskEXIT_CRITICAL(&s_command_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_command_state_lock);
    return DEVICE_STATUS_OK;
}

void command_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_command_state_lock);
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_command_state_lock);
}

device_status_t command_service_start(void) {
    if (!s_host_installed || !s_command_cancel_ui_ready || !s_command_cancel_stopped) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    taskENTER_CRITICAL(&s_command_state_lock);
    const bool retirement_failed = s_command_cancel_registry_retirement_failed;
    const bool task_active = s_command_cancel_task != NULL || s_command_cancel_retiring;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (retirement_failed || task_active) return DEVICE_STATUS_BUSY;
    // The cancel worker draws a full CJK frame, cancels the foreground TLS or
    // ML307 request and then posts "/cancel" through a complete HTTPS/AT
    // round trip. 4096 bytes overflowed on Fangtang (crash/reboot on cancel);
    // 8192 matches the other network-facing workers. Plain xTaskCreate keeps
    // the stack in internal RAM, so the Wi-Fi path stays cache-safe.
    taskENTER_CRITICAL(&s_command_state_lock);
    s_command_cancel_stop_requested = false;
    s_command_cancel_exit_status = ESP_OK;
    s_command_cancel_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_command_state_lock);
    if (xTaskCreate(command_cancel_worker, "maclaw_cancel", 8192, NULL, 6,
                    &s_command_cancel_task) != pdPASS) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    esp_err_t cancel_registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_INTERACTION,
        .name = "command_cancel",
        .context = (void *)s_command_cancel_task,
        .stop = stop_command_cancel_registry_entry,
    });
    if (cancel_registry_err != ESP_OK) {
        (void)stop_command_cancel_worker(500);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    return DEVICE_STATUS_OK;
}
