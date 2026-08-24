#include "configuration_reconcile.h"

static bool rollback_observers(uint64_t previous_revision,
                               const configuration_reconcile_observer_t *observers,
                               size_t count) {
    bool rollback_known = true;
    while (count != 0u) {
        --count;
        if (!observers[count].rollback(previous_revision, observers[count].context)) {
            rollback_known = false;
        }
    }
    return rollback_known;
}

configuration_reconcile_result_t configuration_reconcile_execute(
    uint64_t previous_revision,
    uint64_t candidate_revision,
    const void *candidate,
    size_t candidate_size,
    const configuration_reconcile_transaction_t *transaction,
    const configuration_reconcile_observer_t *observers,
    size_t observer_count) {
    if (!candidate || candidate_size == 0u || !transaction ||
        !transaction->validate || !transaction->publish ||
        candidate_revision == 0u || candidate_revision <= previous_revision ||
        (observer_count != 0u && !observers)) {
        return CONFIGURATION_RECONCILE_INVALID_ARGUMENT;
    }
    if (!transaction->validate(candidate, candidate_size, transaction->context)) {
        return CONFIGURATION_RECONCILE_VALIDATE_FAILED;
    }
    for (size_t i = 0; i < observer_count; ++i) {
        if (!observers[i].prepare || !observers[i].apply || !observers[i].rollback) {
            return CONFIGURATION_RECONCILE_INVALID_ARGUMENT;
        }
    }
    size_t prepared_count = 0u;
    for (; prepared_count < observer_count; ++prepared_count) {
        const configuration_reconcile_observer_t *observer = &observers[prepared_count];
        if (!observer->prepare(previous_revision, candidate_revision,
                               candidate, candidate_size, observer->context)) {
            return rollback_observers(previous_revision, observers, prepared_count)
                       ? CONFIGURATION_RECONCILE_PREPARE_FAILED
                       : CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME;
        }
    }
    if (!transaction->publish(candidate_revision, candidate, candidate_size,
                              transaction->context)) {
        return rollback_observers(previous_revision, observers, prepared_count)
                   ? CONFIGURATION_RECONCILE_PUBLISH_FAILED
                   : CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME;
    }
    for (size_t i = 0; i < observer_count; ++i) {
        if (observers[i].apply(candidate_revision, candidate, candidate_size,
                               observers[i].context)) {
            continue;
        }
        /* Roll back the failed observer as well: `apply=false` is not proof
         * that a codec, panel or asynchronous worker was untouched. */
        const bool rollback_known = rollback_observers(previous_revision, observers, i + 1u);
        return rollback_known ? CONFIGURATION_RECONCILE_APPLY_FAILED
                              : CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME;
    }
    return CONFIGURATION_RECONCILE_OK;
}
