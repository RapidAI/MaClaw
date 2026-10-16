#include "services/gateway_tool_result_service.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"

#include "device_tool_registry.h"
#include "persistence_service.h"
#include "services/factory_reset_service.h"
#include "services/gateway_tool_result_outbox_policy.h"
#include "services/gateway_transport.h"

/* Keep the log tag identical to the original main.c owner so existing
 * tool-result trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define GATEWAY_TOOL_RESULT_DELIVERED_ID_CAPACITY 128u

/* One-shot record of the result id replayed by the most recent outbox POST.
 * The Dispatcher consumes it to skip re-executing an already delivered
 * toolCall; a failed replay POST must clear it. */
static char s_delivered_tool_result_id[GATEWAY_TOOL_RESULT_DELIVERED_ID_CAPACITY];

static const char *json_string(cJSON *root, const char *key) {
    cJSON *node = cJSON_GetObjectItemCaseSensitive(root, key);
    return cJSON_IsString(node) && node->valuestring ? node->valuestring : NULL;
}

void gateway_tool_result_service_init(void) {
    s_delivered_tool_result_id[0] = '\0';
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

int32_t gateway_tool_result_service_handle_tool_call(const void *message_item) {
    return (int32_t)handle_client_tool_call((cJSON *)message_item);
}

bool gateway_tool_result_service_outbox_already_delivered(const void *message_item) {
    if (!message_item || !s_delivered_tool_result_id[0]) return false;
    cJSON *item = (cJSON *)message_item;
    cJSON *call = cJSON_GetObjectItemCaseSensitive(item, "toolCallId");
    if (!cJSON_IsString(call) || !call->valuestring) call = cJSON_GetObjectItemCaseSensitive(item, "id");
    if (!cJSON_IsString(call) || !call->valuestring || strcmp(call->valuestring, s_delivered_tool_result_id) != 0) return false;
    s_delivered_tool_result_id[0] = '\0';
    return true;
}

int32_t gateway_tool_result_service_flush_outbox(void) {
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
