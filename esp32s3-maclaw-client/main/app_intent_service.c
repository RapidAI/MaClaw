#include "app_intent_service.h"

#include <stddef.h>
#include <string.h>

#include "esp_log.h"
#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

typedef struct {
    device_input_action_t input_action;
    app_intent_type_t intent;
} input_binding_t;

/* The table is intentionally hardware-neutral. A new profile adapts its
 * touch/key/rotary scanner to Device Input; it never gets a private business
 * callback or a board-specific branch in application policy. */
static const input_binding_t s_input_bindings[] = {
    { DEVICE_INPUT_PRIMARY,      APP_INTENT_PRIMARY_ACTIVATE },
    { DEVICE_INPUT_SECONDARY,    APP_INTENT_SECONDARY_ACTIVATE },
    { DEVICE_INPUT_CONFIGURE,    APP_INTENT_OPEN_CONFIGURATION },
    { DEVICE_INPUT_VOLUME_UP,    APP_INTENT_INCREASE_VOLUME },
    { DEVICE_INPUT_VOLUME_DOWN,  APP_INTENT_DECREASE_VOLUME },
};

static const char *TAG = "maclaw_intent";

#define APP_INTENT_CONTROL_QUEUE_DEPTH 16
#define APP_INTENT_AUXILIARY_QUEUE_DEPTH 8
/* Reservation for cancel, alarm dismissal and configuration. Normal primary
 * activation and volume traffic can never consume these slots. */
#define APP_INTENT_CRITICAL_QUEUE_DEPTH 4
#define APP_INTENT_SERVICE_TASK_STACK 6144
#define APP_INTENT_SERVICE_TASK_PRIORITY 9

typedef enum {
    APP_INTENT_QUEUE_EVENT_INTENT = 0,
    APP_INTENT_QUEUE_EVENT_STOP,
} app_intent_queue_event_kind_t;

typedef struct {
    app_intent_event_t intent;
    app_intent_queue_event_kind_t kind;
} app_intent_queue_event_t;

typedef struct {
    QueueHandle_t critical_queue;
    QueueHandle_t control_queue;
    QueueHandle_t auxiliary_queue;
    QueueSetHandle_t queue_set;
    TaskHandle_t task;
    SemaphoreHandle_t stopped;
    app_intent_cb_t consumer;
    void *consumer_context;
    uint32_t critical_pending;
    uint32_t control_pending;
    uint32_t auxiliary_pending;
    uint32_t dropped_critical;
    uint32_t dropped_control;
    uint32_t dropped_auxiliary;
    bool critical_overflow;
    bool started;
    bool accepting;
    bool stopping;
    bool stop_enqueued;
    uint32_t publishers_in_flight;
} app_intent_service_state_t;

static app_intent_service_state_t s_service;
static portMUX_TYPE s_service_lock = portMUX_INITIALIZER_UNLOCKED;

static bool is_control_intent(app_intent_type_t intent) {
    return intent != APP_INTENT_INCREASE_VOLUME &&
           intent != APP_INTENT_DECREASE_VOLUME;
}

static bool is_critical_intent(app_intent_type_t intent) {
    return intent == APP_INTENT_SECONDARY_ACTIVATE ||
           intent == APP_INTENT_OPEN_CONFIGURATION ||
           intent == APP_INTENT_PRIMARY_CONTACT_DOWN;
}

static bool binding_for(device_input_action_t action, app_intent_type_t *out_intent) {
    if (!out_intent) return false;
    for (size_t i = 0; i < sizeof(s_input_bindings) / sizeof(s_input_bindings[0]); ++i) {
        if (s_input_bindings[i].input_action == action) {
            *out_intent = s_input_bindings[i].intent;
            return true;
        }
    }
    return false;
}

typedef enum {
    APP_INTENT_QUEUE_LANE_CRITICAL = 0,
    APP_INTENT_QUEUE_LANE_CONTROL,
    APP_INTENT_QUEUE_LANE_AUXILIARY,
} app_intent_queue_lane_t;

static bool take_next(app_intent_queue_event_t *out_event,
                      app_intent_queue_lane_t *out_lane) {
    if (xQueueReceive(s_service.critical_queue, out_event, 0) == pdPASS) {
        *out_lane = APP_INTENT_QUEUE_LANE_CRITICAL;
        return true;
    }
    if (xQueueReceive(s_service.control_queue, out_event, 0) == pdPASS) {
        *out_lane = APP_INTENT_QUEUE_LANE_CONTROL;
        return true;
    }
    (void)xQueueSelectFromSet(s_service.queue_set, portMAX_DELAY);
    if (xQueueReceive(s_service.critical_queue, out_event, 0) == pdPASS) {
        *out_lane = APP_INTENT_QUEUE_LANE_CRITICAL;
        return true;
    }
    if (xQueueReceive(s_service.control_queue, out_event, 0) == pdPASS) {
        *out_lane = APP_INTENT_QUEUE_LANE_CONTROL;
        return true;
    }
    if (xQueueReceive(s_service.auxiliary_queue, out_event, 0) == pdPASS) {
        *out_lane = APP_INTENT_QUEUE_LANE_AUXILIARY;
        return true;
    }
    return false;
}

static void app_interaction_task(void *arg) {
    (void)arg;
    app_intent_queue_event_t event;
    for (;;) {
        app_intent_queue_lane_t lane;
        if (!take_next(&event, &lane)) continue;
        if (event.kind == APP_INTENT_QUEUE_EVENT_INTENT) {
            taskENTER_CRITICAL(&s_service_lock);
            uint32_t *pending = lane == APP_INTENT_QUEUE_LANE_CRITICAL
                                    ? &s_service.critical_pending
                                    : lane == APP_INTENT_QUEUE_LANE_CONTROL
                                          ? &s_service.control_pending
                                          : &s_service.auxiliary_pending;
            if (*pending > 0) --*pending;
            taskEXIT_CRITICAL(&s_service_lock);
        }
        if (event.kind == APP_INTENT_QUEUE_EVENT_STOP) break;
        if (s_service.stopping) continue;
        if (event.intent.struct_size != sizeof(app_intent_event_t) ||
            event.intent.abi_version != APP_INTENT_ABI_VERSION) {
            ESP_LOGW(TAG, "discarded incompatible app intent: size=%lu abi=%lu",
                     (unsigned long)event.intent.struct_size,
                     (unsigned long)event.intent.abi_version);
            continue;
        }
        if (s_service.consumer) {
            s_service.consumer(&event.intent, s_service.consumer_context);
        }
    }
    if (s_service.stopped) xSemaphoreGive(s_service.stopped);
    vTaskDelete(NULL);
}

static void on_device_input(const device_input_event_t *input, void *context) {
    (void)context;
    if (!input || input->struct_size != sizeof(*input) ||
        input->abi_version != DEVICE_PROFILE_ABI_VERSION) {
        ESP_LOGW(TAG, "discarded invalid Device Input event");
        return;
    }
    bool primary_source = device_input_is_primary_interaction_source(input->source);
    app_intent_type_t intent;
    if (input->action == DEVICE_INPUT_CONTACT_DOWN) {
        /* Contact ownership is decided at the binding seam, not in app
         * policy. The app can distinguish a standard activation contact from
         * an optional auxiliary control without knowing a profile or GPIO. */
        intent = primary_source
                     ? APP_INTENT_PRIMARY_CONTACT_DOWN
                     : APP_INTENT_AUXILIARY_CONTACT_DOWN;
    } else if (!binding_for(input->action, &intent)) {
        ESP_LOGW(TAG, "discarded unbound Device Input action=%d", (int)input->action);
        return;
    }
    app_intent_event_t event = {
        .struct_size = sizeof(app_intent_event_t),
        .abi_version = APP_INTENT_ABI_VERSION,
        .input_sequence = input->sequence,
        .timestamp_us = input->timestamp_us,
        .type = intent,
        .primary_interaction_source = primary_source,
        .source = input->source,
    };
    bool critical = is_critical_intent(intent);
    bool control = !critical && is_control_intent(intent);
    QueueHandle_t queue;
    taskENTER_CRITICAL(&s_service_lock);
    if (!s_service.accepting) {
        taskEXIT_CRITICAL(&s_service_lock);
        return;
    }
    ++s_service.publishers_in_flight;
    queue = critical ? s_service.critical_queue
                     : control ? s_service.control_queue : s_service.auxiliary_queue;
    taskEXIT_CRITICAL(&s_service_lock);

    app_intent_queue_event_t queued = {
        .intent = event,
        .kind = APP_INTENT_QUEUE_EVENT_INTENT,
    };
    if (xQueueSend(queue, &queued, 0) != pdPASS) {
        taskENTER_CRITICAL(&s_service_lock);
        uint32_t *dropped = critical ? &s_service.dropped_critical
                            : control ? &s_service.dropped_control
                                      : &s_service.dropped_auxiliary;
        if (critical) s_service.critical_overflow = true;
        ++*dropped;
        uint32_t count = *dropped;
        taskEXIT_CRITICAL(&s_service_lock);
        ESP_LOGE(TAG, "app intent queue full: lane=%s intent=%d source=%d dropped=%lu",
                 critical ? "critical" : control ? "control" : "auxiliary",
                 (int)intent, (int)input->source,
                 (unsigned long)count);
    } else {
        taskENTER_CRITICAL(&s_service_lock);
        uint32_t *pending = critical ? &s_service.critical_pending
                            : control ? &s_service.control_pending
                                      : &s_service.auxiliary_pending;
        ++*pending;
        taskEXIT_CRITICAL(&s_service_lock);
    }
    taskENTER_CRITICAL(&s_service_lock);
    --s_service.publishers_in_flight;
    taskEXIT_CRITICAL(&s_service_lock);
}

device_status_t app_intent_service_start(app_intent_cb_t on_intent, void *context) {
    if (!on_intent) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_service.started || s_service.stopping) return DEVICE_STATUS_BUSY;
    memset(&s_service, 0, sizeof(s_service));
    s_service.critical_queue = xQueueCreate(APP_INTENT_CRITICAL_QUEUE_DEPTH,
                                             sizeof(app_intent_queue_event_t));
    s_service.control_queue = xQueueCreate(APP_INTENT_CONTROL_QUEUE_DEPTH,
                                            sizeof(app_intent_queue_event_t));
    s_service.auxiliary_queue = xQueueCreate(APP_INTENT_AUXILIARY_QUEUE_DEPTH,
                                              sizeof(app_intent_queue_event_t));
    s_service.queue_set = xQueueCreateSet(APP_INTENT_CRITICAL_QUEUE_DEPTH +
                                           APP_INTENT_CONTROL_QUEUE_DEPTH +
                                           APP_INTENT_AUXILIARY_QUEUE_DEPTH);
    s_service.stopped = xSemaphoreCreateBinary();
    if (!s_service.critical_queue || !s_service.control_queue || !s_service.auxiliary_queue ||
        !s_service.queue_set || !s_service.stopped ||
        xQueueAddToSet(s_service.critical_queue, s_service.queue_set) != pdPASS ||
        xQueueAddToSet(s_service.control_queue, s_service.queue_set) != pdPASS ||
        xQueueAddToSet(s_service.auxiliary_queue, s_service.queue_set) != pdPASS) {
        if (s_service.queue_set) vQueueDelete(s_service.queue_set);
        if (s_service.auxiliary_queue) vQueueDelete(s_service.auxiliary_queue);
        if (s_service.control_queue) vQueueDelete(s_service.control_queue);
        if (s_service.critical_queue) vQueueDelete(s_service.critical_queue);
        if (s_service.stopped) vSemaphoreDelete(s_service.stopped);
        memset(&s_service, 0, sizeof(s_service));
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    s_service.consumer = on_intent;
    s_service.consumer_context = context;
    /* Keep the substantial business task stack out of the internal heap used
     * by Wi-Fi/TLS and ESP-SR. All formal profiles enable external stacks. */
    if (xTaskCreateWithCaps(app_interaction_task, "maclaw_interact",
                            APP_INTENT_SERVICE_TASK_STACK, NULL,
                            APP_INTENT_SERVICE_TASK_PRIORITY, &s_service.task,
                            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) != pdPASS) {
        vQueueDelete(s_service.queue_set);
        vQueueDelete(s_service.auxiliary_queue);
        vQueueDelete(s_service.control_queue);
        vQueueDelete(s_service.critical_queue);
        vSemaphoreDelete(s_service.stopped);
        memset(&s_service, 0, sizeof(s_service));
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    /* The service task and queues exist before Input Service installs its
     * board scanner. A scan edge can therefore never run app policy directly. */
    device_status_t status = device_input_start(on_device_input, NULL);
    if (status != DEVICE_STATUS_OK) {
        vTaskDelete(s_service.task);
        vQueueDelete(s_service.queue_set);
        vQueueDelete(s_service.auxiliary_queue);
        vQueueDelete(s_service.control_queue);
        vQueueDelete(s_service.critical_queue);
        vSemaphoreDelete(s_service.stopped);
        memset(&s_service, 0, sizeof(s_service));
        return status;
    }
    taskENTER_CRITICAL(&s_service_lock);
    s_service.accepting = true;
    s_service.started = true;
    taskEXIT_CRITICAL(&s_service_lock);
    ESP_LOGI(TAG, "app interaction task started: critical=%u control=%u auxiliary=%u",
             APP_INTENT_CRITICAL_QUEUE_DEPTH, APP_INTENT_CONTROL_QUEUE_DEPTH,
             APP_INTENT_AUXILIARY_QUEUE_DEPTH);
    return DEVICE_STATUS_OK;
}

device_status_t app_intent_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_service.task && xTaskGetCurrentTaskHandle() == s_service.task) {
        return DEVICE_STATUS_BUSY;
    }
    bool enqueue_stop = false;
    taskENTER_CRITICAL(&s_service_lock);
    if (!s_service.started && !s_service.stopping) {
        taskEXIT_CRITICAL(&s_service_lock);
        return DEVICE_STATUS_OK;
    }
    if (!s_service.stopping) {
        s_service.accepting = false;
        s_service.stopping = true;
    }
    if (!s_service.stop_enqueued) {
        s_service.stop_enqueued = true;
        enqueue_stop = true;
    }
    taskEXIT_CRITICAL(&s_service_lock);
    if (!enqueue_stop) return DEVICE_STATUS_BUSY;

    const TickType_t deadline = pdMS_TO_TICKS(timeout_ms);
    const TickType_t started_at = xTaskGetTickCount();
    /* First stop Device Input. Its bounded join guarantees no binding callback
     * remains in flight before this service releases its own queues. */
    device_status_t input_status = device_input_stop(timeout_ms);
    if (input_status != DEVICE_STATUS_OK) return input_status;
    for (;;) {
        taskENTER_CRITICAL(&s_service_lock);
        uint32_t publishers = s_service.publishers_in_flight;
        taskEXIT_CRITICAL(&s_service_lock);
        if (publishers == 0) break;
        if ((xTaskGetTickCount() - started_at) >= deadline) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    app_intent_queue_event_t stop_event = {
        .intent = {
            .struct_size = sizeof(app_intent_event_t),
            .abi_version = APP_INTENT_ABI_VERSION,
        },
        .kind = APP_INTENT_QUEUE_EVENT_STOP,
    };
    if (xQueueSend(s_service.critical_queue, &stop_event, deadline) != pdPASS) {
        return DEVICE_STATUS_TIMEOUT;
    }
    TickType_t elapsed = xTaskGetTickCount() - started_at;
    TickType_t remaining = elapsed >= deadline ? 0 : deadline - elapsed;
    if (xSemaphoreTake(s_service.stopped, remaining) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    vQueueDelete(s_service.queue_set);
    vQueueDelete(s_service.auxiliary_queue);
    vQueueDelete(s_service.control_queue);
    vQueueDelete(s_service.critical_queue);
    vSemaphoreDelete(s_service.stopped);
    memset(&s_service, 0, sizeof(s_service));
    ESP_LOGI(TAG, "app interaction task stopped");
    return DEVICE_STATUS_OK;
}

bool app_intent_service_get_snapshot(app_intent_service_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_service_lock);
    *out_snapshot = (app_intent_service_snapshot_t){
        .started = s_service.started,
        .critical_overflow = s_service.critical_overflow,
        .critical_pending = s_service.critical_pending,
        .control_pending = s_service.control_pending,
        .auxiliary_pending = s_service.auxiliary_pending,
        .dropped_critical = s_service.dropped_critical,
        .dropped_control = s_service.dropped_control,
        .dropped_auxiliary = s_service.dropped_auxiliary,
    };
    taskEXIT_CRITICAL(&s_service_lock);
    return true;
}
