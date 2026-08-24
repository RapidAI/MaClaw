#pragma once

/*
 * Scene Presenter (A7 second increment).
 *
 * Maps a semantic scene payload onto the existing shared App UI model.  This
 * increment owns ambient/network/scheduled/pet/glyph/pet-asset, Alarm visual,
 * Command/Meeting recording, command-stage, live capture meters/PCM,
 * message pages, reply text/image, upload progress, ready prompts, Setup QR,
 * startup splash, command display lock / cancel-enabled, input-driven
 * navigate/dismiss/restore-standby/wake, service_ready, and DISPLAY_OFF
 * idle/brightness/schedule-wake.  App UI init and snapshot stay with the
 * composition root / coordinator observer.
 *
 * Pixel output, input routing and audio focus stay with App UI / board
 * renderers.  The presenter never enables Foreground Coordinator
 * authoritative mode.
 *
 * The public contract exposes value types only.
 */

#include "device_api.h"
#include "presentation/scene_model.h"

device_status_t scene_presenter_init(void);

/* Forwards ambient fields into the shared UI model.  App UI already stores
 * updates received behind a result/upload/setup/alarm surface for the first
 * restored standby frame; the presenter does not add a second guard. */
void scene_presenter_publish_ambient(const scene_ambient_fields_t *fields);

void scene_presenter_publish_network(const scene_network_fields_t *fields);
void scene_presenter_publish_alarm_scheduled(bool scheduled);

/* NULL pet state matches App UI: it becomes idle.  NULL skin is a valid
 * profile update that only refreshes motion. */
void scene_presenter_publish_pet_state(const char *state);
void scene_presenter_publish_pet_profile(const char *skin, bool motion_enabled);

/* Pet animation frames.  Borrowed pointers stay borrowed; the consuming
 * path forwards the same mutable array so App UI / board can NULL consumed
 * entries.  NULL/0 is a valid clear.  Status is App UI's, unrewritten.
 * Download, cache and startup restore stay with the composition root. */
device_status_t scene_presenter_set_pet_asset(const uint8_t *const *frames,
                                              size_t frame_count, size_t width,
                                              size_t height, uint32_t frame_ms);
device_status_t scene_presenter_set_pet_asset_consuming(uint8_t **frames,
                                                        size_t frame_count, size_t width,
                                                        size_t height, uint32_t frame_ms);

/* Borrowed 72-byte 24x24 bitmap.  Returns the App UI cache result; the
 * caller may release the source after this returns. */
int scene_presenter_cache_glyph(uint32_t codepoint,
                                const uint8_t bitmap[SCENE_GLYPH_BITMAP_BYTES]);

/* NULL command stage matches App UI (it substitutes the default processing
 * copy).  Alarm visual NULL time/label are empty strings at App UI. */
void scene_presenter_publish_command_stage(const char *stage);
void scene_presenter_publish_recording_mode(bool meeting);
void scene_presenter_publish_recording_visual(bool active, bool paused,
                                              uint32_t elapsed_seconds);
/* Live capture meters.  Level is forwarded unclamped; App UI still owns the
 * 1000 cap and Bread smoothing.  PCM samples stay borrowed; App UI currently
 * no-ops them and advances the waveform from the level path. */
void scene_presenter_publish_audio_level(uint16_t level, uint32_t elapsed_seconds);
void scene_presenter_push_recording_pcm(const int16_t *samples, size_t count);
void scene_presenter_publish_alarm_visual(bool active, unsigned frame,
                                          const char *time_text, const char *label,
                                          unsigned attempt, unsigned max_attempts);

/* NULL title/body match App UI (empty replay copy).  Response image pixels
 * stay borrowed until App UI copies them; NULL/oversized buffers are App UI
 * no-ops. */
void scene_presenter_publish_message(const char *title, const char *text);
void scene_presenter_publish_response(const char *title, const char *text);
void scene_presenter_publish_response_image(const char *title, const char *caption,
                                            const uint16_t *pixels, size_t width,
                                            size_t height);
void scene_presenter_publish_upload_progress(size_t completed_bytes, size_t total_bytes,
                                             const char *stage);
void scene_presenter_publish_ready_prompt(const char *title, const char *text);
void scene_presenter_cancel_ready_prompt(void);
bool scene_presenter_publish_setup_qr(const uint8_t *modules, size_t module_count,
                                      const char *ssid);

void scene_presenter_publish_startup_splash(void);
void scene_presenter_publish_command_display_lock(bool locked);
void scene_presenter_publish_command_cancel_enabled(bool enabled);

/* Input-driven scene control.  Return values match App UI so callers keep
 * the same fall-through (unhandled page / not-a-response / already awake). */
bool scene_presenter_navigate_response(int page_delta);
bool scene_presenter_dismiss_response(void);
void scene_presenter_restore_standby(void);
bool scene_presenter_wake_from_idle(void);

/* Hub/transport readiness shown on the shared model.  Distinct from
 * firmware_identity_set_service_ready, which is not a scene publish. */
void scene_presenter_publish_service_ready(bool ready);

/* DISPLAY_OFF policy. Zero idle disables auto-off. OK confirms the common UI
 * accepted the policy; it is deliberately not a physical-panel-off claim.
 * Remote brightness status is App UI's (percent>100 rejected there). Schedule
 * wake is not an input event and must not be confused with wake_from_idle. */
device_status_t scene_presenter_apply_display_off_idle_policy(uint32_t idle_after_ms);
/* Acknowledgement of the common UI policy only; it never exposes a panel,
 * timer, Power lease, or board-private rendering state. */
#define SCENE_DISPLAY_OFF_IDLE_POLICY_STATE_ABI_VERSION 2u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool known;
    uint32_t idle_after_ms;
    device_status_t last_status;
    /* A deferred foreground scene has no deadline to observe. When required,
     * ARMED is evidence only that Power accepted the DISPLAY_OFF deadline;
     * it is not evidence that a panel has already powered down. */
    bool schedule_required;
    bool schedule_known;
    bool schedule_armed;
    device_status_t schedule_last_status;
} scene_display_off_idle_policy_state_t;
bool scene_presenter_get_display_off_idle_policy_state(
    scene_display_off_idle_policy_state_t *out_state);
device_status_t scene_presenter_apply_remote_brightness(uint8_t percent);
void scene_presenter_note_schedule_display_wake(void);
