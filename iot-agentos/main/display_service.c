#include "display_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "esp_log.h"

#include "platform_display.h"
#include "provisioning_failure_injection.h"
#include "task_registry.h"

/* The current board renderers are synchronous, but all calls into Platform
 * Display now originate from this one task. Requests retain synchronous
 * result semantics so response pagination, brightness failures and pet
 * admission keep their established caller contracts while the full immutable
 * UI-snapshot/coalescing migration is still pending. */
#define DISPLAY_SERVICE_QUEUE_DEPTH 2u
#define DISPLAY_SERVICE_TASK_STACK_WORDS 4096u
#define DISPLAY_SERVICE_TASK_PRIORITY (tskIDLE_PRIORITY + 5u)

typedef enum {
    DISPLAY_REQUEST_GET_PET_BUDGET = 0,
    DISPLAY_REQUEST_SET_COMMAND_LOCK,
    DISPLAY_REQUEST_SET_BRIGHTNESS,
    DISPLAY_REQUEST_ENTER_DISPLAY_OFF,
    DISPLAY_REQUEST_WAKE_DISPLAY,
    DISPLAY_REQUEST_DISPLAY_IS_OFF,
    DISPLAY_REQUEST_SHOW_STARTUP,
    DISPLAY_REQUEST_SET_PET_STATE,
    DISPLAY_REQUEST_SET_COMMAND_STAGE,
    DISPLAY_REQUEST_SET_COMMAND_CANCEL_ENABLED,
    DISPLAY_REQUEST_SET_PET_PROFILE,
    DISPLAY_REQUEST_SET_PET_ASSET,
    DISPLAY_REQUEST_SET_PET_ASSET_CONSUMING,
    DISPLAY_REQUEST_SET_RECORDING_MODE,
    DISPLAY_REQUEST_SET_RECORDING_VISUAL,
    DISPLAY_REQUEST_SET_AUDIO_LEVEL,
    DISPLAY_REQUEST_SHOW_TEXT,
    DISPLAY_REQUEST_SHOW_UPLOAD_PROGRESS,
    DISPLAY_REQUEST_SHOW_RESPONSE,
    DISPLAY_REQUEST_SHOW_RESPONSE_IMAGE,
    DISPLAY_REQUEST_NAVIGATE_RESPONSE,
    DISPLAY_REQUEST_GET_RESPONSE_PAGE,
    DISPLAY_REQUEST_RESTORE_RESPONSE_PAGE,
    DISPLAY_REQUEST_CACHE_GLYPH,
    DISPLAY_REQUEST_SHOW_QRCODE_MODULES,
    DISPLAY_REQUEST_SHOW_READY_PROMPT,
    DISPLAY_REQUEST_CANCEL_READY_PROMPT,
    DISPLAY_REQUEST_SET_WIFI_STATUS,
    DISPLAY_REQUEST_SET_SERVICE_READY,
    DISPLAY_REQUEST_SET_AMBIENT,
    DISPLAY_REQUEST_SET_ALARM_SCHEDULED,
    DISPLAY_REQUEST_SET_ALARM_VISUAL,
    DISPLAY_REQUEST_STOP,
} display_request_kind_t;

typedef struct {
    display_request_kind_t kind;
    uint32_t revision;
    bool bool_a;
    bool bool_b;
    bool bool_c;
    uint8_t u8_a;
    uint16_t u16_a;
    int int_a;
    uint32_t u32_a;
    uint32_t u32_b;
    uint32_t u32_c;
    uint32_t u32_d;
    const char *text_a;
    const char *text_b;
    const char *text_c;
    const char *text_d;
    const char *text_e;
    const uint8_t *bytes;
    const uint8_t *const *frames;
    uint8_t **consuming_frames;
    const uint16_t *pixels;
    void *out;
    device_status_t status_result;
    bool bool_result;
    int int_result;
    SemaphoreHandle_t completion;
} display_service_request_t;

static portMUX_TYPE s_display_service_state_lock = portMUX_INITIALIZER_UNLOCKED;
static StaticSemaphore_t s_display_service_submission_mutex_storage;
static SemaphoreHandle_t s_display_service_submission_mutex;
static StaticQueue_t s_display_service_queue_storage;
static uint8_t s_display_service_queue_buffer[
    DISPLAY_SERVICE_QUEUE_DEPTH * sizeof(display_service_request_t *)];
static QueueHandle_t s_display_service_queue;
static StaticTask_t s_display_service_task_storage;
static StackType_t s_display_service_task_stack[DISPLAY_SERVICE_TASK_STACK_WORDS];
static TaskHandle_t s_display_service_task;
/* Only test builds opt in to this static worker. It intentionally issues a
 * second bounded STOP while the primary lifecycle caller is already waiting,
 * exercising the shared STOP record without ever publishing a test hook to
 * production callers. */
static StaticTask_t s_display_service_test_secondary_stopper_task_storage;
static StackType_t s_display_service_test_secondary_stopper_task_stack[3072u];
static TaskHandle_t s_display_service_test_secondary_stopper_task;
/* STOP can outlive a lifecycle caller that times out waiting for a renderer
 * already executing a synchronous panel transaction. Its request/completion
 * storage is therefore boot-lifetime, never a deinit caller's stack. */
static StaticSemaphore_t s_display_service_stop_completion_storage;
static SemaphoreHandle_t s_display_service_stop_completion;
static display_service_request_t s_display_service_stop_request;
static uint32_t s_submitted_revision;
static uint32_t s_completed_revision;
static uint32_t s_task_generation;
static bool s_stopping;
static bool s_initializing;
static bool s_initialization_failed;
static bool s_registry_registered;
static bool s_stop_enqueued;

static void display_service_dispatch(display_service_request_t *request);
static void display_service_submission_lock(void);
static void display_service_submission_unlock(void);
static bool display_service_take_submission_lock(TickType_t timeout);
static bool display_service_wait_for_stop(TickType_t started, TickType_t budget);
static esp_err_t display_service_registry_stop(void *context, uint32_t timeout_ms);
static void display_service_test_secondary_stopper_task(void *unused);
static bool display_service_start_test_secondary_stopper(void);

static TickType_t display_service_stop_remaining_ticks(TickType_t started,
                                                        TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

/* STOP is a one-shot binary completion because only one task can consume the
 * signal.  More than one lifecycle caller may, however, observe the same
 * closed generation. The task handle is cleared before that signal is given,
 * so poll the authoritative terminal state in small bounded slices instead
 * of making a second stopper sleep until its entire parent deadline expires
 * after another stopper has consumed the one token. */
static bool display_service_wait_for_stop(TickType_t started, TickType_t budget) {
    const TickType_t poll_ticks = pdMS_TO_TICKS(10) == 0 ? 1 : pdMS_TO_TICKS(10);
    for (;;) {
        taskENTER_CRITICAL(&s_display_service_state_lock);
        const bool stopped = s_display_service_task == NULL;
        if (stopped) s_registry_registered = false;
        SemaphoreHandle_t completion = s_display_service_stop_completion;
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        if (stopped) return true;

        const TickType_t remaining =
            display_service_stop_remaining_ticks(started, budget);
        if (!completion || remaining == 0) return false;
        const TickType_t slice = remaining < poll_ticks ? remaining : poll_ticks;
        (void)xSemaphoreTake(completion, slice);
    }
}

static void display_service_test_secondary_stopper_task(void *unused) {
    (void)unused;
    const uint32_t delay_ms =
        provisioning_failure_injection_display_service_secondary_stop_delay_ms();
    const uint32_t timeout_ms =
        provisioning_failure_injection_display_service_secondary_stop_timeout_ms();
    TickType_t delay_ticks = pdMS_TO_TICKS(delay_ms);
    if (delay_ticks == 0) delay_ticks = 1;
    vTaskDelay(delay_ticks);
    /* The worker is armed at service publication, while the composition root
     * reaches its injected failure immediately afterwards. Poll only the
     * private terminal state for a short bounded window instead of guessing
     * which boot/board setup step will consume the first milliseconds. */
    const TickType_t observe_started = xTaskGetTickCount();
    const TickType_t observe_budget = pdMS_TO_TICKS(3000) == 0 ? 1 :
                                        pdMS_TO_TICKS(3000);
    bool primary_stopper_active = false;
    bool task_alive = false;
    do {
        taskENTER_CRITICAL(&s_display_service_state_lock);
        primary_stopper_active = s_stopping;
        task_alive = s_display_service_task != NULL;
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        if (primary_stopper_active || !task_alive) break;
        vTaskDelay(1);
    } while (xTaskGetTickCount() - observe_started < observe_budget);
    if (!primary_stopper_active || !task_alive || timeout_ms == 0) {
        ESP_LOGW("display_service",
                 "test: secondary stopper skipped (closing=%d task_alive=%d timeout=%lu)",
                 primary_stopper_active, task_alive, (unsigned long)timeout_ms);
    } else {
        ESP_LOGW("display_service", "test: secondary stopper joining terminal STOP");
        const device_status_t status = display_service_deinit(timeout_ms);
        ESP_LOGW("display_service", "test: secondary stopper result=%d", (int)status);
    }
    taskENTER_CRITICAL(&s_display_service_state_lock);
    s_display_service_test_secondary_stopper_task = NULL;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    vTaskDelete(NULL);
}

static bool display_service_start_test_secondary_stopper(void) {
    if (provisioning_failure_injection_display_service_secondary_stop_delay_ms() == 0) {
        return true;
    }
    TaskHandle_t task = xTaskCreateStatic(
        display_service_test_secondary_stopper_task, "display_stop_test", 3072u,
        NULL, DISPLAY_SERVICE_TASK_PRIORITY, s_display_service_test_secondary_stopper_task_stack,
        &s_display_service_test_secondary_stopper_task_storage);
    taskENTER_CRITICAL(&s_display_service_state_lock);
    s_display_service_test_secondary_stopper_task = task;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    if (!task) {
        ESP_LOGE("display_service", "test: cannot create secondary stopper");
        return false;
    }
    ESP_LOGI("display_service", "test: secondary stopper armed");
    return true;
}

static void display_service_task(void *unused) {
    (void)unused;
    for (;;) {
        display_service_request_t *request = NULL;
        if (xQueueReceive(s_display_service_queue, &request, portMAX_DELAY) != pdTRUE ||
            !request) {
            continue;
        }
        if (request->kind == DISPLAY_REQUEST_STOP) {
            const uint32_t stop_delay_ms =
                provisioning_failure_injection_display_service_stop_delay_ms();
            if (stop_delay_ms != 0) {
                /* The task owns the static STOP record and completion while
                 * deliberately delayed. A bounded stopper may time out, but
                 * it must neither enqueue a second sentinel nor tear down
                 * this boot-lifetime storage underneath the late exit. */
                ESP_LOGW("display_service",
                         "test: delaying terminal STOP for %lu ms",
                         (unsigned long)stop_delay_ms);
                TickType_t delay_ticks = pdMS_TO_TICKS(stop_delay_ms);
                if (delay_ticks == 0) delay_ticks = 1;
                vTaskDelay(delay_ticks);
                ESP_LOGW("display_service", "test: delayed terminal STOP released");
            }
            taskENTER_CRITICAL(&s_display_service_state_lock);
            s_display_service_task = NULL;
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            if (request->completion) (void)xSemaphoreGive(request->completion);
            ESP_LOGI("display_service", "display task stopped");
            vTaskDelete(NULL);
            return;
        }
        display_service_dispatch(request);
        if (request->revision != 0) {
            taskENTER_CRITICAL(&s_display_service_state_lock);
            s_completed_revision = request->revision;
            taskEXIT_CRITICAL(&s_display_service_state_lock);
        }
        if (request->completion) (void)xSemaphoreGive(request->completion);
    }
}

bool display_service_init(void) {
    for (;;) {
        taskENTER_CRITICAL(&s_display_service_state_lock);
        if (s_stopping) {
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            return false;
        }
        if (s_initialization_failed) {
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            return false;
        }
        if (s_display_service_submission_mutex && s_display_service_queue &&
            s_display_service_task) {
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            return true;
        }
        if (!s_initializing) {
            s_initializing = true;
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            break;
        }
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        /* Calls originate from ordinary tasks, never ISR.  Do not create a
         * second static queue/task while another caller publishes this boot
         * generation. */
        vTaskDelay(1);
    }

    SemaphoreHandle_t mutex = xSemaphoreCreateRecursiveMutexStatic(
        &s_display_service_submission_mutex_storage);
    SemaphoreHandle_t stop_completion = mutex ? xSemaphoreCreateBinaryStatic(
        &s_display_service_stop_completion_storage) : NULL;
    QueueHandle_t queue = stop_completion ? xQueueCreateStatic(
        DISPLAY_SERVICE_QUEUE_DEPTH, sizeof(display_service_request_t *),
        s_display_service_queue_buffer, &s_display_service_queue_storage) : NULL;
    /* xTaskCreateStatic() may schedule the child before it returns.  Publish
     * every object the task dereferences before creating it; otherwise the
     * first xQueueReceive() can observe a NULL global queue handle.  The
     * initializer gate remains closed, so no submitter can enqueue a request
     * during this publication window. */
    taskENTER_CRITICAL(&s_display_service_state_lock);
    s_display_service_submission_mutex = mutex;
    s_display_service_stop_completion = stop_completion;
    s_display_service_queue = queue;
    taskEXIT_CRITICAL(&s_display_service_state_lock);

    TaskHandle_t task = queue ? xTaskCreateStatic(
        display_service_task, "display_service", DISPLAY_SERVICE_TASK_STACK_WORDS,
        NULL, DISPLAY_SERVICE_TASK_PRIORITY, s_display_service_task_stack,
        &s_display_service_task_storage) : NULL;

    esp_err_t registry_err = task ? task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_BOARD,
        .name = "display_service",
        .context = NULL,
        .stop = display_service_registry_stop,
    }) : ESP_ERR_NO_MEM;
    if (registry_err != ESP_OK) {
        /* Static-task deletion is safe here: no request can have been
         * published before init returns and the queue is still private. */
        if (task) vTaskDelete(task);
        task = NULL;
    }

    taskENTER_CRITICAL(&s_display_service_state_lock);
    s_display_service_task = task;
    if (task) {
        ++s_task_generation;
        if (s_task_generation == 0) ++s_task_generation;
    }
    s_registry_registered = task != NULL;
    if (!task) s_initialization_failed = true;
    s_initializing = false;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    return task != NULL && display_service_start_test_secondary_stopper();
}

static esp_err_t display_service_registry_stop(void *context, uint32_t timeout_ms) {
    (void)context;
    const device_status_t status = display_service_deinit(timeout_ms);
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        default: return ESP_FAIL;
    }
}

device_status_t display_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms) == 0
                                 ? 1 : pdMS_TO_TICKS(timeout_ms);

    /* A lifecycle stop must not construct a Display Task merely because its
     * parent is rolling back. If an initializer is already publishing the
     * boot-lifetime task, wait only within this caller's original deadline. */
    for (;;) {
        taskENTER_CRITICAL(&s_display_service_state_lock);
        const bool initializing = s_initializing;
        const bool absent = !s_display_service_submission_mutex &&
                            !s_display_service_task && !s_stopping;
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        if (absent) return DEVICE_STATUS_OK;
        const TickType_t remaining =
            display_service_stop_remaining_ticks(started, budget);
        if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
        if (!initializing) break;
        vTaskDelay(1);
    }

    TickType_t remaining = display_service_stop_remaining_ticks(started, budget);
    if (!display_service_take_submission_lock(remaining)) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_display_service_state_lock);
    if (s_stopping) {
        const bool stopped = s_display_service_task == NULL;
        if (stopped) s_registry_registered = false;
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        display_service_submission_unlock();
        if (stopped) return DEVICE_STATUS_OK;
        /* The first bounded stopper owns the static STOP record. A timeout
         * must leave it intact; later lifecycle callers only consume that
         * same completion rather than enqueueing a second sentinel. */
        return display_service_wait_for_stop(started, budget)
                   ? DEVICE_STATUS_OK : DEVICE_STATUS_TIMEOUT;
    }
    s_stopping = true;
    TaskHandle_t task = s_display_service_task;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    if (!task) {
        display_service_submission_unlock();
        return DEVICE_STATUS_OK;
    }

    if (!s_stop_enqueued) {
        s_display_service_stop_request = (display_service_request_t){
            .kind = DISPLAY_REQUEST_STOP,
            .completion = s_display_service_stop_completion,
        };
        display_service_request_t *queued = &s_display_service_stop_request;
        if (!s_display_service_stop_completion || !s_display_service_queue ||
            xQueueSend(s_display_service_queue, &queued, 0) != pdTRUE) {
            taskENTER_CRITICAL(&s_display_service_state_lock);
            s_stopping = false;
            taskEXIT_CRITICAL(&s_display_service_state_lock);
            display_service_submission_unlock();
            return DEVICE_STATUS_BUSY;
        }
        taskENTER_CRITICAL(&s_display_service_state_lock);
        s_stop_enqueued = true;
        taskEXIT_CRITICAL(&s_display_service_state_lock);
    }
    display_service_submission_unlock();
    return display_service_wait_for_stop(started, budget)
               ? DEVICE_STATUS_OK : DEVICE_STATUS_TIMEOUT;
}

static void display_service_submission_lock(void) {
    (void)display_service_init();
    if (s_display_service_submission_mutex) {
        (void)xSemaphoreTakeRecursive(s_display_service_submission_mutex, portMAX_DELAY);
    }
}

static bool display_service_take_submission_lock(TickType_t timeout) {
    SemaphoreHandle_t mutex;
    taskENTER_CRITICAL(&s_display_service_state_lock);
    mutex = s_display_service_submission_mutex;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    return mutex && timeout != 0 &&
           xSemaphoreTakeRecursive(mutex, timeout) == pdTRUE;
}

static void display_service_submission_unlock(void) {
    if (s_display_service_submission_mutex) {
        (void)xSemaphoreGiveRecursive(s_display_service_submission_mutex);
    }
}

/* A revision is deliberately allocated before forwarding: some legacy display
 * intents return void and their profile renderers own any later presentation
 * failure.  This is therefore a monotonic service-admission sequence, not a
 * claim that a framebuffer reached the panel. */
static uint32_t note_display_submission(void) {
    taskENTER_CRITICAL(&s_display_service_state_lock);
    ++s_submitted_revision;
    if (s_submitted_revision == 0) ++s_submitted_revision;
    const uint32_t revision = s_submitted_revision;
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    return revision;
}

/* Request objects live on the submitting task's stack, but the caller waits
 * for Display Task completion before returning.  Thus these borrowed fields
 * are valid for the current handoff. This is intentionally not a future
 * asynchronous payload contract: async scenes must own per-entry data. */
static bool display_service_submit(display_service_request_t *request,
                                   bool mutates_scene) {
    if (!request) return false;
    if (!display_service_init()) return false;
    if (xTaskGetCurrentTaskHandle() == s_display_service_task) {
        /* A renderer callback may synchronously re-enter Device Display while
         * this task is presenting a request. It must never enqueue-and-wait
         * on itself, but it also must not bypass a lifecycle admission close
         * which raced the outer request after its submitter released the
         * submission mutex. The task identity keeps panel ownership singular;
         * this state check keeps the terminal STOP generation singular too. */
        taskENTER_CRITICAL(&s_display_service_state_lock);
        const bool accepting = !s_stopping &&
                               s_display_service_task == xTaskGetCurrentTaskHandle();
        taskEXIT_CRITICAL(&s_display_service_state_lock);
        if (!accepting) return false;
        if (mutates_scene) request->revision = note_display_submission();
        display_service_dispatch(request);
        if (request->revision != 0) {
            taskENTER_CRITICAL(&s_display_service_state_lock);
            s_completed_revision = request->revision;
            taskEXIT_CRITICAL(&s_display_service_state_lock);
        }
        return true;
    }

    StaticSemaphore_t completion_storage;
    SemaphoreHandle_t completion = xSemaphoreCreateBinaryStatic(&completion_storage);
    if (!completion) return false;
    request->completion = completion;
    display_service_submission_lock();
    if (!s_display_service_queue || !s_display_service_task || s_stopping) {
        display_service_submission_unlock();
        return false;
    }
    if (mutates_scene) request->revision = note_display_submission();
    display_service_request_t *queued = request;
    /* The submission owner admits one synchronous request at a time. A depth
     * of two leaves an entry free while Display Task renders the previous
     * request, so this zero-wait send cannot turn UI producers into an
     * unbounded queue backlog. */
    if (xQueueSend(s_display_service_queue, &queued, 0) != pdTRUE) {
        display_service_submission_unlock();
        return false;
    }
    display_service_submission_unlock();
    return xSemaphoreTake(completion, portMAX_DELAY) == pdTRUE;
}

/* Only Display Task enters this dispatcher. The explicit reentrant path in
 * display_service_submit is still the already-running Display Task, not a
 * second renderer owner. Keep renderer calls here so a future
 * immutable/coalescing request representation has exactly one replacement
 * point. */
static void display_service_dispatch(display_service_request_t *request) {
    switch (request->kind) {
        case DISPLAY_REQUEST_GET_PET_BUDGET:
            request->bool_result = platform_display_get_pet_asset_install_budget(
                request->u32_a, request->u32_b, request->u32_c, request->out);
            break;
        case DISPLAY_REQUEST_SET_COMMAND_LOCK:
            platform_display_set_command_lock(request->bool_a); break;
        case DISPLAY_REQUEST_SET_BRIGHTNESS:
            request->status_result = platform_display_set_brightness(request->u8_a); break;
        case DISPLAY_REQUEST_ENTER_DISPLAY_OFF:
            request->bool_result = platform_display_enter_display_off(); break;
        case DISPLAY_REQUEST_WAKE_DISPLAY:
            request->bool_result = platform_display_wake_display(); break;
        case DISPLAY_REQUEST_DISPLAY_IS_OFF:
            request->bool_result = platform_display_is_off(); break;
        case DISPLAY_REQUEST_SHOW_STARTUP: platform_display_show_startup(); break;
        case DISPLAY_REQUEST_SET_PET_STATE: platform_display_set_pet_state(request->text_a); break;
        case DISPLAY_REQUEST_SET_COMMAND_STAGE: platform_display_set_command_stage(request->text_a); break;
        case DISPLAY_REQUEST_SET_COMMAND_CANCEL_ENABLED:
            platform_display_set_command_cancel_enabled(request->bool_a); break;
        case DISPLAY_REQUEST_SET_PET_PROFILE:
            platform_display_set_pet_profile(request->text_a, request->bool_a); break;
        case DISPLAY_REQUEST_SET_PET_ASSET:
            request->status_result = platform_display_set_pet_asset(
                request->frames, request->u32_a, request->u32_b, request->u32_c,
                request->u32_d);
            break;
        case DISPLAY_REQUEST_SET_PET_ASSET_CONSUMING:
            request->status_result = platform_display_set_pet_asset_consuming(
                request->consuming_frames, request->u32_a, request->u32_b,
                request->u32_c, request->u32_d);
            break;
        case DISPLAY_REQUEST_SET_RECORDING_MODE:
            platform_display_set_recording_mode(request->bool_a); break;
        case DISPLAY_REQUEST_SET_RECORDING_VISUAL:
            platform_display_set_recording_visual(request->bool_a, request->bool_b,
                                                   request->u32_a); break;
        case DISPLAY_REQUEST_SET_AUDIO_LEVEL:
            platform_display_set_audio_level(request->u16_a, request->u32_a); break;
        case DISPLAY_REQUEST_SHOW_TEXT:
            platform_display_show_text(request->text_a, request->text_b); break;
        case DISPLAY_REQUEST_SHOW_UPLOAD_PROGRESS:
            platform_display_show_upload_progress(request->u32_a, request->u32_b,
                                                  request->text_a); break;
        case DISPLAY_REQUEST_SHOW_RESPONSE:
            platform_display_show_response(request->text_a, request->text_b); break;
        case DISPLAY_REQUEST_SHOW_RESPONSE_IMAGE:
            platform_display_show_response_image(request->text_a, request->text_b,
                                                 request->pixels, request->u32_a,
                                                 request->u32_b); break;
        case DISPLAY_REQUEST_NAVIGATE_RESPONSE:
            request->bool_result = platform_display_navigate_response(request->int_a); break;
        case DISPLAY_REQUEST_GET_RESPONSE_PAGE:
            request->bool_result = platform_display_get_response_page(request->out); break;
        case DISPLAY_REQUEST_RESTORE_RESPONSE_PAGE:
            request->bool_result = platform_display_restore_response_page(request->u32_a); break;
        case DISPLAY_REQUEST_CACHE_GLYPH:
            request->int_result = platform_display_cache_glyph(request->u32_a, request->bytes); break;
        case DISPLAY_REQUEST_SHOW_QRCODE_MODULES:
            platform_display_show_qrcode_modules(request->bytes, request->u32_a,
                                                 request->text_a); break;
        case DISPLAY_REQUEST_SHOW_READY_PROMPT:
            platform_display_show_ready_prompt(request->text_a, request->text_b); break;
        case DISPLAY_REQUEST_CANCEL_READY_PROMPT: platform_display_cancel_ready_prompt(); break;
        case DISPLAY_REQUEST_SET_WIFI_STATUS:
            platform_display_set_wifi_status(request->text_a, request->bool_a); break;
        case DISPLAY_REQUEST_SET_SERVICE_READY:
            platform_display_set_service_ready(request->bool_a); break;
        case DISPLAY_REQUEST_SET_AMBIENT:
            platform_display_set_ambient(request->text_a, request->text_b, request->text_c,
                                         request->text_d, request->text_e, request->int_a,
                                         request->bool_a, request->bool_b);
            break;
        case DISPLAY_REQUEST_SET_ALARM_SCHEDULED:
            platform_display_set_alarm_scheduled(request->bool_a); break;
        case DISPLAY_REQUEST_SET_ALARM_VISUAL:
            platform_display_set_alarm_visual(request->bool_a, request->u32_a,
                                              request->text_a, request->text_b,
                                              request->u32_b, request->u32_c);
            break;
        case DISPLAY_REQUEST_STOP:
            /* Consumed directly by display_service_task before dispatch. */
            break;
    }
}

#define DISPLAY_SERVICE_SUBMIT_REQUEST(request_kind, ...) \
    do { \
        display_service_request_t request = { .kind = (request_kind), __VA_ARGS__ }; \
        (void)display_service_submit(&request, true); \
    } while (0)

bool display_service_get_snapshot(display_service_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_display_service_state_lock);
    *out_snapshot = (display_service_snapshot_t){
        .submitted_revision = s_submitted_revision,
        .completed_revision = s_completed_revision,
        .task_generation = s_task_generation,
        .task_ready = s_display_service_task != NULL,
        .task_registered = s_registry_registered,
    };
    taskEXIT_CRITICAL(&s_display_service_state_lock);
    return true;
}

bool display_service_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_GET_PET_BUDGET,
        .u32_a = source_width, .u32_b = source_height, .u32_c = frame_count,
        .out = out_budget,
    };
    return display_service_submit(&request, false) && request.bool_result;
}

void display_service_set_command_lock(bool locked) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_COMMAND_LOCK, .bool_a = locked);
}

device_status_t display_service_set_brightness(uint8_t percent) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_SET_BRIGHTNESS, .u8_a = percent,
        .status_result = DEVICE_STATUS_INTERNAL_ERROR,
    };
    return display_service_submit(&request, true) ? request.status_result
                                                   : DEVICE_STATUS_BUSY;
}

bool display_service_enter_display_off(void) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_ENTER_DISPLAY_OFF,
    };
    return display_service_submit(&request, true) && request.bool_result;
}

bool display_service_wake_display(void) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_WAKE_DISPLAY,
    };
    return display_service_submit(&request, true) && request.bool_result;
}

bool display_service_display_is_off(void) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_DISPLAY_IS_OFF,
    };
    return display_service_submit(&request, false) && request.bool_result;
}

void display_service_show_startup(void) {
    display_service_request_t request = { .kind = DISPLAY_REQUEST_SHOW_STARTUP };
    (void)display_service_submit(&request, true);
}
void display_service_set_pet_state(const char *state) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_PET_STATE, .text_a = state);
}
void display_service_set_command_stage(const char *stage) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_COMMAND_STAGE, .text_a = stage);
}
void display_service_set_command_cancel_enabled(bool enabled) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_COMMAND_CANCEL_ENABLED,
                                   .bool_a = enabled);
}
void display_service_set_pet_profile(const char *skin, bool motion_enabled) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_PET_PROFILE,
                                   .text_a = skin, .bool_a = motion_enabled);
}
device_status_t display_service_set_pet_asset(const uint8_t *const *frames,
                                              uint32_t frame_count,
                                              uint32_t width, uint32_t height,
                                              uint32_t frame_ms) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_SET_PET_ASSET, .frames = frames,
        .u32_a = frame_count, .u32_b = width, .u32_c = height, .u32_d = frame_ms,
        .status_result = DEVICE_STATUS_INTERNAL_ERROR,
    };
    return display_service_submit(&request, true) ? request.status_result
                                                   : DEVICE_STATUS_BUSY;
}
device_status_t display_service_set_pet_asset_consuming(uint8_t **frames,
                                                        uint32_t frame_count,
                                                        uint32_t width,
                                                        uint32_t height,
                                                        uint32_t frame_ms) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_SET_PET_ASSET_CONSUMING, .consuming_frames = frames,
        .u32_a = frame_count, .u32_b = width, .u32_c = height, .u32_d = frame_ms,
        .status_result = DEVICE_STATUS_INTERNAL_ERROR,
    };
    return display_service_submit(&request, true) ? request.status_result
                                                   : DEVICE_STATUS_BUSY;
}
void display_service_set_recording_mode(bool meeting) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_RECORDING_MODE, .bool_a = meeting);
}
void display_service_set_recording_visual(bool active, bool paused,
                                          uint32_t elapsed_seconds) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_RECORDING_VISUAL,
                                   .bool_a = active, .bool_b = paused,
                                   .u32_a = elapsed_seconds);
}
void display_service_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_AUDIO_LEVEL,
                                   .u16_a = level, .u32_a = elapsed_seconds);
}
void display_service_show_text(const char *title, const char *text) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_TEXT, .text_a = title, .text_b = text);
}
void display_service_show_upload_progress(uint32_t completed_bytes,
                                          uint32_t total_bytes,
                                          const char *stage) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_UPLOAD_PROGRESS,
                                   .u32_a = completed_bytes, .u32_b = total_bytes,
                                   .text_a = stage);
}
void display_service_show_response(const char *title, const char *text) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_RESPONSE,
                                   .text_a = title, .text_b = text);
}
void display_service_show_response_image(const char *title, const char *caption,
                                         const uint16_t *pixels,
                                         uint32_t width, uint32_t height) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_RESPONSE_IMAGE,
                                   .text_a = title, .text_b = caption, .pixels = pixels,
                                   .u32_a = width, .u32_b = height);
}
bool display_service_navigate_response(int page_delta) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_NAVIGATE_RESPONSE, .int_a = page_delta,
    };
    return display_service_submit(&request, true) && request.bool_result;
}
bool display_service_get_response_page(uint32_t *out_page) {
    /* Renderer pagination state stays on Display Task. An observation does
     * not create a new scene revision. */
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_GET_RESPONSE_PAGE, .out = out_page,
    };
    return display_service_submit(&request, false) && request.bool_result;
}
bool display_service_restore_response_page(uint32_t page) {
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_RESTORE_RESPONSE_PAGE, .u32_a = page,
    };
    return display_service_submit(&request, true) && request.bool_result;
}
int display_service_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    /* A Hub decoder commonly supplies this bitmap from a stack buffer.  Make
     * the synchronous Service -> Platform hand-off explicitly independent of
     * that producer storage before entering the selected renderer.  This is a
     * submission-local copy only: the current facade has no Display Task or
     * deferred glyph queue, so Platform must still consume/copy it before
     * returning.  A future asynchronous implementation must replace this
     * with per-record owned storage plus a completion/release policy; it must
     * not retain `bitmap` or this local array. */
    if (!bitmap) return 0;
    uint8_t submission_bitmap[72];
    memcpy(submission_bitmap, bitmap, sizeof(submission_bitmap));
    display_service_request_t request = {
        .kind = DISPLAY_REQUEST_CACHE_GLYPH, .u32_a = codepoint,
        .bytes = submission_bitmap,
    };
    return display_service_submit(&request, true) ? request.int_result : 0;
}
void display_service_show_qrcode_modules(const uint8_t *modules,
                                         uint32_t module_count,
                                         const char *ssid) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_QRCODE_MODULES,
                                   .bytes = modules, .u32_a = module_count,
                                   .text_a = ssid);
}
void display_service_show_ready_prompt(const char *title, const char *text) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SHOW_READY_PROMPT,
                                   .text_a = title, .text_b = text);
}
void display_service_cancel_ready_prompt(void) {
    display_service_request_t request = { .kind = DISPLAY_REQUEST_CANCEL_READY_PROMPT };
    (void)display_service_submit(&request, true);
}
void display_service_set_wifi_status(const char *ssid, bool connected) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_WIFI_STATUS,
                                   .text_a = ssid, .bool_a = connected);
}
void display_service_set_service_ready(bool ready) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_SERVICE_READY, .bool_a = ready);
}
void display_service_set_ambient(const char *time, const char *location,
                                 const char *date, const char *weekday,
                                 const char *weather_summary,
                                 int temperature_c, bool weather_valid,
                                 bool weather_stale) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_AMBIENT,
                                   .text_a = time, .text_b = location, .text_c = date,
                                   .text_d = weekday, .text_e = weather_summary,
                                   .int_a = temperature_c, .bool_a = weather_valid,
                                   .bool_b = weather_stale);
}
void display_service_set_alarm_scheduled(bool scheduled) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_ALARM_SCHEDULED,
                                   .bool_a = scheduled);
}
void display_service_set_alarm_visual(bool active, uint32_t frame,
                                      const char *time_text, const char *label,
                                      uint32_t attempt, uint32_t max_attempts) {
    DISPLAY_SERVICE_SUBMIT_REQUEST(DISPLAY_REQUEST_SET_ALARM_VISUAL,
                                   .bool_a = active, .u32_a = frame,
                                   .text_a = time_text, .text_b = label,
                                   .u32_b = attempt, .u32_c = max_attempts);
}

#undef DISPLAY_SERVICE_SUBMIT_REQUEST
