/* Fangtang-4G physical direct-I2S audio profile.
 *
 * The shared compact audio implementation owns only the normalized 16 kHz
 * capture/playback contract, PCM conversion and session arbitration.  This
 * profile owns the wiring facts of the Fangtang MEMS microphone and amplifier,
 * so a new direct-I2S board does not inherit GPIO assignments from Bread.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang audio adapter may only be included by the Fangtang profile"
#endif

#ifndef MACLAW_COMPACT_AUDIO_ADAPTER_IMPLEMENTATION
#error "Fangtang audio adapter is owned exclusively by compact_audio_service.c"
#endif

#include "driver/gpio.h"
#include "driver/i2s_std.h"
#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "boards/compact_audio_calibration.h"

#define FANGTANG_AUDIO_SAMPLE_RATE 16000

#define FANGTANG_AUDIO_MIC_WS GPIO_NUM_4
#define FANGTANG_AUDIO_MIC_BCLK GPIO_NUM_5
#define FANGTANG_AUDIO_MIC_DIN GPIO_NUM_6

#define FANGTANG_AUDIO_SPK_DOUT GPIO_NUM_7
#define FANGTANG_AUDIO_SPK_BCLK GPIO_NUM_15
#define FANGTANG_AUDIO_SPK_WS GPIO_NUM_16

/* Capture and wake calibration are electrical/acoustic properties of this
 * microphone path, not different command behaviour.  The shared capture
 * state machine consumes these values through neutral aliases below. */
#define FANGTANG_AUDIO_COMMAND_SILENCE_MS 1500u
#define FANGTANG_AUDIO_COMMAND_START_CONFIRM_MS 160u
#define FANGTANG_AUDIO_COMMAND_START_LEVEL 45u
#define FANGTANG_AUDIO_COMMAND_SILENCE_FLOOR 55u
#define FANGTANG_AUDIO_COMMAND_SILENCE_MARGIN 35u
#define FANGTANG_AUDIO_COMMAND_SILENCE_CEILING 180u

#define FANGTANG_AUDIO_WAKE_WORD_DETECTION_THRESHOLD 0.20f
#define FANGTANG_AUDIO_WAKE_WORD_GAIN_NUM 3
#define FANGTANG_AUDIO_WAKE_WORD_GAIN_DEN 2


static inline const compact_audio_calibration_t *compact_audio_adapter_calibration(void) {
    static const compact_audio_calibration_t calibration = {
        .sample_rate = FANGTANG_AUDIO_SAMPLE_RATE,
        .command_silence_ms = FANGTANG_AUDIO_COMMAND_SILENCE_MS,
        .command_start_confirm_ms = FANGTANG_AUDIO_COMMAND_START_CONFIRM_MS,
        .command_start_level = FANGTANG_AUDIO_COMMAND_START_LEVEL,
        .command_silence_floor = FANGTANG_AUDIO_COMMAND_SILENCE_FLOOR,
        .command_silence_margin = FANGTANG_AUDIO_COMMAND_SILENCE_MARGIN,
        .command_silence_ceiling = FANGTANG_AUDIO_COMMAND_SILENCE_CEILING,
        .wake_word_detection_threshold = FANGTANG_AUDIO_WAKE_WORD_DETECTION_THRESHOLD,
        .wake_word_gain_num = FANGTANG_AUDIO_WAKE_WORD_GAIN_NUM,
        .wake_word_gain_den = FANGTANG_AUDIO_WAKE_WORD_GAIN_DEN,
        .output_volume_default = 70,
    };
    return &calibration;
}
/* The direct-I2S slot and DMA contract is intentionally the same shape as
 * Bread's; only the profile-private wiring/calibration belongs here. Driver
 * handles stay private so the shared compact renderer cannot operate I2S. */
static i2s_chan_handle_t s_fangtang_audio_rx;
static i2s_chan_handle_t s_fangtang_audio_tx;

static inline esp_err_t compact_audio_adapter_init_hardware(void) {
    if (s_fangtang_audio_rx || s_fangtang_audio_tx) return ESP_ERR_INVALID_STATE;
    i2s_chan_config_t config = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    config.dma_desc_num = 8;
    config.dma_frame_num = 256;
    i2s_chan_handle_t rx = NULL;
    i2s_chan_handle_t tx = NULL;
    esp_err_t err = i2s_new_channel(&config, NULL, &rx);
    if (err != ESP_OK) return err;
    i2s_std_config_t microphone = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(FANGTANG_AUDIO_SAMPLE_RATE),
        .slot_cfg = I2S_STD_MSB_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT,
                                                      I2S_SLOT_MODE_MONO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = FANGTANG_AUDIO_MIC_BCLK,
                     .ws = FANGTANG_AUDIO_MIC_WS, .dout = I2S_GPIO_UNUSED,
                     .din = FANGTANG_AUDIO_MIC_DIN, .invert_flags = {0}},
    };
    microphone.slot_cfg.slot_mask = I2S_STD_SLOT_LEFT;
    err = i2s_channel_init_std_mode(rx, &microphone);
    if (err == ESP_OK) err = i2s_channel_enable(rx);
    if (err != ESP_OK) goto fail;

    config.id = I2S_NUM_1;
    config.auto_clear_after_cb = true;
    err = i2s_new_channel(&config, &tx, NULL);
    if (err != ESP_OK) goto fail;
    const i2s_std_config_t speaker = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(FANGTANG_AUDIO_SAMPLE_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT,
                                                          I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = FANGTANG_AUDIO_SPK_BCLK,
                     .ws = FANGTANG_AUDIO_SPK_WS, .dout = FANGTANG_AUDIO_SPK_DOUT,
                     .din = I2S_GPIO_UNUSED, .invert_flags = {0}},
    };
    err = i2s_channel_init_std_mode(tx, &speaker);
    if (err == ESP_OK) {
        s_fangtang_audio_rx = rx;
        s_fangtang_audio_tx = tx;
        return ESP_OK;
    }

fail:
    if (tx) {
        (void)i2s_channel_disable(tx);
        (void)i2s_del_channel(tx);
    }
    if (rx) {
        (void)i2s_channel_disable(rx);
        (void)i2s_del_channel(rx);
    }
    return err;
}

/* The wake recognizer reads raw direct-I2S samples continuously.  Keep its
 * internal-RAM placement as a profile-private physical-audio contract so the
 * shared wake state machine asks only for a buffer role. */
/* Command WAV payloads are ordinary upload media rather than direct-I2S DMA
 * buffers.  The selected direct-I2S board keeps its PSRAM policy and matching
 * release private so shared command policy does not depend on heap details. */
static inline void *compact_audio_adapter_allocate_command_wav(size_t bytes) {
    if (bytes == 0) return NULL;
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static inline void compact_audio_adapter_free_command_wav(void *buffer) {
    heap_caps_free(buffer);
}
static inline void *compact_audio_adapter_allocate_wake_capture_buffer(size_t bytes) {
    if (bytes == 0) return NULL;
    return heap_caps_malloc(bytes, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

static inline void compact_audio_adapter_free_wake_capture_buffer(void *buffer) {
    heap_caps_free(buffer);
}
static inline esp_err_t compact_audio_adapter_read(void *buffer, size_t bytes,
                                                    size_t *out_read,
                                                    TickType_t timeout) {
    if (!s_fangtang_audio_rx || !buffer || !out_read) return ESP_ERR_INVALID_STATE;
    return i2s_channel_read(s_fangtang_audio_rx, buffer, bytes, out_read, timeout);
}

static inline esp_err_t compact_audio_adapter_write(const void *buffer, size_t bytes,
                                                     size_t *out_written,
                                                     TickType_t timeout) {
    if (!s_fangtang_audio_tx || !buffer || !out_written) return ESP_ERR_INVALID_STATE;
    return i2s_channel_write(s_fangtang_audio_tx, buffer, bytes, out_written, timeout);
}

static inline esp_err_t compact_audio_adapter_playback_begin(void) {
    if (!s_fangtang_audio_tx) return ESP_ERR_INVALID_STATE;
    return i2s_channel_enable(s_fangtang_audio_tx);
}

static inline esp_err_t compact_audio_adapter_playback_end(void) {
    if (!s_fangtang_audio_tx) return ESP_ERR_INVALID_STATE;
    return i2s_channel_disable(s_fangtang_audio_tx);
}
