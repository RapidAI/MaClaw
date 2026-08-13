#pragma once

/*
 * Selected circular-product visual contract.
 *
 * This deliberately contains display geometry, safe-scene layout and font
 * raster choices only.  It must not include a panel, codec, touch, GPIO,
 * I2C/I2S, DMA or FreeRTOS adapter: those are source-owned by the hardware
 * services.  A new circular screen therefore adds its visual adapters here
 * independently of its electrical HAL implementation.
 */

#include "round_display_font_profile.h"
#include "round_alarm_layout.h"
#include "round_display_layout.h"
#include "round_message_layout.h"
#include "round_qrcode_layout.h"
#include "round_recording_layout.h"
#include "round_response_image_layout.h"
#include "round_upload_layout.h"

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C

#include "waveshare_amoled_1_75c/waveshare_round_font_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_recording_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_upload_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_alarm_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_message_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_response_image_layout_adapter.h"
#include "waveshare_amoled_1_75c/waveshare_round_qrcode_layout_adapter.h"

static inline const round_display_layout_t *round_visual_adapter_display_layout(void) {
    return waveshare_round_display_layout();
}
static inline const round_display_font_profile_t *round_visual_adapter_font(void) {
    return waveshare_round_font_profile();
}
static inline const round_recording_layout_t *round_visual_adapter_recording_layout(void) {
    return waveshare_round_recording_layout();
}
static inline const round_upload_layout_t *round_visual_adapter_upload_layout(void) {
    return waveshare_round_upload_layout();
}
static inline const round_alarm_layout_t *round_visual_adapter_alarm_layout(void) {
    return waveshare_round_alarm_layout();
}
static inline const round_message_layout_t *round_visual_adapter_message_layout(void) {
    return waveshare_round_message_layout();
}
static inline const round_response_image_layout_t *
round_visual_adapter_response_image_layout(void) {
    return waveshare_round_response_image_layout();
}
static inline const round_qrcode_layout_t *round_visual_adapter_qrcode_layout(void) {
    return waveshare_round_qrcode_layout();
}

#else

#include "echoear_2st/echoear_round_font_adapter.h"
#include "echoear_2st/echoear_round_layout_adapter.h"
#include "echoear_2st/echoear_round_recording_layout_adapter.h"
#include "echoear_2st/echoear_round_upload_layout_adapter.h"
#include "echoear_2st/echoear_round_alarm_layout_adapter.h"
#include "echoear_2st/echoear_round_message_layout_adapter.h"
#include "echoear_2st/echoear_round_response_image_layout_adapter.h"
#include "echoear_2st/echoear_round_qrcode_layout_adapter.h"

static inline const round_display_layout_t *round_visual_adapter_display_layout(void) {
    return echoear_round_display_layout();
}
static inline const round_display_font_profile_t *round_visual_adapter_font(void) {
    return echoear_round_font_profile();
}
static inline const round_recording_layout_t *round_visual_adapter_recording_layout(void) {
    return echoear_round_recording_layout();
}
static inline const round_upload_layout_t *round_visual_adapter_upload_layout(void) {
    return echoear_round_upload_layout();
}
static inline const round_alarm_layout_t *round_visual_adapter_alarm_layout(void) {
    return echoear_round_alarm_layout();
}
static inline const round_message_layout_t *round_visual_adapter_message_layout(void) {
    return echoear_round_message_layout();
}
static inline const round_response_image_layout_t *
round_visual_adapter_response_image_layout(void) {
    return echoear_round_response_image_layout();
}
static inline const round_qrcode_layout_t *round_visual_adapter_qrcode_layout(void) {
    return echoear_round_qrcode_layout();
}

#endif
