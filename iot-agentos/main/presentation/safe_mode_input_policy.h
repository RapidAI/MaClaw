#pragma once

/*
 * Value-only admission policy for the local SAFE_MODE input surface.
 *
 * Alarm dismissal deliberately precedes the SAFE_MODE ordinary-input gate:
 * the minimum recovery service set retains a local alarm, while command,
 * meeting, provisioning and configuration actions must remain closed.  The
 * policy has no board, SDK, task, or service dependency so the ordering can
 * be regression-tested independently of a physical touch/key adapter.
 */

#include <stdbool.h>

typedef enum {
    SAFE_MODE_INPUT_ROUTE_CONTINUE = 0,
    SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM,
    SAFE_MODE_INPUT_ROUTE_IGNORE_RINGING_ALARM,
    SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE,
} safe_mode_input_route_t;

safe_mode_input_route_t safe_mode_input_policy_route(bool alarm_initialized,
                                                      bool alarm_ringing,
                                                      bool primary_interaction_source,
                                                      bool safe_mode_active);
