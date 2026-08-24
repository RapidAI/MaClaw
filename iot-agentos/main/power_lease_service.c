#include "power_lease_service.h"

#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"

#include "provisioning_failure_injection.h"

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
static bool s_stopping;
/* A physical DISPLAY_OFF commit happens below the Power Service mutex (it may
 * synchronously hand work to Display Service), so it cannot retain `s_lock`.
 * This gate is the explicit substitute: it closes only *new* foreground
 * leases for the short PREPARE -> COMMIT interval. Existing handles cannot
 * become active here because PREPARE first proves there are none. */
static bool s_display_off_commit_in_progress;
static uint32_t s_display_off_commit_generation;
/* This is deliberately independent from the DISPLAY_OFF fence: a future MCU
 * sleep PREPARE has a wider rollback surface, while both fences atomically
 * block a new foreground lease from crossing their final recheck. */
static bool s_system_sleep_prepare_in_progress;
static uint32_t s_system_sleep_prepare_generation;
static device_power_state_t s_system_sleep_prepare_target;

static const char *TAG = "maclaw_power_lease";

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
    if (s_initializing || s_stopping) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_initializing = true;
    /* Keep the slot generations across a fully drained generation. A stale
     * handle from before a later init must not match the first newly issued
     * lease in the same slot. */
    for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
        s_slots[i].active = false;
        s_slots[i].owner = DEVICE_POWER_LEASE_OWNER_NONE;
    }
    s_initialized = true;
    s_initializing = false;
    s_display_off_commit_in_progress = false;
    s_system_sleep_prepare_in_progress = false;
    s_system_sleep_prepare_target = DEVICE_POWER_STATE_ACTIVE;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

void power_lease_service_close_admission(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized || s_stopping) {
        s_initialized = false;
        s_stopping = true;
    }
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t power_lease_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    power_lease_service_close_admission();

    const int64_t deadline_us = esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    for (;;) {
        taskENTER_CRITICAL(&s_lock);
        uint8_t active_count = 0;
        for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
            if (s_slots[i].active) ++active_count;
        }
        const bool stopping = s_stopping;
        /* A DISPLAY_OFF PREPARE has closed acquisition but may still own a
         * synchronous Display Service transaction. Do not declare this lease
         * generation drained merely because it has zero client leases: a
         * subsequent init could otherwise reuse the domain while the old
         * Power worker still holds its commit generation. */
        /* Keep the DISPLAY_OFF fence as a named invariant for the original
         * lifecycle contract; System Sleep PREPARE is a second independent
         * admission fence and must drain by the same rule. */
        const bool commit_in_progress = s_display_off_commit_in_progress;
        const bool system_sleep_prepare_in_progress =
            s_system_sleep_prepare_in_progress;
        taskEXIT_CRITICAL(&s_lock);
        if (!stopping || (active_count == 0 && !commit_in_progress &&
                          !system_sleep_prepare_in_progress)) break;
        if (esp_timer_get_time() >= deadline_us) {
            /* Keep admission closed; existing owners retain the only legal
             * operation (release), while an in-flight Power transaction can
             * still execute its mandatory commit-fence finish. A later drain
             * attempt, not a fresh init, completes this generation. */
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }

    taskENTER_CRITICAL(&s_lock);
    s_initialized = false;
    s_stopping = false;
    s_initializing = false;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t power_lease_service_acquire(device_power_lease_owner_t owner,
                                            device_power_lease_t *out_lease) {
    if (!out_lease || !owner_is_valid(owner)) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_stopping || s_display_off_commit_in_progress ||
        s_system_sleep_prepare_in_progress) {
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

device_status_t power_lease_service_begin_display_off_commit(uint32_t *out_generation) {
    if (!out_generation) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_stopping || s_display_off_commit_in_progress ||
        s_system_sleep_prepare_in_progress) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
        if (s_slots[i].active) {
            taskEXIT_CRITICAL(&s_lock);
            return DEVICE_STATUS_BUSY;
        }
    }
    uint32_t next_generation = s_display_off_commit_generation + 1u;
    if (next_generation == 0) next_generation = 1u;
    s_display_off_commit_generation = next_generation;
    s_display_off_commit_in_progress = true;
    *out_generation = next_generation;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

bool power_lease_service_display_off_commit_is_current(uint32_t generation) {
    if (generation == 0) return false;
    taskENTER_CRITICAL(&s_lock);
    bool current = s_initialized && !s_stopping &&
                   s_display_off_commit_in_progress &&
                   s_display_off_commit_generation == generation;
    if (current) {
        /* This second read is deliberately adjacent to COMMIT.  It protects
         * against lifecycle admission closure after PREPARE.  Acquires are
         * refused while the commit fence is set, so an active lease cannot
         * appear after this check and before the panel transaction begins. */
        for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
            if (s_slots[i].active) {
                current = false;
                break;
            }
        }
    }
    taskEXIT_CRITICAL(&s_lock);
    return current;
}

void power_lease_service_end_display_off_commit(uint32_t generation) {
    if (generation == 0) return;
    taskENTER_CRITICAL(&s_lock);
    if (s_display_off_commit_in_progress &&
        s_display_off_commit_generation == generation) {
        s_display_off_commit_in_progress = false;
    }
    taskEXIT_CRITICAL(&s_lock);
}

device_status_t power_lease_service_begin_system_sleep_prepare(
    device_power_state_t target_state, uint32_t *out_generation) {
    if (!out_generation ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    *out_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_stopping || s_display_off_commit_in_progress ||
        s_system_sleep_prepare_in_progress) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_BUSY;
    }
    for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
        if (s_slots[i].active) {
            taskEXIT_CRITICAL(&s_lock);
            return DEVICE_STATUS_BUSY;
        }
    }
    uint32_t next_generation = s_system_sleep_prepare_generation + 1u;
    if (next_generation == 0) next_generation = 1u;
    s_system_sleep_prepare_generation = next_generation;
    s_system_sleep_prepare_target = target_state;
    s_system_sleep_prepare_in_progress = true;
    *out_generation = next_generation;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

bool power_lease_service_system_sleep_prepare_is_current(
    device_power_state_t target_state, uint32_t generation) {
    if (generation == 0 ||
        (target_state != DEVICE_POWER_STATE_LIGHT_SLEEP &&
         target_state != DEVICE_POWER_STATE_DEEP_SLEEP)) {
        return false;
    }
    taskENTER_CRITICAL(&s_lock);
    bool current = s_initialized && !s_stopping &&
                   s_system_sleep_prepare_in_progress &&
                   s_system_sleep_prepare_generation == generation &&
                   s_system_sleep_prepare_target == target_state;
    if (current) {
        for (size_t i = 0; i < POWER_LEASE_MAX_SLOTS; ++i) {
            if (s_slots[i].active) {
                current = false;
                break;
            }
        }
    }
    taskEXIT_CRITICAL(&s_lock);
    return current;
}

void power_lease_service_end_system_sleep_prepare(uint32_t generation) {
    if (generation == 0) return;
    taskENTER_CRITICAL(&s_lock);
    if (s_system_sleep_prepare_in_progress &&
        s_system_sleep_prepare_generation == generation) {
        s_system_sleep_prepare_in_progress = false;
        s_system_sleep_prepare_target = DEVICE_POWER_STATE_ACTIVE;
    }
    taskEXIT_CRITICAL(&s_lock);
}

bool power_lease_service_get_snapshot(device_power_lease_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    memset(out_snapshot, 0, sizeof(*out_snapshot));
    taskENTER_CRITICAL(&s_lock);
    out_snapshot->initialized = s_initialized && !s_stopping;
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

device_status_t power_lease_service_run_display_off_commit_lifecycle_test(void) {
    if (!provisioning_failure_injection_power_lease_display_off_commit_test_enabled()) {
        return DEVICE_STATUS_OK;
    }

    /* The test runs immediately after Power Service initialization, before
     * App UI can arm an idle timer or any normal foreground owner can exist.
     * It deliberately stops at the lease boundary: panel/DMA/wake behavior is
     * verified by profile HIL separately and must never leak into this shared
     * ownership proof. */
    uint32_t generation = 0;
    device_status_t status = power_lease_service_begin_display_off_commit(&generation);
    if (status != DEVICE_STATUS_OK || generation == 0) {
        ESP_LOGE(TAG, "test: cannot begin DISPLAY_OFF commit (%d)", (int)status);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    status = power_lease_service_acquire(DEVICE_POWER_LEASE_OWNER_VOICE_INTERACTION,
                                         &lease);
    if (status != DEVICE_STATUS_BUSY || lease != DEVICE_POWER_LEASE_INVALID) {
        power_lease_service_end_display_off_commit(generation);
        ESP_LOGE(TAG, "test: foreground acquire crossed commit fence (%d)",
                 (int)status);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (!power_lease_service_display_off_commit_is_current(generation)) {
        power_lease_service_end_display_off_commit(generation);
        ESP_LOGE(TAG, "test: freshly prepared commit is not current");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    /* Close admission while the commit is deliberately outstanding.  A drain
     * must time out rather than allowing a future init to overlap a previous
     * synchronous Display transaction. */
    power_lease_service_close_admission();
    if (power_lease_service_display_off_commit_is_current(generation)) {
        power_lease_service_end_display_off_commit(generation);
        ESP_LOGE(TAG, "test: closed lifecycle still admits commit");
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    status = power_lease_service_deinit(1);
    if (status != DEVICE_STATUS_TIMEOUT) {
        power_lease_service_end_display_off_commit(generation);
        ESP_LOGE(TAG, "test: drain ignored outstanding commit (%d)", (int)status);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    power_lease_service_end_display_off_commit(generation);
    status = power_lease_service_deinit(100);
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "test: cannot drain ended commit (%d)", (int)status);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    status = power_lease_service_init();
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGE(TAG, "test: cannot reopen clean lease generation (%d)", (int)status);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }

    ESP_LOGI(TAG, "test: DISPLAY_OFF commit lifecycle passed");
    return DEVICE_STATUS_OK;
}
