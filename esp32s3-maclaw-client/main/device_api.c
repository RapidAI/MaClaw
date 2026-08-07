#include "device_api.h"
#include <limits.h>
#include "board_profile.h"
#include "connectivity_service.h"
#include "input_service.h"
#include "power_service.h"

/*
 * Compatibility facade during the board_port cutover.
 *
 * There is intentionally no state, task, resource ownership or policy here:
 * input service callers use Device API names, while the legacy board adapter
 * remains the sole owner of profile-specific touch/key knowledge until its
 * implementation is split into boards/<profile>/ input adapters.
 */
#include "board_port.h"

static device_status_t device_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_FAIL: return DEVICE_STATUS_IO_ERROR;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

int device_status_to_platform_error(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        case DEVICE_STATUS_INTERNAL_ERROR:
        default: return ESP_FAIL;
    }
}

bool device_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    device_profile_t candidate = {0};
    if (!board_profile_get(&candidate) || !device_profile_is_valid(&candidate)) {
        return false;
    }
    *out_profile = candidate;
    return true;
}

bool device_profile_is_valid(const device_profile_t *profile) {
    if (!profile || profile->struct_size != sizeof(*profile) ||
        profile->abi_version != DEVICE_PROFILE_ABI_VERSION ||
        !profile->id || !profile->id[0] ||
        !profile->primary_interaction_label ||
        !profile->primary_interaction_label[0] ||
        profile->display_width == 0 || profile->display_height == 0 ||
        (profile->capabilities & ~DEVICE_CAPABILITY_KNOWN_MASK) != 0 ||
        (profile->capabilities & DEVICE_CAPABILITY_REQUIRED_BASELINE) !=
            DEVICE_CAPABILITY_REQUIRED_BASELINE) {
        return false;
    }

    if (profile->primary_interaction_source == DEVICE_INPUT_SOURCE_TOUCH) {
        if ((profile->capabilities & DEVICE_CAPABILITY_TOUCH_INPUT) == 0) return false;
    } else if (profile->primary_interaction_source != DEVICE_INPUT_SOURCE_PRIMARY_CONTROL) {
        return false;
    }

    /* The public viewport is what shared scenes target.  A round renderer
     * cannot describe a rectangular logical safe area as an ordinary panel. */
    if ((profile->capabilities & DEVICE_CAPABILITY_ROUND_DISPLAY) != 0 &&
        profile->display_width != profile->display_height) {
        return false;
    }
    return true;
}

bool device_profile_has_capability(device_capability_flags_t capability) {
    device_profile_t profile;
    return capability != 0 && device_profile_get(&profile) &&
           (profile.capabilities & capability) == capability;
}

device_status_t device_audio_set_output_volume(uint8_t percent) {
    if (percent > 100) return DEVICE_STATUS_INVALID_ARGUMENT;
    return device_status_from_esp_err(board_port_set_output_volume(percent));
}

device_status_t device_audio_adjust_output_volume(int delta_percent,
                                                  uint8_t *out_percent) {
    unsigned applied = 0;
    device_status_t status = device_status_from_esp_err(
        board_port_adjust_output_volume(delta_percent, &applied));
    if (status == DEVICE_STATUS_OK && out_percent) *out_percent = (uint8_t)applied;
    return status;
}

device_status_t device_audio_play_wav(const uint8_t *wav, uint32_t wav_len) {
    return device_status_from_esp_err(board_port_play_wav(wav, wav_len));
}

device_status_t device_audio_play_alarm_burst(void) {
    return device_status_from_esp_err(board_port_play_alarm_burst());
}

device_status_t device_audio_capture_wav(uint8_t **out_wav, uint32_t *out_len) {
    if (!out_wav || !out_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    size_t length = 0;
    device_status_t status = device_status_from_esp_err(
        board_port_capture_wav(out_wav, &length));
    if (status == DEVICE_STATUS_OK) {
        if (length > UINT32_MAX) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        *out_len = (uint32_t)length;
    }
    return status;
}

device_status_t device_audio_stream_start(void) {
    return device_status_from_esp_err(board_port_audio_stream_start());
}

device_status_t device_audio_stream_read(int16_t *mono, uint32_t capacity,
                                         uint32_t *samples_read, uint16_t *level) {
    if (!mono || !samples_read || capacity == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    size_t count = 0;
    device_status_t status = device_status_from_esp_err(
        board_port_audio_stream_read(mono, capacity, &count, level));
    if (status == DEVICE_STATUS_OK) {
        if (count > UINT32_MAX) return DEVICE_STATUS_INTERNAL_ERROR;
        *samples_read = (uint32_t)count;
    }
    return status;
}

void device_audio_stream_stop(void) {
    board_port_audio_stream_stop();
}

device_status_t device_audio_playback_begin(void) {
    return device_status_from_esp_err(board_port_audio_playback_begin());
}

device_status_t device_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                            uint8_t channels) {
    if (!pcm || frames == 0 || (channels != 1 && channels != 2)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return device_status_from_esp_err(
        board_port_audio_playback_write(pcm, frames, channels));
}

device_status_t device_audio_playback_end(bool playback_succeeded) {
    return device_status_from_esp_err(
        board_port_audio_playback_end(playback_succeeded ? ESP_OK : ESP_FAIL));
}

void device_audio_request_playback_stop(void) {
    board_port_request_audio_playback_stop();
}

void device_audio_request_capture_stop(void) {
    board_port_request_capture_stop();
}

void device_audio_reset_capture_stop(void) {
    board_port_reset_capture_stop();
}

device_status_t device_wake_word_start(device_wake_word_cb_t on_wake, void *context) {
    if (!on_wake) return DEVICE_STATUS_INVALID_ARGUMENT;
    return device_status_from_esp_err(board_port_start_wake_word(on_wake, context));
}

device_status_t device_wake_word_stop(void) {
    return device_status_from_esp_err(board_port_stop_wake_word());
}

void device_wake_word_pause(bool paused) {
    board_port_pause_wake_word(paused);
}

device_status_t device_power_init(void) {
    return power_service_init();
}

device_status_t device_power_schedule_display_off(uint32_t idle_after_ms) {
    return power_service_schedule_display_off(idle_after_ms);
}

void device_power_cancel_display_off(void) {
    power_service_cancel_display_off();
}

bool device_power_wake_display_from_user(void) {
    return power_service_wake_display_from_user();
}

bool device_power_get_snapshot(device_power_snapshot_t *out_snapshot) {
    return power_service_get_snapshot(out_snapshot);
}

bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    if (!out_telemetry) return false;
    unsigned level = 0;
    bool charging = false;
    bool available = board_port_get_power_status(&level, &charging);
    out_telemetry->available = available;
    out_telemetry->level_percent = level > 100 ? 100 : (uint8_t)level;
    out_telemetry->charging = charging;
    return available;
}

void device_connectivity_set_active_cellular(bool active) {
    connectivity_service_set_active_uplink(active ? DEVICE_UPLINK_CELLULAR
                                                 : DEVICE_UPLINK_WIFI);
    board_port_set_network_transport(active);
}

bool device_connectivity_is_active_cellular(void) {
    return connectivity_service_is_active_cellular();
}

void device_connectivity_set_wifi_ready(bool ready) {
    connectivity_service_set_wifi_ready(ready);
}

void device_connectivity_set_cellular_ready(bool ready) {
    connectivity_service_set_cellular_ready(ready);
}

bool device_connectivity_is_active_uplink_ready(void) {
    return connectivity_service_is_active_uplink_ready();
}

bool device_connectivity_get_snapshot(device_connectivity_snapshot_t *out_snapshot) {
    return connectivity_service_get_snapshot(out_snapshot);
}

device_status_t device_connectivity_prepare_cellular_transport(void) {
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return device_status_from_esp_err(board_port_prepare_cellular_transport());
}

bool device_connectivity_take_startup_transport_toggle(uint32_t window_ms) {
    return board_port_wait_for_boot_network_toggle(window_ms);
}

void device_display_set_command_lock(bool locked) {
    board_port_set_command_display_lock(locked);
}

void device_display_show_startup(void) {
    board_port_show_startup_screen();
}

void device_display_set_pet_state(const char *state) {
    board_port_set_pet_state(state);
}

void device_display_set_command_stage(const char *stage) {
    board_port_set_command_stage(stage);
}

void device_display_set_command_cancel_enabled(bool enabled) {
    board_port_set_command_cancel_enabled(enabled);
}

void device_display_set_pet_profile(const char *skin, bool motion_enabled) {
    board_port_set_pet_profile(skin, motion_enabled);
}

device_status_t device_display_set_pet_asset(const uint8_t *const *frames,
                                             uint32_t frame_count,
                                             uint32_t width, uint32_t height,
                                             uint32_t frame_ms) {
    return device_status_from_esp_err(board_port_set_pet_asset(
        frames, frame_count, width, height, frame_ms));
}

void device_display_set_recording_mode(bool meeting) {
    board_port_set_recording_mode(meeting);
}

void device_display_set_recording_visual(bool active, bool paused,
                                         uint32_t elapsed_seconds) {
    board_port_set_recording_visual(active, paused, elapsed_seconds);
}

void device_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    board_port_set_audio_level(level, elapsed_seconds);
}

void device_display_show_text(const char *title, const char *text) {
    board_port_show_text(title, text);
}

void device_display_show_upload_progress(uint32_t completed_bytes,
                                         uint32_t total_bytes,
                                         const char *stage) {
    board_port_show_upload_progress(completed_bytes, total_bytes, stage);
}

void device_display_show_response(const char *title, const char *text) {
    board_port_show_response(title, text);
}

void device_display_show_response_image(const char *title, const char *caption,
                                        const uint16_t *pixels,
                                        uint32_t width, uint32_t height) {
    board_port_show_response_image(title, caption, pixels, width, height);
}

bool device_display_navigate_response(int page_delta) {
    return board_port_navigate_response(page_delta);
}

bool device_display_get_response_page(uint32_t *out_page) {
    if (!out_page) return false;
    unsigned page = 0;
    bool available = board_port_get_response_page(&page);
    if (available) *out_page = page;
    return available;
}

bool device_display_restore_response_page(uint32_t page) {
    if (page > UINT_MAX) return false;
    return board_port_restore_response_page((unsigned)page);
}

int device_display_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return board_port_cache_glyph(codepoint, bitmap);
}

void device_display_show_qrcode_modules(const uint8_t *modules,
                                        uint32_t module_count,
                                        const char *ssid) {
    board_port_show_qrcode_matrix(modules, module_count, ssid);
}

void device_display_show_ready_prompt(const char *title, const char *text) {
    board_port_show_ready_prompt(title, text);
}

void device_display_cancel_ready_prompt(void) {
    board_port_cancel_ready_prompt();
}

void device_display_set_wifi_status(const char *ssid, bool connected) {
    board_port_set_wifi_status(ssid, connected);
}

void device_display_set_service_ready(bool ready) {
    board_port_set_service_ready(ready);
}

void device_display_set_ambient(const char *time, const char *location,
                                const char *date, const char *weekday,
                                const char *weather_summary,
                                int temperature_c, bool weather_valid,
                                bool weather_stale) {
    board_port_set_ambient(time, location, date, weekday, weather_summary,
                           temperature_c, weather_valid, weather_stale);
}

void device_display_set_alarm_scheduled(bool scheduled) {
    board_port_set_alarm_scheduled(scheduled);
}

void device_display_set_alarm_visual(bool active, uint32_t frame,
                                     const char *time_text, const char *label,
                                     uint32_t attempt, uint32_t max_attempts) {
    board_port_set_alarm_visual(active, frame, time_text, label, attempt,
                                max_attempts);
}

device_status_t device_input_start(device_input_cb_t on_input, void *context) {
    return input_service_start(on_input, context);
}

device_status_t device_input_stop(uint32_t timeout_ms) {
    return input_service_stop(timeout_ms);
}

bool device_input_is_primary_interaction_source(device_input_source_t source) {
    device_profile_t profile;
    return device_profile_get(&profile) &&
           profile.primary_interaction_source != DEVICE_INPUT_SOURCE_UNKNOWN &&
           source == profile.primary_interaction_source;
}

const char *device_input_primary_interaction_label(void) {
    device_profile_t profile;
    if (!device_profile_get(&profile) || !profile.primary_interaction_label ||
        !profile.primary_interaction_label[0]) {
        return "本机控件";
    }
    return profile.primary_interaction_label;
}
