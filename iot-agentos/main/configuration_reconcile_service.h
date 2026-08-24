#pragma once

/*
 * Configuration consumer reconciliation owner.
 *
 * This is deliberately above Device/Platform HAL and below the composition
 * root.  It owns no configuration persistence and knows no panel, codec,
 * GPIO, controller, task or board profile.  Its one serialized operation
 * consumes a copied effective Configuration snapshot and drives only the two
 * consumers that currently have a common acknowledged semantic contract:
 * output volume, display brightness and ambient DISPLAY_OFF idle policy.
 *
 * Transport selection stays out until it offers equivalent observed/revert
 * semantics. A caller may invoke reconcile after durable
 * publication, runtime-override expiry, or boot restoration; callers must not
 * directly apply these two values in parallel.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_apply_state.h"
#include "configuration_effective_policy.h"
#include "configuration_reconcile_retry_policy.h"
#include "device_api.h"

#define CONFIGURATION_RECONCILE_SERVICE_SNAPSHOT_ABI_VERSION 1u

typedef enum {
    /* Cold boot never replays persisted brightness=0: zero is a live panel
     * management command, and boot must retain a local visible recovery path. */
    CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE = 0,
    /* A newly published durable setting or a future volatile override. */
    CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY,
    /* A future owner may retry a prior failed/unknown application. */
    CONFIGURATION_RECONCILE_REASON_RETRY,
    /* The sole expiry worker removed one or more volatile overrides and must
     * restore durable effective values through the same consumer owner. */
    CONFIGURATION_RECONCILE_REASON_RUNTIME_OVERRIDE_EXPIRY,
} configuration_reconcile_service_reason_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool initialized;
    bool reconciling;
    bool retry_armed;
    uint32_t retry_attempt;
    configuration_apply_state_t apply_state;
    device_status_t last_status;
} configuration_reconcile_service_snapshot_t;

/* An optional caller-owned authorization epoch fences a reconciliation that
 * originated at an external policy boundary.  It is intentionally generic:
 * Configuration sees only value fields and asks the composition root whether
 * this epoch is still current.  It never includes Gateway, JSON, transport,
 * RTOS or board types. */
#define CONFIGURATION_RECONCILE_AUTHORIZATION_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t authority_kind;
    uint32_t generation;
    uint32_t required_permissions;
} configuration_reconcile_authorization_t;

typedef bool (*configuration_reconcile_authorization_current_t)(
    const configuration_reconcile_authorization_t *authorization, void *context);

/* Installs the composition-root validator before any externally authorized
 * reconcile can be admitted. Passing NULL clears it; callers carrying an
 * authorization then fail closed. This does not alter ordinary local/boot
 * reconciliation. */
void configuration_reconcile_service_set_authorization_validator(
    configuration_reconcile_authorization_current_t validator, void *context);

device_status_t configuration_reconcile_service_init(void);
/* Stops new callers after taking the same serialized mutex. It does not alter
 * Audio/Display hardware; their normal lifecycle remains independently owned. */
device_status_t configuration_reconcile_service_deinit(uint32_t timeout_ms);

/* Loads exactly one effective revisioned snapshot, updates desired state, then
 * serially applies volume, brightness and screen-idle policy. A non-OK result
 * means durable intent remains published but at least one consumer lacks
 * convergence proof; the retained coordinator schedules its one bounded retry
 * path for transient results. Callers must never compensate by writing another
 * Configuration revision. */
device_status_t configuration_reconcile_service_reconcile(
    configuration_reconcile_service_reason_t reason);

/* Same serialized apply owner, bound to one external authorization epoch.
 * Every consumer pass and retained retry revalidates the copied value before
 * it starts side effects and before it keeps another retry. An expired epoch
 * leaves durable desired configuration intact but never applies it later. */
device_status_t configuration_reconcile_service_reconcile_authorized(
    configuration_reconcile_service_reason_t reason,
    const configuration_reconcile_authorization_t *authorization);

/* Volatile policy ingress belongs to the same owner as its single absolute
 * expiry timer. Each successful mutation immediately reconciles and rearms
 * the earliest deadline; callers must not invoke the raw Configuration
 * runtime-override facade when they expect consumer state to change.
 * Transport selection is deliberately rejected with UNAVAILABLE until its
 * complete quiesce/readiness/rollback transaction is implemented. */
device_status_t configuration_reconcile_service_apply_runtime_override(
    const configuration_runtime_override_t *override);
device_status_t configuration_reconcile_service_remove_runtime_override(
    configuration_runtime_override_value_kind_t kind);
device_status_t configuration_reconcile_service_clear_runtime_overrides(void);

bool configuration_reconcile_service_get_snapshot(
    configuration_reconcile_service_snapshot_t *out_snapshot);
