#include "round_display_service.h"

#include "provisioning_failure_injection.h"

/* Keep FreeRTOS implementation details in this source owner. The private
 * header deliberately exposes only millisecond animation semantics. */
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

/* Exactly one source owner includes the selected profile display adapter.
 * The common scene renderer never sees these controller-level helpers. */
#include "boards/round_display_adapter.h"

/* These are physical-display facts, intentionally private to this service.
 * `board_port.c` still owns whether a scene is eligible to sleep; it never
 * owns the profile-specific pending-brightness or wake ordering transaction. */
static unsigned s_round_display_brightness;
static bool s_round_display_off;
static SemaphoreHandle_t s_round_display_animation_lock;
/* Publish the task handle and callback generation before profile scheduling
 * is allowed to enter the shared renderer. A binary semaphore, rather than a
 * volatile spin loop, provides the cross-core ordering required by this
 * handoff. */
static SemaphoreHandle_t s_round_display_animation_started;
static SemaphoreHandle_t s_round_display_animation_stopped;
static TaskHandle_t s_round_display_animation_task;
static TaskHandle_t s_round_display_animation_owner;
/* Read by the animation task outside the lifecycle mutex after its
 * notification wait. Keep the stop request visibly shared; the task
 * notification is still the wakeup mechanism. */
static volatile bool s_round_display_animation_stop_requested;
static bool s_round_display_animation_completed;
static round_display_service_animation_fn_t s_round_display_animation_entry;
static void *s_round_display_animation_context;
/* A completed DMA fence alone cannot stop the next decorative frame from
 * being submitted by the retained round animator. Keep the reversible park
 * wholly below the selected Display profile: Platform Display receives only
 * a value result and no round-screen/controller/RTOS detail escapes. */
static bool s_round_display_animation_system_sleep_preparing;
static SemaphoreHandle_t s_round_display_animation_system_sleep_quiesced;

static void round_display_service_test_delay(uint32_t delay_ms) {
    if (delay_ms == 0) return;
    TickType_t ticks = pdMS_TO_TICKS(delay_ms);
    if (ticks == 0) ticks = 1;
    vTaskDelay(ticks);
}

int round_display_service_width(void) {
    return round_display_adapter_width();
}

int round_display_service_height(void) {
    return round_display_adapter_height();
}

int round_display_service_transfer_stripe_rows(void) {
    return round_display_adapter_transfer_stripe_rows();
}

uint32_t round_display_service_pet_animation_frame_ms(void) {
    return round_display_adapter_pet_animation_frame_ms();
}

static bool round_display_service_park_for_system_sleep(void) {
    if (!s_round_display_animation_lock) return true;
    for (;;) {
        bool preparing = false;
        SemaphoreHandle_t quiesced = NULL;
        (void)xSemaphoreTake(s_round_display_animation_lock, portMAX_DELAY);
        preparing = s_round_display_animation_system_sleep_preparing;
        quiesced = s_round_display_animation_system_sleep_quiesced;
        (void)xSemaphoreGive(s_round_display_animation_lock);
        if (!preparing) return !s_round_display_animation_stop_requested;
        if (quiesced) xSemaphoreGive(quiesced);
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
    }
}

static void round_display_service_animation_task(void *arg) {
    (void)arg;
    /* xTaskCreate may schedule this task before its creator receives the
     * returned handle. The service opens this gate only after it publishes the
     * handle, callback and completion generation under its lifecycle lock. */
    if (!s_round_display_animation_started ||
        xSemaphoreTake(s_round_display_animation_started, portMAX_DELAY) != pdTRUE) {
        vTaskDelete(NULL);
        return;
    }
    s_round_display_animation_owner = xTaskGetCurrentTaskHandle();
    round_display_service_animation_fn_t entry = s_round_display_animation_entry;
    if (round_display_service_park_for_system_sleep() && entry) {
        entry(s_round_display_animation_context);
    }
    /* The task must not publish a completion while the stopper still has a
     * notification target for it.  The lifecycle lock serializes this final
     * hand-off with stop/start and prevents a recycled task handle from being
     * notified by a later generation. */
    /* The `started` gate is opened only after the creator has taken the
     * lifecycle mutex, so this handle is part of the published generation.
     * Do not add a volatile spin/yield fallback here. */
    (void)xSemaphoreTake(s_round_display_animation_lock, portMAX_DELAY);
    const uint32_t pre_completion_delay_ms =
        provisioning_failure_injection_round_display_animation_pre_completion_delay_ms();
    if (pre_completion_delay_ms != 0) {
        ESP_LOGW("round_display",
                 "test: delaying animation completion for %lu ms while lifecycle lock held",
                 (unsigned long)pre_completion_delay_ms);
        round_display_service_test_delay(pre_completion_delay_ms);
    }
    if (s_round_display_animation_stopped) {
        (void)xSemaphoreGive(s_round_display_animation_stopped);
    }
    const uint32_t post_completion_delay_ms =
        provisioning_failure_injection_round_display_animation_post_completion_delay_ms();
    if (post_completion_delay_ms != 0) {
        ESP_LOGW("round_display",
                 "test: delaying animation cleanup for %lu ms while lifecycle lock held",
                 (unsigned long)post_completion_delay_ms);
        round_display_service_test_delay(post_completion_delay_ms);
    }
    /* A generation can finish after PREPARE snapshots it as active but
     * before it reaches its next animation wait. Completion is itself a
     * renderer-safe boundary, so acknowledge the same transaction rather
     * than making PREPARE time out waiting for a nonexistent next wait. */
    if (s_round_display_animation_system_sleep_preparing &&
        s_round_display_animation_system_sleep_quiesced) {
        (void)xSemaphoreGive(s_round_display_animation_system_sleep_quiesced);
    }
    s_round_display_animation_owner = NULL;
    s_round_display_animation_completed = true;
    (void)xSemaphoreGive(s_round_display_animation_lock);
    vTaskDelete(NULL);
}

static esp_err_t round_display_service_animation_lock_take(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_round_display_animation_lock) {
        s_round_display_animation_lock = xSemaphoreCreateMutex();
        if (!s_round_display_animation_lock) return ESP_ERR_NO_MEM;
    }
    return xSemaphoreTake(s_round_display_animation_lock, pdMS_TO_TICKS(timeout_ms)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

esp_err_t round_display_service_initialize(unsigned brightness) {
    if (brightness > 100) return ESP_ERR_INVALID_ARG;
    const esp_err_t err = round_display_adapter_init_hardware(brightness);
    if (err == ESP_OK) {
        s_round_display_brightness = brightness;
        s_round_display_off = false;
    }
    return err;
}
bool round_display_service_ready(void) { return round_display_adapter_ready(); }
esp_err_t round_display_service_set_brightness(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    /* While DISPLAY_OFF is active the requested level is deliberately
     * pending.  Touch/key wake restores both controller visibility and this
     * level in one adapter-owned transaction. */
    if (s_round_display_off) {
        s_round_display_brightness = percent;
        return ESP_OK;
    }
    const esp_err_t err = round_display_adapter_apply_brightness(percent);
    if (err == ESP_OK) s_round_display_brightness = percent;
    return err;
}
esp_err_t round_display_service_enter_display_off(void) {
    if (s_round_display_off) return ESP_OK;
    const esp_err_t err = round_display_adapter_enter_display_off();
    if (err == ESP_OK) s_round_display_off = true;
    return err;
}
esp_err_t round_display_service_wake_from_display_off(void) {
    if (!s_round_display_off) return ESP_OK;
    const esp_err_t err = round_display_adapter_wake_from_display_off(
        s_round_display_brightness);
    if (err == ESP_OK) s_round_display_off = false;
    return err;
}
esp_err_t round_display_service_wait_for_scanout_idle(uint32_t timeout_ms) {
    return round_display_adapter_wait_for_transfer_idle(timeout_ms);
}
esp_err_t round_display_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    if (budget == 0) budget = 1;
    ESP_RETURN_ON_ERROR(round_display_service_animation_lock_take(timeout_ms), "round_display",
                        "system-sleep animation lifecycle lock timeout");
    if (s_round_display_animation_system_sleep_preparing) {
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_round_display_animation_system_sleep_quiesced) {
        s_round_display_animation_system_sleep_quiesced = xSemaphoreCreateBinary();
        if (!s_round_display_animation_system_sleep_quiesced) {
            (void)xSemaphoreGive(s_round_display_animation_lock);
            return ESP_ERR_NO_MEM;
        }
    }
    while (xSemaphoreTake(s_round_display_animation_system_sleep_quiesced, 0) == pdTRUE) {
    }
    const bool active = s_round_display_animation_task != NULL &&
                        !s_round_display_animation_completed;
    s_round_display_animation_system_sleep_preparing = true;
    if (active) xTaskNotifyGive(s_round_display_animation_task);
    (void)xSemaphoreGive(s_round_display_animation_lock);
    if (!active) return ESP_OK;
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (remaining == 0 ||
        xSemaphoreTake(s_round_display_animation_system_sleep_quiesced, remaining) != pdTRUE) {
        /* Preserve the animation admission fence until the owning Power
         * transaction performs reverse-order ABORT. Reopening here could
         * issue a late renderer/controller operation while sibling rollback
         * is still in progress. */
        return ESP_ERR_TIMEOUT;
    }
    return ESP_OK;
}

void round_display_service_abort_system_sleep_prepare(void) {
    if (!s_round_display_animation_lock ||
        xSemaphoreTake(s_round_display_animation_lock, pdMS_TO_TICKS(3000)) != pdTRUE) return;
    if (s_round_display_animation_system_sleep_preparing) {
        s_round_display_animation_system_sleep_preparing = false;
        if (s_round_display_animation_task && !s_round_display_animation_completed) {
            xTaskNotifyGive(s_round_display_animation_task);
        }
    }
    (void)xSemaphoreGive(s_round_display_animation_lock);
}
esp_err_t round_display_service_draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                                  const void *pixels) {
    return round_display_adapter_draw_bitmap_sync(x0, y0, x1, y1, pixels);
}
uint16_t *round_display_service_stripe_buffer(void) {
    return round_display_adapter_stripe_buffer();
}
uint16_t round_display_service_rgb565(uint8_t r, uint8_t g, uint8_t b) {
    return round_display_adapter_rgb565(r, g, b);
}
uint16_t round_display_service_rgb565_lerp(uint16_t from, uint16_t to, uint8_t amount) {
    return round_display_adapter_rgb565_lerp(from, to, amount);
}
void round_display_service_align_dirty_columns(int *left, int *right, int width) {
    round_display_adapter_align_dirty_columns(left, right, width);
}
uint16_t *round_display_service_allocate_framebuffer(size_t bytes) {
    return round_display_adapter_allocate_framebuffer(bytes);
}
uint16_t *round_display_service_allocate_ambient_overlay(size_t bytes) {
    return round_display_adapter_allocate_ambient_overlay(bytes);
}
void round_display_service_free_render_buffer(void *buffer) {
    round_display_adapter_free_render_buffer(buffer);
}
uint8_t *round_display_service_allocate_remote_pet_frame(size_t bytes) {
    return round_display_adapter_allocate_remote_pet_frame(bytes);
}
void round_display_service_free_remote_pet_frame(void *buffer) {
    round_display_adapter_free_remote_pet_frame(buffer);
}
void round_display_service_release_consumed_pet_source(void *frame) {
    round_display_adapter_release_consumed_pet_source(frame);
}
esp_err_t round_display_service_start_animation(round_display_service_animation_fn_t entry,
                                                 void *context) {
    if (!entry) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(round_display_service_animation_lock_take(1000), "round_display",
                        "animation lifecycle lock timeout");
    if (s_round_display_animation_system_sleep_preparing) {
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (s_round_display_animation_task) {
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_round_display_animation_started = xSemaphoreCreateBinary();
    s_round_display_animation_stopped = xSemaphoreCreateBinary();
    if (!s_round_display_animation_started || !s_round_display_animation_stopped) {
        if (s_round_display_animation_started) {
            vSemaphoreDelete(s_round_display_animation_started);
        }
        if (s_round_display_animation_stopped) {
            vSemaphoreDelete(s_round_display_animation_stopped);
        }
        s_round_display_animation_started = NULL;
        s_round_display_animation_stopped = NULL;
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_NO_MEM;
    }
    s_round_display_animation_entry = entry;
    s_round_display_animation_context = context;
    s_round_display_animation_stop_requested = false;
    s_round_display_animation_completed = false;
    const BaseType_t created = round_display_adapter_start_pet_animation_task(
        round_display_service_animation_task, &s_round_display_animation_task);
    if (created != pdPASS) {
        vSemaphoreDelete(s_round_display_animation_started);
        vSemaphoreDelete(s_round_display_animation_stopped);
        s_round_display_animation_started = NULL;
        s_round_display_animation_stopped = NULL;
        s_round_display_animation_entry = NULL;
        s_round_display_animation_context = NULL;
        s_round_display_animation_task = NULL;
        s_round_display_animation_owner = NULL;
        s_round_display_animation_completed = false;
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_NO_MEM;
    }
    /* The handle is stable now. A stop issued after this point can safely
     * notify it before the worker begins its semantic wait. */
    (void)xSemaphoreGive(s_round_display_animation_started);
    (void)xSemaphoreGive(s_round_display_animation_lock);
    return ESP_OK;
}

bool round_display_service_animation_wait_ms(uint32_t timeout_ms) {
    if (!s_round_display_animation_task || timeout_ms == 0) return false;
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(timeout_ms));
    return !s_round_display_animation_stop_requested &&
           round_display_service_park_for_system_sleep();
}

bool round_display_service_animation_running(void) {
    /* Keep a timed-out generation visibly present until its completion is
     * actually consumed.  A later stop must join the original task, never
     * treat a pending stop as permission to start/reuse a task identity. */
    return s_round_display_animation_task != NULL;
}

esp_err_t round_display_service_stop_animation(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    ESP_RETURN_ON_ERROR(round_display_service_animation_lock_take(timeout_ms), "round_display",
                        "animation lifecycle lock timeout");
    if (!s_round_display_animation_task) {
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_OK;
    }
    /* A prior bounded stop may have consumed the completion semaphore just as
     * its deadline expired. The worker has already published a fully-dead
     * generation under this lock, so a later stop can finish cleanup without
     * waiting for a second completion that will never arrive. */
    if (s_round_display_animation_completed) {
        vSemaphoreDelete(s_round_display_animation_started);
        s_round_display_animation_started = NULL;
        vSemaphoreDelete(s_round_display_animation_stopped);
        s_round_display_animation_stopped = NULL;
        s_round_display_animation_entry = NULL;
        s_round_display_animation_context = NULL;
        s_round_display_animation_task = NULL;
        s_round_display_animation_owner = NULL;
        s_round_display_animation_stop_requested = false;
        s_round_display_animation_completed = false;
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == s_round_display_animation_owner) {
        (void)xSemaphoreGive(s_round_display_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_round_display_animation_stop_requested) {
        s_round_display_animation_stop_requested = true;
        xTaskNotifyGive(s_round_display_animation_task);
    }
    /* The worker takes this same lock to publish completion, so never wait
     * while holding it. `s_round_display_animation_task` stays non-NULL until
     * the completion semaphore is consumed, which still prevents a new
     * generation from starting in this gap. */
    (void)xSemaphoreGive(s_round_display_animation_lock);
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (!s_round_display_animation_stopped || remaining == 0 ||
        xSemaphoreTake(s_round_display_animation_stopped, remaining) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    const TickType_t after_join_elapsed = xTaskGetTickCount() - started;
    const TickType_t after_join_remaining = after_join_elapsed >= deadline
                                                ? 0 : deadline - after_join_elapsed;
    if (after_join_remaining == 0 ||
        round_display_service_animation_lock_take(
            (uint32_t)(after_join_remaining * portTICK_PERIOD_MS)) != ESP_OK) {
        /* Completion has been consumed, but no other task can start while
         * the task identity remains published. Preserve its resources for a
         * later stop instead of deleting them without the lifecycle lock. */
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_round_display_animation_started);
    s_round_display_animation_started = NULL;
    vSemaphoreDelete(s_round_display_animation_stopped);
    s_round_display_animation_stopped = NULL;
    s_round_display_animation_entry = NULL;
    s_round_display_animation_context = NULL;
    s_round_display_animation_task = NULL;
    s_round_display_animation_owner = NULL;
    s_round_display_animation_stop_requested = false;
    s_round_display_animation_completed = false;
    (void)xSemaphoreGive(s_round_display_animation_lock);
    return ESP_OK;
}

static void round_display_service_animation_deadline_test_entry(void *context) {
    (void)context;
    while (round_display_service_animation_wait_ms(1000)) {}
}

esp_err_t round_display_service_run_animation_deadline_test(void) {
    if (!provisioning_failure_injection_round_display_animation_deadline_test_enabled()) {
        return ESP_OK;
    }
    /* The worker consumes 70 ms before completion and holds the same mutex for
     * another 70 ms after completion. A 100 ms stopper must time out around
     * its original deadline. The test fails if cleanup incorrectly gets a
     * fresh full timeout after completion publication. */
    const uint32_t stop_timeout_ms = 100u;
    const TickType_t started = xTaskGetTickCount();
    ESP_LOGW("round_display", "test: starting animation stop-deadline proof");
    ESP_RETURN_ON_ERROR(round_display_service_start_animation(
                            round_display_service_animation_deadline_test_entry, NULL),
                        "round_display", "test animation start failed");
    vTaskDelay(1);
    const esp_err_t first_stop = round_display_service_stop_animation(stop_timeout_ms);
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t budget = pdMS_TO_TICKS(stop_timeout_ms);
    const TickType_t tolerance = pdMS_TO_TICKS(30u) == 0 ? 1 : pdMS_TO_TICKS(30u);
    if (first_stop != ESP_ERR_TIMEOUT || elapsed > budget + tolerance) {
        ESP_LOGE("round_display",
                 "test: animation stop deadline failed: status=%s elapsed=%lu budget=%lu",
                 esp_err_to_name(first_stop), (unsigned long)elapsed,
                 (unsigned long)budget);
        return first_stop == ESP_OK ? ESP_FAIL : first_stop;
    }
    /* Let the owner publish the fully-dead generation, then exercise the
     * normal idempotent later-stop cleanup path. */
    round_display_service_test_delay(160u);
    const esp_err_t cleanup = round_display_service_stop_animation(1000u);
    if (cleanup != ESP_OK) {
        ESP_LOGE("round_display", "test: animation late cleanup failed: %s",
                 esp_err_to_name(cleanup));
        return cleanup;
    }
    ESP_LOGI("round_display",
             "test: PASS animation stop deadline elapsed=%lu ticks budget=%lu",
             (unsigned long)elapsed, (unsigned long)budget);
    return ESP_OK;
}

const char *round_display_service_ambient_overlay_memory_name(void) {
    return round_display_adapter_ambient_overlay_memory_name();
}
bool round_display_service_has_startup_art(void) {
    return round_display_adapter_has_startup_art();
}
void round_display_service_compose_startup_art(uint16_t *frame, int width, int height) {
    round_display_adapter_compose_startup_art(frame, width, height);
}
