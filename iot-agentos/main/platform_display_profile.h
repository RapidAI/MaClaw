#pragma once

/*
 * Selected physical Display profile seam.
 *
 * Platform Display owns the stable, hardware-neutral scene-value contract.
 * The compact/round family bridge below this header owns the legacy renderer
 * facade and selected controller adapter.  Neither board ID nor panel/I/O
 * handles can escape into Display Service or business callers.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

bool platform_display_profile_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget);
void platform_display_profile_set_command_lock(bool locked);
device_status_t platform_display_profile_set_brightness(uint8_t percent);
device_status_t platform_display_profile_enter_display_off(void);
device_status_t platform_display_profile_wake_display(void);
device_status_t platform_display_profile_prepare_system_sleep(uint32_t timeout_ms);
void platform_display_profile_abort_system_sleep_prepare(void);
bool platform_display_profile_is_off(void);
void platform_display_profile_show_startup(void);
void platform_display_profile_set_pet_state(const char *state);
void platform_display_profile_set_command_stage(const char *stage);
void platform_display_profile_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t platform_display_profile_set_pet_asset(const uint8_t *const *frames,
                                                        uint32_t frame_count,
                                                        uint32_t width,
                                                        uint32_t height,
                                                        uint32_t frame_ms);
device_status_t platform_display_profile_set_pet_asset_consuming(uint8_t **frames,
                                                                  uint32_t frame_count,
                                                                  uint32_t width,
                                                                  uint32_t height,
                                                                  uint32_t frame_ms);
void platform_display_profile_set_recording_mode(bool meeting);
void platform_display_profile_set_recording_visual(bool active, bool paused,
                                                   uint32_t elapsed_seconds);
void platform_display_profile_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void platform_display_profile_show_text(const char *title, const char *text);
void platform_display_profile_show_upload_progress(uint32_t completed_bytes,
                                                   uint32_t total_bytes,
                                                   const char *stage);
void platform_display_profile_show_response(const char *title, const char *text);
void platform_display_profile_show_response_image(const char *title, const char *caption,
                                                  const uint16_t *pixels,
                                                  uint32_t width, uint32_t height);
bool platform_display_profile_navigate_response(int page_delta);
bool platform_display_profile_get_response_page(uint32_t *out_page);
bool platform_display_profile_restore_response_page(uint32_t page);
int platform_display_profile_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void platform_display_profile_show_qrcode_modules(const uint8_t *modules,
                                                  uint32_t module_count,
                                                  const char *ssid);
void platform_display_profile_show_ready_prompt(const char *title, const char *text);
void platform_display_profile_cancel_ready_prompt(void);
void platform_display_profile_set_wifi_status(const char *ssid, bool connected);
void platform_display_profile_set_service_ready(bool ready);
void platform_display_profile_set_ambient(const char *time, const char *location,
                                          const char *date, const char *weekday,
                                          const char *weather_summary,
                                          int temperature_c, bool weather_valid,
                                          bool weather_stale);
void platform_display_profile_set_alarm_scheduled(bool scheduled);
void platform_display_profile_set_alarm_visual(bool active, uint32_t frame,
                                               const char *time_text, const char *label,
                                               uint32_t attempt, uint32_t max_attempts);
