#include <stdio.h>
#include <string.h>

#include "services/connectivity_restart_coordinator.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

enum operation {
    OP_QUIESCE_NETWORK_DEPENDENTS = 0,
    OP_STOP_PROVISIONING,
    OP_STOP_PHYSICAL_ROOT,
    OP_INITIALIZE_LOGICAL,
    OP_INITIALIZE_PHYSICAL,
    OP_START_UPLINK,
    OP_START_CLOCK,
    OP_REARM_GATEWAY,
    OP_COUNT,
};

typedef struct {
    uint64_t now_ms;
    unsigned calls[OP_COUNT];
    enum operation sequence[OP_COUNT];
    unsigned sequence_count;
    enum operation fail_operation;
    device_status_t failure;
    uint32_t last_timeout_ms;
    uint32_t elapsed_per_call_ms;
} host_state_t;

static uint64_t now_ms(void *context) {
    return ((host_state_t *)context)->now_ms;
}

static device_status_t invoke(host_state_t *state, enum operation operation,
                              uint32_t timeout_ms) {
    state->calls[operation]++;
    state->sequence[state->sequence_count++] = operation;
    state->last_timeout_ms = timeout_ms;
    state->now_ms += state->elapsed_per_call_ms;
    return state->fail_operation == operation ? state->failure : DEVICE_STATUS_OK;
}

#define OPERATION_BRIDGE(name, operation) \
    static device_status_t name(void *context, uint32_t timeout_ms) { \
        return invoke((host_state_t *)context, operation, timeout_ms); \
    }

OPERATION_BRIDGE(quiesce_network_dependents, OP_QUIESCE_NETWORK_DEPENDENTS)
OPERATION_BRIDGE(stop_provisioning, OP_STOP_PROVISIONING)
OPERATION_BRIDGE(stop_physical_root, OP_STOP_PHYSICAL_ROOT)
OPERATION_BRIDGE(initialize_logical, OP_INITIALIZE_LOGICAL)
OPERATION_BRIDGE(initialize_physical, OP_INITIALIZE_PHYSICAL)
OPERATION_BRIDGE(start_uplink, OP_START_UPLINK)
OPERATION_BRIDGE(start_clock, OP_START_CLOCK)
OPERATION_BRIDGE(rearm_gateway, OP_REARM_GATEWAY)

static connectivity_restart_coordinator_host_t make_host(host_state_t *state) {
    return (connectivity_restart_coordinator_host_t){
        .now_ms = now_ms,
        .quiesce_network_dependents = quiesce_network_dependents,
        .stop_provisioning = stop_provisioning,
        .stop_physical_root = stop_physical_root,
        .initialize_logical_connectivity = initialize_logical,
        .initialize_physical_root = initialize_physical,
        .start_selected_uplink = start_uplink,
        .start_clock_sync = start_clock,
        .rearm_gateway = rearm_gateway,
        .context = state,
    };
}

int main(void) {
    host_state_t state = { .fail_operation = OP_COUNT, .failure = DEVICE_STATUS_INTERNAL_ERROR };
    connectivity_restart_coordinator_t coordinator;
    connectivity_restart_coordinator_host_t host = make_host(&state);
    CHECK(connectivity_restart_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
    CHECK(connectivity_restart_coordinator_restart(&coordinator, 100) == DEVICE_STATUS_OK);
    CHECK(state.sequence_count == OP_COUNT);
    for (unsigned index = 0; index < OP_COUNT; ++index) CHECK(state.sequence[index] == index);
    connectivity_restart_stage_t stage;
    device_status_t status;
    bool physical_stop_committed;
    CHECK(connectivity_restart_coordinator_get_snapshot(&coordinator, &stage, &status,
                                                        &physical_stop_committed));
    CHECK(stage == CONNECTIVITY_RESTART_STAGE_COMPLETE && status == DEVICE_STATUS_OK);
    CHECK(physical_stop_committed);
    CHECK(connectivity_restart_coordinator_restart(&coordinator, 100) == DEVICE_STATUS_BUSY);

    for (unsigned failed = 0; failed < OP_COUNT; ++failed) {
        memset(&state, 0, sizeof(state));
        state.fail_operation = failed;
        state.failure = DEVICE_STATUS_IO_ERROR;
        host = make_host(&state);
        CHECK(connectivity_restart_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
        CHECK(connectivity_restart_coordinator_restart(&coordinator, 100) == DEVICE_STATUS_IO_ERROR);
        CHECK(state.sequence_count == failed + 1u);
        for (unsigned index = 0; index <= failed; ++index) CHECK(state.sequence[index] == index);
        CHECK(connectivity_restart_coordinator_get_snapshot(&coordinator, &stage, &status,
                                                            &physical_stop_committed));
        CHECK(stage == CONNECTIVITY_RESTART_STAGE_FAILED && status == DEVICE_STATUS_IO_ERROR);
        CHECK(physical_stop_committed == (failed >= OP_STOP_PHYSICAL_ROOT));
        CHECK(connectivity_restart_coordinator_restart(&coordinator, 100) == DEVICE_STATUS_BUSY);
    }

    memset(&state, 0, sizeof(state));
    state.fail_operation = OP_COUNT;
    state.elapsed_per_call_ms = 40;
    host = make_host(&state);
    CHECK(connectivity_restart_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
    CHECK(connectivity_restart_coordinator_restart(&coordinator, 100) == DEVICE_STATUS_TIMEOUT);
    CHECK(state.sequence_count == 3u);
    CHECK(state.sequence[2] == OP_STOP_PHYSICAL_ROOT);
    CHECK(connectivity_restart_coordinator_get_snapshot(&coordinator, &stage, &status,
                                                        &physical_stop_committed));
    CHECK(stage == CONNECTIVITY_RESTART_STAGE_FAILED && status == DEVICE_STATUS_TIMEOUT);
    CHECK(physical_stop_committed);
    puts("PASS Connectivity restart coordinator preserves ordering, deadline and fail-closed generations");
    return 0;
}
