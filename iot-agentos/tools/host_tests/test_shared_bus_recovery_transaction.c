#include <stdio.h>

#include "shared_bus_recovery_transaction.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

typedef enum {
    STEP_QUIESCE = 0,
    STEP_DRAIN,
    STEP_DETACH_PERIPHERALS,
    STEP_DETACH_CODEC,
    STEP_DELETE_BUS,
    STEP_CREATE_BUS,
    STEP_ATTACH_PERIPHERALS,
    STEP_ATTACH_CODEC,
    STEP_SELF_TEST,
    STEP_COUNT,
} step_id_t;

typedef struct {
    uint32_t remaining_ms;
    int calls[STEP_COUNT];
    shared_bus_recovery_step_result_t results[STEP_COUNT];
    shared_bus_lifecycle_t *lifecycle;
    bool release_borrower_when_drained;
    shared_bus_lease_t borrower;
    int expire_after_step;
} fixture_t;

static shared_bus_recovery_step_result_t step_result(
    shared_bus_recovery_step_disposition_t disposition, shared_bus_recovery_status_t status) {
    return (shared_bus_recovery_step_result_t){
        .struct_size = sizeof(shared_bus_recovery_step_result_t),
        .abi_version = SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION,
        .disposition = disposition,
        .status = status,
    };
}

static void make_ready(shared_bus_lifecycle_t *lifecycle) {
    shared_bus_lifecycle_reset(lifecycle, FAULT_DOMAIN_ID_SHARED_BUS);
    (void)shared_bus_lifecycle_begin_reinitialize(lifecycle);
    (void)shared_bus_lifecycle_mark_attached(lifecycle);
    (void)shared_bus_lifecycle_begin_self_test(lifecycle);
    (void)shared_bus_lifecycle_mark_ready(lifecycle);
}

static void fixture_init(fixture_t *fixture, shared_bus_lifecycle_t *lifecycle) {
    *fixture = (fixture_t){.remaining_ms = 5000u, .lifecycle = lifecycle,
                           .expire_after_step = -1};
    for (unsigned i = 0u; i < STEP_COUNT; ++i) {
        fixture->results[i] = step_result(SHARED_BUS_RECOVERY_STEP_OK,
                                          SHARED_BUS_RECOVERY_STATUS_OK);
    }
}

static uint32_t remaining_timeout(void *context) {
    return ((fixture_t *)context)->remaining_ms;
}

static shared_bus_recovery_step_result_t call_step(step_id_t id, void *context) {
    fixture_t *fixture = context;
    ++fixture->calls[id];
    if (id == STEP_DRAIN && fixture->release_borrower_when_drained) {
        if (shared_bus_lifecycle_release(fixture->lifecycle, &fixture->borrower) !=
            SHARED_BUS_LIFECYCLE_STALE_LEASE) {
            return step_result(SHARED_BUS_RECOVERY_STEP_UNKNOWN,
                               SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
        }
    }
    const shared_bus_recovery_step_result_t result = fixture->results[id];
    if ((int)id == fixture->expire_after_step) fixture->remaining_ms = 0u;
    return result;
}

#define DEFINE_STEP(name, id) \
static shared_bus_recovery_step_result_t name(uint32_t timeout_ms, void *context) { \
    (void)timeout_ms; \
    return call_step(id, context); \
}
DEFINE_STEP(quiesce, STEP_QUIESCE)
DEFINE_STEP(drain, STEP_DRAIN)
DEFINE_STEP(detach_peripherals, STEP_DETACH_PERIPHERALS)
DEFINE_STEP(detach_codec, STEP_DETACH_CODEC)
DEFINE_STEP(delete_bus, STEP_DELETE_BUS)
DEFINE_STEP(create_bus, STEP_CREATE_BUS)
DEFINE_STEP(attach_peripherals, STEP_ATTACH_PERIPHERALS)
DEFINE_STEP(attach_codec, STEP_ATTACH_CODEC)
DEFINE_STEP(self_test, STEP_SELF_TEST)

static shared_bus_recovery_transaction_result_t execute(fixture_t *fixture) {
    const shared_bus_recovery_transaction_callbacks_t callbacks = {
        .struct_size = sizeof(callbacks),
        .abi_version = SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION,
        .remaining_timeout_ms = remaining_timeout,
        .quiesce_consumers = quiesce,
        .wait_for_borrowers = drain,
        .detach_peripherals = detach_peripherals,
        .detach_codec = detach_codec,
        .delete_bus = delete_bus,
        .create_bus = create_bus,
        .attach_peripherals = attach_peripherals,
        .attach_codec = attach_codec,
        .self_test = self_test,
        .context = fixture,
    };
    return shared_bus_recovery_transaction_execute(fixture->lifecycle, &callbacks);
}

int main(void) {
    shared_bus_lifecycle_t lifecycle = {0};
    fixture_t fixture;
    shared_bus_lifecycle_snapshot_t snapshot = {0};

    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    shared_bus_recovery_transaction_result_t result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_OK && result.ready);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_READY && snapshot.domain.admission_open);
    CHECK(fixture.calls[STEP_QUIESCE] == 1 && fixture.calls[STEP_DRAIN] == 1 &&
          fixture.calls[STEP_DETACH_PERIPHERALS] == 1 &&
          fixture.calls[STEP_DETACH_CODEC] == 1 && fixture.calls[STEP_DELETE_BUS] == 1 &&
          fixture.calls[STEP_CREATE_BUS] == 1 && fixture.calls[STEP_ATTACH_PERIPHERALS] == 1 &&
          fixture.calls[STEP_ATTACH_CODEC] == 1 && fixture.calls[STEP_SELF_TEST] == 1);

    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &fixture.borrower) ==
          SHARED_BUS_LIFECYCLE_OK);
    fixture.release_borrower_when_drained = true;
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_OK && result.ready);

    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    fixture.results[STEP_DETACH_CODEC] = step_result(SHARED_BUS_RECOVERY_STEP_REJECTED,
                                                      SHARED_BUS_RECOVERY_STATUS_IO_ERROR);
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_CLEANUP_FAILED && !result.ready);
    CHECK(fixture.calls[STEP_DELETE_BUS] == 0 && fixture.calls[STEP_CREATE_BUS] == 0);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME && !snapshot.domain.admission_open);

    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    fixture.results[STEP_SELF_TEST] = step_result(SHARED_BUS_RECOVERY_STEP_UNKNOWN,
                                                   SHARED_BUS_RECOVERY_STATUS_TIMEOUT);
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME && !result.ready);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME);

    /* A callback cannot report an unknown physical result while retaining OK
     * as its status: the round owner maps status to esp_err_t, so accepting
     * that contradictory payload could surface success for a closed bus. */
    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    fixture.results[STEP_SELF_TEST] = step_result(SHARED_BUS_RECOVERY_STEP_UNKNOWN,
                                                   SHARED_BUS_RECOVERY_STATUS_OK);
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME && !result.ready);
    CHECK(result.status == SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME &&
          !snapshot.domain.admission_open);

    /* A callback that starts within budget but returns after it expires must
     * not reopen the physical bus.  This specifically covers the final
     * self-test, which has no later callback preflight to catch the overrun. */
    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    fixture.expire_after_step = STEP_SELF_TEST;
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED &&
          !result.ready && result.status == SHARED_BUS_RECOVERY_STATUS_TIMEOUT);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME &&
          !snapshot.domain.admission_open);

    make_ready(&lifecycle);
    fixture_init(&fixture, &lifecycle);
    fixture.remaining_ms = 0u;
    result = execute(&fixture);
    CHECK(result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED);
    CHECK(fixture.calls[STEP_QUIESCE] == 0);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_READY && snapshot.domain.admission_open);
    return 0;
}
