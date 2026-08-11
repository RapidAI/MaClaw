#include "platform_audio.h"

#include <limits.h>

#include "board_port.h"

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_FAIL: return DEVICE_STATUS_IO_ERROR;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t platform_audio_set_output_volume(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_set_output_volume(percent));
}

device_status_t platform_audio_adjust_output_volume(int delta_percent,
                                                    uint8_t *out_percent) {
    unsigned applied = 0;
    device_status_t status = status_from_esp_err(
        board_port_adjust_output_volume(delta_percent, &applied));
    if (status != DEVICE_STATUS_OK) return status;
    if (applied > 100) return DEVICE_STATUS_INTERNAL_ERROR;
    if (out_percent) *out_percent = (uint8_t)applied;
    return DEVICE_STATUS_OK;
}

device_status_t platform_audio_play_wav(const uint8_t *wav, uint32_t wav_len) {
    if (!wav || wav_len == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_play_wav(wav, wav_len));
}

device_status_t platform_audio_play_alarm_burst(void) {
    return status_from_esp_err(board_port_play_alarm_burst());
}

device_status_t platform_audio_capture_wav(uint8_t **out_wav, uint32_t *out_len) {
    if (!out_wav || !out_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_wav = NULL;
    *out_len = 0;
    size_t length = 0;
    device_status_t status = status_from_esp_err(board_port_capture_wav(out_wav, &length));
    if (status != DEVICE_STATUS_OK) {
        board_port_release_captured_wav(*out_wav);
        *out_wav = NULL;
        return status;
    }
    if (!*out_wav || length == 0 || length > UINT32_MAX) {
        board_port_release_captured_wav(*out_wav);
        *out_wav = NULL;
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    *out_len = (uint32_t)length;
    return DEVICE_STATUS_OK;
}

void platform_audio_release_captured_wav(uint8_t *wav) {
    board_port_release_captured_wav(wav);
}

device_status_t platform_audio_stream_start(void) {
    return status_from_esp_err(board_port_audio_stream_start());
}

device_status_t platform_audio_stream_read(int16_t *mono, uint32_t capacity,
                                           uint32_t *samples_read, uint16_t *level) {
    if (!mono || !samples_read || capacity == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    *samples_read = 0;
    size_t count = 0;
    device_status_t status = status_from_esp_err(
        board_port_audio_stream_read(mono, capacity, &count, level));
    if (status != DEVICE_STATUS_OK) return status;
    if (count > capacity || count > UINT32_MAX) return DEVICE_STATUS_INTERNAL_ERROR;
    *samples_read = (uint32_t)count;
    return DEVICE_STATUS_OK;
}

void platform_audio_stream_stop(void) {
    board_port_audio_stream_stop();
}

device_status_t platform_audio_playback_begin(void) {
    return status_from_esp_err(board_port_audio_playback_begin());
}

device_status_t platform_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                              uint8_t channels) {
    if (!pcm || frames == 0 || (channels != 1 && channels != 2)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return status_from_esp_err(board_port_audio_playback_write(pcm, frames, channels));
}

device_status_t platform_audio_playback_end(bool playback_succeeded) {
    return status_from_esp_err(board_port_audio_playback_end(
        playback_succeeded ? ESP_OK : ESP_FAIL));
}

void platform_audio_request_playback_stop(void) {
    board_port_request_audio_playback_stop();
}

void platform_audio_request_capture_stop(void) {
    board_port_request_capture_stop();
}

void platform_audio_reset_capture_stop(void) {
    board_port_reset_capture_stop();
}

device_status_t platform_audio_wake_word_start(device_wake_word_cb_t on_wake,
                                               void *context) {
    if (!on_wake) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_start_wake_word(on_wake, context));
}

device_status_t platform_audio_wake_word_stop(void) {
    return status_from_esp_err(board_port_stop_wake_word());
}

device_status_t platform_audio_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_stop_wake_word_with_timeout(timeout_ms));
}

void platform_audio_wake_word_pause(bool paused) {
    board_port_pause_wake_word(paused);
}
