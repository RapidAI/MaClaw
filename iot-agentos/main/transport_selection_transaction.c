#include "transport_selection_transaction.h"

static bool valid_uplink(device_uplink_t uplink) {
    return uplink == DEVICE_UPLINK_WIFI || uplink == DEVICE_UPLINK_CELLULAR;
}

static transport_selection_transaction_result_t make_result(
    transport_selection_transaction_outcome_t outcome,
    device_status_t status,
    device_uplink_t previous_uplink,
    device_uplink_t requested_uplink,
    bool committed) {
    return (transport_selection_transaction_result_t){
        .struct_size = sizeof(transport_selection_transaction_result_t),
        .abi_version = TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION,
        .outcome = outcome,
        .status = status,
        .previous_uplink = previous_uplink,
        .requested_uplink = requested_uplink,
        .committed = committed,
    };
}

static bool valid_step_result(transport_selection_step_result_t result) {
    if (result.struct_size != sizeof(result) ||
        result.abi_version != TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION) {
        return false;
    }
    if (result.disposition == TRANSPORT_SELECTION_STEP_OK) {
        return result.status == DEVICE_STATUS_OK;
    }
    /* A rejected or unknown physical operation cannot truthfully carry an OK
     * terminal status.  Treat that contradictory callback ABI as malformed
     * evidence rather than letting a profile accidentally turn uncertainty
     * into an apparent successful rollback/failure classification. */
    return (result.disposition == TRANSPORT_SELECTION_STEP_REJECTED ||
            result.disposition == TRANSPORT_SELECTION_STEP_UNKNOWN) &&
           result.status != DEVICE_STATUS_OK;
}

static uint32_t remaining_timeout_ms(
    const transport_selection_transaction_callbacks_t *callbacks) {
    return callbacks->remaining_timeout_ms(callbacks->context);
}

static transport_selection_transaction_result_t restore_or_unknown(
    device_uplink_t previous_uplink,
    device_uplink_t requested_uplink,
    uint64_t generation,
    uint32_t timeout_ms,
    transport_selection_transaction_outcome_t failed_outcome,
    device_status_t failed_status,
    const transport_selection_transaction_callbacks_t *callbacks) {
    /* Once a lifecycle generation changes, this transaction no longer owns
     * either link. Attempting a late rollback could overwrite the new owner. */
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    (void)timeout_ms;
    const uint32_t restore_timeout_ms = remaining_timeout_ms(callbacks);
    if (restore_timeout_ms == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    const transport_selection_step_result_t restore = callbacks->restore_previous(
        previous_uplink, restore_timeout_ms, callbacks->context);
    if (!valid_step_result(restore) ||
        restore.disposition != TRANSPORT_SELECTION_STEP_OK ||
        !callbacks->generation_is_current(generation, callbacks->context) ||
        remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           !valid_step_result(restore)
                               ? DEVICE_STATUS_INTERNAL_ERROR
                               : (restore.status == DEVICE_STATUS_OK
                                      ? DEVICE_STATUS_TIMEOUT
                                      : restore.status),
                           previous_uplink, requested_uplink, false);
    }
    return make_result(failed_outcome, failed_status, previous_uplink,
                       requested_uplink, false);
}

transport_selection_transaction_result_t transport_selection_transaction_execute(
    device_uplink_t previous_uplink,
    device_uplink_t requested_uplink,
    uint64_t generation,
    uint32_t timeout_ms,
    const transport_selection_transaction_callbacks_t *callbacks) {
    if (!callbacks || callbacks->struct_size != sizeof(*callbacks) ||
        callbacks->abi_version != TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION ||
        !callbacks->generation_is_current || !callbacks->remaining_timeout_ms ||
        !callbacks->check_supported ||
        !callbacks->drain_current || !callbacks->quiesce_current ||
        !callbacks->activate_target || !callbacks->wait_target_ready ||
        !callbacks->restore_previous || !callbacks->commit || generation == 0u ||
        timeout_ms == 0u || !valid_uplink(previous_uplink) ||
        !valid_uplink(requested_uplink)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_INVALID_ARGUMENT,
                           DEVICE_STATUS_INVALID_ARGUMENT, previous_uplink,
                           requested_uplink, false);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_DEADLINE_EXPIRED,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    if (previous_uplink == requested_uplink) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNCHANGED,
                           DEVICE_STATUS_OK, previous_uplink, requested_uplink,
                           false);
    }
    const device_status_t support = callbacks->check_supported(requested_uplink,
                                                                 callbacks->context);
    if (support != DEVICE_STATUS_OK) {
        return make_result(support == DEVICE_STATUS_UNAVAILABLE
                               ? TRANSPORT_SELECTION_TRANSACTION_UNAVAILABLE
                               : TRANSPORT_SELECTION_TRANSACTION_INVALID_ARGUMENT,
                           support, previous_uplink, requested_uplink, false);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }

    uint32_t step_timeout_ms = remaining_timeout_ms(callbacks);
    if (step_timeout_ms == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_DEADLINE_EXPIRED,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    transport_selection_step_result_t step = callbacks->drain_current(
        previous_uplink, step_timeout_ms, callbacks->context);
    if (!valid_step_result(step)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_INTERNAL_ERROR, previous_uplink,
                           requested_uplink, false);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (step.disposition == TRANSPORT_SELECTION_STEP_UNKNOWN) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           step.status, previous_uplink, requested_uplink, false);
    }
    if (step.disposition != TRANSPORT_SELECTION_STEP_OK) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_DRAIN_FAILED,
                           step.status, previous_uplink, requested_uplink, false);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_DRAIN_FAILED,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }

    step_timeout_ms = remaining_timeout_ms(callbacks);
    if (step_timeout_ms == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_DRAIN_FAILED,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    step = callbacks->quiesce_current(previous_uplink, step_timeout_ms,
                                      callbacks->context);
    if (!valid_step_result(step)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_INTERNAL_ERROR, previous_uplink,
                           requested_uplink, false);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (step.disposition != TRANSPORT_SELECTION_STEP_OK) {
        if (step.disposition == TRANSPORT_SELECTION_STEP_REJECTED) {
            return make_result(TRANSPORT_SELECTION_TRANSACTION_QUIESCE_FAILED,
                               step.status, previous_uplink, requested_uplink,
                               false);
        }
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_QUIESCE_FAILED,
                                  step.status, callbacks);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }

    step_timeout_ms = remaining_timeout_ms(callbacks);
    if (step_timeout_ms == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    step = callbacks->activate_target(requested_uplink, step_timeout_ms,
                                      callbacks->context);
    if (!valid_step_result(step)) {
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_ACTIVATE_FAILED,
                                  DEVICE_STATUS_INTERNAL_ERROR, callbacks);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (step.disposition != TRANSPORT_SELECTION_STEP_OK) {
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_ACTIVATE_FAILED,
                                  step.status, callbacks);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }

    step_timeout_ms = remaining_timeout_ms(callbacks);
    if (step_timeout_ms == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    step = callbacks->wait_target_ready(requested_uplink, step_timeout_ms,
                                        callbacks->context);
    if (!valid_step_result(step)) {
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_READINESS_FAILED,
                                  DEVICE_STATUS_INTERNAL_ERROR, callbacks);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (step.disposition != TRANSPORT_SELECTION_STEP_OK) {
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_READINESS_FAILED,
                                  step.status, callbacks);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    const device_status_t committed = callbacks->commit(requested_uplink, generation,
                                                          callbacks->context);
    if (committed != DEVICE_STATUS_OK) {
        return restore_or_unknown(previous_uplink, requested_uplink, generation,
                                  timeout_ms,
                                  TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                                  committed, callbacks);
    }
    /* `commit` is logically side-effectful even though its contract forbids it
     * from starting or stopping a transport.  A lifecycle hand-off immediately
     * after an apparently successful commit means this transaction no longer
     * owns enough state to assert which selection remains published; do not
     * issue a late restore against the new owner. */
    if (!callbacks->generation_is_current(generation, callbacks->context)) {
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_BUSY, previous_uplink, requested_uplink,
                           false);
    }
    if (remaining_timeout_ms(callbacks) == 0u) {
        /* Commit may update the logical selection in an external owner.  Once
         * its deadline has elapsed we cannot prove that a late observer will
         * see this transaction's publish, and must not issue a restore. */
        return make_result(TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
                           DEVICE_STATUS_TIMEOUT, previous_uplink,
                           requested_uplink, false);
    }
    return make_result(TRANSPORT_SELECTION_TRANSACTION_OK, DEVICE_STATUS_OK,
                       previous_uplink, requested_uplink, true);
}
