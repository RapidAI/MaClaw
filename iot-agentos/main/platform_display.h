#pragma once

/*
 * Internal physical-display SPI.
 *
 * Device API owns hardware-neutral scene intent and public value contracts.
 * This port translates those intents to the selected adapter's renderer and
 * panel transaction.  It deliberately exposes neither LCD controller/I/O
 * handles, framebuffer ownership, GPIOs, touch geometry nor renderer tasks.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

bool platform_display_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget);

void platform_display_set_command_lock(bool locked);
device_status_t platform_display_set_brightness(uint8_t percent);
/* Physical visibility transitions are submitted by Display Service. Power
 * Service owns eligibility, leases and deadlines; this port performs only the
 * selected board renderer's already-authorized panel/backlight transaction. */
bool platform_display_enter_display_off(void);
bool platform_display_wake_display(void);
bool platform_display_is_off(void);
void platform_display_show_startup(void);
void platform_display_set_pet_state(const char *state);
void platform_display_set_command_stage(const char *stage);
void platform_display_set_command_cancel_enabled(bool enabled);
void platform_display_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t platform_display_set_pet_asset(const uint8_t *const *frames,
                                                uint32_t frame_count,
                                                uint32_t width, uint32_t height,
                                                uint32_t frame_ms);
device_status_t platform_display_set_pet_asset_consuming(uint8_t **frames,
                                                          uint32_t frame_count,
                                                          uint32_t width,
                                                          uint32_t height,
                                                          uint32_t frame_ms);
void platform_display_set_recording_mode(bool meeting);
void platform_display_set_recording_visual(bool active, bool paused,
                                           uint32_t elapsed_seconds);
void platform_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void platform_display_show_text(const char *title, const char *text);
void platform_display_show_upload_progress(uint32_t completed_bytes,
                                           uint32_t total_bytes,
                                           const char *stage);
void platform_display_show_response(const char *title, const char *text);
void platform_display_show_response_image(const char *title, const char *caption,
                                          const uint16_t *pixels,
                                          uint32_t width, uint32_t height);
bool platform_display_navigate_response(int page_delta);
bool platform_display_get_response_page(uint32_t *out_page);
bool platform_display_restore_response_page(uint32_t page);
/* Internal synchronous SPI: implementation must consume/copy the borrowed
 * 72-byte bitmap before return. It must never retain this pointer. */
int platform_display_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void platform_display_show_qrcode_modules(const uint8_t *modules,
                                          uint32_t module_count,
                                          const char *ssid);
void platform_display_show_ready_prompt(const char *title, const char *text);
void platform_display_cancel_ready_prompt(void);
void platform_display_set_wifi_status(const char *ssid, bool connected);
void platform_display_set_service_ready(bool ready);
void platform_display_set_ambient(const char *time, const char *location,
                                  const char *date, const char *weekday,
                                  const char *weather_summary,
                                  int temperature_c, bool weather_valid,
                                  bool weather_stale);
void platform_display_set_alarm_scheduled(bool scheduled);
void platform_display_set_alarm_visual(bool active, uint32_t frame,
                                       const char *time_text, const char *label,
                                       uint32_t attempt, uint32_t max_attempts);
