#include "services/startup_pet_asset_sleep_service.h"

static bool host_valid(const startup_pet_asset_sleep_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->monotonic_time_us &&
           host->prepare_state && host->prepare_worker && host->prepare_retry &&
           host->prepare_cache && host->abort_state && host->abort_worker &&
           host->abort_retry && host->abort_cache && host->server_audio_lease_active &&
           host->take_audio_preemption && host->rearm_preempted;
}

static uint32_t remaining_timeout_ms(const startup_pet_asset_sleep_service_host_t *host,
                                     int64_t deadline_us) {
    const int64_t now_us = host->monotonic_time_us(host->context);
    if (now_us >= deadline_us) return 0;
    const int64_t remaining_us = deadline_us - now_us;
    return (uint32_t)((remaining_us + 999) / 1000);
}

static device_status_t prepare_postcondition(
    const startup_pet_asset_sleep_service_host_t *host,
    device_status_t status, int64_t deadline_us) {
    /* A child may finish exactly at the parent boundary and still report OK.
     * Keep the composite PREPARE fail-closed so Power, not this helper, owns
     * the decision to roll back the already-closed participants. */
    if (status == DEVICE_STATUS_OK && remaining_timeout_ms(host, deadline_us) == 0u) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return status;
}

device_status_t startup_pet_asset_sleep_service_prepare(
    const startup_pet_asset_sleep_service_host_t *host, uint32_t timeout_ms) {
    if (!host_valid(host) || timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t started_us = host->monotonic_time_us(host->context);
    if (started_us < 0) return DEVICE_STATUS_INTERNAL_ERROR;
    const int64_t deadline_us = started_us + (int64_t)timeout_ms * 1000;

    device_status_t status = prepare_postcondition(host,
                                                   host->prepare_state(host->context),
                                                   deadline_us);
    if (status != DEVICE_STATUS_OK) return status;
    uint32_t remaining_ms = remaining_timeout_ms(host, deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    status = prepare_postcondition(host,
                                   host->prepare_worker(remaining_ms, host->context),
                                   deadline_us);
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = remaining_timeout_ms(host, deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    status = prepare_postcondition(host,
                                   host->prepare_retry(remaining_ms, host->context),
                                   deadline_us);
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = remaining_timeout_ms(host, deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return prepare_postcondition(host,
                                 host->prepare_cache(remaining_ms, host->context),
                                 deadline_us);
}

void startup_pet_asset_sleep_service_abort(
    const startup_pet_asset_sleep_service_host_t *host) {
    if (!host_valid(host)) return;
    bool restored_audio_preemption = false;
    if (!host->abort_state(&restored_audio_preemption, host->context)) return;
    host->abort_cache(host->context);
    host->abort_retry(host->context);
    host->abort_worker(host->context);
    if (restored_audio_preemption && !host->server_audio_lease_active(host->context) &&
        host->take_audio_preemption(host->context)) {
        host->rearm_preempted(host->context);
    }
}
