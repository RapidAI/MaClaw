#pragma once

/* Provisioning QR presentation adapter. It owns the ESP QR encoder callback
 * and short-lived module matrix allocation. The public contract carries only
 * AP strings and semantic UI values; QR SDK, renderer, allocator and RTOS
 * details remain private. */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    bool (*publish_modules)(const uint8_t *modules, size_t module_count,
                            const char *ssid, void *context);
    void (*publish_fallback_message)(const char *title, const char *body,
                                     void *context);
    void *context;
} provisioning_qr_service_host_t;

device_status_t provisioning_qr_service_init(
    const provisioning_qr_service_host_t *host);

/* Encodes one ephemeral WPA/WPA2 AP credential pair. This service neither
 * retains nor directly logs the passphrase. Encoding/allocation failure
 * publishes a setup fallback message through the host and returns an error. */
device_status_t provisioning_qr_service_show(const char *ssid,
                                             const char *passphrase);
