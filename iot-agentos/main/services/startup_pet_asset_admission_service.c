#include "services/startup_pet_asset_admission_service.h"

static bool host_valid(const startup_pet_asset_admission_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->snapshot &&
           host->stop_requested && host->system_sleep_preparing &&
           host->prepare_for_display && host->capacity_available &&
           host->drop_stale_cache && host->take_capacity_retry &&
           host->return_capacity_retry && host->schedule_retry &&
           host->finish_generation && host->worker_active &&
           host->gateway_operational && host->start_worker &&
           host->revision_installed && host->set_pending;
}

startup_pet_asset_admission_result_t
startup_pet_asset_admission_service_admit_pending(
    const startup_pet_asset_admission_service_host_t *host,
    uint32_t retry_limit, uint32_t *out_retry_attempt,
    device_status_t *out_start_status) {
    if (out_retry_attempt) *out_retry_attempt = 0;
    if (out_start_status) *out_start_status = DEVICE_STATUS_OK;
    if (!host_valid(host) || retry_limit == 0) {
        if (out_start_status) *out_start_status = DEVICE_STATUS_INVALID_ARGUMENT;
        return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }

    startup_pet_asset_state_snapshot_t state = {0};
    if (!host->snapshot(&state, host->context) || !state.pending ||
        host->stop_requested(host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }

    /* An absent descriptor is a local clear transaction, not optional network
     * artwork. Let the startup transaction clear a previously retained frame
     * even after the Hub has withdrawn pet-asset capability. */
    if (!state.present) {
        if (host->system_sleep_preparing(host->context) ||
            host->worker_active(host->context)) {
            return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
        }
        const device_status_t status = host->start_worker(host->context);
        if (out_start_status) *out_start_status = status;
        return status == DEVICE_STATUS_OK ? STARTUP_PET_ASSET_ADMISSION_STARTED
                                          : STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }

    pet_asset_descriptor_t descriptor = {0};
    const bool display_ready = host->prepare_for_display(
        &state.descriptor, &descriptor, host->context);
    if (display_ready && !host->capacity_available(&descriptor, host->context)) {
        (void)host->drop_stale_cache(&descriptor, host->context);
    }
    if (!display_ready || !host->capacity_available(&descriptor, host->context)) {
        uint32_t attempt = 0;
        if (display_ready && host->take_capacity_retry(state.generation, retry_limit,
                                                        &attempt, host->context)) {
            if (host->schedule_retry(host->context)) {
                if (out_retry_attempt) *out_retry_attempt = attempt;
                return STARTUP_PET_ASSET_ADMISSION_RETRY_SCHEDULED;
            }
            host->return_capacity_retry(state.generation, host->context);
        }
        host->finish_generation(state.generation, host->context);
        return STARTUP_PET_ASSET_ADMISSION_FINISHED;
    }

    if (host->system_sleep_preparing(host->context) ||
        host->worker_active(host->context) ||
        !host->gateway_operational(host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }

    const device_status_t status = host->start_worker(host->context);
    if (out_start_status) *out_start_status = status;
    return status == DEVICE_STATUS_OK ? STARTUP_PET_ASSET_ADMISSION_STARTED
                                      : STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
}

startup_pet_asset_admission_result_t
startup_pet_asset_admission_service_rearm_preempted(
    const startup_pet_asset_admission_service_host_t *host) {
    if (!host_valid(host)) return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;

    startup_pet_asset_state_snapshot_t state = {0};
    if (!host->snapshot(&state, host->context) || !state.present || state.pending ||
        host->stop_requested(host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }
    pet_asset_descriptor_t descriptor = {0};
    if (!host->prepare_for_display(&state.descriptor, &descriptor, host->context) ||
        host->revision_installed(&descriptor, host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
    }

    host->set_pending(true, host->context);
    if (!host->worker_active(host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_REARMED;
    }
    if (host->schedule_retry(host->context)) {
        return STARTUP_PET_ASSET_ADMISSION_REARMED;
    }
    host->set_pending(false, host->context);
    return STARTUP_PET_ASSET_ADMISSION_NO_ACTION;
}
