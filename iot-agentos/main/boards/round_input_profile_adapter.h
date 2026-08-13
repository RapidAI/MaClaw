#pragma once

/* Selected round Input profile.  This adapter has a single source owner in
 * round_input_profile_service.c, keeping GPIO and scanner scheduling out of
 * the shared gesture classifier and every other hardware source owner. */

#include "sdkconfig.h"

#if CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#include "waveshare_amoled_1_75c/waveshare_input_adapter.h"

static inline const round_input_profile_t *round_selected_input_profile(void) {
    return waveshare_input_profile();
}
static inline esp_err_t round_selected_input_initialize_activate_key(void) {
    return waveshare_input_initialize_activate_key();
}
static inline bool round_selected_input_activate_key_pressed(void) {
    return waveshare_input_activate_key_pressed();
}
static inline device_input_source_t round_selected_input_resolve_source(bool key_pressed,
                                                                          bool touch_pressed) {
    return waveshare_input_resolve_source(key_pressed, touch_pressed);
}
static inline bool round_selected_input_consume_boot_gesture(device_input_action_t action,
                                                               device_input_source_t source) {
    return waveshare_input_consume_boot_gesture(action, source);
}
static inline BaseType_t round_selected_input_start_scan_task(TaskFunction_t entry,
                                                               TaskHandle_t *out_task) {
    return waveshare_input_start_scan_task(entry, out_task);
}
#else
#include "echoear_2st/echoear_input_adapter.h"

static inline const round_input_profile_t *round_selected_input_profile(void) {
    return echoear_input_profile();
}
static inline esp_err_t round_selected_input_initialize_activate_key(void) {
    return echoear_input_initialize_activate_key();
}
static inline bool round_selected_input_activate_key_pressed(void) {
    return echoear_input_activate_key_pressed();
}
static inline device_input_source_t round_selected_input_resolve_source(bool key_pressed,
                                                                          bool touch_pressed) {
    return echoear_input_resolve_source(key_pressed, touch_pressed);
}
static inline bool round_selected_input_consume_boot_gesture(device_input_action_t action,
                                                               device_input_source_t source) {
    return echoear_input_consume_boot_gesture(action, source);
}
static inline BaseType_t round_selected_input_start_scan_task(TaskFunction_t entry,
                                                               TaskHandle_t *out_task) {
    return echoear_input_start_scan_task(entry, out_task);
}
#endif
