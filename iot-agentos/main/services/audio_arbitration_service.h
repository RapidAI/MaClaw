#pragma once

/*
 * Audio Arbitration Service (A11 first increment, SHADOW mode).
 *
 * Owns the business-layer capture/playback/alarm lease vocabulary from the
 * appendix §9 matrix.  This increment does not preempt: the physical session
 * remains the exclusive owner inside Audio Service (BUSY if not idle).
 * evaluate_appendix() records what §9 would do; observe_request() logs when
 * that differs from the live exclusive policy.  set_authoritative() is
 * reserved and defaults to false.
 *
 * Command/Meeting/Alarm/WAV/PCM/volume/stop/wake obtain audio through the
 * wrappers below.  Codec/I2S and power leases stay in Audio Service /
 * Platform Audio.  This increment still does not preempt.
 *
 * The public contract exposes value types only.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef enum {
    AUDIO_ARBITRATION_KIND_IDLE = 0,
    AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE,
    AUDIO_ARBITRATION_KIND_MEETING_STREAM,
    AUDIO_ARBITRATION_KIND_WAV_PLAYBACK,
    AUDIO_ARBITRATION_KIND_PCM_PLAYBACK,
    AUDIO_ARBITRATION_KIND_ALARM_BURST,
} audio_arbitration_kind_t;

typedef enum {
    AUDIO_ARBITRATION_GRANT = 0,
    AUDIO_ARBITRATION_BUSY,
    AUDIO_ARBITRATION_WOULD_PREEMPT,
} audio_arbitration_decision_t;

/* Live exclusive policy: GRANT only when the physical session is idle. */
static inline audio_arbitration_decision_t
audio_arbitration_evaluate_exclusive(audio_arbitration_kind_t request,
                                     audio_arbitration_kind_t current) {
    (void)request;
    return current == AUDIO_ARBITRATION_KIND_IDLE
               ? AUDIO_ARBITRATION_GRANT
               : AUDIO_ARBITRATION_BUSY;
}

/* Appendix §9 desired policy.  Meeting full-duplex is unverified, so alarm
 * versus an in-progress meeting is WOULD_PREEMPT (pause), never parallel. */
static inline audio_arbitration_decision_t
audio_arbitration_evaluate_appendix(audio_arbitration_kind_t request,
                                    audio_arbitration_kind_t current) {
    if (current == AUDIO_ARBITRATION_KIND_IDLE) return AUDIO_ARBITRATION_GRANT;
    if (request == current) return AUDIO_ARBITRATION_BUSY;
    if (request == AUDIO_ARBITRATION_KIND_ALARM_BURST &&
        (current == AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE ||
         current == AUDIO_ARBITRATION_KIND_MEETING_STREAM ||
         current == AUDIO_ARBITRATION_KIND_WAV_PLAYBACK ||
         current == AUDIO_ARBITRATION_KIND_PCM_PLAYBACK)) {
        return AUDIO_ARBITRATION_WOULD_PREEMPT;
    }
    return AUDIO_ARBITRATION_BUSY;
}

device_status_t audio_arbitration_init(void);

/* Reserved cut-over.  Default false; this increment never enables it. */
void audio_arbitration_set_authoritative(bool enabled);
bool audio_arbitration_is_authoritative(void);

/* Append-only shadow feed from Audio Service begin_foreground_session. */
void audio_arbitration_observe_request(audio_arbitration_kind_t request,
                                       audio_arbitration_kind_t current);

device_status_t audio_arbitration_play_alarm_burst(void);
device_status_t audio_arbitration_play_wav(const uint8_t *wav, uint32_t wav_len);
device_status_t audio_arbitration_playback_begin(void);
device_status_t audio_arbitration_playback_write(const int16_t *pcm, uint32_t frames,
                                                 uint8_t channels);
device_status_t audio_arbitration_playback_end(bool playback_succeeded);
device_status_t audio_arbitration_capture_wav(uint8_t **out_wav, uint32_t *out_len);
void audio_arbitration_release_captured_wav(uint8_t *wav);
device_status_t audio_arbitration_stream_start(void);
device_status_t audio_arbitration_stream_read(int16_t *mono, uint32_t capacity,
                                              uint32_t *samples_read, uint16_t *level);
void audio_arbitration_stream_stop(void);

device_status_t audio_arbitration_set_output_volume(uint8_t percent);
device_status_t audio_arbitration_adjust_output_volume(int delta_percent,
                                                       uint8_t *out_percent);
void audio_arbitration_request_playback_stop(void);
void audio_arbitration_request_capture_stop(void);
void audio_arbitration_reset_capture_stop(void);

device_status_t audio_arbitration_wake_word_start(device_wake_word_cb_t on_wake,
                                                  void *context);
device_status_t audio_arbitration_wake_word_stop(void);
device_status_t audio_arbitration_wake_word_stop_with_timeout(uint32_t timeout_ms);
void audio_arbitration_wake_word_pause(bool paused);
