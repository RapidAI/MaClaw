#pragma once

/*
 * Runtime transport-selection transaction.
 *
 * This is a deliberately platform-neutral orchestration contract.  It owns
 * neither Wi-Fi nor cellular drivers, tasks, modem handles, persistence, or
 * a Configuration lock.  Its caller must hold the one lifecycle admission
 * that prevents another selection transaction from running concurrently.
 *
 * A profile/composition adapter supplies bounded operations for the current
 * transport.  The transaction does not publish a selected uplink until the
 * target has proved ready.  Any failure after the old uplink has been
 * quiesced attempts to restore the old uplink.  A failed restore, or a stale
 * lifecycle generation observed after a physical step, is deliberately an
 * UNKNOWN_OUTCOME: callers must not pretend that either link is usable.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* V2 adds the caller-owned remaining-deadline callback.  An older callback
 * layout must be rejected rather than being interpreted as an unbounded
 * transaction by a newer coordinator. */
#define TRANSPORT_SELECTION_TRANSACTION_ABI_VERSION 2u

typedef enum {
    /* The callback completed and its externally visible state is known. */
    TRANSPORT_SELECTION_STEP_OK = 0,
    /* The callback made no externally visible change. */
    TRANSPORT_SELECTION_STEP_REJECTED,
    /* It may have changed a link, but cannot prove its final external state. */
    TRANSPORT_SELECTION_STEP_UNKNOWN,
} transport_selection_step_disposition_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    transport_selection_step_disposition_t disposition;
    device_status_t status;
} transport_selection_step_result_t;

typedef enum {
    TRANSPORT_SELECTION_TRANSACTION_OK = 0,
    TRANSPORT_SELECTION_TRANSACTION_UNCHANGED,
    TRANSPORT_SELECTION_TRANSACTION_INVALID_ARGUMENT,
    TRANSPORT_SELECTION_TRANSACTION_UNAVAILABLE,
    /* The caller-owned absolute deadline was already exhausted before any
     * physical step could be admitted. */
    TRANSPORT_SELECTION_TRANSACTION_DEADLINE_EXPIRED,
    TRANSPORT_SELECTION_TRANSACTION_DRAIN_FAILED,
    TRANSPORT_SELECTION_TRANSACTION_QUIESCE_FAILED,
    TRANSPORT_SELECTION_TRANSACTION_ACTIVATE_FAILED,
    TRANSPORT_SELECTION_TRANSACTION_READINESS_FAILED,
    /* A callback may have touched transport state and the previous link could
     * not be re-established with proof.  No logical selection is committed. */
    TRANSPORT_SELECTION_TRANSACTION_UNKNOWN_OUTCOME,
    /* A lifecycle owner changed while an asynchronous/physical step was in
     * flight.  No commit is made; ownership must resolve the physical state. */
    TRANSPORT_SELECTION_TRANSACTION_STALE_GENERATION,
} transport_selection_transaction_outcome_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    transport_selection_transaction_outcome_t outcome;
    device_status_t status;
    device_uplink_t previous_uplink;
    device_uplink_t requested_uplink;
    bool committed;
} transport_selection_transaction_result_t;

typedef bool (*transport_selection_generation_is_current_t)(
    uint64_t generation, void *context);
/* Returns the remaining time in the caller-owned absolute switch deadline.
 * Zero denies a later physical operation, including rollback. */
typedef uint32_t (*transport_selection_remaining_timeout_t)(void *context);
typedef device_status_t (*transport_selection_support_t)(
    device_uplink_t requested_uplink, void *context);
typedef transport_selection_step_result_t (*transport_selection_step_t)(
    device_uplink_t uplink, uint32_t timeout_ms, void *context);
typedef device_status_t (*transport_selection_commit_t)(
    device_uplink_t selected_uplink, uint64_t generation, void *context);

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    /* Called before and after every physical step and immediately before
     * commit. A false value never authorizes a logical selection publish. */
    transport_selection_generation_is_current_t generation_is_current;
    transport_selection_remaining_timeout_t remaining_timeout_ms;
    /* Returns UNAVAILABLE when the selected profile cannot offer target (for
     * example cellular on Bread Compact/EchoEar/Waveshare). */
    transport_selection_support_t check_supported;
    /* Waits for requests admitted to the old transport before it is stopped. */
    transport_selection_step_t drain_current;
    transport_selection_step_t quiesce_current;
    transport_selection_step_t activate_target;
    transport_selection_step_t wait_target_ready;
    /* Restores and proves readiness of `previous_uplink` after old-link
     * quiesce. It must be idempotent for a partly completed target activation. */
    transport_selection_step_t restore_previous;
    /* Publishes the selected logical uplink only after target readiness and a
     * final generation check. It must not start/stop a physical transport. */
    transport_selection_commit_t commit;
    void *context;
} transport_selection_transaction_callbacks_t;

/* Executes one bounded switch. `timeout_ms` must match the caller's initial
 * absolute budget; every adapter callback instead receives the current
 * remaining budget. All callback values are borrowed for this call only. */
transport_selection_transaction_result_t transport_selection_transaction_execute(
    device_uplink_t previous_uplink,
    device_uplink_t requested_uplink,
    uint64_t generation,
    uint32_t timeout_ms,
    const transport_selection_transaction_callbacks_t *callbacks);
