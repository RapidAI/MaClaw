#include <stdio.h>
#include <stdlib.h>
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
    uint32_t elapsed_per_call_ms;
} host_state_t;

static uint64_t now_ms(void *context) { return ((host_state_t *)context)->now_ms; }

static device_status_t invoke(host_state_t *state, enum operation operation) {
    state->calls[operation]++;
    state->sequence[state->sequence_count++] = operation;
    state->now_ms += state->elapsed_per_call_ms;
    return state->fail_operation == operation ? state->failure : DEVICE_STATUS_OK;
}

#define OPERATION_BRIDGE(name, operation) \
    static device_status_t name(void *context, uint32_t timeout_ms) { \
        (void)timeout_ms; \
        return invoke((host_state_t *)context, operation); \
    }

OPERATION_BRIDGE(quiesce_nonessential, OP_QUIESCE_NONESSENTIAL)
OPERATION_BRIDGE(initialize_clock_feedback, OP_INITIALIZE_CLOCK_FEEDBACK)
OPERATION_BRIDGE(initialize_alarm, OP_INITIALIZE_ALARM)

static device_status_t publish_diagnostic_surface(void *context,
                                                  const safe_mode_entry_t *entry,
                                                  uint32_t timeout_ms) {
    (void)entry;
    (void)timeout_ms;
    return invoke((host_state_t *)context, OP_PUBLISH_DIAGNOSTIC);
}

int main(int argc, char **argv) {
    unsigned failure_index = OP_COUNT;
    uint32_t elapsed_ms = 0u;
    if (argc > 1) failure_index = (unsigned)strtoul(argv[1], NULL, 10);
    if (argc > 2) elapsed_ms = (uint32_t)strtoul(argv[2], NULL, 10);
    host_state_t state = {
        .fail_operation = failure_index < OP_COUNT ? (enum operation)failure_index : OP_COUNT,
        .failure = DEVICE_STATUS_IO_ERROR,
        .elapsed_per_call_ms = elapsed_ms,
    };
    const safe_mode_coordinator_host_t host = {
        .now_ms = now_ms,
        .quiesce_nonessential = quiesce_nonessential,
        .initialize_clock_feedback = initialize_clock_feedback,
        .initialize_alarm = initialize_alarm,
        .publish_diagnostic_surface = publish_diagnostic_surface,
        .context = &state,
    };
    const safe_mode_entry_t entry = {
        .struct_size = sizeof(entry),
        .abi_version = SAFE_MODE_COORDINATOR_ABI_VERSION,
        .failed_phase = DEVICE_RUNTIME_PHASE_LOCAL_READY,
        .failure_status = DEVICE_STATUS_INTERNAL_ERROR,
    };
    CHECK(safe_mode_coordinator_configure_host(&host) == DEVICE_STATUS_OK);
    const device_status_t result = safe_mode_coordinator_enter(&entry, 100u);
    const unsigned expected_count = failure_index < OP_COUNT ? failure_index + 1u
                                                              : (elapsed_ms ? 3u : OP_COUNT);
    CHECK(result == (failure_index < OP_COUNT ? DEVICE_STATUS_IO_ERROR
                                               : (elapsed_ms ? DEVICE_STATUS_TIMEOUT : DEVICE_STATUS_OK)));
    CHECK(state.sequence_count == expected_count);
    for (unsigned index = 0; index < expected_count; ++index) CHECK(state.sequence[index] == index);
    safe_mode_stage_t stage;
    device_status_t terminal;
    CHECK(safe_mode_coordinator_get_snapshot(&stage, &terminal));
    CHECK(stage == (result == DEVICE_STATUS_OK ? SAFE_MODE_STAGE_COMPLETE : SAFE_MODE_STAGE_FAILED));
    CHECK(terminal == result);
    CHECK(safe_mode_coordinator_enter(&entry, 100u) == DEVICE_STATUS_BUSY);
    puts("PASS SAFE_MODE coordinator failure closure");
    return 0;
}
