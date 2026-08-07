#pragma once

/*
 * MaClaw AgentOS public Device API foundation.
 *
 * This header is intentionally restricted to ISO C value types.  Domain/app
 * code may include it, while ESP-IDF error values, FreeRTOS handles, driver
 * objects and board-profile details must remain below the adapter boundary.
 */

#include <stdbool.h>
#include <stdint.h>

typedef enum {
    DEVICE_STATUS_OK = 0,
    DEVICE_STATUS_INVALID_ARGUMENT,
    DEVICE_STATUS_UNAVAILABLE,
    DEVICE_STATUS_BUSY,
    DEVICE_STATUS_TIMEOUT,
    DEVICE_STATUS_RESOURCE_EXHAUSTED,
    DEVICE_STATUS_IO_ERROR,
    DEVICE_STATUS_INTERNAL_ERROR,
} device_status_t;

/* Maps a Device API result to ESP-IDF only at a legacy component boundary.
 * New business/services code should keep device_status_t end-to-end. */
int device_status_to_platform_error(device_status_t status);

/*
 * Static, board-supplied facts.  These flags describe physical adapters only;
 * they are not a menu of product features.  In particular, a formal MaClaw
 * AgentOS board must implement the common business baseline even when it
 * exposes a different local control or renderer.
 */
typedef uint32_t device_capability_flags_t;

enum {
    DEVICE_CAPABILITY_DISPLAY             = 1u << 0,
    DEVICE_CAPABILITY_TOUCH_INPUT         = 1u << 1,
    DEVICE_CAPABILITY_PRIMARY_CONTROL     = 1u << 2,
    DEVICE_CAPABILITY_VOLUME_CONTROL      = 1u << 3,
    DEVICE_CAPABILITY_OUTPUT_VOLUME       = 1u << 4,
    DEVICE_CAPABILITY_AUDIO_CAPTURE       = 1u << 5,
    DEVICE_CAPABILITY_AUDIO_PLAYBACK      = 1u << 6,
    DEVICE_CAPABILITY_OFFLINE_WAKE_WORD   = 1u << 7,
    DEVICE_CAPABILITY_PERSISTENT_STORAGE  = 1u << 8,
    DEVICE_CAPABILITY_BATTERY_TELEMETRY   = 1u << 9,
    /* Panel/backlight off only.  This does not claim MCU light/deep sleep. */
    DEVICE_CAPABILITY_DISPLAY_OFF          = 1u << 10,
    DEVICE_CAPABILITY_CELLULAR_TRANSPORT  = 1u << 11,
    DEVICE_CAPABILITY_ROUND_DISPLAY       = 1u << 12,
};

#define DEVICE_PROFILE_ABI_VERSION 1u

/* Every production MaClaw AgentOS board carries this complete business
 * baseline.  Optional capability bits describe the local adaptation, never a
 * product feature subset. */
#define DEVICE_CAPABILITY_REQUIRED_BASELINE \
    (DEVICE_CAPABILITY_DISPLAY | DEVICE_CAPABILITY_PRIMARY_CONTROL | \
     DEVICE_CAPABILITY_OUTPUT_VOLUME | DEVICE_CAPABILITY_AUDIO_CAPTURE | \
     DEVICE_CAPABILITY_AUDIO_PLAYBACK | DEVICE_CAPABILITY_OFFLINE_WAKE_WORD | \
     DEVICE_CAPABILITY_PERSISTENT_STORAGE | DEVICE_CAPABILITY_DISPLAY_OFF)

#define DEVICE_CAPABILITY_KNOWN_MASK \
    (DEVICE_CAPABILITY_DISPLAY | DEVICE_CAPABILITY_TOUCH_INPUT | \
     DEVICE_CAPABILITY_PRIMARY_CONTROL | DEVICE_CAPABILITY_VOLUME_CONTROL | \
     DEVICE_CAPABILITY_OUTPUT_VOLUME | DEVICE_CAPABILITY_AUDIO_CAPTURE | \
     DEVICE_CAPABILITY_AUDIO_PLAYBACK | DEVICE_CAPABILITY_OFFLINE_WAKE_WORD | \
     DEVICE_CAPABILITY_PERSISTENT_STORAGE | DEVICE_CAPABILITY_BATTERY_TELEMETRY | \
     DEVICE_CAPABILITY_DISPLAY_OFF | DEVICE_CAPABILITY_CELLULAR_TRANSPORT | \
     DEVICE_CAPABILITY_ROUND_DISPLAY)

/* A profile describes the role of its local physical control without exposing
 * GPIOs, touch-controller details or gesture timing.  Business policy can use
 * this to identify the standard local control on every hardware variant. */
typedef enum {
    DEVICE_INPUT_SOURCE_UNKNOWN = 0,
    DEVICE_INPUT_SOURCE_TOUCH,
    DEVICE_INPUT_SOURCE_PRIMARY_CONTROL,
    DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL,
} device_input_source_t;

/*
 * Returned by value so callers cannot retain a mutable board-owned pointer.
 * Width/height describe the renderer's logical viewport, rather than panel
 * controller RAM (for example Fangtang's 80-line GRAM offset is not exposed).
 */
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    const char *id;
    uint16_t display_width;
    uint16_t display_height;
    device_capability_flags_t capabilities;
    device_input_source_t primary_interaction_source;
    const char *primary_interaction_label;
} device_profile_t;

/* Reads the immutable profile compiled into this image. */
bool device_profile_get(device_profile_t *out_profile);

/* Validates a by-value profile snapshot against the stable Device API ABI.
 * This protects composition roots and future test adapters from treating an
 * incomplete or self-contradictory profile as a supported hardware target. */
bool device_profile_is_valid(const device_profile_t *profile);

bool device_profile_has_capability(device_capability_flags_t capability);

/*
 * Shared 0..100 output-volume contract. The implementation may use codec
 * gain or direct-I2S software gain; callers never select either mechanism.
 */
device_status_t device_audio_set_output_volume(uint8_t percent);
device_status_t device_audio_adjust_output_volume(int delta_percent,
                                                  uint8_t *out_percent);

/* Raw PCM/WAV and offline-wake primitives. Higher-level command/meeting
 * policy remains outside this API; these calls only own hardware adaptation. */
device_status_t device_audio_play_wav(const uint8_t *wav, uint32_t wav_len);
/* Plays one bounded, locally generated alarm burst. Alarm scheduling and
 * retry/dismiss policy remain in the domain service; adapters only own the
 * board's PCM/codec/I2S implementation of this primitive. */
device_status_t device_audio_play_alarm_burst(void);
device_status_t device_audio_capture_wav(uint8_t **out_wav, uint32_t *out_len);
device_status_t device_audio_stream_start(void);
device_status_t device_audio_stream_read(int16_t *mono, uint32_t capacity,
                                         uint32_t *samples_read, uint16_t *level);
void device_audio_stream_stop(void);

/* Streaming playback is deliberately a Device API transaction rather than a
 * decoder-to-board shortcut.  Codecs and I2S wiring remain below this seam;
 * decoders only submit normalized 16 kHz signed PCM frames. */
device_status_t device_audio_playback_begin(void);
device_status_t device_audio_playback_write(const int16_t *pcm, uint32_t frames,
                                            uint8_t channels);
/* Ends the current playback transaction. `playback_succeeded` tells the
 * adapter whether to drain normally or abort its local sink. */
device_status_t device_audio_playback_end(bool playback_succeeded);

/* Cooperative foreground-audio controls.  They never identify a GPIO, codec
 * or I2S peripheral, so alarm and interaction policy can be shared by every
 * profile. */
void device_audio_request_playback_stop(void);
void device_audio_request_capture_stop(void);
void device_audio_reset_capture_stop(void);

typedef void (*device_wake_word_cb_t)(void *context);
device_status_t device_wake_word_start(device_wake_word_cb_t on_wake, void *context);
device_status_t device_wake_word_stop(void);
void device_wake_word_pause(bool paused);

// Absolute time on the device's monotonic millisecond clock.  A value of zero
// means "no deadline"; it is never a wall-clock timestamp.
typedef uint64_t device_deadline_ms_t;

/*
 * Power is modeled explicitly even though the currently proven common state
 * is DISPLAY_OFF only.  DISPLAY_OFF means panel/backlight off while the MCU,
 * network, alarms and offline wake-word remain active; it must never be
 * presented as light or deep sleep.  Future LIGHT_SLEEP/DEEP_SLEEP states
 * require their own verified wake-source and lifecycle contracts.
 */
typedef enum {
    DEVICE_POWER_STATE_ACTIVE = 0,
    DEVICE_POWER_STATE_DISPLAY_OFF,
} device_power_state_t;

/* A by-value observation of the only currently supported power transition.
 * `display_off_armed` means an application idle deadline is pending; it does
 * not imply that the physical panel is already off.  No field in this
 * snapshot asserts LIGHT_SLEEP or DEEP_SLEEP support. */
typedef struct {
    device_power_state_t state;
    bool display_off_armed;
} device_power_snapshot_t;

/* Optional physical telemetry. A profile without calibrated battery hardware
 * reports available=false; callers must never infer battery state from a
 * board ID or treat a missing sensor as an empty/zero battery. */
typedef struct {
    bool available;
    uint8_t level_percent;
    bool charging;
} device_power_telemetry_t;

/* Starts the common power-service scheduler after the selected board adapter
 * is initialized.  Repeated initialization is safe. */
device_status_t device_power_init(void);

/* Arms/cancels the application-owned idle deadline for DISPLAY_OFF. A zero
 * delay is invalid; callers must cancel before publishing foreground work. */
device_status_t device_power_schedule_display_off(uint32_t idle_after_ms);
void device_power_cancel_display_off(void);

/* Consumes a local physical wake only when the panel was off. It deliberately
 * preserves each adapter's existing first-contact semantics. */
bool device_power_wake_display_from_user(void);

/* Returns the Power Service's serialized observation of the panel/backlight
 * state and any pending display-off deadline. */
bool device_power_get_snapshot(device_power_snapshot_t *out_snapshot);

/* Reads the latest board-owned power telemetry snapshot without exposing ADC
 * units, divider ratios, charge GPIO polarity, or sampling implementation. */
bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry);

/* Connectivity presentation is a shared intent: it selects which currently
 * active uplink is shown in the ambient UI.  The cellular modem, Wi-Fi stack
 * and profile-specific visual treatment remain below this API. */
typedef enum {
    DEVICE_UPLINK_WIFI = 0,
    DEVICE_UPLINK_CELLULAR,
} device_uplink_t;

/* A by-value observation of the selected transport. `ready` is published by
 * the Wi-Fi or cellular adapter after its own bounded start/recovery work; it
 * is deliberately not a claim that Hub authentication or a request is ready. */
typedef struct {
    device_uplink_t active_uplink;
    bool wifi_ready;
    bool cellular_ready;
    bool ready;
} device_connectivity_snapshot_t;

void device_connectivity_set_active_cellular(bool active);
bool device_connectivity_is_active_cellular(void);

/* Transport adapters publish readiness after their own bounded start/recovery
 * work. App/domain code queries a single selected-uplink observation; it does
 * not read a Wi-Fi event group or a board-specific modem readiness value. */
void device_connectivity_set_wifi_ready(bool ready);
void device_connectivity_set_cellular_ready(bool ready);
bool device_connectivity_is_active_uplink_ready(void);
bool device_connectivity_get_snapshot(device_connectivity_snapshot_t *out_snapshot);

/* Performs the selected profile's bounded physical preparation for its
 * cellular transport (for example modem guard/power sequencing).  It neither
 * starts a modem nor applies gateway policy; those remain transport/domain
 * responsibilities. Profiles without cellular hardware report UNAVAILABLE. */
device_status_t device_connectivity_prepare_cellular_transport(void);

/* Some profiles expose a bounded, pre-input startup selector.  It is a
 * hardware-normalized intent rather than a GPIO gesture; profiles without
 * such a selector return false. */
bool device_connectivity_take_startup_transport_toggle(uint32_t window_ms);

/*
 * Display Device API.  The shared UI state machine publishes semantic scenes
 * and display state through this boundary; it never selects a panel driver,
 * framebuffer format, controller RAM offset, round-screen clipping rule, or
 * a physical backlight.  The selected profile adapter remains responsible
 * for synchronously copying any caller-owned payload that it needs after the
 * call returns.
 */
void device_display_set_command_lock(bool locked);
void device_display_show_startup(void);
void device_display_set_pet_state(const char *state);
void device_display_set_command_stage(const char *stage);
void device_display_set_command_cancel_enabled(bool enabled);
void device_display_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t device_display_set_pet_asset(const uint8_t *const *frames,
                                             uint32_t frame_count,
                                             uint32_t width, uint32_t height,
                                             uint32_t frame_ms);
void device_display_set_recording_mode(bool meeting);
void device_display_set_recording_visual(bool active, bool paused,
                                         uint32_t elapsed_seconds);
void device_display_set_audio_level(uint16_t level, uint32_t elapsed_seconds);
void device_display_show_text(const char *title, const char *text);
void device_display_show_upload_progress(uint32_t completed_bytes,
                                         uint32_t total_bytes,
                                         const char *stage);
void device_display_show_response(const char *title, const char *text);
void device_display_show_response_image(const char *title, const char *caption,
                                        const uint16_t *pixels,
                                        uint32_t width, uint32_t height);
bool device_display_navigate_response(int page_delta);
bool device_display_get_response_page(uint32_t *out_page);
bool device_display_restore_response_page(uint32_t page);
int device_display_cache_glyph(uint32_t codepoint,
                               const uint8_t bitmap[72]);
void device_display_show_qrcode_modules(const uint8_t *modules,
                                        uint32_t module_count,
                                        const char *ssid);
void device_display_show_ready_prompt(const char *title, const char *text);
void device_display_cancel_ready_prompt(void);
void device_display_set_wifi_status(const char *ssid, bool connected);
void device_display_set_service_ready(bool ready);
void device_display_set_ambient(const char *time, const char *location,
                                const char *date, const char *weekday,
                                const char *weather_summary,
                                int temperature_c, bool weather_valid,
                                bool weather_stale);
void device_display_set_alarm_scheduled(bool scheduled);
void device_display_set_alarm_visual(bool active, uint32_t frame,
                                     const char *time_text, const char *label,
                                     uint32_t attempt, uint32_t max_attempts);

// Shared business intents.  These names describe the operation requested by
// the user, not the button, touch controller or gesture recognizer that
// generated it.
typedef enum {
    DEVICE_INPUT_PRIMARY = 0,
    DEVICE_INPUT_SECONDARY,
    DEVICE_INPUT_CONFIGURE,
    DEVICE_INPUT_VOLUME_UP,
    DEVICE_INPUT_VOLUME_DOWN,
    DEVICE_INPUT_CONTACT_DOWN,
} device_input_action_t;

// Physical provenance is retained only for policy such as consuming an alarm
// dismissal contact.  Profiles map GPIO/touch-controller specifics into these
// stable categories; app code must not infer pin or board identity from them.

typedef void (*device_input_cb_t)(device_input_action_t action,
                                  device_input_source_t source, void *context);

/*
 * Starts the shared input boundary.  Board adapters publish normalized input
 * values here; a single application consumer receives them from a bounded
 * task-owned queue.  The callback never executes in a GPIO/touch scan task.
 *
 * This is deliberately a lifecycle operation rather than a board-init hook:
 * application code must not register a board callback or know which physical
 * controller produced the input.  The service is currently boot-lifetime;
 * Stop is supported for coordinated shutdown.  Re-starting after a stop is
 * intentionally unavailable until every board adapter also implements a real
 * scan-task deinit/join lifecycle; this prevents duplicate GPIO/touch scanner
 * tasks from being silently created.
 */
device_status_t device_input_start(device_input_cb_t on_input, void *context);

/* Stops application event delivery and joins the service task within the
 * supplied bounded timeout.  The underlying physical scanner remains owned
 * by the current board adapter, but its already-registered publisher becomes
 * a no-op before service queues are released. */
device_status_t device_input_stop(uint32_t timeout_ms);

// Returns whether a physical source is the profile's normal local control for
// the shared primary intent.  It deliberately hides whether that control is a
// touch surface, button, rotary input, or future accessibility switch.
bool device_input_is_primary_interaction_source(device_input_source_t source);

// A short, profile-local noun for user-facing prompts (for example "激活键"
// or "屏幕"). The caller owns the surrounding workflow text; this accessor
// supplies no board ID, GPIO, or gesture policy.
const char *device_input_primary_interaction_label(void);
