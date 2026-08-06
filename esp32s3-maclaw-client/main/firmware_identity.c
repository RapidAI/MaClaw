#include "firmware_identity.h"

#include <ctype.h>
#include <stdio.h>
#include <string.h>

#include "cJSON.h"
#include "esp_app_desc.h"
#include "esp_chip_info.h"
#include "esp_flash.h"
#include "esp_psram.h"
#include "hal/usb_serial_jtag_ll.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define IDENTITY_PROTOCOL_VERSION 1
#define IDENTITY_QUERY_PREFIX "CLAWMATE_QUERY "
#define IDENTITY_EVENT_PREFIX "CLAWMATE_EVT "
#define IDENTITY_LINE_CAPACITY 512
#define IDENTITY_NONCE_MAX_LENGTH 64

static volatile bool s_local_ready;
static volatile bool s_service_ready;

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
    cJSON_AddStringToObject(root, "board_id", CONFIG_MACLAW_BOARD_ID);
    cJSON_AddStringToObject(root, "hw_rev", CONFIG_MACLAW_HW_REV);
    cJSON_AddStringToObject(root, "layout_id", CONFIG_MACLAW_LAYOUT_ID);
    cJSON_AddStringToObject(root, "compat_id", CONFIG_MACLAW_COMPAT_ID);
    cJSON_AddStringToObject(root, "project_name", app->project_name);
    cJSON_AddStringToObject(root, "firmware_version", app->version);
    cJSON_AddStringToObject(root, "idf_version", app->idf_ver);
    cJSON_AddStringToObject(root, "app_elf_sha256", elf_sha256);
    cJSON_AddStringToObject(root, "chip", chip_name(chip.model));
    cJSON_AddNumberToObject(root, "chip_revision", chip.revision);
    cJSON_AddNumberToObject(root, "flash_size_bytes", flash_size);
    cJSON_AddNumberToObject(root, "psram_size_bytes", esp_psram_get_size());
    cJSON_AddBoolToObject(root, "local_ready", s_local_ready);
    cJSON_AddBoolToObject(root, "service_ready", s_service_ready);
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
        else if (strcmp(type, "BOOT_STATUS") == 0) emit_event("BOOT_OK", nonce);
        else if (strcmp(type, "SERVICE_STATUS") == 0) emit_event("SERVICE_STATUS", nonce);
    }
    cJSON_Delete(query);
}

static void identity_query_task(void *arg) {
    (void)arg;
    char line[IDENTITY_LINE_CAPACITY];
    size_t used = 0;
    bool discarding = false;
    while (true) {
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
}

esp_err_t firmware_identity_start(void) {
    emit_event("IDENTITY", NULL);
#if CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG
    BaseType_t created = xTaskCreate(identity_query_task, "firmware_identity", 4096,
                                     NULL, 1, NULL);
    if (created != pdPASS) return ESP_ERR_NO_MEM;
#endif
    return ESP_OK;
}

void firmware_identity_set_local_ready(bool ready) {
    s_local_ready = ready;
    if (ready) emit_event("BOOT_OK", NULL);
}

void firmware_identity_set_service_ready(bool ready) {
    bool changed = s_service_ready != ready;
    s_service_ready = ready;
    if (changed) emit_event("SERVICE_STATUS", NULL);
}
