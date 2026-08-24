#include "services/gateway_capability_projection.h"

#include <string.h>

static bool flags_are_known(gateway_capability_flags_t flags) {
    return (flags & ~GATEWAY_CAPABILITY_KNOWN_MASK) == 0;
}

static bool projection_shape_is_valid(
    const gateway_capability_projection_t *projection) {
    if (!projection ||
        projection->struct_size != sizeof(*projection) ||
        projection->abi_version != GATEWAY_CAPABILITY_PROJECTION_ABI_VERSION ||
        projection->generation == 0 ||
        !flags_are_known(projection->effective_capabilities) ||
        !flags_are_known(projection->accepted_capabilities) ||
        !flags_are_known(projection->negotiated_capabilities) ||
        !flags_are_known(projection->operational_capabilities) ||
        projection->health > GATEWAY_CAPABILITY_HEALTH_UNAVAILABLE ||
        projection->consecutive_failures > GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE ||
        projection->consecutive_successes > GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY) {
        return false;
    }
    if (!projection->acceptance_observed) {
        return projection->accepted_capabilities == 0 &&
               projection->negotiated_capabilities == 0 &&
               projection->operational_capabilities == 0 &&
               projection->health == GATEWAY_CAPABILITY_HEALTH_UNKNOWN &&
               projection->consecutive_failures == 0 &&
               projection->consecutive_successes == 0;
    }
    if ((projection->accepted_capabilities & ~projection->effective_capabilities) != 0 ||
        projection->negotiated_capabilities !=
            (projection->effective_capabilities & projection->accepted_capabilities)) {
        return false;
    }
    if (projection->health == GATEWAY_CAPABILITY_HEALTH_HEALTHY ||
        projection->health == GATEWAY_CAPABILITY_HEALTH_DEGRADED) {
        return projection->operational_capabilities == projection->negotiated_capabilities;
    }
    return projection->operational_capabilities == 0;
}

static void reset_acceptance_and_health(
    gateway_capability_projection_t *projection) {
    projection->accepted_capabilities = 0;
    projection->negotiated_capabilities = 0;
    projection->operational_capabilities = 0;
    projection->health = GATEWAY_CAPABILITY_HEALTH_UNKNOWN;
    projection->consecutive_failures = 0;
    projection->consecutive_successes = 0;
    projection->acceptance_observed = false;
}

static void advance_generation(gateway_capability_projection_t *projection) {
    /* Zero is reserved for callers that have not captured a projection. */
    ++projection->generation;
    if (projection->generation == 0) projection->generation = 1;
}

static bool lease_shape_is_valid(const gateway_capability_lease_t *lease) {
    return lease &&
           lease->struct_size == sizeof(*lease) &&
           lease->abi_version == GATEWAY_CAPABILITY_LEASE_ABI_VERSION &&
           lease->generation != 0 &&
           lease->required_capabilities != 0 &&
           flags_are_known(lease->required_capabilities);
}

void gateway_capability_projection_init(gateway_capability_projection_t *projection) {
    if (!projection) return;
    memset(projection, 0, sizeof(*projection));
    projection->struct_size = sizeof(*projection);
    projection->abi_version = GATEWAY_CAPABILITY_PROJECTION_ABI_VERSION;
    projection->generation = 1;
    projection->health = GATEWAY_CAPABILITY_HEALTH_UNKNOWN;
}

bool gateway_capability_projection_set_effective(
    gateway_capability_projection_t *projection,
    gateway_capability_flags_t effective_capabilities) {
    if (!projection || !flags_are_known(effective_capabilities) ||
        (projection->struct_size != sizeof(*projection) ||
         projection->abi_version != GATEWAY_CAPABILITY_PROJECTION_ABI_VERSION)) {
        return false;
    }
    projection->effective_capabilities = effective_capabilities;
    advance_generation(projection);
    reset_acceptance_and_health(projection);
    return true;
}

bool gateway_capability_projection_observe_accepted(
    gateway_capability_projection_t *projection,
    gateway_capability_flags_t accepted_capabilities) {
    if (!projection || !projection_shape_is_valid(projection) ||
        !flags_are_known(accepted_capabilities) ||
        (accepted_capabilities & ~projection->effective_capabilities) != 0) {
        return false;
    }
    /* A repeated successful handshake confirms the existing contract; it
     * must not reset success hysteresis, otherwise a two-success admission
     * threshold can never be reached. */
    if (projection->acceptance_observed &&
        projection->accepted_capabilities == accepted_capabilities) {
        return true;
    }
    projection->accepted_capabilities = accepted_capabilities;
    projection->negotiated_capabilities =
        projection->effective_capabilities & accepted_capabilities;
    projection->operational_capabilities = 0;
    projection->health = GATEWAY_CAPABILITY_HEALTH_UNKNOWN;
    projection->consecutive_failures = 0;
    projection->consecutive_successes = 0;
    projection->acceptance_observed = true;
    advance_generation(projection);
    return true;
}

bool gateway_capability_projection_withdraw_acceptance(
    gateway_capability_projection_t *projection) {
    if (!projection || !projection_shape_is_valid(projection)) return false;
    if (projection->acceptance_observed) advance_generation(projection);
    reset_acceptance_and_health(projection);
    return true;
}

bool gateway_capability_projection_observe_transport_result(
    gateway_capability_projection_t *projection, bool success) {
    if (!projection || !projection_shape_is_valid(projection) ||
        !projection->acceptance_observed) {
        return false;
    }
    if (success) {
        projection->consecutive_failures = 0;
        if (projection->consecutive_successes < GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY) {
            ++projection->consecutive_successes;
        }
        if (projection->consecutive_successes >= GATEWAY_CAPABILITY_SUCCESSES_TO_HEALTHY) {
            projection->health = GATEWAY_CAPABILITY_HEALTH_HEALTHY;
            projection->operational_capabilities = projection->negotiated_capabilities;
        }
        return true;
    }

    projection->consecutive_successes = 0;
    if (projection->consecutive_failures < GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE) {
        ++projection->consecutive_failures;
    }
    if (projection->consecutive_failures >= GATEWAY_CAPABILITY_FAILURES_TO_UNAVAILABLE) {
        /* A health withdrawal is a real contraction of the usable business
         * surface. Advance before clearing it so a worker which captured the
         * former operational set can never resume into a later recovery. */
        if (projection->operational_capabilities != 0) {
            advance_generation(projection);
        }
        projection->health = GATEWAY_CAPABILITY_HEALTH_UNAVAILABLE;
        projection->operational_capabilities = 0;
    } else if (projection->health == GATEWAY_CAPABILITY_HEALTH_HEALTHY ||
               projection->health == GATEWAY_CAPABILITY_HEALTH_DEGRADED) {
        projection->health = GATEWAY_CAPABILITY_HEALTH_DEGRADED;
        /* Keep the last known negotiated surface while retries have not yet
         * crossed the withdrawal threshold. */
        projection->operational_capabilities = projection->negotiated_capabilities;
    }
    return true;
}

bool gateway_capability_projection_snapshot(
    const gateway_capability_projection_t *projection,
    gateway_capability_projection_t *out_snapshot) {
    if (!out_snapshot || !projection_shape_is_valid(projection)) return false;
    *out_snapshot = *projection;
    return true;
}

bool gateway_capability_projection_capture_lease(
    const gateway_capability_projection_t *projection,
    gateway_capability_flags_t required_capabilities,
    gateway_capability_lease_t *out_lease) {
    if (!out_lease || !projection_shape_is_valid(projection) ||
        required_capabilities == 0 || !flags_are_known(required_capabilities) ||
        (projection->operational_capabilities & required_capabilities) !=
            required_capabilities) {
        return false;
    }
    *out_lease = (gateway_capability_lease_t){
        .struct_size = sizeof(*out_lease),
        .abi_version = GATEWAY_CAPABILITY_LEASE_ABI_VERSION,
        .required_capabilities = required_capabilities,
        .generation = projection->generation,
    };
    return true;
}

bool gateway_capability_projection_lease_current(
    const gateway_capability_projection_t *projection,
    const gateway_capability_lease_t *lease) {
    return projection_shape_is_valid(projection) && lease_shape_is_valid(lease) &&
           lease->generation == projection->generation &&
           (projection->operational_capabilities & lease->required_capabilities) ==
               lease->required_capabilities;
}
