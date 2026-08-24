#include "shared_bus_lifecycle.h"

#include <limits.h>
#include <string.h>

static bool lifecycle_valid(const shared_bus_lifecycle_t *lifecycle) {
    return lifecycle && lifecycle->struct_size == sizeof(*lifecycle) &&
           lifecycle->abi_version == SHARED_BUS_LIFECYCLE_ABI_VERSION &&
           lifecycle->domain.struct_size == sizeof(lifecycle->domain) &&
           lifecycle->domain.abi_version == FAULT_DOMAIN_ABI_VERSION;
}

static bool lease_valid(const shared_bus_lease_t *lease) {
    return lease && lease->struct_size == sizeof(*lease) &&
           lease->abi_version == SHARED_BUS_LIFECYCLE_ABI_VERSION &&
           lease->generation != 0u && lease->token != 0u;
}

static uint32_t active_lease_count(const shared_bus_lifecycle_t *lifecycle) {
    uint32_t count = 0u;
    for (uint32_t i = 0u; i < SHARED_BUS_LIFECYCLE_MAX_LEASES; ++i) {
        if (lifecycle->leases[i].occupied) ++count;
    }
    return count;
}

static shared_bus_lifecycle_result_t from_fault_result(bool ok,
                                                        shared_bus_lifecycle_t *lifecycle) {
    if (ok) return SHARED_BUS_LIFECYCLE_OK;
    fault_domain_snapshot_t snapshot = {0};
    if (!fault_domain_get_snapshot(&lifecycle->domain, &snapshot)) {
        return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
    }
    return snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME ||
           snapshot.phase == FAULT_DOMAIN_DEGRADED
               ? SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME
               : SHARED_BUS_LIFECYCLE_BUSY;
}

void shared_bus_lifecycle_reset(shared_bus_lifecycle_t *lifecycle,
                                fault_domain_id_t id) {
    if (!lifecycle) return;
    memset(lifecycle, 0, sizeof(*lifecycle));
    lifecycle->struct_size = sizeof(*lifecycle);
    lifecycle->abi_version = SHARED_BUS_LIFECYCLE_ABI_VERSION;
    lifecycle->attachment = SHARED_BUS_ATTACHMENT_DETACHED;
    lifecycle->next_lease_token = 1u;
    fault_domain_reset(&lifecycle->domain, id);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_reinitialize(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (lifecycle->attachment != SHARED_BUS_ATTACHMENT_DETACHED ||
        active_lease_count(lifecycle) != 0u) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    return from_fault_result(fault_domain_begin_start(&lifecycle->domain), lifecycle);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_attached(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    fault_domain_snapshot_t snapshot = {0};
    if (!fault_domain_get_snapshot(&lifecycle->domain, &snapshot)) {
        return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
    }
    if (snapshot.phase != FAULT_DOMAIN_REINITIALIZING ||
        lifecycle->attachment != SHARED_BUS_ATTACHMENT_DETACHED) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    lifecycle->attachment = SHARED_BUS_ATTACHMENT_ATTACHED;
    return SHARED_BUS_LIFECYCLE_OK;
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_self_test(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (lifecycle->attachment != SHARED_BUS_ATTACHMENT_ATTACHED) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    return from_fault_result(fault_domain_begin_self_test(&lifecycle->domain), lifecycle);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_ready(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (lifecycle->attachment != SHARED_BUS_ATTACHMENT_ATTACHED) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    return from_fault_result(fault_domain_mark_ready(&lifecycle->domain), lifecycle);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_acquire(
    shared_bus_lifecycle_t *lifecycle, shared_bus_lease_t *out_lease) {
    if (!lifecycle_valid(lifecycle) || !out_lease) {
        return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    }
    fault_domain_snapshot_t snapshot = {0};
    if (!fault_domain_get_snapshot(&lifecycle->domain, &snapshot)) {
        return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
    }
    if (!snapshot.admission_open || lifecycle->attachment != SHARED_BUS_ATTACHMENT_ATTACHED) {
        return SHARED_BUS_LIFECYCLE_UNAVAILABLE;
    }
    if (lifecycle->next_lease_token == 0u ||
        lifecycle->next_lease_token == UINT64_MAX) {
        return SHARED_BUS_LIFECYCLE_RESOURCE_EXHAUSTED;
    }
    for (uint32_t i = 0u; i < SHARED_BUS_LIFECYCLE_MAX_LEASES; ++i) {
        if (lifecycle->leases[i].occupied) continue;
        const uint64_t token = lifecycle->next_lease_token++;
        lifecycle->leases[i] = (shared_bus_lifecycle_lease_slot_t){
            .occupied = true,
            .generation = snapshot.generation,
            .token = token,
        };
        *out_lease = (shared_bus_lease_t){
            .struct_size = sizeof(*out_lease),
            .abi_version = SHARED_BUS_LIFECYCLE_ABI_VERSION,
            .generation = snapshot.generation,
            .token = token,
        };
        return SHARED_BUS_LIFECYCLE_OK;
    }
    return SHARED_BUS_LIFECYCLE_RESOURCE_EXHAUSTED;
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_release(
    shared_bus_lifecycle_t *lifecycle, const shared_bus_lease_t *lease) {
    if (!lifecycle_valid(lifecycle) || !lease_valid(lease)) {
        return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    }
    for (uint32_t i = 0u; i < SHARED_BUS_LIFECYCLE_MAX_LEASES; ++i) {
        if (!lifecycle->leases[i].occupied ||
            lifecycle->leases[i].generation != lease->generation ||
            lifecycle->leases[i].token != lease->token) {
            continue;
        }
        lifecycle->leases[i] = (shared_bus_lifecycle_lease_slot_t){0};
        fault_domain_snapshot_t snapshot = {0};
        if (!fault_domain_get_snapshot(&lifecycle->domain, &snapshot)) {
            return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
        }
        return lease->generation == snapshot.generation && snapshot.admission_open
                   ? SHARED_BUS_LIFECYCLE_OK
                   : SHARED_BUS_LIFECYCLE_STALE_LEASE;
    }
    return SHARED_BUS_LIFECYCLE_STALE_LEASE;
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_begin_recovery(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    fault_domain_snapshot_t snapshot = {0};
    if (!fault_domain_get_snapshot(&lifecycle->domain, &snapshot)) {
        return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
    }
    if ((snapshot.phase != FAULT_DOMAIN_READY &&
         snapshot.phase != FAULT_DOMAIN_UNKNOWN_OUTCOME) ||
        lifecycle->attachment != SHARED_BUS_ATTACHMENT_ATTACHED) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    return from_fault_result(fault_domain_begin_quiesce(&lifecycle->domain), lifecycle);
}

bool shared_bus_lifecycle_can_detach(const shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle) || active_lease_count(lifecycle) != 0u) return false;
    fault_domain_snapshot_t snapshot = {0};
    return fault_domain_get_snapshot(&lifecycle->domain, &snapshot) &&
           snapshot.phase == FAULT_DOMAIN_QUIESCING &&
           lifecycle->attachment == SHARED_BUS_ATTACHMENT_ATTACHED;
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_detached(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (!shared_bus_lifecycle_can_detach(lifecycle)) return SHARED_BUS_LIFECYCLE_BUSY;
    lifecycle->attachment = SHARED_BUS_ATTACHMENT_DETACHED;
    return from_fault_result(fault_domain_mark_stopped(&lifecycle->domain), lifecycle);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_cancel_unattached_start(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (lifecycle->attachment != SHARED_BUS_ATTACHMENT_DETACHED ||
        active_lease_count(lifecycle) != 0u) {
        return SHARED_BUS_LIFECYCLE_BUSY;
    }
    return from_fault_result(fault_domain_mark_stopped(&lifecycle->domain), lifecycle);
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_unknown(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    if (fault_domain_mark_unknown_outcome(&lifecycle->domain)) {
        return SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME;
    }
    fault_domain_snapshot_t snapshot = {0};
    return fault_domain_get_snapshot(&lifecycle->domain, &snapshot) &&
           snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME
               ? SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME
               : SHARED_BUS_LIFECYCLE_BUSY;
}

shared_bus_lifecycle_result_t shared_bus_lifecycle_mark_degraded(
    shared_bus_lifecycle_t *lifecycle) {
    if (!lifecycle_valid(lifecycle)) return SHARED_BUS_LIFECYCLE_INVALID_ARGUMENT;
    return from_fault_result(fault_domain_mark_degraded(&lifecycle->domain), lifecycle);
}

bool shared_bus_lifecycle_get_snapshot(const shared_bus_lifecycle_t *lifecycle,
                                       shared_bus_lifecycle_snapshot_t *out_snapshot) {
    if (!lifecycle_valid(lifecycle) || !out_snapshot ||
        !fault_domain_get_snapshot(&lifecycle->domain, &out_snapshot->domain)) {
        return false;
    }
    *out_snapshot = (shared_bus_lifecycle_snapshot_t){
        .struct_size = sizeof(*out_snapshot),
        .abi_version = SHARED_BUS_LIFECYCLE_ABI_VERSION,
        .domain = out_snapshot->domain,
        .attachment = lifecycle->attachment,
        .active_lease_count = active_lease_count(lifecycle),
    };
    return true;
}
