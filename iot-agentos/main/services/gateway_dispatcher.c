#include "services/gateway_dispatcher.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

#include "cJSON.h"
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "mbedtls/base64.h"

#include "presentation/scene_presenter.h"
#include "services/ambient_service.h"
#include "firmware_identity.h"
#include "services/command_service.h"
#include "services/gateway_transport.h"
#include "services/gateway_ack_outbox_policy.h"
#include "services/reply_service.h"
#include "persistence_service.h"
#include "task_registry.h"

/* Keep the log tag identical to the original main.c owner so existing poll /
 * reply trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define HARDWARE_AUDIO_RESPONSE_CAPACITY (512 * 1024 + 1)
#define RESPONSE_IMAGE_MAX_DIMENSION 64
#define RESPONSE_IMAGE_MAX_BYTES (RESPONSE_IMAGE_MAX_DIMENSION * RESPONSE_IMAGE_MAX_DIMENSION * 2)
#define RESPONSE_IMAGE_MIME "application/vnd.maclaw.rgb565be"
#define OUTGOING_RESPONSE_CAPACITY (256 * 1024)
#define ACK_OUTBOX_CAPACITY GATEWAY_ACK_OUTBOX_CAPACITY

static portMUX_TYPE s_dispatcher_lock = portMUX_INITIALIZER_UNLOCKED;

static TaskHandle_t s_gateway_poll_task;
/* The worker must not run before its immutable Registry identity exists.  A
 * persistent gate also lets a bounded stop release a task which was created
 * just as the stop transaction began. */
static SemaphoreHandle_t s_gateway_poll_start_gate;
static SemaphoreHandle_t s_gateway_poll_stopped;
static bool s_gateway_poll_stop_requested;
static bool s_gateway_poll_starting;
/* Lifecycle fence for a future system-sleep transaction. Connectivity owns
 * request admission; this service owns only the long-poll task lifetime. */
static bool s_system_sleep_preparing;
static bool s_system_sleep_restart_poll;
static bool s_system_sleep_restart_pending;
static bool s_gateway_poll_retiring;
static esp_err_t s_gateway_poll_exit_status = ESP_OK;
static bool s_gateway_poll_registry_retirement_failed;
static int64_t s_cursor;
static bool s_cursor_loaded;

static gateway_dispatcher_host_t s_host;
static bool s_host_installed;

static esp_err_t stop_gateway_poll_task(uint32_t timeout_ms);

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

static bool gateway_message_capability_allowed(
    gateway_capability_flags_t required, const char *message_type) {
    if (gateway_transport_capabilities_operational(required)) return true;
    ESP_LOGW(TAG, "discarded %s: Gateway capability is not operational (required=0x%08lx)",
             message_type ? message_type : "message", (unsigned long)required);
    return false;
}

/* A downlink can wait behind an unrelated foreground operation, DMA arbitration
 * or an HTTP request.  The ingress bit check above is therefore not enough:
 * workers which retain a reply across one of those boundaries bind their work
 * to this value-only generation lease and re-check it before every subsequent
 * externally visible action. */
static bool gateway_reply_audio_lease_current(const gateway_capability_lease_t *lease,
                                              const char *boundary) {
    if (gateway_transport_capability_lease_current(lease)) return true;
    ESP_LOGW(TAG, "discarded reply audio at %s: capability lease changed",
             boundary ? boundary : "unknown boundary");
    return false;
}

static gateway_capability_flags_t gateway_hardware_config_required_capabilities(cJSON *extra) {
    if (!cJSON_IsObject(extra)) return 0;
    struct {
        const char *field;
        gateway_capability_flags_t capability;
    } const requirements[] = {
        {"volume", GATEWAY_CAPABILITY_VOLUME_CONTROL},
        {"brightness", GATEWAY_CAPABILITY_BRIGHTNESS_CONTROL},
        {"screenSleepSeconds", GATEWAY_CAPABILITY_SCREEN_SLEEP_CONTROL},
    };
    gateway_capability_flags_t required = 0;
    for (size_t i = 0; i < sizeof(requirements) / sizeof(requirements[0]); ++i) {
        if (cJSON_GetObjectItemCaseSensitive(extra, requirements[i].field)) {
            required |= requirements[i].capability;
        }
    }
    return required;
}

static bool gateway_hardware_config_capabilities_allowed(cJSON *extra) {
    const gateway_capability_flags_t required =
        gateway_hardware_config_required_capabilities(extra);
    return required == 0 || gateway_message_capability_allowed(required, "hardware_config");
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

static bool poll_stop_requested(void) {
    bool requested;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    requested = s_gateway_poll_stop_requested;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    return requested;
}

static device_status_t dispatcher_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

/* Rollback children receive only what remains of the parent's monotonic
 * deadline.  Round up a non-zero remainder so a child can observe and return
 * a real timeout instead of treating a sub-millisecond budget as invalid. */
static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

/* ACKs are idempotent message-level mutations, but losing one between the
 * transport call and a reboot would force the dispatcher to replay the page
 * and repeat its business side effect. Keep one bounded ACK envelope in the
 * Persistence Service until the Gateway confirms it. The poll lane is single
 * reader, so no additional queue/mutex is needed here. */
static device_status_t gateway_dispatcher_persist_ack_outbox(const char *payload) {
    if (!payload || !payload[0]) return DEVICE_STATUS_INVALID_ARGUMENT;
    const size_t bytes = strlen(payload) + 1u;
    if (bytes > ACK_OUTBOX_CAPACITY) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (gateway_ack_outbox_validate_record(payload, bytes, ACK_OUTBOX_CAPACITY) != DEVICE_STATUS_OK) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* ACK outbox is intentionally one slot because the poll lane is one
     * message at a time. Never overwrite an older envelope if a future caller
     * reaches this path concurrently or after an unknown erase outcome: doing
     * so would lose the only durable evidence needed to replay that ACK. The
     * existing record must be flushed (or the caller must remain blocked) first. */
    char existing[ACK_OUTBOX_CAPACITY] = {0};
    size_t existing_size = sizeof(existing);
    const device_status_t existing_status = persistence_service_read_blob(
        "gateway", "ack_outbox", existing, &existing_size);
    if (existing_status == DEVICE_STATUS_OK) {
        return gateway_ack_outbox_validate_record(existing, existing_size,
                                                  sizeof(existing)) == DEVICE_STATUS_OK
                   ? DEVICE_STATUS_BUSY
                   : DEVICE_STATUS_IO_ERROR;
    }
    if (existing_status != DEVICE_STATUS_NOT_FOUND) return existing_status;
    return persistence_service_write_blob("gateway", "ack_outbox", payload, bytes);
}

static esp_err_t gateway_dispatcher_flush_ack_outbox(void) {
    char payload[ACK_OUTBOX_CAPACITY] = {0};
    size_t size = sizeof(payload);
    const device_status_t read_status = persistence_service_read_blob(
        "gateway", "ack_outbox", payload, &size);
    if (read_status == DEVICE_STATUS_NOT_FOUND) return ESP_OK;
    if (read_status != DEVICE_STATUS_OK ||
        gateway_ack_outbox_validate_record(payload, size, sizeof(payload)) != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "discarding malformed ACK outbox record: status=%d size=%u",
                 (int)read_status, (unsigned)size);
        return read_status == DEVICE_STATUS_OK ? ESP_ERR_INVALID_RESPONSE
                                                : device_status_to_platform_error(read_status);
    }
    const esp_err_t ack_err = (esp_err_t)gateway_transport_ack_messages(payload);
    if (ack_err != ESP_OK) return ack_err;
    const device_status_t erase_status = persistence_service_erase_key(
        "gateway", "ack_outbox");
    if (erase_status != DEVICE_STATUS_OK && erase_status != DEVICE_STATUS_NOT_FOUND) {
        ESP_LOGW(TAG, "ACK outbox cleanup failed: status=%d", (int)erase_status);
        return device_status_to_platform_error(erase_status);
    }
    return ESP_OK;
}

bool gateway_dispatcher_current_task_is_poll_worker(void) {
    bool matches;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    matches = s_gateway_poll_task != NULL && s_gateway_poll_task == xTaskGetCurrentTaskHandle();
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    return matches;
}

static void finish_wake_lease_and_restart(void) {
    if (s_host.finish_server_audio_wake_lease && s_host.finish_server_audio_wake_lease() &&
        s_host.schedule_wake_restart) {
        s_host.schedule_wake_restart();
    }
}

static esp_err_t poll_reply(void) {
    // Keep one and only one reader for the outgoing stream. A bounded long
    // poll removes the old TLS reconnect loop while still letting interaction
    // uploads run without waiting behind a 30-second request.
    /* The boot greeting is queued by the handshake and should be consumed
     * immediately.  A long-poll request made after the hardware-config item
     * can otherwise remain stuck behind a flaky keep-alive socket until the
     * Welcome gate expires, even though the greeting is already queued. */
    int poll_timeout_seconds = s_host.welcome_gate_active()
                                   ? 0
                                   : (command_service_display_active() ? 2 : 5);
    /* Drain the previous generation's ACK before asking for another page.
     * Until this succeeds, do not re-run a message whose side effect may
     * already have completed before the earlier transport outage. */
    const esp_err_t outbox_err = gateway_dispatcher_flush_ack_outbox();
    if (outbox_err != ESP_OK) {
        ESP_LOGW(TAG, "pending gateway ACK could not be delivered: %s",
                 esp_err_to_name(outbox_err));
        return outbox_err;
    }
    if (s_host.flush_tool_result_outbox) {
        const esp_err_t tool_outbox_err =
            (esp_err_t)s_host.flush_tool_result_outbox();
        if (tool_outbox_err != ESP_OK) {
            ESP_LOGW(TAG, "pending tool-result outbox could not be delivered: %s",
                     esp_err_to_name(tool_outbox_err));
            return tool_outbox_err;
        }
    }
    int64_t poll_started_us = esp_timer_get_time();
    long long previous_cursor = (long long)s_cursor;
	// A 64x64 RGB565 image expands to about 10.7 KiB in JSON. Fetch one
    // message at a time and retain enough space for queued dynamic glyphs and
    // rich replies. A full glyph preload observed in the field exceeded the
    // old 16 KiB buffer and pinned cursor zero forever, starving later replies.
    bool server_audio_wake_lease_used = false;
    char path[320];
    snprintf(path, sizeof(path),
             "/api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=%d",
             gateway_transport_device_id(), (long long)s_cursor, poll_timeout_seconds);
    gateway_transport_response_t response;
    esp_err_t err = (esp_err_t)gateway_transport_request_with_capacity(
        "GET", path, NULL, NULL, 0, OUTGOING_RESPONSE_CAPACITY, &response);
    if (err != ESP_OK || response.status != 200) {
        esp_err_t result = err == ESP_OK ? ESP_FAIL : err;
        gateway_transport_response_release(&response);
        finish_wake_lease_and_restart();
        return result;
    }
    cJSON *json = cJSON_Parse(response.data);
    if (!json) {
        ESP_LOGW(TAG, "outgoing response is not valid JSON");
        gateway_transport_response_release(&response);
        finish_wake_lease_and_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    const char *next = json_string(json, "nextCursor");
    cJSON *messages = cJSON_GetObjectItemCaseSensitive(json, "messages");
    if (!next || !cJSON_IsArray(messages)) {
        ESP_LOGW(TAG, "outgoing response missing nextCursor/messages");
        cJSON_Delete(json);
        gateway_transport_response_release(&response);
        finish_wake_lease_and_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    errno = 0;
    char *cursor_end = NULL;
    long long parsed_cursor = strtoll(next, &cursor_end, 10);
    if (errno == ERANGE || cursor_end == next || *cursor_end != '\0' || parsed_cursor < 0) {
        ESP_LOGW(TAG, "outgoing response has invalid cursor: %s", next);
        cJSON_Delete(json);
        gateway_transport_response_release(&response);
        finish_wake_lease_and_restart();
        return ESP_ERR_INVALID_RESPONSE;
    }
    /* A successful, authenticated outgoing response is the second
     * control-plane observation that can open the post-handshake capability
     * projection. Do this before dispatching its messages, so no valid reply
     * is dropped merely because the initial handshake provided observation #1.
     * Poll transport/JSON/cursor failures intentionally do not count as global
     * Hub health failures; only the handshake contract currently owns negative
     * health evidence. */
    gateway_transport_observe_capability_control_plane_success();
    cJSON *delivered_ack_ids = cJSON_CreateArray();
    cJSON *failed_ack_ids = cJSON_CreateArray();
    if (!delivered_ack_ids || !failed_ack_ids) {
        cJSON_Delete(delivered_ack_ids);
        cJSON_Delete(failed_ack_ids);
        cJSON_Delete(json);
        gateway_transport_response_release(&response);
        finish_wake_lease_and_restart();
        return ESP_ERR_NO_MEM;
    }
    bool keep_cursor_for_retry = false;
    /* Outgoing pages currently request one message, but retain the lease with
     * its delivered ACK entry explicitly so the final Gateway side effect can
     * be revalidated immediately before the ACK request. */
    gateway_capability_lease_t delivered_audio_ack_lease = {0};
    bool delivered_audio_ack_lease_pending = false;
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
        bool invalid_audio_url = audio_url && !s_host.audio_url_allowed(audio_url);
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
        gateway_capability_lease_t hardware_config_lease = {0};
        bool hardware_config_lease_captured = false;
        const gateway_capability_flags_t hardware_config_required_capabilities =
            hardware_config_message
                ? gateway_hardware_config_required_capabilities(extra)
                : 0;
        bool hardware_config_capability_allowed = !hardware_config_message ||
            gateway_hardware_config_capabilities_allowed(extra);
        if (hardware_config_message && hardware_config_capability_allowed) {
            if (hardware_config_required_capabilities != 0) {
                hardware_config_lease_captured = gateway_transport_capture_capability_lease(
                    hardware_config_required_capabilities, &hardware_config_lease);
                if (!hardware_config_lease_captured) {
                    /* Ingress and the durable/reconcile handoff are separate
                     * scheduling points. A capture failure belongs to the old
                     * generation and must not pin the ordered cursor. */
                    hardware_config_permanently_invalid = true;
                    ESP_LOGW(TAG, "discarded hardware config: capability lease unavailable");
                }
            }
        }
        bool text_capability_allowed = !text_message ||
            gateway_message_capability_allowed(GATEWAY_CAPABILITY_OUTPUT_TEXT, type);
        bool audio_capability_allowed = !audio_message ||
            gateway_message_capability_allowed(GATEWAY_CAPABILITY_OUTPUT_AUDIO, type);
        bool image_capability_allowed = !image_message ||
            gateway_message_capability_allowed(GATEWAY_CAPABILITY_OUTPUT_IMAGE, type);
        bool ambient_update_present = cJSON_GetObjectItemCaseSensitive(item, "glyphs") ||
            cJSON_GetObjectItemCaseSensitive(item, "ambient") ||
            (type && !strcmp(type, "ambient"));
        bool ambient_capability_allowed = !ambient_update_present ||
            gateway_message_capability_allowed(GATEWAY_CAPABILITY_AMBIENT_DISPLAY, type);
        bool ambient_permanently_invalid = ambient_update_present &&
            !ambient_capability_allowed;
        bool welcome_audio = id && (!strncmp(id, "mc_welcome_", 11) || !strncmp(id, "hub_welcome_", 12));
		bool preview_audio = cJSON_IsObject(extra) &&
			cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(extra, "hardware_audio_preview"));
        int32_t welcome_class = s_host.welcome_classify(item, id, welcome_audio, preview_audio);
		bool startup_welcome = welcome_class == GATEWAY_DISPATCHER_WELCOME_CURRENT ||
		                       welcome_class == GATEWAY_DISPATCHER_WELCOME_DISCARD_CURRENT;
		bool discard_startup_welcome =
		    welcome_class == GATEWAY_DISPATCHER_WELCOME_DISCARD_CURRENT ||
		    welcome_class == GATEWAY_DISPATCHER_WELCOME_STALE;
        // Resolve correlation once. The hand-off helper deliberately waits for
        // up to 200 ms while interaction_task publishes its accepted message ID;
        // calling it again for the same item adds avoidable poll latency and can
        // make a multipart spoken reply feel stalled.
        bool cancelled_reply = reply_service_cancelled_matches(reply_to);
        bool active_reply = !cancelled_reply &&
                            reply_service_active_matches_after_handoff(reply_to);
        bool result_speech_reply = !cancelled_reply && audio_message &&
                                   reply_service_result_speech_matches(reply_to);
		if (speech_end_message) {
			reply_service_finish_result_speech(reply_to);
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
        bool audio_can_play = !command_service_display_active() || welcome_audio ||
                              preview_audio || active_reply || result_speech_reply;
        bool audio_handled = discard_startup_welcome ||
                             (audio_message && orphaned_command_result);
        bool audio_permanently_invalid = false;
        /* Capture once for this individual message, after all policy checks
         * which can defer it without doing work.  The lease is deliberately
         * local to Dispatcher: audio/board adapters receive only audio bytes,
         * never gateway-protocol state or a mutable projection. */
        gateway_capability_lease_t audio_lease = {0};
        bool audio_lease_captured = false;
        bool audio_work_admitted = audio_message && audio_capability_allowed &&
                                  !discard_startup_welcome &&
                                  !orphaned_command_result && !cancelled_reply &&
                                  audio_can_play &&
                                  s_host.audio_mime_supported(audio_mime) &&
                                  (audio_data || audio_url);
        if (audio_work_admitted) {
            audio_lease_captured = gateway_transport_capture_capability_lease(
                GATEWAY_CAPABILITY_OUTPUT_AUDIO, &audio_lease);
            if (!audio_lease_captured) {
                /* The operational surface changed after ingress classification.
                 * This message cannot become deliverable by retrying the old
                 * generation, so consume it as a failed acknowledgement rather
                 * than pinning the ordered cursor indefinitely. */
                audio_permanently_invalid = true;
                ESP_LOGW(TAG, "discarded reply audio: capability lease unavailable");
            }
        }
        bool progress = outgoing_message_is_progress(item);
        bool final = outgoing_message_is_final(item);
        const char *skin = json_string(item, "pet_skin");
        cJSON *motion = cJSON_GetObjectItemCaseSensitive(item, "pet_motion_enabled");
        cJSON *metadata = cJSON_GetObjectItemCaseSensitive(item, "metadata");
        const char *turn = cJSON_IsObject(metadata) ? json_string(metadata, "acp_turn") : NULL;
        if (!turn) turn = json_string(item, "acp_turn");
        cJSON *seq_item = cJSON_GetObjectItemCaseSensitive(item, "seq");
        long long message_seq = cJSON_IsNumber(seq_item) ? (long long)seq_item->valuedouble : 0;
        char active_reply_to[REPLY_SERVICE_REPLY_ID_CAPACITY] = {0};
        reply_service_copy_active_reply_to(active_reply_to, sizeof(active_reply_to));
        ESP_LOGI(TAG, "outgoing message: id=%s seq=%lld type=%s replyTo=%s progress=%s final=%s turn=%s text=%u active=%s",
                 id && id[0] ? id : "<none>", message_seq,
                 type && type[0] ? type : "<none>",
                 reply_to && reply_to[0] ? reply_to : "<none>", progress ? "yes" : "no",
                 final ? "yes" : "no", turn && turn[0] ? turn : "<none>",
                 (unsigned)(text ? strlen(text) : 0),
                 active_reply_to[0] ? active_reply_to : "<none>");
        if (!skin && cJSON_IsObject(extra)) skin = json_string(extra, "pet_skin");
		if (tool_message) {
			if (s_host.tool_result_outbox_already_delivered &&
				s_host.tool_result_outbox_already_delivered(item)) {
				tool_handled = true;
				ESP_LOGI(TAG, "skipping already-delivered client tool result");
			} else {
				esp_err_t tool_err = (esp_err_t)s_host.handle_tool_call(item);
				tool_handled = tool_err == ESP_OK;
				if (!tool_handled) {
					ESP_LOGW(TAG, "client tool execution/result delivery failed: %s", esp_err_to_name(tool_err));
					keep_cursor_for_retry = true;
				}
			}
		}
        bool pet_profile_message = type && !strcmp(type, "pet_profile");
        bool pet_profile_handled = !pet_profile_message;
        bool pet_profile_permanently_invalid = false;
        bool pet_profile_capability_allowed = !pet_profile_message ||
            gateway_message_capability_allowed(GATEWAY_CAPABILITY_PET_ASSET, type);
        if (!skin && cJSON_IsObject(metadata)) skin = json_string(metadata, "pet_skin");
        if (skin && gateway_message_capability_allowed(GATEWAY_CAPABILITY_PET_STATE, type)) {
            ambient_service_apply_pet_profile(skin, !motion || cJSON_IsTrue(motion));
        }
        if (pet_profile_message && !pet_profile_capability_allowed) {
            pet_profile_permanently_invalid = true;
        } else if (pet_profile_message) {
            s_host.handle_pet_profile(item, id, &pet_profile_handled,
                                      &pet_profile_permanently_invalid);
        }
        if (ambient_capability_allowed) {
            s_host.apply_glyphs(cJSON_GetObjectItemCaseSensitive(item, "glyphs"));
            s_host.apply_ambient(cJSON_GetObjectItemCaseSensitive(item, "ambient"));
            if (type && !strcmp(type, "ambient")) s_host.apply_ambient(item);
        }
        if (hardware_config_message &&
            (!hardware_config_capability_allowed ||
             (hardware_config_required_capabilities != 0 &&
              !hardware_config_lease_captured))) {
            hardware_config_permanently_invalid = true;
        } else if (hardware_config_message) {
            if (!hardware_config_lease_captured) {
                /* An empty/malformed object has no exact surface to lease;
                 * preserve existing composition-root protocol validation. */
                s_host.handle_hardware_config(extra, NULL, &hardware_config_handled,
                                              &hardware_config_permanently_invalid);
            } else if (!gateway_transport_capability_lease_current(
                           &hardware_config_lease)) {
                hardware_config_permanently_invalid = true;
                ESP_LOGW(TAG, "discarded hardware config before reconcile: capability lease changed");
            } else {
                s_host.handle_hardware_config(extra, &hardware_config_lease,
                                              &hardware_config_handled,
                                              &hardware_config_permanently_invalid);
            }
        }
        if (type && !strcmp(type, "pet_state")) {
            const char *state = cJSON_IsObject(extra) ? json_string(extra, "state") : NULL;
            if (!state) state = json_string(item, "state");
            // An unsolicited idle/quiet state must never interrupt the
            // foreground thinking -> result transition.
            if (state && !command_service_display_active() &&
                gateway_message_capability_allowed(GATEWAY_CAPABILITY_PET_STATE, type)) {
                ambient_service_apply_pet_state(state);
            }
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
            ambient_service_apply_pet_state("done");
            scene_presenter_publish_response("会议处理完成", message);
        }
		if (image_message && !image_capability_allowed) {
			image_permanently_invalid = true;
		} else if (image_message && !orphaned_command_result) {
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
						uintptr_t waiter = reply_service_begin_active_reply();
						if (waiter) {
							// A terminal image can be followed by the same correlated
							// multipart TTS stream as a text result.  Arm that hand-off
							// before waking the interaction task: completing the image
							// clears the active reply correlation, and without this
							// transaction a later audio part would be classified as an
							// orphan and acknowledged silently.
							unsigned pending_speech_parts = outgoing_pending_speech_parts(item);
							if (pending_speech_parts > 0) {
								reply_service_arm_result_speech(reply_to, pending_speech_parts);
							}
							reply_service_complete_active_image_reply(waiter, "码卡龙", caption,
									(const uint16_t *)pixels, (size_t)image_width,
									(size_t)image_height);
							if (reply_service_correlation_matches(reply_to)) command_service_log_timing("image-result");
							image_handled = true;
						}
					} else if (!command_service_display_active()) {
						scene_presenter_publish_response_image("码卡龙", caption, (const uint16_t *)pixels,
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
		if (text_message && !text_capability_allowed) {
			text_permanently_invalid = true;
		} else if (text_message && text && text[0] && !orphaned_command_result) {
			if (cancelled_reply) {
				ESP_LOGI(TAG, "ignored late reply for cancelled command: %s", reply_to);
				text_handled = true;
            } else if (progress && !final && active_reply) {
                // Progress refreshes the thinking state but is not the answer.
                // A few Hub paths retain progress=true on the terminal envelope;
                // final must win so a completed answer cannot remain hidden
                // behind the remote-processing surface.
                if (reply_service_correlation_matches(reply_to) &&
                    command_service_timing_mark_first_progress()) {
                    ESP_LOGI(TAG, "command first progress: replyTo=%s afterAccepted=%ums",
                             reply_to,
                             (unsigned)command_service_timing_accepted_to_first_progress_ms());
                }
                ESP_LOGI(TAG, "remote progress received: replyTo=%s", reply_to);
                text_handled = true;
			} else if (active_reply) {
                // Once a reply is present the thinking phase has ended; a
                // double tap arriving while this frame is drawn must not turn
                // an already completed command into a cancellation.
                uintptr_t waiter = reply_service_begin_active_reply();
                if (!waiter) {
                    ESP_LOGI(TAG, "reply arrived while cancellation owns command: %s", reply_to);
                } else {
                    // Arm the exact post-terminal speech transaction before
                    // waking the command worker; it may clear active replyTo
                    // immediately after the result frame is published.
                    unsigned pending_speech_parts = outgoing_pending_speech_parts(item);
                    if (pending_speech_parts > 0) {
                        reply_service_arm_result_speech(reply_to, pending_speech_parts);
                    }
                    // Keep the final response surface continuous with the
                    // thinking surface. Do not briefly switch to idle here.
                    reply_service_complete_active_text_reply(waiter, "码卡龙", text);
                    if (reply_service_correlation_matches(reply_to)) command_service_log_timing("text-result");
                    text_handled = true;
                }
			} else {
                // The outgoing stream can contain unrelated notifications or
                // late replies from before this boot. They may still be shown
                // when the device is idle, but must never complete or replace
                // an active command unless replyTo identifies that command.
				if (!command_service_display_active()) {
					scene_presenter_publish_response("码卡龙", text);
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
                uintptr_t waiter = reply_service_begin_active_reply();
                if (waiter) {
                    const char *detail = text && text[0] ? text : "远端返回错误，但没有详细说明";
                    ambient_service_apply_pet_state("alert");
                    reply_service_complete_active_text_reply(waiter, "远端处理失败", detail);
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
            uintptr_t waiter = reply_service_begin_active_reply();
            if (waiter) {
                reply_service_complete_active_text_reply(
                    waiter, "任务已完成",
                    text && text[0] ? text : "远端已完成，但没有可显示的文字结果");
            }
        }
        if (audio_message && audio_capability_allowed && audio_data && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            s_host.audio_mime_supported(audio_mime) && audio_lease_captured) {
            // Inline server speech does not open a second media HTTP request,
            // but it still competes with resident MultiNet for the DMA/codec
            // path and must obey the same foreground wake lifecycle as URL
            // speech. Keep the lease through the ACK below so no later poll
            // can recreate the recognizer before this reply is committed.
            server_audio_wake_lease_used =
                s_host.begin_server_audio_wake_lease("inline server audio") ||
                server_audio_wake_lease_used;
            if (!gateway_reply_audio_lease_current(&audio_lease, "before inline decode")) {
                audio_permanently_invalid = true;
            } else {
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
                        if (!gateway_reply_audio_lease_current(&audio_lease, "before inline playback")) {
                            audio_permanently_invalid = true;
                        } else {
                            ESP_LOGI(TAG, "playing server audio: %u bytes mime=%s",
                                     (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                            esp_err_t play_err = (esp_err_t)s_host.play_audio_payload(
                                audio_mime, audio, (uint32_t)audio_len);
                            if (play_err != ESP_OK) {
                                ESP_LOGW(TAG, "server speech playback failed: %s",
                                         esp_err_to_name(play_err));
                            }
                            audio_handled = play_err == ESP_OK;
                            audio_permanently_invalid =
                                s_host.audio_presentation_error_is_permanent((int32_t)play_err);
                        }
                    } else if (audio) {
                        ESP_LOGW(TAG, "invalid server speech payload");
                        audio_permanently_invalid = true;
                    } else {
                        ESP_LOGW(TAG, "server speech allocation failed: %u bytes",
                                 (unsigned)audio_capacity);
                    }
                    s_host.release_audio(audio);
                } else {
                    ESP_LOGW(TAG, "ignored server audio payload: base64=%d size=%u",
                             decode_status, (unsigned)audio_capacity);
                }
            }
        }
        if (audio_message && audio_capability_allowed && !audio_data && audio_url && !discard_startup_welcome &&
            !orphaned_command_result &&
            !cancelled_reply && audio_can_play &&
            s_host.audio_mime_supported(audio_mime) && audio_lease_captured) {
            uint8_t *audio = NULL;
            uint32_t audio_len = 0;
            if (!gateway_reply_audio_lease_current(&audio_lease, "before audio download")) {
                audio_permanently_invalid = true;
            } else {
                esp_err_t fetch_err = (esp_err_t)s_host.download_audio(audio_url, &audio, &audio_len);
                server_audio_wake_lease_used = true;
                if (fetch_err == ESP_OK) {
                    /* The HTTP request may have been in flight when a later
                     * handshake revoked this generation. Drop its buffer; do
                     * not submit stale content to the board audio adapter. */
                    if (!gateway_reply_audio_lease_current(&audio_lease,
                                                           "after audio download")) {
                        audio_permanently_invalid = true;
                    } else {
                        ESP_LOGI(TAG, "playing downloaded server audio: %u bytes mime=%s",
                                 (unsigned)audio_len, audio_mime ? audio_mime : "auto");
                        esp_err_t play_err = (esp_err_t)s_host.play_audio_payload(
                            audio_mime, audio, (uint32_t)audio_len);
                        if (play_err != ESP_OK) {
                            ESP_LOGW(TAG, "downloaded server speech playback failed: %s",
                                     esp_err_to_name(play_err));
                        }
                        audio_handled = play_err == ESP_OK;
                        audio_permanently_invalid =
                                s_host.audio_presentation_error_is_permanent((int32_t)play_err);
                    }
                } else {
                    ESP_LOGW(TAG, "server speech download failed: %s", esp_err_to_name(fetch_err));
                    audio_permanently_invalid =
                        s_host.audio_download_error_is_permanent((int32_t)fetch_err);
                }
            }
            s_host.release_audio(audio);
        }
        // Do not acknowledge an audio message that we could neither fetch nor
        // play. Keeping it pending lets a transient network/I2S failure retry
        // on the next poll instead of silently losing the welcome sound. Late
        // cancelled audio is intentionally discarded so it cannot retry forever.
        // Permanent protocol/content errors must not pin the page cursor and
        // create a hot retry loop. Transient states (busy audio bus, download,
        // allocation or I2S failure) remain pending and retry on the next poll.
        audio_permanently_invalid = audio_permanently_invalid || (audio_message &&
            !audio_capability_allowed) || (audio_message &&
            (invalid_audio_url || !s_host.audio_mime_supported(audio_mime) ||
             (!audio_data && !audio_url)));
        /* A delivered ACK is itself the final observable Gateway action for a
         * reply.  If the generation changed while I2S was consuming the bytes,
         * do not report ordinary delivery for the now-stale authorization. */
        if (audio_message && audio_handled && audio_lease_captured &&
            !gateway_reply_audio_lease_current(&audio_lease, "before audio acknowledgement")) {
            audio_handled = false;
            audio_permanently_invalid = true;
        }
        if (result_speech_reply && (audio_handled || audio_permanently_invalid)) {
            reply_service_finish_result_speech_part(reply_to);
        }
        if (startup_welcome && !discard_startup_welcome &&
            (audio_handled || audio_permanently_invalid)) {
            s_host.welcome_complete(audio_handled);
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
				ambient_permanently_invalid ||
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
                gateway_transport_response_release(&response);
                finish_wake_lease_and_restart();
                return ESP_ERR_NO_MEM;
            }
            if (audio_message && audio_handled && audio_lease_captured) {
                delivered_audio_ack_lease = audio_lease;
                delivered_audio_ack_lease_pending = true;
            }
        }
    }
    cJSON *ack_groups[2] = {delivered_ack_ids, failed_ack_ids};
    const char *ack_statuses[2] = {"delivered", "failed"};
    for (size_t ack_index = 0; ack_index < 2; ++ack_index) {
        cJSON *ack_ids = ack_groups[ack_index];
        if (cJSON_GetArraySize(ack_ids) == 0) continue;
        const char *ack_status = ack_statuses[ack_index];
        if (ack_index == 0 && delivered_audio_ack_lease_pending &&
            !gateway_reply_audio_lease_current(&delivered_audio_ack_lease,
                                                "before audio acknowledgement commit")) {
            /* The poll requests limit=1, so this delivered group is the exact
             * reply associated with the retained lease.  Report the stale
             * generation as failed rather than falsely committing delivery. */
            ack_status = "failed";
            delivered_audio_ack_lease_pending = false;
        }
        cJSON *ack = cJSON_CreateObject();
        if (!ack) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            gateway_transport_response_release(&response);
            finish_wake_lease_and_restart();
            return ESP_ERR_NO_MEM;
        }
        cJSON_AddStringToObject(ack, "clientId", gateway_transport_device_id());
        cJSON_AddItemReferenceToObject(ack, "messageIds", ack_ids);
        cJSON_AddStringToObject(ack, "status", ack_status);
        char *payload = cJSON_PrintUnformatted(ack);
        cJSON_Delete(ack);
        if (!payload) {
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            gateway_transport_response_release(&response);
            finish_wake_lease_and_restart();
            return ESP_ERR_NO_MEM;
        }
        esp_err_t ack_err = (esp_err_t)gateway_transport_ack_messages(payload);
        if (ack_err != ESP_OK) {
            ESP_LOGW(TAG, "gateway ack failed: err=%s", esp_err_to_name(ack_err));
            const device_status_t outbox_status =
                gateway_dispatcher_persist_ack_outbox(payload);
            if (outbox_status != DEVICE_STATUS_OK) {
                ESP_LOGE(TAG, "cannot persist failed ACK for retry: status=%d",
                         (int)outbox_status);
            }
            free(payload);
            esp_err_t result = ack_err;
            cJSON_Delete(delivered_ack_ids);
            cJSON_Delete(failed_ack_ids);
            cJSON_Delete(json);
            gateway_transport_response_release(&response);
            finish_wake_lease_and_restart();
            return result;
        }
        free(payload);
    }
    cJSON_Delete(delivered_ack_ids);
    cJSON_Delete(failed_ack_ids);
    // Cursor is page-level while acknowledgements are message-level. If one
    // audio item was intentionally left unacknowledged, advancing the cursor
    // would hide it from the next poll despite the missing ACK.
    if (!keep_cursor_for_retry && parsed_cursor != (long long)s_cursor) {
        /* Cursor advancement is a durable checkpoint.  If flash is busy or
         * unavailable, keep the old cursor so the page is replayed rather
         * than silently skipping messages after a reboot. */
        const device_status_t persist_status = persistence_service_write_i64(
            "gateway", "out_cursor", (int64_t)parsed_cursor);
        if (persist_status != DEVICE_STATUS_OK) {
            ESP_LOGW(TAG, "cursor checkpoint failed: status=%d", (int)persist_status);
            cJSON_Delete(json);
            gateway_transport_response_release(&response);
            if (server_audio_wake_lease_used) finish_wake_lease_and_restart();
            return device_status_to_platform_error(persist_status);
        }
        s_cursor = (int64_t)parsed_cursor;
    }
    cJSON_Delete(json);
    gateway_transport_response_release(&response);
    if (server_audio_wake_lease_used) {
        // The acknowledgement also uses TLS.  Restart only once this ordered
        // server-audio transaction has released every AES/TLS allocation.
        finish_wake_lease_and_restart();
    }
    return ESP_OK;
}
static void gateway_poll_task(void *arg) {
    (void)arg;
    if (!s_gateway_poll_start_gate ||
        xSemaphoreTake(s_gateway_poll_start_gate, portMAX_DELAY) != pdTRUE) {
        ESP_LOGW(TAG, "gateway poll start gate unavailable");
        goto finish;
    }
    unsigned consecutive_failures = 0;
    while (true) {
        bool stop_requested = poll_stop_requested();
        bool retry_pet = s_host.take_startup_pet_retry_due();
        if (stop_requested) break;
        if (gateway_transport_is_paired()) {
            int64_t started_us = esp_timer_get_time();
            esp_err_t err = poll_reply();
            int64_t elapsed_ms = (esp_timer_get_time() - started_us) / 1000;
            if (err != ESP_OK) {
                if (++consecutive_failures >= 2) {
                    scene_presenter_publish_service_ready(false);
                    firmware_identity_set_service_ready(false);
                }
                if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000)) != 0) break;
            } else {
                consecutive_failures = 0;
                scene_presenter_publish_service_ready(true);
                firmware_identity_set_service_ready(true);
                if (elapsed_ms >= 4000) continue;
                // Legacy Hub versions return an empty poll immediately. Avoid
                // a tight TLS reconnect loop until that Hub is upgraded to
                // the v1.1 long-poll implementation.
                // During a foreground command, avoid repeated two-second
                // blind spots while still preventing a hot reconnect loop.
                if (ulTaskNotifyTake(pdTRUE,
                                     pdMS_TO_TICKS(command_service_display_active() ? 80 : 2000)) != 0) {
                    break;
                }
            }
        } else {
            consecutive_failures = 0;
            scene_presenter_publish_service_ready(false);
            firmware_identity_set_service_ready(false);
            if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(3000)) != 0) break;
        }
        if (retry_pet && !poll_stop_requested()) {
            /* The retry timer only admits work.  Run the actual resource
             * check/worker creation in this normal task, never in esp_timer.
             * Do it after the poll's HTTP response has released its TLS heap. */
            s_host.apply_deferred_startup_pet_asset();
        }
    }
finish: {
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    s_gateway_poll_retiring = true;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    /* Do not block the PSRAM worker indefinitely behind a lifecycle Registry
     * stop. A missed short bookkeeping pass deliberately retains its immutable
     * entry for a later fail-closed owner transaction. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_CONNECTIVITY, (void *)self, 10);
    taskENTER_CRITICAL(&s_dispatcher_lock);
    s_gateway_poll_exit_status = registry_err;
    if (s_gateway_poll_task == self) s_gateway_poll_task = NULL;
    s_gateway_poll_starting = false;
    s_gateway_poll_retiring = false;
    if (registry_err != ESP_OK) {
        s_gateway_poll_stop_requested = true;
        s_gateway_poll_registry_retirement_failed = true;
    }
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        registry_err == ESP_OK && !s_gateway_poll_registry_retirement_failed) {
        s_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    }
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (s_gateway_poll_stopped) xSemaphoreGive(s_gateway_poll_stopped);
    if (restart_after_system_sleep_abort && !gateway_dispatcher_ensure_poll_task()) {
        ESP_LOGE(TAG, "cannot defer-restart gateway poll after system-sleep abort");
    }
    vTaskDeleteWithCaps(NULL);
}
}

static esp_err_t stop_gateway_poll_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    task = s_gateway_poll_task;
    if (task) s_gateway_poll_stop_requested = true;
    const esp_err_t exit_status = s_gateway_poll_exit_status;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (!task) return exit_status;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    /* The poll uses this lane's persistent ESP client. Cancel the active
     * request before waiting, so a 30s network timeout cannot delay rollback.
     * Cellular polling remains bounded by its adapter request timeout; do not
     * touch its private handle from here. */
    if (s_host.cancel_poll_http) s_host.cancel_poll_http(deadline_us);
    xTaskNotifyGive(task);
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!s_gateway_poll_stopped ||
        remaining_ms == 0 ||
        xSemaphoreTake(s_gateway_poll_stopped, pdMS_TO_TICKS(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_dispatcher_lock);
    const esp_err_t completed_status = s_gateway_poll_exit_status;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (completed_status != ESP_OK) return completed_status;
    ESP_LOGI(TAG, "gateway poll task stopped");
    return ESP_OK;
}

static esp_err_t stop_gateway_poll_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    task = s_gateway_poll_task;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return stop_gateway_poll_task(timeout_ms);
}

bool gateway_dispatcher_ensure_poll_task(void) {
    if (!s_cursor_loaded) {
        int64_t stored_cursor = 0;
        const device_status_t status = persistence_service_read_i64(
            "gateway", "out_cursor", &stored_cursor);
        if (status == DEVICE_STATUS_OK && stored_cursor >= 0) {
            s_cursor = stored_cursor;
            s_cursor_loaded = true;
        } else if (status == DEVICE_STATUS_NOT_FOUND) {
            s_cursor = 0;
            s_cursor_loaded = true;
        } else if (status != DEVICE_STATUS_BUSY) {
            ESP_LOGW(TAG, "cursor checkpoint unavailable: status=%d; starting at zero",
                     (int)status);
            s_cursor = 0;
            s_cursor_loaded = true;
        }
    }
    taskENTER_CRITICAL(&s_dispatcher_lock);
    if (!s_host_installed || s_system_sleep_preparing || s_system_sleep_restart_pending ||
        s_gateway_poll_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return false;
    }
    bool already_started = s_gateway_poll_task != NULL || s_gateway_poll_retiring ||
                           s_gateway_poll_starting;
    if (!already_started) s_gateway_poll_starting = true;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (!already_started) {
        // MP3 is decoded synchronously when an outgoing audio message arrives.
        // The official decoder needs substantially more stack than JSON/TLS
        // polling alone, especially for stereo Layer III frames.  EchoEar's
        // wake model leaves less than a 16 KiB contiguous internal block at
        // this point, so an internal-stack task fails to start and prevents
        // the final ready/standby pet transition.  This worker only performs
        // HTTP/JSON/MP3 work; keep its large stack in PSRAM, like the clock
        // and recovery workers, to preserve internal RAM for Wi-Fi and I2S.
        /* Completion survives normal task generations.  Replacing it after a
         * natural exit would leak the old semaphore and, worse, let a late
         * stop observer confuse the new generation's completion token. */
        taskENTER_CRITICAL(&s_dispatcher_lock);
        SemaphoreHandle_t stopped = s_gateway_poll_stopped;
        SemaphoreHandle_t start_gate = s_gateway_poll_start_gate;
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        SemaphoreHandle_t new_stopped = stopped ? NULL : xSemaphoreCreateBinary();
        SemaphoreHandle_t new_start_gate = start_gate ? NULL : xSemaphoreCreateBinary();
        if ((!stopped && !new_stopped) || (!start_gate && !new_start_gate)) {
            if (new_stopped) vSemaphoreDelete(new_stopped);
            if (new_start_gate) vSemaphoreDelete(new_start_gate);
            taskENTER_CRITICAL(&s_dispatcher_lock);
            s_gateway_poll_starting = false;
            taskEXIT_CRITICAL(&s_dispatcher_lock);
            ESP_LOGE(TAG, "cannot allocate gateway poll lifecycle semaphores");
            return false;
        }
        taskENTER_CRITICAL(&s_dispatcher_lock);
        if (!s_gateway_poll_stopped) s_gateway_poll_stopped = new_stopped;
        if (!s_gateway_poll_start_gate) s_gateway_poll_start_gate = new_start_gate;
        stopped = s_gateway_poll_stopped;
        start_gate = s_gateway_poll_start_gate;
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        /* This caller owns s_gateway_poll_starting, so no competing creator
         * can publish one of these semaphores. */
        if (new_stopped && new_stopped != stopped) vSemaphoreDelete(new_stopped);
        if (new_start_gate && new_start_gate != start_gate) vSemaphoreDelete(new_start_gate);
        while (xSemaphoreTake(stopped, 0) == pdTRUE) {}
        while (xSemaphoreTake(start_gate, 0) == pdTRUE) {}
        taskENTER_CRITICAL(&s_dispatcher_lock);
        const bool admitted = !s_system_sleep_preparing &&
                              !s_system_sleep_restart_pending &&
                              !s_gateway_poll_registry_retirement_failed &&
                              s_gateway_poll_starting && s_gateway_poll_task == NULL;
        if (!admitted) {
            s_gateway_poll_starting = false;
            taskEXIT_CRITICAL(&s_dispatcher_lock);
            return false;
        }
        s_gateway_poll_stop_requested = false;
        s_gateway_poll_exit_status = ESP_OK;
        s_gateway_poll_registry_retirement_failed = false;
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        TaskHandle_t task = NULL;
        BaseType_t created = xTaskCreateWithCaps(gateway_poll_task,
                                                 "maclaw_gateway_poll", 16384,
                                                 NULL, 3, &task,
                                                 MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (created != pdPASS) {
            taskENTER_CRITICAL(&s_dispatcher_lock);
            s_gateway_poll_starting = false;
            taskEXIT_CRITICAL(&s_dispatcher_lock);
            ESP_LOGE(TAG, "cannot start gateway poll task");
            return false;
        }
        taskENTER_CRITICAL(&s_dispatcher_lock);
        s_gateway_poll_task = task;
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
            .struct_size = sizeof(task_registry_entry_t),
            .owner = TASK_REGISTRY_OWNER_CONNECTIVITY,
            .name = "gateway_poll",
            .context = (void *)task,
            .stop = stop_gateway_poll_registry_entry,
        });
        if (registry_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot register gateway poll lifecycle owner: %s",
                      esp_err_to_name(registry_err));
            taskENTER_CRITICAL(&s_dispatcher_lock);
            s_gateway_poll_stop_requested = true;
            taskEXIT_CRITICAL(&s_dispatcher_lock);
            xSemaphoreGive(start_gate);
            (void)stop_gateway_poll_task(500);
            return false;
        }
        xSemaphoreGive(start_gate);
    }
    return true;
}

device_status_t gateway_dispatcher_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    bool restart_poll = false;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return DEVICE_STATUS_BUSY;
    }
    if (s_gateway_poll_starting || s_gateway_poll_retiring ||
        s_gateway_poll_registry_retirement_failed) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    restart_poll = s_gateway_poll_task != NULL;
    s_system_sleep_restart_poll = restart_poll;
    taskEXIT_CRITICAL(&s_dispatcher_lock);

    if (!restart_poll) return DEVICE_STATUS_OK;
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (remaining_ms == 0) return DEVICE_STATUS_TIMEOUT;
    const device_status_t status =
        dispatcher_status_from_esp_err(stop_gateway_poll_task(remaining_ms));
    /* A child may report success exactly as the parent budget expires.  Keep
     * the PREPARE fence closed and fail closed rather than allowing a late
     * success to advance the sleep transaction. */
    if (status == DEVICE_STATUS_OK && esp_timer_get_time() >= deadline_us) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return status;
}

void gateway_dispatcher_abort_system_sleep_prepare(void) {
    bool restart_poll = false;
    taskENTER_CRITICAL(&s_dispatcher_lock);
    restart_poll = s_system_sleep_restart_poll;
    s_system_sleep_restart_poll = false;
    /* This does not reopen Connectivity request admission; Power restores
     * that fence after every worker has been recreated. */
    s_system_sleep_preparing = false;
    if (restart_poll && (s_gateway_poll_starting || s_gateway_poll_retiring ||
                         s_gateway_poll_registry_retirement_failed)) {
        s_system_sleep_restart_pending = true;
        restart_poll = false;
    }
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    if (restart_poll && !gateway_dispatcher_ensure_poll_task()) {
        ESP_LOGE(TAG, "cannot restore gateway poll worker after system-sleep abort");
    }
}

device_status_t gateway_dispatcher_commit_prepared_network_restart(void) {
    taskENTER_CRITICAL(&s_dispatcher_lock);
    if (!s_host_installed) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (!s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_dispatcher_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_restart_poll = false;
    s_system_sleep_restart_pending = false;
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_dispatcher_lock);
    return DEVICE_STATUS_OK;
}

device_status_t gateway_dispatcher_init(const gateway_dispatcher_host_t *host) {
    if (!host || !host->cancel_poll_http || !host->welcome_gate_active ||
        !host->welcome_classify || !host->welcome_complete ||
        !host->handle_tool_call || !host->handle_pet_profile ||
        !host->handle_hardware_config || !host->apply_glyphs || !host->apply_ambient ||
        !host->audio_url_allowed || !host->audio_mime_supported ||
        !host->audio_download_error_is_permanent ||
        !host->audio_presentation_error_is_permanent ||
        !host->begin_server_audio_wake_lease ||
        !host->finish_server_audio_wake_lease || !host->download_audio ||
        !host->release_audio ||
        !host->play_audio_payload || !host->schedule_wake_restart ||
        !host->take_startup_pet_retry_due || !host->apply_deferred_startup_pet_asset) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    s_host = *host;
    s_host_installed = true;
    return DEVICE_STATUS_OK;
}
