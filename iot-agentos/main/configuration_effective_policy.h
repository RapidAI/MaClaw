#pragma once

/*
 * Effective configuration value model.
 *
 * Configuration Service owns the durable snapshot. This helper owns neither
 * NVS nor a clock: callers provide a monotonic time and may layer at most one
 * authenticated, bounded runtime override for each reversible user policy.
 * It is deliberately a pure value operation so board-specific radio/panel
 * paths cannot reinterpret precedence independently.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_policy.h"
#include "configuration_service.h"

#define CONFIGURATION_RUNTIME_OVERRIDE_ABI_VERSION 1u

typedef enum {
    CONFIGURATION_RUNTIME_OVERRIDE_VALUE_INVALID = 0,
    CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME,
    CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS,
    CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS,
    CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION,
} configuration_runtime_override_value_kind_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    configuration_runtime_override_value_kind_t kind;
    uint32_t value_u32;
    uint64_t expires_at_monotonic_ms;
    /* This is evidence supplied by the owner that already authenticated the
     * incoming control surface.  The value layer cannot authenticate a Hub
     * message itself, but it must never manufacture that fact.  `ttl_ms` is
     * derived from expires_at_monotonic_ms at resolution time. */
    configuration_policy_request_t provenance;
} configuration_runtime_override_t;

/* Validates only a single override record against caller-supplied monotonic
 * time. It does not retain the record or read durable configuration. */
bool configuration_runtime_override_validate(
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms);

/* Resolves `durable` plus one optional runtime override into an immutable-by-
 * copy effective snapshot. Invalid/expired override records are rejected, not
 * silently applied. `now_monotonic_ms` is supplied by a future clock owner;
 * this module does not sample wall time or retain mutable state. */
bool configuration_effective_policy_resolve(
    const configuration_snapshot_t *durable,
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms,
    configuration_snapshot_t *out_effective,
    bool *out_override_active);
