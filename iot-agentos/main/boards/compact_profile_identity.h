#pragma once
/*
 * Immutable scene facts supplied to a compact product-identity composer.
 *
 * The shared renderer captures this under its LCD lock before dispatching a
 * profile hook. A profile may map the facts to its own visual identity, but
 * must not reach back into renderer globals for application/scene state.
 */

#include <stdbool.h>

typedef struct {
    const char *state;
    const char *ambient_time;
    const char *ambient_date;
    const char *ambient_weekday;
    const char *command_stage;
    bool gateway_ready;
    unsigned animation_phase;
} compact_profile_identity_state_t;
