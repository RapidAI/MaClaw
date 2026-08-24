#pragma once

#include <stdbool.h>
#include <stdint.h>

/* Firmware Identity exposes synchronous, by-value diagnostic snapshots in
 * addition to its retained USB reader.  This tiny private admission helper
 * gives the System Sleep participant one linearization point for both kinds
 * of observer: PREPARE closes the marker, then waits for the count to drain.
 *
 * It deliberately contains no FreeRTOS, USB or power-profile details.  The
 * owner supplies the storage and decides how/where to wait, which keeps this
 * reusable concurrency rule independently host-testable without widening the
 * public Firmware Identity API. */
static inline bool firmware_identity_sleep_gate_begin(
    volatile bool *preparing, volatile uint32_t *active_observers) {
    if (!preparing || !active_observers ||
        __atomic_load_n(preparing, __ATOMIC_ACQUIRE)) {
        return false;
    }
    __atomic_add_fetch(active_observers, 1u, __ATOMIC_ACQ_REL);
    /* PREPARE may have closed the marker between the first observation and
     * our increment.  Give that admission back and fail closed rather than
     * allowing a new snapshot/start operation across electrical preparation. */
    if (__atomic_load_n(preparing, __ATOMIC_ACQUIRE)) {
        __atomic_sub_fetch(active_observers, 1u, __ATOMIC_ACQ_REL);
        return false;
    }
    return true;
}

static inline void firmware_identity_sleep_gate_end(
    volatile uint32_t *active_observers) {
    if (!active_observers) return;
    uint32_t observed = __atomic_load_n(active_observers, __ATOMIC_ACQUIRE);
    while (observed != 0 &&
           !__atomic_compare_exchange_n(active_observers, &observed,
                                        observed - 1u, false,
                                        __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
    }
}
