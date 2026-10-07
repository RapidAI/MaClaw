#pragma once

/*
 * Pending approval ack delivery (plan N1-6, device side).
 *
 * Owns the one card that is waiting for the user's answer and the ack that
 * answer owes the Hub.  The safety rules themselves live in the host-tested
 * services/event_decision.h; what stays here is the ESP-IDF glue: the
 * monotonic clock, the cJSON envelope, the transport POST and the screen.
 *
 * Two callers, deliberately:
 *   - the downlink dispatcher arms the slot when an event carrying actions
 *     arrives (main.c's gateway_host_apply_event);
 *   - the presentation input binding records the answer when the user presses
 *     the confirm or decline gesture.
 *
 * Neither of them blocks on the network.  The gesture only records the
 * decision, and gateway_event_ack_service_flush() delivers it from the poll
 * worker, which already owns the outbound lane -- a hardware input task must
 * never wait on TLS, and the poll worker is already where the other outboxes
 * are drained.
 *
 * Not persisted, on purpose.  A decision is only meaningful while the Hub still
 * holds the event's snapshot and the window is open; a reboot therefore loses
 * the answer rather than replaying it.  That is the safe direction: the Hub
 * replays a `persist=true` card after reconnecting, so the user is asked again
 * instead of the device resurrecting a stale approval that the Hub has since
 * adjudicated as expired (which would close the desktop prompt as "nobody
 * answered" while the user believes they approved it).
 */

#include <stdbool.h>
#include <stdint.h>

#include "services/event_decision.h"
#include "services/event_ingest.h"

/* Clears the slot.  Called once at startup beside the other service inits. */
void gateway_event_ack_service_init(void);

/* True when this event carries at least one answerable action.  A card with no
 * actions is not a decision: the caller shows the plain message card and the
 * slot stays untouched. */
bool gateway_event_ack_service_offers_decision(const event_ingest_fields_t *fields);

/* True when the slot can take a new card.  Exposed so the dispatcher can ask
 * *before* it records the event's dedupe key: a card held back because an
 * earlier answer has not reached the Hub yet must be able to present itself on
 * the retry, and recording the key first would suppress exactly that retry. */
bool gateway_event_ack_service_slot_free(void);

/* Arms the slot and puts the answerable card on screen.  Returns false while
 * an undelivered answer still holds the slot, so the caller keeps the event
 * pending instead of overwriting an answer the Hub has not received yet. */
bool gateway_event_ack_service_begin(const event_ingest_fields_t *fields);

/* Records a gesture as the answer and repaints once.  Returns true when the
 * input was taken, so the binding must not fall through to voice or meeting.
 * Never touches the network. */
bool gateway_event_ack_service_handle_input(event_decision_input_t input);

/* Closes the window if it has passed, then delivers whatever ack the slot
 * still owes.  0 on success, including the common "nothing owed" case.
 *
 * Both jobs live in one call because they are the same question asked once per
 * poll: which ack is owed right now.  Splitting them would let a caller report
 * a receipt for a card whose window had already closed, and would leave the
 * timeout -- "nobody answered" -- unreported for a card the user never saw.
 *
 * A failed delivery keeps the ack owed so the next poll retries it: an
 * approval must not be lost to a flaky socket. */
int32_t gateway_event_ack_service_flush(void);
