#include "services/startup_welcome_service.h"

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static startup_welcome_service_host_t s_host;
static SemaphoreHandle_t s_completion;
static bool s_handshake_queued;
static bool s_gate_active;
static bool s_timed_out;
static bool s_consumed;
static bool s_initialized;

static bool host_valid(const startup_welcome_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->log_gate_released && host->log_gate_timed_out;
}

device_status_t startup_welcome_service_init(
    const startup_welcome_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);

    SemaphoreHandle_t completion = xSemaphoreCreateBinary();
    if (!completion) return DEVICE_STATUS_RESOURCE_EXHAUSTED;

    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        s_host = *host;
        s_completion = completion;
        s_handshake_queued = false;
        s_gate_active = false;
        s_timed_out = false;
        s_consumed = false;
        s_initialized = true;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);
    vSemaphoreDelete(completion);
    return DEVICE_STATUS_OK;
}

void startup_welcome_service_note_handshake_queued(bool queued) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) s_handshake_queued = queued;
    taskEXIT_CRITICAL(&s_lock);
}

bool startup_welcome_service_begin_sequence(void) {
    SemaphoreHandle_t completion = NULL;
    bool queued = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) completion = s_completion;
    taskEXIT_CRITICAL(&s_lock);
    if (!completion) return false;

    while (xSemaphoreTake(completion, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_lock);
    queued = s_handshake_queued;
    s_gate_active = queued;
    s_timed_out = false;
    s_consumed = false;
    taskEXIT_CRITICAL(&s_lock);
    return queued;
}

void startup_welcome_service_mark_startup_failed(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        s_gate_active = false;
        s_timed_out = s_handshake_queued;
    }
    taskEXIT_CRITICAL(&s_lock);
}

bool startup_welcome_service_wait_for_completion(uint32_t timeout_ms) {
    if (timeout_ms == 0) return false;
    SemaphoreHandle_t completion = NULL;
    startup_welcome_service_host_t host = {0};
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        completion = s_completion;
        host = s_host;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!completion) return false;
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    if (ticks == 0) ticks = 1;
    if (xSemaphoreTake(completion, ticks) == pdTRUE) return true;

    bool timed_out = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized && s_gate_active) {
        s_gate_active = false;
        s_timed_out = true;
        timed_out = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (timed_out) host.log_gate_timed_out(timeout_ms, host.context);
    return false;
}

bool startup_welcome_service_gate_active(void) {
    bool active = false;
    taskENTER_CRITICAL(&s_lock);
    active = s_initialized && s_gate_active;
    taskEXIT_CRITICAL(&s_lock);
    return active;
}

bool startup_welcome_service_should_discard_current(void) {
    bool discard = true;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) discard = s_timed_out || s_consumed;
    taskEXIT_CRITICAL(&s_lock);
    return discard;
}

void startup_welcome_service_complete_current(bool playback_succeeded) {
    startup_welcome_service_host_t host = {0};
    SemaphoreHandle_t completion = NULL;
    bool notify = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        s_consumed = true;
        if (s_gate_active) {
            s_gate_active = false;
            notify = true;
        }
        host = s_host;
        completion = s_completion;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!notify) return;
    host.log_gate_released(playback_succeeded ? "playback complete" :
                                                "playback unavailable",
                           host.context);
    if (completion) xSemaphoreGive(completion);
}
