#pragma once

/*
 * Private codec-family adapter for the circular profiles.
 *
 * The shared circular renderer owns capture/playback session state, PCM
 * framing and wake-word arbitration.  ES7210/ES8311 register programming,
 * including the known-good 16 kHz clock coefficients and analogue mute
 * sequence, belongs below that boundary.  This header is intentionally not a
 * Device or Platform API: it is included only through round_profile_adapter.h
 * by the selected circular board implementation.
 */

#include <math.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <inttypes.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "esp_heap_caps.h"
#include "esp_err.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static esp_err_t round_audio_codec_write(i2c_master_dev_handle_t device,
                                         uint8_t reg, uint8_t value) {
    if (!device) return ESP_ERR_INVALID_STATE;
    const uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(device, bytes, sizeof(bytes), 1000);
}

static esp_err_t round_audio_codec_read(i2c_master_dev_handle_t device,
                                        uint8_t reg, uint8_t *value) {
    if (!device || !value) return ESP_ERR_INVALID_ARG;
    return i2c_master_transmit_receive(device, &reg, sizeof(reg), value,
                                       sizeof(*value), 1000);
}

/* ES8311 DAC volume is 0.5 dB/step.  Map the normalized Device API percentage
 * through a bounded 40 dB logarithmic taper and never apply its +gain range,
 * which clips voice playback on the tiny speaker. */
static uint8_t round_audio_adapter_volume_register(unsigned percent) {
    if (percent == 0) return 0;
    if (percent > 100) percent = 100;
    float db = 40.0f * log10f((float)percent / 100.0f);
    if (db < -95.5f) return 0;
    if (db > 0.0f) db = 0.0f;
    int reg = (int)lroundf(2.0f * db + 191.0f);
    if (reg < 0) reg = 0;
    if (reg > 0xBF) reg = 0xBF;
    return (uint8_t)reg;
}

static esp_err_t round_audio_adapter_set_output_volume(unsigned percent);

static esp_err_t round_audio_adapter_set_output_muted(bool muted);

static esp_err_t round_audio_adapter_restore_input_gain(void);

static esp_err_t round_audio_adapter_set_output_volume_with_device(
    i2c_master_dev_handle_t output, uint8_t volume_reg, unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    return round_audio_codec_write(output, volume_reg,
                                   round_audio_adapter_volume_register(percent));
}

/* Keep every DAC/reference path muted until a playback owner has queued the
 * first PCM block.  PA shutdown alone leaves analogue floor noise audible. */
static esp_err_t round_audio_adapter_set_output_muted_with_device(
    i2c_master_dev_handle_t output, uint8_t mute_reg, bool muted) {
    uint8_t value = 0;
    esp_err_t err = round_audio_codec_read(output, mute_reg, &value);
    if (err != ESP_OK) return err;
    value = muted ? (uint8_t)(value | 0x60u) : (uint8_t)(value & ~0x60u);
    return round_audio_codec_write(output, mute_reg, value);
}

static esp_err_t round_audio_adapter_initialize_input_codec(
    i2c_master_dev_handle_t input) {
    /* ES7210 microphone: preserve the tested 0x1A analogue gain.  Higher
     * values continuously clip speech on the circular boards. */
    static const uint8_t input_init[][2] = {
        {0x00,0xFF},{0x00,0x32},{0x09,0x30},{0x0A,0x30},
        {0x23,0x2A},{0x22,0x0A},{0x21,0x2A},{0x20,0x0A},
        {0x11,0x60},{0x12,0x00},{0x40,0xC3},{0x41,0x70},{0x42,0x70},
        {0x43,0x1A},{0x44,0x1A},{0x45,0x1A},{0x46,0x1A},
        {0x47,0x08},{0x48,0x08},{0x49,0x00},{0x4A,0x00},
        {0x07,0x20},{0x02,0xC1},{0x04,0x01},{0x05,0x00},
        {0x06,0x04},{0x4B,0x0F},{0x4C,0x0F},{0x00,0x71},{0x00,0x41},
    };
    for (size_t i = 0; i < sizeof(input_init) / sizeof(input_init[0]); ++i) {
        esp_err_t err = round_audio_codec_write(input, input_init[i][0], input_init[i][1]);
        if (err != ESP_OK) return err;
    }

    return ESP_OK;
}

static esp_err_t round_audio_adapter_initialize_output_codec(
    i2c_master_dev_handle_t output, uint8_t mute_reg, uint8_t volume_reg,
    unsigned output_volume) {
    /* ES8311 slave-mode 16 kHz coefficients for an external 4.096 MHz MCLK
     * and 16-bit Philips stereo wire format (32 BCLK/LRCK).  The DAC-side ADC
     * serial port remains disabled; capture is supplied by ES7210. */
    static const uint8_t output_init[][2] = {
        {0x00, 0x1F}, {0x00, 0x00}, {0x00, 0x80},
        {0x01, 0x3F}, {0x02, 0x00}, {0x03, 0x10}, {0x04, 0x20},
        {0x05, 0x00}, {0x06, 0x03}, {0x07, 0x00}, {0x08, 0xFF},
        {0x0B, 0x00}, {0x0C, 0x00}, {0x10, 0x1F}, {0x11, 0x7F},
        {0x09, 0x0C}, {0x0A, 0x4C}, {0x0D, 0x01}, {0x0E, 0x02},
        {0x12, 0x00}, {0x13, 0x10}, {0x14, 0x1A}, {0x15, 0x40},
        {0x16, 0x24}, {0x1B, 0x0A}, {0x1C, 0x6A}, {0x37, 0x08},
        {0x44, 0x58}, {0x45, 0x00}, {0x31, 0x60},
    };
    esp_err_t err = round_audio_codec_write(output, output_init[0][0], output_init[0][1]);
    if (err != ESP_OK) return err;
    vTaskDelay(pdMS_TO_TICKS(20));
    for (size_t i = 1; i < sizeof(output_init) / sizeof(output_init[0]); ++i) {
        err = round_audio_codec_write(output, output_init[i][0], output_init[i][1]);
        if (err != ESP_OK) return err;
    }
    (void)mute_reg; /* the init row above leaves the documented mute bits set */
    return round_audio_adapter_set_output_volume_with_device(
        output, volume_reg, output_volume);
}

static esp_err_t round_audio_adapter_restore_input_gain_with_device(
    i2c_master_dev_handle_t input) {
    for (uint8_t reg = 0x43; reg <= 0x46; ++reg) {
        esp_err_t err = round_audio_codec_write(input, reg, 0x1A);
        if (err != ESP_OK) return err;
    }
    return ESP_OK;
}

/* The PA GPIO is deliberately carried as a private profile value.  Session
 * policy decides when playback owns the amplifier; this adapter alone knows
 * how that intent reaches a physical output pin. */
static esp_err_t round_audio_adapter_initialize_power_amplifier(void) {
    const gpio_config_t config = {
        .pin_bit_mask = 1ULL << ROUND_PROFILE_AUDIO_PA_ENABLE,
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    esp_err_t err = gpio_config(&config);
    if (err != ESP_OK) return err;
    return gpio_set_level(ROUND_PROFILE_AUDIO_PA_ENABLE, 0);
}

static esp_err_t round_audio_adapter_set_power_amplifier(bool enabled) {
    return gpio_set_level(ROUND_PROFILE_AUDIO_PA_ENABLE, enabled ? 1 : 0);
}

/* The shared round code needs codec handles to run its normalized capture and
 * playback sessions, but must not know I²C port, pins or device addresses.
 * Keep allocation and exact reverse-order cleanup alongside those facts. */
static i2c_master_bus_handle_t s_round_audio_i2c_bus;
static i2c_master_dev_handle_t s_round_audio_input_codec;
static i2c_master_dev_handle_t s_round_audio_output_codec;

static void round_audio_adapter_release_codec_bus(void) {
    if (s_round_audio_output_codec) {
        (void)i2c_master_bus_rm_device(s_round_audio_output_codec);
        s_round_audio_output_codec = NULL;
    }
    if (s_round_audio_input_codec) {
        (void)i2c_master_bus_rm_device(s_round_audio_input_codec);
        s_round_audio_input_codec = NULL;
    }
    if (s_round_audio_i2c_bus) {
        (void)i2c_del_master_bus(s_round_audio_i2c_bus);
        s_round_audio_i2c_bus = NULL;
    }
}

static esp_err_t round_audio_adapter_open_codec_bus(void) {
    if (s_round_audio_i2c_bus) return ESP_ERR_INVALID_STATE;
    const i2c_master_bus_config_t bus_config = {
        .i2c_port = I2C_NUM_0,
        .sda_io_num = ROUND_PROFILE_AUDIO_I2C_SDA,
        .scl_io_num = ROUND_PROFILE_AUDIO_I2C_SCL,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .flags.enable_internal_pullup = true,
    };
    return i2c_new_master_bus(&bus_config, &s_round_audio_i2c_bus);
}

/* Touch/PMIC may share the physical bus.  Preserve the established order:
 * profile peripheral discovery first, then codec device attachment. */
static esp_err_t round_audio_adapter_attach_codecs(void) {
    if (!s_round_audio_i2c_bus || s_round_audio_input_codec ||
        s_round_audio_output_codec) return ESP_ERR_INVALID_STATE;
    const i2c_device_config_t input_config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = ROUND_PROFILE_AUDIO_ES7210_ADDRESS,
        .scl_speed_hz = 100000,
    };
    esp_err_t err = i2c_master_bus_add_device(
        s_round_audio_i2c_bus, &input_config, &s_round_audio_input_codec);
    if (err != ESP_OK) goto fail;
    const i2c_device_config_t output_config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = ROUND_PROFILE_AUDIO_ES8311_ADDRESS,
        .scl_speed_hz = 100000,
    };
    err = i2c_master_bus_add_device(
        s_round_audio_i2c_bus, &output_config, &s_round_audio_output_codec);
    if (err != ESP_OK) goto fail;
    return ESP_OK;

fail:
    /* The caller owns the shared bus and may still need it for a retryable
     * touch/peripheral rollback, so remove only codec devices here. */
    if (s_round_audio_output_codec) {
        (void)i2c_master_bus_rm_device(s_round_audio_output_codec);
        s_round_audio_output_codec = NULL;
    }
    if (s_round_audio_input_codec) {
        (void)i2c_master_bus_rm_device(s_round_audio_input_codec);
        s_round_audio_input_codec = NULL;
    }
    return err;
}

/* Full-duplex I2S is an electrical/profile contract: the two codecs share one
 * clock domain and the tested 16 kHz 32-BCLK stereo framing.  The renderer
 * receives opaque RX/TX handles and continues to own all session arbitration. */
static i2s_chan_handle_t s_round_audio_tx;
static i2s_chan_handle_t s_round_audio_rx;

static void round_audio_adapter_release_i2s(void) {
    if (s_round_audio_tx) {
        (void)i2s_channel_disable(s_round_audio_tx);
        (void)i2s_del_channel(s_round_audio_tx);
        s_round_audio_tx = NULL;
    }
    if (s_round_audio_rx) {
        (void)i2s_channel_disable(s_round_audio_rx);
        (void)i2s_del_channel(s_round_audio_rx);
        s_round_audio_rx = NULL;
    }
}

static esp_err_t round_audio_adapter_initialize_i2s(void) {
    if (s_round_audio_tx || s_round_audio_rx) return ESP_ERR_INVALID_STATE;
    i2s_chan_config_t channels = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    /* 4 x 256 frames per direction holds 64 ms but leaves contiguous internal
     * RAM for interaction tasks. Auto-clear prevents stale DMA audio on feed
     * gaps while TX remains enabled to preserve the shared microphone clock. */
    channels.dma_desc_num = 4;
    channels.dma_frame_num = 256;
    channels.auto_clear_after_cb = true;
    esp_err_t err = i2s_new_channel(&channels, &s_round_audio_tx, &s_round_audio_rx);
    if (err != ESP_OK) goto fail;

    i2s_std_config_t rx_config = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(ROUND_PROFILE_AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = ROUND_PROFILE_AUDIO_MCLK,
            .bclk = ROUND_PROFILE_AUDIO_BCLK,
            .ws = ROUND_PROFILE_AUDIO_WS,
            .dout = I2S_GPIO_UNUSED,
            .din = ROUND_PROFILE_AUDIO_DIN,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    rx_config.clk_cfg.mclk_multiple = ROUND_PROFILE_AUDIO_MCLK_MULTIPLE;
    err = i2s_channel_init_std_mode(s_round_audio_rx, &rx_config);
    if (err != ESP_OK) goto fail;

    i2s_std_config_t tx_config = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(ROUND_PROFILE_AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = ROUND_PROFILE_AUDIO_MCLK,
            .bclk = ROUND_PROFILE_AUDIO_BCLK,
            .ws = ROUND_PROFILE_AUDIO_WS,
            .dout = ROUND_PROFILE_AUDIO_DOUT,
            .din = I2S_GPIO_UNUSED,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    tx_config.clk_cfg.mclk_multiple = ROUND_PROFILE_AUDIO_MCLK_MULTIPLE;
    err = i2s_channel_init_std_mode(s_round_audio_tx, &tx_config);
    if (err != ESP_OK) goto fail;
    err = i2s_channel_enable(s_round_audio_tx);
    if (err != ESP_OK) goto fail;
    err = i2s_channel_enable(s_round_audio_rx);
    if (err != ESP_OK) goto fail;
    return ESP_OK;

fail:
    round_audio_adapter_release_i2s();
    return err;
}

static void round_audio_adapter_log_i2s_ready(const char *profile_name) {
    i2s_chan_info_t info = {0};
    if (s_round_audio_tx && i2s_channel_get_info(s_round_audio_tx, &info) == ESP_OK) {
        ESP_LOGI("round_audio", "%s audio ready: 16kHz STD stereo 16-bit "
                 "(MCLK=%" PRIu32 "Hz, BCLK=%" PRIu32 "Hz, 32 BCLK/LRCK)",
                 profile_name ? profile_name : "round", info.mclk_hz, info.bclk_hz);
    } else {
        ESP_LOGW("round_audio", "%s audio ready; I2S clock diagnostics unavailable",
                 profile_name ? profile_name : "round");
    }
}

/* Session code works only with normalized PCM buffers; the driver channels
 * remain private to this adapter. */
/* Wake-word PCM is consumed directly by an I2S DMA reader and then by the
 * recognizer.  Its placement is a physical audio constraint, so expose an
 * allocation role rather than leaking heap capability bits into the shared
 * wake/session state machine. */
/* Command WAV payloads are ordinary byte-addressable upload media, not I2S
 * DMA descriptors.  Keep their PSRAM placement and matched release inside
 * the selected Audio HAL so business code never assumes a heap family. */
static void *round_audio_adapter_allocate_command_wav(size_t bytes) {
    if (bytes == 0) return NULL;
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}

static void round_audio_adapter_free_command_wav(void *buffer) {
    heap_caps_free(buffer);
}
static void *round_audio_adapter_allocate_wake_capture_buffer(size_t bytes) {
    if (bytes == 0) return NULL;
    return heap_caps_malloc(bytes, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

static void round_audio_adapter_free_wake_capture_buffer(void *buffer) {
    heap_caps_free(buffer);
}

/* Wake dispatch runs only after the recognizer has relinquished its large
 * model buffers.  The task is audio-session infrastructure, so keep its
 * PSRAM stack placement in this private adapter instead of the shared wake
 * state machine. */
static BaseType_t round_audio_adapter_start_wake_dispatch_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreateWithCaps(entry, "maclaw_wake_dispatch", 3072, NULL, 5,
                               out_task, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
}
/* MultiNet owns a large model arena while it listens.  Its task placement and
 * CPU affinity are audio-runtime facts, not wake-word session policy. */
static BaseType_t round_audio_adapter_start_wake_recognizer_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreatePinnedToCore(entry, "maclaw_offline_wake", 10240, NULL,
                                   4, out_task, 1);
}
/* Capture arrives from the ES7210 as a profile-selected interleaved I2S
 * format.  Business/session code works in logical mono frames only; this
 * private seam owns the wire-frame size and selected microphone slot. */
static size_t round_audio_adapter_capture_wire_bytes(size_t frames) {
    return frames * ROUND_PROFILE_MIC_SLOT_COUNT * sizeof(int16_t);
}

static size_t round_audio_adapter_extract_capture_mono(
    const int16_t *wire, size_t wire_bytes, int16_t *mono, size_t mono_capacity) {
    if (!wire || !mono || mono_capacity == 0) return 0;
    const size_t available = wire_bytes /
                             (ROUND_PROFILE_MIC_SLOT_COUNT * sizeof(*wire));
    const size_t frames = available < mono_capacity ? available : mono_capacity;
    for (size_t frame = 0; frame < frames; ++frame) {
        mono[frame] = wire[frame * ROUND_PROFILE_MIC_SLOT_COUNT +
                           ROUND_PROFILE_MIC_SELECTED_SLOT];
    }
    return frames;
}

static esp_err_t round_audio_adapter_read_pcm(void *buffer, size_t capacity,
                                              size_t *received, TickType_t timeout) {
    if (!buffer || !received || capacity == 0) return ESP_ERR_INVALID_ARG;
    if (!s_round_audio_rx) return ESP_ERR_INVALID_STATE;
    return i2s_channel_read(s_round_audio_rx, buffer, capacity, received, timeout);
}

/* The shared renderer supplies ordinary mono/stereo PCM frames.  The selected
 * audio profile owns the electrical wire format, including the fact that both
 * circular boards drive an ES8311 through 16-bit STD stereo slots.  Keep the
 * slot expansion here so application/session code never needs to know that
 * physical bus constraint. */
static esp_err_t round_audio_adapter_write_pcm(const int16_t *pcm, size_t frames,
                                                unsigned channels, size_t *written,
                                                TickType_t timeout) {
    if (!pcm || !written || frames == 0 || frames > 256 ||
        (channels != 1 && channels != 2)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!s_round_audio_tx) return ESP_ERR_INVALID_STATE;
    int16_t slots[512];
    for (size_t i = 0; i < frames; ++i) {
        const int16_t left = pcm[i * channels];
        const int16_t right = channels == 2 ? pcm[i * 2 + 1] : left;
        slots[i * 2] = left;
        slots[i * 2 + 1] = right;
    }
    return i2s_channel_write(s_round_audio_tx, slots,
                             frames * 2 * sizeof(int16_t), written, timeout);
}

/* This is the profile-private physical-audio transaction.  A circular profile
 * may share I2C with touch/PMIC/IMU, so preserve the established order: create
 * bus, let the peripheral adapter claim its devices, then attach codecs.  The
 * shared renderer observes only success/failure and owns the session mutexes,
 * PCM conversion, wake arbitration and retry policy. */
static void round_audio_adapter_release(void) {
    round_audio_adapter_release_i2s();
    round_touch_adapter_deinit();
    round_audio_adapter_release_codec_bus();
}

static esp_err_t round_audio_adapter_initialize(unsigned output_volume) {
    round_audio_adapter_release();
    esp_err_t err = round_audio_adapter_open_codec_bus();
    if (err != ESP_OK) goto fail;

    const esp_err_t touch_err = round_touch_adapter_init(s_round_audio_i2c_bus);
    if (touch_err != ESP_OK) {
        if (round_touch_adapter_init_is_required()) {
            err = touch_err;
            goto fail;
        }
        ESP_LOGW("round_audio", "touch controller init deferred: %s",
                 esp_err_to_name(touch_err));
    }

    err = round_audio_adapter_attach_codecs();
    if (err != ESP_OK) goto fail;
    err = round_audio_adapter_initialize_input_codec(s_round_audio_input_codec);
    if (err != ESP_OK) goto fail;
    err = round_audio_adapter_initialize_i2s();
    if (err != ESP_OK) goto fail;
    err = round_audio_adapter_initialize_power_amplifier();
    if (err != ESP_OK) goto fail;
    err = round_audio_adapter_initialize_output_codec(
        s_round_audio_output_codec, ROUND_PROFILE_AUDIO_ES8311_DAC_MUTE_REG,
        ROUND_PROFILE_AUDIO_ES8311_DAC_VOLUME_REG, output_volume);
    if (err != ESP_OK) goto fail;
    round_audio_adapter_log_i2s_ready(ROUND_PROFILE_NAME);
    return ESP_OK;

fail:
    round_audio_adapter_release();
    return err;
}

/* PA and DAC timing is board electrical behaviour.  The shared playback
 * transaction asks for semantic phases only; this adapter preserves the
 * tested mute/settle/drain/power-down sequence for circular hardware. */
static esp_err_t round_audio_adapter_playback_prepare(void) {
    esp_err_t err = round_audio_adapter_set_output_muted(true);
    if (err != ESP_OK) return err;
    err = round_audio_adapter_set_power_amplifier(true);
    if (err != ESP_OK) return err;
    vTaskDelay(pdMS_TO_TICKS(10));
    return ESP_OK;
}

static esp_err_t round_audio_adapter_playback_reveal(void) {
    return round_audio_adapter_set_output_muted(false);
}

static esp_err_t round_audio_adapter_playback_abort(void) {
    return round_audio_adapter_set_power_amplifier(false);
}

static esp_err_t round_audio_adapter_playback_finish(
    const int16_t *silence, size_t silence_frames, size_t *written,
    TickType_t timeout) {
    if (!silence || !written || silence_frames == 0) return ESP_ERR_INVALID_ARG;
    /* Let the final queued descriptor leave the peripheral before the explicit
     * zero tail. TX stays enabled because RX shares the codec clock domain.
     * A failed DMA tail must not skip mute/PA teardown; retain its error after
     * the physical output has been made safe. */
    vTaskDelay(pdMS_TO_TICKS(20));
    esp_err_t result = round_audio_adapter_write_pcm(
        silence, silence_frames, 1, written, timeout);
    const size_t expected = silence_frames * 2 * sizeof(*silence);
    if (result == ESP_OK && *written != expected) result = ESP_ERR_TIMEOUT;
    vTaskDelay(pdMS_TO_TICKS(10));
    const esp_err_t mute_err = round_audio_adapter_set_output_muted(true);
    vTaskDelay(pdMS_TO_TICKS(5));
    const esp_err_t power_err = round_audio_adapter_set_power_amplifier(false);
    if (result != ESP_OK) return result;
    if (mute_err != ESP_OK) return mute_err;
    return power_err;
}
static esp_err_t round_audio_adapter_set_output_volume(unsigned percent) {
    return round_audio_adapter_set_output_volume_with_device(
        s_round_audio_output_codec, ROUND_PROFILE_AUDIO_ES8311_DAC_VOLUME_REG,
        percent);
}

static esp_err_t round_audio_adapter_set_output_muted(bool muted) {
    return round_audio_adapter_set_output_muted_with_device(
        s_round_audio_output_codec, ROUND_PROFILE_AUDIO_ES8311_DAC_MUTE_REG,
        muted);
}

static esp_err_t round_audio_adapter_restore_input_gain(void) {
    return round_audio_adapter_restore_input_gain_with_device(
        s_round_audio_input_codec);
}
