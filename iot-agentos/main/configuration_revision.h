#pragma once

/*
 * Configuration revision value rule.
 *
 * This deliberately contains no persistence, time, network, RTOS or board
 * dependency. Configuration Service is the only durable publisher; this
 * helper merely makes the non-wrapping revision rule testable on the host.
 */

#include <stdbool.h>
#include <stdint.h>

/* Produces the revision for one successfully committed configuration
 * transaction. Revision zero is reserved for an image that has no durable
 * configuration record yet. UINT64_MAX fails closed rather than wrapping and
 * making a stale snapshot appear new. `out_next` is unchanged on failure. */
bool configuration_revision_next(uint64_t current, uint64_t *out_next);
