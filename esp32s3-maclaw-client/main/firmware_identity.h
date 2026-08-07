#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"
#include "device_api.h"

#define FIRMWARE_IDENTITY_PRODUCT_ID_CAPACITY 96
#define FIRMWARE_IDENTITY_BOARD_ID_CAPACITY 128
#define FIRMWARE_IDENTITY_HW_REV_CAPACITY 64
#define FIRMWARE_IDENTITY_LAYOUT_ID_CAPACITY 128
#define FIRMWARE_IDENTITY_COMPAT_ID_CAPACITY 256
#define FIRMWARE_IDENTITY_APP_VERSION_CAPACITY 128
#define FIRMWARE_IDENTITY_ELF_SHA256_CAPACITY 65

typedef struct {
    char product_id[FIRMWARE_IDENTITY_PRODUCT_ID_CAPACITY];
    char board_id[FIRMWARE_IDENTITY_BOARD_ID_CAPACITY];
    char hardware_rev[FIRMWARE_IDENTITY_HW_REV_CAPACITY];
    char layout_id[FIRMWARE_IDENTITY_LAYOUT_ID_CAPACITY];
    char compatibility_id[FIRMWARE_IDENTITY_COMPAT_ID_CAPACITY];
    int64_t release_sequence;
    char app_version[FIRMWARE_IDENTITY_APP_VERSION_CAPACITY];
    char elf_sha256[FIRMWARE_IDENTITY_ELF_SHA256_CAPACITY];
    /* By-value, ABI-checked profile snapshot.  Do not expose a board-owned
     * descriptor pointer through diagnostics or gateway serialization. */
    device_profile_t profile;
    /* The current physical panel/backlight observation and the idle policy
     * timer are runtime facts, separate from the static profile.  This only
     * represents DISPLAY_OFF; it never asserts MCU light/deep sleep. */
    device_power_snapshot_t power;
    bool power_available;
    /* Observed selected uplink state. This is separate from external-service
     * readiness and does not disclose credentials, AP names, or modem data. */
    device_connectivity_snapshot_t connectivity;
} firmware_identity_info_t;

#ifdef __cplusplus
extern "C" {
#endif

// Starts the non-blocking USB Serial/JTAG identity query task and emits the
// boot identity event once. Safe to call only after the ESP-IDF console VFS is
// initialized (app_main satisfies this requirement).
esp_err_t firmware_identity_start(void);

// Stops and joins the USB Serial/JTAG query task.  It is intentionally
// idempotent so Boot/Power coordinators can use it during failed startup,
// quiesce and shutdown without having to infer task ownership.  `timeout_ms`
// bounds the cooperative join; no task is forcibly deleted while it may be
// parsing a host request or emitting diagnostic output.
esp_err_t firmware_identity_stop(uint32_t timeout_ms);

// Returns the immutable identity of the currently running firmware. The
// caller supplies the physical device ID (derived from the chip MAC) when it
// serializes this snapshot into a gateway request.
esp_err_t firmware_identity_get(firmware_identity_info_t *out);

// Local readiness deliberately excludes Wi-Fi and Hub availability. It means
// the firmware, NVS, local storage, UI and board HAL completed initialization.
void firmware_identity_set_local_ready(bool ready);

// Service readiness is reported separately so an offline device can still
// prove that the newly flashed application booted successfully.
void firmware_identity_set_service_ready(bool ready);

#ifdef __cplusplus
}
#endif
