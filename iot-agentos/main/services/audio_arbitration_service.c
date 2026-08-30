#include "services/audio_arbitration_service.h"

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "audio_service.h"

/* New shadow diagnostics only; existing capture/alarm traces stay on their
 * original tags. */
static const char *TAG = "audio_arb";

static portMUX_TYPE s_arb_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_authoritative;
static bool s_divergence_open;
static int s_last_request = -1;
static int s_last_current = -1;
static int s_last_exclusive = -1;
static int s_last_appendix = -1;

device_status_t audio_arbitration_init(void) {
    s_authoritative = false;
    s_divergence_open = false;
    s_last_request = -1;
    s_last_current = -1;
    s_last_exclusive = -1;
    s_last_appendix = -1;
    return DEVICE_STATUS_OK;
}

void audio_arbitration_set_authoritative(bool enabled) {
    taskENTER_CRITICAL(&s_arb_lock);
    s_authoritative = enabled;
    taskEXIT_CRITICAL(&s_arb_lock);
}

bool audio_arbitration_is_authoritative(void) {
    taskENTER_CRITICAL(&s_arb_lock);
    bool enabled = s_authoritative;
    taskEXIT_CRITICAL(&s_arb_lock);
    return enabled;
}

void audio_arbitration_observe_request(audio_arbitration_kind_t request,
                                       audio_arbitration_kind_t current) {
    const audio_arbitration_decision_t exclusive =
        audio_arbitration_evaluate_exclusive(request, current);
    const audio_arbitration_decision_t appendix =
        audio_arbitration_evaluate_appendix(request, current);
    taskENTER_CRITICAL(&s_arb_lock);
    bool changed = !s_divergence_open ||
                   s_last_request != (int)request ||
                   s_last_current != (int)current ||
                   s_last_exclusive != (int)exclusive ||
                   s_last_appendix != (int)appendix;
    if (exclusive == appendix) {
        bool was_open = s_divergence_open;
        s_divergence_open = false;
        taskEXIT_CRITICAL(&s_arb_lock);
        if (was_open) {
            ESP_LOGD(TAG, "audio shadow converged: request=%d current=%d decision=%d",
                     (int)request, (int)current, (int)exclusive);
        }
        return;
    }
    if (changed) {
        s_divergence_open = true;
        s_last_request = (int)request;
        s_last_current = (int)current;
        s_last_exclusive = (int)exclusive;
        s_last_appendix = (int)appendix;
    }
    taskEXIT_CRITICAL(&s_arb_lock);
    if (changed) {
        ESP_LOGI(TAG,
                 "audio shadow divergence: request=%d current=%d exclusive=%d appendix=%d",
                 (int)request, (int)current, (int)exclusive, (int)appendix);
    }
}

device_status_t audio_arbitration_play_alarm_burst(void) {
    audio_service_snapshot_t snapshot = {0};
    (void)audio_service_get_snapshot(&snapshot);
    audio_arbitration_kind_t current = AUDIO_ARBITRATION_KIND_IDLE;
    switch (snapshot.foreground_session) {
        case AUDIO_SERVICE_SESSION_COMMAND_CAPTURE:
            current = AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE; break;
        case AUDIO_SERVICE_SESSION_MEETING_STREAM:
            current = AUDIO_ARBITRATION_KIND_MEETING_STREAM; break;
        case AUDIO_SERVICE_SESSION_WAV_PLAYBACK:
            current = AUDIO_ARBITRATION_KIND_WAV_PLAYBACK; break;
        case AUDIO_SERVICE_SESSION_PCM_PLAYBACK:
            current = AUDIO_ARBITRATION_KIND_PCM_PLAYBACK; break;
        case AUDIO_SERVICE_SESSION_ALARM_BURST:
            current = AUDIO_ARBITRATION_KIND_ALARM_BURST; break;
        default: break;
    }
    if (audio_arbitration_alarm_preemption_allowed(
            AUDIO_ARBITRATION_KIND_ALARM_BURST, current,
            audio_arbitration_is_authoritative())) {
        const device_status_t preempt = audio_service_preempt_for_alarm(300u);
        if (preempt != DEVICE_STATUS_OK) return preempt;
    }
    return audio_service_play_alarm_burst();
}

device_status_t audio_arbitration_play_wav(const uint8_t *wav, uint32_t wav_len) {
    return audio_service_play_wav(wav, wav_len);
}

device_status_t audio_arbitration_playback_begin(void) {
    return audio_service_playback_begin();
}

device_status_t audio_arbitration_playback_write(const int16_t *pcm, uint32_t frames,
                                                 uint8_t channels) {
    return audio_service_playback_write(pcm, frames, channels);
}

device_status_t audio_arbitration_playback_end(bool playback_succeeded) {
    return audio_service_playback_end(playback_succeeded);
}

device_status_t audio_arbitration_capture_wav(uint8_t **out_wav, uint32_t *out_len) {
    return audio_service_capture_wav(out_wav, out_len);
}

void audio_arbitration_release_captured_wav(uint8_t *wav) {
    audio_service_release_captured_wav(wav);
}

device_status_t audio_arbitration_stream_start(void) {
    return audio_service_stream_start();
}

device_status_t audio_arbitration_stream_read(int16_t *mono, uint32_t capacity,
                                              uint32_t *samples_read, uint16_t *level) {
    return audio_service_stream_read(mono, capacity, samples_read, level);
}

void audio_arbitration_stream_stop(void) {
    audio_service_stream_stop();
}

device_status_t audio_arbitration_set_output_volume(uint8_t percent) {
    return audio_service_set_output_volume(percent);
}

device_status_t audio_arbitration_adjust_output_volume(int delta_percent,
                                                       uint8_t *out_percent) {
    return audio_service_adjust_output_volume(delta_percent, out_percent);
}

void audio_arbitration_request_playback_stop(void) {
    audio_service_request_playback_stop();
}

void audio_arbitration_request_capture_stop(void) {
    audio_service_request_capture_stop();
}

void audio_arbitration_reset_capture_stop(void) {
    audio_service_reset_capture_stop();
}

void audio_arbitration_alarm_transaction_begin(void) {
    audio_service_alarm_transaction_begin();
}

void audio_arbitration_alarm_transaction_end(void) {
    audio_service_alarm_transaction_end();
}

device_status_t audio_arbitration_preempt_for_alarm(uint32_t timeout_ms) {
    if (!audio_arbitration_is_authoritative()) return DEVICE_STATUS_BUSY;
    return audio_service_preempt_for_alarm(timeout_ms);
}

bool audio_arbitration_consume_alarm_interruption(audio_arbitration_kind_t expected_kind) {
    audio_service_session_t expected_session = AUDIO_SERVICE_SESSION_IDLE;
    switch (expected_kind) {
        case AUDIO_ARBITRATION_KIND_COMMAND_CAPTURE:
            expected_session = AUDIO_SERVICE_SESSION_COMMAND_CAPTURE; break;
        case AUDIO_ARBITRATION_KIND_MEETING_STREAM:
            expected_session = AUDIO_SERVICE_SESSION_MEETING_STREAM; break;
        case AUDIO_ARBITRATION_KIND_WAV_PLAYBACK:
            expected_session = AUDIO_SERVICE_SESSION_WAV_PLAYBACK; break;
        case AUDIO_ARBITRATION_KIND_PCM_PLAYBACK:
            expected_session = AUDIO_SERVICE_SESSION_PCM_PLAYBACK; break;
        default: return false;
    }
    return audio_service_consume_alarm_interruption((int)expected_session);
}

device_status_t audio_arbitration_wake_word_start(device_wake_word_cb_t on_wake,
                                                  void *context) {
    return audio_service_wake_word_start(on_wake, context);
}

device_status_t audio_arbitration_wake_word_stop(void) {
    return audio_service_wake_word_stop();
}

device_status_t audio_arbitration_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    return audio_service_wake_word_stop_with_timeout(timeout_ms);
}

void audio_arbitration_wake_word_pause(bool paused) {
    audio_service_wake_word_pause(paused);
}
