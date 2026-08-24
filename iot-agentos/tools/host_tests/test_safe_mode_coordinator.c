#include <stdio.h>
#include <string.h>

#include "services/safe_mode_coordinator.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

enum operation {
    OP_QUIESCE_NONESSENTIAL = 0,
    OP_INITIALIZE_CLOCK_FEEDBACK,
    OP_INITIALIZE_ALARM,
    OP_PUBLISH_DIAGNOSTIC,
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
    safe_mode_entry_t published_entry;
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

OPERATION_BRIDGE(quiesce_nonessential, OP_QUIESCE_NONESSENTIAL)
OPERATION_BRIDGE(initialize_clock_feedback, OP_INITIALIZE_CLOCK_FEEDBACK)
OPERATION_BRIDGE(initialize_alarm, OP_INITIALIZE_ALARM)

static device_status_t publish_diagnostic_surface(void *context,
                                                  const safe_mode_entry_t *entry,
                                                  uint32_t timeout_ms) {
    host_state_t *state = context;
    state->published_entry = *entry;
    return invoke(state, OP_PUBLISH_DIAGNOSTIC, timeout_ms);
}

static safe_mode_coordinator_host_t make_host(host_state_t *state) {
    return (safe_mode_coordinator_host_t){
        .now_ms = now_ms,
        .quiesce_nonessential = quiesce_nonessential,
        .initialize_clock_feedback = initialize_clock_feedback,
        .initialize_alarm = initialize_alarm,
        .publish_diagnostic_surface = publish_diagnostic_surface,
        .context = state,
    };
}

static safe_mode_entry_t make_entry(void) {
    return (safe_mode_entry_t){
        .struct_size = sizeof(safe_mode_entry_t),
        .abi_version = SAFE_MODE_COORDINATOR_ABI_VERSION,
        .failed_phase = DEVICE_RUNTIME_PHASE_LOCAL_READY,
        .failure_status = DEVICE_STATUS_INTERNAL_ERROR,
    };
}

int main(void) {
    host_state_t state = { .fail_operation = OP_COUNT, .failure = DEVICE_STATUS_IO_ERROR };
    safe_mode_coordinator_t coordinator;
    safe_mode_coordinator_host_t host = make_host(&state);
    safe_mode_entry_t entry = make_entry();
    CHECK(safe_mode_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
    CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_OK);
    CHECK(state.sequence_count == OP_COUNT);
    for (unsigned index = 0; index < OP_COUNT; ++index) CHECK(state.sequence[index] == index);
    CHECK(state.published_entry.failed_phase == entry.failed_phase);
    CHECK(state.published_entry.failure_status == entry.failure_status);
    safe_mode_stage_t stage;
    device_status_t status;
    CHECK(safe_mode_coordinator_get_snapshot(&coordinator, &stage, &status));
    CHECK(stage == SAFE_MODE_STAGE_COMPLETE && status == DEVICE_STATUS_OK);
    CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_BUSY);

    for (unsigned failed = 0; failed < OP_COUNT; ++failed) {
        memset(&state, 0, sizeof(state));
        state.fail_operation = failed;
        state.failure = DEVICE_STATUS_IO_ERROR;
        host = make_host(&state);
        CHECK(safe_mode_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
        CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_IO_ERROR);
        CHECK(state.sequence_count == failed + 1u);
        for (unsigned index = 0; index <= failed; ++index) CHECK(state.sequence[index] == index);
        CHECK(safe_mode_coordinator_get_snapshot(&coordinator, &stage, &status));
        CHECK(stage == SAFE_MODE_STAGE_FAILED && status == DEVICE_STATUS_IO_ERROR);
        CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_BUSY);
    }

    memset(&state, 0, sizeof(state));
    state.fail_operation = OP_COUNT;
    state.elapsed_per_call_ms = 40;
    host = make_host(&state);
    CHECK(safe_mode_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
    CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_TIMEOUT);
    CHECK(state.sequence_count == 3u);
    CHECK(state.sequence[2] == OP_INITIALIZE_ALARM);
    CHECK(safe_mode_coordinator_get_snapshot(&coordinator, &stage, &status));
    CHECK(stage == SAFE_MODE_STAGE_FAILED && status == DEVICE_STATUS_TIMEOUT);

    entry.failure_status = DEVICE_STATUS_OK;
    CHECK(safe_mode_coordinator_init(&coordinator, &host) == DEVICE_STATUS_OK);
    CHECK(safe_mode_coordinator_enter(&coordinator, &entry, 100) == DEVICE_STATUS_INVALID_ARGUMENT);
    puts("PASS SAFE_MODE coordinator preserves minimum-service ordering, deadline and terminal closure");
    return 0;
}
