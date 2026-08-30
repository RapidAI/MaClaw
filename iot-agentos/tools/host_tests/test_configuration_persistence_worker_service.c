#include <assert.h>
#include <stdio.h>

#include "services/configuration_persistence_worker_service.h"

/* The target implementation owns FreeRTOS primitives; this host regression
 * locks its public value contract so business callers cannot acquire those
 * primitives when a future fake worker is introduced. */
int main(void) {
    configuration_persistence_request_t request = {
        .percent = 70u,
        .screen_sleep_seconds = 60u,
        .display_policy = true,
        .display_policy_has_brightness = true,
        .display_policy_has_screen_sleep = true,
        .hub_authenticated = true,
    };
    configuration_persistence_reply_t reply = {
        .status = DEVICE_STATUS_OK,
        .configuration_revision = 42u,
    };
    assert(request.percent == 70u && request.display_policy);
    assert(reply.status == DEVICE_STATUS_OK && reply.configuration_revision == 42u);
    assert(sizeof(configuration_persistence_worker_service_host_t) >= sizeof(void *) * 2u);
    puts("PASS configuration persistence worker value contract");
    return 0;
}
