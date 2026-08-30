#include "services/credential_service.h"

#include <stdatomic.h>
#include <string.h>

static atomic_flag s_lock = ATOMIC_FLAG_INIT;
static bool s_initialized;
static bool s_persistence_fault;
static uint32_t s_generation;
static char s_token[CREDENTIAL_SERVICE_MAX_TOKEN + 1u];
static char s_identity[CREDENTIAL_SERVICE_IDENTITY_CAPACITY];
static credential_service_generation_write_fn s_write_floor;
static void *s_generation_persistence_context;
static atomic_flag s_floor_write_lock = ATOMIC_FLAG_INIT;

static void lock_service(void) {
    while (atomic_flag_test_and_set_explicit(&s_lock, memory_order_acquire)) {}
}
static void unlock_service(void) {
    atomic_flag_clear_explicit(&s_lock, memory_order_release);
}
static void wipe(void *ptr, size_t length) {
    volatile unsigned char *p = (volatile unsigned char *)ptr;
    while (p && length--) *p++ = 0;
}
/* Bound caller-owned strings before inspecting them.  A malformed
 * non-NUL-terminated value must be rejected without reading past the service
 * contract's fixed capacity. */
static size_t bounded_length(const char *value, size_t capacity) {
    if (!value) return SIZE_MAX;
    for (size_t i = 0; i < capacity; ++i) {
        if (value[i] == '\0') return i;
    }
    return SIZE_MAX;
}
static uint32_t next_generation(void) {
    ++s_generation;
    if (s_generation == 0u) ++s_generation;
    return s_generation;
}

/* Serialize durable floor writes and re-read the in-memory generation after
 * taking the writer lock.  Without this second read, two concurrent callers
 * could complete out of order (generation N+1 written after N+2) and lower
 * the persisted revocation floor across a reboot. */
static device_status_t persist_generation_floor(uint32_t requested_generation,
                                                credential_service_generation_write_fn writer,
                                                void *context) {
    if (!writer) return DEVICE_STATUS_OK;
    while (atomic_flag_test_and_set_explicit(&s_floor_write_lock,
                                             memory_order_acquire)) {}
    lock_service();
    const uint32_t current_generation = s_generation;
    const bool faulted = s_persistence_fault;
    unlock_service();
    const uint32_t floor = current_generation > requested_generation
                               ? current_generation : requested_generation;
    const device_status_t status = faulted
        ? DEVICE_STATUS_UNAVAILABLE : writer(floor, context);
    atomic_flag_clear_explicit(&s_floor_write_lock, memory_order_release);
    return status;
}

device_status_t credential_service_init(void) {
    lock_service();
    if (!s_initialized) {
        wipe(s_token, sizeof(s_token));
        wipe(s_identity, sizeof(s_identity));
        s_generation = 1u;
        s_persistence_fault = false;
        s_write_floor = NULL;
        s_generation_persistence_context = NULL;
        s_initialized = true;
    }
    unlock_service();
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_set_generation_persistence(
    credential_service_generation_read_fn read_floor,
    credential_service_generation_write_fn write_floor,
    void *context) {
    if (!read_floor || !write_floor) return DEVICE_STATUS_INVALID_ARGUMENT;
    uint64_t floor = 0u;
    const device_status_t read_status = read_floor(&floor, context);
    if (read_status != DEVICE_STATUS_OK && read_status != DEVICE_STATUS_NOT_FOUND) {
        return read_status;
    }
    uint32_t current_generation = 0u;
    bool need_floor_write = false;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (floor > UINT32_MAX) {
        unlock_service();
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    /* gateway_transport_init() can create the process-local generation before
     * Persistence Service is ready.  A durable floor that is behind that
     * generation is therefore not corruption: keep the higher local value and
     * repair the floor below.  Never move the generation backwards. */
    if (floor > (uint64_t)s_generation) s_generation = (uint32_t)floor;
    current_generation = s_generation;
    need_floor_write = floor == 0u || floor < (uint64_t)current_generation;
    s_write_floor = write_floor;
    s_generation_persistence_context = context;
    unlock_service();
    /* Persist the generation that already existed before this bridge was
     * installed (gateway transport may initialize Credential Service before
     * Persistence Service).  Do this outside the service spinlock because the
     * callback may block on a storage worker. */
    if (need_floor_write &&
        persist_generation_floor(current_generation, write_floor, context) != DEVICE_STATUS_OK) {
        lock_service();
        s_persistence_fault = true;
        unlock_service();
        return DEVICE_STATUS_IO_ERROR;
    }
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_begin_generation(uint32_t *out_generation) {
    if (!out_generation) return DEVICE_STATUS_INVALID_ARGUMENT;
    credential_service_generation_write_fn write_floor = NULL;
    void *write_context = NULL;
    uint32_t next = 0u;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (s_persistence_fault) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    wipe(s_token, sizeof(s_token));
    wipe(s_identity, sizeof(s_identity));
    next = next_generation();
    write_floor = s_write_floor;
    write_context = s_generation_persistence_context;
    unlock_service();
    if (write_floor && persist_generation_floor(next, write_floor, write_context) != DEVICE_STATUS_OK) {
        /* The in-memory generation remains advanced and credentials remain
         * wiped. A failed durable floor write therefore cannot resurrect an
         * older token; callers must keep the service fail-closed and retry the
         * persistence path explicitly. */
        lock_service();
        s_persistence_fault = true;
        unlock_service();
        return DEVICE_STATUS_IO_ERROR;
    }
    *out_generation = next;
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_store_gateway_token(uint32_t generation,
                                                        const char *token) {
    const size_t length = bounded_length(token, CREDENTIAL_SERVICE_MAX_TOKEN + 1u);
    if (length == SIZE_MAX || length == 0u)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (s_persistence_fault) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (generation == 0u || generation != s_generation) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    wipe(s_token, sizeof(s_token));
    memcpy(s_token, token, length + 1u);
    unlock_service();
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_bind_identity(uint32_t generation,
                                                  const char *identity) {
    const size_t length = bounded_length(identity, sizeof(s_identity));
    if (length == SIZE_MAX || length == 0u)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (s_persistence_fault) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (generation == 0u || generation != s_generation) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    wipe(s_identity, sizeof(s_identity));
    memcpy(s_identity, identity, length + 1u);
    unlock_service();
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_restore_gateway_token(uint32_t generation,
                                                          const char *token,
                                                          const char *identity) {
    const size_t token_length = bounded_length(token, CREDENTIAL_SERVICE_MAX_TOKEN + 1u);
    const size_t identity_length = bounded_length(identity, sizeof(s_identity));
    if (token_length == SIZE_MAX || token_length == 0u ||
        identity_length == SIZE_MAX || identity_length == 0u) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (s_persistence_fault) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (generation == 0u || generation != s_generation) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    /* Publish both fields while holding the same lock. Readers can therefore
     * never observe a restored token paired with a stale/empty identity. */
    wipe(s_token, sizeof(s_token));
    wipe(s_identity, sizeof(s_identity));
    memcpy(s_token, token, token_length + 1u);
    memcpy(s_identity, identity, identity_length + 1u);
    unlock_service();
    return DEVICE_STATUS_OK;
}

device_status_t credential_service_copy_gateway_token(uint32_t generation,
                                                       char *out,
                                                       size_t capacity,
                                                       size_t *out_length,
                                                       const char *identity) {
    if (!out || capacity == 0u) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* Scrub the destination on every path so stale token bytes cannot survive
     * a generation or identity mismatch. */
    wipe(out, capacity);
    if (out_length) *out_length = 0u;
    const size_t identity_length = bounded_length(identity, sizeof(s_identity));
    if (identity_length == SIZE_MAX || identity_length == 0u)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (s_persistence_fault) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (generation == 0u || generation != s_generation) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    if (!s_identity[0] || strcmp(s_identity, identity) != 0) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    const size_t length = bounded_length(s_token, sizeof(s_token));
    if (length == SIZE_MAX) { unlock_service(); return DEVICE_STATUS_INTERNAL_ERROR; }
    if (length + 1u > capacity) { unlock_service(); return DEVICE_STATUS_RESOURCE_EXHAUSTED; }
    memcpy(out, s_token, length + 1u);
    if (out_length) *out_length = length;
    unlock_service();
    return length ? DEVICE_STATUS_OK : DEVICE_STATUS_NOT_FOUND;
}

device_status_t credential_service_revoke_gateway_token(uint32_t generation) {
    credential_service_generation_write_fn write_floor = NULL;
    void *write_context = NULL;
    uint32_t next = 0u;
    lock_service();
    if (!s_initialized) { unlock_service(); return DEVICE_STATUS_UNAVAILABLE; }
    if (generation == 0u || generation != s_generation) {
        unlock_service(); return DEVICE_STATUS_BUSY;
    }
    wipe(s_token, sizeof(s_token));
    wipe(s_identity, sizeof(s_identity));
    next = next_generation();
    write_floor = s_write_floor;
    write_context = s_generation_persistence_context;
    unlock_service();
    if (write_floor && persist_generation_floor(next, write_floor, write_context) != DEVICE_STATUS_OK) {
        /* Keep the new generation as a tombstone even when persistence fails;
         * the in-memory credential remains revoked and cannot be resurrected. */
        lock_service();
        s_persistence_fault = true;
        unlock_service();
        return DEVICE_STATUS_IO_ERROR;
    }
    return DEVICE_STATUS_OK;
}

bool credential_service_snapshot(credential_service_snapshot_t *out) {
    if (!out || out->struct_size < sizeof(*out) ||
        out->abi_version != CREDENTIAL_SERVICE_ABI_VERSION) return false;
    lock_service();
    if (s_persistence_fault) { unlock_service(); return false; }
    out->generation = s_generation;
    const bool initialized = s_initialized;
    out->present = initialized && s_token[0] != '\0';
    out->length = out->present ? (uint32_t)strlen(s_token) : 0u;
    unlock_service();
    return initialized;
}
