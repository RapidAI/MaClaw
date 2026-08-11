#include "task_registry.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#define TASK_REGISTRY_MAX_ENTRIES 24u

static SemaphoreHandle_t s_lock;
static task_registry_entry_t s_entries[TASK_REGISTRY_MAX_ENTRIES];
static uint32_t s_count;
static uint32_t s_stop_failures;

/* Lifecycle callers pass one owner-wide deadline to the registry.  The
 * registry itself must obey that deadline as well: using portMAX_DELAY while
 * taking the bookkeeping mutex could otherwise make a nominally bounded
 * startup rollback wait forever behind an unrelated register/unregister
 * caller. */
static TickType_t ticks_for_timeout_ms(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool take_before_deadline(SemaphoreHandle_t lock, TickType_t started,
                                 TickType_t budget) {
    const TickType_t remaining = remaining_ticks(started, budget);
    return remaining != 0 && xSemaphoreTake(lock, remaining) == pdTRUE;
}

static void note_stop_failure_before_deadline(TickType_t started,
                                              TickType_t budget) {
    if (!s_lock || !take_before_deadline(s_lock, started, budget)) return;
    ++s_stop_failures;
    xSemaphoreGive(s_lock);
}

static bool entry_is_valid(const task_registry_entry_t *entry) {
    return entry && entry->struct_size == sizeof(*entry) && entry->owner != 0 &&
           entry->name && entry->name[0] && entry->stop;
}

esp_err_t task_registry_init(void) {
    if (s_lock) return ESP_OK;
    s_lock = xSemaphoreCreateMutex();
    return s_lock ? ESP_OK : ESP_ERR_NO_MEM;
}

esp_err_t task_registry_register(const task_registry_entry_t *entry) {
    if (!entry_is_valid(entry)) return ESP_ERR_INVALID_ARG;
    esp_err_t init_err = task_registry_init();
    if (init_err != ESP_OK) return init_err;
    if (xSemaphoreTake(s_lock, portMAX_DELAY) != pdTRUE) return ESP_ERR_TIMEOUT;
    for (uint32_t i = 0; i < s_count; ++i) {
        if (s_entries[i].owner == entry->owner && s_entries[i].context == entry->context) {
            xSemaphoreGive(s_lock);
            return ESP_ERR_INVALID_STATE;
        }
    }
    if (s_count == TASK_REGISTRY_MAX_ENTRIES) {
        xSemaphoreGive(s_lock);
        return ESP_ERR_NO_MEM;
    }
    s_entries[s_count++] = *entry;
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

void task_registry_unregister(task_registry_owner_t owner, void *context) {
    if (!s_lock || owner == 0) return;
    if (xSemaphoreTake(s_lock, portMAX_DELAY) != pdTRUE) return;
    for (uint32_t i = 0; i < s_count; ++i) {
        if (s_entries[i].owner != owner || s_entries[i].context != context) continue;
        if (i + 1u < s_count) {
            memmove(&s_entries[i], &s_entries[i + 1u],
                    (s_count - i - 1u) * sizeof(s_entries[0]));
        }
        --s_count;
        memset(&s_entries[s_count], 0, sizeof(s_entries[0]));
        break;
    }
    xSemaphoreGive(s_lock);
}

esp_err_t task_registry_unregister_with_timeout(task_registry_owner_t owner,
                                                void *context,
                                                uint32_t timeout_ms) {
    if (owner == 0 || timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_lock) return ESP_OK;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = ticks_for_timeout_ms(timeout_ms);
    if (!take_before_deadline(s_lock, started, budget)) return ESP_ERR_TIMEOUT;
    for (uint32_t i = 0; i < s_count; ++i) {
        if (s_entries[i].owner != owner || s_entries[i].context != context) continue;
        if (i + 1u < s_count) {
            memmove(&s_entries[i], &s_entries[i + 1u],
                    (s_count - i - 1u) * sizeof(s_entries[0]));
        }
        --s_count;
        memset(&s_entries[s_count], 0, sizeof(s_entries[0]));
        break;
    }
    xSemaphoreGive(s_lock);
    return ESP_OK;
}

static esp_err_t stop_matching(task_registry_owner_t owner, bool all, uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_lock) return ESP_OK;

    task_registry_entry_t pending[TASK_REGISTRY_MAX_ENTRIES];
    uint32_t pending_count = 0;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = ticks_for_timeout_ms(timeout_ms);
    if (!take_before_deadline(s_lock, started, budget)) return ESP_ERR_TIMEOUT;
    for (uint32_t i = s_count; i > 0; --i) {
        const task_registry_entry_t *entry = &s_entries[i - 1u];
        if (all || entry->owner == owner) pending[pending_count++] = *entry;
    }
    xSemaphoreGive(s_lock);

    /* `timeout_ms` is an owner-wide deadline, not a fresh allowance for each
     * entry.  Giving every registered worker the full timeout made a nominal
     * 500 ms rollback wait for several seconds as the owner grew.  A failed
     * entry remains registered as before; later entries are still offered the
     * residual budget so independently drainable resources can make progress. */
    esp_err_t first_error = ESP_OK;
    for (uint32_t i = 0; i < pending_count; ++i) {
        const TickType_t remaining = remaining_ticks(started, budget);
        if (remaining == 0) {
            if (first_error == ESP_OK) first_error = ESP_ERR_TIMEOUT;
            note_stop_failure_before_deadline(started, budget);
            break;
        }
        uint32_t remaining_ms = (uint32_t)(remaining * portTICK_PERIOD_MS);
        if (remaining_ms == 0) remaining_ms = 1;
        esp_err_t err = pending[i].stop(pending[i].context, remaining_ms);
        if (err == ESP_OK) {
            /* Do not call the public unregister helper here: it deliberately
             * has no caller deadline because natural worker exit is not a
             * lifecycle transaction.  Rollback must not reintroduce an
             * unbounded wait after a successful child stop. */
            if (take_before_deadline(s_lock, started, budget)) {
                for (uint32_t entry_index = 0; entry_index < s_count; ++entry_index) {
                    if (s_entries[entry_index].owner != pending[i].owner ||
                        s_entries[entry_index].context != pending[i].context) {
                        continue;
                    }
                    if (entry_index + 1u < s_count) {
                        memmove(&s_entries[entry_index], &s_entries[entry_index + 1u],
                                (s_count - entry_index - 1u) * sizeof(s_entries[0]));
                    }
                    --s_count;
                    memset(&s_entries[s_count], 0, sizeof(s_entries[0]));
                    break;
                }
                xSemaphoreGive(s_lock);
            } else if (first_error == ESP_OK) {
                /* The child is stopped, but retaining its entry is safer than
                 * mutating bookkeeping after the parent's deadline expired. */
                first_error = ESP_ERR_TIMEOUT;
            }
        } else {
            if (first_error == ESP_OK) first_error = err;
            note_stop_failure_before_deadline(started, budget);
        }
    }
    return first_error;
}

esp_err_t task_registry_stop_owner(task_registry_owner_t owner, uint32_t timeout_ms) {
    if (owner == 0) return ESP_ERR_INVALID_ARG;
    return stop_matching(owner, false, timeout_ms);
}

esp_err_t task_registry_stop_all(uint32_t timeout_ms) {
    return stop_matching(0, true, timeout_ms);
}

bool task_registry_get_snapshot(task_registry_snapshot_t *out_snapshot) {
    if (!out_snapshot || !s_lock) return false;
    if (xSemaphoreTake(s_lock, portMAX_DELAY) != pdTRUE) return false;
    *out_snapshot = (task_registry_snapshot_t){
        .registered_count = s_count,
        .stop_failures = s_stop_failures,
    };
    xSemaphoreGive(s_lock);
    return true;
}
