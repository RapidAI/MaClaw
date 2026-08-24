#pragma once

/*
 * Internal shared display-service facade.
 *
 * The public Device API and shared business/UI code address semantic display
 * intents through this module.  The selected Platform Display implementation
 * remains the only route to a board renderer and its physical panel.
 *
 * Scene transitions retain synchronous result semantics: existing board
 * renderers still own framebuffer, DMA fence and scene ordering.  High-rate
 * microphone meter updates are latest-value coalesced inside the service, so
 * capture never queues one panel request per PCM block.  Neither path exposes
 * queue, framebuffer or panel handles through Device API.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "fault_domain.h"

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

/* Last semantic brightness result acknowledged by the single Display Task.
 * It does not expose panel controller state or a framebuffer; `known` means
 * only that Platform Display returned success for the latest ordered request.
 * A failure retains prior evidence but records its status so a Configuration
 * reconciliation owner never treats durable intent as completed scan-out. */
#define DISPLAY_SERVICE_BRIGHTNESS_STATE_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool known;
    uint8_t percent;
    device_status_t last_status;
} display_service_brightness_state_t;

/* Initializes the boot-lifetime Display Task before App UI or startup
 * restoration publishes its first scene. The task is the sole caller of
 * Platform Display/renderer operations; board display deinit/reinit is not
 * implemented yet. */
/* Creation failure is normalized at the Display Service boundary so the
 * composition root and any future hardware profile never need to interpret
 * an ESP-IDF/FreeRTOS allocation result. */
device_status_t display_service_init(void);
/* Lifecycle-only stop for failed startup rollback. It closes request admission
 * and joins the Display Task without deinitializing the boot-lifetime board
 * renderer/panel. A successful stop is terminal for this boot generation. */
device_status_t display_service_deinit(uint32_t timeout_ms);
/*
 * Internal System Sleep participant. PREPARE closes new semantic display
 * submissions, waits until the sole Display Task has consumed every admitted
 * scene request, then asks Platform Display for the selected profile's
 * bounded scan-out/DMA-idle fence. It does not turn the panel off, stop the
 * task, or alter any rail; a later electrical commit and resume contract
 * still require profile HIL. Abort is idempotent and reopens normal Display
 * Service admission on rollback.
 */
device_status_t display_service_prepare_system_sleep(uint32_t timeout_ms);
void display_service_abort_system_sleep_prepare(void);
/* Private test-build rendezvous: when the configured busy-request seam is
 * enabled, waits until Display Task is executing that request. Production
 * configs return success immediately and expose no control surface. */
bool display_service_start_test_request(void);
bool display_service_wait_for_test_request_start(uint32_t timeout_ms);
bool display_service_get_snapshot(display_service_snapshot_t *out_snapshot);
bool display_service_get_brightness_state(
    display_service_brightness_state_t *out_state);
/* Read-only evidence for the logical Display Service domain (semantic
 * admission, queue and worker). It deliberately does not report profile
 * renderer/panel/DMA restart state. */
bool display_service_get_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot);

bool display_service_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget);

void display_service_set_command_lock(bool locked);
device_status_t display_service_set_brightness(uint8_t percent);
/* Power Service owns policy/timing, but its panel-visible transitions must
 * join the same Display Task as every scene and brightness update. */
/* A visibility transition remains synchronous with the Display Task.  Its
 * status distinguishes an ineligible/currently active scene from queue,
 * panel or adapter failure without exposing any controller detail. */
device_status_t display_service_enter_display_off(void);
device_status_t display_service_wake_display(void);
bool display_service_display_is_off(void);
void display_service_show_startup(void);
void display_service_set_pet_state(const char *state);
void display_service_set_command_stage(const char *stage);
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
/* Per-entry owned 72-byte 24x24 bitmap. The service creates a heap-owned
 * copy of the caller's buffer before forwarding, so the caller may reuse or
 * release it when this call returns and the Display Task never retains the
 * producer pointer. The owned record is freed after the synchronous Platform
 * copy into the board's bounded LRU. */
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
