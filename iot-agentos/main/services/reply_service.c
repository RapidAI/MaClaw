#include "services/reply_service.h"

#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "app_ui.h"
#include "presentation/scene_presenter.h"
#include "services/ambient_service.h"
#include "operation_context.h"
#include "services/command_service.h"
#include "services/foreground_coordinator.h"
#include "services/interaction_service.h"

/* Keep the log tag identical to the original main.c owner so existing reply /
 * correlation trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define CANCELLED_REPLY_SLOTS 4
#define RESULT_SPEECH_IDLE_TIMEOUT_US (5LL * 60LL * 1000000LL)

static portMUX_TYPE s_reply_state_lock = portMUX_INITIALIZER_UNLOCKED;

static char s_active_command_reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY];
// A terminal text can deliberately precede its TTS parts. Retain only that
// exact correlation and its declared remaining part count after the command
// worker exits, so result-page speech is accepted without admitting stale audio.
// The idle deadline also bounds a partially generated/failed multipart reply;
// each successfully consumed part refreshes it for the next part.
static char s_result_speech_reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY];
static unsigned s_result_speech_parts_remaining;
static int64_t s_result_speech_deadline_us;
static char s_cancelled_command_reply_to[CANCELLED_REPLY_SLOTS][REPLY_SERVICE_REPLY_ID_CAPACITY];
static unsigned s_cancelled_command_reply_next;

void reply_service_set_active_reply_to(const char *reply_to) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    strlcpy(s_active_command_reply_to, reply_to ? reply_to : "",
            sizeof(s_active_command_reply_to));
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

void reply_service_clear_active_reply_to(void) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    s_active_command_reply_to[0] = '\0';
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

void reply_service_copy_active_reply_to(char *out_reply_to, uint32_t capacity) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    strlcpy(out_reply_to, s_active_command_reply_to, capacity);
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

bool reply_service_correlation_matches(const char *reply_to) {
    bool matches = false;
    if (!reply_to || !reply_to[0]) return false;
    taskENTER_CRITICAL(&s_reply_state_lock);
    matches = s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_reply_state_lock);
    return matches;
}

bool reply_service_active_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    interaction_service_snapshot_t snapshot;
    interaction_service_snapshot(&snapshot);
    bool matches;
    taskENTER_CRITICAL(&s_reply_state_lock);
    matches = snapshot.task_active && s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_reply_state_lock);
    return matches;
}

bool reply_service_active_matches_after_handoff(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    for (unsigned attempt = 0; attempt < 20; ++attempt) {
        if (reply_service_active_matches(reply_to)) return true;
        interaction_service_snapshot_t snapshot;
        interaction_service_snapshot(&snapshot);
        bool awaiting_correlation;
        taskENTER_CRITICAL(&s_reply_state_lock);
        awaiting_correlation = snapshot.task_active &&
                               snapshot.processing &&
                               !command_service_cancel_requested() &&
                               !s_active_command_reply_to[0];
        taskEXIT_CRITICAL(&s_reply_state_lock);
        if (!awaiting_correlation) break;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    return reply_service_active_matches(reply_to);
}

uintptr_t reply_service_begin_active_reply(void) {
    // Atomically close the cancellation window and take a stable waiter
    // snapshot before drawing. A simultaneous double tap then observes either
    // a cancellable command or a completed one, never a half-transition.
    uintptr_t waiter = 0;
    uint32_t generation = 0;
    if (!command_service_cancel_requested()) {
        interaction_service_snapshot_t snapshot;
        interaction_service_snapshot(&snapshot);
        generation = snapshot.generation;
    }
    /* Win the terminal token before any display side effect. A concurrent
     * cancellation that arrives after this point observes terminal_committed
     * and is discarded instead of painting "cancelled" over a final reply. */
    if (!generation || !operation_context_commit_terminal(generation)) return 0;
    waiter = interaction_service_claim_reply_waiter(generation);
    scene_presenter_publish_command_cancel_enabled(false);
    if (waiter) {
        /* The result page (plus any declared result speech) now owns the
         * foreground; the voice-capture lease ends at this terminal
         * transition. */
        foreground_coordinator_observe_release(FOREGROUND_OWNER_COMMAND_VOICE);
        foreground_coordinator_observe_acquire(FOREGROUND_OWNER_COMMAND_RESULT,
                                               FOREGROUND_PRIORITY_RESULT,
                                               FOREGROUND_SCENE_COMMAND_RESULT);
    }
    return waiter;
}

void reply_service_complete_active_text_reply(uintptr_t waiter,
                                              const char *title,
                                              const char *text) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal text transition: waiter=%p bytes=%u", (void *)waiter,
             (unsigned)(text ? strlen(text) : 0));
    interaction_service_notify_waiter(waiter);
    // The notification only makes the higher-priority interaction worker
    // runnable; it may not actually leave the timed wait before this poll task
    // reaches the LCD.  Clear the thinking surface synchronously as part of
    // the terminal transition so its mouth animator is unable to repaint over
    // the first result frame even under TLS/HTTP load.
    ambient_service_apply_pet_state("speaking");
    scene_presenter_publish_response(title, text);
}

void reply_service_complete_active_image_reply(uintptr_t waiter,
                                               const char *title,
                                               const char *caption,
                                               const uint16_t *pixels,
                                               size_t width,
                                               size_t height) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal image transition: waiter=%p size=%ux%u", (void *)waiter,
             (unsigned)width, (unsigned)height);
    interaction_service_notify_waiter(waiter);
    ambient_service_apply_pet_state("speaking");
    scene_presenter_publish_response_image(title, caption, pixels, width, height);
}

void reply_service_remember_cancelled(void) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    if (s_active_command_reply_to[0]) {
        bool already_remembered = false;
        for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
            if (!strcmp(s_cancelled_command_reply_to[i], s_active_command_reply_to)) {
                already_remembered = true;
                break;
            }
        }
        if (!already_remembered) {
            strlcpy(s_cancelled_command_reply_to[s_cancelled_command_reply_next],
                    s_active_command_reply_to, REPLY_SERVICE_REPLY_ID_CAPACITY);
            s_cancelled_command_reply_next =
                (s_cancelled_command_reply_next + 1) % CANCELLED_REPLY_SLOTS;
        }
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

bool reply_service_cancelled_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    taskENTER_CRITICAL(&s_reply_state_lock);
    for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
        if (s_cancelled_command_reply_to[i][0] &&
            !strcmp(s_cancelled_command_reply_to[i], reply_to)) {
            matches = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
    return matches;
}

bool reply_service_result_speech_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    bool expired = false;
    unsigned expired_parts = 0;
    char expired_reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY] = {0};
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_reply_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        s_result_speech_reply_to[0] &&
        s_result_speech_deadline_us > 0 &&
        now_us >= s_result_speech_deadline_us) {
        expired = true;
        expired_parts = s_result_speech_parts_remaining;
        strlcpy(expired_reply_to, s_result_speech_reply_to,
                sizeof(expired_reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    } else {
        matches = s_result_speech_parts_remaining > 0 &&
                  s_result_speech_reply_to[0] &&
                  !strcmp(s_result_speech_reply_to, reply_to);
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
    if (expired) {
        ESP_LOGW(TAG, "result speech expired after idle timeout: replyTo=%s missing=%u next=%s",
                 expired_reply_to, expired_parts, reply_to);
    }
    return matches;
}

void reply_service_arm_result_speech(const char *reply_to, unsigned parts) {
    if (!reply_to || !reply_to[0] || parts == 0) return;
    int64_t deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_reply_state_lock);
    strlcpy(s_result_speech_reply_to, reply_to, sizeof(s_result_speech_reply_to));
    s_result_speech_parts_remaining = parts;
    s_result_speech_deadline_us = deadline_us;
    taskEXIT_CRITICAL(&s_reply_state_lock);
    ESP_LOGI(TAG, "result speech armed: replyTo=%s parts=%u idleTimeout=%us",
             reply_to, parts,
             (unsigned)(RESULT_SPEECH_IDLE_TIMEOUT_US / 1000000LL));
}

void reply_service_finish_result_speech_part(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned remaining = 0;
    int64_t next_deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_reply_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        --s_result_speech_parts_remaining;
        remaining = s_result_speech_parts_remaining;
        if (remaining == 0) {
            s_result_speech_reply_to[0] = '\0';
            s_result_speech_deadline_us = 0;
        } else {
            s_result_speech_deadline_us = next_deadline_us;
        }
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
    ESP_LOGI(TAG, "result speech part complete: replyTo=%s remaining=%u",
             reply_to, remaining);
}

void reply_service_finish_result_speech(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned missing = 0;
    taskENTER_CRITICAL(&s_reply_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        missing = s_result_speech_parts_remaining;
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
    ESP_LOGW(TAG, "result speech transaction closed early: replyTo=%s missing=%u",
             reply_to, missing);
}

void reply_service_dismiss_result_speech(void) {
    unsigned missing = 0;
    char reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY] = {0};
    taskENTER_CRITICAL(&s_reply_state_lock);
    if (s_result_speech_parts_remaining > 0 && s_result_speech_reply_to[0]) {
        missing = s_result_speech_parts_remaining;
        strlcpy(reply_to, s_result_speech_reply_to, sizeof(reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_reply_state_lock);
    if (missing) {
        ESP_LOGI(TAG, "result speech dismissed with response: replyTo=%s skipped=%u",
                 reply_to, missing);
    }
}

void reply_service_clear_result_speech(void) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    s_result_speech_reply_to[0] = '\0';
    s_result_speech_parts_remaining = 0;
    s_result_speech_deadline_us = 0;
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

void reply_service_reset_for_command_start(void) {
    taskENTER_CRITICAL(&s_reply_state_lock);
    s_active_command_reply_to[0] = '\0';
    s_result_speech_reply_to[0] = '\0';
    s_result_speech_parts_remaining = 0;
    s_result_speech_deadline_us = 0;
    taskEXIT_CRITICAL(&s_reply_state_lock);
}

device_status_t reply_service_init(void) {
    return DEVICE_STATUS_OK;
}
