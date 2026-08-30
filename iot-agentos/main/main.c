#include <stdio.h>
#include <dirent.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include "cJSON.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "mbedtls/base64.h"
#include "mbedtls/platform_util.h"
#include "services/entropy_service.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "lwip/inet.h"
#include "lwip/sockets.h"
#include "psa/crypto.h"

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
#include "services/credential_service.h"
#include "power_service.h"
#include "provisioning_failure_injection.h"
#include "task_registry.h"
#include "storage_service.h"
#include "pet_asset_cache_storage.h"
#include "weather_cache_service.h"
#include "meeting_recovery_service.h"
#include "meeting_recording_storage.h"
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
#include "services/gateway_tool_result_outbox_policy.h"
#include "services/cellular_recovery_service.h"
#include "services/wifi_runtime_configuration_service.h"
#include "services/wifi_startup_service.h"
#include "services/configuration_persistence_worker_service.h"
#include "services/deferred_setup_worker_service.h"
#include "services/wake_restart_worker_service.h"
#include "services/clock_sync_service.h"
#include "services/ambient_service.h"
#include "services/pet_asset_service.h"
#include "services/pet_asset_download_service.h"
#include "services/pet_asset_integrity_service.h"
#include "services/pet_asset_apply_service.h"
#include "services/pet_asset_runtime_service.h"
#include "services/pet_asset_startup_service.h"
#include "services/pet_asset_restore_service.h"
#include "services/pet_asset_restore_worker_service.h"
#include "services/pet_asset_retry_service.h"
#include "services/pet_asset_profile_service.h"
#include "services/media_transfer_service.h"
#include "services/server_audio_presentation_service.h"
#include "services/startup_welcome_service.h"
#include "services/startup_runtime_state_service.h"
#include "services/startup_pet_asset_admission_service.h"
#include "services/startup_pet_asset_sleep_service.h"
#include "services/startup_pet_asset_state_service.h"
#include "services/pet_cache_service.h"
#include "services/startup_pet_retry_service.h"
#include "services/startup_pet_worker_service.h"
#include "services/factory_reset_service.h"

/* Factory-reset composition callbacks are defined before the bulk of the
 * service includes below; keep this narrow forward declaration local to the
 * composition root. */
bool provisioning_service_has_live_resources(void);
static device_status_t startup_status_from_esp_err(esp_err_t err);
static const char *json_string(cJSON *root, const char *key);

typedef struct {
    const char *name_space;
    const char *key;
} factory_reset_fixed_key_t;

/* One authoritative fixed erase inventory is shared by the destructive pass
 * and its post-erase verifier.  Keeping these two passes on the same table
 * prevents a newly added personal-data key from being erased without also
 * being checked (or vice versa).  The recovery verifier below intentionally
 * uses a narrower table because the durable tool-result outbox is handoff
 * evidence, not an erased class. */
static const factory_reset_fixed_key_t s_factory_reset_fixed_keys[] = {
    {"maclaw", "configuration"},
    {"maclaw", "configuration_migration_journal"},
    {"maclaw", "configuration_migration_source_fingerprint"},
    {"maclaw", "configuration_migration_target_fingerprint"},
    {"maclaw", "weather_cache"},
    {"maclaw", "weather"}, {"maclaw", "weather_loc"},
    {"maclaw", "weather_exp"}, {"maclaw", "weather_temp"},
    {"maclaw", "update_meta"},
    {"maclaw", "upd_seq"}, {"maclaw", "upd_after"},
    {"maclaw", "upd_dseq"}, {"maclaw", "upd_duntil"},
    {"maclaw", "upd_digest"}, {"maclaw", "upd_ddigest"},
    {"maclaw", "meeting_recovery"},
    {"maclaw", "wifi_ssid"}, {"maclaw", "wifi_pass"},
    {"maclaw", "wifi_sec"}, {"maclaw", "wifi_eap"},
    {"maclaw", "wifi_ident"}, {"maclaw", "wifi_user"},
    {"maclaw", "wifi_ttls"}, {"maclaw", "wifi_ca"},
    {"maclaw", "wifi_domain"}, {"maclaw", "gateway_url"},
    {"maclaw", "pair_code"}, {"maclaw", "gateway_token"},
    {"maclaw", "output_vol"},
    {"maclaw", "net_transport"}, {"maclaw", "brightness"},
    {"maclaw", "screen_sleep_s"},
    {"alarms", "store"}, {"sleep_sched", "store"},
    {"fall_detect", "config"},
    {"gateway", "ack_outbox"}, {"gateway", "tool_result_outbox"},
};

#define FACTORY_RESET_FIXED_KEY_COUNT \
    (sizeof(s_factory_reset_fixed_keys) / sizeof(s_factory_reset_fixed_keys[0]))

static bool factory_reset_is_handoff_key(const factory_reset_fixed_key_t *entry) {
    return entry && strcmp(entry->name_space, "gateway") == 0 &&
           strcmp(entry->key, "tool_result_outbox") == 0;
}

static device_status_t factory_reset_erase_classes(uint32_t classes, void *context) {
    (void)context;
    if (classes != CONFIGURATION_FACTORY_RESET_CLASS_ALL) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* Closed, audited erase list.  No caller-supplied namespace/key reaches
     * Persistence; adding a new personal-data record requires editing this
     * composition-root inventory and its verification below. */
    for (size_t i = 0; i < FACTORY_RESET_FIXED_KEY_COUNT; ++i) {
        device_status_t status = persistence_service_erase_key(
            s_factory_reset_fixed_keys[i].name_space,
            s_factory_reset_fixed_keys[i].key);
        if (status != DEVICE_STATUS_OK && status != DEVICE_STATUS_NOT_FOUND) {
            return status;
        }
    }
    return DEVICE_STATUS_OK;
}

static device_status_t factory_reset_verify_classes_absent(uint32_t classes,
                                                            void *context) {
    (void)context;
    if (classes != CONFIGURATION_FACTORY_RESET_CLASS_ALL) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    for (size_t i = 0; i < FACTORY_RESET_FIXED_KEY_COUNT; ++i) {
        size_t size = 0;
        device_status_t status = persistence_service_read_blob(
            s_factory_reset_fixed_keys[i].name_space,
            s_factory_reset_fixed_keys[i].key, NULL, &size);
        if (status != DEVICE_STATUS_NOT_FOUND) return DEVICE_STATUS_IO_ERROR;
    }
    return DEVICE_STATUS_OK;
}

static device_status_t factory_reset_clear_meeting_recording(void *context) {
    (void)context;
    return meeting_recording_storage_clear();
}

static device_status_t factory_reset_clear_pet_cache(void *context) {
    (void)context;
    /* factory_reset_prepare() has already retired the Pet cache worker and
     * closed its normal admission.  Calling the ordinary service operation
     * here would therefore (correctly) reject the request.  At this point the
     * composition root owns the bounded, single-threaded reset transaction,
     * so invoke the cache adapter's fixed-object clear directly; the service
     * verifies the descriptor is absent before COMMITTED is published. */
    pet_asset_cache_storage_clear();
    return DEVICE_STATUS_OK;
}

static device_status_t factory_reset_verify_personal_storage_absent(void *context) {
    (void)context;
    if (meeting_recording_storage_has_pending_audio()) return DEVICE_STATUS_IO_ERROR;
    pet_asset_descriptor_t descriptor = {0};
    if (pet_asset_cache_storage_read_descriptor(&descriptor)) return DEVICE_STATUS_IO_ERROR;
    return DEVICE_STATUS_OK;
}

static device_status_t factory_reset_verify_recovery_state(uint32_t classes,
                                                           void *context) {
    /* Recovery must prove the same fixed erase inventory is absent before it
     * can trust a COMMITTED journal.  The setup marker and tool-result outbox
     * are intentionally not part of this inventory: they are the durable
     * handoff evidence required to finish the transaction after power loss. */
    if (classes != CONFIGURATION_FACTORY_RESET_CLASS_ALL) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* The reset result itself is intentionally allowed to survive in the
     * durable outbox.  Verify every other fixed erase key, then require that
     * the queue is well-formed and that its head is the factory_reset result;
     * an unrelated/empty outbox must never unlock recovery. */
    for (size_t i = 0; i < FACTORY_RESET_FIXED_KEY_COUNT; ++i) {
        /* The tool-result outbox is the only fixed key intentionally retained
         * as handoff evidence. It is validated below and must contain the
         * factory_reset result at its queue head. */
        if (factory_reset_is_handoff_key(&s_factory_reset_fixed_keys[i])) continue;
        size_t size = 0;
        device_status_t status = persistence_service_read_blob(
            s_factory_reset_fixed_keys[i].name_space,
            s_factory_reset_fixed_keys[i].key, NULL, &size);
        if (status != DEVICE_STATUS_NOT_FOUND) return DEVICE_STATUS_IO_ERROR;
    }
    char *queue = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                   MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    char *record = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                    MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    char *upgraded = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                      MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!queue || !record || !upgraded) {
        free(upgraded); free(record); free(queue);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    size_t queue_size = GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY;
    device_status_t status = persistence_service_read_blob(
        "gateway", "tool_result_outbox", queue, &queue_size);
    /* Devices upgraded from the original length-only queue format may reboot
     * while a COMMITTED reset journal is pending.  Normalize that queue
     * before peeking at the factory-reset result, but only after the complete
     * conversion has succeeded and has been durably persisted.  Current-format
     * corruption, magic collisions, malformed records, or persistence failure
     * remain fail-closed and therefore cannot unlock recovery. */
    char *active_queue = queue;
    size_t active_queue_size = queue_size;
    if (status == DEVICE_STATUS_OK &&
        gateway_tool_result_outbox_validate_queue(queue, queue_size,
                                                  GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) != DEVICE_STATUS_OK) {
        size_t upgraded_size = 0;
        if (gateway_tool_result_outbox_upgrade_legacy(
                queue, queue_size, upgraded,
                GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY, &upgraded_size) != DEVICE_STATUS_OK ||
            persistence_service_write_blob("gateway", "tool_result_outbox",
                                           upgraded, upgraded_size) != DEVICE_STATUS_OK) {
            free(upgraded); free(record); free(queue);
            return DEVICE_STATUS_IO_ERROR;
        }
        active_queue = upgraded;
        active_queue_size = upgraded_size;
    }
    size_t record_size = 0;
    bool valid_factory_result = status == DEVICE_STATUS_OK &&
        gateway_tool_result_outbox_peek(active_queue, active_queue_size, record,
                                        GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                        &record_size) == DEVICE_STATUS_OK;
    cJSON *json = valid_factory_result ? cJSON_Parse(record) : NULL;
    const char *tool_name = json ? json_string(json, "toolName") : NULL;
    valid_factory_result = valid_factory_result && tool_name &&
                           strcmp(tool_name, "factory_reset") == 0;
    cJSON_Delete(json);
    free(upgraded); free(record); free(queue);
    return valid_factory_result ? DEVICE_STATUS_OK : DEVICE_STATUS_IO_ERROR;
}

static bool factory_reset_validate_authorization(configuration_source_t source,
                                                 uint64_t generation,
                                                 void *context) {
    (void)context;
    if (source == CONFIGURATION_SOURCE_USER_LOCAL) {
        /* No local UI confirmation fact is wired yet; fail closed until the
         * physical confirmation flow supplies one. */
        return false;
    }
    if (source != CONFIGURATION_SOURCE_HUB_AUTHENTICATED ||
        generation > UINT32_MAX) return false;
    gateway_capability_projection_t projection = {0};
    if (!gateway_transport_get_capability_projection(&projection) ||
        !projection.acceptance_observed || projection.generation != (uint32_t)generation ||
        projection.operational_capabilities == 0u) return false;
    return true;
}

static device_status_t factory_reset_prepare(uint32_t timeout_ms, void *context) {
    (void)context;
    if (timeout_ms == 0u) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* Destructive reset never races a user-visible meeting, alarm ring, or
     * credential-bearing setup portal. The caller may retry after these
     * activities have naturally quiesced. */
    if (meeting_service_is_active() || alarm_manager_is_ringing() ||
        provisioning_service_has_live_resources()) return DEVICE_STATUS_BUSY;
    const int64_t deadline_us = esp_timer_get_time() +
                                (int64_t)timeout_ms * 1000;
#define FACTORY_RESET_REMAINING_MS() \
    ((esp_timer_get_time() >= deadline_us) ? 0u : \
     (uint32_t)(((deadline_us - esp_timer_get_time()) + 999) / 1000))
#define FACTORY_RESET_REQUIRE(_expr) do { \
    uint32_t remaining_ms = FACTORY_RESET_REMAINING_MS(); \
    if (remaining_ms == 0u) return DEVICE_STATUS_TIMEOUT; \
    device_status_t _status = (_expr); \
    if (_status != DEVICE_STATUS_OK) return _status; \
} while (0)
    /* Cancel the transport request first, while the Gateway dispatcher and
     * its result path remain alive for the post-reset tool envelope. */
    FACTORY_RESET_REQUIRE(gateway_transport_cancel_active_requests(
        GATEWAY_TRANSPORT_CANCEL_ALL, remaining_ms));
    /* Drain the consumer-side reconcile owner before closing Configuration's
     * value mutation gate. Its retained expiry/retry worker can otherwise
     * apply a snapshot concurrently with the destructive erase inventory. */
    FACTORY_RESET_REQUIRE(configuration_reconcile_service_prepare_system_sleep(
        remaining_ms));
    /* Close the Configuration value owner's mutation gate before draining
     * workers.  Otherwise a portal/reconcile callback can publish a new V7
     * record while the reset PREPARE is still quiescing its consumers. */
    FACTORY_RESET_REQUIRE(configuration_service_prepare_system_sleep(
        remaining_ms));
    /* Interaction and meeting workers own foreground leases/streams. Their
     * registry stop callbacks perform cooperative cancellation and bounded
     * join; a timeout leaves the registry entry in place and aborts reset. */
    FACTORY_RESET_REQUIRE(startup_status_from_esp_err(task_registry_stop_owner(
        TASK_REGISTRY_OWNER_INTERACTION, remaining_ms)));
    FACTORY_RESET_REQUIRE(startup_status_from_esp_err(task_registry_stop_owner(
        TASK_REGISTRY_OWNER_AUDIO, remaining_ms)));
    /* Configuration persistence remains available for the reset journal, but
     * its admission is fenced and its already-queued work is quiesced. */
    FACTORY_RESET_REQUIRE(configuration_persistence_worker_service_prepare_system_sleep(
        remaining_ms));
    FACTORY_RESET_REQUIRE(pet_cache_service_stop(remaining_ms));
    FACTORY_RESET_REQUIRE(startup_pet_worker_service_stop(remaining_ms));
    FACTORY_RESET_REQUIRE(startup_pet_retry_service_stop(remaining_ms));
    FACTORY_RESET_REQUIRE(wake_restart_worker_service_stop(remaining_ms));
    FACTORY_RESET_REQUIRE(deferred_setup_worker_service_stop(remaining_ms));
    /* Re-check activity after all cooperative joins. This closes the small
     * observation-to-stop window before destructive storage writes begin. */
    if (meeting_service_is_active() || interaction_service_worker_active() ||
        startup_pet_worker_service_active() || provisioning_service_has_live_resources() ||
        media_transfer_service_audio_download_active() ||
        media_transfer_service_server_audio_wake_lease_active()) {
        return DEVICE_STATUS_BUSY;
    }
    return DEVICE_STATUS_OK;
#undef FACTORY_RESET_REQUIRE
#undef FACTORY_RESET_REMAINING_MS
}

static void factory_reset_abort_prepare(void *context) {
    (void)context;
    /* Factory-reset PREPARE uses the reversible System Sleep fences for the
     * persistence worker. Terminal workers intentionally stay stopped. */
    configuration_persistence_worker_service_abort_system_sleep_prepare();
    configuration_service_abort_system_sleep_prepare();
    configuration_reconcile_service_abort_system_sleep_prepare();
}

static device_status_t factory_reset_complete(void *context) {
    (void)context;
    /* The reset transaction has intentionally erased the configuration blob
     * and all legacy scalar keys.  Calling the normal configuration mutation
     * would therefore fail with NOT_FOUND before it could create the setup
     * marker.  Publish the one fixed, durable handoff key directly; the next
     * boot's authoritative configuration loader imports it into a fresh
     * default snapshot and consumes it atomically. */
    return persistence_service_write_u8("maclaw", "force_setup", 1u);
}

static void factory_reset_reboot(void *context) {
    (void)context;
    /* The final factory-reset result has already reached the Hub or durable
     * outbox before this callback. Retire the in-RAM bearer now so a delayed
     * worker cannot issue an authenticated request during the reboot window. */
    gateway_transport_revoke_credentials();
    esp_restart();
}

static const factory_reset_service_host_t s_factory_reset_service_host = {
    .struct_size = sizeof(factory_reset_service_host_t),
    .abi_version = FACTORY_RESET_SERVICE_HOST_ABI_VERSION,
    .erase_classes = factory_reset_erase_classes,
    .verify_classes_absent = factory_reset_verify_classes_absent,
    .clear_meeting_recording = factory_reset_clear_meeting_recording,
    .clear_pet_cache = factory_reset_clear_pet_cache,
    .verify_personal_storage_absent = factory_reset_verify_personal_storage_absent,
    .verify_recovery_state = factory_reset_verify_recovery_state,
    .validate_authorization = factory_reset_validate_authorization,
    .prepare_for_reset = factory_reset_prepare,
    .abort_prepare_for_reset = factory_reset_abort_prepare,
    .complete_reset = factory_reset_complete,
    .reboot_after_reset = factory_reset_reboot,
    .context = NULL,
};
#include "services/audio_arbitration_service.h"
#include "services/provisioning_service.h"
#include "services/provisioning_qr_service.h"
#include "services/provisioning_network_owner.h"
#include "services/connectivity_network_root_owner.h"
#include "services/connectivity_network_lifecycle_service.h"
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
#define SETUP_AP_IP_ADDR "192.168.4.1"
#define DNS_PORT 53
#define DNS_PACKET_CAPACITY 512
#define DHCPS_OFFER_DNS 0x02
#define SETUP_SCAN_MAX_APS 24
#define SETUP_SSID_OPTIONS_CAPACITY 6144
#define SETUP_SSID_CHOICES_CAPACITY (SETUP_SCAN_MAX_APS * WIFI_VALUE_CAPACITY)
#define PET_ASSET_MAX_FRAMES PET_ASSET_SERVICE_MAX_FRAMES
#define CONFIGURATION_RECONCILE_AUTHORITY_GATEWAY_CAPABILITY 1u

_Static_assert(URL_CAPACITY == PET_ASSET_SERVICE_URL_CAPACITY,
               "legacy gateway URL capacity must match the pet asset service contract");

static const char *TAG = "maclaw_client";
// Bread's first TLS certificate verification is cache/PSRAM intensive. Its
// alarm scheduler is deliberately initialized after that transaction, not in
// parallel with it; see ensure_alarm_manager_started().
/* SAFE_MODE never re-opens this boot's ordinary interaction/Gateway admission.
 * Input Binding consults this value after its alarm-dismiss path, so a local
 * physical control remains useful for alarms but cannot start voice, meeting,
 * pairing, provisioning or configuration work. */
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
// The outgoing worker can receive a server speech URL while the offline
// recognizer is resident.  ESP-SR's model allocations fragment the internal
// heap enough for mbedTLS/AES to fail even though the eventual audio body is
// kept in PSRAM.  This flag records a successful, temporary recognizer
// teardown so the poll worker can restore it only after the entire message
// transaction (including its ACK) has finished.
/* Large TLS media transfers share one internal-memory lease. A foreground
 * server-audio transaction may preempt an optional pet worker; only the final
 * lease holder may restore the offline recognizer. */
static void media_transfer_stop_wake_word_for_media(const char *source, void *context);
static void media_transfer_cancel_startup_pet_for_server_audio(void *context);
static bool media_transfer_take_startup_pet_audio_preemption(void *context);
static void media_transfer_rearm_preempted_startup_pet(void *context);
static void media_transfer_schedule_wake_restart(void *context);
static void cancel_optional_startup_pet_asset_for_audio(void);
static void apply_deferred_startup_pet_asset(void);
static void rearm_preempted_startup_pet_asset(void);
static device_status_t prepare_startup_pet_asset_system_sleep(uint32_t timeout_ms);
static void abort_startup_pet_asset_system_sleep_prepare(void);
static device_status_t stop_startup_pet_asset_for_network_restart(uint32_t timeout_ms);
static bool startup_pet_asset_stop_requested(void);
static esp_err_t apply_deferred_pet_asset(void);
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
static device_status_t configuration_persistence_run_transaction(
    const configuration_persistence_request_t *request,
    configuration_persistence_reply_t *out_reply, void *context);
static device_status_t battery_emergency_checkpoint(uint32_t timeout_ms, void *context);
static device_status_t configuration_persistence_prepare_system_sleep(
    uint32_t timeout_ms, void *context);
static void configuration_persistence_abort_system_sleep_prepare(void *context);
/* Last display policy revision successfully handed to the shared
 * Display/Power consumer. This is composition state only; Configuration's
 * durable revision remains authoritative after a failed external apply. */
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


static void wifi_event(void *arg, const connectivity_wifi_driver_event_t *event);

/* Event callbacks execute on ESP-IDF's default loop.  Do not borrow a service
 * or UI state once lifecycle admission has closed.  The counter covers a
 * callback that was selected immediately before unregister: ESP-IDF marks its
 * handler unregistered, but its API does not offer a caller-bounded drain
 * guarantee. */
static void process_update_metadata(cJSON *update, bool defer_presentation);
static void publish_pending_update_reminder(void);
static device_status_t cancel_gateway_requests_for_system_sleep(uint32_t timeout_ms,
                                                                 void *context);
static device_status_t quiesce_network_dependents_for_restart(uint32_t timeout_ms,
                                                               void *context);
static device_status_t startup_status_from_esp_err(esp_err_t err);
static esp_err_t stop_network_core_transaction(uint32_t timeout_ms);
static esp_err_t stop_connectivity_root_transaction(uint32_t timeout_ms);
static bool hardware_audio_url_allowed(const char *url);
static esp_err_t handle_client_tool_call(cJSON *item);
static char s_delivered_tool_result_id[128];
static const char *json_string(cJSON *root, const char *key);
static bool json_number(cJSON *root, const char *key, int *value);
static void schedule_wake_restart(void);
static esp_err_t stop_wake_restart_task(uint32_t timeout_ms);
static device_status_t prepare_wake_restart_system_sleep(uint32_t timeout_ms);
static void abort_wake_restart_system_sleep_prepare(void);
static device_status_t prepare_wake_restart_network_restart(uint32_t timeout_ms);
static device_status_t commit_wake_restart_network_restart(void);
static device_status_t prepare_deferred_setup_system_sleep(uint32_t timeout_ms);
static void abort_deferred_setup_system_sleep_prepare(void);
static device_status_t server_audio_play_mp3(const uint8_t *data, uint32_t length,
                                             void *context);
static device_status_t server_audio_play_wav(const uint8_t *data, uint32_t length,
                                             void *context);
static device_status_t prepare_deferred_setup_network_restart(uint32_t timeout_ms);
static device_status_t commit_deferred_setup_network_restart(void);
static esp_err_t audio_wake_word_stop(void);
static esp_err_t audio_wake_word_stop_with_timeout(uint32_t timeout_ms);
static void pet(const char *state);
static void apply_deferred_startup_pet_asset(void);
static bool ensure_alarm_manager_started(void);

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

static device_status_t configuration_persistence_prepare_system_sleep(
    uint32_t timeout_ms, void *context) {
    (void)context;
    return configuration_persistence_worker_service_prepare_system_sleep(timeout_ms);
}

static void configuration_persistence_abort_system_sleep_prepare(void *context) {
    (void)context;
    configuration_persistence_worker_service_abort_system_sleep_prepare();
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
        factory_reset_service_reboot_if_pending(false);
        return ESP_ERR_NO_MEM;
    }
    cJSON_AddStringToObject(body, "clientId", gateway_transport_device_id());
    cJSON_AddStringToObject(body, "resultId", call_id);
    cJSON_AddStringToObject(body, "toolCallId", call_id);
    /* Retain the originating tool name in the durable envelope. Gateway
     * outbox replay uses this value to distinguish factory-reset results. */
    cJSON_AddStringToObject(body, "toolName", name);
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
    if (!payload) {
        factory_reset_service_reboot_if_pending(false);
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = (esp_err_t)gateway_transport_post_json(
        "/api/im-gateway/v1/tool-result", payload,
        GATEWAY_TRANSPORT_ACCEPT_200 | GATEWAY_TRANSPORT_ACCEPT_202 |
        GATEWAY_TRANSPORT_ACCEPT_204);
    bool result_durable = err == ESP_OK;
    if (err != ESP_OK) {
        const size_t payload_bytes = strlen(payload) + 1u;
        if (gateway_tool_result_outbox_validate_record(payload, payload_bytes,
                                                        GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) == DEVICE_STATUS_OK) {
            /* A failed tool result may be close to the 64 KiB envelope bound.
             * Keep both queue copies in PSRAM so an internal-heap pressure
             * event cannot turn a transport failure into an undeliverable
             * result. Persistence copies through its internal-stack worker. */
            char *queue = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                           MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
            size_t queue_size = GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY;
            device_status_t read_status = queue ? persistence_service_read_blob(
                "gateway", "tool_result_outbox", queue, &queue_size) : DEVICE_STATUS_RESOURCE_EXHAUSTED;
            if (read_status == DEVICE_STATUS_NOT_FOUND) queue_size = 0;
            char *updated = queue ? heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                                     MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) : NULL;
            size_t updated_size = 0;
            device_status_t append_status = (queue && updated &&
                (read_status == DEVICE_STATUS_OK || read_status == DEVICE_STATUS_NOT_FOUND)) ?
                gateway_tool_result_outbox_append(queue_size ? queue : NULL, queue_size,
                                                  payload, payload_bytes, updated,
                                                  GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY, &updated_size) :
                DEVICE_STATUS_RESOURCE_EXHAUSTED;
            const device_status_t persist_status = append_status == DEVICE_STATUS_OK ?
                persistence_service_write_blob("gateway", "tool_result_outbox", updated, updated_size) : append_status;
            result_durable = persist_status == DEVICE_STATUS_OK;
            if (persist_status != DEVICE_STATUS_OK) {
                ESP_LOGE(TAG, "cannot persist failed tool result: status=%d",
                         (int)persist_status);
            }
            free(updated);
            free(queue);
        }
    }
    free(payload);
    ESP_LOGI(TAG, "client tool result delivered: name=%s call=%s err=%s",
             name, call_id, esp_err_to_name(err));
    /* Factory reset marks the reboot handoff only after its durable journal is
     * cleared.  Let this tool-result path attempt delivery/outbox persistence
     * first, then perform the final reboot exactly once. */
    /* Only the factory_reset envelope can authorize the pending reset
     * handoff. A later unrelated tool-result must never accidentally satisfy
     * the delivery gate if the reset result itself was still undelivered. */
    factory_reset_service_reboot_if_pending(
        strcmp(name, "factory_reset") == 0 && result_durable);
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
    return gateway_transport_send_text_event(
        "/cancel", reply_to && reply_to[0] ? reply_to : NULL);
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
    (void)media_transfer_service_begin_server_audio_wake_lease(
        "server audio download");
    // A delivered voice reply is a foreground user outcome.  The cold-start
    // pet is decorative, so stop it before waiting for the media lane.  Its
    // worker observes this flag after the one already-running bounded request,
    // releases the lane, and cannot start another frame over the reply.
    cancel_optional_startup_pet_asset_for_audio();
    if (media_transfer_service_take_lane(35000) != DEVICE_STATUS_OK) {
        return ESP_ERR_TIMEOUT;
    }
    // Advertise priority before starting TLS so the optional pet worker cannot
    // begin another large transfer between the cursor parse and this request.
    media_transfer_service_set_audio_download_active(true);
    uint8_t *audio = NULL;
    uint32_t audio_len = 0;
    esp_err_t err = (esp_err_t)gateway_transport_download_media(url, &audio, &audio_len);
    media_transfer_service_set_audio_download_active(false);
    media_transfer_service_release_lane();
    if (err != ESP_OK) return err;
    *out_audio = audio;
    *out_len = audio_len;
    return ESP_OK;
}

typedef pet_asset_descriptor_t pet_asset_ref_t;

// Pet artwork is optional startup decoration. Keep its small descriptor here
// so the authenticated handshake can release TLS/JSON memory and initialize
// ESP-SR before any media download or SPIFFS write takes place.
// Written by the gateway poll task to pre-empt the optional cold-start pack,
// then observed by the startup worker between frame downloads/installs.
/* The cold-start descriptor may arrive before capability health has reached
 * operational.  The actual asynchronous download captures this value only
 * when it is admitted, and validates it at every network/cache/display
 * boundary.  It is never an HTTP, task, JSON, or board handle. */
/* Set when a server-audio transaction preempts the deferred cold-start pet
 * install.  The audio memory-lease finish re-arms the install afterwards, so
 * a slow backhaul (Fangtang's 4G, where the welcome audio routinely arrives
 * inside the deferred pet window) no longer loses the standby pet for the
 * entire boot. */
/* 在线宠物更新失败的有序重试会停住整个出站页游标（keep_cursor_for_retry），
 * 容量类失败（ESP_ERR_NO_MEM）永不成功时后面的远程播放等消息全部被堵死。
 * 记录同一消息 id 的连续失败次数，达到上限后按永久失败 ACK 放行队列。 */

static void free_pet_asset_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES], size_t frame_count) {
    pet_asset_apply_service_free_frames(frames, (uint32_t)frame_count);
}

/* The display install deliberately consumes its verified HTTP sources while it
 * creates renderer-owned scaled copies. A persistent full animation needs a
 * separate short-lived source set: never cache first and make a slow SPIFFS
 * operation delay the visible install. Failure is intentionally non-fatal;
 * the in-memory animation remains the user-visible result. */
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

static void media_transfer_stop_wake_word_for_media(const char *source, void *context) {
    (void)context;
    log_heap_snapshot("media-transfer-before-wake-stop");
    esp_err_t wake_stop_err = audio_wake_word_stop();
    if (wake_stop_err != ESP_OK && wake_stop_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "offline wake stop before %s: %s",
                 source ? source : "media transfer", esp_err_to_name(wake_stop_err));
    }
    log_heap_snapshot("media-transfer-after-wake-stop");
}

static void media_transfer_cancel_startup_pet_for_server_audio(void *context) {
    (void)context;
    cancel_optional_startup_pet_asset_for_audio();
}

static bool media_transfer_take_startup_pet_audio_preemption(void *context) {
    (void)context;
    return startup_pet_asset_state_service_take_audio_preemption();
}

static void media_transfer_rearm_preempted_startup_pet(void *context) {
    (void)context;
    rearm_preempted_startup_pet_asset();
}

static void media_transfer_schedule_wake_restart(void *context) {
    (void)context;
    schedule_wake_restart();
}

static void cancel_optional_startup_pet_asset_for_audio(void) {
    const bool preempted = startup_pet_asset_state_service_preempt_for_audio(
        startup_pet_worker_service_stop_requested());
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
static int64_t startup_pet_sleep_monotonic_time_us(void *context) {
    (void)context;
    return esp_timer_get_time();
}

static device_status_t startup_pet_sleep_prepare_state(void *context) {
    (void)context;
    return startup_pet_asset_state_service_prepare_system_sleep();
}

static device_status_t startup_pet_sleep_prepare_worker(uint32_t timeout_ms,
                                                         void *context) {
    (void)context;
    return startup_pet_worker_service_prepare_system_sleep(timeout_ms);
}

static device_status_t startup_pet_sleep_prepare_retry(uint32_t timeout_ms,
                                                        void *context) {
    (void)context;
    return startup_pet_retry_service_prepare_system_sleep(timeout_ms);
}

static device_status_t startup_pet_sleep_prepare_cache(uint32_t timeout_ms,
                                                        void *context) {
    (void)context;
    return pet_cache_service_prepare_system_sleep(timeout_ms);
}

static bool startup_pet_sleep_abort_state(bool *out_restored_audio_preemption,
                                          void *context) {
    (void)context;
    return startup_pet_asset_state_service_abort_system_sleep_prepare(
        out_restored_audio_preemption);
}

static void startup_pet_sleep_abort_worker(void *context) {
    (void)context;
    /* The lifecycle service itself delays a replacement until an old immutable
     * Registry identity is gone; this callback runs without root state locks. */
    startup_pet_worker_service_abort_system_sleep_prepare();
}

static void startup_pet_sleep_abort_retry(void *context) {
    (void)context;
    startup_pet_retry_service_abort_system_sleep_prepare();
}

static void startup_pet_sleep_abort_cache(void *context) {
    (void)context;
    pet_cache_service_abort_system_sleep_prepare();
}

static bool startup_pet_sleep_server_audio_lease_active(void *context) {
    (void)context;
    return media_transfer_service_server_audio_wake_lease_active();
}

static bool startup_pet_sleep_take_audio_preemption(void *context) {
    (void)context;
    return startup_pet_asset_state_service_take_audio_preemption();
}

static void startup_pet_sleep_rearm_preempted(void *context) {
    (void)context;
    rearm_preempted_startup_pet_asset();
}

static const startup_pet_asset_sleep_service_host_t s_startup_pet_asset_sleep_service_host = {
    .struct_size = sizeof(startup_pet_asset_sleep_service_host_t),
    .monotonic_time_us = startup_pet_sleep_monotonic_time_us,
    .prepare_state = startup_pet_sleep_prepare_state,
    .prepare_worker = startup_pet_sleep_prepare_worker,
    .prepare_retry = startup_pet_sleep_prepare_retry,
    .prepare_cache = startup_pet_sleep_prepare_cache,
    .abort_state = startup_pet_sleep_abort_state,
    .abort_worker = startup_pet_sleep_abort_worker,
    .abort_retry = startup_pet_sleep_abort_retry,
    .abort_cache = startup_pet_sleep_abort_cache,
    .server_audio_lease_active = startup_pet_sleep_server_audio_lease_active,
    .take_audio_preemption = startup_pet_sleep_take_audio_preemption,
    .rearm_preempted = startup_pet_sleep_rearm_preempted,
    .context = NULL,
};

static device_status_t prepare_startup_pet_asset_system_sleep(uint32_t timeout_ms) {
    return startup_pet_asset_sleep_service_prepare(
        &s_startup_pet_asset_sleep_service_host, timeout_ms);
}

static void abort_startup_pet_asset_system_sleep_prepare(void) {
    startup_pet_asset_sleep_service_abort(&s_startup_pet_asset_sleep_service_host);
}

/* This is terminal fault-domain quiescence, not the reversible startup-pet
 * sleep transaction. Once a later physical root stop begins, no ABORT may
 * revive the old download/retry/cache generation. */
static device_status_t stop_startup_pet_asset_for_network_restart(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    device_status_t status = startup_pet_worker_service_stop(remaining_ms);
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    status = startup_pet_retry_service_stop(remaining_ms);
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    return pet_cache_service_stop(remaining_ms);
}

static bool pet_asset_gateway_lease_current(
    const gateway_capability_lease_t *lease) {
    return lease && gateway_transport_capability_lease_current(lease);
}

static esp_err_t install_pet_asset_first_frame(
    const pet_asset_ref_t *ref,
    uint8_t *const frames[PET_ASSET_MAX_FRAMES]) {
    return device_status_to_platform_error(
        pet_asset_apply_service_install_preview(ref, frames));
}

static bool startup_pet_asset_install_still_admitted(void *context) {
    const uint32_t generation = (uint32_t)(uintptr_t)context;
    return startup_pet_asset_state_service_pending_generation(generation) &&
           !startup_pet_asset_stop_requested();
}

static bool pet_cache_host_storage_mounted(void *context) {
    (void)context;
    return storage_service_is_available();
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

static device_status_t pet_asset_download_host_admitted(bool startup_transaction,
                                                        void *context) {
    if (!startup_transaction) return DEVICE_STATUS_OK;
    const uint32_t generation = (uint32_t)(uintptr_t)context;
    return startup_pet_asset_state_service_pending_generation(generation) &&
           !startup_pet_asset_stop_requested()
               ? DEVICE_STATUS_OK
               : DEVICE_STATUS_BUSY;
}

static bool pet_asset_download_host_transaction_admitted(bool startup_transaction,
                                                         void *context) {
    return pet_asset_download_host_admitted(startup_transaction, context) ==
           DEVICE_STATUS_OK;
}

static bool pet_asset_download_host_gateway_lease_current(
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    return pet_asset_gateway_lease_current(lease);
}

static device_status_t pet_asset_download_host_request_frame(
    const char *url, uint32_t expected_bytes, bool startup_transaction,
    const gateway_capability_lease_t *lease, uint8_t **out_frame,
    uint32_t *out_length, int32_t *out_http_status, void *context) {
    if (!url || !out_frame || !out_length || !out_http_status) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    *out_frame = NULL;
    *out_length = 0;
    *out_http_status = 0;
    if (!pet_asset_download_host_transaction_admitted(startup_transaction, context) ||
        !pet_asset_gateway_lease_current(lease)) {
        return DEVICE_STATUS_BUSY;
    }
    /* Decorative startup artwork yields between frames to foreground voice
     * work.  A reply that arrives while a request is active still cancels via
     * the admission probe below, but no new TLS transfer may start ahead of
     * an interactive exchange. */
    while (startup_transaction &&
           (interaction_service_foreground_http_requested() ||
            media_transfer_service_audio_download_active() ||
            media_transfer_service_server_audio_wake_lease_active()) &&
           pet_asset_download_host_transaction_admitted(true, context)) {
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(100)) != 0) {
            return DEVICE_STATUS_BUSY;
        }
    }
    if (!pet_asset_download_host_transaction_admitted(startup_transaction, context)) {
        return DEVICE_STATUS_BUSY;
    }
    if (media_transfer_service_take_lane(35000) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_TIMEOUT;
    }

    bool optional_lease_held = false;
    if (startup_transaction) {
        /* The media lane makes this test-and-reserve atomic with foreground
         * audio. The shared download service owns traversal/retry only; the
         * physical wake-memory lease remains composition-root policy. */
        if (!pet_asset_download_host_transaction_admitted(true, context) ||
            media_transfer_service_server_audio_wake_lease_active()) {
            media_transfer_service_release_lane();
            return DEVICE_STATUS_BUSY;
        }
        media_transfer_service_begin_optional_wake_lease("optional pet asset");
        optional_lease_held = true;
    }

    uint8_t *frame = NULL;
    uint32_t frame_length = 0;
    int32_t http_status = 0;
    esp_err_t err = pet_asset_gateway_lease_current(lease)
                        ? (esp_err_t)gateway_transport_download_frame(
                              url, expected_bytes, &frame, &frame_length,
                              &http_status)
                        : ESP_ERR_INVALID_STATE;
    if (optional_lease_held) media_transfer_service_finish_optional_wake_lease();
    media_transfer_service_release_lane();
    if (startup_transaction &&
        !pet_asset_download_host_transaction_admitted(true, context)) {
        gateway_transport_release_media(frame);
        return DEVICE_STATUS_BUSY;
    }

    *out_http_status = http_status;
    if (err == ESP_OK) {
        *out_frame = frame;
        *out_length = frame_length;
        return DEVICE_STATUS_OK;
    }
    gateway_transport_release_media(frame);
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return err == ESP_OK ? DEVICE_STATUS_IO_ERROR : DEVICE_STATUS_IO_ERROR;
}

static device_status_t pet_asset_compute_sha256(const uint8_t *frame,
                                                uint32_t frame_bytes,
                                                uint8_t out_digest[32],
                                                void *context) {
    (void)context;
    if (!frame || frame_bytes == 0 || !out_digest) return DEVICE_STATUS_INVALID_ARGUMENT;
    size_t digest_len = 0;
    const psa_status_t status = psa_hash_compute(
        PSA_ALG_SHA_256, frame, frame_bytes, out_digest, 32, &digest_len);
    return (status == PSA_SUCCESS && digest_len == 32)
               ? DEVICE_STATUS_OK
               : DEVICE_STATUS_INTERNAL_ERROR;
}

static device_status_t pet_asset_download_host_verify_frame_sha256(
    const uint8_t *frame, uint32_t frame_bytes, const char expected_sha256[65],
    void *context) {
    (void)context;
    if (!frame || !expected_sha256) return DEVICE_STATUS_INVALID_ARGUMENT;
    return pet_asset_integrity_service_verify_frame(
        &(pet_asset_integrity_service_host_t){
            .struct_size = sizeof(pet_asset_integrity_service_host_t),
            .compute_sha256 = pet_asset_compute_sha256,
            .context = NULL,
        }, frame, frame_bytes, expected_sha256);
}

static void pet_asset_download_host_release_frame(uint8_t *frame, void *context) {
    (void)context;
    heap_caps_free(frame);
}

static bool pet_asset_download_host_wait_before_retry(uint32_t delay_ms,
                                                      bool startup_transaction,
                                                      void *context) {
    if (!pet_asset_download_host_transaction_admitted(startup_transaction, context)) {
        return false;
    }
    if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(delay_ms)) != 0) return false;
    return pet_asset_download_host_transaction_admitted(startup_transaction, context);
}

static bool pet_asset_download_host_wait_before_pack_retry(uint32_t delay_ms,
                                                           void *context) {
    return pet_asset_download_host_wait_before_retry(delay_ms, true, context);
}

static device_status_t pet_asset_download_host_install_preview(
    const pet_asset_descriptor_t *descriptor, const uint8_t *frame,
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    if (!pet_asset_gateway_lease_current(lease)) return DEVICE_STATUS_BUSY;
    uint8_t *frames[PET_ASSET_MAX_FRAMES] = {(uint8_t *)frame};
    /* The download service has revalidated the captured Gateway lease before
     * this callback. Preview remains best-effort, while the full install
     * performs its own late-admission check before consuming all frames. */
    const esp_err_t err = install_pet_asset_first_frame(descriptor, frames);
    return err == ESP_OK ? DEVICE_STATUS_OK : DEVICE_STATUS_IO_ERROR;
}

static const pet_asset_download_service_host_t s_pet_asset_download_service_host = {
    .struct_size = sizeof(pet_asset_download_service_host_t),
    .transaction_admitted = pet_asset_download_host_transaction_admitted,
    .gateway_lease_current = pet_asset_download_host_gateway_lease_current,
    .request_frame = pet_asset_download_host_request_frame,
    .verify_frame_sha256 = pet_asset_download_host_verify_frame_sha256,
    .release_frame = pet_asset_download_host_release_frame,
    .wait_before_retry = pet_asset_download_host_wait_before_retry,
    .wait_before_pack_retry = pet_asset_download_host_wait_before_pack_retry,
    .install_first_frame_preview = pet_asset_download_host_install_preview,
    .context = NULL,
};

static esp_err_t download_pet_asset_frames(const pet_asset_ref_t *ref,
                                           uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                           bool startup_transaction,
                                           const gateway_capability_lease_t *gateway_lease,
                                           uint32_t startup_generation) {
    if (!ref || !frames) return ESP_ERR_INVALID_ARG;
    if (!pet_asset_capacity_available(ref)) return ESP_ERR_NO_MEM;
    pet_asset_download_service_host_t host = s_pet_asset_download_service_host;
    host.context = (void *)(uintptr_t)startup_generation;
    const device_status_t status = startup_transaction
                                       ? pet_asset_download_service_fetch_startup_pack(
                                             &host, ref, gateway_lease, frames)
                                       : pet_asset_download_service_fetch(
                                             &host, ref, false, gateway_lease, frames);
    return device_status_to_platform_error(status);
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

static esp_err_t clear_pet_asset_cache_with_cancel(
    pet_cache_service_cancelled_fn cancelled, void *cancel_context) {
    return device_status_to_platform_error(
        pet_cache_service_clear(cancelled, cancel_context));
}

static esp_err_t clear_pet_asset_cache(void) {
    return clear_pet_asset_cache_with_cancel(NULL, NULL);
}

static esp_err_t clear_applied_pet_asset(void) {
    esp_err_t err = device_status_to_platform_error(
        pet_asset_apply_service_clear(NULL, NULL));
    if (err == ESP_OK) {
        if (device_storage_allows_optional_flash_work()) {
            esp_err_t cache_err = clear_pet_asset_cache();
            if (cache_err != ESP_OK) err = cache_err;
        }
    }
    return err;
}

static bool pet_asset_restore_storage_allowed(void *context) {
    (void)context;
    return device_storage_allows_optional_flash_work();
}

static bool pet_asset_restore_read_descriptor(pet_asset_ref_t *out_ref, void *context) {
    (void)context;
    return pet_asset_cache_storage_read_descriptor(out_ref);
}

static device_status_t pet_asset_restore_load_verified_frame(
    const pet_asset_ref_t *ref, uint32_t frame_index, uint8_t **out_frame,
    void *context) {
    (void)context;
    if (!ref || !out_frame || frame_index >= (uint32_t)ref->frame_count) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    *out_frame = NULL;
    size_t frame_bytes = 0;
    if (!pet_asset_service_frame_bytes(ref->width, ref->height, &frame_bytes)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    uint8_t *frame = heap_caps_malloc(frame_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!frame) frame = malloc(frame_bytes);
    if (!frame) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (!pet_asset_cache_storage_read_frame(ref, frame_index, frame, frame_bytes)) {
        heap_caps_free(frame);
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const device_status_t verify_status = pet_asset_integrity_service_verify_frame(
        &(pet_asset_integrity_service_host_t){
            .struct_size = sizeof(pet_asset_integrity_service_host_t),
            .compute_sha256 = pet_asset_compute_sha256,
            .context = NULL,
        }, frame, (uint32_t)frame_bytes, ref->sha256[frame_index]);
    if (verify_status != DEVICE_STATUS_OK) {
        heap_caps_free(frame);
        return verify_status;
    }
    *out_frame = frame;
    return DEVICE_STATUS_OK;
}

static device_status_t pet_asset_restore_install_full(
    const pet_asset_ref_t *ref, uint8_t *frames[PET_ASSET_MAX_FRAMES],
    int *out_installed_frame_count, int *out_installed_frame_ms, void *context) {
    (void)context;
    return pet_asset_apply_service_install_full(
        ref, frames, false, NULL, NULL, NULL, out_installed_frame_count,
        out_installed_frame_ms);
}

static void pet_asset_restore_release_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                             void *context) {
    (void)context;
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
}

static void pet_asset_restore_clear_cache(void *context) {
    (void)context;
    (void)clear_pet_asset_cache();
}

static void pet_asset_restore_apply_cached_profile(void *context) {
    (void)context;
    /* A cached pack has no live Hub descriptor yet, but it was previously
     * accepted under this profile. Preserve its animated standby behavior
     * until a runtime Hub profile replaces it. */
    ambient_service_apply_pet_profile(NULL, true);
}

static const pet_asset_restore_service_host_t s_pet_asset_restore_service_host = {
    .struct_size = sizeof(pet_asset_restore_service_host_t),
    .storage_restore_allowed = pet_asset_restore_storage_allowed,
    .read_descriptor = pet_asset_restore_read_descriptor,
    .load_verified_frame = pet_asset_restore_load_verified_frame,
    .install_full = pet_asset_restore_install_full,
    .release_frames = pet_asset_restore_release_frames,
    .clear_cache = pet_asset_restore_clear_cache,
    .apply_cached_profile = pet_asset_restore_apply_cached_profile,
    .context = NULL,
};

static device_status_t pet_asset_restore_run_transaction(void *context) {
    (void)context;
    const device_status_t status = pet_asset_restore_service_restore(
        &s_pet_asset_restore_service_host);
    if (status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "cached pet animation restored before connectivity startup");
    }
    return status;
}

static const pet_asset_restore_worker_service_host_t
    s_pet_asset_restore_worker_service_host = {
        .struct_size = sizeof(pet_asset_restore_worker_service_host_t),
        .run_restore = pet_asset_restore_run_transaction,
        .context = NULL,
    };

static bool pet_asset_runtime_revision_installed(const pet_asset_ref_t *ref,
                                                 void *context) {
    (void)context;
    return pet_asset_apply_service_revision_installed(ref);
}

static bool pet_asset_runtime_capture_gateway_lease(
    gateway_capability_lease_t *out_lease, void *context) {
    (void)context;
    return gateway_transport_capture_capability_lease(
        GATEWAY_CAPABILITY_PET_ASSET, out_lease);
}

static bool pet_asset_runtime_gateway_lease_current(
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    return pet_asset_gateway_lease_current(lease);
}

static bool pet_asset_runtime_transaction_admitted(void *context) {
    (void)context;
    /* Runtime profile updates are admitted only while the Hub control-plane
     * surface still exposes PET_ASSET.  This value probe complements the
     * captured lease: it closes the small media/cache/install windows during
     * System Sleep or a capability withdrawal without owning HTTP state. */
    return gateway_transport_capabilities_operational(
        GATEWAY_CAPABILITY_PET_ASSET);
}

static void pet_asset_runtime_begin_optional_media_work(void *context) {
    (void)context;
    media_transfer_service_begin_optional_wake_lease("runtime pet asset");
}

static void pet_asset_runtime_finish_optional_media_work(void *context) {
    (void)context;
    media_transfer_service_finish_optional_wake_lease();
}

static bool pet_asset_runtime_capacity_available(const pet_asset_ref_t *ref,
                                                 void *context) {
    (void)context;
    return pet_asset_capacity_available(ref);
}

static bool pet_asset_runtime_drop_stale_cache(const pet_asset_ref_t *ref,
                                               void *context) {
    (void)context;
    return drop_stale_pet_asset_cache(ref);
}

static device_status_t pet_asset_runtime_download(
    const pet_asset_ref_t *ref, const gateway_capability_lease_t *lease,
    uint8_t *frames[PET_ASSET_MAX_FRAMES], void *context) {
    (void)context;
    const esp_err_t err = download_pet_asset_frames(ref, frames, false, lease, 0);
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_IO_ERROR;
}

static bool pet_asset_runtime_prepare_cache_mirror(void *context) {
    (void)context;
    return device_storage_allows_optional_flash_work();
}

static device_status_t pet_asset_runtime_install_full(
    const pet_asset_ref_t *ref, uint8_t *frames[PET_ASSET_MAX_FRAMES],
    bool prepare_cache_mirror, uint8_t *cache_frames[PET_ASSET_MAX_FRAMES],
    int *out_installed_frame_count, int *out_installed_frame_ms, void *context) {
    (void)context;
    return pet_asset_apply_service_install_full(
        ref, frames, prepare_cache_mirror, cache_frames, NULL, NULL,
        out_installed_frame_count, out_installed_frame_ms);
}

static void pet_asset_runtime_cache_in_background(
    const pet_asset_ref_t *ref, uint8_t *cache_frames[PET_ASSET_MAX_FRAMES],
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    cache_pet_asset_in_background(ref, cache_frames, lease);
}

static void pet_asset_runtime_release_frames(
    uint8_t *frames[PET_ASSET_MAX_FRAMES], void *context) {
    (void)context;
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
}

static const pet_asset_runtime_service_host_t s_pet_asset_runtime_service_host = {
    .struct_size = sizeof(pet_asset_runtime_service_host_t),
    .revision_installed = pet_asset_runtime_revision_installed,
    .capture_gateway_lease = pet_asset_runtime_capture_gateway_lease,
    .gateway_lease_current = pet_asset_runtime_gateway_lease_current,
    .transaction_admitted = pet_asset_runtime_transaction_admitted,
    .begin_optional_media_work = pet_asset_runtime_begin_optional_media_work,
    .finish_optional_media_work = pet_asset_runtime_finish_optional_media_work,
    .capacity_available = pet_asset_runtime_capacity_available,
    .drop_stale_cache = pet_asset_runtime_drop_stale_cache,
    .download = pet_asset_runtime_download,
    .prepare_cache_mirror = pet_asset_runtime_prepare_cache_mirror,
    .install_full = pet_asset_runtime_install_full,
    .cache_in_background = pet_asset_runtime_cache_in_background,
    .release_frames = pet_asset_runtime_release_frames,
    .context = NULL,
};

static bool pet_asset_profile_startup_matches(const char *revision, const char *skin,
                                              void *context) {
    (void)context;
    return startup_pet_asset_state_service_matches_profile(revision, skin);
}

static bool pet_asset_profile_startup_pending(void *context) {
    (void)context;
    return startup_pet_asset_state_service_pending();
}

static void pet_asset_profile_set_startup_pending(bool pending, void *context) {
    (void)context;
    startup_pet_asset_state_service_set_pending(pending);
}

static device_status_t pet_asset_profile_apply(const pet_asset_ref_t *descriptor,
                                                void *context) {
    (void)context;
    const device_status_t status = pet_asset_runtime_service_apply(
        &s_pet_asset_runtime_service_host, descriptor);
    if (status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "GUI pet asset applied: revision=%s frames=%d/%d size=%dx%d",
                 descriptor->revision, descriptor->frame_count, descriptor->frame_count,
                 descriptor->width, descriptor->height);
    } else if (status == DEVICE_STATUS_BUSY) {
        ESP_LOGW(TAG, "pet asset update deferred: Gateway capability is not operational");
    }
    return status;
}

static device_status_t pet_asset_profile_clear(void *context) {
    (void)context;
    const esp_err_t err = clear_applied_pet_asset();
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_IO_ERROR;
}

static bool pet_asset_profile_status_permanently_invalid(device_status_t status,
                                                          void *context) {
    (void)context;
    return status == DEVICE_STATUS_INVALID_ARGUMENT ||
           status == DEVICE_STATUS_UNAVAILABLE ||
           status == DEVICE_STATUS_NOT_FOUND;
}

static uint32_t pet_asset_profile_note_transient_failure(const char *message_id,
                                                          void *context) {
    (void)context;
    return pet_asset_retry_service_note_failure(message_id);
}

static bool pet_asset_profile_retry_exhausted(uint32_t retry_limit, void *context) {
    (void)context;
    return pet_asset_retry_service_exhausted(retry_limit);
}

static void pet_asset_profile_reset_retries(void *context) {
    (void)context;
    pet_asset_retry_service_reset();
}

static const pet_asset_profile_service_host_t s_pet_asset_profile_service_host = {
    .struct_size = sizeof(pet_asset_profile_service_host_t),
    .startup_profile_matches = pet_asset_profile_startup_matches,
    .startup_pending = pet_asset_profile_startup_pending,
    .set_startup_pending = pet_asset_profile_set_startup_pending,
    .apply_asset = pet_asset_profile_apply,
    .clear_asset = pet_asset_profile_clear,
    .status_permanently_invalid = pet_asset_profile_status_permanently_invalid,
    .note_transient_failure = pet_asset_profile_note_transient_failure,
    .retry_exhausted = pet_asset_profile_retry_exhausted,
    .reset_retries = pet_asset_profile_reset_retries,
    .context = NULL,
};

static bool pet_asset_startup_snapshot(startup_pet_asset_state_snapshot_t *out_state,
                                       void *context) {
    (void)context;
    return startup_pet_asset_state_service_snapshot(out_state);
}

static bool pet_asset_startup_stop_requested(void *context) {
    (void)context;
    return startup_pet_asset_stop_requested();
}

static device_status_t pet_asset_startup_clear_applied(uint32_t generation,
                                                       void *context) {
    (void)context;
    /* The probe must run after acquiring the renderer mutex. A pre-check here
     * races a newer runtime install while this old withdrawal waits on it. */
    const device_status_t status = pet_asset_apply_service_clear(
        startup_pet_asset_install_still_admitted, (void *)(uintptr_t)generation);
    if (status != DEVICE_STATUS_OK) return status;
    /* Cache clear is an asynchronous Flash transaction. Carry the same
     * generation probe through its worker so a superseding descriptor cannot
     * lose its retained cache while the old withdrawal waits for Flash. */
    if (device_storage_allows_optional_flash_work() &&
        clear_pet_asset_cache_with_cancel(
            startup_pet_asset_install_still_admitted,
            (void *)(uintptr_t)generation) != ESP_OK) {
        return DEVICE_STATUS_IO_ERROR;
    }
    return DEVICE_STATUS_OK;
}

static bool pet_asset_startup_prepare_for_display(
    const pet_asset_ref_t *source, pet_asset_ref_t *out_display, void *context) {
    (void)context;
    return pet_asset_prepare_for_display(source, out_display);
}

static bool pet_asset_startup_revision_installed(const pet_asset_ref_t *ref,
                                                  void *context) {
    (void)context;
    return pet_asset_apply_service_revision_installed(ref);
}

static bool pet_asset_startup_capture_gateway_lease(
    gateway_capability_lease_t *out_lease, void *context) {
    (void)context;
    return gateway_transport_capture_capability_lease(
        GATEWAY_CAPABILITY_PET_ASSET, out_lease);
}

static bool pet_asset_startup_gateway_lease_current(
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    return pet_asset_gateway_lease_current(lease);
}

static bool pet_asset_startup_generation_admitted(uint32_t generation, void *context) {
    (void)context;
    return startup_pet_asset_state_service_pending_generation(generation) &&
           !startup_pet_asset_stop_requested();
}

static device_status_t pet_asset_startup_download(
    const pet_asset_ref_t *ref, const gateway_capability_lease_t *lease,
    uint32_t generation, uint8_t *frames[PET_ASSET_MAX_FRAMES], void *context) {
    (void)context;
    const esp_err_t err = download_pet_asset_frames(ref, frames, true, lease, generation);
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_INVALID_ARG || err == ESP_ERR_INVALID_SIZE) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_IO_ERROR;
}

static bool pet_asset_startup_prepare_cache_mirror(void *context) {
    (void)context;
    return device_storage_allows_optional_flash_work();
}

static device_status_t pet_asset_startup_install_full(
    const pet_asset_ref_t *ref, uint8_t *frames[PET_ASSET_MAX_FRAMES],
    bool prepare_cache_mirror, uint8_t *cache_frames[PET_ASSET_MAX_FRAMES],
    uint32_t generation, int *out_installed_frame_count,
    int *out_installed_frame_ms, void *context) {
    (void)context;
    /* The apply service holds the renderer transaction. This callback adds the
     * generation fence immediately before it consumes source frames. */
    return pet_asset_apply_service_install_full(
        ref, frames, prepare_cache_mirror, cache_frames,
        startup_pet_asset_install_still_admitted, (void *)(uintptr_t)generation,
        out_installed_frame_count, out_installed_frame_ms);
}

static void pet_asset_startup_cache_in_background(
    const pet_asset_ref_t *ref, uint8_t *cache_frames[PET_ASSET_MAX_FRAMES],
    const gateway_capability_lease_t *lease, void *context) {
    (void)context;
    cache_pet_asset_in_background(ref, cache_frames, lease);
}

static void pet_asset_startup_release_frames(uint8_t *frames[PET_ASSET_MAX_FRAMES],
                                             void *context) {
    (void)context;
    free_pet_asset_frames(frames, PET_ASSET_MAX_FRAMES);
}

static void pet_asset_startup_finish_generation(uint32_t generation, void *context) {
    (void)context;
    (void)startup_pet_asset_state_service_finish_generation(generation);
}

static const pet_asset_startup_service_host_t s_pet_asset_startup_service_host = {
    .struct_size = sizeof(pet_asset_startup_service_host_t),
    .snapshot = pet_asset_startup_snapshot,
    .stop_requested = pet_asset_startup_stop_requested,
    .clear_applied = pet_asset_startup_clear_applied,
    .prepare_for_display = pet_asset_startup_prepare_for_display,
    .revision_installed = pet_asset_startup_revision_installed,
    .capture_gateway_lease = pet_asset_startup_capture_gateway_lease,
    .gateway_lease_current = pet_asset_startup_gateway_lease_current,
    .generation_admitted = pet_asset_startup_generation_admitted,
    .download = pet_asset_startup_download,
    .prepare_cache_mirror = pet_asset_startup_prepare_cache_mirror,
    .install_full = pet_asset_startup_install_full,
    .cache_in_background = pet_asset_startup_cache_in_background,
    .release_frames = pet_asset_startup_release_frames,
    .finish_generation = pet_asset_startup_finish_generation,
    .context = NULL,
};

static esp_err_t apply_pet_asset_ref(cJSON *object) {
    pet_asset_ref_t descriptor;
    pet_asset_ref_t ref;
    if (!pet_asset_service_parse_hub_descriptor(object, &descriptor) ||
        !pet_asset_prepare_for_display(&descriptor, &ref)) return ESP_ERR_INVALID_ARG;
    return device_status_to_platform_error(
        pet_asset_profile_apply(&ref, NULL));
}

static esp_err_t apply_deferred_pet_asset(void) {
    const device_status_t status = pet_asset_startup_service_apply(
        &s_pet_asset_startup_service_host);
    if (status == DEVICE_STATUS_OK) {
        ESP_LOGI(TAG, "deferred startup pet asset transaction complete");
    } else if (status == DEVICE_STATUS_BUSY) {
        ESP_LOGW(TAG, "startup pet asset deferred: admission or Gateway capability changed");
    }
    return device_status_to_platform_error(status);
}

#define STARTUP_PET_RETRY_MAX 6u
#define STARTUP_PET_RETRY_INTERVAL_US (10ULL * 1000ULL * 1000ULL)

static bool schedule_startup_pet_retry_timer(void) {
    if (startup_pet_asset_stop_requested()) return false;
    return startup_pet_retry_service_schedule(STARTUP_PET_RETRY_INTERVAL_US) ==
           DEVICE_STATUS_OK;
}

static bool startup_pet_admission_snapshot(
    startup_pet_asset_state_snapshot_t *out_state, void *context) {
    (void)context;
    return startup_pet_asset_state_service_snapshot(out_state);
}

static bool startup_pet_admission_stop_requested(void *context) {
    (void)context;
    return startup_pet_asset_stop_requested();
}

static bool startup_pet_admission_system_sleep_preparing(void *context) {
    (void)context;
    return startup_pet_asset_state_service_system_sleep_preparing();
}

static bool startup_pet_admission_prepare_for_display(
    const pet_asset_ref_t *source, pet_asset_ref_t *out_display, void *context) {
    (void)context;
    return pet_asset_prepare_for_display(source, out_display);
}

static bool startup_pet_admission_capacity_available(const pet_asset_ref_t *ref,
                                                     void *context) {
    (void)context;
    return pet_asset_capacity_available(ref);
}

static bool startup_pet_admission_drop_stale_cache(const pet_asset_ref_t *ref,
                                                    void *context) {
    (void)context;
    return drop_stale_pet_asset_cache(ref);
}

static bool startup_pet_admission_take_capacity_retry(uint32_t generation,
                                                       uint32_t retry_limit,
                                                       uint32_t *out_attempt,
                                                       void *context) {
    (void)context;
    return startup_pet_asset_state_service_take_capacity_retry(
        generation, retry_limit, out_attempt);
}

static void startup_pet_admission_return_capacity_retry(uint32_t generation,
                                                        void *context) {
    (void)context;
    startup_pet_asset_state_service_return_capacity_retry(generation);
}

static bool startup_pet_admission_schedule_retry(void *context) {
    (void)context;
    return schedule_startup_pet_retry_timer();
}

static void startup_pet_admission_finish_generation(uint32_t generation, void *context) {
    (void)context;
    (void)startup_pet_asset_state_service_finish_generation(generation);
}

static bool startup_pet_admission_worker_active(void *context) {
    (void)context;
    return startup_pet_worker_service_active();
}

static bool startup_pet_admission_gateway_operational(void *context) {
    (void)context;
    return gateway_transport_capabilities_operational(
        GATEWAY_CAPABILITY_PET_ASSET);
}

static device_status_t startup_pet_admission_start_worker(void *context) {
    (void)context;
    return startup_pet_worker_service_start();
}

static bool startup_pet_admission_revision_installed(const pet_asset_ref_t *ref,
                                                      void *context) {
    (void)context;
    return pet_asset_apply_service_revision_installed(ref);
}

static void startup_pet_admission_set_pending(bool pending, void *context) {
    (void)context;
    startup_pet_asset_state_service_set_pending(pending);
}

static const startup_pet_asset_admission_service_host_t
    s_startup_pet_asset_admission_service_host = {
        .struct_size = sizeof(startup_pet_asset_admission_service_host_t),
        .snapshot = startup_pet_admission_snapshot,
        .stop_requested = startup_pet_admission_stop_requested,
        .system_sleep_preparing = startup_pet_admission_system_sleep_preparing,
        .prepare_for_display = startup_pet_admission_prepare_for_display,
        .capacity_available = startup_pet_admission_capacity_available,
        .drop_stale_cache = startup_pet_admission_drop_stale_cache,
        .take_capacity_retry = startup_pet_admission_take_capacity_retry,
        .return_capacity_retry = startup_pet_admission_return_capacity_retry,
        .schedule_retry = startup_pet_admission_schedule_retry,
        .finish_generation = startup_pet_admission_finish_generation,
        .worker_active = startup_pet_admission_worker_active,
        .gateway_operational = startup_pet_admission_gateway_operational,
        .start_worker = startup_pet_admission_start_worker,
        .revision_installed = startup_pet_admission_revision_installed,
        .set_pending = startup_pet_admission_set_pending,
        .context = NULL,
};

static void apply_deferred_startup_pet_asset(void) {
    uint32_t retry_attempt = 0;
    device_status_t start_status = DEVICE_STATUS_OK;
    const startup_pet_asset_admission_result_t result =
        startup_pet_asset_admission_service_admit_pending(
            &s_startup_pet_asset_admission_service_host, STARTUP_PET_RETRY_MAX,
            &retry_attempt, &start_status);
    if (result == STARTUP_PET_ASSET_ADMISSION_RETRY_SCHEDULED) {
        ESP_LOGW(TAG, "startup pet asset deferred: capacity tight, retry %u/%u in 10 s",
                 (unsigned)retry_attempt, STARTUP_PET_RETRY_MAX);
    } else if (result == STARTUP_PET_ASSET_ADMISSION_FINISHED) {
        ESP_LOGW(TAG, "startup pet asset skipped: shared optional capacity unavailable");
    } else if (result == STARTUP_PET_ASSET_ADMISSION_NO_ACTION &&
               !startup_pet_asset_stop_requested() &&
               !gateway_transport_capabilities_operational(
                   GATEWAY_CAPABILITY_PET_ASSET)) {
        ESP_LOGI(TAG, "startup pet asset remains deferred: Gateway capability unavailable");
    }
    if (result == STARTUP_PET_ASSET_ADMISSION_NO_ACTION &&
        start_status != DEVICE_STATUS_OK) {
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
    const startup_pet_asset_admission_result_t result =
        startup_pet_asset_admission_service_rearm_preempted(
            &s_startup_pet_asset_admission_service_host);
    if (result != STARTUP_PET_ASSET_ADMISSION_REARMED) return;
    if (!startup_pet_worker_service_active()) {
        ESP_LOGI(TAG, "startup pet asset re-armed after server audio");
        apply_deferred_startup_pet_asset();
        return;
    }
    ESP_LOGI(TAG, "startup pet asset re-armed after server audio (worker unwinding)");
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

static device_status_t server_audio_play_mp3(const uint8_t *data, uint32_t length,
                                             void *context) {
    (void)context;
    return mp3_player_play(data, length);
}

static device_status_t server_audio_play_wav(const uint8_t *data, uint32_t length,
                                             void *context) {
    (void)context;
    return audio_arbitration_play_wav(data, length);
}

static bool hardware_audio_url_allowed(const char *url) {
    return server_audio_presentation_service_url_allowed(url);
}

static bool gateway_transport_error_is_permanent(esp_err_t err) {
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
    return startup_runtime_state_service_matches_boot_session_id(boot_session_id);
}

static void startup_welcome_log_gate_released(const char *reason, void *context) {
    (void)context;
    ESP_LOGI(TAG, "startup Welcome gate released: %s", reason ? reason : "complete");
}

static void startup_welcome_log_gate_timed_out(uint32_t timeout_ms, void *context) {
    (void)context;
    ESP_LOGW(TAG, "startup Welcome gate timed out after %u ms; late greeting will be discarded",
             (unsigned)timeout_ms);
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
    const bool startup_welcome_queued = startup_welcome_service_begin_sequence();
    if (!startup_runtime_state_service_begin_sequence()) {
        ESP_LOGW(TAG, "startup sequence rejected because ordinary admission is closed");
        return false;
    }
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
        startup_runtime_state_service_complete_sequence();
        startup_welcome_service_mark_startup_failed();
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
    if (startup_welcome_queued) {
        ESP_LOGI(TAG, "startup Welcome gate armed; wake listener ready=%s",
                 wake_ready ? "yes" : "no");
        (void)startup_welcome_service_wait_for_completion(STARTUP_WELCOME_TIMEOUT_MS);
    } else {
        ESP_LOGI(TAG, "startup Welcome unavailable or disabled; continuing without playback");
    }
    // The normal standby surface is still published last. Touch/wake callbacks
    // remain blocked by the startup admission service while the greeting owns
    // the startup surface, although recognition itself is already hot.
    startup_runtime_state_service_complete_sequence();
    bool restart_after_startup = wake_restart_worker_service_consume_startup_teardown();
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
    if (lifecycle_status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (lifecycle_status != DEVICE_STATUS_OK) return lifecycle_status;

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms ? clock_sync_service_prepare_system_sleep(remaining_ms)
                                    : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (lifecycle_status != DEVICE_STATUS_OK) {
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms
        ? cellular_recovery_service_prepare_system_sleep(remaining_ms)
        : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (lifecycle_status != DEVICE_STATUS_OK) {
        cellular_recovery_service_abort_system_sleep_prepare();
        clock_sync_service_abort_system_sleep_prepare();
        abort_startup_pet_asset_system_sleep_prepare();
        return lifecycle_status;
    }

    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    lifecycle_status = remaining_ms ? prepare_wake_restart_system_sleep(remaining_ms)
                                    : DEVICE_STATUS_TIMEOUT;
    if (lifecycle_status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
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
    if (lifecycle_status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
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

/* The restart coordinator remains deliberately unbound from production
 * triggers: physical root reinitialization/rearm is not yet restart-safe.
 * This bridge only proves the terminal half of its first stage. It uses the
 * same single parent deadline as any future coordinator host and never calls
 * a System Sleep ABORT on error. */
static device_status_t quiesce_network_dependents_for_restart(uint32_t timeout_ms,
                                                               void *context) {
    (void)context;
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    device_status_t status = remaining_ms
        ? stop_startup_pet_asset_for_network_restart(remaining_ms)
        : DEVICE_STATUS_TIMEOUT;
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    status = remaining_ms ? prepare_wake_restart_network_restart(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    status = remaining_ms ? prepare_deferred_setup_network_restart(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    status = remaining_ms ? cellular_recovery_service_prepare_network_restart(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) return status;
    remaining_ms = startup_rollback_remaining_timeout_ms(deadline_us);
    status = remaining_ms ? gateway_lifecycle_service_prepare_network_restart(remaining_ms)
                          : DEVICE_STATUS_TIMEOUT;
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) return status;

    /* Commit in dependency order. From here every participating old
     * generation stays terminally closed, including if a later root phase
     * fails. */
    status = gateway_lifecycle_service_commit_prepared_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    if (startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    status = cellular_recovery_service_commit_prepared_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    if (startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    status = commit_deferred_setup_network_restart();
    if (status != DEVICE_STATUS_OK) return status;
    if (startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    status = commit_wake_restart_network_restart();
    /* The wake-restart worker is the terminal participant in this bridge.
     * Its successful return must still fit inside the one parent deadline;
     * otherwise the coordinator could publish a restart-ready generation
     * after its caller's bounded transaction has already expired. */
    if (status == DEVICE_STATUS_OK &&
        startup_rollback_remaining_timeout_ms(deadline_us) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return status;
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
    wake_deadline_service_on_trusted_wall_clock_updated();
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
    /* Keep Hub and SNTP on one lifecycle owner so generation, anomaly and
     * System Sleep admission are identical for both sources. */
    (void)clock_sync_service_apply_authenticated_millis(server_time->valuedouble);
}


static bool wake_restart_host_allowed(void *context) {
    (void)context;
    return !device_connectivity_is_provisioning_active() &&
           startup_runtime_state_service_sequence_complete();
}

static bool wake_restart_host_foreground_active(void *context) {
    (void)context;
    return interaction_service_worker_active() ||
           interaction_service_foreground_http_requested();
}

static bool wake_restart_host_meeting_active(void *context) {
    (void)context;
    return meeting_service_is_active();
}

static bool wake_restart_host_optional_pet_worker_active(void *context) {
    (void)context;
    return startup_pet_worker_service_active();
}

static void wake_restart_host_discard_asset_client(void *context) {
    (void)context;
    gateway_transport_discard_asset_client();
}

static device_status_t wake_restart_host_start_wake_word(void *context) {
    (void)context;
    return startup_status_from_esp_err(audio_wake_word_start(on_wake_word, NULL));
}

static const wake_restart_worker_service_host_t s_wake_restart_worker_service_host = {
    .struct_size = sizeof(wake_restart_worker_service_host_t),
    .restart_allowed = wake_restart_host_allowed,
    .foreground_active = wake_restart_host_foreground_active,
    .meeting_active = wake_restart_host_meeting_active,
    .optional_pet_worker_active = wake_restart_host_optional_pet_worker_active,
    .discard_asset_client = wake_restart_host_discard_asset_client,
    .start_wake_word = wake_restart_host_start_wake_word,
    .context = NULL,
};

static void schedule_wake_restart(void) {
    (void)wake_restart_worker_service_start();
}

static esp_err_t stop_wake_restart_task(uint32_t timeout_ms) {
    return device_status_to_esp_err(wake_restart_worker_service_stop(timeout_ms));
}

static device_status_t prepare_wake_restart_system_sleep(uint32_t timeout_ms) {
    return wake_restart_worker_service_prepare_system_sleep(timeout_ms);
}

static void abort_wake_restart_system_sleep_prepare(void) {
    wake_restart_worker_service_abort_system_sleep_prepare();
}

static device_status_t prepare_wake_restart_network_restart(uint32_t timeout_ms) {
    return wake_restart_worker_service_prepare_network_restart(timeout_ms);
}

static device_status_t commit_wake_restart_network_restart(void) {
    return wake_restart_worker_service_commit_prepared_network_restart();
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
        mbedtls_platform_zeroize(snapshot, sizeof(*snapshot));
        heap_caps_free(snapshot);
        return err;
    }
    /* This evidence is valid only under Configuration's durable boot-snapshot
     * lock. Capture it before any later transaction: a re-query after a
     * transient admission/read failure must never turn a candidate into a
     * false confirmed boot. It also prevents multi-network convenience
     * fallback from validating the confirmed network as the candidate. */
    if (!startup_runtime_state_service_capture_staged_provisioning(
            staged_provisioning)) {
        ESP_LOGE(TAG, "cannot capture boot provisioning candidate evidence");
        mbedtls_platform_zeroize(snapshot, sizeof(*snapshot));
        heap_caps_free(snapshot);
        return ESP_ERR_INVALID_STATE;
    }
    if (!wifi_runtime_configuration_service_capture_boot_snapshot(snapshot)) {
        ESP_LOGE(TAG, "cannot capture boot Wi-Fi runtime configuration");
        mbedtls_platform_zeroize(snapshot, sizeof(*snapshot));
        heap_caps_free(snapshot);
        return ESP_ERR_INVALID_STATE;
    }
    char snapshot_gateway_url[URL_CAPACITY];
    strlcpy(snapshot_gateway_url, snapshot->gateway_url, sizeof(snapshot_gateway_url));
    char snapshot_gateway_token[CONFIGURATION_GATEWAY_TOKEN_CAPACITY];
    strlcpy(snapshot_gateway_token, snapshot->gateway_token, sizeof(snapshot_gateway_token));
    char snapshot_pair_code[PROVISIONING_PAIR_CODE_CAPACITY];
    strlcpy(snapshot_pair_code, snapshot->pair_code, sizeof(snapshot_pair_code));
    gateway_transport_set_gateway_credentials(snapshot_gateway_url, snapshot_gateway_token,
                                              snapshot_pair_code);
    mbedtls_platform_zeroize(snapshot_gateway_token, sizeof(snapshot_gateway_token));
    mbedtls_platform_zeroize(snapshot_pair_code, sizeof(snapshot_pair_code));
    mbedtls_platform_zeroize(snapshot_gateway_url, sizeof(snapshot_gateway_url));
    mbedtls_platform_zeroize(snapshot, sizeof(*snapshot));
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
    return ESP_OK;
}

static esp_err_t save_output_volume(unsigned percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
    return device_status_to_platform_error(
        configuration_service_set_output_volume((uint8_t)percent));
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

static device_status_t configuration_persistence_run_transaction(
    const configuration_persistence_request_t *request,
    configuration_persistence_reply_t *out_reply, void *context) {
    (void)context;
    if (!request || !out_reply) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_reply = (configuration_persistence_reply_t){.status = DEVICE_STATUS_INTERNAL_ERROR};
    const configuration_policy_request_t hub_policy = {
        .struct_size = sizeof(configuration_policy_request_t),
        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
        .authenticated = true,
    };
    if (request->display_policy) {
        const configuration_display_policy_update_t update = {
            .struct_size = sizeof(configuration_display_policy_update_t),
            .abi_version = CONFIGURATION_DISPLAY_POLICY_UPDATE_ABI_VERSION,
            .has_brightness = request->display_policy_has_brightness,
            .brightness = (uint8_t)request->percent,
            .has_screen_sleep_seconds = request->display_policy_has_screen_sleep,
            .screen_sleep_seconds = request->screen_sleep_seconds,
        };
        out_reply->status = configuration_service_apply_display_policy_with_policy(
            &update, &hub_policy, &out_reply->configuration_revision);
    } else if (request->output_volume_policy) {
        out_reply->status = configuration_service_set_output_volume_with_policy_revision(
            (uint8_t)request->percent, &hub_policy, &out_reply->configuration_revision);
    } else if (request->gateway_token) {
        out_reply->status = configuration_service_commit_gateway_pairing_token(request->token);
    } else if (request->checkpoint_current_snapshot) {
        out_reply->status = configuration_service_checkpoint_current_snapshot(
            &out_reply->configuration_revision);
    } else if (request->brightness) {
        out_reply->status = request->hub_authenticated
            ? configuration_service_set_display_brightness_with_policy((uint8_t)request->percent,
                                                                        &hub_policy)
            : (device_status_t)startup_status_from_esp_err(save_display_brightness(request->percent));
    } else if (request->screen_sleep) {
        out_reply->status = request->hub_authenticated
            ? configuration_service_set_screen_sleep_seconds_with_policy(
                  request->screen_sleep_seconds, &hub_policy)
            : (device_status_t)startup_status_from_esp_err(
                  save_screen_sleep_seconds(request->screen_sleep_seconds));
    } else {
        out_reply->status = request->hub_authenticated
            ? configuration_service_set_output_volume_with_policy((uint8_t)request->percent,
                                                                   &hub_policy)
            : (device_status_t)startup_status_from_esp_err(save_output_volume(request->percent));
    }
    return out_reply->status;
}

static device_status_t battery_emergency_checkpoint(uint32_t timeout_ms, void *context) {
    (void)context;
    if (timeout_ms == 0u) return DEVICE_STATUS_INVALID_ARGUMENT;
    configuration_persistence_reply_t reply = {0};
    const configuration_persistence_request_t request = {
        .checkpoint_current_snapshot = true,
    };
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    return configuration_persistence_worker_service_submit_until(
        &request, deadline_us, &reply);
}

static esp_err_t submit_configuration_persistence(
    const configuration_persistence_request_t *request, uint32_t mutex_timeout_ms,
    uint32_t queue_timeout_ms, uint32_t completion_timeout_ms, uint64_t *out_revision) {
    configuration_persistence_reply_t reply = {0};
    const device_status_t status = configuration_persistence_worker_service_submit(
        request, mutex_timeout_ms, queue_timeout_ms, completion_timeout_ms, &reply);
    if (status == DEVICE_STATUS_OK && out_revision) *out_revision = reply.configuration_revision;
    return device_status_to_platform_error(status);
}

static esp_err_t persist_hardware_level(unsigned percent, bool brightness,
                                        bool hub_authenticated) {
    if (percent > 100u) return ESP_ERR_INVALID_ARG;
    return submit_configuration_persistence(
        &(configuration_persistence_request_t){
            .percent = percent, .brightness = brightness,
            .hub_authenticated = hub_authenticated,
        }, 4000, 1000, 3000, NULL);
}

static esp_err_t persist_output_volume(unsigned percent) {
    return persist_hardware_level(percent, false, false);
}

static esp_err_t persist_hub_output_volume(unsigned percent, uint64_t *out_revision) {
    if (percent > 100u || !out_revision) return ESP_ERR_INVALID_ARG;
    *out_revision = 0u;
    return submit_configuration_persistence(
        &(configuration_persistence_request_t){
            .percent = percent, .output_volume_policy = true, .hub_authenticated = true,
        }, 4000, 1000, 3000, out_revision);
}

static esp_err_t persist_hub_display_policy(bool has_brightness, unsigned brightness,
                                            bool has_screen_sleep, unsigned seconds,
                                            uint64_t *out_revision) {
    if ((!has_brightness && !has_screen_sleep) ||
        (has_brightness && brightness > 100u) ||
        (has_screen_sleep && !valid_screen_sleep_seconds((int)seconds)) || !out_revision) {
        return ESP_ERR_INVALID_ARG;
    }
    *out_revision = 0u;
    return submit_configuration_persistence(
        &(configuration_persistence_request_t){
            .percent = brightness, .screen_sleep_seconds = (uint32_t)seconds,
            .display_policy = true, .display_policy_has_brightness = has_brightness,
            .display_policy_has_screen_sleep = has_screen_sleep, .hub_authenticated = true,
        }, 4000, 1000, 3000, out_revision);
}

static bool is_enterprise_wifi(void) {
    wifi_runtime_configuration_snapshot_t runtime = {0};
    return wifi_runtime_configuration_service_get_snapshot(&runtime) &&
           !strcmp(runtime.security, "enterprise");
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
    configuration_persistence_request_t request = {.gateway_token = true};
    strlcpy(request.token, token, sizeof(request.token));
    const esp_err_t result = submit_configuration_persistence(&request, 4000, 4000, 4000, NULL);
    mbedtls_platform_zeroize(&request, sizeof(request));
    return result;
}

/* Meeting chunk streaming is owned by Gateway Transport. */

static void on_wake_word(void *arg) {
    (void)arg;
    if (!startup_runtime_state_service_sequence_complete()) {
        ESP_LOGI(TAG, "offline wake detected while startup greeting owns audio; ignored until ready");
        // The board has already retired MultiNet to safely hand this callback
        // off. Remember the one-shot teardown here; startup completion cannot
        // infer it from its earlier successful start result.
        wake_restart_worker_service_note_startup_teardown();
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

static bool deferred_setup_host_meeting_active(void *context) {
    (void)context;
    return meeting_service_is_active();
}

static void deferred_setup_host_start_portal(void *context) {
    (void)context;
    ESP_LOGI(TAG, "deferred configuration portal starting");
    enter_setup_portal();
}

static const deferred_setup_worker_service_host_t s_deferred_setup_worker_service_host = {
    .struct_size = sizeof(deferred_setup_worker_service_host_t),
    .meeting_active = deferred_setup_host_meeting_active,
    .start_setup_portal = deferred_setup_host_start_portal,
    .context = NULL,
};

static device_status_t prepare_deferred_setup_system_sleep(uint32_t timeout_ms) {
    return deferred_setup_worker_service_prepare_system_sleep(timeout_ms);
}

static void abort_deferred_setup_system_sleep_prepare(void) {
    deferred_setup_worker_service_abort_system_sleep_prepare();
}

static device_status_t prepare_deferred_setup_network_restart(uint32_t timeout_ms) {
    return deferred_setup_worker_service_prepare_network_restart(timeout_ms);
}

static device_status_t commit_deferred_setup_network_restart(void) {
    return deferred_setup_worker_service_commit_prepared_network_restart();
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
    return startup_runtime_state_service_sequence_complete();
}

static bool input_host_wifi_configured(void) {
    wifi_runtime_configuration_snapshot_t runtime = {0};
    return wifi_runtime_configuration_service_get_snapshot(&runtime) && runtime.ssid[0] != '\0';
}

static int32_t input_host_persist_output_volume(uint8_t percent) {
    return (int32_t)persist_output_volume(percent);
}

static bool input_host_start_deferred_setup(void) {
    return deferred_setup_worker_service_start() == DEVICE_STATUS_OK;
}

static bool input_host_safe_mode_active(void) {
    return startup_runtime_state_service_safe_mode_active();
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
    return device_status_to_platform_error(
        connectivity_network_lifecycle_service_ensure_core());
}

static device_status_t network_lifecycle_initialize_logical(void *context) {
    (void)context;
    const device_status_t connectivity_init_status = device_connectivity_initialize();
    if (connectivity_init_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "Connectivity Service initialization failed: device status=%d",
                 (int)connectivity_init_status);
        return connectivity_init_status;
    }
    /* Connectivity Service owns system-sleep admission/drain.  Keep the
     * concrete client/task cancellation bridge in this composition root so
     * neither Device API nor Power Service acquires ESP HTTP/RTOS knowledge. */
    connectivity_service_set_system_sleep_request_canceller(
        cancel_gateway_requests_for_system_sleep, NULL);
    connectivity_service_set_system_sleep_request_resumer(
        resume_gateway_workers_after_system_sleep_abort, NULL);

    return DEVICE_STATUS_OK;
}

static uint64_t network_lifecycle_monotonic_time_ms(void *context) {
    (void)context;
    const int64_t now_us = esp_timer_get_time();
    return now_us > 0 ? (uint64_t)now_us / 1000u : 0;
}

static device_status_t network_lifecycle_configure_physical_lifecycle(void *context) {
    (void)context;
    const connectivity_network_root_owner_lifecycle_host_t lifecycle_host = {
        .stop_provisioning = network_root_stop_provisioning,
        .stop_callback_admission = network_root_stop_callback_admission,
        .stop_clock_sync = network_root_stop_clock_sync,
        .provisioning_has_live_resources = network_root_provisioning_has_live_resources,
        .context = NULL,
    };
    const device_status_t lifecycle_host_status =
        connectivity_network_root_owner_configure_lifecycle_host(&lifecycle_host);
    if (lifecycle_host_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "network physical-root lifecycle bridge failed: device status=%d",
                 (int)lifecycle_host_status);
        return lifecycle_host_status;
    }
    return DEVICE_STATUS_OK;
}

static bool network_lifecycle_physical_has_resources(void *context) {
    (void)context;
    return connectivity_network_root_owner_has_resources();
}

static bool network_lifecycle_physical_core_ready(void *context) {
    (void)context;
    return connectivity_network_root_owner_core_ready();
}

static device_status_t network_lifecycle_ensure_physical_core(void *context) {
    (void)context;
    return connectivity_network_root_owner_ensure_core();
}

static bool network_lifecycle_wifi_has_resources(void *context) {
    (void)context;
    return connectivity_network_root_owner_wifi_has_resources();
}

static bool network_lifecycle_wifi_ready(void *context) {
    (void)context;
    return connectivity_network_root_owner_wifi_ready();
}

static device_status_t network_lifecycle_initialize_wifi(void *context) {
    (void)context;
    return connectivity_network_root_owner_initialize_wifi(wifi_event, NULL);
}

static void network_lifecycle_open_wifi_callback_admission(void *context) {
    (void)context;
    connectivity_service_open_wifi_event_callback_admission();
}

static device_status_t network_lifecycle_stop_physical(
    uint32_t timeout_ms, bool *out_wifi_radio_stopped, void *context) {
    (void)context;
    const device_status_t status = connectivity_network_root_owner_stop(
        timeout_ms, out_wifi_radio_stopped);
    if (out_wifi_radio_stopped && *out_wifi_radio_stopped) {
        device_connectivity_set_wifi_ready(false);
    }
    return status;
}

static device_status_t network_lifecycle_deinitialize_logical(uint32_t timeout_ms,
                                                               void *context) {
    (void)context;
    return device_connectivity_deinit(timeout_ms);
}

static const connectivity_network_lifecycle_service_host_t
    s_connectivity_network_lifecycle_service_host = {
        .struct_size = sizeof(connectivity_network_lifecycle_service_host_t),
        .now_ms = network_lifecycle_monotonic_time_ms,
        .initialize_logical = network_lifecycle_initialize_logical,
        .configure_physical_lifecycle = network_lifecycle_configure_physical_lifecycle,
        .physical_has_resources = network_lifecycle_physical_has_resources,
        .physical_core_ready = network_lifecycle_physical_core_ready,
        .ensure_physical_core = network_lifecycle_ensure_physical_core,
        .wifi_has_resources = network_lifecycle_wifi_has_resources,
        .wifi_ready = network_lifecycle_wifi_ready,
        .initialize_wifi = network_lifecycle_initialize_wifi,
        .open_wifi_callback_admission = network_lifecycle_open_wifi_callback_admission,
        .stop_physical = network_lifecycle_stop_physical,
        .deinitialize_logical = network_lifecycle_deinitialize_logical,
        .context = NULL,
};

/* Wi-Fi driver initialization is likewise a bounded transaction.  Event
 * handlers are registered one by one by ESP-IDF, hence a later registration
 * failure must remove each earlier instance before deinitializing the driver.
 * This gives the cold-start rollback a truthful ownership picture. */
static esp_err_t init_network(void) {
    return device_status_to_platform_error(
        connectivity_network_lifecycle_service_ensure_wifi());
}

static bool provisioning_qr_publish_modules(const uint8_t *modules,
                                            size_t module_count,
                                            const char *ssid, void *context) {
    (void)context;
    return scene_presenter_publish_setup_qr(modules, module_count, ssid);
}

static void provisioning_qr_publish_fallback_message(const char *title,
                                                      const char *body,
                                                      void *context) {
    (void)context;
    scene_presenter_publish_message(title, body);
}

static const provisioning_qr_service_host_t s_provisioning_qr_service_host = {
    .struct_size = sizeof(provisioning_qr_service_host_t),
    .publish_modules = provisioning_qr_publish_modules,
    .publish_fallback_message = provisioning_qr_publish_fallback_message,
    .context = NULL,
};


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
    device_status_t status = connectivity_network_lifecycle_service_stop(
        timeout_ms, &wifi_radio_stopped);
    return status == DEVICE_STATUS_OK ? ESP_OK : device_status_to_platform_error(status);
}

/* Keep lifecycle layering explicit: the composition root first stops all
 * ESP-IDF Wi-Fi/SNTP/netif/event-loop resources, then and only then lets the
 * hardware-neutral Connectivity Service delete its attempt EventGroup.  A
 * failed physical stop intentionally retains the logical generation: live
 * registered callbacks may still need that state and a new generation would
 * be unsafe. */
static esp_err_t stop_connectivity_root_transaction(uint32_t timeout_ms) {
    return stop_network_core_transaction(timeout_ms);
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
    const device_status_t status = provisioning_qr_service_show(ap_ssid, ap_passphrase);
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "cannot present setup QR: device status=%d", (int)status);
    }
}

static void provisioning_host_copy_runtime_wifi(provisioning_runtime_wifi_t *out) {
    if (!out) return;
    memset(out, 0, sizeof(*out));
    wifi_runtime_configuration_snapshot_t runtime = {0};
    if (!wifi_runtime_configuration_service_get_snapshot(&runtime)) return;
    strlcpy(out->wifi_ssid, runtime.ssid, sizeof(out->wifi_ssid));
    strlcpy(out->wifi_password, runtime.password, sizeof(out->wifi_password));
    strlcpy(out->wifi_security, runtime.security, sizeof(out->wifi_security));
    strlcpy(out->wifi_eap_method, runtime.eap_method, sizeof(out->wifi_eap_method));
    strlcpy(out->wifi_identity, runtime.identity, sizeof(out->wifi_identity));
    strlcpy(out->wifi_username, runtime.username, sizeof(out->wifi_username));
    strlcpy(out->wifi_ttls_phase2, runtime.ttls_phase2, sizeof(out->wifi_ttls_phase2));
    strlcpy(out->wifi_ca_mode, runtime.ca_mode, sizeof(out->wifi_ca_mode));
    strlcpy(out->wifi_server_domain, runtime.server_domain, sizeof(out->wifi_server_domain));
    (void)gateway_transport_gateway_url(out->gateway_url, sizeof(out->gateway_url));
}

static void provisioning_host_sync_runtime_after_network_delete(const char *ssid) {
    configuration_wifi_network_t networks[CONFIGURATION_WIFI_NETWORK_CAPACITY] = {0};
    uint8_t network_count = 0u;
    if (configuration_service_list_wifi_networks(
            networks, CONFIGURATION_WIFI_NETWORK_CAPACITY, &network_count) == DEVICE_STATUS_OK) {
        (void)wifi_runtime_configuration_service_sync_saved_networks_after_delete(
            networks, network_count, ssid);
    }
}

static void provisioning_host_copy_preferred_scan_ssid(char *out, uint32_t capacity) {
    if (!out || capacity == 0u) return;
    out[0] = '\0';
    wifi_runtime_configuration_snapshot_t runtime = {0};
    if (wifi_runtime_configuration_service_get_snapshot(&runtime)) {
        strlcpy(out, runtime.ssid, capacity);
    }
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
    .copy_preferred_scan_ssid = provisioning_host_copy_preferred_scan_ssid,
};

static void wifi_event(void *arg, const connectivity_wifi_driver_event_t *event) {
    (void)arg;
    if (!event || !connectivity_service_wifi_event_callback_enter()) return;
    wifi_runtime_configuration_snapshot_t runtime = {0};
    const bool runtime_available = wifi_runtime_configuration_service_get_snapshot(&runtime);
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
        ambient_service_apply_network(runtime_available ? runtime.ssid : "", false);
        scene_presenter_publish_service_ready(false);
        firmware_identity_set_service_ready(false);
        if (connectivity_wifi_driver_owner_take_expected_disconnect()) {
            ESP_LOGI(TAG, "station disconnected for setup scan");
            goto finish;
        }
        if (connectivity_wifi_driver_owner_should_auto_connect()) {
            ESP_LOGW(TAG, "Wi-Fi disconnected from %s; retrying",
                     runtime_available ? runtime.ssid : "");
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
        ESP_LOGI(TAG, "Wi-Fi connected to %s", runtime_available ? runtime.ssid : "");
        cellular_recovery_service_note_wifi_ready();
    }
finish:
    connectivity_service_wifi_event_callback_leave();
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
    return startup_runtime_state_service_gateway_startup_recovery_allowed();
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

static device_status_t wifi_startup_ensure_network(void *context) {
    (void)context;
    return startup_status_from_esp_err(init_network());
}

static device_status_t wifi_startup_ensure_station(void *context) {
    (void)context;
    return provisioning_network_owner_ensure_station();
}

static void wifi_startup_set_station_policy(bool auto_connect,
                                            bool expected_disconnect,
                                            void *context) {
    (void)context;
    connectivity_wifi_driver_owner_set_station_policy(auto_connect, expected_disconnect);
}

static device_status_t wifi_startup_configure_station(
    const wifi_startup_service_station_config_t *config, void *context) {
    (void)context;
    if (!config) return DEVICE_STATUS_INVALID_ARGUMENT;
    return connectivity_wifi_driver_owner_configure_station(
        &(connectivity_wifi_driver_station_config_t){
            .ssid = config->ssid,
            .password = config->password,
            .enterprise = config->enterprise,
            .keep_setup_ap = config->keep_setup_ap,
        });
}

static bool wifi_startup_enterprise_enabled(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_enterprise_enabled();
}

static device_status_t wifi_startup_configure_enterprise(
    const wifi_startup_service_enterprise_config_t *config, void *context) {
    (void)context;
    if (!config) return DEVICE_STATUS_INVALID_ARGUMENT;
    return connectivity_wifi_driver_owner_configure_enterprise(
        &(connectivity_wifi_driver_enterprise_config_t){
            .identity = config->identity,
            .username = config->username,
            .password = config->password,
            .server_domain = config->server_domain,
            .use_ttls = config->use_ttls,
            .ttls_phase2_pap = config->ttls_phase2_pap,
            .use_system_ca = config->use_system_ca,
        });
}

static device_status_t wifi_startup_disable_enterprise(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_disable_enterprise();
}

static bool wifi_startup_started(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_started();
}

static device_status_t wifi_startup_start(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_start();
}

static device_status_t wifi_startup_connect(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_connect();
}

static device_status_t wifi_startup_disconnect(void *context) {
    (void)context;
    return connectivity_wifi_driver_owner_disconnect();
}

typedef struct {
    wifi_startup_service_scan_observer_t observer;
    void *context;
} wifi_startup_scan_context_t;

static bool wifi_startup_scan_adapter(const char *ssid, int8_t rssi,
                                      connectivity_wifi_driver_security_t security,
                                      void *context) {
    (void)security;
    wifi_startup_scan_context_t *scan_context = context;
    return scan_context && scan_context->observer &&
           scan_context->observer(ssid, rssi, scan_context->context);
}

static device_status_t wifi_startup_scan(uint32_t maximum_records,
                                         wifi_startup_service_scan_observer_t observer,
                                         void *observer_context, void *context) {
    (void)context;
    if (!observer) return DEVICE_STATUS_INVALID_ARGUMENT;
    wifi_startup_scan_context_t scan_context = {
        .observer = observer,
        .context = observer_context,
    };
    return connectivity_wifi_driver_owner_scan_visible(
        maximum_records, wifi_startup_scan_adapter, &scan_context);
}

static void wifi_startup_select_saved_network(const char *ssid, const char *password,
                                              void *context) {
    (void)context;
    if (!ssid || !password) return;
    (void)wifi_runtime_configuration_service_select_saved_network(ssid, password);
}

static uint32_t wifi_startup_begin_attempt(const char *ssid, void *context) {
    (void)context;
    return device_connectivity_begin_wifi_attempt(ssid);
}

static bool wifi_startup_wait_attempt(uint32_t attempt_epoch, uint32_t timeout_ms,
                                      void *context) {
    (void)context;
    return device_connectivity_wait_wifi_attempt(attempt_epoch, timeout_ms);
}

static void wifi_startup_publish_network_ready(const char *ssid, bool ready,
                                               void *context) {
    (void)context;
    device_connectivity_set_wifi_ready(ready);
    wifi_runtime_configuration_snapshot_t runtime = {0};
    const char *network = ssid;
    if (!network && wifi_runtime_configuration_service_get_snapshot(&runtime)) {
        network = runtime.ssid;
    }
    ambient_service_apply_network(network ? network : "", ready);
}

static bool wifi_startup_setup_portal_active(void *context) {
    (void)context;
    return provisioning_service_has_live_resources();
}

static const wifi_startup_service_host_t s_wifi_startup_service_host = {
    .ensure_network = wifi_startup_ensure_network,
    .ensure_station = wifi_startup_ensure_station,
    .set_station_policy = wifi_startup_set_station_policy,
    .configure_station = wifi_startup_configure_station,
    .enterprise_enabled = wifi_startup_enterprise_enabled,
    .configure_enterprise = wifi_startup_configure_enterprise,
    .disable_enterprise = wifi_startup_disable_enterprise,
    .wifi_started = wifi_startup_started,
    .wifi_start = wifi_startup_start,
    .wifi_connect = wifi_startup_connect,
    .wifi_disconnect = wifi_startup_disconnect,
    .scan_visible = wifi_startup_scan,
    .select_saved_network = wifi_startup_select_saved_network,
    .begin_attempt = wifi_startup_begin_attempt,
    .wait_attempt = wifi_startup_wait_attempt,
    .publish_network_ready = wifi_startup_publish_network_ready,
    .setup_portal_active = wifi_startup_setup_portal_active,
    .context = NULL,
};

static bool start_wifi(void) {
    /* A prior DHCP session is not evidence for this new adapter start. The
     * IP event publishes readiness only after this attempt acquires an
     * address. This is particularly important when Fangtang switches from
     * ML307 back to Wi-Fi during a recovery path. */
    wifi_runtime_configuration_snapshot_t runtime = {0};
    if (!wifi_runtime_configuration_service_get_snapshot(&runtime)) {
        ESP_LOGE(TAG, "Wi-Fi runtime configuration unavailable");
        return false;
    }
    wifi_startup_service_saved_network_t
        saved_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY] = {0};
    const uint8_t saved_count = runtime.saved_network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY
                                    ? CONFIGURATION_WIFI_NETWORK_CAPACITY
                                    : runtime.saved_network_count;
    for (uint8_t index = 0; index < saved_count; ++index) {
        saved_networks[index].ssid = runtime.saved_networks[index].ssid;
        saved_networks[index].password = runtime.saved_networks[index].password;
    }
    const bool enterprise = !strcmp(runtime.security, "enterprise");
    const wifi_startup_service_request_t request = {
        .ssid = runtime.ssid,
        .password = runtime.password,
        .boot_provisioning_staged =
            startup_runtime_state_service_staged_provisioning_pending(),
        .enterprise = enterprise,
        .enterprise_config = {
            .identity = runtime.identity[0] ? runtime.identity : runtime.username,
            .username = runtime.username,
            .password = runtime.password,
            .server_domain = runtime.server_domain,
            .use_ttls = !strcmp(runtime.eap_method, "ttls"),
            .ttls_phase2_pap = !strcmp(runtime.ttls_phase2, "pap"),
            .use_system_ca = !strcmp(runtime.ca_mode, "system"),
        },
        .saved_networks = saved_networks,
        .saved_network_count = saved_count,
        .scan_maximum_records = SETUP_SCAN_MAX_APS,
        .candidate_connect_timeout_ms = WIFI_CANDIDATE_CONNECT_TIMEOUT_MS,
        .connect_timeout_ms = WIFI_CONNECT_TIMEOUT_MS,
    };
    const device_status_t status = wifi_startup_service_connect(
        &s_wifi_startup_service_host, &request);
    if (status == DEVICE_STATUS_OK) return true;
    ESP_LOGW(TAG, "Wi-Fi startup did not reach readiness: device status=%d ssid=%s",
             (int)status, runtime.ssid);
    return false;
}

static bool ensure_alarm_manager_started(void) {
    if (alarm_manager_is_initialized()) return true;
    device_status_t status = alarm_manager_init();
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "cannot start alarm scheduler: device status=%d", (int)status);
        return false;
    }
    alarm_manager_set_ring_callback(on_alarm_ring_start, NULL);
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
    if (deferred_setup_worker_service_stop(remaining_ms) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_BUSY;
    }
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
    SAFE_MODE_NEXT_TIMEOUT("cellular recovery");
    device_status_t cellular_status =
        cellular_recovery_service_prepare_network_restart(remaining_ms);
    if (cellular_status != DEVICE_STATUS_OK) return cellular_status;
    SAFE_MODE_NEXT_TIMEOUT("gateway lifecycle");
    device_status_t gateway_status =
        gateway_lifecycle_service_prepare_network_restart(remaining_ms);
    if (gateway_status != DEVICE_STATUS_OK) return gateway_status;
    SAFE_MODE_NEXT_TIMEOUT("Gateway terminal commit");
    gateway_status = gateway_lifecycle_service_commit_prepared_network_restart();
    if (gateway_status != DEVICE_STATUS_OK) return gateway_status;
    SAFE_MODE_NEXT_TIMEOUT("cellular recovery terminal commit");
    cellular_status = cellular_recovery_service_commit_prepared_network_restart();
    if (cellular_status != DEVICE_STATUS_OK) return cellular_status;
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
    if (status == DEVICE_STATUS_OK || startup_runtime_state_service_safe_mode_active()) {
        return STARTUP_SAFE_MODE_ENTRY_NOT_STARTED;
    }
    const safe_mode_entry_t entry = {
        .struct_size = sizeof(entry),
        .abi_version = SAFE_MODE_COORDINATOR_ABI_VERSION,
        .failed_phase = phase,
        .failure_status = status,
    };
    if (safe_mode_coordinator_configure_host(&s_safe_mode_host) !=
        DEVICE_STATUS_OK) {
        return STARTUP_SAFE_MODE_ENTRY_NOT_STARTED;
    }
    /* Close ordinary admission before quiescence starts.  A concurrently
     * completed gesture or Wi-Fi callback then cannot create work while the
     * bridge is isolating its fault domain. */
    if (!startup_runtime_state_service_enter_safe_mode()) {
        return STARTUP_SAFE_MODE_ENTRY_NOT_STARTED;
    }
    wake_restart_worker_service_close_admission();
    const device_status_t safe_mode_status = safe_mode_coordinator_enter(
        &entry, SAFE_MODE_ENTRY_TIMEOUT_MS);
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
    if (app_ui_is_initialized()) {
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
    if (app_ui_is_initialized()) {
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
    if (device_connectivity_has_cellular_transport_session()) {
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
    if (alarm_stop_status != DEVICE_STATUS_OK) {
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
    return storage_service_is_available();
}

static int32_t meeting_host_wake_word_stop(void) {
    return (int32_t)audio_wake_word_stop();
}

static int32_t meeting_host_wake_word_start(void) {
    return (int32_t)audio_wake_word_start(on_wake_word, NULL);
}

static int32_t meeting_host_recording_create(char *out_recording_id, uint32_t capacity) {
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    return gateway_transport_create_meeting(base_path, out_recording_id, capacity);
}

static int32_t meeting_host_recording_get_status(const char *recording_id,
                                                 char *out_status, uint32_t capacity) {
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    return gateway_transport_get_meeting_status(base_path, recording_id, out_status, capacity);
}

static int32_t meeting_host_recording_post_action(const char *recording_id,
                                                  const char *action,
                                                  const char *payload,
                                                  int32_t expected_a, int32_t expected_b) {
    char base_path[MEETING_SERVICE_BASE_PATH_CAPACITY];
    meeting_service_base_path(base_path, sizeof(base_path));
    return gateway_transport_post_meeting_action(base_path, recording_id, action, payload,
                                                 expected_a, expected_b);
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
    return startup_runtime_state_service_staged_provisioning_pending();
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
    startup_welcome_service_note_handshake_queued(queued);
}

static const char *transport_host_boot_session_id(void) {
    return startup_runtime_state_service_boot_session_id();
}

static void transport_host_note_cold_start_pet_asset(const void *pet_asset_node,
                                                     const char *skin) {
    cJSON *pet_asset = (cJSON *)pet_asset_node;
    pet_asset_ref_t descriptor = {0};
    const bool present = cJSON_IsObject(pet_asset) &&
                         pet_asset_service_parse_hub_descriptor(pet_asset, &descriptor);
    if (startup_pet_asset_state_service_record(
            present ? &descriptor : NULL, present, skin) != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "startup pet asset state unavailable; ignoring handshake descriptor");
        return;
    }
    if (cJSON_IsObject(pet_asset) && !present) {
        ESP_LOGW(TAG, "startup pet asset descriptor is invalid; cached asset will be cleared after wake readiness");
    }
    ESP_LOGI(TAG, "startup pet asset deferred until wake ready: %s",
             present ? descriptor.revision : "none");
}

static int32_t transport_host_apply_pet_asset(const void *pet_asset_node) {
    return (int32_t)apply_pet_asset_ref((cJSON *)pet_asset_node);
}

static int32_t transport_host_clear_pet_asset(void) {
    return (int32_t)clear_applied_pet_asset();
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
    return startup_welcome_service_gate_active();
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
    if (startup_welcome_service_should_discard_current()) {
        // Never play a boot greeting after MultiNet has been started. ACK it
        // as handled so a late delivery cannot retry forever. The same rule
        // also turns an ACK retry after successful playback into a silent,
        // idempotent delivery instead of replaying the greeting.
        ESP_LOGW(TAG, "late or already consumed startup Welcome discarded: id=%s", id);
        return GATEWAY_DISPATCHER_WELCOME_DISCARD_CURRENT;
    }
    return GATEWAY_DISPATCHER_WELCOME_CURRENT;
}

static void gateway_host_welcome_complete(bool playback_succeeded) {
    startup_welcome_service_complete_current(playback_succeeded);
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
    pet_asset_ref_t descriptor = {0};
    pet_asset_ref_t display_descriptor = {0};
    const pet_asset_ref_t *normalized = NULL;
    if (cJSON_IsObject(pet_asset)) {
        if (!pet_asset_service_parse_hub_descriptor(pet_asset, &descriptor) ||
            !pet_asset_prepare_for_display(&descriptor, &display_descriptor)) {
            *out_handled = false;
            *out_permanently_invalid = true;
            ESP_LOGW(TAG, "ignored malformed pet asset profile");
            return;
        }
        normalized = &display_descriptor;
    }

    const pet_asset_profile_service_result_t result =
        pet_asset_profile_service_apply(
            &s_pet_asset_profile_service_host, normalized, skin, id,
            PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT);
    *out_handled = result.handled;
    *out_permanently_invalid = result.permanently_invalid;
    if (result.deferred_to_startup) {
        ESP_LOGI(TAG, "startup pet_profile asset deferred to handshake installer");
    } else if (result.superseded_startup) {
        ESP_LOGI(TAG, "new GUI pet revision supersedes startup asset");
    }
    if (!result.handled) {
        ESP_LOGW(TAG, "pet asset %s failed: status=%d (retry %u/%u)",
                 normalized ? "update" : "clear", (int)result.status,
                 (unsigned)result.retry_count,
                 (unsigned)PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT);
    }
}

static bool gateway_host_tool_result_outbox_already_delivered(const void *message_item) {
    if (!message_item || !s_delivered_tool_result_id[0]) return false;
    cJSON *item = (cJSON *)message_item;
    cJSON *call = cJSON_GetObjectItemCaseSensitive(item, "toolCallId");
    if (!cJSON_IsString(call) || !call->valuestring) call = cJSON_GetObjectItemCaseSensitive(item, "id");
    if (!cJSON_IsString(call) || !call->valuestring || strcmp(call->valuestring, s_delivered_tool_result_id) != 0) return false;
    s_delivered_tool_result_id[0] = '\0';
    return true;
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
    return server_audio_presentation_service_mime_supported(mime);
}

static bool gateway_host_audio_download_error_is_permanent(int32_t err) {
    return gateway_transport_error_is_permanent((esp_err_t)err);
}

static bool gateway_host_audio_presentation_error_is_permanent(int32_t err) {
    return server_audio_presentation_service_error_is_permanent(
        (device_status_t)err);
}

static bool gateway_host_begin_server_audio_wake_lease(const char *source) {
    return media_transfer_service_begin_server_audio_wake_lease(source);
}

static bool gateway_host_finish_server_audio_wake_lease(void) {
    return media_transfer_service_finish_server_audio_wake_lease();
}

static int32_t gateway_host_download_audio(const char *url, uint8_t **out_audio,
                                           uint32_t *out_len) {
    size_t len = 0;
    esp_err_t err = download_audio(url, out_audio, &len);
    *out_len = (uint32_t)len;
    return (int32_t)err;
}

static void gateway_host_release_audio(uint8_t *audio) {
    gateway_transport_release_media(audio);
}

static int32_t gateway_host_play_audio_payload(const char *mime, const uint8_t *data,
                                               uint32_t len) {
    return (int32_t)server_audio_presentation_service_play(mime, data, len);
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

static int32_t gateway_host_flush_tool_result_outbox(void) {
    /* A full queue is intentionally kept out of internal heap: a Tool-result
     * envelope may approach the 64 KiB bound while Wi-Fi/TLS and audio still
     * require internal DMA-capable memory. Persistence routes the request to
     * its internal-stack worker and safely copies from PSRAM. */
    char *payload = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                     MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!payload) return ESP_ERR_NO_MEM;
    size_t size = GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY;
    const device_status_t read_status = persistence_service_read_blob(
        "gateway", "tool_result_outbox", payload, &size);
    if (read_status == DEVICE_STATUS_NOT_FOUND) {
        free(payload);
        return ESP_OK;
    }
    if (read_status != DEVICE_STATUS_OK) {
        free(payload);
        return ESP_ERR_INVALID_RESPONSE;
    }
    /* Upgrade the pre-versioned length-only queue before replay.  The
     * migration is value-only and is committed before any POST, so a reset
     * cannot expose a partially interpreted legacy record. */
    if (gateway_tool_result_outbox_validate_queue(payload, size,
                                                  GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) != DEVICE_STATUS_OK) {
        char *upgraded = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                          MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        size_t upgraded_size = 0;
        const device_status_t upgrade_status = upgraded ?
            gateway_tool_result_outbox_upgrade_legacy(payload, size, upgraded,
                                                      GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                                      &upgraded_size) :
            DEVICE_STATUS_RESOURCE_EXHAUSTED;
        if (upgrade_status != DEVICE_STATUS_OK ||
            persistence_service_write_blob("gateway", "tool_result_outbox",
                                           upgraded, upgraded_size) != DEVICE_STATUS_OK) {
            free(upgraded); free(payload); return ESP_ERR_INVALID_RESPONSE;
        }
        memcpy(payload, upgraded, upgraded_size);
        size = upgraded_size;
        free(upgraded);
    }
    char *record = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                    MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    size_t record_size = 0;
    if (!record || gateway_tool_result_outbox_peek(payload, size, record,
            GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY, &record_size) != DEVICE_STATUS_OK) {
        free(record); free(payload); return ESP_ERR_INVALID_RESPONSE;
    }
    cJSON *record_json = cJSON_Parse(record);
    const char *result_id = record_json ? json_string(record_json, "resultId") : NULL;
    if (!result_id && record_json) result_id = json_string(record_json, "toolCallId");
    const char *tool_name = record_json ? json_string(record_json, "toolName") : NULL;
    const bool is_factory_reset_result = tool_name &&
                                         strcmp(tool_name, "factory_reset") == 0;
    if (result_id && result_id[0]) snprintf(s_delivered_tool_result_id, sizeof(s_delivered_tool_result_id), "%s", result_id);
    cJSON_Delete(record_json);
    const int32_t post_status = gateway_transport_post_json(
        "/api/im-gateway/v1/tool-result", record,
        GATEWAY_TRANSPORT_ACCEPT_200 | GATEWAY_TRANSPORT_ACCEPT_202 |
        GATEWAY_TRANSPORT_ACCEPT_204);
    if (post_status == ESP_OK) {
        char *remaining = heap_caps_malloc(GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY,
                                           MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        size_t remaining_size = 0;
        device_status_t pop_status = remaining ? gateway_tool_result_outbox_pop(
            payload, size, remaining, GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY, &remaining_size) : DEVICE_STATUS_RESOURCE_EXHAUSTED;
        /* Never erase the durable head when the value-only pop failed.  The
         * POST may have succeeded, but without a durable dequeue result the
         * record remains the only replay evidence; fail closed and let the
         * next poll retry/resolve it rather than turning an internal buffer
         * or validation error into data loss. */
        const device_status_t erase_status = pop_status != DEVICE_STATUS_OK
            ? pop_status
            : (remaining_size
                ? persistence_service_write_blob("gateway", "tool_result_outbox", remaining, remaining_size)
                : persistence_service_erase_key("gateway", "tool_result_outbox"));
        free(remaining);
        if (erase_status != DEVICE_STATUS_OK && erase_status != DEVICE_STATUS_NOT_FOUND) {
            free(payload);
            return device_status_to_platform_error(erase_status);
        }
        if (is_factory_reset_result) {
            factory_reset_service_reboot_if_pending(true);
        }
    }
    if (post_status != ESP_OK) s_delivered_tool_result_id[0] = '\0';
    free(record);
    free(payload);
    return post_status;
}

static const gateway_dispatcher_host_t s_gateway_dispatcher_host = {
    .cancel_poll_http = gateway_host_cancel_poll_http,
    .welcome_gate_active = gateway_host_welcome_gate_active,
    .welcome_classify = gateway_host_welcome_classify,
    .welcome_complete = gateway_host_welcome_complete,
    .handle_tool_call = gateway_host_handle_tool_call,
    .tool_result_outbox_already_delivered = gateway_host_tool_result_outbox_already_delivered,
    .handle_pet_profile = gateway_host_handle_pet_profile,
    .handle_hardware_config = gateway_host_handle_hardware_config,
    .apply_glyphs = gateway_host_apply_glyphs,
    .apply_ambient = gateway_host_apply_ambient,
    .audio_url_allowed = gateway_host_audio_url_allowed,
    .audio_mime_supported = gateway_host_audio_mime_supported,
    .audio_download_error_is_permanent = gateway_host_audio_download_error_is_permanent,
    .audio_presentation_error_is_permanent = gateway_host_audio_presentation_error_is_permanent,
    .begin_server_audio_wake_lease = gateway_host_begin_server_audio_wake_lease,
    .finish_server_audio_wake_lease = gateway_host_finish_server_audio_wake_lease,
    .download_audio = gateway_host_download_audio,
    .release_audio = gateway_host_release_audio,
    .play_audio_payload = gateway_host_play_audio_payload,
    .schedule_wake_restart = gateway_host_schedule_wake_restart,
    .take_startup_pet_retry_due = gateway_host_take_startup_pet_retry_due,
    .apply_deferred_startup_pet_asset = gateway_host_apply_deferred_startup_pet_asset,
    .flush_tool_result_outbox = gateway_host_flush_tool_result_outbox,
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
    return gateway_transport_upload_voice(wav, wav_len, out_media_id, media_id_capacity);
}

static int32_t interaction_host_send_voice_event(const char *media_id,
                                                 const char *event_id,
                                                 char *out_reply_to,
                                                 uint32_t reply_to_capacity) {
    return gateway_transport_send_voice_event(media_id, event_id, out_reply_to,
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

/* Credential Service persists only a monotonic generation floor.  The
 * callbacks deliberately stay in the composition root so the value-only
 * credential contract never learns an NVS namespace, key, or storage worker.
 * Missing floor is a valid first-boot state; every other storage error is
 * propagated and keeps credential lifecycle changes fail-closed. */
#define CREDENTIAL_GENERATION_NAMESPACE "maclaw"
#define CREDENTIAL_GENERATION_FLOOR_KEY "credential_generation_floor"

static device_status_t credential_generation_floor_read(uint64_t *out_floor,
                                                        void *context) {
    (void)context;
    if (!out_floor) return DEVICE_STATUS_INVALID_ARGUMENT;
    int64_t floor = 0;
    const device_status_t status = persistence_service_read_i64(
        CREDENTIAL_GENERATION_NAMESPACE, CREDENTIAL_GENERATION_FLOOR_KEY, &floor);
    if (status == DEVICE_STATUS_NOT_FOUND) {
        *out_floor = 0u;
        return status;
    }
    if (status != DEVICE_STATUS_OK || floor <= 0 || (uint64_t)floor > UINT32_MAX) {
        return status == DEVICE_STATUS_OK ? DEVICE_STATUS_INTERNAL_ERROR : status;
    }
    *out_floor = (uint64_t)floor;
    return DEVICE_STATUS_OK;
}

static device_status_t credential_generation_floor_write(uint64_t floor,
                                                         void *context) {
    (void)context;
    if (floor == 0u || floor > UINT32_MAX) return DEVICE_STATUS_INVALID_ARGUMENT;
    return persistence_service_write_i64(CREDENTIAL_GENERATION_NAMESPACE,
                                         CREDENTIAL_GENERATION_FLOOR_KEY,
                                         (int64_t)floor);
}

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
    if (startup_runtime_state_service_init() != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_RESOURCE_EXHAUSTED,
                               "startup runtime state service");
        return;
    }
    if (wifi_runtime_configuration_service_init() != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_RESOURCE_EXHAUSTED,
                               "Wi-Fi runtime configuration service");
        return;
    }
    uint8_t boot_random[16];
    if (entropy_service_init() != DEVICE_STATUS_OK ||
        !entropy_service_fill(boot_random, sizeof(boot_random))) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR, "entropy readiness");
        return;
    }
    char boot_session_id[STARTUP_RUNTIME_STATE_BOOT_SESSION_ID_CAPACITY] = {0};
    for (size_t i = 0; i < sizeof(boot_random); ++i) {
        snprintf(boot_session_id + i * 2, 3, "%02x", boot_random[i]);
    }
    if (!startup_runtime_state_service_capture_boot_session_id(boot_session_id)) {
        mbedtls_platform_zeroize(boot_random, sizeof(boot_random));
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR,
                               "boot session identity");
        return;
    }
    mbedtls_platform_zeroize(boot_random, sizeof(boot_random));
    if (psa_crypto_init() != PSA_SUCCESS) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_INTERNAL_ERROR, "PSA crypto initialization");
        return;
    }
    device_status_t storage_mount_status = storage_service_init();
    if (storage_mount_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "durable storage unavailable; preserving existing contents: %d",
                 (int)storage_mount_status);
    }
    device_status_t pressure_status = resource_pressure_service_init(
        storage_service_label(), storage_service_is_available());
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
    if (provisioning_qr_service_init(&s_provisioning_qr_service_host) !=
        DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (connectivity_network_lifecycle_service_init(
            &s_connectivity_network_lifecycle_service_host) != DEVICE_STATUS_OK) {
        goto startup_core_no_memory;
    }
    pet_asset_retry_service_init();
    if (startup_pet_asset_state_service_init() != DEVICE_STATUS_OK) {
        goto startup_core_no_memory;
    }
    if (media_transfer_service_init(
            &(media_transfer_service_host_t){
                .struct_size = sizeof(media_transfer_service_host_t),
                .stop_wake_word_for_media = media_transfer_stop_wake_word_for_media,
                .cancel_startup_pet_for_server_audio =
                    media_transfer_cancel_startup_pet_for_server_audio,
                .take_startup_pet_audio_preemption =
                    media_transfer_take_startup_pet_audio_preemption,
                .rearm_preempted_startup_pet =
                    media_transfer_rearm_preempted_startup_pet,
                .schedule_wake_restart = media_transfer_schedule_wake_restart,
                .context = NULL,
            }) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (startup_welcome_service_init(
            &(startup_welcome_service_host_t){
                .struct_size = sizeof(startup_welcome_service_host_t),
                .log_gate_released = startup_welcome_log_gate_released,
                .log_gate_timed_out = startup_welcome_log_gate_timed_out,
                .context = NULL,
            }) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (pet_asset_apply_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
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
    if (wake_restart_worker_service_init(&s_wake_restart_worker_service_host) !=
        DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (provisioning_service_init(&s_provisioning_service_host) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (deferred_setup_worker_service_init(&s_deferred_setup_worker_service_host) !=
        DEVICE_STATUS_OK) goto startup_core_no_memory;
    if (startup_pet_retry_service_init() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    device_status_t persistence_init_status = persistence_service_init();
    if (persistence_init_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               persistence_init_status,
                               "persistence service");
        return;
    }
    /* Once the Persistence worker is live, bind Credential Service to a
     * durable monotonic floor before loading any token-bearing snapshot.  A
     * corrupt/out-of-range floor is a startup integrity failure; do not let
     * Gateway continue with an in-memory generation that could collide with
     * a prior boot after reset. */
    device_status_t credential_floor_status =
        credential_service_set_generation_persistence(
            credential_generation_floor_read,
            credential_generation_floor_write, NULL);
    if (credential_floor_status != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               credential_floor_status,
                               "credential generation floor");
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
    /* Recover a COMMITTED factory-reset handoff immediately after the
     * persistence/configuration owners exist, before loading the paired
     * snapshot or starting any radio/Gateway work.  A pending PREPARED record
     * therefore cannot race network admission, and a committed erase can only
     * return to setup mode through the durable recovery path. */
    if (factory_reset_service_init(&s_factory_reset_service_host) != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_RESOURCE_EXHAUSTED,
                               "factory reset service");
        return;
    }
    if (factory_reset_service_recover() != DEVICE_STATUS_OK) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_BUSY, "factory reset recovery");
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
    if (command_service_start() != DEVICE_STATUS_OK) goto startup_core_no_memory;
    /* Configuration/NVS commits run with flash cache transitions. The shared
     * worker owns the explicitly internal 8 KiB stack, queue/task lifecycle,
     * immutable Storage Registry identity and sleep fence; root injects only
     * the configuration transaction plus runtime projection updates. */
    if (configuration_persistence_worker_service_init(
            &(configuration_persistence_worker_service_host_t){
                .struct_size = sizeof(configuration_persistence_worker_service_host_t),
                .run_transaction = configuration_persistence_run_transaction,
                .context = NULL,
            }) != DEVICE_STATUS_OK) goto startup_core_no_memory;
    device_status_t storage_bridge_status =
        power_service_set_system_sleep_storage_bridge(
            configuration_persistence_prepare_system_sleep,
            configuration_persistence_abort_system_sleep_prepare, NULL);
    if (storage_bridge_status != DEVICE_STATUS_OK) {
        (void)configuration_persistence_worker_service_stop(500);
        goto startup_core_no_memory;
    }
    if (server_audio_presentation_service_init(
            &(server_audio_presentation_service_host_t){
                .struct_size = sizeof(server_audio_presentation_service_host_t),
                .play_mp3 = server_audio_play_mp3,
                .play_wav = server_audio_play_wav,
                .context = NULL,
            }) != DEVICE_STATUS_OK) {
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
    if (device_storage_allows_optional_flash_work()) {
        (void)pet_asset_restore_worker_service_run(
            &s_pet_asset_restore_worker_service_host);
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
    if (battery_policy_service_set_emergency_checkpoint_callback(
            battery_emergency_checkpoint, NULL) != DEVICE_STATUS_OK) {
        (void)app_intent_service_stop(500);
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_CORE_SERVICES_READY,
                               DEVICE_STATUS_BUSY, "battery checkpoint callback");
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
    wifi_runtime_configuration_snapshot_t runtime_wifi = {0};
    const bool wifi_configured =
        wifi_runtime_configuration_service_get_snapshot(&runtime_wifi) &&
        runtime_wifi.ssid[0] != '\0';
    if (!wifi_configured && !device_connectivity_is_active_cellular()) {
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
    if (!startup_runtime_state_service_permit_gateway_startup()) {
        startup_enter_degraded(DEVICE_RUNTIME_PHASE_LOCAL_READY,
                               DEVICE_STATUS_BUSY,
                               "gateway startup admission");
        return;
    }
    if (!network_ready && !device_connectivity_is_active_cellular()) {
        network_ready = device_connectivity_is_active_uplink_ready();
        if (network_ready) {
            ESP_LOGI(TAG, "Wi-Fi recovered at startup boundary; continuing gateway startup");
        }
    }
    if (!network_ready) {
        if (startup_runtime_state_service_staged_provisioning_pending()) {
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
