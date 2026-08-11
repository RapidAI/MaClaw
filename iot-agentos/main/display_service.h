#pragma once

/*
 * Internal shared display-service facade.
 *
 * The public Device API and shared business/UI code address semantic display
 * intents through this module.  The selected Platform Display implementation
 * remains the only route to a board renderer and its physical panel.
 *
 * This first Phase 3 seam is deliberately synchronous: the existing board
 * renderers still own their framebuffer, DMA fence and scene-transition
 * ordering.  A later single Display Task may move the forwarding below this
 * interface without making Device API callers aware of its queue or panel
 * handles.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Internal submission observation.  The revision identifies ordering of
 * semantic intents accepted by this service; completed_revision means the
 * Display Task returned from its synchronous Platform call, not that a panel
 * DMA fence or physical scan-out completed.  No panel/framebuffer state is
 * exposed. */
typedef struct {
    uint32_t submitted_revision;
    uint32_t completed_revision;
    uint32_t task_generation;
    bool task_ready;
    bool task_registered;
} display_service_snapshot_t;

/* Initializes the boot-lifetime Display Task before App UI or startup
 * restoration publishes its first scene. The task is the sole caller of
 * Platform Display/renderer operations; board display deinit/reinit is not
 * implemented yet. */
bool display_service_init(void);
/* Lifecycle-only stop for failed startup rollback. It closes request admission
 * and joins the Display Task without deinitializing the boot-lifetime board
 * renderer/panel. A successful stop is terminal for this boot generation. */
device_status_t display_service_deinit(uint32_t timeout_ms);
bool display_service_get_snapshot(display_service_snapshot_t *out_snapshot);

bool display_service_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget);

void display_service_set_command_lock(bool locked);
device_status_t display_service_set_brightness(uint8_t percent);
/* Power Service owns policy/timing, but its panel-visible transitions must
 * join the same Display Task as every scene and brightness update. */
bool display_service_enter_display_off(void);
bool display_service_wake_display(void);
bool display_service_display_is_off(void);
void display_service_show_startup(void);
void display_service_set_pet_state(const char *state);
void display_service_set_command_stage(const char *stage);
void display_service_set_command_cancel_enabled(bool enabled);
void display_service_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t display_service_set_pet_asset(const uint8_t *const *frames,
                                              uint32_t frame_count,
                                              uint32_t width, uint32_t height,
                                              uint32_t frame_ms);
device_status_t display_service_set_pet_asset_consuming(uint8_t **frames,
                                                        uint32_t frame_count,
                                                        uint32_t width,
                                                        uint32_t height,
                                                        uint32_t frame_ms);
void display_service_set_recording_mode(bool meeting);
void display_service_set_recording_visual(bool active, bool paused,
                                          uint32_t elapsed_seconds);
void display_service_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void display_service_show_text(const char *title, const char *text);
void display_service_show_upload_progress(uint32_t completed_bytes,
                                          uint32_t total_bytes,
                                          const char *stage);
void display_service_show_response(const char *title, const char *text);
void display_service_show_response_image(const char *title, const char *caption,
                                         const uint16_t *pixels,
                                         uint32_t width, uint32_t height);
bool display_service_navigate_response(int page_delta);
bool display_service_get_response_page(uint32_t *out_page);
bool display_service_restore_response_page(uint32_t page);
/* Borrowed 72-byte 24x24 bitmap. This synchronous facade copies it before
 * forwarding, so the caller may reuse or release it when this call returns.
 * It is not an asynchronous queue admission contract. */
int display_service_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
void display_service_show_qrcode_modules(const uint8_t *modules,
                                         uint32_t module_count,
                                         const char *ssid);
void display_service_show_ready_prompt(const char *title, const char *text);
void display_service_cancel_ready_prompt(void);
void display_service_set_wifi_status(const char *ssid, bool connected);
void display_service_set_service_ready(bool ready);
void display_service_set_ambient(const char *time, const char *location,
                                 const char *date, const char *weekday,
                                 const char *weather_summary,
                                 int temperature_c, bool weather_valid,
                                 bool weather_stale);
void display_service_set_alarm_scheduled(bool scheduled);
void display_service_set_alarm_visual(bool active, uint32_t frame,
                                      const char *time_text, const char *label,
                                      uint32_t attempt, uint32_t max_attempts);
