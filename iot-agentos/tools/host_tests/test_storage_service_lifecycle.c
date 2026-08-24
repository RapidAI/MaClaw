#include <stdio.h>

#include "storage_service.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static device_status_t s_mount_result;
static device_status_t s_unmount_result;
static bool s_platform_mounted;
static bool s_mount_publishes_mounted;
static bool s_mount_leaves_mounted_after_error;
static bool s_optional_allowed;
static unsigned s_mount_calls;
static unsigned s_unmount_calls;
static bool s_reenter_init_during_mount;
static bool s_reenter_deinit_during_unmount;
static bool s_unmount_clears_mounted;

device_status_t platform_storage_mount(void) {
    ++s_mount_calls;
    if (s_reenter_init_during_mount) {
        CHECK(storage_service_init() == DEVICE_STATUS_BUSY);
    }
    if ((s_mount_result == DEVICE_STATUS_OK && s_mount_publishes_mounted) ||
        (s_mount_result != DEVICE_STATUS_OK && s_mount_leaves_mounted_after_error)) {
        s_platform_mounted = true;
    }
    return s_mount_result;
}
device_status_t platform_storage_unmount(void) {
    ++s_unmount_calls;
    if (s_reenter_deinit_during_unmount) {
        CHECK(storage_service_deinit() == DEVICE_STATUS_BUSY);
    }
    if (s_unmount_result == DEVICE_STATUS_OK && s_unmount_clears_mounted) {
        s_platform_mounted = false;
    }
    return s_unmount_result;
}
bool platform_storage_is_mounted(void) { return s_platform_mounted; }
bool platform_storage_allows_optional_flash_work(void) { return s_optional_allowed; }
const char *platform_storage_label(void) { return "storage"; }

static void reset_platform(void) {
    s_mount_result = DEVICE_STATUS_OK;
    s_unmount_result = DEVICE_STATUS_OK;
    s_platform_mounted = false;
    s_mount_publishes_mounted = true;
    s_mount_leaves_mounted_after_error = false;
    s_optional_allowed = true;
    s_mount_calls = 0;
    s_unmount_calls = 0;
    s_reenter_init_during_mount = false;
    s_reenter_deinit_during_unmount = false;
    s_unmount_clears_mounted = true;
}

int main(void) {
    fault_domain_snapshot_t fault_snapshot = {0};
    reset_platform();
    CHECK(!storage_service_is_available());
    CHECK(storage_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_STOPPED);
    CHECK(fault_snapshot.generation == 1u);
    CHECK(!storage_service_allows_optional_flash_work());
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);

    s_reenter_init_during_mount = true;
    CHECK(storage_service_init() == DEVICE_STATUS_OK);
    CHECK(s_mount_calls == 1);
    CHECK(storage_service_is_available());
    CHECK(storage_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_READY);
    CHECK(fault_snapshot.admission_open);
    CHECK(storage_service_allows_optional_flash_work());
    CHECK(storage_service_init() == DEVICE_STATUS_OK);
    CHECK(s_mount_calls == 1);
    CHECK(storage_service_label()[0] == 's');

    s_reenter_deinit_during_unmount = true;
    s_unmount_result = DEVICE_STATUS_TIMEOUT;
    CHECK(storage_service_deinit() == DEVICE_STATUS_TIMEOUT);
    CHECK(s_unmount_calls == 1);
    CHECK(!storage_service_is_available());
    CHECK(!storage_service_allows_optional_flash_work());
    CHECK(storage_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME);
    CHECK(!fault_snapshot.admission_open);
    CHECK(storage_service_init() == DEVICE_STATUS_BUSY);
    CHECK(s_mount_calls == 1);

    s_reenter_deinit_during_unmount = false;
    s_unmount_result = DEVICE_STATUS_OK;
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);
    CHECK(s_unmount_calls == 2);
    CHECK(!storage_service_is_available());

    s_mount_result = DEVICE_STATUS_OK;
    s_platform_mounted = false;
    s_mount_publishes_mounted = false;
    /* A broken private port cannot make callers believe durable storage is
     * usable simply because it returned an inconsistent successful status. */
    CHECK(storage_service_init() == DEVICE_STATUS_INTERNAL_ERROR);
    CHECK(!storage_service_is_available());

    s_mount_publishes_mounted = true;
    CHECK(storage_service_init() == DEVICE_STATUS_OK);
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);

    /* A port may not claim a successful unmount while still publishing a live
     * VFS. Storage remains closed and cleanup, rather than mount, is the only
     * legal retry. */
    CHECK(storage_service_init() == DEVICE_STATUS_OK);
    s_unmount_clears_mounted = false;
    CHECK(storage_service_deinit() == DEVICE_STATUS_INTERNAL_ERROR);
    CHECK(!storage_service_is_available());
    CHECK(storage_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME);
    CHECK(storage_service_init() == DEVICE_STATUS_BUSY);
    s_unmount_clears_mounted = true;
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);

    /* An error return after a private mount is likewise an unknown-outcome
     * transaction. Do not mount again over it; force the same cleanup path. */
    s_platform_mounted = false;
    s_mount_result = DEVICE_STATUS_IO_ERROR;
    s_mount_leaves_mounted_after_error = true;
    CHECK(storage_service_init() == DEVICE_STATUS_IO_ERROR);
    CHECK(s_platform_mounted);
    CHECK(storage_service_init() == DEVICE_STATUS_BUSY);
    CHECK(storage_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME);
    s_mount_result = DEVICE_STATUS_OK;
    s_mount_leaves_mounted_after_error = false;
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);

    s_mount_result = DEVICE_STATUS_IO_ERROR;
    CHECK(storage_service_init() == DEVICE_STATUS_IO_ERROR);
    CHECK(!storage_service_is_available());
    s_mount_result = DEVICE_STATUS_OK;
    CHECK(storage_service_init() == DEVICE_STATUS_OK);
    CHECK(storage_service_deinit() == DEVICE_STATUS_OK);

    puts("PASS Storage Service serializes VFS lifecycle and preserves fail-closed retry");
    return 0;
}
