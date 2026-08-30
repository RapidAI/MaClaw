#pragma once

/*
 * Audio Arbitration Service (A11 incremental cut-over; SHADOW by default).
 *
 * Owns the business-layer capture/playback/alarm lease vocabulary from the
 * appendix §9 matrix.  Shadow remains the default; authoritative mode adds a
 * bounded, generation-fenced Alarm stop request while the displaced owner
 * retains normal cleanup ownership.
 * evaluate_appendix() records what §9 would do; observe_request() logs when
 * that differs from the live exclusive policy.  set_authoritative() is
 * reserved and defaults to false.
 *
 * Command/Meeting/Alarm/WAV/PCM/volume/stop/wake obtain audio through the
 * wrappers below.  Codec/I2S and power leases stay in Audio Service /
 * Platform Audio.  Non-alarm callers remain exclusive and return BUSY.
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

/* Pure generation fence for an authoritative Alarm interruption marker.
 * The displaced owner normally cleans up at marker_generation. Alarm is
 * composed of repeated short bursts, so the original owner can return during
 * either an Alarm foreground burst or an intervening idle gap. A non-Alarm
 * successor is excluded by Audio Service before it advances generation; the
 * numeric marker still proves the request was issued after the old owner was
 * current. */
static inline bool audio_arbitration_alarm_interruption_generation_allowed(
    uint32_t marker_generation, uint32_t current_generation,
    audio_arbitration_kind_t foreground) {
    (void)foreground;
    if (marker_generation == 0u || current_generation == 0u) return false;
    return current_generation == marker_generation;
}

/* During one whole Alarm ring transaction, foreground generation advances for
 * each short burst and the idle gaps between them.  Epoch equality is the
 * additional fence that permits the displaced owner to finish cleanup while
 * that transaction is still active; a later transaction or a new non-Alarm
 * owner can never consume the stale marker. */
static inline bool audio_arbitration_alarm_interruption_generation_allowed_scoped(
    uint32_t marker_generation, uint32_t current_generation,
    audio_arbitration_kind_t foreground, bool transaction_active,
    uint32_t marker_epoch, uint32_t transaction_epoch,
    bool recent_window, uint32_t recent_generation) {
    if (audio_arbitration_alarm_interruption_generation_allowed(
            marker_generation, current_generation, foreground)) {
        /* A numeric generation can eventually wrap.  Once a marker carries a
         * transaction epoch, equality alone is insufficient evidence: require
         * the same active/recent Alarm scope before accepting it. */
        return marker_epoch != 0u &&
               (transaction_active || recent_window) &&
               marker_epoch == transaction_epoch;
    }
    if ((!transaction_active && !recent_window) || marker_epoch == 0u ||
        transaction_epoch == 0u || marker_epoch != transaction_epoch) return false;
    if (!transaction_active) {
        return foreground == AUDIO_ARBITRATION_KIND_IDLE &&
               current_generation == recent_generation;
    }
    return foreground == AUDIO_ARBITRATION_KIND_ALARM_BURST ||
           foreground == AUDIO_ARBITRATION_KIND_IDLE;
}

typedef enum {
    AUDIO_ARBITRATION_GRANT = 0,
    AUDIO_ARBITRATION_BUSY,
    AUDIO_ARBITRATION_WOULD_PREEMPT,
} audio_arbitration_decision_t;

/* Pure §9 admission rule used by both Host regression and the runtime bridge.
 * Alarm is the only preempting requester in the current non-full-duplex
 * policy; idle/alarm-alarm and all non-authoritative paths remain inert. */
static inline bool audio_arbitration_alarm_preemption_allowed(
    audio_arbitration_kind_t request, audio_arbitration_kind_t current,
    bool authoritative) {
    if (!authoritative || request != AUDIO_ARBITRATION_KIND_ALARM_BURST ||
        current == AUDIO_ARBITRATION_KIND_IDLE ||
        current == AUDIO_ARBITRATION_KIND_ALARM_BURST) {
        return false;
    }
    return current == AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE ||
           current == AUDIO_ARBITRATION_KIND_MEETING_STREAM ||
           current == AUDIO_ARBITRATION_KIND_WAV_PLAYBACK ||
           current == AUDIO_ARBITRATION_KIND_PCM_PLAYBACK;
}

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
void audio_arbitration_alarm_transaction_begin(void);
void audio_arbitration_alarm_transaction_end(void);
device_status_t audio_arbitration_preempt_for_alarm(uint32_t timeout_ms);
bool audio_arbitration_consume_alarm_interruption(audio_arbitration_kind_t expected_kind);

device_status_t audio_arbitration_wake_word_start(device_wake_word_cb_t on_wake,
                                                  void *context);
device_status_t audio_arbitration_wake_word_stop(void);
device_status_t audio_arbitration_wake_word_stop_with_timeout(uint32_t timeout_ms);
void audio_arbitration_wake_word_pause(bool paused);
