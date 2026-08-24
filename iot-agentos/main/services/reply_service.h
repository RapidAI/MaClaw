#pragma once

/*
 * Reply business service.
 *
 * Owns the reply-domain runtime state that used to live in main.c: the active
 * command reply correlation, the cancelled-command reply registry used to
 * discard late answers, and the bounded post-terminal result-speech (TTS)
 * transaction.  It also owns the terminal reply transition that closes the
 * cancellation window, wakes the interaction worker and publishes the final
 * text/image surface as one ordered step.
 *
 * The long-poll loop itself remains in main.c (Gateway Dispatcher, A8); it
 * resolves correlation, late-reply filtering and presentation admission only
 * through this API.  Command Service reaches the cancelled-reply registry
 * through the same typed value calls (service-to-service, no shared mutable
 * globals).
 *
 * The public contract exposes value types only.  The interaction waiter is an
 * opaque integer token minted and interpreted by the composition root; no
 * FreeRTOS handle, ESP-IDF error code or JSON object crosses this boundary.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

/* Single source of truth for the command reply correlation id capacity.
 * main.c (voice submit buffer) and Command Service (/cancel correlation) use
 * this same constant. */
#define REPLY_SERVICE_REPLY_ID_CAPACITY 96u

device_status_t reply_service_init(void);

/* Active command reply correlation. */
void reply_service_set_active_reply_to(const char *reply_to);
void reply_service_clear_active_reply_to(void);
void reply_service_copy_active_reply_to(char *out_reply_to, uint32_t capacity);
/* Correlation id match without requiring a live interaction worker (timing
 * checkpoints still belong to the accepted command after hand-off). */
bool reply_service_correlation_matches(const char *reply_to);
/* Correlation id match that additionally requires a live interaction worker. */
bool reply_service_active_matches(const char *reply_to);
/* The outgoing poll can resume as soon as the POST releases the shared HTTP
 * lock. On a very fast reply it may therefore see the result during the few
 * scheduler ticks in which the interaction worker is still parsing/publishing
 * the returned maclawMessageId. Give that correlation hand-off a short bounded
 * grace period instead of acknowledging and losing the result as unrelated. */
bool reply_service_active_matches_after_handoff(const char *reply_to);

/* Terminal reply transition.  begin atomically closes the cancellation window
 * and wins the terminal token before any display side effect; it returns a
 * non-zero waiter token on success.  complete publishes the terminal screen
 * and wakes the interaction worker as one ordered UI transition. */
uintptr_t reply_service_begin_active_reply(void);
void reply_service_complete_active_text_reply(uintptr_t waiter,
                                              const char *title,
                                              const char *text);
void reply_service_complete_active_image_reply(uintptr_t waiter,
                                               const char *title,
                                               const char *caption,
                                               const uint16_t *pixels,
                                               size_t width,
                                               size_t height);

/* Cancelled-command reply registry: late replies for a cancelled command are
 * discarded/acknowledged without presentation. */
void reply_service_remember_cancelled(void);
bool reply_service_cancelled_matches(const char *reply_to);

/* Post-terminal result speech (TTS) transaction.  A terminal text/image can
 * deliberately precede its TTS parts; only the exact correlation and its
 * declared remaining part count are retained after the command worker exits,
 * behind a bounded idle deadline that each consumed part refreshes. */
bool reply_service_result_speech_matches(const char *reply_to);
void reply_service_arm_result_speech(const char *reply_to, unsigned parts);
void reply_service_finish_result_speech_part(const char *reply_to);
/* Server-declared speech end closes the transaction early. */
void reply_service_finish_result_speech(const char *reply_to);
/* Closing the visible result is also an explicit choice to leave that command
 * behind: a delayed TTS part must not pull audio back into the ambient screen
 * minutes later. */
void reply_service_dismiss_result_speech(void);
void reply_service_clear_result_speech(void);

/* New voice command start: clears the active correlation and the result
 * speech gate as one atomic reset. */
void reply_service_reset_for_command_start(void);
