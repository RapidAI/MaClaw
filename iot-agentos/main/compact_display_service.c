#include "compact_display_service.h"

#include "provisioning_failure_injection.h"

/* The selector is the only compact profile mapping.  The shared renderer
 * cannot see controller handles, SPI callbacks or GPIO. */
#include "boards/compact_display_adapter.h"

#include "freertos/semphr.h"
#include "freertos/task.h"

static SemaphoreHandle_t s_transfer_done;

typedef struct {
    TaskHandle_t task;
    TaskHandle_t owner;
    /* The profile task may run on another core before its creator receives
     * the returned handle.  This gate is the explicit publication barrier:
     * the worker cannot read its slot or invoke scene code until the service
     * has stored the handle and published the complete generation.  Do not
     * replace it with a volatile spin/yield; that would not establish the
     * required cross-core memory ordering. */
    SemaphoreHandle_t started;
    SemaphoreHandle_t stopped;
    compact_display_service_animation_fn_t entry;
    void *context;
    /* `started` publishes creation; the lifecycle mutex serializes creation
     * and cleanup. This flag is also read by the worker's notification wait
     * loop without taking that mutex, so retain volatile visibility for the
     * cross-task stop request. The notification provides the actual wakeup. */
    volatile bool stop_requested;
    bool completed;
} compact_display_animation_slot_t;

static SemaphoreHandle_t s_animation_lock;
static compact_display_animation_slot_t s_animations[COMPACT_DISPLAY_ANIMATION_COUNT];

static void compact_display_service_test_delay(uint32_t delay_ms) {
    if (delay_ms == 0) return;
    TickType_t ticks = pdMS_TO_TICKS(delay_ms);
    if (ticks == 0) ticks = 1;
    vTaskDelay(ticks);
}

static bool compact_display_animation_kind_valid(
    compact_display_service_animation_kind_t kind) {
    return kind >= COMPACT_DISPLAY_ANIMATION_THINKING &&
           kind < COMPACT_DISPLAY_ANIMATION_COUNT;
}

static esp_err_t compact_display_animation_lock_take(uint32_t timeout_ms) {
    if (!s_animation_lock) {
        s_animation_lock = xSemaphoreCreateMutex();
        if (!s_animation_lock) return ESP_ERR_NO_MEM;
    }
    return xSemaphoreTake(s_animation_lock, pdMS_TO_TICKS(timeout_ms)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static BaseType_t compact_display_service_start_animation_task(
    compact_display_service_animation_kind_t kind, TaskFunction_t entry,
    void *context, TaskHandle_t *out_task) {
    switch (kind) {
        case COMPACT_DISPLAY_ANIMATION_THINKING:
            return compact_display_adapter_start_thinking_animation_task(entry, context,
                                                                          out_task);
        case COMPACT_DISPLAY_ANIMATION_PET:
            return compact_display_adapter_start_pet_animation_task(entry, context, out_task);
        default:
            return pdFAIL;
    }
}

static void compact_display_service_animation_task(void *arg) {
    const compact_display_service_animation_kind_t kind =
        (compact_display_service_animation_kind_t)(uintptr_t)arg;
    if (!compact_display_animation_kind_valid(kind)) {
        vTaskDelete(NULL);
        return;
    }

    compact_display_animation_slot_t *slot = &s_animations[kind];
    if (!slot->started || xSemaphoreTake(slot->started, portMAX_DELAY) != pdTRUE) {
        vTaskDelete(NULL);
        return;
    }
    slot->owner = xTaskGetCurrentTaskHandle();
    compact_display_service_animation_fn_t entry = slot->entry;
    if (entry) entry(slot->context);

    /* `started` is given only after the creator has taken this lifecycle
     * mutex. The gate therefore publishes both the task generation and the
     * non-null lock; no spin/yield publication fallback is permitted here. */
    (void)xSemaphoreTake(s_animation_lock, portMAX_DELAY);
    const uint32_t pre_completion_delay_ms =
        provisioning_failure_injection_compact_display_animation_pre_completion_delay_ms();
    if (pre_completion_delay_ms != 0) {
        ESP_LOGW("compact_display",
                 "test: delaying animation completion for %lu ms while lifecycle lock held",
                 (unsigned long)pre_completion_delay_ms);
        compact_display_service_test_delay(pre_completion_delay_ms);
    }
    if (slot->stopped) (void)xSemaphoreGive(slot->stopped);
    const uint32_t post_completion_delay_ms =
        provisioning_failure_injection_compact_display_animation_post_completion_delay_ms();
    if (post_completion_delay_ms != 0) {
        ESP_LOGW("compact_display",
                 "test: delaying animation cleanup for %lu ms while lifecycle lock held",
                 (unsigned long)post_completion_delay_ms);
        compact_display_service_test_delay(post_completion_delay_ms);
    }
    slot->owner = NULL;
    slot->completed = true;
    (void)xSemaphoreGive(s_animation_lock);
    vTaskDelete(NULL);
}

int compact_display_service_width(void) { return compact_display_adapter_width(); }
int compact_display_service_height(void) { return compact_display_adapter_height(); }
unsigned compact_display_service_default_brightness(void) {
    return compact_display_adapter_default_brightness();
}
int compact_display_service_transfer_stripe_rows(void) {
    return compact_display_adapter_transfer_stripe_rows();
}
bool compact_display_service_ready(void) { return compact_display_adapter_ready(); }

esp_err_t compact_display_service_initialize(void) {
    if (s_transfer_done || compact_display_adapter_ready()) return ESP_ERR_INVALID_STATE;
    s_transfer_done = xSemaphoreCreateBinary();
    if (!s_transfer_done) return ESP_ERR_NO_MEM;
    const esp_err_t err = compact_display_adapter_init_hardware(s_transfer_done);
    if (err != ESP_OK) {
        vSemaphoreDelete(s_transfer_done);
        s_transfer_done = NULL;
    }
    return err;
}

void compact_display_service_discard_unpublished_state(void) {
    if (s_transfer_done) {
        vSemaphoreDelete(s_transfer_done);
        s_transfer_done = NULL;
    }
}

esp_err_t compact_display_service_set_brightness(unsigned percent) {
    return compact_display_adapter_set_brightness(percent);
}
esp_err_t compact_display_service_enter_display_off(void) {
    return compact_display_enter_display_off();
}
esp_err_t compact_display_service_wake_from_display_off(unsigned brightness) {
    return compact_display_wake_from_display_off(brightness);
}
esp_err_t compact_display_service_draw_bitmap_sync(int x0, int y0, int x1, int y1,
                                                    const void *pixels) {
    return compact_display_adapter_draw_bitmap_sync(s_transfer_done, x0, y0, x1, y1, pixels);
}
bool compact_display_service_uses_delta_presentation(void) {
    return compact_display_adapter_uses_delta_presentation();
}
bool compact_display_service_uses_profile_thinking_patch(void) {
    return compact_display_adapter_uses_profile_thinking_patch();
}
bool compact_display_service_compose_thinking_patch(
    uint16_t *pixels, size_t pixel_capacity, unsigned phase, uint16_t background,
    compact_display_animation_patch_t *out_patch) {
    return compact_display_adapter_compose_thinking_patch(
        pixels, pixel_capacity, phase, background, out_patch);
}
compact_startup_full_frame_t compact_display_service_startup_full_frame(void) {
    return compact_display_adapter_startup_full_frame();
}
uint16_t *compact_display_service_allocate_framebuffer(size_t bytes) {
    return compact_display_adapter_alloc_framebuffer(bytes);
}
uint16_t *compact_display_service_allocate_transfer_buffer(size_t bytes) {
    return compact_display_adapter_alloc_transfer_buffer(bytes);
}
void compact_display_service_free_buffer(void *buffer) {
    compact_display_adapter_free_buffer(buffer);
}
uint16_t *compact_display_service_allocate_temporary_composition_bitmap(size_t bytes) {
    return compact_display_adapter_allocate_temporary_composition_bitmap(bytes);
}
uint16_t *compact_display_service_allocate_temporary_transfer_bitmap(size_t bytes) {
    return compact_display_adapter_allocate_temporary_transfer_bitmap(bytes);
}
void compact_display_service_free_temporary_bitmap(void *bitmap) {
    compact_display_adapter_free_temporary_bitmap(bitmap);
}
uint8_t *compact_display_service_allocate_remote_pet_frame(size_t bytes) {
    return compact_display_adapter_allocate_remote_pet_frame(bytes);
}
void compact_display_service_free_remote_pet_frame(void *frame) {
    compact_display_adapter_free_remote_pet_frame(frame);
}
void compact_display_service_release_consumed_pet_source(void *frame) {
    compact_display_adapter_release_consumed_pet_source(frame);
}
uint32_t compact_display_service_thinking_worker_wait_ms(uint32_t common_interval_ms) {
    return compact_display_adapter_thinking_worker_wait_ms(common_interval_ms);
}
uint32_t compact_display_service_pet_worker_wait_ms(size_t remote_pet_frame_count,
                                                     uint32_t animated_frame_ms) {
    return compact_display_adapter_pet_worker_wait_ms(remote_pet_frame_count,
                                                       animated_frame_ms);
}
esp_err_t compact_display_service_start_animation(
    compact_display_service_animation_kind_t kind,
    compact_display_service_animation_fn_t entry, void *context) {
    if (!compact_display_animation_kind_valid(kind) || !entry) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(compact_display_animation_lock_take(1000), "compact_display",
                        "animation lifecycle lock timeout");
    compact_display_animation_slot_t *slot = &s_animations[kind];
    if (slot->task) {
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    slot->started = xSemaphoreCreateBinary();
    slot->stopped = xSemaphoreCreateBinary();
    if (!slot->started || !slot->stopped) {
        if (slot->started) vSemaphoreDelete(slot->started);
        if (slot->stopped) vSemaphoreDelete(slot->stopped);
        *slot = (compact_display_animation_slot_t){0};
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_ERR_NO_MEM;
    }
    slot->entry = entry;
    slot->context = context;
    slot->stop_requested = false;
    slot->completed = false;
    const BaseType_t created = compact_display_service_start_animation_task(
        kind, compact_display_service_animation_task, (void *)(uintptr_t)kind, &slot->task);
    if (created != pdPASS) {
        vSemaphoreDelete(slot->started);
        vSemaphoreDelete(slot->stopped);
        *slot = (compact_display_animation_slot_t){0};
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_ERR_NO_MEM;
    }
    /* Release only after xTaskCreate has returned the stable task identity.
     * A concurrent stop may then notify that identity even if the worker has
     * not entered its first wait; FreeRTOS retains the notification. */
    (void)xSemaphoreGive(slot->started);
    (void)xSemaphoreGive(s_animation_lock);
    return ESP_OK;
}

bool compact_display_service_animation_wait_ms(
    compact_display_service_animation_kind_t kind, uint32_t timeout_ms) {
    if (!compact_display_animation_kind_valid(kind) || timeout_ms == 0) return false;
    compact_display_animation_slot_t *slot = &s_animations[kind];
    if (!slot->task) return false;
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(timeout_ms));
    return !slot->stop_requested;
}

bool compact_display_service_animation_running(
    compact_display_service_animation_kind_t kind) {
    return compact_display_animation_kind_valid(kind) && s_animations[kind].task != NULL;
}

esp_err_t compact_display_service_stop_animation(
    compact_display_service_animation_kind_t kind, uint32_t timeout_ms) {
    if (!compact_display_animation_kind_valid(kind) || timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    ESP_RETURN_ON_ERROR(compact_display_animation_lock_take(timeout_ms), "compact_display",
                        "animation lifecycle lock timeout");
    compact_display_animation_slot_t *slot = &s_animations[kind];
    if (!slot->task) {
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_OK;
    }
    if (slot->completed) {
        vSemaphoreDelete(slot->started);
        vSemaphoreDelete(slot->stopped);
        *slot = (compact_display_animation_slot_t){0};
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == slot->owner) {
        (void)xSemaphoreGive(s_animation_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (!slot->stop_requested) {
        slot->stop_requested = true;
        xTaskNotifyGive(slot->task);
    }
    (void)xSemaphoreGive(s_animation_lock);

    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    const TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (!slot->stopped || remaining == 0 ||
        xSemaphoreTake(slot->stopped, remaining) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* The completion wait has already consumed part of this caller-owned
     * transaction.  Cleanup may only use the remaining parent budget: using
     * the original timeout here would make a single stop exceed its declared
     * deadline under lifecycle-lock contention. */
    const TickType_t after_join_elapsed = xTaskGetTickCount() - started;
    const TickType_t after_join_remaining = after_join_elapsed >= deadline
                                                 ? 0 : deadline - after_join_elapsed;
    if (after_join_remaining == 0 ||
        compact_display_animation_lock_take(
            (uint32_t)(after_join_remaining * portTICK_PERIOD_MS)) != ESP_OK) {
        /* The worker has completed, but another lifecycle transaction owns
         * the mutex. Keep this closed generation and its semaphores intact;
         * a later stop performs cleanup rather than deleting resources that a
         * concurrent owner may still inspect. */
        return ESP_ERR_TIMEOUT;
    }
    if (slot->started) vSemaphoreDelete(slot->started);
    if (slot->stopped) vSemaphoreDelete(slot->stopped);
    *slot = (compact_display_animation_slot_t){0};
    (void)xSemaphoreGive(s_animation_lock);
    return ESP_OK;
}

static void compact_display_service_animation_deadline_test_entry(void *context) {
    (void)context;
    while (compact_display_service_animation_wait_ms(
        COMPACT_DISPLAY_ANIMATION_THINKING, 1000)) {}
}

esp_err_t compact_display_service_run_animation_deadline_test(void) {
    if (!provisioning_failure_injection_compact_display_animation_deadline_test_enabled()) {
        return ESP_OK;
    }
    /* The worker consumes 70 ms before completion and holds the same mutex for
     * another 70 ms after completion. A 100 ms stopper must time out around
     * its original deadline. The old cleanup path incorrectly waited a fresh
     * 100 ms and returned success around 140 ms. */
    const uint32_t stop_timeout_ms = 100u;
    const TickType_t started = xTaskGetTickCount();
    ESP_LOGW("compact_display", "test: starting animation stop-deadline proof");
    ESP_RETURN_ON_ERROR(compact_display_service_start_animation(
                            COMPACT_DISPLAY_ANIMATION_THINKING,
                            compact_display_service_animation_deadline_test_entry, NULL),
                        "compact_display", "test animation start failed");
    vTaskDelay(1);
    const esp_err_t first_stop = compact_display_service_stop_animation(
        COMPACT_DISPLAY_ANIMATION_THINKING, stop_timeout_ms);
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t budget = pdMS_TO_TICKS(stop_timeout_ms);
    const TickType_t tolerance = pdMS_TO_TICKS(30u) == 0 ? 1 : pdMS_TO_TICKS(30u);
    if (first_stop != ESP_ERR_TIMEOUT || elapsed > budget + tolerance) {
        ESP_LOGE("compact_display",
                 "test: animation stop deadline failed: status=%s elapsed=%lu budget=%lu",
                 esp_err_to_name(first_stop), (unsigned long)elapsed,
                 (unsigned long)budget);
        return first_stop == ESP_OK ? ESP_FAIL : first_stop;
    }
    /* Let the owner publish the fully-dead generation, then require the
     * normal idempotent later-stop cleanup to reclaim its retained resources. */
    compact_display_service_test_delay(160u);
    const esp_err_t cleanup = compact_display_service_stop_animation(
        COMPACT_DISPLAY_ANIMATION_THINKING, 1000u);
    if (cleanup != ESP_OK) {
        ESP_LOGE("compact_display", "test: animation late cleanup failed: %s",
                 esp_err_to_name(cleanup));
        return cleanup;
    }
    ESP_LOGI("compact_display",
             "test: PASS animation stop deadline elapsed=%lu ticks budget=%lu",
             (unsigned long)elapsed, (unsigned long)budget);
    return ESP_OK;
}
bool compact_display_service_pet_animation_tracks_elapsed_time(void) {
    return compact_display_adapter_pet_animation_tracks_elapsed_time();
}

void compact_display_service_note_pet_animation_tick(uint32_t target_interval_ms,
                                                     bool presented,
                                                     uint32_t presentation_us) {
    compact_display_adapter_note_pet_animation_tick(target_interval_ms, presented,
                                                    presentation_us);
}
