#pragma once

/*
 * Shared physical-bus lifecycle value contract.
 *
 * This object is owned and serialized by one profile-private resource owner.
 * It deliberately contains no I2C/SPI handle, mutex, task, timer, GPIO or
 * board identity.  Codec, touch, PMIC and IMU adapters borrow a generation
 * lease before touching their profile-private handles; a bus recovery first
 * closes admission, then waits for every issued lease to return before an
 * adapter may detach/recreate/reprobe physical devices.
 *
 * The contract is also valid for a future shared SPI/I2S control resource.
 * It does not itself reset electrical hardware and cannot turn an unknown
 * physical outcome into READY.
 */

#include <stdbool.h>
#include <stdint.h>

#include "fault_domain.h"

#define SHARED_BUS_LIFECYCLE_ABI_VERSION 1u
#define SHARED_BUS_LIFECYCLE_MAX_LEASES 12u

typedef enum {
    SHARED_BUS_ATTACHMENT_DETACHED = 0,
    SHARED_BUS_ATTACHMENT_ATTACHED,
} shared_bus_attachment_t;

typedef enum {
    SHARED_BUS_LIFECYCLE_OK = 0,
    SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT,
    SHARED_BUS_LIFECYCLE_BUSY,
    SHARED_BUS_LIFECYCLE_UNAVAILABLE,
    SHARED_BUS_LIFECYCLE_RESOURCE_EXHAUSTED,
    SHARED_BUS_LIFECYCLE_STALE_LEASE,
    SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME,
} shared_bus_lifecycle_result_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t generation;
    uint64_t token;
} shared_bus_lease_t;

typedef struct {
    bool occupied;
    uint32_t generation;
    uint64_t token;
} shared_bus_lifecycle_lease_slot_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    fault_domain_t domain;
    shared_bus_attachment_t attachment;
    uint64_t next_lease_token;
    shared_bus_lifecycle_lease_slot_t leases[SHARED_BUS_LIFECYCLE_MAX_LEASES];
} shared_bus_lifecycle_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    fault_domain_snapshot_t domain;
    shared_bus_attachment_t attachment;
    uint32_t active_lease_count;
} shared_bus_lifecycle_snapshot_t;

/* Initializes a detached, STOPPED lifecycle. The caller supplies a stable
 * logical resource id for diagnostics; it is not a GPIO port or board id. */
void shared_bus_lifecycle_reset(shared_bus_lifecycle_t *lifecycle,
                                fault_domain_id_t id);

/* Startup sequence: begin_reinitialize -> mark_attached -> begin_self_test ->
 * mark_ready. `mark_attached` merely records that the profile has created its
 * bus; it must be followed by complete adapter reprobe before READY. */
shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_reinitialize(
    shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_attached(
    shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_self_test(
    shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_ready(
    shared_bus_lifecycle_t *lifecycle);

/* Acquires one unique, generation-bound borrower lease. A full lease table
 * fails closed rather than allowing an untracked handle to outlive recovery. */
shared_bus_lifecycle_result_t shared_bus_lifecycle_acquire(
    shared_bus_lifecycle_t *lifecycle, shared_bus_lease_t *out_lease);
shared_bus_lifecycle_result_t shared_bus_lifecycle_release(
    shared_bus_lifecycle_t *lifecycle, const shared_bus_lease_t *lease);

/* Recovery starts by closing admission and invalidating every extant lease
 * generation. The caller must wait for `can_detach` before removing any
 * profile-private device or master-bus handle. `mark_detached` enters STOPPED
 * only after that drain. Any probe/cleanup ambiguity must use mark_unknown or
 * mark_degraded; neither can later be opened as READY without an explicit
 * safe cleanup/reinitialization sequence. */
shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_recovery(
    shared_bus_lifecycle_t *lifecycle);
bool shared_bus_lifecycle_can_detach(const shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_detached(
    shared_bus_lifecycle_t *lifecycle);
/* A failed startup can return to STOPPED only while no physical bus was ever
 * attached.  Once attached, cleanup must use begin_recovery + mark_detached
 * or conservatively mark_unknown. */
shared_bus_lifecycle_result_t shared_bus_lifecycle_cancel_unattached_start(
    shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_unknown(
    shared_bus_lifecycle_t *lifecycle);
shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_degraded(
    shared_bus_lifecycle_t *lifecycle);

bool shared_bus_lifecycle_get_snapshot(const shared_bus_lifecycle_t *lifecycle,
                                       shared_bus_lifecycle_snapshot_t *out_snapshot);
