#pragma once

/*
 * Normalized circular-board audio facts.
 *
 * This private HAL seam deliberately uses plain scalar values: it does not
 * expose GPIO, I2S, I2C, heap or task-driver types to the shared circular
 * renderer. A new round board supplies one instance in its selected profile;
 * codec transport is owned by the private round_audio_service source.
 */

#include <stdbool.h>
#include <stdint.h>

typedef struct {
    const char *name;
    /* Touch is electrically initialized on the audio-created I2C bus. This
     * flag is therefore an Audio-profile availability policy, not an Input
     * gesture policy. */
    bool touch_initialization_required;
    int i2c_scl;
    int i2c_sda;
    int mclk;
    int bclk;
    int ws;
    int dout;
    int din;
    int power_amplifier_enable;
    uint8_t input_codec_address;
    uint8_t output_codec_address;
    uint8_t output_mute_register;
    uint8_t output_volume_register;
    uint8_t output_volume_default;
    uint8_t microphone_slot_count;
    uint8_t microphone_selected_slot;
    uint32_t sample_rate;
    uint32_t mclk_multiple;
} round_audio_profile_t;
