#include "task_registry.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "esp_log.h"
#include "sdkconfig.h"

#define TASK_REGISTRY_MAX_ENTRIES 24u

static SemaphoreHandle_t s_lock;
static task_registry_entry_t s_entries[TASK_REGISTRY_MAX_ENTRIES];
static uint32_t s_count;
static uint32_t s_stop_failures;

#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_TASK_REGISTRY_LIFECYCLE_TEST
#define TASK_REGISTRY_TEST_TAG "task_registry_test"
#define TASK_REGISTRY_TEST_CONTENTION_TIMEOUT_MS 40u
#define TASK_REGISTRY_TEST_CONTENTION_HOLD_MS 120u
#define TASK_REGISTRY_TEST_MIXED_TIMEOUT_MS 100u
#define TASK_REGISTRY_TEST_SETTLE_MS 300u

typedef enum {
    TASK_REGISTRY_TEST_STOP_OK = 0,
    TASK_REGISTRY_TEST_STOP_ERROR_ONCE,
    TASK_REGISTRY_TEST_STOP_TIMEOUT_ONCE,
} task_registry_test_stop_behavior_t;

typedef struct {
    task_registry_test_stop_behavior_t behavior;
    uint32_t calls;
    uint32_t first_timeout_ms;
} task_registry_test_stop_context_t;

static StaticSemaphore_t s_test_lock_acquired_storage;
static StaticSemaphore_t s_test_lock_released_storage;
static SemaphoreHandle_t s_test_lock_acquired;
static SemaphoreHandle_t s_test_lock_released;
static StaticTask_t s_test_lock_holder_storage;
static StackType_t s_test_lock_holder_stack[2048u];
static bool s_lifecycle_test_has_run;
#endif

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

#if CONFIG_MACLAW_TEST_BUILD && CONFIG_MACLAW_TASK_REGISTRY_LIFECYCLE_TEST
/* The test task owns the same mutex as a concurrent register/unregister
 * caller would. It deliberately avoids touching registry entries: the test
 * validates that a caller-owned stop deadline remains bounded before a
 * snapshot can even be taken. */
static void task_registry_test_lock_holder(void *unused) {
    (void)unused;
    if (s_lock && xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
        (void)xSemaphoreGive(s_test_lock_acquired);
        TickType_t hold_ticks = pdMS_TO_TICKS(TASK_REGISTRY_TEST_CONTENTION_HOLD_MS);
        if (hold_ticks == 0) hold_ticks = 1;
        vTaskDelay(hold_ticks);
        xSemaphoreGive(s_lock);
    }
    (void)xSemaphoreGive(s_test_lock_released);
    vTaskDelete(NULL);
}

static esp_err_t task_registry_test_stop(void *context, uint32_t timeout_ms) {
    task_registry_test_stop_context_t *test = context;
    if (!test || timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ++test->calls;
    if (test->calls == 1u) test->first_timeout_ms = timeout_ms;
    if (test->behavior == TASK_REGISTRY_TEST_STOP_TIMEOUT_ONCE && test->calls == 1u) {
        /* Consume a known fraction of the passed residual budget. This proves
         * subsequent callbacks receive the parent transaction remainder,
         * without intentionally exceeding it (a child that ignores its own
         * deadline cannot be forcibly interrupted by this registry). */
        TickType_t delay_ticks = pdMS_TO_TICKS(20u);
        if (delay_ticks == 0) delay_ticks = 1;
        vTaskDelay(delay_ticks);
        return ESP_ERR_TIMEOUT;
    }
    if (test->behavior == TASK_REGISTRY_TEST_STOP_ERROR_ONCE && test->calls == 1u) {
        return ESP_FAIL;
    }
    return ESP_OK;
}

static bool task_registry_test_register(task_registry_owner_t owner, const char *name,
                                        task_registry_test_stop_context_t *context) {
    return task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = owner,
        .name = name,
        .context = context,
        .stop = task_registry_test_stop,
    }) == ESP_OK;
}

esp_err_t task_registry_run_lifecycle_test(void) {
    if (!s_lock || s_lifecycle_test_has_run) return ESP_ERR_INVALID_STATE;
    s_lifecycle_test_has_run = true;

    s_test_lock_acquired = xSemaphoreCreateBinaryStatic(&s_test_lock_acquired_storage);
    s_test_lock_released = xSemaphoreCreateBinaryStatic(&s_test_lock_released_storage);
    if (!s_test_lock_acquired || !s_test_lock_released) return ESP_ERR_NO_MEM;
    (void)xSemaphoreTake(s_test_lock_acquired, 0);
    (void)xSemaphoreTake(s_test_lock_released, 0);
    TaskHandle_t holder = xTaskCreateStatic(task_registry_test_lock_holder,
                                             "registry_lock_test",
                                             sizeof(s_test_lock_holder_stack) /
                                                 sizeof(s_test_lock_holder_stack[0]),
                                             NULL, tskIDLE_PRIORITY + 1u,
                                             s_test_lock_holder_stack,
                                             &s_test_lock_holder_storage);
    if (!holder || xSemaphoreTake(s_test_lock_acquired,
                                  ticks_for_timeout_ms(TASK_REGISTRY_TEST_SETTLE_MS)) != pdTRUE) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG, "FAIL contention holder did not acquire registry mutex");
        return ESP_FAIL;
    }
    const TickType_t contention_started = xTaskGetTickCount();
    const esp_err_t contention_result =
        task_registry_stop_all(TASK_REGISTRY_TEST_CONTENTION_TIMEOUT_MS);
    const TickType_t contention_elapsed = xTaskGetTickCount() - contention_started;
    const TickType_t contention_budget =
        ticks_for_timeout_ms(TASK_REGISTRY_TEST_CONTENTION_TIMEOUT_MS);
    const bool bounded_contention = contention_result == ESP_ERR_TIMEOUT &&
        contention_elapsed <= contention_budget + 1u;
    if (xSemaphoreTake(s_test_lock_released,
                       ticks_for_timeout_ms(TASK_REGISTRY_TEST_SETTLE_MS)) != pdTRUE) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG, "FAIL contention holder did not release registry mutex");
        return ESP_FAIL;
    }
    if (!bounded_contention) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG,
                 "FAIL contention result=%s elapsed_ticks=%lu budget_ticks=%lu",
                 esp_err_to_name(contention_result), (unsigned long)contention_elapsed,
                 (unsigned long)contention_budget);
        return ESP_FAIL;
    }

    task_registry_test_stop_context_t success = {.behavior = TASK_REGISTRY_TEST_STOP_OK};
    task_registry_test_stop_context_t error = {.behavior = TASK_REGISTRY_TEST_STOP_ERROR_ONCE};
    task_registry_test_stop_context_t timeout = {.behavior = TASK_REGISTRY_TEST_STOP_TIMEOUT_ONCE};
    if (!task_registry_test_register(TASK_REGISTRY_OWNER_AUDIO, "test_success", &success) ||
        !task_registry_test_register(TASK_REGISTRY_OWNER_POWER, "test_error", &error) ||
        !task_registry_test_register(TASK_REGISTRY_OWNER_BOARD, "test_timeout", &timeout)) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG, "FAIL could not register multi-owner test entries");
        return ESP_FAIL;
    }
    const TickType_t mixed_started = xTaskGetTickCount();
    const esp_err_t mixed_result = task_registry_stop_all(TASK_REGISTRY_TEST_MIXED_TIMEOUT_MS);
    const TickType_t mixed_elapsed = xTaskGetTickCount() - mixed_started;
    const TickType_t mixed_budget = ticks_for_timeout_ms(TASK_REGISTRY_TEST_MIXED_TIMEOUT_MS);
    task_registry_snapshot_t snapshot = {0};
    const bool got_snapshot = task_registry_get_snapshot(&snapshot);
    const bool mixed_contract = mixed_result == ESP_ERR_TIMEOUT &&
        timeout.calls == 1u && error.calls == 1u && success.calls == 1u &&
        timeout.first_timeout_ms >= error.first_timeout_ms &&
        error.first_timeout_ms >= success.first_timeout_ms &&
        mixed_elapsed <= mixed_budget + 1u && got_snapshot &&
        snapshot.registered_count == 2u && snapshot.stop_failures == 2u;
    if (!mixed_contract) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG,
                 "FAIL mixed result=%s calls=%lu/%lu/%lu budgets=%lu/%lu/%lu elapsed=%lu/%lu entries=%lu failures=%lu",
                 esp_err_to_name(mixed_result), (unsigned long)timeout.calls,
                 (unsigned long)error.calls, (unsigned long)success.calls,
                 (unsigned long)timeout.first_timeout_ms, (unsigned long)error.first_timeout_ms,
                 (unsigned long)success.first_timeout_ms, (unsigned long)mixed_elapsed,
                 (unsigned long)mixed_budget, (unsigned long)snapshot.registered_count,
                 (unsigned long)snapshot.stop_failures);
        return ESP_FAIL;
    }
    if (task_registry_stop_all(TASK_REGISTRY_TEST_MIXED_TIMEOUT_MS) != ESP_OK ||
        !task_registry_get_snapshot(&snapshot) || snapshot.registered_count != 0u ||
        snapshot.stop_failures != 2u) {
        ESP_LOGE(TASK_REGISTRY_TEST_TAG, "FAIL retained entries did not clean up safely");
        return ESP_FAIL;
    }
    ESP_LOGI(TASK_REGISTRY_TEST_TAG,
             "PASS mutex contention + multi-owner mixed stop; elapsed_ticks=%lu/%lu budgets=%lu/%lu/%lu",
             (unsigned long)mixed_elapsed, (unsigned long)mixed_budget,
             (unsigned long)timeout.first_timeout_ms, (unsigned long)error.first_timeout_ms,
             (unsigned long)success.first_timeout_ms);
    return ESP_OK;
}
#else
esp_err_t task_registry_run_lifecycle_test(void) {
    return ESP_ERR_NOT_SUPPORTED;
}
#endif
