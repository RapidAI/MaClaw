#include "services/interaction_service.h"

#include <stdio.h>
#include <string.h>

#include "esp_err.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "presentation/scene_presenter.h"
#include "services/ambient_service.h"
#include "services/audio_arbitration_service.h"
#include "operation_context.h"
#include "services/command_service.h"
#include "services/gateway_transport.h"
#include "services/meeting_service.h"
#include "services/reply_service.h"
#include "task_registry.h"

/* Keep the log tag identical to the original main.c owner so existing voice /
 * cancel trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define COMMAND_RESULT_PROGRESS_MS 15000
#define COMMAND_SUBMIT_RETRY_COUNT 3

static portMUX_TYPE s_interaction_state_lock = portMUX_INITIALIZER_UNLOCKED;

static TaskHandle_t s_interaction_task;
static device_power_lease_t s_interaction_power_lease;
static SemaphoreHandle_t s_interaction_start_gate;
static SemaphoreHandle_t s_interaction_stopped;
static bool s_interaction_stop_requested;
static bool s_interaction_starting;
/* The interaction admission token cannot be released merely because capture
 * has finished.  Its immutable Registry identity must retire first; otherwise
 * a new voice worker could be admitted while owner-wide rollback still holds
 * the previous generation's stop callback. */
static bool s_interaction_retiring;
static esp_err_t s_interaction_exit_status = ESP_OK;
static bool s_interaction_registry_retirement_failed;
static uint32_t s_interaction_generation;
static volatile interaction_service_phase_t s_interaction_phase = INTERACTION_SERVICE_IDLE;
// Set as soon as a short voice command is requested. Background meeting
// recovery yields between chunks so the interactive upload gets the HTTP lock.
static volatile bool s_foreground_http_requested;
// A foreground interaction starts in the button callback but finishes in
// its worker task, therefore mutual exclusion must use a binary semaphore
// rather than an ownership-tracked mutex.
static SemaphoreHandle_t s_interaction_lock;

static interaction_service_host_t s_host;
static bool s_host_installed;

/* Mirror of the composition root's Device-status mapping so capture-facing
 * log lines keep their original esp_err_to_name() text. */
static esp_err_t device_status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_NOT_FOUND: return ESP_ERR_NOT_FOUND;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        default: return ESP_ERR_INVALID_RESPONSE;
    }
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

static void host_log_heap_snapshot(const char *stage) {
    if (s_host_installed && s_host.log_heap_snapshot) s_host.log_heap_snapshot(stage);
}

static void host_schedule_wake_restart(void) {
    if (s_host_installed && s_host.schedule_wake_restart) s_host.schedule_wake_restart();
}

static bool interaction_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    requested = s_interaction_stop_requested;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return requested;
}

void interaction_service_snapshot(interaction_service_snapshot_t *out_snapshot) {
    taskENTER_CRITICAL(&s_interaction_state_lock);
    out_snapshot->task_active = s_interaction_task != NULL;
    out_snapshot->processing = s_interaction_phase == INTERACTION_SERVICE_PROCESSING;
    out_snapshot->generation = s_interaction_generation;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
}

uint32_t interaction_service_generation(void) {
    uint32_t generation;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    generation = s_interaction_generation;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return generation;
}

interaction_service_phase_t interaction_service_phase(void) {
    interaction_service_phase_t phase;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    phase = s_interaction_phase;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return phase;
}

void interaction_service_set_phase(interaction_service_phase_t phase) {
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_interaction_phase = phase;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
}

bool interaction_service_worker_active(void) {
    bool active;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    active = s_interaction_task != NULL;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return active;
}

bool interaction_service_current_task_is_worker(void) {
    bool matches;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    matches = s_interaction_task != NULL && s_interaction_task == xTaskGetCurrentTaskHandle();
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return matches;
}

bool interaction_service_foreground_http_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    requested = s_foreground_http_requested;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    return requested;
}

bool interaction_service_admission_take(uint32_t timeout_ms) {
    return s_interaction_lock &&
           xSemaphoreTake(s_interaction_lock, pdMS_TO_TICKS(timeout_ms)) == pdTRUE;
}

void interaction_service_admission_give(void) {
    if (s_interaction_lock) xSemaphoreGive(s_interaction_lock);
}

uintptr_t interaction_service_claim_reply_waiter(uint32_t generation) {
    uintptr_t waiter = 0;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    if (s_interaction_generation == generation && s_interaction_task) {
        waiter = (uintptr_t)s_interaction_task;
    }
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (waiter) command_service_clear_cancel_state();
    return waiter;
}

void interaction_service_notify_waiter(uintptr_t waiter) {
    if (waiter) xTaskNotifyGive((TaskHandle_t)waiter);
}

void interaction_service_notify_worker(void) {
    taskENTER_CRITICAL(&s_interaction_state_lock);
    TaskHandle_t task = s_interaction_task;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (task) xTaskNotifyGive(task);
}

void interaction_service_finish_with_surface(uint32_t generation,
                                             bool restore_standby) {
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    scene_presenter_publish_command_cancel_enabled(false);
    bool operation_current = operation_context_is_current(generation);
    taskENTER_CRITICAL(&s_interaction_state_lock);
    bool owns_interaction = operation_current && s_interaction_generation == generation &&
                            s_interaction_task == xTaskGetCurrentTaskHandle();
    uint32_t current_generation = s_interaction_generation;
    if (owns_interaction) {
        s_interaction_retiring = true;
        s_interaction_phase = restore_standby ? INTERACTION_SERVICE_IDLE
                                              : INTERACTION_SERVICE_RESULT;
        s_foreground_http_requested = false;
    }
    device_power_lease_t interaction_lease = DEVICE_POWER_LEASE_INVALID;
    if (owns_interaction) {
        interaction_lease = s_interaction_power_lease;
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
    }
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (owns_interaction) {
        command_service_clear_cancel_state();
        reply_service_clear_active_reply_to();
        if (restore_standby) {
            reply_service_clear_result_speech();
            command_service_set_display_locked(false);
        }
    }
    device_power_lease_release(interaction_lease);
    if (owns_interaction && restore_standby) {
        // A cancelled command ends on APP_UI_SURFACE_MESSAGE ("已取消"), so the
        // normal response-only dismiss path cannot restore the ambient screen.
        // Restore the whole shared UI model before admitting another command.
        scene_presenter_restore_standby();
        ESP_LOGI(TAG, "cancelled command returned to standby: generation=%lu",
                 (unsigned long)generation);
    }
    if (!owns_interaction) {
        ESP_LOGW(TAG, "stale interaction finish ignored: generation=%lu current=%lu",
                 (unsigned long)generation, (unsigned long)current_generation);
        (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_INTERACTION,
                                                     (void *)self, 10);
        if (s_interaction_stopped) xSemaphoreGive(s_interaction_stopped);
        vTaskDelete(NULL);
        return;
    }
    /* A poller may have committed the final response before waking this
     * worker.  In that case commit_terminal simply reports false; this task
     * still owns cleanup and must release the admission token exactly once. */
    (void)operation_context_commit_terminal(generation);
    // This is a binary admission token, not a mutex: the button task starts
    // the interaction task, which completes it on another task context.
    // Releasing a FreeRTOS mutex from that child task asserts and reboots.
    // The interaction worker now uses ordinary xTaskCreate() with an internal
    // RAM stack, so it must be destroyed by the matching FreeRTOS API.
    // vTaskDeleteWithCaps() asserts when given a normally allocated task.
    if (owns_interaction) {
        const esp_err_t registry_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_INTERACTION, (void *)self, 10);
        taskENTER_CRITICAL(&s_interaction_state_lock);
        s_interaction_exit_status = registry_err;
        if (s_interaction_task == self) s_interaction_task = NULL;
        s_interaction_starting = false;
        s_interaction_retiring = false;
        if (registry_err != ESP_OK) {
            s_interaction_stop_requested = true;
            s_interaction_registry_retirement_failed = true;
        }
        taskEXIT_CRITICAL(&s_interaction_state_lock);
        if (s_interaction_stopped) xSemaphoreGive(s_interaction_stopped);
        /* A failed Registry retirement intentionally retains the admission
         * token.  This makes the failure visible as a closed interaction
         * surface rather than allowing a replacement task to inherit it. */
        if (registry_err == ESP_OK) {
            if (s_interaction_lock) xSemaphoreGive(s_interaction_lock);
            host_schedule_wake_restart();
        }
    }
    vTaskDelete(NULL);
}

static void finish_interaction_task(uint32_t generation) {
    interaction_service_finish_with_surface(generation, false);
}

// A local validation, capture, upload, or submission failure has no result to
// keep on screen.  Treat it like Bread Compact's short status acknowledgement:
// leave the message readable, then return every layer (application model,
// board renderer, and command admission state) to the ambient pet surface.
// Final remote replies deliberately continue to use finish_interaction_task(),
// so a user can read and dismiss them explicitly.
static void finish_interaction_message(uint32_t generation, uint32_t dwell_ms) {
    if (dwell_ms) (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(dwell_ms));
    interaction_service_finish_with_surface(generation, true);
}

static void interaction_task(void *arg) {
    uint32_t interaction_generation = (uint32_t)(uintptr_t)arg;
    /* xTaskCreate may run this worker before the creator publishes
     * s_interaction_task on the other core. Do not let an immediate capture
     * failure complete against a NULL/stale owner and strand the admission
     * token. The creator releases this one-shot gate only after publishing
     * the handle under the same lock. Later notifications retain their normal
     * result/progress/cancellation meaning. */
    if (!s_interaction_start_gate ||
        xSemaphoreTake(s_interaction_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "interaction start gate unavailable");
        goto finish;
    }
    if (interaction_stop_requested()) goto finish;
    int64_t interaction_started_us = esp_timer_get_time();
    // The wake-phrase path creates this worker from inside MultiNet, while a
    // panel tap unloads it before task creation. Converge both paths here so
    // command HTTPS upload always has enough contiguous DMA RAM for TLS AES.
    int32_t wake_stop_err = s_host.wake_word_stop ? s_host.wake_word_stop()
                                                  : (int32_t)ESP_ERR_INVALID_STATE;
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before voice capture: %s",
                 esp_err_to_name((esp_err_t)wake_stop_err));
    }
    host_log_heap_snapshot("voice-after-wake-stop");
    // Keep capture screen-neutral. Once a spoken command is accepted, the
    // visible path is only thinking -> result (or an explicit error).
    scene_presenter_publish_recording_mode(false);
    scene_presenter_publish_recording_visual(true, false, 0);
    uint8_t *wav = NULL;
    uint32_t wav_len = 0;
    esp_err_t err = device_status_to_esp_err(audio_arbitration_capture_wav(&wav, &wav_len));
    command_service_timing_capture_done();
    ESP_LOGI(TAG, "voice capture complete: generation=%lu err=%s wav=%u elapsed=%lldms",
             (unsigned long)interaction_generation, esp_err_to_name(err), (unsigned)wav_len,
             (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    if (interaction_stop_requested()) {
        audio_arbitration_release_captured_wav(wav);
        goto finish;
    }
    if (command_service_cancel_requested_for(interaction_generation)) {
        scene_presenter_publish_recording_visual(false, false, 0);
        audio_arbitration_release_captured_wav(wav);
        command_service_finish_cancelled(interaction_generation);
        return;
    }
    if (err != ESP_OK || !wav || wav_len == 0) {
        // The natural endpoint did not observe speech. This is an expected
        // cancellation-like outcome, not a microphone failure and certainly
        // not a request to send the legacy text probe to the gateway.
        if (err == ESP_ERR_NOT_FOUND) {
            scene_presenter_publish_recording_visual(false, false, 0);
            scene_presenter_publish_message("未检测到语音", "请再试一次");
            audio_arbitration_release_captured_wav(wav);
            finish_interaction_message(interaction_generation, 1400);
            return;
        }
        // Do not turn a local capture failure into an unrelated server text
        // command. That legacy probe leaves the command correlation empty and
        // can strand the EchoEar in a foreground message. Bread treats this as
        // a local, retryable status and returns to standby after the notice.
        ambient_service_apply_pet_state("alert");
        scene_presenter_publish_recording_visual(false, false, 0);
        scene_presenter_publish_message("麦克风不可用", "请稍后再试");
        audio_arbitration_release_captured_wav(wav);
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    if (!gateway_transport_is_paired()) {
        scene_presenter_publish_message("设备配对", "请说出六位配对码");
        err = (esp_err_t)gateway_transport_pair_by_voice(wav, wav_len);
        audio_arbitration_release_captured_wav(wav);
        if (err == ESP_OK && (esp_err_t)gateway_transport_handshake(false) == ESP_OK) {
            if (s_host.ensure_gateway_poll_task()) {
                ambient_service_apply_pet_state("done");
                char pairing_ready_hint[72];
                snprintf(pairing_ready_hint, sizeof(pairing_ready_hint),
                         "按%s后说话", device_input_primary_interaction_label());
                scene_presenter_publish_ready_prompt("配对成功", pairing_ready_hint);
            } else {
                err = ESP_ERR_NO_MEM;
                ambient_service_apply_pet_state("alert");
                scene_presenter_publish_message("设备启动失败", "无法启动网关轮询");
            }
        }
        else { ambient_service_apply_pet_state("alert"); scene_presenter_publish_message("配对失败", "请生成新的配对码"); }
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // The server is the interaction runtime: it owns ASR, intent routing,
    // authorization, agent/tool execution, IM delivery, and the final reply.
    // The ESP32 only submits a server-owned `voice` media attachment.
    char media_id[96] = {0};
    taskENTER_CRITICAL(&s_interaction_state_lock);
    if (s_interaction_generation == interaction_generation) {
        s_interaction_phase = INTERACTION_SERVICE_PROCESSING;
    }
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    // Switch state before closing the recorder. scene_presenter_publish_recording_visual
    // redraws the pet when it removes the waveform; doing that while the
    // previous state is idle briefly drew the time/weather face between
    // “received” and “thinking”.
    scene_presenter_publish_command_stage("正在上传语音");
    ambient_service_apply_pet_state("thinking");
    // Keep the foreground screen locked after capture as well.  The task can
    // receive its reply and clear its task handle before a delayed gateway
    // `pet_state: idle` notification is processed; that notification used to
    // repaint the Wi-Fi/time face in the gap before the final response draw.
    command_service_set_display_locked(true);
    scene_presenter_publish_command_display_lock(true);
    scene_presenter_publish_recording_visual(false, false, 0);
    command_service_set_cancel_enabled(true);
    scene_presenter_publish_command_cancel_enabled(true);
    // Keep the pet's animated thinking state on screen during upload and the
    // server-side reply wait. Do not switch the shared I2S bus to playback
    // here: on EchoEar it races the just-stopped microphone DMA and resets
    // the CPU. The thinking screen is the immediate acknowledgement.
    // A cancel that arrived during capture must not still upload the audio.
    if (interaction_stop_requested()) {
        audio_arbitration_release_captured_wav(wav);
        goto finish;
    }
    if (command_service_cancel_requested_for(interaction_generation)) {
        audio_arbitration_release_captured_wav(wav);
        command_service_finish_cancelled(interaction_generation);
        return;
    }
    err = (esp_err_t)s_host.upload_voice(wav, wav_len, media_id, sizeof(media_id));
    if (err == ESP_OK) command_service_timing_upload_done();
    audio_arbitration_release_captured_wav(wav);
    if (interaction_stop_requested()) goto finish;
    if (command_service_cancel_requested_for(interaction_generation)) { command_service_finish_cancelled(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice media upload failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        ambient_service_apply_pet_state("alert");
        scene_presenter_publish_message("语音上传失败", command_service_submit_error_detail((int32_t)err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    scene_presenter_publish_command_stage("正在提交指令");
    char reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY] = {0};
    char command_event_id[80];
    snprintf(command_event_id, sizeof(command_event_id), "voice-%lld",
             (long long)esp_timer_get_time());
    for (unsigned attempt = 1; attempt <= COMMAND_SUBMIT_RETRY_COUNT; ++attempt) {
        if (interaction_stop_requested()) goto finish;
        err = (esp_err_t)s_host.send_voice_event(media_id, command_event_id,
                                                 reply_to, sizeof(reply_to));
        if (err == ESP_OK || command_service_cancel_requested_for(interaction_generation)) break;
        ESP_LOGW(TAG, "voice command submit attempt %u/%u failed: %s",
                 attempt, COMMAND_SUBMIT_RETRY_COUNT, esp_err_to_name(err));
        if (attempt < COMMAND_SUBMIT_RETRY_COUNT) {
            // Reuse the idempotency key. If the Hub accepted an attempt but
            // its response was lost, the retry resolves the same command
            // instead of starting a duplicate Agent task.
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500u << (attempt - 1u)));
        }
    }
    if (err == ESP_OK && !reply_to[0]) {
        ESP_LOGE(TAG, "incoming voice accepted without maclawMessageId");
        err = ESP_ERR_INVALID_RESPONSE;
    }
    if (err == ESP_OK) {
        command_service_timing_accepted();
        scene_presenter_publish_command_stage("远端处理中");
        ESP_LOGI(TAG, "voice command waiting: generation=%lu replyTo=%s total=%lldms",
                 (unsigned long)interaction_generation, reply_to,
                 (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    if (err == ESP_OK) {
        reply_service_set_active_reply_to(reply_to);
    }
    if (interaction_stop_requested()) goto finish;
    if (command_service_cancel_requested_for(interaction_generation)) { command_service_finish_cancelled(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice command submit failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        ambient_service_apply_pet_state("alert");
        scene_presenter_publish_message("指令提交失败", command_service_submit_error_detail((int32_t)err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // Agent work is not bounded like a normal HTTP request. Complex remote
    // tasks routinely take longer than the old 90-second deadline; treating
    // that deadline as final also cleared replyTo, so the poller discarded the
    // eventual result. Keep the correlated command alive until a reply arrives
    // or the user explicitly cancels it. Refresh the message periodically so
    // the device never looks stalled while the remote Agent is still working.
    while (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(COMMAND_RESULT_PROGRESS_MS)) == 0) {
        if (interaction_stop_requested()) goto finish;
        if (command_service_cancel_requested_for(interaction_generation)) break;
        // Keep the animated thinking surface intact. This is a state
        // reassertion, not a full-screen refresh; unchanged labels do no LCD IO.
        scene_presenter_publish_command_stage("远端处理中");
        ESP_LOGI(TAG, "remote Agent still processing command generation=%lu",
                 (unsigned long)interaction_generation);
        ESP_LOGI(TAG, "remote wait detail: replyTo=%s elapsed=%lldms",
                 reply_to, (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    if (interaction_stop_requested()) goto finish;
    if (command_service_cancel_requested_for(interaction_generation)) { command_service_finish_cancelled(interaction_generation); return; }
    // The poller has already painted the final reply in the speaking state.
    // Returning through done/idle immediately after the notification repaints
    // the ambient face over it, producing the distracting apparent reboot.
    // Leave the response visible until the next user interaction or a later
    // server state update explicitly changes it.
    finish_interaction_task(interaction_generation);

finish:
    /* The standard finish helper owns the operation/power/foreground token.
     * Use it for every lifecycle stop too, but avoid drawing a terminal UI
     * from a rollback path. */
    interaction_service_finish_with_surface(interaction_generation, false);
}

static esp_err_t stop_interaction_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    uint32_t generation = 0;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_interaction_stop_requested = true;
    task = s_interaction_task;
    generation = s_interaction_generation;
    const esp_err_t exit_status = s_interaction_exit_status;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* Capture observes this board-neutral cooperative request at its next
     * bounded read. It is safe from the input, rollback, or registry task;
     * the actual stream mutex remains owned and released by the worker. */
    audio_arbitration_request_capture_stop();
    (void)operation_context_request_cancel(generation);
    if (s_host.cancel_foreground_http) s_host.cancel_foreground_http(deadline_us);
    if (device_connectivity_is_active_cellular()) {
        (void)device_connectivity_cancel_cellular_foreground_request();
    }
    if (s_interaction_start_gate) xSemaphoreGive(s_interaction_start_gate);
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_interaction_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_interaction_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_interaction_state_lock);
    const esp_err_t completed_status = s_interaction_exit_status;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "foreground interaction worker stopped");
    return ESP_OK;
}

static esp_err_t stop_interaction_task_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    task = s_interaction_task;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_interaction_task(timeout_ms);
}

bool interaction_service_start_voice(bool physical_screen_wake) {
    if (command_service_input_guarded()) {
        ESP_LOGI(TAG, "voice interaction ignored while cancel gesture drains");
        return false;
    }
    if (meeting_service_is_active()) {
        ESP_LOGW(TAG, "voice interaction ignored: meeting transition/upload active");
        return false;
    }
    bool network_available = device_connectivity_is_active_uplink_ready();
    bool gateway_paired = gateway_transport_is_paired();
    if (device_connectivity_is_provisioning_active() || !gateway_paired || !network_available) {
        ESP_LOGW(TAG,
                 "voice interaction rejected before capture: setup=%s paired=%s network=%s",
                 device_connectivity_is_provisioning_active() ? "active" : "inactive",
                 gateway_paired ? "yes" : "no",
                 network_available ? "connected" : "offline");
        scene_presenter_publish_message("暂时无法说话",
                             !network_available ? "网络未连接，请稍后重试"
                                                : "设备尚未配对或正在设置");
        return false;
    }
    // A physical tap after ambient sleep only restores the ready pet. A
    // hands-free wake phrase, however, is an intentional voice action: wake
    // the panel and continue into this same capture rather than asking the
    // user to repeat the phrase.
    if (physical_screen_wake && scene_presenter_wake_from_idle()) {
        ESP_LOGI(TAG, "sleeping display restored; voice capture deferred to next press");
        return false;
    }
    if (!physical_screen_wake && scene_presenter_wake_from_idle()) {
        ESP_LOGI(TAG, "offline wake restored sleeping display; continuing into voice capture");
    }
    if (!interaction_service_admission_take(0)) {
        ESP_LOGW(TAG, "voice interaction ignored: interaction already active");
        return false;
    }
    taskENTER_CRITICAL(&s_interaction_state_lock);
    const bool retirement_failed = s_interaction_registry_retirement_failed;
    const bool task_active = s_interaction_task != NULL || s_interaction_retiring ||
                             s_interaction_starting;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (retirement_failed || task_active) {
        interaction_service_admission_give();
        ESP_LOGW(TAG, "voice interaction rejected: previous worker has not retired");
        return false;
    }
    device_operation_context_t operation = {0};
    device_status_t operation_status = operation_context_begin(
        DEVICE_OPERATION_KIND_VOICE_INTERACTION, 0, &operation);
    if (operation_status != DEVICE_STATUS_OK) {
        interaction_service_admission_give();
        ESP_LOGW(TAG, "voice interaction operation admission rejected: status=%d",
                 (int)operation_status);
        return false;
    }
    device_power_lease_t interaction_lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t lease_status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION, &interaction_lease);
    if (lease_status != DEVICE_STATUS_OK) {
        (void)operation_context_commit_terminal(operation.generation);
        interaction_service_admission_give();
        ESP_LOGW(TAG, "voice interaction rejected: power lease unavailable status=%d",
                 (int)lease_status);
        return false;
    }
    command_service_set_display_locked(true);
    command_service_timing_begin();
    command_service_reset_cancel_state();
    // A stop request belongs to the preceding capture only. Clear it before
    // RECORDING becomes visible to the input task, so a rapid next tap is
    // retained and can end this newly started command.
    audio_arbitration_reset_capture_stop();
    reply_service_reset_for_command_start();
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_foreground_http_requested = true;
    s_interaction_power_lease = interaction_lease;
    uint32_t interaction_generation = operation.generation;
    s_interaction_generation = interaction_generation;
    s_interaction_phase = INTERACTION_SERVICE_RECORDING;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    ESP_LOGI(TAG, "voice operation started: id=%llu generation=%lu",
             (unsigned long long)operation.operation_id,
             (unsigned long)interaction_generation);
    if (physical_screen_wake) {
        // MultiNet leaves the largest internal block below this worker's 10 KiB
        // stack requirement. A physical tap can safely release the model here;
        // wake-phrase entry instead releases it inside interaction_task().
        // RECORDING was already published above, so a stop press during this
        // teardown window is routed to the new capture's stop flag instead of
        // being silently swallowed while the phase still looked idle.
        int32_t wake_stop_err = s_host.wake_word_stop ? s_host.wake_word_stop()
                                                      : (int32_t)ESP_ERR_INVALID_STATE;
        if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
            ESP_LOGW(TAG, "offline wake stop before voice task: %s",
                     esp_err_to_name((esp_err_t)wake_stop_err));
        }
        host_log_heap_snapshot("voice-before-task-create");
    }
    command_service_drain_cancel_ui_ready();
    if (!s_interaction_start_gate || !s_interaction_stopped) {
        device_power_lease_release(interaction_lease);
        taskENTER_CRITICAL(&s_interaction_state_lock);
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
        s_interaction_phase = INTERACTION_SERVICE_RESULT;
        taskEXIT_CRITICAL(&s_interaction_state_lock);
        (void)operation_context_commit_terminal(interaction_generation);
        interaction_service_admission_give();
        ESP_LOGE(TAG, "interaction lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_interaction_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_interaction_stop_requested = false;
    s_interaction_starting = true;
    s_interaction_exit_status = ESP_OK;
    s_interaction_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    scene_presenter_publish_command_display_lock(true);
    scene_presenter_cancel_ready_prompt();
    TaskHandle_t created_handle = NULL;
    // Keep the command worker stack in internal RAM.  It calls Wi-Fi/TLS and
    // its callbacks can run while the flash cache is temporarily disabled;
    // a PSRAM-backed task stack is then unsafe and manifests as an intermittent
    // reboot immediately after the six-second recording completes.  Payloads
    // and HTTP buffers still use PSRAM, so this only reserves a small, stable
    // internal stack for control flow.
    BaseType_t created = xTaskCreate(interaction_task, "maclaw_interaction",
                                     10240, (void *)(uintptr_t)interaction_generation,
                                     5, &created_handle);
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_interaction_task = created == pdPASS ? created_handle : NULL;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_interaction_state_lock);
        s_foreground_http_requested = false;
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
        s_interaction_phase = INTERACTION_SERVICE_RESULT;
        s_interaction_starting = false;
        taskEXIT_CRITICAL(&s_interaction_state_lock);
        device_power_lease_release(interaction_lease);
        (void)operation_context_commit_terminal(interaction_generation);
        host_log_heap_snapshot("interaction-task-create-fail");
        interaction_service_admission_give();
        host_schedule_wake_restart();
        ambient_service_apply_pet_state("alert");
        scene_presenter_publish_message("操作失败", "无法启动语音任务");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_INTERACTION,
        .name = "foreground_interaction",
        .context = (void *)created_handle,
        .stop = stop_interaction_task_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register foreground interaction: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_interaction_state_lock);
        s_interaction_stop_requested = true;
        taskEXIT_CRITICAL(&s_interaction_state_lock);
        xSemaphoreGive(s_interaction_start_gate);
        (void)stop_interaction_task(500);
        return false;
    }
    // Release only after the task handle and Registry identity are visible to
    // cancellation/reply correlation. No worker side effect can escape this
    // task's lifecycle contract during the create-to-register window.
    xSemaphoreGive(s_interaction_start_gate);
    return true;
}

device_status_t interaction_service_init(const interaction_service_host_t *host) {
    if (!host || !host->ensure_gateway_poll_task ||
        !host->upload_voice || !host->send_voice_event || !host->wake_word_stop ||
        !host->cancel_foreground_http || !host->log_heap_snapshot ||
        !host->schedule_wake_restart) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    s_host = *host;
    s_host_installed = true;
    taskENTER_CRITICAL(&s_interaction_state_lock);
    s_interaction_retiring = false;
    s_interaction_exit_status = ESP_OK;
    s_interaction_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_interaction_state_lock);
    s_interaction_start_gate = xSemaphoreCreateBinary();
    if (!s_interaction_start_gate) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_interaction_stopped = xSemaphoreCreateBinary();
    if (!s_interaction_stopped) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    s_interaction_lock = xSemaphoreCreateBinary();
    if (!s_interaction_lock || xSemaphoreGive(s_interaction_lock) != pdTRUE) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    return DEVICE_STATUS_OK;
}
