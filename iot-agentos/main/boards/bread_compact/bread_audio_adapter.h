/* Bread Compact physical direct-I2S audio profile.
 *
 * The shared compact audio implementation owns PCM conversion, capture /
 * playback session arbitration and wake-word policy.  This adapter owns the
 * port, pin and slot-clock facts of Bread's microphone and amplifier.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD
#error "Bread audio adapter may only be included by the Bread Compact profile"
#endif

#include "driver/gpio.h"
#include "driver/i2s_std.h"
#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/idf_additions.h"
#include "boards/compact_audio_calibration.h"

#define BREAD_AUDIO_SAMPLE_RATE 16000
#define BREAD_AUDIO_MIC_WS GPIO_NUM_4
#define BREAD_AUDIO_MIC_BCLK GPIO_NUM_5
#define BREAD_AUDIO_MIC_DIN GPIO_NUM_6
#define BREAD_AUDIO_SPK_DOUT GPIO_NUM_7
#define BREAD_AUDIO_SPK_BCLK GPIO_NUM_15
#define BREAD_AUDIO_SPK_WS GPIO_NUM_16

/* These thresholds describe the direct-I2S microphone's measured signal
 * range.  The shared command and wake state machines consume them through
 * profile-neutral aliases; they must not become Bread-only business policy. */
#define BREAD_AUDIO_COMMAND_SILENCE_MS 1200u
#define BREAD_AUDIO_COMMAND_START_CONFIRM_MS 80u
#define BREAD_AUDIO_COMMAND_START_LEVEL 55u
#define BREAD_AUDIO_COMMAND_SILENCE_FLOOR 20u
#define BREAD_AUDIO_COMMAND_SILENCE_MARGIN 15u
#define BREAD_AUDIO_COMMAND_SILENCE_CEILING 90u

#define BREAD_AUDIO_WAKE_WORD_DETECTION_THRESHOLD 0.20f
#define BREAD_AUDIO_WAKE_WORD_GAIN_NUM 1
#define BREAD_AUDIO_WAKE_WORD_GAIN_DEN 1


static inline const compact_audio_calibration_t *compact_audio_adapter_calibration(void) {
    static const compact_audio_calibration_t calibration = {
        .sample_rate = BREAD_AUDIO_SAMPLE_RATE,
        .command_silence_ms = BREAD_AUDIO_COMMAND_SILENCE_MS,
        .command_start_confirm_ms = BREAD_AUDIO_COMMAND_START_CONFIRM_MS,
        .command_start_level = BREAD_AUDIO_COMMAND_START_LEVEL,
        .command_silence_floor = BREAD_AUDIO_COMMAND_SILENCE_FLOOR,
        .command_silence_margin = BREAD_AUDIO_COMMAND_SILENCE_MARGIN,
        .command_silence_ceiling = BREAD_AUDIO_COMMAND_SILENCE_CEILING,
        .wake_word_detection_threshold = BREAD_AUDIO_WAKE_WORD_DETECTION_THRESHOLD,
        .wake_word_gain_num = BREAD_AUDIO_WAKE_WORD_GAIN_NUM,
        .wake_word_gain_den = BREAD_AUDIO_WAKE_WORD_GAIN_DEN,
        .output_volume_default = 70,
    };
    return &calibration;
}
/* I2S driver handles are physical objects: keep them private to this adapter.
 * The shared compact audio state machine only transfers ordinary PCM buffers
 * and retains session/ownership policy. */
static i2s_chan_handle_t s_bread_audio_rx;
static i2s_chan_handle_t s_bread_audio_tx;

static inline esp_err_t compact_audio_adapter_init_hardware(void) {
    if (s_bread_audio_rx || s_bread_audio_tx) return ESP_ERR_INVALID_STATE;
    i2s_chan_config_t config = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    config.dma_desc_num = 8;
    config.dma_frame_num = 256;
    i2s_chan_handle_t rx = NULL;
    i2s_chan_handle_t tx = NULL;
    esp_err_t err = i2s_new_channel(&config, NULL, &rx);
    if (err != ESP_OK) return err;
    i2s_std_config_t microphone = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(BREAD_AUDIO_SAMPLE_RATE),
        .slot_cfg = I2S_STD_MSB_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT,
                                                      I2S_SLOT_MODE_MONO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = BREAD_AUDIO_MIC_BCLK,
                     .ws = BREAD_AUDIO_MIC_WS, .dout = I2S_GPIO_UNUSED,
                     .din = BREAD_AUDIO_MIC_DIN, .invert_flags = {0}},
    };
    microphone.slot_cfg.slot_mask = I2S_STD_SLOT_LEFT;
    err = i2s_channel_init_std_mode(rx, &microphone);
    if (err == ESP_OK) err = i2s_channel_enable(rx);
    if (err != ESP_OK) goto fail;

    config.id = I2S_NUM_1;
    /* The amplifier must see silence after a completed DMA descriptor. */
    config.auto_clear_after_cb = true;
    err = i2s_new_channel(&config, &tx, NULL);
    if (err != ESP_OK) goto fail;
    const i2s_std_config_t speaker = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(BREAD_AUDIO_SAMPLE_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT,
                                                          I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = BREAD_AUDIO_SPK_BCLK,
                     .ws = BREAD_AUDIO_SPK_WS, .dout = BREAD_AUDIO_SPK_DOUT,
                     .din = I2S_GPIO_UNUSED, .invert_flags = {0}},
    };
    err = i2s_channel_init_std_mode(tx, &speaker);
    if (err == ESP_OK) {
        s_bread_audio_rx = rx;
        s_bread_audio_tx = tx;
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
    if (!s_bread_audio_rx || !buffer || !out_read) return ESP_ERR_INVALID_STATE;
    return i2s_channel_read(s_bread_audio_rx, buffer, bytes, out_read, timeout);
}

static inline esp_err_t compact_audio_adapter_write(const void *buffer, size_t bytes,
                                                     size_t *out_written,
                                                     TickType_t timeout) {
    if (!s_bread_audio_tx || !buffer || !out_written) return ESP_ERR_INVALID_STATE;
    return i2s_channel_write(s_bread_audio_tx, buffer, bytes, out_written, timeout);
}

static inline esp_err_t compact_audio_adapter_playback_begin(void) {
    if (!s_bread_audio_tx) return ESP_ERR_INVALID_STATE;
    return i2s_channel_enable(s_bread_audio_tx);
}

static inline esp_err_t compact_audio_adapter_playback_end(void) {
    if (!s_bread_audio_tx) return ESP_ERR_INVALID_STATE;
    return i2s_channel_disable(s_bread_audio_tx);
}

/* The shared wake state machine owns model lifecycle, pause/stop semantics
 * and callbacks. This board profile owns the worker scheduling footprint. */
static inline BaseType_t compact_audio_adapter_start_wake_recognizer_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreatePinnedToCore(entry, "maclaw_bread_wake", 10240,
                                   NULL, 4, out_task, 1);
}
