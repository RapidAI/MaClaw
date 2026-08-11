#include "firmware_identity.h"

#include <ctype.h>
#include <stdio.h>
#include <string.h>

#include "cJSON.h"
#include "esp_app_desc.h"
#include "esp_chip_info.h"
#include "esp_timer.h"
#include "esp_flash.h"
#include "esp_psram.h"
#include "hal/usb_serial_jtag_ll.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "app_intent_service.h"
#include "fall_detection_service.h"
#include "operation_context.h"
#include "sleep_schedule_service.h"
#include "task_registry.h"

#define IDENTITY_PROTOCOL_VERSION 2
#define IDENTITY_QUERY_PREFIX "CLAWMATE_QUERY "
#define IDENTITY_EVENT_PREFIX "CLAWMATE_EVT "
#define IDENTITY_LINE_CAPACITY 512
#define IDENTITY_NONCE_MAX_LENGTH 64

static volatile bool s_local_ready;
static volatile bool s_service_ready;
// This diagnostic service is lifecycle-owned even though the current boot
// path keeps it alive for the lifetime of the application.  Future Power and
// Manufacturing coordinators must be able to quiesce it before USB is
// reconfigured; never leave a hidden permanent reader task behind.
static volatile bool s_query_task_stop_requested;
static TaskHandle_t s_query_task;
/* Startup registers the diagnostic owner before the reader can execute.  The
 * start gate closes the short xTaskCreate()/registry window, and the completion
 * semaphore gives stop() a real cooperative-join acknowledgement instead of
 * inferring task death from a periodically-polled handle. */
static SemaphoreHandle_t s_query_task_start_gate;
static SemaphoreHandle_t s_query_task_stopped;
static volatile bool s_query_task_starting;

static TaskHandle_t query_task_handle(void);

static esp_err_t stop_identity_registry_entry(void *context, uint32_t timeout_ms) {
    TaskHandle_t task = query_task_handle();
    if (context && task && context != (void *)task) return ESP_ERR_INVALID_STATE;
    return firmware_identity_stop(timeout_ms);
}

static TaskHandle_t query_task_handle(void) {
    return __atomic_load_n(&s_query_task, __ATOMIC_ACQUIRE);
}

static void set_query_task_handle(TaskHandle_t handle) {
    __atomic_store_n(&s_query_task, handle, __ATOMIC_RELEASE);
}

static bool query_task_is_starting(void) {
    return __atomic_load_n(&s_query_task_starting, __ATOMIC_ACQUIRE);
}

static void set_query_task_starting(bool starting) {
    __atomic_store_n(&s_query_task_starting, starting, __ATOMIC_RELEASE);
}

static bool query_task_is_active(void) {
    return query_task_handle() != NULL || query_task_is_starting();
}

static esp_err_t ensure_query_task_sync_primitives(void) {
    if (!s_query_task_start_gate) {
        s_query_task_start_gate = xSemaphoreCreateBinary();
        if (!s_query_task_start_gate) return ESP_ERR_NO_MEM;
    }
    if (!s_query_task_stopped) {
        s_query_task_stopped = xSemaphoreCreateBinary();
        if (!s_query_task_stopped) return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

static void signal_query_task_stopped(void) {
    if (s_query_task_stopped) (void)xSemaphoreGive(s_query_task_stopped);
}

static bool nonce_is_valid(const char *nonce) {
    if (!nonce) return true;
    size_t length = strlen(nonce);
    if (length == 0 || length > IDENTITY_NONCE_MAX_LENGTH) return false;
    for (size_t i = 0; i < length; ++i) {
        unsigned char value = (unsigned char)nonce[i];
        if (!(isalnum(value) || value == '-' || value == '_' || value == '.')) return false;
    }
    return true;
}

static const char *chip_name(esp_chip_model_t model) {
    switch (model) {
        case CHIP_ESP32: return "esp32";
        case CHIP_ESP32S2: return "esp32s2";
        case CHIP_ESP32S3: return "esp32s3";
        case CHIP_ESP32C3: return "esp32c3";
        case CHIP_ESP32C2: return "esp32c2";
        case CHIP_ESP32C6: return "esp32c6";
        case CHIP_ESP32H2: return "esp32h2";
        case CHIP_ESP32P4: return "esp32p4";
        case CHIP_ESP32C5: return "esp32c5";
        case CHIP_ESP32C61: return "esp32c61";
        case CHIP_ESP32H21: return "esp32h21";
        default: return "unknown";
    }
}

static cJSON *create_identity_event(const char *type, const char *nonce) {
    const esp_app_desc_t *app = esp_app_get_description();
    esp_chip_info_t chip;
    esp_chip_info(&chip);
    uint32_t flash_size = 0;
    (void)esp_flash_get_size(NULL, &flash_size);
    char elf_sha256[65] = {0};
    (void)esp_app_get_elf_sha256(elf_sha256, sizeof(elf_sha256));

    cJSON *root = cJSON_CreateObject();
    if (!root) return NULL;
    cJSON_AddStringToObject(root, "type", type);
    cJSON_AddNumberToObject(root, "protocol", IDENTITY_PROTOCOL_VERSION);
    if (nonce) cJSON_AddStringToObject(root, "nonce", nonce);
    cJSON_AddStringToObject(root, "display_name", CONFIG_MACLAW_FIRMWARE_DISPLAY_NAME);
    cJSON_AddStringToObject(root, "product_id", CONFIG_MACLAW_PRODUCT_ID);
    // A running application can report its intended board configuration, but
    // the desktop flasher must still verify ROM/manufacturing evidence before
    // treating this as a physical hardware identity.
    cJSON_AddStringToObject(root, "firmware_target_board_id", CONFIG_MACLAW_BOARD_ID);
    cJSON_AddStringToObject(root, "hw_rev", CONFIG_MACLAW_HW_REV);
    cJSON_AddStringToObject(root, "layout_id", CONFIG_MACLAW_LAYOUT_ID);
    cJSON_AddStringToObject(root, "compat_id", CONFIG_MACLAW_COMPAT_ID);
    firmware_identity_info_t identity = {0};
    if (firmware_identity_get(&identity) != ESP_OK) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *power_json = cJSON_AddObjectToObject(root, "power");
    if (!power_json ||
        !cJSON_AddBoolToObject(power_json, "available", identity.power_available) ||
        !cJSON_AddNumberToObject(power_json, "state", identity.power.state) ||
        !cJSON_AddBoolToObject(power_json, "display_off_armed",
                                identity.power.display_off_armed) ||
        !cJSON_AddBoolToObject(power_json, "telemetry_available",
                                identity.power_telemetry_available) ||
        !cJSON_AddNumberToObject(power_json, "battery_level_percent",
                                 identity.power_telemetry_available
                                     ? identity.power_telemetry.level_percent
                                     : -1) ||
        !cJSON_AddBoolToObject(power_json, "charging",
                                identity.power_telemetry_available &&
                                    identity.power_telemetry.charging)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *battery_json = cJSON_AddObjectToObject(root, "battery_policy");
    if (!battery_json ||
        !cJSON_AddBoolToObject(battery_json, "available", identity.battery_policy_available) ||
        !cJSON_AddBoolToObject(battery_json, "telemetry_available",
                                identity.battery_policy.telemetry_available) ||
        !cJSON_AddNumberToObject(battery_json, "level", identity.battery_policy.level) ||
        !cJSON_AddBoolToObject(battery_json, "optional_work_allowed",
                                identity.battery_policy.optional_work_allowed) ||
        !cJSON_AddBoolToObject(battery_json, "high_power_work_allowed",
                                identity.battery_policy.high_power_work_allowed)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *connectivity_json = cJSON_AddObjectToObject(root, "connectivity");
    if (!connectivity_json ||
        !cJSON_AddNumberToObject(connectivity_json, "active_uplink",
                                 identity.connectivity.active_uplink) ||
        !cJSON_AddBoolToObject(connectivity_json, "wifi_ready",
                               identity.connectivity.wifi_ready) ||
        !cJSON_AddBoolToObject(connectivity_json, "cellular_ready",
                               identity.connectivity.cellular_ready) ||
        !cJSON_AddBoolToObject(connectivity_json, "ready",
                               identity.connectivity.ready)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *pressure_json = cJSON_AddObjectToObject(root, "resource_pressure");
    if (!pressure_json ||
        !cJSON_AddBoolToObject(pressure_json, "available",
                               identity.resource_pressure_available) ||
        !cJSON_AddNumberToObject(pressure_json, "level",
                                 identity.resource_pressure.level) ||
        !cJSON_AddNumberToObject(pressure_json, "internal_largest_free_bytes",
                                 identity.resource_pressure.internal_largest_free_bytes) ||
        !cJSON_AddNumberToObject(pressure_json, "external_largest_free_bytes",
                                 identity.resource_pressure.external_largest_free_bytes) ||
        !cJSON_AddBoolToObject(pressure_json, "storage_available",
                               identity.resource_pressure.storage_available) ||
        !cJSON_AddNumberToObject(pressure_json, "storage_free_bytes",
                                 identity.resource_pressure.storage_free_bytes)) {
        cJSON_Delete(root);
        return NULL;
    }
    device_runtime_snapshot_t runtime = {0};
    if (!device_runtime_get_snapshot(&runtime)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *runtime_json = cJSON_AddObjectToObject(root, "runtime");
    if (!runtime_json ||
        !cJSON_AddNumberToObject(runtime_json, "abi_version", runtime.abi_version) ||
        !cJSON_AddNumberToObject(runtime_json, "phase", runtime.phase) ||
        !cJSON_AddNumberToObject(runtime_json, "first_failure_phase",
                                 runtime.first_failure_phase) ||
        !cJSON_AddNumberToObject(runtime_json, "first_failure_status",
                                 runtime.first_failure_status) ||
        !cJSON_AddBoolToObject(runtime_json, "local_services_allowed",
                               runtime.local_services_allowed)) {
        cJSON_Delete(root);
        return NULL;
    }
    app_intent_service_snapshot_t intents = {0};
    if (!app_intent_service_get_snapshot(&intents)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *intents_json = cJSON_AddObjectToObject(root, "input_queue");
    if (!intents_json ||
        !cJSON_AddBoolToObject(intents_json, "started", intents.started) ||
        !cJSON_AddBoolToObject(intents_json, "critical_overflow",
                               intents.critical_overflow) ||
        !cJSON_AddNumberToObject(intents_json, "critical_pending",
                                 intents.critical_pending) ||
        !cJSON_AddNumberToObject(intents_json, "control_pending",
                                 intents.control_pending) ||
        !cJSON_AddNumberToObject(intents_json, "auxiliary_pending",
                                 intents.auxiliary_pending) ||
        !cJSON_AddNumberToObject(intents_json, "dropped_critical",
                                 intents.dropped_critical) ||
        !cJSON_AddNumberToObject(intents_json, "dropped_control",
                                 intents.dropped_control) ||
        !cJSON_AddNumberToObject(intents_json, "dropped_auxiliary",
                                 intents.dropped_auxiliary)) {
        cJSON_Delete(root);
        return NULL;
    }
    device_operation_context_t operation = {0};
    if (!operation_context_get_active(&operation)) {
        cJSON_Delete(root);
        return NULL;
    }
    char operation_id[24];
    snprintf(operation_id, sizeof(operation_id), "%llu",
             (unsigned long long)operation.operation_id);
    cJSON *operation_json = cJSON_AddObjectToObject(root, "operation");
    if (!operation_json ||
        !cJSON_AddNumberToObject(operation_json, "abi_version", operation.abi_version) ||
        !cJSON_AddStringToObject(operation_json, "id", operation_id) ||
        !cJSON_AddNumberToObject(operation_json, "generation", operation.generation) ||
        !cJSON_AddNumberToObject(operation_json, "kind", operation.kind) ||
        !cJSON_AddBoolToObject(operation_json, "cancel_requested",
                               operation.cancel_requested) ||
        !cJSON_AddBoolToObject(operation_json, "terminal_committed",
                               operation.terminal_committed)) {
        cJSON_Delete(root);
        return NULL;
    }
    sleep_schedule_status_t schedule = {0};
    sleep_schedule_service_get_status(&schedule);
    cJSON *schedule_json = cJSON_AddObjectToObject(root, "sleep_schedule");
    if (!schedule_json ||
        !cJSON_AddBoolToObject(schedule_json, "initialized", schedule.initialized) ||
        !cJSON_AddBoolToObject(schedule_json, "enabled", schedule.enabled) ||
        !cJSON_AddBoolToObject(schedule_json, "active_window", schedule.active_window) ||
        !cJSON_AddBoolToObject(schedule_json, "manual_override_active",
                               schedule.override_active) ||
        !cJSON_AddBoolToObject(schedule_json, "display_off_requested",
                               schedule.display_off_requested) ||
        !cJSON_AddNumberToObject(schedule_json, "revision", schedule.revision) ||
        !cJSON_AddNumberToObject(schedule_json, "next_transition_epoch",
                                 (double)schedule.next_transition_epoch)) {
        cJSON_Delete(root);
        return NULL;
    }
    fall_detection_snapshot_t fall_detection = {0};
    if (!fall_detection_service_get_snapshot(&fall_detection)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *fall_json = cJSON_AddObjectToObject(root, "fall_detection");
    if (!fall_json ||
        !cJSON_AddBoolToObject(fall_json, "available", fall_detection.available) ||
        !cJSON_AddBoolToObject(fall_json, "enabled", fall_detection.enabled) ||
        !cJSON_AddNumberToObject(fall_json, "state", fall_detection.state) ||
        !cJSON_AddNumberToObject(fall_json, "suspected_count",
                                 fall_detection.suspected_count) ||
        !cJSON_AddNumberToObject(fall_json, "configuration_revision",
                                 fall_detection.configuration_revision)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON *profile_json = cJSON_AddObjectToObject(root, "device_profile");
    if (!profile_json ||
        !cJSON_AddNumberToObject(profile_json, "abi_version", identity.profile.abi_version) ||
        !cJSON_AddStringToObject(profile_json, "id", identity.profile.id) ||
        !cJSON_AddNumberToObject(profile_json, "display_width", identity.profile.display_width) ||
        !cJSON_AddNumberToObject(profile_json, "display_height", identity.profile.display_height) ||
        !cJSON_AddNumberToObject(profile_json, "capabilities", identity.profile.capabilities) ||
        !cJSON_AddNumberToObject(profile_json, "primary_interaction_source",
                                 identity.profile.primary_interaction_source)) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON_AddStringToObject(root, "project_name", app->project_name);
    cJSON_AddNumberToObject(root, "release_sequence", CONFIG_MACLAW_RELEASE_SEQUENCE);
    cJSON_AddNumberToObject(root, "firmware_version", CONFIG_MACLAW_RELEASE_SEQUENCE);
    cJSON_AddStringToObject(root, "app_version", app->version);
    cJSON_AddStringToObject(root, "idf_version", app->idf_ver);
    cJSON_AddStringToObject(root, "app_elf_sha256", elf_sha256);
    cJSON_AddStringToObject(root, "chip", chip_name(chip.model));
    cJSON_AddNumberToObject(root, "chip_revision", chip.revision);
    cJSON_AddNumberToObject(root, "flash_size_bytes", flash_size);
    cJSON_AddNumberToObject(root, "psram_size_bytes", esp_psram_get_size());
    cJSON *self_test = cJSON_AddObjectToObject(root, "self_test");
    if (!self_test) {
        cJSON_Delete(root);
        return NULL;
    }
    cJSON_AddStringToObject(self_test, "flash", "ok");
    cJSON_AddStringToObject(self_test, "psram", esp_psram_get_size() > 0 ? "ok" : "unavailable");
    cJSON_AddStringToObject(self_test, "local_ready", s_local_ready ? "ok" : "pending");
    // BOOT_STATUS proves local firmware/HAL readiness, while SERVICE_STATUS
    // independently reports authenticated external-service readiness. An offline
    // device must still be able to prove that a newly flashed App booted.
    bool ready = strcmp(type, "SERVICE_STATUS") == 0 ? s_service_ready : s_local_ready;
    cJSON_AddBoolToObject(root, "ready", ready);
    return root;
}

static void emit_event(const char *type, const char *nonce) {
    cJSON *root = create_identity_event(type, nonce);
    if (!root) return;
    char *json = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    if (!json) return;
    printf(IDENTITY_EVENT_PREFIX "%s\n", json);
    fflush(stdout);
    cJSON_free(json);
}

static void handle_query_line(char *line) {
    const size_t prefix_length = strlen(IDENTITY_QUERY_PREFIX);
    if (strncmp(line, IDENTITY_QUERY_PREFIX, prefix_length) != 0) return;
    cJSON *query = cJSON_Parse(line + prefix_length);
    if (!cJSON_IsObject(query)) {
        cJSON_Delete(query);
        return;
    }
    cJSON *type_item = cJSON_GetObjectItemCaseSensitive(query, "type");
    cJSON *nonce_item = cJSON_GetObjectItemCaseSensitive(query, "nonce");
    const char *type = cJSON_IsString(type_item) ? type_item->valuestring : NULL;
    const char *nonce = cJSON_IsString(nonce_item) ? nonce_item->valuestring : NULL;
    if (type && nonce_is_valid(nonce)) {
        if (strcmp(type, "IDENTIFY") == 0) emit_event("IDENTITY", nonce);
        else if (strcmp(type, "BOOT_STATUS") == 0) emit_event("BOOT_STATUS", nonce);
        else if (strcmp(type, "SERVICE_STATUS") == 0) emit_event("SERVICE_STATUS", nonce);
    }
    cJSON_Delete(query);
}

static void identity_query_task(void *arg) {
    (void)arg;
    /* The registry entry is installed before the task is created.  Do not read
     * USB (or emit a response) until that ownership is visible to lifecycle
     * rollback. A registration failure instead releases this gate with the
     * stop request already set, so the worker exits without touching USB. */
    if (!s_query_task_start_gate ||
        xSemaphoreTake(s_query_task_start_gate, portMAX_DELAY) != pdTRUE) {
        TaskHandle_t self = xTaskGetCurrentTaskHandle();
        if (query_task_handle() == self) set_query_task_handle(NULL);
        set_query_task_starting(false);
        signal_query_task_stopped();
        vTaskDelete(NULL);
        return;
    }
    char line[IDENTITY_LINE_CAPACITY];
    size_t used = 0;
    bool discarding = false;
    while (!__atomic_load_n(&s_query_task_stop_requested, __ATOMIC_ACQUIRE)) {
        unsigned char input[64];
        int count = usb_serial_jtag_ll_read_rxfifo(input, sizeof(input));
        for (int i = 0; i < count; ++i) {
            unsigned char value = input[i];
            if (value == '\r') continue;
            if (value == '\n') {
                if (!discarding && used > 0) {
                    line[used] = '\0';
                    handle_query_line(line);
                }
                used = 0;
                discarding = false;
            } else if (!discarding) {
                if (used + 1 < sizeof(line)) line[used++] = (char)value;
                else {
                    used = 0;
                    discarding = true;
                }
            }
        }
        vTaskDelay(pdMS_TO_TICKS(count > 0 ? 10 : 50));
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    if (query_task_handle() == self) set_query_task_handle(NULL);
    set_query_task_starting(false);
    signal_query_task_stopped();
    vTaskDelete(NULL);
}

esp_err_t firmware_identity_start(void) {
    // Broadcasts remain diagnostic only. The host accepts only a query-bound,
    // nonce-bearing BOOT_STATUS response as post-flash success evidence.
#if CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG
    if (query_task_is_active()) return ESP_OK;
    esp_err_t sync_err = ensure_query_task_sync_primitives();
    if (sync_err != ESP_OK) return sync_err;
    /* A prior generation always consumed its gate before completion. Drain a
     * stale completion token so this generation's bounded join cannot report
     * the predecessor's exit. */
    (void)xSemaphoreTake(s_query_task_stopped, 0);
    (void)xSemaphoreTake(s_query_task_start_gate, 0);
    __atomic_store_n(&s_query_task_stop_requested, false, __ATOMIC_RELEASE);
    set_query_task_starting(true);
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreate(identity_query_task, "firmware_identity", 4096,
                                     NULL, 1, &task);
    if (created != pdPASS) {
        set_query_task_starting(false);
        return ESP_ERR_NO_MEM;
    }
    /* The newborn task is held at its start gate, so publishing its immutable
     * handle and registering that exact generation cannot race USB reads or
     * lifecycle rollback.  Context is deliberately the task handle rather
     * than NULL: an old generation can never unregister a later one. */
    set_query_task_handle(task);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_DIAGNOSTICS,
        .name = "firmware_identity",
        .context = (void *)task,
        .stop = stop_identity_registry_entry,
    });
    if (registry_err != ESP_OK) {
        __atomic_store_n(&s_query_task_stop_requested, true, __ATOMIC_RELEASE);
        set_query_task_starting(false);
        (void)xSemaphoreGive(s_query_task_start_gate);
        (void)firmware_identity_stop(500);
        return registry_err;
    }
    set_query_task_starting(false);
    /* A concurrent lifecycle stop may have closed admission while this task
     * was held at the gate. Releasing it is still required: the task observes
     * the stop request and acknowledges its own cooperative join. */
    if (xSemaphoreGive(s_query_task_start_gate) != pdTRUE) {
        __atomic_store_n(&s_query_task_stop_requested, true, __ATOMIC_RELEASE);
        (void)firmware_identity_stop(500);
        return ESP_FAIL;
    }
#endif
    emit_event("IDENTITY", NULL);
    return ESP_OK;
}

esp_err_t firmware_identity_stop(uint32_t timeout_ms) {
#if CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = query_task_handle();
    if (!task && !query_task_is_starting()) return ESP_OK;
    __atomic_store_n(&s_query_task_stop_requested, true, __ATOMIC_RELEASE);
    while (query_task_is_active()) {
        int64_t remaining_us = deadline_us - esp_timer_get_time();
        if (remaining_us <= 0) return ESP_ERR_TIMEOUT;
        TickType_t wait_ticks = pdMS_TO_TICKS((uint32_t)((remaining_us + 999) / 1000));
        if (wait_ticks == 0) wait_ticks = 1;
        (void)xSemaphoreTake(s_query_task_stopped, wait_ticks);
    }
    /* Natural exit must not take the Registry's unbounded lock after it has
     * published completion.  The lifecycle caller owns removal under its
     * parent deadline; an exhausted budget leaves the entry fail-closed. */
    if (task) {
        int64_t remaining_us = deadline_us - esp_timer_get_time();
        if (remaining_us <= 0) return ESP_ERR_TIMEOUT;
        uint32_t remaining_ms = (uint32_t)((remaining_us + 999) / 1000);
        esp_err_t unregister_err = task_registry_unregister_with_timeout(
            TASK_REGISTRY_OWNER_DIAGNOSTICS, (void *)task, remaining_ms);
        if (unregister_err != ESP_OK) return unregister_err;
    }
#else
    (void)timeout_ms;
#endif
    return ESP_OK;
}

void firmware_identity_set_local_ready(bool ready) {
    s_local_ready = ready;
    if (ready) emit_event("BOOT_STATUS", NULL);
}

esp_err_t firmware_identity_get(firmware_identity_info_t *out) {
    if (!out) return ESP_ERR_INVALID_ARG;
    const esp_app_desc_t *app = esp_app_get_description();
    if (!app) return ESP_ERR_INVALID_STATE;
    memset(out, 0, sizeof(*out));
    strlcpy(out->product_id, CONFIG_MACLAW_PRODUCT_ID, sizeof(out->product_id));
    strlcpy(out->board_id, CONFIG_MACLAW_BOARD_ID, sizeof(out->board_id));
    strlcpy(out->hardware_rev, CONFIG_MACLAW_HW_REV, sizeof(out->hardware_rev));
    strlcpy(out->layout_id, CONFIG_MACLAW_LAYOUT_ID, sizeof(out->layout_id));
    strlcpy(out->compatibility_id, CONFIG_MACLAW_COMPAT_ID, sizeof(out->compatibility_id));
    out->release_sequence = CONFIG_MACLAW_RELEASE_SEQUENCE;
    strlcpy(out->app_version, app->version, sizeof(out->app_version));
    (void)esp_app_get_elf_sha256(out->elf_sha256, sizeof(out->elf_sha256));
    if (!device_profile_get(&out->profile)) return ESP_ERR_INVALID_STATE;
    out->power_available = device_power_get_snapshot(&out->power);
    out->power_telemetry_available =
        device_power_get_telemetry(&out->power_telemetry);
    out->battery_policy_available =
        device_battery_policy_get_snapshot(&out->battery_policy);
    (void)device_connectivity_get_snapshot(&out->connectivity);
    out->resource_pressure_available =
        device_resource_pressure_get_snapshot(&out->resource_pressure);
    return ESP_OK;
}

void firmware_identity_set_service_ready(bool ready) {
    bool changed = s_service_ready != ready;
    s_service_ready = ready;
    if (changed) emit_event("SERVICE_STATUS", NULL);
}
