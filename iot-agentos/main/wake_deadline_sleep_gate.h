#pragma once

#include <stdbool.h>
#include <stdint.h>

/* Wake Deadline owns the ESP timer privately, but System Sleep needs a small
 * value-only rule shared by its dispatcher task and Power participant: once
 * PREPARE closes delivery, no later callback selection may begin; a timeout
 * keeps that marker closed until the transaction owner explicitly ABORTs.
 *
 * Callers serialize begin/selection with their own lifecycle mutex.  This
 * helper deliberately has no FreeRTOS or ESP timer dependency so the closed
 * generation rule can be exercised on the host. */
static inline bool wake_deadline_sleep_gate_begin(volatile bool *preparing) {
    if (!preparing || __atomic_load_n(preparing, __ATOMIC_ACQUIRE)) return false;
    __atomic_store_n(preparing, true, __ATOMIC_RELEASE);
    return true;
}

static inline bool wake_deadline_sleep_gate_callbacks_drained(
    volatile uint32_t *callbacks_inflight) {
    return callbacks_inflight &&
           __atomic_load_n(callbacks_inflight, __ATOMIC_ACQUIRE) == 0;
}

static inline void wake_deadline_sleep_gate_abort(volatile bool *preparing) {
    if (!preparing) return;
    __atomic_store_n(preparing, false, __ATOMIC_RELEASE);
}
