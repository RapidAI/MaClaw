#pragma once

/*
 * Meeting-recorder business service.
 *
 * Owns the meeting-domain runtime state and workers that used to live in
 * main.c: the recording state machine (16 kHz/16-bit/mono streamed WAV with
 * header finalize/repair), the chunked upload state machine (chunk cursor,
 * SHA256, complete/process, delete after server acknowledgement), the
 * crash/outage recovery metadata decisions backed by
 * meeting_recovery_service, the boot/reconnect resume supervisor and the
 * on-demand capability refresh worker.
 *
 * Gateway Transport owns endpoint requests and the chunk stream.  This
 * service invokes its value-only streaming contract, so neither meeting
 * business logic nor the composition root owns an HTTP client. UI
 * presentation calls app_ui.h directly, following the
 * command_service / reply_service precedent (A7 will收口).
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles, HTTP clients or JSON objects.  Worker identity crosses
 * the boundary as an opaque integer token minted and interpreted by the
 * composition root.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_transport.h"

/* This is a value-only callback signature shared with the Device API's
 * streaming transport.  Repeating it here prevents the meeting business
 * contract from depending on a transport profile header. */
typedef gateway_transport_stream_reader_t meeting_service_recording_reader_t;

#define MEETING_SERVICE_BASE_PATH_CAPACITY 96u
#define MEETING_SERVICE_RECORDING_ID_CAPACITY 96u

/* Numeric values are part of the observable behaviour (input diagnostics log
 * the raw state); keep the original order. */
typedef enum {
    MEETING_SERVICE_IDLE = 0,
    MEETING_SERVICE_STARTING,
    MEETING_SERVICE_RECORDING,
    MEETING_SERVICE_PAUSED,
    MEETING_SERVICE_FINALIZING,
    MEETING_SERVICE_UPLOADING,
    MEETING_SERVICE_PROCESSING,
    MEETING_SERVICE_DONE,
    MEETING_SERVICE_ERROR,
} meeting_service_state_t;

/* One chunk stream request handed to the main.c transport. storage_context is
 * opaque even to the transport; read_range is the only way it can obtain WAV
 * bytes.  io_buffer is the worker's bounce buffer.  The transport keeps any
 * reusable connection privately. */
typedef struct {
    const char *recording_id;
    int32_t index;
    uint32_t offset;
    uint32_t length;
    const char *sha256_hex;
    uint32_t completed_before;
    uint32_t total_bytes;
    bool publish_progress;
    void *storage_context;
    meeting_service_recording_reader_t read_range;
    uint8_t *io_buffer;
    uint32_t io_buffer_size;
} meeting_service_chunk_upload_t;

/* Seam towards subsystems not yet extracted (gateway HTTP lane, interaction
 * admission, wake-word lifecycle).  Installed by the composition root at
 * startup.  Network results are plain integers (0 on success). */
typedef struct {
    bool (*storage_mounted)(void);
    /* Offline recognizer offload around upload (0 on success). */
    int32_t (*wake_word_stop)(void);
    int32_t (*wake_word_start)(void);
    /* Gateway meeting endpoints (0 on success). */
    int32_t (*recording_create)(char *out_recording_id, uint32_t capacity);
    int32_t (*recording_get_status)(const char *recording_id,
                                    char *out_status, uint32_t capacity);
    int32_t (*recording_post_action)(const char *recording_id,
                                     const char *action, const char *payload,
                                     int32_t expected_a, int32_t expected_b);
    bool (*capability_transport_ready)(void);
    void (*cancel_capability_http)(int64_t deadline_us);
    void (*log_heap_snapshot)(const char *stage);
    void (*schedule_wake_restart)(void);
} meeting_service_host_t;

device_status_t meeting_service_init(const meeting_service_host_t *host);
/* Boot-time recovery scan: restores the pending marker/cursor/phase only
 * when durable metadata and the retained WAV pass the same integrity policy
 * used for upload.  A marker whose WAV is definitively absent is cleared;
 * malformed/I/O-failed retained media stays preserved for diagnosis. */
void meeting_service_load_recovery(void);

/* Recording state. */
meeting_service_state_t meeting_service_state(void);
bool meeting_service_is_active(void);
/* A completed stop gesture (or the configuration escape hatch) asks the
 * recorder to finalize and deliver. */
void meeting_service_request_finalize(void);
bool meeting_service_pending(void);
bool meeting_service_available(void);
bool meeting_service_worker_running(void);
/* Foreground double-tap start; owns admission, operation context and lease. */
bool meeting_service_start_recording(void);
bool meeting_service_ensure_resume_supervisor(void);
bool meeting_service_refresh_capability(void);
/* Future system-sleep participant for the one-shot capability handshake.
 * It closes only this task's admission, stops a pre-existing refresh, and
 * records whether rollback must retry the user's interrupted refresh. It does
 * not stop/alter an active meeting recording, upload, or resume supervisor. */
device_status_t meeting_service_prepare_capability_refresh_system_sleep(uint32_t timeout_ms);
void meeting_service_abort_capability_refresh_system_sleep_prepare(void);
device_status_t meeting_service_commit_capability_refresh_network_restart(void);
/* Future system-sleep participant for the retained-meeting retry supervisor.
 * This stops only the supervisory backoff loop. An already-created recording
 * or upload worker retains its own durable/audio/transport contract and is
 * intentionally not claimed by this slice. */
device_status_t meeting_service_prepare_resume_supervisor_system_sleep(uint32_t timeout_ms);
void meeting_service_abort_resume_supervisor_system_sleep_prepare(void);
device_status_t meeting_service_commit_resume_supervisor_network_restart(void);
/* Future system-sleep participant for a retained-meeting upload pass. This
 * accepts only a resume-only worker (foreground recording owns a Power lease
 * and is rejected), cancels/joins it, and relies on the restored supervisor
 * to restart from its durable chunk cursor after rollback. */
device_status_t meeting_service_prepare_resumed_worker_system_sleep(uint32_t timeout_ms);
void meeting_service_abort_resumed_worker_system_sleep_prepare(void);
device_status_t meeting_service_commit_resumed_worker_network_restart(void);
/* Handshake capability descriptor and transport-health projection are kept as
 * separate evidence. The effective business availability is their conjunction:
 * a Hub endpoint alone cannot enable recording until the Hub explicitly
 * accepted this device's meeting capability and the Gateway projection is
 * operational. Raw descriptor values are validated here. */
bool meeting_service_set_capability_descriptor(bool advertised,
                                               const char *base_path,
                                               int32_t chunk_size,
                                               const char *process_mode);
void meeting_service_set_capability_operational(bool operational);
/* Current negotiated gateway endpoint base path (transport reads it here so
 * the capability value stays single-writer inside the service). */
void meeting_service_base_path(char *out_base_path, uint32_t capacity);

/* Worker identity queries for the main.c HTTP lane and lifecycle. */
bool meeting_service_current_task_is_worker(void);
bool meeting_service_current_task_is_capability_refresh(void);
uintptr_t meeting_service_worker_owner_token(void);
bool meeting_service_worker_stop_requested(void);
