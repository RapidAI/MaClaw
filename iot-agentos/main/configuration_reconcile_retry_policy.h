#pragma once

/*
 * Pure retry cadence for the one Configuration reconciliation owner.
 *
 * This is deliberately a value-only policy: it owns neither a clock nor a
 * timer and does not know why an individual consumer failed. The coordinator
 * decides whether a status is transient, then uses this bounded curve to arm
 * its one retry path. Keeping the curve here prevents Audio, Display and a
 * future screen policy consumer from independently choosing retry pressure.
 */

#include <stdbool.h>
#include <stdint.h>

#define CONFIGURATION_RECONCILE_RETRY_INITIAL_DELAY_MS 1000u
#define CONFIGURATION_RECONCILE_RETRY_MAX_DELAY_MS 60000u

/* `retry_attempt` is one-based. The bounded sequence is 1, 2, 4, 8, 16,
 * 32, 60, 60... seconds. Zero or a NULL output is invalid. */
bool configuration_reconcile_retry_delay_ms(uint32_t retry_attempt,
                                            uint32_t *out_delay_ms);
