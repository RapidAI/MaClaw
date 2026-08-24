#pragma once

/*
 * Voice-command business service.
 *
 * Owns the command-domain runtime state that used to live in main.c:
 * cancellation flags and generations, the dedicated cancel worker and its UI
 * acknowledgement handshake, the foreground display lock, the capture-stop
 * gesture drain barrier, the post-cancel input guard, and the command timing
 * checkpoints.  The voice session orchestration (capture -> upload -> submit
 * -> processing) still runs in main.c for now because it is interleaved with
 * the gateway HTTP lane and reply polling; that orchestration publishes and
 * consumes this service's state only through the API below.
 *
 * The public contract exposes value types only.  FreeRTOS primitives, ESP-IDF
 * error codes and the interaction task handle remain private to the
 * implementation and to the composition root behind the host callback table.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Seams towards subsystems that have not been extracted yet (gateway HTTP
 * lane).  The composition root installs these at startup before the cancel
 * worker is created. */
typedef struct {
    /* Gateway seam: the protocol-level /cancel command.  send_server_cancel
     * returns the transport result as a plain integer (0 on success); the
     * service only logs it. */
    int32_t (*send_server_cancel)(const char *reply_to);
    /* Aborts any in-flight foreground HTTP request (Wi-Fi TLS lane). */
    void (*cancel_foreground_http)(void);
} command_service_host_t;

/* Creates the service synchronization primitives and installs the host seam.
 * Must succeed before command_service_start(). */
device_status_t command_service_init(const command_service_host_t *host);
/* Creates the permanent cancel worker and registers its lifecycle entry. */
device_status_t command_service_start(void);

/* Reversible System Sleep boundary for the command-cancellation coordinator.
 * PREPARE closes only new double-tap cancellation admission and refuses a
 * live/queued cancellation, whose UI, local HTTP abort and protocol /cancel
 * side effects cannot be replayed by ABORT.  The retained worker itself is
 * deliberately not stopped or recreated. */
device_status_t command_service_prepare_system_sleep(uint32_t timeout_ms);
void command_service_abort_system_sleep_prepare(void);

/* Cancellation state. */
bool command_service_cancel_requested(void);
bool command_service_cancel_requested_for(uint32_t generation);
/* True on the dedicated cancel worker task; the HTTP lane uses it to apply
 * the short cancellation request timeouts. */
bool command_service_current_task_is_cancel_worker(void);
/* Double-tap cancel admission.  Returns true when a running command accepted
 * the request and the cancel worker has been notified. */
bool command_service_request_cancel(void);
/* Runs the cancelled-command terminal flow from the interaction worker:
 * waits for the cancel UI acknowledgement, draws "已取消" if the worker
 * timed out, then hands over to the host finish path (which does not return
 * for the owning worker). */
void command_service_finish_cancelled(uint32_t generation);
void command_service_set_cancel_enabled(bool enabled);
/* Full reset at a new voice interaction start; clear at terminal/hand-off. */
void command_service_reset_cancel_state(void);
void command_service_clear_cancel_state(void);
/* Drops stale cancel-UI acknowledgements before a new command starts. */
void command_service_drain_cancel_ui_ready(void);

/* Foreground display ownership.  A foreground command (or a meeting upload
 * borrowing the same surface) holds the LCD until a final answer or explicit
 * error is displayed; background updates must not replace that flow. */
void command_service_set_display_locked(bool locked);
bool command_service_display_active(void);

/* Capture-stop gesture drain barrier.  The activation down edge stops capture
 * immediately; the completed gesture from that same physical contact is then
 * consumed within a bounded window so it cannot dismiss the new thinking
 * surface or start another command. */
void command_service_arm_capture_stop_gesture(device_input_source_t source);
/* Returns true when the completed gesture was consumed by the barrier.  A new
 * contact-down edge disarms the barrier and is admitted normally. */
bool command_service_consume_capture_stop_gesture(device_input_source_t source,
                                                  bool contact_down);
/* True while the post-cancel input guard rejects new command gestures. */
bool command_service_input_guarded(void);

/* Command timing checkpoints (monotonic microseconds are kept private). */
void command_service_timing_begin(void);
void command_service_timing_capture_done(void);
void command_service_timing_upload_done(void);
void command_service_timing_accepted(void);
/* Records the first remote progress exactly once; returns true when this call
 * performed the recording. */
bool command_service_timing_mark_first_progress(void);
uint32_t command_service_timing_accepted_to_first_progress_ms(void);
void command_service_log_timing(const char *terminal);

/* Voice upload retry policy and user-facing submit error detail.  Transport
 * errors arrive as plain integers; the mapping to SDK codes is private. */
bool command_service_voice_upload_should_retry(int32_t err, int status);
void command_service_voice_upload_retry_delay(unsigned attempt);
const char *command_service_submit_error_detail(int32_t err);
