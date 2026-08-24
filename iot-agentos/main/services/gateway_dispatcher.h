#pragma once

/*
 * Gateway downlink dispatcher (A8 first increment).
 *
 * Owns the outgoing-stream scheduling that used to live in main.c: the single
 * long-poll reader, the page cursor, per-message ACK/failed-ACK ordering and
 * the retry semantics (a message that cannot be handled is never ACKed and
 * halts the page so the tail cannot overtake it).  Message classification and
 * dispatch call the business services' typed APIs (reply correlation and
 * presentation, command timing/cancel state, tool routing).  Everything not
 * yet extracted stays behind the value-typed host table: the HTTP transport
 * (A8 second increment), startup Welcome gating, pet profile/hardware config
 * domain handlers, ambient/glyph application and server-audio playback.
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles, HTTP clients or JSON types.  Raw message nodes cross the
 * host boundary as opaque context pointers interpreted by the composition
 * root, and the poll worker identity as an opaque integer token.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"

/* welcome_classify result: how the startup Welcome gate treats a reserved
 * greeting message on this boot. */
#define GATEWAY_DISPATCHER_WELCOME_NONE 0
#define GATEWAY_DISPATCHER_WELCOME_CURRENT 1
#define GATEWAY_DISPATCHER_WELCOME_DISCARD_CURRENT 2
#define GATEWAY_DISPATCHER_WELCOME_STALE 3

typedef struct {
    /* The request lane itself is Gateway Transport (typed service calls).
     * Only its cancellation pipe stays with the composition root: bounded
     * cooperative cancellation of an in-flight poll request. */
    void (*cancel_poll_http)(int64_t deadline_us);
    /* Startup Welcome gate (startup domain in main.c). */
    bool (*welcome_gate_active)(void);
    int32_t (*welcome_classify)(const void *message_item, const char *id,
                                bool welcome_audio, bool preview_audio);
    void (*welcome_complete)(bool playback_succeeded);
    /* Domain handlers still owned by the composition root.  handled=false
     * keeps the page cursor for an ordered retry; permanently_invalid ACKs
     * the message as failed so it cannot pin the cursor. */
    int32_t (*handle_tool_call)(const void *message_item);
    void (*handle_pet_profile)(const void *message_item, const char *id,
                               bool *out_handled, bool *out_permanently_invalid);
    /* Dispatcher owns this value-only gateway authorization. The composition
     * root may fence its config/reconcile boundaries with it, but neither
     * Configuration nor board HAL may retain or interpret Gateway state. */
    void (*handle_hardware_config)(const void *extra_node,
                                   const gateway_capability_lease_t *lease,
                                   bool *out_handled, bool *out_permanently_invalid);
    void (*apply_glyphs)(const void *glyphs_node);
    void (*apply_ambient)(const void *ambient_node);
    /* Server-audio policy and playback (audio domain seam). */
    bool (*audio_url_allowed)(const char *url);
    bool (*audio_mime_supported)(const char *mime);
    bool (*audio_error_is_permanent)(int32_t err);
    bool (*begin_server_audio_wake_lease)(const char *source);
    bool (*finish_server_audio_wake_lease)(void);
    int32_t (*download_audio)(const char *url, uint8_t **out_audio, uint32_t *out_len);
    int32_t (*play_audio_payload)(const char *mime, const uint8_t *data, uint32_t len);
    void (*schedule_wake_restart)(void);
    /* Startup pet retry housekeeping drained by the poll worker loop. */
    bool (*take_startup_pet_retry_due)(void);
    void (*apply_deferred_startup_pet_asset)(void);
} gateway_dispatcher_host_t;

device_status_t gateway_dispatcher_init(const gateway_dispatcher_host_t *host);
/* Creates/registers the single outgoing long-poll worker (idempotent). */
bool gateway_dispatcher_ensure_poll_task(void);
/* Poll worker identity for the main.c HTTP lane. */
bool gateway_dispatcher_current_task_is_poll_worker(void);
/* Future system-sleep lifecycle participant for the outgoing long-poll
 * worker. PREPARE records and stops only a pre-existing worker; ABORT
 * recreates only that worker and leaves network admission to Connectivity. */
device_status_t gateway_dispatcher_prepare_system_sleep(uint32_t timeout_ms);
void gateway_dispatcher_abort_system_sleep_prepare(void);
/* Terminal counterpart of PREPARE for a physical network restart. It drops
 * the remembered poll generation rather than re-creating it. */
device_status_t gateway_dispatcher_commit_prepared_network_restart(void);
