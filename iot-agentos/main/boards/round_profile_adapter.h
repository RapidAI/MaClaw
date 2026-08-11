#pragma once

#include "round_display_font_profile.h"

/*
 * Private profile-selection seam for the shared circular renderer.
 *
 * `board_port.c` owns scene composition and normalized input/audio session
 * state; it must not contain controller, codec or pin selection.  CMake
 * selects only the circular board-port source, while this private adapter
 * selects the matching electrical/layout implementation for that source.
 * No declaration from this file is part of Device API or Platform API.
 */

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C

#include "waveshare_amoled_1_75c/waveshare_peripheral_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_display_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_font_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_recording_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_upload_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_alarm_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_message_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_response_image_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_qrcode_layout_adapter.h"

#define ROUND_PROFILE_LCD_WIDTH                    WAVESHARE_DISPLAY_WIDTH
#define ROUND_PROFILE_LCD_HEIGHT                   WAVESHARE_DISPLAY_HEIGHT
#define ROUND_PROFILE_DISPLAY_STRIPE_ROWS          WAVESHARE_DISPLAY_STRIPE_ROWS
#define ROUND_PROFILE_NAME                         "Waveshare 1.75C"
#define ROUND_PROFILE_AUDIO_I2C_SCL                WAVESHARE_AUDIO_I2C_SCL
#define ROUND_PROFILE_AUDIO_I2C_SDA                WAVESHARE_AUDIO_I2C_SDA
#define ROUND_PROFILE_AUDIO_MCLK                   WAVESHARE_AUDIO_MCLK
#define ROUND_PROFILE_AUDIO_BCLK                   WAVESHARE_AUDIO_BCLK
#define ROUND_PROFILE_AUDIO_WS                     WAVESHARE_AUDIO_WS
#define ROUND_PROFILE_AUDIO_DOUT                   WAVESHARE_AUDIO_DOUT
#define ROUND_PROFILE_AUDIO_DIN                    WAVESHARE_AUDIO_DIN
#define ROUND_PROFILE_AUDIO_PA_ENABLE              WAVESHARE_AUDIO_PA_ENABLE
#define ROUND_PROFILE_AUDIO_ES7210_ADDRESS         WAVESHARE_AUDIO_ES7210_ADDRESS
#define ROUND_PROFILE_AUDIO_ES8311_ADDRESS         WAVESHARE_AUDIO_ES8311_ADDRESS
#define ROUND_PROFILE_AUDIO_ES8311_DAC_MUTE_REG    WAVESHARE_AUDIO_ES8311_DAC_MUTE_REG
#define ROUND_PROFILE_AUDIO_ES8311_DAC_VOLUME_REG  WAVESHARE_AUDIO_ES8311_DAC_VOLUME_REG
#define ROUND_PROFILE_AUDIO_OUTPUT_VOLUME_DEFAULT  WAVESHARE_AUDIO_OUTPUT_VOLUME_DEFAULT
#define ROUND_PROFILE_AUDIO_RATE                   WAVESHARE_AUDIO_RATE
#define ROUND_PROFILE_AUDIO_MCLK_MULTIPLE          WAVESHARE_AUDIO_MCLK_MULTIPLE
/* The selected ES7210 path is transported as ordinary STD stereo I2S. */
#define ROUND_PROFILE_MIC_SLOT_COUNT                2u
#define ROUND_PROFILE_MIC_SELECTED_SLOT             0u
static inline const round_display_layout_t *round_profile_display_layout(void) {
    return waveshare_round_display_layout();
}

static inline const round_display_font_profile_t *round_profile_font(void) {
    return waveshare_round_font_profile();
}

static inline const round_recording_layout_t *round_profile_recording_layout(void) {
    return waveshare_round_recording_layout();
}

static inline const round_upload_layout_t *round_profile_upload_layout(void) {
    return waveshare_round_upload_layout();
}

static inline const round_alarm_layout_t *round_profile_alarm_layout(void) {
    return waveshare_round_alarm_layout();
}

static inline const round_message_layout_t *round_profile_message_layout(void) {
    return waveshare_round_message_layout();
}

static inline const round_response_image_layout_t *round_profile_response_image_layout(void) {
    return waveshare_round_response_image_layout();
}

static inline const round_qrcode_layout_t *round_profile_qrcode_layout(void) {
    return waveshare_round_qrcode_layout();
}

#else

#include "echoear_2st/echoear_hardware_adapter.h"
#include "echoear_2st/echoear_round_font_adapter.h"
#include "echoear_2st/echoear_round_layout_adapter.h"
#include "echoear_2st/echoear_round_recording_layout_adapter.h"
#include "echoear_2st/echoear_round_upload_layout_adapter.h"
#include "echoear_2st/echoear_round_alarm_layout_adapter.h"
#include "echoear_2st/echoear_round_message_layout_adapter.h"
#include "echoear_2st/echoear_round_response_image_layout_adapter.h"
#include "echoear_2st/echoear_round_qrcode_layout_adapter.h"

#define ROUND_PROFILE_LCD_WIDTH                    ECHOEAR_DISPLAY_WIDTH
#define ROUND_PROFILE_LCD_HEIGHT                   ECHOEAR_DISPLAY_HEIGHT
#define ROUND_PROFILE_DISPLAY_STRIPE_ROWS          ECHOEAR_DISPLAY_STRIPE_ROWS
#define ROUND_PROFILE_NAME                         "EchoEar-2ST"
#define ROUND_PROFILE_AUDIO_I2C_SCL                ECHOEAR_AUDIO_I2C_SCL
#define ROUND_PROFILE_AUDIO_I2C_SDA                ECHOEAR_AUDIO_I2C_SDA
#define ROUND_PROFILE_AUDIO_MCLK                   ECHOEAR_AUDIO_MCLK
#define ROUND_PROFILE_AUDIO_BCLK                   ECHOEAR_AUDIO_BCLK
#define ROUND_PROFILE_AUDIO_WS                     ECHOEAR_AUDIO_WS
#define ROUND_PROFILE_AUDIO_DOUT                   ECHOEAR_AUDIO_DOUT
#define ROUND_PROFILE_AUDIO_DIN                    ECHOEAR_AUDIO_DIN
#define ROUND_PROFILE_AUDIO_PA_ENABLE              ECHOEAR_AUDIO_PA_ENABLE
#define ROUND_PROFILE_AUDIO_ES7210_ADDRESS         ECHOEAR_AUDIO_ES7210_ADDRESS
#define ROUND_PROFILE_AUDIO_ES8311_ADDRESS         ECHOEAR_AUDIO_ES8311_ADDRESS
#define ROUND_PROFILE_AUDIO_ES8311_DAC_MUTE_REG    ECHOEAR_AUDIO_ES8311_DAC_MUTE_REG
#define ROUND_PROFILE_AUDIO_ES8311_DAC_VOLUME_REG  ECHOEAR_AUDIO_ES8311_DAC_VOLUME_REG
#define ROUND_PROFILE_AUDIO_OUTPUT_VOLUME_DEFAULT  ECHOEAR_AUDIO_OUTPUT_VOLUME_DEFAULT
#define ROUND_PROFILE_AUDIO_RATE                   ECHOEAR_AUDIO_RATE
#define ROUND_PROFILE_AUDIO_MCLK_MULTIPLE          ECHOEAR_AUDIO_MCLK_MULTIPLE
/* The selected ES7210 path is transported as ordinary STD stereo I2S. */
#define ROUND_PROFILE_MIC_SLOT_COUNT                2u
#define ROUND_PROFILE_MIC_SELECTED_SLOT             0u
static inline const round_display_layout_t *round_profile_display_layout(void) {
    return echoear_round_display_layout();
}

static inline const round_display_font_profile_t *round_profile_font(void) {
    return echoear_round_font_profile();
}

static inline const round_recording_layout_t *round_profile_recording_layout(void) {
    return echoear_round_recording_layout();
}

static inline const round_upload_layout_t *round_profile_upload_layout(void) {
    return echoear_round_upload_layout();
}

static inline const round_alarm_layout_t *round_profile_alarm_layout(void) {
    return echoear_round_alarm_layout();
}

static inline const round_message_layout_t *round_profile_message_layout(void) {
    return echoear_round_message_layout();
}

static inline const round_response_image_layout_t *round_profile_response_image_layout(void) {
    return echoear_round_response_image_layout();
}

static inline const round_qrcode_layout_t *round_profile_qrcode_layout(void) {
    return echoear_round_qrcode_layout();
}

#endif

/* This is a codec-family implementation detail, not an app/Device API. */
#include "round_audio_codec_adapter.h"
