#pragma once

/*
 * Small, value-only fault-domain lifecycle contract.
 *
 * A domain generation invalidates every borrower reference from the preceding
 * lifecycle.  This module deliberately contains no task, driver, bus or board
 * ownership: the owning service must drain its own borrowers before it reports
 * STOPPED, and must complete its own physical self-test before it reports
 * READY.
 */

#include <stdbool.h>
#include <stdint.h>

#define FAULT_DOMAIN_ABI_VERSION 1u

typedef enum {
    FAULT_DOMAIN_ID_UNKNOWN = 0,
    FAULT_DOMAIN_ID_STORAGE,
    FAULT_DOMAIN_ID_DISPLAY,
    FAULT_DOMAIN_ID_AUDIO,
    FAULT_DOMAIN_ID_CONNECTIVITY,
    FAULT_DOMAIN_ID_SHARED_BUS,
} fault_domain_id_t;

typedef enum {
    FAULT_DOMAIN_STOPPED = 0,
    FAULT_DOMAIN_REINITIALIZING,
    FAULT_DOMAIN_SELF_TEST,
    FAULT_DOMAIN_READY,
    FAULT_DOMAIN_QUIESCING,
    /* A physical action may have completed even though its caller received an
     * error/timeout.  New admission stays closed until an explicit cleanup
     * transaction proves the observed state. */
    FAULT_DOMAIN_UNKNOWN_OUTCOME,
    FAULT_DOMAIN_DEGRADED,
} fault_domain_phase_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    fault_domain_id_t id;
    fault_domain_phase_t phase;
    uint32_t generation;
} fault_domain_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    fault_domain_id_t id;
    fault_domain_phase_t phase;
    uint32_t generation;
    bool admission_open;
} fault_domain_snapshot_t;

/* Called once by the owning service before it publishes the domain.  It is
 * not a restart primitive and must not race with any lifecycle operation. */
void fault_domain_reset(fault_domain_t *domain, fault_domain_id_t id);
bool fault_domain_begin_start(fault_domain_t *domain);
/* READY and UNKNOWN_OUTCOME are the only states that can begin a bounded
 * cleanup transaction.  Both invalidate old borrower generations. */
bool fault_domain_begin_quiesce(fault_domain_t *domain);
bool fault_domain_begin_self_test(fault_domain_t *domain);
bool fault_domain_mark_ready(fault_domain_t *domain);
bool fault_domain_mark_stopped(fault_domain_t *domain);
bool fault_domain_mark_unknown_outcome(fault_domain_t *domain);
bool fault_domain_mark_degraded(fault_domain_t *domain);
bool fault_domain_get_snapshot(const fault_domain_t *domain,
                               fault_domain_snapshot_t *out_snapshot);

