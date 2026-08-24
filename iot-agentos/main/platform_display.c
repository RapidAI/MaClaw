#include "platform_display.h"

#include "platform_display_profile.h"

bool platform_display_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget) {
    return platform_display_profile_get_pet_asset_install_budget(
        source_width, source_height, frame_count, out_budget);
}

void platform_display_set_command_lock(bool locked) {
    platform_display_profile_set_command_lock(locked);
}

device_status_t platform_display_set_brightness(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_display_profile_set_brightness(percent);
}

device_status_t platform_display_enter_display_off(void) {
    return platform_display_profile_enter_display_off();
}

device_status_t platform_display_wake_display(void) {
    return platform_display_profile_wake_display();
}

device_status_t platform_display_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_display_profile_prepare_system_sleep(timeout_ms);
}

void platform_display_abort_system_sleep_prepare(void) {
    platform_display_profile_abort_system_sleep_prepare();
}

bool platform_display_is_off(void) {
    return platform_display_profile_is_off();
}

void platform_display_show_startup(void) { platform_display_profile_show_startup(); }
void platform_display_set_pet_state(const char *state) { platform_display_profile_set_pet_state(state); }
void platform_display_set_command_stage(const char *stage) { platform_display_profile_set_command_stage(stage); }
void platform_display_set_pet_profile(const char *skin, bool motion_enabled) {
    platform_display_profile_set_pet_profile(skin, motion_enabled);
}
device_status_t platform_display_set_pet_asset(const uint8_t *const *frames,
                                                uint32_t frame_count,
                                                uint32_t width, uint32_t height,
                                                uint32_t frame_ms) {
    return platform_display_profile_set_pet_asset(frames, frame_count, width, height, frame_ms);
}
device_status_t platform_display_set_pet_asset_consuming(uint8_t **frames,
                                                          uint32_t frame_count,
                                                          uint32_t width,
                                                          uint32_t height,
                                                          uint32_t frame_ms) {
    return platform_display_profile_set_pet_asset_consuming(frames, frame_count, width, height,
                                                             frame_ms);
}
void platform_display_set_recording_mode(bool meeting) {
    platform_display_profile_set_recording_mode(meeting);
}
void platform_display_set_recording_visual(bool active, bool paused,
                                           uint32_t elapsed_seconds) {
    platform_display_profile_set_recording_visual(active, paused, elapsed_seconds);
}
void platform_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    platform_display_profile_set_audio_level(level, elapsed_seconds);
}
void platform_display_show_text(const char *title, const char *text) {
    platform_display_profile_show_text(title, text);
}
void platform_display_show_upload_progress(uint32_t completed_bytes,
                                           uint32_t total_bytes,
                                           const char *stage) {
    platform_display_profile_show_upload_progress(completed_bytes, total_bytes, stage);
}
void platform_display_show_response(const char *title, const char *text) {
    platform_display_profile_show_response(title, text);
}
void platform_display_show_response_image(const char *title, const char *caption,
                                          const uint16_t *pixels,
                                          uint32_t width, uint32_t height) {
    platform_display_profile_show_response_image(title, caption, pixels, width, height);
}
bool platform_display_navigate_response(int page_delta) {
    return platform_display_profile_navigate_response(page_delta);
}
bool platform_display_get_response_page(uint32_t *out_page) {
    return platform_display_profile_get_response_page(out_page);
}
bool platform_display_restore_response_page(uint32_t page) {
    return platform_display_profile_restore_response_page(page);
}
int platform_display_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return platform_display_profile_cache_glyph(codepoint, bitmap);
}
void platform_display_show_qrcode_modules(const uint8_t *modules,
                                          uint32_t module_count,
                                          const char *ssid) {
    platform_display_profile_show_qrcode_modules(modules, module_count, ssid);
}
void platform_display_show_ready_prompt(const char *title, const char *text) {
    platform_display_profile_show_ready_prompt(title, text);
}
void platform_display_cancel_ready_prompt(void) { platform_display_profile_cancel_ready_prompt(); }
void platform_display_set_wifi_status(const char *ssid, bool connected) {
    platform_display_profile_set_wifi_status(ssid, connected);
}
void platform_display_set_service_ready(bool ready) { platform_display_profile_set_service_ready(ready); }
void platform_display_set_ambient(const char *time, const char *location,
                                  const char *date, const char *weekday,
                                  const char *weather_summary,
                                  int temperature_c, bool weather_valid,
                                  bool weather_stale) {
    platform_display_profile_set_ambient(time, location, date, weekday, weather_summary,
                                         temperature_c, weather_valid, weather_stale);
}
void platform_display_set_alarm_scheduled(bool scheduled) {
    platform_display_profile_set_alarm_scheduled(scheduled);
}
void platform_display_set_alarm_visual(bool active, uint32_t frame,
                                       const char *time_text, const char *label,
                                       uint32_t attempt, uint32_t max_attempts) {
    platform_display_profile_set_alarm_visual(active, frame, time_text, label, attempt,
                                              max_attempts);
}
