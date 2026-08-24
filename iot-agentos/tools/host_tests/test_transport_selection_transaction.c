#include <stdio.h>

#include "transport_selection_transaction.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

typedef enum {
    STEP_DRAIN = 0,
    STEP_QUIESCE,
    STEP_ACTIVATE,
    STEP_READY,
    STEP_RESTORE,
    STEP_COUNT,
} step_id_t;

typedef struct {
    uint64_t current_generation;
    bool cellular_supported;
    bool stale_after_activate;
    bool stale_after_commit;
    bool expire_after_commit;
    int expire_after_step;
    int calls[STEP_COUNT];
    int commits;
    device_uplink_t committed_uplink;
    transport_selection_step_result_t steps[STEP_COUNT];
} fixture_t;

static transport_selection_step_result_t step_result(
    transport_selection_step_disposition_t disposition, device_status_t status) {
    return (transport_selection_step_result_t){
        .struct_size = sizeof(transport_selection_step_result_t),
        .abi_version = TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION,
        .disposition = disposition,
        .status = status,
    };
}

static void fixture_init(fixture_t *fixture) {
    *fixture = (fixture_t){
        .current_generation = 9u,
        .cellular_supported = true,
        .committed_uplink = DEVICE_UPLINK_WIFI,
        .expire_after_step = -1,
    };
    for (unsigned i = 0u; i < STEP_COUNT; ++i) {
        fixture->steps[i] = step_result(TRANSPORT_SELECTION_STEP_OK, DEVICE_STATUS_OK);
    }
}

static bool generation_is_current(uint64_t generation, void *context) {
    return generation == ((fixture_t *)context)->current_generation;
}

static uint32_t remaining_timeout_ms(void *context) {
    const fixture_t *fixture = context;
    return fixture->expire_after_step == -2 ? 0u : 5000u;
}

static device_status_t check_supported(device_uplink_t requested, void *context) {
    const fixture_t *fixture = context;
    return requested == DEVICE_UPLINK_CELLULAR && !fixture->cellular_supported
               ? DEVICE_STATUS_UNAVAILABLE
               : DEVICE_STATUS_OK;
}

static transport_selection_step_result_t call_step(step_id_t id, void *context) {
    fixture_t *fixture = context;
    ++fixture->calls[id];
    if ((int)id == fixture->expire_after_step) fixture->expire_after_step = -2;
    return fixture->steps[id];
}

static transport_selection_step_result_t drain(device_uplink_t uplink,
                                                uint32_t timeout_ms, void *context) {
    (void)uplink;
    (void)timeout_ms;
    return call_step(STEP_DRAIN, context);
}

static transport_selection_step_result_t quiesce(device_uplink_t uplink,
                                                  uint32_t timeout_ms, void *context) {
    (void)uplink;
    (void)timeout_ms;
    return call_step(STEP_QUIESCE, context);
}

static transport_selection_step_result_t activate(device_uplink_t uplink,
                                                   uint32_t timeout_ms, void *context) {
    (void)uplink;
    (void)timeout_ms;
    fixture_t *fixture = context;
    transport_selection_step_result_t result = call_step(STEP_ACTIVATE, context);
    if (fixture->stale_after_activate) ++fixture->current_generation;
    return result;
}

static transport_selection_step_result_t ready(device_uplink_t uplink,
                                                uint32_t timeout_ms, void *context) {
    (void)uplink;
    (void)timeout_ms;
    return call_step(STEP_READY, context);
}

static transport_selection_step_result_t restore(device_uplink_t uplink,
                                                  uint32_t timeout_ms, void *context) {
    (void)uplink;
    (void)timeout_ms;
    return call_step(STEP_RESTORE, context);
}

static device_status_t commit(device_uplink_t uplink, uint64_t generation,
                              void *context) {
    fixture_t *fixture = context;
    if (generation != fixture->current_generation) return DEVICE_STATUS_BUSY;
    ++fixture->commits;
    fixture->committed_uplink = uplink;
    if (fixture->stale_after_commit) ++fixture->current_generation;
    if (fixture->expire_after_commit) fixture->expire_after_step = -2;
    return DEVICE_STATUS_OK;
}

static transport_selection_transaction_result_t execute(fixture_t *fixture,
                                                          device_uplink_t previous,
                                                          device_uplink_t requested) {
    const transport_selection_transaction_callbacks_t callbacks = {
        .struct_size = sizeof(callbacks),
        .abi_version = TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION,
        .generation_is_current = generation_is_current,
        .remaining_timeout_ms = remaining_timeout_ms,
        .check_supported = check_supported,
        .drain_current = drain,
        .quiesce_current = quiesce,
        .activate_target = activate,
        .wait_target_ready = ready,
        .restore_previous = restore,
        .commit = commit,
        .context = fixture,
    };
    return transport_selection_transaction_execute(previous, requested,
                                                   fixture->current_generation,
                                                   5000u, &callbacks);
}

int main(void) {
    fixture_t fixture;

    fixture_init(&fixture);
    transport_selection_transaction_result_t result = execute(
        &fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_WIFI);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNCHANGED);
    CHECK(result.status == DEVICE_STATUS_OK && !result.committed);
    CHECK(fixture.calls[STEP_DRAIN] == 0 && fixture.commits == 0);

    fixture_init(&fixture);
    fixture.steps[STEP_DRAIN] = step_result(TRANSPORT_SELECTION_STEP_REJECTED,
                                             DEVICE_STATUS_TIMEOUT);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_DRAIN_FAILED);
    CHECK(result.status == DEVICE_STATUS_TIMEOUT && !result.committed);
    CHECK(fixture.calls[STEP_QUIESCE] == 0 && fixture.calls[STEP_RESTORE] == 0);

    fixture_init(&fixture);
    fixture.steps[STEP_QUIESCE] = step_result(TRANSPORT_SELECTION_STEP_REJECTED,
                                               DEVICE_STATUS_IO_ERROR);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_QUIESCE_FAILED);
    CHECK(result.status == DEVICE_STATUS_IO_ERROR && fixture.calls[STEP_RESTORE] == 0);

    fixture_init(&fixture);
    fixture.steps[STEP_READY] = step_result(TRANSPORT_SELECTION_STEP_REJECTED,
                                            DEVICE_STATUS_TIMEOUT);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_READINESS_FAILED);
    CHECK(result.status == DEVICE_STATUS_TIMEOUT && !result.committed);
    CHECK(fixture.calls[STEP_RESTORE] == 1 && fixture.commits == 0);

    fixture_init(&fixture);
    fixture.steps[STEP_READY] = step_result(TRANSPORT_SELECTION_STEP_REJECTED,
                                            DEVICE_STATUS_TIMEOUT);
    fixture.steps[STEP_RESTORE] = step_result(TRANSPORT_SELECTION_STEP_UNKNOWN,
                                              DEVICE_STATUS_IO_ERROR);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME);
    CHECK(result.status == DEVICE_STATUS_IO_ERROR && !result.committed);

    fixture_init(&fixture);
    fixture.steps[STEP_READY] = step_result(TRANSPORT_SELECTION_STEP_UNKNOWN,
                                            DEVICE_STATUS_OK);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_READINESS_FAILED);
    CHECK(result.status == DEVICE_STATUS_INTERNAL_ERROR && !result.committed);
    CHECK(fixture.calls[STEP_RESTORE] == 1 && fixture.commits == 0);

    fixture_init(&fixture);
    fixture.stale_after_activate = true;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION);
    CHECK(result.status == DEVICE_STATUS_BUSY && !result.committed);
    CHECK(fixture.calls[STEP_READY] == 0 && fixture.calls[STEP_RESTORE] == 0);

    fixture_init(&fixture);
    fixture.stale_after_commit = true;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME);
    CHECK(result.status == DEVICE_STATUS_BUSY && !result.committed);
    CHECK(fixture.commits == 1 && fixture.calls[STEP_RESTORE] == 0);

    fixture_init(&fixture);
    fixture.expire_after_commit = true;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME);
    CHECK(result.status == DEVICE_STATUS_TIMEOUT && !result.committed);
    CHECK(fixture.commits == 1 && fixture.calls[STEP_RESTORE] == 0);

    fixture_init(&fixture);
    fixture.expire_after_step = STEP_READY;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME);
    CHECK(result.status == DEVICE_STATUS_TIMEOUT && !result.committed);
    CHECK(fixture.calls[STEP_RESTORE] == 0 && fixture.commits == 0);

    fixture_init(&fixture);
    fixture.expire_after_step = -2;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_DEADLINE_EXPIRED);
    CHECK(result.status == DEVICE_STATUS_TIMEOUT && !result.committed);
    CHECK(fixture.calls[STEP_DRAIN] == 0 && fixture.commits == 0);

    fixture_init(&fixture);
    fixture.cellular_supported = false;
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_UNAVAILABLE);
    CHECK(result.status == DEVICE_STATUS_UNAVAILABLE && fixture.calls[STEP_DRAIN] == 0);

    fixture_init(&fixture);
    result = execute(&fixture, DEVICE_UPLINK_WIFI, DEVICE_UPLINK_CELLULAR);
    CHECK(result.outcome == TRANSPORT_SELECTION_TRANSACTION_OK && result.committed);
    CHECK(fixture.commits == 1 && fixture.committed_uplink == DEVICE_UPLINK_CELLULAR);
    CHECK(fixture.calls[STEP_DRAIN] == 1 && fixture.calls[STEP_QUIESCE] == 1 &&
          fixture.calls[STEP_ACTIVATE] == 1 && fixture.calls[STEP_READY] == 1 &&
          fixture.calls[STEP_RESTORE] == 0);
    return 0;
}
