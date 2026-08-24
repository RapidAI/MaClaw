#include "shared_bus_recovery_transaction.h"

static shared_bus_recovery_transaction_result_t make_result(
    shared_bus_recovery_transaction_outcome_t outcome,
    shared_bus_recovery_status_t status,
    bool ready) {
    return (shared_bus_recovery_transaction_result_t){
        .struct_size = sizeof(shared_bus_recovery_transaction_result_t),
        .abi_version = SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION,
        .outcome = outcome,
        .status = status,
        .ready = ready,
    };
}

static bool callbacks_valid(const shared_bus_recovery_transaction_callbacks_t *callbacks) {
    return callbacks && callbacks->struct_size == sizeof(*callbacks) &&
           callbacks->abi_version == SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION &&
           callbacks->remaining_timeout_ms && callbacks->quiesce_consumers &&
           callbacks->wait_for_borrowers && callbacks->detach_peripherals &&
           callbacks->detach_codec && callbacks->delete_bus && callbacks->create_bus &&
           callbacks->attach_peripherals && callbacks->attach_codec && callbacks->self_test;
}

static bool step_result_valid(shared_bus_recovery_step_result_t result) {
    if (result.struct_size != sizeof(result) ||
        result.abi_version != SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION ||
        result.status > SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR) {
        return false;
    }
    if (result.disposition == SHARED_BUS_RECOVERY_STEP_OK) {
        return result.status == SHARED_BUS_RECOVERY_STATUS_OK;
    }
    /* A non-OK disposition is failure evidence.  Allowing it to retain an OK
     * status lets a profile callback say "the physical result is rejected or
     * unknown" while the round owner maps the completed transaction back to
     * ESP_OK.  Reject that contradictory ABI payload at the value boundary;
     * malformed external evidence always becomes UNKNOWN_OUTCOME. */
    if (result.status == SHARED_BUS_RECOVERY_STATUS_OK) return false;
    return result.disposition == SHARED_BUS_RECOVERY_STEP_REJECTED ||
           result.disposition == SHARED_BUS_RECOVERY_STEP_UNKNOWN;
}

static shared_bus_recovery_transaction_result_t fail_closed(
    shared_bus_lifecycle_t *lifecycle,
    shared_bus_recovery_transaction_outcome_t outcome,
    shared_bus_recovery_status_t status) {
    (void)shared_bus_lifecycle_mark_unknown(lifecycle);
    return make_result(outcome, status, false);
}

static shared_bus_recovery_transaction_result_t run_step(
    shared_bus_lifecycle_t *lifecycle,
    const shared_bus_recovery_transaction_callbacks_t *callbacks,
    shared_bus_recovery_step_t step,
    shared_bus_recovery_transaction_outcome_t rejected_outcome) {
    const uint32_t timeout_ms = callbacks->remaining_timeout_ms(callbacks->context);
    if (timeout_ms == 0u) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED,
                           SHARED_BUS_RECOVERY_STATUS_TIMEOUT);
    }
    const shared_bus_recovery_step_result_t result = step(timeout_ms, callbacks->context);
    if (!step_result_valid(result)) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }
    if (result.disposition == SHARED_BUS_RECOVERY_STEP_OK) {
        /* The previous preflight only proves that this physical callback was
         * admitted with time remaining.  It does not prove the callback
         * returned before the caller's absolute deadline.  In particular the
         * final self-test has no subsequent step whose preflight could catch
         * an overrun, so accepting it here would reopen a bus after its
         * recovery budget had expired.  A completed-but-late action has an
         * externally uncertain result: retain the closed lifecycle rather
         * than publishing READY. */
        if (callbacks->remaining_timeout_ms(callbacks->context) == 0u) {
            return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED,
                               SHARED_BUS_RECOVERY_STATUS_TIMEOUT);
        }
        return make_result(SHARED_BUS_RECOVERY_TRANSACTION_OK,
                           SHARED_BUS_RECOVERY_STATUS_OK, true);
    }
    return fail_closed(lifecycle,
                       result.disposition == SHARED_BUS_RECOVERY_STEP_UNKNOWN
                           ? SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME
                           : rejected_outcome,
                       result.status);
}

static bool step_succeeded(shared_bus_recovery_transaction_result_t result) {
    return result.outcome == SHARED_BUS_RECOVERY_TRANSACTION_OK && result.ready;
}

shared_bus_recovery_transaction_result_t shared_bus_recovery_transaction_execute(
    shared_bus_lifecycle_t *lifecycle,
    const shared_bus_recovery_transaction_callbacks_t *callbacks) {
    if (!lifecycle || !callbacks_valid(callbacks)) {
        return make_result(SHARED_BUS_RECOVERY_TRANSACTION_INVALID_ARGUMENT,
                           SHARED_BUS_RECOVERY_STATUS_INVALID_ARGUMENT, false);
    }
    if (callbacks->remaining_timeout_ms(callbacks->context) == 0u) {
        return make_result(SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED,
                           SHARED_BUS_RECOVERY_STATUS_TIMEOUT, false);
    }
    if (shared_bus_lifecycle_begin_recovery(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return make_result(SHARED_BUS_RECOVERY_TRANSACTION_UNAVAILABLE,
                           SHARED_BUS_RECOVERY_STATUS_UNAVAILABLE, false);
    }

    shared_bus_recovery_transaction_result_t result = run_step(
        lifecycle, callbacks, callbacks->quiesce_consumers,
        SHARED_BUS_RECOVERY_TRANSACTION_QUIESCE_FAILED);
    if (!step_succeeded(result)) return result;
    result = run_step(lifecycle, callbacks, callbacks->wait_for_borrowers,
                      SHARED_BUS_RECOVERY_TRANSACTION_DRAIN_FAILED);
    if (!step_succeeded(result)) return result;
    if (!shared_bus_lifecycle_can_detach(lifecycle)) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_DRAIN_FAILED,
                           SHARED_BUS_RECOVERY_STATUS_BUSY);
    }

    result = run_step(lifecycle, callbacks, callbacks->detach_peripherals,
                      SHARED_BUS_RECOVERY_TRANSACTION_CLEANUP_FAILED);
    if (!step_succeeded(result)) return result;
    result = run_step(lifecycle, callbacks, callbacks->detach_codec,
                      SHARED_BUS_RECOVERY_TRANSACTION_CLEANUP_FAILED);
    if (!step_succeeded(result)) return result;
    result = run_step(lifecycle, callbacks, callbacks->delete_bus,
                      SHARED_BUS_RECOVERY_TRANSACTION_CLEANUP_FAILED);
    if (!step_succeeded(result)) return result;
    if (shared_bus_lifecycle_mark_detached(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }
    if (shared_bus_lifecycle_begin_reinitialize(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }

    result = run_step(lifecycle, callbacks, callbacks->create_bus,
                      SHARED_BUS_RECOVERY_TRANSACTION_RECREATE_FAILED);
    if (!step_succeeded(result)) return result;
    if (shared_bus_lifecycle_mark_attached(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }
    result = run_step(lifecycle, callbacks, callbacks->attach_peripherals,
                      SHARED_BUS_RECOVERY_TRANSACTION_ATTACH_FAILED);
    if (!step_succeeded(result)) return result;
    if (shared_bus_lifecycle_begin_self_test(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }
    result = run_step(lifecycle, callbacks, callbacks->attach_codec,
                      SHARED_BUS_RECOVERY_TRANSACTION_ATTACH_FAILED);
    if (!step_succeeded(result)) return result;
    result = run_step(lifecycle, callbacks, callbacks->self_test,
                      SHARED_BUS_RECOVERY_TRANSACTION_SELF_TEST_FAILED);
    if (!step_succeeded(result)) return result;
    if (shared_bus_lifecycle_mark_ready(lifecycle) != SHARED_BUS_LIFECYCLE_OK) {
        return fail_closed(lifecycle, SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
                           SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR);
    }
    return make_result(SHARED_BUS_RECOVERY_TRANSACTION_OK,
                       SHARED_BUS_RECOVERY_STATUS_OK, true);
}
