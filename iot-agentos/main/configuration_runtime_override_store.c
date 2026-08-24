#include "configuration_runtime_override_store.h"

#include <limits.h>
#include <string.h>

static int slot_index(configuration_runtime_override_value_kind_t kind) {
    switch (kind) {
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_OUTPUT_VOLUME: return 0;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_DISPLAY_BRIGHTNESS: return 1;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_SCREEN_SLEEP_SECONDS: return 2;
        case CONFIGURATION_RUNTIME_OVERRIDE_VALUE_TRANSPORT_SELECTION: return 3;
        default: return -1;
    }
}

static bool store_is_valid(const configuration_runtime_override_store_t *store) {
    return store && store->struct_size == sizeof(*store) &&
           store->abi_version == CONFIGURATION_RUNTIME_OVERRIDE_STORE_ABI_VERSION &&
           store->effective_revision != 0u;
}

static configuration_runtime_override_store_result_t advance_revision(
    configuration_runtime_override_store_t *store) {
    if (store->effective_revision == UINT64_MAX) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED;
    }
    ++store->effective_revision;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

void configuration_runtime_override_store_init(
    configuration_runtime_override_store_t *store) {
    if (!store) return;
    memset(store, 0, sizeof(*store));
    store->struct_size = sizeof(*store);
    store->abi_version = CONFIGURATION_RUNTIME_OVERRIDE_STORE_ABI_VERSION;
    store->effective_revision = 1u;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_put(
    configuration_runtime_override_store_t *store,
    const configuration_runtime_override_t *override,
    uint64_t now_monotonic_ms) {
    if (!store_is_valid(store) || !override) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    }
    const int index = slot_index(override->kind);
    if (index < 0) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    if (override->expires_at_monotonic_ms <= now_monotonic_ms) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_EXPIRED;
    }
    if (!configuration_runtime_override_validate(override, now_monotonic_ms)) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    }
    configuration_runtime_override_store_result_t result = advance_revision(store);
    if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) return result;
    store->slots[index] = *override;
    store->occupied[index] = true;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_remove(
    configuration_runtime_override_store_t *store,
    configuration_runtime_override_value_kind_t kind) {
    if (!store_is_valid(store)) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    const int index = slot_index(kind);
    if (index < 0) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    if (!store->occupied[index]) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_NOT_FOUND;
    configuration_runtime_override_store_result_t result = advance_revision(store);
    if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) return result;
    memset(&store->slots[index], 0, sizeof(store->slots[index]));
    store->occupied[index] = false;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_clear_all(
    configuration_runtime_override_store_t *store) {
    if (!store_is_valid(store)) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    bool any = false;
    for (unsigned i = 0; i < CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT; ++i) {
        any = any || store->occupied[i];
    }
    if (!any) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
    configuration_runtime_override_store_result_t result = advance_revision(store);
    if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) return result;
    memset(store->occupied, 0, sizeof(store->occupied));
    memset(store->slots, 0, sizeof(store->slots));
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_discard_expired(
    configuration_runtime_override_store_t *store,
    uint64_t now_monotonic_ms,
    bool *out_changed) {
    if (!store_is_valid(store) || !out_changed) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    }
    bool changed = false;
    for (unsigned i = 0; i < CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT; ++i) {
        if (store->occupied[i] &&
            store->slots[i].expires_at_monotonic_ms <= now_monotonic_ms) {
            changed = true;
        }
    }
    if (changed) {
        configuration_runtime_override_store_result_t result = advance_revision(store);
        if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) return result;
        for (unsigned i = 0; i < CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT; ++i) {
            if (store->occupied[i] &&
                store->slots[i].expires_at_monotonic_ms <= now_monotonic_ms) {
                memset(&store->slots[i], 0, sizeof(store->slots[i]));
                store->occupied[i] = false;
            }
        }
    }
    *out_changed = changed;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_resolve(
    configuration_runtime_override_store_t *store,
    const configuration_snapshot_t *durable,
    uint64_t now_monotonic_ms,
    configuration_snapshot_t *out_effective,
    uint64_t *out_effective_revision,
    uint32_t *out_active_mask) {
    if (!store_is_valid(store) || !durable || !out_effective ||
        !out_effective_revision || !out_active_mask) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    }
    bool discarded = false;
    configuration_runtime_override_store_result_t result =
        configuration_runtime_override_store_discard_expired(store, now_monotonic_ms, &discarded);
    if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) return result;
    (void)discarded;
    *out_effective = *durable;
    uint32_t mask = 0u;
    for (unsigned i = 0; i < CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT; ++i) {
        if (!store->occupied[i]) continue;
        configuration_snapshot_t next = {0};
        bool active = false;
        if (!configuration_effective_policy_resolve(out_effective, &store->slots[i],
                                                    now_monotonic_ms, &next, &active) || !active) {
            return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
        }
        *out_effective = next;
        mask |= (1u << i);
    }
    *out_effective_revision = store->effective_revision;
    *out_active_mask = mask;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}

configuration_runtime_override_store_result_t
configuration_runtime_override_store_next_expiry(
    const configuration_runtime_override_store_t *store,
    uint64_t *out_expires_at_monotonic_ms) {
    if (!store_is_valid(store) || !out_expires_at_monotonic_ms) {
        return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
    }
    uint64_t earliest = 0u;
    for (unsigned i = 0; i < CONFIGURATION_RUNTIME_OVERRIDE_STORE_SLOT_COUNT; ++i) {
        if (!store->occupied[i]) continue;
        const uint64_t expiry = store->slots[i].expires_at_monotonic_ms;
        if (expiry == 0u) return CONFIGURATION_RUNTIME_OVERRIDE_STORE_INVALID_ARGUMENT;
        if (earliest == 0u || expiry < earliest) earliest = expiry;
    }
    *out_expires_at_monotonic_ms = earliest;
    return CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK;
}
