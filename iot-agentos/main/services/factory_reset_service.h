#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "configuration_factory_reset_policy.h"
#include "device_api.h"

#define FACTORY_RESET_SERVICE_HOST_ABI_VERSION 4u

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* The composition root supplies value-only operations for the fixed erase
     * set. It never receives a namespace/key or an NVS handle. */
    device_status_t (*erase_classes)(uint32_t classes, void *context);
    device_status_t (*verify_classes_absent)(uint32_t classes, void *context);
    device_status_t (*clear_meeting_recording)(void *context);
    device_status_t (*clear_pet_cache)(void *context);
    device_status_t (*verify_personal_storage_absent)(void *context);
    /* Recovery verifier used after a power loss between COMMITTED and
     * journal cleanup. It may admit only the explicit setup handoff and the
     * originating reset result outbox; all other fixed erase classes must be
     * absent. */
    device_status_t (*verify_recovery_state)(uint32_t classes, void *context);
    /* Validates the caller's external authorization epoch while no reset
     * journal is active.  The service never interprets Gateway credentials. */
    bool (*validate_authorization)(configuration_source_t source,
                                   uint64_t generation, void *context);
    /* Rejects or drains active foreground/network work within one bounded
     * deadline before any destructive write begins. */
    device_status_t (*prepare_for_reset)(uint32_t timeout_ms, void *context);
    /* Reverses only the PREPARE fences when the transaction aborts before
     * COMMITTED. Terminally stopped workers remain closed; this callback is
     * deliberately separate so a failed reset cannot strand reversible
     * Configuration persistence admission. */
    void (*abort_prepare_for_reset)(void *context);
    /* Requests setup mode; this must not reboot before the journal is cleared. */
    device_status_t (*complete_reset)(void *context);
    /* Performs the final reboot only after COMMITTED journal cleanup. */
    void (*reboot_after_reset)(void *context);
    void *context;
} factory_reset_service_host_t;

device_status_t factory_reset_service_init(const factory_reset_service_host_t *host);
device_status_t factory_reset_service_execute(
    const configuration_factory_reset_request_t *request);
device_status_t factory_reset_service_recover(void);
/* Reboot only after the caller has durably delivered the tool result.  A
 * false value leaves the reset handoff pending so a later retry can deliver
 * the result before rebooting. */
void factory_reset_service_reboot_if_pending(bool result_durable);
bool factory_reset_service_is_initialized(void);
