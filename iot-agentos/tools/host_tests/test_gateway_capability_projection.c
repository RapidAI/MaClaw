#include <stdio.h>

#include "services/gateway_capability_projection.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    const gateway_capability_flags_t local =
        GATEWAY_CAPABILITY_INPUT_TEXT | GATEWAY_CAPABILITY_INPUT_AUDIO |
        GATEWAY_CAPABILITY_OUTPUT_TEXT | GATEWAY_CAPABILITY_OUTPUT_AUDIO |
        GATEWAY_CAPABILITY_AMBIENT_DISPLAY |
        GATEWAY_CAPABILITY_VOLUME_CONTROL;
    const gateway_capability_flags_t accepted =
        GATEWAY_CAPABILITY_INPUT_TEXT | GATEWAY_CAPABILITY_OUTPUT_TEXT |
        GATEWAY_CAPABILITY_OUTPUT_AUDIO | GATEWAY_CAPABILITY_VOLUME_CONTROL;
    gateway_capability_projection_t projection;
    gateway_capability_projection_t snapshot;
    gateway_capability_lease_t lease;

    gateway_capability_projection_init(&projection);
    CHECK(gateway_capability_projection_set_effective(&projection, local));
    CHECK(!gateway_capability_projection_observe_accepted(
        &projection, local | (1u << 31)));
    CHECK(!projection.acceptance_observed);
    CHECK(gateway_capability_projection_observe_accepted(&projection, accepted));
    CHECK(projection.negotiated_capabilities == accepted);
    CHECK(projection.operational_capabilities == 0);
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_UNKNOWN);

    CHECK(gateway_capability_projection_observe_transport_result(&projection, true));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_UNKNOWN);
    CHECK(projection.operational_capabilities == 0);
    CHECK(gateway_capability_projection_observe_transport_result(&projection, true));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_HEALTHY);
    CHECK(projection.operational_capabilities == accepted);
    CHECK(gateway_capability_projection_capture_lease(
        &projection, GATEWAY_CAPABILITY_OUTPUT_AUDIO, &lease));
    CHECK(gateway_capability_projection_lease_current(&projection, &lease));

    const uint32_t stable_generation = projection.generation;
    CHECK(gateway_capability_projection_observe_accepted(&projection, accepted));
    CHECK(projection.generation == stable_generation);
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_HEALTHY);
    CHECK((projection.operational_capabilities &
           GATEWAY_CAPABILITY_OUTPUT_IMAGE) == 0);

    CHECK(gateway_capability_projection_observe_transport_result(&projection, false));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_DEGRADED);
    CHECK(projection.operational_capabilities == accepted);
    CHECK(gateway_capability_projection_observe_transport_result(&projection, false));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_DEGRADED);
    CHECK(gateway_capability_projection_observe_transport_result(&projection, false));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_UNAVAILABLE);
    CHECK(projection.operational_capabilities == 0);
    CHECK(!gateway_capability_projection_lease_current(&projection, &lease));
    CHECK(!gateway_capability_projection_capture_lease(
        &projection, GATEWAY_CAPABILITY_OUTPUT_AUDIO, &lease));

    CHECK(gateway_capability_projection_observe_transport_result(&projection, true));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_UNAVAILABLE);
    CHECK(gateway_capability_projection_observe_transport_result(&projection, true));
    CHECK(projection.health == GATEWAY_CAPABILITY_HEALTH_HEALTHY);
    CHECK(projection.operational_capabilities == accepted);
    CHECK(!gateway_capability_projection_lease_current(&projection, &lease));
    CHECK(gateway_capability_projection_capture_lease(
        &projection, GATEWAY_CAPABILITY_OUTPUT_AUDIO, &lease));
    CHECK(gateway_capability_projection_lease_current(&projection, &lease));
    CHECK(gateway_capability_projection_snapshot(&projection, &snapshot));
    CHECK(snapshot.operational_capabilities == accepted);

    CHECK(gateway_capability_projection_set_effective(
        &projection, GATEWAY_CAPABILITY_OUTPUT_TEXT));
    CHECK(!projection.acceptance_observed);
    CHECK(projection.negotiated_capabilities == 0);
    CHECK(projection.operational_capabilities == 0);
    CHECK(!gateway_capability_projection_snapshot(NULL, &snapshot));
    CHECK(gateway_capability_projection_observe_accepted(
        &projection, GATEWAY_CAPABILITY_OUTPUT_TEXT));
    const uint32_t before_withdraw = projection.generation;
    CHECK(gateway_capability_projection_withdraw_acceptance(&projection));
    CHECK(!projection.acceptance_observed);
    CHECK(projection.generation != before_withdraw);
    CHECK(projection.operational_capabilities == 0);
    return 0;
}
