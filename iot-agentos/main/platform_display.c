#include "platform_display.h"

#include "board_port.h"

static device_status_t status_from_esp_err(esp_err_t err) {
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

bool platform_display_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget) {
    if (!out_budget) return false;
    size_t total_bytes = 0;
    size_t max_allocation_bytes = 0;
    size_t max_frame_count = 0;
    if (!board_port_get_pet_asset_install_budget(source_width, source_height,
                                                   frame_count, &total_bytes,
                                                   &max_allocation_bytes,
                                                   &max_frame_count) ||
        total_bytes > UINT32_MAX || max_allocation_bytes > UINT32_MAX ||
        max_frame_count > UINT32_MAX) {
        return false;
    }
    *out_budget = (device_display_pet_asset_install_budget_t){
        .struct_size = sizeof(*out_budget),
        .abi_version = DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION,
        .total_external_bytes = (uint32_t)total_bytes,
        .max_external_allocation_bytes = (uint32_t)max_allocation_bytes,
        .max_frame_count = (uint32_t)max_frame_count,
    };
    return true;
}

void platform_display_set_command_lock(bool locked) {
    board_port_set_command_display_lock(locked);
}

device_status_t platform_display_set_brightness(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_set_display_brightness(percent));
}

bool platform_display_enter_display_off(void) {
    return board_port_enter_display_off();
}

bool platform_display_wake_display(void) {
    return board_port_wake_from_idle();
}

bool platform_display_is_off(void) {
    return board_port_display_is_off();
}

void platform_display_show_startup(void) { board_port_show_startup_screen(); }
void platform_display_set_pet_state(const char *state) { board_port_set_pet_state(state); }
void platform_display_set_command_stage(const char *stage) { board_port_set_command_stage(stage); }
void platform_display_set_pet_profile(const char *skin, bool motion_enabled) {
    board_port_set_pet_profile(skin, motion_enabled);
}
device_status_t platform_display_set_pet_asset(const uint8_t *const *frames,
                                                uint32_t frame_count,
                                                uint32_t width, uint32_t height,
                                                uint32_t frame_ms) {
    return status_from_esp_err(board_port_set_pet_asset(
        frames, frame_count, width, height, frame_ms));
}
device_status_t platform_display_set_pet_asset_consuming(uint8_t **frames,
                                                          uint32_t frame_count,
                                                          uint32_t width,
                                                          uint32_t height,
                                                          uint32_t frame_ms) {
    return status_from_esp_err(board_port_set_pet_asset_consuming(
        frames, frame_count, width, height, frame_ms));
}
void platform_display_set_recording_mode(bool meeting) {
    board_port_set_recording_mode(meeting);
}
void platform_display_set_recording_visual(bool active, bool paused,
                                           uint32_t elapsed_seconds) {
    board_port_set_recording_visual(active, paused, elapsed_seconds);
}
void platform_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    board_port_set_audio_level(level, elapsed_seconds);
}
void platform_display_show_text(const char *title, const char *text) {
    board_port_show_text(title, text);
}
void platform_display_show_upload_progress(uint32_t completed_bytes,
                                           uint32_t total_bytes,
                                           const char *stage) {
    board_port_show_upload_progress(completed_bytes, total_bytes, stage);
}
void platform_display_show_response(const char *title, const char *text) {
    board_port_show_response(title, text);
}
void platform_display_show_response_image(const char *title, const char *caption,
                                          const uint16_t *pixels,
                                          uint32_t width, uint32_t height) {
    board_port_show_response_image(title, caption, pixels, width, height);
}
bool platform_display_navigate_response(int page_delta) {
    return board_port_navigate_response(page_delta);
}
bool platform_display_get_response_page(uint32_t *out_page) {
    if (!out_page) return false;
    unsigned page = 0;
    bool available = board_port_get_response_page(&page);
    if (available) *out_page = page;
    return available;
}
bool platform_display_restore_response_page(uint32_t page) {
    return board_port_restore_response_page(page);
}
int platform_display_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return board_port_cache_glyph(codepoint, bitmap);
}
void platform_display_show_qrcode_modules(const uint8_t *modules,
                                          uint32_t module_count,
                                          const char *ssid) {
    board_port_show_qrcode_matrix(modules, module_count, ssid);
}
void platform_display_show_ready_prompt(const char *title, const char *text) {
    board_port_show_ready_prompt(title, text);
}
void platform_display_cancel_ready_prompt(void) { board_port_cancel_ready_prompt(); }
void platform_display_set_wifi_status(const char *ssid, bool connected) {
    board_port_set_wifi_status(ssid, connected);
}
void platform_display_set_service_ready(bool ready) { board_port_set_service_ready(ready); }
void platform_display_set_ambient(const char *time, const char *location,
                                  const char *date, const char *weekday,
                                  const char *weather_summary,
                                  int temperature_c, bool weather_valid,
                                  bool weather_stale) {
    board_port_set_ambient(time, location, date, weekday, weather_summary,
                           temperature_c, weather_valid, weather_stale);
}
void platform_display_set_alarm_scheduled(bool scheduled) {
    board_port_set_alarm_scheduled(scheduled);
}
void platform_display_set_alarm_visual(bool active, uint32_t frame,
                                       const char *time_text, const char *label,
                                       uint32_t attempt, uint32_t max_attempts) {
    board_port_set_alarm_visual(active, frame, time_text, label, attempt,
                                max_attempts);
}
