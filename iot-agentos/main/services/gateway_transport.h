#pragma once

/*
 * Gateway transport service (A8 second increment).
 *
 * Owns the gateway HTTP transport infrastructure that used to live in
 * main.c: the shared request lane (Wi-Fi TLS client pools with keep-alive,
 * non-Wi-Fi transport via the Connectivity abstraction, foreground/cancel
 * lane detection and short-timeout lanes), the device identity (gateway URL,
 * bearer token, device id), the handshake, pairing (one-time code and voice
 * code) and the gateway startup coordinator worker.
 *
 * This service owns the cancellable active-client registry for its own request
 * lanes.  System-sleep and worker-stop paths request bounded cancellation
 * through value-typed APIs below; no caller receives an ESP HTTP handle.
 * Domain
 * side effects of the handshake (pet assets, ambient data, update metadata,
 * server time, tool descriptors, token persistence) likewise remain
 * composition-root concerns behind value-typed callbacks.
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles, HTTP client handles or JSON types.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"

/* Response body container returned by the request lane.  The buffer is
 * transport-allocated (PSRAM preferred); release it with
 * gateway_transport_response_release().  Field layout mirrors the legacy
 * main.c http_response_t so callers keep their original access patterns. */
typedef struct {
    char *data;
    size_t capacity;
    size_t len;
    int status;
    bool truncated;
} gateway_transport_response_t;

/* Streaming is used for a resumable meeting chunk, whose bytes remain owned
 * by Storage.  The Gateway transport owns the Wi-Fi HTTP/TLS client and the
 * selected cellular adapter, while this reader remains a value-only bridge so
 * neither transport learns a file path or a Storage handle. */
typedef device_status_t (*gateway_transport_stream_reader_t)(
    void *context, uint32_t offset, void *buffer, uint32_t requested,
    uint32_t *out_read);
typedef bool (*gateway_transport_stream_stop_probe_t)(void *context);
typedef void (*gateway_transport_stream_progress_callback_t)(
    void *context, uint32_t transferred);

typedef struct {
    const char *path;
    const char *sha256_hex;
    int32_t chunk_index;
    void *storage_context;
    gateway_transport_stream_reader_t read_range;
    uint32_t offset;
    uint32_t length;
    uint8_t *io_buffer;
    uint32_t io_buffer_size;
    gateway_transport_stream_stop_probe_t stop_requested;
    void *stop_context;
    gateway_transport_stream_progress_callback_t progress;
    void *progress_context;
} gateway_transport_stream_request_t;

/* Seam towards composition-root concerns.  Installed at startup.  Network
 * results are plain integers (0 on success); JSON nodes cross as opaque
 * context pointers. */
typedef struct {
    bool (*current_task_is_startup_pet_asset)(void);
    /* Startup coordinator domain actions. */
    bool (*start_gateway_ready_tasks)(void);
    void (*apply_deferred_startup_pet_asset)(void);
    void (*start_setup_portal)(void);
    /* Configuration evidence flows through the composition root as a boolean
     * value. Gateway Transport must not know Configuration's service API or
     * persistence representation. A staged candidate is not confirmed until
     * pairing has obtained and durably stored a Hub token. */
    bool (*staged_provisioning_pending)(void);
    /* On authoritative rejection, composition root restores the confirmed
     * snapshot and restarts before any recovery portal can expose stale
     * candidate runtime state. */
    bool (*rollback_staged_provisioning)(void);
    /* Handshake domain side effects. */
    void (*log_heap_snapshot)(const char *stage);
    void (*apply_server_time)(const void *json_node);
    void (*apply_ambient)(const void *ambient_node);
    void (*set_handshake_welcome_queued)(bool queued);
    const char *(*boot_session_id)(void);
    void (*note_cold_start_pet_asset)(const void *pet_asset_node, const char *skin);
    int32_t (*apply_pet_asset)(const void *pet_asset_node);
    int32_t (*clear_pet_asset)(void);
    void (*process_update_metadata)(const void *update_node, bool cold_start);
    bool (*append_tool_descriptors)(const void *tools_array);
    /* Pairing persistence: durably stores the token and clears the consumed
     * one-time code as one transaction (0 on success). */
    int32_t (*persist_gateway_token)(const char *token);
} gateway_transport_host_t;

device_status_t gateway_transport_init(const gateway_transport_host_t *host);

/* Shared request lane.  Results are platform error codes (0 on success). */
int32_t gateway_transport_request(const char *method, const char *path,
                                  const char *content_type,
                                  const char *body, int32_t body_len,
                                  gateway_transport_response_t *out);
/* Posts an already-serialized JSON envelope through the shared transport
 * lane. Serialization remains a domain concern; HTTP admission, bearer
 * handling, response ownership and accepted status are transport concerns. */
enum {
    GATEWAY_TRANSPORT_ACCEPT_200 = 1u << 0,
    GATEWAY_TRANSPORT_ACCEPT_202 = 1u << 1,
    GATEWAY_TRANSPORT_ACCEPT_204 = 1u << 2,
};
int32_t gateway_transport_post_json(const char *path, const char *payload,
                                    uint32_t accepted_status_mask);
int32_t gateway_transport_ack_messages(const char *payload);
int32_t gateway_transport_create_meeting(const char *base_path,
                                         char *out_recording_id,
                                         uint32_t capacity);
int32_t gateway_transport_get_meeting_status(const char *base_path,
                                             const char *recording_id,
                                             char *out_status,
                                             uint32_t capacity);
int32_t gateway_transport_post_meeting_action(const char *base_path,
                                              const char *recording_id,
                                              const char *action,
                                              const char *payload,
                                              int32_t expected_a,
                                              int32_t expected_b);
int32_t gateway_transport_request_with_capacity(const char *method, const char *path,
                                                const char *content_type,
                                                const char *body, int32_t body_len,
                                                uint32_t response_capacity,
                                                gateway_transport_response_t *out);
/* Sends a protocol text event using the transport-owned JSON envelope.  The
 * optional reply_to preserves control-message correlation (for example the
 * command cancellation path) without exposing cJSON or HTTP ownership. */
int32_t gateway_transport_send_text_event(const char *text, const char *reply_to);
/* Voice media upload and incoming-event submission share the same transport
 * admission, retry and bearer lane.  The caller supplies PCM/WAV bytes and
 * receives only value strings; cJSON and HTTP response ownership stay here. */
int32_t gateway_transport_upload_voice(const uint8_t *wav, uint32_t wav_len,
                                       char *out_media_id,
                                       uint32_t media_id_capacity);
int32_t gateway_transport_send_voice_event(const char *media_id,
                                           const char *event_id,
                                           char *out_reply_to,
                                           uint32_t reply_to_capacity);
/* Downloads a bounded media body through the transport lane. The returned
 * bytes are transport-owned and must be released with
 * gateway_transport_response_release(); no media/audio policy is applied. */
int32_t gateway_transport_download_media(const char *url,
                                         uint8_t **out_data,
                                         uint32_t *out_len);
int32_t gateway_transport_download_frame(const char *url, uint32_t expected_bytes,
                                         uint8_t **out_data, uint32_t *out_len,
                                         int32_t *out_http_status);
void gateway_transport_release_media(uint8_t *data);
void gateway_transport_response_release(gateway_transport_response_t *response);

/* Bounded cancellation for active Wi-Fi ESP HTTP requests owned by this
 * service.  `mask` is composed from the lane bits below.  A busy or failed
 * guard is fail-closed: callers must not proceed into an electrical sleep or
 * destroy a dependent worker while a borrowed client might still be live. */
typedef uint32_t gateway_transport_cancel_mask_t;
enum {
    GATEWAY_TRANSPORT_CANCEL_STARTUP = 1u << 0,
    GATEWAY_TRANSPORT_CANCEL_CAPABILITY_REFRESH = 1u << 1,
    GATEWAY_TRANSPORT_CANCEL_FOREGROUND = 1u << 2,
    GATEWAY_TRANSPORT_CANCEL_POLL = 1u << 3,
    GATEWAY_TRANSPORT_CANCEL_ASSET = 1u << 4,
    GATEWAY_TRANSPORT_CANCEL_ALL = GATEWAY_TRANSPORT_CANCEL_STARTUP |
                                   GATEWAY_TRANSPORT_CANCEL_CAPABILITY_REFRESH |
                                   GATEWAY_TRANSPORT_CANCEL_FOREGROUND |
                                   GATEWAY_TRANSPORT_CANCEL_POLL |
                                   GATEWAY_TRANSPORT_CANCEL_ASSET,
};
device_status_t gateway_transport_cancel_active_requests(
    gateway_transport_cancel_mask_t mask, uint32_t timeout_ms);
void gateway_transport_cancel_foreground_request(uint32_t timeout_ms);
void gateway_transport_cancel_capability_refresh(uint32_t timeout_ms);

/* The meeting PUT has a separate open/write/fetch transaction because its
 * body is streamed from Storage.  It is nevertheless a Gateway Transport
 * lane: active Wi-Fi client cancellation, retained keep-alive cleanup, the
 * Connectivity request admission and cellular fallback all live here. */
int32_t gateway_transport_stream_meeting_chunk(
    const gateway_transport_stream_request_t *request);
device_status_t gateway_transport_cancel_meeting_stream(
    const void *owner_token, uint32_t timeout_ms);
void gateway_transport_reset_meeting_stream(void);
bool gateway_transport_meeting_stream_ready(void);

/* Identity. */
bool gateway_transport_is_paired(void);
/* True only while this boot has an unconsumed pairing code supplied by the
 * Configuration snapshot. It exposes no credential bytes. */
bool gateway_transport_pairing_pending(void);
const char *gateway_transport_device_id(void);
void gateway_transport_set_device_id(const char *device_id);
void gateway_transport_set_gateway_credentials(const char *gateway_url,
                                               const char *gateway_token,
                                               const char *pair_code);
/* Retire the active boot credential after a destructive ownership reset has
 * delivered its final result. This clears both Credential Service state and
 * the transport's compatibility mirror; it never creates a new generation. */
void gateway_transport_revoke_credentials(void);
/* Returns the current value-only capability projection.  A successful
 * transport request alone never manufactures Hub acceptance; callers must
 * treat a false return or zero operational set as unavailable. */
bool gateway_transport_get_capability_projection(
    gateway_capability_projection_t *out_projection);
/* Returns true only when every requested capability is currently admitted by
 * the value projection. Consumers use this instead of inspecting handshake
 * JSON or profile facts. A zero request is invalid and always rejected. */
bool gateway_transport_capabilities_operational(
    gateway_capability_flags_t required_capabilities);
/* Capture and subsequently validate an operation lease without exposing the
 * mutable projection, handshake JSON, HTTP handles, or board facts to the
 * business worker. A worker must validate at every safe boundary before it
 * starts another externally visible Gateway transaction. */
bool gateway_transport_capture_capability_lease(
    gateway_capability_flags_t required_capabilities,
    gateway_capability_lease_t *out_lease);
bool gateway_transport_capability_lease_current(
    const gateway_capability_lease_t *lease);
/* Records only an authenticated, structurally valid Gateway control-plane
 * completion. This is intentionally not a generic request-health API: a
 * voice upload, media download, tool result or meeting endpoint failure must
 * never withdraw the whole negotiated surface. Handshake failure remains the
 * current negative observation source. */
void gateway_transport_observe_capability_control_plane_success(void);
/* Renders the current gateway origin / Bearer header value for callers that
 * build their own transport transactions (meeting chunk stream). */
uint32_t gateway_transport_gateway_url(char *out, uint32_t capacity);
uint32_t gateway_transport_bearer_authorization(char *out, uint32_t capacity);

/* Shared-lane access for transport users still living in main.c (meeting
 * chunk stream) and the optional-asset pool discard used by the pet domain. */
bool gateway_transport_general_lane_lock(uint32_t timeout_ms);
void gateway_transport_general_lane_unlock(void);
void gateway_transport_discard_asset_client(void);
void gateway_transport_set_gateway_url(const char *gateway_url);

/* Gateway startup coordinator worker (idempotent start). */
bool gateway_transport_start_startup_task(void);
bool gateway_transport_startup_running(void);
/* Future system-sleep lifecycle participant for the startup coordinator.
 * PREPARE stops only a coordinator that was already running and remembers
 * that fact; ABORT restarts only that remembered worker.  It neither changes
 * pairing state nor opens network admission. */
device_status_t gateway_transport_prepare_system_sleep(uint32_t timeout_ms);
void gateway_transport_abort_system_sleep_prepare(void);
/* Commits a successfully prepared terminal network restart. Unlike ABORT it
 * never recreates the pre-existing startup worker; the next physical network
 * generation must explicitly rearm Gateway after connectivity is ready. */
device_status_t gateway_transport_commit_prepared_network_restart(void);

/* Handshake and pairing (used by interaction/meeting service host seams). */
int32_t gateway_transport_handshake(bool cold_start);
int32_t gateway_transport_pair_by_voice(const uint8_t *wav, uint32_t wav_len);
