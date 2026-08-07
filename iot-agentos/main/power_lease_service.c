#include "power_lease_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"

#define POWER_LEASE_MAX_SLOTS 8u

typedef struct {
    uint32_t generation;
    device_power_lease_owner_t owner;
    bool active;
} power_lease_slot_t;

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static power_lease_slot_t s_slots[POWER_LEASE_MAX_SLOTS];
static bool s_initialized;
static bool s_initializing;

static bool owner_is_valid(device_power_lease_owner_t owner) {
    return owner > DEVICE_POWER_LEASE_OWNER_NONE &&
           owner < DEVICE_POWER_LEASE_OWNER_COUNT;
}

static device_power_lease_t make_lease(size_t slot, uint32_t generation) {
    /* Reserve zero as invalid.  Slot lives in the low byte, so stale handles
     * cannot release a later owner even when that slot is reused. */
    return ((device_power_lease_t)generation << 8) | (device_power_lease_t)(slot + 1u);
}

device_status_t power_lease_service_init(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    if (s_initializing) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_initializing = true;
    memset(s_slots, 0, sizeof(s_slots));
    s_initialized = true;
    s_initializing = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t power_lease_service_acquire(device_power_lease_owner_t owner,
                                            device_power_lease_t *out_lease) {
    if (!out_lease || !owner_is_valid(owner)) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
        power_lease_slot_t *slot = &s_slots[i];
        if (slot->active) continue;
        uint32_t next_generation = slot->generation + 1u;
        if (next_generation == 0) next_generation = 1u;
        slot->generation = next_generation;
        slot->owner = owner;
        slot->active = true;
        *out_lease = make_lease(i, next_generation);
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_RESOURCE_EXHAUSTED;
}

void power_lease_service_release(device_power_lease_t lease) {
    if (lease == DEVICE_POWER_LEASE_INVALID) return;
    size_t slot_index = (size_t)((lease & 0xffu) - 1u);
    uint32_t generation = lease >> 8;
    if (slot_index >= POWER_LEASE_MAX_SLOTS || generation == 0) return;
    taskENTER_CRITICAL(&s_lock);
    power_lease_slot_t *slot = &s_slots[slot_index];
    if (slot->active && slot->generation == generation) {
        slot->active = false;
        slot->owner = DEVICE_POWER_LEASE_OWNER_NONE;
    }
    taskEXIT_CRITICAL(&s_lock);
}

bool power_lease_service_allows_display_off(void) {
    bool allowed = true;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized) {
        allowed = false;
    } else {
        for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
            if (s_slots[i].active) {
                allowed = false;
                break;
            }
        }
    }
    taskEXIT_CRITICAL(&s_lock);
    return allowed;
}

bool power_lease_service_get_snapshot(device_power_lease_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    memset(out_snapshot, 0, sizeof(*out_snapshot));
    taskENTER_CRITICAL(&s_lock);
    out_snapshot->initialized = s_initialized;
    for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
        if (!s_slots[i].active) continue;
        ++out_snapshot->active_count;
        if (s_slots[i].owner < DEVICE_POWER_LEASE_OWNER_COUNT) {
            out_snapshot->owner_mask |= (uint32_t)1u << s_slots[i].owner;
        }
    }
    taskEXIT_CRITICAL(&s_lock);
    return out_snapshot->initialized;
}
