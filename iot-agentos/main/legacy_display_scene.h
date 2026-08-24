#pragma once

/*
 * Private legacy-renderer Display scene seam.
 *
 * Platform Display is the only shared caller of the selected profile bridge.
 * During renderer decomposition the bridge needs these scene operations, but
 * it must not include the broad board_port compatibility facade (which also
 * contains audio, input, connectivity and bootstrap operations).  Concrete
 * renderers retain framebuffer, task, panel and electrical ownership.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

bool legacy_display_scene_get_pet_asset_install_budget(
    size_t source_width, size_t source_height, size_t frame_count,
    size_t *out_total_external_bytes, size_t *out_max_external_allocation_bytes,
    size_t *out_max_frame_count);
void legacy_display_scene_set_command_lock(bool locked);
esp_err_t legacy_display_scene_set_brightness(unsigned percent);
esp_err_t legacy_display_scene_enter_display_off(void);
esp_err_t legacy_display_scene_wake_display(void);
bool legacy_display_scene_is_off(void);
void legacy_display_scene_show_startup(void);
void legacy_display_scene_set_pet_state(const char *state);
void legacy_display_scene_set_command_stage(const char *stage);
void legacy_display_scene_set_pet_profile(const char *skin, bool motion_enabled);
esp_err_t legacy_display_scene_set_pet_asset(const uint8_t *const *frames,
                                              size_t frame_count,
                                              size_t width, size_t height,
                                              uint32_t frame_ms);
esp_err_t legacy_display_scene_set_pet_asset_consuming(uint8_t **frames,
                                                        size_t frame_count,
                                                        size_t width,
                                                        size_t height,
                                                        uint32_t frame_ms);
void legacy_display_scene_set_recording_mode(bool meeting);
void legacy_display_scene_set_recording_visual(bool active, bool paused,
                                               uint32_t elapsed_seconds);
void legacy_display_scene_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void legacy_display_scene_show_text(const char *title, const char *text);
void legacy_display_scene_show_upload_progress(size_t completed_bytes,
                                              size_t total_bytes,
                                              const char *stage);
void legacy_display_scene_show_response(const char *title, const char *text);
void legacy_display_scene_show_response_image(const char *title, const char *caption,
                                              const uint16_t *pixels,
                                              size_t width, size_t height);
bool legacy_display_scene_navigate_response(int page_delta);
bool legacy_display_scene_get_response_page(unsigned *page);
bool legacy_display_scene_restore_response_page(unsigned page);
int legacy_display_scene_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void legacy_display_scene_show_qrcode_modules(const uint8_t *modules,
                                              size_t module_count,
                                              const char *ssid);
void legacy_display_scene_show_ready_prompt(const char *title, const char *text);
void legacy_display_scene_cancel_ready_prompt(void);
void legacy_display_scene_set_wifi_status(const char *ssid, bool connected);
void legacy_display_scene_set_service_ready(bool ready);
void legacy_display_scene_set_ambient(const char *time, const char *location,
                                      const char *date, const char *weekday,
                                      const char *weather_summary,
                                      int temperature_c, bool weather_valid,
                                      bool weather_stale);
void legacy_display_scene_set_alarm_scheduled(bool scheduled);
void legacy_display_scene_set_alarm_visual(bool active, unsigned frame,
                                           const char *time_text,
                                           const char *label,
                                           unsigned attempt,
                                           unsigned max_attempts);

/* Renderer source owners implement this narrow scene contract directly.
 * Platform Display never needs a broad board_port compatibility name to
 * submit semantic scene changes to a profile-private renderer. */
