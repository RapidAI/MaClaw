#include "fault_domain.h"

#include <limits.h>
#include <stddef.h>

static bool fault_domain_id_is_valid(fault_domain_id_t id) {
    return id > FAULT_DOMAIN_ID_UNKNOWN && id <= FAULT_DOMAIN_ID_SHARED_BUS;
}

static bool fault_domain_phase_is_valid(fault_domain_phase_t phase) {
    return phase >= FAULT_DOMAIN_STOPPED && phase <= FAULT_DOMAIN_DEGRADED;
}

static bool fault_domain_is_valid(const fault_domain_t *domain) {
    return domain && domain->struct_size == sizeof(*domain) &&
           domain->abi_version == FAULT_DOMAIN_ABI_VERSION &&
           fault_domain_id_is_valid(domain->id);
}

static fault_domain_phase_t fault_domain_load_phase(const fault_domain_t *domain) {
    return __atomic_load_n(&domain->phase, __ATOMIC_ACQUIRE);
}

static bool fault_domain_transition(fault_domain_t *domain,
                                    fault_domain_phase_t expected,
                                    fault_domain_phase_t desired,
                                    bool invalidate_generation) {
    if (!fault_domain_is_valid(domain)) return false;
    if (invalidate_generation) {
        uint32_t generation = __atomic_load_n(&domain->generation, __ATOMIC_ACQUIRE);
        if (generation == UINT32_MAX) return false;
    }
    if (!__atomic_compare_exchange_n(&domain->phase, &expected, desired, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return false;
    }
    if (invalidate_generation) {
        /* Generation is never zero after reset.  The phase is already closed
         * while this increment is published, so no borrower can use the small
         * transition interval as an admission window. */
        (void)__atomic_add_fetch(&domain->generation, 1u, __ATOMIC_RELEASE);
    }
    return true;
}

void fault_domain_reset(fault_domain_t *domain, fault_domain_id_t id) {
    if (!domain || !fault_domain_id_is_valid(id)) return;
    domain->struct_size = sizeof(*domain);
    domain->abi_version = FAULT_DOMAIN_ABI_VERSION;
    domain->id = id;
    __atomic_store_n(&domain->generation, 1u, __ATOMIC_RELEASE);
    __atomic_store_n(&domain->phase, FAULT_DOMAIN_STOPPED, __ATOMIC_RELEASE);
}

bool fault_domain_begin_start(fault_domain_t *domain) {
    return fault_domain_transition(domain, FAULT_DOMAIN_STOPPED,
                                   FAULT_DOMAIN_REINITIALIZING, true);
}

bool fault_domain_begin_quiesce(fault_domain_t *domain) {
    if (fault_domain_transition(domain, FAULT_DOMAIN_READY,
                                FAULT_DOMAIN_QUIESCING, true)) {
        return true;
    }
    return fault_domain_transition(domain, FAULT_DOMAIN_UNKNOWN_OUTCOME,
                                   FAULT_DOMAIN_QUIESCING, true);
}

bool fault_domain_begin_self_test(fault_domain_t *domain) {
    return fault_domain_transition(domain, FAULT_DOMAIN_REINITIALIZING,
                                   FAULT_DOMAIN_SELF_TEST, false);
}

bool fault_domain_mark_ready(fault_domain_t *domain) {
    return fault_domain_transition(domain, FAULT_DOMAIN_SELF_TEST,
                                   FAULT_DOMAIN_READY, false);
}

bool fault_domain_mark_stopped(fault_domain_t *domain) {
    if (fault_domain_transition(domain, FAULT_DOMAIN_QUIESCING,
                                FAULT_DOMAIN_STOPPED, false)) {
        return true;
    }
    /* A failed start whose self-test proves no resource was published has no
     * borrower to drain.  It may return directly to STOPPED and be retried. */
    if (fault_domain_transition(domain, FAULT_DOMAIN_REINITIALIZING,
                                FAULT_DOMAIN_STOPPED, false)) {
        return true;
    }
    return fault_domain_transition(domain, FAULT_DOMAIN_SELF_TEST,
                                   FAULT_DOMAIN_STOPPED, false);
}

bool fault_domain_mark_unknown_outcome(fault_domain_t *domain) {
    if (!fault_domain_is_valid(domain)) return false;
    for (;;) {
        const fault_domain_phase_t phase = fault_domain_load_phase(domain);
        if (phase != FAULT_DOMAIN_QUIESCING &&
            phase != FAULT_DOMAIN_REINITIALIZING &&
            phase != FAULT_DOMAIN_SELF_TEST) {
            return false;
        }
        if (fault_domain_transition(domain, phase,
                                    FAULT_DOMAIN_UNKNOWN_OUTCOME, false)) {
            return true;
        }
    }
}

bool fault_domain_mark_degraded(fault_domain_t *domain) {
    if (!fault_domain_is_valid(domain)) return false;
    for (;;) {
        const fault_domain_phase_t phase = fault_domain_load_phase(domain);
        if (phase != FAULT_DOMAIN_QUIESCING &&
            phase != FAULT_DOMAIN_REINITIALIZING &&
            phase != FAULT_DOMAIN_SELF_TEST &&
            phase != FAULT_DOMAIN_UNKNOWN_OUTCOME) {
            return false;
        }
        if (fault_domain_transition(domain, phase, FAULT_DOMAIN_DEGRADED, false)) {
            return true;
        }
    }
}

bool fault_domain_get_snapshot(const fault_domain_t *domain,
                               fault_domain_snapshot_t *out_snapshot) {
    if (!fault_domain_is_valid(domain) || !out_snapshot) return false;
    const fault_domain_phase_t phase = fault_domain_load_phase(domain);
    if (!fault_domain_phase_is_valid(phase)) return false;
    const uint32_t generation = __atomic_load_n(&domain->generation, __ATOMIC_ACQUIRE);
    if (generation == 0) return false;
    *out_snapshot = (fault_domain_snapshot_t){
        .struct_size = sizeof(*out_snapshot),
        .abi_version = FAULT_DOMAIN_ABI_VERSION,
        .id = domain->id,
        .phase = phase,
        .generation = generation,
        .admission_open = phase == FAULT_DOMAIN_READY,
    };
    return true;
}
