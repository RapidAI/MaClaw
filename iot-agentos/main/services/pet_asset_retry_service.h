#pragma once

/*
 * Ordered downlink pet-asset retry state.
 *
 * The Dispatcher remains owner of page/cursor/ACK order. This small value
 * service only remembers how often the currently blocked pet-profile message
 * has failed, so a permanently pressure-constrained asset cannot starve the
 * messages behind it forever. It contains no Gateway, task, JSON, HTTP or
 * board state.
 */

#include <stdbool.h>
#include <stdint.h>

#define PET_ASSET_RETRY_SERVICE_MESSAGE_ID_CAPACITY 80u
#define PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT 3u

/* Initializes the service-owned retry state. This remains a pure value
 * operation; the composition root retains no retry global. */
void pet_asset_retry_service_init(void);

/* Clears all retry accounting. */
void pet_asset_retry_service_reset(void);

/* Records a transient failure for `message_id`; returns the count for this
 * message. A different id begins a fresh count. Empty/missing IDs are kept as
 * one stable value, matching Dispatcher’s existing ordered-retry behavior. */
uint32_t pet_asset_retry_service_note_failure(const char *message_id);

/* True once the state has reached the supplied retry limit. A zero limit is
 * invalid and never exhausts, preventing accidental immediate failed-ACKs. */
bool pet_asset_retry_service_exhausted(uint32_t retry_limit);
