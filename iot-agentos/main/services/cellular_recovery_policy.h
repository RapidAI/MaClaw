#pragma once

#include <stdint.h>

/*
 * Value-only retry policy for the cellular-recovery coordinator.  Keeping the
 * backoff calculation independent of FreeRTOS makes the recovery contract
 * directly testable on the host and prevents board-specific modem code from
 * acquiring retry policy.
 */
#define CELLULAR_RECOVERY_RETRY_INITIAL_MS 2000u
#define CELLULAR_RECOVERY_RETRY_MAX_MS 60000u

uint32_t cellular_recovery_policy_next_retry_ms(uint32_t current_ms);

