#pragma once

#include <stdbool.h>

#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

// Starts the non-blocking USB Serial/JTAG identity query task and emits the
// boot identity event once. Safe to call only after the ESP-IDF console VFS is
// initialized (app_main satisfies this requirement).
esp_err_t firmware_identity_start(void);

// Local readiness deliberately excludes Wi-Fi and Hub availability. It means
// the firmware, NVS, local storage, UI and board HAL completed initialization.
void firmware_identity_set_local_ready(bool ready);

// Service readiness is reported separately so an offline device can still
// prove that the newly flashed application booted successfully.
void firmware_identity_set_service_ready(bool ready);

#ifdef __cplusplus
}
#endif
