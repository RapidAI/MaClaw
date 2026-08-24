#pragma once

/*
 * Shared physical-bus recovery transaction.
 *
 * This is the value-only orchestration above shared_bus_lifecycle.  It owns
 * no controller handle, task, clock, mutex, driver, or board identity.  The
 * profile-private resource owner supplies short, bounded callbacks and keeps
 * its own serialization while those callbacks touch real hardware.
 *
 * Recovery deliberately never tries to infer that a failed controller action
 * left the bus usable.  Once recovery has closed lifecycle admission, every
 * deadline expiry, rejected physical cleanup, malformed callback result, or
 * unknown result leaves the lifecycle UNKNOWN_OUTCOME.  A later explicit
 * recovery may begin from that closed state, but business and HAL borrowers
 * cannot obtain a new lease in the meantime.
 */

#include <stdbool.h>
#include <stdint.h>

#include "shared_bus_lifecycle.h"

#define SHARED_BUS_RECOVERY_TRANSACTION_ABI_VERSION 1u

typedef enum {
    SHARED_BUS_RECOVERY_STATUS_OK = 0,
    SHARED_BUS_RECOVERY_STATUS_INVALID_ARGUMENT,
    SHARED_BUS_RECOVERY_STATUS_BUSY,
    SHARED_BUS_RECOVERY_STATUS_UNAVAILABLE,
    SHARED_BUS_RECOVERY_STATUS_TIMEOUT,
    SHARED_BUS_RECOVERY_STATUS_IO_ERROR,
    SHARED_BUS_RECOVERY_STATUS_INTERNAL_ERROR,
} shared_bus_recovery_status_t;

typedef enum {
    /* The callback completed and can prove its resulting physical state. */
    SHARED_BUS_RECOVERY_STEP_OK = 0,
    /* The callback made no externally visible change. */
    SHARED_BUS_RECOVERY_STEP_REJECTED,
    /* The callback may have changed the physical bus but cannot prove state. */
    SHARED_BUS_RECOVERY_STEP_UNKNOWN,
} shared_bus_recovery_step_disposition_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    shared_bus_recovery_step_disposition_t disposition;
    shared_bus_recovery_status_t status;
} shared_bus_recovery_step_result_t;

typedef enum {
    SHARED_BUS_RECOVERY_TRANSACTION_OK = 0,
    SHARED_BUS_RECOVERY_TRANSACTION_INVALID_ARGUMENT,
    SHARED_BUS_RECOVERY_TRANSACTION_UNAVAILABLE,
    SHARED_BUS_RECOVERY_TRANSACTION_DEADLINE_EXPIRED,
    SHARED_BUS_RECOVERY_TRANSACTION_QUIESCE_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_DRAIN_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_CLEANUP_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_RECREATE_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_ATTACH_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_SELF_TEST_FAILED,
    SHARED_BUS_RECOVERY_TRANSACTION_UNKNOWN_OUTCOME,
} shared_bus_recovery_transaction_outcome_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    shared_bus_recovery_transaction_outcome_t outcome;
    shared_bus_recovery_status_t status;
    bool ready;
} shared_bus_recovery_transaction_result_t;

/* The profile owner maps its monotonic absolute deadline to a remaining
 * budget.  Returning zero prevents the next operation; the core itself never
 * reads a timer, so it remains portable and host-testable. */
typedef uint32_t (*shared_bus_recovery_remaining_timeout_t)(void *context);
typedef shared_bus_recovery_step_result_t (*shared_bus_recovery_step_t)(
    uint32_t timeout_ms, void *context);

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    shared_bus_recovery_remaining_timeout_t remaining_timeout_ms;
    /* Parks scanner reads, codec-control admission and active audio session
     * start. It must not itself remove a controller or master-bus handle. */
    shared_bus_recovery_step_t quiesce_consumers;
    /* Returns OK only after all previously issued lifecycle leases have been
     * released. The core independently verifies can_detach afterwards. */
    shared_bus_recovery_step_t wait_for_borrowers;
    /* Strict cleanup order: peripherals -> codec -> master bus. */
    shared_bus_recovery_step_t detach_peripherals;
    shared_bus_recovery_step_t detach_codec;
    shared_bus_recovery_step_t delete_bus;
    /* Strict reconstruction order: master bus -> peripherals -> codec ->
     * physical probe/self-test. No callback may publish user-facing work. */
    shared_bus_recovery_step_t create_bus;
    shared_bus_recovery_step_t attach_peripherals;
    shared_bus_recovery_step_t attach_codec;
    shared_bus_recovery_step_t self_test;
    void *context;
} shared_bus_recovery_transaction_callbacks_t;

/* Executes one synchronous, deadline-bounded recovery attempt.  The caller
 * must serialize this operation with its profile-private handle owner.  The
 * transaction turns all post-fence failures into a closed UNKNOWN_OUTCOME;
 * it never rolls forward a partial recreation or silently reopens admission. */
shared_bus_recovery_transaction_result_t shared_bus_recovery_transaction_execute(
    shared_bus_lifecycle_t *lifecycle,
    const shared_bus_recovery_transaction_callbacks_t *callbacks);
