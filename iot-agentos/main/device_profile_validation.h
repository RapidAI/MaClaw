#pragma once

/* Internal value-only validation helpers for the immutable board profile.
 * They deliberately know no board name, driver, RTOS object, or SDK type. */

#include "device_api.h"

bool device_profile_input_source_is_wake_eligible(device_input_source_t source);
/* Shared Input Service validates the normalized adapter→business event pair
 * before it chooses an event lane or allocates a sequence number. */
bool device_input_action_is_valid(device_input_action_t action);
bool device_input_source_is_valid(device_input_source_t source);
bool device_input_action_source_is_valid(device_input_action_t action,
                                         device_input_source_t source);
