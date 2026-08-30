#include <stdio.h>
#include <string.h>

#include "services/factory_reset_service.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static int64_t s_time_us;
int64_t esp_timer_get_time(void) { return s_time_us; }

typedef struct { int locked; } host_mutex_t;
static host_mutex_t s_mutex;
SemaphoreHandle_t xSemaphoreCreateMutex(void) { return &s_mutex; }
BaseType_t xSemaphoreTake(SemaphoreHandle_t semaphore, TickType_t timeout) {
    (void)timeout;
    host_mutex_t *mutex = (host_mutex_t *)semaphore;
    if (!mutex || mutex->locked) return pdFALSE;
    mutex->locked = 1;
    return pdTRUE;
}
BaseType_t xSemaphoreGive(SemaphoreHandle_t semaphore) {
    host_mutex_t *mutex = (host_mutex_t *)semaphore;
    if (!mutex) return pdFALSE;
    mutex->locked = 0;
    return pdTRUE;
}

typedef struct {
    char name_space[24];
    char key[40];
    unsigned char value[sizeof(configuration_factory_reset_journal_t) + 8u];
    size_t size;
    int present;
} record_t;
static record_t s_records[4];
static const char *s_fail_write_key;
static const char *s_fail_erase_key;
static int s_advance_before_prepare;

static record_t *record(const char *name_space, const char *key, int create) {
    for (size_t i = 0; i < sizeof(s_records) / sizeof(s_records[0]); ++i) {
        if (s_records[i].present && !strcmp(s_records[i].name_space, name_space) &&
            !strcmp(s_records[i].key, key)) return &s_records[i];
    }
    if (!create) return NULL;
    for (size_t i = 0; i < sizeof(s_records) / sizeof(s_records[0]); ++i) {
        if (!s_records[i].present) {
            s_records[i].present = 1;
            snprintf(s_records[i].name_space, sizeof(s_records[i].name_space), "%s", name_space);
            snprintf(s_records[i].key, sizeof(s_records[i].key), "%s", key);
            return &s_records[i];
        }
    }
    return NULL;
}

bool persistence_service_is_initialized(void) { return true; }
device_status_t persistence_service_read_blob(const char *name_space, const char *key,
                                               void *out, size_t *size) {
    if (s_advance_before_prepare && !strcmp(key, "factory_reset_journal")) {
        /* Consume part of the parent budget before PREPARE is admitted. */
        s_time_us += 1200000;
        s_advance_before_prepare = 0;
    }
    record_t *entry = record(name_space, key, 0);
    if (!entry) return DEVICE_STATUS_NOT_FOUND;
    if (!size || !out || *size < entry->size) return DEVICE_STATUS_INVALID_ARGUMENT;
    memcpy(out, entry->value, entry->size);
    *size = entry->size;
    return DEVICE_STATUS_OK;
}
device_status_t persistence_service_write_blob(const char *name_space, const char *key,
                                                const void *value, size_t size) {
    if (s_fail_write_key && !strcmp(s_fail_write_key, key)) return DEVICE_STATUS_IO_ERROR;
    record_t *entry = record(name_space, key, 1);
    if (!entry || !value || size > sizeof(entry->value)) return DEVICE_STATUS_IO_ERROR;
    memcpy(entry->value, value, size); entry->size = size; return DEVICE_STATUS_OK;
}
device_status_t persistence_service_erase_key(const char *name_space, const char *key) {
    if (s_fail_erase_key && !strcmp(s_fail_erase_key, key)) return DEVICE_STATUS_IO_ERROR;
    record_t *entry = record(name_space, key, 0);
    if (!entry) return DEVICE_STATUS_NOT_FOUND;
    entry->present = 0; entry->size = 0; return DEVICE_STATUS_OK;
}
device_status_t persistence_service_read_u8(const char *name_space, const char *key,
                                            uint8_t *out) {
    size_t size = 1; return persistence_service_read_blob(name_space, key, out, &size);
}
device_status_t persistence_service_write_u8(const char *name_space, const char *key,
                                             uint8_t value) {
    return persistence_service_write_blob(name_space, key, &value, sizeof(value));
}

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL:%s\n", #x); return 1; } } while (0)
static int s_prepare_calls, s_abort_calls, s_erase_calls, s_verify_calls;
static uint32_t s_last_prepare_timeout;
static int s_personal_calls, s_complete_calls, s_reboot_calls;
static int s_contract_failed;
static int s_recover_during_prepare;
static device_status_t s_prepare_status, s_erase_status, s_verify_status, s_personal_status,
                       s_complete_status;

static bool authorize(configuration_source_t source, uint64_t generation, void *context) {
    (void)context; return source == CONFIGURATION_SOURCE_HUB_AUTHENTICATED && generation == 7u;
}
static device_status_t prepare(uint32_t timeout, void *context) {
    (void)context; CHECK(timeout != 0); CHECK(s_mutex.locked == 0);
    s_last_prepare_timeout = timeout;
    /* The service must publish its active transaction before invoking the
     * potentially long-running prepare callback; recovery is rejected in
     * this window rather than racing journal admission. */
    if (factory_reset_service_recover() != DEVICE_STATUS_BUSY) {
        s_recover_during_prepare = 1;
    }
    ++s_prepare_calls; return s_prepare_status;
}
static void abort_prepare(void *context) { (void)context; if (s_mutex.locked) { s_contract_failed = 1; return; } ++s_abort_calls; }
static device_status_t erase(uint32_t classes, void *context) {
    (void)context; CHECK(classes == CONFIGURATION_FACTORY_RESET_CLASS_ALL); CHECK(s_mutex.locked == 0); ++s_erase_calls;
    return s_erase_status;
}
static device_status_t verify(uint32_t classes, void *context) {
    (void)context; CHECK(classes == CONFIGURATION_FACTORY_RESET_CLASS_ALL); CHECK(s_mutex.locked == 0); ++s_verify_calls;
    return s_verify_status;
}
static device_status_t personal(void *context) { (void)context; CHECK(s_mutex.locked == 0); ++s_personal_calls; return s_personal_status; }
static device_status_t complete(void *context) { (void)context; CHECK(s_mutex.locked == 0); ++s_complete_calls; return s_complete_status; }
static void reboot(void *context) { (void)context; if (s_mutex.locked) { s_contract_failed = 1; return; } ++s_reboot_calls; }

static factory_reset_service_host_t host(void) {
    return (factory_reset_service_host_t){
        .struct_size = sizeof(factory_reset_service_host_t),
        .abi_version = FACTORY_RESET_SERVICE_HOST_ABI_VERSION,
        .erase_classes = erase,
        .verify_classes_absent = verify,
        .clear_meeting_recording = personal,
        .clear_pet_cache = personal,
        .verify_personal_storage_absent = personal,
        .verify_recovery_state = verify,
        .validate_authorization = authorize,
        .prepare_for_reset = prepare,
        .abort_prepare_for_reset = abort_prepare,
        .complete_reset = complete,
        .reboot_after_reset = reboot,
    };
}

static configuration_factory_reset_request_t request(void) {
    return (configuration_factory_reset_request_t){
        .struct_size = sizeof(configuration_factory_reset_request_t),
        .abi_version = CONFIGURATION_FACTORY_RESET_POLICY_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_HUB_AUTHENTICATED,
        .authenticated = true, .explicit_confirmation = true, .generation = 7u,
        .classes = CONFIGURATION_FACTORY_RESET_CLASS_ALL,
    };
}

int main(void) {
    factory_reset_service_host_t invalid = host();
    invalid.abort_prepare_for_reset = NULL;
    CHECK(factory_reset_service_init(&invalid) == DEVICE_STATUS_INVALID_ARGUMENT);
    factory_reset_service_host_t valid = host();
    CHECK(factory_reset_service_init(&valid) == DEVICE_STATUS_OK);

    configuration_factory_reset_request_t req = request();
    s_prepare_status = DEVICE_STATUS_BUSY;
    s_advance_before_prepare = 1;
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_BUSY);
    CHECK(s_prepare_calls == 1 && s_abort_calls == 1);
    CHECK(s_last_prepare_timeout < 5000u);
    CHECK(s_recover_during_prepare == 0);

    /* A durable PREPARED record has an ambiguous erase outcome. Recovery must
     * remain fail-closed and a new destructive request must be rejected. */
    configuration_factory_reset_journal_t prepared = {0};
    uint8_t prepared_bytes[sizeof(prepared)] = {0};
    CHECK(configuration_factory_reset_journal_begin(
              &prepared, CONFIGURATION_FACTORY_RESET_CLASS_ALL, 7u));
    CHECK(configuration_factory_reset_journal_encode(&prepared, prepared_bytes));
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_journal",
                                         prepared_bytes, sizeof(prepared_bytes)) == DEVICE_STATUS_OK);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_BUSY);
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_BUSY);
    memset(s_records, 0, sizeof(s_records));

    s_prepare_status = DEVICE_STATUS_OK; s_erase_status = DEVICE_STATUS_IO_ERROR;
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_IO_ERROR);
    CHECK(s_erase_calls == 1 && s_verify_calls == 0);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_BUSY);

    memset(s_records, 0, sizeof(s_records));
    s_erase_status = DEVICE_STATUS_OK; s_verify_status = DEVICE_STATUS_OK;
    s_personal_status = DEVICE_STATUS_OK; s_complete_status = DEVICE_STATUS_OK;
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_OK);
    CHECK(s_reboot_calls == 0);
    factory_reset_service_reboot_if_pending(false);
    CHECK(s_reboot_calls == 0);
    factory_reset_service_reboot_if_pending(true);
    CHECK(s_reboot_calls == 1);
    factory_reset_service_reboot_if_pending(true);
    CHECK(s_reboot_calls == 1);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);
    CHECK(!s_contract_failed);

    /* Journal cleanup is safety-critical. If its erase is interrupted, the
     * reboot gate must retain the pending handoff and allow a later retry;
     * it must never reboot while COMMITTED evidence remains durable. */
    memset(s_records, 0, sizeof(s_records));
    s_complete_status = DEVICE_STATUS_OK;
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_OK);
    s_fail_erase_key = "factory_reset_journal";
    factory_reset_service_reboot_if_pending(true);
    CHECK(s_reboot_calls == 1);
    s_fail_erase_key = NULL;
    factory_reset_service_reboot_if_pending(true);
    CHECK(s_reboot_calls == 2);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);

    /* Corrupt journal bytes are integrity failures, never an absent journal. */
    uint8_t corrupt_journal[sizeof(prepared)] = {0};
    corrupt_journal[0] = 0xA5u;
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_journal",
                                         corrupt_journal, sizeof(corrupt_journal)) == DEVICE_STATUS_OK);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_INTERNAL_ERROR);
    memset(s_records, 0, sizeof(s_records));

    /* A malformed delivery marker cannot unlock a COMMITTED transaction; the
     * journal remains durable until a valid marker is supplied. */
    CHECK(configuration_factory_reset_journal_begin(
              &prepared, CONFIGURATION_FACTORY_RESET_CLASS_ALL, 7u));
    CHECK(configuration_factory_reset_journal_commit(&prepared));
    CHECK(configuration_factory_reset_journal_encode(&prepared, prepared_bytes));
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_journal",
                                         prepared_bytes, sizeof(prepared_bytes)) == DEVICE_STATUS_OK);
    uint8_t malformed_delivery = 2u;
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_result_delivered",
                                         &malformed_delivery, sizeof(malformed_delivery)) == DEVICE_STATUS_OK);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_IO_ERROR);
    size_t retained_size = sizeof(prepared);
    CHECK(persistence_service_read_blob("maclaw", "factory_reset_journal",
                                       prepared_bytes, &retained_size) == DEVICE_STATUS_OK);
    CHECK(retained_size == sizeof(prepared));
    memset(s_records, 0, sizeof(s_records));

    /* Simulate a fresh-boot COMMITTED + delivery-marker window in-place.
     * Recovery must re-verify personal storage, clear both records, and
     * reboot exactly once for that recovered transaction. */
    configuration_factory_reset_journal_t recovered = {0};
    uint8_t encoded[sizeof(recovered)] = {0};
    CHECK(configuration_factory_reset_journal_begin(
              &recovered, CONFIGURATION_FACTORY_RESET_CLASS_ALL, 7u));
    CHECK(configuration_factory_reset_journal_commit(&recovered));
    CHECK(configuration_factory_reset_journal_encode(&recovered, encoded));
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_journal",
                                         encoded, sizeof(encoded)) == DEVICE_STATUS_OK);
    CHECK(persistence_service_write_u8("maclaw", "factory_reset_result_delivered",
                                       1u) == DEVICE_STATUS_OK);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);
    CHECK(s_reboot_calls == 3);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);

    /* Marker cleanup is best-effort after the journal is gone: a failure must
     * not suppress the single reboot or leave the transaction gate wedged. */
    CHECK(configuration_factory_reset_journal_begin(
              &recovered, CONFIGURATION_FACTORY_RESET_CLASS_ALL, 7u));
    CHECK(configuration_factory_reset_journal_commit(&recovered));
    CHECK(configuration_factory_reset_journal_encode(&recovered, encoded));
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_journal",
                                         encoded, sizeof(encoded)) == DEVICE_STATUS_OK);
    CHECK(persistence_service_write_u8("maclaw", "factory_reset_result_delivered",
                                       1u) == DEVICE_STATUS_OK);
    s_fail_erase_key = "factory_reset_result_delivered";
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);
    CHECK(s_reboot_calls == 4);
    s_fail_erase_key = NULL;
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);

    /* A malformed delivery marker is not an implicit "delivered" proof. */
    uint8_t malformed_marker = 2u;
    CHECK(persistence_service_write_blob("maclaw", "factory_reset_result_delivered",
                                         &malformed_marker, sizeof(malformed_marker)) == DEVICE_STATUS_OK);
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_IO_ERROR);
    /* Recovery with no journal safely removes the orphan marker without
     * treating it as delivery evidence for a new transaction. */
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);
    /* A COMMITTED journal must survive a setup-handoff failure. Recovery may
     * retry the idempotent handoff, but must not clear evidence early. */
    memset(s_records, 0, sizeof(s_records));
    s_complete_status = DEVICE_STATUS_IO_ERROR;
    CHECK(factory_reset_service_execute(&req) == DEVICE_STATUS_IO_ERROR);
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_IO_ERROR);
    size_t handoff_journal_size = sizeof(prepared);
    CHECK(persistence_service_read_blob("maclaw", "factory_reset_journal",
                                       prepared_bytes, &handoff_journal_size) == DEVICE_STATUS_OK);
    CHECK(handoff_journal_size == sizeof(prepared));
    s_complete_status = DEVICE_STATUS_OK;
    CHECK(factory_reset_service_recover() == DEVICE_STATUS_OK);
    factory_reset_service_reboot_if_pending(true);
    CHECK(s_reboot_calls == 5);

    /* An unknown PREPARED-journal write outcome is terminal for this boot:
     * the service must not admit another destructive request or pretend that
     * recovery can establish the missing durable truth in-place. */
    memset(s_records, 0, sizeof(s_records));
    s_fail_write_key = "factory_reset_journal";
    device_status_t unknown_write_status = factory_reset_service_execute(&req);
    CHECK(unknown_write_status != DEVICE_STATUS_OK);
    device_status_t second_status = factory_reset_service_execute(&req);
    CHECK(second_status == DEVICE_STATUS_BUSY || second_status == DEVICE_STATUS_IO_ERROR);
    device_status_t recovery_status = factory_reset_service_recover();
    CHECK(recovery_status == DEVICE_STATUS_BUSY || recovery_status == DEVICE_STATUS_IO_ERROR);
    s_fail_write_key = NULL;
    puts("PASS factory reset service lifecycle and delivery evidence");
    return 0;
}
