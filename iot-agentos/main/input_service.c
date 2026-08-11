#include "input_service.h"

#include <string.h>

#include "esp_err.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "platform_input.h"

/*
 * The board port is still the owner of scan/debounce/gesture recognition.
 * This service owns the cross-task handoff.  In particular, a slow Hub
 * capability refresh or NVS write in application policy must never run in a
 * touch or GPIO polling task.
 *
 * There are two bounded lanes instead of a single FIFO: contact/gesture
 * controls can dismiss an alarm or cancel capture even while a burst of
 * volume-key navigation is being processed.  No board callback is an ISR at
 * present; if a future adapter publishes from an ISR it must use a dedicated
 * ISR-safe adapter rather than calling this function directly.
 */

#define INPUT_CONTROL_QUEUE_DEPTH 16
#define INPUT_AUXILIARY_QUEUE_DEPTH 8
#define INPUT_SERVICE_TASK_STACK 4096
#define INPUT_SERVICE_TASK_PRIORITY 8

typedef enum {
    INPUT_SERVICE_EVENT_INPUT = 0,
    INPUT_SERVICE_EVENT_STOP,
} input_service_event_kind_t;

typedef struct {
    device_input_event_t input;
    input_service_event_kind_t kind;
} input_service_event_t;

typedef struct {
    QueueHandle_t control_queue;
    QueueHandle_t auxiliary_queue;
    QueueSetHandle_t queue_set;
    TaskHandle_t task;
    SemaphoreHandle_t stopped;
    device_input_cb_t consumer;
    void *consumer_context;
    uint32_t generation;
    uint32_t next_sequence;
    uint32_t dropped_control;
    uint32_t dropped_auxiliary;
    bool started;
    bool accepting;
    bool stopping;
    /* Exactly one caller owns an active stop attempt.  A timeout releases
     * this ownership but keeps admission closed, so a later lifecycle pass
     * can finish the same generation without reopening its board queues. */
    bool stop_in_progress;
    bool stop_enqueued;
    uint32_t publishers_in_flight;
} input_service_state_t;

static const char *TAG = "maclaw_input";
static input_service_state_t s_input_service;
/* The scanner can be stopped before Input Service frees its queues, but the
 * selected physical adapter remains boot-lifetime today.  Do not re-open the
 * adapter without its future full deinit/restart contract. */
static bool s_board_scanner_initialized;
static portMUX_TYPE s_input_service_lock = portMUX_INITIALIZER_UNLOCKED;
static uint32_t s_next_input_generation;

static uint32_t input_service_allocate_generation(void) {
    taskENTER_CRITICAL(&s_input_service_lock);
    uint32_t generation = ++s_next_input_generation;
    if (generation == 0) generation = ++s_next_input_generation;
    taskEXIT_CRITICAL(&s_input_service_lock);
    return generation;
}

static void input_service_release_stop_owner(void) {
    taskENTER_CRITICAL(&s_input_service_lock);
    s_input_service.stop_in_progress = false;
    taskEXIT_CRITICAL(&s_input_service_lock);
}

static bool input_service_is_control_action(device_input_action_t action) {
    return action != DEVICE_INPUT_VOLUME_UP && action != DEVICE_INPUT_VOLUME_DOWN;
}

static void input_service_publish_from_board(device_input_action_t action,
                                             device_input_source_t source,
                                             void *context) {
    (void)context;
    input_service_state_t *service = &s_input_service;
    QueueHandle_t queue;
    bool control = input_service_is_control_action(action);

    /* Stop first closes this admission gate, then waits for every publisher
     * which passed it.  No scanner task can then enqueue into a deleted queue.
     * Board callbacks currently run from tasks rather than ISRs; a future ISR
     * adapter needs a separate ISR-safe publishing path. */
    taskENTER_CRITICAL(&s_input_service_lock);
    if (!service->accepting) {
        taskEXIT_CRITICAL(&s_input_service_lock);
        return;
    }
    ++service->publishers_in_flight;
    queue = control ? service->control_queue : service->auxiliary_queue;
    taskEXIT_CRITICAL(&s_input_service_lock);

    taskENTER_CRITICAL(&s_input_service_lock);
    uint32_t generation = service->generation;
    uint32_t sequence = ++service->next_sequence;
    if (sequence == 0) sequence = ++service->next_sequence;
    bool still_accepting = service->accepting && generation != 0;
    taskEXIT_CRITICAL(&s_input_service_lock);
    /* Stop may close admission between the publisher's first check and this
     * metadata allocation. Do not create a late event for a closing
     * generation merely because its queue object has not been reclaimed yet. */
    if (!still_accepting) {
        taskENTER_CRITICAL(&s_input_service_lock);
        --service->publishers_in_flight;
        taskEXIT_CRITICAL(&s_input_service_lock);
        return;
    }
    input_service_event_t event = {
        .input = {
            .struct_size = sizeof(device_input_event_t),
            .abi_version = DEVICE_INPUT_EVENT_ABI_VERSION,
            .generation = generation,
            .sequence = sequence,
            .timestamp_us = (uint64_t)esp_timer_get_time(),
            .action = action,
            .source = source,
        },
        .kind = INPUT_SERVICE_EVENT_INPUT,
    };
    if (xQueueSend(queue, &event, 0) != pdPASS) {
        uint32_t *dropped = control ? &service->dropped_control : &service->dropped_auxiliary;
        ++*dropped;
        ESP_LOGW(TAG, "input queue full: lane=%s action=%d source=%d dropped=%lu",
                 control ? "control" : "auxiliary", (int)action, (int)source,
                 (unsigned long)*dropped);
    }
    taskENTER_CRITICAL(&s_input_service_lock);
    --service->publishers_in_flight;
    taskEXIT_CRITICAL(&s_input_service_lock);
}

static bool input_service_take_next(input_service_event_t *out_event) {
    if (xQueueReceive(s_input_service.control_queue, out_event, 0) == pdPASS) return true;

    (void)xQueueSelectFromSet(s_input_service.queue_set, portMAX_DELAY);

    /* A control event may have arrived together with the selected auxiliary
     * event. Always service it first; the auxiliary event remains queued. */
    if (xQueueReceive(s_input_service.control_queue, out_event, 0) == pdPASS) return true;
    return xQueueReceive(s_input_service.auxiliary_queue, out_event, 0) == pdPASS;
}

static void input_service_task(void *arg) {
    (void)arg;
    input_service_event_t event;
    for (;;) {
        if (!input_service_take_next(&event)) continue;
        if (event.kind == INPUT_SERVICE_EVENT_STOP) break;
        if (s_input_service.stopping) continue;
        if (event.input.struct_size != sizeof(device_input_event_t) ||
            event.input.abi_version != DEVICE_INPUT_EVENT_ABI_VERSION ||
            event.input.generation != s_input_service.generation ||
            event.input.generation == 0) {
            ESP_LOGW(TAG, "discarded incompatible input event: size=%lu abi=%lu generation=%lu",
                     (unsigned long)event.input.struct_size,
                     (unsigned long)event.input.abi_version,
                     (unsigned long)event.input.generation);
            continue;
        }
        if (s_input_service.consumer) {
            s_input_service.consumer(&event.input, s_input_service.consumer_context);
        }
    }
    if (s_input_service.stopped) xSemaphoreGive(s_input_service.stopped);
    vTaskDelete(NULL);
}

device_status_t input_service_start(device_input_cb_t on_input, void *context) {
    if (!on_input) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_input_service.started || s_input_service.stopping) return DEVICE_STATUS_BUSY;
    if (s_board_scanner_initialized) return DEVICE_STATUS_UNAVAILABLE;

    memset(&s_input_service, 0, sizeof(s_input_service));
    s_input_service.generation = input_service_allocate_generation();
    s_input_service.control_queue = xQueueCreate(INPUT_CONTROL_QUEUE_DEPTH,
                                                  sizeof(input_service_event_t));
    s_input_service.auxiliary_queue = xQueueCreate(INPUT_AUXILIARY_QUEUE_DEPTH,
                                                    sizeof(input_service_event_t));
    s_input_service.queue_set = xQueueCreateSet(INPUT_CONTROL_QUEUE_DEPTH +
                                                 INPUT_AUXILIARY_QUEUE_DEPTH);
    s_input_service.stopped = xSemaphoreCreateBinary();
    if (!s_input_service.control_queue || !s_input_service.auxiliary_queue ||
        !s_input_service.queue_set || !s_input_service.stopped ||
        xQueueAddToSet(s_input_service.control_queue, s_input_service.queue_set) != pdPASS ||
        xQueueAddToSet(s_input_service.auxiliary_queue, s_input_service.queue_set) != pdPASS) {
        if (s_input_service.queue_set) vQueueDelete(s_input_service.queue_set);
        if (s_input_service.auxiliary_queue) vQueueDelete(s_input_service.auxiliary_queue);
        if (s_input_service.control_queue) vQueueDelete(s_input_service.control_queue);
        if (s_input_service.stopped) vSemaphoreDelete(s_input_service.stopped);
        memset(&s_input_service, 0, sizeof(s_input_service));
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    s_input_service.consumer = on_input;
    s_input_service.consumer_context = context;
    if (xTaskCreate(input_service_task, "maclaw_input", INPUT_SERVICE_TASK_STACK,
                    NULL, INPUT_SERVICE_TASK_PRIORITY, &s_input_service.task) != pdPASS) {
        vQueueDelete(s_input_service.queue_set);
        vQueueDelete(s_input_service.auxiliary_queue);
        vQueueDelete(s_input_service.control_queue);
        vSemaphoreDelete(s_input_service.stopped);
        memset(&s_input_service, 0, sizeof(s_input_service));
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    device_status_t status = platform_input_start(input_service_publish_from_board, NULL);
    if (status != DEVICE_STATUS_OK) {
        vTaskDelete(s_input_service.task);
        vQueueDelete(s_input_service.queue_set);
        vQueueDelete(s_input_service.auxiliary_queue);
        vQueueDelete(s_input_service.control_queue);
        vSemaphoreDelete(s_input_service.stopped);
        memset(&s_input_service, 0, sizeof(s_input_service));
        return status;
    }

    /* Do not admit hardware events until the platform publisher is fully
     * installed. This closes the startup race where a key/touch edge could
     * otherwise be queued before its physical adapter completed setup. */
    taskENTER_CRITICAL(&s_input_service_lock);
    s_input_service.accepting = true;
    s_input_service.started = true;
    taskEXIT_CRITICAL(&s_input_service_lock);
    s_board_scanner_initialized = true;

    ESP_LOGI(TAG, "input service started: control=%u auxiliary=%u",
             INPUT_CONTROL_QUEUE_DEPTH, INPUT_AUXILIARY_QUEUE_DEPTH);
    return DEVICE_STATUS_OK;
}

device_status_t input_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_input_service.task && xTaskGetCurrentTaskHandle() == s_input_service.task) {
        /* A consumer callback cannot synchronously join its own dispatcher. */
        return DEVICE_STATUS_BUSY;
    }

    bool enqueue_stop = false;
    taskENTER_CRITICAL(&s_input_service_lock);
    if (!s_input_service.started && !s_input_service.stopping) {
        taskEXIT_CRITICAL(&s_input_service_lock);
        return DEVICE_STATUS_OK;
    }
    if (s_input_service.stop_in_progress) {
        taskEXIT_CRITICAL(&s_input_service_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (!s_input_service.stopping) {
        s_input_service.accepting = false;
        s_input_service.stopping = true;
    }
    s_input_service.stop_in_progress = true;
    enqueue_stop = !s_input_service.stop_enqueued;
    taskEXIT_CRITICAL(&s_input_service_lock);

    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    const TickType_t started_at = xTaskGetTickCount();
    for (;;) {
        taskENTER_CRITICAL(&s_input_service_lock);
        uint32_t active_publishers = s_input_service.publishers_in_flight;
        taskEXIT_CRITICAL(&s_input_service_lock);
        if (active_publishers == 0) break;
        if ((xTaskGetTickCount() - started_at) >= deadline) {
            input_service_release_stop_owner();
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }

    /* Board scanner callbacks publish into these queues.  Join the scanner
     * before releasing them, so the earlier publisher count cannot be made
     * stale by a new scan iteration after admission has closed. */
    TickType_t elapsed = xTaskGetTickCount() - started_at;
    TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (remaining == 0) {
        input_service_release_stop_owner();
        return DEVICE_STATUS_TIMEOUT;
    }
    device_status_t board_stop_status =
        platform_input_stop((uint32_t)remaining * portTICK_PERIOD_MS);
    if (board_stop_status != DEVICE_STATUS_OK) {
        input_service_release_stop_owner();
        return board_stop_status;
    }

    /* With admission closed, this bounded control lane is only drained by the
     * consumer.  Waiting here cannot deadlock a board scanner. */
    input_service_event_t stop_event = {
        .input = {
            .struct_size = sizeof(device_input_event_t),
            .abi_version = DEVICE_INPUT_EVENT_ABI_VERSION,
            .generation = s_input_service.generation,
        },
        .kind = INPUT_SERVICE_EVENT_STOP,
    };
    elapsed = xTaskGetTickCount() - started_at;
    remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (enqueue_stop) {
        if (remaining == 0 ||
            xQueueSend(s_input_service.control_queue, &stop_event, remaining) != pdPASS) {
            input_service_release_stop_owner();
            return DEVICE_STATUS_TIMEOUT;
        }
        taskENTER_CRITICAL(&s_input_service_lock);
        s_input_service.stop_enqueued = true;
        taskEXIT_CRITICAL(&s_input_service_lock);
    }
    elapsed = xTaskGetTickCount() - started_at;
    remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (xSemaphoreTake(s_input_service.stopped, remaining) != pdTRUE) {
        input_service_release_stop_owner();
        return DEVICE_STATUS_TIMEOUT;
    }

    /* Do not clear the struct until the stop owner has been released. A
     * concurrent caller can otherwise observe a partly-cleared generation. */
    vQueueDelete(s_input_service.queue_set);
    vQueueDelete(s_input_service.auxiliary_queue);
    vQueueDelete(s_input_service.control_queue);
    vSemaphoreDelete(s_input_service.stopped);
    taskENTER_CRITICAL(&s_input_service_lock);
    memset(&s_input_service, 0, sizeof(s_input_service));
    taskEXIT_CRITICAL(&s_input_service_lock);
    ESP_LOGI(TAG, "input service and board scanner stopped");
    return DEVICE_STATUS_OK;
}
