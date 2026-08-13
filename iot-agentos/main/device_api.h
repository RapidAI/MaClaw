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
    /* A capture/search completed normally but found nothing (e.g. no speech
     * before the start timeout). Kept distinct from INTERNAL_ERROR so callers
     * can present a retry hint instead of a hardware-failure message. */
    DEVICE_STATUS_NOT_FOUND,
} device_status_t;

/* Shared resource-pressure observation is deliberately a small value type:
 * callers see usable capacity, not allocator/partition handles.  NORMAL,
 * PRESSURE and CRITICAL are common policy tiers, never board-specific modes. */
#define DEVICE_RESOURCE_PRESSURE_ABI_VERSION 1u
typedef enum {
    DEVICE_RESOURCE_PRESSURE_NORMAL = 0,
    DEVICE_RESOURCE_PRESSURE_PRESSURE,
    DEVICE_RESOURCE_PRESSURE_CRITICAL,
} device_resource_pressure_level_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    device_resource_pressure_level_t level;
    uint32_t internal_free_bytes;
    uint32_t internal_largest_free_bytes;
    uint32_t external_free_bytes;
    uint32_t external_largest_free_bytes;
    bool storage_available;
    uint32_t storage_total_bytes;
    uint32_t storage_free_bytes;
} device_resource_pressure_snapshot_t;

bool device_resource_pressure_get_snapshot(
    device_resource_pressure_snapshot_t *out_snapshot);
bool device_resource_pressure_allows_optional_work(void);
/* Admission for new decorative/background work that needs a bounded peak
 * allocation or a durable write.  The requested values are peak *additional*
 * bytes, not total system usage.  A false result means callers must defer or
 * decline the optional operation; it never authorizes rejecting foreground
 * voice, alarm, recording finalization or persistence recovery. */
bool device_resource_pressure_allows_optional_allocation(
    uint32_t internal_bytes, uint32_t external_bytes, uint32_t storage_bytes);

/* Display adapters expose their bounded installation memory plan.  Total
 * retained bytes and the largest one-shot allocation are distinct allocator
 * facts, so common business code does not need to know renderer geometry. */
#define DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION 2u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t total_external_bytes;
    uint32_t max_external_allocation_bytes;
    /* Hardware/profile limit for one optional animated pack.  This is a
     * renderer capability, not a business policy: a constrained display can
     * request fewer keyframes while presenting the identical selected pet. */
    uint32_t max_frame_count;
} device_display_pet_asset_install_budget_t;

bool device_display_get_pet_asset_install_budget(
    uint32_t source_width, uint32_t source_height, uint32_t frame_count,
    device_display_pet_asset_install_budget_t *out_budget);

/* Optional flash writes (such as a pet-preview cache) are capability-gated
 * separately from required durable storage. This keeps decorative work from
 * destabilizing a board whose flash/PSRAM cache sharing has stricter timing. */
bool device_storage_allows_optional_flash_work(void);

/*
 * Read-only boot/lifecycle diagnostics.  This is deliberately a small
 * observation API, rather than a second application state machine: no caller
 * can use it to grant gateway access or revive a failed service.
 */
#define DEVICE_RUNTIME_ABI_VERSION 1u

typedef enum {
    DEVICE_RUNTIME_PHASE_BOOTING = 0,
    DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
    DEVICE_RUNTIME_PHASE_IDENTITY_READY,
    DEVICE_RUNTIME_PHASE_STORAGE_READY,
    DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
    DEVICE_RUNTIME_PHASE_LOCAL_READY,
    DEVICE_RUNTIME_PHASE_DEGRADED,
} device_runtime_phase_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    device_runtime_phase_t phase;
    device_runtime_phase_t first_failure_phase;
    device_status_t first_failure_status;
    bool local_services_allowed;
} device_runtime_snapshot_t;

/* Returns an immutable-by-value view of the current startup state. */
bool device_runtime_get_snapshot(device_runtime_snapshot_t *out_snapshot);

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
    /* A board can provide normalized acceleration and angular-rate samples.
     * This advertises sensor hardware only; it does not claim a calibrated
     * fall-detection product feature. */
    DEVICE_CAPABILITY_MOTION_SENSOR        = 1u << 13,
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
     DEVICE_CAPABILITY_ROUND_DISPLAY | DEVICE_CAPABILITY_MOTION_SENSOR)

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
 * Hardware-neutral inertial sample.  Values use fixed engineering units so
 * application services never need a sensor model, I2C address, register map,
 * configured full scale or floating point conversion.
 *
 * `timestamp_us` is the local monotonic time at which the board read the
 * sample.  It is suitable for interval/state-machine work but not wall-clock
 * correlation.  A board without a motion sensor returns UNAVAILABLE.
 */
#define DEVICE_MOTION_SAMPLE_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint64_t timestamp_us;
    int32_t acceleration_mg_x;
    int32_t acceleration_mg_y;
    int32_t acceleration_mg_z;
    int32_t angular_rate_mdps_x;
    int32_t angular_rate_mdps_y;
    int32_t angular_rate_mdps_z;
} device_motion_sample_t;

/* Reads one fresh, normalized inertial sample.  It has no policy side effect:
 * fall classification, alerting and wake behavior belong to a future shared
 * service above this boundary. */
device_status_t device_motion_get_sample(device_motion_sample_t *out_sample);

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
/* On DEVICE_STATUS_OK the caller owns the opaque capture payload and must
 * release it through device_audio_release_captured_wav(), never free(). */
void device_audio_release_captured_wav(uint8_t *wav);
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
/* Internal lifecycle callers use the remaining deadline of their composition
 * root. Regular interaction uses device_wake_word_stop(). */
device_status_t device_wake_word_stop_with_timeout(uint32_t timeout_ms);
void device_wake_word_pause(bool paused);

// Absolute time on the device's monotonic millisecond clock.  A value of zero
// means "no deadline"; it is never a wall-clock timestamp.
typedef uint64_t device_deadline_ms_t;

/* A lease is a business-level assertion that the caller has foreground work
 * which must remain visible.  It applies to DISPLAY_OFF only: it is not a
 * hardware wakelock and does not claim LIGHT/DEEP_SLEEP support.  Leases are
 * opaque and must be released by their owning domain on every terminal path. */
typedef uint32_t device_power_lease_t;
#define DEVICE_POWER_LEASE_INVALID ((device_power_lease_t)0)

typedef enum {
    DEVICE_POWER_LEASE_OWNER_NONE = 0,
    DEVICE_POWER_LEASE_OWNER_ALARM,
    DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION,
    DEVICE_POWER_LEASE_OWNER_MEETING_RECORDING,
    DEVICE_POWER_LEASE_OWNER_AUDIO_PLAYBACK,
    /* A local suspected-fall confirmation window owns the presentation until
     * it is cancelled or expires.  The classifier itself remains a domain
     * service and never knows a board's panel or input implementation. */
    DEVICE_POWER_LEASE_OWNER_FALL_DETECTION,
    /* Captive portal remains a foreground user flow until configuration is
     * submitted and the deliberate restart completes. */
    DEVICE_POWER_LEASE_OWNER_PROVISIONING,
    DEVICE_POWER_LEASE_OWNER_COUNT,
} device_power_lease_owner_t;

typedef struct {
    bool initialized;
    uint8_t active_count;
    uint32_t owner_mask;
} device_power_lease_snapshot_t;

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
/* Composition-root lifecycle hook. Shared business code must not call board
 * power internals directly. */
device_status_t device_power_deinit(uint32_t timeout_ms);

/* Acquires/releases a shared foreground lease.  The Power Service refuses a
 * pending DISPLAY_OFF commit while at least one valid lease is active. */
device_status_t device_power_lease_acquire(device_power_lease_owner_t owner,
                                           device_power_lease_t *out_lease);
void device_power_lease_release(device_power_lease_t lease);
bool device_power_lease_get_snapshot(device_power_lease_snapshot_t *out_snapshot);

/* Arms/cancels the application-owned idle deadline for DISPLAY_OFF. A zero
 * delay is invalid; callers must cancel before publishing foreground work. */
device_status_t device_power_schedule_display_off(uint32_t idle_after_ms);
void device_power_cancel_display_off(void);

/* Consumes a local physical wake only when the panel was off. It deliberately
 * preserves each adapter's existing first-contact semantics. */
bool device_power_wake_display_from_user(void);

/* Restores a DISPLAY_OFF panel for an approved domain deadline.  This does
 * not impersonate touch/button input and is not a LIGHT/DEEP_SLEEP wake API. */
bool device_power_wake_display_from_schedule(void);
/* Restores a DISPLAY_OFF panel for a remote management action such as a
 * non-zero GUI brightness update.  This is not a physical input wake and
 * must not start voice capture or alter manual-wake scheduling policy. */
bool device_power_wake_display_from_remote_control(void);

/* Returns the Power Service's serialized observation of the panel/backlight
 * state and any pending display-off deadline. */
bool device_power_get_snapshot(device_power_snapshot_t *out_snapshot);

/* Reads the latest board-owned power telemetry snapshot without exposing ADC
 * units, divider ratios, charge GPIO polarity, or sampling implementation. */
bool device_power_get_telemetry(device_power_telemetry_t *out_telemetry);

/* Battery policy is shared business admission based on normalized telemetry,
 * never on ADC/GPIO details.  `telemetry_available=false` means there is no
 * calibrated signal; it is intentionally not treated as zero battery. */
#define DEVICE_BATTERY_POLICY_ABI_VERSION 1u
typedef enum {
    DEVICE_BATTERY_POLICY_NORMAL = 0,
    DEVICE_BATTERY_POLICY_CONSERVE,
    DEVICE_BATTERY_POLICY_PROTECT,
} device_battery_policy_level_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool telemetry_available;
    bool charging;
    uint8_t level_percent;
    device_battery_policy_level_t level;
    bool optional_work_allowed;
    bool high_power_work_allowed;
} device_battery_policy_snapshot_t;

bool device_battery_policy_get_snapshot(device_battery_policy_snapshot_t *out_snapshot);
bool device_battery_policy_allows_optional_work(void);
bool device_battery_policy_allows_high_power_work(void);

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
bool device_connectivity_initialize(void);
/* Stops the hardware-neutral Connectivity Service state.  It neither stops
 * Wi-Fi/SoftAP/DHCP/DNS nor deinitializes ESP-NETIF, SNTP, or a modem; those
 * physical resources remain the Connectivity composition root's transaction. */
device_status_t device_connectivity_deinit(uint32_t timeout_ms);
/* Wi-Fi driver owners must use one non-zero attempt epoch for every station
 * configuration/connect sequence, then wait on that exact epoch.  These are
 * lifecycle/adapter calls, not business connectivity policy. */
uint32_t device_connectivity_begin_wifi_attempt(const char *network_id);
bool device_connectivity_wait_wifi_attempt(uint32_t attempt_epoch,
                                           uint32_t timeout_ms);
bool device_connectivity_observe_wifi_disconnected(const char *network_id);
bool device_connectivity_observe_wifi_got_ip(const char *connected_network_id);
void device_connectivity_set_wifi_ready(bool ready);
void device_connectivity_set_cellular_ready(bool ready);
bool device_connectivity_is_active_uplink_ready(void);
bool device_connectivity_get_snapshot(device_connectivity_snapshot_t *out_snapshot);

/* Logical configuration-session state used by shared application workers.
 * These calls neither configure nor stop Wi-Fi, SoftAP, DHCP, DNS or HTTP;
 * their physical lifecycle remains a separate Connectivity/Provisioning
 * responsibility. `pairing_recovery` means the existing uplink is retained
 * and the portal is collecting only a replacement Hub pairing code. */
void device_connectivity_begin_provisioning(bool pairing_recovery);
void device_connectivity_end_provisioning(void);
bool device_connectivity_is_provisioning_active(void);
bool device_connectivity_is_pairing_recovery_provisioning(void);

/* Performs the selected profile's bounded physical preparation for its
 * cellular transport (for example modem guard/power sequencing).  It neither
 * starts a modem nor applies gateway policy; those remain transport/domain
 * responsibilities. Profiles without cellular hardware report UNAVAILABLE. */
device_status_t device_connectivity_prepare_cellular_transport(void);

/* Starts or probes the selected profile's cellular transport. Board-specific
 * UART pins, APN and modem implementation stay below this API; callers only
 * receive the stable Device status/readiness contract. */
device_status_t device_connectivity_start_cellular_transport(uint32_t timeout_ms);
/* Establishes the selected cellular uplink and publishes readiness through the
 * hardware-neutral Connectivity Service. */
device_status_t device_connectivity_establish_cellular_transport(uint32_t timeout_ms);
bool device_connectivity_is_cellular_transport_ready(void);

/* Stops new cellular transport/start admission and transport-owned recovery
 * coordination. It does not promise ML307/UART deinitialization or cancellation
 * of arbitrary in-flight HTTP borrowers. */
device_status_t device_connectivity_quiesce_cellular_transport(uint32_t timeout_ms);

/* Cellular request parameters are transport-neutral. The caller owns all
 * buffers and keeps them valid until the synchronous call returns; adapters
 * must not retain them. Wi-Fi stays on the existing shared HTTP client. */
typedef device_status_t (*device_connectivity_body_reader_t)(
    void *context, void *buffer, uint32_t requested, uint32_t *read_bytes);

typedef struct {
    const char *method;
    const char *url;
    const char *content_type;
    const char *authorization;
    const char *extra_header_name;
    const char *extra_header_value;
    const void *body;
    uint32_t body_len;
    char *response;
    uint32_t response_capacity;
    uint32_t *response_len;
    int *status_code;
    bool *truncated;
    uint32_t timeout_ms;
    /* Optional logical owner of this synchronous request.  The pointer is
     * compared only while the caller keeps it alive; it is never retained
     * after the request returns.  A worker that is being stopped can use the
     * matching cancellation API to interrupt its own cellular request without
     * knowing the modem implementation. */
    const void *cancellation_owner;
    bool foreground;
} device_connectivity_http_request_t;

typedef struct {
    device_connectivity_http_request_t request;
    device_connectivity_body_reader_t body_reader;
    void *body_reader_context;
    void *stream_buffer;
    uint32_t stream_buffer_size;
} device_connectivity_stream_request_t;

device_status_t device_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request);
device_status_t device_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);

/* Best-effort cancellation of the active profile's cellular foreground
 * request. This is intentionally a no-op on Wi-Fi-only profiles and does
 * not cancel the shared Wi-Fi HTTP client. */
bool device_connectivity_cancel_cellular_foreground_request(void);
/* Best-effort cancellation for the request currently owned by `owner`.
 * This is intentionally logical-worker based rather than modem based, so a
 * meeting/upload, a future provisioning worker, and a foreground command can
 * share the same HAL contract without exposing cellular handles above it. */
bool device_connectivity_cancel_cellular_requests_for_owner(const void *owner);

/* Profile-owned selection/persistence is normalized here so business startup
 * does not inspect a board Kconfig symbol or vendor NVS namespace. A Wi-Fi-
 * only profile restores Wi-Fi and reports no toggle. */
void device_connectivity_restore_selected_uplink(void);
bool device_connectivity_apply_startup_transport_toggle(uint32_t window_ms);

/* Gives a selected physical transport one narrowly scoped chance to adapt a
 * configured Gateway origin for a documented protocol limitation. The caller
 * keeps URL storage and all gateway/business semantics. */
void device_connectivity_adapt_gateway_url(char *gateway_url,
                                           uint32_t gateway_url_capacity);

/*
 * Display Device API.  The shared UI state machine publishes semantic scenes
 * and display state through this boundary; it never selects a panel driver,
 * framebuffer format, controller RAM offset, round-screen clipping rule, or
 * a physical backlight.  The selected profile adapter remains responsible
 * for synchronously copying any caller-owned payload that it needs after the
 * call returns.
 */
void device_display_set_command_lock(bool locked);
/* Applies a normalized 0..100 display-brightness level received from MaClaw.
 * 0 turns the backlight off while the system keeps running. */
device_status_t device_display_set_brightness(uint8_t percent);
void device_display_show_startup(void);
void device_display_set_pet_state(const char *state);
void device_display_set_command_stage(const char *stage);
void device_display_set_pet_profile(const char *skin, bool motion_enabled);
device_status_t device_display_set_pet_asset(const uint8_t *const *frames,
                                             uint32_t frame_count,
                                             uint32_t width, uint32_t height,
                                             uint32_t frame_ms);
device_status_t device_display_set_pet_asset_consuming(uint8_t **frames,
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
/* Borrowed 72-byte 24x24 bitmap. The synchronous Display Service copies it;
 * callers may reuse or release the storage after this call returns. */
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

/*
 * Versioned input envelope delivered across the Device API boundary.
 *
 * `generation` identifies one Input Service lifetime, while `sequence` is
 * monotonic only within that lifetime. Together they are a diagnostic and
 * correlation identity, not a persisted identifier. A caller must not accept
 * an event from an earlier generation after Input Service has stopped and
 * restarted. The timestamp is the local monotonic microsecond clock. Board
 * adapters never construct this value: they publish only normalized
 * action/source pairs, so touch coordinates, GPIO numbers, debounce state
 * and controller details cannot leak into business policy.
 */
#define DEVICE_INPUT_EVENT_ABI_VERSION 2u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t generation;
    uint32_t sequence;
    uint64_t timestamp_us;
    device_input_action_t action;
    device_input_source_t source;
} device_input_event_t;

typedef void (*device_input_cb_t)(const device_input_event_t *event,
                                  void *context);

/*
 * Starts the shared input boundary.  Board adapters publish normalized input
 * values here; a single application consumer receives versioned envelopes
 * from a bounded task-owned queue.  The callback never executes in a
 * GPIO/touch scan task, and the event pointer is valid only for the callback
 * duration.
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

/* Arms or disarms the short-lived command-cancellation input policy.  The
 * application owns when a command may be cancelled; the selected Input HAL
 * owns how its local touch/key gesture is recognized.  This deliberately does
 * not describe a display surface, controller, debounce interval, or gesture
 * timing. */
void device_input_set_command_cancel_enabled(bool enabled);

// Returns whether a physical source is the profile's normal local control for
// the shared primary intent.  It deliberately hides whether that control is a
// touch surface, button, rotary input, or future accessibility switch.
bool device_input_is_primary_interaction_source(device_input_source_t source);

// A short, profile-local noun for user-facing prompts (for example "激活键"
// or "屏幕"). The caller owns the surrounding workflow text; this accessor
// supplies no board ID, GPIO, or gesture policy.
const char *device_input_primary_interaction_label(void);
