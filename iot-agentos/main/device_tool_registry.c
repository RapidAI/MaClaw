#include "device_tool_registry.h"

#include <string.h>

#include "alarm_manager.h"
#include "battery_policy_service.h"
#include "fall_detection_service.h"
#include "sleep_schedule_service.h"
#include "update_service.h"

static esp_err_t alarm_execute(const char *name, cJSON *arguments,
                               const char *idempotency_key, cJSON **out_result,
                               char *error, size_t error_size) {
    return alarm_manager_execute_tool(name, arguments, idempotency_key, out_result,
                                      error, error_size);
}

static esp_err_t schedule_execute(const char *name, cJSON *arguments,
                                  const char *idempotency_key, cJSON **out_result,
                                  char *error, size_t error_size) {
    return sleep_schedule_service_execute_tool(name, arguments, idempotency_key,
                                               out_result, error, error_size);
}

static esp_err_t update_execute(const char *name, cJSON *arguments,
                                const char *idempotency_key, cJSON **out_result,
                                char *error, size_t error_size) {
    (void)idempotency_key;
    return update_service_execute_tool(name, arguments, out_result, error, error_size);
}

static esp_err_t fall_detection_execute(const char *name, cJSON *arguments,
                                        const char *idempotency_key, cJSON **out_result,
                                        char *error, size_t error_size) {
    return fall_detection_service_execute_tool(name, arguments, idempotency_key,
                                               out_result, error, error_size);
}

static esp_err_t battery_policy_execute(const char *name, cJSON *arguments,
                                        const char *idempotency_key, cJSON **out_result,
                                        char *error, size_t error_size) {
    (void)arguments;
    (void)idempotency_key;
    if (out_result) *out_result = NULL;
    if (error && error_size) error[0] = '\0';
    if (!name || !out_result || strcmp(name, "battery_policy_status")) {
        return ESP_ERR_NOT_SUPPORTED;
    }

    device_battery_policy_snapshot_t snapshot = {0};
    if (!battery_policy_service_get_snapshot(&snapshot)) {
        if (error && error_size) strlcpy(error, "battery policy service is unavailable", error_size);
        return ESP_ERR_INVALID_STATE;
    }
    cJSON *result = cJSON_CreateObject();
    if (!result ||
        !cJSON_AddBoolToObject(result, "telemetryAvailable", snapshot.telemetry_available) ||
        !cJSON_AddBoolToObject(result, "charging",
                                snapshot.telemetry_available && snapshot.charging) ||
        !cJSON_AddNumberToObject(result, "levelPercent",
                                 snapshot.telemetry_available ? snapshot.level_percent : -1) ||
        !cJSON_AddNumberToObject(result, "policyLevel", snapshot.level) ||
        !cJSON_AddBoolToObject(result, "optionalWorkAllowed",
                                snapshot.optional_work_allowed) ||
        !cJSON_AddBoolToObject(result, "highPowerWorkAllowed",
                                snapshot.high_power_work_allowed)) {
        cJSON_Delete(result);
        return ESP_ERR_NO_MEM;
    }
    *out_result = result;
    return ESP_OK;
}

static bool ready(void) { return true; }
static bool alarm_ready(void) { return alarm_manager_is_initialized(); }
static bool fall_detection_ready(void) { return fall_detection_service_is_initialized(); }

static const device_tool_definition_t s_tools[] = {
    {"alarm_create", true, "{\"name\":\"alarm_create\",\"description\":\"Create one alarm on this device. Resolve relative spoken time to an absolute future epoch in the device timezone before calling.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"triggerAtEpochMs\":{\"type\":\"integer\",\"description\":\"Absolute Unix epoch milliseconds in the future\"},\"label\":{\"type\":\"string\",\"maxLength\":48}},\"required\":[\"triggerAtEpochMs\"]},\"outputSchema\":{\"type\":\"object\"}}", alarm_execute, alarm_ready},
    {"alarm_clear_all", true, "{\"name\":\"alarm_clear_all\",\"description\":\"Clear every alarm on this device.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", alarm_execute, alarm_ready},
    {"alarm_clear", true, "{\"name\":\"alarm_clear\",\"description\":\"Clear one alarm by its current 1-based list index.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"index\":{\"type\":\"integer\",\"minimum\":1}},\"required\":[\"index\"]}}", alarm_execute, alarm_ready},
    {"alarm_list", false, "{\"name\":\"alarm_list\",\"description\":\"List all alarms on this device in chronological order with 1-based indices.\",\"risk\":\"read\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", alarm_execute, alarm_ready},
    {"sleep_schedule_set", true, "{\"name\":\"sleep_schedule_set\",\"description\":\"Set a display-off rest schedule. Only displayOff is available; light and deep sleep are not yet verified.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"mode\":{\"enum\":[\"once\",\"periodic\"]},\"target\":{\"const\":\"displayOff\"},\"timeZone\":{\"const\":\"CST-8\"},\"startAtEpochMs\":{\"type\":\"integer\"},\"endAtEpochMs\":{\"type\":\"integer\"},\"startTime\":{\"type\":\"string\"},\"endTime\":{\"type\":\"string\"},\"weekdayMask\":{\"type\":\"integer\",\"minimum\":1,\"maximum\":127},\"manualWakeOverrideSeconds\":{\"type\":\"integer\",\"minimum\":0,\"maximum\":43200}},\"required\":[\"mode\"]}}", schedule_execute, ready},
    {"sleep_schedule_get", false, "{\"name\":\"sleep_schedule_get\",\"description\":\"Read the current display-off rest schedule and its next transition.\",\"risk\":\"read\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", schedule_execute, ready},
    {"sleep_schedule_disable", true, "{\"name\":\"sleep_schedule_disable\",\"description\":\"Disable the current display-off rest schedule.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", schedule_execute, ready},
    {"fall_detection_status", false, "{\"name\":\"fall_detection_status\",\"description\":\"Read local suspected-device-fall monitoring state. This is a non-medical safety reminder, not a person-fall diagnosis.\",\"risk\":\"read\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", fall_detection_execute, fall_detection_ready},
    {"fall_detection_set", true, "{\"name\":\"fall_detection_set\",\"description\":\"Enable or disable local suspected-device-fall monitoring. Does not configure a medical diagnosis or emergency dispatch.\",\"risk\":\"write\",\"timeoutMs\":5000,\"inputSchema\":{\"type\":\"object\",\"properties\":{\"enabled\":{\"type\":\"boolean\"}},\"required\":[\"enabled\"]}}", fall_detection_execute, fall_detection_ready},
    {"battery_policy_status", false, "{\"name\":\"battery_policy_status\",\"description\":\"Read normalized battery telemetry availability and shared low-power admission policy. Missing telemetry is reported explicitly and is not 0% battery.\",\"risk\":\"read\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", battery_policy_execute, ready},
    {"update_check", false, "{\"name\":\"update_check\",\"description\":\"Read the latest Hub-confirmed firmware update metadata. Updates require a connected computer and ClawMate Maker.\",\"risk\":\"read\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", update_execute, ready},
    {"update_status", false, "{\"name\":\"update_status\",\"description\":\"Read local firmware update reminder status.\",\"risk\":\"read\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", update_execute, ready},
    {"update_remind_later", true, "{\"name\":\"update_remind_later\",\"description\":\"Defer the current firmware update reminder. Does not download or install firmware.\",\"risk\":\"write\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", update_execute, ready},
    {"update_dismiss_version", true, "{\"name\":\"update_dismiss_version\",\"description\":\"Temporarily dismiss the current non-critical firmware update. Does not download or install firmware.\",\"risk\":\"write\",\"timeoutMs\":3000,\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}", update_execute, ready},
};

bool device_tool_registry_find(const char *name, const device_tool_definition_t **out_definition) {
    if (out_definition) *out_definition = NULL;
    if (!name) return false;
    for (size_t i = 0; i < sizeof(s_tools) / sizeof(s_tools[0]); ++i) {
        if (!strcmp(name, s_tools[i].name)) {
            if (out_definition) *out_definition = &s_tools[i];
            return true;
        }
    }
    return false;
}

bool device_tool_registry_requires_idempotency(const device_tool_definition_t *definition) {
    return definition && definition->mutation;
}

bool device_tool_registry_is_ready(const device_tool_definition_t *definition) {
    return definition && (!definition->ready || definition->ready());
}

esp_err_t device_tool_registry_execute(const device_tool_definition_t *definition,
                                       cJSON *arguments, const char *idempotency_key,
                                       cJSON **out_result, char *error, size_t error_size) {
    if (!definition || !definition->execute) return ESP_ERR_NOT_SUPPORTED;
    return definition->execute(definition->name, arguments, idempotency_key,
                               out_result, error, error_size);
}

bool device_tool_registry_append_descriptors(cJSON *tools) {
    if (!cJSON_IsArray(tools)) return false;
    for (size_t i = 0; i < sizeof(s_tools) / sizeof(s_tools[0]); ++i) {
        /* Handshake advertises the stable tool contract.  A service such as
         * Alarm may finish its deferred startup immediately after handshake,
         * so suppressing its descriptor based on a transient task state would
         * make the negotiated catalog flap.  Execution still checks readiness
         * and returns temporarily unavailable until the domain is live. */
        cJSON *descriptor = cJSON_Parse(s_tools[i].descriptor_json);
        if (!descriptor) return false;
        cJSON_AddItemToArray(tools, descriptor);
    }
    return true;
}
