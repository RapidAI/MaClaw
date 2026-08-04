#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "esp_err.h"
#include "qrcode.h"

// Board-specific contract implemented by board_port.c for EchoEar-2ST's
// display, CST816 touch controller, I2S microphone and ES8311 speaker path.
typedef enum {
    BOARD_BUTTON_SHORT = 0,
    BOARD_BUTTON_DOUBLE,
    BOARD_BUTTON_LONG,
} board_port_button_event_t;

typedef void (*board_port_button_cb_t)(board_port_button_event_t event, void *arg);
typedef void (*board_port_wake_word_cb_t)(void *arg);

typedef enum {
    BOARD_SETTINGS_CLOSED = 0,
    BOARD_SETTINGS_VOLUME_CHANGED,
    BOARD_SETTINGS_CLEAR_PAIRING,
    BOARD_SETTINGS_CLEAR_ALL,
    // Appended at the end so the existing values never change; the payload
    // is the chosen preset index, not seconds.
    BOARD_SETTINGS_TIMEOUT_CHANGED,
} board_port_settings_event_t;

typedef void (*board_port_settings_cb_t)(board_port_settings_event_t event,
                                         uint8_t value, void *arg);

esp_err_t board_port_init(board_port_button_cb_t on_button, void *arg);
void board_port_set_settings_callback(board_port_settings_cb_t callback, void *arg);
void board_port_set_pet_state(const char *state);
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
// Installs up to two GUI-rendered RGB565LE frames. Pixels are in ordinary
// little-endian RGB565 byte order; the board converts them to panel wire order
// while compositing. Passing NULL/zero clears the remote asset and restores
// the native skin renderer.
esp_err_t board_port_set_pet_asset(const uint8_t *frames, size_t frame_count,
                                   uint16_t width, uint16_t height,
                                   uint32_t frame_interval_ms,
                                   const char *revision);
// Returns true when the exact revision is already installed. This lets the
// network layer skip redundant downloads and flash writes after reconnects.
bool board_port_has_pet_asset_revision(const char *revision);
// Shows the dedicated dynamic meeting-recording surface. Call every second
// with the elapsed duration while recording; passing active=false restores the
// selected pet screen.
void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed_seconds);
// Selects the copy shown by the shared waveform renderer. The waveform is
// reused for both modes, but a one-shot command must never claim to be a
// meeting recording.
void board_port_set_recording_mode(bool meeting);
// Updates the normalized microphone level and elapsed time without forcing an LCD refresh from the
// I2S task. The renderer receives its signed waveform directly from the PCM capture paths.
void board_port_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void board_port_show_text(const char *title, const char *text);
// Full-screen meeting upload state. The caller supplies completed/total bytes
// so users can distinguish a slow transfer from a stalled device.
void board_port_show_upload_progress(size_t completed_bytes, size_t total_bytes,
                                     const char *stage);
// Shows an assistant reply in a four-line paged reading surface. Long text
// advances automatically while keeping each page readable on the round LCD.
void board_port_show_response(const char *title, const char *text);
// Adds/refreshes compact 24x24 glyphs supplied by the Hub. The RAM cache is
// bounded and uses least-recently-used replacement, so arbitrary UTF-8 text
// can render without embedding a full Chinese font in the firmware.
int board_port_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]);
// Draws a scannable QR code on the full display while the provisioning access
// point is active. It stays on screen until another display operation occurs.
void board_port_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid);
// Pairing recovery keeps the already-connected station online but also opens
// the same local setup AP. Show both the hotspot and manual portal address on
// a stable full-screen surface when a Wi-Fi QR is not needed.
void board_port_show_setup_portal(const char *ssid, const char *url,
                                  bool pairing_only);
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
// Adds calm, glanceable context to the ready pet surface. `time` is the local
// clock in "HH:MM:SS" format, `location` comes from the weather payload,
// `date` is a compact local date such as "08/02", `weekday` is localized by
// the firmware, and the weather fields are supplied by MaClaw GUI through the
// Hub protocol.
void board_port_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
                            const char *weather_summary, int temperature_c,
                            bool weather_valid, bool weather_stale);

// Must return a complete PCM WAV buffer allocated with heap_caps_malloc or
// malloc. Caller owns it and releases it with free(). A 16 kHz/16-bit/mono
// WAV is the hardware-to-MaClawSrv media contract; the server transcribes it
// before handing the resulting command to the agent.
esp_err_t board_port_capture_wav(uint8_t **out_wav, size_t *out_len);
// Ends an in-progress short command capture at the next PCM chunk boundary.
// Returns true only when a live capture accepted the request; callers can use
// that to consume the touch instead of treating it as another command.
bool board_port_request_capture_stop(void);

// Streaming capture for long meetings. Reads 16 kHz, signed 16-bit mono PCM
// into caller-owned memory without buffering the full recording in RAM.
esp_err_t board_port_audio_stream_start(void);
esp_err_t board_port_audio_stream_read(int16_t *mono, size_t sample_capacity,
                                       size_t *samples_read, uint16_t *level);
void board_port_audio_stream_stop(void);
// Recording health counters (I2S read failures / short reads) accumulated
// since the last capture_wav()/audio_stream_start().
void board_port_get_record_stats(uint32_t *overruns, uint32_t *short_reads);
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
// Speaker volume, 0 (mute) to 100 (full scale). Stored in the board layer and
// re-applied after every codec reset; the app layer persists it in NVS.
esp_err_t board_port_set_volume(uint8_t percent);
uint8_t board_port_get_volume(void);
// Idle screen-off timeout in seconds; 0 keeps the panel always on. Stored in
// the board layer and re-arms the running idle countdown; the app layer
// persists it in NVS.
void board_port_set_screen_timeout(uint32_t seconds);
uint32_t board_port_get_screen_timeout(void);
// LCD backlight brightness, 0 (off) to 100 (full). Stored in the board layer;
// the idle-sleep fade returns to this level on wake.
void board_port_set_backlight(uint8_t percent);
// Powers down display backlight and speaker PA, then holds both pads so their
// state survives deep sleep. Call before esp_deep_sleep_start().
void board_port_prepare_deep_sleep(void);
// Short local acknowledgement used while the network/TTS reply is pending.
esp_err_t board_port_play_ack_chime(void);
