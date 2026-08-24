#include "storage_service.h"

#include "platform_storage.h"

/* Mount/unmount runs below Platform Storage and may involve a slow physical
 * VFS transaction.  The reusable fault-domain contract closes admission before
 * each physical change and advances a generation before any old VFS borrower
 * could be accepted again.  Storage remains the sole owner of actual mount,
 * cleanup and observed-state self-test. */
static fault_domain_t s_domain = {
    .struct_size = sizeof(fault_domain_t),
    .abi_version = FAULT_DOMAIN_ABI_VERSION,
    .id = FAULT_DOMAIN_ID_STORAGE,
    .phase = FAULT_DOMAIN_STOPPED,
    .generation = 1u,
};

device_status_t storage_service_init(void) {
    for (;;) {
        fault_domain_snapshot_t snapshot;
        if (!fault_domain_get_snapshot(&s_domain, &snapshot)) {
            return DEVICE_STATUS_INTERNAL_ERROR;
        }
        switch (snapshot.phase) {
            case FAULT_DOMAIN_READY:
                return DEVICE_STATUS_OK;
            case FAULT_DOMAIN_REINITIALIZING:
            case FAULT_DOMAIN_SELF_TEST:
            case FAULT_DOMAIN_QUIESCING:
            case FAULT_DOMAIN_UNKNOWN_OUTCOME:
            case FAULT_DOMAIN_DEGRADED:
                return DEVICE_STATUS_BUSY;
            case FAULT_DOMAIN_STOPPED:
                if (fault_domain_begin_start(&s_domain)) {
                    break;
                }
                continue;
        }
        break;
    }
    const device_status_t status = platform_storage_mount();
    const bool mounted = platform_storage_is_mounted();
    if (status == DEVICE_STATUS_OK && mounted) {
        /* `is_mounted` is Storage's physical self-test: only the observed
         * VFS fact, never a private success return, can reopen admission. */
        if (fault_domain_begin_self_test(&s_domain) &&
            platform_storage_is_mounted() &&
            fault_domain_mark_ready(&s_domain)) {
            return DEVICE_STATUS_OK;
        }
        (void)fault_domain_mark_unknown_outcome(&s_domain);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* A physical port that reports an error after it has nevertheless mounted
     * leaves the VFS outcome unknown to its caller.  Do not retry mount over
     * that live volume: close admission and require the same bounded cleanup
     * retry path used after an unmount failure. */
    if (mounted) {
        (void)fault_domain_mark_unknown_outcome(&s_domain);
    } else {
        /* No physical volume is observable; this failed start is safe to retry
         * without pretending the incomplete runtime state is authoritative. */
        (void)fault_domain_mark_stopped(&s_domain);
    }
    /* A Platform port may never publish successful mount without an observed
     * mounted volume.  Turn such a broken private contract into a stable
     * common failure instead of returning OK with Storage unavailable. */
    return status == DEVICE_STATUS_OK ? DEVICE_STATUS_INTERNAL_ERROR : status;
}

device_status_t storage_service_deinit(void) {
    for (;;) {
        fault_domain_snapshot_t snapshot;
        if (!fault_domain_get_snapshot(&s_domain, &snapshot)) {
            return DEVICE_STATUS_INTERNAL_ERROR;
        }
        if (snapshot.phase == FAULT_DOMAIN_STOPPED) return DEVICE_STATUS_OK;
        if (snapshot.phase == FAULT_DOMAIN_REINITIALIZING ||
            snapshot.phase == FAULT_DOMAIN_SELF_TEST ||
            snapshot.phase == FAULT_DOMAIN_QUIESCING ||
            snapshot.phase == FAULT_DOMAIN_DEGRADED) {
            return DEVICE_STATUS_BUSY;
        }
        if (fault_domain_begin_quiesce(&s_domain)) {
            break;
        }
    }
    /* State switched away from READY before the platform transaction. A
     * concurrent optional-cache caller therefore fails closed throughout the
     * VFS teardown; the composition root still proves existing users joined. */
    const device_status_t status = platform_storage_unmount();
    const bool mounted = platform_storage_is_mounted();
    if (status == DEVICE_STATUS_OK && !mounted) {
        return fault_domain_mark_stopped(&s_domain) ? DEVICE_STATUS_OK
                                                    : DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* Symmetric to mount: a successful private return is not enough to reopen
     * the lifecycle if the VFS still reports mounted.  Preserve closed
     * admission and let the composition-root cleanup retry rather than hiding
     * a live volume behind STOPPED. */
    (void)fault_domain_mark_unknown_outcome(&s_domain);
    return status == DEVICE_STATUS_OK ? DEVICE_STATUS_INTERNAL_ERROR : status;
}

bool storage_service_is_available(void) {
    fault_domain_snapshot_t snapshot;
    return fault_domain_get_snapshot(&s_domain, &snapshot) && snapshot.admission_open;
}

bool storage_service_get_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot) {
    return fault_domain_get_snapshot(&s_domain, out_snapshot);
}

const char *storage_service_label(void) {
    /* The profile volume identity stays stable even when mount failed. Resource
     * Pressure then records unavailable rather than being unable to initialize. */
    return platform_storage_label();
}

bool storage_service_allows_optional_flash_work(void) {
    return storage_service_is_available() && platform_storage_allows_optional_flash_work();
}
