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

typedef struct {
    audio_service_session_t foreground_session;
    uint32_t session_generation;
    bool wake_word_running;
    bool wake_word_paused;
} audio_service_snapshot_t;

bool audio_service_get_snapshot(audio_service_snapshot_t *out_snapshot);

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
