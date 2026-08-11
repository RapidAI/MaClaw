#include <stdio.h>
#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include "cJSON.h"
#include "esp_crt_bundle.h"
#include "esp_event.h"
#include "esp_eap_client.h"
#include "esp_http_client.h"
#include "esp_http_server.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_mac.h"
#include "mbedtls/base64.h"
#include "mbedtls/platform_util.h"
#include "esp_netif.h"
#include "esp_netif_sntp.h"
#include "esp_partition.h"
#include "esp_random.h"
#include "esp_system.h"
#include "esp_spiffs.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/inet.h"
#include "lwip/sockets.h"
#include "nvs.h"
#include "nvs_flash.h"
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
#include "provisioning_failure_injection.h"
#include "task_registry.h"
#include "weather_cache_service.h"
#include "meeting_recovery_service.h"
#include "configuration_service.h"
#include "platform_lifecycle.h"

#define WIFI_CONNECT_TIMEOUT_MS 20000
// 多热点逐个尝试时每个候选的连接超时，避免 5 个候选把启动拖得过长。
#define WIFI_CANDIDATE_CONNECT_TIMEOUT_MS 10000
#define CELLULAR_CONNECT_TIMEOUT_MS 60000
#define STARTUP_TRANSPORT_SELECTOR_WINDOW_MS 1800
/* A failed cold start must reach its degraded diagnostics state predictably.
 * Every rollback child consumes this one deadline; it is not a fresh wait per
 * worker/service.  The HTTP server's own stop API remains a documented
 * unbounded ESP-IDF boundary, but all caller-controlled joins below share it. */
#define STARTUP_ROLLBACK_TIMEOUT_MS 6000u
#define RESPONSE_CAPACITY 16384
#define HANDSHAKE_RESPONSE_CAPACITY 24576
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
#define GATEWAY_RETRY_INITIAL_MS 2000
#define GATEWAY_RETRY_MAX_MS 60000
// Once MultiNet is listening, give the optional boot greeting only a short
// grace period. Hardware/profile messages may precede it in the outgoing
// queue; they must not keep activation input blocked for most of the boot.
#define STARTUP_WELCOME_TIMEOUT_MS 2500
#define CLOCK_SYNC_WAIT_MS 12000
#define CLOCK_SYNC_RETRY_MS 30000
#define COMMAND_RESULT_PROGRESS_MS 15000
#define COMMAND_CANCEL_WORKER_TIMEOUT_MS 13000
#define COMMAND_CANCEL_ACKNOWLEDGEMENT_MS 450
// Rich text can carry dozens of 24x24 dynamic glyph bitmaps. Field captures
// reached 96 KiB for one (limit=1) item, so size this PSRAM-backed buffer with
// enough headroom to keep the outgoing cursor moving without burdening the
// scarce internal heap used by Wi-Fi/TLS and ESP-SR.
#define OUTGOING_RESPONSE_CAPACITY (256 * 1024)
#define COMMAND_SUBMIT_RETRY_COUNT 3
#define VOICE_UPLOAD_RETRY_COUNT 3
#define MEETING_RESUME_RETRY_INITIAL_MS 5000
#define MEETING_RESUME_RETRY_MAX_MS 300000
#define SETUP_AP_IP_ADDR "192.168.4.1"
/* DHCP option 114 identifies a Captive Portal API (RFC 8910/RFC 8908), not
 * the human-facing login form itself.  Clients which understand the option
 * fetch this endpoint and receive the form URL below. */
#define SETUP_CAPTIVE_PORTAL_URI "http://192.168.4.1/captive-portal/api"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define SETUP_SCAN_MAX_APS 24
#define SETUP_SSID_OPTIONS_CAPACITY 6144
#define SETUP_SSID_CHOICES_CAPACITY (SETUP_SCAN_MAX_APS * WIFI_VALUE_CAPACITY)
#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_MAX_PER_MESSAGE 96
#define PET_ASSET_MAX_DIMENSION 256
#define PET_ASSET_MAX_FRAMES 8
#define PET_ASSET_BYTES_PER_PIXEL 3
#define PET_ASSET_DEFAULT_FRAME_MS 450
#define PET_ASSET_MAX_BYTES (PET_ASSET_MAX_DIMENSION * PET_ASSET_MAX_DIMENSION * PET_ASSET_BYTES_PER_PIXEL)
#define PET_ASSET_STARTUP_TRANSACTION_ATTEMPTS 3
#define PET_ASSET_STARTUP_RETRY_DELAY_MS 3000
#define PET_ASSET_CACHE_META_PATH "/storage/pet_asset.meta"
#define PET_ASSET_CACHE_META_TMP_PATH "/storage/pet_asset.meta.tmp"
#define PET_ASSET_CACHE_FRAME_PATH_FORMAT "/storage/pet_asset_%u.rgb565a8"
#define PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT "/storage/pet_asset_%u.tmp"
#define MEETING_WAV_PATH "/storage/meeting.wav"
#define MEETING_SAMPLE_RATE 16000
#define MEETING_DEFAULT_CHUNK_SIZE (1U << 20)
#define MEETING_MIN_CHUNK_SIZE (64U << 10)
#define MEETING_MAX_CHUNK_SIZE (8U << 20)
#define MEETING_IO_BUFFER_SIZE 16384
#define MEETING_RESPONSE_CAPACITY 2048
#define MEETING_INTERNAL_TLS_RESERVE (16U * 1024U)
#define MEETING_BASE_PATH_CAPACITY 96
#define MEETING_RECORDING_ID_CAPACITY 96

static const char *TAG = "maclaw_client";
// ESP-IDF DHCP server retains this pointer for the duration of the AP. Keep
// it static: a stack buffer would become invalid after portal startup returns.
static const char s_setup_captive_portal_uri[] = SETUP_CAPTIVE_PORTAL_URI;
static int64_t s_cursor;
static char s_boot_session_id[33];
static char s_gateway_token[96];
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
static char s_gateway_url[URL_CAPACITY];
static char s_pair_code[PAIR_CODE_CAPACITY];
static char s_device_id[DEVICE_ID_CAPACITY];
static httpd_handle_t s_setup_server;
/* Portal HTTP is a separate resource from the AP, DHCP and captive DNS.  Its
 * admission flag is closed before httpd_stop(), so a teardown can never
 * accept a new credential-changing request while its sockets are draining. */
static bool s_setup_http_admission_open;
/* Keep physical network-root ownership finer than the public-ready bit.
 * `s_network_initialized` means the complete ESP-NETIF + default-event-loop
 * transaction succeeded.  A failure between those two calls must still leave
 * an exact resource owner for a later fail-closed rollback/retry; otherwise a
 * residual netif singleton is invisible to stop_network_core_transaction(). */
static bool s_netif_initialized;
static bool s_default_event_loop_created;
static bool s_network_initialized;
static bool s_startup_ui_initialized;
static bool s_wifi_driver_initialized;
static bool s_ap_netif_created;
static bool s_sta_netif_created;
static bool s_wifi_started;
static esp_netif_t *s_sta_netif;
/* Keep exactly the event-registration handles created by this composition
 * root.  The default Wi-Fi netif helpers manage their own system handlers;
 * rollback must unregister only the application callback instances it owns. */
static esp_event_handler_instance_t s_wifi_event_instance;
static esp_event_handler_instance_t s_wifi_got_ip_event_instance;
static esp_event_handler_instance_t s_wifi_assigned_ip_event_instance;
/* ESP-IDF event-handler unregister serializes with the default event-loop
 * mutex, but exposes no caller deadline.  Keep our callback admission and
 * in-flight drain separate so the composition root can prove no application
 * callback is still touching Connectivity/UI state before it stops the radio
 * or waits in SDK unregistration. */
static SemaphoreHandle_t s_wifi_event_callbacks_drained;
static bool s_wifi_event_callback_admission_open;
static uint32_t s_wifi_event_callbacks_inflight;
static TaskHandle_t s_cellular_recovery_task;
static SemaphoreHandle_t s_cellular_recovery_start_gate;
static SemaphoreHandle_t s_cellular_recovery_stopped;
static bool s_cellular_recovery_stop_requested;
static bool s_cellular_recovery_starting;
static bool s_cellular_recovery_admission_open;
// The provisioning portal needs APSTA mode to scan nearby networks, but a
// first-run portal must not repeatedly attempt an unconfigured STA join.
static bool s_station_auto_connect;
static bool s_station_expected_disconnect;
// ESP-IDF's enterprise Wi-Fi teardown path must only run after enterprise
// mode was actually enabled. Calling it during a cold personal-Wi-Fi boot can
// leave the Wi-Fi driver's scan timer with a stale task notification target,
// which then asserts just after esp_wifi_start().
static bool s_wifi_enterprise_enabled;
/* The setup AP/QR page is a foreground flow.  Keep its ownership explicit so
 * an ambient idle deadline cannot blank the only configuration surface. */
static device_power_lease_t s_setup_power_lease;
static esp_netif_t *s_setup_ap_netif;
static TaskHandle_t s_dns_task;
static SemaphoreHandle_t s_dns_start_gate;
static SemaphoreHandle_t s_dns_stopped;
/* A created DNS task is not proof that UDP/53 was actually bound.  Portal
 * startup waits for this one-shot result before admitting the HTTP form. */
static SemaphoreHandle_t s_dns_ready;
static bool s_dns_ready_success;
static bool s_dns_stop_requested;
static bool s_dns_starting;
static bool s_dns_admission_open;
/* The provisioning portal is one composite resource: HTTP, captive DNS,
 * session scratch, a foreground power lease and a reversible radio snapshot.
 * Its callers originate in button/input, gateway recovery, boot and the
 * post-save reset coordinator, so a single transaction gate is required to
 * prevent one generation's stop from racing another generation's start. */
static SemaphoreHandle_t s_setup_portal_mutex;
static SemaphoreHandle_t s_setup_options_mutex;
// Provisioning-only scratch storage is allocated when the portal starts. It
// must not permanently shift ESP-IDF's prebuilt Wi-Fi globals in internal
// DRAM during every configured station boot.
static char *s_setup_ssid_options;
static char *s_setup_ssid_choices;
static wifi_ap_record_t *s_setup_scan_records;
static TaskHandle_t s_gateway_task;
static volatile bool s_gateway_startup_running;
static SemaphoreHandle_t s_gateway_startup_start_gate;
static SemaphoreHandle_t s_gateway_startup_stopped;
static SemaphoreHandle_t s_gateway_startup_client_mutex;
static esp_http_client_handle_t s_gateway_startup_active_client;
static bool s_gateway_startup_stop_requested;
static bool s_gateway_startup_starting;
// Bread's first TLS certificate verification is cache/PSRAM intensive. Its
// alarm scheduler is deliberately initialized after that transaction, not in
// parallel with it; see ensure_alarm_manager_started().
static bool s_alarm_manager_started;
// Radio/IP callbacks can run before app_main() has finished the stability-
// sensitive startup boundary (Wi-Fi driver, clock and alarm scheduler).  They
// may only launch TLS/pairing after app_main explicitly opens this gate.
static volatile bool s_gateway_startup_allowed;
static TaskHandle_t s_interaction_task;
static device_power_lease_t s_interaction_power_lease;
static SemaphoreHandle_t s_interaction_start_gate;
static SemaphoreHandle_t s_interaction_stopped;
static bool s_interaction_stop_requested;
static bool s_interaction_starting;
static TaskHandle_t s_meeting_task;
static device_power_lease_t s_meeting_power_lease;
/* The recording/upload worker owns live audio, a retained WAV and sometimes
 * a Wi-Fi HTTP client.  It therefore needs its own lifecycle contract rather
 * than being mistaken for either the short resume supervisor or a permanent
 * audio-driver task. */
static SemaphoreHandle_t s_meeting_task_start_gate;
static SemaphoreHandle_t s_meeting_task_stopped;
static SemaphoreHandle_t s_meeting_task_client_mutex;
static esp_http_client_handle_t s_meeting_task_active_client;
static bool s_meeting_task_stop_requested;
static TaskHandle_t s_meeting_resume_supervisor_task;
static SemaphoreHandle_t s_meeting_resume_supervisor_start_gate;
static SemaphoreHandle_t s_meeting_resume_supervisor_stopped;
static bool s_meeting_resume_supervisor_stop_requested;
static bool s_meeting_resume_supervisor_starting;
static volatile bool s_wake_restart_scheduled;
static volatile bool s_wake_restart_after_startup;
static TaskHandle_t s_wake_restart_task;
static SemaphoreHandle_t s_wake_restart_start_gate;
static SemaphoreHandle_t s_wake_restart_stopped;
static bool s_wake_restart_stop_requested;
static bool s_wake_restart_admission_open;
static TaskHandle_t s_meeting_capability_refresh_task;
static SemaphoreHandle_t s_meeting_capability_refresh_start_gate;
static SemaphoreHandle_t s_meeting_capability_refresh_stopped;
static SemaphoreHandle_t s_meeting_capability_refresh_client_mutex;
static esp_http_client_handle_t s_meeting_capability_refresh_active_client;
static bool s_meeting_capability_refresh_stop_requested;
static bool s_meeting_capability_refresh_starting;
static bool s_meeting_task_running;
/* The worker receives this explicit context instead of inferring ownership
 * from a naked resume flag or global task handle. */
typedef struct {
    uint32_t generation;
    bool resume_only;
} meeting_task_context_t;
static TaskHandle_t s_ambient_task;
static SemaphoreHandle_t s_ambient_task_stopped;
static TaskHandle_t s_clock_sync_task;
static SemaphoreHandle_t s_clock_sync_start_gate;
static SemaphoreHandle_t s_clock_sync_stopped;
static bool s_clock_sync_stop_requested;
/* The retry monitor and esp-netif's SNTP client have separate lifetimes.
 * Keep their ownership explicit: the monitor must leave before a rollback may
 * deinitialize the client, otherwise a late sync_wait/restart would access a
 * released esp-netif SNTP singleton. */
static bool s_sntp_initialized;
static TaskHandle_t s_gateway_poll_task;
static SemaphoreHandle_t s_gateway_poll_stopped;
static SemaphoreHandle_t s_gateway_poll_client_mutex;
static esp_http_client_handle_t s_gateway_poll_active_client;
static bool s_gateway_poll_stop_requested;
static TaskHandle_t s_startup_pet_asset_task;
static SemaphoreHandle_t s_startup_pet_asset_stopped;
static bool s_startup_pet_asset_stop_requested;
static SemaphoreHandle_t s_pet_cache_flash_mutex;
static TaskHandle_t s_pet_cache_task;
static bool s_pet_cache_stop_requested;
static TaskHandle_t s_setup_restart_task;
static SemaphoreHandle_t s_setup_restart_start_gate;
static SemaphoreHandle_t s_setup_restart_stopped;
static bool s_setup_restart_stop_requested;
static bool s_setup_restart_starting;
static bool s_setup_restart_admission_open;
/* Fallback only when the durable force-setup request cannot be committed.
 * It still changes radio/portal state, so it is a normal Connectivity-owned
 * worker rather than an untracked fire-and-forget task. */
static TaskHandle_t s_deferred_setup_task;
static SemaphoreHandle_t s_deferred_setup_start_gate;
static SemaphoreHandle_t s_deferred_setup_stopped;
static bool s_deferred_setup_stop_requested;
static bool s_deferred_setup_starting;
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
static volatile bool s_command_display_locked;
static volatile bool s_command_cancel_requested;
static volatile bool s_command_cancel_enabled;
static bool s_command_cancel_ui_shown;
// The activation down edge is intentionally useful while recording: it stops
// capture immediately instead of waiting for the 500 ms single/double gesture
// decision. Consume the completed gesture from that same physical contact so
// its delayed SHORT can never dismiss the new thinking/result surface or start
// another command. A fresh down edge disarms this one-contact barrier.  The
// scanner is allowed to lose a completion during controller recovery, so the
// barrier has a bounded lifetime; it must never consume a later, independent
// press in the next command.
#define INPUT_GESTURE_DRAIN_WINDOW_US 1500000ULL
static bool s_command_capture_stop_gesture_pending;
static device_input_source_t s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
static uint64_t s_command_capture_stop_gesture_deadline_us;
#define CANCELLED_REPLY_SLOTS 4
#define COMMAND_REPLY_ID_CAPACITY 96
#define RESULT_SPEECH_IDLE_TIMEOUT_US (5LL * 60LL * 1000000LL)
static char s_active_command_reply_to[COMMAND_REPLY_ID_CAPACITY];
// A terminal text can deliberately precede its TTS parts. Retain only that
// exact correlation and its declared remaining part count after the command
// worker exits, so result-page speech is accepted without admitting stale audio.
// The idle deadline also bounds a partially generated/failed multipart reply;
// each successfully consumed part refreshes it for the next part.
static char s_result_speech_reply_to[COMMAND_REPLY_ID_CAPACITY];
static unsigned s_result_speech_parts_remaining;
static int64_t s_result_speech_deadline_us;
static char s_cancelled_command_reply_to[CANCELLED_REPLY_SLOTS][COMMAND_REPLY_ID_CAPACITY];
static unsigned s_cancelled_command_reply_next;
static int64_t s_ignore_command_input_until_us;
static uint32_t s_interaction_generation;
static uint32_t s_cancel_requested_generation;
static uint32_t s_cancel_ui_ready_generation;
static int64_t s_command_timing_started_us;
static int64_t s_command_timing_capture_done_us;
static int64_t s_command_timing_upload_done_us;
static int64_t s_command_timing_accepted_us;
static int64_t s_command_timing_first_progress_us;
// Foreground traffic must never wait behind the outgoing long poll. Each lane
// owns both its mutex and persistent esp_http_client handle; no handle is ever
// operated by two tasks concurrently.
static SemaphoreHandle_t s_http_mutex;
static esp_http_client_handle_t s_gateway_http_client;
static char s_gateway_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_poll_http_mutex;
static esp_http_client_handle_t s_gateway_poll_http_client;
static char s_gateway_poll_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_asset_http_mutex;
static esp_http_client_handle_t s_gateway_asset_http_client;
static char s_gateway_asset_http_origin[URL_CAPACITY];
static SemaphoreHandle_t s_gateway_asset_client_mutex;
static esp_http_client_handle_t s_gateway_asset_active_client;
static esp_timer_handle_t s_startup_pet_retry_timer;
/* Retry timer callbacks run on ESP-IDF's timer-service task.  Stop alone does
 * not prove a callback that already began has returned, so lifecycle teardown
 * tracks the small callback lease explicitly before it destroys the timer or
 * releases the optional pet domain it can otherwise re-admit. */
static SemaphoreHandle_t s_startup_pet_retry_callback_drained;
/* Serializes the public retry-arm path with lifecycle stop.  esp_timer_stop()
 * leaves a handle reusable, so callback admission alone cannot prevent a
 * late caller from rearming a retained one-shot timer after rollback. */
static SemaphoreHandle_t s_startup_pet_retry_timer_mutex;
static bool s_startup_pet_retry_callback_admission_open;
static uint32_t s_startup_pet_retry_callbacks_inflight;
static unsigned s_startup_pet_retry_count;
/* esp_timer callbacks run on ESP-IDF's small timer-service stack.  They may
 * only mark deferred work; allocating pet frames, sampling the resource
 * policy, and creating the optional worker belongs to a normal task. */
static volatile bool s_startup_pet_retry_due;
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
static esp_err_t stop_startup_pet_asset_task(uint32_t timeout_ms);
static esp_err_t stop_startup_pet_retry_timer(uint32_t timeout_ms);
static void startup_pet_retry_timer_cb(void *arg);
static bool schedule_startup_pet_retry_timer(void);
static bool startup_pet_asset_stop_requested(void);
static esp_err_t stop_pet_cache_task(uint32_t timeout_ms);
static bool pet_cache_stop_requested(void);
static void discard_gateway_asset_http_client(void);
static bool begin_server_audio_wake_memory_lease(const char *source);
static void begin_optional_media_wake_memory_lease(const char *source);
static bool finish_server_audio_wake_memory_lease(void);
static bool finish_optional_media_wake_memory_lease(void);
static void finish_optional_pet_asset_memory_lease(void);
static bool server_audio_wake_memory_lease_active(void);
static void startup_stop_local_workers(void);
static uint32_t startup_rollback_remaining_timeout_ms(int64_t deadline_us);
static esp_err_t stop_command_cancel_worker(uint32_t timeout_ms);
static esp_err_t stop_output_volume_persist_worker(uint32_t timeout_ms);
static esp_err_t stop_command_cancel_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_output_volume_persist_registry_entry(void *context,
                                                            uint32_t timeout_ms);
// Exactly one pet pack may own the renderer at a time.  A cold-start pack is
// deliberately cancellable, but once it starts touching the display its final
// install/cache sequence must not race an online pet_profile update.
static SemaphoreHandle_t s_pet_asset_apply_mutex;
// Protects the foreground client pointer through cancel/cleanup. The general
// HTTP mutex cannot serve this purpose because it remains owned for the whole
// request and cancellation must run concurrently with esp_http_client_perform.
static SemaphoreHandle_t s_foreground_http_client_mutex;
static esp_http_client_handle_t s_foreground_http_client;
static SemaphoreHandle_t s_command_cancel_ui_ready;
static TaskHandle_t s_command_cancel_task;
static SemaphoreHandle_t s_command_cancel_stopped;
static bool s_command_cancel_stop_requested;
static SemaphoreHandle_t s_interaction_lock;
static SemaphoreHandle_t s_nvs_mutex;
// The outgoing long-poll worker deliberately has a PSRAM-backed stack so it
// can decode audio replies without consuming the small internal-RAM budget.
// Flash writes temporarily disable caches, however, and ESP-IDF requires the
// calling task's stack to remain accessible while that happens.  Never invoke
// NVS directly from that worker; route volume persistence through this small
// internal-stack worker instead.
/* Display brightness is a standalone scalar key rather than a new field in
 * the versioned configuration blob: it shares no schema with the Wi-Fi/
 * gateway credentials and never needs their migration machinery. */
#define DISPLAY_BRIGHTNESS_NVS_NAMESPACE "maclaw"
#define DISPLAY_BRIGHTNESS_NVS_KEY "brightness"
#define DISPLAY_SLEEP_NVS_KEY "screen_sleep_s"

typedef struct {
    unsigned percent;
    uint32_t generation;
    bool brightness;
    bool stop;
} output_volume_persist_request_t;

typedef struct {
    esp_err_t result;
    uint32_t generation;
} output_volume_persist_reply_t;

static QueueHandle_t s_output_volume_persist_queue;
static QueueHandle_t s_output_volume_persist_reply_queue;
static SemaphoreHandle_t s_output_volume_persist_request_mutex;
static TaskHandle_t s_output_volume_persist_task_handle;
static SemaphoreHandle_t s_output_volume_persist_stopped;
static uint32_t s_output_volume_persist_generation;
static bool s_output_volume_persist_stop_requested;
static unsigned s_configured_output_volume = 70;
static bool s_configured_output_volume_saved;
static portMUX_TYPE s_task_state_lock = portMUX_INITIALIZER_UNLOCKED;
static char s_weather_summary[24];
static char s_weather_location[24];
static int s_weather_temperature_c;
static int64_t s_weather_expires_at_ms;
static bool s_weather_valid;
static void on_wake_word(void *arg);
static void on_fall_detection_event(fall_detection_event_t event, void *arg);
static esp_err_t stop_setup_portal_transaction(uint32_t timeout_ms,
                                               bool restore_wake_word);
static esp_err_t stop_setup_portal_transaction_locked(uint32_t timeout_ms,
                                                      bool restore_wake_word);

// NVS may contain Wi-Fi credentials, the pairing token, alarms and the
// meeting-recovery cursor.  ESP-IDF suggests erasing the whole partition for
// these two errors, but doing so silently converts a recoverable storage or
// schema fault into data loss.  Keep the partition untouched until a future
// versioned persistence migration or the explicit, authenticated recovery
// workflow can make that decision with the user.
static esp_err_t initialize_nvs_preserving_user_data(void) {
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_LOGE(TAG,
                 "NVS unavailable (%s); preserving user data and entering diagnostic recovery",
                 esp_err_to_name(err));
        return err;
    }
    return err;
}

static void setup_restart_task(void *arg) {
    (void)arg;
    /* The HTTP handler creates this task before it is visible to the lifecycle
     * registry.  Do no work until that publication has completed: an early
     * startup rollback must be able to close admission and join it rather than
     * allowing an untracked delayed reset to fire later. */
    if (!s_setup_restart_start_gate ||
        xSemaphoreTake(s_setup_restart_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        if (s_setup_restart_task == self) s_setup_restart_task = NULL;
        s_setup_restart_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_setup_restart_stopped) xSemaphoreGive(s_setup_restart_stopped);
        (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                     (void *)self, 10);
        vTaskDelete(NULL);
        return;
    }

    /* Let esp_http_server flush its response, but make the delay cancellable.
     * A task notification is deliberately used instead of vTaskDelay(): it
     * provides the stop safe point without taking ownership of the portal,
     * DNS responder, HTTP server or Wi-Fi mode. */
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1200));
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_setup_restart_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) {
        goto finish;
    }
    /* Saving credentials is a terminal provisioning transition.  Reboot is
     * still the supported way to apply the new station configuration, but it
     * must not be the only cleanup mechanism: close portal admission, join
     * HTTP/DNS and release the logical session before the reset.  AP/DHCP and
     * Wi-Fi are intentionally left to that reset; this is not an APSTA->STA
     * runtime-restart claim. */
    esp_err_t portal_stop_err = stop_setup_portal_transaction(500, false);
    if (portal_stop_err != ESP_OK) {
        /* The saved configuration has already committed.  A physical restart
         * is safer than retaining an admission-closed but partially drained
         * portal whose new configuration cannot take effect in this boot. */
        ESP_LOGW(TAG, "setup portal cleanup before restart incomplete: %s",
                 esp_err_to_name(portal_stop_err));
    }
    /* `stop_setup_restart_task()` may arrive while HTTP/DNS are draining.
     * Observe its token a second time before committing the reset; otherwise
     * the caller could receive our completion and continue rollback while this
     * coordinator restarts the chip. */
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_setup_restart_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
finish:
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_setup_restart_task == self) s_setup_restart_task = NULL;
    s_setup_restart_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_restart_stopped) xSemaphoreGive(s_setup_restart_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    if (stop_requested) {
        ESP_LOGI(TAG, "setup restart coordinator stopped before reset");
        vTaskDelete(NULL);
        return;
    }
    ESP_LOGI(TAG, "setup saved; restarting into normal mode");
    esp_restart();
}

/* This coordinator owns the post-save delay and the terminal user-space
 * portal cleanup before an intentional reset.  It still does not own AP/STA,
 * DHCP or Wi-Fi-event lifetimes; those stay with the reset/physical network
 * composition root and are not claimed as runtime-restartable here. */
static esp_err_t stop_setup_restart_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_admission_open = false;
    s_setup_restart_stop_requested = true;
    task = s_setup_restart_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_setup_restart_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_setup_restart_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "setup restart coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_setup_restart_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_setup_restart_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_setup_restart_task(timeout_ms);
}

static bool schedule_setup_restart(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_setup_restart_admission_open;
    bool already_starting = s_setup_restart_starting || s_setup_restart_task != NULL;
    if (admission_open && !already_starting) s_setup_restart_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "setup restart rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_setup_restart_start_gate || !s_setup_restart_stopped ||
        provisioning_failure_injection_lifecycle_primitives_unavailable()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "setup restart lifecycle primitives unavailable%s",
                 provisioning_failure_injection_lifecycle_primitives_unavailable()
                     ? " (test injection)" : "");
        return false;
    }
    while (xSemaphoreTake(s_setup_restart_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    BaseType_t create_result = provisioning_failure_injection_task_create_fails()
                                   ? pdFAIL
                                   : xTaskCreate(setup_restart_task,
                                                 "maclaw_setup_restart", 2048,
                                                 NULL, 2, &task);
    if (create_result != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_starting = false;
        s_setup_restart_task = NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot schedule restart after setup save%s",
                 provisioning_failure_injection_task_create_fails()
                     ? " (test injection)" : "");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_restart_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = provisioning_failure_injection_task_registry_register_fails()
                                 ? ESP_ERR_NO_MEM
                                 : task_registry_register(&(task_registry_entry_t){
                                       .struct_size = sizeof(task_registry_entry_t),
                                       .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
                                       .name = "setup_restart",
                                       .context = (void *)task,
                                       .stop = stop_setup_restart_registry_entry,
                                   });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup restart coordinator: %s%s",
                 esp_err_to_name(registry_err),
                 provisioning_failure_injection_task_registry_register_fails()
                     ? " (test injection)" : "");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_setup_restart_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_setup_restart_start_gate);
        (void)stop_setup_restart_task(500);
        return false;
    }
    xSemaphoreGive(s_setup_restart_start_gate);
    return true;
}

/* A post-save coordinator is deliberately terminal: once credentials have
 * committed, this generation must either reset or remain fail-closed.  Do not
 * reopen its admission from a later manual/recovery portal request before the
 * reset takes place; a second coordinator could otherwise reuse its stopped
 * token while the first task is still unwinding. */
static bool setup_restart_is_pending(void) {
    bool pending;
    taskENTER_CRITICAL(&s_task_state_lock);
    pending = s_setup_restart_starting || s_setup_restart_task != NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return pending;
}
// Once SNTP supplies an epoch, the display advances from ESP32's monotonic
// microsecond counter. This keeps the visible seconds moving independently of
// network timing and avoids a network request or SNTP poll per screen update.
static time_t s_display_clock_epoch;
static int64_t s_display_clock_anchor_us;
static bool s_display_clock_valid;
static volatile bool s_clock_sync_complete;

typedef enum {
    MEETING_IDLE = 0,
    MEETING_STARTING,
    MEETING_RECORDING,
    MEETING_PAUSED,
    MEETING_FINALIZING,
    MEETING_UPLOADING,
    MEETING_PROCESSING,
    MEETING_DONE,
    MEETING_ERROR,
} meeting_state_t;

static volatile meeting_state_t s_meeting_state = MEETING_IDLE;
static bool s_storage_mounted;
static bool s_meeting_available;
static size_t s_meeting_chunk_size = MEETING_DEFAULT_CHUNK_SIZE;
static char s_meeting_base_path[MEETING_BASE_PATH_CAPACITY] = "/api/device-gateway/v1/meeting-recordings";
static char s_meeting_process_mode[12] = "keep";
static bool s_meeting_pending;
static int32_t s_meeting_next_chunk;
static int32_t s_meeting_phase;
static char s_meeting_recording_id[MEETING_RECORDING_ID_CAPACITY];
static volatile uint32_t s_meeting_elapsed_seconds;
// Set as soon as a short voice command is requested. Background meeting
// recovery yields between chunks so the interactive upload gets the HTTP lock.
static volatile bool s_foreground_http_requested;

// Hardware gestures are interpreted from this application-owned foreground
// phase, never from whatever screen happened to be painted most recently.
// In particular, a SECONDARY gesture during a voice command must not become a
// meeting request just because the interaction task is between creation and
// publishing its task handle.
typedef enum {
    INTERACTION_IDLE = 0,
    INTERACTION_RECORDING,
    INTERACTION_PROCESSING,
    INTERACTION_RESULT,
} interaction_phase_t;

static volatile interaction_phase_t s_interaction_phase = INTERACTION_IDLE;

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data);

/* Event callbacks execute on ESP-IDF's default loop.  Do not borrow a service
 * or UI state once lifecycle admission has closed.  The counter covers a
 * callback that was selected immediately before unregister: ESP-IDF marks its
 * handler unregistered, but its API does not offer a caller-bounded drain
 * guarantee. */
static bool wifi_event_callback_enter(void) {
    bool entered = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_wifi_event_callback_admission_open) {
        ++s_wifi_event_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    return entered;
}

static void wifi_event_callback_leave(void) {
    bool drained = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_wifi_event_callbacks_inflight > 0) {
        --s_wifi_event_callbacks_inflight;
        drained = !s_wifi_event_callback_admission_open &&
                  s_wifi_event_callbacks_inflight == 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (drained && s_wifi_event_callbacks_drained) {
        xSemaphoreGive(s_wifi_event_callbacks_drained);
    }
}

static esp_err_t stop_wifi_event_callback_admission(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    bool already_drained = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wifi_event_callback_admission_open = false;
    already_drained = s_wifi_event_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_drained) return ESP_OK;
    if (!s_wifi_event_callbacks_drained ||
        xSemaphoreTake(s_wifi_event_callbacks_drained, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    return ESP_OK;
}

typedef struct {
    char *data;
    size_t capacity;
    size_t len;
    int status;
    bool truncated;
} http_response_t;

static bool gateway_auth_failed(const http_response_t *response, esp_err_t err);
static void process_update_metadata(cJSON *update, bool defer_presentation);
static void publish_pending_update_reminder(void);
static void save_ambient_weather(void);
static void load_ambient_weather(void);
static esp_err_t poll_reply(void);
static esp_err_t stop_gateway_poll_task(uint32_t timeout_ms);
static esp_err_t stop_ambient_clock_task(uint32_t timeout_ms);
static esp_err_t stop_clock_sync_task(uint32_t timeout_ms);
static esp_err_t stop_sntp_service(uint32_t timeout_ms);
static esp_err_t stop_gateway_poll_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_ambient_clock_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_clock_sync_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_meeting_resume_supervisor(uint32_t timeout_ms);
static esp_err_t stop_meeting_resume_supervisor_registry_entry(void *context,
                                                                 uint32_t timeout_ms);
static esp_err_t stop_meeting_capability_refresh_task(uint32_t timeout_ms);
static esp_err_t stop_meeting_capability_refresh_registry_entry(void *context,
                                                                  uint32_t timeout_ms);
static esp_err_t stop_meeting_task(uint32_t timeout_ms);
static esp_err_t stop_meeting_task_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_interaction_task(uint32_t timeout_ms);
static esp_err_t stop_interaction_task_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_gateway_startup_task(uint32_t timeout_ms);
static esp_err_t stop_gateway_startup_registry_entry(void *context,
                                                       uint32_t timeout_ms);
static esp_err_t stop_captive_dns_task(uint32_t timeout_ms);
static esp_err_t stop_captive_dns_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_setup_portal_http_server(void);
static void release_setup_portal_scratch(void);
static esp_err_t stop_network_core_transaction(uint32_t timeout_ms);
static esp_err_t stop_connectivity_root_transaction(uint32_t timeout_ms);
static esp_err_t send_text_event(const char *text, const char *reply_to);
static bool hardware_audio_url_allowed(const char *url);
static void digest_hex(const uint8_t digest[32], char out[65]);
static esp_err_t handle_client_tool_call(cJSON *item);
static void response_release(http_response_t *response);
static esp_err_t request(const char *method, const char *path, const char *content_type,
                         const char *body, int body_len, http_response_t *out);
static const char *json_string(cJSON *root, const char *key);
static bool json_number(cJSON *root, const char *key, int *value);
static int apply_glyphs_json(cJSON *glyphs);
static bool start_meeting_task(bool resume_only);
static bool meeting_operation_is_current(uint32_t generation);
static bool meeting_task_stop_requested(void);
static void meeting_task_set_active_http_client(esp_http_client_handle_t client);
static bool interaction_stop_requested(void);
static esp_err_t gateway_handshake(bool cold_start);
static void start_setup_portal(bool keep_station);
static void start_setup_portal_locked(bool keep_station);
static void build_setup_saved_networks_html(void);
/* 已存热点列表的页面片段在 httpd 任务上构建，沿用 PSRAM 缓冲。 */
#define SETUP_SAVED_HTML_CAPACITY 2048
static char *s_setup_saved_html;
static void schedule_wake_restart(void);
static esp_err_t stop_wake_restart_task(uint32_t timeout_ms);
static esp_err_t stop_wake_restart_registry_entry(void *context, uint32_t timeout_ms);
static bool schedule_setup_restart(void);
static esp_err_t stop_setup_restart_task(uint32_t timeout_ms);
static esp_err_t stop_setup_restart_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t stop_deferred_setup_task(uint32_t timeout_ms);
static esp_err_t stop_deferred_setup_registry_entry(void *context, uint32_t timeout_ms);
static esp_err_t audio_wake_word_stop(void);
static esp_err_t audio_wake_word_stop_with_timeout(uint32_t timeout_ms);
static esp_err_t clear_meeting_recovery(bool delete_audio);
static void pet(const char *state);
static void apply_deferred_startup_pet_asset(void);
static bool start_gateway_startup_task(void);
static bool ensure_alarm_manager_started(void);

static bool setup_portal_http_admission_open(void) {
    bool open;
    taskENTER_CRITICAL(&s_task_state_lock);
    open = s_setup_http_admission_open;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return open;
}

static void set_setup_portal_http_admission(bool open) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_setup_http_admission_open = open;
    taskEXIT_CRITICAL(&s_task_state_lock);
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
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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
        app_ui_show_text(title, detail);
    }
}

static void process_update_metadata(cJSON *update, bool defer_presentation) {
    int64_t now_epoch = time(NULL);
    if (now_epoch < 1672531200) now_epoch = 0;
    if (update_service_apply_metadata(update, now_epoch, defer_presentation) && !defer_presentation) {
        publish_pending_update_reminder();
    }
}

static bool meeting_is_active(void) {
    meeting_state_t state = s_meeting_state;
    return state != MEETING_IDLE && state != MEETING_DONE && state != MEETING_ERROR;
}

static void meeting_set_state(meeting_state_t state) {
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_state = state;
    taskEXIT_CRITICAL(&s_task_state_lock);
}
static void finish_interaction_task_with_surface(uint32_t generation,
                                                 bool restore_standby) {
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    app_ui_set_command_cancel_enabled(false);
    bool operation_current = operation_context_is_current(generation);
    taskENTER_CRITICAL(&s_task_state_lock);
    bool owns_interaction = operation_current && s_interaction_generation == generation &&
                            s_interaction_task == xTaskGetCurrentTaskHandle();
    if (owns_interaction) {
        s_interaction_task = NULL;
        s_interaction_phase = restore_standby ? INTERACTION_IDLE : INTERACTION_RESULT;
        s_foreground_http_requested = false;
        s_command_cancel_enabled = false;
        s_command_cancel_requested = false;
        s_cancel_requested_generation = 0;
        s_active_command_reply_to[0] = '\0';
        if (restore_standby) {
            s_result_speech_reply_to[0] = '\0';
            s_result_speech_parts_remaining = 0;
            s_result_speech_deadline_us = 0;
            s_command_display_locked = false;
        }
    }
    device_power_lease_t interaction_lease = DEVICE_POWER_LEASE_INVALID;
    if (owns_interaction) {
        interaction_lease = s_interaction_power_lease;
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    device_power_lease_release(interaction_lease);
    if (owns_interaction && restore_standby) {
        // A cancelled command ends on APP_UI_SURFACE_MESSAGE ("已取消"), so the
        // normal response-only dismiss path cannot restore the ambient screen.
        // Restore the whole shared UI model before admitting another command.
        app_ui_restore_standby();
        ESP_LOGI(TAG, "cancelled command returned to standby: generation=%lu",
                 (unsigned long)generation);
    }
    if (!owns_interaction) {
        ESP_LOGW(TAG, "stale interaction finish ignored: generation=%lu current=%lu",
                 (unsigned long)generation, (unsigned long)s_interaction_generation);
        if (s_interaction_stopped) xSemaphoreGive(s_interaction_stopped);
        (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_INTERACTION,
                                                     (void *)self, 10);
        vTaskDelete(NULL);
        return;
    }
    /* A poller may have committed the final response before waking this
     * worker.  In that case commit_terminal simply reports false; this task
     * still owns cleanup and must release the admission token exactly once. */
    (void)operation_context_commit_terminal(generation);
    // This is a binary admission token, not a mutex: the button task starts
    // the interaction task, which completes it on another task context.
    // Releasing a FreeRTOS mutex from that child task asserts and reboots.
    if (owns_interaction && s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    // The interaction worker now uses ordinary xTaskCreate() with an internal
    // RAM stack, so it must be destroyed by the matching FreeRTOS API.
    // vTaskDeleteWithCaps() asserts when given a normally allocated task.
    if (owns_interaction) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_interaction_stopped) xSemaphoreGive(s_interaction_stopped);
        (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_INTERACTION,
                                                     (void *)self, 10);
        schedule_wake_restart();
    }
    vTaskDelete(NULL);
}

static uint32_t elapsed_ms_between(int64_t started_us, int64_t finished_us) {
    return started_us > 0 && finished_us >= started_us
               ? (uint32_t)((finished_us - started_us) / 1000)
               : 0;
}

static void log_command_timing(const char *terminal) {
    int64_t now_us = esp_timer_get_time();
    ESP_LOGI(TAG,
             "command timing: terminal=%s capture=%ums upload=%ums submit=%ums firstProgress=%ums total=%ums",
             terminal ? terminal : "unknown",
             (unsigned)elapsed_ms_between(s_command_timing_started_us,
                                          s_command_timing_capture_done_us),
             (unsigned)elapsed_ms_between(s_command_timing_capture_done_us,
                                          s_command_timing_upload_done_us),
             (unsigned)elapsed_ms_between(s_command_timing_upload_done_us,
                                          s_command_timing_accepted_us),
             (unsigned)elapsed_ms_between(s_command_timing_accepted_us,
                                          s_command_timing_first_progress_us),
             (unsigned)elapsed_ms_between(s_command_timing_started_us, now_us));
}

static void finish_interaction_task(uint32_t generation) {
    finish_interaction_task_with_surface(generation, false);
}

/* A foreground meeting is an operation-context owner.  Keep this guard local
 * to the meeting flow while its UI and recorder state are migrated out of
 * main.c; background recovery deliberately has no foreground generation. */
static bool meeting_operation_is_current(uint32_t generation) {
    return generation != 0 && operation_context_matches(generation);
}

static bool interaction_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_interaction_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

// A local validation, capture, upload, or submission failure has no result to
// keep on screen.  Treat it like Bread Compact's short status acknowledgement:
// leave the message readable, then return every layer (application model,
// board renderer, and command admission state) to the ambient pet surface.
// Final remote replies deliberately continue to use finish_interaction_task(),
// so a user can read and dismiss them explicitly.
static void finish_interaction_message(uint32_t generation, uint32_t dwell_ms) {
    if (dwell_ms) (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(dwell_ms));
    finish_interaction_task_with_surface(generation, true);
}

static bool command_cancel_requested_for(uint32_t generation) {
    bool operation_cancelled = operation_context_cancel_requested(generation);
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_command_cancel_requested &&
                s_cancel_requested_generation == generation &&
                operation_cancelled;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static void remember_cancelled_command_reply(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_active_command_reply_to[0]) {
        bool already_remembered = false;
        for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
            if (!strcmp(s_cancelled_command_reply_to[i], s_active_command_reply_to)) {
                already_remembered = true;
                break;
            }
        }
        if (!already_remembered) {
            strlcpy(s_cancelled_command_reply_to[s_cancelled_command_reply_next],
                    s_active_command_reply_to, COMMAND_REPLY_ID_CAPACITY);
            s_cancelled_command_reply_next =
                (s_cancelled_command_reply_next + 1) % CANCELLED_REPLY_SLOTS;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
}

static bool cancelled_command_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    for (unsigned i = 0; i < CANCELLED_REPLY_SLOTS; ++i) {
        if (s_cancelled_command_reply_to[i][0] &&
            !strcmp(s_cancelled_command_reply_to[i], reply_to)) {
            matches = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}

static bool active_command_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches;
    taskENTER_CRITICAL(&s_task_state_lock);
    matches = s_interaction_task != NULL && s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}
static bool result_speech_reply_matches(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    bool matches = false;
    bool expired = false;
    unsigned expired_parts = 0;
    char expired_reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        s_result_speech_reply_to[0] &&
        s_result_speech_deadline_us > 0 &&
        now_us >= s_result_speech_deadline_us) {
        expired = true;
        expired_parts = s_result_speech_parts_remaining;
        strlcpy(expired_reply_to, s_result_speech_reply_to,
                sizeof(expired_reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    } else {
        matches = s_result_speech_parts_remaining > 0 &&
                  s_result_speech_reply_to[0] &&
                  !strcmp(s_result_speech_reply_to, reply_to);
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (expired) {
        ESP_LOGW(TAG, "result speech expired after idle timeout: replyTo=%s missing=%u next=%s",
                 expired_reply_to, expired_parts, reply_to);
    }
    return matches;
}

static bool command_timing_matches(const char *reply_to) {
    bool matches = false;
    if (!reply_to || !reply_to[0]) return false;
    taskENTER_CRITICAL(&s_task_state_lock);
    matches = s_active_command_reply_to[0] &&
              !strcmp(s_active_command_reply_to, reply_to);
    taskEXIT_CRITICAL(&s_task_state_lock);
    return matches;
}

static void remember_result_speech_reply(const char *reply_to, unsigned parts) {
    if (!reply_to || !reply_to[0] || parts == 0) return;
    int64_t deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_task_state_lock);
    strlcpy(s_result_speech_reply_to, reply_to, sizeof(s_result_speech_reply_to));
    s_result_speech_parts_remaining = parts;
    s_result_speech_deadline_us = deadline_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "result speech armed: replyTo=%s parts=%u idleTimeout=%us",
             reply_to, parts,
             (unsigned)(RESULT_SPEECH_IDLE_TIMEOUT_US / 1000000LL));
}

static void finish_result_speech_part(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned remaining = 0;
    int64_t next_deadline_us = esp_timer_get_time() + RESULT_SPEECH_IDLE_TIMEOUT_US;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        --s_result_speech_parts_remaining;
        remaining = s_result_speech_parts_remaining;
        if (remaining == 0) {
            s_result_speech_reply_to[0] = '\0';
            s_result_speech_deadline_us = 0;
        } else {
            s_result_speech_deadline_us = next_deadline_us;
        }
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "result speech part complete: replyTo=%s remaining=%u",
             reply_to, remaining);
}

static unsigned outgoing_pending_speech_parts(cJSON *item) {
    if (!cJSON_IsObject(item)) return 0;
    cJSON *sources[3] = {
        cJSON_GetObjectItemCaseSensitive(item, "metadata"),
        cJSON_GetObjectItemCaseSensitive(item, "extra"),
        item,
    };
    for (unsigned i = 0; i < 3; ++i) {
        if (!cJSON_IsObject(sources[i])) continue;
        cJSON *value = cJSON_GetObjectItemCaseSensitive(
            sources[i], "speech_parts_pending");
        if (cJSON_IsNumber(value) && value->valuedouble > 0 &&
            value->valuedouble <= 1000) {
            return (unsigned)value->valuedouble;
        }
        if (cJSON_IsString(value) && value->valuestring) {
            char *end = NULL;
            errno = 0;
            unsigned long parsed = strtoul(value->valuestring, &end, 10);
            if (errno == 0 && end != value->valuestring && *end == '\0' &&
                parsed > 0 && parsed <= 1000) {
                return (unsigned)parsed;
            }
        }
    }
    return 0;
}

static const char *outgoing_reply_correlation(cJSON *item) {
    if (!cJSON_IsObject(item)) return NULL;
    const char *value = json_string(item, "replyTo");
    if (!value || !value[0]) value = json_string(item, "replyToMessageId");
    if (!value || !value[0]) value = json_string(item, "source_message_id");
    if (!value || !value[0]) value = json_string(item, "sourceMessageId");
    if (!value || !value[0]) value = json_string(item, "sourceMessageID");
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    if ((!value || !value[0]) && cJSON_IsObject(metadata)) {
        value = json_string(metadata, "replyTo");
        if (!value || !value[0]) value = json_string(metadata, "replyToMessageId");
        if (!value || !value[0]) value = json_string(metadata, "source_message_id");
        if (!value || !value[0]) value = json_string(metadata, "sourceMessageId");
        if (!value || !value[0]) value = json_string(metadata, "sourceMessageID");
    }
    cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
    if ((!value || !value[0]) && cJSON_IsObject(extra)) {
        value = json_string(extra, "replyTo");
        if (!value || !value[0]) value = json_string(extra, "replyToMessageId");
        if (!value || !value[0]) value = json_string(extra, "source_message_id");
        if (!value || !value[0]) value = json_string(extra, "sourceMessageId");
        if (!value || !value[0]) value = json_string(extra, "sourceMessageID");
    }
    return value;
}

static bool outgoing_message_is_progress(cJSON *item) {
    cJSON *progress = cJSON_GetObjectItemCaseSensitive(item, "progress");
    if (cJSON_IsTrue(progress)) return true;
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    if (cJSON_IsObject(metadata)) {
        const char *turn = json_string(metadata, "acp_turn");
        if (turn && (!strcasecmp(turn, "progress") || !strcasecmp(turn, "working"))) return true;
    }
    return false;
}

static bool outgoing_message_is_final(cJSON *item) {
    if (!cJSON_IsObject(item)) return false;
    cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
    const char *turn = cJSON_IsObject(metadata) ? json_string(metadata, "acp_turn") : NULL;
    if (!turn) turn = json_string(item, "acp_turn");
    if (turn && (!strcasecmp(turn, "final") || !strcasecmp(turn, "complete") ||
                 !strcasecmp(turn, "completed"))) return true;
    cJSON *final = cJSON_GetObjectItemCaseSensitive(item, "final");
    if (cJSON_IsTrue(final)) return true;
    cJSON *complete = cJSON_GetObjectItemCaseSensitive(item, "complete");
    if (cJSON_IsTrue(complete)) return true;
    if (cJSON_IsObject(metadata)) {
        final = cJSON_GetObjectItemCaseSensitive(metadata, "final");
        complete = cJSON_GetObjectItemCaseSensitive(metadata, "complete");
        return cJSON_IsTrue(final) || cJSON_IsTrue(complete);
    }
    return false;
}

// The outgoing poll can resume as soon as the POST releases the shared HTTP
// lock. On a very fast reply it may therefore see the result during the few
// scheduler ticks in which interaction_task is still parsing/publishing the
// returned maclawMessageId. Give that correlation hand-off a short bounded
// grace period instead of acknowledging and losing the result as unrelated.
static bool active_command_reply_matches_after_handoff(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return false;
    for (unsigned attempt = 0; attempt < 20; ++attempt) {
        if (active_command_reply_matches(reply_to)) return true;
        bool awaiting_correlation;
        taskENTER_CRITICAL(&s_task_state_lock);
        awaiting_correlation = s_interaction_task != NULL &&
                               s_interaction_phase == INTERACTION_PROCESSING &&
                               !s_command_cancel_requested &&
                               !s_active_command_reply_to[0];
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!awaiting_correlation) break;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    return active_command_reply_matches(reply_to);
}

static TaskHandle_t begin_active_command_reply(void) {
    // Atomically close the cancellation window and take a stable waiter
    // snapshot before drawing. A simultaneous double tap then observes either
    // a cancellable command or a completed one, never a half-transition.
    TaskHandle_t waiter = NULL;
    uint32_t generation = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_command_cancel_requested) {
        generation = s_interaction_generation;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* Win the terminal token before any display side effect. A concurrent
     * cancellation that arrives after this point observes terminal_committed
     * and is discarded instead of painting "cancelled" over a final reply. */
    if (!generation || !operation_context_commit_terminal(generation)) return NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_interaction_generation == generation && s_interaction_task) {
        s_command_cancel_requested = false;
        s_cancel_requested_generation = 0;
        s_command_cancel_enabled = false;
        waiter = s_interaction_task;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    app_ui_set_command_cancel_enabled(false);
    return waiter;
}

// Publish the terminal screen and wake the interaction task as one ordered UI
// transition. The waiter refreshes "远端处理中" every 15 seconds; notifying it
// only after a potentially slow full-frame LCD transfer lets that refresh race
// with the result draw and cover a reply that is already stored underneath.
// Once notified first, the worker exits its refresh loop and cannot repaint the
// processing surface while the poller commits the response page.
static void complete_active_command_text_reply(TaskHandle_t waiter,
                                               const char *title,
                                               const char *text) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal text transition: waiter=%p bytes=%u", waiter,
             (unsigned)(text ? strlen(text) : 0));
    xTaskNotifyGive(waiter);
    // The notification only makes the higher-priority interaction worker
    // runnable; it may not actually leave the timed wait before this poll task
    // reaches the LCD.  Clear the thinking surface synchronously as part of
    // the terminal transition so its mouth animator is unable to repaint over
    // the first result frame even under TLS/HTTP load.
    pet("speaking");
    app_ui_show_response(title, text);
}

static void complete_active_command_image_reply(TaskHandle_t waiter,
                                                const char *title,
                                                const char *caption,
                                                const uint16_t *pixels,
                                                size_t width,
                                                size_t height) {
    if (!waiter) return;
    ESP_LOGI(TAG, "terminal image transition: waiter=%p size=%ux%u", waiter,
             (unsigned)width, (unsigned)height);
    xTaskNotifyGive(waiter);
    pet("speaking");
    app_ui_show_response_image(title, caption, pixels, width, height);
}

static void show_cancelled_command(uint32_t generation) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool cancellation_still_active = s_command_cancel_requested &&
                                     s_cancel_requested_generation == generation &&
                                     s_interaction_generation == generation;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!cancellation_still_active) return;
    remember_cancelled_command_reply();
    app_ui_set_command_cancel_enabled(false);
    bool should_draw = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    // Let CST816 finish reporting the second contact before a SHORT gesture is
    // allowed to start another recording. This guard complements the board
    // driver's raw-event drain and also covers the physical BOOT button.
    s_ignore_command_input_until_us = esp_timer_get_time() + 1200000;
    if (!s_command_cancel_ui_shown) {
        s_command_cancel_ui_shown = true;
        should_draw = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (should_draw) {
        app_ui_show_text("已取消", "本次操作已停止");
        ESP_LOGI(TAG, "voice command cancelled by double tap");
    }
}

static void finish_cancelled_command(uint32_t generation) {
    // The high-priority cancellation worker owns LCD rendering so the touch
    // scanner never blocks on a full display transfer. Wait briefly for that
    // final state before releasing the interaction token; this also prevents a
    // delayed cancellation frame from overwriting the next command screen.
    if (s_command_cancel_ui_ready) {
        TickType_t started = xTaskGetTickCount();
        bool worker_finished = false;
        while ((xTaskGetTickCount() - started) <
               pdMS_TO_TICKS(COMMAND_CANCEL_WORKER_TIMEOUT_MS)) {
            if (xSemaphoreTake(s_command_cancel_ui_ready, pdMS_TO_TICKS(50)) == pdTRUE) {
                taskENTER_CRITICAL(&s_task_state_lock);
                bool ready_for_this_command = s_cancel_ui_ready_generation == generation;
                taskEXIT_CRITICAL(&s_task_state_lock);
                if (ready_for_this_command) {
                    worker_finished = true;
                    break;
                }
            }
        }
        if (!worker_finished) {
            ESP_LOGW(TAG, "command cancellation worker timed out: generation=%lu",
                     (unsigned long)generation);
        }
    }
    if (command_cancel_requested_for(generation)) show_cancelled_command(generation);
    // Keep the acknowledgement long enough to be perceived, then perform one
    // explicit cancel -> idle transition. The gesture input guard remains in
    // force, so the second contact cannot immediately start another command.
    vTaskDelay(pdMS_TO_TICKS(COMMAND_CANCEL_ACKNOWLEDGEMENT_MS));
    finish_interaction_task_with_surface(generation, true);
}

static void command_cancel_worker(void *arg) {
    (void)arg;
    while (true) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);

        taskENTER_CRITICAL(&s_task_state_lock);
        bool stop_requested = s_command_cancel_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested) break;

        uint32_t cancel_generation = 0;
        taskENTER_CRITICAL(&s_task_state_lock);
        cancel_generation = s_cancel_requested_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!cancel_generation) continue;

        bool cancellation_still_active;
        taskENTER_CRITICAL(&s_task_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    s_interaction_generation == cancel_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!cancellation_still_active) continue;

        show_cancelled_command(cancel_generation);

        // Hold the pointer guard for the entire cancel call. The request task
        // must acquire the same guard before clearing/cleaning the handle, so
        // this can never race esp_http_client_cleanup() or dereference a stale
        // client pointer.
        if (s_foreground_http_client_mutex &&
            xSemaphoreTake(s_foreground_http_client_mutex, pdMS_TO_TICKS(1000)) == pdTRUE) {
            esp_http_client_handle_t http_client = s_foreground_http_client;
            if (http_client) {
                esp_err_t cancel_err = esp_http_client_cancel_request(http_client);
                if (cancel_err != ESP_OK) {
                    ESP_LOGW(TAG, "foreground HTTP cancel failed: %s",
                             esp_err_to_name(cancel_err));
                }
            }
            xSemaphoreGive(s_foreground_http_client_mutex);
        } else {
            ESP_LOGW(TAG, "foreground HTTP cancel skipped: client guard timeout");
        }
        if (device_connectivity_is_active_cellular() &&
            device_connectivity_cancel_cellular_foreground_request()) {
            ESP_LOGI(TAG, "foreground ML307 HTTP request cancelled");
        }

        // Local cancellation stops waiting immediately, but the server-side
        // agent may already be executing after accepting the voice event. Send
        // the protocol's normal /cancel command before releasing the local
        // interaction token so it cannot accidentally target a newer command.
        taskENTER_CRITICAL(&s_task_state_lock);
        cancellation_still_active = s_command_cancel_requested &&
                                    s_cancel_requested_generation == cancel_generation &&
                                    s_interaction_generation == cancel_generation;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_gateway_token[0] && cancellation_still_active) {
            char cancelled_reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
            taskENTER_CRITICAL(&s_task_state_lock);
            strlcpy(cancelled_reply_to, s_active_command_reply_to,
                    sizeof(cancelled_reply_to));
            taskEXIT_CRITICAL(&s_task_state_lock);
            esp_err_t server_cancel_err = send_text_event(
                "/cancel", cancelled_reply_to[0] ? cancelled_reply_to : NULL);
            if (server_cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "server command cancel failed: %s",
                         esp_err_to_name(server_cancel_err));
            } else {
                ESP_LOGI(TAG, "server command cancel accepted");
            }
        }

        taskENTER_CRITICAL(&s_task_state_lock);
        s_cancel_ui_ready_generation = cancel_generation;
        TaskHandle_t waiter = NULL;
        if (s_command_cancel_requested &&
            s_cancel_requested_generation == cancel_generation) {
            waiter = s_interaction_task;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_command_cancel_ui_ready) xSemaphoreGive(s_command_cancel_ui_ready);
        if (waiter) xTaskNotifyGive(waiter);
        // High-water mark after a full cancel cycle (CJK frame + transport
        // cancel + /cancel POST) so real-device runs can confirm the stack
        // margin on both the Wi-Fi/TLS and the ML307 paths.
        ESP_LOGI(TAG, "command cancel handled: generation=%lu stack_hwm=%u",
                 (unsigned long)cancel_generation,
                 (unsigned)uxTaskGetStackHighWaterMark(NULL));
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_command_cancel_task == self) s_command_cancel_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_command_cancel_stopped) xSemaphoreGive(s_command_cancel_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_INTERACTION,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
}

/* The cancellation coordinator owns display/network cancellation side effects,
 * but no driver lifetime.  Stop only at its notification wait boundary and
 * keep its completion semaphore alive on timeout: a still-running worker may
 * still reference the UI and foreground HTTP guards. */
static esp_err_t stop_command_cancel_worker(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_command_cancel_stop_requested = true;
    task = s_command_cancel_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_command_cancel_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_command_cancel_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_INTERACTION, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "command cancel worker stopped");
    return ESP_OK;
}

static esp_err_t stop_command_cancel_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_command_cancel_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_command_cancel_worker(timeout_ms);
}

static bool request_command_cancel(void) {
    TaskHandle_t waiter = NULL;
    uint32_t generation = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    // Cancellation belongs strictly to the thinking phase. Once the poller has
    // accepted a result it clears this flag before drawing the answer, so a
    // late double tap cannot replace a completed command with “已取消”.
    if (s_interaction_task && s_command_cancel_enabled &&
        !s_command_cancel_requested) {
        s_command_cancel_requested = true;
        s_cancel_requested_generation = s_interaction_generation;
        generation = s_interaction_generation;
        s_command_cancel_enabled = false;
        waiter = s_interaction_task;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!waiter) return false;
    if (!operation_context_request_cancel(generation)) {
        /* A terminal reply won while the gesture was being classified.  Do
         * not let the stale gesture revive a cancellation side effect. */
        taskENTER_CRITICAL(&s_task_state_lock);
        if (s_cancel_requested_generation == generation) {
            s_command_cancel_requested = false;
            s_cancel_requested_generation = 0;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    app_ui_set_command_cancel_enabled(false);
    // Keep the touch task responsive: a dedicated internal-RAM worker renders
    // the final frame and interrupts any in-flight HTTP operation safely.
    if (s_command_cancel_task) {
        xTaskNotifyGive(s_command_cancel_task);
    } else {
        // Startup treats creation failure as fatal, but retain a cooperative
        // fallback so a partially initialized device cannot wait for 90 s.
        xTaskNotifyGive(waiter);
    }
    ESP_LOGI(TAG, "voice command cancel requested by double tap");
    return true;
}

// A foreground command owns the LCD from the end of capture until a final
// answer or explicit error is displayed. Background updates may refresh data,
// but must not replace that flow with the ambient/weather screen.
static bool command_display_active(void) {
    bool active;
    taskENTER_CRITICAL(&s_task_state_lock);
    active = s_interaction_task != NULL || s_command_display_locked;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return active;
}

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
    app_ui_set_pet_state(state);
}

static esp_err_t on_http_event(esp_http_client_event_t *event) {
    http_response_t *out = event->user_data;
    if (event->event_id == HTTP_EVENT_ON_DATA && out && out->data && event->data_len > 0) {
        if (out->capacity == 0 || out->len >= out->capacity - 1) {
            out->truncated = true;
            return ESP_OK;
        }
        size_t available = out->capacity - out->len - 1;
        size_t copy_len = event->data_len < available ? event->data_len : available;
        memcpy(out->data + out->len, event->data, copy_len);
        out->len += copy_len;
        out->data[out->len] = '\0';
        if (copy_len < (size_t)event->data_len) out->truncated = true;
    }
    return ESP_OK;
}

// Match the URL's textual scheme + authority (including an explicit port).
// Equivalent default-port spellings may miss reuse, which is preferable to
// ever pooling an untrusted absolute media URL or leaking gateway credentials.
static bool url_has_same_origin(const char *left, const char *right) {
    if (!left || !right) return false;
    const char *left_scheme = strstr(left, "://");
    const char *right_scheme = strstr(right, "://");
    if (!left_scheme || !right_scheme) return false;
    const char *left_end = strpbrk(left_scheme + 3, "/?#");
    const char *right_end = strpbrk(right_scheme + 3, "/?#");
    size_t left_len = left_end ? (size_t)(left_end - left) : strlen(left);
    size_t right_len = right_end ? (size_t)(right_end - right) : strlen(right);
    return left_len == right_len && strncasecmp(left, right, left_len) == 0;
}

static esp_err_t request_with_capacity(const char *method, const char *path, const char *content_type,
                                       const char *body, int body_len, size_t response_capacity,
                                       http_response_t *out) {
    if (!out) return ESP_ERR_INVALID_ARG;
    memset(out, 0, sizeof(*out));
    if (!method || !path || response_capacity < 2) return ESP_ERR_INVALID_ARG;
    char url[URL_CAPACITY];
    int n = strncmp(path, "http://", 7) == 0 || strncmp(path, "https://", 8) == 0
                ? snprintf(url, sizeof(url), "%s", path)
                : snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    bool foreground_request = false;
    bool meeting_request = false;
    bool meeting_capability_refresh_request = false;
    bool gateway_startup_request = false;
    uint32_t foreground_generation = 0;
    TaskHandle_t current_task = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    foreground_request = current_task == s_interaction_task;
    meeting_request = current_task == s_meeting_task;
    meeting_capability_refresh_request =
        current_task == s_meeting_capability_refresh_task;
    gateway_startup_request = current_task == s_gateway_task;
    if (foreground_request) foreground_generation = s_interaction_generation;
    taskEXIT_CRITICAL(&s_task_state_lock);

    bool poll_request = current_task == s_gateway_poll_task;
    bool asset_request = current_task == s_startup_pet_asset_task;
    SemaphoreHandle_t request_mutex = asset_request
                                            ? s_gateway_asset_http_mutex
                                            : poll_request ? s_gateway_poll_http_mutex : s_http_mutex;
    if (!request_mutex) return ESP_ERR_INVALID_STATE;
    int64_t request_started_us = esp_timer_get_time();
    TickType_t lock_started = xTaskGetTickCount();
    bool cancellation_request = current_task == s_command_cancel_task;
    const TickType_t lock_timeout = pdMS_TO_TICKS(cancellation_request ? 6000 : 35000);
    while (xSemaphoreTake(request_mutex, pdMS_TO_TICKS(100)) != pdTRUE) {
        if (foreground_request && command_cancel_requested_for(foreground_generation)) {
            ESP_LOGI(TAG, "foreground HTTP lock wait cancelled: %s %s", method, path);
            return ESP_ERR_INVALID_STATE;
        }
        if ((xTaskGetTickCount() - lock_started) >= lock_timeout) {
            ESP_LOGW(TAG, "HTTP request lock timeout: %s %s", method, path);
            return ESP_ERR_TIMEOUT;
        }
    }
    uint32_t lock_wait_ms = (uint32_t)((xTaskGetTickCount() - lock_started) * portTICK_PERIOD_MS);
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        xSemaphoreGive(request_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    // Prefer PSRAM for every HTTP body. Request buffers must not consume the
    // small internal heap reserved for the TLS handshake and Wi-Fi stacks.
    out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!out->data) out->data = heap_caps_malloc(response_capacity, MALLOC_CAP_8BIT);
    if (!out->data) {
        ESP_LOGE(TAG, "HTTP buffer allocation failed: need=%u path=%s", (unsigned)response_capacity, path);
        log_heap_snapshot("http-buffer-fail");
        xSemaphoreGive(request_mutex);
        return ESP_ERR_NO_MEM;
    }
    out->capacity = response_capacity;
    out->data[0] = '\0';
    bool absolute_url = !strncmp(path, "http://", 7) || !strncmp(path, "https://", 8);
    bool reusable_gateway_request = !absolute_url || url_has_same_origin(s_gateway_url, url);
    bool bearer_request = !absolute_url;
    if (device_connectivity_is_active_cellular()) {
        char authorization[128] = {0};
        uint32_t cellular_response_len = 0;
        if (s_gateway_token[0] && bearer_request) {
            snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        }
        device_connectivity_http_request_t cellular_request = {
            .method = method, .url = url, .content_type = content_type,
            .authorization = authorization, .body = body,
            .body_len = body_len > 0 ? (uint32_t)body_len : 0,
            .response = out->data, .response_capacity = (uint32_t)out->capacity,
            .response_len = &cellular_response_len, .status_code = &out->status,
            .truncated = &out->truncated,
            .timeout_ms = cancellation_request ? 5000
                         : (foreground_request && body_len > 32768 ? 90000 : 30000),
            .cancellation_owner = foreground_request ? (const void *)current_task
                                : meeting_request ? (const void *)current_task : NULL,
            .foreground = foreground_request,
        };
        esp_err_t cellular_err = device_status_to_platform_error(
            device_connectivity_cellular_http_request(&cellular_request));
        out->len = cellular_response_len;
        xSemaphoreGive(request_mutex);
        ESP_LOGI(TAG, "ML307 HTTP %s %s status=%d err=%s response=%u%s",
                 method, absolute_url ? "<absolute URL>" : path, out->status,
                 esp_err_to_name(cellular_err), (unsigned)out->len,
                 out->truncated ? " truncated" : "");
        return cellular_err;
    }
    esp_http_client_handle_t *pool_client = asset_request
                                                ? &s_gateway_asset_http_client
                                                : poll_request ? &s_gateway_poll_http_client
                                                               : &s_gateway_http_client;
    char *pool_origin = asset_request
                            ? s_gateway_asset_http_origin
                            : poll_request ? s_gateway_poll_http_origin
                                           : s_gateway_http_origin;
    esp_http_client_handle_t client = NULL;
    bool owns_client = false;
    bool pooled_client = false;
    if (reusable_gateway_request) {
        if (*pool_client && strcmp(pool_origin, s_gateway_url)) {
            esp_http_client_cleanup(*pool_client);
            *pool_client = NULL;
            pool_origin[0] = '\0';
        }
        client = *pool_client;
        pooled_client = client != NULL;
    }
    if (!client) {
        esp_http_client_config_t config = {
            .url = url, .event_handler = on_http_event, .user_data = out,
            .timeout_ms = cancellation_request ? 5000 : 30000,
            .crt_bundle_attach = esp_crt_bundle_attach,
            .keep_alive_enable = true,
        };
        client = esp_http_client_init(&config);
        owns_client = true;
        if (client && reusable_gateway_request) {
            *pool_client = client;
            strlcpy(pool_origin, s_gateway_url, URL_CAPACITY);
            owns_client = false;
        }
    } else {
        esp_err_t setup_err = esp_http_client_set_url(client, url);
        if (setup_err == ESP_OK) setup_err = esp_http_client_set_user_data(client, out);
        if (setup_err == ESP_OK) setup_err = esp_http_client_set_timeout_ms(client, cancellation_request ? 5000 : 30000);
        if (setup_err != ESP_OK) {
            ESP_LOGW(TAG, "pooled HTTP client setup failed: %s", esp_err_to_name(setup_err));
            esp_http_client_cleanup(client);
            if (*pool_client == client) {
                *pool_client = NULL;
                pool_origin[0] = '\0';
            }
            free(out->data);
            out->data = NULL;
            xSemaphoreGive(request_mutex);
            return setup_err;
        }
    }
    if (!client) {
        ESP_LOGE(TAG, "HTTP client allocation failed: path=%s", path);
        log_heap_snapshot("http-client-fail");
        free(out->data);
        out->data = NULL;
        xSemaphoreGive(request_mutex);
        return ESP_ERR_NO_MEM;
    }
    if (gateway_startup_request) {
        if (!s_gateway_startup_client_mutex) {
            if (owns_client) esp_http_client_cleanup(client);
            xSemaphoreGive(request_mutex);
            free(out->data);
            out->data = NULL;
            return ESP_ERR_INVALID_STATE;
        }
        xSemaphoreTake(s_gateway_startup_client_mutex, portMAX_DELAY);
        s_gateway_startup_active_client = client;
        xSemaphoreGive(s_gateway_startup_client_mutex);
    }
    if (meeting_capability_refresh_request) {
        if (!s_meeting_capability_refresh_client_mutex) {
            if (owns_client) esp_http_client_cleanup(client);
            xSemaphoreGive(request_mutex);
            free(out->data);
            out->data = NULL;
            return ESP_ERR_INVALID_STATE;
        }
        xSemaphoreTake(s_meeting_capability_refresh_client_mutex, portMAX_DELAY);
        s_meeting_capability_refresh_active_client = client;
        xSemaphoreGive(s_meeting_capability_refresh_client_mutex);
    }
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        s_foreground_http_client = client;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    if (poll_request && s_gateway_poll_client_mutex) {
        xSemaphoreTake(s_gateway_poll_client_mutex, portMAX_DELAY);
        s_gateway_poll_active_client = client;
        xSemaphoreGive(s_gateway_poll_client_mutex);
    }
    if (asset_request && s_gateway_asset_client_mutex) {
        xSemaphoreTake(s_gateway_asset_client_mutex, portMAX_DELAY);
        s_gateway_asset_active_client = client;
        xSemaphoreGive(s_gateway_asset_client_mutex);
    }
    esp_http_client_method_t http_method = HTTP_METHOD_GET;
    if (!strcmp(method, "POST")) http_method = HTTP_METHOD_POST;
    else if (!strcmp(method, "PUT")) http_method = HTTP_METHOD_PUT;
    esp_http_client_set_method(client, http_method);
    if (content_type) esp_http_client_set_header(client, "Content-Type", content_type);
    else esp_http_client_delete_header(client, "Content-Type");
    esp_http_client_set_header(client, "Accept", "application/json");
    if (s_gateway_token[0] && bearer_request) {
        char authorization[128];
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        esp_http_client_set_header(client, "Authorization", authorization);
    } else {
        // A reused gateway handle may still carry the previous bearer. Never
        // let it leak to an absolute media URL or survive after token removal.
        esp_http_client_delete_header(client, "Authorization");
    }
    if (body && body_len > 0) {
        esp_http_client_set_post_field(client, body, body_len);
    } else {
        // Clear a previous POST payload before reusing the handle for GET/ACK.
        esp_http_client_set_post_field(client, NULL, 0);
    }
    int64_t perform_started_us = esp_timer_get_time();
    esp_err_t err;
    if (foreground_request && command_cancel_requested_for(foreground_generation)) {
        // Cancellation skips the perform entirely. Do not report the pooled
        // handle's previous status (usually a stale 200) for a request that
        // never ran.
        err = ESP_ERR_INVALID_STATE;
        out->status = 0;
    } else {
        err = esp_http_client_perform(client);
        out->status = esp_http_client_get_status_code(client);
    }
    uint32_t perform_ms = (uint32_t)((esp_timer_get_time() - perform_started_us) / 1000);
    if (gateway_startup_request) {
        xSemaphoreTake(s_gateway_startup_client_mutex, portMAX_DELAY);
        if (s_gateway_startup_active_client == client) {
            s_gateway_startup_active_client = NULL;
        }
        xSemaphoreGive(s_gateway_startup_client_mutex);
    }
    if (meeting_capability_refresh_request) {
        xSemaphoreTake(s_meeting_capability_refresh_client_mutex, portMAX_DELAY);
        if (s_meeting_capability_refresh_active_client == client) {
            s_meeting_capability_refresh_active_client = NULL;
        }
        xSemaphoreGive(s_meeting_capability_refresh_client_mutex);
    }
    if (foreground_request) {
        xSemaphoreTake(s_foreground_http_client_mutex, portMAX_DELAY);
        if (s_foreground_http_client == client) s_foreground_http_client = NULL;
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    if (poll_request && s_gateway_poll_client_mutex) {
        xSemaphoreTake(s_gateway_poll_client_mutex, portMAX_DELAY);
        if (s_gateway_poll_active_client == client) s_gateway_poll_active_client = NULL;
        xSemaphoreGive(s_gateway_poll_client_mutex);
    }
    if (asset_request && s_gateway_asset_client_mutex) {
        xSemaphoreTake(s_gateway_asset_client_mutex, portMAX_DELAY);
        if (s_gateway_asset_active_client == client) s_gateway_asset_active_client = NULL;
        xSemaphoreGive(s_gateway_asset_client_mutex);
    }
    // The body and callback point at caller-owned memory. Clear both before a
    // pooled handle can outlive this stack frame.
    esp_http_client_set_post_field(client, NULL, 0);
    esp_http_client_set_user_data(client, NULL);
    if (owns_client) {
        esp_http_client_cleanup(client);
    } else if (err != ESP_OK) {
        // A failed perform can leave the reusable handle's transport/parser in
        // an indeterminate state. Discard it so the retry starts with a clean
        // TLS connection instead of repeatedly failing on the poisoned one.
        esp_http_client_cleanup(client);
        if (*pool_client == client) {
            *pool_client = NULL;
            pool_origin[0] = '\0';
        }
    }
    xSemaphoreGive(request_mutex);
    char target[96];
    if (!strncmp(path, "http://", 7) || !strncmp(path, "https://", 8)) {
        strlcpy(target, "<absolute media URL>", sizeof(target));
    } else {
        strlcpy(target, path, sizeof(target));
        char *query = strchr(target, '?');
        if (query) *query = '\0';
    }
    ESP_LOGI(TAG, "HTTP %s %s status=%d err=%s lane=%s client=%s lock=%ums perform=%ums total=%ums response=%u%s",
             method, target, out->status, esp_err_to_name(err),
             asset_request ? "asset" : poll_request ? "poll" : "foreground",
             pooled_client ? "pooled" : "dedicated", (unsigned)lock_wait_ms,
             (unsigned)perform_ms, (unsigned)((esp_timer_get_time() - request_started_us) / 1000),
             (unsigned)out->len, out->truncated ? " truncated" : "");
    if (err != ESP_OK) {
        log_heap_snapshot("http-perform-fail");
    }
    if (out->truncated) {
        ESP_LOGE(TAG, "HTTP response truncated: capacity=%u path=%s", (unsigned)response_capacity, path);
        return ESP_ERR_INVALID_SIZE;
    }
    return err;
}

static esp_err_t request(const char *method, const char *path, const char *content_type,
                         const char *body, int body_len, http_response_t *out) {
    return request_with_capacity(method, path, content_type, body, body_len, RESPONSE_CAPACITY, out);
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

typedef struct {
    char encoding[16];
    char revision[40];
    int width;
    int height;
    int frame_ms;
    int frame_count;
    char urls[PET_ASSET_MAX_FRAMES][URL_CAPACITY];
    char sha256[PET_ASSET_MAX_FRAMES][65];
} pet_asset_ref_t;

// Pet artwork is optional startup decoration. Keep its small descriptor here
// so the authenticated handshake can release TLS/JSON memory and initialize
// ESP-SR before any media download or SPIFFS write takes place.
// Written by the gateway poll task to pre-empt the optional cold-start pack,
// then observed by the startup worker between frame downloads/installs.
static volatile bool s_startup_pet_asset_pending;
static bool s_startup_pet_asset_present;
static pet_asset_ref_t *s_startup_pet_asset_ref;
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

static bool pet_asset_url_allowed(const char *url) {
    return hardware_audio_url_allowed(url);
}

static bool parse_pet_asset_ref(cJSON *object, pet_asset_ref_t *out) {
    if (!cJSON_IsObject(object) || !out) return false;
    memset(out, 0, sizeof(*out));
    const char *encoding = json_string(object, "encoding");
    const char *revision = json_string(object, "revision");
    cJSON *urls = cJSON_GetObjectItemCaseSensitive(object, "urls");
    cJSON *hashes = cJSON_GetObjectItemCaseSensitive(object, "sha256");
    if (!encoding || strcmp(encoding, "rgb565a8") || !revision || !revision[0] ||
        strlen(revision) >= sizeof(out->revision) ||
        !json_number(object, "width", &out->width) ||
        !json_number(object, "height", &out->height) ||
        out->width < 32 || out->width > PET_ASSET_MAX_DIMENSION ||
        out->height < 32 || out->height > PET_ASSET_MAX_DIMENSION ||
        !cJSON_IsArray(urls) || !cJSON_IsArray(hashes)) return false;
    strlcpy(out->encoding, encoding, sizeof(out->encoding));
    strlcpy(out->revision, revision, sizeof(out->revision));
    if (!json_number(object, "frameMs", &out->frame_ms) || out->frame_ms < 50 || out->frame_ms > 10000) {
        out->frame_ms = PET_ASSET_DEFAULT_FRAME_MS;
    }
    int count = cJSON_GetArraySize(urls);
    if (count < 1 || count > PET_ASSET_MAX_FRAMES || cJSON_GetArraySize(hashes) != count) return false;
    for (int i = 0; i < count; ++i) {
        cJSON *entry = cJSON_GetArrayItem(urls, i);
        cJSON *hash = cJSON_GetArrayItem(hashes, i);
        if (!cJSON_IsString(entry) || !entry->valuestring || !pet_asset_url_allowed(entry->valuestring) ||
            strlen(entry->valuestring) >= sizeof(out->urls[i]) || !cJSON_IsString(hash) ||
            !hash->valuestring || strlen(hash->valuestring) != 64) return false;
        for (size_t j = 0; j < 64; ++j) {
            char ch = hash->valuestring[j];
            if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F'))) return false;
        }
        strlcpy(out->urls[i], entry->valuestring, sizeof(out->urls[i]));
        strlcpy(out->sha256[i], hash->valuestring, sizeof(out->sha256[i]));
    }
    out->frame_count = count;
    return true;
}

static void free_pet_asset_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES], size_t frame_count) {
    for (size_t i = 0; i < frame_count && i < PET_ASSET_MAX_FRAMES; ++i) {
        heap_caps_free(frames[i]);
    }
}

/* A full remote pack keeps every verified source frame until the renderer has
 * atomically accepted the pack. Reserve the aggregate peak up front, but
 * compare only the real one-shot allocation against heap largest: fragmented
 * PSRAM can safely hold a pack when no call asks it for one giant block. */
static bool pet_asset_capacity_available(const pet_asset_ref_t *ref) {
    if (!ref || ref->frame_count < 1) return false;
    const size_t frame_bytes = (size_t)ref->width * (size_t)ref->height *
                               PET_ASSET_BYTES_PER_PIXEL;
    const size_t source_external = frame_bytes * (size_t)ref->frame_count;
    device_display_pet_asset_install_budget_t install = {0};
    /* A display profile may decline decorative flash mutations when its panel
     * DMA and PSRAM share cache fabric. Do not reserve space for a cache that
     * HAL will never create; the verified in-memory pack remains usable. */
    const size_t cache_storage = device_storage_allows_optional_flash_work()
                                     ? frame_bytes + 4096u : 0u;
    if (frame_bytes == 0 || source_external > UINT32_MAX || cache_storage > UINT32_MAX ||
        !device_display_get_pet_asset_install_budget(
            (uint32_t)ref->width, (uint32_t)ref->height, (uint32_t)ref->frame_count,
            &install) ||
        install.struct_size != sizeof(install) ||
        install.abi_version != DEVICE_DISPLAY_PET_ASSET_INSTALL_BUDGET_ABI_VERSION ||
        install.total_external_bytes > UINT32_MAX - source_external) {
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
    if (install.total_external_bytes > UINT32_MAX - source_external) return false;
    const uint32_t total_peak_external =
        (uint32_t)source_external + install.total_external_bytes;
    uint32_t max_allocation_external = (uint32_t)frame_bytes;
    if (install.max_external_allocation_bytes > max_allocation_external) {
        max_allocation_external = install.max_external_allocation_bytes;
    }
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
        snapshot.external_free_bytes < total_peak_external ||
        snapshot.external_free_bytes - total_peak_external < 512u * 1024u ||
        snapshot.external_largest_free_bytes < max_allocation_external ||
        (cache_storage &&
         (!snapshot.storage_available ||
          snapshot.storage_free_bytes < (uint32_t)cache_storage ||
          snapshot.storage_free_bytes - (uint32_t)cache_storage < 1024u * 1024u))) {
        ESP_LOGW(TAG, "pet asset deferred: insufficient shared optional capacity "
                      "(source_psram=%u install_psram=%u max_alloc=%u storage=%u) "
                      "snapshot: level=%d ext_free=%lu ext_largest=%lu storage_free=%lu",
                 (unsigned)source_external, (unsigned)install.total_external_bytes,
                 (unsigned)max_allocation_external,
                 (unsigned)cache_storage, (int)snapshot.level,
                 (unsigned long)snapshot.external_free_bytes,
                 (unsigned long)snapshot.external_largest_free_bytes,
                 (unsigned long)snapshot.storage_free_bytes);
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
    *out = *source;
    if ((uint32_t)out->frame_count > install.max_frame_count) {
        ESP_LOGI(TAG, "pet asset keyframes adapted by display HAL: %d -> %lu",
                 out->frame_count, (unsigned long)install.max_frame_count);
        out->frame_count = (int)install.max_frame_count;
    }
    return out->frame_count > 0;
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
    if (s_startup_pet_asset_pending && !s_startup_pet_asset_stop_requested) {
        s_startup_pet_asset_pending = false;
        // Deferral, not abandonment: the audio lease finish re-arms the install.
        s_startup_pet_asset_preempted_by_audio = true;
        preempted = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (preempted) ESP_LOGI(TAG, "startup pet asset preempted by server audio");
}

static bool startup_pet_asset_stop_requested(void) {
    bool stop_requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_startup_pet_asset_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return stop_requested;
}

static esp_err_t stop_startup_pet_retry_timer(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    const uint32_t lock_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (lock_timeout_ms == 0 || !s_startup_pet_retry_timer_mutex ||
        xSemaphoreTake(s_startup_pet_retry_timer_mutex,
                       pdMS_TO_TICKS(lock_timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    esp_timer_handle_t timer = NULL;
    bool already_drained = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_pet_retry_callback_admission_open = false;
    s_startup_pet_retry_due = false;
    timer = s_startup_pet_retry_timer;
    already_drained = s_startup_pet_retry_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_task_state_lock);

    if (timer) {
        esp_err_t stop_err = esp_timer_stop(timer);
        if (stop_err != ESP_OK && stop_err != ESP_ERR_INVALID_STATE) {
            xSemaphoreGive(s_startup_pet_retry_timer_mutex);
            return stop_err;
        }
    }
    if (!already_drained) {
        const uint32_t drain_timeout_ms =
            startup_rollback_remaining_timeout_ms(deadline_us);
        if (drain_timeout_ms == 0 || !s_startup_pet_retry_callback_drained ||
            xSemaphoreTake(s_startup_pet_retry_callback_drained,
                           pdMS_TO_TICKS(drain_timeout_ms)) != pdTRUE) {
            xSemaphoreGive(s_startup_pet_retry_timer_mutex);
            return ESP_ERR_TIMEOUT;
        }
    }
    /* Retain the stopped esp_timer object for this boot generation. Deleting it
     * would race an ordinary retry-arm caller between reading the static handle
     * and calling esp_timer_start_once(); this optional one-shot timer has no
     * independently restartable owner after rollback. Admission remains closed
     * and its callback is drained, so retaining the small SDK object is safer
     * than presenting a freed handle to a late optional caller. */
    xSemaphoreGive(s_startup_pet_retry_timer_mutex);
    return ESP_OK;
}

static esp_err_t stop_startup_pet_asset_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_startup_pet_asset_task;
    s_startup_pet_asset_stop_requested = true;
    // Suppress both a pending retry callback and a re-arm after a foreground
    // media transaction. The artwork is optional during lifecycle rollback.
    s_startup_pet_asset_pending = false;
    s_startup_pet_asset_preempted_by_audio = false;
    taskEXIT_CRITICAL(&s_task_state_lock);

    const uint32_t retry_timer_timeout_ms =
        startup_rollback_remaining_timeout_ms(deadline_us);
    if (retry_timer_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t retry_timer_err = stop_startup_pet_retry_timer(retry_timer_timeout_ms);
    if (retry_timer_err != ESP_OK) {
        ESP_LOGW(TAG, "startup pet retry timer did not stop: %s",
                 esp_err_to_name(retry_timer_err));
        return retry_timer_err;
    }
    if (!task) {
        // A task may have naturally completed just before rollback took the
        // state lock. Its optional pooled TLS client still belongs to this
        // domain and must not survive into a degraded startup state.
        discard_gateway_asset_http_client();
        return ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* Wi-Fi asset downloads own a separate persistent client. Cancel only the
     * active request while it is published; the worker clears publication
     * before cleanup, so lifecycle code never races a freed handle. Cellular
     * work remains bounded by the adapter request timeout. */
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_gateway_asset_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_gateway_asset_client_mutex, pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_gateway_asset_active_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "startup pet HTTP cancel returned: %s",
                         esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_gateway_asset_client_mutex);
    }
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_startup_pet_asset_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_startup_pet_asset_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_startup_pet_asset_stopped);
    s_startup_pet_asset_stopped = NULL;
    discard_gateway_asset_http_client();
    ESP_LOGI(TAG, "startup pet asset task stopped");
    return ESP_OK;
}

static void discard_gateway_asset_http_client(void) {
    if (!s_gateway_asset_http_mutex) return;
    if (xSemaphoreTake(s_gateway_asset_http_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        ESP_LOGW(TAG, "optional pet HTTP client cleanup deferred: lane busy");
        return;
    }
    if (s_gateway_asset_http_client) {
        esp_http_client_cleanup(s_gateway_asset_http_client);
        s_gateway_asset_http_client = NULL;
        s_gateway_asset_http_origin[0] = '\0';
        ESP_LOGI(TAG, "optional pet HTTP client released before wake restore");
    }
    xSemaphoreGive(s_gateway_asset_http_mutex);
}

static esp_err_t install_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                               uint8_t *const frames[PET_ASSET_MAX_FRAMES]);

static esp_err_t download_pet_asset_frames(const pet_asset_ref_t *ref,
                                           uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                           bool startup_transaction) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    if (startup_transaction && startup_pet_asset_stop_requested()) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!pet_asset_capacity_available(ref)) return ESP_ERR_NO_MEM;
    size_t expected = (size_t)ref->width * (size_t)ref->height * PET_ASSET_BYTES_PER_PIXEL;
    if (expected == 0 || expected > PET_ASSET_MAX_BYTES) return ESP_ERR_INVALID_SIZE;
    memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
    for (int i = 0; i < ref->frame_count; ++i) {
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
               (s_foreground_http_requested || audio_media_download_active() ||
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
            err = request_with_capacity("GET", ref->urls[i], NULL, NULL, 0,
                                        expected + 1, &response);
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
        char actual[65];
        if (status != PSA_SUCCESS || digest_len != sizeof(digest)) {
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_FAIL;
        }
        digest_hex(digest, actual);
        if (strcasecmp(actual, ref->sha256[i])) {
            ESP_LOGW(TAG, "pet asset SHA-256 mismatch: frame=%d", i);
            free_pet_asset_frames(frames, (size_t)i + 1);
            memset(frames, 0, sizeof(uint8_t *) * PET_ASSET_MAX_FRAMES);
            return ESP_ERR_INVALID_CRC;
        }
        // Make standby useful as soon as the first verified frame is available.
        // The remaining frames can be retried without leaving EchoEar's round
        // idle screen blank when a later TLS transfer is interrupted.
        if (startup_transaction && i == 0) {
            esp_err_t preview_err = install_pet_asset_first_frame(ref, frames);
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

static bool write_all(FILE *file, const void *data, size_t size,
                      const char *path, int frame_index) {
    if (!file || !data) {
        ESP_LOGE(TAG, "pet cache frame %d open failed: path=%s errno=%d (%s)",
                 frame_index, path ? path : "<null>", errno, strerror(errno));
        return false;
    }
    // SPIFFS garbage collection is bounded by CONFIG_SPIFFS_GC_MAX_RUNS.
    // A single 192 KiB write can therefore report ENOSPC on a mostly empty but
    // fragmented partition. Page-sized writes let GC make bounded progress.
    const uint8_t *cursor = (const uint8_t *)data;
    size_t written = 0;
    while (written < size) {
        if (pet_cache_stop_requested()) {
            ESP_LOGI(TAG, "pet cache frame %d write cancelled: path=%s bytes=%u/%u",
                     frame_index, path, (unsigned)written, (unsigned)size);
            return false;
        }
        size_t chunk = size - written;
        if (chunk > 4096) chunk = 4096;
        size_t count = fwrite(cursor + written, 1, chunk, file);
        if (count != chunk) {
            ESP_LOGE(TAG, "pet cache frame %d write failed: path=%s bytes=%u/%u errno=%d (%s)",
                     frame_index, path, (unsigned)(written + count), (unsigned)size,
                     errno, strerror(errno));
            return false;
        }
        written += count;
        // Let the pinned core's idle task run after every flash page-sized
        // write. The cache is decorative; it must never compete with the
        // task watchdog or the foreground audio lane.
        vTaskDelay(1);
    }
    if (fflush(file) != 0) {
        ESP_LOGE(TAG, "pet cache frame %d flush failed: path=%s errno=%d (%s)",
                 frame_index, path, errno, strerror(errno));
        return false;
    }
    return true;
}

static bool replace_cached_file(const char *temp_path, const char *final_path,
                                const char *kind, int frame_index) {
    if (!temp_path || !final_path) return false;
    // SPIFFS does not reliably replace an existing destination via rename().
    // The temporary file is already complete here, so remove only the stale
    // destination before installing it and always clean up on failure.
    if (unlink(final_path) != 0 && errno != ENOENT) {
        ESP_LOGE(TAG, "pet cache %s %d remove failed: path=%s errno=%d (%s)",
                 kind, frame_index, final_path, errno, strerror(errno));
        unlink(temp_path);
        return false;
    }
    if (rename(temp_path, final_path) != 0) {
        ESP_LOGE(TAG, "pet cache %s %d rename failed: %s -> %s errno=%d (%s)",
                 kind, frame_index, temp_path, final_path, errno, strerror(errno));
        unlink(temp_path);
        return false;
    }
    return true;
}

static esp_err_t cache_pet_asset(const pet_asset_ref_t *ref,
                                 uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!s_storage_mounted || !ref || !frames) return ESP_ERR_INVALID_STATE;
    size_t frame_bytes = (size_t)ref->width * (size_t)ref->height * PET_ASSET_BYTES_PER_PIXEL;
    char final_path[64], temp_path[64];
    // The metadata is the commit record. Remove it before modifying frames so
    // an interrupted update can never make a mixture of revisions look valid.
    // Writing each final frame directly avoids keeping a second 192 KiB copy
    // in fragmented SPIFFS and eliminates the multi-minute GC stalls observed
    // with per-frame temp-file renames.
    unlink(PET_ASSET_CACHE_META_PATH);
    unlink(PET_ASSET_CACHE_META_TMP_PATH);
    for (int i = 0; i < ref->frame_count; ++i) {
        if (pet_cache_stop_requested()) return ESP_ERR_INVALID_STATE;
        snprintf(final_path, sizeof(final_path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        snprintf(temp_path, sizeof(temp_path), PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT, (unsigned)i);
        unlink(temp_path);
        // The preview cache is optional. Do not request eager multi-block
        // SPIFFS GC here: it can monopolize CPU1 long enough to trip the idle
        // watchdog. A fragmented cache may fail this best-effort write while
        // the already verified in-memory pet remains fully usable.
        unlink(final_path);
        vTaskDelay(1);
        errno = 0;
        FILE *file = fopen(final_path, "wb");
        bool ok = write_all(file, frames[i], frame_bytes, final_path, i);
        if (file && fclose(file) != 0) {
            ESP_LOGE(TAG, "pet cache frame %d close failed: path=%s errno=%d (%s)",
                     i, final_path, errno, strerror(errno));
            ok = false;
        }
        if (!ok) {
            unlink(final_path);
            return ESP_FAIL;
        }
    }
    for (int i = ref->frame_count; i < PET_ASSET_MAX_FRAMES; ++i) {
        if (pet_cache_stop_requested()) return ESP_ERR_INVALID_STATE;
        snprintf(final_path, sizeof(final_path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        unlink(final_path);
    }
    errno = 0;
    FILE *meta_file = fopen(PET_ASSET_CACHE_META_TMP_PATH, "wb");
    if (!meta_file || fprintf(meta_file, "MACLAW_PET_V2\n%s\n%d %d %d %d\n",
                             ref->revision, ref->width, ref->height, ref->frame_ms,
                             ref->frame_count) < 0) {
        ESP_LOGE(TAG, "pet cache metadata open/header failed: path=%s errno=%d (%s)",
                 PET_ASSET_CACHE_META_TMP_PATH, errno, strerror(errno));
        if (meta_file) fclose(meta_file);
        unlink(PET_ASSET_CACHE_META_TMP_PATH);
        return ESP_FAIL;
    }
    bool meta_ok = true;
    for (int i = 0; i < ref->frame_count; ++i) {
        if (pet_cache_stop_requested()) {
            meta_ok = false;
            break;
        }
        if (fprintf(meta_file, "%s\n", ref->sha256[i]) < 0) {
            ESP_LOGE(TAG, "pet cache metadata hash %d failed: errno=%d (%s)",
                     i, errno, strerror(errno));
            meta_ok = false;
            break;
        }
    }
    if (fclose(meta_file) != 0) {
        ESP_LOGE(TAG, "pet cache metadata close failed: errno=%d (%s)", errno, strerror(errno));
        meta_ok = false;
    }
    if (!meta_ok || !replace_cached_file(PET_ASSET_CACHE_META_TMP_PATH,
                                         PET_ASSET_CACHE_META_PATH, "metadata", -1)) {
        unlink(PET_ASSET_CACHE_META_TMP_PATH);
        return ESP_FAIL;
    }
    ESP_LOGI(TAG, "pet asset cached: revision=%s frames=%d bytes_per_frame=%u",
             ref->revision, ref->frame_count, (unsigned)frame_bytes);
    return ESP_OK;
}

static esp_err_t cache_pet_asset_first_frame(const pet_asset_ref_t *ref,
                                             uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    if (!s_storage_mounted || !ref || !frames || !frames[0]) return ESP_ERR_INVALID_STATE;
    pet_asset_ref_t preview = *ref;
    preview.frame_count = 1;
    return cache_pet_asset(&preview, frames);
}

static void clear_pet_asset_cache_direct(void);

/* 过期缓存自愈：容量门禁会把注定被替换的旧 revision 缓存误算成永久占用。
 * 仅当磁盘缓存 revision 与服务器正在提供的不同（或元数据缺失/损坏，即上
 * 次更新被中断留下的孤儿帧）才删除；同 revision 的缓存本身就是目标内容，
 * 删了只会白白重新下载一遍。当前渲染用的是安装时交给 UI 的 PSRAM 缩放副
 * 本，磁盘缓存在运行时没有任何读者，unlink 不影响正在显示的宠物。
 * 只碰 pet_asset_* 文件；meeting.wav 是用户录音，绝不在此触碰。
 * 必须在内置栈的 pet_cache worker 里执行（flash 编程期 PSRAM 栈不可用）。 */
static bool drop_pet_asset_cache_if_stale_direct(const char *new_revision) {
    char magic[24] = {0}, revision[40] = {0};
    FILE *meta = fopen(PET_ASSET_CACHE_META_PATH, "rb");
    bool current = false;
    if (meta) {
        if (fgets(magic, sizeof(magic), meta) && !strcmp(magic, "MACLAW_PET_V2\n") &&
            fgets(revision, sizeof(revision), meta)) {
            char *newline = strpbrk(revision, "\r\n");
            if (newline) *newline = '\0';
            current = new_revision && new_revision[0] &&
                      !strcmp(revision, new_revision);
        }
        fclose(meta);
    }
    if (current) return false;
    if (pet_cache_stop_requested()) return false;
    clear_pet_asset_cache_direct();
    ESP_LOGI(TAG, "stale pet cache dropped: cached=%s new=%s",
             meta ? revision : "none", new_revision ? new_revision : "?");
    return true;
}

typedef enum {
    PET_CACHE_FIRST_FRAME,
    PET_CACHE_CLEAR,
    PET_CACHE_DROP_STALE,
} pet_cache_operation_t;

typedef struct {
    pet_cache_operation_t operation;
    pet_asset_ref_t preview;
    uint8_t *frames[PET_ASSET_MAX_FRAMES];
    esp_err_t result;
    bool dropped;
} pet_cache_job_t;

static void pet_cache_task(void *arg) {
    pet_cache_job_t *job = (pet_cache_job_t *)arg;
    /* The creator publishes this task before opening its start gate. That
     * removes the create/exit race where a very short CLEAR job could clear
     * its handle and then be resurrected by the creator's late assignment. */
    (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
    if (pet_cache_stop_requested()) {
        job->result = ESP_ERR_INVALID_STATE;
    } else if (job->operation == PET_CACHE_CLEAR) {
        clear_pet_asset_cache_direct();
        job->result = ESP_OK;
    } else if (job->operation == PET_CACHE_DROP_STALE) {
        job->dropped = drop_pet_asset_cache_if_stale_direct(job->preview.revision);
        job->result = ESP_OK;
    } else {
        job->result = cache_pet_asset_first_frame(&job->preview, job->frames);
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_pet_cache_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // This worker is explicitly created with an internal-RAM stack because
    // SPIFFS temporarily disables the shared flash/PSRAM cache. Pair the
    // allocator with its matching deleter; the ordinary FreeRTOS deleter can
    // pick the external heap on this target and reintroduce the same fault at
    // task teardown.
    vTaskDeleteWithCaps(NULL);
}

static bool pet_cache_stop_requested(void) {
    bool stop_requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_pet_cache_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return stop_requested;
}

/* Cache jobs borrow both their descriptor and their first RGB565 frame from
 * the requesting task. Never force-delete this worker: join its normal exit
 * before either owner can free that borrowed memory. The task only blocks at
 * page-sized writes/yields, so polling the published handle is bounded by the
 * same lifecycle deadline and does not need a completion semaphore whose
 * lifetime could race the borrowed job. */
static esp_err_t stop_pet_cache_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_pet_cache_stop_requested = true;
    task = s_pet_cache_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    xTaskNotifyGive(task);
    TickType_t started = xTaskGetTickCount();
    while (true) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool stopped = s_pet_cache_task != task;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stopped) {
            ESP_LOGI(TAG, "pet cache task stopped");
            return ESP_OK;
        }
        if (xTaskGetTickCount() - started >= pdMS_TO_TICKS(timeout_ms)) {
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(10));
    }
}

static esp_err_t run_pet_cache_operation(
    pet_cache_operation_t operation,
    const pet_asset_ref_t *ref,
    uint8_t *const frames[PET_ASSET_MAX_FRAMES],
    bool *dropped) {
    if (operation == PET_CACHE_FIRST_FRAME &&
        (!ref || !frames || !frames[0])) return ESP_ERR_INVALID_ARG;
    if (operation == PET_CACHE_DROP_STALE && !ref) return ESP_ERR_INVALID_ARG;
    if (!device_storage_allows_optional_flash_work()) {
        ESP_LOGI(TAG, "pet cache skipped: board declines optional flash work");
        return ESP_ERR_NOT_SUPPORTED;
    }
    if (!s_pet_cache_flash_mutex || pet_cache_stop_requested()) return ESP_ERR_INVALID_STATE;
    /* SPIFFS/esp_flash disables the shared flash/PSRAM cache while programming.
     * Both startup media and gateway polling deliberately use PSRAM stacks, so
     * neither may execute unlink/fopen itself. Serialize all pet-cache flash
     * mutations and run them on a short-lived internal-stack worker while any
     * large RGB565A8 frame remains borrowed from the waiting owner. */
    const TickType_t admission_started = xTaskGetTickCount();
    while (xSemaphoreTake(s_pet_cache_flash_mutex, pdMS_TO_TICKS(50)) != pdTRUE) {
        if (pet_cache_stop_requested()) return ESP_ERR_INVALID_STATE;
        if (xTaskGetCurrentTaskHandle() == s_startup_pet_asset_task &&
            startup_pet_asset_stop_requested()) return ESP_ERR_INVALID_STATE;
        if (xTaskGetTickCount() - admission_started >= pdMS_TO_TICKS(500)) {
            return ESP_ERR_TIMEOUT;
        }
    }
    if (pet_cache_stop_requested() ||
        (xTaskGetCurrentTaskHandle() == s_startup_pet_asset_task &&
         startup_pet_asset_stop_requested())) {
        xSemaphoreGive(s_pet_cache_flash_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    pet_cache_job_t *job = heap_caps_calloc(
        1, sizeof(*job), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!job) {
        xSemaphoreGive(s_pet_cache_flash_mutex);
        return ESP_ERR_NO_MEM;
    }
    job->operation = operation;
    if (operation == PET_CACHE_FIRST_FRAME) {
        job->preview = *ref;
        job->preview.frame_count = 1;
        job->frames[0] = frames[0];
    } else if (operation == PET_CACHE_DROP_STALE) {
        job->preview = *ref;
    }
    job->result = ESP_FAIL;
    TaskHandle_t task = NULL;
    // Do not let the generic task allocator place this stack in PSRAM.  Even
    // an unlink() reads stack-resident libc/SPIFFS state while flash cache is
    // disabled, which manifests as a cache-disabled assert and software reset
    // during an online pet switch.
    BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        pet_cache_task, "maclaw_pet_cache", 8192,
        job, 1, &task, 1, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_pet_cache_task = created == pdPASS ? task : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        heap_caps_free(job);
        xSemaphoreGive(s_pet_cache_flash_mutex);
        return ESP_ERR_NO_MEM;
    }
    xTaskNotifyGive(task);
    while (true) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool running = s_pet_cache_task == task;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!running) break;
        if (pet_cache_stop_requested() ||
            (xTaskGetCurrentTaskHandle() == s_startup_pet_asset_task &&
             startup_pet_asset_stop_requested())) {
            /* A running cache job is always joined before its caller frees the
             * borrowed RGB565 frame and job storage. The worker sees the same
             * stop token between page writes, so this never strands a task. */
            (void)stop_pet_cache_task(500);
        }
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    esp_err_t result = job->result;
    if (dropped) *dropped = job->dropped;
    heap_caps_free(job);
    xSemaphoreGive(s_pet_cache_flash_mutex);
    return result;
}

static esp_err_t cache_pet_first_frame_safe(
    const pet_asset_ref_t *ref,
    uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    return run_pet_cache_operation(PET_CACHE_FIRST_FRAME, ref, frames, NULL);
}

static esp_err_t clear_pet_asset_cache_safe(void) {
    return run_pet_cache_operation(PET_CACHE_CLEAR, NULL, NULL, NULL);
}

/* 安装新宠物前的自愈入口：先回收注定被替换的旧 revision 缓存，再让容量
 * 门禁重新取样。返回 true 表示确实删掉了旧缓存文件。 */
static bool drop_stale_pet_asset_cache(const pet_asset_ref_t *ref) {
    if (!s_storage_mounted || !ref) return false;
    bool dropped = false;
    if (run_pet_cache_operation(PET_CACHE_DROP_STALE, ref, NULL, &dropped) != ESP_OK) {
        return false;
    }
    return dropped;
}

static void clear_pet_asset_cache_direct(void) {
    unlink(PET_ASSET_CACHE_META_PATH);
    unlink(PET_ASSET_CACHE_META_TMP_PATH);
    char path[64];
    for (int i = 0; i < PET_ASSET_MAX_FRAMES; ++i) {
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        unlink(path);
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_TMP_PATH_FORMAT, (unsigned)i);
        unlink(path);
    }
    // Remove opaque V1 assets written by builds that used the old two-byte
    // RGB565 format. Their metadata can otherwise survive an OTA update and
    // be misread as transparent frames.
    for (int i = 0; i < PET_ASSET_MAX_FRAMES; ++i) {
        snprintf(path, sizeof(path), "/storage/pet_asset_%u.rgb565le", (unsigned)i);
        unlink(path);
    }
}

static esp_err_t clear_pet_asset_cache(void) {
    return clear_pet_asset_cache_safe();
}

static esp_err_t clear_applied_pet_asset(void) {
    if (!s_pet_asset_apply_mutex ||
        xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
        return ESP_ERR_INVALID_STATE;
    }
    esp_err_t err = device_status_to_platform_error(
        app_ui_set_pet_asset(NULL, 0, 0, 0, 0));
    if (err == ESP_OK) {
        s_loaded_pet_asset_revision[0] = '\0';
        s_loaded_pet_asset_frame_count = 0;
        esp_err_t cache_err = clear_pet_asset_cache();
        if (cache_err != ESP_OK) err = cache_err;
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
    FILE *meta = fopen(PET_ASSET_CACHE_META_PATH, "rb");
    if (!meta) return false;
    char magic[24] = {0}, revision[40] = {0};
    pet_asset_ref_t ref = {0};
    char hashes[PET_ASSET_MAX_FRAMES][66] = {{0}};
    bool valid = fgets(magic, sizeof(magic), meta) && !strcmp(magic, "MACLAW_PET_V2\n") &&
                 fgets(revision, sizeof(revision), meta) &&
                 fscanf(meta, "%d %d %d %d", &ref.width, &ref.height,
                        &ref.frame_ms, &ref.frame_count) == 4 &&
                 ref.frame_count >= 1 && ref.frame_count <= PET_ASSET_MAX_FRAMES;
    for (int i = 0; valid && i < ref.frame_count; ++i) {
        valid = fscanf(meta, "%65s", hashes[i]) == 1;
    }
    fclose(meta);
    char *revision_newline = strpbrk(revision, "\r\n");
    if (revision_newline) *revision_newline = '\0';
    if (!valid || ref.width < 32 || ref.width > PET_ASSET_MAX_DIMENSION ||
        ref.height < 32 || ref.height > PET_ASSET_MAX_DIMENSION ||
        ref.frame_count < 1 || ref.frame_count > PET_ASSET_MAX_FRAMES) {
        (void)clear_pet_asset_cache();
        return false;
    }
    size_t frame_bytes = (size_t)ref.width * (size_t)ref.height * PET_ASSET_BYTES_PER_PIXEL;
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
    char path[64];
    for (int i = 0; i < ref.frame_count; ++i) {
        snprintf(path, sizeof(path), PET_ASSET_CACHE_FRAME_PATH_FORMAT, (unsigned)i);
        struct stat info;
        if (stat(path, &info) != 0 || info.st_size != (off_t)frame_bytes) break;
        FILE *file = fopen(path, "rb");
        frames[i] = heap_caps_malloc(frame_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!frames[i]) frames[i] = malloc(frame_bytes);
        bool ok = frames[i] && file && fread(frames[i], 1, frame_bytes, file) == frame_bytes;
        if (file) fclose(file);
        if (ok) {
            uint8_t digest[32]; size_t digest_len = 0; char actual[65];
            ok = psa_hash_compute(PSA_ALG_SHA_256, (const uint8_t *)frames[i], frame_bytes, digest,
                                  sizeof(digest), &digest_len) == PSA_SUCCESS &&
                 digest_len == sizeof(digest);
            if (ok) { digest_hex(digest, actual); ok = !strcasecmp(actual, hashes[i]); }
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
            strlcpy(s_loaded_pet_asset_revision, revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = installed_frames;
            ESP_LOGI(TAG, "cached pet asset applied: revision=%s frames=%d/%d frame_ms=%d",
                     revision, installed_frames, ref.frame_count, installed_frame_ms);
        }
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    if (!loaded) (void)clear_pet_asset_cache();
    return loaded;
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
        app_ui_set_pet_asset_consuming(frames, (size_t)ref->frame_count,
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
        int next_count = used_count > 4 ? 4 : used_count > 2 ? 2 : 1;
        if (next_count > remaining_count) next_count = remaining_count;
        if (!next_count) break;
        used_frame_ms = ref->frame_ms * ref->frame_count / next_count;
        ESP_LOGW(TAG, "pet asset memory pressure; retrying with %d/%d frames",
                 next_count, ref->frame_count);
        err = device_status_to_platform_error(
            app_ui_set_pet_asset_consuming(frames, (size_t)next_count,
                                            (size_t)ref->width, (size_t)ref->height,
                                            (uint32_t)used_frame_ms));
        used_count = next_count;
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
        app_ui_set_pet_asset(first, 1, (size_t)ref->width,
                             (size_t)ref->height,
                             (uint32_t)ref->frame_ms));
}
static esp_err_t apply_pet_asset_ref(cJSON *object) {
    pet_asset_ref_t descriptor;
    pet_asset_ref_t ref;
    if (!parse_pet_asset_ref(object, &descriptor) ||
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
    if (err == ESP_OK) err = download_pet_asset_frames(&ref, frames, false);
    if (err == ESP_OK) {
        if (!s_pet_asset_apply_mutex ||
            xSemaphoreTake(s_pet_asset_apply_mutex, portMAX_DELAY) != pdTRUE) {
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK) {
        // The consuming install releases the source frames; commit the preview
        // cache first.  Frames are SHA-verified after download.
        esp_err_t cache_err = cache_pet_first_frame_safe(&ref, frames);
        if (cache_err != ESP_OK) ESP_LOGW(TAG, "pet asset cache failed: %s", esp_err_to_name(cache_err));
        int installed_frames = 0, installed_frame_ms = 0;
        err = install_pet_asset_with_fallback(&ref, frames, &installed_frames,
                                              &installed_frame_ms);
        if (err == ESP_OK) {
            strlcpy(s_loaded_pet_asset_revision, ref.revision,
                    sizeof(s_loaded_pet_asset_revision));
            s_loaded_pet_asset_frame_count = installed_frames;
            ESP_LOGI(TAG, "GUI pet asset applied: revision=%s frames=%d/%d frame_ms=%d size=%dx%d",
                     ref.revision, installed_frames, ref.frame_count, installed_frame_ms,
                     ref.width, ref.height);
        }
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
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
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {0};
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
        err = download_pet_asset_frames(&ref, frames, true);
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
        // The consuming install releases each source frame as it is scaled, so
        // the preview cache must be committed first.  Every downloaded frame is
        // SHA-verified, and this same first frame is already what standby shows.
        // EchoEar runs this transfer worker on a PSRAM stack; SPIFFS programming
        // turns off the shared cache and cannot execute safely on that stack.
        // Flash/VFS work must run from an internal-stack task. The startup
        // installer itself is PSRAM-backed on EchoEar and Fangtang.
        if (device_storage_allows_optional_flash_work()) {
            esp_err_t cache_err = cache_pet_first_frame_safe(&ref, frames);
            if (cache_err != ESP_OK) {
                ESP_LOGW(TAG, "deferred pet preview cache failed: %s",
                         esp_err_to_name(cache_err));
            }
        } else {
            /* Decorative persistence is deliberately unsupported by this
             * profile.  Avoid manufacturing a warning after the HAL made
             * that policy decision; the verified in-memory pet is retained. */
            ESP_LOGI(TAG, "deferred pet preview cache skipped by board policy");
        }
        int installed_frames = 0, installed_frame_ms = 0;
        if (startup_pet_asset_stop_requested()) {
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
        xSemaphoreGive(s_pet_asset_apply_mutex);
    }
    // The display port retains its own scaled PSRAM copies.  The source HTTP
    // buffers are normally released here, but on EchoEar that deallocation
    // races the QSPI full-frame path and causes a cache-disable assertion
    // immediately after the visible pet has been installed. Retain the tiny
    // one-shot source set for this boot; it is bounded (8 × 192 KiB) and avoids
    // a restart that would otherwise erase the successful standby transition.
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
    // Keep pending true for the entire download/install/cache transaction so
    // a queued pet_profile mirror is ACKed without starting a competing copy.
    s_startup_pet_asset_pending = false;
    return err;
}

static void startup_pet_asset_task(void *arg) {
    (void)arg;
    int64_t started_us = esp_timer_get_time();
    // Offline wake is a core standby capability; an optional pet download must
    // never leave a visibly ready device unable to hear its wake phrase. Keep
    // MultiNet listening throughout this low-priority transaction. HTTP data
    // and this worker's stack live in PSRAM, while SPIFFS mutation is delegated
    // to the internal-stack cache worker below.
    esp_err_t err = apply_deferred_pet_asset();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "deferred startup pet asset ignored: %s", esp_err_to_name(err));
    }
    ESP_LOGI(TAG, "post-ready pet asset work complete in %lu ms",
             (unsigned long)((esp_timer_get_time() - started_us) / 1000));
    /* Signal completion before making the handle reusable. A re-arm may create
     * its next worker as soon as the handle becomes NULL; publishing the join
     * first prevents this retiring worker from ever signalling the new
     * worker's completion semaphore or clearing its handle. */
    taskENTER_CRITICAL(&s_task_state_lock);
    SemaphoreHandle_t completed = s_startup_pet_asset_stopped;
    if (completed) xSemaphoreGive(completed);
    s_startup_pet_asset_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // This worker's stack comes from xTaskCreatePinnedToCoreWithCaps().  The
    // regular FreeRTOS deleter frees it through the internal heap and asserts
    // immediately after the full pet pack is installed; use the paired caps
    // deleter so its PSRAM stack is released by the right allocator.
    vTaskDeleteWithCaps(NULL);
}

#define STARTUP_PET_RETRY_MAX 6u
#define STARTUP_PET_RETRY_INTERVAL_US (10LL * 1000 * 1000)

static bool ensure_startup_pet_retry_timer(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    esp_timer_handle_t existing = s_startup_pet_retry_timer;
    bool stopped = s_startup_pet_asset_stop_requested;
    bool callback_admission_open = s_startup_pet_retry_callback_admission_open;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stopped) return false;
    if (existing) return callback_admission_open;
    if (!s_startup_pet_retry_callback_drained) return false;
    esp_timer_handle_t timer = NULL;
    const esp_timer_create_args_t timer_args = {
        .callback = startup_pet_retry_timer_cb,
        .name = "pet_retry",
    };
    if (esp_timer_create(&timer_args, &timer) != ESP_OK) return false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_startup_pet_retry_timer && !s_startup_pet_asset_stop_requested) {
        while (xSemaphoreTake(s_startup_pet_retry_callback_drained, 0) == pdTRUE) {}
        s_startup_pet_retry_callback_admission_open = true;
        s_startup_pet_retry_timer = timer;
        timer = NULL;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (timer) (void)esp_timer_delete(timer);
    taskENTER_CRITICAL(&s_task_state_lock);
    const bool ready = s_startup_pet_retry_timer != NULL &&
                       s_startup_pet_retry_callback_admission_open &&
                       !s_startup_pet_asset_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return ready;
}

/* A retained timer object can be stopped while an optional caller is deciding
 * to rearm it. Hold the same gate as lifecycle stop across ensure + start, so
 * rollback cannot return with a newly scheduled callback behind it. */
static bool schedule_startup_pet_retry_timer(void) {
    if (!s_startup_pet_retry_timer_mutex ||
        xSemaphoreTake(s_startup_pet_retry_timer_mutex, 0) != pdTRUE) {
        return false;
    }
    bool scheduled = false;
    if (ensure_startup_pet_retry_timer()) {
        esp_timer_handle_t timer = NULL;
        taskENTER_CRITICAL(&s_task_state_lock);
        if (s_startup_pet_retry_callback_admission_open &&
            !s_startup_pet_asset_stop_requested) {
            timer = s_startup_pet_retry_timer;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        scheduled = timer &&
                    esp_timer_start_once(timer, STARTUP_PET_RETRY_INTERVAL_US) == ESP_OK;
    }
    xSemaphoreGive(s_startup_pet_retry_timer_mutex);
    return scheduled;
}

static void startup_pet_retry_timer_cb(void *arg) {
    (void)arg;
    /* Never call apply_deferred_startup_pet_asset() here.  On Waveshare that
     * path may examine storage/PSRAM and create a worker, which exceeds the
     * timer service task's stack and reboots the device.  gateway_poll_task()
     * owns a PSRAM-backed 16 KiB stack and consumes this flag below. */
    bool entered = false;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_startup_pet_retry_callback_admission_open) {
        ++s_startup_pet_retry_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!entered) return;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (!s_startup_pet_asset_stop_requested) s_startup_pet_retry_due = true;
    --s_startup_pet_retry_callbacks_inflight;
    bool drained = !s_startup_pet_retry_callback_admission_open &&
                   s_startup_pet_retry_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (drained && s_startup_pet_retry_callback_drained) {
        xSemaphoreGive(s_startup_pet_retry_callback_drained);
    }
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
    // Never block the gateway startup owner for an optional asset. Downloads
    // and SPIFFS GC may take minutes on a fragmented partition; the independent
    // worker keeps handshake retries and subsequent runtime state responsive.
    if (s_startup_pet_asset_task) return;
    // The ready pet is a visible part of the normal standby flow.  After the
    // wake model and long-poll worker are live, EchoEar no longer has an 8 KiB
    // contiguous internal block, so use the PSRAM-backed task-stack path used
    // by the other deferred workers instead of silently dropping the asset.
    // HTTPS ECDH is synchronous inside esp_http_client_perform().  The startup
    // animation transaction is optional after the cached/first-frame preview
    // has made standby usable, so it must not outrank IDLE1: a cold TLS
    // handshake can otherwise occupy CPU1 for more than the task-WDT window.
    // At idle priority FreeRTOS time-slices this worker with IDLE1; downloads
    // still complete whenever the device is otherwise idle, without trading
    // away the first-frame preview or the full-resolution animation install.
    // A naturally finished worker leaves its completion token for a possible
    // observer. Replace it only after its task handle is clear, so a later
    // stop cannot consume stale completion state for a newly created worker.
    /* A finished worker leaves its semaphore token behind. Replace it only
     * while holding the same state lock used by stop/join and task exit, so a
     * late completion cannot be mistaken for the next worker's join. */
    taskENTER_CRITICAL(&s_task_state_lock);
    SemaphoreHandle_t stale_completion = s_startup_pet_asset_stopped;
    s_startup_pet_asset_stopped = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stale_completion) vSemaphoreDelete(stale_completion);
    SemaphoreHandle_t completion = xSemaphoreCreateBinary();
    if (!completion) {
        ESP_LOGW(TAG, "cannot allocate deferred pet asset completion semaphore");
        return;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_startup_pet_asset_stop_requested = false;
    s_startup_pet_asset_stopped = completion;
    taskEXIT_CRITICAL(&s_task_state_lock);
    BaseType_t created = xTaskCreatePinnedToCoreWithCaps(
        startup_pet_asset_task, "maclaw_pet_startup", 8192, NULL, 0,
        &s_startup_pet_asset_task, 1, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_startup_pet_asset_task = NULL;
        s_startup_pet_asset_stopped = NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        vSemaphoreDelete(completion);
        ESP_LOGW(TAG, "cannot start deferred pet asset worker");
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
    if (!s_startup_pet_asset_task) {
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
    return device_status_to_esp_err(device_wake_word_start(on_wake, context));
}

static esp_err_t audio_wake_word_stop(void) {
    return device_status_to_esp_err(device_wake_word_stop());
}

static esp_err_t audio_wake_word_stop_with_timeout(uint32_t timeout_ms) {
    return device_status_to_esp_err(device_wake_word_stop_with_timeout(timeout_ms));
}

static esp_err_t play_audio_payload(const char *mime, const uint8_t *data, size_t len) {
    if (!data || len == 0) return ESP_ERR_INVALID_ARG;
    if (audio_payload_is_mp3(mime, data, len)) return mp3_player_play(data, len);
    return device_status_to_esp_err(device_audio_play_wav(data, (uint32_t)len));
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

static void response_release(http_response_t *response) {
    if (!response) return;
    // HTTP bodies are allocated with heap_caps_malloc() in PSRAM (with an
    // internal-capable fallback). Release them through the same allocator;
    // the ordinary libc heap path can assert while the LCD transfer briefly
    // suspends flash-cache activity during a large pet install.
    heap_caps_free(response->data);
    response->data = NULL;
    response->capacity = 0;
    response->len = 0;
    response->status = 0;
    response->truncated = false;
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

static void apply_ambient_json(cJSON *ambient) {
    if (!cJSON_IsObject(ambient)) return;
    int glyphs_cached = apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(ambient, "glyphs"));
    cJSON *weather = cJSON_GetObjectItemCaseSensitive(ambient, "weather");
    if (!cJSON_IsObject(weather)) return;
    const char *summary = json_string(weather, "summary");
    const char *location = json_string(weather, "location");
    int temperature_c = 0;
    if (!summary || !summary[0] || !json_number(weather, "temperatureC", &temperature_c) ||
        temperature_c < -80 || temperature_c > 80) {
        ESP_LOGW(TAG, "ignored invalid ambient weather payload");
        return;
    }
    strlcpy(s_weather_summary, summary, sizeof(s_weather_summary));
    strlcpy(s_weather_location, location ? location : "", sizeof(s_weather_location));
    s_weather_temperature_c = temperature_c;
    cJSON *expires = cJSON_GetObjectItemCaseSensitive(ambient, "expiresAt");
    s_weather_expires_at_ms = cJSON_IsNumber(expires) ? (int64_t)expires->valuedouble : 0;
    s_weather_valid = true;
    // The long-poll worker intentionally has a PSRAM stack to leave internal
    // memory for TLS/I2S. NVS disables caches during flash operations, where a
    // PSRAM-backed stack is illegal and asserts. Persist only from an
    // internal-stack execution context; the in-memory weather model is already
    // authoritative and a later handshake will safely refresh the cache.
    if (esp_ptr_internal((const void *)&ambient)) {
        save_ambient_weather();
    } else {
        ESP_LOGI(TAG, "ambient weather cache deferred from external-stack poll task");
    }
    ESP_LOGI(TAG, "ambient weather received: summary='%s' temp=%d location='%s' glyphs_cached=%d raw_location=%s",
             s_weather_summary, s_weather_temperature_c, s_weather_location,
             glyphs_cached, location ? "present" : "missing");
}

static bool glyph_codepoint_from_key(const char *key, uint32_t *codepoint) {
    if (!key || !codepoint || strlen(key) != 6 || key[0] != 'U' || key[1] != '+') return false;
    char *end = NULL;
    unsigned long value = strtoul(key + 2, &end, 16);
    if (!end || *end || value < 0x20 || value > 0xFFFF ||
        (value >= 0xD800 && value <= 0xDFFF)) return false;
    *codepoint = (uint32_t)value;
    return true;
}

// Decode every glyph before accepting it into the display cache. A bad value
// never invalidates previously cached glyphs, so a transient/corrupt payload
// cannot turn already-readable text back into blanks.
static int apply_glyphs_json(cJSON *glyphs) {
    if (!cJSON_IsObject(glyphs)) return 0;
    int accepted = 0;
    cJSON *entry = NULL;
    cJSON_ArrayForEach(entry, glyphs) {
        if (accepted >= DYNAMIC_GLYPH_MAX_PER_MESSAGE || !cJSON_IsString(entry) || !entry->string) continue;
        uint32_t codepoint = 0;
        if (!glyph_codepoint_from_key(entry->string, &codepoint)) continue;
        uint8_t bitmap[DYNAMIC_GLYPH_BYTES];
        size_t decoded = 0;
        int result = mbedtls_base64_decode(bitmap, sizeof(bitmap), &decoded,
                                           (const unsigned char *)entry->valuestring,
                                           strlen(entry->valuestring));
        if (result != 0 || decoded != sizeof(bitmap)) {
            ESP_LOGW(TAG, "ignored invalid dynamic glyph %s", entry->string);
            continue;
        }
        if (app_ui_cache_glyph(codepoint, bitmap)) {
            ++accepted;
            ESP_LOGI(TAG, "dynamic glyph cached: U+%04lX", (unsigned long)codepoint);
        }
    }
    if (accepted) ESP_LOGI(TAG, "dynamic glyph cache updated: received=%d", accepted);
    return accepted;
}

static void refresh_ambient_display(void) {
    time_t system_now = 0;
    time(&system_now);
    int64_t monotonic_us = esp_timer_get_time();
    bool system_clock_ready = system_now >= 1672531200; // 2023-01-01 UTC
    taskENTER_CRITICAL(&s_task_state_lock);
    if (system_clock_ready) {
        time_t predicted = s_display_clock_epoch;
        if (s_display_clock_valid) {
            predicted += (time_t)((monotonic_us - s_display_clock_anchor_us) / 1000000);
        }
        // Accept the initial SNTP value and any later material correction, but
        // otherwise advance only from the local ESP32 monotonic clock.
        if (!s_display_clock_valid || llabs((long long)(system_now - predicted)) > 2) {
            s_display_clock_epoch = system_now;
            s_display_clock_anchor_us = monotonic_us;
            s_display_clock_valid = true;
        }
    }
    bool display_clock_valid = s_display_clock_valid;
    time_t display_clock_epoch = s_display_clock_epoch;
    int64_t display_clock_anchor_us = s_display_clock_anchor_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    time_t now = display_clock_valid
                     ? display_clock_epoch + (time_t)((monotonic_us - display_clock_anchor_us) / 1000000)
                     : 0;
    struct tm local = {0};
    localtime_r(&now, &local);
    char current_time[9] = "--:--:--";
    char date[8] = "--/--";
    const char *weekdays[] = {"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"};
    const char *weekday = "时间同步中";
    if (display_clock_valid) {
        unsigned month = (unsigned)(local.tm_mon + 1) % 100u;
        unsigned day = (unsigned)local.tm_mday % 100u;
        snprintf(current_time, sizeof(current_time), "%02d:%02d:%02d",
                 local.tm_hour, local.tm_min, local.tm_sec);
        snprintf(date, sizeof(date), "%02u/%02u", month, day);
        weekday = weekdays[local.tm_wday];
    }
    int64_t now_ms = (int64_t)now * 1000;
    bool stale = s_weather_valid && s_weather_expires_at_ms > 0 && now_ms > s_weather_expires_at_ms;
    app_ui_set_ambient(current_time, s_weather_location, date, weekday,
                           s_weather_summary, s_weather_temperature_c,
                           s_weather_valid, stale);
}

static void ambient_task(void *arg) {
    (void)arg;
    while (true) {
        refresh_ambient_display();
        // Redraw immediately after the next monotonic second boundary rather
        // than drifting with scheduler latency. This keeps the displayed
        // seconds visibly advancing even after the task has been running for
        // a long time.
        int64_t now_us = esp_timer_get_time();
        int64_t wait_us = 1000000 - (now_us % 1000000) + 1000;
        if (ulTaskNotifyTake(pdTRUE,
                             pdMS_TO_TICKS((wait_us + 999) / 1000)) != 0) {
            break;
        }
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_ambient_task == self) s_ambient_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_ambient_task_stopped) xSemaphoreGive(s_ambient_task_stopped);
    /* A natural exit still releases its own entry so a future clock cadence
     * can start.  Crucially, this task never takes the Registry mutex
     * unbounded: an owner-wide rollback may be joining it concurrently. If
     * the short bookkeeping attempt loses that race, the retained immutable
     * entry remains fail-closed for the lifecycle owner to remove later. */
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_POWER,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
}

// Ambient state and pet-profile updates are server initiated. Keep a single
// long-poll running even while the user is not speaking; otherwise weather
// pushed after the startup handshake would sit at Hub until the next button
// interaction. Its dedicated client lane prevents an idle long poll from
// adding several seconds to foreground voice upload and command submission.
static void gateway_poll_task(void *arg) {
    (void)arg;
    unsigned consecutive_failures = 0;
    while (true) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool stop_requested = s_gateway_poll_stop_requested;
        bool retry_pet = s_startup_pet_retry_due;
        s_startup_pet_retry_due = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested) break;
        if (s_gateway_token[0]) {
            int64_t started_us = esp_timer_get_time();
            esp_err_t err = poll_reply();
            int64_t elapsed_ms = (esp_timer_get_time() - started_us) / 1000;
            if (err != ESP_OK) {
                if (++consecutive_failures >= 2) {
                    app_ui_set_service_ready(false);
                    firmware_identity_set_service_ready(false);
                }
                if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000)) != 0) break;
            } else {
                consecutive_failures = 0;
                app_ui_set_service_ready(true);
                firmware_identity_set_service_ready(true);
                if (elapsed_ms >= 4000) continue;
                // Legacy Hub versions return an empty poll immediately. Avoid
                // a tight TLS reconnect loop until that Hub is upgraded to
                // the v1.1 long-poll implementation.
                // During a foreground command, avoid repeated two-second
                // blind spots while still preventing a hot reconnect loop.
                if (ulTaskNotifyTake(pdTRUE,
                                     pdMS_TO_TICKS(command_display_active() ? 80 : 2000)) != 0) {
                    break;
                }
            }
        } else {
            consecutive_failures = 0;
            app_ui_set_service_ready(false);
            firmware_identity_set_service_ready(false);
            if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000)) != 0) break;
        }
        if (retry_pet && !s_gateway_poll_stop_requested) {
            /* The retry timer only admits work.  Run the actual resource
             * check/worker creation in this normal task, never in esp_timer.
             * Do it after the poll's HTTP response has released its TLS heap. */
            apply_deferred_startup_pet_asset();
        }
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_gateway_poll_task == self) s_gateway_poll_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_gateway_poll_stopped) xSemaphoreGive(s_gateway_poll_stopped);
    /* Do not block the PSRAM worker indefinitely behind a lifecycle Registry
     * stop. A missed short bookkeeping pass deliberately retains its immutable
     * entry for a later fail-closed owner transaction. */
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDeleteWithCaps(NULL);
}

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

static bool ensure_gateway_poll_task(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_started = s_gateway_poll_task != NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!already_started) {
        // MP3 is decoded synchronously when an outgoing audio message arrives.
        // The official decoder needs substantially more stack than JSON/TLS
        // polling alone, especially for stereo Layer III frames.  EchoEar's
        // wake model leaves less than a 16 KiB contiguous internal block at
        // this point, so an internal-stack task fails to start and prevents
        // the final ready/standby pet transition.  This worker only performs
        // HTTP/JSON/MP3 work; keep its large stack in PSRAM, like the clock
        // and recovery workers, to preserve internal RAM for Wi-Fi and I2S.
        s_gateway_poll_stopped = xSemaphoreCreateBinary();
        if (!s_gateway_poll_stopped) {
            ESP_LOGE(TAG, "cannot allocate gateway poll completion semaphore");
            return false;
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_poll_stop_requested = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        TaskHandle_t task = NULL;
        BaseType_t created = xTaskCreateWithCaps(gateway_poll_task,
                                                 "maclaw_gateway_poll", 16384,
                                                 NULL, 3, &task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_poll_task = created == pdPASS ? task : NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (created != pdPASS) {
            vSemaphoreDelete(s_gateway_poll_stopped);
            s_gateway_poll_stopped = NULL;
            ESP_LOGE(TAG, "cannot start gateway poll task");
            return false;
        }
        esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
            .struct_size = sizeof(task_registry_entry_t),
            .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
            .name = "gateway_poll",
            .context = (void *)s_gateway_poll_task,
            .stop = stop_gateway_poll_registry_entry,
        });
        if (registry_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot register gateway poll lifecycle owner: %s",
                     esp_err_to_name(registry_err));
            (void)stop_gateway_poll_task(500);
            return false;
        }
    }
    return true;
}

static bool meeting_resume_supervisor_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_meeting_resume_supervisor_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

/* This registry owner controls only the supervisory retry loop.  If a resume
 * worker was already created, it owns its own NVS/audio/HTTP transaction and
 * is deliberately not force-stopped here.  This keeps the join contract
 * truthful: stopping the supervisor prevents future retries, not a running
 * meeting upload. */
static void meeting_resume_supervisor_task(void *arg) {
    (void)arg;
    if (!s_meeting_resume_supervisor_start_gate ||
        xSemaphoreTake(s_meeting_resume_supervisor_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "meeting resume supervisor start gate unavailable");
        goto finish;
    }

    uint32_t retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
    while (s_meeting_pending && !meeting_resume_supervisor_stop_requested()) {
        bool network_available = device_connectivity_is_active_uplink_ready();
        if (!device_connectivity_is_provisioning_active() && s_gateway_token[0] && network_available &&
            !s_meeting_task_running && !s_foreground_http_requested) {
            // MultiNet can consume the final internal task-stack block before
            // this low-priority supervisor gets scheduled. Unload it here so
            // the resumable worker can be created; meeting_task() restores it
            // after delivery.
            esp_err_t wake_stop_err = audio_wake_word_stop();
            if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
                ESP_LOGW(TAG, "offline wake stop before resume worker: %s",
                         esp_err_to_name(wake_stop_err));
            }
            if (meeting_resume_supervisor_stop_requested()) break;
            log_heap_snapshot("meeting-resume-before-task-create");
            if (start_meeting_task(true)) {
                // The worker persists progress at every chunk. Wait until that
                // pass finishes before deciding whether another retry is needed.
                while (s_meeting_task_running && !meeting_resume_supervisor_stop_requested()) {
                    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500));
                }
                if (meeting_resume_supervisor_stop_requested() || !s_meeting_pending) break;
                // A foreground command may have intentionally preempted this
                // pass. Resume quickly after it releases HTTP instead of
                // escalating the outage backoff to several minutes.
                if (s_foreground_http_requested) {
                    while (s_foreground_http_requested &&
                           !meeting_resume_supervisor_stop_requested()) {
                        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250));
                    }
                    if (meeting_resume_supervisor_stop_requested()) break;
                    retry_ms = MEETING_RESUME_RETRY_INITIAL_MS;
                    continue;
                }
            } else if (!device_connectivity_is_provisioning_active() && !meeting_resume_supervisor_stop_requested()) {
                esp_err_t wake_start_err = audio_wake_word_start(on_wake_word, NULL);
                if (wake_start_err != ESP_OK && wake_start_err != ESP_ERR_INVALID_STATE) {
                    ESP_LOGW(TAG, "offline wake restart after resume create failure: %s",
                             esp_err_to_name(wake_start_err));
                }
            }
        }
        if (meeting_resume_supervisor_stop_requested()) break;
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
        if (retry_ms < MEETING_RESUME_RETRY_MAX_MS) {
            retry_ms *= 2;
            if (retry_ms > MEETING_RESUME_RETRY_MAX_MS) retry_ms = MEETING_RESUME_RETRY_MAX_MS;
        }
    }

finish:
    taskENTER_CRITICAL(&s_task_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    if (s_meeting_resume_supervisor_task == self) {
        s_meeting_resume_supervisor_task = NULL;
    }
    s_meeting_resume_supervisor_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_meeting_resume_supervisor_stopped) xSemaphoreGive(s_meeting_resume_supervisor_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_meeting_resume_supervisor(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_resume_supervisor_stop_requested = true;
    task = s_meeting_resume_supervisor_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_meeting_resume_supervisor_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_resume_supervisor_stopped,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "meeting resume supervisor stopped; active meeting worker, if any, was not interrupted");
    return ESP_OK;
}

static esp_err_t stop_meeting_resume_supervisor_registry_entry(void *context,
                                                                 uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_meeting_resume_supervisor_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_resume_supervisor(timeout_ms);
}

static bool ensure_meeting_resume_supervisor(void) {
    if (!s_meeting_pending) return true;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_running = s_meeting_resume_supervisor_task != NULL ||
                           s_meeting_resume_supervisor_starting;
    if (!already_running) s_meeting_resume_supervisor_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_running) return true;
    if (!s_meeting_resume_supervisor_start_gate || !s_meeting_resume_supervisor_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_resume_supervisor_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "meeting resume supervisor lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_resume_supervisor_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_resume_supervisor_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    // This supervisor only waits and starts a worker. Put its stack in PSRAM
    // so it cannot consume the last contiguous internal block needed by the
    // real upload worker. It never writes flash/NVS, so this is safe.
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreateWithCaps(meeting_resume_supervisor_task,
                                             "maclaw_meeting_resume", 2048,
                                             NULL, 1, &task,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_resume_supervisor_task = NULL;
        s_meeting_resume_supervisor_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot start meeting resume supervisor");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_resume_supervisor_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "meeting_resume_supervisor",
        .context = (void *)task,
        .stop = stop_meeting_resume_supervisor_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting resume supervisor: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_resume_supervisor_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_meeting_resume_supervisor_start_gate);
        (void)stop_meeting_resume_supervisor(500);
        return false;
    }
    xSemaphoreGive(s_meeting_resume_supervisor_start_gate);
    return true;
}

static bool start_gateway_ready_tasks(void) {
    // The authenticated handshake has released its TLS certificate-validation
    // working set. Start the durable local alarm service before polling can
    // expose alarm tools, avoiding a boot-time TLS/cache overlap while keeping
    // the scheduler independent of the selected input/display adapter.
    if (!ensure_alarm_manager_started()) {
        pet("alert");
        app_ui_show_text("设备启动失败", "无法启动闹钟服务");
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
    app_ui_show_startup_screen();
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
    if (!ensure_gateway_poll_task()) {
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
        app_ui_show_text("设备启动失败", "无法启动网关轮询");
        return false;
    }
    /* A retained meeting is durable recovery work.  Start its lightweight
     * supervisor only after the authenticated poll lane and wake listener are
     * both initialized, so it shares the normal connectivity lifecycle and
     * never races cold-start TLS setup. */
    if (!ensure_meeting_resume_supervisor()) {
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
    app_ui_set_service_ready(true);
    const char *primary_input = device_input_primary_interaction_label();
    char ready_hint[72];
    char wake_failed_hint[72];
    snprintf(ready_hint, sizeof(ready_hint), "按%s说话 双击开会议", primary_input);
    snprintf(wake_failed_hint, sizeof(wake_failed_hint),
             "唤醒加载失败，可按%s说话", primary_input);
    app_ui_show_ready_prompt(wake_ready ? "设备已就绪" : "设备基本就绪",
                                 wake_ready ? ready_hint : wake_failed_hint);
    // Handshake metadata is parsed before the Welcome/startup surface is
    // released.  Publish a pending update notice only after that sequence has
    // completed so it cannot cover the boot artwork or interrupt greeting
    // playback.  Runtime handshakes keep their immediate notification path.
    publish_pending_update_reminder();
    return true;
}

static esp_err_t stop_gateway_poll_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_gateway_poll_task;
    if (task) s_gateway_poll_stop_requested = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* The poll uses this lane's persistent ESP client. Cancel the active
     * request before waiting, so a 30s network timeout cannot delay rollback.
     * Cellular polling remains bounded by its adapter request timeout; do not
     * touch its private handle from here. */
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_gateway_poll_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_gateway_poll_client_mutex, pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_gateway_poll_active_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "gateway poll HTTP cancel returned: %s",
                         esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_gateway_poll_client_mutex);
    }
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_gateway_poll_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_gateway_poll_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_gateway_poll_stopped);
    s_gateway_poll_stopped = NULL;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "gateway poll task stopped");
    return ESP_OK;
}

static void clock_sync_cb(struct timeval *tv) {
    if (!tv || tv->tv_sec < 1672531200) return; // 2023-01-01 UTC
    taskENTER_CRITICAL(&s_task_state_lock);
    s_display_clock_epoch = tv->tv_sec;
    s_display_clock_anchor_us = esp_timer_get_time();
    s_display_clock_valid = true;
    // An authenticated handshake can refine an earlier Wi-Fi/SNTP clock, but
    // the SNTP monitor must not interpret that valid first sync as incomplete
    // and unnecessarily restart the client. Both sources feed this callback.
    s_clock_sync_complete = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* Re-evaluate all wall-clock deadlines after either SNTP or authenticated
     * Hub time changes the epoch.  The dispatcher does this in task context. */
    wake_deadline_service_on_wall_clock_updated();
    sleep_schedule_service_on_wall_clock_updated();
    ESP_LOGI(TAG, "clock synchronized: epoch=%lld", (long long)tv->tv_sec);
}

static void start_ambient_clock_task(void) {
    setenv("TZ", "CST-8", 1);
    tzset();
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_started = s_ambient_task != NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_started) return;

    s_ambient_task_stopped = xSemaphoreCreateBinary();
    if (!s_ambient_task_stopped) {
        ESP_LOGE(TAG, "cannot allocate ambient clock completion semaphore");
        return;
    }

    // Clock cadence must remain independent of animation/render load. A higher
    // priority lets the once-per-second update preempt a slow LCD presentation.
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreate(ambient_task, "maclaw_ambient", 3072, NULL, 3, &task);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_ambient_task = created == pdPASS ? task : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        vSemaphoreDelete(s_ambient_task_stopped);
        s_ambient_task_stopped = NULL;
        ESP_LOGE(TAG, "cannot start ambient clock task");
        return;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_POWER,
        .name = "ambient_clock",
        .context = (void *)s_ambient_task,
        .stop = stop_ambient_clock_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register ambient clock lifecycle owner: %s",
                 esp_err_to_name(registry_err));
        (void)stop_ambient_clock_task(500);
    }
}

static esp_err_t stop_ambient_clock_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_ambient_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_ambient_task_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_ambient_task_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_ambient_task_stopped);
    s_ambient_task_stopped = NULL;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_POWER, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "ambient clock task stopped");
    return ESP_OK;
}

static esp_err_t stop_gateway_poll_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_gateway_poll_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_gateway_poll_task(timeout_ms);
}

static esp_err_t stop_ambient_clock_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_ambient_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_ambient_clock_task(timeout_ms);
}

static void apply_gateway_server_time(cJSON *json) {
    cJSON *server_time = json ? cJSON_GetObjectItemCaseSensitive(json, "serverTime") : NULL;
    if (!cJSON_IsNumber(server_time)) return;

    // Milliseconds are exact at current Unix epochs in cJSON's double. Reject
    // malformed or implausible values before changing the device wall clock.
    const double server_time_ms = server_time->valuedouble;
    const double minimum_time_ms = 1672531200000.0; // 2023-01-01 UTC
    const double maximum_time_ms = 4102444800000.0; // 2100-01-01 UTC
    if (server_time_ms < minimum_time_ms || server_time_ms >= maximum_time_ms) {
        ESP_LOGW(TAG, "ignored invalid gateway serverTime: %.0f", server_time_ms);
        return;
    }

    int64_t epoch_ms = (int64_t)server_time_ms;
    struct timeval tv = {
        .tv_sec = (time_t)(epoch_ms / 1000),
        .tv_usec = (suseconds_t)((epoch_ms % 1000) * 1000),
    };
    setenv("TZ", "CST-8", 1);
    tzset();
    if (settimeofday(&tv, NULL) != 0) {
        ESP_LOGW(TAG, "cannot apply gateway serverTime: errno=%d", errno);
        return;
    }
    clock_sync_cb(&tv);
    // ML307 has no ESP-NETIF route for SNTP. Start the display cadence only
    // after authenticated Hub time exists; an unpaired device remains on the
    // recovery portal and does not need a competing once-per-second LCD task.
    start_ambient_clock_task();
    ESP_LOGI(TAG, "clock source: gateway serverTime");
}

static void clock_sync_task(void *arg) {
    (void)arg;
    /* Publish the task handle and registry ownership before this monitor can
     * observe an already-synchronized clock and exit. Without this admission
     * gate, a fast SNTP completion can leave a stale registered entry. */
    if (!s_clock_sync_start_gate ||
        xSemaphoreTake(s_clock_sync_start_gate, portMAX_DELAY) != pdTRUE) {
        return;
    }
    unsigned attempt = 1;
    while (true) {
        bool stop_requested;
        taskENTER_CRITICAL(&s_task_state_lock);
        stop_requested = s_clock_sync_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested || s_clock_sync_complete) break;
        /* esp_netif_sntp_sync_wait() has no cancellation handle. Slice its
         * public timeout so Registry stop/join has the same bounded latency as
         * the rest of the Connectivity owner, while retaining the former
         * 12-second total retry window. */
        esp_err_t wait_err = ESP_ERR_TIMEOUT;
        TickType_t wait_started = xTaskGetTickCount();
        const TickType_t wait_budget = pdMS_TO_TICKS(CLOCK_SYNC_WAIT_MS);
        while ((xTaskGetTickCount() - wait_started) < wait_budget) {
            TickType_t elapsed = xTaskGetTickCount() - wait_started;
            TickType_t remaining = wait_budget - elapsed;
            TickType_t slice = pdMS_TO_TICKS(250);
            if (slice > remaining) slice = remaining;
            wait_err = esp_netif_sntp_sync_wait(slice);
            taskENTER_CRITICAL(&s_task_state_lock);
            stop_requested = s_clock_sync_stop_requested;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (stop_requested || wait_err == ESP_OK || s_clock_sync_complete) break;
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        stop_requested = s_clock_sync_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested || wait_err == ESP_OK || s_clock_sync_complete) break;

        unsigned int reachability[CONFIG_LWIP_SNTP_MAX_SERVERS] = {0};
        for (unsigned i = 0; i < CONFIG_LWIP_SNTP_MAX_SERVERS; ++i) {
            if (esp_netif_sntp_reachability(i, &reachability[i]) != ESP_OK) {
                reachability[i] = 0;
            }
        }
        ESP_LOGW(TAG,
                 "clock sync attempt %u timed out: wait=%s reachability=%02x/%02x/%02x; retrying",
                 attempt, esp_err_to_name(wait_err),
                 reachability[0], reachability[1], reachability[2]);
        esp_err_t restart_err = esp_netif_sntp_start();
        if (restart_err != ESP_OK) {
            ESP_LOGW(TAG, "SNTP restart failed: %s", esp_err_to_name(restart_err));
        }
        ++attempt;
        /* Direct notification makes the retry backoff interruptible without
         * touching esp-netif/SNTP from a foreign lifecycle owner. */
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(CLOCK_SYNC_RETRY_MS));
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_clock_sync_task == self) s_clock_sync_task = NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_clock_sync_stopped) xSemaphoreGive(s_clock_sync_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDeleteWithCaps(NULL);
}

/* Stop the retry monitor before touching esp-netif SNTP state.  The monitor
 * slices sync_wait to 250 ms, so this bounded join is also the lifetime fence
 * that lets the caller prove no task can restart or query SNTP afterwards. */
static esp_err_t stop_clock_sync_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_clock_sync_stop_requested = true;
    task = s_clock_sync_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_clock_sync_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_clock_sync_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "clock sync monitor stopped");
    return ESP_OK;
}

static esp_err_t stop_sntp_service(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    /* The retry monitor join and esp-netif teardown are one SNTP transaction.
     * `esp_netif_sntp_deinit()` itself has no caller-controlled timeout, so
     * the only bounded child must consume the parent's remaining allowance. */
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    const uint32_t monitor_timeout_ms =
        startup_rollback_remaining_timeout_ms(deadline_us);
    if (monitor_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t monitor_err = stop_clock_sync_task(monitor_timeout_ms);
    if (monitor_err != ESP_OK) return monitor_err;
    if (!s_sntp_initialized) return ESP_OK;
    /* esp_netif exposes deinit as a void API. It is safe only after the
     * monitor join above; no unrelated task accesses this singleton. */
    esp_netif_sntp_deinit();
    s_sntp_initialized = false;
    ESP_LOGI(TAG, "SNTP service deinitialized");
    return ESP_OK;
}

static esp_err_t stop_clock_sync_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_clock_sync_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* Registry entries identify the created task, never the mutable global
     * that will later be cleared and reused by a new clock-sync monitor. */
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_clock_sync_task(timeout_ms);
}

static void start_clock_sync(void) {
    start_ambient_clock_task();
    if (s_sntp_initialized) {
        ESP_LOGI(TAG, "SNTP service already initialized");
        return;
    }
    esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
        3, ESP_SNTP_SERVER_LIST("ntp.aliyun.com", "time.cloudflare.com", "pool.ntp.org"));
    config.sync_cb = clock_sync_cb;
    esp_err_t err = esp_netif_sntp_init(&config);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "SNTP init failed: %s", esp_err_to_name(err));
    } else {
        s_sntp_initialized = true;
    }
    if (err == ESP_OK && !s_clock_sync_task) {
        // This monitor mostly sleeps; keep its stack in PSRAM so clock recovery
        // cannot take the last internal block needed by Wi-Fi/TLS or ESP-SR.
        taskENTER_CRITICAL(&s_task_state_lock);
        s_clock_sync_stop_requested = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        while (xSemaphoreTake(s_clock_sync_stopped, 0) == pdTRUE) {}
        BaseType_t created = xTaskCreateWithCaps(clock_sync_task, "maclaw_clock_sync",
                                                 3072, NULL, 3,
                                                 &s_clock_sync_task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (created != pdPASS) {
            s_clock_sync_task = NULL;
            ESP_LOGE(TAG, "cannot start clock sync monitor task");
        } else {
            esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
                .struct_size = sizeof(task_registry_entry_t),
                .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
                .name = "clock_sync",
                .context = (void *)s_clock_sync_task,
                .stop = stop_clock_sync_registry_entry,
            });
            if (registry_err != ESP_OK) {
                ESP_LOGE(TAG, "cannot register clock sync monitor: %s",
                         esp_err_to_name(registry_err));
                xSemaphoreGive(s_clock_sync_start_gate);
                (void)stop_clock_sync_task(500);
            } else {
                xSemaphoreGive(s_clock_sync_start_gate);
            }
        }
    }
}

static void save_ambient_weather(void) {
    weather_cache_snapshot_t snapshot = {
        .temperature_c = s_weather_temperature_c,
        .expires_at_ms = s_weather_expires_at_ms,
        .valid = s_weather_valid,
    };
    strlcpy(snapshot.summary, s_weather_summary, sizeof(snapshot.summary));
    strlcpy(snapshot.location, s_weather_location, sizeof(snapshot.location));
    esp_err_t err = weather_cache_service_save(&snapshot);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "weather cache save deferred: %s", esp_err_to_name(err));
    }
}

static void wake_restart_task(void *arg) {
    (void)arg;
    /* schedule_wake_restart() registers this one-shot worker before releasing
     * the gate.  A recognizer that is already torn down can otherwise make a
     * very fast worker exit between xTaskCreateWithCaps() and registration. */
    if (!s_wake_restart_start_gate ||
        xSemaphoreTake(s_wake_restart_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        if (s_wake_restart_task == self) {
            s_wake_restart_task = NULL;
            s_wake_restart_scheduled = false;
        }
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_wake_restart_stopped) xSemaphoreGive(s_wake_restart_stopped);
        (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_AUDIO,
                                                     (void *)self, 10);
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
        bool foreground_active = s_interaction_task != NULL || s_foreground_http_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (foreground_active || meeting_is_active()) {
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
        if (s_startup_pet_asset_task) {
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
        discard_gateway_asset_http_client();
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
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_wake_restart_task == self) {
        s_wake_restart_task = NULL;
        s_wake_restart_scheduled = false;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_wake_restart_stopped) xSemaphoreGive(s_wake_restart_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_AUDIO,
                                                 (void *)self, 10);
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
    if (!task) return ESP_OK;
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
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_AUDIO, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
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

static void schedule_wake_restart(void) {
    if (device_connectivity_is_provisioning_active() || !s_startup_sequence_complete) return;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_wake_restart_admission_open;
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

static void load_ambient_weather(void) {
    weather_cache_snapshot_t snapshot;
    esp_err_t err = weather_cache_service_load(&snapshot);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "weather cache unavailable: %s", esp_err_to_name(err));
        return;
    }
    strlcpy(s_weather_summary, snapshot.summary, sizeof(s_weather_summary));
    strlcpy(s_weather_location, snapshot.location, sizeof(s_weather_location));
    s_weather_temperature_c = snapshot.temperature_c;
    s_weather_expires_at_ms = snapshot.expires_at_ms;
    s_weather_valid = snapshot.valid;
}

static bool is_valid_gateway_url(const char *url) {
    if (!url || !url[0] || strlen(url) >= URL_CAPACITY) return false;
    const char *host = NULL;
    if (!strncmp(url, "https://", 8)) host = url + 8;
    else if (!strncmp(url, "http://", 7)) host = url + 7;
    else return false;
    return host[0] != '\0' && host[0] != '/' && !strchr(host, ' ');
}

static bool is_six_digit_pair_code(const char *code) {
    if (!code || strlen(code) != 6) return false;
    for (size_t i = 0; i < 6; ++i) {
        if (code[i] < '0' || code[i] > '9') return false;
    }
    return true;
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
    esp_err_t err = configuration_service_load(snapshot);
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
    strlcpy(s_gateway_url, snapshot->gateway_url, sizeof(s_gateway_url));
    strlcpy(s_pair_code, snapshot->pair_code, sizeof(s_pair_code));
    strlcpy(s_gateway_token, snapshot->gateway_token, sizeof(s_gateway_token));
    s_configured_output_volume = snapshot->output_volume;
    s_configured_output_volume_saved = snapshot->output_volume_saved;
    heap_caps_free(snapshot);
    return ESP_OK;
}

static esp_err_t save_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    esp_err_t err = configuration_service_set_output_volume((uint8_t)percent);
    if (err == ESP_OK) {
        s_configured_output_volume = percent;
        s_configured_output_volume_saved = true;
    }
    return err;
}

static esp_err_t save_display_brightness(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    return persistence_service_write_u8(DISPLAY_BRIGHTNESS_NVS_NAMESPACE,
                                        DISPLAY_BRIGHTNESS_NVS_KEY,
                                        (uint8_t)percent);
}

static bool valid_screen_sleep_seconds(int seconds) {
    switch (seconds) {
        case 0: case 60: case 180: case 300: case 600: case 1800:
        case 3600: case 7200: case 10800: case 14400: case 18000:
            return true;
        default:
            return false;
    }
}

static esp_err_t save_screen_sleep_seconds(unsigned seconds) {
	uint32_t value = seconds;
	return persistence_service_write_blob(DISPLAY_BRIGHTNESS_NVS_NAMESPACE,
								  DISPLAY_SLEEP_NVS_KEY, &value, sizeof(value));
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
        output_volume_persist_reply_t reply = {
            .result = request.brightness
                          ? save_display_brightness(request.percent)
                          : save_output_volume(request.percent),
            .generation = request.generation,
        };
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
    if (s_output_volume_persist_task_handle == self) {
        s_output_volume_persist_task_handle = NULL;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_output_volume_persist_stopped) xSemaphoreGive(s_output_volume_persist_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_STORAGE,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
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
    TaskHandle_t task = s_output_volume_persist_task_handle;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
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
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_STORAGE, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
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
// from internal RAM, but the gateway poller does not, so use one shared
// internal-stack persistence worker for both call paths.
static esp_err_t persist_hardware_level(unsigned percent, bool brightness) {
    if (percent > 100 || !s_output_volume_persist_queue ||
        !s_output_volume_persist_reply_queue || !s_output_volume_persist_request_mutex) {
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool stop_requested = s_output_volume_persist_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (stop_requested) return ESP_ERR_INVALID_STATE;
    if (xSemaphoreTake(s_output_volume_persist_request_mutex,
                       pdMS_TO_TICKS(4000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    stop_requested = s_output_volume_persist_stop_requested;
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
    return persist_hardware_level(percent, false);
}

static esp_err_t persist_display_brightness(unsigned percent) {
    return persist_hardware_level(percent, true);
}

static void finish_result_speech_transaction(const char *reply_to) {
    if (!reply_to || !reply_to[0]) return;
    unsigned missing = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 &&
        !strcmp(s_result_speech_reply_to, reply_to)) {
        missing = s_result_speech_parts_remaining;
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGW(TAG, "result speech transaction closed early: replyTo=%s missing=%u",
             reply_to, missing);
}

// Closing the visible result is also an explicit choice to leave that command
// behind.  Do not let a delayed TTS part from the same reply pull audio back
// into the ambient screen minutes later.  Clearing the exact correlation makes
// such queued parts ordinary orphaned command output, which the poller safely
// acknowledges without playback.
static void dismiss_result_speech_transaction(void) {
    unsigned missing = 0;
    char reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_result_speech_parts_remaining > 0 && s_result_speech_reply_to[0]) {
        missing = s_result_speech_parts_remaining;
        strlcpy(reply_to, s_result_speech_reply_to, sizeof(reply_to));
        s_result_speech_reply_to[0] = '\0';
        s_result_speech_parts_remaining = 0;
        s_result_speech_deadline_us = 0;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (missing) {
        ESP_LOGI(TAG, "result speech dismissed with response: replyTo=%s skipped=%u",
                 reply_to, missing);
    }
}

static bool is_enterprise_wifi(void) {
    return !strcmp(s_wifi_security, "enterprise");
}

static bool is_valid_choice(const char *value, const char *first, const char *second,
                            const char *third) {
    return value && (!strcmp(value, first) || (second && !strcmp(value, second)) ||
                     (third && !strcmp(value, third)));
}

static esp_err_t save_device_config(const char *ssid, const char *password, const char *gateway_url,
                                    const char *pair_code, const char *security,
                                    const char *eap_method, const char *identity,
                                    const char *username, const char *ttls_phase2,
                                    const char *ca_mode, const char *server_domain) {
    bool enterprise = security && !strcmp(security, "enterprise");
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN ||
        strlen(password) >= sizeof(s_wifi_password) || !is_valid_gateway_url(gateway_url) ||
        !is_six_digit_pair_code(pair_code) ||
        !is_valid_choice(security, "personal", "enterprise", NULL) ||
        (enterprise && (!is_valid_choice(eap_method, "peap", "ttls", NULL) || !username || !username[0] ||
                        strlen(username) >= sizeof(s_wifi_username) || strlen(identity) >= sizeof(s_wifi_identity) ||
                        !is_valid_choice(ttls_phase2, "mschapv2", "pap", NULL) ||
                        !is_valid_choice(ca_mode, "system", "none", NULL) ||
                        strlen(server_domain) >= sizeof(s_wifi_server_domain)))) return ESP_ERR_INVALID_ARG;
    /* 快照随 v3 多热点列表增长到 ~1.6 KB。本函数运行在 httpd 任务（6 KB
     * 栈）上，工作副本必须放 PSRAM，避免重演保存途中栈溢出重启。 */
    configuration_snapshot_t *snapshot =
        heap_caps_calloc(1, sizeof(*snapshot), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!snapshot) return ESP_ERR_NO_MEM;
    snapshot->output_volume = (uint8_t)s_configured_output_volume;
    snapshot->output_volume_saved = s_configured_output_volume_saved;
    strlcpy(snapshot->wifi_ssid, ssid, sizeof(snapshot->wifi_ssid));
    strlcpy(snapshot->wifi_password, password, sizeof(snapshot->wifi_password));
    strlcpy(snapshot->wifi_security, enterprise ? "enterprise" : "personal",
            sizeof(snapshot->wifi_security));
    strlcpy(snapshot->wifi_eap_method, enterprise ? eap_method : "peap",
            sizeof(snapshot->wifi_eap_method));
    strlcpy(snapshot->wifi_identity, enterprise ? identity : "", sizeof(snapshot->wifi_identity));
    strlcpy(snapshot->wifi_username, enterprise ? username : "", sizeof(snapshot->wifi_username));
    strlcpy(snapshot->wifi_ttls_phase2, enterprise ? ttls_phase2 : "mschapv2",
            sizeof(snapshot->wifi_ttls_phase2));
    strlcpy(snapshot->wifi_ca_mode, enterprise ? ca_mode : "system", sizeof(snapshot->wifi_ca_mode));
    strlcpy(snapshot->wifi_server_domain, enterprise ? server_domain : "",
            sizeof(snapshot->wifi_server_domain));
    strlcpy(snapshot->gateway_url, gateway_url, sizeof(snapshot->gateway_url));
    strlcpy(snapshot->pair_code, pair_code, sizeof(snapshot->pair_code));
    /* New network/pairing data atomically invalidates an old Hub credential. */
    snapshot->gateway_token[0] = '\0';
    esp_err_t err = configuration_service_save_provisioning(snapshot);
    heap_caps_free(snapshot);
    if (err == ESP_OK) {
        strlcpy(s_wifi_ssid, ssid, sizeof(s_wifi_ssid));
        strlcpy(s_wifi_password, password, sizeof(s_wifi_password));
        strlcpy(s_wifi_security, enterprise ? "enterprise" : "personal", sizeof(s_wifi_security));
        strlcpy(s_wifi_eap_method, enterprise ? eap_method : "peap", sizeof(s_wifi_eap_method));
        strlcpy(s_wifi_identity, enterprise ? identity : "", sizeof(s_wifi_identity));
        strlcpy(s_wifi_username, enterprise ? username : "", sizeof(s_wifi_username));
        strlcpy(s_wifi_ttls_phase2, enterprise ? ttls_phase2 : "mschapv2", sizeof(s_wifi_ttls_phase2));
        strlcpy(s_wifi_ca_mode, enterprise ? ca_mode : "system", sizeof(s_wifi_ca_mode));
        strlcpy(s_wifi_server_domain, enterprise ? server_domain : "", sizeof(s_wifi_server_domain));
        strlcpy(s_gateway_url, gateway_url, sizeof(s_gateway_url));
        strlcpy(s_pair_code, pair_code, sizeof(s_pair_code));
        /* The durable transaction cleared the former Hub credential.  Match
         * the runtime authority before the portal tears down/restarts so no
         * concurrent retry can issue one more authenticated request with an
         * owner token that no longer belongs to the new network/pairing flow. */
        s_gateway_token[0] = '\0';
        /* Configuration Service makes personal primary credentials and their
         * multi-network recovery entry durable in the same provisioning
         * commit. Reload only to refresh this process cache; a refresh failure
         * cannot weaken the credential/catalogue transaction. */
        if (!enterprise) {
            esp_err_t list_err = configuration_service_list_wifi_networks(
                s_wifi_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY,
                &s_wifi_network_count);
            if (list_err != ESP_OK) {
                ESP_LOGW(TAG, "could not refresh committed saved Wi-Fi list: %s",
                         esp_err_to_name(list_err));
            }
        }
    }
    ESP_LOGI(TAG, "config save: ssid_len=%u security=%s gateway_len=%u code_len=%u result=%s",
             (unsigned)strlen(ssid), security, (unsigned)strlen(gateway_url),
             (unsigned)strlen(pair_code), esp_err_to_name(err));
    return err;
}

static esp_err_t save_pairing_code_only(const char *pair_code) {
    if (!is_six_digit_pair_code(pair_code)) return ESP_ERR_INVALID_ARG;
    esp_err_t err = configuration_service_set_pairing_code(pair_code);
    if (err == ESP_OK) strlcpy(s_pair_code, pair_code, sizeof(s_pair_code));
    return err;
}

static void load_gateway_token(void) {
    /* Token is now loaded with the atomic configuration snapshot. Kept as a
     * compatibility call-site seam while Gateway startup is migrated. */
}

static bool is_valid_setup_selected_ssid(const char *ssid) {
    if (!ssid || !ssid[0] || strlen(ssid) > WIFI_SSID_MAX_LEN) return false;
    for (const unsigned char *p = (const unsigned char *)ssid; *p; ++p) {
        // SSIDs may contain UTF-8, but controls can alter form/log parsing and
        // are not present in the visible scan list presented to the user.
        if (*p < 0x20 || *p == 0x7f) return false;
    }
    return true;
}

static void load_device_id(void) {
    // Always derive the physical identity from the chip MAC. Reading an NVS
    // copy first makes cloned factory NVS partitions duplicate client IDs across
    // devices, which defeats independent tokens. Keep a best-effort copy only
    // for diagnostics and future migrations.
    uint8_t mac[6] = {0};
    if (esp_read_mac(mac, ESP_MAC_WIFI_STA) == ESP_OK) {
        snprintf(s_device_id, sizeof(s_device_id), "esp32s3-%02x%02x%02x%02x%02x%02x",
                 mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]);
    }
    if (!s_device_id[0]) snprintf(s_device_id, sizeof(s_device_id), "%s", CONFIG_MACLAW_CLIENT_ID);
}

static bool meeting_storage_partition_is_blank(void) {
    const esp_partition_t *partition = esp_partition_find_first(
        ESP_PARTITION_TYPE_DATA, ESP_PARTITION_SUBTYPE_DATA_SPIFFS, "storage");
    if (!partition || partition->size == 0) return false;

    // Prove that the complete partition is factory-erased before allowing an
    // automatic format. Sampling only its first sector is unsafe: after wear
    // leveling or interrupted metadata updates that sector can be blank while
    // later SPIFFS blocks still contain recoverable meeting audio.
    uint8_t sample[1024];
    for (size_t offset = 0; offset < partition->size; offset += sizeof(sample)) {
        size_t count = partition->size - offset;
        if (count > sizeof(sample)) count = sizeof(sample);
        if (esp_partition_read(partition, offset, sample, count) != ESP_OK) {
            return false;
        }
        for (size_t i = 0; i < count; ++i) {
            if (sample[i] != 0xff) return false;
        }
    }
    return true;
}

/* 启动存储清单：逐文件记录 /storage 占用，最后一行给总用量/剩余。
 * 小容量分区（如 waveshare 的 3MB）容量门禁失败时，靠这段日志直接定位
 * 空间去向。长期保留。这里运行在主任务内置栈上，VFS 遍历是安全的。 */
static void log_storage_inventory(void) {
    DIR *dir = opendir("/storage");
    if (dir) {
        struct dirent *entry;
        while ((entry = readdir(dir)) != NULL) {
            if (entry->d_name[0] == '.') continue;
            // d_name 按 POSIX 上限 255 字节声明，缓冲留足避免截断告警。
            char path[9 + 256];
            snprintf(path, sizeof(path), "/storage/%s", entry->d_name);
            struct stat info;
            if (stat(path, &info) == 0) {
                ESP_LOGI(TAG, "storage file: %s size=%ld",
                         entry->d_name, (long)info.st_size);
            } else {
                ESP_LOGW(TAG, "storage file: %s stat failed errno=%d",
                         entry->d_name, errno);
            }
        }
        closedir(dir);
    } else {
        ESP_LOGW(TAG, "storage inventory: opendir failed errno=%d", errno);
    }
    size_t total = 0;
    size_t used = 0;
    if (esp_spiffs_info("storage", &total, &used) == ESP_OK && used <= total) {
        ESP_LOGI(TAG, "storage usage: total=%u used=%u free=%u",
                 (unsigned)total, (unsigned)used, (unsigned)(total - used));
    }
}

static esp_err_t mount_meeting_storage(void) {
    esp_vfs_spiffs_conf_t config = {
        .base_path = "/storage",
        .partition_label = "storage",
        // The pet cache keeps one metadata file plus up to eight animation
        // frames open over its save/load lifetime. Four descriptors was enough
        // for meeting audio, but it makes fopen() fail partway through a full
        // eight-frame pet update while the HTTP/audio tasks also hold files.
        .max_files = 16,
        .format_if_mount_failed = false,
    };
    esp_err_t err = esp_vfs_spiffs_register(&config);
    if (err != ESP_OK && meeting_storage_partition_is_blank()) {
        // Production flashing preserves the recording partition. Initialize a
        // genuinely factory-blank device once, but never use mount failure by
        // itself as permission to erase potentially recoverable recordings.
        ESP_LOGW(TAG, "blank meeting storage detected; formatting once");
        config.format_if_mount_failed = true;
        err = esp_vfs_spiffs_register(&config);
    }
    if (err == ESP_OK || err == ESP_ERR_INVALID_STATE) {
        s_storage_mounted = true;
        size_t total = 0;
        size_t used = 0;
        if (esp_spiffs_info("storage", &total, &used) == ESP_OK) {
            ESP_LOGI(TAG, "meeting storage mounted: total=%u used=%u",
                     (unsigned)total, (unsigned)used);
        }
        log_storage_inventory();
        return ESP_OK;
    }
    ESP_LOGE(TAG, "meeting storage mount failed; preserving existing contents: %s",
             esp_err_to_name(err));
    return err;
}

static void load_meeting_recovery(void) {
    s_meeting_pending = false;
    s_meeting_next_chunk = 0;
    s_meeting_phase = 0;
    s_meeting_recording_id[0] = '\0';
    meeting_recovery_snapshot_t snapshot;
    esp_err_t load_err = meeting_recovery_service_load(&snapshot);
    if (load_err != ESP_OK) {
        ESP_LOGW(TAG, "meeting recovery metadata unavailable: %s",
                 esp_err_to_name(load_err));
        return;
    }
    s_meeting_next_chunk = snapshot.next_chunk;
    s_meeting_phase = snapshot.phase;
    strlcpy(s_meeting_recording_id, snapshot.recording_id,
            sizeof(s_meeting_recording_id));
    struct stat info;
    s_meeting_pending = snapshot.pending && s_storage_mounted &&
                        stat(MEETING_WAV_PATH, &info) == 0 && info.st_size > 44;
    if (!s_meeting_pending) {
        s_meeting_recording_id[0] = '\0';
        s_meeting_next_chunk = 0;
        s_meeting_phase = 0;
    }
}

static esp_err_t save_meeting_recovery(bool pending, const char *recording_id,
                                       int32_t next_chunk, int32_t phase) {
    meeting_recovery_snapshot_t snapshot = {
        .pending = pending,
        .next_chunk = next_chunk,
        .phase = phase,
    };
    strlcpy(snapshot.recording_id, recording_id ? recording_id : "",
            sizeof(snapshot.recording_id));
    esp_err_t err = meeting_recovery_service_save(&snapshot);
    if (err == ESP_OK) {
        s_meeting_pending = pending;
        s_meeting_next_chunk = next_chunk;
        s_meeting_phase = phase;
        strlcpy(s_meeting_recording_id, recording_id ? recording_id : "",
                sizeof(s_meeting_recording_id));
    }
    return err;
}

static esp_err_t clear_meeting_recovery(bool delete_audio) {
    esp_err_t err = save_meeting_recovery(false, "", 0, 0);
    if (delete_audio && unlink(MEETING_WAV_PATH) != 0 && errno != ENOENT && err == ESP_OK) {
        err = ESP_FAIL;
    }
    return err;
}
static esp_err_t save_gateway_token(const char *token) {
    if (!token || !token[0] || strlen(token) >= sizeof(s_gateway_token)) return ESP_ERR_INVALID_SIZE;
    esp_err_t err = configuration_service_set_gateway_token(token, true);
    if (err == ESP_OK) strlcpy(s_gateway_token, token, sizeof(s_gateway_token));
    return err;
}

static esp_err_t gateway_handshake(bool cold_start) {
	char boot_field[64] = {0};
    http_response_t response = {0};
    if (cold_start) {
        s_handshake_startup_welcome_queued = false;
        snprintf(boot_field, sizeof(boot_field), "\"bootSessionId\":\"%s\",", s_boot_session_id);
    }
    // The screen renderer keeps several DMA buffers in internal RAM. Asking
    // Hub for embedded RGB565+A8 pet frames forces a 100+ KiB response and starves
    // the TLS allocation on this device. The built-in pet stays visible, while
    // the small handshake response still delivers city/weather immediately.
    firmware_identity_info_t identity = {0};
    if (firmware_identity_get(&identity) != ESP_OK) return ESP_ERR_INVALID_STATE;
    cJSON *request_json = cJSON_CreateObject();
    if (!request_json) return ESP_ERR_NO_MEM;
    cJSON_AddStringToObject(request_json, "clientId", s_device_id);
    cJSON_AddStringToObject(request_json, "clientName", "ESP32-S3 Pet");
    if (cold_start) cJSON_AddStringToObject(request_json, "bootSessionId", s_boot_session_id);
    cJSON_AddStringToObject(request_json, "protocolVersion", "1.1");
    cJSON *capabilities = cJSON_AddObjectToObject(request_json, "clientCapabilities");
    cJSON *input = capabilities ? cJSON_AddObjectToObject(capabilities, "input") : NULL;
    cJSON *input_modalities = input ? cJSON_AddArrayToObject(input, "modalities") : NULL;
    if (!input_modalities || !cJSON_AddItemToArray(input_modalities, cJSON_CreateString("text")) ||
        !cJSON_AddItemToArray(input_modalities, cJSON_CreateString("audio"))) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *input_audio = cJSON_AddObjectToObject(input, "audio");
    cJSON_AddItemToObject(input_audio, "mimeTypes", cJSON_CreateStringArray((const char *[]){"audio/wav"}, 1));
    cJSON_AddItemToObject(input_audio, "sampleRates", cJSON_CreateIntArray((const int[]){16000}, 1));
    cJSON_AddNumberToObject(input_audio, "channels", 1);
    cJSON *output = cJSON_AddObjectToObject(capabilities, "output");
    cJSON_AddItemToObject(output, "modalities", cJSON_CreateStringArray((const char *[]){"text", "audio", "image"}, 3));
    cJSON_AddItemToObject(output, "preferred", cJSON_CreateStringArray((const char *[]){"audio", "image", "text"}, 3));
    cJSON *combinations = cJSON_AddArrayToObject(output, "combinations");
    cJSON_AddItemToArray(combinations, cJSON_CreateStringArray((const char *[]){"text"}, 1));
    cJSON_AddItemToArray(combinations, cJSON_CreateStringArray((const char *[]){"audio", "text"}, 2));
    cJSON_AddItemToArray(combinations, cJSON_CreateStringArray((const char *[]){"image"}, 1));
    cJSON *output_text = cJSON_AddObjectToObject(output, "text");
    cJSON_AddNumberToObject(output_text, "maxChars", 240);
    cJSON_AddBoolToObject(output_text, "markdown", false);
    cJSON_AddStringToObject(output_text, "locale", "zh-CN");
    cJSON *output_audio = cJSON_AddObjectToObject(output, "audio");
    cJSON_AddItemToObject(output_audio, "mimeTypes", cJSON_CreateStringArray((const char *[]){"audio/wav", "audio/mpeg", "audio/mp3"}, 3));
    cJSON_AddItemToObject(output_audio, "sampleRates", cJSON_CreateIntArray((const int[]){16000, 22050, 24000, 32000, 44100, 48000}, 6));
    cJSON_AddNumberToObject(output_audio, "channels", 2);
    cJSON_AddBoolToObject(output_audio, "playback", true);
    cJSON_AddItemToObject(output_audio, "deliveryModes", cJSON_CreateStringArray((const char *[]){"inline", "url"}, 2));
    cJSON_AddNumberToObject(output_audio, "maxInlineBytes", 8192);
    cJSON_AddNumberToObject(output_audio, "maxDownloadBytes", 524288);
    cJSON *output_image = cJSON_AddObjectToObject(output, "image");
    cJSON_AddItemToObject(output_image, "mimeTypes", cJSON_CreateStringArray((const char *[]){RESPONSE_IMAGE_MIME}, 1));
    cJSON_AddNumberToObject(output_image, "maxWidth", 64);
    cJSON_AddNumberToObject(output_image, "maxHeight", 64);
    cJSON_AddBoolToObject(output_image, "animated", false);
    cJSON *features = cJSON_AddObjectToObject(capabilities, "features");
    cJSON_AddBoolToObject(features, "petStates", true);
    cJSON_AddBoolToObject(features, "petAnimation", true);
    cJSON_AddBoolToObject(features, "petAsset", true);
    cJSON_AddNumberToObject(features, "petAssetMaxFrames", 8);
    cJSON_AddBoolToObject(features, "ambientDisplay", true);
    cJSON_AddBoolToObject(features, "meetingRecorder", true);
    cJSON_AddBoolToObject(features, "volumeControl", true);
    cJSON_AddBoolToObject(features, "brightnessControl", true);
    cJSON_AddBoolToObject(features, "screenSleepControl", true);
    cJSON *legacy_capabilities = cJSON_AddObjectToObject(request_json, "capabilities");
    cJSON *firmware = legacy_capabilities ? cJSON_AddObjectToObject(legacy_capabilities, "firmwareIdentity") : NULL;
    if (!firmware) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON_AddStringToObject(firmware, "deviceId", s_device_id);
    cJSON_AddStringToObject(firmware, "productId", identity.product_id);
    cJSON_AddStringToObject(firmware, "boardId", identity.board_id);
    cJSON_AddStringToObject(firmware, "hardwareRev", identity.hardware_rev);
    cJSON_AddStringToObject(firmware, "layoutId", identity.layout_id);
    cJSON_AddStringToObject(firmware, "compatibilityId", identity.compatibility_id);
    cJSON_AddNumberToObject(firmware, "releaseSequence", (double)identity.release_sequence);
    cJSON_AddStringToObject(firmware, "appVersion", identity.app_version);
    cJSON_AddStringToObject(firmware, "elfSha256", identity.elf_sha256);
    cJSON *profile = cJSON_AddObjectToObject(firmware, "deviceProfile");
    if (!profile ||
        !cJSON_AddNumberToObject(profile, "abiVersion", identity.profile.abi_version) ||
        !cJSON_AddStringToObject(profile, "id", identity.profile.id) ||
        !cJSON_AddNumberToObject(profile, "displayWidth", identity.profile.display_width) ||
        !cJSON_AddNumberToObject(profile, "displayHeight", identity.profile.display_height) ||
        !cJSON_AddNumberToObject(profile, "capabilities", identity.profile.capabilities) ||
        !cJSON_AddNumberToObject(profile, "primaryInteractionSource",
                                 identity.profile.primary_interaction_source)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *power = cJSON_AddObjectToObject(firmware, "power");
    if (!power ||
        !cJSON_AddBoolToObject(power, "available", identity.power_available) ||
        !cJSON_AddNumberToObject(power, "state", identity.power.state) ||
        !cJSON_AddBoolToObject(power, "displayOffArmed",
                                identity.power.display_off_armed) ||
        !cJSON_AddBoolToObject(power, "telemetryAvailable",
                                identity.power_telemetry_available) ||
        !cJSON_AddNumberToObject(power, "batteryLevelPercent",
                                 identity.power_telemetry_available
                                     ? identity.power_telemetry.level_percent
                                     : -1) ||
        !cJSON_AddBoolToObject(power, "charging",
                                identity.power_telemetry_available &&
                                    identity.power_telemetry.charging)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *battery = cJSON_AddObjectToObject(firmware, "batteryPolicy");
    if (!battery ||
        !cJSON_AddBoolToObject(battery, "available", identity.battery_policy_available) ||
        !cJSON_AddBoolToObject(battery, "telemetryAvailable",
                                identity.battery_policy.telemetry_available) ||
        !cJSON_AddNumberToObject(battery, "level", identity.battery_policy.level) ||
        !cJSON_AddBoolToObject(battery, "optionalWorkAllowed",
                                identity.battery_policy.optional_work_allowed) ||
        !cJSON_AddBoolToObject(battery, "highPowerWorkAllowed",
                                identity.battery_policy.high_power_work_allowed)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *connectivity = cJSON_AddObjectToObject(firmware, "connectivity");
    if (!connectivity ||
        !cJSON_AddNumberToObject(connectivity, "activeUplink",
                                 identity.connectivity.active_uplink) ||
        !cJSON_AddBoolToObject(connectivity, "wifiReady",
                               identity.connectivity.wifi_ready) ||
        !cJSON_AddBoolToObject(connectivity, "cellularReady",
                               identity.connectivity.cellular_ready) ||
        !cJSON_AddBoolToObject(connectivity, "ready", identity.connectivity.ready)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    cJSON *tools = cJSON_AddArrayToObject(request_json, "tools");
    if (!device_tool_registry_append_descriptors(tools)) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    /* The descriptor grows with the common tool registry.  Keep it out of
     * maclaw_gateway_startup's stack: even an 8 KiB automatic buffer trips the
     * FreeRTOS stack canary before TLS gets a chance to run. */
    char *payload = heap_caps_malloc(HANDSHAKE_REQUEST_CAPACITY,
                                     MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!payload) payload = heap_caps_malloc(HANDSHAKE_REQUEST_CAPACITY, MALLOC_CAP_8BIT);
    if (!payload) {
        cJSON_Delete(request_json);
        return ESP_ERR_NO_MEM;
    }
    bool printed = cJSON_PrintPreallocated(request_json, payload,
                                            HANDSHAKE_REQUEST_CAPACITY, false);
    cJSON_Delete(request_json);
    if (!printed) {
        ESP_LOGE(TAG, "gateway handshake descriptor exceeds capacity=%u",
                 (unsigned)HANDSHAKE_REQUEST_CAPACITY);
        heap_caps_free(payload);
        return ESP_ERR_INVALID_SIZE;
    }
    int request_len = strlen(payload);
    if (request_len <= 0 || request_len >= HANDSHAKE_REQUEST_CAPACITY) {
        ESP_LOGE(TAG, "gateway handshake descriptor too large: bytes=%d capacity=%u",
                 request_len, (unsigned)HANDSHAKE_REQUEST_CAPACITY);
        heap_caps_free(payload);
        return ESP_ERR_INVALID_SIZE;
    }
    log_heap_snapshot("handshake-before");
    esp_err_t err = request_with_capacity("POST", "/api/im-gateway/v1/handshake", "application/json",
                                          payload, (size_t)request_len, HANDSHAKE_RESPONSE_CAPACITY, &response);
    heap_caps_free(payload);
    if (err != ESP_OK || response.status != 200) {
        ESP_LOGE(TAG, "gateway handshake failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data);
        log_heap_snapshot("handshake-fail");
        esp_err_t result = gateway_auth_failed(&response, err) ? ESP_ERR_INVALID_STATE
                           : err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    cJSON *ok = json ? cJSON_GetObjectItemCaseSensitive(json, "ok") : NULL;
    if (!cJSON_IsTrue(ok)) {
        cJSON_Delete(json);
        response_release(&response);
        return ESP_ERR_INVALID_RESPONSE;
    }
    apply_gateway_server_time(json);
    cJSON *startup_welcome = cJSON_GetObjectItemCaseSensitive(json, "startupWelcomeQueued");
    bool startup_welcome_queued = cJSON_IsTrue(startup_welcome);
    if (cold_start) {
        s_handshake_startup_welcome_queued = startup_welcome_queued;
        ESP_LOGI(TAG, "gateway startup Welcome queued: %s",
                 s_handshake_startup_welcome_queued ? "yes" : "no");
    } else if (startup_welcome_queued) {
        // Runtime capability refreshes deliberately omit bootSessionId. Treat
        // an unexpected legacy response as informational; it must never re-arm
        // or otherwise mutate the completed cold-start Welcome transaction.
        ESP_LOGW(TAG, "runtime handshake ignored unexpected startup Welcome flag");
    }
    cJSON *accepted = cJSON_GetObjectItemCaseSensitive(json, "capabilitiesAccepted");
    cJSON *accepted_output = accepted ? cJSON_GetObjectItemCaseSensitive(accepted, "output") : NULL;
    cJSON *accepted_modalities = accepted_output ? cJSON_GetObjectItemCaseSensitive(accepted_output, "modalities") : NULL;
    bool accepted_text = false;
    cJSON *accepted_modality = NULL;
    cJSON_ArrayForEach(accepted_modality, accepted_modalities) {
        if (cJSON_IsString(accepted_modality) && strcmp(accepted_modality->valuestring, "text") == 0) {
            accepted_text = true;
            break;
        }
    }
    if (accepted) {
        ESP_LOGI(TAG, "client capabilities accepted: output=%s+audio maxChars=240",
                 accepted_text ? "text" : "unsupported");
    } else {
        ESP_LOGW(TAG, "gateway did not acknowledge client capabilities (legacy Hub?)");
    }
    cJSON *meeting = cJSON_GetObjectItemCaseSensitive(json, "meetingRecording");
    s_meeting_available = cJSON_IsObject(meeting);
    if (s_meeting_available) {
        const char *base_path = json_string(meeting, "basePath");
        int chunk_size = 0;
        if (base_path && strlen(base_path) < sizeof(s_meeting_base_path)) {
            strlcpy(s_meeting_base_path, base_path, sizeof(s_meeting_base_path));
        }
        if (json_number(meeting, "chunkSize", &chunk_size) &&
            chunk_size >= (int)MEETING_MIN_CHUNK_SIZE &&
            chunk_size <= (int)MEETING_MAX_CHUNK_SIZE) {
            s_meeting_chunk_size = (size_t)chunk_size;
        }
        cJSON *modes = cJSON_GetObjectItemCaseSensitive(meeting, "modes");
        cJSON *minutes = modes ? cJSON_GetObjectItemCaseSensitive(modes, "minutes") : NULL;
        cJSON *transcript = modes ? cJSON_GetObjectItemCaseSensitive(modes, "transcript") : NULL;
        strlcpy(s_meeting_process_mode,
                cJSON_IsTrue(minutes) ? "minutes" : cJSON_IsTrue(transcript) ? "transcript" : "keep",
                sizeof(s_meeting_process_mode));
        ESP_LOGI(TAG, "meeting recording accepted: base=%s chunk=%u mode=%s",
                 s_meeting_base_path, (unsigned)s_meeting_chunk_size, s_meeting_process_mode);
    } else {
        ESP_LOGW(TAG, "Hub does not advertise meeting recording support");
    }
    cJSON *pet_profile = cJSON_GetObjectItemCaseSensitive(json, "pet");
    const char *skin = pet_profile ? json_string(pet_profile, "skin") : NULL;
    cJSON *motion = pet_profile ? cJSON_GetObjectItemCaseSensitive(pet_profile, "motionEnabled") : NULL;
    if (skin) app_ui_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
    cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(json, "petAsset");
    if (cold_start) {
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
                                      parse_pet_asset_ref(pet_asset, s_startup_pet_asset_ref);
        strlcpy(s_startup_pet_asset_skin, skin ? skin : "",
                sizeof(s_startup_pet_asset_skin));
        if (cJSON_IsObject(pet_asset) && !s_startup_pet_asset_present) {
            ESP_LOGW(TAG, "startup pet asset descriptor is invalid; cached asset will be cleared after wake readiness");
        }
        ESP_LOGI(TAG, "startup pet asset deferred until wake ready: %s",
                 s_startup_pet_asset_present ? s_startup_pet_asset_ref->revision : "none");
    } else if (cJSON_IsObject(pet_asset)) {
        esp_err_t asset_err = apply_pet_asset_ref(pet_asset);
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset ignored: %s", esp_err_to_name(asset_err));
    } else {
        // Runtime refreshes remain authoritative and can update the visible
        // asset synchronously; only the cold-start path is latency-sensitive.
        esp_err_t asset_err = clear_applied_pet_asset();
        if (asset_err == ESP_OK) s_loaded_pet_asset_revision[0] = '\0';
        if (asset_err != ESP_OK) ESP_LOGW(TAG, "handshake pet asset clear failed: %s", esp_err_to_name(asset_err));
    }
    apply_ambient_json(cJSON_GetObjectItemCaseSensitive(json, "ambient"));
    process_update_metadata(cJSON_GetObjectItemCaseSensitive(json, "update"), cold_start);
    cJSON_Delete(json);
    response_release(&response);
    log_heap_snapshot("handshake-ok");
    if (cold_start) {
        // The caller initializes ESP-SR immediately after this function
        // returns. Keep optional media work outside the authenticated response
        // parsing path; gateway_startup_task applies it only after wake ready.
        ESP_LOGI(TAG, "cold-start handshake essentials complete; optional pet asset remains deferred");
    }
    return ESP_OK;
}

static bool gateway_auth_failed(const http_response_t *response, esp_err_t err) {
    if (!response) return false;
    if (response->status == 401 || response->status == 403) return true;
    return err == ESP_ERR_NOT_SUPPORTED && response->status == 401;
}

// Unpaired devices speak the one-time six-digit code shown in the owner's
// MaClaw UI. MaClawSrv performs ASR and returns the gateway bearer over TLS.
static esp_err_t pair_by_voice(const uint8_t *wav, size_t wav_len) {
    http_response_t response;
    char client_header[96];
    snprintf(client_header, sizeof(client_header), "%s", s_device_id);
    // pair endpoint needs a client ID header rather than authorization; use a
    // short dedicated request because the normal helper only emits fixed headers.
    char url[URL_CAPACITY];
    int n = snprintf(url, sizeof(url), "%s/api/device-gateway/v1/pair/voice", s_gateway_url);
    if (n < 0 || n >= sizeof(url)) return ESP_ERR_INVALID_SIZE;
    memset(&response, 0, sizeof(response));
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        ESP_LOGW(TAG, "HTTP request lock timeout: POST pair/voice");
        return ESP_ERR_TIMEOUT;
    }
    response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = heap_caps_malloc(RESPONSE_CAPACITY, MALLOC_CAP_8BIT);
    if (!response.data) {
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    response.capacity = RESPONSE_CAPACITY;
    response.data[0] = '\0';
    if (device_connectivity_is_active_cellular()) {
        bool truncated = false;
        uint32_t cellular_response_len = 0;
        device_connectivity_http_request_t cellular_request = {
            .method = "POST", .url = url, .content_type = "audio/wav",
            .extra_header_name = "X-MaClaw-Client-ID",
            .extra_header_value = client_header, .body = wav, .body_len = (uint32_t)wav_len,
            .response = response.data, .response_capacity = (uint32_t)response.capacity,
            .response_len = &cellular_response_len, .status_code = &response.status,
            .truncated = &truncated, .timeout_ms = 30000, .foreground = true,
        };
        esp_err_t err = device_status_to_platform_error(
            device_connectivity_cellular_http_request(&cellular_request));
        response.len = cellular_response_len;
        response.truncated = truncated;
        xSemaphoreGive(s_http_mutex);
        if (err != ESP_OK || response.status != 201) {
            esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
            response_release(&response);
            return result;
        }
        cJSON *json = cJSON_Parse(response.data);
        const char *token = json ? json_string(json, "gatewayToken") : NULL;
        err = token ? save_gateway_token(token) : ESP_ERR_INVALID_RESPONSE;
        cJSON_Delete(json);
        response_release(&response);
        return err;
    }
    esp_http_client_config_t config = {.url = url, .event_handler = on_http_event, .user_data = &response, .timeout_ms = 30000, .crt_bundle_attach = esp_crt_bundle_attach};
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) {
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_http_client_set_method(client, HTTP_METHOD_POST);
    esp_http_client_set_header(client, "Content-Type", "audio/wav");
    esp_http_client_set_header(client, "X-MaClaw-Client-ID", client_header);
    esp_http_client_set_post_field(client, (const char *)wav, wav_len);
    esp_err_t err = esp_http_client_perform(client);
    response.status = esp_http_client_get_status_code(client);
    esp_http_client_cleanup(client);
    xSemaphoreGive(s_http_mutex);
    if (response.truncated) err = ESP_ERR_INVALID_SIZE;
    if (err != ESP_OK || response.status != 201) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? save_gateway_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    response_release(&response);
    return err;
}

static esp_err_t pair_by_code(void) {
    if (strlen(s_pair_code) != 6) return ESP_ERR_INVALID_STATE;
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", s_device_id);
    // pairCode is the canonical device-gateway field across Hub and
    // MaClawSrv. Hub retains a server-side code alias solely for old firmware.
    cJSON_AddStringToObject(body, "pairCode", s_pair_code);
    char *payload = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
    if (!payload) return ESP_ERR_NO_MEM;
    // One-time 6-digit code with a 30-minute TTL; logging the code and Hub URL
    // actually used is the only on-device evidence when the Hub reports
    // invalid_pairing_code despite a freshly generated code.
    ESP_LOGI(TAG, "pairing request: url=%s client=%s code=%s",
             s_gateway_url, s_device_id, s_pair_code);
    http_response_t response;
    esp_err_t err = request("POST", "/api/device-gateway/v1/pair", "application/json", payload, strlen(payload), &response);
    free(payload);
    if (err != ESP_OK || response.status != 201) {
        ESP_LOGE(TAG, "pair failed: err=%s status=%d body=%s", esp_err_to_name(err), response.status, response.data ? response.data : "");
        // Transport failures, rate limiting and server errors are temporary.
        // Keep the one-time code and retry instead of incorrectly telling the
        // user that the code expired and replacing the normal UI with a setup AP.
        esp_err_t result = err;
        // esp_http_client may return ESP_ERR_NOT_SUPPORTED after it has already
        // received an HTTP authentication error. The status and JSON body are
        // authoritative once a response exists.
        if (response.status > 0) {
            switch (response.status) {
                case 400:
                case 401:
                case 403:
                case 404:
                case 409:
                case 410:
                case 422:
                    result = ESP_ERR_INVALID_STATE;
                    break;
                default:
                    if (response.status >= 500 || response.status == 408 || response.status == 429) {
                        result = ESP_FAIL;
                    } else if (err == ESP_OK) {
                        result = ESP_FAIL;
                    }
                    break;
            }
        }
        response_release(&response);
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    const char *token = json ? json_string(json, "gatewayToken") : NULL;
    err = token ? save_gateway_token(token) : ESP_ERR_INVALID_RESPONSE;
    cJSON_Delete(json);
    if (err == ESP_OK) {
        // Token and pairing-code clear were committed by save_gateway_token()
        // as one Configuration Service transaction.
        s_pair_code[0] = '\0';
    }
    response_release(&response);
    return err;
}

static bool voice_upload_should_retry(esp_err_t err, int status) {
    switch (err) {
        case ESP_ERR_TIMEOUT:
        case ESP_ERR_HTTP_CONNECT:
        case ESP_ERR_HTTP_WRITE_DATA:
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_CONNECTING:
        case ESP_ERR_HTTP_EAGAIN:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return true;
        default:
            break;
    }
    return err == ESP_OK &&
           (status == 408 || status == 425 || status == 429 || status >= 500);
}

static void voice_upload_retry_delay(unsigned attempt) {
    vTaskDelay(pdMS_TO_TICKS(250u << (attempt - 1u)));
}

static esp_err_t upload_voice(const uint8_t *wav, size_t wav_len, char *media_id, size_t media_id_cap) {
    int64_t upload_started_us = esp_timer_get_time();
    cJSON *body = cJSON_CreateObject();
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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
        bool retry = voice_upload_should_retry(err, response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media prepare attempt %u/%u failed: err=%s status=%d retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), response.status,
                 retry ? "yes" : "no");
        response_release(&response);
        if (!retry) break;
        voice_upload_retry_delay(attempt);
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
        bool retry = voice_upload_should_retry(err, put_response.status) &&
                     attempt < VOICE_UPLOAD_RETRY_COUNT;
        ESP_LOGW(TAG, "media upload attempt %u/%u failed: err=%s status=%d wav=%u retry=%s",
                 attempt, VOICE_UPLOAD_RETRY_COUNT, esp_err_to_name(err), put_response.status,
                 (unsigned)wav_len, retry ? "yes" : "no");
        response_release(&put_response);
        if (!retry) break;
        voice_upload_retry_delay(attempt);
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
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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

static const char *command_submit_error_detail(esp_err_t err) {
    switch (err) {
        case ESP_ERR_TIMEOUT: return "网关响应超时，请稍后重试";
        case ESP_ERR_HTTP_CONNECT:
            return device_connectivity_is_active_cellular() ? "网络连接失败，请检查 4G"
                                           : "网络连接失败，请检查 Wi-Fi";
        case ESP_ERR_HTTP_WRITE_DATA: return "语音发送中断，请重新尝试";
        case ESP_ERR_HTTP_FETCH_HEADER:
        case ESP_ERR_HTTP_READ_TIMEOUT:
        case ESP_ERR_HTTP_CONNECTION_CLOSED:
        case ESP_ERR_HTTP_INCOMPLETE_DATA:
            return "网关连接不稳定，请重新尝试";
        case ESP_ERR_NO_MEM: return "设备内存不足，请重启后重试";
        case ESP_ERR_INVALID_RESPONSE: return "网关响应格式不兼容";
        case ESP_ERR_INVALID_STATE: return "请求已取消或网络状态异常";
        case ESP_FAIL: return "网关拒绝请求或服务异常";
        default: return esp_err_to_name(err);
    }
}

static esp_err_t send_text_event(const char *text, const char *reply_to) {
    if (!text || !text[0]) return ESP_ERR_INVALID_ARG;
    cJSON *body = cJSON_CreateObject();
    char event_id[80];
    snprintf(event_id, sizeof(event_id), "text-%lld", (long long)esp_timer_get_time());
    cJSON_AddStringToObject(body, "clientId", s_device_id);
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

static esp_err_t poll_reply(void) {
    char path[320];
    // Keep one and only one reader for the outgoing stream. A bounded long
    // poll removes the old TLS reconnect loop while still letting interaction
    // uploads run without waiting behind a 30-second request.
    /* The boot greeting is queued by the handshake and should be consumed
     * immediately.  A long-poll request made after the hardware-config item
     * can otherwise remain stuck behind a flaky keep-alive socket until the
     * Welcome gate expires, even though the greeting is already queued. */
    int poll_timeout_seconds = s_startup_welcome_gate_active
                                   ? 0
                                   : (command_display_active() ? 2 : 5);
    int64_t poll_started_us = esp_timer_get_time();
    long long previous_cursor = s_cursor;
	// A 64x64 RGB565 image expands to about 10.7 KiB in JSON. Fetch one
    // message at a time and retain enough space for queued dynamic glyphs and
    // rich replies. A full glyph preload observed in the field exceeded the
    // old 16 KiB buffer and pinned cursor zero forever, starving later replies.
    snprintf(path, sizeof(path), "/api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=%d",
             s_device_id, s_cursor, poll_timeout_seconds);
    bool server_audio_wake_lease_used = false;
    http_response_t response;
    esp_err_t err = request_with_capacity("GET", path, NULL, NULL, 0,
                                          OUTGOING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        response_release(&response);
        if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    if (!json) {
        ESP_LOGW(TAG, "outgoing response is not valid JSON");
        response_release(&response);
        if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    const char *next = json_string(json, "nextCursor");
    cJSON *messages = cJSON_GetObjectItemCaseSensitive(json, "messages");
    if (!next || !cJSON_IsArray(messages)) {
        ESP_LOGW(TAG, "outgoing response missing nextCursor/messages");
        cJSON_Delete(json);
        response_release(&response);
        if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    errno = 0;
    char *cursor_end = NULL;
    long long parsed_cursor = strtoll(next, &cursor_end, 10);
    if (errno == ERANGE || cursor_end == next || *cursor_end != '\0' || parsed_cursor < 0) {
        ESP_LOGW(TAG, "outgoing response has invalid cursor: %s", next);
        cJSON_Delete(json);
        response_release(&response);
        if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON *delivered_ack_ids = cJSON_CreateArray();
    cJSON *failed_ack_ids = cJSON_CreateArray();
    if (!delivered_ack_ids || !failed_ack_ids) {
        cJSON_Delete(delivered_ack_ids);
        cJSON_Delete(failed_ack_ids);
        cJSON_Delete(json);
        response_release(&response);
        if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
        return ESP_ERR_NO_MEM;
    }
    bool keep_cursor_for_retry = false;
    int message_count = cJSON_GetArraySize(messages);
    if (message_count > 0) {
        ESP_LOGI(TAG, "gateway poll: messages=%d cursor=%lld->%lld elapsed=%lldms",
                 message_count, previous_cursor, parsed_cursor,
                 (long long)((esp_timer_get_time() - poll_started_us) / 1000));
    }
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, messages) {
        const char *type = json_string(item, "type");
        const char *text = json_string(item, "text");
        const char *audio_data = json_string(item, "file_data");
        if (!audio_data) audio_data = json_string(item, "data");
        // Some producers serialize an absent inline body as an empty string
        // while also supplying the media URL. Treat that as absent so the URL
        // path remains usable instead of permanently failing an empty Base64
        // payload and discarding valid audio.
        if (audio_data && !audio_data[0]) audio_data = NULL;
        const char *audio_mime = json_string(item, "mime_type");
        if (!audio_mime) audio_mime = json_string(item, "mimeType");
        const char *audio_url = json_string(item, "url");
        bool invalid_audio_url = audio_url && !hardware_audio_url_allowed(audio_url);
        if (invalid_audio_url) {
            ESP_LOGW(TAG, "ignored unsafe server audio URL");
            audio_url = NULL;
        }
        const char *reply_to = outgoing_reply_correlation(item);
        const char *id = json_string(item, "id");
        cJSON *extra = cJSON_GetObjectItemCaseSensitive(item, "extra");
        bool audio_message = type && (!strcmp(type, "voice") || !strcmp(type, "audio"));
        bool speech_end_message = type && !strcmp(type, "speech_end");
		bool image_message = type && !strcmp(type, "image");
		bool image_handled = !image_message;
		bool image_permanently_invalid = false;
		bool text_message = type && !strcmp(type, "text");
		bool text_handled = !text_message;
		bool text_permanently_invalid = false;
		bool tool_message = type && !strcmp(type, "tool_call");
		bool tool_handled = !tool_message;
        bool hardware_config_message = type && !strcmp(type, "hardware_config");
        bool hardware_config_handled = !hardware_config_message;
        bool hardware_config_permanently_invalid = false;
        bool welcome_audio = id && (!strncmp(id, "mc_welcome_", 11) || !strncmp(id, "hub_welcome_", 12));
		bool preview_audio = cJSON_IsObject(extra) &&
			cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(extra, "hardware_audio_preview"));
		bool startup_welcome = !preview_audio &&
			startup_welcome_is_current_boot(item, welcome_audio);
		// Reserved Welcome messages are boot-scoped transactions. A greeting left
		// pending by an interrupted ACK from an earlier boot must never be treated
		// as ordinary speech: doing so plays the stale greeting and then this boot's
		// greeting. Explicit GUI previews are exempt and remain user-triggered.
		bool stale_startup_welcome = welcome_audio && !preview_audio && !startup_welcome;
		bool discard_startup_welcome = stale_startup_welcome;
		bool startup_welcome_already_consumed = false;
		if (startup_welcome) {
			taskENTER_CRITICAL(&s_task_state_lock);
			startup_welcome_already_consumed = s_startup_welcome_consumed;
			discard_startup_welcome = s_startup_welcome_timed_out ||
			                          startup_welcome_already_consumed;
			taskEXIT_CRITICAL(&s_task_state_lock);
			if (discard_startup_welcome) {
				// Never play a boot greeting after MultiNet has been started. ACK it
				// as handled so a late delivery cannot retry forever. The same rule
				// also turns an ACK retry after successful playback into a silent,
				// idempotent delivery instead of replaying the greeting.
				ESP_LOGW(TAG, "%s startup Welcome discarded: id=%s",
				         startup_welcome_already_consumed ? "already consumed" : "late",
					         id);
			}
		} else if (stale_startup_welcome) {
			ESP_LOGW(TAG, "stale or unscoped startup Welcome discarded: id=%s", id);
		}
        // Resolve correlation once. The hand-off helper deliberately waits for
        // up to 200 ms while interaction_task publishes its accepted message ID;
        // calling it again for the same item adds avoidable poll latency and can
        // make a multipart spoken reply feel stalled.
        bool cancelled_reply = cancelled_command_reply_matches(reply_to);
        bool active_reply = !cancelled_reply &&
                            active_command_reply_matches_after_handoff(reply_to);
        bool result_speech_reply = !cancelled_reply && audio_message &&
                                   result_speech_reply_matches(reply_to);
		if (speech_end_message) {
			finish_result_speech_transaction(reply_to);
			tool_handled = true;
		}
        // A reboot has no live correlation for the command that produced an
        // older queued result. Treat every unmatched replyTo as an orphan and
        // acknowledge it silently. This also protects against older Hub/GUI
        // versions that do not clear their runtime queue on cold handshake.
        bool orphaned_command_result = reply_to && reply_to[0] &&
                                       !active_reply && !result_speech_reply &&
                                       !cancelled_reply;
        // Non-system speech may arrive while the foreground command still owns
        // the display/audio bus. Preserve those messages for retry. Greetings
        // and explicit GUI previews are safe to play during initialization or
        // while a previous command result remains on screen.
        // Correlated speech normally follows terminal text. The result page
        // arms a bounded correlation/count gate before waking the command
        // worker, so only that answer's declared parts may play while the
        // result surface remains foregrounded.
        bool audio_can_play = !command_display_active() || welcome_audio ||
                              preview_audio || active_reply || result_speech_reply;
        bool audio_handled = discard_startup_welcome ||
                             (audio_message && orphaned_command_result);
        bool audio_permanently_invalid = false;
        bool progress = outgoing_message_is_progress(item);
        bool final = outgoing_message_is_final(item);
        const char *skin = json_string(item, "pet_skin");
        cJSON *motion = cJSON_GetObjectItemCaseSensitive(item, "pet_motion_enabled");
        cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
        const char *turn = cJSON_IsObject(metadata) ? json_string(metadata, "acp_turn") : NULL;
        if (!turn) turn = json_string(item, "acp_turn");
        cJSON *seq_item = cJSON_GetObjectItemCaseSensitive(item, "seq");
        long long message_seq = cJSON_IsNumber(seq_item) ? (long long)seq_item->valuedouble : 0;
        ESP_LOGI(TAG, "outgoing message: id=%s seq=%lld type=%s replyTo=%s progress=%s final=%s turn=%s text=%u active=%s",
                 id && id[0] ? id : "<none>", message_seq,
                 type && type[0] ? type : "<none>",
                 reply_to && reply_to[0] ? reply_to : "<none>", progress ? "yes" : "no",
                 final ? "yes" : "no", turn && turn[0] ? turn : "<none>",
                 (unsigned)(text ? strlen(text) : 0),
                 s_active_command_reply_to[0] ? s_active_command_reply_to : "<none>");
        if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
		if (tool_message) {
			esp_err_t tool_err = handle_client_tool_call(item);
			tool_handled = tool_err == ESP_OK;
			if (!tool_handled) {
				ESP_LOGW(TAG, "client tool execution/result delivery failed: %s", esp_err_to_name(tool_err));
				keep_cursor_for_retry = true;
			}
		}
        bool pet_profile_message = type && !strcmp(type, "pet_profile");
        bool pet_profile_handled = !pet_profile_message;
        bool pet_profile_permanently_invalid = false;
        if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
        cJSON *pet_asset = cJSON_GetObjectItemCaseSensitive(item, "pet_asset");
        if (!cJSON_IsObject(pet_asset) && cJSON_IsObject(extra)) {
            pet_asset = cJSON_GetObjectItemCaseSensitive(extra, "pet_asset");
        }
        if (skin) app_ui_set_pet_profile(skin, !motion || cJSON_IsTrue(motion));
        if (pet_profile_message) {
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
            pet_profile_handled = true;
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
                pet_profile_handled = asset_err == ESP_OK;
                pet_profile_permanently_invalid = audio_error_is_permanent(asset_err) ||
                                                  asset_err == ESP_ERR_INVALID_CRC;
                if (!pet_profile_handled) {
                    if (id && !strcmp(id, s_pet_asset_retry_id)) {
                        ++s_pet_asset_retry_count;
                    } else {
                        strlcpy(s_pet_asset_retry_id, id ? id : "",
                                sizeof(s_pet_asset_retry_id));
                        s_pet_asset_retry_count = 1;
                    }
                    // 连续失败达到上限按永久失败处理，避免堵死整页消息。
                    if (s_pet_asset_retry_count >= PET_ASSET_RETRY_LIMIT) {
                        pet_profile_permanently_invalid = true;
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
                pet_profile_handled = asset_err == ESP_OK;
                pet_profile_permanently_invalid = audio_error_is_permanent(asset_err);
                if (!pet_profile_handled) ESP_LOGW(TAG, "pet asset clear failed: %s", esp_err_to_name(asset_err));
            }
        }
        apply_glyphs_json(cJSON_GetObjectItemCaseSensitive(item, "glyphs"));
        apply_ambient_json(cJSON_GetObjectItemCaseSensitive(item, "ambient"));
        if (type && !strcmp(type, "ambient")) apply_ambient_json(item);
        if (hardware_config_message && cJSON_IsObject(extra)) {
            int volume = 0;
            int brightness = 0;
            int screen_sleep_seconds = 0;
            bool has_volume = json_number(extra, "volume", &volume);
            bool has_brightness = json_number(extra, "brightness", &brightness);
            bool has_screen_sleep = json_number(extra, "screenSleepSeconds", &screen_sleep_seconds);
            // Each hardware_config field is independently durable. A partial
            // success must stay queued so a reconnect can retry the failed
            // setting instead of ACKing it as if all three applied.
            bool volume_handled = !has_volume;
            bool brightness_handled = !has_brightness;
            bool screen_sleep_handled = !has_screen_sleep;
            if (!has_volume && !has_brightness && !has_screen_sleep) {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "ignored hardware config without volume/brightness/screen sleep");
            }
            if (has_volume && volume >= 0 && volume <= 100) {
                device_status_t volume_status = device_audio_set_output_volume((uint8_t)volume);
                if (volume_status == DEVICE_STATUS_OK) {
                    ESP_LOGI(TAG, "server output volume: %d%%", volume);
                    esp_err_t save_err = persist_output_volume((unsigned)volume);
                    volume_handled = save_err == ESP_OK;
                    if (!volume_handled) {
                        ESP_LOGW(TAG, "server output volume persistence failed: %s",
                                 esp_err_to_name(save_err));
                    }
                } else if (volume_status != DEVICE_STATUS_UNAVAILABLE) {
                    ESP_LOGW(TAG, "server output volume failed: device status=%d", volume_status);
                } else {
                    hardware_config_permanently_invalid = true;
                }
            } else if (has_volume) {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "ignored invalid server output volume");
            }
            // 亮度与音量同通道下发：0 = 背光熄灭但系统继续运行，>=1 恢复。
            if (has_brightness && brightness >= 0 && brightness <= 100) {
                device_status_t brightness_status =
                    app_ui_apply_remote_brightness((uint8_t)brightness);
                if (brightness_status == DEVICE_STATUS_OK) {
                    ESP_LOGI(TAG, "server display brightness: %d%%", brightness);
                    esp_err_t save_err = persist_display_brightness((unsigned)brightness);
                    brightness_handled = save_err == ESP_OK;
                    if (!brightness_handled) {
                        ESP_LOGW(TAG, "server display brightness persistence failed: %s",
                                 esp_err_to_name(save_err));
                    }
                } else if (brightness_status != DEVICE_STATUS_UNAVAILABLE) {
                    ESP_LOGW(TAG, "server display brightness failed: device status=%d", brightness_status);
                } else {
                    hardware_config_permanently_invalid = true;
                }
            } else if (has_brightness) {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "ignored invalid server display brightness");
            }
            if (has_screen_sleep && valid_screen_sleep_seconds(screen_sleep_seconds)) {
                esp_err_t save_err = save_screen_sleep_seconds((unsigned)screen_sleep_seconds);
                if (save_err == ESP_OK) {
                    app_ui_set_display_off_idle_ms((uint32_t)screen_sleep_seconds * 1000u);
                    screen_sleep_handled = true;
                    ESP_LOGI(TAG, "server screen sleep timeout: %d seconds", screen_sleep_seconds);
                } else {
                    ESP_LOGW(TAG, "screen sleep timeout persistence failed: %s", esp_err_to_name(save_err));
                }
            } else if (has_screen_sleep) {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "ignored invalid server screen sleep timeout");
            }
            hardware_config_handled = volume_handled && brightness_handled && screen_sleep_handled;
        } else if (hardware_config_message) {
            hardware_config_permanently_invalid = true;
            ESP_LOGW(TAG, "ignored hardware config without extra object");
        }
        if (type && !strcmp(type, "pet_state")) {
            const char *state = cJSON_IsObject(extra) ? json_string(extra, "state") : NULL;
            if (!state) state = json_string(item, "state");
            // An unsolicited idle/quiet state must never interrupt the
            // foreground thinking -> result transition.
            if (state && !command_display_active()) pet(state);
        }
        if (orphaned_command_result) {
            ESP_LOGW(TAG, "orphaned result discarded: id=%s replyTo=%s type=%s",
                     id && id[0] ? id : "<none>", reply_to,
                     type && type[0] ? type : "<none>");
            text_handled = text_message;
            image_handled = image_message;
        } else if (type && !strcmp(type, "meeting_result")) {
            const char *summary = cJSON_IsObject(extra) ? json_string(extra, "summary") : NULL;
            const char *status = cJSON_IsObject(extra) ? json_string(extra, "status") : NULL;
            const char *message = summary && summary[0] ? summary :
                                  text && text[0] ? text :
                                  status && status[0] ? status : "已保存到文稿库";
            pet("done");
            app_ui_show_response("会议处理完成", message);
        }
		if (image_message && !orphaned_command_result) {
			const char *image_data = json_string(item, "data");
			const char *image_mime = json_string(item, "mimeType");
			if (!image_mime) image_mime = json_string(item, "mime_type");
			const char *caption = json_string(item, "caption");
			int image_width = 0, image_height = 0;
			bool dimensions_valid = json_number(item, "width", &image_width) &&
				json_number(item, "height", &image_height) && image_width >= 1 &&
				image_width <= RESPONSE_IMAGE_MAX_DIMENSION && image_height >= 1 &&
				image_height <= RESPONSE_IMAGE_MAX_DIMENSION;
			size_t expected = dimensions_valid ? (size_t)image_width * (size_t)image_height * 2u : 0;
			if (!image_data || !image_mime || strcmp(image_mime, RESPONSE_IMAGE_MIME) ||
				!dimensions_valid || expected > RESPONSE_IMAGE_MAX_BYTES) {
				image_permanently_invalid = true;
			} else {
				uint8_t *pixels = heap_caps_malloc(expected, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
				if (!pixels) pixels = malloc(expected);
				size_t decoded = 0;
				if (pixels && mbedtls_base64_decode(pixels, expected, &decoded,
						(const unsigned char *)image_data, strlen(image_data)) == 0 && decoded == expected) {
					if (cancelled_reply) {
						// A cancelled command deliberately discards late media so it
						// cannot reappear after the cancellation screen.
						image_handled = true;
					} else if (active_reply) {
						TaskHandle_t waiter = begin_active_command_reply();
						if (waiter) {
							// A terminal image can be followed by the same correlated
							// multipart TTS stream as a text result.  Arm that hand-off
							// before waking the interaction task: completing the image
							// clears the active reply correlation, and without this
							// transaction a later audio part would be classified as an
							// orphan and acknowledged silently.
							unsigned pending_speech_parts = outgoing_pending_speech_parts(item);
							if (pending_speech_parts > 0) {
								remember_result_speech_reply(reply_to, pending_speech_parts);
							}
							complete_active_command_image_reply(waiter, "码卡龙", caption,
									(const uint16_t *)pixels, (size_t)image_width,
									(size_t)image_height);
							if (command_timing_matches(reply_to)) log_command_timing("image-result");
							image_handled = true;
						}
					} else if (!command_display_active()) {
						app_ui_show_response_image("码卡龙", caption, (const uint16_t *)pixels,
								(size_t)image_width, (size_t)image_height);
						image_handled = true;
					}
				} else if (pixels) {
					image_permanently_invalid = true;
				}
				free(pixels);
			}
			if (image_permanently_invalid) {
				ESP_LOGW(TAG, "ignored invalid response image: mime=%s size=%dx%d",
						 image_mime ? image_mime : "<none>", image_width, image_height);
			}
		}
		if (text_message && text && text[0] && !orphaned_command_result) {
			if (cancelled_reply) {
				ESP_LOGI(TAG, "ignored late reply for cancelled command: %s", reply_to);
				text_handled = true;
            } else if (progress && !final && active_reply) {
                // Progress refreshes the thinking state but is not the answer.
                // A few Hub paths retain progress=true on the terminal envelope;
                // final must win so a completed answer cannot remain hidden
                // behind the remote-processing surface.
                if (command_timing_matches(reply_to) && !s_command_timing_first_progress_us) {
                    s_command_timing_first_progress_us = esp_timer_get_time();
                    ESP_LOGI(TAG, "command first progress: replyTo=%s afterAccepted=%ums",
                             reply_to,
                             (unsigned)elapsed_ms_between(s_command_timing_accepted_us,
                                                          s_command_timing_first_progress_us));
                }
                ESP_LOGI(TAG, "remote progress received: replyTo=%s", reply_to);
                text_handled = true;
			} else if (active_reply) {
                // Once a reply is present the thinking phase has ended; a
                // double tap arriving while this frame is drawn must not turn
                // an already completed command into a cancellation.
                TaskHandle_t waiter = begin_active_command_reply();
                if (!waiter) {
                    ESP_LOGI(TAG, "reply arrived while cancellation owns command: %s", reply_to);
                } else {
                    // Arm the exact post-terminal speech transaction before
                    // waking the command worker; it may clear active replyTo
                    // immediately after the result frame is published.
                    unsigned pending_speech_parts = outgoing_pending_speech_parts(item);
                    if (pending_speech_parts > 0) {
                        remember_result_speech_reply(reply_to, pending_speech_parts);
                    }
                    // Keep the final response surface continuous with the
                    // thinking surface. Do not briefly switch to idle here.
                    complete_active_command_text_reply(waiter, "码卡龙", text);
                    if (command_timing_matches(reply_to)) log_command_timing("text-result");
                    text_handled = true;
                }
			} else {
                // The outgoing stream can contain unrelated notifications or
                // late replies from before this boot. They may still be shown
                // when the device is idle, but must never complete or replace
                // an active command unless replyTo identifies that command.
				if (!command_display_active()) {
					app_ui_show_response("码卡龙", text);
					text_handled = true;
				} else if (final && (!reply_to || !reply_to[0])) {
					// Older Hub/GUI builds could enqueue a terminal hardware result
					// without its command correlation. Keeping that item pending pins
					// the shared page cursor, so the correctly correlated result behind
					// it can never arrive. Consume the malformed terminal frame while
					// preserving the active command: it is neither displayed nor used
					// to complete/cancel the foreground transaction.
					ESP_LOGW(TAG, "discarded uncorrelated terminal text during active command: id=%s",
					         id && id[0] ? id : "<none>");
					text_handled = true;
				} else {
                    ESP_LOGI(TAG, "deferred unrelated text during active command: replyTo=%s",
                             reply_to && reply_to[0] ? reply_to : "<none>");
			}
		}
		if (text_message && (!text || !text[0])) {
			text_permanently_invalid = true;
			ESP_LOGW(TAG, "ignored text response without content");
		}
        }
        if (type && !strcmp(type, "error") && orphaned_command_result) {
            // The generic acknowledgement path has no separate error handled
            // flag. Logging above is sufficient; the queue entry is consumed.
        } else if (type && !strcmp(type, "error")) {
            if (cancelled_reply) {
                ESP_LOGI(TAG, "ignored late error for cancelled command: %s",
                         reply_to && reply_to[0] ? reply_to : "<none>");
            } else if (active_reply) {
                TaskHandle_t waiter = begin_active_command_reply();
                if (waiter) {
                    const char *detail = text && text[0] ? text : "远端返回错误，但没有详细说明";
                    pet("alert");
                    complete_active_command_text_reply(waiter, "远端处理失败", detail);
                    ESP_LOGE(TAG, "remote command failed: replyTo=%s error=%s detail=%s",
                             reply_to, json_string(item, "error") ? json_string(item, "error") : "<none>", detail);
                }
            } else {
                ESP_LOGW(TAG, "unmatched remote error: replyTo=%s detail=%s",
                         reply_to && reply_to[0] ? reply_to : "<none>",
                         text && text[0] ? text : "<none>");
            }
        } else if (final && active_reply && (!type || (strcmp(type, "text") && strcmp(type, "image") &&
                                                      strcmp(type, "voice") && strcmp(type, "audio")))) {
            TaskHandle_t waiter = begin_active_command_reply();
            if (waiter) {
                complete_active_command_text_reply(
                    waiter, "任务已完成",
                    text && text[0] ? text : "远端已完成，但没有可显示的文字结果");
            }
        }
        if (audio_message && audio_data && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            audio_mime_supported(audio_mime)) {
            // Inline server speech does not open a second media HTTP request,
            // but it still competes with resident MultiNet for the DMA/codec
            // path and must obey the same foreground wake lifecycle as URL
            // speech. Keep the lease through the ACK below so no later poll
            // can recreate the recognizer before this reply is committed.
            server_audio_wake_lease_used =
                begin_server_audio_wake_memory_lease("inline server audio") ||
                server_audio_wake_lease_used;
            size_t audio_capacity = 0;
            int decode_status = mbedtls_base64_decode(NULL, 0, &audio_capacity,
                                                       (const unsigned char *)audio_data,
                                                       strlen(audio_data));
            if (decode_status != MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL || audio_capacity < 2 ||
                audio_capacity >= HARDWARE_AUDIO_RESPONSE_CAPACITY) {
                audio_permanently_invalid = true;
            }
            if (decode_status == MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL && audio_capacity >= 2 &&
                audio_capacity < HARDWARE_AUDIO_RESPONSE_CAPACITY) {
                uint8_t *audio = heap_caps_malloc(audio_capacity, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
                if (!audio) audio = malloc(audio_capacity);
                size_t audio_len = 0;
                if (audio && mbedtls_base64_decode(audio, audio_capacity, &audio_len,
                                                  (const unsigned char *)audio_data,
                                                  strlen(audio_data)) == 0) {
                    ESP_LOGI(TAG, "playing server audio: %u bytes mime=%s",
                             (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                    esp_err_t play_err = play_audio_payload(audio_mime, audio, audio_len);
                    if (play_err != ESP_OK) ESP_LOGW(TAG, "server speech playback failed: %s", esp_err_to_name(play_err));
                    audio_handled = play_err == ESP_OK;
                    audio_permanently_invalid = audio_error_is_permanent(play_err);

                } else if (audio) {
                    ESP_LOGW(TAG, "invalid server speech payload");
                    audio_permanently_invalid = true;
                } else {
                    ESP_LOGW(TAG, "server speech allocation failed: %u bytes",
                             (unsigned)audio_capacity);
                }
                free(audio);
            } else {
                ESP_LOGW(TAG, "ignored server audio payload: base64=%d size=%u", decode_status, (unsigned)audio_capacity);
            }
        }
        if (audio_message && !audio_data && audio_url && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            audio_mime_supported(audio_mime)) {
            uint8_t *audio = NULL;
            size_t audio_len = 0;
            esp_err_t fetch_err = download_audio(audio_url, &audio, &audio_len);
            server_audio_wake_lease_used = true;
            if (fetch_err == ESP_OK) {
                ESP_LOGI(TAG, "playing downloaded server audio: %u bytes mime=%s",
                         (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                esp_err_t play_err = play_audio_payload(audio_mime, audio, audio_len);
                if (play_err != ESP_OK) ESP_LOGW(TAG, "downloaded server speech playback failed: %s", esp_err_to_name(play_err));
                audio_handled = play_err == ESP_OK;
                audio_permanently_invalid = audio_error_is_permanent(play_err);

            } else {
                ESP_LOGW(TAG, "server speech download failed: %s", esp_err_to_name(fetch_err));
                audio_permanently_invalid = audio_error_is_permanent(fetch_err);
            }
            free(audio);
        }
        // Do not acknowledge an audio message that we could neither fetch nor
        // play. Keeping it pending lets a transient network/I2S failure retry
        // on the next poll instead of silently losing the welcome sound. Late
        // cancelled audio is intentionally discarded so it cannot retry forever.
        // Permanent protocol/content errors must not pin the page cursor and
        // create a hot retry loop. Transient states (busy audio bus, download,
        // allocation or I2S failure) remain pending and retry on the next poll.
        audio_permanently_invalid = audio_permanently_invalid || (audio_message &&
            (invalid_audio_url || !audio_mime_supported(audio_mime) ||
             (!audio_data && !audio_url)));
        if (result_speech_reply && (audio_handled || audio_permanently_invalid)) {
            finish_result_speech_part(reply_to);
        }
        if (startup_welcome && !discard_startup_welcome &&
            (audio_handled || audio_permanently_invalid)) {
            taskENTER_CRITICAL(&s_task_state_lock);
            s_startup_welcome_consumed = true;
            taskEXIT_CRITICAL(&s_task_state_lock);
            finish_startup_welcome_gate(audio_handled ? "playback complete" : "playback unavailable");
        }
		bool ack_message = tool_handled &&
            (hardware_config_handled || hardware_config_permanently_invalid) &&
			(pet_profile_handled || pet_profile_permanently_invalid) &&
			(!text_message || text_handled || cancelled_reply || text_permanently_invalid) &&
			(!audio_message || audio_handled || cancelled_reply || audio_permanently_invalid) &&
			(!image_message || image_handled || cancelled_reply || image_permanently_invalid);
        if (id && !ack_message) {
            keep_cursor_for_retry = true;
            // The page cursor is shared by all messages. Stop at the first
            // transient failure so a later speech part or terminal text cannot
            // overtake it and complete the command with missing audio. Already
            // handled messages are acknowledged below; the server then resends
            // only this item and the untouched tail of the page.
            ESP_LOGW(TAG, "halting outgoing page for ordered retry: id=%s type=%s",
                     id, type && type[0] ? type : "<none>");
            break;
        }
        if (id && ack_message) {
            cJSON *ack_id = cJSON_CreateString(id);
			bool permanently_failed = hardware_config_permanently_invalid ||
				pet_profile_permanently_invalid ||
				(text_message && text_permanently_invalid && !cancelled_reply) ||
				(audio_message && audio_permanently_invalid && !audio_handled && !cancelled_reply) ||
				(image_message && image_permanently_invalid && !image_handled && !cancelled_reply);
            cJSON *target = permanently_failed
                                ? failed_ack_ids : delivered_ack_ids;
            if (!ack_id || !cJSON_AddItemToArray(target, ack_id)) {
                cJSON_Delete(ack_id);
                cJSON_Delete(delivered_ack_ids);
                cJSON_Delete(failed_ack_ids);
                cJSON_Delete(json);
                response_release(&response);
                if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
                return ESP_ERR_NO_MEM;
            }
        }
    }
    cJSON *ack_groups[2] = {delivered_ack_ids, failed_ack_ids};
    const char *ack_statuses[2] = {"delivered", "failed"};
    for (size_t ack_index = 0; ack_index < 2; ++ack_index) {
        cJSON *ack_ids = ack_groups[ack_index];
        if (cJSON_GetArraySize(ack_ids) == 0) continue;
        cJSON *ack = cJSON_CreateObject();
        if (!ack) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
            return ESP_ERR_NO_MEM;
        }
        cJSON_AddStringToObject(ack, "clientId", s_device_id);
        cJSON_AddItemReferenceToObject(ack, "messageIds", ack_ids);
        cJSON_AddStringToObject(ack, "status", ack_statuses[ack_index]);
        char *payload = cJSON_PrintUnformatted(ack);
        cJSON_Delete(ack);
        if (!payload) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
            return ESP_ERR_NO_MEM;
        }
        http_response_t ack_resp;
        esp_err_t ack_err = request("POST", "/api/im-gateway/v1/ack", "application/json",
                                    payload, strlen(payload), &ack_resp);
        free(payload);
        if (ack_err != ESP_OK || (ack_resp.status != 200 && ack_resp.status != 204)) {
            ESP_LOGW(TAG, "gateway ack failed: err=%s status=%d",
                     esp_err_to_name(ack_err), ack_resp.status);
            esp_err_t result = ack_err == ESP_OK ? ESP_FAIL : ack_err;
            response_release(&ack_resp);
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            response_release(&response);
            if (finish_server_audio_wake_memory_lease()) schedule_wake_restart();
            return result;
        }
        response_release(&ack_resp);
    }
    cJSON_Delete(delivered_ack_ids);
    cJSON_Delete(failed_ack_ids);
    // Cursor is page-level while acknowledgements are message-level. If one
    // audio item was intentionally left unacknowledged, advancing the cursor
    // would hide it from the next poll despite the missing ACK.
    if (!keep_cursor_for_retry) s_cursor = (int64_t)parsed_cursor;
    cJSON_Delete(json);
    response_release(&response);
    if (server_audio_wake_lease_used && finish_server_audio_wake_memory_lease()) {
        // The acknowledgement also uses TLS.  Restart only once this ordered
        // server-audio transaction has released every AES/TLS allocation.
        schedule_wake_restart();
    }
    return ESP_OK;
}

static void put_le16(uint8_t *out, uint16_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
}

static void put_le32(uint8_t *out, uint32_t value) {
    out[0] = (uint8_t)value;
    out[1] = (uint8_t)(value >> 8);
    out[2] = (uint8_t)(value >> 16);
    out[3] = (uint8_t)(value >> 24);
}

static void build_meeting_wav_header(uint8_t header[44], uint32_t pcm_bytes) {
    memset(header, 0, 44);
    memcpy(header, "RIFF", 4);
    put_le32(header + 4, 36u + pcm_bytes);
    memcpy(header + 8, "WAVEfmt ", 8);
    put_le32(header + 16, 16);
    put_le16(header + 20, 1);
    put_le16(header + 22, 1);
    put_le32(header + 24, MEETING_SAMPLE_RATE);
    put_le32(header + 28, MEETING_SAMPLE_RATE * 2u);
    put_le16(header + 32, 2);
    put_le16(header + 34, 16);
    memcpy(header + 36, "data", 4);
    put_le32(header + 40, pcm_bytes);
}

static esp_err_t finalize_meeting_wav(FILE *file, uint64_t samples) {
    if (!file || samples > (UINT32_MAX / sizeof(int16_t))) return ESP_ERR_INVALID_SIZE;
    uint8_t header[44];
    build_meeting_wav_header(header, (uint32_t)(samples * sizeof(int16_t)));
    if (fseek(file, 0, SEEK_SET) != 0 || fwrite(header, 1, sizeof(header), file) != sizeof(header)) {
        return ESP_FAIL;
    }
    if (fflush(file) != 0 || fsync(fileno(file)) != 0) return ESP_FAIL;
    return ESP_OK;
}

static esp_err_t ensure_meeting_wav_header(FILE *file, size_t file_size) {
    if (!file || file_size <= 44 || ((file_size - 44) % sizeof(int16_t)) != 0) {
        return ESP_ERR_INVALID_SIZE;
    }
    uint64_t samples = (file_size - 44) / sizeof(int16_t);
    if (samples > (UINT32_MAX / sizeof(int16_t))) return ESP_ERR_INVALID_SIZE;
    uint8_t expected[44];
    uint8_t existing[44];
    build_meeting_wav_header(expected, (uint32_t)(samples * sizeof(int16_t)));
    if (fseek(file, 0, SEEK_SET) != 0 || fread(existing, 1, sizeof(existing), file) != sizeof(existing)) {
        return ESP_FAIL;
    }
    if (memcmp(existing, expected, sizeof(expected)) == 0) return ESP_OK;
    // A reset or capture error may leave the initial zero-length placeholder
    // header in front of otherwise valid PCM. Repair it before any retry so a
    // retained meeting is always uploaded as a valid, self-describing WAV.
    ESP_LOGW(TAG, "repairing retained meeting WAV header: bytes=%u",
             (unsigned)file_size);
    return finalize_meeting_wav(file, samples);
}

static void digest_hex(const uint8_t digest[32], char out[65]) {
    static const char hex[] = "0123456789abcdef";
    for (size_t i = 0; i < 32; ++i) {
        out[i * 2] = hex[digest[i] >> 4];
        out[i * 2 + 1] = hex[digest[i] & 15];
    }
    out[64] = '\0';
}

static esp_err_t hash_file_range(FILE *file, size_t offset, size_t length,
                                 uint8_t *buffer, size_t buffer_size, char out_hex[65]) {
    if (!file || !buffer || buffer_size == 0 || fseek(file, (long)offset, SEEK_SET) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    psa_hash_operation_t operation = PSA_HASH_OPERATION_INIT;
    psa_status_t status = psa_hash_setup(&operation, PSA_ALG_SHA_256);
    size_t remaining = length;
    while (status == PSA_SUCCESS && remaining > 0) {
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        size_t count = fread(buffer, 1, wanted, file);
        if (count != wanted) {
            psa_hash_abort(&operation);
            return ESP_FAIL;
        }
        status = psa_hash_update(&operation, buffer, count);
        remaining -= count;
    }
    uint8_t digest[32];
    size_t digest_length = 0;
    if (status == PSA_SUCCESS) {
        status = psa_hash_finish(&operation, digest, sizeof(digest), &digest_length);
    } else {
        psa_hash_abort(&operation);
    }
    if (status != PSA_SUCCESS || digest_length != sizeof(digest)) return ESP_FAIL;
    digest_hex(digest, out_hex);
    return ESP_OK;
}

static esp_err_t configure_meeting_chunk_client(esp_http_client_handle_t client,
                                                const char *url,
                                                const char sha256_hex[65],
                                                http_response_t *response) {
    if (!client || !url || !sha256_hex || !response) return ESP_ERR_INVALID_ARG;
    esp_err_t err = esp_http_client_set_url(client, url);
    if (err == ESP_OK) err = esp_http_client_set_user_data(client, response);
    if (err == ESP_OK) err = esp_http_client_set_timeout_ms(client, 60000);
    if (err == ESP_OK) err = esp_http_client_set_method(client, HTTP_METHOD_PUT);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Content-Type", "application/octet-stream");
    if (err == ESP_OK) err = esp_http_client_set_header(client, "X-Chunk-SHA256", sha256_hex);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Accept", "application/json");
    if (err == ESP_OK) err = esp_http_client_delete_header(client, "Connection");
    char authorization[128];
    snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Authorization", authorization);
    return err;
}

static device_status_t read_meeting_chunk_body(void *context, void *buffer,
                                               uint32_t requested, uint32_t *read_bytes) {
    if (!context || !buffer || !read_bytes) return DEVICE_STATUS_INVALID_ARGUMENT;
    *read_bytes = fread(buffer, 1, requested, (FILE *)context);
    if (*read_bytes == requested) return DEVICE_STATUS_OK;
    return ferror((FILE *)context) ? DEVICE_STATUS_IO_ERROR : DEVICE_STATUS_INVALID_ARGUMENT;
}

/* The meeting's Wi-Fi PUT uses open/write/fetch rather than request_with_capacity(),
 * so it has to publish its own cancellable client pointer.  The pointer is valid
 * only while the worker owns the HTTP transaction; the stopper takes this mutex
 * before issuing cancel_request(), and the worker clears it before cleanup. */
static void meeting_task_set_active_http_client(esp_http_client_handle_t client) {
    if (!s_meeting_task_client_mutex) return;
    xSemaphoreTake(s_meeting_task_client_mutex, portMAX_DELAY);
    s_meeting_task_active_client = client;
    xSemaphoreGive(s_meeting_task_client_mutex);
}

static bool meeting_task_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_meeting_task_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static esp_err_t stream_meeting_chunk(const char *recording_id, int index, FILE *file,
                                      size_t offset, size_t length, const char sha256_hex[65],
                                      uint8_t *buffer, size_t buffer_size,
                                      size_t completed_before, size_t total_bytes,
                                      bool publish_progress,
                                      esp_http_client_handle_t *reusable_client) {
    if (meeting_task_stop_requested()) return ESP_ERR_INVALID_STATE;
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 48];
    char url[URL_CAPACITY];
    int path_len = snprintf(path, sizeof(path), "%s/%s/chunks/%d",
                            s_meeting_base_path, recording_id, index);
    int url_len = snprintf(url, sizeof(url), "%s%s", s_gateway_url, path);
    if (path_len < 0 || path_len >= (int)sizeof(path) ||
        url_len < 0 || url_len >= (int)sizeof(url) ||
        fseek(file, (long)offset, SEEK_SET) != 0) return ESP_ERR_INVALID_SIZE;
    if (device_connectivity_is_active_cellular()) {
        http_response_t response = {0};
        response.data = heap_caps_malloc(MEETING_RESPONSE_CAPACITY,
                                         MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!response.data) response.data = malloc(MEETING_RESPONSE_CAPACITY);
        if (!response.data) {
            return ESP_ERR_NO_MEM;
        }
        response.capacity = MEETING_RESPONSE_CAPACITY;
        char authorization[128];
        uint32_t cellular_response_len = 0;
        snprintf(authorization, sizeof(authorization), "Bearer %s", s_gateway_token);
        int64_t started_us = esp_timer_get_time();
        device_connectivity_stream_request_t cellular_request = {
            .request = {
                .method = "PUT", .url = url, .content_type = "application/octet-stream",
                .authorization = authorization, .extra_header_name = "X-Chunk-SHA256",
                .extra_header_value = sha256_hex, .body_len = (uint32_t)length,
                .response = response.data, .response_capacity = (uint32_t)response.capacity,
                .response_len = &cellular_response_len, .status_code = &response.status,
                .truncated = &response.truncated, .timeout_ms = 60000,
                .cancellation_owner = (const void *)xTaskGetCurrentTaskHandle(),
            },
            .body_reader = read_meeting_chunk_body, .body_reader_context = file,
            .stream_buffer = buffer, .stream_buffer_size = (uint32_t)buffer_size,
        };
        esp_err_t err = device_status_to_platform_error(
            device_connectivity_cellular_http_stream_request(&cellular_request));
        if (meeting_task_stop_requested() && err == ESP_OK) err = ESP_ERR_INVALID_STATE;
        response.len = cellular_response_len;
        uint32_t total_ms = (uint32_t)((esp_timer_get_time() - started_us) / 1000);
        ESP_LOGI(TAG, "meeting chunk %d upload bytes=%u connection=ML307 total=%ums status=%d err=%s",
                 index, (unsigned)length, (unsigned)total_ms, response.status,
                 esp_err_to_name(err));
        if (publish_progress && err == ESP_OK) {
            app_ui_show_upload_progress(completed_before + length, total_bytes,
                                            "正在上传录音");
        }
        if (err == ESP_OK && response.status != 200 && response.status != 201) err = ESP_FAIL;
        response_release(&response);
        return err;
    }
    if (!s_http_mutex || xSemaphoreTake(s_http_mutex, pdMS_TO_TICKS(35000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    // Hardware AES needs short-lived DMA-capable memory only when a new TLS
    // transport is opened. A retained keep-alive connection already owns its
    // crypto buffers, so avoid reserving another 16 KiB at every chunk.
    bool reused_connection = reusable_client && *reusable_client;
    void *tls_internal_reserve = NULL;
    if (!reused_connection) {
        tls_internal_reserve = heap_caps_malloc(MEETING_INTERNAL_TLS_RESERVE,
                                                MALLOC_CAP_INTERNAL |
                                                MALLOC_CAP_DMA |
                                                MALLOC_CAP_8BIT);
        if (!tls_internal_reserve) {
            ESP_LOGE(TAG, "meeting TLS reserve failed: need=%u", (unsigned)MEETING_INTERNAL_TLS_RESERVE);
            log_heap_snapshot("meeting-tls-reserve-fail");
            xSemaphoreGive(s_http_mutex);
            return ESP_ERR_NO_MEM;
        }
    }
    http_response_t response = {0};
    response.data = heap_caps_malloc(MEETING_RESPONSE_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!response.data) response.data = malloc(MEETING_RESPONSE_CAPACITY);
    if (!response.data) {
        heap_caps_free(tls_internal_reserve);
        xSemaphoreGive(s_http_mutex);
        return ESP_ERR_NO_MEM;
    }
    response.capacity = MEETING_RESPONSE_CAPACITY;
    response.data[0] = '\0';
    esp_http_client_handle_t client = reused_connection ? *reusable_client : NULL;
    esp_err_t err = ESP_OK;
    if (!client) {
        esp_http_client_config_t config = {
            .url = url, .event_handler = on_http_event, .user_data = &response,
            .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
            .keep_alive_enable = true,
        };
        client = esp_http_client_init(&config);
    }
    if (client) err = configure_meeting_chunk_client(client, url, sha256_hex, &response);
    if (!client || err != ESP_OK) {
        if (client) esp_http_client_cleanup(client);
        if (reusable_client) *reusable_client = NULL;
        heap_caps_free(tls_internal_reserve);
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return client ? err : ESP_ERR_NO_MEM;
    }
    meeting_task_set_active_http_client(client);
    int64_t request_started_us = esp_timer_get_time();
    err = esp_http_client_open(client, (int)length);
    bool stale_connection_retry = false;
    if (err != ESP_OK && reused_connection) {
        // A server or intermediary can expire an otherwise valid keep-alive
        // socket between chunks. PUT is idempotent for this indexed/hash-checked
        // endpoint, and no body was written when open failed, so retry once on
        // a fresh TLS transport instead of aborting the resumable upload.
        ESP_LOGW(TAG, "meeting chunk %d stale keep-alive open failed: %s; retrying fresh TLS",
                 index, esp_err_to_name(err));
        stale_connection_retry = true;
        meeting_task_set_active_http_client(NULL);
        esp_http_client_cleanup(client);
        client = NULL;
        if (reusable_client) *reusable_client = NULL;
        tls_internal_reserve = heap_caps_malloc(MEETING_INTERNAL_TLS_RESERVE,
                                                MALLOC_CAP_INTERNAL |
                                                MALLOC_CAP_DMA |
                                                MALLOC_CAP_8BIT);
        if (!tls_internal_reserve) {
            err = ESP_ERR_NO_MEM;
        } else {
            esp_http_client_config_t retry_config = {
                .url = url, .event_handler = on_http_event, .user_data = &response,
                .timeout_ms = 60000, .crt_bundle_attach = esp_crt_bundle_attach,
                .keep_alive_enable = true,
            };
            client = esp_http_client_init(&retry_config);
            err = client ? configure_meeting_chunk_client(client, url, sha256_hex, &response)
                         : ESP_ERR_NO_MEM;
            if (client) meeting_task_set_active_http_client(client);
            if (err == ESP_OK) err = esp_http_client_open(client, (int)length);
        }
    }
    uint32_t open_ms = (uint32_t)((esp_timer_get_time() - request_started_us) / 1000);
    heap_caps_free(tls_internal_reserve);
    tls_internal_reserve = NULL;
    if (!client) {
        ESP_LOGE(TAG,
                 "meeting chunk %d fresh TLS retry setup failed after reused connection: %s",
                 index, esp_err_to_name(err));
        response_release(&response);
        xSemaphoreGive(s_http_mutex);
        return err;
    }
    size_t remaining = length;
    while (err == ESP_OK && remaining > 0) {
        if (meeting_task_stop_requested()) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
        size_t wanted = remaining < buffer_size ? remaining : buffer_size;
        size_t count = fread(buffer, 1, wanted, file);
        if (count != wanted) {
            err = ESP_FAIL;
            break;
        }
        size_t written = 0;
        while (written < count) {
            int result = esp_http_client_write(client, (const char *)buffer + written, count - written);
            if (result <= 0) {
                err = ESP_FAIL;
                break;
            }
            written += (size_t)result;
        }
        remaining -= count;
        // Repainting the complete 360x360 LCD after each 16 KiB TLS write
        // overlaps QSPI DMA, PSRAM traffic and Wi-Fi for the whole upload. On
        // this board that causes a repeatable brownout/watchdog-style reset.
        // Update once per 256 KiB (and at completion) instead.
        size_t transferred = length - remaining;
        if (publish_progress &&
            (remaining == 0 || (transferred % (256u * 1024u)) < count)) {
            app_ui_show_upload_progress(completed_before + transferred,
                                            total_bytes, "正在上传录音");
        }
        // A multi-megabyte HTTPS PUT can otherwise monopolize this task long
        // enough to starve the idle watchdog on a slow Wi-Fi link.
        vTaskDelay(1);
    }
    if (err == ESP_OK) {
        int headers = esp_http_client_fetch_headers(client);
        if (headers < 0) err = ESP_FAIL;
        while (err == ESP_OK && !esp_http_client_is_complete_data_received(client)) {
            if (meeting_task_stop_requested()) {
                err = ESP_ERR_INVALID_STATE;
                break;
            }
            int count = esp_http_client_read(client, (char *)buffer, buffer_size);
            if (count < 0) err = ESP_FAIL;
            if (count <= 0 && !esp_http_client_is_complete_data_received(client)) err = ESP_FAIL;
        }
    }
    response.status = esp_http_client_get_status_code(client);
    if (err == ESP_OK && (response.status < 200 || response.status >= 300)) {
        ESP_LOGE(TAG, "meeting chunk %d rejected: status=%d body=%s",
                 index, response.status, response.data ? response.data : "");
        err = ESP_FAIL;
    }
    uint32_t total_ms = (uint32_t)((esp_timer_get_time() - request_started_us) / 1000);
    // Once the full response body has been drained, ESP-IDF leaves a
    // keep-alive transport in CONNECTED state. Retain that handle for the next
    // same-origin PUT; discard any failed/closed parser state immediately.
    bool keep_client = err == ESP_OK &&
                       esp_http_client_is_complete_data_received(client) &&
                       reusable_client;
    esp_http_client_set_user_data(client, NULL);
    meeting_task_set_active_http_client(NULL);
    if (keep_client) {
        *reusable_client = client;
    } else {
        esp_http_client_cleanup(client);
        if (reusable_client) *reusable_client = NULL;
    }
    xSemaphoreGive(s_http_mutex);
    ESP_LOGI(TAG,
             "meeting chunk %d upload bytes=%u connection=%s open=%ums total=%ums status=%d err=%s keep=%s",
             index, (unsigned)length,
             stale_connection_retry ? "reused->new" : reused_connection ? "reused" : "new",
             (unsigned)open_ms, (unsigned)total_ms, response.status,
             esp_err_to_name(err), keep_client ? "yes" : "no");
    response_release(&response);
    return err;
}

static esp_err_t create_meeting_recording(char recording_id[MEETING_RECORDING_ID_CAPACITY]) {
    char payload[192];
    int length = snprintf(payload, sizeof(payload),
                          "{\"title\":\"硬件会议录音\",\"purpose\":\"\","
                          "\"conversation_id\":\"%s\",\"content_type\":\"audio/wav\"}",
                          CONFIG_MACLAW_CONVERSATION_ID);
    if (length <= 0 || length >= (int)sizeof(payload)) return ESP_ERR_INVALID_SIZE;
    http_response_t response;
    esp_err_t err = request_with_capacity("POST", s_meeting_base_path, "application/json",
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
    if (!id || strlen(id) >= MEETING_RECORDING_ID_CAPACITY) err = ESP_ERR_INVALID_RESPONSE;
    else strlcpy(recording_id, id, MEETING_RECORDING_ID_CAPACITY);
    cJSON_Delete(json);
    response_release(&response);
    return err;
}

static esp_err_t get_meeting_status(const char *recording_id, char *status, size_t status_cap) {
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 8];
    int length = snprintf(path, sizeof(path), "%s/%s", s_meeting_base_path, recording_id);
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
    char path[MEETING_BASE_PATH_CAPACITY + MEETING_RECORDING_ID_CAPACITY + 32];
    int length = snprintf(path, sizeof(path), "%s/%s/%s", s_meeting_base_path, recording_id, action);
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

static esp_err_t upload_pending_meeting(bool publish_state) {
    struct stat info;
    if (!s_storage_mounted || stat(MEETING_WAV_PATH, &info) != 0 || info.st_size <= 44) {
        return ESP_ERR_NOT_FOUND;
    }
    FILE *file = fopen(MEETING_WAV_PATH, "rb+");
    if (!file) return ESP_FAIL;
    size_t file_size = (size_t)info.st_size;
    esp_err_t header_err = ensure_meeting_wav_header(file, file_size);
    if (header_err != ESP_OK) {
        ESP_LOGE(TAG, "retained meeting WAV is not recoverable: %s",
                 esp_err_to_name(header_err));
        fclose(file);
        return header_err;
    }
    uint8_t *buffer = heap_caps_malloc(MEETING_IO_BUFFER_SIZE, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!buffer) buffer = malloc(MEETING_IO_BUFFER_SIZE);
    if (!buffer) {
        fclose(file);
        return ESP_ERR_NO_MEM;
    }
    char recording_id[MEETING_RECORDING_ID_CAPACITY];
    strlcpy(recording_id, s_meeting_recording_id, sizeof(recording_id));
    int next_chunk = s_meeting_next_chunk;
    int phase = s_meeting_phase;
    esp_err_t err = ESP_OK;
    if (recording_id[0] != '\0') {
        char status[20] = {0};
        esp_err_t status_err = get_meeting_status(recording_id, status, sizeof(status));
        if (status_err == ESP_ERR_NOT_FOUND) {
            recording_id[0] = '\0';
            next_chunk = 0;
            phase = 0;
            err = save_meeting_recovery(true, "", 0, 0);
        } else if (status_err != ESP_OK) {
            err = status_err;
        } else if (!strcmp(status, "processing") || !strcmp(status, "ready")) {
            phase = 2;
            next_chunk = (int)((size_t)info.st_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (!strcmp(status, "uploaded") || !strcmp(status, "failed")) {
            phase = 1;
            next_chunk = (int)((size_t)info.st_size + s_meeting_chunk_size - 1) /
                         (int)s_meeting_chunk_size;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        } else if (strcmp(status, "uploading")) {
            err = ESP_ERR_INVALID_STATE;
        }
    }
    if (err == ESP_OK && recording_id[0] == '\0') {
        err = create_meeting_recording(recording_id);
        if (err == ESP_OK) err = save_meeting_recovery(true, recording_id, 0, 0);
        next_chunk = 0;
        phase = 0;
    }
    int chunks = (int)((file_size + s_meeting_chunk_size - 1) / s_meeting_chunk_size);
    esp_http_client_handle_t meeting_upload_client = NULL;
    for (int index = next_chunk; err == ESP_OK && index < chunks; ++index) {
        if (meeting_task_stop_requested()) {
            err = ESP_ERR_INVALID_STATE;
            break;
        }
        size_t offset = (size_t)index * s_meeting_chunk_size;
        size_t length = file_size - offset;
        if (length > s_meeting_chunk_size) length = s_meeting_chunk_size;
        char chunk_hash[65];
        err = hash_file_range(file, offset, length, buffer, MEETING_IO_BUFFER_SIZE, chunk_hash);
        if (err == ESP_OK) {
            if (publish_state) {
                app_ui_show_upload_progress(offset, file_size, "正在上传录音");
            }
            err = stream_meeting_chunk(recording_id, index, file, offset, length,
                                       chunk_hash, buffer, MEETING_IO_BUFFER_SIZE,
                                       offset, file_size, publish_state,
                                       &meeting_upload_client);
        }
        if (err == ESP_OK) {
            next_chunk = index + 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
            if (publish_state) {
                app_ui_show_upload_progress(offset + length, file_size, "正在上传录音");
            }
        }
        if (!publish_state && err == ESP_OK && s_foreground_http_requested) {
            // Recovery metadata is already durable at this chunk boundary. End
            // this pass cleanly and let the foreground command acquire HTTP;
            // the reconnect/resume path continues from next_chunk later.
            ESP_LOGI(TAG, "background meeting resume yielded after chunk %d", index);
            err = ESP_ERR_TIMEOUT;
        }
    }
    if (meeting_upload_client) {
        meeting_task_set_active_http_client(NULL);
        esp_http_client_cleanup(meeting_upload_client);
        meeting_upload_client = NULL;
    }
    char whole_hash[65];
    if (err == ESP_OK && !meeting_task_stop_requested() && phase < 1) {
        if (publish_state) meeting_set_state(MEETING_FINALIZING);
        if (publish_state) app_ui_show_upload_progress(file_size, file_size, "正在校验录音");
        err = hash_file_range(file, 0, file_size, buffer, MEETING_IO_BUFFER_SIZE, whole_hash);
        if (err == ESP_OK) {
            uint32_t pcm_bytes = file_size > 44 ? (uint32_t)(file_size - 44) : 0;
            double duration = (double)pcm_bytes / (MEETING_SAMPLE_RATE * 2.0);
            char payload[192];
            int length = snprintf(payload, sizeof(payload),
                                  "{\"chunks\":%d,\"sha256\":\"%s\",\"duration_sec\":%.3f}",
                                  chunks, whole_hash, duration);
            if (length <= 0 || length >= (int)sizeof(payload)) err = ESP_ERR_INVALID_SIZE;
            else err = post_meeting_action(recording_id, "complete", payload, 200, 200);
        }
        if (err == ESP_OK) {
            phase = 1;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && !meeting_task_stop_requested() && phase >= 1) {
        char status[20] = {0};
        if (get_meeting_status(recording_id, status, sizeof(status)) == ESP_OK &&
            (!strcmp(status, "processing") || !strcmp(status, "ready"))) {
            phase = 2;
            (void)save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    if (err == ESP_OK && !meeting_task_stop_requested() && phase < 2) {
        if (publish_state) meeting_set_state(MEETING_PROCESSING);
        if (publish_state) app_ui_show_upload_progress(file_size, file_size, "正在提交处理");
        char payload[48];
        int length = snprintf(payload, sizeof(payload), "{\"mode\":\"%s\"}", s_meeting_process_mode);
        if (length <= 0 || length >= (int)sizeof(payload)) err = ESP_ERR_INVALID_SIZE;
        else err = post_meeting_action(recording_id, "process", payload, 200, 202);
        if (err == ESP_OK) {
            phase = 2;
            err = save_meeting_recovery(true, recording_id, next_chunk, phase);
        }
    }
    free(buffer);
    fclose(file);
    if (err == ESP_OK && meeting_task_stop_requested()) err = ESP_ERR_INVALID_STATE;
    if (err == ESP_OK) {
        err = clear_meeting_recovery(true);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "meeting delivered but local cleanup failed: %s", esp_err_to_name(err));
        }
    }
    return err;
}

static void meeting_task(void *arg) {
    meeting_task_context_t *context = arg;
    uint32_t operation_generation = 0;
    bool resume_only = false;
    if (!context) {
        vTaskDelete(NULL);
        return;
    }
    /* Publish s_meeting_task/s_meeting_task_running before this worker can
     * complete a fast local failure.  Otherwise a cross-core task create can
     * leave the creator writing a stale handle after the worker cleared it. */
    if (!s_meeting_task_start_gate ||
        xSemaphoreTake(s_meeting_task_start_gate, portMAX_DELAY) != pdTRUE) {
        free(context);
        context = NULL;
        ESP_LOGW(TAG, "meeting task start gate unavailable");
        goto finish;
    }
    operation_generation = context->generation;
    resume_only = context->resume_only;
    free(context);
    context = NULL;
    if (meeting_task_stop_requested()) goto finish;
    if (!resume_only && !meeting_operation_is_current(operation_generation)) {
        ESP_LOGW(TAG, "stale meeting worker discarded: generation=%lu",
                 (unsigned long)operation_generation);
        goto finish;
    }
    if (resume_only) {
        // Recovery is a background transfer. It must not take over the pet UI,
        // publish an active meeting state, or block a new short voice command.
        ESP_LOGI(TAG, "background meeting resume started");
    } else {
        /* Do not publish a foreground meeting surface unless this worker still
         * owns the operation slot.  The current interaction lock makes this
         * normally true, but the explicit guard keeps UI ownership correct as
         * future cancellation and restart paths become asynchronous. */
        if (!meeting_operation_is_current(operation_generation)) goto finish;
        meeting_set_state(MEETING_STARTING);
        FILE *file = fopen(MEETING_WAV_PATH, "wb+");
        if (!file) {
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            app_ui_show_text("录音失败", "无法创建录音文件");
            goto finish;
        }
        uint8_t header[44];
        build_meeting_wav_header(header, 0);
        esp_err_t start_err = ESP_OK;
        if (fwrite(header, 1, sizeof(header), file) != sizeof(header)) {
            start_err = ESP_FAIL;
            ESP_LOGE(TAG, "meeting start: WAV header write failed");
        }
        if (start_err == ESP_OK) {
            start_err = save_meeting_recovery(true, "", 0, 0);
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: recovery metadata failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err == ESP_OK) {
            start_err = device_status_to_esp_err(device_audio_stream_start());
            if (start_err != ESP_OK) {
                ESP_LOGE(TAG, "meeting start: audio stream failed: %s",
                         esp_err_to_name(start_err));
            }
        }
        if (start_err != ESP_OK) {
            fclose(file);
            // Startup produced no PCM. Clear the marker and placeholder so a
            // transient microphone/mutex failure cannot permanently turn every
            // later double tap into a bogus retained-file recovery attempt.
            (void)clear_meeting_recovery(true);
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            app_ui_show_text("录音失败", "麦克风或存储不可用");
            goto finish;
        }
        int16_t samples[512];
        uint64_t total_samples = 0;
        s_meeting_elapsed_seconds = 0;
        uint32_t last_elapsed = UINT32_MAX;
        bool last_paused = false;
        meeting_set_state(MEETING_RECORDING);
        pet("listening");
        app_ui_set_recording_mode(true);
        app_ui_set_recording_visual(true, false, 0);
        while (s_meeting_state == MEETING_RECORDING || s_meeting_state == MEETING_PAUSED) {
            if (meeting_task_stop_requested()) {
                meeting_set_state(MEETING_ERROR);
                break;
            }
            if (!meeting_operation_is_current(operation_generation)) {
                meeting_set_state(MEETING_ERROR);
                break;
            }
            uint32_t count = 0;
            uint16_t level = 0;
            esp_err_t capture = device_status_to_esp_err(
                device_audio_stream_read(samples, 512, &count, &level));
            if (capture != ESP_OK) {
                meeting_set_state(MEETING_ERROR);
                break;
            }
            bool paused = s_meeting_state == MEETING_PAUSED;
            if (!paused && count > 0) {
                // A paused meeting keeps the I2S reader alive to retain bus
                // ownership, but its samples are neither persisted nor shown.
                // Pushing them into the renderer made resume reveal a strip of
                // audio captured while the user believed recording was paused.
                app_ui_push_recording_pcm(samples, count);
                if (fwrite(samples, sizeof(int16_t), count, file) != count) {
                    meeting_set_state(MEETING_ERROR);
                    break;
                }
                total_samples += count;
            }
            uint32_t elapsed = (uint32_t)(total_samples / MEETING_SAMPLE_RATE);
            s_meeting_elapsed_seconds = elapsed;
            // While paused, Bread Compact keeps the frozen meter exactly as it
            // was at the pause boundary. Passing a synthetic zero level through
            // the normal attack/release path made EchoEar's supposedly paused
            // waveform visibly decay for every discarded 512-sample block.
            // The visual-state transition below already recolours the frozen
            // bars and applies the paused quiet display treatment.
            if (!paused) app_ui_set_audio_level(level, elapsed);
            // The timer deliberately freezes while paused, so elapsed alone
            // cannot represent this state transition. Publish it immediately
            // and let the shared recorder switch its rule, copy and waveform.
            if (elapsed != last_elapsed || paused != last_paused) {
                app_ui_set_recording_visual(true, paused, elapsed);
                last_elapsed = elapsed;
                last_paused = paused;
            }
        }
        device_audio_stream_stop();
        meeting_state_t stopped_state = s_meeting_state;
        esp_err_t finalize_err = total_samples > 0
                                     ? finalize_meeting_wav(file, total_samples)
                                     : ESP_ERR_INVALID_SIZE;
        if (!meeting_operation_is_current(operation_generation)) {
            fclose(file);
            goto finish;
        }
        if (stopped_state == MEETING_FINALIZING && finalize_err == ESP_OK) {
            fclose(file);
            meeting_set_state(MEETING_UPLOADING);
            // Meeting delivery has its own status surface. Reusing the normal
            // command "thinking" pet made a completed meeting look like a
            // short voice command and allowed ambient frames to replace it.
            s_command_display_locked = true;
            app_ui_set_command_display_lock(true);
            app_ui_set_recording_visual(false, false, 0);
            app_ui_show_upload_progress(0, 1, "正在准备上传");
        } else {
            fclose(file);
            if (total_samples == 0) {
                // There is no recoverable audio. Leaving the pending marker set
                // would make every later double press retry a 44-byte placeholder
                // forever, preventing the user from starting a fresh meeting.
                (void)clear_meeting_recovery(true);
            } else if (finalize_err == ESP_OK) {
                ESP_LOGW(TAG, "partial meeting finalized for recovery: samples=%llu",
                         (unsigned long long)total_samples);
            } else {
                // Keep both PCM and recovery metadata. upload_pending_meeting()
                // will retry header repair before it sends any bytes.
                ESP_LOGE(TAG, "partial meeting header finalize failed; preserving PCM: %s",
                         esp_err_to_name(finalize_err));
            }
            app_ui_set_recording_visual(false, false, 0);
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            app_ui_show_text("录音失败", "文件已保留待恢复");
            goto finish;
        }
    }
    // MultiNet keeps its model, task stack and inference buffers alive even
    // while microphone capture is merely paused. On this ESP32-S3 that leaves
    // the internal DMA heap too fragmented for hardware AES during HTTPS PUT
    // (mbedTLS reports -0x0084). Fully unload it for delivery, then restore the
    // hands-free listener after the HTTP/NVS work has finished.
    log_heap_snapshot("meeting-upload-before-wake-stop");
    esp_err_t wake_stop_err = audio_wake_word_stop();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before meeting upload: %s",
                 esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("meeting-upload-after-wake-stop");

    if (meeting_task_stop_requested()) goto finish;
    if (!resume_only && !meeting_operation_is_current(operation_generation)) goto finish;
    esp_err_t upload_err = upload_pending_meeting(!resume_only);

    if (upload_err == ESP_OK) {
        if (!resume_only && meeting_operation_is_current(operation_generation)) {
            meeting_set_state(MEETING_DONE);
            pet("done");
            app_ui_show_text("会议记录已保存", "可在文稿库中查看");
            /* A rollback must not spend the whole result dwell time waiting
             * for this worker. The same notification used by stop wakes this
             * cosmetic delay without changing the durable upload outcome. */
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000));
            s_command_display_locked = false;
            app_ui_set_command_display_lock(false);
            pet("idle");
        } else {
            ESP_LOGI(TAG, "background meeting resume delivered");
        }
    } else {
        ESP_LOGE(TAG, "meeting upload pass failed: %s resume=%s id=%s next=%ld phase=%ld",
                 esp_err_to_name(upload_err), resume_only ? "yes" : "no",
                 s_meeting_recording_id,
                 (long)s_meeting_next_chunk, (long)s_meeting_phase);
        log_heap_snapshot("meeting-upload-fail");
        if (!resume_only && meeting_operation_is_current(operation_generation)) {
            meeting_set_state(MEETING_ERROR);
            pet("alert");
            app_ui_show_text("上传未完成", "联网后将自动续传");
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(2200));
            s_command_display_locked = false;
            app_ui_set_command_display_lock(false);
            pet("idle");
        } else {
            ESP_LOGW(TAG, "background meeting resume deferred until next reconnect");
        }
    }
finish:
    /* The meeting has no user cancel action once finalization/upload begins,
     * but every success/error/recovery exit still claims the same terminal
     * token.  This makes a late worker unable to publish a second terminal
     * outcome after another foreground operation owns the slot. */
    if (!resume_only) (void)operation_context_commit_terminal(operation_generation);
    taskENTER_CRITICAL(&s_task_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    if (s_meeting_task == self) s_meeting_task = NULL;
    s_meeting_task_running = false;
    device_power_lease_t meeting_lease = s_meeting_power_lease;
    s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only) device_power_lease_release(meeting_lease);
    if (!resume_only) {
        // Error exits before the normal success/deferred UI cleanup still
        // need to release the display for the ambient screen.
        s_command_display_locked = false;
        app_ui_set_command_display_lock(false);
        if (s_interaction_lock) xSemaphoreGive(s_interaction_lock);
    }
    meeting_task_set_active_http_client(NULL);
    if (s_meeting_task_stopped) xSemaphoreGive(s_meeting_task_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_AUDIO,
                                                 (void *)self, 10);
    schedule_wake_restart();
    vTaskDelete(NULL);
}

static esp_err_t stop_meeting_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task_stop_requested = true;
    task = s_meeting_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    /* The meeting worker owns the audio stream mutex.  Do not release it from
     * this coordinator: FreeRTOS mutex ownership is task-local and a cross-
     * task give would corrupt the next foreground capture.  The worker checks
     * the token after its bounded read and releases the stream itself. */
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_meeting_task_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_meeting_task_client_mutex, pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_meeting_task_active_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "meeting HTTP cancel failed: %s", esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_meeting_task_client_mutex);
    }
    if (device_connectivity_is_active_cellular() &&
        device_connectivity_cancel_cellular_requests_for_owner((const void *)task)) {
        ESP_LOGI(TAG, "meeting ML307 HTTP request cancellation requested");
    }
    if (s_meeting_task_start_gate) xSemaphoreGive(s_meeting_task_start_gate);
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_meeting_task_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_task_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_AUDIO, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "meeting worker stopped; retained audio remains resumable");
    return ESP_OK;
}

static esp_err_t stop_meeting_task_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_meeting_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_task(timeout_ms);
}

static bool start_meeting_task(bool resume_only) {
    if (!s_storage_mounted) {
        ESP_LOGW(TAG, "meeting start refused: storage is not mounted");
        return false;
    }
    if (!resume_only && !s_meeting_available) {
        ESP_LOGW(TAG, "meeting start refused: capability is unavailable");
        return false;
    }
    if (!s_gateway_token[0]) {
        ESP_LOGW(TAG, "meeting start refused: device is not paired");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_meeting_task_running) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    s_meeting_task_running = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!s_meeting_task_start_gate || !s_meeting_task_stopped || !s_meeting_task_client_mutex) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "meeting task lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_task_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!resume_only && (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, pdMS_TO_TICKS(1500)) != pdTRUE)) {
        ESP_LOGI(TAG, "meeting start deferred: foreground interaction owns the lock");
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    device_operation_context_t operation = {0};
    if (!resume_only) {
        device_status_t operation_status = operation_context_begin(
            DEVICE_OPERATION_KIND_MEETING_RECORDING, 0, &operation);
        if (operation_status != DEVICE_STATUS_OK) {
            xSemaphoreGive(s_interaction_lock);
            taskENTER_CRITICAL(&s_task_state_lock);
            s_meeting_task_running = false;
            taskEXIT_CRITICAL(&s_task_state_lock);
            ESP_LOGI(TAG, "meeting operation admission rejected: status=%d", (int)operation_status);
            return false;
        }
    }
    if (!resume_only) app_ui_cancel_ready_prompt();
    device_power_lease_t meeting_lease = DEVICE_POWER_LEASE_INVALID;
    if (!resume_only) {
        device_status_t lease_status = device_power_lease_acquire(
            DEVICE_POWER_LEASE_OWNER_MEETING_RECORDING, &meeting_lease);
        if (lease_status != DEVICE_STATUS_OK) {
            (void)operation_context_commit_terminal(operation.generation);
            xSemaphoreGive(s_interaction_lock);
            taskENTER_CRITICAL(&s_task_state_lock);
            s_meeting_task_running = false;
            taskEXIT_CRITICAL(&s_task_state_lock);
            ESP_LOGW(TAG, "meeting start rejected: power lease unavailable status=%d",
                     (int)lease_status);
            return false;
        }
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_power_lease = meeting_lease;
        taskEXIT_CRITICAL(&s_task_state_lock);
    }
    meeting_task_context_t *context = calloc(1, sizeof(*context));
    if (!context) {
        device_power_lease_release(meeting_lease);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!resume_only) (void)operation_context_commit_terminal(operation.generation);
        if (!resume_only) xSemaphoreGive(s_interaction_lock);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_running = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        return false;
    }
    *context = (meeting_task_context_t){
        .generation = operation.generation,
        .resume_only = resume_only,
    };
    TaskHandle_t handle = NULL;
    // Meeting startup writes recovery metadata to NVS before the microphone
    // begins.  Flash writes disable the cache, so this task must keep its
    // stack in internal RAM; a PSRAM stack causes a cache-disabled assertion
    // and an apparent reboot on a double tap.
    // At steady state the offline speech model leaves roughly 21 KB internal
    // heap, whose largest contiguous block is only about 9 KB. A 12 KB stack
    // therefore cannot be created even though total RAM appears sufficient.
    // The worker's large audio/network buffers are heap/PSRAM allocations;
    // 8 KB internal stack is enough for its bounded local frames and keeps NVS
    // writes safe while flash cache is disabled.
    // Foreground and resumed uploads both persist progress to NVS. Flash
    // commits disable the external-memory cache, therefore both modes need an
    // internal stack. A PSRAM stack here reset the MCU just after chunk PUT.
    BaseType_t created = xTaskCreate(meeting_task, "maclaw_meeting", 8192,
                                     context, 5, &handle);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_task = created == pdPASS ? handle : NULL;
    if (created != pdPASS) s_meeting_task_running = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        free(context);
        device_power_lease_release(meeting_lease);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_power_lease = DEVICE_POWER_LEASE_INVALID;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (!resume_only) (void)operation_context_commit_terminal(operation.generation);
        if (!resume_only) xSemaphoreGive(s_interaction_lock);
        log_heap_snapshot("meeting-task-create-fail");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_AUDIO,
        .name = "meeting_worker",
        .context = (void *)handle,
        .stop = stop_meeting_task_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting worker: %s", esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_task_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_meeting_task_start_gate);
        (void)stop_meeting_task(500);
        return false;
    }
    xSemaphoreGive(s_meeting_task_start_gate);
    return true;
}

static bool meeting_capability_refresh_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_meeting_capability_refresh_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

// A Hub can be upgraded while the watch remains online. Meeting capability is
// negotiated during handshake, so do not make the user reboot the device just
// because it still holds an older, capability-less response in RAM. The
// refresh runs outside the input scan task because TLS can take several
// seconds; after a successful refresh it retries the original double-tap.
//
// It is deliberately a small CONNECTIVITY owner rather than a disguised
// meeting-worker owner: stopping it cancels only its one handshake/retry pass.
// A meeting task it has already started retains its independent NVS/audio/HTTP
// recovery contract and is never force-deleted by this lifecycle slice.
static void meeting_capability_refresh_task(void *arg) {
    (void)arg;
    if (!s_meeting_capability_refresh_start_gate ||
        xSemaphoreTake(s_meeting_capability_refresh_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "meeting capability refresh start gate unavailable");
        goto finish;
    }
    if (meeting_capability_refresh_stop_requested()) goto finish;
    ESP_LOGI(TAG, "refreshing gateway handshake for meeting recording");
    app_ui_show_text("会议录音", "正在检查网关支持");
    esp_err_t err = gateway_handshake(false);
    if (!meeting_capability_refresh_stop_requested() && err == ESP_OK && s_meeting_available) {
        // A just-finished touch/voice action can still own the foreground
        // mutex for a moment.  This task is deliberately off the input scan
        // path, so wait and retry instead of turning that harmless race into
        // a visible recording failure.
        bool started = false;
        for (unsigned retry = 0; retry < 32 && !started &&
                                  !meeting_capability_refresh_stop_requested(); ++retry) {
            started = start_meeting_task(false);
            if (!started) {
                if (retry == 0) {
                    app_ui_show_text("会议录音", "正在等待设备就绪");
                }
                (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(250));
            }
        }
        if (!started && !meeting_capability_refresh_stop_requested()) {
            pet("alert");
            char meeting_retry_hint[72];
            snprintf(meeting_retry_hint, sizeof(meeting_retry_hint),
                     "请稍后再次双击%s", device_input_primary_interaction_label());
            app_ui_show_text("录音启动失败", meeting_retry_hint);
        }
    } else if (!meeting_capability_refresh_stop_requested()) {
        ESP_LOGW(TAG, "meeting capability refresh failed: err=%s available=%s",
                 esp_err_to_name(err), s_meeting_available ? "yes" : "no");
        pet("alert");
        app_ui_show_text("会议录音不可用", "请检查网关连接");
    }
finish:
    taskENTER_CRITICAL(&s_task_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    if (s_meeting_capability_refresh_task == self) {
        s_meeting_capability_refresh_task = NULL;
    }
    s_meeting_capability_refresh_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_meeting_capability_refresh_stopped) {
        xSemaphoreGive(s_meeting_capability_refresh_stopped);
    }
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDeleteWithCaps(NULL);
}

static esp_err_t stop_meeting_capability_refresh_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_capability_refresh_stop_requested = true;
    task = s_meeting_capability_refresh_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* The refresh uses the normal HTTP lane, but publishes its active ESP
     * client only for the perform interval. Hold the guard through cancel so
     * request cleanup cannot race a stale client pointer. */
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_meeting_capability_refresh_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_meeting_capability_refresh_client_mutex,
                       pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_meeting_capability_refresh_active_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "meeting capability refresh HTTP cancel returned: %s",
                         esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_meeting_capability_refresh_client_mutex);
    }
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_meeting_capability_refresh_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_meeting_capability_refresh_stopped,
                       pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "meeting capability refresh stopped");
    return ESP_OK;
}

static esp_err_t stop_meeting_capability_refresh_registry_entry(void *context,
                                                                  uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_meeting_capability_refresh_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_meeting_capability_refresh_task(timeout_ms);
}

static bool refresh_meeting_capability(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool already_refreshing = s_meeting_capability_refresh_task != NULL ||
                              s_meeting_capability_refresh_starting;
    if (!already_refreshing) s_meeting_capability_refresh_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (already_refreshing) return true;
    if (!s_meeting_capability_refresh_start_gate ||
        !s_meeting_capability_refresh_stopped ||
        !s_meeting_capability_refresh_client_mutex) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_capability_refresh_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "meeting capability refresh lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_meeting_capability_refresh_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_capability_refresh_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    TaskHandle_t handle = NULL;
    // gateway_handshake() persists fresh ambient data in NVS.  Flash writes
    // temporarily disable the cache, so this task's stack must remain in
    // internal RAM; a PSRAM stack causes esp_task_stack_is_sane_cache_disabled
    // to assert and looks like a reboot immediately after a double tap.
    BaseType_t created = xTaskCreate(meeting_capability_refresh_task,
                                     "maclaw_meeting_cap", 8192, NULL, 4,
                                     &handle);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_meeting_capability_refresh_task = created == pdPASS ? handle : NULL;
    if (created != pdPASS) s_meeting_capability_refresh_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        ESP_LOGE(TAG, "cannot start meeting capability refresh task");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "meeting_capability_refresh",
        .context = (void *)handle,
        .stop = stop_meeting_capability_refresh_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register meeting capability refresh: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_meeting_capability_refresh_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_meeting_capability_refresh_start_gate);
        (void)stop_meeting_capability_refresh_task(500);
        return false;
    }
    xSemaphoreGive(s_meeting_capability_refresh_start_gate);
    return true;
}

static void interaction_task(void *arg) {
    uint32_t interaction_generation = (uint32_t)(uintptr_t)arg;
    /* xTaskCreate may run this worker before the creator publishes
     * s_interaction_task on the other core. Do not let an immediate capture
     * failure complete against a NULL/stale owner and strand the admission
     * token. The creator releases this one-shot gate only after publishing
     * the handle under the same lock. Later notifications retain their normal
     * result/progress/cancellation meaning. */
    if (!s_interaction_start_gate ||
        xSemaphoreTake(s_interaction_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "interaction start gate unavailable");
        goto finish;
    }
    if (interaction_stop_requested()) goto finish;
    int64_t interaction_started_us = esp_timer_get_time();
    // The wake-phrase path creates this worker from inside MultiNet, while a
    // panel tap unloads it before task creation. Converge both paths here so
    // command HTTPS upload always has enough contiguous DMA RAM for TLS AES.
    esp_err_t wake_stop_err = audio_wake_word_stop();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before voice capture: %s",
                 esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("voice-after-wake-stop");
    // Keep capture screen-neutral. Once a spoken command is accepted, the
    // visible path is only thinking -> result (or an explicit error).
    app_ui_set_recording_mode(false);
    app_ui_set_recording_visual(true, false, 0);
    uint8_t *wav = NULL;
    uint32_t wav_len = 0;
    esp_err_t err = device_status_to_esp_err(device_audio_capture_wav(&wav, &wav_len));
    s_command_timing_capture_done_us = esp_timer_get_time();
    ESP_LOGI(TAG, "voice capture complete: generation=%lu err=%s wav=%u elapsed=%lldms",
             (unsigned long)interaction_generation, esp_err_to_name(err), (unsigned)wav_len,
             (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    if (interaction_stop_requested()) {
        device_audio_release_captured_wav(wav);
        goto finish;
    }
    if (command_cancel_requested_for(interaction_generation)) {
        app_ui_set_recording_visual(false, false, 0);
        device_audio_release_captured_wav(wav);
        finish_cancelled_command(interaction_generation);
        return;
    }
    if (err != ESP_OK || !wav || wav_len == 0) {
        // The natural endpoint did not observe speech. This is an expected
        // cancellation-like outcome, not a microphone failure and certainly
        // not a request to send the legacy text probe to the gateway.
        if (err == ESP_ERR_NOT_FOUND) {
            app_ui_set_recording_visual(false, false, 0);
            app_ui_show_text("未检测到语音", "请再试一次");
            device_audio_release_captured_wav(wav);
            finish_interaction_message(interaction_generation, 1400);
            return;
        }
        // Do not turn a local capture failure into an unrelated server text
        // command. That legacy probe leaves the command correlation empty and
        // can strand the EchoEar in a foreground message. Bread treats this as
        // a local, retryable status and returns to standby after the notice.
        pet("alert");
        app_ui_set_recording_visual(false, false, 0);
        app_ui_show_text("麦克风不可用", "请稍后再试");
        device_audio_release_captured_wav(wav);
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    if (!s_gateway_token[0]) {
        app_ui_show_text("设备配对", "请说出六位配对码");
        err = pair_by_voice(wav, wav_len);
        device_audio_release_captured_wav(wav);
        if (err == ESP_OK && gateway_handshake(false) == ESP_OK) {
            if (ensure_gateway_poll_task()) {
                pet("done");
                char pairing_ready_hint[72];
                snprintf(pairing_ready_hint, sizeof(pairing_ready_hint),
                         "按%s后说话", device_input_primary_interaction_label());
                app_ui_show_ready_prompt("配对成功", pairing_ready_hint);
            } else {
                err = ESP_ERR_NO_MEM;
                pet("alert");
                app_ui_show_text("设备启动失败", "无法启动网关轮询");
            }
        }
        else { pet("alert"); app_ui_show_text("配对失败", "请生成新的配对码"); }
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // The server is the interaction runtime: it owns ASR, intent routing,
    // authorization, agent/tool execution, IM delivery, and the final reply.
    // The ESP32 only submits a server-owned `voice` media attachment.
    char media_id[96] = {0};
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_interaction_generation == interaction_generation) {
        s_interaction_phase = INTERACTION_PROCESSING;
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    // Switch state before closing the recorder. app_ui_set_recording_visual
    // redraws the pet when it removes the waveform; doing that while the
    // previous state is idle briefly drew the time/weather face between
    // “received” and “thinking”.
    app_ui_set_command_stage("正在上传语音");
    pet("thinking");
    // Keep the foreground screen locked after capture as well.  The task can
    // receive its reply and clear its task handle before a delayed gateway
    // `pet_state: idle` notification is processed; that notification used to
    // repaint the Wi-Fi/time face in the gap before the final response draw.
    s_command_display_locked = true;
    app_ui_set_command_display_lock(true);
    app_ui_set_recording_visual(false, false, 0);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_command_cancel_enabled = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    app_ui_set_command_cancel_enabled(true);
    // Keep the pet's animated thinking state on screen during upload and the
    // server-side reply wait. Do not switch the shared I2S bus to playback
    // here: on EchoEar it races the just-stopped microphone DMA and resets
    // the CPU. The thinking screen is the immediate acknowledgement.
    // A cancel that arrived during capture must not still upload the audio.
    if (interaction_stop_requested()) {
        device_audio_release_captured_wav(wav);
        goto finish;
    }
    if (command_cancel_requested_for(interaction_generation)) {
        device_audio_release_captured_wav(wav);
        finish_cancelled_command(interaction_generation);
        return;
    }
    err = upload_voice(wav, wav_len, media_id, sizeof(media_id));
    if (err == ESP_OK) s_command_timing_upload_done_us = esp_timer_get_time();
    device_audio_release_captured_wav(wav);
    if (interaction_stop_requested()) goto finish;
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice media upload failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        pet("alert");
        app_ui_show_text("语音上传失败", command_submit_error_detail(err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    app_ui_set_command_stage("正在提交指令");
    char reply_to[COMMAND_REPLY_ID_CAPACITY] = {0};
    char command_event_id[80];
    snprintf(command_event_id, sizeof(command_event_id), "voice-%lld",
             (long long)esp_timer_get_time());
    for (unsigned attempt = 1; attempt <= COMMAND_SUBMIT_RETRY_COUNT; ++attempt) {
        if (interaction_stop_requested()) goto finish;
        err = send_voice_event(media_id, command_event_id, reply_to, sizeof(reply_to));
        if (err == ESP_OK || command_cancel_requested_for(interaction_generation)) break;
        ESP_LOGW(TAG, "voice command submit attempt %u/%u failed: %s",
                 attempt, COMMAND_SUBMIT_RETRY_COUNT, esp_err_to_name(err));
        if (attempt < COMMAND_SUBMIT_RETRY_COUNT) {
            // Reuse the idempotency key. If the Hub accepted an attempt but
            // its response was lost, the retry resolves the same command
            // instead of starting a duplicate Agent task.
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(500u << (attempt - 1u)));
        }
    }
    if (err == ESP_OK && !reply_to[0]) {
        ESP_LOGE(TAG, "incoming voice accepted without maclawMessageId");
        err = ESP_ERR_INVALID_RESPONSE;
    }
    if (err == ESP_OK) {
        s_command_timing_accepted_us = esp_timer_get_time();
        app_ui_set_command_stage("远端处理中");
        ESP_LOGI(TAG, "voice command waiting: generation=%lu replyTo=%s total=%lldms",
                 (unsigned long)interaction_generation, reply_to,
                 (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    if (err == ESP_OK) {
        strlcpy(s_active_command_reply_to, reply_to, sizeof(s_active_command_reply_to));
    }
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (interaction_stop_requested()) goto finish;
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "voice command submit failed: %s (0x%x)",
                 esp_err_to_name(err), (unsigned)err);
        pet("alert");
        app_ui_show_text("指令提交失败", command_submit_error_detail(err));
        finish_interaction_message(interaction_generation, 1800);
        return;
    }
    // Agent work is not bounded like a normal HTTP request. Complex remote
    // tasks routinely take longer than the old 90-second deadline; treating
    // that deadline as final also cleared replyTo, so the poller discarded the
    // eventual result. Keep the correlated command alive until a reply arrives
    // or the user explicitly cancels it. Refresh the message periodically so
    // the device never looks stalled while the remote Agent is still working.
    while (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(COMMAND_RESULT_PROGRESS_MS)) == 0) {
        if (interaction_stop_requested()) goto finish;
        if (command_cancel_requested_for(interaction_generation)) break;
        // Keep the animated thinking surface intact. This is a state
        // reassertion, not a full-screen refresh; unchanged labels do no LCD IO.
        app_ui_set_command_stage("远端处理中");
        ESP_LOGI(TAG, "remote Agent still processing command generation=%lu",
                 (unsigned long)interaction_generation);
        ESP_LOGI(TAG, "remote wait detail: replyTo=%s elapsed=%lldms",
                 reply_to, (long long)((esp_timer_get_time() - interaction_started_us) / 1000));
    }
    if (interaction_stop_requested()) goto finish;
    if (command_cancel_requested_for(interaction_generation)) { finish_cancelled_command(interaction_generation); return; }
    // The poller has already painted the final reply in the speaking state.
    // Returning through done/idle immediately after the notification repaints
    // the ambient face over it, producing the distracting apparent reboot.
    // Leave the response visible until the next user interaction or a later
    // server state update explicitly changes it.
    finish_interaction_task(interaction_generation);

finish:
    /* The standard finish helper owns the operation/power/foreground token.
     * Use it for every lifecycle stop too, but avoid drawing a terminal UI
     * from a rollback path. */
    finish_interaction_task_with_surface(interaction_generation, false);
}

static esp_err_t stop_interaction_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    uint32_t generation = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_stop_requested = true;
    task = s_interaction_task;
    generation = s_interaction_generation;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* Capture observes this board-neutral cooperative request at its next
     * bounded read. It is safe from the input, rollback, or registry task;
     * the actual stream mutex remains owned and released by the worker. */
    device_audio_request_capture_stop();
    (void)operation_context_request_cancel(generation);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_foreground_http_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_foreground_http_client_mutex, pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_foreground_http_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "interaction HTTP cancel failed: %s", esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_foreground_http_client_mutex);
    }
    if (device_connectivity_is_active_cellular()) {
        (void)device_connectivity_cancel_cellular_foreground_request();
    }
    if (s_interaction_start_gate) xSemaphoreGive(s_interaction_start_gate);
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_interaction_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_interaction_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_INTERACTION, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "foreground interaction worker stopped");
    return ESP_OK;
}

static esp_err_t stop_interaction_task_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_interaction_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_interaction_task(timeout_ms);
}

static bool start_voice_interaction(bool physical_screen_wake) {
    bool input_guarded;
    taskENTER_CRITICAL(&s_task_state_lock);
    input_guarded = esp_timer_get_time() < s_ignore_command_input_until_us;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (input_guarded) {
        ESP_LOGI(TAG, "voice interaction ignored while cancel gesture drains");
        return false;
    }
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "voice interaction ignored: meeting transition/upload active");
        return false;
    }
    bool network_available = device_connectivity_is_active_uplink_ready();
    if (device_connectivity_is_provisioning_active() || !s_gateway_token[0] || !network_available) {
        ESP_LOGW(TAG,
                 "voice interaction rejected before capture: setup=%s paired=%s network=%s",
                 device_connectivity_is_provisioning_active() ? "active" : "inactive",
                 s_gateway_token[0] ? "yes" : "no",
                 network_available ? "connected" : "offline");
        app_ui_show_text("暂时无法说话",
                             !network_available ? "网络未连接，请稍后重试"
                                                : "设备尚未配对或正在设置");
        return false;
    }
    // A physical tap after ambient sleep only restores the ready pet. A
    // hands-free wake phrase, however, is an intentional voice action: wake
    // the panel and continue into this same capture rather than asking the
    // user to repeat the phrase.
    if (physical_screen_wake && app_ui_wake_from_idle()) {
        ESP_LOGI(TAG, "sleeping display restored; voice capture deferred to next press");
        return false;
    }
    if (!physical_screen_wake && app_ui_wake_from_idle()) {
        ESP_LOGI(TAG, "offline wake restored sleeping display; continuing into voice capture");
    }
    if (!s_interaction_lock || xSemaphoreTake(s_interaction_lock, 0) != pdTRUE) {
        ESP_LOGW(TAG, "voice interaction ignored: interaction already active");
        return false;
    }
    device_operation_context_t operation = {0};
    device_status_t operation_status = operation_context_begin(
        DEVICE_OPERATION_KIND_VOICE_INTERACTION, 0, &operation);
    if (operation_status != DEVICE_STATUS_OK) {
        xSemaphoreGive(s_interaction_lock);
        ESP_LOGW(TAG, "voice interaction operation admission rejected: status=%d",
                 (int)operation_status);
        return false;
    }
    device_power_lease_t interaction_lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t lease_status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION, &interaction_lease);
    if (lease_status != DEVICE_STATUS_OK) {
        (void)operation_context_commit_terminal(operation.generation);
        xSemaphoreGive(s_interaction_lock);
        ESP_LOGW(TAG, "voice interaction rejected: power lease unavailable status=%d",
                 (int)lease_status);
        return false;
    }
    s_foreground_http_requested = true;
    s_command_display_locked = true;
    s_command_timing_started_us = esp_timer_get_time();
    s_command_timing_capture_done_us = 0;
    s_command_timing_upload_done_us = 0;
    s_command_timing_accepted_us = 0;
    s_command_timing_first_progress_us = 0;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_power_lease = interaction_lease;
    s_command_cancel_requested = false;
    s_command_cancel_enabled = false;
    s_command_cancel_ui_shown = false;
    s_cancel_requested_generation = 0;
    s_cancel_ui_ready_generation = 0;
    // A stop request belongs to the preceding capture only. Clear it before
    // RECORDING becomes visible to the input task, so a rapid next tap is
    // retained and can end this newly started command.
    device_audio_reset_capture_stop();
    uint32_t interaction_generation = operation.generation;
    s_interaction_generation = interaction_generation;
    s_interaction_phase = INTERACTION_RECORDING;
    s_active_command_reply_to[0] = '\0';
    s_result_speech_reply_to[0] = '\0';
    s_result_speech_parts_remaining = 0;
    s_result_speech_deadline_us = 0;
    taskEXIT_CRITICAL(&s_task_state_lock);
    ESP_LOGI(TAG, "voice operation started: id=%llu generation=%lu",
             (unsigned long long)operation.operation_id,
             (unsigned long)interaction_generation);
    if (physical_screen_wake) {
        // MultiNet leaves the largest internal block below this worker's 10 KiB
        // stack requirement. A physical tap can safely release the model here;
        // wake-phrase entry instead releases it inside interaction_task().
        // RECORDING was already published above, so a stop press during this
        // teardown window is routed to the new capture's stop flag instead of
        // being silently swallowed while the phase still looked idle.
        esp_err_t wake_stop_err = audio_wake_word_stop();
        if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
            ESP_LOGW(TAG, "offline wake stop before voice task: %s",
                     esp_err_to_name(wake_stop_err));
        }
        log_heap_snapshot("voice-before-task-create");
    }
    if (s_command_cancel_ui_ready) {
        while (xSemaphoreTake(s_command_cancel_ui_ready, 0) == pdTRUE) {}
    }
    if (!s_interaction_start_gate || !s_interaction_stopped) {
        device_power_lease_release(interaction_lease);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
        s_interaction_phase = INTERACTION_RESULT;
        taskEXIT_CRITICAL(&s_task_state_lock);
        (void)operation_context_commit_terminal(interaction_generation);
        xSemaphoreGive(s_interaction_lock);
        ESP_LOGE(TAG, "interaction lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_interaction_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_stop_requested = false;
    s_interaction_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    app_ui_set_command_display_lock(true);
    app_ui_cancel_ready_prompt();
    TaskHandle_t created_handle = NULL;
    // Keep the command worker stack in internal RAM.  It calls Wi-Fi/TLS and
    // its callbacks can run while the flash cache is temporarily disabled;
    // a PSRAM-backed task stack is then unsafe and manifests as an intermittent
    // reboot immediately after the six-second recording completes.  Payloads
    // and HTTP buffers still use PSRAM, so this only reserves a small, stable
    // internal stack for control flow.
    BaseType_t created = xTaskCreate(interaction_task, "maclaw_interaction",
                                     10240, (void *)(uintptr_t)interaction_generation,
                                     5, &created_handle);
    taskENTER_CRITICAL(&s_task_state_lock);
    s_interaction_task = created == pdPASS ? created_handle : NULL;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (created != pdPASS) {
        s_foreground_http_requested = false;
        device_power_lease_release(interaction_lease);
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_power_lease = DEVICE_POWER_LEASE_INVALID;
        s_interaction_phase = INTERACTION_RESULT;
        s_interaction_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        (void)operation_context_commit_terminal(interaction_generation);
        log_heap_snapshot("interaction-task-create-fail");
        xSemaphoreGive(s_interaction_lock);
        schedule_wake_restart();
        pet("alert");
        app_ui_show_text("操作失败", "无法启动语音任务");
        return false;
    }
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_INTERACTION,
        .name = "foreground_interaction",
        .context = (void *)created_handle,
        .stop = stop_interaction_task_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register foreground interaction: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_interaction_start_gate);
        (void)stop_interaction_task(500);
        return false;
    }
    // Release only after the task handle and Registry identity are visible to
    // cancellation/reply correlation. No worker side effect can escape this
    // task's lifecycle contract during the create-to-register window.
    xSemaphoreGive(s_interaction_start_gate);
    return true;
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
    if (device_connectivity_is_provisioning_active() || !s_gateway_token[0] || !network_available) {
        ESP_LOGW(TAG, "offline wake detected but online interaction is unavailable: setup=%s paired=%s network=%s",
                 device_connectivity_is_provisioning_active() ? "active" : "inactive",
                 s_gateway_token[0] ? "yes" : "no",
                 network_available ? "connected" : "offline");
        // Recognition is one-shot on EchoEar: the model is released before
        // this callback to make room for a possible voice worker. A rejected
        // phrase must therefore explicitly restore it, otherwise one wake
        // while Wi-Fi reconnects leaves hands-free input disabled forever.
        schedule_wake_restart();
        return;
    }
    ESP_LOGI(TAG, "offline wake accepted; starting voice interaction");
    (void)start_voice_interaction(false);
}

static void enter_setup_portal(void) {
    // Reconfiguration is explicitly requested by a long press. Do not erase
    // the working Wi-Fi or paired token before the replacement form is saved:
    // an accidental press, power loss, or abandoned phone session must leave
    // the device recoverable. The full setup form will atomically replace the
    // saved values and its normal save path will invalidate the old token only
    // when a new pairing code has actually been committed.
    pet("quiet");
    app_ui_show_text("重新配置设备", "正在开启设置热点");
    /* Keep the existing STA up while enabling the setup AP. This avoids
     * tearing down an outstanding gateway long-poll inside the button task,
     * which previously stalled the portal before the QR screen appeared. */
    start_setup_portal(true);
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
    while (!deferred_setup_stop_requested() && meeting_is_active() &&
           esp_timer_get_time() < deadline) {
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100));
    }
    if (deferred_setup_stop_requested()) goto finish;
    ESP_LOGI(TAG, "deferred configuration portal starting");
    enter_setup_portal();
finish:
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_deferred_setup_task == self) s_deferred_setup_task = NULL;
    s_deferred_setup_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_deferred_setup_stopped) xSemaphoreGive(s_deferred_setup_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
}

static esp_err_t stop_deferred_setup_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_deferred_setup_stop_requested = true;
    task = s_deferred_setup_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
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
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
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
    bool already_starting = s_deferred_setup_task != NULL || s_deferred_setup_starting;
    if (!already_starting) s_deferred_setup_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
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

// Alarm ringing is a local safety-critical foreground owner. The scheduler
// invokes this from its own task immediately before it begins an attempt.
// Keep it lock-free: each board audio HAL observes the request at its next
// bounded PCM write, releases its current transaction, and lets the alarm
// acquire audio without business logic knowing the physical codec or display.
static void on_alarm_ring_start(void *arg) {
    (void)arg;
    device_audio_request_playback_stop();
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
        app_ui_show_text("疑似设备跌落", "请点击取消");
        return;
    }
    app_ui_show_text("疑似设备跌落", "未收到本机取消");
}

static void on_app_intent(const app_intent_event_t *event, void *arg) {
    (void)arg;
    if (!event || event->struct_size != sizeof(*event) ||
        event->abi_version != APP_INTENT_ABI_VERSION ||
        event->input_generation == 0) {
        ESP_LOGW(TAG, "discarded invalid app intent event");
        return;
    }
    app_intent_type_t action = event->type;
    device_input_source_t source = event->source;
    bool primary_interaction_source = event->primary_interaction_source;
    static bool suppress_alarm_dismiss_gesture;
    static device_input_source_t alarm_dismiss_source = DEVICE_INPUT_SOURCE_UNKNOWN;
    /* DISPLAY_OFF is exited on a physical down edge, while the input adapter
     * will still subsequently publish the completed short/double/long
     * gesture for that same contact.  Remember its source so waking the panel
     * cannot fall through into voice capture when the gesture completes. */
    static bool suppress_display_wake_gesture;
    static device_input_source_t display_wake_source = DEVICE_INPUT_SOURCE_UNKNOWN;

    /* DISPLAY_OFF is a presentation-only state.  Consume the very first
     * primary contact at the shared business boundary, before any recorder or
     * meeting transition can observe it.  The Power Service serializes this
     * with the idle timer; the board adapter restores its own round/rectangular
     * ambient scene.  This is deliberately source-neutral: touch and physical
     * controls follow the same wake-then-act contract. */
    if (primary_interaction_source && app_ui_wake_from_idle()) {
        sleep_schedule_service_note_manual_wake();
        suppress_display_wake_gesture = true;
        display_wake_source = source;
        ESP_LOGI(TAG, "primary interaction consumed as display wake: source=%d action=%d",
                 (int)source, (int)action);
        return;
    }

    /* Consume the completed gesture emitted for the interaction that woke the
     * display.  Keep the barrier armed across every contact-down: a first
     * interaction can be a double tap/click, whose second down edge arrives
     * before the scanner emits its final SECONDARY action.  Clearing on that
     * edge would make a wake double-tap start a meeting/command. This keeps
     * the contract identical for touch, Bread/Fangtang's activation key, and
     * future primary controls: one whole initial gesture wakes only; the next
     * completed, deliberate gesture may invoke a command. */
    if (suppress_display_wake_gesture && source == display_wake_source) {
        if (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
            action == APP_INTENT_AUXILIARY_CONTACT_DOWN) {
            ESP_LOGD(TAG, "display-wake contact retained until gesture completes: source=%d",
                     (int)source);
        } else {
            suppress_display_wake_gesture = false;
            display_wake_source = DEVICE_INPUT_SOURCE_UNKNOWN;
            ESP_LOGI(TAG, "completed display-wake gesture consumed: source=%d action=%d",
                     (int)source, (int)action);
            return;
        }
    }

    /* A suspected-fall prompt is a local safety surface.  Accept the normal
     * primary interaction for every profile (touch or physical control) before
     * the gesture can enter voice/meeting policy.  Contact-down cancels early
     * on touch devices; a completed primary action performs the same action on
     * button-only devices. */
    if (primary_interaction_source && fall_detection_service_cancel_from_user()) {
        ESP_LOGI(TAG, "suspected-fall prompt cancelled by input source=%d action=%d",
                 (int)source, (int)action);
        app_ui_restore_standby();
        return;
    }

    if (s_command_capture_stop_gesture_pending &&
        esp_timer_get_time() >= (int64_t)s_command_capture_stop_gesture_deadline_us) {
        /* A malformed/aborted physical contact must not leave the old
         * completion barrier armed indefinitely.  The next activation is a
         * new user gesture and must retain its normal command semantics. */
        s_command_capture_stop_gesture_pending = false;
        s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
        s_command_capture_stop_gesture_deadline_us = 0;
        ESP_LOGW(TAG, "command-capture stop gesture barrier expired");
    }
    if (s_command_capture_stop_gesture_pending &&
        source == s_command_capture_stop_source) {
        if (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
            action == APP_INTENT_AUXILIARY_CONTACT_DOWN) {
            // This is a genuinely new contact, not completion of the stop
            // contact. Admit it normally after retiring the old barrier.
            s_command_capture_stop_gesture_pending = false;
            s_command_capture_stop_source = DEVICE_INPUT_SOURCE_UNKNOWN;
            s_command_capture_stop_gesture_deadline_us = 0;
        } else {
            ESP_LOGI(TAG, "completed command-capture stop gesture consumed");
            return;
        }
    }

    // An alarm is an urgent local foreground owner and may become due while
    // networking or Welcome playback is still finishing. Keep its physical
    // dismiss control available before applying the normal startup gate.
    bool alarm_dismiss_input = primary_interaction_source;
    if (s_alarm_manager_started && alarm_manager_is_ringing()) {
        if (alarm_dismiss_input) {
            alarm_manager_dismiss();
            if (action == APP_INTENT_PRIMARY_CONTACT_DOWN) {
                suppress_alarm_dismiss_gesture = true;
                alarm_dismiss_source = source;
            }
            ESP_LOGI(TAG, "ringing alarm dismissed by input source=%d action=%d",
                     (int)source, (int)action);
        } else {
            ESP_LOGI(TAG, "input ignored while alarm rings: source=%d action=%d",
                     (int)source, (int)action);
        }
        return;
    }

    if (!s_startup_sequence_complete) {
        // Startup owns the audio/display path until the optional greeting has
        // completed and the wake listener is loaded. Volume keys remain useful,
        // but activation gestures must not overtake this ordering boundary.
        // The configuration gesture is the exception: it is the maintenance
        // escape hatch precisely for the states that can never complete the
        // Welcome sequence (e.g. a saved Wi-Fi password that no longer
        // connects leaves the device on the "network unavailable" surface
        // with the sequence unfinished), so it must always reach the handler
        // that persists the setup request and reboots into the portal.
        if (action != APP_INTENT_INCREASE_VOLUME && action != APP_INTENT_DECREASE_VOLUME &&
            action != APP_INTENT_OPEN_CONFIGURATION) {
            ESP_LOGI(TAG, "input ignored until startup Welcome sequence completes");
            return;
        }
    }

    // A down edge dismisses immediately; consume the completed gesture from
    // that same contact so it cannot also start voice, cancel, or configure.
    if (suppress_alarm_dismiss_gesture &&
        action != APP_INTENT_PRIMARY_CONTACT_DOWN &&
        action != APP_INTENT_AUXILIARY_CONTACT_DOWN &&
        source == alarm_dismiss_source) {
        // A native double gesture may be followed by a delayed short from the
        // same contact-drain window. Keep suppression armed; the next real
        // down edge disarms it below before being handled normally.
        ESP_LOGI(TAG, "completed alarm-dismiss gesture consumed");
        return;
    }
    if (suppress_alarm_dismiss_gesture &&
        (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
         action == APP_INTENT_AUXILIARY_CONTACT_DOWN)) {
        suppress_alarm_dismiss_gesture = false;
        alarm_dismiss_source = DEVICE_INPUT_SOURCE_UNKNOWN;
    }
    // The down-edge action exists only for latency-sensitive foreground
    // surfaces. Preserve all established behavior on the completed gesture.
    if (action == APP_INTENT_PRIMARY_CONTACT_DOWN ||
        action == APP_INTENT_AUXILIARY_CONTACT_DOWN) {
        interaction_phase_t interaction_phase;
        taskENTER_CRITICAL(&s_task_state_lock);
        interaction_phase = s_interaction_phase;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (interaction_phase == INTERACTION_RECORDING) {
            device_audio_request_capture_stop();
            s_command_capture_stop_gesture_pending = true;
            s_command_capture_stop_source = source;
            s_command_capture_stop_gesture_deadline_us =
                (uint64_t)esp_timer_get_time() + INPUT_GESTURE_DRAIN_WINDOW_US;
            ESP_LOGI(TAG, "command recording stop requested by input source=%d",
                     (int)source);
        }
        return;
    }
    if (action == APP_INTENT_INCREASE_VOLUME || action == APP_INTENT_DECREASE_VOLUME) {
        // On a response page the available upper side key advances through the
        // reply. This keeps one-key reading in the natural 1 -> 2 -> 3 order;
        // the board renderer wraps the final page back to page 1. If the lower
        // key is confirmed later, it can use the opposite direction.
        int page_delta = action == APP_INTENT_INCREASE_VOLUME ? 1 : -1;
        bool page_handled = app_ui_navigate_response(page_delta);
        ESP_LOGI(TAG, "volume key: %s page_delta=%d response_handled=%s",
                 action == APP_INTENT_INCREASE_VOLUME ? "up" : "down", page_delta,
                 page_handled ? "yes" : "no");
        if (page_handled) return;
        uint8_t volume = 0;
        int delta = action == APP_INTENT_INCREASE_VOLUME ? 10 : -10;
        device_status_t volume_status = device_audio_adjust_output_volume(delta, &volume);
        if (volume_status == DEVICE_STATUS_OK) {
            ESP_LOGI(TAG, "output volume: %u%%", volume);
            esp_err_t save_err = persist_output_volume(volume);
            if (save_err != ESP_OK) {
                ESP_LOGW(TAG, "output volume persistence failed: %s", esp_err_to_name(save_err));
            }
        }
        return;
    }
    ESP_LOGI(TAG, "input action received: %s",
             action == APP_INTENT_PRIMARY_ACTIVATE ? "primary" :
             action == APP_INTENT_SECONDARY_ACTIVATE ? "secondary" : "configure");
    // The setup screen owns both the display and the radio. Treat touch/BOOT
    // input as inert until the submitted form deliberately restarts the
    // device; otherwise a stray tap starts normal voice UI and repaints the
    // QR while the phone is trying to configure the AP.
    if (device_connectivity_is_provisioning_active()) {
        ESP_LOGI(TAG, "button ignored while setup portal is active");
        return;
    }
    meeting_state_t meeting = s_meeting_state;
    /* Reconfiguration is the emergency/maintenance gesture and must take
     * precedence over voice, meeting and upload state. Previously a long hold
     * was detected correctly but silently consumed by the meeting guards. */
    if (action == APP_INTENT_OPEN_CONFIGURATION) {
        if (!s_wifi_ssid[0] && !device_connectivity_is_active_cellular()) {
            ESP_LOGI(TAG, "long press ignored while setup portal is active");
            return;
        }
        ESP_LOGW(TAG, "long press: configuration requested (meeting state=%d)", (int)meeting);
        /* Use a clean reboot as the transaction boundary. The next boot sees
         * the persisted setup request before starting STA/TLS, so it can enter
         * AP mode deterministically without racing an active long poll. */
        esp_err_t setup_err = configuration_service_request_force_setup();
        if (setup_err == ESP_OK) {
            ESP_LOGW(TAG, "configuration request saved; rebooting into setup");
            esp_restart();
        }
        ESP_LOGE(TAG, "cannot persist configuration request: %s", esp_err_to_name(setup_err));
        if (meeting == MEETING_RECORDING || meeting == MEETING_PAUSED) {
            meeting_set_state(MEETING_FINALIZING);
        }
        if (start_deferred_setup_task()) {
            ESP_LOGI(TAG, "configuration portal worker created");
        }
        return;
    }
    if (meeting == MEETING_RECORDING || meeting == MEETING_PAUSED) {
        // Stopping must work with the one dependable primary input fitted to
        // each enclosure: touch on EchoEar, or the activation key on Bread and
        // Fangtang. Accept every completed gesture as stop/save; a user should
        // not need a tight double tap while recording.
        // Do not repaint here: this callback runs in a hardware input task and
        // a full LCD DMA present can block it long enough to trip task_wdt. The
        // meeting task observes FINALIZING and owns the following UI updates.
        meeting_set_state(MEETING_FINALIZING);
        ESP_LOGI(TAG, "meeting stop requested: gesture=%s",
                 action == APP_INTENT_PRIMARY_ACTIVATE ? "primary" :
                 action == APP_INTENT_SECONDARY_ACTIVATE ? "secondary" : "configure");
        return;
    }
    if (meeting_is_active()) {
        ESP_LOGW(TAG, "button ignored: meeting transition/upload active");
        return;
    }
    if (action == APP_INTENT_SECONDARY_ACTIVATE) {
        bool interaction_active;
        interaction_phase_t interaction_phase;
        taskENTER_CRITICAL(&s_task_state_lock);
        interaction_active = s_interaction_task != NULL;
        interaction_phase = s_interaction_phase;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (interaction_active || interaction_phase == INTERACTION_RECORDING ||
            interaction_phase == INTERACTION_PROCESSING) {
            // One foreground action owns the activation key until it reaches a
            // result. During processing a double press means cancel; during the
            // fixed-length capture it is simply consumed. It can never fall
            // through and start a meeting recording in either phase.
            if (interaction_phase == INTERACTION_PROCESSING) {
                (void)request_command_cancel();
            } else {
                ESP_LOGI(TAG, "secondary input consumed by command recording");
            }
            return;
        }
        if (s_meeting_pending) {
            bool resume_running;
            taskENTER_CRITICAL(&s_task_state_lock);
            resume_running = s_meeting_task_running;
            taskEXIT_CRITICAL(&s_task_state_lock);
            if (resume_running) {
                // A worker is already transferring the retained file. Calling
                // start_meeting_task() again only reports a busy condition; it
                // is not a network failure and must not be labelled as one.
                app_ui_show_text("会议记录续传中", "完成后可开始新会议");
            } else if (ensure_meeting_resume_supervisor()) {
                app_ui_show_text("正在续传上次录音", "完成后可开始新会议");
            } else {
                pet("alert");
                app_ui_show_text("续传任务未启动", "设备将稍后自动重试");
            }
            return;
        }
        if (!s_meeting_available) {
            // A stale handshake must not permanently disable a local hardware
            // feature. Re-negotiate on demand, then continue the same double
            // tap if the current Hub advertises meeting recording.
            if (!refresh_meeting_capability()) {
                pet("alert");
                app_ui_show_text("录音启动失败", "无法检查网关支持");
            }
            return;
        }
        // A previous answer may deliberately remain on screen after its task
        // completes. Release that presentation lock as part of the explicit
        // transition into meeting mode so old command UI cannot interleave
        // with the meeting recorder.
        s_command_display_locked = false;
        app_ui_set_command_display_lock(false);
        if (!start_meeting_task(false)) {
            pet("alert");
            app_ui_show_text("录音启动失败", "设备正在处理其它操作");
        }
        return;
    }
    if (action != APP_INTENT_PRIMARY_ACTIVATE) return;
    // A completed short press during command capture requests the same stop
    // as its down edge. Without this branch the release gesture fell through
    // to start_voice_interaction() and was silently rejected by the
    // interaction lock.
    interaction_phase_t primary_phase;
    taskENTER_CRITICAL(&s_task_state_lock);
    primary_phase = s_interaction_phase;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (primary_phase == INTERACTION_RECORDING) {
        device_audio_request_capture_stop();
        ESP_LOGI(TAG, "command recording stop requested by completed primary gesture");
        return;
    }
    // The result is a deliberate terminal step in the command flow. The first
    // activation press closes it and returns to the clock/date/weather screen;
    // only a later press starts a new recording. This avoids accidentally
    // recording while the user is still reading the answer.
    if (app_ui_dismiss_response()) {
        dismiss_result_speech_transaction();
        taskENTER_CRITICAL(&s_task_state_lock);
        s_interaction_phase = INTERACTION_IDLE;
        taskEXIT_CRITICAL(&s_task_state_lock);
        s_command_display_locked = false;
        // app_ui_dismiss_response() releases the board guard and publishes the
        // matching PET model atomically.  Calling the board port directly here
        // left the shared model on RESPONSE, so later ambient/profile updates
        // could reason about a result page that was no longer on screen.
        ESP_LOGI(TAG, "response dismissed; ambient screen restored");
        return;
    }
    // A physical press only wakes a sleeping LCD; the offline wake phrase is
    // hands-free and therefore wakes the panel and records in the same event.
    (void)start_voice_interaction(true);
}
/* This is deliberately an explicit transaction rather than ESP_ERROR_CHECK:
 * a failed cold network start must enter the composition root's degraded
 * rollback path, not reboot into the same partial allocation indefinitely.
 * `s_network_initialized` is published only after both ESP-NETIF and the
 * default event loop exist, so callers never treat a partially initialized
 * core as restartable. */
static esp_err_t init_network_core(void) {
    /* A full-ready bit is meaningful only while both singleton owners still
     * exist.  A failed rollback may have released the event loop before
     * `esp_netif_deinit()` failed; in that state `s_network_initialized`
     * deliberately remains true for diagnostics, but it must never let a
     * later caller use a half-dismantled core as if it were ready. */
    if (s_network_initialized && s_netif_initialized &&
        s_default_event_loop_created) {
        return ESP_OK;
    }
    /* Do not try to create a second netif/event loop after a partial start or
     * partial stop. The rollback owner below must first consume the recorded
     * resources; a fresh generation here could collide with ESP-IDF's
     * singleton netif/event-loop state. */
    if (s_network_initialized || s_netif_initialized ||
        s_default_event_loop_created) {
        ESP_LOGW(TAG, "network core start rejected: prior partial generation still owns resources");
        return ESP_ERR_INVALID_STATE;
    }
    if (!device_connectivity_initialize()) return ESP_ERR_NO_MEM;

    esp_err_t err = esp_netif_init();
    if (err != ESP_OK) {
        /* Connectivity's logical EventGroup has no useful lifetime without
         * the physical netif root. Do not retain it after a failed first
         * allocation and accidentally treat a later retry as the same Wi-Fi
         * generation. */
        device_status_t stop_status = device_connectivity_deinit(500);
        if (stop_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cannot stop Connectivity after netif init failure: %d",
                     (int)stop_status);
        }
        return err;
    }
    s_netif_initialized = true;
    err = esp_event_loop_create_default();
    if (err != ESP_OK) {
        /* No event-loop client exists yet, but `esp_netif_init()` has an
         * independent singleton lifetime.  Preserve the ownership flag until
         * the common root transaction actually releases it; even a failed
         * cleanup now remains observable and cannot be overwritten by a
         * second init attempt. */
        ESP_LOGW(TAG, "default event-loop init failed; rolling back recorded netif owner: %s",
                 esp_err_to_name(err));
        esp_err_t rollback_err = stop_connectivity_root_transaction(500);
        if (rollback_err != ESP_OK) {
            ESP_LOGW(TAG, "network-core rollback after event-loop init failure incomplete: %s",
                     esp_err_to_name(rollback_err));
        }
        return err;
    }
    s_default_event_loop_created = true;
    s_network_initialized = true;
    return ESP_OK;
}

/* Wi-Fi driver initialization is likewise a bounded transaction.  Event
 * handlers are registered one by one by ESP-IDF, hence a later registration
 * failure must remove each earlier instance before deinitializing the driver.
 * This gives the cold-start rollback a truthful ownership picture. */
static esp_err_t init_network(void) {
    esp_err_t err = init_network_core();
    if (err != ESP_OK) return err;
    if (s_wifi_driver_initialized) return ESP_OK;
    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    // This access point triggers an ESP-IDF 6.0.2 Wi-Fi RX timer crash after
    // the first block-ack (BA) setup.  EchoEar's command traffic is tiny, so
    // disable aggregation before driver startup for a stable station link.
    init.ampdu_rx_enable = 0;
    init.ampdu_tx_enable = 0;
    err = esp_wifi_init(&init);
    if (err != ESP_OK) {
        /* esp_wifi_init() is itself after the netif/event-loop transaction.
         * Reuse the sole cold-start teardown root even though no driver flag
         * was published, so a retry never inherits a live Connectivity
         * EventGroup or ESP-IDF singleton core. */
        ESP_LOGW(TAG, "Wi-Fi driver initialization failed: %s", esp_err_to_name(err));
        esp_err_t rollback_err = stop_connectivity_root_transaction(500);
        if (rollback_err != ESP_OK) {
            ESP_LOGW(TAG, "network-core rollback after Wi-Fi driver init failure incomplete: %s",
                     esp_err_to_name(rollback_err));
        }
        return err;
    }
    s_wifi_driver_initialized = true;
    err = esp_event_handler_instance_register(
        WIFI_EVENT, ESP_EVENT_ANY_ID, wifi_event, NULL, &s_wifi_event_instance);
    if (err != ESP_OK) goto fail;
    err = esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_STA_GOT_IP, wifi_event, NULL, &s_wifi_got_ip_event_instance);
    if (err != ESP_OK) goto fail;
    err = esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_ASSIGNED_IP_TO_CLIENT, wifi_event, NULL,
        &s_wifi_assigned_ip_event_instance);
    if (err != ESP_OK) goto fail;
    /* A previous closed generation may have left a drain notification after
     * its last callback. It belongs to that generation, never to this newly
     * admitted set of handlers. */
    while (s_wifi_event_callbacks_drained &&
           xSemaphoreTake(s_wifi_event_callbacks_drained, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wifi_event_callback_admission_open = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    s_wifi_driver_initialized = true;
    return ESP_OK;

fail:
    /* No station/AP netif has been created at this stage.  The shared stop
     * transaction safely removes any prior handler instances and the driver;
     * when that cleanup succeeds it also releases the core.  If it cannot,
     * its retained handles make the failure fail-closed rather than
     * pretending a full runtime restart has occurred. */
    ESP_LOGW(TAG, "Wi-Fi initialization transaction failed: %s", esp_err_to_name(err));
    (void)stop_connectivity_root_transaction(500);
    return err;
}

static void ensure_setup_ap_netif(void) {
    if (!s_ap_netif_created) {
        s_setup_ap_netif = esp_netif_create_default_wifi_ap();
        if (s_setup_ap_netif) s_ap_netif_created = true;
    }
}

static void ensure_station_netif(void) {
    if (!s_sta_netif_created) {
        s_sta_netif = esp_netif_create_default_wifi_sta();
        if (s_sta_netif) s_sta_netif_created = true;
    }
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
    const bool shown = app_ui_show_qrcode_modules(
        modules, module_count, user_data ? (const char *)user_data : NULL);
    free(modules);
    if (!shown) ESP_LOGW(TAG, "cannot publish setup QR module matrix");
}

static void show_setup_qrcode(const char *ssid) {
    // This is the standard no-password Wi-Fi QR payload, understood by the
    // iOS/Android camera handlers and by WeChat's Wi-Fi scanner.
    char payload[96];
    int length = snprintf(payload, sizeof(payload), "WIFI:T:nopass;S:%s;;", ssid);
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
        app_ui_show_text("设备网络设置", ssid);
    }
}

static size_t append_setup_html_escaped(char *out, size_t used, size_t cap,
                                        const char *value) {
    for (const unsigned char *p = (const unsigned char *)(value ? value : ""); *p; ++p) {
        const char *replacement = NULL;
        switch (*p) {
            case '&': replacement = "&amp;"; break;
            case '<': replacement = "&lt;"; break;
            case '>': replacement = "&gt;"; break;
            case '\"': replacement = "&quot;"; break;
            case '\'': replacement = "&#39;"; break;
            default: break;
        }
        if (replacement) {
            size_t len = strlen(replacement);
            if (used + len >= cap) return cap;
            memcpy(out + used, replacement, len);
            used += len;
        } else {
            if (used + 1 >= cap) return cap;
            out[used++] = (char)*p;
        }
    }
    return used;
}

static size_t setup_html_escaped_length(const char *value) {
    size_t length = 0;
    for (const unsigned char *p = (const unsigned char *)(value ? value : ""); *p; ++p) {
        switch (*p) {
            case '&': length += 5; break;   // &amp;
            case '<':
            case '>': length += 4; break;   // &lt; / &gt;
            case '\"': length += 6; break; // &quot;
            case '\'': length += 5; break; // &#39;
            default: ++length; break;
        }
    }
    return length;
}

static bool setup_ssid_is_selectable(const char *ssid) {
    if (!ssid || !ssid[0]) return false;
    const char *choice = s_setup_ssid_choices;
    while (*choice) {
        if (!strcmp(choice, ssid)) return true;
        choice += strlen(choice) + 1;
    }
    return false;
}

static bool remember_setup_ssid_choice(const char *ssid) {
    if (!ssid || !ssid[0] || setup_ssid_is_selectable(ssid)) return true;
    size_t used = 0;
    while (used < SETUP_SSID_CHOICES_CAPACITY && s_setup_ssid_choices[used]) {
        used += strlen(s_setup_ssid_choices + used) + 1;
    }
    size_t length = strlen(ssid);
    if (used + length + 1 > SETUP_SSID_CHOICES_CAPACITY) return false;
    memcpy(s_setup_ssid_choices + used, ssid, length + 1);
    return true;
}

static bool can_remember_setup_ssid_choice(const char *ssid) {
    if (!ssid || !ssid[0] || setup_ssid_is_selectable(ssid)) return true;
    size_t used = 0;
    while (used < SETUP_SSID_CHOICES_CAPACITY && s_setup_ssid_choices[used]) {
        used += strlen(s_setup_ssid_choices + used) + 1;
    }
    return used + strlen(ssid) + 1 <= SETUP_SSID_CHOICES_CAPACITY;
}

static const char *setup_auth_mode_label(wifi_auth_mode_t mode);

static bool setup_auth_mode_is_enterprise(wifi_auth_mode_t mode) {
    // ESP-IDF 6 distinguishes WPA, WPA2, WPA3-transition and WPA3-192-bit
    // enterprise networks.  All of them need the 802.1X part of the setup
    // form; limiting this to the WPA2 alias silently selected "Personal" for
    // newer office networks.
    return mode == WIFI_AUTH_WPA_ENTERPRISE ||
           mode == WIFI_AUTH_WPA2_ENTERPRISE ||
           mode == WIFI_AUTH_WPA3_ENTERPRISE ||
           mode == WIFI_AUTH_WPA2_WPA3_ENTERPRISE ||
           mode == WIFI_AUTH_WPA3_ENT_192;
}

static bool append_setup_ssid_option(const char *ssid, int rssi, wifi_auth_mode_t authmode,
                                     bool selected) {
    if (!ssid || !ssid[0]) return true;
    size_t used = strlen(s_setup_ssid_options);
    if (setup_ssid_is_selectable(ssid)) return true;
    const char *prefix = "<option value=\"";
    const char *selected_attr = selected ? " selected" : "";
    const char *enterprise_attr = setup_auth_mode_is_enterprise(authmode)
                                       ? " data-enterprise=1" : "";
    const char *suffix = "</option>";
    size_t escaped_length = setup_html_escaped_length(ssid);
    const char *security = setup_auth_mode_label(authmode);
    // 2 bytes for the closing quote/bracket, 32 bytes for signal/security.
    if (used + strlen(prefix) + escaped_length * 2 + 2 + 32 +
        strlen(enterprise_attr) + strlen(selected_attr) + strlen(suffix) >=
            SETUP_SSID_OPTIONS_CAPACITY ||
        !can_remember_setup_ssid_choice(ssid)) return false;
    memcpy(s_setup_ssid_options + used, prefix, strlen(prefix));
    used += strlen(prefix);
    used = append_setup_html_escaped(s_setup_ssid_options, used, SETUP_SSID_OPTIONS_CAPACITY, ssid);
    int attribute_length = snprintf(s_setup_ssid_options + used,
                                    SETUP_SSID_OPTIONS_CAPACITY - used,
                                    "\"%s%s>", enterprise_attr, selected_attr);
    if (attribute_length <= 0 || (size_t)attribute_length >=
                                     SETUP_SSID_OPTIONS_CAPACITY - used) {
        return false;
    }
    used += (size_t)attribute_length;
    used = append_setup_html_escaped(s_setup_ssid_options, used, SETUP_SSID_OPTIONS_CAPACITY, ssid);
    int written = snprintf(s_setup_ssid_options + used, SETUP_SSID_OPTIONS_CAPACITY - used,
                           " (%d dBm, %s)%s", rssi, security, suffix);
    return written > 0 && (size_t)written < SETUP_SSID_OPTIONS_CAPACITY - used &&
           remember_setup_ssid_choice(ssid);
}

static int compare_setup_ap_records(const void *left, const void *right) {
    const wifi_ap_record_t *a = left;
    const wifi_ap_record_t *b = right;
    return (int)b->rssi - (int)a->rssi;
}

static const char *setup_auth_mode_label(wifi_auth_mode_t mode) {
    switch (mode) {
        case WIFI_AUTH_OPEN: return "open";
        case WIFI_AUTH_WEP: return "WEP";
        case WIFI_AUTH_WPA_PSK: return "WPA";
        case WIFI_AUTH_WPA2_PSK: return "WPA2";
        case WIFI_AUTH_WPA_WPA2_PSK: return "WPA/WPA2";
        case WIFI_AUTH_WPA3_PSK: return "WPA3";
        case WIFI_AUTH_WPA2_WPA3_PSK: return "WPA2/WPA3";
        case WIFI_AUTH_WPA_ENTERPRISE: return "WPA-802.1X";
        case WIFI_AUTH_WPA2_ENTERPRISE: return "WPA2-802.1X";
        case WIFI_AUTH_WPA3_ENTERPRISE: return "WPA3-802.1X";
        case WIFI_AUTH_WPA2_WPA3_ENTERPRISE: return "WPA2/WPA3-802.1X";
        case WIFI_AUTH_WPA3_ENT_192: return "WPA3-192 802.1X";
        default: return "secured";
    }
}

static bool refresh_setup_ssid_options(void) {
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(15000)) != pdTRUE) {
        ESP_LOGW(TAG, "setup Wi-Fi scan already in progress");
        return false;
    }

    wifi_scan_config_t config = {0};
    config.show_hidden = false;
    esp_err_t err = esp_wifi_scan_start(&config, true);
    bool refreshed = false;
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "Wi-Fi scan for setup list failed: %s", esp_err_to_name(err));
    } else {
        uint16_t count = SETUP_SCAN_MAX_APS;
        memset(s_setup_scan_records, 0, SETUP_SCAN_MAX_APS * sizeof(*s_setup_scan_records));
        err = esp_wifi_scan_get_ap_records(&count, s_setup_scan_records);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot read Wi-Fi scan results: %s", esp_err_to_name(err));
        } else {
            // A successful scan atomically replaces the selectable list while
            // this mutex is held.  On a scan error above, the old list remains
            // untouched so a user can safely retry without losing their choice.
            s_setup_ssid_options[0] = '\0';
            s_setup_ssid_choices[0] = '\0';
            qsort(s_setup_scan_records, count, sizeof(s_setup_scan_records[0]),
                  compare_setup_ap_records);
            for (uint16_t i = 0; i < count; ++i) {
                const char *ssid = (const char *)s_setup_scan_records[i].ssid;
                // A scan may report the same SSID from multiple radios.
                if (setup_ssid_is_selectable(ssid)) continue;
                if (!append_setup_ssid_option(ssid, s_setup_scan_records[i].rssi,
                                                s_setup_scan_records[i].authmode,
                                                s_wifi_ssid[0] && !strcmp(ssid, s_wifi_ssid))) break;
            }
            refreshed = true;
            ESP_LOGI(TAG, "setup Wi-Fi selection list contains %u scanned networks", (unsigned)count);
        }
    }
    if (refreshed && !s_setup_ssid_options[0]) {
        strlcpy(s_setup_ssid_options,
                "<option value=\"\" selected disabled>No visible Wi-Fi networks found; refresh the hotspot and try again.</option>",
                SETUP_SSID_OPTIONS_CAPACITY);
    }
    xSemaphoreGive(s_setup_options_mutex);
    return refreshed;
}

/* The captive DNS worker may successfully bind while the AP's DHCP service is
 * still misconfigured.  Treat the entire IP/DHCP advertisement as one
 * prerequisite: the portal must not subsequently start a radio that hands
 * clients a stale resolver or no gateway. */
static esp_err_t configure_setup_ap_ip(void) {
    if (!s_setup_ap_netif) return ESP_ERR_INVALID_STATE;
    esp_netif_ip_info_t ip_info = {0};
    IP4_ADDR(&ip_info.ip, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.gw, 192, 168, 4, 1);
    IP4_ADDR(&ip_info.netmask, 255, 255, 255, 0);
    esp_err_t stop_err = esp_netif_dhcps_stop(s_setup_ap_netif);
    if (stop_err != ESP_OK && stop_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STOPPED) {
        ESP_LOGW(TAG, "cannot pause DHCP server to configure setup IP: %s",
                 esp_err_to_name(stop_err));
        return stop_err;
    }
    esp_err_t ip_err = esp_netif_set_ip_info(s_setup_ap_netif, &ip_info);
    // Explicitly advertise the SoftAP as DNS. On IDF 6 the DHCP server can
    // otherwise inherit a stale upstream resolver while APSTA is entered from
    // a connected station, so the phone never sends its captive probe to us.
    esp_netif_dns_info_t dns = {0};
    IP4_ADDR(&dns.ip.u_addr.ip4, 192, 168, 4, 1);
    dns.ip.type = ESP_IPADDR_TYPE_V4;
    uint8_t offer_dns = DHCPS_OFFER_DNS;
    esp_err_t dns_offer_err = ip_err == ESP_OK
                                  ? esp_netif_dhcps_option(s_setup_ap_netif, ESP_NETIF_OP_SET,
                                                           ESP_NETIF_DOMAIN_NAME_SERVER,
                                                           &offer_dns, sizeof(offer_dns))
                                  : ip_err;
    esp_err_t dns_err = dns_offer_err == ESP_OK
                            ? esp_netif_set_dns_info(s_setup_ap_netif, ESP_NETIF_DNS_MAIN, &dns)
                            : dns_offer_err;
    // DHCP option 114 is the standards-based captive-portal signal used by
    // recent Android and iOS releases. DNS interception remains necessary for
    // older clients and for Windows.
    esp_err_t portal_uri_err = dns_err == ESP_OK
                                   ? esp_netif_dhcps_option(s_setup_ap_netif, ESP_NETIF_OP_SET,
                                                            ESP_NETIF_CAPTIVEPORTAL_URI,
                                                            (void *)s_setup_captive_portal_uri,
                                                            sizeof(s_setup_captive_portal_uri))
                                   : dns_err;
    esp_err_t start_err = esp_netif_dhcps_start(s_setup_ap_netif);
    if (ip_err != ESP_OK || dns_err != ESP_OK || portal_uri_err != ESP_OK ||
        (start_err != ESP_OK && start_err != ESP_ERR_ESP_NETIF_DHCP_ALREADY_STARTED)) {
        ESP_LOGW(TAG, "cannot configure setup DHCP server: ip=%s dns=%s portal=%s start=%s",
                 esp_err_to_name(ip_err), esp_err_to_name(dns_err),
                 esp_err_to_name(portal_uri_err), esp_err_to_name(start_err));
        if (ip_err != ESP_OK) return ip_err;
        if (dns_err != ESP_OK) return dns_err;
        if (portal_uri_err != ESP_OK) return portal_uri_err;
        return start_err;
    } else {
        ESP_LOGI(TAG, "setup DHCP advertises gateway/DNS/portal=%s", SETUP_CAPTIVE_PORTAL_URI);
    }
    return ESP_OK;
}

static void dns_server_task(void *arg) {
    (void)arg;
    /* The task is created before it is entered in Task Registry.  Keep it
     * dormant until the entry is published so a rollback never races an
     * untracked DNS socket with portal/AP teardown. */
    if (!s_dns_start_gate ||
        xSemaphoreTake(s_dns_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        taskENTER_CRITICAL(&s_task_state_lock);
        if (s_dns_task == self) s_dns_task = NULL;
        s_dns_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_dns_stopped) xSemaphoreGive(s_dns_stopped);
        vTaskDelete(NULL);
        return;
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool ready_published = false;
    int socket_fd = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (socket_fd < 0) {
        ESP_LOGE(TAG, "cannot create captive DNS socket: errno=%d", errno);
        goto finish;
    }
    struct sockaddr_in address = {
        .sin_family = AF_INET,
        .sin_port = htons(DNS_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    /* Bounded receive is the DNS worker's cancellation safe point.  Do not
     * close the descriptor from another task: lwIP socket ownership stays with
     * this worker until it has left recvfrom() and completed its join. */
    struct timeval receive_timeout = {.tv_sec = 0, .tv_usec = 100000};
    (void)setsockopt(socket_fd, SOL_SOCKET, SO_RCVTIMEO, &receive_timeout, sizeof(receive_timeout));
    if (bind(socket_fd, (struct sockaddr *)&address, sizeof(address)) < 0) {
        ESP_LOGE(TAG, "cannot bind captive DNS socket: errno=%d", errno);
        goto finish;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_ready_success = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_dns_ready) xSemaphoreGive(s_dns_ready);
    ready_published = true;
    ESP_LOGI(TAG, "captive DNS is answering all hostnames at %s", SETUP_AP_IP_ADDR);
    while (device_connectivity_is_provisioning_active()) {
        taskENTER_CRITICAL(&s_task_state_lock);
        bool stop_requested = s_dns_stop_requested;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (stop_requested) break;
        uint8_t packet[DNS_PACKET_CAPACITY];
        struct sockaddr_in source = {0};
        socklen_t source_len = sizeof(source);
        int received = recvfrom(socket_fd, packet, sizeof(packet), 0,
                                (struct sockaddr *)&source, &source_len);
        if (received < 12) continue;
        // This responder is authoritative only for ordinary DNS queries. Do
        // not turn a malformed response, UPDATE, or another opcode into a
        // seemingly valid captive-portal response.
        if ((packet[2] & 0x80) || (packet[2] & 0x78) ||
            packet[4] != 0 || packet[5] != 1) {
            continue;
        }
        // Locate the end of the first question. The answer MUST be appended
        // immediately after the question section: modern phones attach an EDNS
        // OPT record (ARCOUNT=1) to their queries, and replying after those
        // trailing bytes makes resolvers parse the OPT record as the answer,
        // so the captive-probe hostname never resolves and no portal pops up.
        size_t question_end = 12;
        while (question_end < (size_t)received && packet[question_end] != 0) {
            const uint8_t label_len = packet[question_end];
            // DNS labels are limited to 63 octets.  The two high bits encode
            // compression/reserved forms, which this one-question responder
            // deliberately does not accept in client queries.
            if (label_len > 63 ||
                question_end + 1u + label_len >= (size_t)received) {
                question_end = 0;
                break;
            }
            question_end += 1u + label_len;
        }
        if (question_end == 0 || question_end + 5u > (size_t)received) continue;
        question_end += 5; // root label + QTYPE + QCLASS
        // The A answer is appended below.  Check against the received DNS
        // message as well as the backing array: a short datagram with a valid
        // question must not be expanded in place to an answer beyond its
        // original payload length.
        if (question_end + 16u > (size_t)received ||
            question_end + 16u > sizeof(packet)) continue;
        // Keep the high-frequency captive probes at debug level. Logging every
        // DNS query at INFO can starve the small serial/log path just as a
        // phone is issuing parallel A, AAAA and HTTPS checks.
        ESP_LOGD(TAG, "captive DNS query received");
        // The packet always carries an IPv4 A record.  Return a syntactically
        // valid negative reply for unsupported question types instead of
        // claiming an A answer to an AAAA/HTTPS query; several current mobile
        // resolvers reject that type mismatch and never send the HTTP probe.
        const uint16_t qtype = ((uint16_t)packet[question_end - 4] << 8) |
                               packet[question_end - 3];
        const uint16_t qclass = ((uint16_t)packet[question_end - 2] << 8) |
                                packet[question_end - 1];
        // Mark the answer authoritative. This is not a recursive resolver,
        // so clear the incoming CD/Z/RCODE bits and never advertise RA.
        packet[2] = (packet[2] & 0x01) | 0x84;
        packet[3] = 0;
        packet[4] = 0; packet[5] = 1;   // the reply carries the first question only
        packet[6] = 0; packet[7] = qtype == 1 && qclass == 1 ? 1 : 0;
        packet[8] = 0; packet[9] = 0;
        packet[10] = 0; packet[11] = 0; // drop any additional (EDNS) records
        size_t cursor = question_end;
        if (qtype != 1 || qclass != 1) {
            (void)sendto(socket_fd, packet, cursor, 0,
                         (struct sockaddr *)&source, source_len);
            continue;
        }
        packet[cursor++] = 0xC0; packet[cursor++] = 0x0C; // answer name = question name
        packet[cursor++] = 0; packet[cursor++] = 1;        // A
        packet[cursor++] = 0; packet[cursor++] = 1;        // IN
        packet[cursor++] = 0; packet[cursor++] = 0;
        packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 0; packet[cursor++] = 28;
        packet[cursor++] = 0; packet[cursor++] = 4;
        packet[cursor++] = 192; packet[cursor++] = 168; packet[cursor++] = 4; packet[cursor++] = 1;
        (void)sendto(socket_fd, packet, cursor, 0, (struct sockaddr *)&source, source_len);
    }
finish:
    if (socket_fd >= 0) close(socket_fd);
    if (!ready_published) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_ready_success = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (s_dns_ready) xSemaphoreGive(s_dns_ready);
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_dns_task == self) s_dns_task = NULL;
    s_dns_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_dns_stopped) xSemaphoreGive(s_dns_stopped);
    vTaskDelete(NULL);
}

/* Captive DNS owns only its UDP/53 socket and task.  This does not stop the
 * enclosing portal's HTTP server, DHCP server, SoftAP/STA mode or Wi-Fi event
 * handlers; those remain outside this Registry entry until they share a real
 * Provisioning Service shutdown contract. */
static esp_err_t stop_captive_dns_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_admission_open = false;
    s_dns_stop_requested = true;
    task = s_dns_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    const uint32_t join_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (join_timeout_ms == 0 || !s_dns_stopped ||
        xSemaphoreTake(s_dns_stopped, pdMS_TO_TICKS(join_timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* The worker has published completion but must not take the Registry's
     * unbounded natural-exit path while an owner-wide stop holds its deadline.
     * Remove its immutable registration identity here using only residual
     * budget.  A bookkeeping timeout intentionally leaves the entry in place,
     * so a caller cannot mistake this generation for fully drained. */
    const uint32_t unregister_timeout_ms =
        startup_rollback_remaining_timeout_ms(deadline_us);
    if (unregister_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, unregister_timeout_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "captive DNS worker stopped");
    return ESP_OK;
}

static esp_err_t stop_captive_dns_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_dns_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_captive_dns_task(timeout_ms);
}

static bool start_captive_dns(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_dns_admission_open;
    bool already_starting = s_dns_starting || s_dns_task != NULL;
    if (admission_open && !already_starting) s_dns_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "captive DNS start rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_dns_start_gate || !s_dns_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "captive DNS lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_dns_stopped, 0) == pdTRUE) {}
    while (s_dns_ready && xSemaphoreTake(s_dns_ready, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_stop_requested = false;
    s_dns_ready_success = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (xTaskCreate(dns_server_task, "maclaw_captive_dns", 3072, NULL, 3, &task) != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_task = NULL;
        s_dns_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGW(TAG, "cannot start captive DNS task");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "captive_dns",
        .context = (void *)task,
        .stop = stop_captive_dns_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register captive DNS worker: %s", esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_dns_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_dns_start_gate);
        (void)stop_captive_dns_task(500);
        return false;
    }
    xSemaphoreGive(s_dns_start_gate);
    /* The worker reports only after bind(UDP/53). This stops the portal from
     * advertising a captive form whose DNS interceptor never came online. */
    if (!s_dns_ready ||
        xSemaphoreTake(s_dns_ready, pdMS_TO_TICKS(1200)) != pdTRUE) {
        ESP_LOGE(TAG, "captive DNS did not report readiness");
        (void)stop_captive_dns_task(500);
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    bool ready = s_dns_ready_success;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!ready) {
        ESP_LOGE(TAG, "captive DNS failed before readiness");
        (void)stop_captive_dns_task(500);
    }
    return ready;
}

static esp_err_t setup_get_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    // Keep the setup page small and deterministic. The earlier generated page
    // could exceed its fixed stack buffer when many SSIDs were present, which
    // reset the ESP exactly when a phone requested the portal.
    static const char setup_page_prefix[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        "label{display:block;margin:1rem 0 .3rem}input,select{box-sizing:border-box;width:100%;padding:.7rem;font-size:1rem}"
        ".enterprise{margin-top:1rem;padding:.85rem;border:1px solid #b9c9d7;background:#f5f9fc}.hint{font-size:.85rem;color:#486581;line-height:1.45}"
        ".saved{display:flex;align-items:center;justify-content:space-between;gap:.6rem;padding:.45rem 0;border-bottom:1px solid #dbe4ec;word-break:break-all}"
        ".saved form{margin:0}.saved button{margin:0;padding:.45rem .8rem;background:#b23b3b}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>MaClaw Pet setup</h1><p>Choose your home or office Wi-Fi. The device will restart and connect automatically.</p>"
        "<form method=post action=/save><label>Wi-Fi network</label><select name=ssid required>";
    static const char setup_page_suffix[] =
        "</select><p class=hint>Only visible Wi-Fi networks are shown. <a href=/refresh>Refresh network list</a>. Hidden networks must temporarily enable SSID broadcast.</p>"
        "<label>Security</label><select name=security id=security onchange='document.getElementById(\"enterprise\").hidden=this.value!==\"enterprise\";document.getElementById(\"passlabel\").textContent=this.value===\"enterprise\"?\"Password\":\"Wi-Fi password\"'><option value=personal selected>Personal (WPA/WPA2/WPA3)</option><option value=enterprise>Enterprise (802.1X)</option></select>"
        "<label id=passlabel>Wi-Fi password</label><input name=password type=password maxlength=64>"
        "<section class=enterprise id=enterprise hidden><strong>Enterprise Wi-Fi</strong><p class=hint>Defaults match typical phone settings: PEAP, MSCHAPv2, system certificates. Ask your IT administrator only if your network differs.</p>"
        "<label>EAP method</label><select name=eap_method><option value=peap selected>PEAP</option><option value=ttls>TTLS</option></select>"
        "<label>Identity (optional)</label><input name=identity maxlength=127 autocapitalize=none placeholder='Anonymous identity, if required'>"
        "<label>Username</label><input name=username maxlength=127 autocapitalize=none placeholder='Required'>"
        "<label>TTLS inner authentication</label><select name=ttls_phase2><option value=mschapv2 selected>MSCHAPv2 (default)</option><option value=pap>PAP</option></select>"
        "<label>CA certificate</label><select name=ca_mode><option value=system selected>Use system certificates (recommended)</option><option value=none>Do not validate (not recommended)</option></select>"
        "<label>Server domain (optional)</label><input name=server_domain maxlength=127 autocapitalize=none placeholder='Example: radius.company.com'></section>"
        "<label>MaClaw Hub URL</label><input name=gateway value='https://hub.mypapers.top' required maxlength=255>"
        "<label>6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required>"
        "<button>Save and connect</button></form>";
    // 已存热点列表 chunk 之后是收尾脚本与页面结束标签。
    static const char setup_page_tail[] =
        "<script>(function(){var n=document.querySelector('[name=ssid]'),s=document.getElementById('security');function u(){if(n&&n.selectedOptions[0]&&n.selectedOptions[0].dataset.enterprise==='1'){s.value='enterprise';s.dispatchEvent(new Event('change'))}}n&&n.addEventListener('change',u);u()})()</script></body></html>";
    static const char scan_failed_notice[] =
        "<p class=hint role=alert>Could not refresh Wi-Fi networks. Showing the previous list; please try again.</p>";
    static const char pairing_page[] =
        "<!doctype html><html><head><meta name=viewport content='width=device-width,initial-scale=1'>"
        "<style>body{font-family:system-ui,sans-serif;max-width:26rem;margin:2rem auto;padding:0 1rem;color:#102a43}"
        ".ok{padding:.8rem;background:#e8f7ef;border-radius:.5rem}label{display:block;margin:1rem 0 .3rem}"
        "input{box-sizing:border-box;width:100%;padding:.8rem;font-size:1.2rem;letter-spacing:.25rem}"
        "button{margin-top:1.3rem;padding:.8rem 1.2rem;font-size:1rem;background:#1769aa;color:#fff;border:0;border-radius:.4rem}</style>"
        "</head><body><h1>Restore MaClaw access</h1><p class=ok>The selected network is connected. The saved device token was rejected by the Hub.</p>"
        "<p>Generate a temporary code in MaClaw GUI. It is used once to retrieve a replacement device token.</p>"
        "<form method=post action=/save><input type=hidden name=reuse value=1>"
        "<label>New 6-digit pairing code</label><input name=code inputmode=numeric pattern='[0-9]{6}' maxlength=6 required autofocus>"
        "<button>Pair this device</button></form></body></html>";
    ESP_LOGI(TAG, "setup portal request: %s", req->uri);
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    if (device_connectivity_is_pairing_recovery_provisioning()) {
        return httpd_resp_send(req, pairing_page, HTTPD_RESP_USE_STRLEN);
    }
    if (!s_setup_options_mutex ||
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(2000)) != pdTRUE) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        return httpd_resp_sendstr(req, "Wi-Fi scan is in progress; please retry.");
    }
    char query[32] = {0};
    bool scan_failed = httpd_req_get_url_query_len(req) < sizeof(query) &&
                       httpd_req_get_url_query_str(req, query, sizeof(query)) == ESP_OK &&
                       !strcmp(query, "scan=failed");
    build_setup_saved_networks_html();
    if (httpd_resp_sendstr_chunk(req, setup_page_prefix) != ESP_OK ||
        (scan_failed && httpd_resp_sendstr_chunk(req, scan_failed_notice) != ESP_OK) ||
        httpd_resp_sendstr_chunk(req, s_setup_ssid_options) != ESP_OK ||
        httpd_resp_sendstr_chunk(req, setup_page_suffix) != ESP_OK ||
        (s_setup_saved_html && s_setup_saved_html[0] &&
         httpd_resp_sendstr_chunk(req, s_setup_saved_html) != ESP_OK) ||
        httpd_resp_sendstr_chunk(req, setup_page_tail) != ESP_OK) {
        xSemaphoreGive(s_setup_options_mutex);
        return ESP_FAIL;
    }
    esp_err_t err = httpd_resp_sendstr_chunk(req, NULL);
    xSemaphoreGive(s_setup_options_mutex);
    return err;
}

static esp_err_t captive_redirect_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    // A 302 is intentionally used here instead of a successful probe body:
    // the OS then identifies this as a captive network and presents its login
    // surface, which follows the redirect to the configuration page.
    // Captive probes arrive in parallel and are retried aggressively. Keep
    // the per-request trace out of the normal serial path so it cannot delay
    // the portal on constrained boards; enable debug logging when diagnosing
    // a particular phone/OS instead.
    ESP_LOGD(TAG, "captive probe: %s", req->uri);
    httpd_resp_set_status(req, "302 Found");
    httpd_resp_set_hdr(req, "Location", "http://" SETUP_AP_IP_ADDR "/");
    httpd_resp_set_type(req, "text/html; charset=utf-8");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    // Probe clients do not need a persistent HTTP connection. Closing it makes
    // the redirect deterministic for the small captive-portal web views used
    // by Android, iOS and Windows.
    httpd_resp_set_hdr(req, "Connection", "close");
    return httpd_resp_send(req, NULL, 0);
}

/* RFC 8908 Captive Portal API response for the DHCP option-114 URI.  Keeping
 * this distinct from the form is important: compliant clients parse JSON here
 * and use user-portal-url to open their captive-network surface. */
static esp_err_t captive_portal_api_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    httpd_resp_set_type(req, "application/captive+json");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    httpd_resp_set_hdr(req, "Connection", "close");
    return httpd_resp_sendstr(req,
        "{\"captive\":true,\"user-portal-url\":\"http://" SETUP_AP_IP_ADDR "/\"}");
}

static esp_err_t setup_refresh_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    // Refresh only on explicit user action.  Scanning on every GET would delay
    // the short captive-check requests that are meant to open this page.
    bool refreshed = refresh_setup_ssid_options();
    httpd_resp_set_status(req, "303 See Other");
    httpd_resp_set_hdr(req, "Location", refreshed ? "/" : "/?scan=failed");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req, "Refreshing Wi-Fi networks...");
}

static bool url_decode(const char *src, char *out, size_t cap) {
    size_t used = 0;
    for (; *src; src++) {
        if (used + 1 >= cap) return false;
        if (*src == '+') { out[used++] = ' '; continue; }
        if (*src == '%' && src[1] && src[2]) {
            char hex[] = {src[1], src[2], '\0'};
            char *end = NULL;
            long value = strtol(hex, &end, 16);
            if (!end || *end) return false;
            out[used++] = (char)value;
            src += 2;
            continue;
        }
        out[used++] = *src;
    }
    out[used] = '\0';
    return true;
}

static bool form_value(const char *body, const char *key, char *out, size_t cap) {
    char encoded[URL_CAPACITY + WIFI_VALUE_CAPACITY + 32];
    if (httpd_query_key_value(body, key, encoded, sizeof(encoded)) != ESP_OK) return false;
    return url_decode(encoded, out, cap);
}

/* The form body lived on the httpd task stack and, together with the
 * configuration write chain, overflowed its 6 KB stack mid-save (the
 * reboot swallowed every newly submitted pairing code).  esp_http_server
 * runs all handlers on its single task, so one PSRAM buffer is safe. */
#define SETUP_SAVE_BODY_CAPACITY 1536
static char *s_setup_save_body;

/* Form input can contain Wi-Fi/EAP credentials and pairing input.  Free it
 * only after HTTP has joined; scrub the form body first. */
static void release_setup_portal_scratch(void) {
    if (s_setup_save_body) {
        mbedtls_platform_zeroize(s_setup_save_body, SETUP_SAVE_BODY_CAPACITY);
        heap_caps_free(s_setup_save_body);
        s_setup_save_body = NULL;
    }
    if (s_setup_ssid_options) {
        heap_caps_free(s_setup_ssid_options);
        s_setup_ssid_options = NULL;
    }
    if (s_setup_ssid_choices) {
        heap_caps_free(s_setup_ssid_choices);
        s_setup_ssid_choices = NULL;
    }
    if (s_setup_scan_records) {
        heap_caps_free(s_setup_scan_records);
        s_setup_scan_records = NULL;
    }
    if (s_setup_saved_html) {
        heap_caps_free(s_setup_saved_html);
        s_setup_saved_html = NULL;
    }
}

/* 生成"已存网络"页面片段：只显示 ssid（永不输出密码），每条带删除按钮，
 * 删除走 POST /delete 写回 NVS。 */
static void build_setup_saved_networks_html(void) {
    if (!s_setup_saved_html) return;
    s_setup_saved_html[0] = '\0';
    if (!s_wifi_network_count) return;
    static const char header[] =
        "<h2>Saved networks</h2><p class=hint>The device connects to the strongest visible saved network automatically.</p>";
    size_t used = strlen(header);
    memcpy(s_setup_saved_html, header, used + 1);
    for (uint8_t i = 0; i < s_wifi_network_count; ++i) {
        static const char row_prefix[] = "<div class=saved><span>";
        static const char row_middle[] = "</span><form method=post action=/delete><input type=hidden name=ssid value=\"";
        static const char row_suffix[] = "\"><button type=submit>Delete</button></form></div>";
        // ssid 出现两次（显示文本 + 表单值），都按转义后的长度预留空间。
        size_t escaped = setup_html_escaped_length(s_wifi_networks[i].ssid);
        if (used + strlen(row_prefix) + strlen(row_middle) + strlen(row_suffix) +
                escaped * 2 + 1 >= SETUP_SAVED_HTML_CAPACITY) {
            break; // 空间不足时截断剩余条目，已生成部分仍可正常展示
        }
        memcpy(s_setup_saved_html + used, row_prefix, strlen(row_prefix));
        used += strlen(row_prefix);
        used = append_setup_html_escaped(s_setup_saved_html, used, SETUP_SAVED_HTML_CAPACITY,
                                         s_wifi_networks[i].ssid);
        memcpy(s_setup_saved_html + used, row_middle, strlen(row_middle));
        used += strlen(row_middle);
        used = append_setup_html_escaped(s_setup_saved_html, used, SETUP_SAVED_HTML_CAPACITY,
                                         s_wifi_networks[i].ssid);
        memcpy(s_setup_saved_html + used, row_suffix, strlen(row_suffix) + 1);
        used += strlen(row_suffix);
    }
}

static esp_err_t setup_save_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    if (!s_setup_save_body) {
        s_setup_save_body = heap_caps_malloc(SETUP_SAVE_BODY_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_save_body) {
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Out of memory");
        return ESP_FAIL;
    }
    char *body = s_setup_save_body;
    body[0] = 0;
    char ssid[WIFI_VALUE_CAPACITY] = {0}, password[WIFI_VALUE_CAPACITY] = {0},
         gateway[URL_CAPACITY] = {0}, code[PAIR_CODE_CAPACITY] = {0}, security[WIFI_EAP_MODE_CAPACITY] = "personal",
         eap_method[WIFI_EAP_MODE_CAPACITY] = "peap", identity[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0},
         username[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0}, ttls_phase2[WIFI_EAP_MODE_CAPACITY] = "mschapv2",
         ca_mode[WIFI_EAP_MODE_CAPACITY] = "system", server_domain[WIFI_ENTERPRISE_VALUE_CAPACITY] = {0};
    if (req->content_len <= 0 || req->content_len >= SETUP_SAVE_BODY_CAPACITY) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Form data is too large");
        return ESP_FAIL;
    }
    int received = 0;
    while (received < req->content_len) {
        int n = httpd_req_recv(req, body + received, req->content_len - received);
        if (n <= 0) {
            httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Could not receive the complete form");
            return ESP_FAIL;
        }
        received += n;
    }
    body[received] = '\0';
    char reuse[4] = {0};
    // Recovery preserves the already selected backhaul. On Wi-Fi boards this
    // means the saved station; on Fangtang 4G it means the ML307 connection.
    // The form field remains named "reuse" for wire compatibility.
    bool reuse_network = form_value(body, "reuse", reuse, sizeof(reuse)) && !strcmp(reuse, "1");
    if (reuse_network) {
        strlcpy(ssid, s_wifi_ssid, sizeof(ssid));
        strlcpy(password, s_wifi_password, sizeof(password));
        strlcpy(gateway, s_gateway_url, sizeof(gateway));
        strlcpy(security, s_wifi_security, sizeof(security));
        strlcpy(eap_method, s_wifi_eap_method, sizeof(eap_method));
        strlcpy(identity, s_wifi_identity, sizeof(identity));
        strlcpy(username, s_wifi_username, sizeof(username));
        strlcpy(ttls_phase2, s_wifi_ttls_phase2, sizeof(ttls_phase2));
        strlcpy(ca_mode, s_wifi_ca_mode, sizeof(ca_mode));
        strlcpy(server_domain, s_wifi_server_domain, sizeof(server_domain));
    }
    bool invalid_form = !form_value(body, "code", code, sizeof(code));
    if (!reuse_network) {
        invalid_form = invalid_form || !form_value(body, "ssid", ssid, sizeof(ssid)) ||
                       !form_value(body, "password", password, sizeof(password)) ||
                       !form_value(body, "gateway", gateway, sizeof(gateway)) ||
                       !form_value(body, "security", security, sizeof(security));
        if (!strcmp(security, "enterprise")) {
            invalid_form = invalid_form || !form_value(body, "eap_method", eap_method, sizeof(eap_method)) ||
                           !form_value(body, "identity", identity, sizeof(identity)) ||
                           !form_value(body, "username", username, sizeof(username)) ||
                           !form_value(body, "ttls_phase2", ttls_phase2, sizeof(ttls_phase2)) ||
                           !form_value(body, "ca_mode", ca_mode, sizeof(ca_mode)) ||
                           !form_value(body, "server_domain", server_domain, sizeof(server_domain));
        }
    }
    if (invalid_form) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Invalid form: check Wi-Fi and enterprise authentication fields");
        return ESP_FAIL;
    }
    bool selectable = false;
    if (!reuse_network && s_setup_options_mutex &&
        xSemaphoreTake(s_setup_options_mutex, pdMS_TO_TICKS(2000)) == pdTRUE) {
        selectable = setup_ssid_is_selectable(ssid);
        xSemaphoreGive(s_setup_options_mutex);
    }
    if (!reuse_network && (!is_valid_setup_selected_ssid(ssid) || !selectable)) {
        ESP_LOGW(TAG, "setup rejected SSID that was not in the current scan list");
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST,
                            "Select a Wi-Fi network from the list, then try again.");
        return ESP_FAIL;
    }
    // Recovery changes only the one-time pairing code. Never erase a persisted
    // device token merely because the portal was opened; the code exists only
    // to retrieve a token after authentication has conclusively failed.
    esp_err_t save_err = reuse_network ? save_pairing_code_only(code)
                                       : save_device_config(ssid, password, gateway, code, security, eap_method,
                                                            identity, username, ttls_phase2, ca_mode, server_domain);
    if (save_err != ESP_OK) {
        char reason[160];
        if (!ssid[0]) snprintf(reason, sizeof(reason), "Wi-Fi name is required");
        else if (strlen(ssid) > WIFI_SSID_MAX_LEN) snprintf(reason, sizeof(reason), "Wi-Fi name is too long (max 32 bytes)");
        else if (strlen(password) >= sizeof(s_wifi_password)) snprintf(reason, sizeof(reason), "Wi-Fi password is too long (max 64 bytes)");
        else if (!strcmp(security, "enterprise") && !username[0]) snprintf(reason, sizeof(reason), "Enterprise Wi-Fi username is required");
        else if (!is_valid_choice(security, "personal", "enterprise", NULL)) snprintf(reason, sizeof(reason), "Unsupported Wi-Fi security mode");
        else if (!is_valid_gateway_url(gateway)) snprintf(reason, sizeof(reason), "Hub URL must start with http:// or https://");
        else if (!is_six_digit_pair_code(code)) snprintf(reason, sizeof(reason), "Pairing code must be exactly 6 digits");
        else snprintf(reason, sizeof(reason), "Could not save configuration: %s", esp_err_to_name(save_err));
        ESP_LOGW(TAG, "setup rejected: %s", reason);
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, reason);
        return ESP_FAIL;
    }
    // Do not reset from the HTTP server task.  esp_http_server sends responses
    // asynchronously, so a reset here can race its final socket write and, on
    // this board, leave the setup QR frame on screen indefinitely.  Publish
    // the gated restart coordinator before claiming that a restart will occur:
    // a task/registry allocation failure cannot roll back the already durable
    // credentials, but it must not return a false "restarting" success while
    // the old portal remains live.  The worker itself waits for the response
    // flush and performs the terminal portal cleanup before resetting.
    if (!schedule_setup_restart()) {
        /* Configuration has committed but there is no registered owner left
         * that can safely perform the terminal cleanup/reset.  Keep the HTTP
         * server process alive solely long enough to return this request's
         * unambiguous error, but close admission before doing so.  A browser,
         * captive probe or delayed POST must not continue mutating a portal
         * generation whose persisted credentials no longer describe its
         * runtime state.  Do not call httpd_stop() from its own handler: that
         * would reintroduce the response-flush/self-join race the coordinator
         * exists to avoid.  The explicit recovery action is a manual reset.
         */
        set_setup_portal_http_admission(false);
        ESP_LOGE(TAG, "setup saved but restart coordinator could not start");
        httpd_resp_set_status(req, "500 Internal Server Error");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(
            req, "Configuration was saved, but automatic restart is unavailable. "
                 "Please restart the device manually.");
    }
    return httpd_resp_sendstr(req,
                              "Saved. The device is restarting and will connect to MaClaw.");
}

/* 删除已存热点：按 ssid 从多热点列表移除并写回 NVS。主凭据若正是被删的
 * 个人热点，服务侧会一并清除，避免重启后单凭据回退把它又连回去。 */
static esp_err_t setup_delete_handler(httpd_req_t *req) {
    if (!setup_portal_http_admission_open()) {
        httpd_resp_set_status(req, "503 Service Unavailable");
        httpd_resp_set_hdr(req, "Connection", "close");
        return httpd_resp_sendstr(req, "Setup portal is stopping.");
    }
    if (!s_setup_save_body) {
        s_setup_save_body = heap_caps_malloc(SETUP_SAVE_BODY_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_save_body) {
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Out of memory");
        return ESP_FAIL;
    }
    char *body = s_setup_save_body;
    if (req->content_len <= 0 || req->content_len >= SETUP_SAVE_BODY_CAPACITY) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Form data is too large");
        return ESP_FAIL;
    }
    int received = 0;
    while (received < req->content_len) {
        int n = httpd_req_recv(req, body + received, req->content_len - received);
        if (n <= 0) {
            httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Could not receive the complete form");
            return ESP_FAIL;
        }
        received += n;
    }
    body[received] = '\0';
    char ssid[WIFI_VALUE_CAPACITY] = {0};
    if (!form_value(body, "ssid", ssid, sizeof(ssid)) || !ssid[0]) {
        httpd_resp_send_err(req, HTTPD_400_BAD_REQUEST, "Missing network name");
        return ESP_FAIL;
    }
    esp_err_t err = configuration_service_delete_wifi_network(ssid);
    if (err == ESP_OK) {
        ESP_LOGI(TAG, "saved Wi-Fi deleted via portal: ssid_len=%u", (unsigned)strlen(ssid));
        // 同步运行时镜像；刷新失败不影响已落库的删除结果。
        (void)configuration_service_list_wifi_networks(
            s_wifi_networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &s_wifi_network_count);
        if (!strcmp(ssid, s_wifi_ssid) && !is_enterprise_wifi()) {
            s_wifi_ssid[0] = '\0';
            s_wifi_password[0] = '\0';
        }
    } else if (err != ESP_ERR_NOT_FOUND) {
        ESP_LOGW(TAG, "cannot delete saved Wi-Fi: %s", esp_err_to_name(err));
        httpd_resp_send_err(req, HTTPD_500_INTERNAL_SERVER_ERROR, "Could not delete the saved network");
        return ESP_FAIL;
    }
    // 删除成功（或本就不存在）都回门户首页刷新列表。
    httpd_resp_set_status(req, "303 See Other");
    httpd_resp_set_hdr(req, "Location", "/");
    httpd_resp_set_hdr(req, "Cache-Control", "no-store");
    return httpd_resp_sendstr(req, "Deleted.");
}

/* ESP-IDF HTTP server owns and joins its own worker/task set inside
 * httpd_stop().  The API has no caller-supplied bounded timeout, so this
 * narrow lifecycle slice deliberately owns only admission + handle teardown:
 * callers must invoke it before changing AP/DHCP/DNS state, but must not claim
 * that Wi-Fi/netif/event-loop deinitialization is now supported. */
static esp_err_t stop_setup_portal_http_server(void) {
    set_setup_portal_http_admission(false);
    httpd_handle_t server = s_setup_server;
    if (!server) return ESP_OK;
    s_setup_server = NULL;
    esp_err_t err = httpd_stop(server);
    if (err != ESP_OK) {
        /* A non-null handle is the only reliable indication that the server
         * remains live.  Restore it on failure so a later recovery/retry does
         * not accidentally create a second listener on the same port. */
        s_setup_server = server;
        ESP_LOGW(TAG, "cannot stop setup HTTP server: %s", esp_err_to_name(err));
    } else {
        ESP_LOGI(TAG, "setup HTTP server stopped");
    }
    return err;
}

/*
 * Provisioning shutdown has a strict, fail-closed dependency order:
 * admission -> HTTP -> DNS -> logical session -> credential scratch/lease.
 *
 * HTTP owns the portal handlers and DNS owns UDP/53.  If either join fails we
 * retain the remaining resources and keep the logical session active, because
 * clearing it would let other workers resume while an old credential handler
 * or captive responder can still run.  AP/STA, DHCP, netif, event-loop and
 * Wi-Fi driver lifetime are deliberately outside this narrow transaction.
 */
static esp_err_t stop_setup_portal_transaction_locked(uint32_t timeout_ms,
                                                      bool restore_wake_word) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    esp_err_t http_stop_err = stop_setup_portal_http_server();
    if (http_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "setup HTTP server did not stop: %s",
                 esp_err_to_name(http_stop_err));
        return http_stop_err;
    }
    /* `httpd_stop()` exposes no deadline in ESP-IDF.  It is the one documented
     * uncontrollable boundary of this transaction; the DNS join must consume
     * only the residual caller budget after it returns. */
    const uint32_t dns_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (dns_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t dns_stop_err = stop_captive_dns_task(dns_timeout_ms);
    if (dns_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "captive DNS did not stop: %s", esp_err_to_name(dns_stop_err));
        return dns_stop_err;
    }
    device_connectivity_end_provisioning();
    /* HTTP has joined and DNS has released UDP/53. No remaining worker may
     * dereference the session's form/scratch buffers. */
    release_setup_portal_scratch();
    if (s_setup_power_lease != DEVICE_POWER_LEASE_INVALID) {
        device_power_lease_release(s_setup_power_lease);
        s_setup_power_lease = DEVICE_POWER_LEASE_INVALID;
    }
    if (restore_wake_word) {
        esp_err_t wake_err = audio_wake_word_start(on_wake_word, NULL);
        if (wake_err != ESP_OK && wake_err != ESP_ERR_INVALID_STATE) {
            ESP_LOGW(TAG, "cannot restore offline wake after setup transaction: %s",
                     esp_err_to_name(wake_err));
        }
    }
    return ESP_OK;
}

/* Portal start and stop change the same generation's HTTP/DNS handles,
 * provisioning bit, scratch buffers and power lease.  Serialize that composite
 * ownership rather than relying on individual handles becoming non-null late
 * in startup.  `httpd_stop()` is still an ESP-IDF boundary without a caller
 * timeout; the lock merely makes its state transition linearizable with portal
 * starts. */
static esp_err_t stop_setup_portal_transaction(uint32_t timeout_ms,
                                               bool restore_wake_word) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    if (!s_setup_portal_mutex ||
        xSemaphoreTake(s_setup_portal_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    const uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    esp_err_t err = remaining_ms == 0
                        ? ESP_ERR_TIMEOUT
                        : stop_setup_portal_transaction_locked(remaining_ms, restore_wake_word);
    xSemaphoreGive(s_setup_portal_mutex);
    return err;
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
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    /* First stop application callback admission and drain a handler that was
     * already executing when teardown began.  ESP-IDF unregister posts its own
     * asynchronous cleanup event and has no caller deadline; after this drain
     * no callback can start a worker or touch Connectivity/UI while the radio,
     * default loop and netifs are being released. */
    const uint32_t callback_timeout_ms =
        startup_rollback_remaining_timeout_ms(deadline_us);
    if (callback_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t callback_stop_err =
        stop_wifi_event_callback_admission(callback_timeout_ms);
    if (callback_stop_err != ESP_OK) return callback_stop_err;
    const uint32_t sntp_timeout_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (sntp_timeout_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t err = stop_sntp_service(sntp_timeout_ms);
    if (err != ESP_OK) return err;

    if (s_wifi_driver_initialized && s_wifi_started) {
        err = esp_wifi_stop();
        if (err != ESP_OK && err != ESP_ERR_WIFI_NOT_STARTED) {
            ESP_LOGW(TAG, "cannot stop Wi-Fi driver: %s", esp_err_to_name(err));
            return err;
        }
        s_wifi_started = false;
        device_connectivity_set_wifi_ready(false);
    }
    if (s_wifi_event_instance) {
        err = esp_event_handler_instance_unregister(
            WIFI_EVENT, ESP_EVENT_ANY_ID, s_wifi_event_instance);
        if (err != ESP_OK) return err;
        s_wifi_event_instance = NULL;
    }
    if (s_wifi_got_ip_event_instance) {
        err = esp_event_handler_instance_unregister(
            IP_EVENT, IP_EVENT_STA_GOT_IP, s_wifi_got_ip_event_instance);
        if (err != ESP_OK) return err;
        s_wifi_got_ip_event_instance = NULL;
    }
    if (s_wifi_assigned_ip_event_instance) {
        err = esp_event_handler_instance_unregister(
            IP_EVENT, IP_EVENT_ASSIGNED_IP_TO_CLIENT, s_wifi_assigned_ip_event_instance);
        if (err != ESP_OK) return err;
        s_wifi_assigned_ip_event_instance = NULL;
    }
    if (s_setup_ap_netif) {
        esp_netif_destroy_default_wifi(s_setup_ap_netif);
        s_setup_ap_netif = NULL;
        s_ap_netif_created = false;
    }
    if (s_sta_netif) {
        esp_netif_destroy_default_wifi(s_sta_netif);
        s_sta_netif = NULL;
        s_sta_netif_created = false;
    }
    if (s_wifi_driver_initialized) {
        err = esp_wifi_deinit();
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot deinitialize Wi-Fi driver: %s", esp_err_to_name(err));
            return err;
        }
        s_wifi_driver_initialized = false;
        s_wifi_enterprise_enabled = false;
    }
    if (s_default_event_loop_created) {
        err = esp_event_loop_delete_default();
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot delete default event loop: %s", esp_err_to_name(err));
            return err;
        }
        s_default_event_loop_created = false;
    }
    if (s_netif_initialized) {
        err = esp_netif_deinit();
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot deinitialize esp-netif: %s", esp_err_to_name(err));
            return err;
        }
        s_netif_initialized = false;
    }
    s_network_initialized = false;
    return ESP_OK;
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

/* The portal shares one Wi-Fi driver with an already connected station.  A
 * failed portal must therefore restore the exact pre-entry radio shape rather
 * than blindly stopping Wi-Fi (which would unnecessarily disconnect a normal
 * station) or leaving an unauthenticated AP beacon after HTTP/DNS failed. */
typedef struct {
    bool captured;
    bool radio_changed;
    bool wifi_was_started;
    bool station_auto_connect;
    bool station_expected_disconnect;
    wifi_mode_t wifi_mode;
} setup_portal_radio_snapshot_t;

static esp_err_t capture_setup_portal_radio_snapshot(setup_portal_radio_snapshot_t *snapshot) {
    if (!snapshot) return ESP_ERR_INVALID_ARG;
    memset(snapshot, 0, sizeof(*snapshot));
    snapshot->wifi_was_started = s_wifi_started;
    snapshot->station_auto_connect = s_station_auto_connect;
    snapshot->station_expected_disconnect = s_station_expected_disconnect;
    esp_err_t err = esp_wifi_get_mode(&snapshot->wifi_mode);
    if (err != ESP_OK) return err;
    snapshot->captured = true;
    return ESP_OK;
}

static esp_err_t restore_setup_portal_radio_snapshot(
    const setup_portal_radio_snapshot_t *snapshot) {
    if (!snapshot || !snapshot->captured) return ESP_OK;

    /* Do not let an event callback reconnect while mode/start state is being
     * restored. Its policy flags are put back only after the hardware state
     * has converged. */
    s_station_auto_connect = false;
    s_station_expected_disconnect = true;
    esp_err_t err = ESP_OK;
    if (snapshot->radio_changed && s_wifi_started && !snapshot->wifi_was_started) {
        err = esp_wifi_stop();
        if (err != ESP_OK && err != ESP_ERR_WIFI_NOT_STARTED) {
            ESP_LOGW(TAG, "cannot stop failed setup radio: %s", esp_err_to_name(err));
            return err;
        }
        s_wifi_started = false;
        device_connectivity_set_wifi_ready(false);
    }
    if (snapshot->radio_changed) {
        err = esp_wifi_set_mode(snapshot->wifi_mode);
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot restore pre-setup Wi-Fi mode: %s", esp_err_to_name(err));
            return err;
        }
    }
    s_station_auto_connect = snapshot->station_auto_connect;
    s_station_expected_disconnect = snapshot->station_expected_disconnect;
    /* A prior running STA may have been disconnected deliberately while
     * entering the portal. Re-arm its normal recovery only after AP mode is
     * gone; a failure to reconnect remains the existing offline condition. */
    if (snapshot->wifi_was_started && snapshot->station_auto_connect &&
        (snapshot->wifi_mode == WIFI_MODE_STA || snapshot->wifi_mode == WIFI_MODE_APSTA)) {
        err = esp_wifi_connect();
        if (err != ESP_OK && err != ESP_ERR_WIFI_CONN) {
            ESP_LOGW(TAG, "cannot reconnect station after failed setup: %s",
                     esp_err_to_name(err));
        }
    }
    return ESP_OK;
}

static void recover_after_setup_portal_start_failure(
    bool wake_was_stopped, const setup_portal_radio_snapshot_t *radio_snapshot) {
    /* The portal stops MultiNet before allocating its HTTP server. If a
     * configured device fails to start its portal, restore the normal local
     * interaction path only after all portal borrowers have exited. */
    /* Called only from the serialized portal-start transaction.  Taking the
     * public portal lock again would deadlock the recovery path. */
    esp_err_t stop_err = stop_setup_portal_transaction_locked(500, wake_was_stopped);
    if (stop_err != ESP_OK) return;
    esp_err_t radio_err = restore_setup_portal_radio_snapshot(radio_snapshot);
    if (radio_err != ESP_OK) {
        ESP_LOGW(TAG, "failed setup radio remains fail-closed: %s", esp_err_to_name(radio_err));
    }
}

static void start_setup_portal(bool keep_station) {
    /* The visible portal result is best-effort, but resource mutation is not:
     * all portal generations enter through this gate.  Use a bounded wait so a
     * stalled ESP-IDF HTTP stop cannot block an input or event-loop task
     * forever.  The active generation remains intact/fail-closed on timeout. */
    if (!s_setup_portal_mutex ||
        xSemaphoreTake(s_setup_portal_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) {
        ESP_LOGW(TAG, "setup portal transition already in progress");
        return;
    }
    start_setup_portal_locked(keep_station);
    xSemaphoreGive(s_setup_portal_mutex);
}

static void start_setup_portal_locked(bool keep_station) {
    // Set this before any slow display or Wi-Fi operation. A button event can
    // be delivered by its independent task while the QR page is being drawn.
    if (setup_restart_is_pending()) {
        /* `/save` has durably committed a new configuration and the one
         * terminal coordinator owns the remaining cleanup/reset.  Starting a
         * fresh portal here would reopen DNS/HTTP admission against that old
         * generation and make the eventual reset non-linearizable. */
        ESP_LOGW(TAG, "setup portal start rejected: post-save reset is pending");
        app_ui_show_text("配置已保存", "设备正在重启，请稍候");
        return;
    }
    if (device_connectivity_is_provisioning_active() && s_setup_server &&
        setup_portal_http_admission_open()) {
        ESP_LOGI(TAG, "setup portal already active");
        return;
    }
    /* A stale handle means an earlier stop failed.  Do not stack a second HTTP
     * server on it; keep the existing listener fail-closed and surface a
     * recoverable configuration error instead. */
    if (s_setup_server) {
        set_setup_portal_http_admission(false);
        ESP_LOGE(TAG, "setup portal HTTP server is still active; refusing duplicate start");
        app_ui_show_text("设置失败", "网页服务正在恢复，请重启设备");
        return;
    }
    if (!s_setup_ssid_options) {
        s_setup_ssid_options = heap_caps_calloc(
            1, SETUP_SSID_OPTIONS_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_ssid_choices) {
        s_setup_ssid_choices = heap_caps_calloc(
            1, SETUP_SSID_CHOICES_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_scan_records) {
        s_setup_scan_records = heap_caps_calloc(
            SETUP_SCAN_MAX_APS, sizeof(*s_setup_scan_records),
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_saved_html) {
        s_setup_saved_html = heap_caps_calloc(
            1, SETUP_SAVED_HTML_CAPACITY, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_setup_ssid_options || !s_setup_ssid_choices || !s_setup_scan_records ||
        !s_setup_saved_html) {
        release_setup_portal_scratch();
        ESP_LOGE(TAG, "cannot allocate setup portal Wi-Fi list buffers");
        app_ui_show_text("设置失败", "内存不足，请重启后再试");
        return;
    }
    // A prior DNS responder owns UDP/53 independently of HTTP/AP state. Join
    // it before creating another responder; a timeout preserves its Registry
    // entry and rejects a competing bind instead of guessing it has exited.
    if (s_dns_task) {
        ESP_LOGW(TAG, "waiting for previous captive DNS task before starting portal");
        esp_err_t dns_stop_err = stop_captive_dns_task(1200);
        if (dns_stop_err != ESP_OK || s_dns_task) {
            ESP_LOGE(TAG, "previous captive DNS task did not exit: %s",
                     esp_err_to_name(dns_stop_err));
            app_ui_show_text("配置失败", "请重启设备后再试");
            return;
        }
    }
    /* A previous portal attempt may have intentionally closed DNS admission
     * while draining its socket.  This is the next explicit provisioning
     * transaction, so reopen only this worker's admission before creating its
     * new Registry entry; the wider portal/AP lifecycle remains unchanged. */
    taskENTER_CRITICAL(&s_task_state_lock);
    s_dns_admission_open = true;
    s_dns_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_setup_power_lease == DEVICE_POWER_LEASE_INVALID) {
        device_status_t lease_status = device_power_lease_acquire(
            DEVICE_POWER_LEASE_OWNER_PROVISIONING, &s_setup_power_lease);
        if (lease_status != DEVICE_STATUS_OK) {
            ESP_LOGE(TAG, "cannot acquire power lease for setup portal: status=%d",
                     (int)lease_status);
            app_ui_show_text("设置失败", "电源服务不可用，请重启后再试");
            return;
        }
        /* A boot-time portal can begin while an old ambient deadline has
         * already blanked the panel.  This is an approved foreground wake;
         * the QR/form render below owns the subsequent scene. */
        (void)device_power_wake_display_from_schedule();
    }
    device_connectivity_begin_provisioning(keep_station);
    // Provisioning has no use for the always-listening recognizer. Pause it
    // so it cannot compete for audio/I2S work while the captive portal runs.
    device_wake_word_pause(true);
    // Pairing recovery arrives here with Wi-Fi already associated, and the
    // offline recognizer has already been allocated. Give the small captive
    // portal its memory back before httpd_start(), otherwise the SoftAP can
    // appear while its configuration page fails to start.
    // Stop in both AP and AP+STA paths. start_setup_portal(false) is also used
    // after a configured station times out, by which point ESP-SR may already
    // be alive in future boot sequencing changes.
    esp_err_t wake_stop_err = audio_wake_word_stop();
    bool wake_was_stopped = wake_stop_err == ESP_OK;
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "cannot stop offline wake for setup portal: %s",
                 esp_err_to_name(wake_stop_err));
    }
    uint8_t mac[6];
    esp_err_t mac_err = esp_read_mac(mac, ESP_MAC_WIFI_SOFTAP);
    if (mac_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot read SoftAP MAC for setup portal: %s",
                 esp_err_to_name(mac_err));
        app_ui_show_text("Setup failed", "Network identity unavailable; restart device");
        return;
    }
    char ap_ssid[33];
    snprintf(ap_ssid, sizeof(ap_ssid), "MACLAW-SETUP-%02X%02X", mac[4], mac[5]);
    esp_err_t network_init_err = init_network();
    if (network_init_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot initialize network core for setup portal: %s",
                 esp_err_to_name(network_init_err));
        app_ui_show_text("Setup failed", "Network service unavailable; restart device");
        return;
    }
    /* Capture only after the driver exists. First-boot portal entry reaches
     * this point with an initialized-but-not-started Wi-Fi driver, whereas
     * esp_wifi_get_mode() before init would turn a normal setup path into a
     * false failure. */
    setup_portal_radio_snapshot_t radio_snapshot;
    esp_err_t radio_snapshot_err = capture_setup_portal_radio_snapshot(&radio_snapshot);
    if (radio_snapshot_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, NULL);
        ESP_LOGE(TAG, "cannot inspect Wi-Fi state before setup portal: %s",
                 esp_err_to_name(radio_snapshot_err));
        app_ui_show_text("Setup failed", "Network state unavailable; restart device");
        return;
    }
    ensure_setup_ap_netif();
    if (!s_setup_ap_netif) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot create setup AP netif");
        app_ui_show_text("Setup failed", "Network interface unavailable; restart device");
        return;
    }
    // Use the same AP address/DHCP sequence as the working Nulllab AI Vox3
    // provisioning component before any client can associate.
    esp_err_t ap_ip_err = configure_setup_ap_ip();
    if (ap_ip_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot configure setup AP/DHCP transaction: %s",
                 esp_err_to_name(ap_ip_err));
        app_ui_show_text("Setup failed", "Hotspot network unavailable; restart device");
        return;
    }
    // Bind the captive DNS responder before enabling the AP, matching the
    // working Nulllab implementation.  This closes the gap where a phone can
    // obtain a lease and send its first probe before DNS is listening.
    if (!start_captive_dns()) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot start captive DNS before setup hotspot");
        app_ui_show_text("设置失败", "配网 DNS 服务启动失败，请重启后再试");
        return;
    }
    // A failed first-time Wi-Fi join should show the full form again, even
    // though the submitted SSID is now persisted. Pairing recovery is the
    // only flow that intentionally reuses Wi-Fi and asks solely for a code.
    // Wi-Fi scans require a STA interface, so normal provisioning uses APSTA.
    // Fangtang cellular pairing recovery only asks for a new pairing code: its
    // ML307 remains the backhaul, and starting an unused STA/scan path here can
    // leave ESP-IDF's scan timer with a stale notification target. Use a plain
    // SoftAP for that one flow.
    bool cellular_pairing_ap_only = device_connectivity_is_active_cellular() &&
                                    device_connectivity_is_pairing_recovery_provisioning();
    if (!cellular_pairing_ap_only) {
        ensure_station_netif();
        if (!s_sta_netif) {
            recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
            ESP_LOGE(TAG, "cannot create setup station netif");
            app_ui_show_text("Setup failed", "Network interface unavailable; restart device");
            return;
        }
    }
    // A Fangtang in 4G mode still uses the Wi-Fi AP for local provisioning,
    // but its backhaul is the independent ML307.  Do not reconnect the saved
    // Wi-Fi STA while bringing up that AP: doing so races the setup scan and
    // needlessly runs two network bring-up paths at once.
    bool keep_wifi_station = device_connectivity_is_pairing_recovery_provisioning() &&
                             !device_connectivity_is_active_cellular();
    /* The station policy/disconnect are part of the same radio transaction as
     * APSTA selection. Mark the snapshot before changing either, so a later
     * set_mode failure does not strand a previously associated station. */
    radio_snapshot.radio_changed = true;
    s_station_auto_connect = keep_wifi_station;
    if (!keep_wifi_station && s_wifi_started) {
        s_station_expected_disconnect = true;
        esp_err_t disconnect_err = esp_wifi_disconnect();
        if (disconnect_err != ESP_OK && disconnect_err != ESP_ERR_WIFI_NOT_CONNECT) {
            s_station_expected_disconnect = false;
            ESP_LOGW(TAG, "cannot stop station while entering setup portal: %s",
                     esp_err_to_name(disconnect_err));
        }
    }
    wifi_config_t ap = { .ap = { .channel = 1, .max_connection = 4, .authmode = WIFI_AUTH_OPEN } };
    strlcpy((char *)ap.ap.ssid, ap_ssid, sizeof(ap.ap.ssid));
    ap.ap.ssid_len = strlen(ap_ssid);
    esp_err_t portal_err = esp_wifi_set_mode(cellular_pairing_ap_only
                                                 ? WIFI_MODE_AP
                                                 : WIFI_MODE_APSTA);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot enter setup Wi-Fi mode: %s", esp_err_to_name(portal_err));
        app_ui_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_config(WIFI_IF_AP, &ap);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot configure setup hotspot: %s", esp_err_to_name(portal_err));
        app_ui_show_text("设置失败", "请在网页重新设置");
        return;
    }
    portal_err = esp_wifi_set_ps(WIFI_PS_NONE);
    if (portal_err != ESP_OK) {
        ESP_LOGW(TAG, "cannot disable Wi-Fi power save for setup portal: %s",
                 esp_err_to_name(portal_err));
    }
    if (!s_wifi_started) {
        portal_err = esp_wifi_start();
        if (portal_err != ESP_OK) {
            recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
            ESP_LOGE(TAG, "cannot start setup hotspot: %s", esp_err_to_name(portal_err));
            app_ui_show_text("设置失败", "请在网页重新设置");
            return;
        }
        s_wifi_started = true;
    }
    // When the radio was already running in STA mode, set_mode(APSTA) and
    // set_config() do not always immediately publish the new SoftAP beacon.
    // Reconnect the AP interface explicitly and verify that it is active.
    if (keep_wifi_station) {
        esp_err_t connect_err = esp_wifi_connect();
        if (connect_err != ESP_OK && connect_err != ESP_ERR_WIFI_CONN) {
            ESP_LOGW(TAG, "station reconnect while enabling portal: %s", esp_err_to_name(connect_err));
        }
    }
    wifi_mode_t active_mode = WIFI_MODE_NULL;
    portal_err = esp_wifi_get_mode(&active_mode);
    if (portal_err != ESP_OK || (active_mode != WIFI_MODE_AP && active_mode != WIFI_MODE_APSTA)) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "setup hotspot did not enter AP mode: err=%s mode=%d",
                 esp_err_to_name(portal_err), (int)active_mode);
        app_ui_show_text("设置热点失败", "请重启后再试");
        return;
    }
    // Build the choice list before serving the form.  The scan is performed
    // only once per portal entry, keeping the SoftAP responsive while the
    // phone completes captive-portal checks.
    if (!cellular_pairing_ap_only) refresh_setup_ssid_options();
    httpd_config_t server_config = HTTPD_DEFAULT_CONFIG();
    // ESP-SR consumes a meaningful part of internal RAM. IDF 6 needs more than
    // the default 4 KB while serving the setup form. This task must remain in
    // internal RAM because the handler writes NVS and flash operations disable
    // the external-RAM cache while checking the current task stack.
    server_config.stack_size = 6144;
    // Fourteen platform-specific captive-check endpoints plus the RFC 8908
    // API, exact root, refresh, GET redirect fallback, POST /save and POST
    // /delete. iOS, Android, Windows and Firefox vary by OS version, carrier
    // and whether they are retrying a prior probe.
    // This capacity is checked when routes are registered at runtime.
    server_config.max_uri_handlers = 25;
    // Captive checks are usually parallel.  Keep enough connections for their
    // redirects and the portal web view to coexist; otherwise a probe can
    // occupy the tiny server while the OS decides the hotspot has no portal.
    server_config.max_open_sockets = 5;
    server_config.lru_purge_enable = true;
    // Make the AP behave like a captive portal. Android, iOS and Windows all
    // probe different HTTP paths before showing the setup page; unknown GET
    // paths redirect to the root form rather than receiving a successful 200
    // response that could be mistaken for unrestricted internet access.
    server_config.uri_match_fn = httpd_uri_match_wildcard;
    portal_err = httpd_start(&s_setup_server, &server_config);
    if (portal_err != ESP_OK) {
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        ESP_LOGE(TAG, "cannot start setup web server: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        app_ui_show_text("设置失败", "网页服务内存不足，请重启");
        return;
    }
    /* The handle is live before route registration, but requests cannot be
     * admitted until the complete route set has registered successfully. */
    set_setup_portal_http_admission(false);
    httpd_uri_t apple_success = {.uri = "/hotspot-detect.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t apple_library_success = {.uri = "/library/test/success.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    // Match query/path variants too. Waveshare's reference portal registers
    // this as a wildcard because Android adds a cache-busting suffix on some
    // releases; serving the normal setup HTML with 200 in that case prevents
    // Android from classifying the AP as captive.
    httpd_uri_t android_generate_204 = {.uri = "/generate_204*", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_gen_204 = {.uri = "/gen_204", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_redirect = {.uri = "/redirect", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t android_mobile_status = {.uri = "/mobile/status.php", .method = HTTP_GET, .handler = captive_redirect_handler};
    // Recent Android NetworkMonitor uses this probe. It must receive the same
    // redirect rather than the setup page's 200 response, which otherwise
    // makes the user open 192.168.4.1 manually.
    httpd_uri_t android_canonical = {.uri = "/canonical.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_connect = {.uri = "/connecttest.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    // Older Windows releases use the NCSI probe host/path rather than
    // msftconnecttest.com/connecttest.txt.  It needs the same redirect to
    // make the operating system open its captive-network sign-in surface.
    httpd_uri_t windows_ncsi = {.uri = "/ncsi.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_network_status = {.uri = "/check_network_status.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t windows_fwlink = {.uri = "/fwlink/", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t firefox_connectivity = {.uri = "/connectivity-check.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_success = {.uri = "/success.txt", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t generic_portal = {.uri = "/portal.html", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t captive_portal_api = {.uri = "/captive-portal/api", .method = HTTP_GET, .handler = captive_portal_api_handler};
    // ESP-IDF wildcard matching treats "/*" as paths with a slash after the
    // root; register the exact root separately so a direct 192.168.4.1 request
    // and the redirect target never depend on wildcard edge-case behaviour.
    httpd_uri_t root = {.uri = "/", .method = HTTP_GET, .handler = setup_get_handler};
    httpd_uri_t refresh = {.uri = "/refresh", .method = HTTP_GET, .handler = setup_refresh_handler};
    httpd_uri_t captive = {.uri = "/*", .method = HTTP_GET, .handler = captive_redirect_handler};
    httpd_uri_t save = {.uri = "/save", .method = HTTP_POST, .handler = setup_save_handler};
    httpd_uri_t delete_saved = {.uri = "/delete", .method = HTTP_POST, .handler = setup_delete_handler};
    // Register the wildcard last: ESP-IDF preserves registration order during
    // matching, so it must not shadow the platform-specific probe routes.
    portal_err = httpd_register_uri_handler(s_setup_server, &apple_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &apple_library_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_generate_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_gen_204);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_redirect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_mobile_status);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &android_canonical);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_connect);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_ncsi);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_network_status);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &windows_fwlink);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &firefox_connectivity);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &generic_success);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &generic_portal);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &captive_portal_api);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &root);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &refresh);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &captive);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &save);
    if (portal_err == ESP_OK) portal_err = httpd_register_uri_handler(s_setup_server, &delete_saved);
    if (portal_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register setup portal routes: %s, free_heap=%u",
                 esp_err_to_name(portal_err), (unsigned)esp_get_free_heap_size());
        recover_after_setup_portal_start_failure(wake_was_stopped, &radio_snapshot);
        app_ui_show_text("设置失败", "配置网页路由启动失败");
        return;
    }
    set_setup_portal_http_admission(true);
    if (device_connectivity_is_pairing_recovery_provisioning()) {
        app_ui_show_text("设备配对设置", ap_ssid);
    } else {
        show_setup_qrcode(ap_ssid);
    }
    ESP_LOGI(TAG, "%s portal ready: join %s and open http://192.168.4.1",
             device_connectivity_is_pairing_recovery_provisioning() ? "pairing recovery" : "setup",
             ap_ssid);
}

static void wifi_event(void *arg, esp_event_base_t base, int32_t id, void *data) {
    (void)arg;
    if (!wifi_event_callback_enter()) return;
    if (base == WIFI_EVENT && id == WIFI_EVENT_AP_STACONNECTED) {
        const wifi_event_ap_staconnected_t *event = data;
        if (event) {
            ESP_LOGI(TAG, "setup client associated: " MACSTR, MAC2STR(event->mac));
        }
        goto finish;
    }
    if (base == IP_EVENT && id == IP_EVENT_ASSIGNED_IP_TO_CLIENT) {
        const ip_event_assigned_ip_to_client_t *event = data;
        char address[16] = {0};
        if (event) esp_ip4addr_ntoa(&event->ip, address, sizeof(address));
        ESP_LOGI(TAG, "setup client leased IP=%s hostname=%s", address[0] ? address : "unknown",
                 event && event->hostname[0] ? event->hostname : "unknown");
        goto finish;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        if (s_station_auto_connect) esp_wifi_connect();
        goto finish;
    }
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        const wifi_event_sta_disconnected_t *event = data;
        char disconnected_ssid[sizeof(event->ssid) + 1] = {0};
        if (event) memcpy(disconnected_ssid, event->ssid, sizeof(event->ssid));
        bool accepted = event && device_connectivity_observe_wifi_disconnected(
                                     disconnected_ssid);
        if (!accepted) {
            ESP_LOGW(TAG, "ignoring Wi-Fi disconnect outside current attempt");
            goto finish;
        }
        app_ui_set_wifi_status(s_wifi_ssid, false);
        app_ui_set_service_ready(false);
        firmware_identity_set_service_ready(false);
        if (s_station_expected_disconnect) {
            s_station_expected_disconnect = false;
            ESP_LOGI(TAG, "station disconnected for setup scan");
            goto finish;
        }
        if (s_station_auto_connect) {
            ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retrying", s_wifi_ssid);
            esp_wifi_connect();
        }
        goto finish;
    }
    if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        wifi_ap_record_t connected_ap = {0};
        bool accepted = false;
        if (esp_wifi_sta_get_ap_info(&connected_ap) == ESP_OK) {
            char connected_ssid[sizeof(connected_ap.ssid) + 1];
            memcpy(connected_ssid, connected_ap.ssid, sizeof(connected_ap.ssid));
            connected_ssid[sizeof(connected_ap.ssid)] = '\0';
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
        taskENTER_CRITICAL(&s_task_state_lock);
        bool recover_gateway = s_gateway_startup_allowed &&
                               !s_gateway_startup_running &&
                               !s_startup_sequence_complete &&
                                !device_connectivity_is_provisioning_active();
        taskEXIT_CRITICAL(&s_task_state_lock);
        if (recover_gateway) {
            ESP_LOGI(TAG, "starting gateway startup after Wi-Fi recovery");
            if (!start_gateway_startup_task()) {
                ESP_LOGE(TAG, "cannot restart gateway startup after Wi-Fi recovery");
            }
        }
    }
finish:
    wifi_event_callback_leave();
}

static bool cellular_recovery_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_cellular_recovery_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static void cellular_recovery_task(void *arg) {
    (void)arg;
    if (!s_cellular_recovery_start_gate ||
        xSemaphoreTake(s_cellular_recovery_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "ML307 recovery start gate unavailable");
        goto finish;
    }
    if (cellular_recovery_stop_requested()) goto finish;
    uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
    bool needs_gateway_restart = !device_connectivity_is_cellular_transport_ready();
    while (!cellular_recovery_stop_requested() &&
           !device_connectivity_is_provisioning_active() &&
           device_connectivity_is_active_cellular()) {
        if (!device_connectivity_is_cellular_transport_ready()) {
            device_connectivity_set_cellular_ready(false);
            needs_gateway_restart = true;
            app_ui_set_wifi_status("4G", false);
            app_ui_set_service_ready(false);
            firmware_identity_set_service_ready(false);
            device_status_t transport_status =
                device_connectivity_start_cellular_transport(CELLULAR_CONNECT_TIMEOUT_MS);
            if (transport_status != DEVICE_STATUS_OK) {
                ESP_LOGW(TAG, "cellular recovery failed: device status=%d; retry in %lu ms",
                         transport_status, (unsigned long)retry_ms);
                (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
                if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                    retry_ms *= 2;
                    if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
                }
                continue;
            }
            device_connectivity_set_cellular_ready(true);
            app_ui_set_wifi_status("4G", true);
            ESP_LOGI(TAG, "ML307 network recovered");
        }

        retry_ms = GATEWAY_RETRY_INITIAL_MS;
        if (!cellular_recovery_stop_requested() && needs_gateway_restart &&
            !s_gateway_startup_running &&
            (s_gateway_token[0] || s_pair_code[0])) {
            ESP_LOGI(TAG, "restarting gateway startup after ML307 recovery");
            if (start_gateway_startup_task()) needs_gateway_restart = false;
        }
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000));
    }
finish:
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_cellular_recovery_task == self) s_cellular_recovery_task = NULL;
    s_cellular_recovery_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_cellular_recovery_stopped) xSemaphoreGive(s_cellular_recovery_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
}

static esp_err_t stop_cellular_recovery_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_cellular_recovery_admission_open = false;
    s_cellular_recovery_stop_requested = true;
    task = s_cellular_recovery_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(task);
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_cellular_recovery_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_cellular_recovery_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "ML307 recovery coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_cellular_recovery_registry_entry(void *context,
                                                        uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_cellular_recovery_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    /* The task handle is immutable registry identity. A stale entry must not
     * be allowed to stop a newer coordinator. */
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_cellular_recovery_task(timeout_ms);
}

static bool ensure_cellular_recovery_task(void) {
    taskENTER_CRITICAL(&s_task_state_lock);
    bool admission_open = s_cellular_recovery_admission_open;
    bool already_starting = s_cellular_recovery_starting || s_cellular_recovery_task != NULL;
    if (admission_open && !already_starting) s_cellular_recovery_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!admission_open) {
        ESP_LOGW(TAG, "ML307 recovery start rejected: lifecycle admission is closed");
        return false;
    }
    if (already_starting) return true;
    if (!s_cellular_recovery_start_gate || !s_cellular_recovery_stopped) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_cellular_recovery_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "ML307 recovery lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_cellular_recovery_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_cellular_recovery_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreatePinnedToCore(
        cellular_recovery_task, "maclaw_cellular_recovery", 6144,
        NULL, 3, &task, 1);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_cellular_recovery_task = NULL;
        s_cellular_recovery_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot start ML307 recovery task");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_cellular_recovery_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "cellular_recovery",
        .context = (void *)task,
        .stop = stop_cellular_recovery_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register ML307 recovery coordinator: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_cellular_recovery_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_cellular_recovery_start_gate);
        (void)stop_cellular_recovery_task(500);
        return false;
    }
    xSemaphoreGive(s_cellular_recovery_start_gate);
    return true;
}

static bool start_cellular(void) {
    /* A new start attempt invalidates any readiness observed from an older
     * ML307 session before the adapter touches its pins or UART. */
    device_connectivity_set_cellular_ready(false);
    device_status_t preparation = device_connectivity_prepare_cellular_transport();
    /* A missing cellular profile is a configuration error. Other transport
     * failures are handled below after the shared status surfaces are reset. */
    if (preparation != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cellular transport is unavailable: device status=%d",
                 preparation);
        app_ui_show_text("4G 未配置", "请先确认模块 UART、供电与控制引脚");
        return false;
    }
    // ML307 is controlled through its native AT HTTP/HTTPS/TCP stack. It does
    // not implement the generic ATD*99# PPP path used by esp_modem.
    app_ui_set_wifi_status("4G", false);
    app_ui_set_service_ready(false);
    firmware_identity_set_service_ready(false);
    preparation =
        device_connectivity_start_cellular_transport(CELLULAR_CONNECT_TIMEOUT_MS);
    if (preparation != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cellular transport start failed: device status=%d", preparation);
        app_ui_show_text("4G 模块未响应", "检查 SIM、供电与天线");
        (void)ensure_cellular_recovery_task();
        return false;
    }
    device_connectivity_set_cellular_ready(true);
    app_ui_set_wifi_status("4G", true);
    ESP_LOGI(TAG, "ML307 native network ready");
    (void)ensure_cellular_recovery_task();
    return true;
}

/* 多热点自动选网：扫描可见 AP，把已存热点按最强 RSSI 从强到弱排序后逐个
 * 尝试连接，某个热点拿到 IP 即返回 true。扫描失败、没有可见的已存热点或
 * 全部连接失败时返回 false，由调用方回退到原单凭据连接流程（失败后的
 * "网络暂时不可用"/配网逻辑保持不变）。 */
static bool start_wifi_saved_list(void) {
    wifi_ap_record_t *records =
        heap_caps_calloc(SETUP_SCAN_MAX_APS, sizeof(*records),
                         MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!records) return false;
    // 逐个尝试期间禁止断线回调自动重连，避免它抢连尚未切换的旧配置。
    s_station_auto_connect = false;
    if (!s_wifi_started) {
        esp_err_t start_err = esp_wifi_start();
        if (start_err != ESP_OK) {
            ESP_LOGW(TAG, "cannot start Wi-Fi for saved-network scan: %s",
                     esp_err_to_name(start_err));
            heap_caps_free(records);
            s_station_auto_connect = true;
            return false;
        }
        s_wifi_started = true;
    }
    wifi_scan_config_t scan_config = { .show_hidden = false };
    esp_err_t err = esp_wifi_scan_start(&scan_config, true);
    uint16_t ap_count = SETUP_SCAN_MAX_APS;
    if (err == ESP_OK) {
        err = esp_wifi_scan_get_ap_records(&ap_count, records);
    }
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "Wi-Fi scan for saved networks failed: %s", esp_err_to_name(err));
        heap_caps_free(records);
        s_station_auto_connect = true;
        return false;
    }
    // 已存热点按可见 AP 的最强 RSSI 排序（候选最多 5 个，直接插入排序）。
    uint8_t order[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    int8_t best_rssi[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t candidate_count = 0;
    for (uint8_t i = 0; i < s_wifi_network_count; ++i) {
        int8_t rssi = INT8_MIN;
        for (uint16_t ap = 0; ap < ap_count; ++ap) {
            // 扫描记录的 ssid 达到 32 字节时没有 NUL 结尾，按定长比较。
            if (!strncmp((const char *)records[ap].ssid, s_wifi_networks[i].ssid,
                         sizeof(records[ap].ssid)) &&
                records[ap].rssi > rssi) {
                rssi = records[ap].rssi;
            }
        }
        if (rssi == INT8_MIN) continue; // 当前不可见
        uint8_t pos = candidate_count;
        while (pos > 0 && best_rssi[pos - 1] < rssi) {
            order[pos] = order[pos - 1];
            best_rssi[pos] = best_rssi[pos - 1];
            --pos;
        }
        order[pos] = i;
        best_rssi[pos] = rssi;
        ++candidate_count;
    }
    heap_caps_free(records);
    if (!candidate_count) {
        ESP_LOGI(TAG, "no saved Wi-Fi network is currently visible");
        s_station_auto_connect = true;
        return false;
    }
    bool connected = false;
    for (uint8_t c = 0; c < candidate_count && !connected; ++c) {
        const configuration_wifi_network_t *network = &s_wifi_networks[order[c]];
        ESP_LOGI(TAG, "trying saved Wi-Fi %s (%d dBm)", network->ssid, (int)best_rssi[c]);
        strlcpy(s_wifi_ssid, network->ssid, sizeof(s_wifi_ssid));
        strlcpy(s_wifi_password, network->password, sizeof(s_wifi_password));
        wifi_config_t config = { .sta = { .threshold.authmode = WIFI_AUTH_WPA2_PSK } };
        strlcpy((char *)config.sta.ssid, s_wifi_ssid, sizeof(config.sta.ssid));
        strlcpy((char *)config.sta.password, s_wifi_password, sizeof(config.sta.password));
        esp_err_t config_err = esp_wifi_set_config(WIFI_IF_STA, &config);
        if (config_err != ESP_OK) {
            ESP_LOGW(TAG, "cannot configure saved Wi-Fi %s: %s", s_wifi_ssid,
                     esp_err_to_name(config_err));
            continue;
        }
        app_ui_set_wifi_status(s_wifi_ssid, false);
        if (c > 0) {
            esp_err_t disconnect_err = esp_wifi_disconnect();
            if (disconnect_err != ESP_OK && disconnect_err != ESP_ERR_WIFI_NOT_CONNECT) {
                ESP_LOGW(TAG, "cannot switch saved Wi-Fi candidate: %s",
                         esp_err_to_name(disconnect_err));
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
        esp_err_t connect_err = esp_wifi_connect();
        if (connect_err != ESP_OK && connect_err != ESP_ERR_WIFI_CONN) {
            ESP_LOGW(TAG, "cannot start saved Wi-Fi attempt: %s",
                     esp_err_to_name(connect_err));
            continue; // 发起失败直接试下一个候选
        }
        connected = device_connectivity_wait_wifi_attempt(
            attempt_epoch, WIFI_CANDIDATE_CONNECT_TIMEOUT_MS);
        if (!connected) {
            ESP_LOGW(TAG, "saved Wi-Fi %s did not connect within %u ms",
                     s_wifi_ssid, WIFI_CANDIDATE_CONNECT_TIMEOUT_MS);
        }
    }
    s_station_auto_connect = true;
    if (!connected) {
        // 全部失败：保持原有后台自动重连行为（以最后一个候选继续重试）。
        device_connectivity_set_wifi_ready(false);
        app_ui_set_wifi_status(s_wifi_ssid, false);
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
    ensure_station_netif();
    if (!s_sta_netif) {
        ESP_LOGE(TAG, "cannot create Wi-Fi station netif");
        return false;
    }
    s_station_auto_connect = true;
    s_station_expected_disconnect = false;
    bool enterprise = is_enterprise_wifi();
    wifi_config_t config = { .sta = { .threshold.authmode = enterprise ? WIFI_AUTH_WPA2_ENTERPRISE : WIFI_AUTH_WPA2_PSK } };
    strlcpy((char *)config.sta.ssid, s_wifi_ssid, sizeof(config.sta.ssid));
    if (!enterprise) strlcpy((char *)config.sta.password, s_wifi_password, sizeof(config.sta.password));
    esp_err_t wifi_err = esp_wifi_set_mode(s_setup_server ? WIFI_MODE_APSTA : WIFI_MODE_STA);
    if (wifi_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot select Wi-Fi station mode: %s", esp_err_to_name(wifi_err));
        return false;
    }
    // The connected router's 802.11n management traffic triggers a double
    // exception in this ESP-IDF 6.0.2 S3 Wi-Fi binary immediately after DHCP.
    // EchoEar needs reliability rather than throughput, so use legacy b/g
    // station negotiation until that driver path is upgraded.
    wifi_err = esp_wifi_set_protocol(WIFI_IF_STA, WIFI_PROTOCOL_11B | WIFI_PROTOCOL_11G);
    if (wifi_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot select Wi-Fi station protocol: %s", esp_err_to_name(wifi_err));
        return false;
    }
    wifi_err = esp_wifi_set_config(WIFI_IF_STA, &config);
    if (wifi_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot configure Wi-Fi station: %s", esp_err_to_name(wifi_err));
        return false;
    }
    // ESP-IDF 6.0.2's modem-sleep path can tear down the PHY's esp_timer while
    // its ISR is still armed on this S3 build. The resulting task-notify ISR
    // assertion occurs just after association and reboots the device. EchoEar
    // is USB-powered during normal use, so keep station power-save disabled.
    esp_err_t ps_err = esp_wifi_set_ps(WIFI_PS_NONE);
    if (ps_err != ESP_OK) {
        ESP_LOGW(TAG, "cannot disable Wi-Fi power save: %s", esp_err_to_name(ps_err));
    }
    /* 多热点：个人网络且已存列表非空时，先扫描并连接可见的最强已存热点。
     * 扫描失败、无可见已存热点或全部连接失败都会落回下方原单凭据流程，
     * 保持"网络暂时不可用"与后台重连行为不变。 */
    if (!enterprise && s_wifi_network_count > 0 && start_wifi_saved_list()) {
        return true;
    }
    if (enterprise) {
        // Android/iOS-style defaults: PEAP + MSCHAPv2, username as identity
        // when anonymous identity is omitted, and platform trust anchors.
        const char *identity = s_wifi_identity[0] ? s_wifi_identity : s_wifi_username;
        esp_eap_method_t method = !strcmp(s_wifi_eap_method, "ttls") ? ESP_EAP_TYPE_TTLS : ESP_EAP_TYPE_PEAP;
        wifi_err = esp_eap_client_set_identity((const unsigned char *)identity, strlen(identity));
        if (wifi_err != ESP_OK) goto enterprise_config_failed;
        wifi_err = esp_eap_client_set_username((const unsigned char *)s_wifi_username,
                                               strlen(s_wifi_username));
        if (wifi_err != ESP_OK) goto enterprise_config_failed;
        wifi_err = esp_eap_client_set_password((const unsigned char *)s_wifi_password,
                                               strlen(s_wifi_password));
        if (wifi_err != ESP_OK) goto enterprise_config_failed;
        if (!strcmp(s_wifi_eap_method, "ttls")) {
            wifi_err = esp_eap_client_set_ttls_phase2_method(
                !strcmp(s_wifi_ttls_phase2, "pap") ? ESP_EAP_TTLS_PHASE2_PAP :
                                                       ESP_EAP_TTLS_PHASE2_MSCHAPV2);
            if (wifi_err != ESP_OK) goto enterprise_config_failed;
        }
        if (!strcmp(s_wifi_ca_mode, "system")) {
            wifi_err = esp_eap_client_use_default_cert_bundle(true);
            if (wifi_err != ESP_OK) goto enterprise_config_failed;
        }
        if (s_wifi_server_domain[0]) {
            wifi_err = esp_eap_client_set_domain_name(s_wifi_server_domain);
            if (wifi_err != ESP_OK) goto enterprise_config_failed;
        }
        wifi_err = esp_eap_client_set_eap_methods(method);
        if (wifi_err != ESP_OK) goto enterprise_config_failed;
        wifi_err = esp_wifi_sta_enterprise_enable();
        if (wifi_err != ESP_OK) goto enterprise_config_failed;
        s_wifi_enterprise_enabled = true;
    } else {
        // Enterprise state can only exist after a prior runtime enterprise
        // connection. Do not call this API on a cold personal-Wi-Fi boot:
        // ESP-IDF 6.0.2 can assert from the scan timer in that case.
        if (s_wifi_enterprise_enabled) {
            esp_err_t eap_err = esp_wifi_sta_enterprise_disable();
            if (eap_err != ESP_OK) {
                ESP_LOGW(TAG, "cannot disable prior enterprise Wi-Fi state: %s",
                         esp_err_to_name(eap_err));
            }
            s_wifi_enterprise_enabled = false;
        }
    }
    /* Start a fresh Connectivity-owned readiness session before any station
     * operation can synchronously emit a DHCP event. */
    uint32_t attempt_epoch = device_connectivity_begin_wifi_attempt(s_wifi_ssid);
    if (attempt_epoch == 0) {
        ESP_LOGE(TAG, "cannot create Wi-Fi readiness attempt");
        return false;
    }
    app_ui_set_wifi_status(s_wifi_ssid, false);
    if (!s_wifi_started) {
        wifi_err = esp_wifi_start();
        if (wifi_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot start Wi-Fi station: %s", esp_err_to_name(wifi_err));
            return false;
        }
        s_wifi_started = true;
    } else {
        wifi_err = esp_wifi_connect();
        if (wifi_err != ESP_OK && wifi_err != ESP_ERR_WIFI_CONN) {
            ESP_LOGE(TAG, "cannot connect Wi-Fi station: %s", esp_err_to_name(wifi_err));
            return false;
        }
    }
    if (device_connectivity_wait_wifi_attempt(attempt_epoch, WIFI_CONNECT_TIMEOUT_MS)) {
        return true;
    }
    app_ui_set_wifi_status(s_wifi_ssid, false);
    ESP_LOGW(TAG, "Wi-Fi did not connect within %u ms: %s", WIFI_CONNECT_TIMEOUT_MS, s_wifi_ssid);
    return false;

enterprise_config_failed:
    /* EAP setup can fail for malformed credentials, missing certificate
     * support, or a transient driver state.  It must not reboot the device;
     * readiness remains false and the regular Connectivity recovery policy
     * can surface the fault.  Only undo enterprise mode if this attempt had
     * actually enabled it, avoiding the IDF cold-personal-Wi-Fi disable bug. */
    ESP_LOGE(TAG, "cannot configure enterprise Wi-Fi: %s", esp_err_to_name(wifi_err));
    if (s_wifi_enterprise_enabled) {
        esp_err_t disable_err = esp_wifi_sta_enterprise_disable();
        if (disable_err != ESP_OK) {
            ESP_LOGW(TAG, "cannot undo failed enterprise Wi-Fi setup: %s",
                     esp_err_to_name(disable_err));
        } else {
            s_wifi_enterprise_enabled = false;
        }
    }
    return false;
}

static bool gateway_startup_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_task_state_lock);
    requested = s_gateway_startup_stop_requested;
    taskEXIT_CRITICAL(&s_task_state_lock);
    return requested;
}

static void gateway_startup_task(void *arg) {
    (void)arg;
    if (!s_gateway_startup_start_gate ||
        xSemaphoreTake(s_gateway_startup_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "gateway startup start gate unavailable");
        goto finish;
    }
    if (gateway_startup_stop_requested()) goto finish;
    // Startup remains the clean ambient pet face. Connection progress belongs
    // in the serial log; it must never cover the clock, weather or pet.
    ESP_LOGI(TAG, "gateway startup: url=%s paired=%s pair_code=%s", s_gateway_url, s_gateway_token[0] ? "yes" : "no", s_pair_code[0] ? "present" : "missing");
    // A pending one-time code always takes precedence. It is consumed exactly
    // once to obtain/replace the durable gateway token, then erased by
    // pair_by_code(). Normal boots with no pending code use only the token.
    if (s_pair_code[0]) {
        pet("thinking");
        app_ui_show_text("设备配对", "正在连接码卡龙界面");
        ESP_LOGI(TAG, "gateway pairing request starting");
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        bool paired = false;
        while (!gateway_startup_stop_requested()) {
            ++attempt;
            esp_err_t err = paired ? gateway_handshake(true) : pair_by_code();
            if (gateway_startup_stop_requested()) break;
            if (err == ESP_OK) {
                if (!paired) {
                    paired = true;
                    attempt = 0;
                    retry_ms = GATEWAY_RETRY_INITIAL_MS;
                    continue;
                }
                (void)start_gateway_ready_tasks();
                apply_deferred_startup_pet_asset();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                pet("alert");
                app_ui_show_text(paired ? "令牌认证失败" : "配对码已失效",
                                     "请检查或重新配对");
                start_setup_portal(true);
                break;
            }
            // Preserve the boot surface while the Hub or network is temporarily
            // unavailable. Pet/standby is published only after Welcome + wake.
            app_ui_show_startup_screen();
            ESP_LOGW(TAG, "gateway %s attempt %u failed: %s; retry in %lu ms",
                     paired ? "handshake" : "pairing", attempt, esp_err_to_name(err),
                     (unsigned long)retry_ms);
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    } else if (!s_gateway_token[0]) {
        if (!gateway_startup_stop_requested()) {
            pet("quiet");
            app_ui_show_text("设备未配对", "正在开启配对热点");
            start_setup_portal(true);
        }
    } else {
        uint32_t retry_ms = GATEWAY_RETRY_INITIAL_MS;
        unsigned attempt = 0;
        while (!gateway_startup_stop_requested()) {
            ++attempt;
            esp_err_t err = gateway_handshake(true);
            if (gateway_startup_stop_requested()) break;
            if (err == ESP_OK) {
                (void)start_gateway_ready_tasks();
                apply_deferred_startup_pet_asset();
                break;
            }
            if (err == ESP_ERR_INVALID_STATE) {
                // A 401/403 is not a transient outage: the stored credential
                // was revoked, disabled, or replaced. Keep it persisted for
                // diagnosis and expose recovery; do not confuse a connection
                // failure with permission to erase the device credential.
                ESP_LOGW(TAG, "gateway credential rejected; entering pairing recovery");
                pet("alert");
                app_ui_show_text("令牌认证失败", "请检查或重新配对");
                start_setup_portal(true);
                break;
            }
            // Keep the board-specific boot surface visible during retry. The actual
            // failure cause is logged with a heap/network snapshot for diagnosis.
            app_ui_show_startup_screen();
            ESP_LOGW(TAG, "gateway handshake attempt %u failed: %s; retry in %lu ms",
                     attempt, esp_err_to_name(err), (unsigned long)retry_ms);
            (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry_ms));
            if (retry_ms < GATEWAY_RETRY_MAX_MS) {
                retry_ms *= 2;
                if (retry_ms > GATEWAY_RETRY_MAX_MS) retry_ms = GATEWAY_RETRY_MAX_MS;
            }
        }
    }
finish:
    taskENTER_CRITICAL(&s_task_state_lock);
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    if (s_gateway_task == self) s_gateway_task = NULL;
    s_gateway_startup_running = false;
    s_gateway_startup_starting = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (s_gateway_startup_stopped) xSemaphoreGive(s_gateway_startup_stopped);
    (void)task_registry_unregister_with_timeout(TASK_REGISTRY_OWNER_CONNECTIVITY,
                                                 (void *)self, 10);
    vTaskDelete(NULL);
}

static esp_err_t stop_gateway_startup_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_startup_stop_requested = true;
    task = s_gateway_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    const uint32_t cancel_guard_ms = remaining_ms > 100 ? 100 : remaining_ms;
    if (s_gateway_startup_client_mutex && cancel_guard_ms != 0 &&
        xSemaphoreTake(s_gateway_startup_client_mutex, pdMS_TO_TICKS(cancel_guard_ms)) == pdTRUE) {
        esp_http_client_handle_t client = s_gateway_startup_active_client;
        if (client) {
            esp_err_t cancel_err = esp_http_client_cancel_request(client);
            if (cancel_err != ESP_OK) {
                ESP_LOGW(TAG, "gateway startup HTTP cancel returned: %s",
                         esp_err_to_name(cancel_err));
            }
        }
        xSemaphoreGive(s_gateway_startup_client_mutex);
    }
    xTaskNotifyGive(task);
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (!s_gateway_startup_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_gateway_startup_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return ESP_ERR_TIMEOUT;
    esp_err_t unregister_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)task, remaining_ms);
    if (unregister_err != ESP_OK) return unregister_err;
    ESP_LOGI(TAG, "gateway startup coordinator stopped");
    return ESP_OK;
}

static esp_err_t stop_gateway_startup_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    task = s_gateway_task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_gateway_startup_task(timeout_ms);
}

static bool start_gateway_startup_task(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_task_state_lock);
    if (s_gateway_startup_running || s_gateway_startup_starting) {
        taskEXIT_CRITICAL(&s_task_state_lock);
        return true;
    }
    s_gateway_startup_running = true;
    s_gateway_startup_starting = true;
    taskEXIT_CRITICAL(&s_task_state_lock);

    if (!s_gateway_startup_start_gate || !s_gateway_startup_stopped ||
        !s_gateway_startup_client_mutex) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_startup_running = false;
        s_gateway_startup_starting = false;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "gateway startup lifecycle primitives unavailable");
        return false;
    }
    while (xSemaphoreTake(s_gateway_startup_stopped, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_startup_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);

    /*
     * The gateway's TLS/HTTP work has a large but non-ISR stack.  Keeping it
     * in internal RAM competes with Wi-Fi, the round-screen DMA/display
     * adapter, and the ESP-SR/audio services.  On EchoEar this can make task
     * creation fail after a perfectly healthy local-board startup, leaving
     * the user on the misleading red "cannot start gateway" screen.
     *
     * PSRAM is safe for this task: it does not perform cache-disabled flash
     * mutations (those are isolated in dedicated internal-stack workers).
     * Reserve scarce internal memory for Wi-Fi/interrupt-facing work instead.
     */
    BaseType_t created = xTaskCreatePinnedToCoreWithCaps(gateway_startup_task,
                                                        "maclaw_gateway_startup",
                                                        12288, NULL, 4,
                                                        &task, 1,
                                                        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (created != pdPASS) {
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_startup_running = false;
        s_gateway_startup_starting = false;
        s_gateway_task = NULL;
        taskEXIT_CRITICAL(&s_task_state_lock);
        ESP_LOGE(TAG, "cannot start gateway startup task");
        return false;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_gateway_task = task;
    taskEXIT_CRITICAL(&s_task_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
        .name = "gateway_startup",
        .context = (void *)task,
        .stop = stop_gateway_startup_registry_entry,
    });
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register gateway startup coordinator: %s",
                 esp_err_to_name(registry_err));
        taskENTER_CRITICAL(&s_task_state_lock);
        s_gateway_startup_stop_requested = true;
        taskEXIT_CRITICAL(&s_task_state_lock);
        xSemaphoreGive(s_gateway_startup_start_gate);
        (void)stop_gateway_startup_task(500);
        return false;
    }
    xSemaphoreGive(s_gateway_startup_start_gate);
    return true;
}

static bool ensure_alarm_manager_started(void) {
    if (s_alarm_manager_started) return true;
    esp_err_t err = alarm_manager_init();
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "cannot start alarm scheduler: %s", esp_err_to_name(err));
        return false;
    }
    alarm_manager_set_ring_callback(on_alarm_ring_start, NULL);
    s_alarm_manager_started = true;
    return true;
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
        app_ui_show_text("Startup failed", "Local service unavailable; connect a computer for diagnostics");
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
    /* A saved-configuration coordinator can be between its response-flush
     * delay and the terminal portal transaction when startup rollback begins.
     * It is a Connectivity Registry entry, but the portal below is also
     * explicitly stopped before the generic Registry sweep.  Draining both in
     * parallel would let two callers issue httpd_stop()/DNS join against the
     * same generation.  Stop the coordinator first; its completion is defined
     * to cover its portal cleanup, so only then may this rollback issue an
     * idempotent portal transaction of its own.
     *
     * On timeout keep the generation fail-closed and do not continue tearing
     * down the coordinator's dependencies underneath it.  In particular, do
     * not turn a bounded-stop failure into a racing second portal teardown.
     */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("post-save restart coordinator");
    esp_err_t setup_restart_stop_err = stop_setup_restart_task(timeout_ms);
    if (setup_restart_stop_err != ESP_OK) {
        ESP_LOGW(TAG,
                 "post-save restart coordinator did not stop before rollback: %s; "
                 "leaving provisioning generation isolated",
                 esp_err_to_name(setup_restart_stop_err));
        startup_rollback_step_blocked("post-save restart coordinator", NULL);
        return;
    }

    /* A provisioning attempt can have started after the regular local-service
     * startup boundary (for example through force-setup or rejected pairing).
     * Drain its independently-owned HTTP/DNS resources before the generic
     * Connectivity Registry is stopped.  On a join error the helper remains
     * fail-closed and deliberately retains the logical session, scratch and
     * lease; the degraded boot must not resume gateway/command work beneath a
     * possibly live credentials handler. */
    if (device_connectivity_is_provisioning_active() || s_setup_server || s_dns_task) {
        STARTUP_ROLLBACK_NEXT_TIMEOUT("provisioning transaction");
        esp_err_t portal_stop_err = stop_setup_portal_transaction(timeout_ms, false);
        if (portal_stop_err != ESP_OK) {
            ESP_LOGW(TAG, "provisioning transaction did not stop during startup rollback: %s",
                     esp_err_to_name(portal_stop_err));
            startup_rollback_step_blocked("provisioning transaction", NULL);
            return;
        }
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
    esp_err_t startup_pet_stop_err = stop_startup_pet_asset_task(timeout_ms);
    if (startup_pet_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "startup pet asset task did not stop during startup rollback: %s",
                 esp_err_to_name(startup_pet_stop_err));
        startup_rollback_step_blocked("startup pet worker", NULL);
        return;
    }

    /* Cache work runs on an internal stack and borrows its frame from either
     * the startup worker or a runtime update. Drain it independently: joining
     * the startup worker alone is not proof that Flash/VFS no longer owns the
     * borrowed frame or the shared cache mutex. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("pet cache worker");
    esp_err_t pet_cache_stop_err = stop_pet_cache_task(timeout_ms);
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
    esp_err_t update_stop_err = update_service_deinit(timeout_ms);
    if (update_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "update service did not stop during startup rollback: %s",
                 esp_err_to_name(update_stop_err));
        startup_rollback_step_blocked("Update Service", NULL);
        return;
    }

    /* Weather cache and meeting-recovery metadata are synchronous Persistence
     * consumers. Close their admission after connectivity/meeting workers have
     * stopped, but before the shared NVS request boundary can be closed. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("weather cache");
    esp_err_t weather_cache_stop_err = weather_cache_service_deinit(timeout_ms);
    if (weather_cache_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "weather cache did not stop during startup rollback: %s",
                 esp_err_to_name(weather_cache_stop_err));
        startup_rollback_step_blocked("weather cache", NULL);
        return;
    }
    STARTUP_ROLLBACK_NEXT_TIMEOUT("meeting recovery metadata");
    esp_err_t meeting_recovery_stop_err = meeting_recovery_service_deinit(timeout_ms);
    if (meeting_recovery_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "meeting recovery metadata did not stop during startup rollback: %s",
                 esp_err_to_name(meeting_recovery_stop_err));
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

    /* Configuration has no worker of its own, but it owns three substantial
     * PSRAM schema scratch buffers and an admission boundary used by portal,
     * volume, connectivity and interaction callers. Those clients have all
     * joined above, so release Configuration before this degraded boot retains
     * only diagnostics. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Configuration Service");
    esp_err_t configuration_stop_err = configuration_service_deinit(timeout_ms);
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
    esp_err_t alarm_stop_err = alarm_manager_deinit(timeout_ms);
    if (alarm_stop_err == ESP_OK) {
        s_alarm_manager_started = false;
    } else {
        ESP_LOGW(TAG, "alarm manager did not stop during startup rollback: %s",
                 esp_err_to_name(alarm_stop_err));
        startup_rollback_step_blocked("alarm manager", NULL);
        return;
    }
    STARTUP_ROLLBACK_NEXT_TIMEOUT("sleep schedule");
    esp_err_t schedule_stop_err = sleep_schedule_service_deinit(timeout_ms);
    if (schedule_stop_err != ESP_OK) {
        ESP_LOGW(TAG, "sleep schedule did not stop during startup rollback: %s",
                 esp_err_to_name(schedule_stop_err));
        startup_rollback_step_blocked("sleep schedule", NULL);
        return;
    } else if (alarm_stop_err == ESP_OK) {
        STARTUP_ROLLBACK_NEXT_TIMEOUT("wake deadline dispatcher");
        esp_err_t deadline_stop_err = wake_deadline_service_deinit(timeout_ms);
        if (deadline_stop_err != ESP_OK) {
            ESP_LOGW(TAG, "wake deadline dispatcher did not stop during startup rollback: %s",
                     esp_err_to_name(deadline_stop_err));
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
     * generation and reclaim its queue/semaphores. It intentionally does not
     * destroy main.c's shared NVS transaction mutex. */
    STARTUP_ROLLBACK_NEXT_TIMEOUT("Persistence Service");
    esp_err_t persistence_deinit_err = persistence_service_deinit(timeout_ms);
    if (persistence_deinit_err != ESP_OK) {
        ESP_LOGW(TAG, "persistence service did not fully deinitialize during startup rollback: %s",
                 esp_err_to_name(persistence_deinit_err));
        startup_rollback_step_blocked("Persistence Service", NULL);
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

void app_main(void) {
    ESP_LOGW(TAG, "boot reset reason=%d", (int)esp_reset_reason());
    lifecycle_service_begin();
    if (task_registry_init() != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
                               DEVICE_STATUS_RESOURCE_EXHAUSTED, "task registry");
        return;
    }
    operation_context_service_init();
    if (!validate_compiled_board_profile()) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED,
                               DEVICE_STATUS_INTERNAL_ERROR, "board profile validation");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_PROFILE_VALIDATED);
    esp_err_t identity_err = firmware_identity_start();
    if (identity_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_IDENTITY_READY,
                               startup_status_from_esp_err(identity_err), "identity service");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_IDENTITY_READY);
    esp_err_t nvs_err = initialize_nvs_preserving_user_data();
    if (nvs_err != ESP_OK) {
        // firmware_identity_start() intentionally remains available so a
        // service tool can inspect the board/profile and perform an explicit
        // recovery. Do not start Wi-Fi, audio or writers against an NVS
        // partition whose contents we deliberately chose not to destroy.
        ESP_LOGE(TAG, "startup stopped before user-data writes: %s", esp_err_to_name(nvs_err));
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_STORAGE_READY,
                               startup_status_from_esp_err(nvs_err), "NVS initialization");
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
    (void)mount_meeting_storage();
    device_status_t pressure_status = resource_pressure_service_init("storage", s_storage_mounted);
    if (pressure_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               pressure_status, "resource pressure service");
        return;
    }
    s_http_mutex = xSemaphoreCreateMutex();
    if (!s_http_mutex) goto startup_core_no_memory;
    s_gateway_poll_http_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_poll_http_mutex) goto startup_core_no_memory;
    s_gateway_poll_client_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_poll_client_mutex) goto startup_core_no_memory;
    s_gateway_asset_http_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_asset_http_mutex) goto startup_core_no_memory;
    s_gateway_asset_client_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_asset_client_mutex) goto startup_core_no_memory;
    s_media_transfer_mutex = xSemaphoreCreateMutex();
    if (!s_media_transfer_mutex) goto startup_core_no_memory;
    s_pet_asset_apply_mutex = xSemaphoreCreateMutex();
    if (!s_pet_asset_apply_mutex) goto startup_core_no_memory;
    s_pet_cache_flash_mutex = xSemaphoreCreateMutex();
    if (!s_pet_cache_flash_mutex) goto startup_core_no_memory;
    s_foreground_http_client_mutex = xSemaphoreCreateMutex();
    if (!s_foreground_http_client_mutex) goto startup_core_no_memory;
    s_command_cancel_ui_ready = xSemaphoreCreateBinary();
    if (!s_command_cancel_ui_ready) goto startup_core_no_memory;
    s_command_cancel_stopped = xSemaphoreCreateBinary();
    if (!s_command_cancel_stopped) goto startup_core_no_memory;
    s_clock_sync_start_gate = xSemaphoreCreateBinary();
    if (!s_clock_sync_start_gate) goto startup_core_no_memory;
    s_clock_sync_stopped = xSemaphoreCreateBinary();
    if (!s_clock_sync_stopped) goto startup_core_no_memory;
    s_interaction_start_gate = xSemaphoreCreateBinary();
    if (!s_interaction_start_gate) goto startup_core_no_memory;
    s_interaction_stopped = xSemaphoreCreateBinary();
    if (!s_interaction_stopped) goto startup_core_no_memory;
    s_meeting_task_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_task_start_gate) goto startup_core_no_memory;
    s_meeting_task_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_task_stopped) goto startup_core_no_memory;
    s_meeting_task_client_mutex = xSemaphoreCreateMutex();
    if (!s_meeting_task_client_mutex) goto startup_core_no_memory;
    s_meeting_resume_supervisor_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_resume_supervisor_start_gate) goto startup_core_no_memory;
    s_meeting_resume_supervisor_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_resume_supervisor_stopped) goto startup_core_no_memory;
    s_meeting_capability_refresh_start_gate = xSemaphoreCreateBinary();
    if (!s_meeting_capability_refresh_start_gate) goto startup_core_no_memory;
    s_meeting_capability_refresh_stopped = xSemaphoreCreateBinary();
    if (!s_meeting_capability_refresh_stopped) goto startup_core_no_memory;
    s_meeting_capability_refresh_client_mutex = xSemaphoreCreateMutex();
    if (!s_meeting_capability_refresh_client_mutex) goto startup_core_no_memory;
    s_gateway_startup_start_gate = xSemaphoreCreateBinary();
    if (!s_gateway_startup_start_gate) goto startup_core_no_memory;
    s_gateway_startup_stopped = xSemaphoreCreateBinary();
    if (!s_gateway_startup_stopped) goto startup_core_no_memory;
    s_gateway_startup_client_mutex = xSemaphoreCreateMutex();
    if (!s_gateway_startup_client_mutex) goto startup_core_no_memory;
    s_wake_restart_start_gate = xSemaphoreCreateBinary();
    if (!s_wake_restart_start_gate) goto startup_core_no_memory;
    s_wake_restart_stopped = xSemaphoreCreateBinary();
    if (!s_wake_restart_stopped) goto startup_core_no_memory;
    s_setup_restart_start_gate = xSemaphoreCreateBinary();
    if (!s_setup_restart_start_gate) goto startup_core_no_memory;
    s_setup_restart_stopped = xSemaphoreCreateBinary();
    if (!s_setup_restart_stopped) goto startup_core_no_memory;
    s_deferred_setup_start_gate = xSemaphoreCreateBinary();
    if (!s_deferred_setup_start_gate) goto startup_core_no_memory;
    s_deferred_setup_stopped = xSemaphoreCreateBinary();
    if (!s_deferred_setup_stopped) goto startup_core_no_memory;
    s_dns_start_gate = xSemaphoreCreateBinary();
    if (!s_dns_start_gate) goto startup_core_no_memory;
    s_dns_stopped = xSemaphoreCreateBinary();
    if (!s_dns_stopped) goto startup_core_no_memory;
    s_dns_ready = xSemaphoreCreateBinary();
    if (!s_dns_ready) goto startup_core_no_memory;
    s_setup_portal_mutex = xSemaphoreCreateMutex();
    if (!s_setup_portal_mutex) goto startup_core_no_memory;
    s_wifi_event_callbacks_drained = xSemaphoreCreateBinary();
    if (!s_wifi_event_callbacks_drained) goto startup_core_no_memory;
    s_startup_pet_retry_callback_drained = xSemaphoreCreateBinary();
    if (!s_startup_pet_retry_callback_drained) goto startup_core_no_memory;
    s_startup_pet_retry_timer_mutex = xSemaphoreCreateMutex();
    if (!s_startup_pet_retry_timer_mutex) goto startup_core_no_memory;
    s_cellular_recovery_start_gate = xSemaphoreCreateBinary();
    if (!s_cellular_recovery_start_gate) goto startup_core_no_memory;
    s_cellular_recovery_stopped = xSemaphoreCreateBinary();
    if (!s_cellular_recovery_stopped) goto startup_core_no_memory;
    taskENTER_CRITICAL(&s_task_state_lock);
    s_wake_restart_admission_open = true;
    s_setup_restart_admission_open = true;
    s_dns_admission_open = true;
    s_cellular_recovery_admission_open = true;
    taskEXIT_CRITICAL(&s_task_state_lock);
    s_startup_welcome_done = xSemaphoreCreateBinary();
    if (!s_startup_welcome_done) goto startup_core_no_memory;
    s_nvs_mutex = xSemaphoreCreateMutex();
    if (!s_nvs_mutex) goto startup_core_no_memory;
    esp_err_t persistence_init_err = persistence_service_init(s_nvs_mutex);
    if (persistence_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(persistence_init_err),
                               "persistence service");
        return;
    }
    esp_err_t weather_cache_init_err = weather_cache_service_init();
    if (weather_cache_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(weather_cache_init_err),
                               "weather cache service");
        return;
    }
    esp_err_t meeting_recovery_init_err = meeting_recovery_service_init();
    if (meeting_recovery_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(meeting_recovery_init_err),
                               "meeting recovery service");
        return;
    }
    esp_err_t configuration_init_err = configuration_service_init();
    if (configuration_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(configuration_init_err),
                               "configuration service");
        return;
    }
    load_meeting_recovery();
    esp_err_t update_init_err = update_service_init(&(update_service_config_t){
        .running_release_sequence = CONFIG_MACLAW_RELEASE_SEQUENCE,
    });
    if (update_init_err != ESP_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(update_init_err), "update metadata service");
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
    s_setup_options_mutex = xSemaphoreCreateMutex();
    if (!s_setup_options_mutex) goto startup_core_no_memory;
    // A foreground interaction starts in the button callback but finishes in
    // its worker task, therefore mutual exclusion must use a binary semaphore
    // rather than an ownership-tracked mutex.
    s_interaction_lock = xSemaphoreCreateBinary();
    if (!s_interaction_lock || xSemaphoreGive(s_interaction_lock) != pdTRUE) goto startup_core_no_memory;
    // The cancel worker draws a full CJK frame, cancels the foreground TLS or
    // ML307 request and then posts "/cancel" through a complete HTTPS/AT
    // round trip. 4096 bytes overflowed on Fangtang (crash/reboot on cancel);
    // 8192 matches the other network-facing workers. Plain xTaskCreate keeps
    // the stack in internal RAM, so the Wi-Fi path stays cache-safe.
    if (xTaskCreate(command_cancel_worker, "maclaw_cancel", 8192, NULL, 6,
                    &s_command_cancel_task) != pdPASS) goto startup_core_no_memory;
    esp_err_t cancel_registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_INTERACTION,
        .name = "command_cancel",
        .context = (void *)s_command_cancel_task,
        .stop = stop_command_cancel_registry_entry,
    });
    if (cancel_registry_err != ESP_OK) {
        (void)stop_command_cancel_worker(500);
        goto startup_core_no_memory;
    }
    taskENTER_CRITICAL(&s_task_state_lock);
    s_output_volume_persist_stop_requested = false;
    taskEXIT_CRITICAL(&s_task_state_lock);
    if (xTaskCreate(output_volume_persist_task,
                    "maclaw_volume_nvs", 4096, NULL, 4,
                    &s_output_volume_persist_task_handle) != pdPASS) goto startup_core_no_memory;
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
    device_connectivity_restore_selected_uplink();
    load_gateway_token();
    load_ambient_weather();
    /* Establish the one synchronous display-submission owner before App UI
     * can restore brightness or publish its startup/ambient scene. The board
     * renderer remains boot-lifetime; this only owns service-side ordering. */
    if (!display_service_init()) goto startup_core_no_memory;
    app_ui_init();
    s_startup_ui_initialized = true;
    device_status_t input_status = app_intent_service_start(on_app_intent, NULL);
    if (input_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               input_status, "input service");
        return;
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
    esp_err_t deadline_init_err = wake_deadline_service_init();
    if (deadline_init_err != ESP_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(deadline_init_err),
                               "wake deadline service");
        return;
    }
    esp_err_t schedule_init_err = sleep_schedule_service_init();
    if (schedule_init_err != ESP_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               startup_status_from_esp_err(schedule_init_err),
                               "sleep schedule service");
        return;
    }
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY);
    if (s_configured_output_volume_saved) {
        device_status_t volume_status =
            device_audio_set_output_volume((uint8_t)s_configured_output_volume);
        if (volume_status == DEVICE_STATUS_OK) {
            ESP_LOGI(TAG, "restored output volume: %u%%", s_configured_output_volume);
        } else {
            ESP_LOGW(TAG, "cannot restore output volume: device status=%d", volume_status);
        }
    }

    // 亮度是独立 scalar key（不走 versioned 配置 blob），找到才恢复；
    // 未找到时保持各板默认亮度（waveshare/echoear/fangtang 100，bread 50）。
    uint8_t restored_brightness = 0;
    esp_err_t brightness_err = persistence_service_read_u8(
        DISPLAY_BRIGHTNESS_NVS_NAMESPACE, DISPLAY_BRIGHTNESS_NVS_KEY,
        &restored_brightness);
    if (brightness_err == ESP_OK && restored_brightness <= 100) {
        device_status_t brightness_status =
            device_display_set_brightness(restored_brightness);
        if (brightness_status == DEVICE_STATUS_OK) {
            ESP_LOGI(TAG, "restored display brightness: %u%%",
                     (unsigned)restored_brightness);
        } else {
            ESP_LOGW(TAG, "cannot restore display brightness: device status=%d",
                     brightness_status);
        }
    }

    uint32_t restored_screen_sleep_seconds = 0;
    size_t screen_sleep_size = sizeof(restored_screen_sleep_seconds);
    esp_err_t screen_sleep_err = persistence_service_read_blob(
        DISPLAY_BRIGHTNESS_NVS_NAMESPACE, DISPLAY_SLEEP_NVS_KEY,
        &restored_screen_sleep_seconds, &screen_sleep_size);
	if (screen_sleep_err == ESP_OK && screen_sleep_size == sizeof(restored_screen_sleep_seconds) &&
		valid_screen_sleep_seconds((int)restored_screen_sleep_seconds)) {
        app_ui_set_display_off_idle_ms(restored_screen_sleep_seconds * 1000u);
        ESP_LOGI(TAG, "restored screen sleep timeout: %lu seconds",
                 (unsigned long)restored_screen_sleep_seconds);
    }

    // A profile may provide a bounded boot selector (for example a physical
    // double-click) and an uplink-specific Gateway compatibility adaptation.
    // The business startup path sees only the selected normalized transport.
    if (device_connectivity_apply_startup_transport_toggle(
            STARTUP_TRANSPORT_SELECTOR_WINDOW_MS)) {
        ESP_LOGI(TAG, "startup transport toggle selected: %s",
                 device_connectivity_is_active_cellular() ? "cellular" : "Wi-Fi");
    }
    device_connectivity_adapt_gateway_url(s_gateway_url, sizeof(s_gateway_url));
	// board initialization may briefly show and then clear its ROM/embedded
	// artwork. Re-present it as an explicit foreground UI surface so ambient
	// clock/profile updates cannot replace it while Welcome is being fetched and
	// played. The ready transition releases this surface after wake-word setup.
	app_ui_show_startup_screen();
    // Keep optional background work quiescent until esp_wifi_start() has
    // completed.  Both cached-pet installation (which may create its animation
    // task) and the alarm manager create work that can run while the Wi-Fi ROM
    // is enabling TSF.  On Bread Compact that startup overlap can corrupt the
    // Wi-Fi task's first callback and jump to PC 0x1 (InstrFetchProhibited).
    // The LCD mutex already exists here; only the timing is intentionally
    // deferred.
    firmware_identity_set_local_ready(true);
    lifecycle_service_reach(DEVICE_RUNTIME_PHASE_LOCAL_READY);
    bool force_setup = false;
    esp_err_t force_setup_err = configuration_service_take_force_setup(&force_setup);
    if (force_setup_err != ESP_OK) {
        /* This is the same authoritative configuration snapshot that carries
         * network credentials and the Hub token.  Continuing into radio or
         * gateway startup after its one-shot flag cannot be read/committed
         * would violate the fail-closed configuration boundary. */
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                               startup_status_from_esp_err(force_setup_err),
                               "configuration force-setup request");
        return;
    }
    if (force_setup || provisioning_failure_injection_force_portal_at_boot()) {
        ESP_LOGW(TAG, "booting directly into %s configuration portal%s",
                 force_setup ? "requested" : "test-forced",
                 provisioning_failure_injection_force_portal_at_boot()
                     ? " (test injection)" : "");
        start_setup_portal(false);
        return;
    }
    // Keep the explicit board-specific startup surface until the Welcome/wake-word
    // sequence publishes ready. Do not transition to standby here.
    if (!s_wifi_ssid[0] && !device_connectivity_is_active_cellular()) {
        start_setup_portal(false);
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
    if (!device_connectivity_is_active_cellular()) start_clock_sync();
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
        pet("alert");
        ESP_LOGW(TAG, "saved Wi-Fi is currently unavailable; preserving configuration and retrying in station mode");
        app_ui_show_text("网络暂时不可用", "配置已保留，正在自动重连");
        return;
    }
    // Do not allocate the ESP-SR model while the first TLS pairing/handshake
    // is being established. Both are PSRAM-heavy; starting them concurrently
    // can make mbedtls_ssl_setup() fail with PSA_ERROR_INSUFFICIENT_MEMORY
    // (-0x008D). start_gateway_ready_tasks() starts the listener immediately
    // after the authenticated handshake has released its TLS allocations.
    // Run TLS/HTTP work on core 1. Performing it in the framework main task on
    // core 0 starves that core's interrupt watchdog during TLS initialization.
    if (!start_gateway_startup_task()) {
        pet("alert");
        app_ui_show_text("设备启动失败", "无法启动网关任务");
    }
    return;

startup_core_no_memory:
    startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                           DEVICE_STATUS_RESOURCE_EXHAUSTED, "core startup allocation");
}
