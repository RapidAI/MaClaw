#include "update_service.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "nvs.h" /* ESP_ERR_NVS_NOT_FOUND for legacy-key migration */
#include "persistence_service.h"

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
        esp_err_t err = persistence_service_read_i64(UPDATE_SERVICE_NAMESPACE,
                                                     keys[i], values[i]);
        if (err == ESP_OK) {
            *out_found = true;
        } else if (err != ESP_ERR_NVS_NOT_FOUND) {
            return err;
        }
    }
    size_t digest_size = sizeof(store->manifest_sha256);
    esp_err_t err = persistence_service_read_string(UPDATE_SERVICE_NAMESPACE,
                                                    "upd_digest", store->manifest_sha256,
                                                    &digest_size);
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) {
        return err;
    }
    digest_size = sizeof(store->dismissed_digest);
    err = persistence_service_read_string(UPDATE_SERVICE_NAMESPACE,
                                          "upd_ddigest", store->dismissed_digest,
                                          &digest_size);
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) {
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
    esp_err_t err = persistence_service_read_blob(UPDATE_SERVICE_NAMESPACE,
                                                  UPDATE_SERVICE_STORE_KEY,
                                                  store, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy_store(store, &legacy_found);
        if (err != ESP_OK) return err;
        if (legacy_found) {
            err = persistence_service_write_blob(UPDATE_SERVICE_NAMESPACE,
                                                 UPDATE_SERVICE_STORE_KEY,
                                                 store, sizeof(*store));
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
    return persistence_service_write_blob(UPDATE_SERVICE_NAMESPACE,
                                          UPDATE_SERVICE_STORE_KEY,
                                          store, sizeof(*store));
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
    s_running_release_sequence = config->running_release_sequence;
    memset(&s_status, 0, sizeof(s_status));
    return ESP_OK;
}

bool update_service_apply_metadata(cJSON *metadata, int64_t now_epoch,
                                   bool defer_presentation) {
    // The caller defers only *presentation* during cold startup.  State must
    // still be retained so the Welcome sequence can publish it once it has
    // released ownership of the screen.
    (void)defer_presentation;
    if (!cJSON_IsObject(metadata) ||
        !cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "requiresComputer"))) {
        clear_update_status();
        return false;
    }
    if (!cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "available"))) {
        // An authenticated Hub explicitly says there is no applicable update.
        // Clear the transient in-RAM view, but retain the bounded NVS history:
        // a catalog/network outage must never erase a user's defer preference.
        clear_update_status();
        return false;
    }
    if (cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "withdrawn"))) {
        clear_update_status();
        return false;
    }
    cJSON *sequence_item = cJSON_GetObjectItemCaseSensitive(metadata, "releaseSequence");
    if (!cJSON_IsNumber(sequence_item) || sequence_item->valuedouble <= s_running_release_sequence ||
        sequence_item->valuedouble > INT64_MAX || sequence_item->valuedouble != (double)(int64_t)sequence_item->valuedouble) return false;
    update_service_status_t next = {0};
    next.available = true;
    next.release_sequence = (int64_t)sequence_item->valuedouble;
    next.critical = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(metadata, "critical"));
    if (!metadata_string(metadata, "displayVersion", next.display_version, sizeof(next.display_version)) ||
        !metadata_string(metadata, "releaseTag", next.release_tag, sizeof(next.release_tag)) ||
        !metadata_string(metadata, "manifestSha256", next.manifest_sha256, sizeof(next.manifest_sha256)) ||
        !valid_digest(next.manifest_sha256)) return false;

    update_service_store_t store;
    if (load_store(&store) != ESP_OK) return false;
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
    if (save_store(&store) != ESP_OK) return false;

    s_status = next;
    if (!deferred && (release_changed || due)) set_pending_presentation();
    return s_status.pending_presentation;
}

bool update_service_take_pending_presentation(char *title, size_t title_size,
                                              char *detail, size_t detail_size) {
    if (!s_status.pending_presentation || !title || !detail || title_size == 0 || detail_size == 0) return false;
    strlcpy(title, s_status.title, title_size);
    strlcpy(detail, s_status.detail, detail_size);
    s_status.pending_presentation = false;
    return true;
}

void update_service_get_status(update_service_status_t *out) {
    if (out) *out = s_status;
}

esp_err_t update_service_execute_tool(const char *name, cJSON *arguments,
                                      cJSON **out_result, char *error,
                                      size_t error_size) {
    if (!name || !out_result || !cJSON_IsObject(arguments)) return ESP_ERR_INVALID_ARG;
    *out_result = NULL;
    cJSON *result = cJSON_CreateObject();
    if (!result) return ESP_ERR_NO_MEM;
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
            return ESP_ERR_INVALID_STATE;
        }
        update_service_store_t store;
        if (load_store(&store) != ESP_OK) {
            cJSON_Delete(result);
            return ESP_ERR_TIMEOUT;
        }
        int64_t now = (int64_t)time(NULL);
        if (now < 1672531200) {
            snprintf(error, error_size, "trusted wall clock is unavailable");
            cJSON_Delete(result);
            return ESP_ERR_INVALID_STATE;
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
            return ESP_FAIL;
        }
        s_status.remind_after_epoch = store.remind_after_epoch;
        s_status.pending_presentation = false;
        cJSON_AddNumberToObject(result, "remindAfterEpoch", (double)store.remind_after_epoch);
        cJSON_AddBoolToObject(result, "dismissed", !s_status.critical && !strcmp(name, "update_dismiss_version"));
    } else {
        snprintf(error, error_size, "unsupported client tool: %s", name);
        cJSON_Delete(result);
        return ESP_ERR_NOT_SUPPORTED;
    }
    *out_result = result;
    return ESP_OK;
}
