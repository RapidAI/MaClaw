#pragma once

/*
 * Value-only admission state for the boot-initialized physical input scanner.
 *
 * A successful scanner stop/join releases only its task generation: panel,
 * touch controller, GPIO configuration and other bootstrap-owned hardware
 * remain live.  The next Input Service generation may therefore install a
 * new normalized publisher.  Conversely, a failed scanner join leaves an
 * unknown task potentially holding the old publisher; no new generation may
 * reuse the adapter until a future, explicit recovery contract exists.
 */

#include <stdbool.h>

typedef struct {
    bool scanner_recovery_required;
} input_scanner_lifecycle_gate_t;

static inline bool input_scanner_lifecycle_gate_allows_start(
    const input_scanner_lifecycle_gate_t *gate, bool service_active) {
    return gate && !gate->scanner_recovery_required && !service_active;
}

static inline void input_scanner_lifecycle_gate_note_stop_succeeded(
    input_scanner_lifecycle_gate_t *gate) {
    if (gate) gate->scanner_recovery_required = false;
}

static inline void input_scanner_lifecycle_gate_note_stop_failed(
    input_scanner_lifecycle_gate_t *gate) {
    if (gate) gate->scanner_recovery_required = true;
}
