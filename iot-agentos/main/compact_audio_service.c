#include "compact_audio_service.h"

#include "esp_check.h"
#include "freertos/semphr.h"

#include <limits.h>
#include <stdatomic.h>
#include <string.h>

/* No shared scene/PCM translation unit can instantiate adapter static state
 * or gain access to pin/DMA details. */
#include "boards/compact_audio_adapter.h"

const compact_audio_calibration_t *compact_audio_service_calibration(void) {
    return compact_audio_adapter_calibration();
}

typedef enum {
    COMPACT_AUDIO_SESSION_IDLE = 0,
    COMPACT_AUDIO_SESSION_STREAM,
    COMPACT_AUDIO_SESSION_COMMAND_CAPTURE,
    COMPACT_AUDIO_SESSION_PLAYBACK,
} compact_audio_service_session_t;

static SemaphoreHandle_t s_compact_audio_ownership_mutex;
static bool s_compact_audio_ready;
static compact_audio_service_session_t s_compact_audio_session;
static TaskHandle_t s_compact_audio_playback_owner;
static volatile bool s_compact_audio_playback_stop_requested;
static _Atomic bool s_compact_audio_command_capture_stop_requested;
static int32_t *s_compact_audio_wake_wire;

static esp_err_t compact_audio_service_acquire(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_compact_audio_ownership_mutex) {
        s_compact_audio_ownership_mutex = xSemaphoreCreateMutex();
        if (!s_compact_audio_ownership_mutex) return ESP_ERR_NO_MEM;
    }
    return xSemaphoreTake(s_compact_audio_ownership_mutex, pdMS_TO_TICKS(timeout_ms)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static void compact_audio_service_release(void) {
    if (s_compact_audio_ownership_mutex) {
        (void)xSemaphoreGive(s_compact_audio_ownership_mutex);
    }
}

static esp_err_t compact_audio_service_initialize_locked(void) {
    if (s_compact_audio_ready) return ESP_OK;
    const esp_err_t err = compact_audio_adapter_init_hardware();
    if (err == ESP_OK) s_compact_audio_ready = true;
    return err;
}

esp_err_t compact_audio_service_prepare(uint32_t timeout_ms) {
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "audio preparation ownership timeout");
    const esp_err_t err = compact_audio_service_initialize_locked();
    compact_audio_service_release();
    return err;
}

/* Direct-I2S profiles apply volume in the shared PCM mixer rather than a
 * codec register. Retain a semantic Audio-HAL acknowledgement seam so a
 * future codec-backed compact board can atomically program its physical gain
 * without requiring a renderer or Device API fork. */
esp_err_t compact_audio_service_set_output_volume(unsigned percent, uint32_t timeout_ms) {
    if (percent > 100 || timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "volume ownership timeout");
    const esp_err_t err = compact_audio_service_initialize_locked();
    compact_audio_service_release();
    return err;
}
void *compact_audio_service_allocate_command_wav(size_t bytes) {
    return compact_audio_adapter_allocate_command_wav(bytes);
}
void compact_audio_service_free_command_wav(void *buffer) {
    compact_audio_adapter_free_command_wav(buffer);
}

static int32_t compact_audio_service_slot_to_mono(int32_t raw) {
    return raw >> 14;
}

typedef enum {
    /* Meeting/stream PCM historically bypassed the wake calibration.  Preserve
     * that wire-level behavior: only foreground command and MultiNet wake
     * capture use the selected microphone path's calibrated gain. */
    COMPACT_AUDIO_PCM_GAIN_NONE = 0,
    COMPACT_AUDIO_PCM_GAIN_WAKE,
} compact_audio_service_pcm_gain_t;

static int32_t compact_audio_service_apply_gain(
    int32_t sample, compact_audio_service_pcm_gain_t gain) {
    if (gain == COMPACT_AUDIO_PCM_GAIN_NONE) {
        if (sample > INT16_MAX) return INT16_MAX;
        if (sample < INT16_MIN) return INT16_MIN;
        return sample;
    }
    const compact_audio_calibration_t *calibration = compact_audio_service_calibration();
    const unsigned numerator = calibration && calibration->wake_word_gain_num
                                   ? calibration->wake_word_gain_num : 1u;
    const unsigned denominator = calibration && calibration->wake_word_gain_den
                                     ? calibration->wake_word_gain_den : 1u;
    const int64_t amplified = (int64_t)sample * numerator / denominator;
    if (amplified > INT16_MAX) return INT16_MAX;
    if (amplified < INT16_MIN) return INT16_MIN;
    return (int32_t)amplified;
}

static void compact_audio_service_normalize_pcm(const int32_t *raw, size_t raw_samples,
                                                 int16_t *mono, size_t capacity,
                                                 size_t *samples_read,
                                                 compact_audio_capture_stats_t *out_stats,
                                                 compact_audio_service_pcm_gain_t gain) {
    if (samples_read) *samples_read = 0;
    if (out_stats) *out_stats = (compact_audio_capture_stats_t){0};
    if (!mono || capacity == 0) return;
    const size_t count = raw_samples < capacity ? raw_samples : capacity;
    int32_t input_peak = 0;
    int32_t peak = 0;
    int64_t sum = 0;
    for (size_t i = 0; i < count; ++i) {
        const int32_t input = compact_audio_service_slot_to_mono(raw[i]);
        const int32_t input_magnitude = input < 0 ? -input : input;
        if (input_magnitude > input_peak) input_peak = input_magnitude;
        const int16_t sample = (int16_t)compact_audio_service_apply_gain(input, gain);
        mono[i] = sample;
        const int32_t magnitude = sample < 0 ? -(int32_t)sample : sample;
        if (magnitude > peak) peak = magnitude;
        sum += sample;
    }
    uint64_t deviation_sum = 0;
    if (count) {
        const int32_t dc = (int32_t)(sum / (int64_t)count);
        for (size_t i = 0; i < count; ++i) {
            const int32_t deviation = (int32_t)mono[i] - dc;
            deviation_sum += (uint32_t)(deviation < 0 ? -deviation : deviation);
        }
    }
    const uint32_t mean_deviation = count ? (uint32_t)(deviation_sum / count) : 0;
    if (samples_read) *samples_read = count;
    if (out_stats) {
        out_stats->input_peak = input_peak;
        out_stats->peak = peak;
        out_stats->level = peak >= 12000 ? 1000u : (uint16_t)(peak * 1000 / 12000);
        out_stats->mean_level = mean_deviation >= 12000
                                    ? 1000u : (uint16_t)(mean_deviation * 1000 / 12000);
    }
}
esp_err_t compact_audio_service_stream_begin(uint32_t timeout_ms) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "stream ownership timeout");
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) {
        compact_audio_service_release();
        return ESP_ERR_INVALID_STATE;
    }
    const esp_err_t err = compact_audio_service_initialize_locked();
    if (err != ESP_OK) {
        compact_audio_service_release();
        return err;
    }
    s_compact_audio_session = COMPACT_AUDIO_SESSION_STREAM;
    return ESP_OK;
}

esp_err_t compact_audio_service_command_capture_begin(uint32_t timeout_ms) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "command capture ownership timeout");
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) {
        compact_audio_service_release();
        return ESP_ERR_INVALID_STATE;
    }
    const esp_err_t err = compact_audio_service_initialize_locked();
    if (err != ESP_OK) {
        compact_audio_service_release();
        return err;
    }
    s_compact_audio_session = COMPACT_AUDIO_SESSION_COMMAND_CAPTURE;
    /* A new capture generation never inherits a previous command's cancel. */
    compact_audio_service_reset_command_capture_stop();
    return ESP_OK;
}

esp_err_t compact_audio_service_capture_read(int16_t *mono, size_t capacity,
                                             size_t *samples_read,
                                             compact_audio_capture_stats_t *out_stats,
                                             TickType_t timeout) {
    if ((s_compact_audio_session != COMPACT_AUDIO_SESSION_STREAM &&
         s_compact_audio_session != COMPACT_AUDIO_SESSION_COMMAND_CAPTURE) ||
        !mono || !samples_read || capacity == 0) return ESP_ERR_INVALID_STATE;
    int32_t raw[512];
    size_t bytes = 0;
    ESP_RETURN_ON_ERROR(compact_audio_adapter_read(raw, sizeof(raw), &bytes, timeout),
                        "compact_audio", "capture I2S read failed");
    const compact_audio_service_pcm_gain_t gain =
        s_compact_audio_session == COMPACT_AUDIO_SESSION_COMMAND_CAPTURE
            ? COMPACT_AUDIO_PCM_GAIN_WAKE : COMPACT_AUDIO_PCM_GAIN_NONE;
    compact_audio_service_normalize_pcm(raw, bytes / sizeof(raw[0]), mono, capacity,
                                        samples_read, out_stats, gain);
    return ESP_OK;
}

void compact_audio_service_stream_end(void) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_STREAM) return;
    s_compact_audio_session = COMPACT_AUDIO_SESSION_IDLE;
    compact_audio_service_release();
}

void compact_audio_service_command_capture_end(void) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_COMMAND_CAPTURE) return;
    s_compact_audio_session = COMPACT_AUDIO_SESSION_IDLE;
    compact_audio_service_release();
}

void compact_audio_service_request_command_capture_stop(void) {
    atomic_store_explicit(&s_compact_audio_command_capture_stop_requested,
                          true, memory_order_release);
}

void compact_audio_service_reset_command_capture_stop(void) {
    atomic_store_explicit(&s_compact_audio_command_capture_stop_requested,
                          false, memory_order_release);
}

bool compact_audio_service_command_capture_stop_requested(void) {
    return atomic_load_explicit(&s_compact_audio_command_capture_stop_requested,
                                memory_order_acquire);
}

esp_err_t compact_audio_service_wake_capture_begin(size_t frames,
                                                   compact_audio_wake_capture_t *capture) {
    if (!capture || frames == 0 || s_compact_audio_wake_wire) return ESP_ERR_INVALID_ARG;
    *capture = (compact_audio_wake_capture_t){0};
    capture->mono = compact_audio_adapter_allocate_wake_capture_buffer(frames * sizeof(*capture->mono));
    s_compact_audio_wake_wire = compact_audio_adapter_allocate_wake_capture_buffer(
        frames * sizeof(*s_compact_audio_wake_wire));
    if (!capture->mono || !s_compact_audio_wake_wire) {
        compact_audio_service_wake_capture_end(capture);
        return ESP_ERR_NO_MEM;
    }
    capture->frames = frames;
    return ESP_OK;
}

esp_err_t compact_audio_service_wake_capture_read(compact_audio_wake_capture_t *capture,
                                                  uint32_t timeout_ms,
                                                  compact_audio_capture_stats_t *out_stats) {
    if (!capture || !capture->mono || !s_compact_audio_wake_wire || !capture->frames ||
        timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "wake microphone ownership timeout");
    const esp_err_t init_err = compact_audio_service_initialize_locked();
    size_t bytes = 0;
    const esp_err_t read_err = init_err == ESP_OK
                                   ? compact_audio_adapter_read(
                                         s_compact_audio_wake_wire,
                                         capture->frames * sizeof(*s_compact_audio_wake_wire),
                                         &bytes, pdMS_TO_TICKS(timeout_ms))
                                   : init_err;
    compact_audio_service_release();
    if (read_err != ESP_OK) return read_err;
    size_t samples = 0;
    compact_audio_service_normalize_pcm(s_compact_audio_wake_wire,
                                         bytes / sizeof(*s_compact_audio_wake_wire),
                                         capture->mono, capture->frames, &samples, out_stats,
                                         COMPACT_AUDIO_PCM_GAIN_WAKE);
    if (samples < capture->frames) {
        memset(capture->mono + samples, 0,
               (capture->frames - samples) * sizeof(*capture->mono));
    }
    return ESP_OK;
}

void compact_audio_service_wake_capture_end(compact_audio_wake_capture_t *capture) {
    if (!capture) return;
    compact_audio_adapter_free_wake_capture_buffer(capture->mono);
    capture->mono = NULL;
    capture->frames = 0;
    compact_audio_adapter_free_wake_capture_buffer(s_compact_audio_wake_wire);
    s_compact_audio_wake_wire = NULL;
}

esp_err_t compact_audio_service_playback_begin(uint32_t timeout_ms) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) return ESP_ERR_INVALID_STATE;
    ESP_RETURN_ON_ERROR(compact_audio_service_acquire(timeout_ms), "compact_audio",
                        "playback ownership timeout");
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_IDLE) {
        compact_audio_service_release();
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = compact_audio_service_initialize_locked();
    if (err == ESP_OK) err = compact_audio_adapter_playback_begin();
    if (err != ESP_OK) {
        (void)compact_audio_adapter_playback_end();
        compact_audio_service_release();
        return err;
    }
    s_compact_audio_session = COMPACT_AUDIO_SESSION_PLAYBACK;
    s_compact_audio_playback_stop_requested = false;
    s_compact_audio_playback_owner = xTaskGetCurrentTaskHandle();
    return ESP_OK;
}

esp_err_t compact_audio_service_playback_write(const void *buffer, size_t bytes,
                                               size_t *out_written, TickType_t timeout) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_PLAYBACK ||
        s_compact_audio_playback_owner != xTaskGetCurrentTaskHandle() ||
        !buffer || !out_written) return ESP_ERR_INVALID_STATE;
    if (s_compact_audio_playback_stop_requested) return ESP_ERR_INVALID_STATE;
    return compact_audio_adapter_write(buffer, bytes, out_written, timeout);
}
esp_err_t compact_audio_service_playback_end(void) {
    if (s_compact_audio_session != COMPACT_AUDIO_SESSION_PLAYBACK ||
        s_compact_audio_playback_owner != xTaskGetCurrentTaskHandle()) {
        return ESP_ERR_INVALID_STATE;
    }
    const esp_err_t err = compact_audio_adapter_playback_end();
    s_compact_audio_playback_owner = NULL;
    s_compact_audio_playback_stop_requested = false;
    s_compact_audio_session = COMPACT_AUDIO_SESSION_IDLE;
    compact_audio_service_release();
    return err;
}

void compact_audio_service_request_playback_stop(void) {
    if (s_compact_audio_session == COMPACT_AUDIO_SESSION_PLAYBACK &&
        s_compact_audio_playback_owner) {
        s_compact_audio_playback_stop_requested = true;
    }
}
