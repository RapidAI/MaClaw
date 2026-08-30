#include "services/pet_asset_profile_service.h"

static bool host_valid(const pet_asset_profile_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->startup_profile_matches && host->startup_pending &&
           host->set_startup_pending && host->apply_asset && host->clear_asset &&
           host->status_permanently_invalid && host->note_transient_failure &&
           host->retry_exhausted && host->reset_retries;
}

pet_asset_profile_service_result_t pet_asset_profile_service_apply(
    const pet_asset_profile_service_host_t *host,
    const pet_asset_descriptor_t *descriptor, const char *skin,
    const char *message_id, uint32_t retry_limit) {
    pet_asset_profile_service_result_t result = {
        .handled = false,
        .permanently_invalid = false,
        .status = DEVICE_STATUS_INVALID_ARGUMENT,
    };
    if (!host_valid(host)) return result;

    if (descriptor && descriptor->revision[0]) {
        if (host->startup_profile_matches(descriptor->revision, skin, host->context)) {
            result.handled = true;
            result.deferred_to_startup = true;
            result.status = DEVICE_STATUS_OK;
            return result;
        }
        if (host->startup_pending(host->context)) {
            host->set_startup_pending(false, host->context);
            result.superseded_startup = true;
        }
        result.status = host->apply_asset(descriptor, host->context);
    } else {
        result.status = host->clear_asset(host->context);
    }

    result.handled = result.status == DEVICE_STATUS_OK;
    result.permanently_invalid =
        host->status_permanently_invalid(result.status, host->context);
    if (result.handled) {
        host->reset_retries(host->context);
        return result;
    }
    result.retry_count = host->note_transient_failure(message_id, host->context);
    if (host->retry_exhausted(retry_limit, host->context)) {
        result.permanently_invalid = true;
    }
    return result;
}
