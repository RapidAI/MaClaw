#include "services/startup_runtime_state_service.h"

#include <stdatomic.h>
#include <string.h>
#include <stdint.h>

enum {
    STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE = 1u << 0,
    STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED = 1u << 1,
    STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE = 1u << 2,
    STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED = 1u << 3,
    STARTUP_RUNTIME_STATE_PROVISIONING_STAGED = 1u << 4,
    STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURING = 1u << 5,
    STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURED = 1u << 6,
};

static atomic_bool s_initialized = ATOMIC_VAR_INIT(false);
static atomic_uint_fast8_t s_state = ATOMIC_VAR_INIT(0u);
static char s_boot_session_id[STARTUP_RUNTIME_STATE_BOOT_SESSION_ID_CAPACITY];

static bool initialized(void) {
    return atomic_load_explicit(&s_initialized, memory_order_acquire);
}

device_status_t startup_runtime_state_service_init(void) {
    bool expected = false;
    if (atomic_compare_exchange_strong_explicit(
            &s_initialized, &expected, true, memory_order_acq_rel,
            memory_order_acquire)) {
        atomic_store_explicit(&s_state, 0u, memory_order_release);
    }
    return DEVICE_STATUS_OK;
}

bool startup_runtime_state_service_capture_boot_session_id(const char *session_id) {
    if (!initialized() || !session_id || !session_id[0] ||
        strlen(session_id) >= sizeof(s_boot_session_id)) {
        return false;
    }
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & (STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURING |
                         STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURED)) != 0u) {
            return false;
        }
        const uint_fast8_t desired =
            observed | STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURING;
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            break;
        }
    } while (true);

    memcpy(s_boot_session_id, session_id, strlen(session_id) + 1u);
    observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        const uint_fast8_t desired =
            (observed | STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURED) &
            (uint_fast8_t)~STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURING;
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_release,
                memory_order_acquire)) {
            return true;
        }
    } while (true);
}

const char *startup_runtime_state_service_boot_session_id(void) {
    if (!initialized()) return "";
    const uint_fast8_t state = atomic_load_explicit(&s_state, memory_order_acquire);
    return (state & STARTUP_RUNTIME_STATE_BOOT_SESSION_CAPTURED) != 0u
               ? s_boot_session_id
               : "";
}

bool startup_runtime_state_service_matches_boot_session_id(const char *session_id) {
    return session_id && session_id[0] &&
           strcmp(session_id, startup_runtime_state_service_boot_session_id()) == 0;
}

bool startup_runtime_state_service_capture_staged_provisioning(bool staged) {
    if (!initialized()) return false;
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED) != 0u) {
            return false;
        }
        uint_fast8_t desired = observed | STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED;
        if (staged) {
            desired |= STARTUP_RUNTIME_STATE_PROVISIONING_STAGED;
        }
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            return true;
        }
    } while (true);
}

bool startup_runtime_state_service_staged_provisioning_pending(void) {
    if (!initialized()) return false;
    const uint_fast8_t state = atomic_load_explicit(&s_state, memory_order_acquire);
    return (state & (STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED |
                     STARTUP_RUNTIME_STATE_PROVISIONING_STAGED)) ==
           (STARTUP_RUNTIME_STATE_PROVISIONING_CAPTURED |
            STARTUP_RUNTIME_STATE_PROVISIONING_STAGED);
}

bool startup_runtime_state_service_begin_sequence(void) {
    if (!initialized()) return false;
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) != 0u) return false;
        const uint_fast8_t desired =
            observed & (uint_fast8_t)~STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE;
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            return true;
        }
    } while (true);
}

void startup_runtime_state_service_complete_sequence(void) {
    if (!initialized()) return;
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) != 0u) return;
        const uint_fast8_t desired =
            observed | STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE;
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            return;
        }
    } while (true);
}

bool startup_runtime_state_service_sequence_complete(void) {
    if (!initialized()) return false;
    return (atomic_load_explicit(&s_state, memory_order_acquire) &
            STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE) != 0u;
}

bool startup_runtime_state_service_permit_gateway_startup(void) {
    if (!initialized()) return false;
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) != 0u) return false;
        const uint_fast8_t desired =
            observed | STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED;
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            return true;
        }
    } while (true);
}

bool startup_runtime_state_service_gateway_startup_recovery_allowed(void) {
    if (!initialized()) return false;
    const uint_fast8_t state = atomic_load_explicit(&s_state, memory_order_acquire);
    return (state & (STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED |
                     STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE |
                     STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE)) ==
           STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED;
}

bool startup_runtime_state_service_enter_safe_mode(void) {
    if (!initialized()) return false;
    uint_fast8_t observed = atomic_load_explicit(&s_state, memory_order_acquire);
    do {
        if ((observed & STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) != 0u) return false;
        const uint_fast8_t desired =
            (observed | STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) &
            (uint_fast8_t)~(STARTUP_RUNTIME_STATE_SEQUENCE_COMPLETE |
                            STARTUP_RUNTIME_STATE_GATEWAY_STARTUP_ALLOWED);
        if (atomic_compare_exchange_weak_explicit(
                &s_state, &observed, desired, memory_order_acq_rel,
                memory_order_acquire)) {
            return true;
        }
    } while (true);
}

bool startup_runtime_state_service_safe_mode_active(void) {
    if (!initialized()) return false;
    return (atomic_load_explicit(&s_state, memory_order_acquire) &
            STARTUP_RUNTIME_STATE_SAFE_MODE_ACTIVE) != 0u;
}
