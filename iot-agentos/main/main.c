#include <stdio.h>
#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include "cJSON.h"
#include "esp_http_client.h"

#include "esp_heap_caps.h"
#include "esp_log.h"
#include "mbedtls/base64.h"
#include "mbedtls/platform_util.h"
#include "esp_random.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/inet.h"
#include "lwip/sockets.h"
#include "psa/crypto.h"
#include "qrcode.h"

#include "app_intent_service.h"
#include "device_api.h"
#include "device_tool_registry.h"
#include "app_ui.h"
#include "alarm_manager.h"
#include "fall_detection_service.h"
#include "display_service.h"
#include "mp3_player.h"
#include "firmware_identity.h"
#include "lifecycle_service.h"
#include "operation_context.h"
#include "sleep_schedule_service.h"
#include "update_service.h"
#include "wake_deadline_service.h"
#include "resource_pressure_service.h"
#include "battery_policy_service.h"
#include "persistence_service.h"
#include "power_service.h"
#include "provisioning_failure_injection.h"
#include "task_registry.h"
#include "storage_service.h"
#include "pet_asset_cache_storage.h"
#include "weather_cache_service.h"
#include "meeting_recovery_service.h"
#include "configuration_service.h"
#include "configuration_reconcile_service.h"
#include "connectivity_service.h"
#include "services/command_service.h"
#include "services/reply_service.h"
#include "services/meeting_service.h"
#include "services/interaction_service.h"
#include "services/foreground_coordinator.h"
#include "services/gateway_dispatcher.h"
#include "services/gateway_lifecycle_service.h"
#include "services/gateway_transport.h"
#include "services/cellular_recovery_service.h"
#include "services/clock_sync_service.h"
#include "services/ambient_service.h"
#include "services/pet_asset_service.h"
#include "services/pet_cache_service.h"
#include "services/startup_pet_retry_service.h"
#include "services/startup_pet_worker_service.h"
#include "services/audio_arbitration_service.h"
#include "services/provisioning_service.h"
#include "services/provisioning_network_owner.h"
#include "services/connectivity_network_root_owner.h"
#include "services/connectivity_wifi_driver_owner.h"
#include "services/safe_mode_coordinator.h"
#include "presentation/input_binding.h"
#include "presentation/scene_presenter.h"
#include "platform_lifecycle.h"
#include "platform_nvs.h"
#include "platform_bootstrap.h"

#define WIFI_CONNECT_TIMEOUT_MS 20000
// 多热点逐个尝试时每个候选的连接超时，避免 5 个候选把启动拖得过长。
#define WIFI_CANDIDATE_CONNECT_TIMEOUT_MS 10000
#define STARTUP_TRANSPORT_SELECTOR_WINDOW_MS 1800
/* A failed cold start must reach its degraded diagnostics state predictably.
 * Every rollback child consumes this one deadline; it is not a fresh wait per
 * worker/service.  The HTTP server's own stop API remains a documented
 * unbounded ESP-IDF boundary, but all caller-controlled joins below share it. */
#define STARTUP_ROLLBACK_TIMEOUT_MS 6000u
/* SAFE_MODE is a bounded, local-only recovery transaction.  It is entered
 * only after the composition root has proven the minimum alarm dependencies
 * (Persistence, Display/App UI, Input, Power and Wake Deadline) are alive. */
#define SAFE_MODE_ENTRY_TIMEOUT_MS 5000u
#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
#define MEETING_RESPONSE_CAPACITY 2048
/* The unified tool registry makes the handshake descriptor larger than the
 * former 4 KiB stack buffer.  Rejecting it before the HTTP request leaves a
 * paired device permanently on its boot surface, so keep an explicit bounded
 * request capacity for this control-plane payload. */
#define HANDSHAKE_REQUEST_CAPACITY 8192
#define HARDWARE_AUDIO_RESPONSE_CAPACITY (512 * 1024 + 1)
#define RESPONSE_IMAGE_MAX_DIMENSION 64
#define RESPONSE_IMAGE_MAX_BYTES (RESPONSE_IMAGE_MAX_DIMENSION * RESPONSE_IMAGE_MAX_DIMENSION * 2)
#define RESPONSE_IMAGE_MIME "application/vnd.maclaw.rgb565be"
#define URL_CAPACITY 256
#define WIFI_VALUE_CAPACITY 65
#define WIFI_SSID_MAX_LEN 32
#define WIFI_ENTERPRISE_VALUE_CAPACITY 128
#define WIFI_EAP_MODE_CAPACITY 12
#define PAIR_CODE_CAPACITY 7
#define DEVICE_ID_CAPACITY 40
// Once MultiNet is listening, give the optional boot greeting only a short
// grace period. Hardware/profile messages may precede it in the outgoing
// queue; they must not keep activation input blocked for most of the boot.
#define STARTUP_WELCOME_TIMEOUT_MS 2500
// Rich text can carry dozens of 24x24 dynamic glyph bitmaps. Field captures
// reached 96 KiB for one (limit=1) item, so size this PSRAM-backed buffer with
// enough headroom to keep the outgoing cursor moving without burdening the
// scarce internal heap used by Wi-Fi/TLS and ESP-SR.
#define VOICE_UPLOAD_RETRY_COUNT 3
#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define SETUP_SCAN_MAX_APS 24
#define SETUP_SSID_OPTIONS_CAPACITY 6144
#define SETUP_SSID_CHOICES_CAPACITY (SETUP_SCAN_MAX_APS * WIFI_VALUE_CAPACITY)
#define PET_ASSET_MAX_FRAMES PET_ASSET_SERVICE_MAX_FRAMES
#define PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS 3
#define PET_ASSET_STARTUP_RETRY_DELAY_MS 3000
#define CONFIGURATION_RECONCILE_AUTHORITY_GATEWAY_CAPABILITY 1u

_Static_assert(URL_CAPACITY == PET_ASSET_SERVICE_URL_CAPACITY,
               "legacy gateway URL capacity must match the pet asset service contract");

static const char *TAG = "maclaw_client";
static char s_boot_session_id[33];
static char s_wifi_ssid[WIFI_VALUE_CAPACITY];
static char s_wifi_password[WIFI_VALUE_CAPACITY];
// 已存个人热点列表（NVS v3），启动时按信号强度自动选网；企业热点不进列表。
static configuration_wifi_network_t s_wifi_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];
static uint8_t s_wifi_network_count;
static char s_wifi_security[WIFI_EAP_MODE_CAPACITY] = "personal";
static char s_wifi_eap_method[WIFI_EAP_MODE_CAPACITY] = "peap";
static char s_wifi_identity[WIFI_ENTERPRISE_VALUE_CAPACITY];
static char s_wifi_username[WIFI_ENTERPRISE_VALUE_CAPACITY];
static char s_wifi_ttls_phase2[WIFI_EAP_MODE_CAPACITY] = "mschapv2";
static char s_wifi_ca_mode[WIFI_EAP_MODE_CAPACITY] = "system";
static char s_wifi_server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY];
/* True only for the boot that is attempting a persisted provisioning
 * candidate.  It prevents multi-network convenience fallback from silently
 * connecting the confirmed network and falsely treating that as candidate
 * Wi-Fi validation. */
static bool s_boot_provisioning_staged;
static bool s_startup_ui_initialized;
// Bread's first TLS certificate verification is cache/PSRAM intensive. Its
// alarm scheduler is deliberately initialized after that transaction, not in
// parallel with it; see ensure_alarm_manager_started().
static bool s_alarm_manager_started;
/* SAFE_MODE never re-opens this boot's ordinary interaction/Gateway admission.
 * Input Binding consults this value after its alarm-dismiss path, so a local
 * physical control remains useful for alarms but cannot start voice, meeting,
 * pairing, provisioning or configuration work. */
static volatile bool s_safe_mode_active;
static safe_mode_coordinator_t s_safe_mode_coordinator;
// Radio/IP callbacks can run before app_main() has finished the stability-
// sensitive startup boundary (Wi-Fi driver, clock and alarm scheduler).  They
// may only launch TLS/pairing after app_main explicitly opens this gate.
static volatile bool s_gateway_startup_allowed;
static volatile bool s_wake_restart_scheduled;
static volatile bool s_wake_restart_after_startup;
static TaskHandle_t s_wake_restart_task;
static SemaphoreHandle_t s_wake_restart_start_gate;
static SemaphoreHandle_t s_wake_restart_stopped;
static bool s_wake_restart_stop_requested;
static bool s_wake_restart_admission_open;
/* The offline-wake retry coordinator is a composition-root worker.  Audio
 * Service fences its eventual recognizer start, but this extra marker closes
 * the coordinator's create window and preserves a pre-existing retry across a
 * failed future System Sleep transaction. */
static bool s_wake_restart_system_sleep_preparing;
static bool s_wake_restart_system_sleep_was_running;
static bool s_wake_restart_system_sleep_was_admitted;
/* A timed-out cooperative join leaves the retiring retry worker responsible
 * for releasing its immutable Registry identity.  ABORT must not schedule a
 * replacement against its shared completion semaphore before that point. */
static bool s_wake_restart_system_sleep_restart_pending;
static bool s_wake_restart_retiring;
/* Worker completion does not prove that its immutable Task Registry identity
 * was retired. Keep the result so a later stop/ABORT cannot create a second
 * generation against an old still-visible entry. */
static esp_err_t s_wake_restart_exit_status = ESP_OK;
static bool s_wake_restart_registry_retirement_failed;
/* Fallback only when the durable force-setup request cannot be committed.
 * It still changes radio/portal state, so it is a normal Connectivity-owned
 * worker rather than an untracked fire-and-forget task. */
static TaskHandle_t s_deferred_setup_task;
static SemaphoreHandle_t s_deferred_setup_start_gate;
static SemaphoreHandle_t s_deferred_setup_stopped;
static bool s_deferred_setup_stop_requested;
static bool s_deferred_setup_starting;
/* The delayed portal coordinator is a Connectivity-root producer, rather
 * than part of the portal generation itself.  Provisioning fences the latter,
 * but future System Sleep must also prevent this waiter from being created or
 * reaching its portal-start side effect after PREPARE has reported success. */
static bool s_deferred_setup_admission_open;
static bool s_deferred_setup_system_sleep_preparing;
static bool s_deferred_setup_system_sleep_was_running;
static bool s_deferred_setup_system_sleep_was_admitted;
/* A bounded stop may time out while the old coordinator is publishing its
 * completion.  ABORT leaves this marker for that exact old task; it restarts
 * only after unregistering its old registry identity. */
static bool s_deferred_setup_system_sleep_restart_pending;
static bool s_deferred_setup_retiring;
static esp_err_t s_deferred_setup_exit_status = ESP_OK;
static bool s_deferred_setup_registry_retirement_failed;
static SemaphoreHandle_t s_startup_welcome_done;
static volatile bool s_startup_welcome_gate_active;
static volatile bool s_startup_welcome_timed_out;
// Playback completion and Hub acknowledgement are separate transactions. If
// the ACK request is interrupted after the speaker has already finished, Hub
// legitimately redelivers the same queue entry. Remember that this boot's
// greeting has been consumed so the retry only repairs the ACK and can never
// make the device speak it a second time.
static volatile bool s_startup_welcome_consumed;
static volatile bool s_startup_sequence_complete;
static bool s_handshake_startup_welcome_queued;
static unsigned s_startup_pet_retry_count;
/* System sleep is not enabled in production yet.  These flags only make the
 * optional startup-pet domain a truthful PREPARE participant: it can be
 * stopped and then resumed after an aborted transaction without losing the
 * work that existed before PREPARE. */
static bool s_startup_pet_system_sleep_preparing;
static bool s_startup_pet_system_sleep_was_pending;
static bool s_startup_pet_system_sleep_was_preempted_by_audio;
/* A timed-out PREPARE may observe the worker only after it has signalled its
 * completion, but before it has removed its immutable Task Registry entry.
 * Defer ABORT's replacement until that retiring generation owns neither
 * identity nor completion state; otherwise the replacement could be stopped
 * through the old entry or consume the old completion token. */
// Startup pet artwork is intentionally optional.  Its independent worker may
// otherwise open a second TLS session while the poll worker receives a spoken
// reply, exhausting the small internal TLS heap on EchoEar.  Keep the priority
// state separate from the normal HTTP mutexes: it is only an admission gate
// for the optional asset worker, never an ownership token for a client handle.
static portMUX_TYPE s_media_priority_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_audio_media_download_active;
static SemaphoreHandle_t s_media_transfer_mutex;
// The outgoing worker can receive a server speech URL while the offline
// recognizer is resident.  ESP-SR's model allocations fragment the internal
// heap enough for mbedTLS/AES to fail even though the eventual audio body is
// kept in PSRAM.  This flag records a successful, temporary recognizer
// teardown so the poll worker can restore it only after the entire message
// transaction (including its ACK) has finished.
/* Large TLS media transfers share one internal-memory lease. A foreground
 * server-audio transaction may preempt an optional pet worker; only the final
 * lease holder may restore the offline recognizer. */
static uint32_t s_media_wake_memory_lease_count;
static bool s_server_audio_wake_memory_lease_active;

static void set_audio_media_download_active(bool active);
static bool audio_media_download_active(void);
static void cancel_optional_startup_pet_asset_for_audio(void);
static void apply_deferred_startup_pet_asset(void);
static void rearm_preempted_startup_pet_asset(void);
static device_status_t prepare_startup_pet_asset_system_sleep(uint32_t timeout_ms);
static void abort_startup_pet_asset_system_sleep_prepare(void);
static bool startup_pet_asset_stop_requested(void);
static esp_err_t apply_deferred_pet_asset(void);
static bool begin_server_audio_wake_memory_lease(const char *source);
static void begin_optional_media_wake_memory_lease(const char *source);
static bool finish_server_audio_wake_memory_lease(void);
static bool finish_optional_media_wake_memory_lease(void);
static void finish_optional_pet_asset_memory_lease(void);
static bool server_audio_wake_memory_lease_active(void);
static void startup_stop_local_workers(void);
typedef enum {
    /* SAFE_MODE coordinator could not be initialized, so no SAFE_MODE
     * quiescence/admission change occurred and ordinary rollback is still the
     * truthful cold-start cleanup. */
    STARTUP_SAFE_MODE_ENTRY_NOT_STARTED = 0,
    STARTUP_SAFE_MODE_ENTRY_ACTIVE,
    /* Admission was closed and the coordinator was entered; a later stage
     * failed and the partially retired graph must remain fail-closed. */
    STARTUP_SAFE_MODE_ENTRY_TERMINAL_FAILURE,
} startup_safe_mode_entry_result_t;

static startup_safe_mode_entry_result_t startup_enter_safe_mode(
    device_runtime_phase_t phase, device_status_t status, const char *reason);
static void startup_enter_safe_mode_terminal_failure(device_runtime_phase_t phase,
                                                     device_status_t status,
                                                     const char *reason);
static device_status_t safe_mode_quiesce_nonessential(void *context,
                                                       uint32_t timeout_ms);
static device_status_t safe_mode_initialize_clock_feedback(void *context,
                                                            uint32_t timeout_ms);
static device_status_t safe_mode_initialize_alarm(void *context,
                                                   uint32_t timeout_ms);
static device_status_t safe_mode_publish_diagnostic_surface(
    void *context, const safe_mode_entry_t *entry, uint32_t timeout_ms);
static uint64_t safe_mode_now_ms(void *context);
static uint32_t startup_rollback_remaining_timeout_ms(int64_t deadline_us);
static esp_err_t stop_output_volume_persist_worker(uint32_t timeout_ms);
static esp_err_t stop_output_volume_persist_registry_entry(void *context,
                                                            uint32_t timeout_ms);
static device_status_t prepare_output_volume_persist_system_sleep(uint32_t timeout_ms,
                                                                   void *context);
static void abort_output_volume_persist_system_sleep_prepare(void *context);
// Exactly one pet pack may own the renderer at a time.  A cold-start pack is
// deliberately cancellable, but once it starts touching the display its final
// install/cache sequence must not race an online pet_profile update.
static SemaphoreHandle_t s_pet_asset_apply_mutex;
// The outgoing long-poll worker deliberately has a PSRAM-backed stack so it
// can decode audio replies without consuming the small internal-RAM budget.
// Flash writes temporarily disable caches, however, and ESP-IDF requires the
// calling task's stack to remain accessible while that happens.  Never invoke
// NVS directly from that worker; route volume persistence through this small
// internal-stack worker instead.
/* This worker commits Configuration snapshots. The
 * resulting NVS/persistence call chain exceeds the former 4 KiB stack on
 * ESP32-S3, while a flash write can temporarily disable the PSRAM cache.
 * Keep both the larger stack and its allocation explicitly internal. */
#define OUTPUT_VOLUME_PERSIST_TASK_STACK_BYTES 8192u

typedef struct {
    unsigned percent;
    uint32_t screen_sleep_seconds;
    uint32_t generation;
    bool brightness;
    bool screen_sleep;
    /* A Hub display patch commits both user-visible display values in one
     * Configuration revision before the caller asks Display/Power to apply
     * them.  It remains a product policy, never a panel/GPIO operation. */
    bool display_policy;
    bool display_policy_has_brightness;
    bool display_policy_has_screen_sleep;
    bool output_volume_policy;
    bool stop;
    bool system_sleep_prepare;
    /* Gateway startup intentionally runs on a PSRAM stack.  Its successful
     * pairing path must still commit the one-time-code -> token transition to
     * NVS, and flash writes cannot execute while that stack is inaccessible.
     * Reuse this internal-stack persistence owner for that transaction. */
    bool gateway_token;
    /* The dispatcher receives this only after Gateway Transport has accepted
     * the authenticated Hub session. Preserve that provenance across the
     * internal-stack persistence hop instead of turning remote policy into an
     * indistinguishable local NVS write. */
    bool hub_authenticated;
    char token[CONFIGURATION_GATEWAY_TOKEN_CAPACITY];
} output_volume_persist_request_t;

typedef struct {
    esp_err_t result;
    uint32_t generation;
    uint64_t configuration_revision;
} output_volume_persist_reply_t;

static QueueHandle_t s_output_volume_persist_queue;
static QueueHandle_t s_output_volume_persist_reply_queue;
static SemaphoreHandle_t s_output_volume_persist_request_mutex;
static TaskHandle_t s_output_volume_persist_task_handle;
static SemaphoreHandle_t s_output_volume_persist_stopped;
static SemaphoreHandle_t s_output_volume_persist_system_sleep_quiesced;
static uint32_t s_output_volume_persist_generation;
static bool s_output_volume_persist_stop_requested;
/* A completed persistence operation is not equivalent to a retired Storage
 * Registry identity.  Keep the terminal result so a stop observer never
 * reports success after the worker failed to remove its immutable entry. */
static esp_err_t s_output_volume_persist_exit_status = ESP_OK;
static bool s_output_volume_persist_retiring;
static bool s_output_volume_persist_registry_retirement_failed;
/* The internal-stack persistence worker is a legacy composition-root owner,
 * not the shared Persistence Service.  Future System Sleep keeps the worker
 * alive but closes its request admission and joins the one serialized request
 * mutex, so no volume/brightness/token mutation can cross physical COMMIT. */
static bool s_output_volume_persist_system_sleep_preparing;
static unsigned s_configured_output_volume = 70;
static bool s_configured_output_volume_saved;
static uint8_t s_configured_display_brightness;
static bool s_configured_display_brightness_saved;
static uint32_t s_configured_screen_sleep_seconds;
static bool s_configured_screen_sleep_seconds_saved;
/* Last display policy revision successfully handed to the shared
 * Display/Power consumer. This is composition state only; Configuration's
 * durable revision remains authoritative after a failed external apply. */
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static void on_wake_word(void *arg);
static void on_fall_detection_event(fall_detection_event_t event, void *arg);
static void on_schedule_display_wake(device_status_t status, void *arg);

static bool configuration_reconcile_output_volume_applied(uint64_t revision,
                                                          uint8_t value) {
    configuration_reconcile_service_snapshot_t snapshot = {0};
    return configuration_reconcile_service_get_snapshot(&snapshot) &&
           snapshot.apply_state.desired_valid &&
           snapshot.apply_state.durable_revision == revision &&
           snapshot.apply_state.desired_output_volume == value &&
           snapshot.apply_state.output_volume.known &&
           snapshot.apply_state.output_volume.value == value &&
           snapshot.apply_state.output_volume.observation ==
               CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

static bool configuration_reconcile_display_brightness_applied(uint64_t revision,
                                                                uint8_t value) {
    configuration_reconcile_service_snapshot_t snapshot = {0};
    return configuration_reconcile_service_get_snapshot(&snapshot) &&
           snapshot.apply_state.desired_valid &&
           snapshot.apply_state.durable_revision == revision &&
           snapshot.apply_state.desired_display_brightness == value &&
           snapshot.apply_state.display_brightness.known &&
           snapshot.apply_state.display_brightness.value == value &&
           snapshot.apply_state.display_brightness.observation ==
               CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

static bool configuration_reconcile_screen_sleep_applied(uint64_t revision,
                                                           uint32_t value) {
    configuration_reconcile_service_snapshot_t snapshot = {0};
    return configuration_reconcile_service_get_snapshot(&snapshot) &&
           snapshot.apply_state.desired_valid &&
           snapshot.apply_state.durable_revision == revision &&
           snapshot.apply_state.screen_sleep_policy_required &&
           snapshot.apply_state.desired_screen_sleep_seconds == value &&
           snapshot.apply_state.screen_sleep_seconds.known &&
           snapshot.apply_state.screen_sleep_seconds.value == value &&
           snapshot.apply_state.screen_sleep_seconds.observation ==
               CONFIGURATION_APPLY_OBSERVATION_APPLIED;
}

/* Configuration accepts only this generic value contract. Gateway remains a
 * composition-root concern: no Gateway protocol/type leaks into Configuration
 * or any HAL/board adapter. */
static bool gateway_configuration_authorization_current(
    const configuration_reconcile_authorization_t *authorization, void *context) {
    (void)context;
    if (!authorization ||
        authorization->authority_kind !=
            CONFIGURATION_RECONCILE_AUTHORITY_GATEWAY_CAPABILITY) {
        return false;
    }
    const gateway_capability_lease_t lease = {
        .struct_size = sizeof(lease),
        .abi_version = GATEWAY_CAPABILITY_LEASE_ABI_VERSION,
        .required_capabilities = authorization->required_permissions,
        .generation = authorization->generation,
    };
    return gateway_transport_capability_lease_current(&lease);
}

static bool gateway_configuration_authorization_from_lease(
    const gateway_capability_lease_t *lease,
    configuration_reconcile_authorization_t *out_authorization) {
    if (!lease || !out_authorization ||
        lease->struct_size != sizeof(*lease) ||
        lease->abi_version != GATEWAY_CAPABILITY_LEASE_ABI_VERSION ||
        lease->generation == 0u || lease->required_capabilities == 0u) {
        return false;
    }
    *out_authorization = (configuration_reconcile_authorization_t){
        .struct_size = sizeof(*out_authorization),
        .abi_version = CONFIGURATION_RECONCILE_AUTHORIZATION_ABI_VERSION,
        .authority_kind = CONFIGURATION_RECONCILE_AUTHORITY_GATEWAY_CAPABILITY,
        .generation = lease->generation,
        .required_permissions = lease->required_capabilities,
    };
    return true;
}


static bool s_storage_mounted;

static void wifi_event(void *arg, const connectivity_wifi_driver_event_t *event);

/* Event callbacks execute on ESP-IDF's default loop.  Do not borrow a service
 * or UI state once lifecycle admission has closed.  The counter covers a
 * callback that was selected immediately before unregister: ESP-IDF marks its
 * handler unregistered, but its API does not offer a caller-bounded drain
 * guarantee. */
typedef gateway_transport_response_t http_response_t;

/* Transport delegates: the lane itself lives in Gateway Transport (A8 second
 * increment); these wrappers keep the remaining main.c callers unchanged. */
static void response_release(http_response_t *response) {
    gateway_transport_response_release(response);
}

static esp_err_t request_with_capacity(const char *method, const char *path, const char *content_type,
                                       const char *body, int body_len, size_t response_capacity,
                                       http_response_t *out) {
    return (esp_err_t)gateway_transport_request_with_capacity(
        method, path, content_type, body, body_len, (uint32_t)response_capacity, out);
}

static esp_err_t request(const char *method, const char *path, const char *content_type,
                         const char *body, int body_len, http_response_t *out) {
    return (esp_err_t)gateway_transport_request(method, path, content_type, body, body_len, out);
}

static void process_update_metadata(cJSON *update, bool defer_presentation);
static void publish_pending_update_reminder(void);
static device_status_t cancel_gateway_requests_for_system_sleep(uint32_t timeout_ms,
                                                                 void *context);
static device_status_t startup_status_from_esp_err(esp_err_t err);
static esp_err_t stop_network_core_transaction(uint32_t timeout_ms);
static esp_err_t stop_connectivity_root_transaction(uint32_t timeout_ms);
static esp_err_t send_text_event(const char *text, const char *reply_to);
static bool hardware_audio_url_allowed(const char *url);
static esp_err_t handle_client_tool_call(cJSON *item);
static const char *json_string(cJSON *root, const char *key);
static bool json_number(cJSON *root, const char *key, int *value);
static void schedule_wake_restart(void);
static esp_err_t stop_wake_restart_task(uint32_t timeout_ms);
static esp_err_t stop_wake_restart_registry_entry(void *context, uint32_t timeout_ms);
static device_status_t prepare_wake_restart_system_sleep(uint32_t timeout_ms);
static void abort_wake_restart_system_sleep_prepare(void);
static esp_err_t stop_deferred_setup_task(uint32_t timeout_ms);
static esp_err_t stop_deferred_setup_registry_entry(void *context, uint32_t timeout_ms);
static bool start_deferred_setup_task(void);
static device_status_t prepare_deferred_setup_system_sleep(uint32_t timeout_ms);
static void abort_deferred_setup_system_sleep_prepare(void);
static esp_err_t audio_wake_word_stop(void);
static esp_err_t audio_wake_word_stop_with_timeout(uint32_t timeout_ms);
static void pet(const char *state);
static void apply_deferred_startup_pet_asset(void);
static bool ensure_alarm_manager_started(void);
static bool start_cached_pet_restore_task(void);

/* The private physical root owns the ESP-IDF object ordering. These two
 * bridges preserve the composition root's policy ownership: Connectivity owns
 * callback admission and Clock Sync owns SNTP lifecycle. */
static device_status_t network_root_stop_callback_admission(void *context,
                                                            uint32_t timeout_ms) {
    (void)context;
    return connectivity_service_stop_wifi_event_callback_admission(timeout_ms);
}

static device_status_t network_root_stop_provisioning(void *context,
                                                       uint32_t timeout_ms) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    device_status_t status = provisioning_service_stop_restart(timeout_ms);
    if (status != DEVICE_STATUS_OK) return status;
    if (!provisioning_service_has_live_resources()) return DEVICE_STATUS_OK;
    const uint32_t remaining = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    return provisioning_service_stop_portal(remaining, false);
}

static bool network_root_provisioning_has_live_resources(void *context) {
    (void)context;
    return provisioning_service_has_live_resources();
}

static device_status_t network_root_stop_clock_sync(void *context,
                                                    uint32_t timeout_ms) {
    (void)context;
    return clock_sync_service_stop(timeout_ms);
}


static esp_err_t handle_client_tool_call(cJSON *item) {
    cJSON *call = cJSON_GetObjectItemCaseSensitive(item, "toolCall");
    const char *call_id = json_string(call, "id");
    const char *name = json_string(call, "name");
    const char *idempotency_key = json_string(call, "idempotencyKey");
    const char *conversation_id = json_string(item, "conversationId");
    cJSON *arguments = cJSON_GetObjectItemCaseSensitive(call, "arguments");
    if (!cJSON_IsObject(call) || !call_id || !name) return ESP_ERR_INVALID_ARG;
    const device_tool_definition_t *tool_definition = NULL;
    bool known_tool = device_tool_registry_find(name, &tool_definition);
    const bool requires_idempotency_key =
        device_tool_registry_requires_idempotency(tool_definition);
    bool missing_idempotency_key = !idempotency_key || !idempotency_key[0];
    bool invalid_arguments = arguments && !cJSON_IsObject(arguments);
    bool owned_arguments = false;
    if (!arguments) {
        arguments = cJSON_CreateObject();
        owned_arguments = true;
    }
    cJSON *result = NULL;
    char detail[128] = {0};
    esp_err_t execute_err;
    if (missing_idempotency_key && requires_idempotency_key) {
        snprintf(detail, sizeof(detail), "idempotencyKey is required");
        execute_err = ESP_ERR_INVALID_ARG;
    } else if (invalid_arguments) {
        snprintf(detail, sizeof(detail), "arguments must be an object");
        execute_err = ESP_ERR_INVALID_ARG;
    } else if (!arguments) {
        snprintf(detail, sizeof(detail), "cannot allocate arguments object");
        execute_err = ESP_ERR_NO_MEM;
    } else if (!known_tool) {
        snprintf(detail, sizeof(detail), "unsupported client tool: %s", name);
        execute_err = ESP_ERR_NOT_SUPPORTED;
    } else if (!device_tool_registry_is_ready(tool_definition)) {
        snprintf(detail, sizeof(detail), "client tool is temporarily unavailable");
        execute_err = ESP_ERR_INVALID_STATE;
    } else {
        execute_err = device_tool_registry_execute(tool_definition, arguments,
                                                    idempotency_key, &result,
                                                    detail, sizeof(detail));
    }
    if (owned_arguments) cJSON_Delete(arguments);
    ESP_LOGI(TAG, "client tool executed: name=%s call=%s status=%s",
             name, call_id, execute_err == ESP_OK ? "succeeded" : "failed");

    cJSON *body = cJSON_CreateObject();
    if (!body) {
        cJSON_Delete(result);
        return ESP_ERR_NO_MEM;
    }
    cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id());
    cJSON_AddStringToObject(body, "resultId", call_id);
    cJSON_AddStringToObject(body, "toolCallId", call_id);
    cJSON_AddStringToObject(body, "conversationId", conversation_id && conversation_id[0] ? conversation_id : "default");
    if (!missing_idempotency_key) cJSON_AddStringToObject(body, "idempotencyKey", idempotency_key);
    if (execute_err == ESP_OK) {
        cJSON_AddStringToObject(body, "status", "succeeded");
        cJSON_AddItemToObject(body, "result", result);
        result = NULL;
    } else {
        cJSON_AddStringToObject(body, "status", "failed");
        cJSON *error = cJSON_AddObjectToObject(body, "error");
        bool persistent_capacity_error = execute_err == ESP_ERR_NO_MEM &&
                                         (strstr(detail, "alarm capacity") != NULL ||
                                          strstr(detail, "persistent replay capacity") != NULL);
        const char *error_code = execute_err == ESP_ERR_NOT_SUPPORTED ? "unknown_tool" :
                                 execute_err == ESP_ERR_TIMEOUT ? "device_busy" :
                                 persistent_capacity_error ? "capacity_exhausted" :
                                 execute_err == ESP_ERR_NO_MEM ? "device_busy" :
                                 execute_err == ESP_ERR_INVALID_ARG ? "invalid_arguments" :
                                 "device_error";
        cJSON_AddStringToObject(error, "code", error_code);
        cJSON_AddStringToObject(error, "message", detail[0] ? detail : esp_err_to_name(execute_err));
        cJSON_AddBoolToObject(error, "retryable",
                              execute_err == ESP_ERR_TIMEOUT ||
                              (execute_err == ESP_ERR_NO_MEM && !persistent_capacity_error) ||
                              (execute_err != ESP_ERR_NOT_SUPPORTED &&
                               execute_err != ESP_ERR_INVALID_ARG &&
                               execute_err != ESP_ERR_NO_MEM));
    }
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    cJSON_Delete(result);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/tool-result", "application/json",
                            payload, strlen(payload), &response);
    free(payload);
    if (err == ESP_OK && response.status != 200 && response.status != 202 && response.status != 204) err = ESP_FAIL;
    ESP_LOGI(TAG, "client tool result delivered: name=%s call=%s http=%d err=%s",
             name, call_id, response.status, esp_err_to_name(err));
    response_release(&response);
    return err;
}

static void publish_pending_update_reminder(void) {
    char title[32] = {0};
    char detail[UPDATE_SERVICE_DETAIL_CAPACITY] = {0};
    if (update_service_take_pending_presentation(title, sizeof(title), detail, sizeof(detail))) {
        scene_presenter_publish_message(title, detail);
    }
}

static void process_update_metadata(cJSON *update, bool defer_presentation) {
    int64_t now_epoch = time(NULL);
    if (now_epoch < 1672531200) now_epoch = 0;
    if (update_service_apply_metadata(update, now_epoch, defer_presentation) && !defer_presentation) {
        publish_pending_update_reminder();
    }
}

/* Command Service host seam. Gateway Transport owns the HTTP lane and exposes
 * bounded, value-only cancellation. Command Service reaches it only through
 * this composition-time callback; Interaction orchestration remains behind
 * its typed service contract. */
static int32_t command_host_send_server_cancel(const char *reply_to) {
    return (int32_t)send_text_event("/cancel",
                                    reply_to && reply_to[0] ? reply_to : NULL);
}

static void command_host_cancel_foreground_http(void) {
    gateway_transport_cancel_foreground_request(1000);
}

static const command_service_host_t s_command_service_host = {
    .send_server_cancel = command_host_send_server_cancel,
    .cancel_foreground_http = command_host_cancel_foreground_http,
};

static void log_heap_snapshot(const char *stage) {
    size_t internal_free = heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    size_t internal_largest = heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    size_t psram_free = heap_caps_get_free_size(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    size_t psram_largest = heap_caps_get_largest_free_block(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    ESP_LOGI(TAG, "heap[%s] internal=%u/%u psram=%u/%u", stage ? stage : "?",
             (unsigned)internal_free, (unsigned)internal_largest,
             (unsigned)psram_free, (unsigned)psram_largest);
}

static void pet(const char *state) {
    ambient_service_apply_pet_state(state);
}

static esp_err_t download_audio(const char *url, uint8_t **out_audio, size_t *out_len) {
    if (!url || !url[0] || !out_audio || !out_len) return ESP_ERR_INVALID_ARG;
    *out_audio = NULL;
    *out_len = 0;
    // Unlike a local voice interaction, an outgoing reply is handled by the
    // poll task and historically kept MultiNet alive.  On EchoEar that leaves
    // no contiguous internal block for TLS AES.  Take the same explicit
    // memory lease used by the upload paths before opening the media TLS
    // connection; the caller restores wake after the response has been ACKed.
    (void)begin_server_audio_wake_memory_lease("server audio download");
    // A delivered voice reply is a foreground user outcome.  The cold-start
    // pet is decorative, so stop it before waiting for the media lane.  Its
    // worker observes this flag after the one already-running bounded request,
    // releases the lane, and cannot start another frame over the reply.
    cancel_optional_startup_pet_asset_for_audio();
    if (!s_media_transfer_mutex ||
        xSemaphoreTake(s_media_transfer_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    // Advertise priority before starting TLS so the optional pet worker cannot
    // begin another large transfer between the cursor parse and this request.
    set_audio_media_download_active(true);
    http_response_t response = {0};
    esp_err_t err = request_with_capacity("GET", url, NULL, NULL, 0,
                                          HARDWARE_AUDIO_RESPONSE_CAPACITY, &response);
    set_audio_media_download_active(false);
    xSemaphoreGive(s_media_transfer_mutex);
    if (err != ESP_OK || response.status != 200 || response.len < 2) {
        esp_err_t result = err;
        if (err == ESP_OK) {
            // A successful but empty/malformed media object, or a permanent
            // client/media-token HTTP rejection, cannot heal on another poll.
            // Classify it as invalid content so it is ACKed instead of pinning
            // the outgoing cursor forever. Server failures remain retryable.
            result = (response.status >= 400 && response.status < 500) ||
                     (response.status == 200 && response.len < 2)
                         ? ESP_ERR_INVALID_ARG
                         : ESP_FAIL;
        }
        response_release(&response);
        return result;
    }
    *out_audio = (uint8_t *)response.data;
    *out_len = response.len;
    response.data = NULL;
    response_release(&response);
    return ESP_OK;
}

typedef pet_asset_descriptor_t pet_asset_ref_t;

// Pet artwork is optional startup decoration. Keep its small descriptor here
// so the authenticated handshake can release TLS/JSON memory and initialize
// ESP-SR before any media download or SPIFFS write takes place.
// Written by the gateway poll task to pre-empt the optional cold-start pack,
// then observed by the startup worker between frame downloads/installs.
static volatile bool s_startup_pet_asset_pending;
static bool s_startup_pet_asset_present;
static pet_asset_ref_t *s_startup_pet_asset_ref;
/* The cold-start descriptor may arrive before capability health has reached
 * operational.  The actual asynchronous download captures this value only
 * when it is admitted, and validates it at every network/cache/display
 * boundary.  It is never an HTTP, task, JSON, or board handle. */
/* Set when a server-audio transaction preempts the deferred cold-start pet
 * install.  The audio memory-lease finish re-arms the install afterwards, so
 * a slow backhaul (Fangtang's 4G, where the welcome audio routinely arrives
 * inside the deferred pet window) no longer loses the standby pet for the
 * entire boot. */
static bool s_startup_pet_asset_preempted_by_audio;
static char s_startup_pet_asset_skin[32];
static char s_loaded_pet_asset_revision[40];
static int s_loaded_pet_asset_frame_count;
/* 在线宠物更新失败的有序重试会停住整个出站页游标（keep_cursor_for_retry），
 * 容量类失败（ESP_ERR_NO_MEM）永不成功时后面的远程播放等消息全部被堵死。
 * 记录同一消息 id 的连续失败次数，达到上限后按永久失败 ACK 放行队列。 */
static char s_pet_asset_retry_id[80];
static int s_pet_asset_retry_count;
#define PET_ASSET_RETRY_LIMIT 3

static void free_pet_asset_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES], size_t frame_count) {
    for (size_t i = 0; i < frame_count && i < PET_ASSET_MAX_FRAMES; ++i) {
        heap_caps_free(frames[i]);
    }
}

/* The display install deliberately consumes its verified HTTP sources while it
 * creates renderer-owned scaled copies. A persistent full animation needs a
 * separate short-lived source set: never cache first and make a slow SPIFFS
 * operation delay the visible install. Failure is intentionally non-fatal;
 * the in-memory animation remains the user-visible result. */
static bool clone_pet_asset_frames(const pet_asset_ref_t *ref,
                                   uint8_t *const source[PET_ASSET_MAX_FRAMES],
                                   uint8_t *copies[PET_ASSET_MAX_FRAMES]) {
    if (!ref || !source || !copies || ref->frame_count < 1 ||
        ref->frame_count > PET_ASSET_MAX_FRAMES) return false;
    size_t bytes = 0;
    if (!pet_asset_service_frame_bytes(ref->width, ref->height, &bytes)) return false;
    memset(copies, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
    for (int i = 0; i < ref->frame_count; ++i) {
        if (!source[i]) goto fail;
        copies[i] = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!copies[i]) copies[i] = malloc(bytes);
        if (!copies[i]) goto fail;
        memcpy(copies[i], source[i], bytes);
    }
    return true;
fail:
    free_pet_asset_frames(copies, PET_ASSET_MAX_FRAMES);
    memset(copies, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
    return false;
}

/* A full remote pack keeps every verified source frame until the renderer has
 * atomically accepted the pack. Reserve the aggregate peak up front, but
 * compare only the real one-shot allocation against heap largest: fragmented
 * PSRAM can safely hold a pack when no call asks it for one giant block. */
static bool pet_asset_capacity_available(const pet_asset_ref_t *ref) {
    if (!ref || ref->frame_count < 1) return false;
    device_display_pet_asset_install_budget_t install = {0};
    /* A display profile may decline decorative flash mutations when its panel
     * DMA and PSRAM share cache fabric. Do not reserve space for a cache that
     * HAL will never create; the verified in-memory pack remains usable. */
    /* A profile can support flash persistence while this boot's SPIFFS mount is
     * unavailable (for example, preserve-on-failure recovery of an existing
     * recording partition).  Persistence is decorative; do not reserve a
     * nonexistent cache or reject the verified in-memory animation because of
     * that independent storage fault. */
    if (!device_display_get_pet_asset_install_budget(
            (uint32_t)ref->width, (uint32_t)ref->height, (uint32_t)ref->frame_count,
            &install) ||
        install.struct_size != sizeof(install) ||
        install.abi_version != DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION) {
        return false;
    }
    /* The renderer reserves every replacement target before it can atomically
     * swap the pack.  `max_external_allocation_bytes` describes fragmentation
     * risk for one malloc(), while `total_external_bytes` is the simultaneous
     * retained target set.  Treating the latter as one frame under-admitted a
     * complete pack: the later allocation loop could fail after downloads had
     * already consumed their network/PSRAM budget.  Keep these dimensions
     * separate: aggregate free space protects the transaction peak, largest
     * block protects each individual allocation. */
    pet_asset_memory_requirements_t requirements = {0};
    if (!pet_asset_service_calculate_memory_requirements(
            ref, install.total_external_bytes, install.max_external_allocation_bytes,
            &requirements)) return false;
    if (!device_battery_policy_allows_optional_work()) {
        ESP_LOGW(TAG, "pet asset deferred: battery protection policy is active");
        return false;
    }
    /* The blanket NORMAL-level gate denies this pack whenever the wake
     * engine's internal-RAM working set has tripped the internal waterline
     * (EchoEar idles at ~9.7 KiB largest internal block), even though the
     * transaction itself needs no internal bytes: the download worker stops
     * the wake engine first via the optional-media lease, which releases
     * exactly that internal pressure.  Gate on the actual external/storage
     * requirements instead, using the pressure service's own waterlines. */
    device_resource_pressure_snapshot_t snapshot = {0};
    if (!device_resource_pressure_get_snapshot(&snapshot) ||
        snapshot.external_free_bytes < requirements.total_external_bytes ||
        snapshot.external_free_bytes - requirements.total_external_bytes < 512u * 1024u ||
        snapshot.external_largest_free_bytes < requirements.max_external_allocation_bytes) {
        ESP_LOGW(TAG, "pet asset deferred: insufficient shared optional capacity "
                      "(source_psram=%u install_psram=%u max_alloc=%u) "
                      "snapshot: level=%d ext_free=%lu ext_largest=%lu",
                 (unsigned)requirements.source_bytes, (unsigned)install.total_external_bytes,
                 (unsigned)requirements.max_external_allocation_bytes, (int)snapshot.level,
                 (unsigned long)snapshot.external_free_bytes,
                 (unsigned long)snapshot.external_largest_free_bytes);
        return false;
    }
    return true;
}

/* The Hub describes the complete authored animation.  A display profile may
 * retain fewer keyframes when its framebuffer geometry leaves less PSRAM for
 * optional artwork.  Keep that decision behind the display contract: shared
 * business code selects the same pet revision on every device and never names
 * a board, resolution, or memory size. */
static bool pet_asset_prepare_for_display(const pet_asset_ref_t *source,
                                          pet_asset_ref_t *out) {
    if (!source || !out || source->frame_count < 1) return false;
    device_display_pet_asset_install_budget_t install = {0};
    if (!device_display_get_pet_asset_install_budget(
            (uint32_t)source->width, (uint32_t)source->height,
            (uint32_t)source->frame_count, &install) ||
        install.struct_size != sizeof(install) ||
        install.abi_version != DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION ||
        install.max_frame_count == 0) {
        return false;
    }
    if (!pet_asset_service_limit_frame_count(source, install.max_frame_count, out)) {
        return false;
    }
    if ((uint32_t)source->frame_count > install.max_frame_count) {
        ESP_LOGI(TAG, "pet asset keyframes adapted by display HAL: %d -> %lu",
                 source->frame_count, (unsigned long)out->frame_count);
    }
    return true;
}

static void set_audio_media_download_active(bool active) {
    taskENTER_CRITICAL(&s_media_priority_lock);
    s_audio_media_download_active = active;
    taskEXIT_CRITICAL(&s_media_priority_lock);
}

static bool audio_media_download_active(void) {
    bool active;
    taskENTER_CRITICAL(&s_media_priority_lock);
    active = s_audio_media_download_active;
    taskEXIT_CRITICAL(&s_media_priority_lock);
    return active;
}

static bool begin_server_audio_wake_memory_lease(const char *source) {
    bool should_stop = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_server_audio_wake_memory_lease_active) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    s_server_audio_wake_memory_lease_active = true;
    should_stop = s_media_wake_memory_lease_count == 0;
    ++s_media_wake_memory_lease_count;
    taskEXIT_CRITICAL(&s_task_state_lock);

    // Abort an optional cold-start asset before waiting for model teardown.
    // Otherwise the asset worker can begin (or retain) a 192 KiB TLS body in
    // the gap, and the eventual wake restart races its PSRAM renderer copies.
    cancel_optional_startup_pet_asset_for_audio();
    if (!should_stop) return true;
    log_heap_snapshot("server-audio-before-wake-stop");
    esp_err_t wake_stop_err = audio_wake_word_stop();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before %s: %s",
                 source ? source : "server audio", esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("server-audio-after-wake-stop");
    return true;
}

static void begin_optional_media_wake_memory_lease(const char *source) {
    bool should_stop;
    taskENTER_CRITICAL(&s_task_state_lock);
    should_stop = s_media_wake_memory_lease_count == 0;
    ++s_media_wake_memory_lease_count;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!should_stop) return;

    // Decorative asset downloads are still large HTTPS transactions. They
    // must reserve internal TLS/AES memory before opening their first request,
    // rather than repeatedly failing while MultiNet is resident. If foreground
    // speech arrives it takes a second lease and owns the final restart.
    log_heap_snapshot("optional-media-before-wake-stop");
    esp_err_t wake_stop_err = audio_wake_word_stop();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before %s: %s",
                 source ? source : "optional media", esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("optional-media-after-wake-stop");
}

static bool finish_server_audio_wake_memory_lease(void) {
    bool final_owner = false;
    bool rearm_pet = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_server_audio_wake_memory_lease_active) {
        s_server_audio_wake_memory_lease_active = false;
        if (s_media_wake_memory_lease_count > 0) {
            --s_media_wake_memory_lease_count;
            final_owner = s_media_wake_memory_lease_count == 0;
        }
        if (s_startup_pet_asset_preempted_by_audio) {
            s_startup_pet_asset_preempted_by_audio = false;
            rearm_pet = true;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    // Outside the critical section: the re-arm may spawn the deferred pet
    // worker or arm its retry timer.
    if (rearm_pet) rearm_preempted_startup_pet_asset();
    return final_owner;
}

static bool finish_optional_media_wake_memory_lease(void) {
    bool final_owner = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_media_wake_memory_lease_count > 0) {
        --s_media_wake_memory_lease_count;
        final_owner = s_media_wake_memory_lease_count == 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    return final_owner;
}

/* Optional pet work must not permanently leave the wake recognizer stopped.
 * Its caller owns only the temporary memory lease; when this was the last
 * lease, re-use the normal deferred restart path so all hardware profiles
 * return to the same ready/idle behavior. */
static void finish_optional_pet_asset_memory_lease(void) {
    if (finish_optional_media_wake_memory_lease()) schedule_wake_restart();
}

static bool server_audio_wake_memory_lease_active(void) {
    bool active;
    taskENTER_CRITICAL(&s_task_state_lock);
    active = s_server_audio_wake_memory_lease_active;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return active;
}

static void cancel_optional_startup_pet_asset_for_audio(void) {
    bool preempted = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_pet_asset_pending &&
        !startup_pet_worker_service_stop_requested()) {
        s_startup_pet_asset_pending = false;
        // Deferral, not abandonment: the audio lease finish re-arms the install.
        s_startup_pet_asset_preempted_by_audio = true;
        preempted = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (preempted) ESP_LOGI(TAG, "startup pet asset preempted by server audio");
}

static bool startup_pet_asset_stop_requested(void) {
    return startup_pet_worker_service_stop_requested();
}

/* The decorative startup pet has its own HTTPS worker and retained retry
 * timer.  It is deliberately coordinated here, at the composition root,
 * because neither Connectivity Service nor Power Service may acquire renderer,
 * HTTP-client or FreeRTOS ownership.  This does not enable MCU sleep: it only
 * supplies a reversible participant for the future transaction. */
static device_status_t prepare_startup_pet_asset_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;

    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_pet_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_startup_pet_system_sleep_preparing = true;
    s_startup_pet_system_sleep_was_pending = s_startup_pet_asset_pending;
    s_startup_pet_system_sleep_was_preempted_by_audio =
        s_startup_pet_asset_preempted_by_audio;
    taskEXIT_CRITICAL(&s_task_state_lock);

    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    esp_err_t stop_err = remaining_ms
                             ? device_status_to_platform_error(
                                   startup_pet_worker_service_prepare_system_sleep(remaining_ms))
                             : ESP_ERR_TIMEOUT;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (stop_err == ESP_OK) {
        stop_err = remaining_ms
                       ? device_status_to_platform_error(
                             startup_pet_retry_service_prepare_system_sleep(remaining_ms))
                       : ESP_ERR_TIMEOUT;
        remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    }
    if (stop_err == ESP_OK) {
        stop_err = remaining_ms
                       ? device_status_to_platform_error(
                             pet_cache_service_prepare_system_sleep(remaining_ms))
                       : ESP_ERR_TIMEOUT;
    }
    if (stop_err == ESP_OK) return DEVICE_STATUS_OK;

    /* Keep optional pet admission closed after a timeout. The common
     * Connectivity/Power reverse rollback is the sole owner permitted to
     * restore its recorded retry/download generation. */
    return startup_status_from_esp_err(stop_err);
}

static void abort_startup_pet_asset_system_sleep_prepare(void) {
    bool rearm_after_completed_audio = false;
    bool abort_retry_prepare = false;
    bool abort_pet_cache_prepare = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_pet_system_sleep_preparing) {
        s_startup_pet_system_sleep_preparing = false;
        s_startup_pet_asset_pending = s_startup_pet_system_sleep_was_pending;
        s_startup_pet_asset_preempted_by_audio =
            s_startup_pet_system_sleep_was_preempted_by_audio;
        /* Cache Service owns a separate lock.  Record the reverse action
         * under the root lock, then call it after release: service host probes
         * are expressly lock-free from the root's point of view. */
        abort_pet_cache_prepare = true;
        /* If the audio lease completed while PREPARE held this optional domain
         * closed, its normal finish hook has already consumed the notification.
         * Re-arm once here; otherwise leave the marker for the still-active
         * audio lease to consume at its ordinary finish boundary. */
        if (s_startup_pet_asset_preempted_by_audio &&
            !s_server_audio_wake_memory_lease_active) {
            s_startup_pet_asset_preempted_by_audio = false;
            rearm_after_completed_audio = true;
        }
        abort_retry_prepare = true;
        s_startup_pet_system_sleep_was_pending = false;
        s_startup_pet_system_sleep_was_preempted_by_audio = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);

    if (abort_pet_cache_prepare) pet_cache_service_abort_system_sleep_prepare();

    if (abort_retry_prepare) startup_pet_retry_service_abort_system_sleep_prepare();
    /* The worker service delays an ABORT restart until the old immutable
     * Registry identity has gone away.  Invoke it outside the root lock so a
     * host restart callback can safely re-enter the composition root. */
    if (abort_pet_cache_prepare) startup_pet_worker_service_abort_system_sleep_prepare();
    if (rearm_after_completed_audio) rearm_preempted_startup_pet_asset();
}

static esp_err_t install_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                               uint8_t *const frames[PET_ASSET_MAX_FRAMES]);

static bool pet_asset_gateway_lease_current(
    const gateway_capability_lease_t *lease) {
    return lease && gateway_transport_capability_lease_current(lease);
}

static bool pet_cache_host_storage_mounted(void *context) {
    (void)context;
    return s_storage_mounted;
}

static bool pet_cache_host_allows_optional_flash_work(void *context) {
    (void)context;
    return device_storage_allows_optional_flash_work();
}

static bool pet_cache_host_gateway_lease_current(
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    return pet_asset_gateway_lease_current(lease);
}

static const pet_cache_service_host_t s_pet_cache_service_host = {
    .struct_size = sizeof(pet_cache_service_host_t),
    .storage_mounted = pet_cache_host_storage_mounted,
    .allows_optional_flash_work = pet_cache_host_allows_optional_flash_work,
    .gateway_lease_current = pet_cache_host_gateway_lease_current,
    .context = NULL,
};

/* The lifecycle service owns only task/Registry state. These host callbacks
 * retain the real HTTP transaction and download/install behavior at the
 * composition root, where the physical client and renderer are owned. */
static void startup_pet_worker_run_transaction(void *context) {
    (void)context;
    const int64_t started_us = esp_timer_get_time();
    const esp_err_t err = apply_deferred_pet_asset();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "deferred startup pet asset ignored: %s", esp_err_to_name(err));
    }
    ESP_LOGI(TAG, "post-ready pet asset work complete in %lu ms",
             (unsigned long)((esp_timer_get_time() - started_us) / 1000));
}

static void startup_pet_worker_cancel_active_transaction(uint32_t timeout_ms,
                                                          void *context) {
    (void)context;
    const uint32_t guard_ms = timeout_ms > 100 ? 100 : timeout_ms;
    if (guard_ms != 0) {
        (void)gateway_transport_cancel_active_requests(
            GATEWAY_TRANSPORT_CANCEL_ASSET, guard_ms);
    }
}

static void startup_pet_worker_restart_after_system_sleep_abort(void *context) {
    (void)context;
    apply_deferred_startup_pet_asset();
}

static const startup_pet_worker_service_host_t s_startup_pet_worker_service_host = {
    .struct_size = sizeof(startup_pet_worker_service_host_t),
    .run_transaction = startup_pet_worker_run_transaction,
    .cancel_active_transaction = startup_pet_worker_cancel_active_transaction,
    .restart_after_system_sleep_abort = startup_pet_worker_restart_after_system_sleep_abort,
    .context = NULL,
};

static esp_err_t download_pet_asset_frames(const pet_asset_ref_t *ref,
                                           uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                           bool startup_transaction,
                                           const gateway_capability_lease_t *gateway_lease) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    if (!pet_asset_gateway_lease_current(gateway_lease)) {
        return ESP_ERR_INVALID_STATE;
    }
    if (startup_transaction && startup_pet_asset_stop_requested()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!pet_asset_capacity_available(ref)) return ESP_ERR_NO_MEM;
    size_t expected = 0;
    if (!pet_asset_service_frame_bytes(ref->width, ref->height, &expected)) {
        return ESP_ERR_INVALID_SIZE;
    }
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
    for (int i = 0; i < ref->frame_count; ++i) {
        if (!pet_asset_gateway_lease_current(gateway_lease)) {
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
        if (startup_transaction && startup_pet_asset_stop_requested()) {
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
        // Optional artwork must stay below an interactive voice turn in both
        // connection priority and Wi-Fi airtime. Pause between frames while a
        // command is recording, uploading, or waiting for its result. A frame
        // already in flight is bounded to one 192 KiB response; the next one
        // cannot start until foreground ownership is released.
        while (startup_transaction &&
               (interaction_service_foreground_http_requested() || audio_media_download_active() ||
                server_audio_wake_memory_lease_active()) &&
               s_startup_pet_asset_pending && !startup_pet_asset_stop_requested()) {
            if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100)) != 0) break;
        }
        if (startup_transaction &&
            (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested())) {
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
        // Each frame is only 192 KiB, but on EchoEar it can race a TLS
        // handshake or wake-model transition for internal heap.  A transient
        // incomplete transfer must not discard the whole idle animation pack;
        // failed HTTP handles are discarded by request_with_capacity(), so a
        // bounded retry gets a clean connection and buffer on every attempt.
        http_response_t response = {0};
        esp_err_t err = ESP_FAIL;
        const unsigned max_attempts = startup_transaction ? 3 : 2;
        for (unsigned attempt = 1; attempt <= max_attempts; ++attempt) {
            bool optional_lease_held = false;
            // Serialize all large media bodies.  This does not serialize the
            // gateway poll itself, only the optional pet download against the
            // audible reply it may have just received.  With one TLS media
            // session at a time, EchoEar retains enough internal heap for
            // AES/TLS and completes the voice playback transaction.
            if (!s_media_transfer_mutex ||
                xSemaphoreTake(s_media_transfer_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
                err = ESP_ERR_TIMEOUT;
                break;
            }
            if (startup_transaction) {
                // The media lane makes this test-and-reserve atomic with
                // respect to other large media operations. A foreground reply
                // arriving before the request begins preempts the asset;
                // otherwise this optional TLS transfer gets enough internal
                // memory without resident MultiNet fragmenting AES buffers.
                if (!s_startup_pet_asset_pending ||
                    server_audio_wake_memory_lease_active() ||
                    startup_pet_asset_stop_requested()) {
                    xSemaphoreGive(s_media_transfer_mutex);
                    free_pet_asset_frames(frames, (size_t)i);
                    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
                    return ESP_ERR_INVALID_STATE;
                }
                begin_optional_media_wake_memory_lease("optional pet asset");
                optional_lease_held = true;
            }
            if (!pet_asset_gateway_lease_current(gateway_lease)) {
                err = ESP_ERR_INVALID_STATE;
            } else {
                /* Revalidate directly at the network side-effect boundary:
                 * a foreground lease can outlive descriptor parsing and the
                 * media-lane wait above. */
                err = request_with_capacity("GET", ref->urls[i], NULL, NULL, 0,
                                            expected + 1, &response);
            }
            if (optional_lease_held) finish_optional_pet_asset_memory_lease();
            xSemaphoreGive(s_media_transfer_mutex);
            // The user-visible server reply may have pre-empted this optional
            // request while its TLS read was in flight.  Do not retain/apply a
            // completed decorative frame after that cancellation, and release
            // its response body immediately so wake can reclaim its memory.
            if (startup_transaction &&
                (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested())) {
                response_release(&response);
                free_pet_asset_frames(frames, (size_t)i);
                memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
                return ESP_ERR_INVALID_STATE;
            }
            if (err == ESP_OK && response.status == 200 && response.len == expected) {
                break;
            }
            // Do not retry a bad/expired asset descriptor.  It cannot recover
            // locally and should be rejected by the normal profile refresh.
            int failed_status = response.status;
            size_t failed_len = response.len;
            bool permanent = err == ESP_OK && failed_status >= 400 && failed_status < 500;
            response_release(&response);
            memset(&response, 0, sizeof(response));
            if (permanent || attempt == max_attempts) break;
            ESP_LOGW(TAG, "pet asset frame retry: frame=%d attempt=%u/%u err=%s status=%d bytes=%u",
                     i, attempt, max_attempts, esp_err_to_name(err), failed_status,
                     (unsigned)failed_len);
            if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250u * attempt)) != 0) break;
            if (startup_transaction &&
                (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested())) break;
        }
        if (err != ESP_OK || response.status != 200 || response.len != expected) {
            esp_err_t result = err != ESP_OK ? err :
                               response.status >= 400 && response.status < 500
                                   ? ESP_ERR_INVALID_ARG : ESP_FAIL;
            response_release(&response);
            free_pet_asset_frames(frames, (size_t)i);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return result;
        }
        frames[i] = (uint8_t *)response.data;
        response.data = NULL;
        response_release(&response);
        uint8_t digest[32];
        size_t digest_len = 0;
        psa_status_t status = psa_hash_compute(PSA_ALG_SHA_256, (const uint8_t *)frames[i], expected,
                                               digest, sizeof(digest), &digest_len);
        if (status != PSA_SUCCESS || digest_len != sizeof(digest)) {
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_FAIL;
        }
        if (!pet_asset_service_sha256_matches_hex(digest, ref->sha256[i])) {
            ESP_LOGW(TAG, "pet asset SHA-256 mismatch: frame=%d", i);
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_CRC;
        }
        // Make standby useful as soon as the first verified frame is available.
        // The remaining frames can be retried without leaving EchoEar's round
        // idle screen blank when a later TLS transfer is interrupted.
        if (startup_transaction && i == 0) {
            esp_err_t preview_err = pet_asset_gateway_lease_current(gateway_lease)
                                        ? install_pet_asset_first_frame(ref, frames)
                                        : ESP_ERR_INVALID_STATE;
            if (preview_err == ESP_OK) {
                ESP_LOGI(TAG, "startup pet first frame applied while animation downloads");
            } else {
                ESP_LOGW(TAG, "startup pet first-frame preview failed: %s",
                         esp_err_to_name(preview_err));
            }
        }
        if (startup_transaction &&
            (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested())) {
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_STATE;
        }
    }
    return ESP_OK;
}

/* Pet-cache coordination owns its task, internal stack and Storage Registry
 * identity. The root supplies only immutable values and a startup-worker
 * cancellation probe, never its task-state lock or an SDK handle. */
static bool pet_cache_startup_cancelled(void *context) {
    (void)context;
    return startup_pet_worker_service_is_current_worker() &&
           startup_pet_asset_stop_requested();
}

static void cache_pet_asset_in_background(
    const pet_asset_ref_t *ref, uint8_t *frames[PET_ASSET_MAX_FRAMES],
    const gateway_capability_lease_t *gateway_lease) {
    pet_cache_service_cache_in_background(ref, frames, gateway_lease);
}

static bool drop_stale_pet_asset_cache(const pet_asset_ref_t *ref) {
    if (!ref) return false;
    bool dropped = false;
    return pet_cache_service_drop_if_stale(ref, &dropped,
                                            pet_cache_startup_cancelled, NULL) ==
               DEVICE_STATUS_OK &&
           dropped;
}

static esp_err_t clear_pet_asset_cache(void) {
    return device_status_to_platform_error(
        pet_cache_service_clear(NULL, NULL));
}

static esp_err_t clear_applied_pet_asset(void) {
    if (!s_pet_asset_apply_mutex ||
        xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = device_status_to_platform_error(
        scene_presenter_set_pet_asset(NULL, 0, 0, 0, 0));
    if (err == ESP_OK) {
        s_loaded_pet_asset_revision[0] = '\0';
        s_loaded_pet_asset_frame_count = 0;
        if (s_storage_mounted && device_storage_allows_optional_flash_work()) {
            esp_err_t cache_err = clear_pet_asset_cache();
            if (cache_err != ESP_OK) err = cache_err;
        }
    }
    xSemaphoreGive(s_pet_asset_apply_mutex);
    return err;
}

static esp_err_t install_pet_asset_with_fallback(const pet_asset_ref_t *ref,
                                                 uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                                 int *installed_frame_count,
                                                 int *installed_frame_ms);

static bool load_cached_pet_asset(void) {
    if (!s_storage_mounted) return false;
    pet_asset_ref_t ref = {0};
    size_t frame_bytes = 0;
    if (!pet_asset_cache_storage_read_descriptor(&ref) ||
        !pet_asset_service_frame_bytes(ref.width, ref.height, &frame_bytes)) {
        (void)clear_pet_asset_cache();
        return false;
    }
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    for (int i = 0; i < ref.frame_count; ++i) {
        frames[i] = heap_caps_malloc(frame_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!frames[i]) frames[i] = malloc(frame_bytes);
        bool ok = frames[i] && pet_asset_cache_storage_read_frame(
                                        &ref, (uint32_t)i, frames[i], frame_bytes);
        if (ok) {
            uint8_t digest[32]; size_t digest_len = 0;
            ok = psa_hash_compute(PSA_ALG_SHA_256, (const uint8_t *)frames[i], frame_bytes, digest,
                                  sizeof(digest), &digest_len) == PSA_SUCCESS &&
                 digest_len == sizeof(digest);
            if (ok) ok = pet_asset_service_sha256_matches_hex(digest, ref.sha256[i]);
        }
        if (!ok) break;
    }
    bool loaded = true;
    for (int i = 0; i < ref.frame_count; ++i) loaded = loaded && frames[i] != NULL;
    if (loaded) {
        if (!s_pet_asset_apply_mutex ||
            xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
            loaded = false;
        }
    }
    if (loaded) {
        int installed_frames = 0, installed_frame_ms = 0;
        loaded = install_pet_asset_with_fallback(&ref, frames, &installed_frames,
                                                 &installed_frame_ms) == ESP_OK;
        if (loaded) {
            strlcpy(s_loaded_pet_asset_revision, ref.revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = installed_frames;
            ESP_LOGI(TAG, "cached pet asset applied: revision=%s frames=%d/%d frame_ms=%d",
                     ref.revision, installed_frames, ref.frame_count, installed_frame_ms);
        }
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    if (!loaded) (void)clear_pet_asset_cache();
    return loaded;
}

/* Cache restore reads up to eight 192 KiB frames and then asks the renderer to
 * create retained PSRAM copies. Keep that stack and all VFS locals out of the
 * 4 KiB main task. This worker starts only after App UI has established its
 * single display submission owner, and before gateway connectivity begins its
 * own large TLS allocations. */
static void cached_pet_restore_task(void *arg) {
    SemaphoreHandle_t completion = (SemaphoreHandle_t)arg;
    if (s_storage_mounted && device_storage_allows_optional_flash_work() &&
        load_cached_pet_asset()) {
        /* A cached pack has no live Hub descriptor yet, but its saved frames
         * were previously accepted under the then-current pet profile. Restore
         * that profile's durable behaviour explicitly: the App UI default is
         * deliberately conservative and must not silently turn a cached
         * multi-frame asset into a static first pose before the handshake
         * arrives. Runtime Hub `motionEnabled` remains authoritative and can
         * immediately disable it later through the normal UI/HAL request. */
        ambient_service_apply_pet_profile(NULL, true);
        ESP_LOGI(TAG, "cached pet animation restored before connectivity startup");
    }
    if (completion) xSemaphoreGive(completion);
    vTaskDeleteWithCaps(NULL);
}

static bool start_cached_pet_restore_task(void) {
    SemaphoreHandle_t completion = xSemaphoreCreateBinary();
    if (!completion) {
        ESP_LOGW(TAG, "cached pet restore skipped: cannot allocate completion semaphore");
        return false;
    }
    TaskHandle_t task = NULL;
    /* This is a bounded boot-time operation. The task has no shared lifetime
     * owner and completes before any network worker is started below. Its
     * internal stack makes SPIFFS reads safe if Flash temporarily disables the
     * PSRAM cache. */
    if (xTaskCreatePinnedToCoreWithCaps(cached_pet_restore_task,
                                        "maclaw_pet_restore", 8192, completion, 1,
                                        &task, 1,
                                        MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT) != pdPASS) {
        vSemaphoreDelete(completion);
        ESP_LOGW(TAG, "cached pet restore skipped: cannot allocate worker");
        return false;
    }
    /* The cache is local Flash only. Join before networking starts, otherwise
     * a concurrent TLS transfer could raise PSRAM pressure midway through
     * restore. The worker always signals before deleting its internal stack. */
    xSemaphoreTake(completion, portMAX_DELAY);
    vSemaphoreDelete(completion);
    return true;
}

static esp_err_t install_pet_asset_with_fallback(const pet_asset_ref_t *ref,
                                                 uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                                 int *installed_frame_count,
                                                 int *installed_frame_ms) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    // The full-pack install consumes each source frame as soon as its scaled
    // copy exists, bounding the PSRAM peak at copies-plus-one-source.  Consumed
    // entries come back NULLed, so a memory-pressure retry first compacts the
    // surviving sources to the front and then installs a shorter keyframe loop.
    esp_err_t err = device_status_to_platform_error(
        scene_presenter_set_pet_asset_consuming(frames, (size_t)ref->frame_count,
                                        (size_t)ref->width, (size_t)ref->height,
                                        (uint32_t)ref->frame_ms));
    int used_count = ref->frame_count;
    int used_frame_ms = ref->frame_ms;
    // Keep the selected GUI pet visible on boards with less free PSRAM. A
    // lower keyframe count preserves the animation period and is preferable to
    // falling all the way back to the native robot head.
    while (err == ESP_ERR_NO_MEM && used_count > 1) {
        int remaining_count = 0;
        for (int i = 0; i < ref->frame_count; ++i) {
            if (frames[i]) frames[remaining_count++] = frames[i];
        }
        for (int i = remaining_count; i < ref->frame_count; ++i) frames[i] = NULL;
        uint32_t next_count = 0;
        uint32_t next_frame_ms = 0;
        if (!pet_asset_service_next_memory_fallback(
                ref, (uint32_t)used_count, (uint32_t)remaining_count,
                &next_count, &next_frame_ms)) {
            break;
        }
        used_frame_ms = (int)next_frame_ms;
        ESP_LOGW(TAG, "pet asset memory pressure; retrying with %d/%d frames",
                 (int)next_count, ref->frame_count);
        err = device_status_to_platform_error(
            scene_presenter_set_pet_asset_consuming(frames, (size_t)next_count,
                                            (size_t)ref->width, (size_t)ref->height,
                                            (uint32_t)used_frame_ms));
        used_count = (int)next_count;
    }
    if (installed_frame_count) *installed_frame_count = err == ESP_OK ? used_count : 0;
    if (installed_frame_ms) *installed_frame_ms = err == ESP_OK ? used_frame_ms : 0;
    return err;
}
static esp_err_t install_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                               uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!ref || !frames || !frames[0]) return ESP_ERR_INVALID_ARG;
    const uint8_t *first[1] = {frames[0]};
    return device_status_to_platform_error(
        scene_presenter_set_pet_asset(first, 1, (size_t)ref->width,
                             (size_t)ref->height,
                             (uint32_t)ref->frame_ms));
}
static esp_err_t apply_pet_asset_ref(cJSON *object) {
    pet_asset_ref_t descriptor;
    pet_asset_ref_t ref;
    if (!pet_asset_service_parse_hub_descriptor(object, &descriptor) ||
        !pet_asset_prepare_for_display(&descriptor, &ref)) return ESP_ERR_INVALID_ARG;
    // A pet_profile is durable latest-wins state. Hub can re-deliver its
    // still-unacknowledged control message after a poll retry, so do not fetch
    // and scale the exact revision again. More importantly, the startup
    // installer may already have published this full pack before its delayed
    // mirror reaches the outgoing queue. Bread treats that mirror as applied;
    // EchoEar must do the same instead of spending another eight TLS downloads
    // while the new pet is already on screen.
    if (s_loaded_pet_asset_revision[0] &&
        !strcmp(s_loaded_pet_asset_revision, ref.revision) &&
        s_loaded_pet_asset_frame_count >= ref.frame_count) {
        ESP_LOGI(TAG, "GUI pet asset already applied: revision=%s frames=%d/%d",
                 ref.revision, s_loaded_pet_asset_frame_count, ref.frame_count);
        return ESP_OK;
    }
    gateway_capability_lease_t gateway_lease = {0};
    if (!gateway_transport_capture_capability_lease(
            GATEWAY_CAPABILITY_PET_ASSET, &gateway_lease)) {
        ESP_LOGW(TAG, "pet asset update deferred: Gateway capability is not operational");
        return ESP_ERR_INVALID_STATE;
    }
    // A GUI-initiated pet switch must also succeed while the wake recognizer
    // is resident.  Borrow the same optional-media lease the startup installer
    // uses so the capacity sample and the downloads see the internal/PSRAM
    // blocks MultiNet was holding.
    begin_optional_media_wake_memory_lease("runtime pet asset");
    esp_err_t err = ESP_OK;
    if (!pet_asset_capacity_available(&ref)) {
        // 自愈：回收注定被替换的旧 revision 缓存后让门禁重新取样。
        (void)drop_stale_pet_asset_cache(&ref);
    }
    if (!pet_asset_capacity_available(&ref)) {
        err = ESP_ERR_NO_MEM;
    }
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    uint8_t *cache_frames[PET_ASSET_MAX_FRAMES] = {0};
    if (err == ESP_OK) {
        err = download_pet_asset_frames(&ref, frames, false, &gateway_lease);
    }
    if (err == ESP_OK) {
        if (!s_pet_asset_apply_mutex ||
            xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK) {
        // The consuming install releases the source frames. Commit the complete
        // SHA-verified pack first so a later cold boot can restore animation
        // locally instead of repeating the slow hub download.
        if (s_storage_mounted && device_storage_allows_optional_flash_work() &&
            !clone_pet_asset_frames(&ref, frames, cache_frames)) {
            ESP_LOGI(TAG, "pet asset cache skipped: cannot reserve full-pack mirror");
        }
        int installed_frames = 0, installed_frame_ms = 0;
        err = pet_asset_gateway_lease_current(&gateway_lease)
                  ? install_pet_asset_with_fallback(&ref, frames, &installed_frames,
                                                    &installed_frame_ms)
                  : ESP_ERR_INVALID_STATE;
        if (err == ESP_OK) {
            strlcpy(s_loaded_pet_asset_revision, ref.revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = installed_frames;
            ESP_LOGI(TAG, "GUI pet asset applied: revision=%s frames=%d/%d frame_ms=%d size=%dx%d",
                     ref.revision, installed_frames, ref.frame_count, installed_frame_ms,
                     ref.width, ref.height);
        }
        if (err == ESP_OK && installed_frames == ref.frame_count && cache_frames[0]) {
            cache_pet_asset_in_background(&ref, cache_frames, &gateway_lease);
        }
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    free_pet_asset_frames(cache_frames, PET_ASSET_MAX_FRAMES);
    finish_optional_pet_asset_memory_lease();
    return err;
}

static esp_err_t apply_deferred_pet_asset(void) {
    if (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested()) {
        return startup_pet_asset_stop_requested() ? ESP_ERR_INVALID_STATE : ESP_OK;
    }
    if (!s_startup_pet_asset_present) {
        s_loaded_pet_asset_revision[0] = '\0';
        esp_err_t err = clear_applied_pet_asset();
        s_startup_pet_asset_pending = false;
        return err;
    }
    if (!s_startup_pet_asset_ref) {
        s_startup_pet_asset_pending = false;
        return ESP_ERR_INVALID_STATE;
    }
    pet_asset_ref_t ref;
    if (!pet_asset_prepare_for_display(s_startup_pet_asset_ref, &ref)) {
        s_startup_pet_asset_pending = false;
        return ESP_ERR_INVALID_ARG;
    }
    if (s_loaded_pet_asset_revision[0] &&
        !strcmp(s_loaded_pet_asset_revision, ref.revision) &&
        s_loaded_pet_asset_frame_count >= ref.frame_count) {
        ESP_LOGI(TAG, "startup pet asset already cached: revision=%s",
                 ref.revision);
        s_startup_pet_asset_pending = false;
        return ESP_OK;
    }
    gateway_capability_lease_t gateway_lease = {0};
    if (!gateway_transport_capture_capability_lease(
            GATEWAY_CAPABILITY_PET_ASSET, &gateway_lease)) {
        /* A cold descriptor is data only; it must not authorize a later
         * download after the Hub has withdrawn the advertised capability. */
        ESP_LOGW(TAG, "startup pet asset deferred: Gateway capability is not operational");
        return ESP_ERR_INVALID_STATE;
    }
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    uint8_t *cache_frames[PET_ASSET_MAX_FRAMES] = {0};
    esp_err_t err = ESP_FAIL;
    // A fully installed pack is the smooth-animation target, but the first
    // verified frame has already made standby usable.  Continue the complete
    // transaction after transport-level failures instead of leaving EchoEar on
    // that preview for the rest of the boot.  Each failed pass frees its own
    // partial source frames and request_with_capacity discards the failed TLS
    // handle, so the next pass starts cleanly without growing PSRAM usage.
    for (unsigned attempt = 1; attempt <= PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS;
         ++attempt) {
        if (startup_pet_asset_stop_requested()) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
        err = download_pet_asset_frames(&ref, frames, true, &gateway_lease);
        if (err == ESP_OK &&
            (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested())) {
            err = ESP_ERR_INVALID_STATE;
        }
        if (err == ESP_OK || err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_CRC ||
            err == ESP_ERR_INVALID_SIZE || err == ESP_ERR_INVALID_STATE) {
            break;
        }
        if (attempt == PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS) break;
        ESP_LOGW(TAG, "startup pet pack retry: attempt=%u/%u err=%s; preview remains visible",
                 attempt, PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS, esp_err_to_name(err));
        if (ulTaskNotifyTake(pdTRUE,
                              pdMS_TO_TICKS(PET_ASSET_STARTUP_RETRY_DELAY_MS * attempt)) != 0 ||
            !s_startup_pet_asset_pending || startup_pet_asset_stop_requested()) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
    }
    if (err == ESP_OK) {
        // The live GUI choice may have arrived after the last frame download.
        // Re-check after acquiring renderer ownership so this optional startup
        // transaction can never publish an older pack over a newer selection.
        if (!s_pet_asset_apply_mutex ||
            xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
            err = ESP_ERR_INVALID_STATE;
        } else if (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested()) {
            xSemaphoreGive(s_pet_asset_apply_mutex);
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "startup pet frames downloaded; installing first frame");
        // Put a real pet on the standby surface immediately. Scaling one frame
        // is quick; the remaining seven animated frames can be installed after
        // the durable cache commit completes.
        esp_err_t preview_err = install_pet_asset_first_frame(&ref, frames);
        if (preview_err != ESP_OK) {
            ESP_LOGW(TAG, "startup pet preview failed: %s", esp_err_to_name(preview_err));
        } else {
            ESP_LOGI(TAG, "startup pet first frame applied");
        }
        /* Publish the verified in-memory animation before any optional Flash
         * work. The renderer consumes source frames as it scales, which keeps
         * the visible user outcome ahead of slow/fragmented SPIFFS writes and
         * prevents a timed DISPLAY_OFF from making a completed download look
         * lost. A complete cache mirror is added by a later ownership-safe
         * phase; metadata must never describe a reduced fallback animation. */
        if (s_storage_mounted && device_storage_allows_optional_flash_work() &&
            !clone_pet_asset_frames(&ref, frames, cache_frames)) {
            ESP_LOGI(TAG, "deferred pet cache skipped: cannot reserve full-pack mirror");
        }
        int installed_frames = 0, installed_frame_ms = 0;
        if (startup_pet_asset_stop_requested() ||
            !pet_asset_gateway_lease_current(&gateway_lease)) {
            err = ESP_ERR_INVALID_STATE;
        } else {
            err = install_pet_asset_with_fallback(&ref, frames,
                                                   &installed_frames, &installed_frame_ms);
        }
        if (err == ESP_OK) {
            strlcpy(s_loaded_pet_asset_revision, ref.revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = installed_frames;
            ESP_LOGI(TAG, "deferred pet asset applied: revision=%s frames=%d/%d frame_ms=%d size=%dx%d",
                     ref.revision, installed_frames,
                     ref.frame_count, installed_frame_ms,
                     ref.width, ref.height);
        }
        if (err == ESP_OK && installed_frames == ref.frame_count && cache_frames[0]) {
            cache_pet_asset_in_background(&ref, cache_frames, &gateway_lease);
        }
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    // The display port retains its own scaled PSRAM copies.  The source HTTP
    // buffers are normally released here, but on EchoEar that deallocation
    // races the QSPI full-frame path and causes a cache-disable assertion
    // immediately after the visible pet has been installed. Retain the tiny
    // one-shot source set for this boot; it is bounded (8 × 192 KiB) and avoids
    // a restart that would otherwise erase the successful standby transition.
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    free_pet_asset_frames(cache_frames, PET_ASSET_MAX_FRAMES);
    // Keep pending true for the entire download/install/cache transaction so
    // a queued pet_profile mirror is ACKed without starting a competing copy.
    s_startup_pet_asset_pending = false;
    return err;
}

#define STARTUP_PET_RETRY_MAX 6u
#define STARTUP_PET_RETRY_INTERVAL_US (10ULL * 1000ULL * 1000ULL)

static bool schedule_startup_pet_retry_timer(void) {
    if (startup_pet_asset_stop_requested()) return false;
    return startup_pet_retry_service_schedule(STARTUP_PET_RETRY_INTERVAL_US) ==
           DEVICE_STATUS_OK;
}

static void apply_deferred_startup_pet_asset(void) {
    if (!s_startup_pet_asset_pending || startup_pet_asset_stop_requested()) return;
    pet_asset_ref_t display_ref;
    const bool display_ref_ready = s_startup_pet_asset_ref &&
                                   pet_asset_prepare_for_display(
                                       s_startup_pet_asset_ref, &display_ref);
    if (display_ref_ready && !pet_asset_capacity_available(&display_ref)) {
        // 自愈：旧 revision 缓存注定被这次安装替换，先回收再让门禁重新取样。
        (void)drop_stale_pet_asset_cache(&display_ref);
    }
    if (!display_ref_ready || !pet_asset_capacity_available(&display_ref)) {
        /* Decorative network/cache work is the first domain to yield.  Do not
         * reserve TLS/PSRAM or trigger SPIFFS GC when foreground safety margin
         * is already exhausted; a later Hub state refresh can offer it again. */
        // The capacity sample happens right after boot while the wake model,
        // glyph downloads and the first TLS polls still hold large transient
        // PSRAM blocks.  That pressure subsides within seconds, so retry a few
        // times before giving the boot's pet pack up entirely.
        if (display_ref_ready && s_startup_pet_retry_count < STARTUP_PET_RETRY_MAX) {
            if (schedule_startup_pet_retry_timer()) {
                ++s_startup_pet_retry_count;
                ESP_LOGW(TAG, "startup pet asset deferred: capacity tight, retry %u/%u in 10 s",
                         s_startup_pet_retry_count, STARTUP_PET_RETRY_MAX);
                return;
            }
        }
        ESP_LOGW(TAG, "startup pet asset skipped: shared optional capacity unavailable");
        s_startup_pet_asset_pending = false;
        return;
    }
    // Never block the gateway startup owner for optional artwork. The worker
    // service owns create/publish/Registry/retirement sequencing; root only
    // decides whether the domain is eligible to start a transaction.
    if (s_startup_pet_system_sleep_preparing || startup_pet_worker_service_active()) return;
    if (!gateway_transport_capabilities_operational(
            GATEWAY_CAPABILITY_PET_ASSET)) {
        /* Descriptor retention is harmless, but a retry timer must not keep
         * creating download workers after capability withdrawal. A later
         * authenticated handshake supplies a fresh descriptor and re-arms the
         * normal cold-start path. */
        ESP_LOGI(TAG, "startup pet asset remains deferred: Gateway capability unavailable");
        return;
    }
    const device_status_t start_status = startup_pet_worker_service_start();
    if (start_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "cannot start deferred pet asset worker: status=%d",
                 (int)start_status);
    }
}

/* Server audio preempts the cold-start pet install (the two cannot share the
 * media lane/PSRAM).  When the audio lease finishes, re-arm the deferred
 * install so the standby pet survives the boot even on a slow backhaul.
 * The preempted worker may still be unwinding an in-flight frame request, in
 * which case the retry timer is the backstop that spawns its replacement. */
static void rearm_preempted_startup_pet_asset(void) {
    if (!s_startup_pet_asset_present || !s_startup_pet_asset_ref) return;
    // A pet_profile push or an earlier re-arm may already own the install.
    if (s_startup_pet_asset_pending) return;
    pet_asset_ref_t display_ref;
    if (!pet_asset_prepare_for_display(s_startup_pet_asset_ref, &display_ref)) return;
    if (s_loaded_pet_asset_revision[0] &&
        !strcmp(s_loaded_pet_asset_revision, display_ref.revision) &&
        s_loaded_pet_asset_frame_count >= display_ref.frame_count) {
        return;
    }
    s_startup_pet_asset_pending = true;
    if (!startup_pet_worker_service_active()) {
        ESP_LOGI(TAG, "startup pet asset re-armed after server audio");
        apply_deferred_startup_pet_asset();
        return;
    }
    if (schedule_startup_pet_retry_timer()) {
        ESP_LOGI(TAG, "startup pet asset re-armed after server audio (worker unwinding)");
    } else {
        s_startup_pet_asset_pending = false;
        ESP_LOGW(TAG, "cannot re-arm preempted startup pet asset");
    }
}

static bool audio_mime_supported(const char *mime) {
    return !mime || !strcmp(mime, "audio/wav") || !strcmp(mime, "audio/x-wav") ||
           !strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3");
}

static bool audio_payload_is_mp3(const char *mime, const uint8_t *data, size_t len) {
    if (mime && (!strcmp(mime, "audio/mpeg") || !strcmp(mime, "audio/mp3"))) {
        return true;
    }
    if (!data || len < 2) return false;
    if (len >= 3 && memcmp(data, "ID3", 3) == 0) return true;
    // MPEG audio sync: eleven leading one bits. The decoder validates the
    // layer/version fields; this is only format dispatch when MIME is absent.
    return data[0] == 0xFF && (data[1] & 0xE0) == 0xE0;
}

// Legacy domain workflows still use esp_err_t internally.  Keep conversion at
// this migration seam rather than leaking board_port calls back into them.
static esp_err_t device_status_to_esp_err(device_status_t status) {
    switch (status) {
        case DEVICE_STATUS_OK: return ESP_OK;
        case DEVICE_STATUS_INVALID_ARGUMENT: return ESP_ERR_INVALID_ARG;
        case DEVICE_STATUS_UNAVAILABLE: return ESP_ERR_NOT_SUPPORTED;
        case DEVICE_STATUS_BUSY: return ESP_ERR_INVALID_STATE;
        case DEVICE_STATUS_TIMEOUT: return ESP_ERR_TIMEOUT;
        // Preserve "no speech detected" end-to-end so interaction_task can
        // show its retry hint instead of the generic microphone failure.
        case DEVICE_STATUS_NOT_FOUND: return ESP_ERR_NOT_FOUND;
        case DEVICE_STATUS_RESOURCE_EXHAUSTED: return ESP_ERR_NO_MEM;
        case DEVICE_STATUS_IO_ERROR: return ESP_FAIL;
        default: return ESP_ERR_INVALID_RESPONSE;
    }
}

static esp_err_t audio_wake_word_start(device_wake_word_cb_t on_wake, void *context) {
    return device_status_to_esp_err(audio_arbitration_wake_word_start(on_wake, context));
}

static esp_err_t audio_wake_word_stop(void) {
    return device_status_to_esp_err(audio_arbitration_wake_word_stop());
}

static esp_err_t audio_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    return device_status_to_esp_err(audio_arbitration_wake_word_stop_with_timeout(timeout_ms));
}

static esp_err_t play_audio_payload(const char *mime, const uint8_t *data, size_t len) {
    if (!data || len == 0) return ESP_ERR_INVALID_ARG;
    if (audio_payload_is_mp3(mime, data, len)) {
        return device_status_to_esp_err(mp3_player_play(data, len));
    }
    return device_status_to_esp_err(audio_arbitration_play_wav(data, (uint32_t)len));
}

static bool hardware_audio_url_allowed(const char *url) {
    if (!url || url[0] != '/') return false;
    return !strncmp(url, "/api/im-gateway/v1/media/", strlen("/api/im-gateway/v1/media/"));
}

static bool audio_error_is_permanent(esp_err_t err) {
    return err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE ||
           err == ESP_ERR_INVALID_RESPONSE || err == ESP_ERR_NOT_SUPPORTED ||
           err == ESP_ERR_INVALID_STATE;
}

static const char *json_string(cJSON *root, const char *key) {
    cJSON *node = cJSON_GetObjectItemCaseSensitive(root, key);
    return cJSON_IsString(node) && node->valuestring ? node->valuestring : NULL;
}

static bool json_number(cJSON *root, const char *key, int *value) {
    cJSON *node = root ? cJSON_GetObjectItemCaseSensitive(root, key) : NULL;
    if (!cJSON_IsNumber(node) || !value) return false;
    *value = node->valueint;
    return true;
}

// Ambient state and pet-profile updates are server initiated. Keep a single
// long-poll running even while the user is not speaking; otherwise weather
// pushed after the startup handshake would sit at Hub until the next button
// interaction. Its dedicated client lane prevents an idle long poll from
// adding several seconds to foreground voice upload and command submission.
static bool startup_welcome_is_current_boot(cJSON *item, bool welcome_audio) {
    if (!welcome_audio || !cJSON_IsObject(item)) return false;
    const char *boot_session_id = json_string(item, "bootSessionId");
    // Only a greeting explicitly correlated to this cold boot is allowed to
    // control the gate. Reserved-ID messages without a boot ID remain ordinary
    // compatibility audio and cannot release (or be discarded by) startup.
    return boot_session_id && boot_session_id[0] &&
           strcmp(boot_session_id, s_boot_session_id) == 0;
}

static void finish_startup_welcome_gate(const char *reason) {
    bool notify = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_welcome_gate_active) {
        s_startup_welcome_gate_active = false;
        notify = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!notify) return;
    ESP_LOGI(TAG, "startup Welcome gate released: %s", reason ? reason : "complete");
    if (s_startup_welcome_done) xSemaphoreGive(s_startup_welcome_done);
}

static bool start_gateway_ready_tasks(void) {
    // The authenticated handshake has released its TLS certificate-validation
    // working set. Start the durable local alarm service before polling can
    // expose alarm tools, avoiding a boot-time TLS/cache overlap while keeping
    // the scheduler independent of the selected input/display adapter.
    if (!ensure_alarm_manager_started()) {
        pet("alert");
        scene_presenter_publish_message("设备启动失败", "无法启动闹钟服务");
        return false;
    }
    // The handshake queues this boot's optional greeting before it returns.
    // Initialize MultiNet before the outgoing reader can play the greeting or
    // apply a queued hardware-volume update. This removes the cold-start race
    // for the shared audio bus and makes wake readiness the first service
    // published after the authenticated handshake.
    while (s_startup_welcome_done && xSemaphoreTake(s_startup_welcome_done, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_sequence_complete = false;
    s_startup_welcome_gate_active = s_handshake_startup_welcome_queued;
    s_startup_welcome_timed_out = false;
    s_startup_welcome_consumed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // Re-assert the board-specific boot artwork at the exact Welcome boundary.
    // It may already be visible from app_main(), but this closes every path where
    // pairing/status work temporarily owned the display before the handshake.
    scene_presenter_publish_startup_splash();
    int64_t wake_start_us = esp_timer_get_time();
    esp_err_t wake_err = audio_wake_word_start(on_wake_word, NULL);
    // The board API waits for MultiNet's explicit ready flag and returns OK
    // only when inference is listening. INVALID_STATE is not a success signal:
    // it may mean that audio/model initialization failed or that a stale task
    // is still being cleaned up. Publishing the normal standby surface in that
    // state recreates the exact "screen ready, wake still unavailable" gap.
    bool wake_ready = wake_err == ESP_OK;
    ESP_LOGI(TAG, "startup wake initialization complete: ready=%s elapsed=%lu ms",
             wake_ready ? "yes" : "no",
             (unsigned long)((esp_timer_get_time() - wake_start_us) / 1000));
    if (!wake_ready) {
        ESP_LOGW(TAG, "offline wake start failed: %s", esp_err_to_name(wake_err));
    }
    if (!gateway_dispatcher_ensure_poll_task()) {
        if (wake_ready) {
            esp_err_t stop_err = audio_wake_word_stop();
            if (stop_err != ESP_OK && stop_err != ESP_ERR_INVALID_STATE) {
                ESP_LOGW(TAG, "offline wake cleanup after poll failure: %s",
                         esp_err_to_name(stop_err));
            }
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        s_startup_welcome_gate_active = false;
        s_startup_welcome_timed_out = s_handshake_startup_welcome_queued;
        s_startup_sequence_complete = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        pet("alert");
        scene_presenter_publish_message("设备启动失败", "无法启动网关轮询");
        return false;
    }
    /* A retained meeting is durable recovery work.  Start its lightweight
     * supervisor only after the authenticated poll lane and wake listener are
     * both initialized, so it shares the normal connectivity lifecycle and
     * never races cold-start TLS setup. */
    if (!meeting_service_ensure_resume_supervisor()) {
        ESP_LOGW(TAG, "meeting recovery supervisor unavailable; retained audio remains pending");
    }
    if (s_handshake_startup_welcome_queued) {
        ESP_LOGI(TAG, "startup Welcome gate armed; wake listener ready=%s",
                 wake_ready ? "yes" : "no");
        if (xSemaphoreTake(s_startup_welcome_done,
                           pdMS_TO_TICKS(STARTUP_WELCOME_TIMEOUT_MS)) != pdTRUE) {
            taskENTER_CRITICAL(&s_task_state_lock);
            bool still_pending = s_startup_welcome_gate_active;
            s_startup_welcome_gate_active = false;
            s_startup_welcome_timed_out = still_pending;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (still_pending) {
                ESP_LOGW(TAG, "startup Welcome gate timed out after %u ms; late greeting will be discarded",
                         STARTUP_WELCOME_TIMEOUT_MS);
            }
        }
    } else {
        ESP_LOGI(TAG, "startup Welcome unavailable or disabled; continuing without playback");
    }
    // The normal standby surface is still published last. Touch/wake callbacks
    // remain blocked by s_startup_sequence_complete while the greeting owns
    // the startup surface, although recognition itself is already hot.
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_sequence_complete = true;
    bool restart_after_startup = s_wake_restart_after_startup;
    s_wake_restart_after_startup = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // A phrase can be detected while the optional Welcome audio is still
    // authoritative. In that case the one-shot EchoEar recognizer tears itself
    // down and the callback below records a deferred restart. Avoid spawning a
    // restart worker on every normal boot: calling start_wake_word while the
    // recognizer is already ready only waits and logs a false "restarted"
    // transition, and can overlap the optional pet-asset memory transaction.
    if (!wake_ready || restart_after_startup) schedule_wake_restart();
    firmware_identity_set_service_ready(true);
    scene_presenter_publish_service_ready(true);
    const char *primary_input = device_input_primary_interaction_label();
    const char *volume_hint = device_input_volume_interaction_hint();
    char ready_hint[72];
    char wake_failed_hint[72];
    snprintf(ready_hint, sizeof(ready_hint), "按%s说话，双击开会议；%s",
             primary_input, volume_hint);
    snprintf(wake_failed_hint, sizeof(wake_failed_hint),
             "唤醒加载失败，可按%s说话", primary_input);
    scene_presenter_publish_ready_prompt(wake_ready ? "设备已就绪" : "设备基本就绪",
                                 wake_ready ? ready_hint : wake_failed_hint);
    // Handshake metadata is parsed before the Welcome/startup surface is
    // released.  Publish a pending update notice only after that sequence has
    // completed so it cannot cover the boot artwork or interrupt greeting
    // playback.  Runtime handshakes keep their immediate notification path.
    publish_pending_update_reminder();
    return true;
}

/* Connectivity Service owns the reversible system-sleep admission fence.
 * Root-only callback/SNTP/portal sources are fenced here; restartable Gateway
 * worker policy is delegated to the Connectivity-domain lifecycle service. */
static device_status_t cancel_gateway_requests_for_system_sleep(uint32_t timeout_ms,
                                                                 void *context) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining_ms = 0;
    device_status_t lifecycle_status;

    /* The startup-pet retry callback can otherwise spawn its HTTP worker after
     * the shared cancellation sweep. Preserve only its pre-PREPARE intent. */
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms
        ? prepare_startup_pet_asset_system_sleep(remaining_ms)
        : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status != DEVICE_STATUS_OK) return lifecycle_status;

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms ? clock_sync_service_prepare_system_sleep(remaining_ms)
                                    : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status != DEVICE_STATUS_OK) {
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms
        ? cellular_recovery_service_prepare_system_sleep(remaining_ms)
        : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status != DEVICE_STATUS_OK) {
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms ? prepare_wake_restart_system_sleep(remaining_ms)
                                    : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status != DEVICE_STATUS_OK) {
        abort_wake_restart_system_sleep_prepare();
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms ? prepare_deferred_setup_system_sleep(remaining_ms)
                                    : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status != DEVICE_STATUS_OK) {
        abort_deferred_setup_system_sleep_prepare();
        abort_wake_restart_system_sleep_prepare();
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms
        ? gateway_lifecycle_service_prepare_system_sleep(remaining_ms)
        : DEVICE_STATUS_TIMEOUT;
    if (startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        gateway_lifecycle_service_abort_system_sleep_prepare();
        abort_deferred_setup_system_sleep_prepare();
        abort_wake_restart_system_sleep_prepare();
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return DEVICE_STATUS_TIMEOUT;
    }
    if (lifecycle_status != DEVICE_STATUS_OK) {
        gateway_lifecycle_service_abort_system_sleep_prepare();
        abort_deferred_setup_system_sleep_prepare();
        abort_wake_restart_system_sleep_prepare();
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }
    return DEVICE_STATUS_OK;
}

static void resume_gateway_workers_after_system_sleep_abort(void *context) {
    (void)context;
    /* Reverse PREPARE order. Each service restarts only the worker that it
     * recorded as live before the fence; no portal, recovery or meeting work
     * is accidentally created by a failed future sleep transition. */
    gateway_lifecycle_service_abort_system_sleep_prepare();
    abort_deferred_setup_system_sleep_prepare();
    abort_wake_restart_system_sleep_prepare();
    cellular_recovery_service_abort_system_sleep_prepare();
    clock_sync_service_abort_system_sleep_prepare();
    abort_startup_pet_asset_system_sleep_prepare();
}

/* Clock sync lifecycle moved to services/clock_sync_service.c. */

static void clock_sync_ensure_ambient_clock(void *context) {
    (void)context;
    (void)ambient_service_ensure_clock_task();
}

static void clock_sync_note_wall_clock(int64_t epoch_sec, void *context) {
    (void)context;
    ambient_service_note_wall_clock(epoch_sec);
}

static void clock_sync_notify_wall_clock_updated(void *context) {
    (void)context;
    wake_deadline_service_on_wall_clock_updated();
    sleep_schedule_service_on_wall_clock_updated();
}

static const clock_sync_service_host_t s_clock_sync_service_host = {
    .struct_size = sizeof(clock_sync_service_host_t),
    .ensure_ambient_clock = clock_sync_ensure_ambient_clock,
    .note_wall_clock = clock_sync_note_wall_clock,
    .notify_wall_clock_updated = clock_sync_notify_wall_clock_updated,
    .context = NULL,
};

static void apply_gateway_server_time(cJSON *json) {
    cJSON *server_time = json ? cJSON_GetObjectItemCaseSensitive(json, "serverTime") : NULL;
    if (!cJSON_IsNumber(server_time)) return;
    const double server_time_ms = server_time->valuedouble;
    const double minimum_time_ms = 1672531200000.0; /* 2023-01-01 UTC */
    const double maximum_time_ms = 4102444800000.0; /* 2100-01-01 UTC */
    if (server_time_ms < minimum_time_ms || server_time_ms >= maximum_time_ms) {
        ESP_LOGW(TAG, "ignored invalid gateway serverTime: %.0f", server_time_ms);
        return;
    }
    const int64_t epoch_ms = (int64_t)server_time_ms;
    struct timeval tv = {
        .tv_sec = (time_t)(epoch_ms / 1000),
        .tv_usec = (suseconds_t)((epoch_ms % 1000) * 1000),
    };
    if (settimeofday(&tv, NULL) != 0) {
        ESP_LOGW(TAG, "cannot apply gateway serverTime: errno=%d", errno);
        return;
    }
    clock_sync_service_note_authenticated_epoch((int64_t)tv.tv_sec);
    /* ML307 has no ESP-NETIF route for SNTP. Once authenticated Hub time is
     * available it may safely start the standby cadence. */
    ambient_service_ensure_clock_task();
    ESP_LOGI(TAG, "clock source: gateway serverTime");
}

static void wake_restart_task(void *arg) {
    (void)arg;
    /* schedule_wake_restart() registers this one-shot worker before releasing
     * the gate.  A recognizer that is already torn down can otherwise make a
     * very fast worker exit between xTaskCreateWithCaps() and registration. */
    if (!s_wake_restart_start_gate ||
        xSemaphoreTake(s_wake_restart_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        bool restart_after_system_sleep_abort = false;
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_retiring = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        const esp_err_t registry_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_AUDIO, (void *)self, 10);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_exit_status = registry_err;
        if (s_wake_restart_task == self) {
            s_wake_restart_task = NULL;
            s_wake_restart_scheduled = false;
        }
        s_wake_restart_retiring = false;
        if (registry_err != ESP_OK) {
            s_wake_restart_stop_requested = true;
            s_wake_restart_admission_open = false;
            s_wake_restart_registry_retirement_failed = true;
        }
        if (s_wake_restart_system_sleep_restart_pending &&
            !s_wake_restart_system_sleep_preparing &&
            s_wake_restart_admission_open && registry_err == ESP_OK) {
            s_wake_restart_system_sleep_restart_pending = false;
            restart_after_system_sleep_abort = true;
        } else if (s_wake_restart_system_sleep_restart_pending && registry_err != ESP_OK) {
            /* Preserve the marker but close admission: retaining one failed
             * generation is safer than creating a second Registry identity. */
            s_wake_restart_admission_open = false;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_wake_restart_stopped) xSemaphoreGive(s_wake_restart_stopped);
        if (restart_after_system_sleep_abort) schedule_wake_restart();
        vTaskDeleteWithCaps(NULL);
        return;
    }
    // Let the meeting worker delete its internal stack before MultiNet claims
    // memory again. This task uses a PSRAM stack and does not write flash.
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250));
    esp_err_t err = ESP_FAIL;
    unsigned attempt = 1;
    bool waiting_for_foreground = false;
    bool stop_requested = false;
    while (!device_connectivity_is_provisioning_active()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        stop_requested = s_wake_restart_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested) break;
        if (attempt > 12) {
            /* Keep one registered worker across transient heap pressure.  A
             * former implementation spawned its replacement before this task
             * had released its completion token, so a stop could join the old
             * task while believing the replacement had stopped. */
            ESP_LOGE(TAG, "offline wake restart exhausted: %s; retrying after backoff",
                     esp_err_to_name(err));
            attempt = 1;
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1000));
            continue;
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        bool foreground_active = interaction_service_worker_active() ||
                                 interaction_service_foreground_http_requested();
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (foreground_active || meeting_service_is_active()) {
            if (!waiting_for_foreground) {
                ESP_LOGI(TAG, "offline wake restart waiting for foreground audio owner");
                waiting_for_foreground = true;
            }
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
            continue;
        }
        // Startup pet artwork is nonessential and may still own large PSRAM
        // source/renderer buffers just after a server reply.  Do not repeatedly
        // construct MultiNet against that transient allocation pressure: wait
        // until the cancelled worker has actually exited and released it.
        if (startup_pet_worker_service_active()) {
            if (!waiting_for_foreground) {
                ESP_LOGI(TAG, "offline wake restart waiting for optional pet worker");
                waiting_for_foreground = true;
            }
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
            continue;
        }
        // The asset lane's persistent esp_http_client retains TLS buffers
        // after a cancelled cold-start pet request.  Release that optional
        // client before loading MultiNet; otherwise the recognizer's small
        // internal PCM buffers can fail even after the worker itself exited.
        gateway_transport_discard_asset_client();
        waiting_for_foreground = false;
        err = audio_wake_word_start(on_wake_word, NULL);
        if (err == ESP_OK) break;
        ESP_LOGW(TAG, "offline wake restart attempt %u/12 failed: %s",
                 attempt, esp_err_to_name(err));
        ++attempt;
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500));
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_wake_restart_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "offline wake restarted after foreground interaction");
    } else if (!stop_requested && !device_connectivity_is_provisioning_active()) {
        ESP_LOGI(TAG, "offline wake restart stopped before completion");
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_AUDIO, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_exit_status = registry_err;
    if (s_wake_restart_task == self) {
        s_wake_restart_task = NULL;
        s_wake_restart_scheduled = false;
    }
    s_wake_restart_retiring = false;
    if (registry_err != ESP_OK) {
        s_wake_restart_stop_requested = true;
        s_wake_restart_admission_open = false;
        s_wake_restart_registry_retirement_failed = true;
    }
    if (s_wake_restart_system_sleep_restart_pending &&
        !s_wake_restart_system_sleep_preparing &&
        s_wake_restart_admission_open && registry_err == ESP_OK) {
        s_wake_restart_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    } else if (s_wake_restart_system_sleep_restart_pending && registry_err != ESP_OK) {
        /* Do not create a new retry worker while the old immutable Registry
         * entry is still visible. A later lifecycle pass can repair the
         * retained, deliberately closed generation. */
        s_wake_restart_admission_open = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_wake_restart_stopped) xSemaphoreGive(s_wake_restart_stopped);
    if (restart_after_system_sleep_abort) schedule_wake_restart();
    vTaskDeleteWithCaps(NULL);
}

/* This only drains the restart coordinator.  The actual wake recognizer has
 * its own board-owned stop contract and is deliberately not force-stopped by
 * a Registry owner that does not own its audio/I2S resources. */
static esp_err_t stop_wake_restart_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_stop_requested = true;
    s_wake_restart_admission_open = false;
    task = s_wake_restart_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_task_state_lock);
        const esp_err_t exit_status = s_wake_restart_exit_status;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return exit_status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_wake_restart_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_wake_restart_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t exit_status = s_wake_restart_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (exit_status != ESP_OK) return exit_status;
    ESP_LOGI(TAG, "offline wake restart worker stopped");
    return ESP_OK;
}

static esp_err_t stop_wake_restart_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_wake_restart_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_wake_restart_task(timeout_ms);
}

/* Audio Service already owns the recognizer's physical pause/ACK, but this
 * root-owned retry task can otherwise be created just after Audio has reached
 * that safe point.  Preserve only a pre-existing coordinator across ABORT;
 * an in-progress creator or startup-deferred request has no replay-safe task
 * generation yet, so it deliberately makes a future physical commit BUSY. */
static device_status_t prepare_wake_restart_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    bool was_running = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_wake_restart_system_sleep_preparing ||
        (s_wake_restart_scheduled && !s_wake_restart_task) ||
        s_wake_restart_after_startup) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_wake_restart_system_sleep_preparing = true;
    s_wake_restart_system_sleep_was_admitted = s_wake_restart_admission_open;
    was_running = s_wake_restart_task != NULL;
    s_wake_restart_system_sleep_was_running = was_running;
    /* Close task creation before observing/stopping the retained generation.
     * `schedule_wake_restart()` takes the same critical lock, so no creation
     * can slip between this snapshot and a future electrical PREPARE. */
    s_wake_restart_admission_open = false;
    taskEXIT_CRITICAL(&s_task_state_lock);

    if (!was_running) return DEVICE_STATUS_OK;
    esp_err_t stop_err = stop_wake_restart_task(timeout_ms);
    /* A failed join keeps retry admission closed until the parent rollback;
     * the retiring task may still own its Registry generation. */
    return startup_status_from_esp_err(stop_err);
}

static void abort_wake_restart_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_wake_restart_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return;
    }
    restart = s_wake_restart_system_sleep_was_running;
    s_wake_restart_admission_open = !s_wake_restart_registry_retirement_failed &&
                                    s_wake_restart_system_sleep_was_admitted;
    s_wake_restart_system_sleep_was_running = false;
    s_wake_restart_system_sleep_was_admitted = false;
    s_wake_restart_system_sleep_preparing = false;
    /* A stop timeout can race the old task's completion and Registry
     * self-unregistration.  That task alone may replace the generation after
     * its identity is gone; otherwise a new worker could share its completion
     * semaphore or make a stale Registry entry address the replacement. */
    if (restart && s_wake_restart_retiring) {
        s_wake_restart_system_sleep_restart_pending = true;
        restart = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (restart) schedule_wake_restart();
}

static void schedule_wake_restart(void) {
    if (device_connectivity_is_provisioning_active() || !s_startup_sequence_complete) return;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_wake_restart_admission_open &&
                          !s_wake_restart_registry_retirement_failed &&
                          !s_wake_restart_system_sleep_preparing;
    bool already_scheduled = s_wake_restart_scheduled;
    if (admission_open && !already_scheduled) s_wake_restart_scheduled = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) return;
    if (already_scheduled) return;
    if (!s_wake_restart_start_gate || !s_wake_restart_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_scheduled = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGW(TAG, "offline wake restart unavailable before lifecycle primitives initialize");
        return;
    }
    while (xSemaphoreTake(s_wake_restart_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_stop_requested = false;
    s_wake_restart_exit_status = ESP_OK;
    s_wake_restart_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    BaseType_t created = xTaskCreateWithCaps(wake_restart_task, "maclaw_wake_restart",
                                             2048, NULL, 2, &s_wake_restart_task,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_task = NULL;
        s_wake_restart_scheduled = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot schedule offline wake restart");
    } else {
        esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
            .struct_size = sizeof(task_registry_entry_t),
            .owner = TASK_REGISTRY_OWNER_AUDIO,
            .name = "wake_restart",
            /* Use this task's immutable handle value as entry identity.  A
             * failed attempt may schedule the next retry before it deletes
             * itself; a shared static-handle address would let the retiring
             * worker unregister that newer entry. */
            .context = (void *)s_wake_restart_task,
            .stop = stop_wake_restart_registry_entry,
        });
        if (registry_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot register offline wake restart worker: %s",
                     esp_err_to_name(registry_err));
            xSemaphoreGive(s_wake_restart_start_gate);
            (void)stop_wake_restart_task(500);
        } else {
            xSemaphoreGive(s_wake_restart_start_gate);
            ESP_LOGI(TAG, "offline wake restart scheduled");
        }
    }
}

/* Configuration contains the Wi-Fi credential and the Hub bearer token.  A
 * malformed persisted snapshot is therefore not equivalent to an empty first
 * boot: silently falling back to firmware defaults could reconnect a device
 * using credentials that no longer describe its owner.  Return the read
 * result to the composition root so it can stop before radio/gateway work.
 */
static esp_err_t load_device_config(void) {
    /* Measured at boot, this call chain (caller snapshot + service store +
     * NVS frames) peaked near 4.9 KB of main-task stack and forced a larger
     * boot stack on memory-tight profiles.  The snapshot is transient, so
     * keep it in PSRAM and free it before returning. */
    configuration_snapshot_t *snapshot =
        heap_caps_calloc(1, sizeof(*snapshot), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!snapshot) return ESP_ERR_NO_MEM;
    strlcpy(snapshot->wifi_ssid, CONFIG_MACLAW_WIFI_SSID, sizeof(snapshot->wifi_ssid));
    strlcpy(snapshot->wifi_password, CONFIG_MACLAW_WIFI_PASSWORD, sizeof(snapshot->wifi_password));
    strlcpy(snapshot->wifi_security, "personal", sizeof(snapshot->wifi_security));
    strlcpy(snapshot->wifi_eap_method, "peap", sizeof(snapshot->wifi_eap_method));
    strlcpy(snapshot->wifi_ttls_phase2, "mschapv2", sizeof(snapshot->wifi_ttls_phase2));
    strlcpy(snapshot->wifi_ca_mode, "system", sizeof(snapshot->wifi_ca_mode));
    strlcpy(snapshot->gateway_url, CONFIG_MACLAW_SERVER_URL, sizeof(snapshot->gateway_url));
    strlcpy(snapshot->gateway_token, CONFIG_MACLAW_GATEWAY_TOKEN, sizeof(snapshot->gateway_token));
    bool staged_provisioning = false;
    esp_err_t err = device_status_to_platform_error(
        configuration_service_load_boot_candidate(snapshot, &staged_provisioning));
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "configuration snapshot rejected: %s", esp_err_to_name(err));
        heap_caps_free(snapshot);
        return err;
    }
    strlcpy(s_wifi_ssid, snapshot->wifi_ssid, sizeof(s_wifi_ssid));
    strlcpy(s_wifi_password, snapshot->wifi_password, sizeof(s_wifi_password));
    s_wifi_network_count = snapshot->wifi_network_count;
    if (s_wifi_network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY) {
        s_wifi_network_count = CONFIGURATION_WIFI_NETWORK_CAPACITY;
    }
    memcpy(s_wifi_networks, snapshot->wifi_networks,
           s_wifi_network_count * sizeof(s_wifi_networks[0]));
    strlcpy(s_wifi_security, snapshot->wifi_security, sizeof(s_wifi_security));
    strlcpy(s_wifi_eap_method, snapshot->wifi_eap_method, sizeof(s_wifi_eap_method));
    strlcpy(s_wifi_identity, snapshot->wifi_identity, sizeof(s_wifi_identity));
    strlcpy(s_wifi_username, snapshot->wifi_username, sizeof(s_wifi_username));
    strlcpy(s_wifi_ttls_phase2, snapshot->wifi_ttls_phase2, sizeof(s_wifi_ttls_phase2));
    strlcpy(s_wifi_ca_mode, snapshot->wifi_ca_mode, sizeof(s_wifi_ca_mode));
    strlcpy(s_wifi_server_domain, snapshot->wifi_server_domain, sizeof(s_wifi_server_domain));
    char snapshot_gateway_url[URL_CAPACITY];
    strlcpy(snapshot_gateway_url, snapshot->gateway_url, sizeof(snapshot_gateway_url));
    char snapshot_gateway_token[CONFIGURATION_GATEWAY_TOKEN_CAPACITY];
    strlcpy(snapshot_gateway_token, snapshot->gateway_token, sizeof(snapshot_gateway_token));
    gateway_transport_set_gateway_credentials(snapshot_gateway_url, snapshot_gateway_token,
                                              snapshot->pair_code);
    s_configured_output_volume = snapshot->output_volume;
    s_configured_output_volume_saved = snapshot->output_volume_saved;
    s_configured_display_brightness = snapshot->display_brightness;
    s_configured_display_brightness_saved = snapshot->display_brightness_saved;
    s_configured_screen_sleep_seconds = snapshot->screen_sleep_seconds;
    s_configured_screen_sleep_seconds_saved = snapshot->screen_sleep_seconds_saved;
    heap_caps_free(snapshot);
    if (staged_provisioning) {
        /* The candidate intentionally becomes active for this boot, but it is
         * not the durable owner yet. Gateway token persistence will promote it
         * only after the Hub accepts its one-time pairing code. */
        device_status_t staged_boot_status =
            configuration_service_begin_staged_provisioning_boot();
        if (staged_boot_status == DEVICE_STATUS_NOT_FOUND) {
            ESP_LOGW(TAG, "unconfirmed provisioning candidate retry budget expired; restarting confirmed configuration");
            esp_restart();
            return ESP_ERR_INVALID_STATE;
        }
        if (staged_boot_status != DEVICE_STATUS_OK) {
            ESP_LOGE(TAG, "cannot durably claim provisioning candidate boot: status=%d",
                     (int)staged_boot_status);
            return device_status_to_platform_error(staged_boot_status);
        }
        ESP_LOGW(TAG, "booting unconfirmed provisioning candidate");
    }
    s_boot_provisioning_staged = staged_provisioning;
    return ESP_OK;
}

static esp_err_t save_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    esp_err_t err = device_status_to_platform_error(
        configuration_service_set_output_volume((uint8_t)percent));
    if (err == ESP_OK) {
        s_configured_output_volume = percent;
        s_configured_output_volume_saved = true;
    }
    return err;
}

static esp_err_t save_display_brightness(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    return device_status_to_platform_error(
        configuration_service_set_display_brightness((uint8_t)percent));
}

static bool valid_screen_sleep_seconds(int seconds) {
    return seconds >= 0 &&
        configuration_service_valid_screen_sleep_seconds((uint32_t)seconds);
}

static esp_err_t save_screen_sleep_seconds(unsigned seconds) {
    return device_status_to_platform_error(
        configuration_service_set_screen_sleep_seconds((uint32_t)seconds));
}

static void output_volume_persist_task(void *arg) {
    (void)arg;
    output_volume_persist_request_t request = {0};
    while (true) {
        if (xQueueReceive(s_output_volume_persist_queue, &request,
                          portMAX_DELAY) != pdTRUE) {
            continue;
        }
        if (request.stop) break;
        if (request.system_sleep_prepare) {
            /* The composition-root bridge already closed admission and owns
             * the request mutex.  Acknowledge only after every older queue
             * item has finished; retain this same worker generation for a
             * later ABORT instead of terminally stopping it. */
            if (s_output_volume_persist_system_sleep_quiesced) {
                xSemaphoreGive(s_output_volume_persist_system_sleep_quiesced);
            }
            continue;
        }
        output_volume_persist_reply_t reply = {
            .result = ESP_ERR_INVALID_STATE,
            .generation = request.generation,
        };
        if (request.display_policy) {
            configuration_display_policy_update_t update = {
                .struct_size = sizeof(update),
                .abi_version = CONFIGURATION_DISPLAY_POLICY_UPDATE_ABI_VERSION,
                .has_brightness = request.display_policy_has_brightness,
                .brightness = (uint8_t)request.percent,
                .has_screen_sleep_seconds = request.display_policy_has_screen_sleep,
                .screen_sleep_seconds = request.screen_sleep_seconds,
            };
            reply.result = device_status_to_platform_error(
                configuration_service_apply_display_policy_with_policy(
                    &update,
                    &(configuration_policy_request_t){
                        .struct_size = sizeof(configuration_policy_request_t),
                        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
                        .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
                        .authenticated = true,
                        .ttl_ms = 0u,
                    },
                    &reply.configuration_revision));
        } else if (request.output_volume_policy) {
            reply.result = device_status_to_platform_error(
                configuration_service_set_output_volume_with_policy_revision(
                    (uint8_t)request.percent,
                    &(configuration_policy_request_t){
                        .struct_size = sizeof(configuration_policy_request_t),
                        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
                        .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
                        .authenticated = true,
                        .ttl_ms = 0u,
                    },
                    &reply.configuration_revision));
        } else {
            reply.result = request.gateway_token
                          ? device_status_to_platform_error(
                                configuration_service_commit_gateway_pairing_token(request.token))
                          : (request.brightness
                                 ? (request.hub_authenticated
                                        ? device_status_to_platform_error(
                                              configuration_service_set_display_brightness_with_policy(
                                                  (uint8_t)request.percent,
                                                  &(configuration_policy_request_t){
                                                      .struct_size = sizeof(configuration_policy_request_t),
                                                      .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
                                                      .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
                                                      .authenticated = true,
                                                      .ttl_ms = 0u,
                                                  }))
                                        : save_display_brightness(request.percent))
                                 : (request.screen_sleep
                                        ? (request.hub_authenticated
                                               ? device_status_to_platform_error(
                                                     configuration_service_set_screen_sleep_seconds_with_policy(
                                                         request.screen_sleep_seconds,
                                                         &(configuration_policy_request_t){
                                                             .struct_size = sizeof(configuration_policy_request_t),
                                                             .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
                                                             .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
                                                             .authenticated = true,
                                                             .ttl_ms = 0u,
                                                         }))
                                               : save_screen_sleep_seconds(request.screen_sleep_seconds))
                                 : (request.hub_authenticated
                                        ? device_status_to_platform_error(
                                              configuration_service_set_output_volume_with_policy(
                                                  (uint8_t)request.percent,
                                                  &(configuration_policy_request_t){
                                                      .struct_size = sizeof(configuration_policy_request_t),
                                                      .abi_version =
                                                          CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
                                                      .source =
                                                          CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
                                                      .authenticated = true,
                                                      .ttl_ms = 0u,
                                                  }))
                                        : save_output_volume(request.percent))));
        }
        // The reply queue has room for the active completion plus one stale
        // completion from a timed-out caller. Do not lose the correlation ID:
        // the next caller must be able to distinguish that old result.
        while (xQueueSend(s_output_volume_persist_reply_queue, &reply,
                          pdMS_TO_TICKS(50)) != pdTRUE) {
            bool stop_requested;
            taskENTER_CRITICAL(&s_task_state_lock);
            stop_requested = s_output_volume_persist_stop_requested;
            taskEXIT_CRITICAL(&s_task_state_lock);
            /* During lifecycle drain the waiting caller has already abandoned
             * its reply. Do not let a full reply queue prevent the stop
             * sentinel from ever being consumed. */
            if (stop_requested) break;
        }
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    s_output_volume_persist_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* The Registry entry is the authoritative lifecycle identity.  Retire it
     * before publishing completion, otherwise a timed-out stop could observe
     * no task handle and incorrectly authorize a replacement generation. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_STORAGE, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_output_volume_persist_exit_status = registry_err;
    if (s_output_volume_persist_task_handle == self) {
        s_output_volume_persist_task_handle = NULL;
    }
    s_output_volume_persist_retiring = false;
    if (registry_err != ESP_OK) {
        s_output_volume_persist_stop_requested = true;
        s_output_volume_persist_registry_retirement_failed = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_output_volume_persist_stopped) xSemaphoreGive(s_output_volume_persist_stopped);
    /* This worker is created with xTaskCreatePinnedToCoreWithCaps() so its
     * explicitly internal stack must be released with the matching deleter. */
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_output_volume_persist_worker(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_output_volume_persist_queue || !s_output_volume_persist_stopped ||
        !s_output_volume_persist_request_mutex) {
        return ESP_OK;
    }
    /* Registry stop passes one owner-wide timeout. Taking the request mutex,
     * publishing STOP and joining the internal-stack worker are one teardown
     * transaction, not three independent timeout windows. */
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_output_volume_persist_stop_requested = true;
    s_output_volume_persist_system_sleep_preparing = false;
    TaskHandle_t task = s_output_volume_persist_task_handle;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_task_state_lock);
        const esp_err_t exit_status = s_output_volume_persist_exit_status;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return exit_status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* A caller may have timed out after the worker finished its flash write,
     * leaving both reply slots occupied. Make room before queuing the stop
     * sentinel; the lifecycle caller owns the admission mutex, so no active
     * request can lose a completion at this point. */
    output_volume_persist_reply_t stale_reply;
    while (xQueueReceive(s_output_volume_persist_reply_queue, &stale_reply, 0) == pdTRUE) {}
    output_volume_persist_request_t stop_request = {.stop = true};
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return ESP_ERR_TIMEOUT;
    }
    BaseType_t queued = xQueueSend(s_output_volume_persist_queue, &stop_request,
                                   pdMS_TO_TICKS(remaining_ms));
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    if (queued != pdTRUE) return ESP_ERR_TIMEOUT;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    if (xSemaphoreTake(s_output_volume_persist_stopped,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t exit_status = s_output_volume_persist_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (exit_status != ESP_OK) return exit_status;
    ESP_LOGI(TAG, "volume persistence worker stopped");
    return ESP_OK;
}

static esp_err_t stop_output_volume_persist_registry_entry(void *context,
                                                            uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_output_volume_persist_task_handle;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_output_volume_persist_worker(timeout_ms);
}

// NVS flash writes cannot run from a task whose stack lives in PSRAM: cache
// disable makes that stack inaccessible. The regular key handler already runs
// from internal RAM, but the gateway workers do not, so use one shared
// internal-stack persistence worker for both call paths.
static esp_err_t persist_hardware_level(unsigned percent, bool brightness,
                                        bool hub_authenticated) {
    if (percent > 100 || !s_output_volume_persist_queue ||
        !s_output_volume_persist_reply_queue || !s_output_volume_persist_request_mutex) {
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_output_volume_persist_stop_requested ||
                          s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex,
                       pdMS_TO_TICKS(4000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_output_volume_persist_stop_requested ||
                     s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    output_volume_persist_reply_t reply = {0};
    while (xQueueReceive(s_output_volume_persist_reply_queue, &reply, 0) == pdTRUE) {}
    output_volume_persist_request_t request = {
        .percent = percent,
        .generation = ++s_output_volume_persist_generation,
        .brightness = brightness,
        .hub_authenticated = hub_authenticated,
    };
    if (request.generation == 0) request.generation = ++s_output_volume_persist_generation;
    esp_err_t err = ESP_ERR_TIMEOUT;
    if (xQueueSend(s_output_volume_persist_queue, &request,
                   pdMS_TO_TICKS(1000)) == pdTRUE) {
        TickType_t started = xTaskGetTickCount();
        const TickType_t timeout = pdMS_TO_TICKS(3000);
        while (true) {
            TickType_t elapsed = xTaskGetTickCount() - started;
            if (elapsed >= timeout ||
                xQueueReceive(s_output_volume_persist_reply_queue, &reply,
                              timeout - elapsed) != pdTRUE) {
                break;
            }
            if (reply.generation == request.generation) {
                err = reply.result;
                break;
            }
            ESP_LOGW(TAG, "discarding stale volume persistence reply generation=%lu (want %lu)",
                     (unsigned long)reply.generation,
                     (unsigned long)request.generation);
        }
    }
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    return err;
}

static esp_err_t persist_output_volume(unsigned percent) {
    return persist_hardware_level(percent, false, false);
}

static esp_err_t persist_hub_output_volume(unsigned percent, uint64_t *out_revision) {
    if (percent > 100u || !out_revision || !s_output_volume_persist_queue ||
        !s_output_volume_persist_reply_queue || !s_output_volume_persist_request_mutex) {
        return ESP_ERR_INVALID_STATE;
    }
    *out_revision = 0u;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_output_volume_persist_stop_requested ||
                          s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex, pdMS_TO_TICKS(4000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_output_volume_persist_stop_requested ||
                     s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    output_volume_persist_reply_t reply = {0};
    while (xQueueReceive(s_output_volume_persist_reply_queue, &reply, 0) == pdTRUE) {}
    output_volume_persist_request_t request = {
        .percent = percent,
        .generation = ++s_output_volume_persist_generation,
        .output_volume_policy = true,
        .hub_authenticated = true,
    };
    if (request.generation == 0u) request.generation = ++s_output_volume_persist_generation;
    esp_err_t err = ESP_ERR_TIMEOUT;
    if (xQueueSend(s_output_volume_persist_queue, &request, pdMS_TO_TICKS(1000)) == pdTRUE) {
        TickType_t started = xTaskGetTickCount();
        const TickType_t timeout = pdMS_TO_TICKS(3000);
        while (true) {
            TickType_t elapsed = xTaskGetTickCount() - started;
            if (elapsed >= timeout || xQueueReceive(s_output_volume_persist_reply_queue, &reply,
                                                    timeout - elapsed) != pdTRUE) {
                break;
            }
            if (reply.generation == request.generation) {
                err = reply.result;
                if (err == ESP_OK) *out_revision = reply.configuration_revision;
                break;
            }
            ESP_LOGW(TAG, "discarding stale volume-policy persistence reply generation=%lu (want %lu)",
                     (unsigned long)reply.generation, (unsigned long)request.generation);
        }
    }
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    return err;
}

static esp_err_t persist_hub_display_policy(bool has_brightness, unsigned brightness,
                                            bool has_screen_sleep, unsigned seconds,
                                            uint64_t *out_revision) {
    if ((!has_brightness && !has_screen_sleep) ||
        (has_brightness && brightness > 100u) ||
        (has_screen_sleep && !valid_screen_sleep_seconds((int)seconds)) ||
        !s_output_volume_persist_queue ||
        !s_output_volume_persist_reply_queue || !s_output_volume_persist_request_mutex) {
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_output_volume_persist_stop_requested ||
                          s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex, pdMS_TO_TICKS(4000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_output_volume_persist_stop_requested ||
                     s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    output_volume_persist_reply_t reply = {0};
    while (xQueueReceive(s_output_volume_persist_reply_queue, &reply, 0) == pdTRUE) {}
    output_volume_persist_request_t request = {
        .percent = brightness,
        .screen_sleep_seconds = (uint32_t)seconds,
        .generation = ++s_output_volume_persist_generation,
        .display_policy = true,
        .display_policy_has_brightness = has_brightness,
        .display_policy_has_screen_sleep = has_screen_sleep,
        .hub_authenticated = true,
    };
    if (request.generation == 0) request.generation = ++s_output_volume_persist_generation;
    esp_err_t err = ESP_ERR_TIMEOUT;
    if (xQueueSend(s_output_volume_persist_queue, &request, pdMS_TO_TICKS(1000)) == pdTRUE) {
        TickType_t started = xTaskGetTickCount();
        const TickType_t timeout = pdMS_TO_TICKS(3000);
        while (true) {
            TickType_t elapsed = xTaskGetTickCount() - started;
            if (elapsed >= timeout ||
                xQueueReceive(s_output_volume_persist_reply_queue, &reply,
                              timeout - elapsed) != pdTRUE) {
                break;
            }
            if (reply.generation == request.generation) {
                err = reply.result;
                if (err == ESP_OK && out_revision) {
                    *out_revision = reply.configuration_revision;
                }
                break;
            }
            ESP_LOGW(TAG, "discarding stale display-policy persistence reply generation=%lu (want %lu)",
                      (unsigned long)reply.generation, (unsigned long)request.generation);
        }
    }
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    return err;
}

static bool is_enterprise_wifi(void) {
    return !strcmp(s_wifi_security, "enterprise");
}

static void load_gateway_token(void) {
    /* Token is now loaded with the atomic configuration snapshot. Kept as a
     * compatibility call-site seam while Gateway startup is migrated. */
}

static void load_device_id(void) {
    // Always derive the physical identity from the chip MAC. Reading an NVS
    // copy first makes cloned factory NVS partitions duplicate client IDs across
    // devices, which defeats independent tokens. Keep a best-effort copy only
    // for diagnostics and future migrations.
    uint8_t mac[6] = {0};
    char device_id[DEVICE_ID_CAPACITY] = {0};
    if (connectivity_wifi_driver_owner_read_station_mac(mac) == DEVICE_STATUS_OK) {
        snprintf(device_id, sizeof(device_id), "esp32s3-%02x%02x%02x%02x%02x%02x",
                 mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
    }
    if (!device_id[0]) snprintf(device_id, sizeof(device_id), "%s", CONFIG_MACLAW_CLIENT_ID);
    gateway_transport_set_device_id(device_id);
}

static esp_err_t save_gateway_token(const char *token) {
    if (!token || !token[0] || strlen(token) >= CONFIGURATION_GATEWAY_TOKEN_CAPACITY) return ESP_ERR_INVALID_SIZE;
    /* `gateway_startup_task` uses a PSRAM stack.  Pairing completes with this
     * call, so writing NVS directly here makes a successful Hub `201` reboot
     * before the token reaches flash.  The Hub has then consumed the code,
     * leaving the device permanently retrying an invalid persisted code.
     *
     * Route this through the same internal-stack persistence worker that owns
     * volume/brightness writes.  The durable operation atomically stores the
     * token and clears the consumed pairing code. */
    if (!s_output_volume_persist_queue || !s_output_volume_persist_reply_queue ||
        !s_output_volume_persist_request_mutex) {
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_output_volume_persist_stop_requested ||
                          s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex, pdMS_TO_TICKS(4000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_output_volume_persist_stop_requested ||
                     s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    output_volume_persist_reply_t stale_reply;
    while (xQueueReceive(s_output_volume_persist_reply_queue, &stale_reply, 0) == pdTRUE) {}
    output_volume_persist_request_t request = {
        .generation = ++s_output_volume_persist_generation,
        .gateway_token = true,
    };
    if (request.generation == 0) request.generation = ++s_output_volume_persist_generation;
    strlcpy(request.token, token, sizeof(request.token));
    esp_err_t err = ESP_ERR_TIMEOUT;
    if (xQueueSend(s_output_volume_persist_queue, &request, pdMS_TO_TICKS(4000)) == pdTRUE) {
        const int64_t deadline_us = esp_timer_get_time() + 4000000;
        while (true) {
            int64_t remaining_us = deadline_us - esp_timer_get_time();
            if (remaining_us <= 0) break;
            output_volume_persist_reply_t reply = {0};
            if (xQueueReceive(s_output_volume_persist_reply_queue, &reply,
                              pdMS_TO_TICKS((uint32_t)((remaining_us + 999) / 1000))) != pdTRUE) {
                break;
            }
            if (reply.generation == request.generation) {
                err = reply.result;
                break;
            }
        }
    }
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    return err;
}

static esp_err_t upload_voice(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
    int64_t upload_started_us = esp_timer_get_time();
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id());
    cJSON_AddStringToObject(body, "type", "voice");
    cJSON_AddStringToObject(body, "fileName", "voice.wav");
    cJSON_AddStringToObject(body, "mimeType", "audio/wav");
    cJSON_AddNumberToObject(body, "sizeBytes", (double)wav_len);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response = {0};
    esp_err_t err = ESP_FAIL;
    for (unsigned attempt = 1; attempt <= VOICE_UPLOAD_RETRY_COUNT; ++attempt) {
        err = request("POST", "/api/im-gateway/v1/media/upload-url", "application/json",
                      payload, strlen(payload), &response);
        if (err == ESP_OK && response.status == 200) break;
        bool retry = command_service_voice_upload_should_retry((int32_t)err, response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media prepare attempt %u/%u failed: err=%s status=%d retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), response.status,
                 retry ? "yes" : "no");
        response_release(&response);
        if (!retry) break;
        command_service_voice_upload_retry_delay(attempt);
    }
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        if (err == ESP_ERR_INVALID_STATE) {
            // Foreground cancellation skips/aborts the request; that is a user
            // action, not an upload failure worth an alarm.
            ESP_LOGI(TAG, "media prepare cancelled by user");
        } else {
            ESP_LOGE(TAG, "media prepare failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        }
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *media = json ? cJSON_GetObjectItemCaseSensitive(json, "media") : NULL;
    cJSON *upload = json ? cJSON_GetObjectItemCaseSensitive(json, "upload") : NULL;
    const char *id = media ? json_string(media, "id") : NULL;
    const char *url = upload ? json_string(upload, "url") : NULL;
    if (!id || !url) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    ESP_LOGI(TAG, "voice upload prepared: wav=%u bytes elapsed=%lldms",
             (unsigned)wav_len, (long long)((esp_timer_get_time() - upload_started_us) / 1000));
    char id_copy[96];
    char url_copy[URL_CAPACITY];
    strlcpy(id_copy, id, sizeof(id_copy));
    strlcpy(url_copy, url, sizeof(url_copy));
    cJSON_Delete(json);
    response_release(&response);
    http_response_t put_response = {0};
    for (unsigned attempt = 1; attempt <= VOICE_UPLOAD_RETRY_COUNT; ++attempt) {
        err = request("PUT", url_copy, "audio/wav", (const char *)wav, wav_len, &put_response);
        if (err == ESP_OK && (put_response.status == 200 || put_response.status == 201)) break;
        bool retry = command_service_voice_upload_should_retry((int32_t)err, put_response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media upload attempt %u/%u failed: err=%s status=%d wav=%u retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), put_response.status,
                 (unsigned)wav_len, retry ? "yes" : "no");
        response_release(&put_response);
        if (!retry) break;
        command_service_voice_upload_retry_delay(attempt);
    }
    if (err != ESP_OK || (put_response.status != 200 && put_response.status != 201)) {
        if (err == ESP_ERR_INVALID_STATE) {
            ESP_LOGI(TAG, "media upload cancelled by user");
        } else {
            ESP_LOGE(TAG, "media upload failed: err=%s status=%d", esp_err_to_name(err), put_response.status);
        }
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&put_response);
        return result;
    }
    strlcpy(media_id, id_copy, media_id_cap);
    int64_t upload_elapsed_ms = (esp_timer_get_time() - upload_started_us) / 1000;
    unsigned throughput_kbps = upload_elapsed_ms > 0
                                   ? (unsigned)((wav_len * 1000ULL / (unsigned long long)upload_elapsed_ms) / 1024ULL)
                                   : 0;
    ESP_LOGI(TAG, "voice upload complete: media=%s wav=%u bytes elapsed=%lldms throughput=%uKiB/s",
             id_copy, (unsigned)wav_len, (long long)upload_elapsed_ms, throughput_kbps);
    response_release(&put_response);
    return ESP_OK;
}

static esp_err_t send_voice_event(const char *media_id, const char *event_id,
                                  char *reply_to, size_t reply_to_cap) {
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id());
    cJSON_AddStringToObject(body, "eventId", event_id);
    cJSON_AddStringToObject(body, "messageId", event_id);
    cJSON_AddStringToObject(body, "conversationId", CONFIG_MACLAW_CONVERSATION_ID);
    cJSON *user = cJSON_AddObjectToObject(body, "user");
    cJSON_AddStringToObject(user, "id", "local-user");
    cJSON_AddStringToObject(user, "displayName", "ESP32-S3 user");
    cJSON *message = cJSON_AddObjectToObject(body, "message");
    cJSON_AddStringToObject(message, "id", event_id);
    cJSON_AddStringToObject(message, "type", "voice");
    cJSON_AddStringToObject(message, "mimeType", "audio/wav");
    cJSON *attachments = cJSON_AddArrayToObject(message, "attachments");
    cJSON *attachment = cJSON_CreateObject();
    cJSON_AddStringToObject(attachment, "id", media_id);
    cJSON_AddStringToObject(attachment, "type", "voice");
    cJSON_AddStringToObject(attachment, "mimeType", "audio/wav");
    cJSON_AddItemToArray(attachments, attachment);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    int64_t submit_started_us = esp_timer_get_time();
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/incoming", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "incoming event failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *accepted = json ? cJSON_GetObjectItemCaseSensitive(json, "accepted") : NULL;
    // MaClawSrv returns the canonical `maclawMessageId`, while the embedded
    // Hub relay returns the accepted client message as `messageId`.  Both
    // identify the same reply correlation key.  Keep accepting the canonical
    // response first, but do not reject a command merely because it travelled
    // through the Hub-compatible response shape.
    const char *reply_message_id = json ? json_string(json, "maclawMessageId") : NULL;
    if ((!reply_message_id || !reply_message_id[0]) && json) {
        reply_message_id = json_string(json, "messageId");
    }
    if (!cJSON_IsTrue(accepted)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    if (reply_to && reply_to_cap > 0) {
        // Some older gateways acknowledge the event without echoing either ID.
        // Their outgoing reply is correlated to the submitted message/event ID,
        // so the idempotency key remains the safe protocol-compatible fallback.
        strlcpy(reply_to,
                reply_message_id && reply_message_id[0] ? reply_message_id : event_id,
                reply_to_cap);
    }
    ESP_LOGI(TAG, "voice command accepted: event=%s replyTo=%s duplicate=%s elapsed=%lldms",
             event_id,
             reply_message_id && reply_message_id[0] ? reply_message_id : event_id,
             cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(json, "duplicate")) ? "yes" : "no",
             (long long)((esp_timer_get_time() - submit_started_us) / 1000));
    cJSON_Delete(json);
    response_release(&response);
    return ESP_OK;
}

static esp_err_t send_text_event(const char *text, const char *reply_to) {
    if (!text || !text[0]) return ESP_ERR_INVALID_ARG;
    cJSON *body = cJSON_CreateObject();
    char event_id[80];
    snprintf(event_id, sizeof(event_id), "text-%lld", (long long)esp_timer_get_time());
    cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id());
    cJSON_AddStringToObject(body, "eventId", event_id);
    cJSON_AddStringToObject(body, "messageId", event_id);
    cJSON_AddStringToObject(body, "conversationId", CONFIG_MACLAW_CONVERSATION_ID);
	if (reply_to && reply_to[0]) {
		// Cancellation is a control for the active command, not an independent
		// result-producing turn. Preserve that relationship end-to-end so Hub/GUI
		// can suppress its acknowledgement even if the new control message ID is
		// absent from an older relay envelope.
		cJSON_AddStringToObject(body, "replyTo", reply_to);
		cJSON_AddStringToObject(body, "replyToMessageId", reply_to);
	}
    cJSON *user = cJSON_AddObjectToObject(body, "user");
    cJSON_AddStringToObject(user, "id", "local-user");
    cJSON_AddStringToObject(user, "displayName", "ESP32-S3 user");
    cJSON *message = cJSON_AddObjectToObject(body, "message");
    cJSON_AddStringToObject(message, "id", event_id);
    cJSON_AddStringToObject(message, "type", "text");
    cJSON_AddStringToObject(message, "text", text);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    http_response_t response;
    esp_err_t err = request("POST", "/api/im-gateway/v1/incoming", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *accepted = json ? cJSON_GetObjectItemCaseSensitive(json, "accepted") : NULL;
    bool ok = cJSON_IsTrue(accepted);
    cJSON_Delete(json);
    response_release(&response);
    return ok ? ESP_OK : ESP_ERR_INVALID_RESPONSE;
}

/* Meeting chunk streaming is owned by Gateway Transport. */

static esp_err_t create_meeting_recording(char recording_id[MEETING_SERVICE_RECORDING_ID_CAPACITY]) {
    char payload[192];
    int length = snprintf(payload, sizeof(payload),
                          "{\"title\":\"硬件会议录音\",\"purpose\":\"\","
                          "\"conversation_id\":\"%s\",\"content_type\":\"audio/wav\"}",
                          CONFIG_MACLAW_CONVERSATION_ID);
    if (length <= 0 || length >= (int)sizeof(payload)) return ESP_ERR_INVALID_SIZE;
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    http_response_t response;
    esp_err_t err = request_with_capacity("POST", base_path, "application/json",
                                          payload, length, MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 201) {
        ESP_LOGE(TAG, "meeting create failed: err=%s status=%d body=%s",
                 esp_err_to_name(err), response.status, response.data ? response.data : "");
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *id = json ? json_string(json, "recording_id") : NULL;
    if (!id || strlen(id) >= MEETING_SERVICE_RECORDING_ID_CAPACITY) err = ESP_ERR_INVALID_RESPONSE;
    else strlcpy(recording_id, id, MEETING_SERVICE_RECORDING_ID_CAPACITY);
    cJSON_Delete(json);
    response_release(&response);
    return err;
}

static esp_err_t get_meeting_status(const char *recording_id, char *status, size_t status_cap) {
    char path[MEETING_SERVICE_BASE_PATH_CAPACITY + MEETING_SERVICE_RECORDING_ID_CAPACITY + 8];
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    int length = snprintf(path, sizeof(path), "%s/%s", base_path, recording_id);
    if (length <= 0 || length >= (int)sizeof(path)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("GET", path, NULL, NULL, 0,
                                          MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = response.status == 404 ? ESP_ERR_NOT_FOUND : err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *value = json ? json_string(json, "status") : NULL;
    if (!value || strlen(value) >= status_cap) err = ESP_ERR_INVALID_RESPONSE;
    else strlcpy(status, value, status_cap);
    cJSON_Delete(json);
    response_release(&response);
    return err;
}
static esp_err_t post_meeting_action(const char *recording_id, const char *action,
                                     const char *payload, int expected_a, int expected_b) {
    char path[MEETING_SERVICE_BASE_PATH_CAPACITY + MEETING_SERVICE_RECORDING_ID_CAPACITY + 32];
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    int length = snprintf(path, sizeof(path), "%s/%s/%s", base_path, recording_id, action);
    if (length <= 0 || length >= (int)sizeof(path)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("POST", path, "application/json", payload, strlen(payload),
                                          MEETING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || (response.status != expected_a && response.status != expected_b)) {
        ESP_LOGE(TAG, "meeting %s failed: err=%s status=%d body=%s",
                 action, esp_err_to_name(err), response.status, response.data ? response.data : "");
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    response_release(&response);
    return ESP_OK;
}

static void on_wake_word(void *arg) {
    (void)arg;
    if (!s_startup_sequence_complete) {
        ESP_LOGI(TAG, "offline wake detected while startup greeting owns audio; ignored until ready");
        // The board has already retired MultiNet to safely hand this callback
        // off. Remember the one-shot teardown here; startup completion cannot
        // infer it from its earlier successful start result.
        taskENTER_CRITICAL(&s_task_state_lock);
        s_wake_restart_after_startup = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return;
    }
    bool network_available = device_connectivity_is_active_uplink_ready();
    if (device_connectivity_is_provisioning_active() || !gateway_transport_is_paired() || !network_available) {
        ESP_LOGW(TAG, "offline wake detected but online interaction is unavailable: setup=%s paired=%s network=%s",
                 device_connectivity_is_provisioning_active() ? "active" : "inactive",
                 gateway_transport_is_paired() ? "yes" : "no",
                 network_available ? "connected" : "offline");
        // Recognition is one-shot on EchoEar: the model is released before
        // this callback to make room for a possible voice worker. A rejected
        // phrase must therefore explicitly restore it, otherwise one wake
        // while Wi-Fi reconnects leaves hands-free input disabled forever.
        schedule_wake_restart();
        return;
    }
    ESP_LOGI(TAG, "offline wake accepted; starting voice interaction");
    (void)interaction_service_start_voice(false);
}

static void enter_setup_portal(void) {
    // Reconfiguration is explicitly requested by a long press. Do not erase
    // the working Wi-Fi or paired token before the replacement form is saved:
    // an accidental press, power loss, or abandoned phone session must leave
    // the device recoverable. The full setup form will atomically replace the
    // saved values and its normal save path will invalidate the old token only
    // when a new pairing code has actually been committed.
    pet("quiet");
    scene_presenter_publish_message("重新配置设备", "正在开启设置热点");
    /* Keep the existing STA up while enabling the setup AP. This avoids
     * tearing down an outstanding gateway long-poll inside the button task,
     * which previously stalled the portal before the QR screen appeared. */
    provisioning_service_start_portal(true);
}

static bool deferred_setup_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_deferred_setup_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static void deferred_setup_task(void *arg) {
    (void)arg;
    if (!s_deferred_setup_start_gate ||
        xSemaphoreTake(s_deferred_setup_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "deferred setup start gate unavailable");
        goto finish;
    }
    /* The setup operation changes Wi-Fi mode, starts HTTP/DNS tasks and paints
     * a QR page. Always run it outside the hardware button task so the GPIO
     * scanner stays responsive and networking callbacks can make progress. */
    int64_t deadline = esp_timer_get_time() + 5000000;
    while (!deferred_setup_stop_requested() && meeting_service_is_active() &&
           esp_timer_get_time() < deadline) {
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
    }
    if (deferred_setup_stop_requested()) goto finish;
    ESP_LOGI(TAG, "deferred configuration portal starting");
    enter_setup_portal();
finish:
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_retiring = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* Retire the immutable Registry identity before completion becomes visible.
     * The parent may otherwise see no task and recreate a portal coordinator
     * while the old entry can still be stopped by a later owner sweep. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_exit_status = registry_err;
    if (s_deferred_setup_task == self) s_deferred_setup_task = NULL;
    s_deferred_setup_starting = false;
    s_deferred_setup_retiring = false;
    if (registry_err != ESP_OK) {
        s_deferred_setup_stop_requested = true;
        s_deferred_setup_admission_open = false;
        s_deferred_setup_registry_retirement_failed = true;
    }
    /* If ABORT arrived while this old generation was still completing, do not
     * reuse its stopped semaphore as proof for a new task.  Publish its final
     * state and unregister first; only then create the recorded replacement. */
    restart_after_system_sleep_abort = registry_err == ESP_OK &&
        s_deferred_setup_system_sleep_restart_pending &&
        !s_deferred_setup_system_sleep_preparing &&
        s_deferred_setup_admission_open;
    if (restart_after_system_sleep_abort) {
        s_deferred_setup_system_sleep_restart_pending = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_deferred_setup_stopped) xSemaphoreGive(s_deferred_setup_stopped);
    if (restart_after_system_sleep_abort && !start_deferred_setup_task()) {
        ESP_LOGW(TAG, "cannot restore deferred setup coordinator after system-sleep abort");
    }
    vTaskDelete(NULL);
}

static esp_err_t stop_deferred_setup_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_admission_open = false;
    s_deferred_setup_stop_requested = true;
    task = s_deferred_setup_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) {
        taskENTER_CRITICAL(&s_task_state_lock);
        const esp_err_t exit_status = s_deferred_setup_exit_status;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return exit_status;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    /* A registry stop can race the creator between registration and releasing
     * the start gate. Release it here as well so the worker observes the stop
     * token rather than stranding the join on its initial semaphore wait. */
    if (s_deferred_setup_start_gate) xSemaphoreGive(s_deferred_setup_start_gate);
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_deferred_setup_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_deferred_setup_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_task_state_lock);
    const esp_err_t exit_status = s_deferred_setup_exit_status;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (exit_status != ESP_OK) return exit_status;
    ESP_LOGI(TAG, "deferred setup coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_deferred_setup_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_deferred_setup_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_deferred_setup_task(timeout_ms);
}

static bool start_deferred_setup_task(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_deferred_setup_admission_open &&
                          !s_deferred_setup_registry_retirement_failed &&
                          !s_deferred_setup_system_sleep_preparing;
    bool already_starting = s_deferred_setup_task != NULL || s_deferred_setup_starting;
    if (admission_open && !already_starting) s_deferred_setup_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "deferred setup start rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_deferred_setup_start_gate || !s_deferred_setup_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_deferred_setup_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "deferred setup lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_deferred_setup_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_stop_requested = false;
    s_deferred_setup_exit_status = ESP_OK;
    s_deferred_setup_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    TaskHandle_t task = NULL;
    if (xTaskCreate(deferred_setup_task, "maclaw_setup_wait", 12288, NULL, 5, &task) != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_deferred_setup_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot create configuration portal worker");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "deferred_setup",
        .context = (void *)task,
        .stop = stop_deferred_setup_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register deferred setup coordinator: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_deferred_setup_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_deferred_setup_start_gate);
        (void)stop_deferred_setup_task(500);
        return false;
    }
    xSemaphoreGive(s_deferred_setup_start_gate);
    return true;
}

/* The deferred coordinator waits up to five seconds before it changes radio
 * mode and starts the portal.  Portal-level PREPARE only observes a live
 * portal generation, so this root fence owns the earlier wait/create window.
 * It uses the task's existing cooperative join and records only a generation
 * that existed before PREPARE; ABORT never turns an otherwise idle device into
 * a configuration request. */
static device_status_t prepare_deferred_setup_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    bool was_running = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_deferred_setup_system_sleep_preparing || s_deferred_setup_starting) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_deferred_setup_system_sleep_preparing = true;
    s_deferred_setup_system_sleep_was_admitted = s_deferred_setup_admission_open;
    s_deferred_setup_admission_open = false;
    was_running = s_deferred_setup_task != NULL;
    s_deferred_setup_system_sleep_was_running = was_running;
    taskEXIT_CRITICAL(&s_task_state_lock);

    if (!was_running) return DEVICE_STATUS_OK;
    esp_err_t stop_err = stop_deferred_setup_task(timeout_ms);
    /* Retain portal-coordinator admission closure until the parent rollback
     * restores the pre-PREPARE generation. */
    return startup_status_from_esp_err(stop_err);
}

static void abort_deferred_setup_system_sleep_prepare(void) {
    bool restart = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_deferred_setup_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return;
    }
    restart = s_deferred_setup_system_sleep_was_running;
    s_deferred_setup_admission_open = !s_deferred_setup_registry_retirement_failed &&
                                      s_deferred_setup_system_sleep_was_admitted;
    s_deferred_setup_system_sleep_was_running = false;
    s_deferred_setup_system_sleep_was_admitted = false;
    s_deferred_setup_system_sleep_preparing = false;
    /* A failed bounded join leaves the recorded old task as the only safe
     * owner of a later replacement. Its finish path consumes this marker. */
    if (restart && (s_deferred_setup_task || s_deferred_setup_starting ||
                    s_deferred_setup_retiring)) {
        s_deferred_setup_system_sleep_restart_pending = true;
        restart = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (restart && !start_deferred_setup_task()) {
        ESP_LOGW(TAG, "cannot restore deferred setup coordinator after system-sleep abort");
    }
}

// Alarm ringing is a local safety-critical foreground owner. The scheduler
// invokes this from its own task immediately before it begins an attempt.
// Keep it lock-free: each board audio HAL observes the request at its next
// bounded PCM write, releases its current transaction, and lets the alarm
// acquire audio without business logic knowing the physical codec or display.
static void on_alarm_ring_start(void *arg) {
    (void)arg;
    audio_arbitration_request_playback_stop();
}

/* The classifier owns only sensor evidence and a bounded confirmation timer.
 * Presentation is kept at the application boundary, so it stays identical
 * across touch, buttons, round panels and rectangular panels.  In particular,
 * this wording deliberately does not diagnose a person: an un-worn device can
 * only establish that the device may have fallen. */
static void on_fall_detection_event(fall_detection_event_t event, void *arg) {
    (void)arg;
    (void)device_power_wake_display_from_schedule();
    if (event == FALL_DETECTION_EVENT_SUSPECTED) {
        scene_presenter_publish_message("疑似设备跌落", "请点击取消");
        return;
    }
    scene_presenter_publish_message("疑似设备跌落", "未收到本机取消");
}

static void on_schedule_display_wake(device_status_t status, void *arg) {
    (void)arg;
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "schedule DISPLAY_OFF wake was not completed: status=%d", (int)status);
        return;
    }
    /* Schedule policy and Device Power remain shared services. Composition
     * alone connects their completed event to the shared UI owner, so no
     * schedule/profile code reaches a concrete renderer. */
    scene_presenter_note_schedule_display_wake();
}

static void on_app_intent(const app_intent_event_t *event, void *arg) {
    (void)arg;
    /* All business dispatch lives in the presentation-layer input binding;
     * this callback only hands the abstracted intent event over. */
    input_binding_handle_event(event);
}

static bool input_host_startup_sequence_complete(void) {
    return s_startup_sequence_complete;
}

static bool input_host_wifi_configured(void) {
    return s_wifi_ssid[0] != '\0';
}

static int32_t input_host_persist_output_volume(uint8_t percent) {
    return (int32_t)persist_output_volume(percent);
}

static bool input_host_start_deferred_setup(void) {
    return start_deferred_setup_task();
}

static bool input_host_safe_mode_active(void) {
    return s_safe_mode_active;
}

static const input_binding_host_t s_input_binding_host = {
    .startup_sequence_complete = input_host_startup_sequence_complete,
    .wifi_configured = input_host_wifi_configured,
    .persist_output_volume = input_host_persist_output_volume,
    .start_deferred_setup = input_host_start_deferred_setup,
    .safe_mode_active = input_host_safe_mode_active,
};

/* This is deliberately an explicit transaction rather than ESP_ERROR_CHECK:
 * a failed cold network start must enter the composition root's degraded
 * rollback path, not reboot into the same partial allocation indefinitely.
 * The private network-core owner records ESP-NETIF/default-loop singleton
 * state independently, so a partial generation is never restartable. */
static esp_err_t init_network_core(void) {
    if (connectivity_network_root_owner_core_ready()) {
        return ESP_OK;
    }
    if (connectivity_network_root_owner_has_resources()) {
        ESP_LOGW(TAG, "network core start rejected: prior partial generation still owns resources");
        return ESP_ERR_INVALID_STATE;
    }
    device_status_t connectivity_init_status = device_connectivity_initialize();
    if (connectivity_init_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "Connectivity Service initialization failed: device status=%d",
                 (int)connectivity_init_status);
        return device_status_to_platform_error(connectivity_init_status);
    }
    /* Connectivity Service owns system-sleep admission/drain.  Keep the
     * concrete client/task cancellation bridge in this composition root so
     * neither Device API nor Power Service acquires ESP HTTP/RTOS knowledge. */
    connectivity_service_set_system_sleep_request_canceller(
        cancel_gateway_requests_for_system_sleep, NULL);
    connectivity_service_set_system_sleep_request_resumer(
        resume_gateway_workers_after_system_sleep_abort, NULL);

    const connectivity_network_root_owner_lifecycle_host_t lifecycle_host = {
        .stop_provisioning = network_root_stop_provisioning,
        .stop_callback_admission = network_root_stop_callback_admission,
        .stop_clock_sync = network_root_stop_clock_sync,
        .provisioning_has_live_resources = network_root_provisioning_has_live_resources,
        .context = NULL,
    };
    device_status_t lifecycle_host_status =
        connectivity_network_root_owner_configure_lifecycle_host(&lifecycle_host);
    if (lifecycle_host_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "network physical-root lifecycle bridge failed: device status=%d",
                 (int)lifecycle_host_status);
        return device_status_to_platform_error(lifecycle_host_status);
    }

    device_status_t core_status = connectivity_network_root_owner_ensure_core();
    if (core_status != DEVICE_STATUS_OK) {
        /* Connectivity's logical EventGroup has no useful lifetime without
         * the physical netif root. Do not retain it after a failed first
         * allocation and accidentally treat a later retry as the same Wi-Fi
         * generation. */
        if (connectivity_network_root_owner_has_resources()) {
            esp_err_t rollback_err = stop_connectivity_root_transaction(500);
            if (rollback_err != ESP_OK) {
                ESP_LOGW(TAG, "network-core rollback after singleton init failure incomplete: %s",
                         esp_err_to_name(rollback_err));
            }
        } else {
            device_status_t stop_status = device_connectivity_deinit(500);
            if (stop_status != DEVICE_STATUS_OK) {
                ESP_LOGW(TAG, "cannot stop Connectivity after network-core init failure: %d",
                         (int)stop_status);
            }
        }
        return device_status_to_platform_error(core_status);
    }
    return ESP_OK;
}

/* Wi-Fi driver initialization is likewise a bounded transaction.  Event
 * handlers are registered one by one by ESP-IDF, hence a later registration
 * failure must remove each earlier instance before deinitializing the driver.
 * This gives the cold-start rollback a truthful ownership picture. */
static esp_err_t init_network(void) {
    esp_err_t err = init_network_core();
    if (err != ESP_OK) return err;
    if (connectivity_network_root_owner_wifi_has_resources()) {
        /* A retained partial driver generation must go through the common
         * teardown; treating it as ready would leave some Wi-Fi/IP events
         * unregistered or routed at stale callback state. */
        return connectivity_network_root_owner_wifi_ready() ? ESP_OK : ESP_ERR_INVALID_STATE;
    }
    device_status_t driver_status =
        connectivity_network_root_owner_initialize_wifi(wifi_event, NULL);
    if (driver_status != DEVICE_STATUS_OK) {
        /* Driver creation/handler registration follows the singleton core;
         * common root teardown preserves any partial physical generation. */
        ESP_LOGW(TAG, "Wi-Fi driver initialization failed: device status=%d",
                 (int)driver_status);
        esp_err_t rollback_err = stop_connectivity_root_transaction(500);
        if (rollback_err != ESP_OK) {
            ESP_LOGW(TAG, "network-core rollback after Wi-Fi driver init failure incomplete: %s",
                     esp_err_to_name(rollback_err));
        }
        return device_status_to_platform_error(driver_status);
    }
    /* The default loop is physically owned here, while Connectivity owns the
     * lifecycle admission that prevents a queued callback from crossing a
     * System Sleep or teardown boundary. */
    connectivity_service_open_wifi_event_callback_admission();
    return ESP_OK;
}

static void setup_qrcode_display(esp_qrcode_handle_t qrcode, void *user_data) {
    if (!qrcode) return;
    const int size = esp_qrcode_get_size(qrcode);
    if (size <= 0 || size > 177) return;
    const size_t module_count = (size_t)size * (size_t)size;
    uint8_t *modules = malloc(module_count);
    if (!modules) {
        ESP_LOGW(TAG, "cannot allocate setup QR module matrix");
        return;
    }
    for (int y = 0; y < size; ++y) {
        for (int x = 0; x < size; ++x) {
            modules[(size_t)y * (size_t)size + (size_t)x] =
                esp_qrcode_get_module(qrcode, x, y) ? 1u : 0u;
        }
    }
    const bool shown = scene_presenter_publish_setup_qr(
        modules, module_count, user_data ? (const char *)user_data : NULL);
    free(modules);
    if (!shown) ESP_LOGW(TAG, "cannot publish setup QR module matrix");
}

static void show_setup_qrcode(const char *ssid, const char *passphrase) {
    // Standard WPA/WPA2 Wi-Fi QR payload. The per-portal passphrase is
    // generated by Provisioning Service and must never be persisted or logged.
    char payload[128];
    int length = snprintf(payload, sizeof(payload), "WIFI:T:WPA;S:%s;P:%s;;",
                          ssid ? ssid : "", passphrase ? passphrase : "");
    if (length < 0 || length >= sizeof(payload)) {
        ESP_LOGW(TAG, "setup SSID is too long for QR payload");
        return;
    }
    esp_qrcode_config_t config = ESP_QRCODE_CONFIG_DEFAULT();
    config.display_func_with_cb = setup_qrcode_display;
    config.user_data = (void *)ssid;
    config.max_qrcode_version = 5;
    config.qrcode_ecc_level = ESP_QRCODE_ECC_MED;
    esp_err_t err = esp_qrcode_generate(&config, payload);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "cannot generate setup Wi-Fi QR: %s", esp_err_to_name(err));
        scene_presenter_publish_message("设备网络设置", ssid);
    }
}


/* Wi-Fi core teardown is deliberately a separate, stronger lifecycle level
 * than portal stop: it is used only by failed cold-start rollback, after all
 * Connectivity Registry workers have joined.  A normal portal close keeps
 * AP/STA and the driver alive so it can remain a low-risk runtime operation.
 *
 * The order follows ESP-IDF ownership: stop application SNTP users; stop the
 * radio; unregister our application event instances; detach/destroy default
 * Wi-Fi netifs; deinit Wi-Fi; then delete the default event loop and esp-netif
 * core. On an error we retain the corresponding ownership flag/handle and
 * return immediately, never claiming this partially stopped stack can be
 * restarted safely. */
static esp_err_t stop_network_core_transaction(uint32_t timeout_ms) {
    bool wifi_radio_stopped = false;
    device_status_t status = connectivity_network_root_owner_stop(
        timeout_ms, &wifi_radio_stopped);
    if (wifi_radio_stopped) device_connectivity_set_wifi_ready(false);
    return status == DEVICE_STATUS_OK ? ESP_OK : device_status_to_platform_error(status);
}

/* Keep lifecycle layering explicit: the composition root first stops all
 * ESP-IDF Wi-Fi/SNTP/netif/event-loop resources, then and only then lets the
 * hardware-neutral Connectivity Service delete its attempt EventGroup.  A
 * failed physical stop intentionally retains the logical generation: live
 * registered callbacks may still need that state and a new generation would
 * be unsafe. */
static esp_err_t stop_connectivity_root_transaction(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    const uint32_t network_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (network_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t err = stop_network_core_transaction(network_timeout_ms);
    if (err != ESP_OK) return err;
    const uint32_t service_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (service_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    device_status_t service_status = device_connectivity_deinit(service_timeout_ms);
    if (service_status == DEVICE_STATUS_OK) return ESP_OK;
    ESP_LOGW(TAG, "cannot stop Connectivity Service after physical network stop: %d",
             (int)service_status);
    return device_status_to_platform_error(service_status);
}

static device_status_t provisioning_host_init_network(void) {
    return startup_status_from_esp_err(init_network());
}

static device_status_t provisioning_host_ensure_ap_netif(void) {
    return provisioning_network_owner_ensure_setup_ap();
}

static bool provisioning_host_ap_netif_ready(void) {
    return provisioning_network_owner_setup_ap_ready();
}

static device_status_t provisioning_host_configure_ap_dhcp(void) {
    return provisioning_network_owner_configure_setup_ap_dhcp();
}

/* A portal may run in APSTA only so this device can retain its own authenticated
 * station backhaul during pairing recovery.  It must never turn the temporary
 * setup AP into an Internet gateway: no IP forwarding, no NAT/NAPT, no port
 * maps.  These Kconfig facts are compile-time routing policy, while the
 * explicit runtime disable makes the one-NAPT-netif API fail closed if a
 * future profile changes its defaults. */
static device_status_t provisioning_host_verify_ap_client_isolation(void) {
    return provisioning_network_owner_verify_setup_ap_isolation();
}

static device_status_t provisioning_host_ensure_sta_netif(void) {
    return provisioning_network_owner_ensure_station();
}

static bool provisioning_host_sta_netif_ready(void) {
    return provisioning_network_owner_station_ready();
}

static bool provisioning_host_wifi_started(void) {
    return connectivity_wifi_driver_owner_started();
}

static void provisioning_host_set_wifi_started(bool started) {
    /* Provisioning calls this only after its own wifi_start callback succeeds.
     * The physical owner is authoritative, so do not synthesize radio state.
     * Starting a radio is not evidence that STA already owns an IP address. */
    (void)started;
}

static void provisioning_host_set_station_policy(bool auto_connect, bool expected_disconnect) {
    connectivity_wifi_driver_owner_set_station_policy(auto_connect, expected_disconnect);
}

static device_status_t provisioning_host_wifi_disconnect(void) {
    device_status_t status = connectivity_wifi_driver_owner_disconnect();
    return status == DEVICE_STATUS_NOT_FOUND ? DEVICE_STATUS_UNAVAILABLE : status;
}

static device_status_t provisioning_host_wifi_set_mode(bool ap_only) {
    return connectivity_wifi_driver_owner_set_mode(ap_only);
}

static device_status_t provisioning_host_wifi_configure_protected_ap(const char *ssid,
                                                                       const char *passphrase) {
    return connectivity_wifi_driver_owner_configure_protected_ap(ssid, passphrase);
}

static device_status_t provisioning_host_wifi_disable_ps(void) {
    return connectivity_wifi_driver_owner_disable_power_save();
}

static device_status_t provisioning_host_wifi_start(void) {
    return connectivity_wifi_driver_owner_start();
}

static device_status_t provisioning_host_wifi_connect(void) {
    device_status_t status = connectivity_wifi_driver_owner_connect();
    return status == DEVICE_STATUS_BUSY ? DEVICE_STATUS_BUSY : status;
}

static device_status_t provisioning_host_wifi_confirm_ap_mode(void) {
    return connectivity_wifi_driver_owner_confirm_ap_mode();
}

static device_status_t provisioning_host_read_softap_mac(uint8_t mac[6]) {
    return connectivity_wifi_driver_owner_read_softap_mac(mac);
}

static device_status_t provisioning_host_capture_radio(provisioning_radio_token_t *token) {
    return connectivity_wifi_driver_owner_capture_portal_radio(token);
}

static void provisioning_host_note_radio_changed(provisioning_radio_token_t token) {
    connectivity_wifi_driver_owner_note_portal_radio_changed(token);
}

static device_status_t provisioning_host_restore_radio(provisioning_radio_token_t token) {
    return connectivity_wifi_driver_owner_restore_portal_radio(token);
}

static provisioning_scan_security_t provisioning_host_scan_security_from_driver(
    connectivity_wifi_driver_security_t security) {
    switch (security) {
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_OPEN: return PROVISIONING_SCAN_SECURITY_OPEN;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WEP: return PROVISIONING_SCAN_SECURITY_WEP;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA: return PROVISIONING_SCAN_SECURITY_WPA;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2: return PROVISIONING_SCAN_SECURITY_WPA2;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA_WPA2:
            return PROVISIONING_SCAN_SECURITY_WPA_WPA2;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA3: return PROVISIONING_SCAN_SECURITY_WPA3;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2_WPA3:
            return PROVISIONING_SCAN_SECURITY_WPA2_WPA3;
        case CONNECTIVITY_WIFI_DRIVER_SECURITY_ENTERPRISE:
            return PROVISIONING_SCAN_SECURITY_ENTERPRISE;
        default: return PROVISIONING_SCAN_SECURITY_SECURED;
    }
}

typedef struct {
    provisioning_scan_observer_t observer;
    void *context;
} provisioning_host_scan_context_t;

static bool provisioning_host_scan_adapter(const char *ssid, int8_t rssi,
                                           connectivity_wifi_driver_security_t security,
                                           void *context) {
    provisioning_host_scan_context_t *host_context = context;
    return host_context && host_context->observer &&
           host_context->observer(ssid, rssi,
                                  provisioning_host_scan_security_from_driver(security),
                                  host_context->context);
}

static device_status_t provisioning_host_scan_visible_wifi(
    uint32_t maximum_records, provisioning_scan_observer_t observer, void *context) {
    if (!observer) return DEVICE_STATUS_INVALID_ARGUMENT;
    provisioning_host_scan_context_t host_context = {
        .observer = observer,
        .context = context,
    };
    return connectivity_wifi_driver_owner_scan_visible(
        maximum_records, provisioning_host_scan_adapter, &host_context);
}

static device_status_t provisioning_host_wake_word_stop(void) {
    esp_err_t err = audio_wake_word_stop();
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return startup_status_from_esp_err(err);
}

static device_status_t provisioning_host_wake_word_start(void) {
    esp_err_t err = audio_wake_word_start(on_wake_word, NULL);
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return startup_status_from_esp_err(err);
}

static void provisioning_host_show_text(const char *title, const char *body) {
    scene_presenter_publish_message(title, body);
}

static void provisioning_host_show_qr(const char *ap_ssid, const char *ap_passphrase) {
    show_setup_qrcode(ap_ssid, ap_passphrase);
}

static void provisioning_host_copy_runtime_wifi(provisioning_runtime_wifi_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    strlcpy(out->wifi_ssid, s_wifi_ssid, sizeof(out->wifi_ssid));
    strlcpy(out->wifi_password, s_wifi_password, sizeof(out->wifi_password));
    strlcpy(out->wifi_security, s_wifi_security, sizeof(out->wifi_security));
    strlcpy(out->wifi_eap_method, s_wifi_eap_method, sizeof(out->wifi_eap_method));
    strlcpy(out->wifi_identity, s_wifi_identity, sizeof(out->wifi_identity));
    strlcpy(out->wifi_username, s_wifi_username, sizeof(out->wifi_username));
    strlcpy(out->wifi_ttls_phase2, s_wifi_ttls_phase2, sizeof(out->wifi_ttls_phase2));
    strlcpy(out->wifi_ca_mode, s_wifi_ca_mode, sizeof(out->wifi_ca_mode));
    strlcpy(out->wifi_server_domain, s_wifi_server_domain, sizeof(out->wifi_server_domain));
    (void)gateway_transport_gateway_url(out->gateway_url, sizeof(out->gateway_url));
}

static void provisioning_host_sync_runtime_after_network_delete(const char *ssid) {
    (void)configuration_service_list_wifi_networks(
        s_wifi_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &s_wifi_network_count);
    if (ssid && !strcmp(ssid, s_wifi_ssid) && !is_enterprise_wifi()) {
        s_wifi_ssid[0] = '\0';
        s_wifi_password[0] = '\0';
    }
}

static const char *provisioning_host_preferred_scan_ssid(void) {
    return s_wifi_ssid;
}

static const provisioning_service_host_t s_provisioning_service_host = {
    .init_network = provisioning_host_init_network,
    .ensure_ap_netif = provisioning_host_ensure_ap_netif,
    .ap_netif_ready = provisioning_host_ap_netif_ready,
    .configure_ap_dhcp = provisioning_host_configure_ap_dhcp,
    .verify_ap_client_isolation = provisioning_host_verify_ap_client_isolation,
    .ensure_sta_netif = provisioning_host_ensure_sta_netif,
    .sta_netif_ready = provisioning_host_sta_netif_ready,
    .wifi_started = provisioning_host_wifi_started,
    .set_wifi_started = provisioning_host_set_wifi_started,
    .set_station_policy = provisioning_host_set_station_policy,
    .wifi_disconnect = provisioning_host_wifi_disconnect,
    .wifi_set_mode = provisioning_host_wifi_set_mode,
    .wifi_configure_protected_ap = provisioning_host_wifi_configure_protected_ap,
    .wifi_disable_ps = provisioning_host_wifi_disable_ps,
    .wifi_start = provisioning_host_wifi_start,
    .wifi_connect = provisioning_host_wifi_connect,
    .wifi_confirm_ap_mode = provisioning_host_wifi_confirm_ap_mode,
    .read_softap_mac = provisioning_host_read_softap_mac,
    .capture_radio = provisioning_host_capture_radio,
    .note_radio_changed = provisioning_host_note_radio_changed,
    .restore_radio = provisioning_host_restore_radio,
    .scan_visible_wifi = provisioning_host_scan_visible_wifi,
    .wake_word_stop = provisioning_host_wake_word_stop,
    .wake_word_start = provisioning_host_wake_word_start,
    .show_text = provisioning_host_show_text,
    .show_qr = provisioning_host_show_qr,
    .copy_runtime_wifi = provisioning_host_copy_runtime_wifi,
    .sync_runtime_after_network_delete = provisioning_host_sync_runtime_after_network_delete,
    .preferred_scan_ssid = provisioning_host_preferred_scan_ssid,
};

static void wifi_event(void *arg, const connectivity_wifi_driver_event_t *event) {
    (void)arg;
    if (!event || !connectivity_service_wifi_event_callback_enter()) return;
    if (event->kind == CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_CONNECTED) {
        ESP_LOGI(TAG, "setup client associated: %02X:%02X:%02X:%02X:%02X:%02X",
                 event->mac[0], event->mac[1], event->mac[2], event->mac[3],
                 event->mac[4], event->mac[5]);
        goto finish;
    }
    if (event->kind == CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_LEASED) {
        ESP_LOGI(TAG, "setup client leased IP=%s hostname=%s",
                 event->ipv4[0] ? event->ipv4 : "unknown",
                 event->hostname[0] ? event->hostname : "unknown");
        goto finish;
    }
    if (event->kind == CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_STARTED) {
        if (connectivity_wifi_driver_owner_should_auto_connect()) {
            (void)connectivity_wifi_driver_owner_connect();
        }
        goto finish;
    }
    if (event->kind == CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_DISCONNECTED) {
        bool accepted = device_connectivity_observe_wifi_disconnected(event->ssid);
        if (!accepted) {
            ESP_LOGW(TAG, "ignoring Wi-Fi disconnect outside current attempt");
            goto finish;
        }
        ambient_service_apply_network(s_wifi_ssid, false);
        scene_presenter_publish_service_ready(false);
        firmware_identity_set_service_ready(false);
        if (connectivity_wifi_driver_owner_take_expected_disconnect()) {
            ESP_LOGI(TAG, "station disconnected for setup scan");
            goto finish;
        }
        if (connectivity_wifi_driver_owner_should_auto_connect()) {
            ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retrying", s_wifi_ssid);
            (void)connectivity_wifi_driver_owner_connect();
        }
        goto finish;
    }
    if (event->kind == CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_GOT_IP) {
        bool accepted = false;
        char connected_ssid[WIFI_VALUE_CAPACITY] = {0};
        if (connectivity_wifi_driver_owner_current_station_ssid(
                connected_ssid, sizeof(connected_ssid)) == DEVICE_STATUS_OK) {
            accepted = device_connectivity_observe_wifi_got_ip(connected_ssid);
        } else {
            /* Fail closed: an IP event without a live association identity
             * cannot be attributed to a bounded connection attempt. */
            ESP_LOGW(TAG, "Wi-Fi got IP but cannot read associated SSID");
        }
        if (!accepted) {
            ESP_LOGW(TAG, "ignoring DHCP event outside current Wi-Fi attempt");
            goto finish;
        }
        // The normal status surface is still covered by the explicit startup
        // screen here. Avoid a full LCD transfer from the IP event loop; the
        // ready transition will publish the connected state after handshake.
        ESP_LOGI(TAG, "Wi-Fi connected to %s", s_wifi_ssid);
        cellular_recovery_service_note_wifi_ready();
    }
finish:
    connectivity_service_wifi_event_callback_leave();
}

/* Value-only Power bridge for the one remaining composition-root storage
 * worker.  It never moves queue/task details upward: PREPARE closes admission
 * while holding the requester mutex, queues a fence after any older accepted
 * write, and waits for the retained worker to acknowledge it.  No operation
 * accepted before the marker is replayed by ABORT; it either completed before
 * the fence or its caller receives its original bounded result. */
static device_status_t prepare_output_volume_persist_system_sleep(uint32_t timeout_ms,
                                                                   void *context) {
    (void)context;
    if (timeout_ms == 0 || !s_output_volume_persist_queue ||
        !s_output_volume_persist_request_mutex ||
        !s_output_volume_persist_system_sleep_quiesced) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool unavailable = s_output_volume_persist_stop_requested ||
                             s_output_volume_persist_registry_retirement_failed ||
                             !s_output_volume_persist_task_handle ||
                             s_output_volume_persist_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (unavailable) return DEVICE_STATUS_BUSY;

    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0 ||
        xSemaphoreTake(s_output_volume_persist_request_mutex,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    taskENTER_CRITICAL(&s_task_state_lock);
    const bool still_available = !s_output_volume_persist_stop_requested &&
                                 !s_output_volume_persist_registry_retirement_failed &&
                                 s_output_volume_persist_task_handle &&
                                 !s_output_volume_persist_system_sleep_preparing;
    if (still_available) s_output_volume_persist_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!still_available) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return DEVICE_STATUS_BUSY;
    }

    /* A previous failed PREPARE cannot leave an acknowledgement that would
     * incorrectly prove this generation is quiet.  No caller can queue while
     * this bridge owns the request mutex. */
    while (xSemaphoreTake(s_output_volume_persist_system_sleep_quiesced, 0) == pdTRUE) {}
    output_volume_persist_request_t fence = {.system_sleep_prepare = true};
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    BaseType_t queued = remaining_ms
                            ? xQueueSend(s_output_volume_persist_queue, &fence,
                                         pdMS_TO_TICKS(remaining_ms))
                            : pdFALSE;
    if (queued != pdTRUE) {
        /* No post-PREPARE publisher may cross a failed fence.  Release only
         * the requester mutex; Power's storage-bridge ABORT owns reopening
         * the admission marker. */
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms != 0 &&
        xSemaphoreTake(s_output_volume_persist_system_sleep_quiesced,
                       pdMS_TO_TICKS(remaining_ms)) == pdTRUE) {
        xSemaphoreGive(s_output_volume_persist_request_mutex);
        return DEVICE_STATUS_OK;
    }

    /* The fence may be selected just after the timeout. Keep admission closed
     * until Power's storage-bridge ABORT; the worker performs no mutation for
     * this marker and the mutex excluded later publishers before it was sent. */
    xSemaphoreGive(s_output_volume_persist_request_mutex);
    return DEVICE_STATUS_TIMEOUT;
}

static void abort_output_volume_persist_system_sleep_prepare(void *context) {
    (void)context;
    taskENTER_CRITICAL(&s_task_state_lock);
    /* An unretired Storage identity is terminal for this boot.  ABORT may
     * reopen a reversible PREPARE fence, never a domain whose predecessor can
     * still be selected by the Task Registry. */
    if (!s_output_volume_persist_registry_retirement_failed) {
        s_output_volume_persist_system_sleep_preparing = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
}

static void cellular_recovery_publish_network_ready(bool ready, void *context) {
    (void)context;
    ambient_service_apply_network("4G", ready);
    scene_presenter_publish_service_ready(ready);
    firmware_identity_set_service_ready(ready);
}

static bool cellular_recovery_gateway_startup_running(void *context) {
    (void)context;
    return gateway_transport_startup_running();
}

static bool cellular_recovery_wifi_gateway_startup_recovery_allowed(void *context) {
    (void)context;
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool allowed = s_gateway_startup_allowed && !s_startup_sequence_complete;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return allowed;
}

static bool cellular_recovery_gateway_startup_eligible(void *context) {
    (void)context;
    return gateway_transport_is_paired() || gateway_transport_pairing_pending();
}

static bool cellular_recovery_start_gateway_startup(void *context) {
    (void)context;
    return gateway_transport_start_startup_task();
}

static const cellular_recovery_service_host_t s_cellular_recovery_service_host = {
    .struct_size = sizeof(cellular_recovery_service_host_t),
    .publish_network_ready = cellular_recovery_publish_network_ready,
    .gateway_startup_running = cellular_recovery_gateway_startup_running,
    .wifi_gateway_startup_recovery_allowed =
        cellular_recovery_wifi_gateway_startup_recovery_allowed,
    .gateway_startup_eligible = cellular_recovery_gateway_startup_eligible,
    .start_gateway_startup = cellular_recovery_start_gateway_startup,
};

static bool start_cellular(void) {
    /* The common service owns the actual fail-closed readiness transition.
     * Keep this capability preflight solely for the specific user-facing
     * "module is not configured" diagnostic below. */
    device_status_t preparation =
        device_profile_has_capability(DEVICE_CAPABILITY_CELLULAR_TRANSPORT)
            ? DEVICE_STATUS_OK : DEVICE_STATUS_UNAVAILABLE;
    /* A missing cellular profile is a configuration error. Other transport
     * failures are handled below after the shared status surfaces are reset. */
    if (preparation != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cellular transport is unavailable: device status=%d",
                 preparation);
        scene_presenter_publish_message("4G 未配置", "请先确认模块 UART、供电与控制引脚");
        return false;
    }
    // ML307 is controlled through its native AT HTTP/HTTPS/TCP stack. It does
    // not implement the generic ATD*99# PPP path used by esp_modem.
    preparation = cellular_recovery_service_establish_initial(60000);
    if (preparation != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cellular transport start failed: device status=%d", preparation);
        scene_presenter_publish_message("4G 模块未响应", "检查 SIM、供电与天线");
        return false;
    }
    return true;
}

/* 多热点自动选网：扫描可见 AP，把已存热点按最强 RSSI 从强到弱排序后逐个
 * 尝试连接，某个热点拿到 IP 即返回 true。扫描失败、没有可见的已存热点或
 * 全部连接失败时返回 false，由调用方回退到原单凭据连接流程（失败后的
 * "网络暂时不可用"/配网逻辑保持不变）。 */
typedef struct {
    uint8_t order[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    int8_t best_rssi[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t candidate_count;
} saved_wifi_scan_candidates_t;

static bool collect_saved_wifi_scan_candidate(const char *ssid, int8_t rssi,
                                              connectivity_wifi_driver_security_t security,
                                              void *context) {
    (void)security;
    saved_wifi_scan_candidates_t *candidates = context;
    if (!ssid || !candidates) return false;
    for (uint8_t index = 0; index < s_wifi_network_count; ++index) {
        if (strcmp(ssid, s_wifi_networks[index].ssid) != 0) continue;
        uint8_t position = 0;
        while (position < candidates->candidate_count &&
               candidates->order[position] != index) {
            ++position;
        }
        if (position < candidates->candidate_count &&
            candidates->best_rssi[position] >= rssi) return true;
        if (position < candidates->candidate_count) {
            for (uint8_t shift = position; shift + 1 < candidates->candidate_count; ++shift) {
                candidates->order[shift] = candidates->order[shift + 1];
                candidates->best_rssi[shift] = candidates->best_rssi[shift + 1];
            }
            --candidates->candidate_count;
        }
        position = candidates->candidate_count;
        while (position > 0 && candidates->best_rssi[position - 1] < rssi) {
            candidates->order[position] = candidates->order[position - 1];
            candidates->best_rssi[position] = candidates->best_rssi[position - 1];
            --position;
        }
        candidates->order[position] = index;
        candidates->best_rssi[position] = rssi;
        ++candidates->candidate_count;
        return true;
    }
    return true;
}

static bool start_wifi_saved_list(void) {
    // 逐个尝试期间禁止断线回调自动重连，避免它抢连尚未切换的旧配置。
    connectivity_wifi_driver_owner_set_station_policy(false, false);
    if (!connectivity_wifi_driver_owner_started()) {
        device_status_t start_status = connectivity_wifi_driver_owner_start();
        if (start_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cannot start Wi-Fi for saved-network scan: device status=%d",
                     (int)start_status);
            connectivity_wifi_driver_owner_set_station_policy(true, false);
            return false;
        }
    }
    saved_wifi_scan_candidates_t candidates = {0};
    device_status_t scan_status = connectivity_wifi_driver_owner_scan_visible(
        SETUP_SCAN_MAX_APS, collect_saved_wifi_scan_candidate, &candidates);
    if (scan_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "Wi-Fi scan for saved networks failed: device status=%d",
                 (int)scan_status);
        connectivity_wifi_driver_owner_set_station_policy(true, false);
        return false;
    }
    if (!candidates.candidate_count) {
        ESP_LOGI(TAG, "no saved Wi-Fi network is currently visible");
        connectivity_wifi_driver_owner_set_station_policy(true, false);
        return false;
    }
    bool connected = false;
    for (uint8_t c = 0; c < candidates.candidate_count && !connected; ++c) {
        const configuration_wifi_network_t *network = &s_wifi_networks[candidates.order[c]];
        ESP_LOGI(TAG, "trying saved Wi-Fi %s (%d dBm)", network->ssid,
                 (int)candidates.best_rssi[c]);
        strlcpy(s_wifi_ssid, network->ssid, sizeof(s_wifi_ssid));
        strlcpy(s_wifi_password, network->password, sizeof(s_wifi_password));
        const connectivity_wifi_driver_station_config_t station_config = {
            .ssid = s_wifi_ssid,
            .password = s_wifi_password,
            .enterprise = false,
            .keep_setup_ap = provisioning_service_has_live_resources(),
        };
        device_status_t config_status =
            connectivity_wifi_driver_owner_configure_station(&station_config);
        if (config_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cannot configure saved Wi-Fi %s: device status=%d", s_wifi_ssid,
                     (int)config_status);
            continue;
        }
        ambient_service_apply_network(s_wifi_ssid, false);
        if (c > 0) {
            device_status_t disconnect_status = connectivity_wifi_driver_owner_disconnect();
            if (disconnect_status != DEVICE_STATUS_OK &&
                disconnect_status != DEVICE_STATUS_NOT_FOUND) {
                ESP_LOGW(TAG, "cannot switch saved Wi-Fi candidate: device status=%d",
                         (int)disconnect_status);
            }
        }
        /* Each candidate receives a new Connectivity-owned attempt epoch.
         * A late DHCP event for a prior candidate may update the physical
         * observation, but it cannot satisfy this candidate's waiter unless
         * it was published after the epoch became current. */
        uint32_t attempt_epoch = device_connectivity_begin_wifi_attempt(s_wifi_ssid);
        if (attempt_epoch == 0) {
            ESP_LOGE(TAG, "cannot create Wi-Fi readiness attempt");
            break;
        }
        device_status_t connect_status = connectivity_wifi_driver_owner_connect();
        if (connect_status != DEVICE_STATUS_OK && connect_status != DEVICE_STATUS_BUSY) {
            ESP_LOGW(TAG, "cannot start saved Wi-Fi attempt: device status=%d",
                     (int)connect_status);
            continue; // 发起失败直接试下一个候选
        }
        connected = device_connectivity_wait_wifi_attempt(
            attempt_epoch, WIFI_CANDIDATE_CONNECT_TIMEOUT_MS);
        if (!connected) {
            ESP_LOGW(TAG, "saved Wi-Fi %s did not connect within %u ms",
                     s_wifi_ssid, WIFI_CANDIDATE_CONNECT_TIMEOUT_MS);
        }
    }
    connectivity_wifi_driver_owner_set_station_policy(true, false);
    if (!connected) {
        // 全部失败：保持原有后台自动重连行为（以最后一个候选继续重试）。
        device_connectivity_set_wifi_ready(false);
        ambient_service_apply_network(s_wifi_ssid, false);
    }
    return connected;
}

static bool start_wifi(void) {
    /* A prior DHCP session is not evidence for this new adapter start. The
     * IP event publishes readiness only after this attempt acquires an
     * address. This is particularly important when Fangtang switches from
     * ML307 back to Wi-Fi during a recovery path. */
    esp_err_t network_init_err = init_network();
    if (network_init_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot initialize Wi-Fi transport: %s",
                 esp_err_to_name(network_init_err));
        return false;
    }
    if (provisioning_network_owner_ensure_station() != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cannot create Wi-Fi station netif");
        return false;
    }
    connectivity_wifi_driver_owner_set_station_policy(true, false);
    bool enterprise = is_enterprise_wifi();
    const connectivity_wifi_driver_station_config_t station_config = {
        .ssid = s_wifi_ssid,
        .password = s_wifi_password,
        .enterprise = enterprise,
        .keep_setup_ap = provisioning_service_has_live_resources(),
    };
    device_status_t station_status =
        connectivity_wifi_driver_owner_configure_station(&station_config);
    if (station_status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cannot configure Wi-Fi station: device status=%d",
                 (int)station_status);
        return false;
    }
    esp_err_t wifi_err = ESP_OK;
    /* 多热点：个人网络且已存列表非空时，先扫描并连接可见的最强已存热点。
     * 扫描失败、无可见已存热点或全部连接失败都会落回下方原单凭据流程，
     * 保持"网络暂时不可用"与后台重连行为不变。 */
    if (!s_boot_provisioning_staged && !enterprise && s_wifi_network_count > 0 &&
        start_wifi_saved_list()) {
        return true;
    }
    if (enterprise) {
        // Android/iOS-style defaults: PEAP + MSCHAPv2, username as identity
        // when anonymous identity is omitted, and platform trust anchors.
        const connectivity_wifi_driver_enterprise_config_t enterprise_config = {
            .identity = s_wifi_identity[0] ? s_wifi_identity : s_wifi_username,
            .username = s_wifi_username,
            .password = s_wifi_password,
            .server_domain = s_wifi_server_domain,
            .use_ttls = !strcmp(s_wifi_eap_method, "ttls"),
            .ttls_phase2_pap = !strcmp(s_wifi_ttls_phase2, "pap"),
            .use_system_ca = !strcmp(s_wifi_ca_mode, "system"),
        };
        device_status_t enterprise_status =
            connectivity_wifi_driver_owner_configure_enterprise(&enterprise_config);
        if (enterprise_status != DEVICE_STATUS_OK) {
            wifi_err = device_status_to_platform_error(enterprise_status);
            goto enterprise_config_failed;
        }
    } else {
        // Enterprise state can only exist after a prior runtime enterprise
        // connection. Do not call this API on a cold personal-Wi-Fi boot:
        // ESP-IDF 6.0.2 can assert from the scan timer in that case.
        if (connectivity_wifi_driver_owner_enterprise_enabled()) {
            device_status_t eap_status = connectivity_wifi_driver_owner_disable_enterprise();
            if (eap_status != DEVICE_STATUS_OK) {
                ESP_LOGW(TAG, "cannot disable prior enterprise Wi-Fi state: %s",
                         esp_err_to_name(device_status_to_platform_error(eap_status)));
            }
        }
    }
    /* Start a fresh Connectivity-owned readiness session before any station
     * operation can synchronously emit a DHCP event. */
    uint32_t attempt_epoch = device_connectivity_begin_wifi_attempt(s_wifi_ssid);
    if (attempt_epoch == 0) {
        ESP_LOGE(TAG, "cannot create Wi-Fi readiness attempt");
        return false;
    }
    ambient_service_apply_network(s_wifi_ssid, false);
    if (!connectivity_wifi_driver_owner_started()) {
        device_status_t start_status = connectivity_wifi_driver_owner_start();
        if (start_status != DEVICE_STATUS_OK) {
            ESP_LOGE(TAG, "cannot start Wi-Fi station: device status=%d", (int)start_status);
            return false;
        }
    } else {
        device_status_t connect_status = connectivity_wifi_driver_owner_connect();
        if (connect_status != DEVICE_STATUS_OK && connect_status != DEVICE_STATUS_BUSY) {
            ESP_LOGE(TAG, "cannot connect Wi-Fi station: device status=%d",
                     (int)connect_status);
            return false;
        }
    }
    if (device_connectivity_wait_wifi_attempt(attempt_epoch, WIFI_CONNECT_TIMEOUT_MS)) {
        return true;
    }
    ambient_service_apply_network(s_wifi_ssid, false);
    ESP_LOGW(TAG, "Wi-Fi did not connect within %u ms: %s", WIFI_CONNECT_TIMEOUT_MS, s_wifi_ssid);
    return false;

enterprise_config_failed:
    /* EAP setup can fail for malformed credentials, missing certificate
     * support, or a transient driver state.  It must not reboot the device;
     * readiness remains false and the regular Connectivity recovery policy
     * can surface the fault.  Only undo enterprise mode if this attempt had
     * actually enabled it, avoiding the IDF cold-personal-Wi-Fi disable bug. */
    ESP_LOGE(TAG, "cannot configure enterprise Wi-Fi: %s", esp_err_to_name(wifi_err));
    if (connectivity_wifi_driver_owner_enterprise_enabled()) {
        device_status_t disable_status = connectivity_wifi_driver_owner_disable_enterprise();
        if (disable_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cannot undo failed enterprise Wi-Fi setup: %s",
                     esp_err_to_name(device_status_to_platform_error(disable_status)));
        }
    }
    return false;
}

static bool ensure_alarm_manager_started(void) {
    if (s_alarm_manager_started) return true;
    device_status_t status = alarm_manager_init();
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cannot start alarm scheduler: device status=%d", (int)status);
        return false;
    }
    alarm_manager_set_ring_callback(on_alarm_ring_start, NULL);
    s_alarm_manager_started = true;
    return true;
}

/* SAFE_MODE has a deliberately narrower lifecycle than startup rollback.
 * It may run only after the root has constructed the durable/local minimum
 * set; consequently it never stops Display, Power, Persistence, Wake
 * Deadline, Alarm or the App Intent dispatcher.  Everything here is either
 * decorative/background work or a network/ordinary-interaction owner.  A
 * failure leaves admission closed and the coordinator terminally FAILED. */
static device_status_t safe_mode_quiesce_nonessential(void *context,
                                                       uint32_t timeout_ms) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining_ms = 0;
#define SAFE_MODE_NEXT_TIMEOUT(step_name)                                             \
    do {                                                                               \
        remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);            \
        if (remaining_ms == 0) {                                                       \
            ESP_LOGW(TAG, "SAFE_MODE deadline exhausted before %s", step_name);      \
            return DEVICE_STATUS_TIMEOUT;                                              \
        }                                                                              \
    } while (0)

    /* Close independent root-owned creators before their Registry owners.
     * These helper stops are terminal for this boot and do not rely on a
     * System Sleep ABORT path. */
    SAFE_MODE_NEXT_TIMEOUT("deferred setup coordinator");
    if (stop_deferred_setup_task(remaining_ms) != ESP_OK) return DEVICE_STATUS_BUSY;
    SAFE_MODE_NEXT_TIMEOUT("wake restart coordinator");
    if (stop_wake_restart_task(remaining_ms) != ESP_OK) return DEVICE_STATUS_BUSY;

    /* Decorative rendering has no SAFE_MODE role.  The profile-private
     * adapter retains its boot-lifetime hardware but its background task must
     * be joined before a diagnostic/alarm surface becomes authoritative. */
    SAFE_MODE_NEXT_TIMEOUT("board background workers");
    if (platform_lifecycle_stop_board_background_tasks(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("startup pet worker");
    if (startup_pet_worker_service_stop(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("startup pet retry");
    if (startup_pet_retry_service_stop(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("pet cache worker");
    if (pet_cache_service_stop(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }

    /* Audio and interaction ownership includes normal voice, meeting and
     * their recovery/cancel workers. Alarm feedback itself is driven by the
     * retained Alarm/Device Audio path and has no Registry AUDIO worker. */
    SAFE_MODE_NEXT_TIMEOUT("audio workers");
    if (task_registry_stop_owner(TASK_REGISTRY_OWNER_AUDIO, remaining_ms) != ESP_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("interaction workers");
    if (task_registry_stop_owner(TASK_REGISTRY_OWNER_INTERACTION, remaining_ms) != ESP_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("fall detection");
    if (fall_detection_service_deinit(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }
    SAFE_MODE_NEXT_TIMEOUT("configuration reconcile");
    if (configuration_reconcile_service_deinit(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }

    /* This composition bridge is admitted only at the late pre-uplink boot
     * boundary: Wi-Fi/4G, SNTP, provisioning and Gateway have not started.
     * The Gateway lifecycle commit still closes any pre-created logical
     * generation, but it must not use a System Sleep ABORT or attempt a
     * physical-root teardown that does not exist in this startup state. */
    SAFE_MODE_NEXT_TIMEOUT("gateway lifecycle");
    device_status_t gateway_status = gateway_lifecycle_service_prepare_system_sleep(remaining_ms);
    if (gateway_status != DEVICE_STATUS_OK) return gateway_status;
    SAFE_MODE_NEXT_TIMEOUT("Gateway terminal commit");
    gateway_status = gateway_lifecycle_service_commit_prepared_network_restart();
    if (gateway_status != DEVICE_STATUS_OK) return gateway_status;
    SAFE_MODE_NEXT_TIMEOUT("connectivity workers");
    if (task_registry_stop_owner(TASK_REGISTRY_OWNER_CONNECTIVITY, remaining_ms) != ESP_OK) {
        return DEVICE_STATUS_BUSY;
    }

#undef SAFE_MODE_NEXT_TIMEOUT
    return DEVICE_STATUS_OK;
}

static device_status_t safe_mode_initialize_clock_feedback(void *context,
                                                            uint32_t timeout_ms) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* Ambient cadence is local and retained by Power's live lifecycle.  The
     * explicit return value makes failure to establish feedback terminal for
     * this SAFE_MODE entry rather than silently showing a frozen surface. */
    const device_status_t clock_status = ambient_service_ensure_clock_task();
    if (clock_status != DEVICE_STATUS_OK) return clock_status;
    scene_presenter_publish_message("安全模式", "本地时钟和闹钟仍可用");
    return DEVICE_STATUS_OK;
}

static device_status_t safe_mode_initialize_alarm(void *context, uint32_t timeout_ms) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return ensure_alarm_manager_started() ? DEVICE_STATUS_OK : DEVICE_STATUS_INTERNAL_ERROR;
}

static device_status_t safe_mode_publish_diagnostic_surface(
    void *context, const safe_mode_entry_t *entry, uint32_t timeout_ms) {
    (void)context;
    if (!entry || timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    char detail[96];
    snprintf(detail, sizeof(detail), "本地时钟与闹钟可用（故障阶段 %d，状态 %d）",
             (int)entry->failed_phase, (int)entry->failure_status);
    scene_presenter_publish_message("安全模式", detail);
    return DEVICE_STATUS_OK;
}

static uint64_t safe_mode_now_ms(void *context) {
    (void)context;
    const int64_t now_us = esp_timer_get_time();
    return now_us <= 0 ? 0u : (uint64_t)now_us / 1000u;
}

static const safe_mode_coordinator_host_t s_safe_mode_host = {
    .now_ms = safe_mode_now_ms,
    .quiesce_nonessential = safe_mode_quiesce_nonessential,
    .initialize_clock_feedback = safe_mode_initialize_clock_feedback,
    .initialize_alarm = safe_mode_initialize_alarm,
    .publish_diagnostic_surface = safe_mode_publish_diagnostic_surface,
    .context = NULL,
};

/* This entry point is intentionally private to the late boot composition
 * boundary.  Calling it earlier would be unsafe: its retained dependencies
 * do not yet exist and normal rollback remains the only truthful response. */
static startup_safe_mode_entry_result_t startup_enter_safe_mode(
    device_runtime_phase_t phase, device_status_t status, const char *reason) {
    if (status == DEVICE_STATUS_OK || s_safe_mode_active) {
        return STARTUP_SAFE_MODE_ENTRY_NOT_STARTED;
    }
    const safe_mode_entry_t entry = {
        .struct_size = sizeof(entry),
        .abi_version = SAFE_MODE_COORDINATOR_ABI_VERSION,
        .failed_phase = phase,
        .failure_status = status,
    };
    if (safe_mode_coordinator_init(&s_safe_mode_coordinator, &s_safe_mode_host) !=
        DEVICE_STATUS_OK) {
        return STARTUP_SAFE_MODE_ENTRY_NOT_STARTED;
    }
    /* Close ordinary admission before quiescence starts.  A concurrently
     * completed gesture or Wi-Fi callback then cannot create work while the
     * bridge is isolating its fault domain. */
    taskENTER_CRITICAL(&s_task_state_lock);
    s_safe_mode_active = true;
    s_startup_sequence_complete = false;
    s_gateway_startup_allowed = false;
    s_wake_restart_admission_open = false;
    s_deferred_setup_admission_open = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    const device_status_t safe_mode_status = safe_mode_coordinator_enter(
        &s_safe_mode_coordinator, &entry, SAFE_MODE_ENTRY_TIMEOUT_MS);
    if (safe_mode_status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "SAFE_MODE entry failed: phase=%d status=%d reason=%s",
                 (int)phase, (int)safe_mode_status, reason ? reason : "unknown");
        return STARTUP_SAFE_MODE_ENTRY_TERMINAL_FAILURE;
    }
    lifecycle_service_degrade(phase, status);
    firmware_identity_set_local_ready(true);
    firmware_identity_set_service_ready(false);
    ESP_LOGE(TAG, "SAFE_MODE active: phase=%d device_status=%d reason=%s",
             (int)phase, (int)status, reason ? reason : "unknown");
    return STARTUP_SAFE_MODE_ENTRY_ACTIVE;
}

/* SAFE_MODE entry is itself a terminal transaction.  Once its coordinator has
 * started quiescing a fault domain, a later stage can fail with only a subset
 * of normal owners still alive.  Do not route that case through the ordinary
 * startup rollback: its ordering assumes an untouched normal-startup graph
 * and can otherwise stop/release an already-retired generation.  Admission is
 * already closed by startup_enter_safe_mode(); retain every remaining owner
 * fail-closed and leave the boot in a serial/UI-diagnosable terminal state. */
static void startup_enter_safe_mode_terminal_failure(device_runtime_phase_t phase,
                                                     device_status_t status,
                                                     const char *reason) {
    lifecycle_service_degrade(phase, status);
    firmware_identity_set_local_ready(false);
    firmware_identity_set_service_ready(false);
    ESP_LOGE(TAG,
             "SAFE_MODE terminal failure: phase=%d device_status=%d reason=%s; "
             "ordinary startup rollback is intentionally skipped",
             (int)phase, (int)status, reason ? reason : "unknown");
    if (s_startup_ui_initialized) {
        scene_presenter_publish_message("Startup failed",
                                        "Recovery services unavailable; connect a computer for diagnostics");
    }
}

/*
 * This is intentionally a build/profile consistency check, not hardware
 * autodetection.  The compiled profile is the only profile linked into a
 * firmware image; PCB-revision and electrical-safety validation remain a
 * later Boot Coordinator responsibility.
 */
static bool validate_compiled_board_profile(void) {
    device_profile_t compiled_profile;
    if (!device_profile_get(&compiled_profile) ||
        strcmp(compiled_profile.id, CONFIG_MACLAW_BOARD_ID) != 0) {
        ESP_LOGE(TAG, "compiled board profile is invalid or does not match board ID");
        return false;
    }
    ESP_LOGI(TAG, "board profile: %s (%ux%u, capabilities=0x%08lx)",
             compiled_profile.id, compiled_profile.display_width,
             compiled_profile.display_height, (unsigned long)compiled_profile.capabilities);
    return true;
}

static device_status_t startup_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

/* Rollback is a composition-root transaction.  Do not forward the original
 * allowance repeatedly: helpers receive only what remains of this monotonic
 * deadline.  Round up a non-zero remainder so a child can observe and return
 * a real timeout instead of treating a sub-millisecond budget as invalid. */
static uint32_t startup_rollback_remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

static void startup_rollback_deadline_exhausted(const char *next_step) {
    ESP_LOGW(TAG,
             "startup rollback deadline exhausted before %s; "
             "retaining unvisited owners fail-closed",
             next_step ? next_step : "next step");
}

/* A rollback step that cannot quiesce is a live dependency boundary, not a
 * best-effort warning. The composition root must not continue and release
 * downstream state that the retained task/callback can still enter. */
static void startup_rollback_step_blocked(const char *step, const char *detail) {
    ESP_LOGW(TAG,
             "startup rollback stopped at %s%s%s; retaining downstream owners fail-closed",
             step ? step : "unknown step",
             detail ? ": " : "",
             detail ? detail : "");
}

/* The first lifecycle slice deliberately stops before radio/gateway work when
 * a required local service cannot start.  It is not a full SAFE_MODE: it
 * merely leaves serial identity diagnostics alive and avoids a panic/reboot
 * loop that would conceal the original failing phase. */
static void startup_enter_degraded(device_runtime_phase_t phase,
                                   device_status_t status,
                                   const char *reason) {
    startup_stop_local_workers();
    lifecycle_service_degrade(phase, status);
    firmware_identity_set_local_ready(false);
    firmware_identity_set_service_ready(false);
    ESP_LOGE(TAG, "startup degraded: phase=%d device_status=%d reason=%s",
             (int)phase, (int)status, reason ? reason : "unknown");
    if (s_startup_ui_initialized) {
        scene_presenter_publish_message("Startup failed", "Local service unavailable; connect a computer for diagnostics");
    }
}

/* These two workers own no board driver and have explicit stop sentinels, so
 * they can be joined safely when startup fails after configuration validation.
 * Other services are intentionally not force-deleted here: their lifecycle
 * contracts are still being migrated and may own timers, I/O, or callbacks. */
static void startup_stop_local_workers(void) {
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)STARTUP_ROLLBACK_TIMEOUT_MS * 1000;
    uint32_t timeout_ms = 0;
#define STARTUP_ROLLBACK_NEXT_TIMEOUT(step_name)                                      \
    do {                                                                               \
        timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);              \
        if (timeout_ms == 0) {                                                         \
            startup_rollback_deadline_exhausted(step_name);                            \
            return;                                                                    \
        }                                                                              \
    } while (0)
    /* A post-save coordinator may be between response flush and terminal
     * portal cleanup. Drain it before the generic CONNECTIVITY Registry sweep;
     * otherwise two owners could stop the same HTTP/DNS generation. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("provisioning transaction");
    device_status_t provisioning_stop_status =
        connectivity_network_root_owner_stop_provisioning(timeout_ms);
    if (provisioning_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "provisioning transaction did not stop during startup rollback: status=%d",
                 (int)provisioning_stop_status);
        startup_rollback_step_blocked("provisioning transaction", NULL);
        return;
    }

    /* Close the publisher before draining its persistence workers.  The call
     * is idempotent, so it is also safe on early failures before Input Service
     * was started.  Never delete a foreground interaction here: it has its
     * own operation/cancel protocol and is not yet part of this rollback set. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("input service");
    device_status_t input_stop_status = app_intent_service_stop(timeout_ms);
    if (input_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "input service did not stop during startup rollback: %d",
                 (int)input_stop_status);
        startup_rollback_step_blocked("input service", NULL);
        return;
    }

    /* Decorative board tasks may hold the LCD mutex and continue rendering
     * after the shared local services have entered DEGRADED. Stop/join the
     * explicitly covered animation workers before Power Service can tear down
     * display-off timing; this is intentionally not a full board deinit. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("board background workers");
    device_status_t board_background_stop_status =
        platform_lifecycle_stop_board_background_tasks(timeout_ms);
    if (board_background_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "board background tasks did not stop during startup rollback: %d",
                 (int)board_background_stop_status);
        startup_rollback_step_blocked("board background workers", NULL);
        return;
    }

    /* Display Service owns the only task that can enter Platform Display.
     * Stop it after board-local animation workers have released their LCD
     * borrowing, but before Power can tear down display-off scheduling. The
     * selected renderer/panel remain boot-lifetime diagnostic resources; this
     * is task quiescence, not a restartable display deinit. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Display Service");
    esp_err_t display_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_BOARD, timeout_ms);
    if (display_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "Display Service did not stop during startup rollback: %s",
                 esp_err_to_name(display_stop_err));
        startup_rollback_step_blocked("Display Service", NULL);
        return;
    }

    STARTUP_ROLLBACK_NEXT_TIMEOUT("Power registry workers");
    esp_err_t power_worker_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_POWER, timeout_ms);
    if (power_worker_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "registered power workers did not stop during startup rollback: %s",
                 esp_err_to_name(power_worker_stop_err));
        startup_rollback_step_blocked("Power registry workers", NULL);
        return;
    }

    /* The cold-start pet is decorative, but it owns independent HTTP, media
     * lease and renderer/cache work. Quiesce it before the Gateway worker and
     * later service teardown can release those shared dependencies. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("startup pet worker");
    esp_err_t startup_pet_stop_err = device_status_to_platform_error(
        startup_pet_worker_service_stop(timeout_ms));
    if (startup_pet_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "startup pet asset task did not stop during startup rollback: %s",
                 esp_err_to_name(startup_pet_stop_err));
        startup_rollback_step_blocked("startup pet worker", NULL);
        return;
    }

    /* Terminal startup rollback must also retire the retained retry timer.
     * System Sleep PREPARE is deliberately different: it only parks the timer
     * so the common reverse rollback can restore the pre-existing retry. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("startup pet retry");
    esp_err_t startup_pet_retry_stop_err = device_status_to_platform_error(
        startup_pet_retry_service_stop(timeout_ms));
    if (startup_pet_retry_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "startup pet retry did not stop during startup rollback: %s",
                 esp_err_to_name(startup_pet_retry_stop_err));
        startup_rollback_step_blocked("startup pet retry", NULL);
        return;
    }

    /* Cache work runs on an internal stack and borrows its frame from either
     * the startup worker or a runtime update. Drain it independently: joining
     * the startup worker alone is not proof that Flash/VFS no longer owns the
     * borrowed frame or the shared cache mutex. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("pet cache worker");
    esp_err_t pet_cache_stop_err = device_status_to_platform_error(
        pet_cache_service_stop(timeout_ms));
    if (pet_cache_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "pet cache task did not stop during startup rollback: %s",
                 esp_err_to_name(pet_cache_stop_err));
        startup_rollback_step_blocked("pet cache worker", NULL);
        return;
    }

    /* The restart coordinator is an AUDIO owner but does not own the board's
     * recognizer itself.  Stop it before tearing down connectivity/persistence
     * dependencies so a late retry cannot recreate wake processing during a
     * degraded boot. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Audio registry workers");
    esp_err_t audio_worker_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_AUDIO, timeout_ms);
    if (audio_worker_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "registered audio workers did not stop during startup rollback: %s",
                 esp_err_to_name(audio_worker_stop_err));
        startup_rollback_step_blocked("Audio registry workers", NULL);
        return;
    }

    /* The selected board owns the recognizer's MultiNet/I2S resources rather
     * than a main.c Registry entry, but it is still part of this audio fault
     * domain.  Drain its callback handoff under the same parent deadline
     * before a rollback can tear down services that it may otherwise reenter.
     * A timeout retains the closed generation and stops this dependency chain
     * fail-closed; no later owner may be released underneath a live callback. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("offline wake-word generation");
    esp_err_t wake_stop_err = audio_wake_word_stop_with_timeout(timeout_ms);
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG,
                 "offline wake-word generation did not stop during startup rollback: %s; "
                 "leaving audio generation isolated",
                 esp_err_to_name(wake_stop_err));
        startup_rollback_step_blocked("offline wake-word generation", NULL);
        return;
    }

    STARTUP_ROLLBACK_NEXT_TIMEOUT("Connectivity registry workers");
    esp_err_t connectivity_worker_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_CONNECTIVITY, timeout_ms);
    if (connectivity_worker_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "registered connectivity workers did not stop during startup rollback: %s",
                 esp_err_to_name(connectivity_worker_stop_err));
        startup_rollback_step_blocked("Connectivity registry workers", NULL);
        return;
    }

    /* Registry stops the recovery coordinator before this adapter boundary.
     * The transport quiesce then closes new ML307 start/HTTP admission and
     * joins its registration/probe coordination. It deliberately retains the
     * UART/modem and any active non-foreground HTTP borrower, so a timeout is
     * logged as incomplete isolation rather than treated as full shutdown. */
    if (device_connectivity_is_active_cellular()) {
        STARTUP_ROLLBACK_NEXT_TIMEOUT("cellular transport");
        device_status_t cellular_quiesce_status =
            device_connectivity_quiesce_cellular_transport(timeout_ms);
        if (cellular_quiesce_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cellular transport did not quiesce during startup rollback: %d",
                     (int)cellular_quiesce_status);
            startup_rollback_step_blocked("cellular transport", NULL);
            return;
        }
    }

    /* All Connectivity-owned workers (including the clock monitor) have now
     * been stopped. A cold-start rollback may therefore release the actual
     * Wi-Fi/SNTP/netif/event-loop resources without callbacks racing a dead
     * handler. This is intentionally attempted only in the rollback path;
     * normal setup transitions retain the running radio. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("network core");
    esp_err_t network_core_stop_err = stop_connectivity_root_transaction(timeout_ms);
    if (network_core_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "network core did not fully stop during startup rollback: %s",
                 esp_err_to_name(network_core_stop_err));
        startup_rollback_step_blocked("network core", NULL);
        return;
    }

    /* Update Service is a synchronous Persistence consumer. Close tool and
     * Hub-metadata admission before Persistence shuts its request boundary. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Update Service");
    device_status_t update_stop_status = update_service_deinit(timeout_ms);
    if (update_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "update service did not stop during startup rollback: status=%d",
                 (int)update_stop_status);
        startup_rollback_step_blocked("Update Service", NULL);
        return;
    }

    /* Weather cache and meeting-recovery metadata are synchronous Persistence
     * consumers. Close their admission after connectivity/meeting workers have
     * stopped, but before the shared NVS request boundary can be closed. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("weather cache");
    device_status_t weather_cache_stop_status = weather_cache_service_deinit(timeout_ms);
    if (weather_cache_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "weather cache did not stop during startup rollback: status=%d",
                 (int)weather_cache_stop_status);
        startup_rollback_step_blocked("weather cache", NULL);
        return;
    }
    STARTUP_ROLLBACK_NEXT_TIMEOUT("meeting recovery metadata");
    device_status_t meeting_recovery_stop_status = meeting_recovery_service_deinit(timeout_ms);
    if (meeting_recovery_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "meeting recovery metadata did not stop during startup rollback: status=%d",
                 (int)meeting_recovery_stop_status);
        startup_rollback_step_blocked("meeting recovery metadata", NULL);
        return;
    }

    /* Resource Pressure samples SPIFFS capacity. Stop that observation before
     * any future rollback releases or remounts Storage/VFS; late optional-pet
     * callers then fail closed instead of querying a stale filesystem label. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("resource-pressure service");
    device_status_t pressure_stop_status = resource_pressure_service_deinit(timeout_ms);
    if (pressure_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "resource pressure service did not stop during startup rollback: %d",
                 (int)pressure_stop_status);
        startup_rollback_step_blocked("resource-pressure service", NULL);
        return;
    }

    /* At this point the last VFS users are proven stopped: the startup-pet
     * worker and cache writer joined above, while the Audio owner sweep joined
     * the meeting WAV recorder/uploader. Cached restore is synchronous during
     * boot and cannot outlive its starter. Resource Pressure has also stopped
     * its SPIFFS sampling, so Storage may now close new admission and detach
     * the VFS. Any failure leaves the mounted volume and its consumers
     * fail-closed rather than unmounting below a possible file handle. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Storage Service");
    device_status_t storage_deinit_status = storage_service_deinit();
    if (storage_deinit_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "storage service did not unmount during startup rollback: %d",
                 (int)storage_deinit_status);
        startup_rollback_step_blocked("Storage Service", NULL);
        return;
    }
    s_storage_mounted = false;
    /* These workers have their own admission, stop sentinel and completion
     * contracts. Registry ownership keeps this coordinator from reaching into
     * task handles and preserves an entry when a bounded join times out. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Interaction registry workers");
    esp_err_t interaction_worker_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_INTERACTION, timeout_ms);
    if (interaction_worker_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "registered interaction workers did not stop during startup rollback: %s",
                 esp_err_to_name(interaction_worker_stop_err));
        startup_rollback_step_blocked("Interaction registry workers", NULL);
        return;
    }

    /* Configuration reconciliation owns retained expiry/retry timers and one
     * worker that reads effective Configuration snapshots. Join it before the
     * Configuration value owner closes admission or frees its scratch store;
     * otherwise a timer callback could outlive the snapshot service it uses. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Configuration reconcile coordinator");
    device_status_t configuration_reconcile_stop_status =
        configuration_reconcile_service_deinit(timeout_ms);
    if (configuration_reconcile_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "configuration reconcile coordinator did not stop during startup rollback: %d",
                 (int)configuration_reconcile_stop_status);
        startup_rollback_step_blocked("Configuration reconcile coordinator", NULL);
        return;
    }

    /* Configuration owns substantial schema scratch buffers and the admission
     * boundary used by portal, volume, connectivity and interaction callers.
     * Those clients and its reconciliation consumer have all joined above, so
     * release Configuration before this degraded boot retains only diagnostics. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Configuration Service");
    esp_err_t configuration_stop_err = device_status_to_platform_error(
        configuration_service_deinit(timeout_ms));
    if (configuration_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "configuration service did not deinitialize during startup rollback: %s",
                 esp_err_to_name(configuration_stop_err));
        startup_rollback_step_blocked("Configuration Service", NULL);
        return;
    }

    /* Deadline clients may own a foreground presentation lease. Drain them
     * before closing the lease domain, otherwise a timeout would leave their
     * valid cleanup handle unable to release during the degraded transition. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("alarm manager");
    device_status_t alarm_stop_status = alarm_manager_deinit(timeout_ms);
    if (alarm_stop_status == DEVICE_STATUS_OK) {
        s_alarm_manager_started = false;
    } else {
        ESP_LOGW(TAG, "alarm manager did not stop during startup rollback: device status=%d",
                 (int)alarm_stop_status);
        startup_rollback_step_blocked("alarm manager", NULL);
        return;
    }
    STARTUP_ROLLBACK_NEXT_TIMEOUT("sleep schedule");
    device_status_t schedule_stop_status = sleep_schedule_service_deinit(timeout_ms);
    if (schedule_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "sleep schedule did not stop during startup rollback: device status=%d",
                 (int)schedule_stop_status);
        startup_rollback_step_blocked("sleep schedule", NULL);
        return;
    } else if (alarm_stop_status == DEVICE_STATUS_OK) {
        STARTUP_ROLLBACK_NEXT_TIMEOUT("wake deadline dispatcher");
        device_status_t deadline_stop_status = wake_deadline_service_deinit(timeout_ms);
        if (deadline_stop_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "wake deadline dispatcher did not stop during startup rollback: status=%d",
                     (int)deadline_stop_status);
            startup_rollback_step_blocked("wake deadline dispatcher", NULL);
            return;
        }
    } else {
        ESP_LOGW(TAG, "wake deadline dispatcher retained because alarm client is still active");
        startup_rollback_step_blocked("wake deadline dispatcher", "alarm client remains active");
        return;
    }

    STARTUP_ROLLBACK_NEXT_TIMEOUT("fall detection");
    device_status_t fall_stop_status = fall_detection_service_deinit(timeout_ms);
    if (fall_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "fall detection did not stop during startup rollback: %d",
                 (int)fall_stop_status);
        startup_rollback_step_blocked("fall detection", NULL);
        return;
    }

    /* Alarm, schedule and fall-detection are also Persistence consumers. They
     * must close their own tool/callback admission before the shared NVS
     * boundary may stop; keeping this below their deinit calls prevents their
     * final store operation from racing an already-stopped worker. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Storage registry workers");
    esp_err_t persistence_stop_err =
        task_registry_stop_owner(TASK_REGISTRY_OWNER_STORAGE, timeout_ms);
    if (persistence_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "registered storage workers did not stop during startup rollback: %s",
                 esp_err_to_name(persistence_stop_err));
        startup_rollback_step_blocked("Storage registry workers", NULL);
        return;
    }
    /* The registry is the ordinary owner path. Call the service boundary once
     * more so a stop that raced self-unregistration can finish its closed
     * generation and reclaim its queue/semaphores. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Persistence Service");
    device_status_t persistence_deinit_status = persistence_service_deinit(timeout_ms);
    if (persistence_deinit_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "persistence service did not fully deinitialize during startup rollback: status=%d",
                 (int)persistence_deinit_status);
        startup_rollback_step_blocked("Persistence Service", NULL);
        return;
    }
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Platform NVS");
    device_status_t nvs_deinit_status = platform_nvs_deinit();
    if (nvs_deinit_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "Platform NVS did not deinitialize during startup rollback: %d",
                 (int)nvs_deinit_status);
        startup_rollback_step_blocked("Platform NVS", NULL);
        return;
    }

    /* Battery Policy is a synchronous consumer of normalized Power telemetry.
     * Close it before Power Service teardown so late diagnostics and tool
     * requests fail closed rather than observing a released provider. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("battery policy");
    device_status_t battery_policy_stop_status = battery_policy_service_deinit(timeout_ms);
    if (battery_policy_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "battery policy did not stop during startup rollback: %d",
                 (int)battery_policy_stop_status);
        startup_rollback_step_blocked("battery policy", NULL);
        return;
    }

    STARTUP_ROLLBACK_NEXT_TIMEOUT("Power Service");
    device_status_t power_stop_status = device_power_deinit(timeout_ms);
    if (power_stop_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "power service did not stop during startup rollback: %d",
                 (int)power_stop_status);
        startup_rollback_step_blocked("Power Service", NULL);
        return;
    }

    /* Serial/JTAG identity has no dependency on the normal radio or NVS
     * services, so degraded boot intentionally leaves it alive for recovery
     * tooling.  It is nevertheless a registered DIAGNOSTICS owner: if a
     * future lifecycle path must reconfigure the console, its task now has an
     * admission gate and cooperative join contract. Do not stop it here. */

#undef STARTUP_ROLLBACK_NEXT_TIMEOUT
}

/* Meeting Service host seam. Gateway Transport owns all Wi-Fi HTTP lanes,
 * including the separate streaming meeting PUT transaction. */
static bool meeting_host_storage_mounted(void) {
    return s_storage_mounted;
}

static int32_t meeting_host_wake_word_stop(void) {
    return (int32_t)audio_wake_word_stop();
}

static int32_t meeting_host_wake_word_start(void) {
    return (int32_t)audio_wake_word_start(on_wake_word, NULL);
}

static int32_t meeting_host_recording_create(char *out_recording_id, uint32_t capacity) {
    (void)capacity;
    return (int32_t)create_meeting_recording(out_recording_id);
}

static int32_t meeting_host_recording_get_status(const char *recording_id,
                                                 char *out_status, uint32_t capacity) {
    return (int32_t)get_meeting_status(recording_id, out_status, capacity);
}

static int32_t meeting_host_recording_post_action(const char *recording_id,
                                                  const char *action,
                                                  const char *payload,
                                                  int32_t expected_a, int32_t expected_b) {
    return (int32_t)post_meeting_action(recording_id, action, payload,
                                        (int)expected_a, (int)expected_b);
}

static bool meeting_host_capability_transport_ready(void) {
    return true;
}

static void meeting_host_cancel_capability_http(int64_t deadline_us) {
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (cancel_guard_ms != 0) gateway_transport_cancel_capability_refresh(cancel_guard_ms);
}

static void meeting_host_log_heap_snapshot(const char *stage) {
    log_heap_snapshot(stage);
}

static void meeting_host_schedule_wake_restart(void) {
    schedule_wake_restart();
}

static const meeting_service_host_t s_meeting_service_host = {
    .storage_mounted = meeting_host_storage_mounted,
    .wake_word_stop = meeting_host_wake_word_stop,
    .wake_word_start = meeting_host_wake_word_start,
    .recording_create = meeting_host_recording_create,
    .recording_get_status = meeting_host_recording_get_status,
    .recording_post_action = meeting_host_recording_post_action,
    .capability_transport_ready = meeting_host_capability_transport_ready,
    .cancel_capability_http = meeting_host_cancel_capability_http,
    .log_heap_snapshot = meeting_host_log_heap_snapshot,
    .schedule_wake_restart = meeting_host_schedule_wake_restart,
};

static bool transport_host_current_task_is_startup_pet_asset(void) {
    return startup_pet_worker_service_is_current_worker();
}

static bool transport_host_start_gateway_ready_tasks(void) {
    return start_gateway_ready_tasks();
}

static void transport_host_apply_deferred_startup_pet_asset(void) {
    apply_deferred_startup_pet_asset();
}

static void transport_host_start_setup_portal(void) {
    provisioning_service_start_portal(true);
}

static bool transport_host_staged_provisioning_pending(void) {
    /* Configuration supplied this evidence while load_boot_candidate() was
     * still under its durable snapshot lock. Do not re-query later through a
     * lossy bool API: a transient Configuration admission failure must never
     * make this boot silently treat an unconfirmed candidate as confirmed. */
    return s_boot_provisioning_staged;
}

static bool transport_host_rollback_staged_provisioning(void) {
    device_status_t status = configuration_service_rollback_staged_provisioning();
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cannot roll back unconfirmed provisioning candidate: status=%d",
                 (int)status);
        return false;
    }
    /* The radio/transport are already built around the candidate.  Reboot
     * into the confirmed snapshot instead of attempting a partial live
     * APSTA/STA reconfiguration; this keeps every hardware profile on the
     * same value-only Configuration transaction. */
    ESP_LOGW(TAG, "unconfirmed provisioning candidate rolled back; restarting");
    esp_restart();
    return false;
}

static void transport_host_log_heap_snapshot(const char *stage) {
    log_heap_snapshot(stage);
}

static void transport_host_apply_server_time(const void *json_node) {
    apply_gateway_server_time((cJSON *)json_node);
}

static void transport_host_apply_ambient(const void *ambient_node) {
    ambient_service_apply_hub_ambient(ambient_node);
}

static void transport_host_set_handshake_welcome_queued(bool queued) {
    s_handshake_startup_welcome_queued = queued;
}

static const char *transport_host_boot_session_id(void) {
    return s_boot_session_id;
}

static void transport_host_note_cold_start_pet_asset(const void *pet_asset_node,
                                                     const char *skin) {
    cJSON *pet_asset = (cJSON *)pet_asset_node;
    s_startup_pet_asset_pending = true;
    s_startup_pet_retry_count = 0;
    if (!s_startup_pet_asset_ref) {
        s_startup_pet_asset_ref = heap_caps_calloc(
            1, sizeof(*s_startup_pet_asset_ref), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!s_startup_pet_asset_ref) {
            s_startup_pet_asset_ref = calloc(1, sizeof(*s_startup_pet_asset_ref));
        }
    }
    s_startup_pet_asset_present = s_startup_pet_asset_ref &&
                                  cJSON_IsObject(pet_asset) &&
                                  pet_asset_service_parse_hub_descriptor(
                                      pet_asset, s_startup_pet_asset_ref);
    strlcpy(s_startup_pet_asset_skin, skin ? skin : "",
            sizeof(s_startup_pet_asset_skin));
    if (cJSON_IsObject(pet_asset) && !s_startup_pet_asset_present) {
        ESP_LOGW(TAG, "startup pet asset descriptor is invalid; cached asset will be cleared after wake readiness");
    }
    ESP_LOGI(TAG, "startup pet asset deferred until wake ready: %s",
             s_startup_pet_asset_present ? s_startup_pet_asset_ref->revision : "none");
}

static int32_t transport_host_apply_pet_asset(const void *pet_asset_node) {
    return (int32_t)apply_pet_asset_ref((cJSON *)pet_asset_node);
}

static int32_t transport_host_clear_pet_asset(void) {
    esp_err_t asset_err = clear_applied_pet_asset();
    if (asset_err == ESP_OK) s_loaded_pet_asset_revision[0] = '\0';
    return (int32_t)asset_err;
}

static void transport_host_process_update_metadata(const void *update_node, bool cold_start) {
    process_update_metadata((cJSON *)update_node, cold_start);
}

static bool transport_host_append_tool_descriptors(const void *tools_array) {
    return device_tool_registry_append_descriptors((cJSON *)tools_array);
}

static int32_t transport_host_persist_gateway_token(const char *token) {
    return (int32_t)save_gateway_token(token);
}

static const gateway_transport_host_t s_gateway_transport_host = {
    .current_task_is_startup_pet_asset = transport_host_current_task_is_startup_pet_asset,
    .start_gateway_ready_tasks = transport_host_start_gateway_ready_tasks,
    .apply_deferred_startup_pet_asset = transport_host_apply_deferred_startup_pet_asset,
    .start_setup_portal = transport_host_start_setup_portal,
    .staged_provisioning_pending = transport_host_staged_provisioning_pending,
    .rollback_staged_provisioning = transport_host_rollback_staged_provisioning,
    .log_heap_snapshot = transport_host_log_heap_snapshot,
    .apply_server_time = transport_host_apply_server_time,
    .apply_ambient = transport_host_apply_ambient,
    .set_handshake_welcome_queued = transport_host_set_handshake_welcome_queued,
    .boot_session_id = transport_host_boot_session_id,
    .note_cold_start_pet_asset = transport_host_note_cold_start_pet_asset,
    .apply_pet_asset = transport_host_apply_pet_asset,
    .clear_pet_asset = transport_host_clear_pet_asset,
    .process_update_metadata = transport_host_process_update_metadata,
    .append_tool_descriptors = transport_host_append_tool_descriptors,
    .persist_gateway_token = transport_host_persist_gateway_token,
};

/* Gateway Dispatcher host seam. Gateway Transport owns HTTP execution and
 * cancellation; the startup Welcome gate, pet/profile configuration handlers
 * and server-audio playback remain composition-root domain callbacks. */
static void gateway_host_cancel_poll_http(int64_t deadline_us) {
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (cancel_guard_ms != 0) {
        (void)gateway_transport_cancel_active_requests(
            GATEWAY_TRANSPORT_CANCEL_POLL, cancel_guard_ms);
    }
}

static bool gateway_host_welcome_gate_active(void) {
    return s_startup_welcome_gate_active;
}

static int32_t gateway_host_welcome_classify(const void *message_item, const char *id,
                                             bool welcome_audio, bool preview_audio) {
    cJSON *item = (cJSON *)message_item;
    if (!welcome_audio || preview_audio) return GATEWAY_DISPATCHER_WELCOME_NONE;
    if (!startup_welcome_is_current_boot(item, welcome_audio)) {
        // Reserved Welcome messages are boot-scoped transactions. A greeting left
        // pending by an interrupted ACK from an earlier boot must never be treated
        // as ordinary speech: doing so plays the stale greeting and then this boot's
        // greeting. Explicit GUI previews are exempt and remain user-triggered.
        ESP_LOGW(TAG, "stale or unscoped startup Welcome discarded: id=%s", id);
        return GATEWAY_DISPATCHER_WELCOME_STALE;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool already_consumed = s_startup_welcome_consumed;
    const bool discard = s_startup_welcome_timed_out || already_consumed;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (discard) {
        // Never play a boot greeting after MultiNet has been started. ACK it
        // as handled so a late delivery cannot retry forever. The same rule
        // also turns an ACK retry after successful playback into a silent,
        // idempotent delivery instead of replaying the greeting.
        ESP_LOGW(TAG, "%s startup Welcome discarded: id=%s",
                 already_consumed ? "already consumed" : "late", id);
        return GATEWAY_DISPATCHER_WELCOME_DISCARD_CURRENT;
    }
    return GATEWAY_DISPATCHER_WELCOME_CURRENT;
}

static void gateway_host_welcome_complete(bool playback_succeeded) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_welcome_consumed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    finish_startup_welcome_gate(playback_succeeded ? "playback complete"
                                                   : "playback unavailable");
}

static int32_t gateway_host_handle_tool_call(const void *message_item) {
    return (int32_t)handle_client_tool_call((cJSON *)message_item);
}

static void gateway_host_handle_pet_profile(const void *message_item, const char *id,
                                            bool *out_handled,
                                            bool *out_permanently_invalid) {
    cJSON *item = (cJSON *)message_item;
    cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    const char *skin = json_string(item, "pet_skin");
    if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
    if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
    cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(item, "pet_asset");
    if (!cJSON_IsObject(pet_asset) && cJSON_IsObject(extra)) {
        pet_asset = cJSON_GetObjectItemCaseSensitive(extra, "pet_asset");
    }
    *out_handled = true;
    *out_permanently_invalid = false;
    /* The handshake descriptor owns the initial high-resolution asset.
     * It is already being installed after Welcome on the startup task;
     * downloading the queued mirror here races it, doubles PSRAM usage,
     * and can reduce both attempts to the native robot fallback. */
    // The cold-start worker owns only the descriptor captured from
    // the handshake. A later GUI selection must supersede it even if
    // Hub happened to reuse the same content revision under a new
    // profile message. Comparing the revision alone made that new
    // selection get ACKed as "deferred" while the old startup worker
    // was still the only transaction allowed to publish pixels.
    bool defer_to_startup_installer = s_startup_pet_asset_pending &&
        s_startup_pet_asset_ref && cJSON_IsObject(pet_asset) &&
        !strcmp(s_startup_pet_asset_ref->revision,
                json_string(pet_asset, "revision") ?
                    json_string(pet_asset, "revision") : "") &&
        (!skin || !strcmp(skin, s_startup_pet_asset_skin));
    if (defer_to_startup_installer) {
        ESP_LOGI(TAG, "startup pet_profile asset deferred to handshake installer");
        /* handled by the handshake installer */
    } else if (cJSON_IsObject(pet_asset)) {
        if (s_startup_pet_asset_pending) {
            ESP_LOGI(TAG, "new GUI pet revision supersedes startup asset");
            // Cancel the older boot transaction before downloading the
            // GUI-selected revision. The startup worker checks this
            // flag between frames and cannot overwrite the new result.
            s_startup_pet_asset_pending = false;
        }
        esp_err_t asset_err = apply_pet_asset_ref(pet_asset);
        *out_handled = asset_err == ESP_OK;
        *out_permanently_invalid = audio_error_is_permanent(asset_err) ||
                                   asset_err == ESP_ERR_INVALID_CRC;
        if (!*out_handled) {
            if (id && !strcmp(id, s_pet_asset_retry_id)) {
                ++s_pet_asset_retry_count;
            } else {
                strlcpy(s_pet_asset_retry_id, id ? id : "",
                        sizeof(s_pet_asset_retry_id));
                s_pet_asset_retry_count = 1;
            }
            // 连续失败达到上限按永久失败处理，避免堵死整页消息。
            if (s_pet_asset_retry_count >= PET_ASSET_RETRY_LIMIT) {
                *out_permanently_invalid = true;
            }
            ESP_LOGW(TAG, "pet asset update failed: %s (retry %d/%d)",
                     esp_err_to_name(asset_err), s_pet_asset_retry_count,
                     PET_ASSET_RETRY_LIMIT);
        } else {
            s_pet_asset_retry_id[0] = '\0';
            s_pet_asset_retry_count = 0;
        }
    } else {
        // An asset-less profile means the server selected the native
        // fallback (or rejected malformed GUI data). Remove the old
        // transparent raster and its boot cache as part of the same
        // acknowledged state transition.
        esp_err_t asset_err = clear_applied_pet_asset();
        *out_handled = asset_err == ESP_OK;
        *out_permanently_invalid = audio_error_is_permanent(asset_err);
        if (!*out_handled) ESP_LOGW(TAG, "pet asset clear failed: %s", esp_err_to_name(asset_err));
    }
}

static void gateway_host_handle_hardware_config(
    const void *extra_node, const gateway_capability_lease_t *lease,
    bool *out_handled, bool *out_permanently_invalid) {
    cJSON *extra = (cJSON *)extra_node;
    *out_handled = false;
    *out_permanently_invalid = false;
    if (!cJSON_IsObject(extra)) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "ignored hardware config without extra object");
        return;
    }
    int volume = 0;
    int brightness = 0;
    int screen_sleep_seconds = 0;
    bool has_volume = json_number(extra, "volume", &volume);
    bool has_brightness = json_number(extra, "brightness", &brightness);
    bool has_screen_sleep = json_number(extra, "screenSleepSeconds", &screen_sleep_seconds);
    const bool has_recognized_field = has_volume || has_brightness || has_screen_sleep;
    configuration_reconcile_authorization_t authorization = {0};
    if (has_recognized_field &&
        (!gateway_configuration_authorization_from_lease(lease, &authorization) ||
         !gateway_transport_capability_lease_current(lease))) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "discarded hardware config before persistence: capability lease changed");
        return;
    }
    // Each hardware_config field is independently durable. A partial
    // success must stay queued so a reconnect can retry the failed
    // setting instead of ACKing it as if all three applied.
    bool volume_handled = !has_volume;
    bool brightness_handled = !has_brightness;
    bool screen_sleep_handled = !has_screen_sleep;
    if (!has_volume && !has_brightness && !has_screen_sleep) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "ignored hardware config without volume/brightness/screen sleep");
    }
    if (has_volume && volume >= 0 && volume <= 100) {
        uint64_t volume_revision = 0u;
        /* Publish first: the authenticated Hub policy must survive a reset
         * before the codec mixer sees it. Audio is a separate consumer and is
         * deliberately not called while Configuration holds its NVS mutex. */
        if (!gateway_transport_capability_lease_current(lease)) {
            *out_permanently_invalid = true;
            ESP_LOGW(TAG, "discarded hardware volume before persistence: capability lease changed");
            return;
        }
        esp_err_t save_err = persist_hub_output_volume((unsigned)volume, &volume_revision);
        if (save_err == ESP_OK) {
            if (!gateway_transport_capability_lease_current(lease)) {
                *out_permanently_invalid = true;
                ESP_LOGW(TAG, "discarded hardware volume before reconcile: capability lease changed");
                return;
            }
            const device_status_t reconcile_status =
                configuration_reconcile_service_reconcile_authorized(
                    CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY, &authorization);
            if (reconcile_status == DEVICE_STATUS_OK &&
                configuration_reconcile_output_volume_applied(
                    volume_revision, (uint8_t)volume)) {
                volume_handled = true;
                s_configured_output_volume = (unsigned)volume;
                s_configured_output_volume_saved = true;
                ESP_LOGI(TAG, "server output volume: %d%% (revision=%llu)", volume,
                         (unsigned long long)volume_revision);
            } else {
                if (reconcile_status == DEVICE_STATUS_UNAVAILABLE) {
                    *out_permanently_invalid = true;
                }
                ESP_LOGW(TAG, "committed output volume reconcile incomplete: device status=%d",
                         reconcile_status);
            }
        } else {
            ESP_LOGW(TAG, "output volume persistence failed; no Audio apply: %s",
                     esp_err_to_name(save_err));
        }
    } else if (has_volume) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "ignored invalid server output volume");
    }
    if (has_brightness && (brightness < 0 || brightness > 100)) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "ignored invalid server display brightness");
    }
    if (has_screen_sleep && !valid_screen_sleep_seconds(screen_sleep_seconds)) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "ignored invalid server screen sleep timeout");
    }
    /* Display policy is one Configuration publication. Do not apply the
     * physical/UI effects until its durable revision has committed: a reset,
     * failed flash write or candidate rebase must never produce a visible
     * brightness/idle policy which Configuration cannot later reconcile. */
    if ((!has_brightness || (brightness >= 0 && brightness <= 100)) &&
        (!has_screen_sleep || valid_screen_sleep_seconds(screen_sleep_seconds)) &&
        (has_brightness || has_screen_sleep)) {
        uint64_t display_revision = 0u;
        if (!gateway_transport_capability_lease_current(lease)) {
            *out_permanently_invalid = true;
            ESP_LOGW(TAG, "discarded display policy before persistence: capability lease changed");
            return;
        }
        esp_err_t save_err = persist_hub_display_policy(
            has_brightness, has_brightness ? (unsigned)brightness : 0u,
            has_screen_sleep,
            has_screen_sleep ? (unsigned)screen_sleep_seconds : 0u,
            &display_revision);
        if (save_err == ESP_OK) {
            bool apply_ok = true;
            if (!gateway_transport_capability_lease_current(lease)) {
                *out_permanently_invalid = true;
                ESP_LOGW(TAG, "discarded display policy before reconcile: capability lease changed");
                return;
            }
            const device_status_t reconcile_status =
                configuration_reconcile_service_reconcile_authorized(
                    CONFIGURATION_RECONCILE_REASON_RUNTIME_POLICY, &authorization);
            if (has_brightness) {
                if (reconcile_status == DEVICE_STATUS_OK &&
                    configuration_reconcile_display_brightness_applied(
                        display_revision, (uint8_t)brightness)) {
                    brightness_handled = true;
                    s_configured_display_brightness = (uint8_t)brightness;
                    s_configured_display_brightness_saved = true;
                    ESP_LOGI(TAG, "server display brightness: %d%%", brightness);
                } else {
                    apply_ok = false;
                    if (reconcile_status == DEVICE_STATUS_UNAVAILABLE) {
                        *out_permanently_invalid = true;
                    }
                    ESP_LOGW(TAG, "committed display brightness reconcile incomplete: device status=%d",
                             reconcile_status);
                }
            }
            if (has_screen_sleep) {
                if (reconcile_status == DEVICE_STATUS_OK &&
                    configuration_reconcile_screen_sleep_applied(
                        display_revision, (uint32_t)screen_sleep_seconds)) {
                    screen_sleep_handled = true;
                    s_configured_screen_sleep_seconds = (uint32_t)screen_sleep_seconds;
                    s_configured_screen_sleep_seconds_saved = true;
                    ESP_LOGI(TAG, "server screen sleep timeout: %d seconds", screen_sleep_seconds);
                } else {
                    apply_ok = false;
                    if (reconcile_status == DEVICE_STATUS_UNAVAILABLE) {
                        *out_permanently_invalid = true;
                    }
                    ESP_LOGW(TAG, "committed screen sleep reconcile incomplete: device status=%d",
                             reconcile_status);
                }
            }
            if (!apply_ok || display_revision == 0u) {
                ESP_LOGW(TAG, "display policy revision=%llu needs reconciliation",
                         (unsigned long long)display_revision);
            }
        } else {
            ESP_LOGW(TAG, "display policy persistence failed; no Display/Power apply: %s",
                     esp_err_to_name(save_err));
        }
    }
    if (has_recognized_field && !gateway_transport_capability_lease_current(lease)) {
        *out_permanently_invalid = true;
        ESP_LOGW(TAG, "discarded hardware config before acknowledgement: capability lease changed");
        return;
    }
    *out_handled = volume_handled && brightness_handled && screen_sleep_handled;
}

static void gateway_host_apply_glyphs(const void *glyphs_node) {
    (void)ambient_service_apply_hub_glyphs(glyphs_node);
}

static void gateway_host_apply_ambient(const void *ambient_node) {
    ambient_service_apply_hub_ambient(ambient_node);
}

static bool gateway_host_audio_url_allowed(const char *url) {
    return hardware_audio_url_allowed(url);
}

static bool gateway_host_audio_mime_supported(const char *mime) {
    return audio_mime_supported(mime);
}

static bool gateway_host_audio_error_is_permanent(int32_t err) {
    return audio_error_is_permanent((esp_err_t)err);
}

static bool gateway_host_begin_server_audio_wake_lease(const char *source) {
    return begin_server_audio_wake_memory_lease(source);
}

static bool gateway_host_finish_server_audio_wake_lease(void) {
    return finish_server_audio_wake_memory_lease();
}

static int32_t gateway_host_download_audio(const char *url, uint8_t **out_audio,
                                           uint32_t *out_len) {
    size_t len = 0;
    esp_err_t err = download_audio(url, out_audio, &len);
    *out_len = (uint32_t)len;
    return (int32_t)err;
}

static int32_t gateway_host_play_audio_payload(const char *mime, const uint8_t *data,
                                               uint32_t len) {
    return (int32_t)play_audio_payload(mime, data, len);
}

static void gateway_host_schedule_wake_restart(void) {
    schedule_wake_restart();
}

static bool gateway_host_take_startup_pet_retry_due(void) {
    return startup_pet_retry_service_take_due();
}

static void gateway_host_apply_deferred_startup_pet_asset(void) {
    apply_deferred_startup_pet_asset();
}

static const gateway_dispatcher_host_t s_gateway_dispatcher_host = {
    .cancel_poll_http = gateway_host_cancel_poll_http,
    .welcome_gate_active = gateway_host_welcome_gate_active,
    .welcome_classify = gateway_host_welcome_classify,
    .welcome_complete = gateway_host_welcome_complete,
    .handle_tool_call = gateway_host_handle_tool_call,
    .handle_pet_profile = gateway_host_handle_pet_profile,
    .handle_hardware_config = gateway_host_handle_hardware_config,
    .apply_glyphs = gateway_host_apply_glyphs,
    .apply_ambient = gateway_host_apply_ambient,
    .audio_url_allowed = gateway_host_audio_url_allowed,
    .audio_mime_supported = gateway_host_audio_mime_supported,
    .audio_error_is_permanent = gateway_host_audio_error_is_permanent,
    .begin_server_audio_wake_lease = gateway_host_begin_server_audio_wake_lease,
    .finish_server_audio_wake_lease = gateway_host_finish_server_audio_wake_lease,
    .download_audio = gateway_host_download_audio,
    .play_audio_payload = gateway_host_play_audio_payload,
    .schedule_wake_restart = gateway_host_schedule_wake_restart,
    .take_startup_pet_retry_due = gateway_host_take_startup_pet_retry_due,
    .apply_deferred_startup_pet_asset = gateway_host_apply_deferred_startup_pet_asset,
};

/* Interaction Service host seam: voice upload/submit/pairing are composed
 * here, while Gateway Transport independently owns their foreground HTTP
 * cancellation. Implementations shared with other service tables are reused. */
static bool interaction_host_ensure_gateway_poll_task(void) {
    return gateway_dispatcher_ensure_poll_task();
}

static int32_t interaction_host_upload_voice(const uint8_t *wav, uint32_t wav_len,
                                             char *out_media_id,
                                             uint32_t media_id_capacity) {
    return (int32_t)upload_voice(wav, wav_len, out_media_id, media_id_capacity);
}

static int32_t interaction_host_send_voice_event(const char *media_id,
                                                 const char *event_id,
                                                 char *out_reply_to,
                                                 uint32_t reply_to_capacity) {
    return (int32_t)send_voice_event(media_id, event_id, out_reply_to,
                                     reply_to_capacity);
}

static void interaction_host_cancel_foreground_http(int64_t deadline_us) {
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (cancel_guard_ms != 0) gateway_transport_cancel_foreground_request(cancel_guard_ms);
}

static const interaction_service_host_t s_interaction_service_host = {
    .ensure_gateway_poll_task = interaction_host_ensure_gateway_poll_task,
    .upload_voice = interaction_host_upload_voice,
    .send_voice_event = interaction_host_send_voice_event,
    .wake_word_stop = meeting_host_wake_word_stop,
    .cancel_foreground_http = interaction_host_cancel_foreground_http,
    .log_heap_snapshot = meeting_host_log_heap_snapshot,
    .schedule_wake_restart = meeting_host_schedule_wake_restart,
};

void app_main(void) {
    ESP_LOGW(TAG, "boot reset reason=%d", (int)esp_reset_reason());
    lifecycle_service_begin();
    if (task_registry_init() != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
                               DEVICE_STATUS_RESOURCE_EXHAUSTED, "task registry");
        return;
    }
    if (provisioning_failure_injection_task_registry_lifecycle_test_enabled()) {
        esp_err_t registry_test_err = task_registry_run_lifecycle_test();
        if (registry_test_err != ESP_OK) {
            ESP_LOGE(TAG, "task registry lifecycle test failed: %s",
                     esp_err_to_name(registry_test_err));
            startup_enter_degraded(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
                                   DEVICE_STATUS_INTERNAL_ERROR, "task registry lifecycle test");
            return;
        }
    }
    operation_context_service_init();
    if (!validate_compiled_board_profile()) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
                               DEVICE_STATUS_INTERNAL_ERROR, "board profile validation");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED);
    device_status_t identity_status = firmware_identity_start();
    if (identity_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_IDENTITY_READY,
                               identity_status, "identity service");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_IDENTITY_READY);
    device_status_t nvs_status = platform_nvs_init();
    if (nvs_status != DEVICE_STATUS_OK) {
        // firmware_identity_start() intentionally remains available so a
        // service tool can inspect the board/profile and perform an explicit
        // recovery. Do not start Wi-Fi, audio or writers against an NVS
        // partition whose contents Platform NVS deliberately chose not to destroy.
        ESP_LOGE(TAG, "startup stopped before user-data writes: NVS status=%d", (int)nvs_status);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_STORAGE_READY,
                               nvs_status, "NVS initialization");
        return;
    }
	 lifecycle_service_reach(DEVICE_RUNTIME_PHASE_STORAGE_READY);
	load_device_id();
    uint8_t boot_random[16];
    esp_fill_random(boot_random, sizeof(boot_random));
    for (size_t i = 0; i < sizeof(boot_random); ++i) {
        snprintf(s_boot_session_id + i * 2, 3, "%02x", boot_random[i]);
    }
    if (psa_crypto_init() != PSA_SUCCESS) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR, "PSA crypto initialization");
        return;
    }
    device_status_t storage_mount_status = storage_service_init();
    s_storage_mounted = storage_service_is_available();
    if (storage_mount_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "durable storage unavailable; preserving existing contents: %d",
                 (int)storage_mount_status);
    }
    device_status_t pressure_status = resource_pressure_service_init(
        storage_service_label(), s_storage_mounted);
    if (pressure_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               pressure_status, "resource pressure service");
        return;
    }
    if (gateway_transport_init(&s_gateway_transport_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (clock_sync_service_init(&s_clock_sync_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (cellular_recovery_service_init(&s_cellular_recovery_service_host) !=
        DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (gateway_lifecycle_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    s_media_transfer_mutex = xSemaphoreCreateMutex();
    if (!s_media_transfer_mutex) goto startup_core_no_memory;
    s_pet_asset_apply_mutex = xSemaphoreCreateMutex();
    if (!s_pet_asset_apply_mutex) goto startup_core_no_memory;
    if (pet_cache_service_init(&s_pet_cache_service_host) != DEVICE_STATUS_OK) {
        goto startup_core_no_memory;
    }
    if (startup_pet_worker_service_init(&s_startup_pet_worker_service_host) !=
        DEVICE_STATUS_OK) {
        goto startup_core_no_memory;
    }
    if (foreground_coordinator_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (audio_arbitration_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (scene_presenter_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (ambient_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (reply_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (command_service_init(&s_command_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (interaction_service_init(&s_interaction_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (gateway_dispatcher_init(&s_gateway_dispatcher_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (meeting_service_init(&s_meeting_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    s_wake_restart_start_gate = xSemaphoreCreateBinary();
    if (!s_wake_restart_start_gate) goto startup_core_no_memory;
    s_wake_restart_stopped = xSemaphoreCreateBinary();
    if (!s_wake_restart_stopped) goto startup_core_no_memory;
    if (provisioning_service_init(&s_provisioning_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    s_deferred_setup_start_gate = xSemaphoreCreateBinary();
    if (!s_deferred_setup_start_gate) goto startup_core_no_memory;
    s_deferred_setup_stopped = xSemaphoreCreateBinary();
    if (!s_deferred_setup_stopped) goto startup_core_no_memory;
    if (startup_pet_retry_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_admission_open = true;
    s_deferred_setup_admission_open = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    s_startup_welcome_done = xSemaphoreCreateBinary();
    if (!s_startup_welcome_done) goto startup_core_no_memory;
    device_status_t persistence_init_status = persistence_service_init();
    if (persistence_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               persistence_init_status,
                               "persistence service");
        return;
    }
    device_status_t weather_cache_init_status = weather_cache_service_init();
    if (weather_cache_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               weather_cache_init_status,
                               "weather cache service");
        return;
    }
    device_status_t meeting_recovery_init_status = meeting_recovery_service_init();
    if (meeting_recovery_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               meeting_recovery_init_status,
                               "meeting recovery service");
        return;
    }
    esp_err_t configuration_init_err = device_status_to_platform_error(configuration_service_init());
    if (configuration_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(configuration_init_err),
                               "configuration service");
        return;
    }
    device_status_t configuration_reconcile_init_status =
        configuration_reconcile_service_init();
    if (configuration_reconcile_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               configuration_reconcile_init_status,
                               "configuration reconciliation service");
        return;
    }
    configuration_reconcile_service_set_authorization_validator(
        gateway_configuration_authorization_current, NULL);
    meeting_service_load_recovery();
    device_status_t update_init_status = update_service_init(&(update_service_config_t){
        .running_release_sequence = CONFIG_MACLAW_RELEASE_SEQUENCE,
    });
    if (update_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               update_init_status, "update metadata service");
        return;
    }
    esp_err_t configuration_load_err = load_device_config();
    if (configuration_load_err != ESP_OK) {
        /* Fail closed before restoring an uplink, presenting a pairing flow,
         * or issuing any authenticated request.  USB identity diagnostics
         * remain alive and make the recovery reason observable. */
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(configuration_load_err),
                               "configuration snapshot");
        return;
    }
    /* Do not create any permanent interaction/persistence worker until the
     * authoritative configuration snapshot has passed validation.  A corrupt
     * snapshot deliberately leaves this boot in USB-diagnosable degraded
     * mode; starting these workers first would leak background activity into
     * that otherwise fail-closed state. */
    s_output_volume_persist_queue = xQueueCreate(1, sizeof(output_volume_persist_request_t));
    if (!s_output_volume_persist_queue) goto startup_core_no_memory;
    s_output_volume_persist_reply_queue = xQueueCreate(2, sizeof(output_volume_persist_reply_t));
    if (!s_output_volume_persist_reply_queue) goto startup_core_no_memory;
    s_output_volume_persist_request_mutex = xSemaphoreCreateMutex();
    if (!s_output_volume_persist_request_mutex) goto startup_core_no_memory;
    s_output_volume_persist_stopped = xSemaphoreCreateBinary();
    if (!s_output_volume_persist_stopped) goto startup_core_no_memory;
    s_output_volume_persist_system_sleep_quiesced = xSemaphoreCreateBinary();
    if (!s_output_volume_persist_system_sleep_quiesced) goto startup_core_no_memory;
    if (command_service_start() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_output_volume_persist_stop_requested = false;
    s_output_volume_persist_system_sleep_preparing = false;
    s_output_volume_persist_exit_status = ESP_OK;
    s_output_volume_persist_retiring = false;
    s_output_volume_persist_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* Configuration/NVS commits run with flash cache transitions, therefore
     * the persistence worker must never receive a PSRAM stack.  The explicit
     * 8 KiB internal stack also covers the v3 configuration transaction path
     * observed on ESP32-S3 during Hub hardware_config volume updates. */
    if (xTaskCreatePinnedToCoreWithCaps(
            output_volume_persist_task, "maclaw_volume_nvs",
            OUTPUT_VOLUME_PERSIST_TASK_STACK_BYTES, NULL, 4,
            &s_output_volume_persist_task_handle, 1,
            MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT) != pdPASS) goto startup_core_no_memory;
    esp_err_t volume_registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "output_volume_persist",
        .context = (void *)s_output_volume_persist_task_handle,
        .stop = stop_output_volume_persist_registry_entry,
    });
    if (volume_registry_err != ESP_OK) {
        (void)stop_output_volume_persist_worker(500);
        goto startup_core_no_memory;
    }
    device_status_t storage_bridge_status =
        power_service_set_system_sleep_storage_bridge(
            prepare_output_volume_persist_system_sleep,
            abort_output_volume_persist_system_sleep_prepare, NULL);
    if (storage_bridge_status != DEVICE_STATUS_OK) {
        (void)stop_output_volume_persist_worker(500);
        goto startup_core_no_memory;
    }
    device_connectivity_restore_selected_uplink();
    load_gateway_token();
    ambient_service_load();
    /* Establish the one synchronous display-submission owner before App UI
     * can restore brightness or publish its startup/ambient scene. The board
     * renderer remains boot-lifetime; this only owns service-side ordering. */
    device_status_t display_init_status = display_service_init();
    if (display_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               display_init_status, "display service");
        return;
    }
    if (provisioning_failure_injection_display_service_fail_after_init()) {
        if (!display_service_start_test_request() ||
            !display_service_wait_for_test_request_start(3000)) {
            ESP_LOGE(TAG, "display service test request did not start");
            goto startup_core_no_memory;
        }
        ESP_LOGW(TAG, "forcing startup failure after Display Service publication (test injection)");
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR, "display service test injection");
        return;
    }
    /* Construct the selected panel/audio/peripheral adapter once before any
     * application scene restore or input queue can reach it.  This is not a
     * sleep/wake operation and does not provide runtime hardware restart;
     * Input Service below owns only scanner publication and shutdown. */
    device_status_t bootstrap_status = platform_bootstrap_initialize();
    if (bootstrap_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               bootstrap_status, "platform bootstrap");
        return;
    }
    app_ui_init();
    s_startup_ui_initialized = true;
    if (input_binding_init(&s_input_binding_host) != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR, "input binding");
        return;
    }
    device_status_t input_status = app_intent_service_start(on_app_intent, NULL);
    if (input_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               input_status, "input service");
        return;
    }
    /* Platform Bootstrap has already created the renderer's boot-lifetime
     * primitives. Restore before power/connectivity startup brings large
     * Wi-Fi/4G allocations into contention. */
    if (s_storage_mounted && device_storage_allows_optional_flash_work()) {
        (void)start_cached_pet_restore_task();
    }
    device_status_t power_status = device_power_init();
    if (power_status != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               power_status, "power service");
        return;
    }
    device_status_t battery_policy_status = battery_policy_service_init();
    if (battery_policy_status != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               battery_policy_status, "battery policy service");
        return;
    }
    device_status_t fall_detection_status =
        fall_detection_service_init(on_fall_detection_event, NULL);
    if (fall_detection_status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "suspected-fall service available");
    } else if (fall_detection_status == DEVICE_STATUS_UNAVAILABLE) {
        ESP_LOGI(TAG, "suspected-fall service unavailable on this profile");
    } else {
        /* Motion safety is an optional profile capability; do not convert a
         * local service startup into a reboot/degraded loop on boards that do
         * not carry the sensor or under transient resource pressure. */
        ESP_LOGW(TAG, "cannot start suspected-fall service: device status=%d",
                 (int)fall_detection_status);
    }
    device_status_t deadline_init_status = wake_deadline_service_init();
    if (deadline_init_status != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               deadline_init_status,
                               "wake deadline service");
        return;
    }
    device_status_t schedule_init_status = sleep_schedule_service_init();
    if (schedule_init_status != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               schedule_init_status,
                               "sleep schedule service");
        return;
    }
    device_status_t schedule_observer_status =
        sleep_schedule_service_set_display_wake_observer(on_schedule_display_wake, NULL);
    if (schedule_observer_status != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               schedule_observer_status,
                               "sleep schedule UI observer");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY);
    const device_status_t restore_status = configuration_reconcile_service_reconcile(
        CONFIGURATION_RECONCILE_REASON_BOOT_RESTORE);
    if (restore_status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "restored effective audio/display configuration");
    } else {
        /* The durable setting remains authoritative. Do not publish a second
         * compensating revision: retry must go through the same coordinator. */
        ESP_LOGW(TAG, "effective audio/display restore incomplete: device status=%d",
                 restore_status);
    }

    // A profile may provide a bounded boot selector (for example a physical
    // double-click) and an uplink-specific Gateway compatibility adaptation.
    // The business startup path sees only the selected normalized transport.
    if (device_connectivity_apply_startup_transport_toggle(
            STARTUP_TRANSPORT_SELECTOR_WINDOW_MS)) {
        ESP_LOGI(TAG, "startup transport toggle selected: %s",
                 device_connectivity_is_active_cellular() ? "cellular" : "Wi-Fi");
    }
    char startup_gateway_url[URL_CAPACITY];
    (void)gateway_transport_gateway_url(startup_gateway_url, sizeof(startup_gateway_url));
    device_connectivity_adapt_gateway_url(startup_gateway_url, sizeof(startup_gateway_url));
    gateway_transport_set_gateway_url(startup_gateway_url);
	// board initialization may briefly show and then clear its ROM/embedded
	// artwork. Re-present it as an explicit foreground UI surface so ambient
	// clock/profile updates cannot replace it while Welcome is being fetched and
	// played. The ready transition releases this surface after wake-word setup.
	scene_presenter_publish_startup_splash();
    // Keep optional background work quiescent until esp_wifi_start() has
    // completed.  Both cached-pet installation (which may create its animation
    // task) and the alarm manager create work that can run while the Wi-Fi ROM
    // is enabling TSF.  On Bread Compact that startup overlap can corrupt the
    // Wi-Fi task's first callback and jump to PC 0x1 (InstrFetchProhibited).
    // The LCD mutex already exists here; only the timing is intentionally
    // deferred.
    firmware_identity_set_local_ready(true);
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_LOCAL_READY);
    if (provisioning_failure_injection_safe_mode_at_local_ready()) {
        ESP_LOGW(TAG, "forcing proven-local-ready SAFE_MODE entry (test injection)");
        const startup_safe_mode_entry_result_t safe_mode_result =
            startup_enter_safe_mode(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                                    DEVICE_STATUS_INTERNAL_ERROR,
                                    "local-ready SAFE_MODE test injection");
        if (safe_mode_result == STARTUP_SAFE_MODE_ENTRY_NOT_STARTED) {
            startup_enter_degraded(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                                   DEVICE_STATUS_INTERNAL_ERROR,
                                   "local-ready SAFE_MODE test injection");
        } else if (safe_mode_result == STARTUP_SAFE_MODE_ENTRY_TERMINAL_FAILURE) {
            startup_enter_safe_mode_terminal_failure(
                DEVICE_RUNTIME_PHASE_LOCAL_READY, DEVICE_STATUS_INTERNAL_ERROR,
                "local-ready SAFE_MODE test injection");
        }
        return;
    }
    bool force_setup = false;
    esp_err_t force_setup_err = device_status_to_platform_error(
        configuration_service_take_force_setup(&force_setup));
    if (force_setup_err != ESP_OK) {
        /* This is the same authoritative configuration snapshot that carries
         * network credentials and the Hub token.  Continuing into radio or
         * gateway startup after its one-shot flag cannot be read/committed
         * would violate the fail-closed configuration boundary. */
        const device_status_t force_setup_status =
            startup_status_from_esp_err(force_setup_err);
        const startup_safe_mode_entry_result_t safe_mode_result =
            startup_enter_safe_mode(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                                    force_setup_status,
                                    "configuration force-setup request");
        if (safe_mode_result == STARTUP_SAFE_MODE_ENTRY_NOT_STARTED) {
            startup_enter_degraded(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                                   force_setup_status,
                                   "configuration force-setup request");
        } else if (safe_mode_result == STARTUP_SAFE_MODE_ENTRY_TERMINAL_FAILURE) {
            startup_enter_safe_mode_terminal_failure(
                DEVICE_RUNTIME_PHASE_LOCAL_READY, force_setup_status,
                "configuration force-setup request");
        }
        return;
    }
    if (force_setup || provisioning_failure_injection_force_portal_at_boot()) {
        ESP_LOGW(TAG, "booting directly into %s configuration portal%s",
                 force_setup ? "requested" : "test-forced",
                 provisioning_failure_injection_force_portal_at_boot()
                     ? " (test injection)" : "");
        provisioning_service_start_portal(false);
        return;
    }
    // Keep the explicit board-specific startup surface until the Welcome/wake-word
    // sequence publishes ready. Do not transition to standby here.
    if (!s_wifi_ssid[0] && !device_connectivity_is_active_cellular()) {
        provisioning_service_start_portal(false);
        return;
    }
    // A configured device runs as a normal Wi-Fi station. Being out of range
    // is an offline runtime condition, not evidence that provisioning was
    // lost. Keep both Wi-Fi credentials and the paired gateway token in NVS,
    // leave the normal pet/status surface visible, and let the Wi-Fi event
    // handler reconnect automatically when this SSID is reachable again.
    // Setup is entered only when no SSID was ever saved or after the user's
    // deliberate long-press reset.
    bool network_ready = device_connectivity_is_active_cellular()
                             ? start_cellular()
                             : start_wifi();
    // Wi-Fi boards have an independent wall-clock source and must not depend
    // on a successful Hub handshake before the standby clock or persisted
    // alarms can advance.  Start SNTP only after esp_wifi_start() has returned,
    // preserving the startup stability boundary above.  ML307 has no
    // ESP-NETIF route, so Fangtang 4G continues to use authenticated
    // handshake serverTime in apply_gateway_server_time().
    if (!device_connectivity_is_active_cellular()) {
        (void)clock_sync_service_start(false);
    }
    // Delay the alarm task until the authenticated gateway startup has
    // released TLS/PSRAM pressure. start_gateway_ready_tasks() initializes it
    // before the outgoing poll can expose any alarm tool.
    // From this point onward a late Wi-Fi DHCP event may safely start the Hub
    // transaction.  This is deliberately after alarm initialization: starting
    // TLS from IP_EVENT_STA_GOT_IP during esp_wifi_start() recreated the same
    // startup overlap that the ordering above is designed to prevent.
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_startup_allowed = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!network_ready && !device_connectivity_is_active_cellular()) {
        network_ready = device_connectivity_is_active_uplink_ready();
        if (network_ready) {
            ESP_LOGI(TAG, "Wi-Fi recovered at startup boundary; continuing gateway startup");
        }
    }
    if (!network_ready) {
        if (s_boot_provisioning_staged) {
            /* A candidate that cannot obtain even local uplink confirmation
             * must not replace a working device indefinitely.  The durable
             * confirmed snapshot is still intact; discard only the candidate
             * and reboot through the normal hardware-profile startup path. */
            device_status_t rollback_status =
                configuration_service_rollback_staged_provisioning();
            if (rollback_status == DEVICE_STATUS_OK) {
                ESP_LOGW(TAG, "unconfirmed provisioning candidate has no uplink; restarting confirmed configuration");
                esp_restart();
            }
            ESP_LOGE(TAG, "cannot roll back unreachable provisioning candidate: status=%d",
                     (int)rollback_status);
        }
        pet("alert");
        ESP_LOGW(TAG, "saved Wi-Fi is currently unavailable; preserving configuration and retrying in station mode");
        scene_presenter_publish_message("网络暂时不可用", "配置已保留，正在自动重连");
        return;
    }
    // Do not allocate the ESP-SR model while the first TLS pairing/handshake
    // is being established. Both are PSRAM-heavy; starting them concurrently
    // can make mbedtls_ssl_setup() fail with PSA_ERROR_INSUFFICIENT_MEMORY
    // (-0x008D). start_gateway_ready_tasks() starts the listener immediately
    // after the authenticated handshake has released its TLS allocations.
    // Run TLS/HTTP work on core 1. Performing it in the framework main task on
    // core 0 starves that core's interrupt watchdog during TLS initialization.
    if (!gateway_transport_start_startup_task()) {
        pet("alert");
        scene_presenter_publish_message("设备启动失败", "无法启动网关任务");
    }
    return;

startup_core_no_memory:
    startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                           DEVICE_STATUS_RESOURCE_EXHAUSTED, "core startup allocation");
}
