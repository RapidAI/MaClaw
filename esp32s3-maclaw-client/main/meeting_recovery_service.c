#include "meeting_recovery_service.h"

#include <string.h>

#include "nvs.h" /* legacy store import error code */
#include "persistence_service.h"

#define MEETING_RECOVERY_NAMESPACE "maclaw"
#define MEETING_RECOVERY_STORE_KEY "meeting_recovery"
#define MEETING_RECOVERY_STORE_MAGIC 0x4d525331u /* MRS1 */
#define MEETING_RECOVERY_STORE_VERSION 1u

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t pending;
    uint8_t reserved[3];
    int32_t next_chunk;
    int32_t phase;
    char recording_id[MEETING_RECOVERY_RECORDING_ID_CAPACITY];
} meeting_recovery_store_t;

static void clear_snapshot(meeting_recovery_snapshot_t *snapshot) {
    memset(snapshot, 0, sizeof(*snapshot));
}

static bool valid_store(const meeting_recovery_store_t *store) {
    return store && store->magic == MEETING_RECOVERY_STORE_MAGIC &&
           store->version == MEETING_RECOVERY_STORE_VERSION && store->pending <= 1 &&
           store->next_chunk >= 0 && store->phase >= 0 && store->phase <= 2 &&
           memchr(store->recording_id, '\0', sizeof(store->recording_id)) != NULL;
}

static bool valid_snapshot(const meeting_recovery_snapshot_t *snapshot) {
    return snapshot && snapshot->next_chunk >= 0 && snapshot->phase >= 0 &&
           snapshot->phase <= 2 &&
           memchr(snapshot->recording_id, '\0', sizeof(snapshot->recording_id)) != NULL;
}

static esp_err_t load_legacy(meeting_recovery_snapshot_t *snapshot, bool *out_found) {
    if (!snapshot || !out_found) return ESP_ERR_INVALID_ARG;
    clear_snapshot(snapshot);
    *out_found = false;
    int32_t value = 0;
    esp_err_t err = persistence_service_read_i32(MEETING_RECOVERY_NAMESPACE, "meet_next", &value);
    if (err == ESP_OK) {
        snapshot->next_chunk = value;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    err = persistence_service_read_i32(MEETING_RECOVERY_NAMESPACE, "meet_phase", &value);
    if (err == ESP_OK) {
        snapshot->phase = value;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    size_t size = sizeof(snapshot->recording_id);
    err = persistence_service_read_string(MEETING_RECOVERY_NAMESPACE, "meet_id",
                                          snapshot->recording_id, &size);
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    uint8_t pending = 0;
    err = persistence_service_read_u8(MEETING_RECOVERY_NAMESPACE, "meet_pending", &pending);
    if (err == ESP_OK) {
        if (pending > 1) return ESP_ERR_INVALID_STATE;
        snapshot->pending = pending != 0;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    if (*out_found && (snapshot->next_chunk < 0 || snapshot->phase < 0 || snapshot->phase > 2)) {
        return ESP_ERR_INVALID_STATE;
    }
    return ESP_OK;
}

esp_err_t meeting_recovery_service_init(void) {
    return persistence_service_is_initialized() ? ESP_OK : ESP_ERR_INVALID_STATE;
}

esp_err_t meeting_recovery_service_load(meeting_recovery_snapshot_t *out_snapshot) {
    if (!out_snapshot || !persistence_service_is_initialized()) return ESP_ERR_INVALID_ARG;
    clear_snapshot(out_snapshot);
    meeting_recovery_store_t store = {0};
    size_t size = sizeof(store);
    esp_err_t err = persistence_service_read_blob(MEETING_RECOVERY_NAMESPACE,
                                                  MEETING_RECOVERY_STORE_KEY,
                                                  &store, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy(out_snapshot, &legacy_found);
        if (err != ESP_OK || !legacy_found) return err;
        return meeting_recovery_service_save(out_snapshot);
    }
    if (err != ESP_OK) return err;
    if (size != sizeof(store) || !valid_store(&store)) return ESP_ERR_INVALID_STATE;
    out_snapshot->pending = store.pending != 0;
    out_snapshot->next_chunk = store.next_chunk;
    out_snapshot->phase = store.phase;
    strlcpy(out_snapshot->recording_id, store.recording_id,
            sizeof(out_snapshot->recording_id));
    return ESP_OK;
}

esp_err_t meeting_recovery_service_save(const meeting_recovery_snapshot_t *snapshot) {
    if (!valid_snapshot(snapshot) || !persistence_service_is_initialized()) {
        return ESP_ERR_INVALID_ARG;
    }
    meeting_recovery_store_t store = {
        .magic = MEETING_RECOVERY_STORE_MAGIC,
        .version = MEETING_RECOVERY_STORE_VERSION,
        .pending = snapshot->pending ? 1u : 0u,
        .next_chunk = snapshot->next_chunk,
        .phase = snapshot->phase,
    };
    strlcpy(store.recording_id, snapshot->recording_id, sizeof(store.recording_id));
    return persistence_service_write_blob(MEETING_RECOVERY_NAMESPACE,
                                          MEETING_RECOVERY_STORE_KEY,
                                          &store, sizeof(store));
}
