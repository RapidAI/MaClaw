#include "update_service.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "persistence_service.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "esp_timer.h"

#define UPDATE_SERVICE_NAMESPACE "maclaw"
#define UPDATE_SERVICE_STORE_KEY "update_meta"
#define UPDATE_SERVICE_STORE_MAGIC 0x55504431u /* UPD1 */
#define UPDATE_SERVICE_STORE_VERSION 1u
#define UPDATE_SERVICE_MIN_INTERVAL_SECONDS (5 * 60)
#define UPDATE_SERVICE_MAX_INTERVAL_SECONDS (7 * 24 * 60 * 60)
#define UPDATE_SERVICE_DEFAULT_INTERVAL_SECONDS (6 * 60 * 60)
#define UPDATE_SERVICE_CRITICAL_INTERVAL_SECONDS (30 * 60)
#define UPDATE_SERVICE_MAX_DISMISS_SECONDS (7 * 24 * 60 * 60)

typedef struct {
    uint32_t magic;
    uint32_t version;
    int64_t release_sequence;
    int64_t remind_after_epoch;
    int64_t dismissed_sequence;
    int64_t dismissed_until_epoch;
    char manifest_sha256[UPDATE_SERVICE_DIGEST_CAPACITY];
    char dismissed_digest[UPDATE_SERVICE_DIGEST_CAPACITY];
} update_service_store_t;

static int64_t s_running_release_sequence;
static update_service_status_t s_status;
/* Metadata delivery and tool execution can be dispatched by separate tasks.
 * Admission tracks callers which may touch Persistence, while this permanent
 * mutex serializes the one shared update-status/store transaction. */
static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_operation_mutex;
/* Retained deinit shell serializes a stop transaction with init. It is not
 * deleted because an already-admitted caller may still be contending for the
 * operation mutex while rollback closes admission. */
static SemaphoreHandle_t s_deinit_mutex;
static bool s_initialized;
static bool s_stopping;
static volatile bool s_initializing;
static uint32_t s_active_calls;

static TickType_t update_stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t update_stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool admission_enter(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping && s_operation_mutex) {
        ++s_active_calls;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void admission_exit(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_active_calls > 0) --s_active_calls;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static bool operation_enter(void) {
    if (!s_operation_mutex ||
        xSemaphoreTake(s_operation_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return false;
    }
    /* A caller can pass admission and then wait here while rollback closes
     * the service. Do not let it access/update the retained status or NVS
     * state after that boundary. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool admitted = s_initialized && !s_stopping;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!admitted) xSemaphoreGive(s_operation_mutex);
    return admitted;
}

static void operation_exit(void) {
    if (s_operation_mutex) xSemaphoreGive(s_operation_mutex);
}

static bool metadata_string(cJSON *object, const char *key, char *out, size_t out_size) {
    cJSON *item = cJSON_GetObjectItemCaseSensitive(object, key);
    if (!cJSON_IsString(item) || !item->valuestring || strlen(item->valuestring) >= out_size) return false;
    strlcpy(out, item->valuestring, out_size);
    return true;
}

static bool valid_digest(const char *digest) {
    if (!digest || strncmp(digest, "sha256:", 7) != 0 || strlen(digest) != 71) return false;
    for (const char *p = digest + 7; *p; ++p) {
        if (!((*p >= '0' && *p <= '9') || (*p >= 'a' && *p <= 'f'))) return false;
    }
    return true;
}

static void reset_store(update_service_store_t *store) {
    memset(store, 0, sizeof(*store));
    store->magic = UPDATE_SERVICE_STORE_MAGIC;
    store->version = UPDATE_SERVICE_STORE_VERSION;
}

/* Versions before Persistence Service stored this state as independent NVS
 * keys.  Import it exactly once into a versioned blob; failure to read an
 * individual old key means that field simply used its original zero/default
 * value.  This is safe because legacy writers are no longer linked. */
static esp_err_t load_legacy_store(update_service_store_t *store, bool *out_found) {
    if (!store || !out_found) return ESP_ERR_INVALID_ARG;
    *out_found = false;
    const char *keys[] = {"upd_seq", "upd_after", "upd_dseq", "upd_duntil"};
    int64_t *values[] = {&store->release_sequence, &store->remind_after_epoch,
                         &store->dismissed_sequence, &store->dismissed_until_epoch};
    for (size_t i = 0; i < sizeof(keys) / sizeof(keys[0]); ++i) {
        esp_err_t err = device_status_to_platform_error(persistence_service_read_i64(UPDATE_SERVICE_NAMESPACE,
                                                     keys[i], values[i]));
        if (err == ESP_OK) {
            *out_found = true;
        } else if (err != ESP_ERR_NOT_FOUND) {
            return err;
        }
    }
    size_t digest_size = sizeof(store->manifest_sha256);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_string(UPDATE_SERVICE_NAMESPACE,
                                                    "upd_digest", store->manifest_sha256,
                                                    &digest_size));
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) {
        return err;
    }
    digest_size = sizeof(store->dismissed_digest);
    err = device_status_to_platform_error(persistence_service_read_string(UPDATE_SERVICE_NAMESPACE,
                                          "upd_ddigest", store->dismissed_digest,
                                          &digest_size));
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) {
        return err;
    }
    return ESP_OK;
}

/* A missing state is the normal first boot.  A non-empty, malformed state is
 * deliberately rejected instead of silently dropping the user's reminder
 * preference. */
static esp_err_t load_store(update_service_store_t *store) {
    if (!store || !persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    reset_store(store);
    size_t size = sizeof(*store);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(UPDATE_SERVICE_NAMESPACE,
                                                  UPDATE_SERVICE_STORE_KEY,
                                                  store, &size));
    if (err == ESP_ERR_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy_store(store, &legacy_found);
        if (err != ESP_OK) return err;
        if (legacy_found) {
            err = device_status_to_platform_error(persistence_service_write_blob(UPDATE_SERVICE_NAMESPACE,
                                                 UPDATE_SERVICE_STORE_KEY,
                                                 store, sizeof(*store)));
            if (err != ESP_OK) return err;
        }
        return ESP_OK;
    }
    if (err != ESP_OK) return err;
    if (size != sizeof(*store) || store->magic != UPDATE_SERVICE_STORE_MAGIC ||
        store->version != UPDATE_SERVICE_STORE_VERSION) return ESP_ERR_INVALID_STATE;
    return ESP_OK;
}

static esp_err_t save_store(const update_service_store_t *store) {
    if (!store || store->magic != UPDATE_SERVICE_STORE_MAGIC ||
        store->version != UPDATE_SERVICE_STORE_VERSION) return ESP_ERR_INVALID_ARG;
    return device_status_to_platform_error(persistence_service_write_blob(UPDATE_SERVICE_NAMESPACE,
                                          UPDATE_SERVICE_STORE_KEY,
                                          store, sizeof(*store)));
}

static int64_t reminder_interval(cJSON *metadata, bool critical) {
    cJSON *item = metadata ? cJSON_GetObjectItemCaseSensitive(metadata, "checkAfterSeconds") : NULL;
    int64_t value = cJSON_IsNumber(item) && item->valuedouble > 0 && item->valuedouble <= INT64_MAX
                        ? (int64_t)item->valuedouble
                        : (critical ? UPDATE_SERVICE_CRITICAL_INTERVAL_SECONDS : UPDATE_SERVICE_DEFAULT_INTERVAL_SECONDS);
    if (value < UPDATE_SERVICE_MIN_INTERVAL_SECONDS) value = UPDATE_SERVICE_MIN_INTERVAL_SECONDS;
    if (value > UPDATE_SERVICE_MAX_INTERVAL_SECONDS) value = UPDATE_SERVICE_MAX_INTERVAL_SECONDS;
    return value;
}

static void set_pending_presentation(void) {
    strlcpy(s_status.title, "发现新版本", sizeof(s_status.title));
    snprintf(s_status.detail, sizeof(s_status.detail),
             "新版本 %.*s，请连接电脑使用 ClawMate Maker 更新",
             48, s_status.display_version);
    s_status.pending_presentation = true;
}

static void clear_update_status(void) {
    memset(&s_status, 0, sizeof(s_status));
}

esp_err_t update_service_init(const update_service_config_t *config) {
    if (!config || !persistence_service_is_initialized() ||
        config->running_release_sequence < 0) return ESP_ERR_INVALID_ARG;
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_operation_mutex) s_operation_mutex = xSemaphoreCreateMutex();
    if (!s_deinit_mutex) s_deinit_mutex = xSemaphoreCreateMutex();
    if (!s_operation_mutex || !s_deinit_mutex) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_NO_MEM;
    }
    if (xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        xSemaphoreGive(s_deinit_mutex);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_OK;
    }
    if (s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        xSemaphoreGive(s_deinit_mutex);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_INVALID_STATE;
    }
    s_running_release_sequence = config->running_release_sequence;
    memset(&s_status, 0, sizeof(s_status));
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    xSemaphoreGive(s_deinit_mutex);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return ESP_OK;
}

esp_err_t update_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = update_stop_timeout_ticks(timeout_ms);
    while (__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE)) {
        if (update_stop_remaining_ticks(started, budget) == 0) return ESP_ERR_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    /* Allocation can fail before Update Service publishes its retained
     * lifecycle shell. That is an idempotent no-op for startup rollback, not
     * a timeout that blocks teardown of the services which did start. */
    if (!s_deinit_mutex) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const bool live = s_initialized || s_stopping || s_active_calls != 0;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        return live ? ESP_ERR_INVALID_STATE : ESP_OK;
    }
    const TickType_t remaining = update_stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_deinit_mutex, remaining) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* Close admission before Persistence stops.  An already admitted metadata
     * or tool transaction owns the one remaining legal path to its NVS state,
     * and must finish before the Persistence boundary is torn down. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_initialized = false;
    s_stopping = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t active_calls = s_active_calls;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (active_calls == 0) break;
        if (update_stop_remaining_ticks(started, budget) == 0) {
            xSemaphoreGive(s_deinit_mutex);
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_running_release_sequence = 0;
    memset(&s_status, 0, sizeof(s_status));
    s_stopping = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    xSemaphoreGive(s_deinit_mutex);
    return ESP_OK;
}

bool update_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool initialized = s_initialized && !s_stopping &&
                             !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE);
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return initialized;
}

bool update_service_apply_metadata(cJSON *metadata, int64_t now_epoch,
                                   bool defer_presentation) {
    // The caller defers only *presentation* during cold startup.  State must
    // still be retained so the Welcome sequence can publish it once it has
    // released ownership of the screen.
    (void)defer_presentation;
    if (!admission_enter()) return false;
    if (!operation_enter()) {
        admission_exit();
        return false;
    }
    bool present = false;
    if (!cJSON_IsObject(metadata) ||
        !cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "requiresComputer"))) {
        clear_update_status();
        goto done;
    }
    if (!cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "available"))) {
        // An authenticated Hub explicitly says there is no applicable update.
        // Clear the transient in-RAM view, but retain the bounded NVS history:
        // a catalog/network outage must never erase a user's defer preference.
        clear_update_status();
        goto done;
    }
    if (cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "withdrawn"))) {
        clear_update_status();
        goto done;
    }
    cJSON *sequence_item = cJSON_GetObjectItemCaseSensitive(metadata, "releaseSequence");
    if (!cJSON_IsNumber(sequence_item) || sequence_item->valuedouble <= s_running_release_sequence ||
        sequence_item->valuedouble > INT64_MAX || sequence_item->valuedouble != (double)(int64_t)sequence_item->valuedouble) goto done;
    update_service_status_t next = {0};
    next.available = true;
    next.release_sequence = (int64_t)sequence_item->valuedouble;
    next.critical = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "critical"));
    if (!metadata_string(metadata, "displayVersion", next.display_version, sizeof(next.display_version)) ||
        !metadata_string(metadata, "releaseTag", next.release_tag, sizeof(next.release_tag)) ||
        !metadata_string(metadata, "manifestSha256", next.manifest_sha256, sizeof(next.manifest_sha256)) ||
        !valid_digest(next.manifest_sha256)) goto done;

    update_service_store_t store;
    if (load_store(&store) != ESP_OK) goto done;
    const bool release_changed = store.release_sequence != next.release_sequence ||
                                 strcmp(store.manifest_sha256, next.manifest_sha256) != 0;
    const bool dismiss_matches = store.dismissed_sequence == next.release_sequence &&
                                 strcmp(store.dismissed_digest, next.manifest_sha256) == 0;
    // Critical releases are never permanently dismissed. Their tool action
    // writes the short critical interval into remind_after, which still avoids
    // a notice on every handshake.
    const bool deferred = dismiss_matches && now_epoch > 0 &&
                          store.dismissed_until_epoch > now_epoch;
    const bool due = now_epoch == 0 || store.remind_after_epoch <= now_epoch;
    next.reminder_interval_seconds = reminder_interval(metadata, next.critical);
    // Do not extend a future deadline on every poll/handshake.  Apart from
    // producing confusing status, that would let a busy connection postpone a
    // reminder forever.  Advance it only when this is a new digest/sequence or
    // when the already-persisted deadline is actually due.
    next.remind_after_epoch = (now_epoch > 0 && (release_changed || due))
                                  ? now_epoch + next.reminder_interval_seconds
                                  : store.remind_after_epoch;

    // A changed manifest digest invalidates a previous dismiss, even where the
    // release sequence was accidentally reused by a publisher.
    if (release_changed) {
        store.dismissed_sequence = 0;
        store.dismissed_until_epoch = 0;
        store.dismissed_digest[0] = '\0';
    }
    store.release_sequence = next.release_sequence;
    store.remind_after_epoch = next.remind_after_epoch;
    strlcpy(store.manifest_sha256, next.manifest_sha256, sizeof(store.manifest_sha256));
    if (save_store(&store) != ESP_OK) goto done;

    s_status = next;
    if (!deferred && (release_changed || due)) set_pending_presentation();
    present = s_status.pending_presentation;
done:
    operation_exit();
    admission_exit();
    return present;
}

bool update_service_take_pending_presentation(char *title, size_t title_size,
                                              char *detail, size_t detail_size) {
    if (!admission_enter()) return false;
    if (!operation_enter()) {
        admission_exit();
        return false;
    }
    bool taken = false;
    if (!s_status.pending_presentation || !title || !detail ||
        title_size == 0 || detail_size == 0) goto done;
    strlcpy(title, s_status.title, title_size);
    strlcpy(detail, s_status.detail, detail_size);
    s_status.pending_presentation = false;
    taken = true;
done:
    operation_exit();
    admission_exit();
    return taken;
}

void update_service_get_status(update_service_status_t *out) {
    if (!out) return;
    if (!admission_enter()) {
        memset(out, 0, sizeof(*out));
        return;
    }
    if (operation_enter()) {
        *out = s_status;
        operation_exit();
    } else {
        memset(out, 0, sizeof(*out));
    }
    admission_exit();
}

esp_err_t update_service_execute_tool(const char *name, cJSON *arguments,
                                      cJSON **out_result, char *error,
                                      size_t error_size) {
    if (!name || !out_result || !cJSON_IsObject(arguments)) return ESP_ERR_INVALID_ARG;
    if (!admission_enter()) return ESP_ERR_INVALID_STATE;
    if (!operation_enter()) {
        admission_exit();
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t status = ESP_OK;
    *out_result = NULL;
    cJSON *result = cJSON_CreateObject();
    if (!result) {
        status = ESP_ERR_NO_MEM;
        goto done;
    }
    if (!strcmp(name, "update_status") || !strcmp(name, "update_check")) {
        cJSON_AddBoolToObject(result, "available", s_status.available);
        cJSON_AddBoolToObject(result, "requiresComputer", true);
        if (s_status.available) {
            cJSON_AddNumberToObject(result, "releaseSequence", (double)s_status.release_sequence);
            cJSON_AddStringToObject(result, "displayVersion", s_status.display_version);
            cJSON_AddStringToObject(result, "releaseTag", s_status.release_tag);
            cJSON_AddStringToObject(result, "manifestSha256", s_status.manifest_sha256);
            cJSON_AddBoolToObject(result, "critical", s_status.critical);
            cJSON_AddNumberToObject(result, "remindAfterEpoch", (double)s_status.remind_after_epoch);
        }
    } else if (!strcmp(name, "update_remind_later") || !strcmp(name, "update_dismiss_version")) {
        if (!s_status.available) {
            snprintf(error, error_size, "no update is available");
            cJSON_Delete(result);
            status = ESP_ERR_INVALID_STATE;
            goto done;
        }
        update_service_store_t store;
        if (load_store(&store) != ESP_OK) {
            cJSON_Delete(result);
            status = ESP_ERR_TIMEOUT;
            goto done;
        }
        int64_t now = (int64_t)time(NULL);
        if (now < 1672531200) {
            snprintf(error, error_size, "trusted wall clock is unavailable");
            cJSON_Delete(result);
            status = ESP_ERR_INVALID_STATE;
            goto done;
        }
        int64_t defer = !strcmp(name, "update_remind_later")
                            ? (s_status.critical ? UPDATE_SERVICE_CRITICAL_INTERVAL_SECONDS
                                                 : s_status.reminder_interval_seconds)
                            : UPDATE_SERVICE_MAX_DISMISS_SECONDS;
        if (s_status.critical && !strcmp(name, "update_dismiss_version")) defer = UPDATE_SERVICE_CRITICAL_INTERVAL_SECONDS;
        store.dismissed_sequence = s_status.release_sequence;
        strlcpy(store.dismissed_digest, s_status.manifest_sha256, sizeof(store.dismissed_digest));
        store.dismissed_until_epoch = now + defer;
        store.remind_after_epoch = store.dismissed_until_epoch;
        if (save_store(&store) != ESP_OK) {
            cJSON_Delete(result);
            status = ESP_FAIL;
            goto done;
        }
        s_status.remind_after_epoch = store.remind_after_epoch;
        s_status.pending_presentation = false;
        cJSON_AddNumberToObject(result, "remindAfterEpoch", (double)store.remind_after_epoch);
        cJSON_AddBoolToObject(result, "dismissed", !s_status.critical && !strcmp(name, "update_dismiss_version"));
    } else {
        snprintf(error, error_size, "unsupported client tool: %s", name);
        cJSON_Delete(result);
        status = ESP_ERR_NOT_SUPPORTED;
        goto done;
    }
    *out_result = result;
done:
    operation_exit();
    admission_exit();
    return status;
}
