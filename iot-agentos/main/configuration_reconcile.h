#pragma once

/*
 * Configuration revision reconciliation value contract.
 *
 * This module deliberately knows neither Configuration Service persistence nor
 * the consumers that eventually perform Audio/Display/Connectivity/Alarm
 * side effects.  It makes the only safe ordering testable: validate a copied
 * candidate, prepare every consumer without publication, publish exactly one
 * immutable revision, then apply consumers in order. Any prepare/publish
 * failure reverses every successful prepare; an apply failure reverses the
 * failing observer too because a false return cannot prove it made no
 * external change. A failed rollback is an unknown external outcome, never a
 * reason to publish another revision.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef enum {
    CONFIGURATION_RECONCILE_OK = 0,
    CONFIGURATION_RECONCILE_INVALID_ARGUMENT,
    CONFIGURATION_RECONCILE_VALIDATE_FAILED,
    CONFIGURATION_RECONCILE_PREPARE_FAILED,
    CONFIGURATION_RECONCILE_PUBLISH_FAILED,
    CONFIGURATION_RECONCILE_APPLY_FAILED,
    CONFIGURATION_RECONCILE_UNKNOWN_OUTCOME,
} configuration_reconcile_result_t;

typedef bool (*configuration_reconcile_validate_t)(const void *candidate,
                                                    size_t candidate_size,
                                                    void *context);
typedef bool (*configuration_reconcile_prepare_t)(uint64_t previous_revision,
                                                   uint64_t candidate_revision,
                                                   const void *candidate,
                                                   size_t candidate_size,
                                                   void *context);
typedef bool (*configuration_reconcile_publish_t)(uint64_t candidate_revision,
                                                   const void *candidate,
                                                   size_t candidate_size,
                                                   void *context);
typedef bool (*configuration_reconcile_apply_t)(uint64_t candidate_revision,
                                                 const void *candidate,
                                                 size_t candidate_size,
                                                 void *context);
typedef bool (*configuration_reconcile_rollback_t)(uint64_t previous_revision,
                                                    void *context);

typedef struct {
    configuration_reconcile_prepare_t prepare;
    configuration_reconcile_apply_t apply;
    configuration_reconcile_rollback_t rollback;
    void *context;
} configuration_reconcile_observer_t;

typedef struct {
    configuration_reconcile_validate_t validate;
    configuration_reconcile_publish_t publish;
    void *context;
} configuration_reconcile_transaction_t;

/* Executes a bounded, synchronous value-level transaction. `candidate` is
 * borrowed only for this call and must be the same immutable value supplied to
 * validate, prepare, publish and apply. The public Configuration owner commits
 * durable data only from its publish callback. Consumers must not publish or
 * mutate configuration from any callback. `rollback` must therefore be safe
 * after a successful prepare before publish, after publish failure, and after
 * a failing apply. */
configuration_reconcile_result_t configuration_reconcile_execute(
    uint64_t previous_revision,
    uint64_t candidate_revision,
    const void *candidate,
    size_t candidate_size,
    const configuration_reconcile_transaction_t *transaction,
    const configuration_reconcile_observer_t *observers,
    size_t observer_count);
