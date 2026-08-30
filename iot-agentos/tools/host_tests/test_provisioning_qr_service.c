#include <assert.h>
#include <stdio.h>

#include "services/provisioning_qr_service.h"

static bool publish_modules(const uint8_t *modules, size_t module_count,
                            const char *ssid, void *context) {
    (void)modules; (void)module_count; (void)ssid; (void)context;
    return true;
}
static void publish_fallback(const char *title, const char *body, void *context) {
    (void)title; (void)body; (void)context;
}

int main(void) {
    provisioning_qr_service_host_t host = {
        .struct_size = sizeof(provisioning_qr_service_host_t),
        .publish_modules = publish_modules,
        .publish_fallback_message = publish_fallback,
        .context = NULL,
    };
    assert(host.struct_size == sizeof(host));
    assert(host.publish_modules && host.publish_fallback_message);
    puts("PASS provisioning QR value contract");
    return 0;
}
