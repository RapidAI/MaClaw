#include "audio_service.h"

#include "freertos/FreeRTOS.h"

#include "platform_audio.h"

/* Platform Audio owns the physical session and rejects conflicting I2S/codec
 * use. Audio Service adds the common DISPLAY_OFF leases required while a
 * capture or decoder streaming transaction is open. */
static portMUX_TYPE s_session_lock = portMUX_INITIALIZER_UNLOCKED;
static device_power_lease_t s_capture_lease = DEVICE_POWER_LEASE_INVALID;
static device_power_lease_t s_playback_lease = DEVICE_POWER_LEASE_INVALID;
static audio_service_session_t s_foreground_session = AUDIO_SERVICE_SESSION_IDLE;
static uint32_t s_session_generation;
static bool s_wake_word_running;
static bool s_wake_word_paused;
/* A service-owned foreground operation can temporarily pause wake-word.
 * Keep this distinct from an explicit Device API pause, which is a caller
 * policy request and must remain in force after a playback/capture finishes. */
static bool s_foreground_wake_pause_requested;
static bool s_external_wake_pause_requested;

static device_power_lease_t replace_session_lease(
    device_power_lease_t *slot, device_power_lease_t next) {
    taskENTER_CRITICAL(&s_session_lock);
    device_power_lease_t prior = *slot;
    *slot = next;
    taskEXIT_CRITICAL(&s_session_lock);
    return prior;
}

static bool begin_foreground_session(audio_service_session_t session) {
    bool accepted = false;
    taskENTER_CRITICAL(&s_session_lock);
    if (s_foreground_session == AUDIO_SERVICE_SESSION_IDLE) {
        s_foreground_session = session;
        ++s_session_generation;
        if (s_session_generation == 0) ++s_session_generation;
        accepted = true;
    }
    taskEXIT_CRITICAL(&s_session_lock);
    return accepted;
}

static void end_foreground_session(audio_service_session_t session) {
    taskENTER_CRITICAL(&s_session_lock);
    if (s_foreground_session == session) {
        s_foreground_session = AUDIO_SERVICE_SESSION_IDLE;
        ++s_session_generation;
        if (s_session_generation == 0) ++s_session_generation;
    }
    taskEXIT_CRITICAL(&s_session_lock);
}

/* Every current board adapter implements pause as a request followed by a
 * bounded acknowledgement before it takes the physical audio mutex. Mirror
 * the request here so the service snapshot is correct while the profile HAL
 * retains sole ownership of I2S hand-off timing. */
static void set_foreground_wake_pause(bool paused) {
    taskENTER_CRITICAL(&s_session_lock);
    const bool wake_running = s_wake_word_running;
    s_foreground_wake_pause_requested = paused;
    const bool effective_paused = s_foreground_wake_pause_requested ||
                                  s_external_wake_pause_requested;
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);
    if (wake_running) platform_audio_wake_word_pause(effective_paused);
}

bool audio_service_get_snapshot(audio_service_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_session_lock);
    *out_snapshot = (audio_service_snapshot_t){
        .foreground_session = s_foreground_session,
        .session_generation = s_session_generation,
        .wake_word_running = s_wake_word_running,
        .wake_word_paused = s_wake_word_paused,
    };
    taskEXIT_CRITICAL(&s_session_lock);
    return true;
}

device_status_t audio_service_set_output_volume(uint8_t percent) {
    return platform_audio_set_output_volume(percent);
}

device_status_t audio_service_adjust_output_volume(int delta_percent,
                                                   uint8_t *out_percent) {
    return platform_audio_adjust_output_volume(delta_percent, out_percent);
}

device_status_t audio_service_play_wav(const uint8_t *wav, uint32_t wav_len) {
    if (!wav || wav_len == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!begin_foreground_session(AUDIO_SERVICE_SESSION_WAV_PLAYBACK)) {
        return DEVICE_STATUS_BUSY;
    }
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_AUDIO_PLAYBACK, &lease);
    if (status != DEVICE_STATUS_OK) {
        end_foreground_session(AUDIO_SERVICE_SESSION_WAV_PLAYBACK);
        return status;
    }
    set_foreground_wake_pause(true);
    status = platform_audio_play_wav(wav, wav_len);
    set_foreground_wake_pause(false);
    device_power_lease_release(lease);
    end_foreground_session(AUDIO_SERVICE_SESSION_WAV_PLAYBACK);
    return status;
}

device_status_t audio_service_play_alarm_burst(void) {
    /* Alarm Manager owns the alarm-domain foreground lease across its entire
     * ringing policy. Audio Service still owns the generic physical-session
     * admission so an alarm burst cannot bypass PCM/capture exclusivity. */
    if (!begin_foreground_session(AUDIO_SERVICE_SESSION_ALARM_BURST)) {
        return DEVICE_STATUS_BUSY;
    }
    set_foreground_wake_pause(true);
    device_status_t status = platform_audio_play_alarm_burst();
    set_foreground_wake_pause(false);
    end_foreground_session(AUDIO_SERVICE_SESSION_ALARM_BURST);
    return status;
}

device_status_t audio_service_capture_wav(uint8_t **out_wav, uint32_t *out_len) {
    if (!out_wav || !out_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!begin_foreground_session(AUDIO_SERVICE_SESSION_COMMAND_CAPTURE)) {
        return DEVICE_STATUS_BUSY;
    }
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION, &lease);
    if (status != DEVICE_STATUS_OK) {
        end_foreground_session(AUDIO_SERVICE_SESSION_COMMAND_CAPTURE);
        return status;
    }
    set_foreground_wake_pause(true);
    status = platform_audio_capture_wav(out_wav, out_len);
    set_foreground_wake_pause(false);
    device_power_lease_release(lease);
    end_foreground_session(AUDIO_SERVICE_SESSION_COMMAND_CAPTURE);
    return status;
}

void audio_service_release_captured_wav(uint8_t *wav) {
    platform_audio_release_captured_wav(wav);
}

device_status_t audio_service_stream_start(void) {
    if (!begin_foreground_session(AUDIO_SERVICE_SESSION_MEETING_STREAM)) {
        return DEVICE_STATUS_BUSY;
    }
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_MEETING_RECORDING, &lease);
    if (status != DEVICE_STATUS_OK) {
        end_foreground_session(AUDIO_SERVICE_SESSION_MEETING_STREAM);
        return status;
    }

    /* Ask the recognizer to yield before the adapter begins its own bounded
     * pause-acknowledgement and audio-mutex transaction.  Publishing this
     * request only after stream_start() leaves a short competing-read window. */
    set_foreground_wake_pause(true);
    status = platform_audio_stream_start();
    if (status != DEVICE_STATUS_OK) {
        set_foreground_wake_pause(false);
        device_power_lease_release(lease);
        end_foreground_session(AUDIO_SERVICE_SESSION_MEETING_STREAM);
        return status;
    }

    device_power_lease_t prior = replace_session_lease(&s_capture_lease, lease);
    /* Platform Audio already rejects a second physical stream. Keep this
     * defensive replacement for the same reason as streaming playback: a
     * legacy duplicate caller cannot retain a stale display lease. */
    device_power_lease_release(prior);
    return DEVICE_STATUS_OK;
}

device_status_t audio_service_stream_read(int16_t *mono, uint32_t capacity,
                                          uint32_t *samples_read, uint16_t *level) {
    taskENTER_CRITICAL(&s_session_lock);
    const bool stream_active =
        s_foreground_session == AUDIO_SERVICE_SESSION_MEETING_STREAM &&
        s_capture_lease != DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_session_lock);
    if (!stream_active) return DEVICE_STATUS_BUSY;
    return platform_audio_stream_read(mono, capacity, samples_read, level);
}

void audio_service_stream_stop(void) {
    platform_audio_stream_stop();
    set_foreground_wake_pause(false);
    device_power_lease_release(replace_session_lease(
        &s_capture_lease, DEVICE_POWER_LEASE_INVALID));
    end_foreground_session(AUDIO_SERVICE_SESSION_MEETING_STREAM);
}

device_status_t audio_service_playback_begin(void) {
    if (!begin_foreground_session(AUDIO_SERVICE_SESSION_PCM_PLAYBACK)) {
        return DEVICE_STATUS_BUSY;
    }
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_AUDIO_PLAYBACK, &lease);
    if (status != DEVICE_STATUS_OK) {
        end_foreground_session(AUDIO_SERVICE_SESSION_PCM_PLAYBACK);
        return status;
    }

    /* Same ordering as capture: wake-word must observe the pause request
     * before an adapter attempts to claim its physical playback mutex. */
    set_foreground_wake_pause(true);
    status = platform_audio_playback_begin();
    if (status != DEVICE_STATUS_OK) {
        set_foreground_wake_pause(false);
        device_power_lease_release(lease);
        end_foreground_session(AUDIO_SERVICE_SESSION_PCM_PLAYBACK);
        return status;
    }

    device_power_lease_t prior = replace_session_lease(&s_playback_lease, lease);
    /* Platform Audio serializes the physical transaction.  Preserve the old
     * defensive duplicate-begin behavior, but make this service the sole
     * owner of the corresponding power lease. */
    device_power_lease_release(prior);
    return DEVICE_STATUS_OK;
}

device_status_t audio_service_playback_write(const int16_t *pcm, uint32_t frames,
                                             uint8_t channels) {
    taskENTER_CRITICAL(&s_session_lock);
    const bool playback_active =
        s_foreground_session == AUDIO_SERVICE_SESSION_PCM_PLAYBACK &&
        s_playback_lease != DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_session_lock);
    if (!playback_active) return DEVICE_STATUS_BUSY;
    return platform_audio_playback_write(pcm, frames, channels);
}

device_status_t audio_service_playback_end(bool playback_succeeded) {
    taskENTER_CRITICAL(&s_session_lock);
    const bool playback_active =
        s_foreground_session == AUDIO_SERVICE_SESSION_PCM_PLAYBACK &&
        s_playback_lease != DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_session_lock);
    if (!playback_active) return DEVICE_STATUS_BUSY;
    device_status_t status = platform_audio_playback_end(playback_succeeded);
    /* An end attempted by a non-owner is rejected by Platform Audio. Keep the
     * actual owner's lease intact in that case; otherwise another task could
     * make DISPLAY_OFF eligible during an active decoder transaction. */
    if (status != DEVICE_STATUS_BUSY) {
        set_foreground_wake_pause(false);
        device_power_lease_release(replace_session_lease(
            &s_playback_lease, DEVICE_POWER_LEASE_INVALID));
        end_foreground_session(AUDIO_SERVICE_SESSION_PCM_PLAYBACK);
    }
    return status;
}

void audio_service_request_playback_stop(void) {
    platform_audio_request_playback_stop();
}

void audio_service_request_capture_stop(void) {
    platform_audio_request_capture_stop();
}

void audio_service_reset_capture_stop(void) {
    platform_audio_reset_capture_stop();
}

device_status_t audio_service_wake_word_start(device_wake_word_cb_t on_wake,
                                              void *context) {
    if (!on_wake) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_session_lock);
    const bool foreground_active = s_foreground_session != AUDIO_SERVICE_SESSION_IDLE;
    taskEXIT_CRITICAL(&s_session_lock);
    if (foreground_active) return DEVICE_STATUS_BUSY;
    device_status_t status = platform_audio_wake_word_start(on_wake, context);
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_running = true;
        s_wake_word_paused = s_foreground_wake_pause_requested ||
                              s_external_wake_pause_requested;
        const bool paused = s_wake_word_paused;
        taskEXIT_CRITICAL(&s_session_lock);
        if (paused) platform_audio_wake_word_pause(true);
    }
    return status;
}

device_status_t audio_service_wake_word_stop(void) {
    device_status_t status = platform_audio_wake_word_stop();
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_running = false;
        s_wake_word_paused = false;
        taskEXIT_CRITICAL(&s_session_lock);
    }
    return status;
}

device_status_t audio_service_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    device_status_t status = platform_audio_wake_word_stop_with_timeout(timeout_ms);
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_running = false;
        s_wake_word_paused = false;
        taskEXIT_CRITICAL(&s_session_lock);
    }
    return status;
}

void audio_service_wake_word_pause(bool paused) {
    taskENTER_CRITICAL(&s_session_lock);
    s_external_wake_pause_requested = paused;
    const bool wake_running = s_wake_word_running;
    const bool effective_paused = s_foreground_wake_pause_requested ||
                                  s_external_wake_pause_requested;
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);
    if (wake_running) platform_audio_wake_word_pause(effective_paused);
}
