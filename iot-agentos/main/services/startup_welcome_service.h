#pragma once

/*
 * Cold-start Welcome gate.
 *
 * This service owns the one-shot Welcome state and its bounded completion
 * wait.  The composition root retains boot-session correlation, Gateway
 * parsing, audio playback and the broader startup sequence.  No SDK, RTOS,
 * transport, JSON, allocator or board object crosses this value contract.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    void (*log_gate_released)(const char *reason, void *context);
    void (*log_gate_timed_out)(uint32_t timeout_ms, void *context);
    void *context;
} startup_welcome_service_host_t;

device_status_t startup_welcome_service_init(
    const startup_welcome_service_host_t *host);

/* Records the authenticated cold-start handshake fact before the normal
 * wake/poll sequence begins. */
void startup_welcome_service_note_handshake_queued(bool queued);

/* Starts one cold-start sequence, drains any stale completion token, and
 * returns whether this sequence owns a Welcome gate. */
bool startup_welcome_service_begin_sequence(void);

/* The poll worker could not start. A queued greeting is now terminally late
 * for this boot and must be silently ACKed if it arrives later. */
void startup_welcome_service_mark_startup_failed(void);

/* Waits for the current Welcome completion. On timeout the gate closes and
 * subsequent current-boot Welcome delivery is classified as late. */
bool startup_welcome_service_wait_for_completion(uint32_t timeout_ms);

bool startup_welcome_service_gate_active(void);
bool startup_welcome_service_should_discard_current(void);

/* Records idempotent playback consumption and releases the gate. */
void startup_welcome_service_complete_current(bool playback_succeeded);
