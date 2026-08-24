#include "platform_audio.h"

#include <limits.h>
#include <string.h>

#include "compact_audio_service.h"
#include "compact_wake_service.h"

#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/* Compact boards share physical direct-I2S session ownership below this
 * boundary.  Bread and Fangtang differ only through compact_audio_service's
 * selected profile adapter; no board identity or GPIO fact enters here. */

#define PLATFORM_AUDIO_COMMAND_CAPTURE_MAX_SECONDS 30u
#define PLATFORM_AUDIO_COMMAND_CAPTURE_START_TIMEOUT_MS 6000u
#define PLATFORM_AUDIO_COMMAND_CAPTURE_PREROLL_MS 300u

static uint8_t s_compact_output_volume;
static bool s_compact_output_volume_initialized;

static const compact_audio_calibration_t *compact_calibration(void) {
    return compact_audio_service_calibration();
}

static uint32_t compact_sample_rate(void) {
    const compact_audio_calibration_t *calibration = compact_calibration();
    return calibration ? calibration->sample_rate : 0;
}

static unsigned compact_active_output_volume(void) {
    const compact_audio_calibration_t *calibration = compact_calibration();
    return s_compact_output_volume_initialized ?
           __atomic_load_n(&s_compact_output_volume, __ATOMIC_RELAXED) :
           (calibration ? calibration->output_volume_default : 0u);
}

static void compact_ensure_output_volume(void) {
    if (!s_compact_output_volume_initialized) {
        __atomic_store_n(&s_compact_output_volume, compact_active_output_volume(),
                         __ATOMIC_RELAXED);
        s_compact_output_volume_initialized = true;
    }
}

static device_status_t status_from_esp_err(esp_err_t err) {
    return platform_audio_status_from_esp_err((int)err);
}

static void compact_pause_wake_word(bool paused) {
    compact_wake_service_set_paused(paused);
}

device_status_t platform_audio_set_output_volume(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;
    const device_status_t status = status_from_esp_err(
        compact_audio_service_set_output_volume(percent, 1500));
    if (status == DEVICE_STATUS_OK) {
        __atomic_store_n(&s_compact_output_volume, percent, __ATOMIC_RELAXED);
        s_compact_output_volume_initialized = true;
    }
    return status;
}

device_status_t platform_audio_adjust_output_volume(int delta_percent,
                                                    uint8_t *out_percent) {
    int next = (int)compact_active_output_volume() + delta_percent;
    if (next < 0) next = 0;
    if (next > 100) next = 100;
    const device_status_t status = platform_audio_set_output_volume((uint8_t)next);
    if (status == DEVICE_STATUS_OK && out_percent) *out_percent = (uint8_t)next;
    return status;
}

device_status_t platform_audio_stream_start(void) {
    compact_pause_wake_word(true);
    (void)compact_wake_service_wait_for_pause_ack(200);
    const device_status_t status = status_from_esp_err(
        compact_audio_service_stream_begin(5000));
    if (status != DEVICE_STATUS_OK) compact_pause_wake_word(false);
    return status;
}

device_status_t platform_audio_stream_read(int16_t *mono, uint32_t capacity,
                                           uint32_t *samples_read, uint16_t *level) {
    if (!mono || !samples_read || capacity == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    *samples_read = 0;
    size_t count = 0;
    compact_audio_capture_stats_t stats = {0};
    const device_status_t status = status_from_esp_err(compact_audio_service_capture_read(
        mono, capacity, &count, &stats, pdMS_TO_TICKS(1000)));
    if (status != DEVICE_STATUS_OK) return status;
    if (count > capacity || count > UINT32_MAX) return DEVICE_STATUS_INTERNAL_ERROR;
    *samples_read = (uint32_t)count;
    if (level) *level = stats.level;
    return DEVICE_STATUS_OK;
}

void platform_audio_stream_stop(void) {
    compact_audio_service_stream_end();
    compact_pause_wake_word(false);
}

device_status_t platform_audio_wake_word_start(device_wake_word_cb_t on_wake,
                                               void *context) {
    if (!on_wake) return DEVICE_STATUS_INVALID_ARGUMENT;
    const device_status_t prepared = status_from_esp_err(compact_audio_service_prepare(5000));
    if (prepared != DEVICE_STATUS_OK) return prepared;
    return status_from_esp_err(compact_wake_service_start(on_wake, context, 10000));
}

device_status_t platform_audio_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(compact_wake_service_stop(timeout_ms));
}

device_status_t platform_audio_wake_word_stop(void) {
    return platform_audio_wake_word_stop_with_timeout(6000);
}

void platform_audio_wake_word_pause(bool paused) {
    compact_pause_wake_word(paused);
}

device_status_t platform_audio_wake_word_pause_with_ack(bool paused,
                                                        uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    compact_pause_wake_word(paused);
    if (!paused) return DEVICE_STATUS_OK;
    return compact_wake_service_wait_for_pause_ack(timeout_ms)
               ? DEVICE_STATUS_OK : DEVICE_STATUS_TIMEOUT;
}

device_status_t platform_audio_capture_wav(
    uint8_t **out_wav, uint32_t *out_len,
    platform_audio_capture_progress_cb_t on_progress, void *progress_context) {
    if (!out_wav || !out_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_wav = NULL;
    *out_len = 0;

    const compact_audio_calibration_t *calibration = compact_calibration();
    const uint32_t sample_rate = compact_sample_rate();
    if (!calibration || sample_rate == 0) return DEVICE_STATUS_UNAVAILABLE;
    const size_t max_samples = (size_t)sample_rate * PLATFORM_AUDIO_COMMAND_CAPTURE_MAX_SECONDS;
    if (max_samples > (SIZE_MAX - 44u) / sizeof(int16_t)) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    const size_t start_timeout_samples =
        (size_t)sample_rate * PLATFORM_AUDIO_COMMAND_CAPTURE_START_TIMEOUT_MS / 1000u;
    const size_t silence_samples =
        (size_t)sample_rate * calibration->command_silence_ms / 1000u;
    const size_t start_confirm_samples =
        (size_t)sample_rate * calibration->command_start_confirm_ms / 1000u;
    const size_t preroll_samples =
        (size_t)sample_rate * PLATFORM_AUDIO_COMMAND_CAPTURE_PREROLL_MS / 1000u;

    compact_pause_wake_word(true);
    (void)compact_wake_service_wait_for_pause_ack(200);
    esp_err_t err = compact_audio_service_command_capture_begin(1500);
    if (err != ESP_OK) {
        compact_pause_wake_word(false);
        return status_from_esp_err(err);
    }

    uint8_t *wav = compact_audio_service_allocate_command_wav(
        44u + max_samples * sizeof(int16_t));
    if (!wav) {
        compact_audio_service_command_capture_end();
        compact_pause_wake_word(false);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    memset(wav, 0, 44);
    memcpy(wav, "RIFF", 4);
    uint32_t riff_size = (uint32_t)(36u + max_samples * sizeof(int16_t));
    memcpy(wav + 4, &riff_size, sizeof(riff_size));
    memcpy(wav + 8, "WAVEfmt ", 8);
    uint32_t format_size = 16;
    uint16_t pcm_format = 1;
    uint16_t channels = 1;
    uint32_t byte_rate = sample_rate * sizeof(int16_t);
    uint16_t block_align = sizeof(int16_t);
    uint16_t bits_per_sample = 16;
    uint32_t data_size = (uint32_t)(max_samples * sizeof(int16_t));
    memcpy(wav + 16, &format_size, sizeof(format_size));
    memcpy(wav + 20, &pcm_format, sizeof(pcm_format));
    memcpy(wav + 22, &channels, sizeof(channels));
    memcpy(wav + 24, &sample_rate, sizeof(sample_rate));
    memcpy(wav + 28, &byte_rate, sizeof(byte_rate));
    memcpy(wav + 32, &block_align, sizeof(block_align));
    memcpy(wav + 34, &bits_per_sample, sizeof(bits_per_sample));
    memcpy(wav + 36, "data", 4);
    memcpy(wav + 40, &data_size, sizeof(data_size));

    int16_t *pcm = (int16_t *)(wav + 44);
    size_t captured = 0;
    size_t voiced = 0;
    size_t silence = 0;
    size_t speech_start = 0;
    bool speech_started = false;
    uint16_t smoothed_level = 0;
    uint16_t idle_level = 0;
    uint32_t last_progress_second = UINT32_MAX;
    device_status_t status = DEVICE_STATUS_OK;

    while (captured < max_samples) {
        size_t received = 0;
        compact_audio_capture_stats_t stats = {0};
        err = compact_audio_service_capture_read(pcm + captured, max_samples - captured,
                                                 &received, &stats,
                                                 pdMS_TO_TICKS(1000));
        if (err != ESP_OK) {
            status = status_from_esp_err(err);
            break;
        }
        if (received == 0) continue;
        captured += received;
        smoothed_level = stats.level > smoothed_level
                             ? (uint16_t)((smoothed_level + stats.level * 3u) / 4u)
                             : (uint16_t)((smoothed_level * 7u + stats.level) / 8u);
        const uint32_t elapsed = (uint32_t)(captured / sample_rate);
        if (on_progress && elapsed != last_progress_second) {
            on_progress(progress_context, smoothed_level, elapsed);
            last_progress_second = elapsed;
        }
        if (!speech_started) {
            if (idle_level == 0 || stats.mean_level < idle_level) idle_level = stats.mean_level;
            voiced = stats.level >= calibration->command_start_level ? voiced + received : 0;
            if (voiced >= start_confirm_samples) {
                speech_started = true;
                silence = 0;
                speech_start = captured - voiced;
            } else if (captured >= start_timeout_samples) {
                status = DEVICE_STATUS_NOT_FOUND;
                break;
            }
        } else {
            uint16_t silence_level = idle_level + calibration->command_silence_margin;
            if (silence_level < calibration->command_silence_floor) {
                silence_level = calibration->command_silence_floor;
            }
            if (silence_level > calibration->command_silence_ceiling) {
                silence_level = calibration->command_silence_ceiling;
            }
            silence = stats.mean_level <= silence_level ? silence + received : 0;
            if (silence >= silence_samples) break;
        }
        if (compact_audio_service_command_capture_stop_requested()) break;
    }

    if (status == DEVICE_STATUS_OK && !speech_started) status = DEVICE_STATUS_NOT_FOUND;
    if (status == DEVICE_STATUS_OK) {
        const size_t trim_start = speech_start > preroll_samples ? speech_start - preroll_samples : 0;
        const size_t payload_samples = captured - trim_start;
        if (trim_start) memmove(pcm, pcm + trim_start, payload_samples * sizeof(*pcm));
        const size_t wav_len = 44u + payload_samples * sizeof(*pcm);
        if (wav_len > UINT32_MAX) {
            status = DEVICE_STATUS_INTERNAL_ERROR;
        } else {
            riff_size = (uint32_t)(wav_len - 8u);
            data_size = (uint32_t)(payload_samples * sizeof(*pcm));
            memcpy(wav + 4, &riff_size, sizeof(riff_size));
            memcpy(wav + 40, &data_size, sizeof(data_size));
            *out_wav = wav;
            *out_len = (uint32_t)wav_len;
            wav = NULL;
        }
    }
    if (wav) compact_audio_service_free_command_wav(wav);
    compact_audio_service_command_capture_end();
    compact_pause_wake_word(false);
    return status;
}

void platform_audio_release_captured_wav(uint8_t *wav) {
    compact_audio_service_free_command_wav(wav);
}

static device_status_t compact_playback_write(const int16_t *pcm, uint32_t frames,
                                              uint8_t channels) {
    if (!pcm || frames == 0 || (channels != 1 && channels != 2)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    int16_t stereo[512];
    size_t offset = 0;
    while (offset < frames) {
        size_t count = frames - offset;
        if (count > 256) count = 256;
        const unsigned volume = __atomic_load_n(&s_compact_output_volume,
                                                 __ATOMIC_RELAXED);
        for (size_t i = 0; i < count; ++i) {
            const int32_t left = pcm[(offset + i) * channels];
            const int32_t right = channels == 2 ? pcm[(offset + i) * 2u + 1u] : left;
            stereo[i * 2u] = (int16_t)(left * (int32_t)volume / 100);
            stereo[i * 2u + 1u] = (int16_t)(right * (int32_t)volume / 100);
        }
        size_t written = 0;
        const size_t expected = count * 2u * sizeof(int16_t);
        const device_status_t status = status_from_esp_err(compact_audio_service_playback_write(
            stereo, expected, &written, pdMS_TO_TICKS(1000)));
        if (status != DEVICE_STATUS_OK) return status;
        if (written != expected) return DEVICE_STATUS_TIMEOUT;
        offset += count;
    }
    return DEVICE_STATUS_OK;
}

device_status_t platform_audio_playback_begin(void) {
    compact_ensure_output_volume();
    compact_pause_wake_word(true);
    (void)compact_wake_service_wait_for_pause_ack(300);
    const device_status_t status = status_from_esp_err(
        compact_audio_service_playback_begin(1500));
    if (status != DEVICE_STATUS_OK) compact_pause_wake_word(false);
    return status;
}

device_status_t platform_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                              uint8_t channels) {
    return compact_playback_write(pcm, frames, channels);
}

device_status_t platform_audio_playback_end(bool playback_succeeded) {
    vTaskDelay(pdMS_TO_TICKS(20));
    int16_t silence[128] = {0};
    device_status_t status = compact_playback_write(silence, 128, 1);
    vTaskDelay(pdMS_TO_TICKS(10));
    const device_status_t finish = status_from_esp_err(compact_audio_service_playback_end());
    compact_pause_wake_word(false);
    if (!playback_succeeded) return DEVICE_STATUS_IO_ERROR;
    return status != DEVICE_STATUS_OK ? status : finish;
}

device_status_t platform_audio_play_wav(const uint8_t *wav, uint32_t wav_len) {
    if (!wav || wav_len < 44 || memcmp(wav, "RIFF", 4) != 0 ||
        memcmp(wav + 8, "WAVE", 4) != 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const uint8_t *format = NULL;
    const uint8_t *data = NULL;
    size_t format_len = 0;
    size_t data_len = 0;
    for (size_t offset = 12; offset + 8 <= wav_len;) {
        const uint8_t *chunk = wav + offset;
        const uint32_t chunk_len = (uint32_t)chunk[4] | ((uint32_t)chunk[5] << 8) |
                                   ((uint32_t)chunk[6] << 16) | ((uint32_t)chunk[7] << 24);
        offset += 8;
        if (chunk_len > wav_len - offset) return DEVICE_STATUS_INVALID_ARGUMENT;
        if (memcmp(chunk, "fmt ", 4) == 0) { format = wav + offset; format_len = chunk_len; }
        else if (memcmp(chunk, "data", 4) == 0) { data = wav + offset; data_len = chunk_len; }
        const size_t padded = (size_t)chunk_len + (chunk_len & 1u);
        if (padded > wav_len - offset) return DEVICE_STATUS_INVALID_ARGUMENT;
        offset += padded;
    }
    if (!format || format_len < 16 || !data || !data_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    const uint16_t encoding = (uint16_t)format[0] | ((uint16_t)format[1] << 8);
    const uint16_t channels = (uint16_t)format[2] | ((uint16_t)format[3] << 8);
    const uint32_t rate = (uint32_t)format[4] | ((uint32_t)format[5] << 8) |
                          ((uint32_t)format[6] << 16) | ((uint32_t)format[7] << 24);
    const uint16_t bits = (uint16_t)format[14] | ((uint16_t)format[15] << 8);
    if (encoding != 1 || bits != 16 || rate != compact_sample_rate() ||
        (channels != 1 && channels != 2)) return DEVICE_STATUS_UNAVAILABLE;
    const size_t frame_bytes = channels * sizeof(int16_t);
    if (data_len % frame_bytes != 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    device_status_t status = platform_audio_playback_begin();
    if (status == DEVICE_STATUS_OK) {
        status = platform_audio_playback_write((const int16_t *)data,
                                               (uint32_t)(data_len / frame_bytes), channels);
        const device_status_t ended = platform_audio_playback_end(status == DEVICE_STATUS_OK);
        if (status == DEVICE_STATUS_OK) status = ended;
    }
    return status;
}

device_status_t platform_audio_play_alarm_burst(void) {
    const uint32_t sample_rate = compact_sample_rate();
    if (!sample_rate) return DEVICE_STATUS_UNAVAILABLE;
    device_status_t status = platform_audio_playback_begin();
    if (status != DEVICE_STATUS_OK) return status;
    int16_t mono[256];
    for (unsigned strike = 0; strike < 3 && status == DEVICE_STATUS_OK; ++strike) {
        const unsigned half_period = strike & 1u ? 4u : 5u;
        for (uint32_t frame = 0; frame < sample_rate / 12u && status == DEVICE_STATUS_OK;
             frame += 256u) {
            uint32_t count = sample_rate / 12u - frame;
            if (count > 256u) count = 256u;
            for (uint32_t i = 0; i < count; ++i) {
                int32_t amplitude = 8200 - (int32_t)(frame + i) * 5;
                if (amplitude < 1400) amplitude = 1400;
                mono[i] = ((frame + i) / half_period & 1u) ? amplitude : -amplitude;
            }
            status = platform_audio_playback_write(mono, count, 1);
        }
    }
    const device_status_t ended = platform_audio_playback_end(status == DEVICE_STATUS_OK);
    return status == DEVICE_STATUS_OK ? ended : status;
}

void platform_audio_request_playback_stop(void) {
    compact_audio_service_request_playback_stop();
}

void platform_audio_request_capture_stop(void) {
    compact_audio_service_request_command_capture_stop();
}

void platform_audio_reset_capture_stop(void) {
    compact_audio_service_reset_command_capture_stop();
}
