#pragma once

/*
 * Selected circular audio/input profile.
 *
 * This profile is private to the round Audio and Input HAL source owners. It
 * intentionally carries the electrical codec wiring needed by those owners,
 * but it is never included by the common renderer or by a Device/Platform
 * API. Visual/layout selection lives separately in round_visual_profile.
 */

#include "round_audio_profile.h"

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C

#include "waveshare_amoled_1_75c/waveshare_audio_profile_adapter.h"

static inline const round_audio_profile_t *round_audio_profile_adapter(void) {
    static const round_audio_profile_t profile = {
        .name = "Waveshare 1.75C",
        .touch_initialization_required = true,
        .i2c_scl = WAVESHARE_AUDIO_I2C_SCL, .i2c_sda = WAVESHARE_AUDIO_I2C_SDA,
        .mclk = WAVESHARE_AUDIO_MCLK, .bclk = WAVESHARE_AUDIO_BCLK,
        .ws = WAVESHARE_AUDIO_WS, .dout = WAVESHARE_AUDIO_DOUT,
        .din = WAVESHARE_AUDIO_DIN, .power_amplifier_enable = WAVESHARE_AUDIO_PA_ENABLE,
        .input_codec_address = WAVESHARE_AUDIO_ES7210_ADDRESS,
        .output_codec_address = WAVESHARE_AUDIO_ES8311_ADDRESS,
        .output_mute_register = WAVESHARE_AUDIO_ES8311_DAC_MUTE_REG,
        .output_volume_register = WAVESHARE_AUDIO_ES8311_DAC_VOLUME_REG,
        .output_volume_default = WAVESHARE_AUDIO_OUTPUT_VOLUME_DEFAULT,
        .microphone_slot_count = 2, .microphone_selected_slot = 0,
        .sample_rate = WAVESHARE_AUDIO_RATE,
        .mclk_multiple = WAVESHARE_AUDIO_MCLK_MULTIPLE,
    };
    return &profile;
}

#else

/* Audio needs codec wiring only; display and touch implementations have
 * independent Display and Peripheral source owners. */
#include "echoear_2st/echoear_audio_profile_adapter.h"

static inline const round_audio_profile_t *round_audio_profile_adapter(void) {
    static const round_audio_profile_t profile = {
        .name = "EchoEar-2ST",
        .touch_initialization_required = false,
        .i2c_scl = ECHOEAR_AUDIO_I2C_SCL, .i2c_sda = ECHOEAR_AUDIO_I2C_SDA,
        .mclk = ECHOEAR_AUDIO_MCLK, .bclk = ECHOEAR_AUDIO_BCLK,
        .ws = ECHOEAR_AUDIO_WS, .dout = ECHOEAR_AUDIO_DOUT,
        .din = ECHOEAR_AUDIO_DIN, .power_amplifier_enable = ECHOEAR_AUDIO_PA_ENABLE,
        .input_codec_address = ECHOEAR_AUDIO_ES7210_ADDRESS,
        .output_codec_address = ECHOEAR_AUDIO_ES8311_ADDRESS,
        .output_mute_register = ECHOEAR_AUDIO_ES8311_DAC_MUTE_REG,
        .output_volume_register = ECHOEAR_AUDIO_ES8311_DAC_VOLUME_REG,
        .output_volume_default = ECHOEAR_AUDIO_OUTPUT_VOLUME_DEFAULT,
        .microphone_slot_count = 2, .microphone_selected_slot = 0,
        .sample_rate = ECHOEAR_AUDIO_RATE,
        .mclk_multiple = ECHOEAR_AUDIO_MCLK_MULTIPLE,
    };
    return &profile;
}

#endif
