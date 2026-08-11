#include "mp3_player.h"

#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

#include "audio_common.h"
#include "device_api.h"
#include "esp_audio_simple_dec.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "impl/esp_mp3_dec.h"

#define MP3_INPUT_CHUNK 1024
#define MP3_OUTPUT_INITIAL (1152 * 2 * sizeof(int16_t))
#define MP3_OUTPUT_MAX (64 * 1024)
#define MP3_MAX_DRAIN_ITERATIONS 32

static const char *TAG = "mp3_player";
static bool s_decoder_registered;

static uint8_t *resize_output(uint8_t *buffer, size_t old_size,
                              size_t new_size) {
    uint8_t *resized = heap_caps_malloc(new_size,
                                        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!resized) resized = malloc(new_size);
    if (!resized) return NULL;
    if (buffer && old_size) {
        size_t copy_size = old_size < new_size ? old_size : new_size;
        memcpy(resized, buffer, copy_size);
    }
    free(buffer);
    return resized;
}

typedef struct {
    uint32_t input_rate;
    unsigned channels;
    uint64_t input_frames;
    uint64_t output_frames;
    int16_t previous[2];
    bool have_previous;
    bool playback_started;
} mp3_playback_t;

static esp_err_t audio_error(esp_audio_err_t err) {
    switch (err) {
        case ESP_AUDIO_ERR_MEM_LACK:
            return ESP_ERR_NO_MEM;
        case ESP_AUDIO_ERR_INVALID_PARAMETER:
        case ESP_AUDIO_ERR_HEADER_PARSE:
            return ESP_ERR_INVALID_ARG;
        case ESP_AUDIO_ERR_DATA_LACK:
            return ESP_ERR_INVALID_SIZE;
        case ESP_AUDIO_ERR_NOT_SUPPORT:
            return ESP_ERR_NOT_SUPPORTED;
        case ESP_AUDIO_ERR_BUFF_NOT_ENOUGH:
            return ESP_ERR_INVALID_SIZE;
        case ESP_AUDIO_ERR_FAIL:
            return ESP_ERR_INVALID_RESPONSE;
        default:
            return ESP_FAIL;
    }
}

static esp_err_t device_status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        case DEVICE_STATUS_INTERNAL_ERROR:
        default: return ESP_FAIL;
    }
}

// The board audio clocks remain at 16 kHz because the microphone, wake-word
// pipeline and EchoEar codec share that hardware configuration. This streaming
// linear resampler keeps the previous block's final frame so interpolation is
// continuous across decoder output boundaries.
static int16_t resample_source(const mp3_playback_t *state, const int16_t *pcm,
                               size_t frames, unsigned channel,
                               uint64_t source, bool final) {
    if (source < state->input_frames) return state->previous[channel];
    size_t local = (size_t)(source - state->input_frames);
    if (local >= frames) local = final ? frames - 1 : 0;
    return pcm[local * state->channels + channel];
}

static esp_err_t write_resampled_until(mp3_playback_t *state, const int16_t *pcm,
                                       size_t frames, uint64_t output_end,
                                       bool final) {
    size_t output_count = (size_t)(output_end - state->output_frames);
    if (output_count == 0) return ESP_OK;
    if (!state->playback_started) {
        esp_err_t err = device_status_to_esp_err(device_audio_playback_begin());
        if (err != ESP_OK) return err;
        state->playback_started = true;
    }
    int16_t resampled[256 * 2];
    size_t written = 0;
    esp_err_t err = ESP_OK;
    while (written < output_count && err == ESP_OK) {
        size_t count = output_count - written;
        if (count > 256) count = 256;
        for (size_t out = 0; out < count; ++out) {
            uint64_t absolute_output = state->output_frames + written + out;
            uint64_t scaled = absolute_output * state->input_rate;
            uint64_t source = scaled / AUDIO_RATE;
            uint32_t fraction = (uint32_t)(scaled % AUDIO_RATE);
            for (unsigned channel = 0; channel < state->channels; ++channel) {
                int32_t first = resample_source(state, pcm, frames, channel,
                                                source, final);
                int32_t second = fraction == 0
                                     ? first
                                     : resample_source(state, pcm, frames, channel,
                                                       source + 1, final);
                int64_t mixed = (int64_t)first * (AUDIO_RATE - fraction) +
                                (int64_t)second * fraction;
                resampled[out * state->channels + channel] =
                    (int16_t)(mixed / AUDIO_RATE);
            }
        }
        err = device_status_to_esp_err(device_audio_playback_write(
            resampled, (uint32_t)count, (uint8_t)state->channels));
        written += count;
    }
    if (err == ESP_OK) state->output_frames = output_end;
    return err;
}

static esp_err_t write_resampled(mp3_playback_t *state, const int16_t *pcm,
                                 size_t frames, unsigned channels,
                                 uint32_t input_rate) {
    if (!state || !pcm || frames == 0 || (channels != 1 && channels != 2) ||
        input_rate < 8000 || input_rate > 48000) {
        return ESP_ERR_INVALID_ARG;
    }
    if (state->input_rate == 0) {
        state->input_rate = input_rate;
        state->channels = channels;
    }
    if (state->input_rate != input_rate || state->channels != channels) {
        ESP_LOGW(TAG, "MP3 format changed mid-stream: %lu Hz/%u ch -> %lu Hz/%u ch",
                 (unsigned long)state->input_rate, state->channels,
                 (unsigned long)input_rate, channels);
        return ESP_ERR_NOT_SUPPORTED;
    }

    uint64_t input_end = state->input_frames + frames;
    // Only emit positions for which both interpolation endpoints are known.
    // Any fraction crossing this block boundary is emitted by the next call.
    uint64_t output_end = ((input_end - 1) * AUDIO_RATE / input_rate) + 1;
    esp_err_t err = write_resampled_until(state, pcm, frames, output_end, false);
    if (err == ESP_OK) {
        for (unsigned channel = 0; channel < channels; ++channel) {
            state->previous[channel] = pcm[(frames - 1) * channels + channel];
        }
        state->have_previous = true;
        state->input_frames = input_end;
    }
    return err;
}

static esp_err_t finish_resampled(mp3_playback_t *state) {
    if (!state || !state->have_previous || state->input_rate == 0) {
        return ESP_ERR_INVALID_ARG;
    }
    uint64_t output_end = (state->input_frames * AUDIO_RATE +
                           state->input_rate - 1) / state->input_rate;
    // All remaining positions lie at the final input frame; repeat that frame
    // for the unavailable right interpolation endpoint.
    return write_resampled_until(state, state->previous, 1, output_end, true);
}

esp_err_t mp3_player_play(const uint8_t *mp3, size_t mp3_len) {
    if (!mp3 || mp3_len < 4 || mp3_len > UINT32_MAX) return ESP_ERR_INVALID_ARG;

    if (!s_decoder_registered) {
        esp_audio_err_t register_err = esp_mp3_dec_register();
        if (register_err != ESP_AUDIO_ERR_OK &&
            register_err != ESP_AUDIO_ERR_ALREADY_EXIST) {
            return audio_error(register_err);
        }
        s_decoder_registered = true;
    }

    esp_audio_simple_dec_cfg_t cfg = {
        .dec_type = ESP_AUDIO_SIMPLE_DEC_TYPE_MP3,
        .use_frame_dec = false,
    };
    esp_audio_simple_dec_handle_t decoder = NULL;
    esp_audio_err_t dec_err = esp_audio_simple_dec_open(&cfg, &decoder);
    if (dec_err != ESP_AUDIO_ERR_OK) return audio_error(dec_err);

    size_t output_capacity = MP3_OUTPUT_INITIAL;
    // The complete compressed response already occupies PSRAM. Keep the PCM
    // frame buffer there too, preserving internal SRAM for Wi-Fi/TLS and the
    // decoder's latency-sensitive working state.
    uint8_t *output = heap_caps_malloc(output_capacity,
                                       MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!output) output = malloc(output_capacity);
    if (!output) {
        esp_audio_simple_dec_close(decoder);
        return ESP_ERR_NO_MEM;
    }

    mp3_playback_t state = {0};
    esp_err_t result = ESP_OK;
    size_t offset = 0;
    while (offset < mp3_len && result == ESP_OK) {
        size_t chunk = mp3_len - offset;
        if (chunk > MP3_INPUT_CHUNK) chunk = MP3_INPUT_CHUNK;
        esp_audio_simple_dec_raw_t raw = {
            .buffer = (uint8_t *)mp3 + offset,
            .len = (uint32_t)chunk,
            .eos = offset + chunk == mp3_len,
        };
        unsigned stalled_iterations = 0;
        while (raw.len > 0 && result == ESP_OK) {
            esp_audio_simple_dec_out_t frame = {
                .buffer = output,
                .len = (uint32_t)output_capacity,
            };
            dec_err = esp_audio_simple_dec_process(decoder, &raw, &frame);
            if (dec_err == ESP_AUDIO_ERR_BUFF_NOT_ENOUGH) {
                if (frame.needed_size <= output_capacity ||
                    frame.needed_size > MP3_OUTPUT_MAX) {
                    result = ESP_ERR_INVALID_SIZE;
                    break;
                }
                uint8_t *larger = resize_output(output, output_capacity,
                                                frame.needed_size);
                if (!larger) {
                    result = ESP_ERR_NO_MEM;
                    break;
                }
                output = larger;
                output_capacity = frame.needed_size;
                continue;
            }
            if (dec_err != ESP_AUDIO_ERR_OK && dec_err != ESP_AUDIO_ERR_CONTINUE) {
                ESP_LOGW(TAG, "MP3 decode failed at byte %u: %d",
                         (unsigned)offset, dec_err);
                result = audio_error(dec_err);
                break;
            }
            if (raw.consumed > raw.len) {
                result = ESP_ERR_INVALID_RESPONSE;
                break;
            }
            if (frame.decoded_size > 0) {
                esp_audio_simple_dec_info_t info = {0};
                dec_err = esp_audio_simple_dec_get_info(decoder, &info);
                if (dec_err != ESP_AUDIO_ERR_OK || info.bits_per_sample != 16 ||
                    (info.channel != 1 && info.channel != 2)) {
                    result = dec_err == ESP_AUDIO_ERR_OK ? ESP_ERR_NOT_SUPPORTED
                                                        : audio_error(dec_err);
                    break;
                }
                size_t sample_bytes = info.channel * sizeof(int16_t);
                if (frame.decoded_size % sample_bytes != 0) {
                    result = ESP_ERR_INVALID_SIZE;
                    break;
                }
                result = write_resampled(&state, (const int16_t *)output,
                                         frame.decoded_size / sample_bytes,
                                         info.channel, info.sample_rate);
            }
            if (raw.consumed == 0) {
                // A decoder may drain cached frames without consuming input.
                // Keep a generous finite guard for a broken/corrupt stream.
                if (frame.decoded_size == 0 ||
                    ++stalled_iterations > MP3_MAX_DRAIN_ITERATIONS) {
                    result = ESP_ERR_INVALID_RESPONSE;
                    break;
                }
                continue;
            }
            stalled_iterations = 0;
            raw.buffer += raw.consumed;
            raw.len -= raw.consumed;
            offset += raw.consumed;
        }
    }

    if (result == ESP_OK) {
        // A syntactically valid but frame-less/corrupt MP3 must not be ACKed
        // as successfully played. It also has no resampler state to flush.
        result = state.have_previous ? finish_resampled(&state)
                                     : ESP_ERR_INVALID_RESPONSE;
    }
    if (state.playback_started) {
        /* Preserve a decoder/write failure. The physical end transaction is
         * still mandatory for codec/session cleanup, but a successful cleanup
         * must not turn a corrupt or truncated MP3 into a false success. */
        esp_err_t end_result = device_status_to_esp_err(
            device_audio_playback_end(result == ESP_OK));
        if (result == ESP_OK) result = end_result;
    }
    free(output);
    esp_audio_simple_dec_close(decoder);
    if (result == ESP_OK) {
        ESP_LOGI(TAG, "MP3 playback complete: %u bytes, %lu Hz -> %u Hz",
                 (unsigned)mp3_len, (unsigned long)state.input_rate, AUDIO_RATE);
    }
    return result;
}
