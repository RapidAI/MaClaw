#include "audio_service.h"

#include "freertos/FreeRTOS.h"
#include "esp_timer.h"

#include "presentation/scene_presenter.h"
#include "platform_audio.h"
#include "services/audio_arbitration_service.h"
#include "fault_domain.h"

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
/* A profile start may allocate a recognizer/dispatcher outside s_session_lock.
 * Make that in-flight transaction visible to System Sleep before it can close
 * its admission fence; Power then fails the wider transaction rather than
 * returning "safe" while a new physical runtime is about to appear. */
static bool s_wake_word_starting;
/* A service-owned foreground operation can temporarily pause wake-word.
 * Keep this distinct from an explicit Device API pause, which is a caller
 * policy request and must remain in force after a playback/capture finishes. */
static bool s_foreground_wake_pause_requested;
static bool s_external_wake_pause_requested;
/* System-sleep PREPARE is a third, transaction-scoped pause owner. It must
 * not overwrite an ordinary caller's external pause preference because a
 * rollback must return to precisely that policy rather than always starting
 * wake-word after a rejected sleep request. */
static bool s_system_sleep_wake_pause_requested;
/* This domain deliberately covers only the runtime created by
 * platform_audio_wake_word_start(): recognizer/dispatcher admission and its
 * confirmed start/stop boundary.  Codec/I2S, capture/playback and any shared
 * round-board I2C lifecycle remain profile-owned physical resources and are
 * not claimed restartable by this value-only record. */
static fault_domain_t s_audio_wake_runtime_fault_domain = {
    .struct_size = sizeof(fault_domain_t),
    .abi_version = FAULT_DOMAIN_ABI_VERSION,
    .id = FAULT_DOMAIN_ID_AUDIO,
    .phase = FAULT_DOMAIN_STOPPED,
    .generation = 1u,
};

/* This is intentionally generous.  A service caller can spend time writing a
 * meeting chunk or decoding an MP3 between two Audio API calls; that is useful
 * HIL evidence of an audio-delivery stall, but it must not be relabelled as a
 * physical DMA overrun without adapter-specific evidence. */
#define AUDIO_SERVICE_DELIVERY_GAP_TOLERANCE_US 200000LL

static audio_service_session_t s_observed_session = AUDIO_SERVICE_SESSION_IDLE;
static device_status_t s_last_terminal_status = DEVICE_STATUS_OK;
static int64_t s_session_started_at_us;
static int64_t s_last_capture_timestamp_us;
static uint64_t s_captured_sample_frames;
static uint32_t s_capture_delivery_sequence;
static uint32_t s_capture_delivery_gap_count;
static uint32_t s_capture_timeout_count;
static uint32_t s_capture_error_count;
static uint32_t s_previous_capture_frames;
static int64_t s_last_playback_timestamp_us;
static uint64_t s_played_sample_frames;
static uint32_t s_playback_delivery_sequence;
static uint32_t s_playback_delivery_gap_count;
static uint32_t s_playback_timeout_count;
static uint32_t s_playback_error_count;
static uint32_t s_previous_playback_frames;
static uint32_t s_capture_progress_last_elapsed_seconds = UINT32_MAX;
/* This is a service-boundary acknowledgement, not a read of mixer registers.
 * The selected Platform Audio adapter remains the sole physical codec owner. */
static bool s_output_volume_known;
static uint8_t s_output_volume_percent;
static device_status_t s_output_volume_last_status = DEVICE_STATUS_UNAVAILABLE;

static bool audio_service_wake_runtime_mark_stopped(void) {
    if (fault_domain_mark_stopped(&s_audio_wake_runtime_fault_domain)) return true;
    return fault_domain_begin_quiesce(&s_audio_wake_runtime_fault_domain) &&
           fault_domain_mark_stopped(&s_audio_wake_runtime_fault_domain);
}

static void audio_service_reset_delivery_metrics_locked(audio_service_session_t session,
                                                        int64_t started_at_us) {
    s_observed_session = session;
    s_last_terminal_status = DEVICE_STATUS_OK;
    s_session_started_at_us = started_at_us;
    s_last_capture_timestamp_us = 0;
    s_captured_sample_frames = 0;
    s_capture_delivery_sequence = 0;
    s_capture_delivery_gap_count = 0;
    s_capture_timeout_count = 0;
    s_capture_error_count = 0;
    s_previous_capture_frames = 0;
    s_last_playback_timestamp_us = 0;
    s_played_sample_frames = 0;
    s_playback_delivery_sequence = 0;
    s_playback_delivery_gap_count = 0;
    s_playback_timeout_count = 0;
    s_playback_error_count = 0;
    s_previous_playback_frames = 0;
}

static void audio_service_note_delivery_result_locked(device_status_t status,
                                                      bool capture) {
    s_last_terminal_status = status;
    if (status == DEVICE_STATUS_OK) return;
    if (capture) {
        if (status == DEVICE_STATUS_TIMEOUT) ++s_capture_timeout_count;
        else ++s_capture_error_count;
    } else {
        if (status == DEVICE_STATUS_TIMEOUT) ++s_playback_timeout_count;
        else ++s_playback_error_count;
    }
}

static bool audio_service_delivery_gap(int64_t previous_timestamp_us,
                                       uint32_t previous_frames,
                                       int64_t current_timestamp_us) {
    if (previous_timestamp_us <= 0 || previous_frames == 0 ||
        current_timestamp_us <= previous_timestamp_us) {
        return false;
    }
    const int64_t expected_us = ((int64_t)previous_frames * 1000000LL) /
                                AUDIO_SERVICE_NOMINAL_SAMPLE_RATE_HZ;
    return current_timestamp_us - previous_timestamp_us >
           expected_us + AUDIO_SERVICE_DELIVERY_GAP_TOLERANCE_US;
}

static void audio_service_note_capture_delivery(uint32_t frames,
                                                device_status_t status,
                                                int64_t completed_at_us) {
    taskENTER_CRITICAL(&s_session_lock);
    audio_service_note_delivery_result_locked(status, true);
    if (status == DEVICE_STATUS_OK && frames > 0) {
        if (audio_service_delivery_gap(s_last_capture_timestamp_us,
                                       s_previous_capture_frames,
                                       completed_at_us)) {
            ++s_capture_delivery_gap_count;
        }
        s_last_capture_timestamp_us = completed_at_us;
        s_previous_capture_frames = frames;
        s_captured_sample_frames += frames;
        ++s_capture_delivery_sequence;
        if (s_capture_delivery_sequence == 0) ++s_capture_delivery_sequence;
    }
    taskEXIT_CRITICAL(&s_session_lock);
}

static void audio_service_note_playback_delivery(uint32_t frames,
                                                 device_status_t status,
                                                 int64_t completed_at_us) {
    taskENTER_CRITICAL(&s_session_lock);
    audio_service_note_delivery_result_locked(status, false);
    if (status == DEVICE_STATUS_OK && frames > 0) {
        if (audio_service_delivery_gap(s_last_playback_timestamp_us,
                                       s_previous_playback_frames,
                                       completed_at_us)) {
            ++s_playback_delivery_gap_count;
        }
        s_last_playback_timestamp_us = completed_at_us;
        s_previous_playback_frames = frames;
        s_played_sample_frames += frames;
        ++s_playback_delivery_sequence;
        if (s_playback_delivery_sequence == 0) ++s_playback_delivery_sequence;
    }
    taskEXIT_CRITICAL(&s_session_lock);
}

static device_power_lease_t replace_session_lease(
    device_power_lease_t *slot, device_power_lease_t next) {
    taskENTER_CRITICAL(&s_session_lock);
    device_power_lease_t prior = *slot;
    *slot = next;
    taskEXIT_CRITICAL(&s_session_lock);
    return prior;
}

static audio_arbitration_kind_t kind_from_session(audio_service_session_t session) {
    switch (session) {
        case AUDIO_SERVICE_SESSION_COMMAND_CAPTURE:
            return AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE;
        case AUDIO_SERVICE_SESSION_MEETING_STREAM:
            return AUDIO_ARBITRATION_KIND_MEETING_STREAM;
        case AUDIO_SERVICE_SESSION_WAV_PLAYBACK:
            return AUDIO_ARBITRATION_KIND_WAV_PLAYBACK;
        case AUDIO_SERVICE_SESSION_PCM_PLAYBACK:
            return AUDIO_ARBITRATION_KIND_PCM_PLAYBACK;
        case AUDIO_SERVICE_SESSION_ALARM_BURST:
            return AUDIO_ARBITRATION_KIND_ALARM_BURST;
        case AUDIO_SERVICE_SESSION_IDLE:
        default:
            return AUDIO_ARBITRATION_KIND_IDLE;
    }
}

static bool begin_foreground_session(audio_service_session_t session) {
    bool accepted = false;
    const int64_t started_at_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_session_lock);
    const audio_arbitration_kind_t current_kind = kind_from_session(s_foreground_session);
    const audio_arbitration_kind_t request_kind = kind_from_session(session);
    taskEXIT_CRITICAL(&s_session_lock);
    audio_arbitration_observe_request(request_kind, current_kind);
    taskENTER_CRITICAL(&s_session_lock);
    if (s_foreground_session == AUDIO_SERVICE_SESSION_IDLE) {
        s_foreground_session = session;
        ++s_session_generation;
        if (s_session_generation == 0) ++s_session_generation;
        audio_service_reset_delivery_metrics_locked(session, started_at_us);
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
                                  s_external_wake_pause_requested ||
                                  s_system_sleep_wake_pause_requested;
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);
    if (wake_running) platform_audio_wake_word_pause(effective_paused);
}

static bool audio_service_effective_wake_pause_locked(void) {
    return s_foreground_wake_pause_requested ||
           s_external_wake_pause_requested ||
           s_system_sleep_wake_pause_requested;
}

static void audio_service_capture_progress(void *context, uint16_t level,
                                           uint32_t elapsed_seconds) {
    (void)context;
    /* Platform Audio owns physical sample conditioning and only publishes a
     * normalized meter observation.  The shared App UI remains the sole
     * business/display-policy owner for recording surfaces. */
    scene_presenter_publish_audio_level(level, elapsed_seconds);
    /* Preserve Bread's established rendering contract: the meter follows each
     * normalized capture block, while the heavier elapsed-time surface only
     * commits when its visible second changes. */
    if (elapsed_seconds != s_capture_progress_last_elapsed_seconds) {
        scene_presenter_publish_recording_visual(true, false, elapsed_seconds);
        s_capture_progress_last_elapsed_seconds = elapsed_seconds;
    }
}

bool audio_service_get_snapshot(audio_service_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_session_lock);
    *out_snapshot = (audio_service_snapshot_t){
        .struct_size = sizeof(audio_service_snapshot_t),
        .abi_version = AUDIO_SERVICE_SNAPSHOT_ABI_VERSION,
        .foreground_session = s_foreground_session,
        .session_generation = s_session_generation,
        .wake_word_running = s_wake_word_running,
        .wake_word_paused = s_wake_word_paused,
        .observed_session = s_observed_session,
        .last_terminal_status = s_last_terminal_status,
        .session_started_at_us = s_session_started_at_us > 0
                                     ? (uint64_t)s_session_started_at_us : 0,
        .last_capture_timestamp_us = s_last_capture_timestamp_us > 0
                                         ? (uint64_t)s_last_capture_timestamp_us : 0,
        .captured_sample_frames = s_captured_sample_frames,
        .capture_delivery_sequence = s_capture_delivery_sequence,
        .capture_delivery_gap_count = s_capture_delivery_gap_count,
        .capture_timeout_count = s_capture_timeout_count,
        .capture_error_count = s_capture_error_count,
        .last_playback_timestamp_us = s_last_playback_timestamp_us > 0
                                          ? (uint64_t)s_last_playback_timestamp_us : 0,
        .played_sample_frames = s_played_sample_frames,
        .playback_delivery_sequence = s_playback_delivery_sequence,
        .playback_delivery_gap_count = s_playback_delivery_gap_count,
        .playback_timeout_count = s_playback_timeout_count,
        .playback_error_count = s_playback_error_count,
    };
    taskEXIT_CRITICAL(&s_session_lock);
    return true;
}

bool audio_service_get_output_volume_state(
    audio_service_output_volume_state_t *out_state) {
    if (!out_state) return false;
    taskENTER_CRITICAL(&s_session_lock);
    *out_state = (audio_service_output_volume_state_t){
        .struct_size = sizeof(*out_state),
        .abi_version = AUDIO_SERVICE_OUTPUT_VOLUME_STATE_ABI_VERSION,
        .known = s_output_volume_known,
        .percent = s_output_volume_percent,
        .last_status = s_output_volume_last_status,
    };
    taskEXIT_CRITICAL(&s_session_lock);
    return true;
}

bool audio_service_get_wake_runtime_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot) {
    return fault_domain_get_snapshot(&s_audio_wake_runtime_fault_domain, out_snapshot);
}

device_status_t audio_service_set_output_volume(uint8_t percent) {
    const device_status_t status = platform_audio_set_output_volume(percent);
    taskENTER_CRITICAL(&s_session_lock);
    s_output_volume_last_status = status;
    if (status == DEVICE_STATUS_OK) {
        s_output_volume_percent = percent;
        s_output_volume_known = true;
    }
    taskEXIT_CRITICAL(&s_session_lock);
    return status;
}

device_status_t audio_service_adjust_output_volume(int delta_percent,
                                                   uint8_t *out_percent) {
    uint8_t adjusted = 0u;
    uint8_t *result = out_percent ? out_percent : &adjusted;
    const device_status_t status = platform_audio_adjust_output_volume(delta_percent, result);
    taskENTER_CRITICAL(&s_session_lock);
    s_output_volume_last_status = status;
    if (status == DEVICE_STATUS_OK) {
        s_output_volume_percent = *result;
        s_output_volume_known = true;
    }
    taskEXIT_CRITICAL(&s_session_lock);
    return status;
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
    /* WAV is the normalized local-device contract.  Only account complete
     * sample frames when its standard 44-byte header is present; malformed
     * payloads still retain their terminal status for diagnosis. */
    const uint32_t frames = wav_len >= 44u ? (wav_len - 44u) / 2u : 0u;
    audio_service_note_playback_delivery(frames, status, esp_timer_get_time());
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
    audio_service_note_playback_delivery(0, status, esp_timer_get_time());
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
    s_capture_progress_last_elapsed_seconds = UINT32_MAX;
    status = platform_audio_capture_wav(out_wav, out_len,
                                        audio_service_capture_progress, NULL);
    const uint32_t frames = status == DEVICE_STATUS_OK && out_len && *out_len >= 44u
                                ? (*out_len - 44u) / 2u : 0u;
    audio_service_note_capture_delivery(frames, status, esp_timer_get_time());
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
    device_status_t status = platform_audio_stream_read(mono, capacity, samples_read, level);
    const uint32_t frames = status == DEVICE_STATUS_OK && samples_read ? *samples_read : 0;
    audio_service_note_capture_delivery(frames, status, esp_timer_get_time());
    return status;
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
    device_status_t status = platform_audio_playback_write(pcm, frames, channels);
    audio_service_note_playback_delivery(frames, status, esp_timer_get_time());
    return status;
}

device_status_t audio_service_playback_end(bool playback_succeeded) {
    taskENTER_CRITICAL(&s_session_lock);
    const bool playback_active =
        s_foreground_session == AUDIO_SERVICE_SESSION_PCM_PLAYBACK &&
        s_playback_lease != DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_session_lock);
    if (!playback_active) return DEVICE_STATUS_BUSY;
    device_status_t status = platform_audio_playback_end(playback_succeeded);
    audio_service_note_playback_delivery(0, status, esp_timer_get_time());
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
    const bool system_sleep_preparing = s_system_sleep_wake_pause_requested;
    const bool wake_starting = s_wake_word_starting;
    if (!foreground_active && !system_sleep_preparing && !wake_starting) {
        s_wake_word_starting = true;
    }
    taskEXIT_CRITICAL(&s_session_lock);
    if (foreground_active || system_sleep_preparing || wake_starting) {
        return DEVICE_STATUS_BUSY;
    }
    if (!fault_domain_begin_start(&s_audio_wake_runtime_fault_domain)) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_starting = false;
        taskEXIT_CRITICAL(&s_session_lock);
        return DEVICE_STATUS_BUSY;
    }
    device_status_t status = platform_audio_wake_word_start(on_wake, context);
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_starting = false;
        s_wake_word_running = true;
        s_wake_word_paused = audio_service_effective_wake_pause_locked();
        const bool paused = s_wake_word_paused;
        taskEXIT_CRITICAL(&s_session_lock);
        if (paused) platform_audio_wake_word_pause(true);
        if (fault_domain_begin_self_test(&s_audio_wake_runtime_fault_domain) &&
            fault_domain_mark_ready(&s_audio_wake_runtime_fault_domain)) {
            return DEVICE_STATUS_OK;
        }
        /* Profile runtime is live, but common lifecycle publication failed.
         * Keep future starts closed and require the existing bounded stop
         * transaction; do not create a second recognizer generation. */
        (void)fault_domain_mark_unknown_outcome(&s_audio_wake_runtime_fault_domain);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* A profile start may fail after a task/dispatcher allocation.  The
     * common service has no physical observation that proves otherwise, so a
     * retry must first use the existing bounded stop/cleanup path. */
    taskENTER_CRITICAL(&s_session_lock);
    s_wake_word_starting = false;
    taskEXIT_CRITICAL(&s_session_lock);
    (void)fault_domain_mark_unknown_outcome(&s_audio_wake_runtime_fault_domain);
    return status;
}

device_status_t audio_service_wake_word_stop(void) {
    fault_domain_snapshot_t snapshot;
    if (fault_domain_get_snapshot(&s_audio_wake_runtime_fault_domain, &snapshot) &&
        snapshot.phase == FAULT_DOMAIN_STOPPED) {
        /* Preserve the legacy no-runtime result from the profile instead of
         * reclassifying an ordinary redundant stop as a domain error. */
        return platform_audio_wake_word_stop();
    }
    if (!fault_domain_begin_quiesce(&s_audio_wake_runtime_fault_domain)) {
        return DEVICE_STATUS_BUSY;
    }
    device_status_t status = platform_audio_wake_word_stop();
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_running = false;
        s_wake_word_paused = false;
        taskEXIT_CRITICAL(&s_session_lock);
        return audio_service_wake_runtime_mark_stopped()
                   ? DEVICE_STATUS_OK : DEVICE_STATUS_INTERNAL_ERROR;
    }
    (void)fault_domain_mark_unknown_outcome(&s_audio_wake_runtime_fault_domain);
    return status;
}

device_status_t audio_service_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    fault_domain_snapshot_t snapshot;
    if (fault_domain_get_snapshot(&s_audio_wake_runtime_fault_domain, &snapshot) &&
        snapshot.phase == FAULT_DOMAIN_STOPPED) {
        return platform_audio_wake_word_stop_with_timeout(timeout_ms);
    }
    if (!fault_domain_begin_quiesce(&s_audio_wake_runtime_fault_domain)) {
        return DEVICE_STATUS_BUSY;
    }
    device_status_t status = platform_audio_wake_word_stop_with_timeout(timeout_ms);
    if (status == DEVICE_STATUS_OK) {
        taskENTER_CRITICAL(&s_session_lock);
        s_wake_word_running = false;
        s_wake_word_paused = false;
        taskEXIT_CRITICAL(&s_session_lock);
        return audio_service_wake_runtime_mark_stopped()
                   ? DEVICE_STATUS_OK : DEVICE_STATUS_INTERNAL_ERROR;
    }
    (void)fault_domain_mark_unknown_outcome(&s_audio_wake_runtime_fault_domain);
    return status;
}

void audio_service_wake_word_pause(bool paused) {
    taskENTER_CRITICAL(&s_session_lock);
    s_external_wake_pause_requested = paused;
    const bool wake_running = s_wake_word_running;
    const bool effective_paused = audio_service_effective_wake_pause_locked();
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);
    if (wake_running) platform_audio_wake_word_pause(effective_paused);
}

device_status_t audio_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_session_lock);
    if (s_foreground_session != AUDIO_SERVICE_SESSION_IDLE || s_wake_word_starting ||
        s_system_sleep_wake_pause_requested) {
        taskEXIT_CRITICAL(&s_session_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_wake_pause_requested = true;
    const bool wake_running = s_wake_word_running;
    const bool effective_paused = audio_service_effective_wake_pause_locked();
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);

    /* A stopped recognizer is already at a safe point. Keep the admission
     * marker nonetheless: it prevents a late start from crossing Power's
     * system-sleep PREPARE fence before rollback/commit decides the outcome. */
    if (!wake_running) return DEVICE_STATUS_OK;
    const device_status_t status =
        platform_audio_wake_word_pause_with_ack(effective_paused, timeout_ms);
    if (status == DEVICE_STATUS_OK) return DEVICE_STATUS_OK;

    /* Keep wake-word admission paused after a failed acknowledgement. The
     * parent Power transaction is the only rollback owner; reopening here
     * could resume profile audio work while a later participant remains in
     * PREPARE. Its reverse-order ABORT restores the pre-existing policy. */
    return status;
}

void audio_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_session_lock);
    if (!s_system_sleep_wake_pause_requested) {
        taskEXIT_CRITICAL(&s_session_lock);
        return;
    }
    s_system_sleep_wake_pause_requested = false;
    const bool wake_running = s_wake_word_running;
    const bool effective_paused = audio_service_effective_wake_pause_locked();
    if (wake_running) s_wake_word_paused = effective_paused;
    taskEXIT_CRITICAL(&s_session_lock);
    if (wake_running) platform_audio_wake_word_pause(effective_paused);
}
