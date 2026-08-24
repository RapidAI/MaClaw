#include "platform_display_profile.h"

#include <stddef.h>

#include "esp_timer.h"
#include "compact_display_service.h"
#include "legacy_display_scene.h"

static device_status_t compact_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_FAIL: return DEVICE_STATUS_IO_ERROR;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

bool platform_display_profile_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget) {
    if (!out_budget) return false;
    size_t total_bytes = 0, max_allocation_bytes = 0, max_frame_count = 0;
    if (!legacy_display_scene_get_pet_asset_install_budget(
            source_width, source_height, frame_count, &total_bytes,
            &max_allocation_bytes, &max_frame_count) ||
        total_bytes > UINT32_MAX || max_allocation_bytes > UINT32_MAX ||
        max_frame_count > UINT32_MAX) return false;
    *out_budget = (device_display_pet_asset_install_budget_t){
        .struct_size = sizeof(*out_budget),
        .abi_version = DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION,
        .total_external_bytes = (uint32_t)total_bytes,
        .max_external_allocation_bytes = (uint32_t)max_allocation_bytes,
        .max_frame_count = (uint32_t)max_frame_count,
    };
    return true;
}
void platform_display_profile_set_command_lock(bool locked) { legacy_display_scene_set_command_lock(locked); }
device_status_t platform_display_profile_set_brightness(uint8_t percent) { return compact_status_from_esp_err(legacy_display_scene_set_brightness(percent)); }
device_status_t platform_display_profile_enter_display_off(void) { return compact_status_from_esp_err(legacy_display_scene_enter_display_off()); }
device_status_t platform_display_profile_wake_display(void) { return compact_status_from_esp_err(legacy_display_scene_wake_display()); }
device_status_t platform_display_profile_prepare_system_sleep(uint32_t timeout_ms) {
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    esp_err_t status = compact_display_service_prepare_system_sleep(timeout_ms);
    if (status != ESP_OK) return compact_status_from_esp_err(status);
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    const uint32_t remaining_ms = remaining_us <= 0 ? 0 :
        (uint32_t)(((uint64_t)remaining_us + 999u) / 1000u);
    status = remaining_ms
                 ? compact_display_service_wait_for_scanout_idle(remaining_ms)
                 : ESP_ERR_TIMEOUT;
    if (status == ESP_OK) return DEVICE_STATUS_OK;
    /* Do not reopen the private animation/scan-out fence here. Display
     * Service keeps its semantic admission closed too, and Power owns the
     * single reverse-order rollback that releases both boundaries. */
    return compact_status_from_esp_err(status);
}
void platform_display_profile_abort_system_sleep_prepare(void) {
    compact_display_service_abort_system_sleep_prepare();
}
bool platform_display_profile_is_off(void) { return legacy_display_scene_is_off(); }
void platform_display_profile_show_startup(void) { legacy_display_scene_show_startup(); }
void platform_display_profile_set_pet_state(const char *state) { legacy_display_scene_set_pet_state(state); }
void platform_display_profile_set_command_stage(const char *stage) { legacy_display_scene_set_command_stage(stage); }
void platform_display_profile_set_pet_profile(const char *skin, bool motion_enabled) { legacy_display_scene_set_pet_profile(skin, motion_enabled); }
device_status_t platform_display_profile_set_pet_asset(const uint8_t *const *frames, uint32_t frame_count, uint32_t width, uint32_t height, uint32_t frame_ms) { return compact_status_from_esp_err(legacy_display_scene_set_pet_asset(frames, frame_count, width, height, frame_ms)); }
device_status_t platform_display_profile_set_pet_asset_consuming(uint8_t **frames, uint32_t frame_count, uint32_t width, uint32_t height, uint32_t frame_ms) { return compact_status_from_esp_err(legacy_display_scene_set_pet_asset_consuming(frames, frame_count, width, height, frame_ms)); }
void platform_display_profile_set_recording_mode(bool meeting) { legacy_display_scene_set_recording_mode(meeting); }
void platform_display_profile_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds) { legacy_display_scene_set_recording_visual(active, paused, elapsed_seconds); }
void platform_display_profile_set_audio_level(uint16_t level, uint32_t elapsed_seconds) { legacy_display_scene_set_audio_level(level, elapsed_seconds); }
void platform_display_profile_show_text(const char *title, const char *text) { legacy_display_scene_show_text(title, text); }
void platform_display_profile_show_upload_progress(uint32_t completed_bytes, uint32_t total_bytes, const char *stage) { legacy_display_scene_show_upload_progress(completed_bytes, total_bytes, stage); }
void platform_display_profile_show_response(const char *title, const char *text) { legacy_display_scene_show_response(title, text); }
void platform_display_profile_show_response_image(const char *title, const char *caption, const uint16_t *pixels, uint32_t width, uint32_t height) { legacy_display_scene_show_response_image(title, caption, pixels, width, height); }
bool platform_display_profile_navigate_response(int page_delta) { return legacy_display_scene_navigate_response(page_delta); }
bool platform_display_profile_get_response_page(uint32_t *out_page) { if (!out_page) return false; unsigned page = 0; const bool available = legacy_display_scene_get_response_page(&page); if (available) *out_page = page; return available; }
bool platform_display_profile_restore_response_page(uint32_t page) { return legacy_display_scene_restore_response_page(page); }
int platform_display_profile_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) { return legacy_display_scene_cache_glyph(codepoint, bitmap); }
void platform_display_profile_show_qrcode_modules(const uint8_t *modules, uint32_t module_count, const char *ssid) { legacy_display_scene_show_qrcode_modules(modules, module_count, ssid); }
void platform_display_profile_show_ready_prompt(const char *title, const char *text) { legacy_display_scene_show_ready_prompt(title, text); }
void platform_display_profile_cancel_ready_prompt(void) { legacy_display_scene_cancel_ready_prompt(); }
void platform_display_profile_set_wifi_status(const char *ssid, bool connected) { legacy_display_scene_set_wifi_status(ssid, connected); }
void platform_display_profile_set_service_ready(bool ready) { legacy_display_scene_set_service_ready(ready); }
void platform_display_profile_set_ambient(const char *time, const char *location, const char *date, const char *weekday, const char *weather_summary, int temperature_c, bool weather_valid, bool weather_stale) { legacy_display_scene_set_ambient(time, location, date, weekday, weather_summary, temperature_c, weather_valid, weather_stale); }
void platform_display_profile_set_alarm_scheduled(bool scheduled) { legacy_display_scene_set_alarm_scheduled(scheduled); }
void platform_display_profile_set_alarm_visual(bool active, uint32_t frame, const char *time_text, const char *label, uint32_t attempt, uint32_t max_attempts) { legacy_display_scene_set_alarm_visual(active, frame, time_text, label, attempt, max_attempts); }
