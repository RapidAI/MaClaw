#include "storage_service.h"

#include "platform_storage.h"

static bool s_initialized;
static bool s_available;

device_status_t storage_service_init(void) {
    if (s_initialized) return s_available ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    const device_status_t status = platform_storage_mount();
    s_available = status == DEVICE_STATUS_OK && platform_storage_is_mounted();
    s_initialized = s_available;
    return status;
}

device_status_t storage_service_deinit(void) {
    if (!s_initialized) return DEVICE_STATUS_OK;
    /* Close availability before calling the physical port. A concurrent cache
     * admission then observes false even if it runs during the brief VFS
     * teardown window; the composition root proves all existing users joined. */
    s_available = false;
    const device_status_t status = platform_storage_unmount();
    if (status != DEVICE_STATUS_OK) return status;
    s_initialized = false;
    return DEVICE_STATUS_OK;
}

bool storage_service_is_available(void) { return s_initialized && s_available; }

const char *storage_service_label(void) {
    /* The profile volume identity stays stable even when mount failed. Resource
     * Pressure then records unavailable rather than being unable to initialize. */
    return platform_storage_label();
}

bool storage_service_allows_optional_flash_work(void) {
    return storage_service_is_available() && platform_storage_allows_optional_flash_work();
}
