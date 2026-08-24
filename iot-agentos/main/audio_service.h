#pragma once

/*
 * Shared audio-session owner behind the hardware-neutral Device Audio API.
 *
 * The selected Platform Audio adapter still owns codec/I2S transactions.  This
 * service owns the cross-profile foreground leases associated with capture and
 * streaming playback, so that no public Device API implementation keeps an
 * independent copy of a session owner. Command and meeting policy deliberately
 * remain above this boundary.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "fault_domain.h"

/* Internal, by-value diagnostics for the shared audio owner.  This reports
 * service intent only: codec/I2S health and DMA counters remain physical
 * adapter diagnostics below Platform Audio. */
typedef enum {
    AUDIO_SERVICE_SESSION_IDLE = 0,
    AUDIO_SERVICE_SESSION_COMMAND_CAPTURE,
    AUDIO_SERVICE_SESSION_MEETING_STREAM,
    AUDIO_SERVICE_SESSION_WAV_PLAYBACK,
    AUDIO_SERVICE_SESSION_PCM_PLAYBACK,
    AUDIO_SERVICE_SESSION_ALARM_BURST,
} audio_service_session_t;

/*
 * Cross-profile audio delivery observation.  These counters deliberately
 * describe what the common service can prove at its boundary: returned PCM
 * frames, transaction results, and unexpectedly long intervals between
 * successful service calls.  They are not a claim that an adapter has
 * measured an I2S/DMA hardware overrun or the codec's exact sample clock.
 *
 * Every Audio HAL profile has the same nominal 16 kHz/16-bit/mono capture
 * contract, so a single HIL collector can compare delivery behaviour without
 * knowing whether a board uses a direct I2S microphone or an ES7210 codec.
 */
#define AUDIO_SERVICE_SNAPSHOT_ABI_VERSION 2u
#define AUDIO_SERVICE_NOMINAL_SAMPLE_RATE_HZ 16000u

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    audio_service_session_t foreground_session;
    uint32_t session_generation;
    bool wake_word_running;
    bool wake_word_paused;
    /* The most recently completed (or current) foreground session.  It stays
     * available after the session returns to IDLE so a USB/HIL observer does
     * not need to race the terminal cleanup. */
    audio_service_session_t observed_session;
    device_status_t last_terminal_status;
    uint64_t session_started_at_us;
    uint64_t last_capture_timestamp_us;
    uint64_t captured_sample_frames;
    uint32_t capture_delivery_sequence;
    uint32_t capture_delivery_gap_count;
    uint32_t capture_timeout_count;
    uint32_t capture_error_count;
    uint64_t last_playback_timestamp_us;
    uint64_t played_sample_frames;
    uint32_t playback_delivery_sequence;
    uint32_t playback_delivery_gap_count;
    uint32_t playback_timeout_count;
    uint32_t playback_error_count;
} audio_service_snapshot_t;

/* Last acknowledged semantic output-volume state.  This is deliberately
 * owned above Platform Audio so Configuration reconciliation never needs to
 * inspect codec-specific shadows. `known` is false until a successful common
 * service call has established evidence for this boot. A failed later call
 * preserves a prior known value but updates `last_status`, which lets the
 * composition root distinguish retryable non-convergence from a value it can
 * safely use for a compensating apply. */
#define AUDIO_SERVICE_OUTPUT_VOLUME_STATE_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool known;
    uint8_t percent;
    device_status_t last_status;
} audio_service_output_volume_state_t;

bool audio_service_get_snapshot(audio_service_snapshot_t *out_snapshot);
bool audio_service_get_output_volume_state(
    audio_service_output_volume_state_t *out_state);
/* Read-only fault-domain evidence for the profile-created wake recognizer and
 * dispatcher runtime. It does not report codec/I2S, foreground media or a
 * shared I2C bus as independently restartable resources. */
bool audio_service_get_wake_runtime_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot);

device_status_t audio_service_set_output_volume(uint8_t percent);
device_status_t audio_service_adjust_output_volume(int delta_percent,
                                                   uint8_t *out_percent);
device_status_t audio_service_play_wav(const uint8_t *wav, uint32_t wav_len);
device_status_t audio_service_play_alarm_burst(void);
device_status_t audio_service_capture_wav(uint8_t **out_wav, uint32_t *out_len);
void audio_service_release_captured_wav(uint8_t *wav);
device_status_t audio_service_stream_start(void);
device_status_t audio_service_stream_read(int16_t *mono, uint32_t capacity,
                                          uint32_t *samples_read, uint16_t *level);
void audio_service_stream_stop(void);

device_status_t audio_service_playback_begin(void);
device_status_t audio_service_playback_write(const int16_t *pcm, uint32_t frames,
                                             uint8_t channels);
device_status_t audio_service_playback_end(bool playback_succeeded);
void audio_service_request_playback_stop(void);
void audio_service_request_capture_stop(void);
void audio_service_reset_capture_stop(void);

device_status_t audio_service_wake_word_start(device_wake_word_cb_t on_wake,
                                              void *context);
device_status_t audio_service_wake_word_stop(void);
/* Lifecycle-only boundary for rollback/fault-domain quiesce.  Unlike the
 * normal user-facing stop, it accepts the caller's remaining parent budget
 * and does not expose any board task or I2S detail above Audio Service. */
device_status_t audio_service_wake_word_stop_with_timeout(uint32_t timeout_ms);
void audio_service_wake_word_pause(bool paused);

/*
 * Internal system-sleep participant. It closes only new wake-word admission
 * and waits for an already-running recognizer to reach its profile-owned
 * pause safe point. Capture/playback/meeting remain protected by their
 * foreground Power leases, which the parent Power transaction fences first.
 * Once PREPARE closes admission, including a profile-acknowledgement failure,
 * only the parent Power transaction's idempotent Abort restores the prior
 * ordinary/external pause policy; neither operation enters an MCU sleep state.
 */
device_status_t audio_service_prepare_system_sleep(uint32_t timeout_ms);
void audio_service_abort_system_sleep_prepare(void);
