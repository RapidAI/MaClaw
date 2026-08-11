#include "device_api.h"
#include <limits.h>
#include "esp_timer.h"
#include "board_profile.h"
#include "audio_service.h"
#include "battery_policy_service.h"
#include "connectivity_service.h"
#include "display_service.h"
#include "input_service.h"
#include "lifecycle_service.h"
#include "power_service.h"
#include "power_lease_service.h"
#include "platform_connectivity.h"
#include "platform_power.h"
#include "platform_sensor.h"
#include "platform_storage.h"
#include "resource_pressure_service.h"

/* A public shutdown budget is owned by the composition root.  Child services
 * must consume the same deadline rather than each starting an independent
 * timeout window: Power first stops the DISPLAY_OFF callback, then the lease
 * domain drains owners that may still release while admission is closed. */
static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

int device_status_to_platform_error(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        case DEVICE_STATUS_NOT_FOUND: return ESP_ERR_NOT_FOUND;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        case DEVICE_STATUS_INTERNAL_ERROR:
        default: return ESP_FAIL;
    }
}

bool device_runtime_get_snapshot(device_runtime_snapshot_t *out_snapshot) {
    return lifecycle_service_get_snapshot(out_snapshot);
}

bool device_resource_pressure_get_snapshot(
    device_resource_pressure_snapshot_t *out_snapshot) {
    return resource_pressure_service_get_snapshot(out_snapshot);
}

bool device_resource_pressure_allows_optional_work(void) {
    return resource_pressure_service_allows_optional_work();
}

bool device_resource_pressure_allows_optional_allocation(
    uint32_t internal_bytes, uint32_t external_bytes, uint32_t storage_bytes) {
    return resource_pressure_service_allows_optional_allocation(
        internal_bytes, external_bytes, storage_bytes);
}

bool device_display_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget) {
    return display_service_get_pet_asset_install_budget(
        source_width, source_height, frame_count, out_budget);
}

bool device_storage_allows_optional_flash_work(void) {
    return platform_storage_allows_optional_flash_work();
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

device_status_t device_motion_get_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    device_motion_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
    };
    device_status_t status = platform_sensor_get_motion_sample(&sample);
    if (status == DEVICE_STATUS_OK) *out_sample = sample;
    return status;
}

device_status_t device_audio_set_output_volume(uint8_t percent) {
    return audio_service_set_output_volume(percent);
}

device_status_t device_audio_adjust_output_volume(int delta_percent,
                                                  uint8_t *out_percent) {
    return audio_service_adjust_output_volume(delta_percent, out_percent);
}

device_status_t device_audio_play_wav(const uint8_t *wav, uint32_t wav_len) {
    return audio_service_play_wav(wav, wav_len);
}

device_status_t device_audio_play_alarm_burst(void) {
    return audio_service_play_alarm_burst();
}

device_status_t device_audio_capture_wav(uint8_t **out_wav, uint32_t *out_len) {
    if (!out_wav || !out_len) return DEVICE_STATUS_INVALID_ARGUMENT;
    return audio_service_capture_wav(out_wav, out_len);
}

void device_audio_release_captured_wav(uint8_t *wav) {
    audio_service_release_captured_wav(wav);
}

device_status_t device_audio_stream_start(void) {
    return audio_service_stream_start();
}

device_status_t device_audio_stream_read(int16_t *mono, uint32_t capacity,
                                         uint32_t *samples_read, uint16_t *level) {
    return audio_service_stream_read(mono, capacity, samples_read, level);
}

void device_audio_stream_stop(void) {
    audio_service_stream_stop();
}

device_status_t device_audio_playback_begin(void) {
    return audio_service_playback_begin();
}

device_status_t device_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                            uint8_t channels) {
    return audio_service_playback_write(pcm, frames, channels);
}

device_status_t device_audio_playback_end(bool playback_succeeded) {
    return audio_service_playback_end(playback_succeeded);
}

void device_audio_request_playback_stop(void) {
    audio_service_request_playback_stop();
}

void device_audio_request_capture_stop(void) {
    audio_service_request_capture_stop();
}

void device_audio_reset_capture_stop(void) {
    audio_service_reset_capture_stop();
}

device_status_t device_wake_word_start(device_wake_word_cb_t on_wake, void *context) {
    return audio_service_wake_word_start(on_wake, context);
}

device_status_t device_wake_word_stop(void) {
    return audio_service_wake_word_stop();
}

device_status_t device_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    return audio_service_wake_word_stop_with_timeout(timeout_ms);
}

void device_wake_word_pause(bool paused) {
    audio_service_wake_word_pause(paused);
}

device_status_t device_power_init(void) {
    device_status_t lease_status = power_lease_service_init();
    if (lease_status != DEVICE_STATUS_OK) return lease_status;
    device_status_t power_status = power_service_init();
    if (power_status == DEVICE_STATUS_OK) return DEVICE_STATUS_OK;

    /* Power Lease has no worker and no valid lease owner can exist before the
     * Power scheduler has published readiness. Do not leave its admission
     * open if timer creation/initialization rejects this generation; otherwise
     * a later startup retry could inherit a lease domain without a scheduler. */
    device_status_t rollback_status = power_lease_service_deinit(100);
    if (rollback_status != DEVICE_STATUS_OK) return rollback_status;
    return power_status;
}

device_status_t device_power_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    /* Close new foreground claims before stopping the display-off timer, but
     * retain old handles so domains already draining can release them. */
    power_lease_service_close_admission();
    device_status_t power_status = power_service_deinit(timeout_ms);
    const uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    /* Do not call a child with a synthetic minimum timeout after the public
     * deadline expires. Admission remains closed and a later lifecycle pass
     * can finish the lease drain without violating this caller's budget. */
    device_status_t lease_status = remaining_ms
                                       ? power_lease_service_deinit(remaining_ms)
                                       : DEVICE_STATUS_TIMEOUT;
    return power_status != DEVICE_STATUS_OK ? power_status : lease_status;
}

device_status_t device_power_lease_acquire(device_power_lease_owner_t owner,
                                           device_power_lease_t *out_lease) {
    return power_lease_service_acquire(owner, out_lease);
}

void device_power_lease_release(device_power_lease_t lease) {
    power_lease_service_release(lease);
}

bool device_power_lease_get_snapshot(device_power_lease_snapshot_t *out_snapshot) {
    return power_lease_service_get_snapshot(out_snapshot);
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

bool device_power_wake_display_from_schedule(void) {
    return power_service_wake_display_from_schedule();
}

bool device_power_wake_display_from_remote_control(void) {
    return power_service_wake_display_from_remote_control();
}

bool device_power_get_snapshot(device_power_snapshot_t *out_snapshot) {
    return power_service_get_snapshot(out_snapshot);
}

bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry) {
    return platform_power_get_telemetry(out_telemetry);
}

bool device_battery_policy_get_snapshot(device_battery_policy_snapshot_t *out_snapshot) {
    return battery_policy_service_get_snapshot(out_snapshot);
}

bool device_battery_policy_allows_optional_work(void) {
    return battery_policy_service_allows_optional_work();
}

bool device_battery_policy_allows_high_power_work(void) {
    return battery_policy_service_allows_high_power_work();
}

void device_connectivity_set_active_cellular(bool active) {
    connectivity_service_set_active_uplink(active ? DEVICE_UPLINK_CELLULAR
                                                 : DEVICE_UPLINK_WIFI);
}

bool device_connectivity_is_active_cellular(void) {
    return connectivity_service_is_active_cellular();
}

bool device_connectivity_initialize(void) {
    return connectivity_service_initialize();
}

device_status_t device_connectivity_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    switch (connectivity_service_deinit(timeout_ms)) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

uint32_t device_connectivity_begin_wifi_attempt(const char *network_id) {
    return connectivity_service_begin_wifi_attempt(network_id);
}

bool device_connectivity_wait_wifi_attempt(uint32_t attempt_epoch,
                                           uint32_t timeout_ms) {
    return connectivity_service_wait_wifi_attempt(attempt_epoch, timeout_ms);
}

bool device_connectivity_observe_wifi_disconnected(const char *network_id) {
    return connectivity_service_observe_wifi_disconnected(network_id);
}

bool device_connectivity_observe_wifi_got_ip(const char *connected_network_id) {
    return connectivity_service_observe_wifi_got_ip(connected_network_id);
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

void device_connectivity_begin_provisioning(bool pairing_recovery) {
    connectivity_service_begin_provisioning(pairing_recovery);
}

void device_connectivity_end_provisioning(void) {
    connectivity_service_end_provisioning();
}

bool device_connectivity_is_provisioning_active(void) {
    return connectivity_service_is_provisioning_active();
}

bool device_connectivity_is_pairing_recovery_provisioning(void) {
    return connectivity_service_is_pairing_recovery_provisioning();
}

device_status_t device_connectivity_prepare_cellular_transport(void) {
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return platform_connectivity_prepare_cellular_transport();
}

device_status_t device_connectivity_start_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return platform_connectivity_start_cellular_transport(timeout_ms);
}

bool device_connectivity_is_cellular_transport_ready(void) {
    return device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT) &&
           platform_connectivity_is_cellular_transport_ready();
}

device_status_t device_connectivity_quiesce_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return platform_connectivity_quiesce_cellular_transport(timeout_ms);
}

static bool cellular_http_request_is_valid(
    const device_connectivity_http_request_t *request) {
    return request && request->method && request->method[0] && request->url &&
           request->url[0] && request->response && request->response_capacity >= 2 &&
           request->response_len && request->status_code && request->truncated &&
           request->timeout_ms > 0;
}

device_status_t device_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!cellular_http_request_is_valid(request)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return platform_connectivity_cellular_http_request(request);
}

device_status_t device_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || !cellular_http_request_is_valid(&request->request) ||
        !request->body_reader || !request->stream_buffer || request->stream_buffer_size == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return platform_connectivity_cellular_http_stream_request(request);
}

bool device_connectivity_cancel_cellular_foreground_request(void) {
    if (!device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return false;
    }
    return platform_connectivity_cancel_cellular_foreground_request();
}

bool device_connectivity_cancel_cellular_requests_for_owner(const void *owner) {
    if (!owner || !device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)) {
        return false;
    }
    return platform_connectivity_cancel_cellular_requests_for_owner(owner);
}

bool device_connectivity_take_startup_transport_toggle(uint32_t window_ms) {
    return connectivity_service_take_startup_transport_toggle(window_ms);
}

void device_connectivity_restore_selected_uplink(void) {
    connectivity_service_restore_selected_uplink();
}

bool device_connectivity_apply_startup_transport_toggle(uint32_t window_ms) {
    return connectivity_service_apply_startup_transport_toggle(window_ms);
}

void device_connectivity_adapt_gateway_url(char *gateway_url,
                                           uint32_t gateway_url_capacity) {
    if (!gateway_url || gateway_url_capacity == 0) return;
    platform_connectivity_adapt_gateway_url(gateway_url, gateway_url_capacity,
                                            device_connectivity_is_active_cellular());
}

void device_display_set_command_lock(bool locked) {
    display_service_set_command_lock(locked);
}

device_status_t device_display_set_brightness(uint8_t percent) {
    return display_service_set_brightness(percent);
}

void device_display_show_startup(void) {
    display_service_show_startup();
}

void device_display_set_pet_state(const char *state) {
    display_service_set_pet_state(state);
}

void device_display_set_command_stage(const char *stage) {
    display_service_set_command_stage(stage);
}

void device_display_set_command_cancel_enabled(bool enabled) {
    display_service_set_command_cancel_enabled(enabled);
}

void device_display_set_pet_profile(const char *skin, bool motion_enabled) {
    display_service_set_pet_profile(skin, motion_enabled);
}

device_status_t device_display_set_pet_asset(const uint8_t *const *frames,
                                             uint32_t frame_count,
                                             uint32_t width, uint32_t height,
                                             uint32_t frame_ms) {
    return display_service_set_pet_asset(frames, frame_count, width, height, frame_ms);
}

device_status_t device_display_set_pet_asset_consuming(uint8_t **frames,
                                                       uint32_t frame_count,
                                                       uint32_t width, uint32_t height,
                                                       uint32_t frame_ms) {
    return display_service_set_pet_asset_consuming(
        frames, frame_count, width, height, frame_ms);
}

void device_display_set_recording_mode(bool meeting) {
    display_service_set_recording_mode(meeting);
}

void device_display_set_recording_visual(bool active, bool paused,
                                         uint32_t elapsed_seconds) {
    display_service_set_recording_visual(active, paused, elapsed_seconds);
}

void device_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds) {
    display_service_set_audio_level(level, elapsed_seconds);
}

void device_display_show_text(const char *title, const char *text) {
    display_service_show_text(title, text);
}

void device_display_show_upload_progress(uint32_t completed_bytes,
                                         uint32_t total_bytes,
                                         const char *stage) {
    display_service_show_upload_progress(completed_bytes, total_bytes, stage);
}

void device_display_show_response(const char *title, const char *text) {
    display_service_show_response(title, text);
}

void device_display_show_response_image(const char *title, const char *caption,
                                        const uint16_t *pixels,
                                        uint32_t width, uint32_t height) {
    display_service_show_response_image(title, caption, pixels, width, height);
}

bool device_display_navigate_response(int page_delta) {
    return display_service_navigate_response(page_delta);
}

bool device_display_get_response_page(uint32_t *out_page) {
    return display_service_get_response_page(out_page);
}

bool device_display_restore_response_page(uint32_t page) {
    return display_service_restore_response_page(page);
}

int device_display_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    return display_service_cache_glyph(codepoint, bitmap);
}

void device_display_show_qrcode_modules(const uint8_t *modules,
                                        uint32_t module_count,
                                        const char *ssid) {
    display_service_show_qrcode_modules(modules, module_count, ssid);
}

void device_display_show_ready_prompt(const char *title, const char *text) {
    display_service_show_ready_prompt(title, text);
}

void device_display_cancel_ready_prompt(void) {
    display_service_cancel_ready_prompt();
}

void device_display_set_wifi_status(const char *ssid, bool connected) {
    display_service_set_wifi_status(ssid, connected);
}

void device_display_set_service_ready(bool ready) {
    display_service_set_service_ready(ready);
}

void device_display_set_ambient(const char *time, const char *location,
                                const char *date, const char *weekday,
                                const char *weather_summary,
                                int temperature_c, bool weather_valid,
                                bool weather_stale) {
    display_service_set_ambient(time, location, date, weekday, weather_summary,
                                 temperature_c, weather_valid, weather_stale);
}

void device_display_set_alarm_scheduled(bool scheduled) {
    display_service_set_alarm_scheduled(scheduled);
}

void device_display_set_alarm_visual(bool active, uint32_t frame,
                                     const char *time_text, const char *label,
                                     uint32_t attempt, uint32_t max_attempts) {
    display_service_set_alarm_visual(active, frame, time_text, label, attempt,
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
