#pragma once

/*
 * Voice-interaction orchestration service (A6 first increment).
 *
 * Owns the interaction worker lifecycle that used to live in main.c: the
 * foreground interaction task identity, operation generation, input-visible
 * phase, the shared foreground admission lock with Meeting Service, the
 * "foreground HTTP requested" projection read by the main.c HTTP lane, and
 * the capture -> upload -> submit -> processing orchestration itself.
 *
 * Command Service (timing/cancel/gesture), Reply Service (correlation) and
 * Meeting Service (admission) are reached through typed service-to-service
 * calls; the gateway transport stays in main.c (A8) behind the value-typed
 * host table.  UI presentation calls app_ui.h directly (A7 precedent).
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles or JSON objects; the worker identity crosses the boundary
 * as an opaque integer token.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Numeric order matches the legacy interaction_phase_t: input gestures are
 * interpreted from this phase, so the values are observable behaviour. */
typedef enum {
    INTERACTION_SERVICE_IDLE = 0,
    INTERACTION_SERVICE_RECORDING,
    INTERACTION_SERVICE_PROCESSING,
    INTERACTION_SERVICE_RESULT,
} interaction_service_phase_t;

/* Atomic view of the interaction worker identity for Command/Reply Service
 * correlation checks. */
typedef struct {
    bool task_active;
    bool processing;
    uint32_t generation;
} interaction_service_snapshot_t;

/* Seam towards the gateway transport still owned by main.c (A8).  Network
 * results are plain integers (0 on success). */
typedef struct {
    bool (*ensure_gateway_poll_task)(void);
    int32_t (*upload_voice)(const uint8_t *wav, uint32_t wav_len,
                            char *out_media_id, uint32_t media_id_capacity);
    int32_t (*send_voice_event)(const char *media_id, const char *event_id,
                                char *out_reply_to, uint32_t reply_to_capacity);
    /* Offline recognizer offload before capture/upload (0 on success). */
    int32_t (*wake_word_stop)(void);
    /* Cooperative cancellation of an in-flight foreground HTTP request,
     * bounded by the caller's monotonic stop deadline. */
    void (*cancel_foreground_http)(int64_t deadline_us);
    void (*log_heap_snapshot)(const char *stage);
    void (*schedule_wake_restart)(void);
} interaction_service_host_t;

/* Creates the lifecycle primitives and the shared foreground admission lock. */
device_status_t interaction_service_init(const interaction_service_host_t *host);
/* Gesture/wake entry point: runs the full admission -> capture orchestration
 * admission path and spawns the interaction worker. */
bool interaction_service_start_voice(bool physical_screen_wake);

/* Identity/state queries for the main.c HTTP lane, input binding and the
 * Command/Reply/Meeting services. */
void interaction_service_snapshot(interaction_service_snapshot_t *out_snapshot);
uint32_t interaction_service_generation(void);
interaction_service_phase_t interaction_service_phase(void);
void interaction_service_set_phase(interaction_service_phase_t phase);
bool interaction_service_worker_active(void);
bool interaction_service_current_task_is_worker(void);
bool interaction_service_foreground_http_requested(void);

/* Shared foreground admission lock with Meeting Service. */
bool interaction_service_admission_take(uint32_t timeout_ms);
void interaction_service_admission_give(void);

/* Reply Service terminal transition: atomically verifies the generation and
 * captures the waiter token, then closes the Command Service cancellation
 * window.  Returns 0 when the interaction no longer owns the generation. */
uintptr_t interaction_service_claim_reply_waiter(uint32_t generation);
void interaction_service_notify_waiter(uintptr_t waiter);
/* Command Service cancellation path: notifies the current worker, if any. */
void interaction_service_notify_worker(void);
/* Command Service cancelled-command hand-off: runs the terminal finish path
 * on the calling interaction worker (does not return for the owner). */
void interaction_service_finish_with_surface(uint32_t generation,
                                             bool restore_standby);
