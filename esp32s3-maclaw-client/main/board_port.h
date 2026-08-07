#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "esp_err.h"
#include "qrcode.h"
#include "device_api.h"

// This is the only board-specific contract in the starter.  Implement the
// functions in board_port.c for the display, button/touch controller, I2S mic,
// and optional speaker fitted to the actual ESP32-S3 smart-speaker board.
typedef device_input_action_t board_input_action_t;
#define BOARD_INPUT_PRIMARY DEVICE_INPUT_PRIMARY
#define BOARD_INPUT_SECONDARY DEVICE_INPUT_SECONDARY
#define BOARD_INPUT_CONFIGURE DEVICE_INPUT_CONFIGURE
#define BOARD_INPUT_VOLUME_UP DEVICE_INPUT_VOLUME_UP
#define BOARD_INPUT_VOLUME_DOWN DEVICE_INPUT_VOLUME_DOWN
// Emitted on the debounced physical down edge. This lets urgent local
// surfaces react immediately without waiting for short/double/long gesture
// classification. Normal command handling ignores this action.
#define BOARD_INPUT_PRESSED DEVICE_INPUT_CONTACT_DOWN

// Physical origin is deliberately independent of gesture semantics. Product
// features can therefore select the input their enclosure actually exposes
// without guessing from PRIMARY/SECONDARY/CONFIGURE.
typedef device_input_source_t board_input_source_t;
#define BOARD_INPUT_SOURCE_UNKNOWN DEVICE_INPUT_SOURCE_UNKNOWN
#define BOARD_INPUT_SOURCE_TOUCH DEVICE_INPUT_SOURCE_TOUCH
#define BOARD_INPUT_SOURCE_ACTIVATE_KEY DEVICE_INPUT_SOURCE_PRIMARY_CONTROL
#define BOARD_INPUT_SOURCE_OTHER_KEY DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL

typedef device_input_cb_t board_input_cb_t;
// Source-compatible names for board ports that have not yet adopted the
// hardware-neutral terminology.
typedef board_input_action_t board_port_button_event_t;
typedef board_input_cb_t board_port_button_cb_t;
#define BOARD_BUTTON_SHORT BOARD_INPUT_PRIMARY
#define BOARD_BUTTON_DOUBLE BOARD_INPUT_SECONDARY
#define BOARD_BUTTON_LONG BOARD_INPUT_CONFIGURE
typedef void (*board_port_wake_word_cb_t)(void *arg);

// The application owns idle policy; the board port owns only the physical
// panel/backlight transaction. DISPLAY_OFF keeps alarm, network and wake-word
// services running, unlike a future MCU light/deep-sleep transition.
bool board_port_enter_display_off(void);
/* Returns the adapter's current physical display-off observation.  This is
 * intentionally separate from an idle deadline: a later renderer may wake a
 * panel to present an urgent scene without changing Power Service policy. */
bool board_port_display_is_off(void);

esp_err_t board_port_init(board_port_button_cb_t on_button, void *arg);
// Fangtang uses GPIO0's initial double click exclusively as a boot-time
// network-transport selector. The board consumes this bounded window before
// normal application callbacks become active. Other boards return false.
bool board_port_wait_for_boot_network_toggle(uint32_t window_ms);
/* Applies only the board-owned modem guard/power wiring required before a
 * cellular transport adapter starts. It deliberately does not start ML307,
 * select an uplink, or issue any network request. */
esp_err_t board_port_prepare_cellular_transport(void);
// Re-presents the board-specific boot artwork and keeps it in the foreground
// until another explicit surface (ready, setup, error, etc.) replaces it.
void board_port_show_startup_screen(void);
// Applies a relative output-volume step. The resulting 0..100 value is optional;
// a board without physical volume keys may still expose software/remote volume.
esp_err_t board_port_adjust_output_volume(int delta_percent, unsigned *out_percent);
// Applies an absolute 0..100 output-volume value received from MaClaw.
esp_err_t board_port_set_output_volume(unsigned percent);
void board_port_set_pet_state(const char *state);
// Updates the short phase label rendered inside the command-owned thinking
// surface without replacing that surface or stopping its animation.
void board_port_set_command_stage(const char *stage);
// Suppresses every background pet/profile/Wi-Fi/ambient repaint while a
// foreground command owns the screen. The command code clears it only when a
// later explicit interaction begins, so the answer remains stable.
void board_port_set_command_display_lock(bool locked);
// Enables a deliberate panel double tap while the short voice command is in
// its thinking phase. Raw CST816 contacts closer than 180 ms are still treated
// as one touch so the controller's duplicate contact cannot cancel a command.
void board_port_set_command_cancel_enabled(bool enabled);
// Applies the selected MaClaw GUI pet profile. The ESP uses a compact native
// renderer for supported skins and falls back gracefully for custom packs.
void board_port_set_pet_profile(const char *skin, bool motion_enabled);
// Installs negotiated GUI-rendered RGB565LE+A8 standby frames (three bytes per
// pixel: little-endian RGB565 followed by alpha). The caller retains
// ownership only for the duration of the call; board ports copy the pixels.
// Passing no frames clears the remote asset and restores the native skin.
esp_err_t board_port_set_pet_asset(const uint8_t *const *frames, size_t frame_count,
                                   size_t width, size_t height, uint32_t frame_ms);
// Shows the dedicated dynamic meeting-recording surface. Call every second
// with the elapsed duration while recording; passing active=false restores the
// selected pet screen.
void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds);
// Selects the copy shown by the shared waveform renderer. The waveform is
// reused for both modes, but a one-shot command must never claim to be a
// meeting recording.
void board_port_set_recording_mode(bool meeting);
// Updates the normalized microphone level and elapsed time without forcing an LCD refresh from the
// I2S task. The renderer advances its 24-column waveform from this filtered level history.
void board_port_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void board_port_push_recording_pcm(const int16_t *samples, size_t count);
void board_port_show_text(const char *title, const char *text);
// Full-screen meeting upload state. The caller supplies completed/total bytes
// so users can distinguish a slow transfer from a stalled device.
void board_port_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                     const char *stage);
// Shows an assistant reply in a paged reading surface. Boards without page
// keys advance automatically; boards with volume keys expose manual paging.
void board_port_show_response(const char *title, const char *text);
// Shows a display-ready RGB565 image. Pixels are in panel wire byte order and
// dimensions are bounded by the negotiated 64x64 device capability.
void board_port_show_response_image(const char *title, const char *caption,
                                    const uint16_t *pixels, size_t width, size_t height);
// Moves a paged response surface. Returns false when no response is visible,
// allowing the caller to retain the keys' normal volume function.
bool board_port_navigate_response(int page_delta);
// Returns the currently rendered zero-based text-reply page. Image replies
// report page 0. This lets the shared foreground coordinator preserve reading
// position when an alarm temporarily owns the display.
bool board_port_get_response_page(unsigned *page);
// Restores a zero-based text-reply page after board_port_show_response(). The
// renderer clamps out-of-range values to the final available page.
bool board_port_restore_response_page(unsigned page);
// Adds/refreshes compact 24x24 glyphs supplied by the Hub. The RAM cache is
// bounded and uses least-recently-used replacement, so arbitrary UTF-8 text
// can render without embedding a full Chinese font in the firmware.
int board_port_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
// Draws a scannable QR code on the full display while the provisioning access
// point is active. It stays on screen until another display operation occurs.
void board_port_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid);
// Renders an owned QR module matrix. This is the replay-safe form used by the
// shared UI coordinator when an alarm temporarily preempts the setup scene.
void board_port_show_qrcode_matrix(const uint8_t *modules, size_t module_count,
                                   const char *ssid);
// Shows the normal ready-to-talk hint temporarily. After one minute with no
// interaction, the display returns to the selected MaClaw GUI pet.
void board_port_show_ready_prompt(const char *title, const char *text);
// Removes a pending ready-screen timeout when a new interaction begins.
void board_port_cancel_ready_prompt(void);
// Restores the LCD after the idle pet screen has slept. Returns true when this
// press should be consumed to return to the ready prompt rather than start voice.
bool board_port_wake_from_idle(void);
// Draws a compact Wi-Fi indicator in the pet status area without repainting the
// full screen. Use an empty SSID to clear it.
void board_port_set_wifi_status(const char *ssid, bool connected);
// Updates authenticated MaClaw Hub reachability independently from the
// selected radio transport. Compact standby screens use this for ONLINE/WAIT;
// boards without that status row retain it as a no-op.
void board_port_set_service_ready(bool ready);

// Optional local power telemetry. Boards without a monitored battery return
// false; Fangtang reports its original ADC2/channel-6 level and GPIO38 charge
// input without exposing those board-specific details to application code.
bool board_port_get_power_status(unsigned *level_percent, bool *charging);
// Adds calm, glanceable context to the ready pet surface. `time` is the local
// clock in "HH:MM:SS" format, `location` comes from the weather payload,
// `date` is a compact local date such as "08/02", `weekday` is localized by
// the firmware, and the weather fields are supplied by MaClaw GUI through the
// Hub protocol.
void board_port_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
                            const char *weather_summary, int temperature_c,
                            bool weather_valid, bool weather_stale);
// Fangtang exposes the selected uplink beside the standby calendar. Other
// boards ignore this hint and retain their established ambient layouts.
void board_port_set_network_transport(bool cellular);
// Shows whether a locally scheduled alarm exists. Compact boards use this as
// a small affordance beside the standby calendar; foreground surfaces do not
// repaint when this state changes.
void board_port_set_alarm_scheduled(bool scheduled);
// Dedicated local alarm foreground. EchoEar animates it; compact boards render
// a reduced surface. Passing active=false restores the ambient pet screen.
void board_port_set_alarm_visual(bool active, unsigned frame, const char *time_text,
                                 const char *label, unsigned attempt, unsigned max_attempts);

// Must return a complete PCM WAV buffer allocated with heap_caps_malloc or
// malloc. Caller owns it and releases it with free(). A 16 kHz/16-bit/mono
// WAV is the hardware-to-MaClawSrv media contract; the server transcribes it
// before handing the resulting command to the agent. A capture that reaches
// the pre-speech timeout returns ESP_ERR_NOT_FOUND and no WAV, so callers must
// not submit silence as a command.
esp_err_t board_port_capture_wav(uint8_t **out_wav, size_t *out_len);
// Ends an active one-shot command capture at the next audio frame boundary.
// If speech has already started, capture_wav returns the accumulated WAV;
// otherwise it returns ESP_ERR_NOT_FOUND. Safe to call from the input task.
void board_port_request_capture_stop(void);
// Clears a stop request before publishing a new command-recording phase.
void board_port_reset_capture_stop(void);

// Streaming capture for long meetings. Reads 16 kHz, signed 16-bit mono PCM
// into caller-owned memory without buffering the full recording in RAM.
esp_err_t board_port_audio_stream_start(void);
esp_err_t board_port_audio_stream_read(int16_t *mono, size_t sample_capacity,
                                       size_t *samples_read, uint16_t *level);
void board_port_audio_stream_stop(void);
// Starts the always-listening ESP-SR/MultiNet detector. The callback runs from
// the recognition task when the Chinese offline wake word “码卡龙” is detected.
esp_err_t board_port_start_wake_word(board_port_wake_word_cb_t on_wake, void *arg);
// Stops the recognizer and releases its model/audio buffers so a provisioning
// portal can run even on the smallest supported ESP32-S3 memory variant.
esp_err_t board_port_stop_wake_word(void);
// Temporarily gives exclusive microphone ownership to interaction/meeting
// capture, then resumes offline recognition when released.
void board_port_pause_wake_word(bool paused);

// Plays PCM WAV returned by MaClawSrv TTS (16 kHz, signed 16-bit mono/stereo).
esp_err_t board_port_play_wav(const uint8_t *wav, size_t wav_len);
// Streaming PCM sink shared by compressed-audio decoders. The caller owns one
// begin/write/end session; samples must be signed 16-bit PCM at AUDIO_RATE.
esp_err_t board_port_audio_playback_begin(void);
esp_err_t board_port_audio_playback_write(const int16_t *pcm, size_t frames,
                                          unsigned channels);
esp_err_t board_port_audio_playback_end(esp_err_t playback_err);
// Stops an in-flight foreground playback at its next bounded PCM write. A call
// while no playback owns the bus is ignored, so the next alarm burst starts
// with a clean playback transaction.
void board_port_request_audio_playback_stop(void);
// Short local acknowledgement used while the network/TTS reply is pending.
esp_err_t board_port_play_ack_chime(void);
// Plays one short, interruptible mechanical double-bell burst. The alarm task
// repeats bursts so user input is observed between them.
esp_err_t board_port_play_alarm_burst(void);
// Spoken local acknowledgement: “好的，正在处理。”
esp_err_t board_port_play_ack_voice(void);
