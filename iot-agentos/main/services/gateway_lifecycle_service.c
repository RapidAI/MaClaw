#include "services/gateway_lifecycle_service.h"

#include "esp_timer.h"
#include "freertos/FreeRTOS.h"

#include "services/gateway_dispatcher.h"
#include "services/gateway_transport.h"
#include "services/meeting_service.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_system_sleep_preparing;

static uint32_t remaining_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const int64_t ms = (remaining_us + 999) / 1000;
    return ms > UINT32_MAX ? UINT32_MAX : (uint32_t)ms;
}

static void restore_prepared_workers(void) {
    /* Strict reverse order. Each participant records its own pre-PREPARE
     * generation, so rollback cannot manufacture unrelated background work. */
    gateway_dispatcher_abort_system_sleep_prepare();
    gateway_transport_abort_system_sleep_prepare();
    meeting_service_abort_capability_refresh_system_sleep_prepare();
    meeting_service_abort_resumed_worker_system_sleep_prepare();
    meeting_service_abort_resume_supervisor_system_sleep_prepare();
}

device_status_t gateway_lifecycle_service_init(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    s_system_sleep_preparing = false;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t gateway_lifecycle_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lock);

    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining = remaining_ms(deadline_us);
    device_status_t status = remaining
        ? gateway_transport_cancel_active_requests(GATEWAY_TRANSPORT_CANCEL_ALL,
                                                   remaining)
        : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) return status;

    /* ML307 cancellation stays behind the Device API. Wi-Fi-only profiles
     * take the same business path and simply report non-cellular. */
    if (device_connectivity_is_active_cellular()) {
        (void)device_connectivity_cancel_cellular_foreground_request();
        uintptr_t meeting_worker = meeting_service_worker_owner_token();
        if (meeting_worker) {
            (void)device_connectivity_cancel_cellular_requests_for_owner(
                (const void *)meeting_worker);
        }
    }

    remaining = remaining_ms(deadline_us);
    status = remaining ? meeting_service_prepare_resume_supervisor_system_sleep(remaining)
                       : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) return status;
    remaining = remaining_ms(deadline_us);
    status = remaining ? meeting_service_prepare_resumed_worker_system_sleep(remaining)
                       : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) return status;
    remaining = remaining_ms(deadline_us);
    status = remaining ? meeting_service_prepare_capability_refresh_system_sleep(remaining)
                       : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) return status;
    remaining = remaining_ms(deadline_us);
    status = remaining ? gateway_transport_prepare_system_sleep(remaining)
                       : DEVICE_STATUS_TIMEOUT;
    if (status != DEVICE_STATUS_OK) return status;
    remaining = remaining_ms(deadline_us);
    status = remaining ? gateway_dispatcher_prepare_system_sleep(remaining)
                       : DEVICE_STATUS_TIMEOUT;
    return status;
}

void gateway_lifecycle_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || !s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return;
    }
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lock);
    restore_prepared_workers();
}

device_status_t gateway_lifecycle_service_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Keep the outer fence closed while every child forgets its restart-on-
     * ABORT record. No child is restarted by this path. */
    taskEXIT_CRITICAL(&s_lock);

    device_status_t status = gateway_dispatcher_commit_prepared_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    status = gateway_transport_commit_prepared_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    status = meeting_service_commit_capability_refresh_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    status = meeting_service_commit_resumed_worker_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    status = meeting_service_commit_resume_supervisor_network_restart();
    if (status != DEVICE_STATUS_OK) return status;

    taskENTER_CRITICAL(&s_lock);
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}
