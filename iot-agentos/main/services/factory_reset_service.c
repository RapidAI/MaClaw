#include "factory_reset_service.h"

#include <string.h>

#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "persistence_service.h"

#define FACTORY_RESET_NAMESPACE "maclaw"
#define FACTORY_RESET_JOURNAL_KEY "factory_reset_journal"
#define FACTORY_RESET_DELIVERY_KEY "factory_reset_result_delivered"
#define FACTORY_RESET_EXECUTE_TIMEOUT_MS 5000u

static factory_reset_service_host_t s_host;
static SemaphoreHandle_t s_lock;
static bool s_initialized;
static bool s_reboot_pending;
static bool s_transaction_active;
/* Once a durable-journal write has an unknown outcome, this boot cannot
 * safely admit another destructive request. Recovery must be retried only
 * after a fresh boot has re-established the journal truth. */
static bool s_terminal_failure;

/* All work performed by one destructive request shares this monotonic parent
 * deadline.  In particular, PREPARE must receive the budget left after the
 * journal/marker admission checks; passing the nominal timeout again would
 * allow the child to exceed the caller's transaction bound. */
static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0u;
    const int64_t rounded_ms = (remaining_us + 999) / 1000;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

static bool mark_terminal_failure(void) {
    if (!s_lock) return false;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) == pdTRUE) {
        s_terminal_failure = true;
        xSemaphoreGive(s_lock);
        return true;
    }
    return false;
}

static void transaction_clear(void) {
    if (!s_lock) return;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) == pdTRUE) {
        s_transaction_active = false;
        xSemaphoreGive(s_lock);
    }
}

static device_status_t journal_read(configuration_factory_reset_journal_t *out) {
    uint8_t bytes[sizeof(*out)] = {0};
    size_t size = sizeof(bytes);
    device_status_t status = persistence_service_read_blob(
        FACTORY_RESET_NAMESPACE, FACTORY_RESET_JOURNAL_KEY, bytes, &size);
    if (status != DEVICE_STATUS_OK) return status;
    return configuration_factory_reset_journal_decode(bytes, size, out)
               ? DEVICE_STATUS_OK : DEVICE_STATUS_INTERNAL_ERROR;
}

static device_status_t journal_write(
    const configuration_factory_reset_journal_t *journal) {
    uint8_t bytes[sizeof(*journal)] = {0};
    if (!configuration_factory_reset_journal_encode(journal, bytes)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return persistence_service_write_blob(FACTORY_RESET_NAMESPACE,
                                          FACTORY_RESET_JOURNAL_KEY,
                                          bytes, sizeof(bytes));
}

static device_status_t journal_clear(void) {
    device_status_t status = persistence_service_erase_key(
        FACTORY_RESET_NAMESPACE, FACTORY_RESET_JOURNAL_KEY);
    return status == DEVICE_STATUS_NOT_FOUND ? DEVICE_STATUS_OK : status;
}

static device_status_t delivery_marker_read(bool *out_marked) {
    if (!out_marked) return DEVICE_STATUS_INVALID_ARGUMENT;
    uint8_t marked = 0;
    device_status_t status = persistence_service_read_u8(
        FACTORY_RESET_NAMESPACE, FACTORY_RESET_DELIVERY_KEY, &marked);
    if (status == DEVICE_STATUS_NOT_FOUND) {
        *out_marked = false;
        return DEVICE_STATUS_OK;
    }
    if (status != DEVICE_STATUS_OK || marked > 1u) return DEVICE_STATUS_IO_ERROR;
    *out_marked = marked != 0u;
    return DEVICE_STATUS_OK;
}

static device_status_t delivery_marker_clear(void) {
    device_status_t status = persistence_service_erase_key(
        FACTORY_RESET_NAMESPACE, FACTORY_RESET_DELIVERY_KEY);
    return status == DEVICE_STATUS_NOT_FOUND ? DEVICE_STATUS_OK : status;
}

device_status_t factory_reset_service_init(const factory_reset_service_host_t *host) {
    if (!host || host->struct_size != sizeof(*host) ||
        host->abi_version != FACTORY_RESET_SERVICE_HOST_ABI_VERSION ||
        !host->erase_classes || !host->verify_classes_absent ||
        !host->clear_meeting_recording || !host->clear_pet_cache ||
        !host->verify_personal_storage_absent || !host->validate_authorization ||
        !host->verify_recovery_state ||
        !host->prepare_for_reset || !host->complete_reset ||
        !host->abort_prepare_for_reset ||
        !host->reboot_after_reset ||
        !persistence_service_is_initialized()) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (!s_lock) s_lock = xSemaphoreCreateMutex();
    if (!s_lock) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    /* Re-initialization must not clear a terminal fence raised after an
     * uncertain durable write.  A fresh boot starts with zeroed globals and
     * is the only supported way to establish a new recovery truth. */
    if (s_initialized && (s_transaction_active || s_reboot_pending ||
                          s_terminal_failure)) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_host = *host;
    s_initialized = true;
    s_transaction_active = false;
    s_reboot_pending = false;
    xSemaphoreGive(s_lock);
    return DEVICE_STATUS_OK;
}

bool factory_reset_service_is_initialized(void) { return s_initialized; }

device_status_t factory_reset_service_execute(
    const configuration_factory_reset_request_t *request) {
    if (!s_initialized || !configuration_factory_reset_authorize(request) ||
        !s_host.validate_authorization(request->source, request->generation,
                                       s_host.context)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(5000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (s_transaction_active || s_reboot_pending || s_terminal_failure) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Re-check the external authorization after acquiring the transaction
     * gate.  Capability generation may have been withdrawn between the
     * caller's initial validation and this lock acquisition; destructive
     * storage work must never cross that TOCTOU window. */
    if (!s_host.validate_authorization(request->source, request->generation,
                                       s_host.context)) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* Reserve the transaction before touching Persistence.  Reads and
     * marker cleanup may block on its worker and must not run under s_lock. */
    s_transaction_active = true;
    xSemaphoreGive(s_lock);

    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)FACTORY_RESET_EXECUTE_TIMEOUT_MS * 1000;

    configuration_factory_reset_journal_t journal = {0};
    device_status_t status = journal_read(&journal);
    /* Persistence reads are worker-backed and may return after the caller's
     * budget has elapsed.  Re-check immediately; otherwise a late NOT_FOUND
     * could still admit PREPARE and destructive work outside the transaction
     * bound. */
    if (remaining_timeout_ms(deadline_us) == 0u) {
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status == DEVICE_STATUS_OK) {
        /* A prior in-flight operation is never silently overwritten. */
        transaction_clear();
        return DEVICE_STATUS_BUSY;
    }
    if (status != DEVICE_STATUS_NOT_FOUND) {
        transaction_clear();
        return status;
    }
    /* A marker without its journal can only be left by a power loss in the
     * final cleanup window.  Remove that stale evidence before accepting a
     * new transaction; otherwise a later COMMITTED record could be mistaken
     * for an already-delivered result. */
    bool stale_delivery = false;
    status = delivery_marker_read(&stale_delivery);
    if (remaining_timeout_ms(deadline_us) == 0u) {
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    if (status != DEVICE_STATUS_OK) {
        transaction_clear();
        return status;
    }
    if (stale_delivery) {
        status = delivery_marker_clear();
        if (remaining_timeout_ms(deadline_us) == 0u) {
            transaction_clear();
            return DEVICE_STATUS_TIMEOUT;
        }
        if (status != DEVICE_STATUS_OK) {
            transaction_clear();
            return DEVICE_STATUS_IO_ERROR;
        }
    }
    const uint32_t prepare_timeout_ms = remaining_timeout_ms(deadline_us);
    if (prepare_timeout_ms == 0u) {
        s_host.abort_prepare_for_reset(s_host.context);
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    status = s_host.prepare_for_reset(prepare_timeout_ms, s_host.context);
    if (status != DEVICE_STATUS_OK) {
        s_host.abort_prepare_for_reset(s_host.context);
        transaction_clear();
        return status;
    }
    /* PREPARE may have consumed the entire parent budget while still
     * returning success.  Do not begin destructive writes once that budget is
     * gone; no journal has been published yet, so this path is safely
     * reversible. */
    if (remaining_timeout_ms(deadline_us) == 0u) {
        s_host.abort_prepare_for_reset(s_host.context);
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!configuration_factory_reset_journal_begin(
            &journal, request->classes, request->generation) ||
        journal_write(&journal) != DEVICE_STATUS_OK) {
        /* Journal admission is now uncertain. Keep the reversible fence
         * closed so a partially persisted PREPARED record cannot race a new
         * configuration mutation; boot recovery remains fail-closed. */
        if (mark_terminal_failure()) transaction_clear();
        return DEVICE_STATUS_IO_ERROR;
    }
    /* A successful PREPARED write is durable evidence.  If that write itself
     * consumed the parent budget, stop before any destructive callback; boot
     * recovery will keep the generation fail-closed. */
    if (remaining_timeout_ms(deadline_us) == 0u) {
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    status = s_host.erase_classes(request->classes, s_host.context);
    if (status == DEVICE_STATUS_OK && remaining_timeout_ms(deadline_us) == 0u) {
        status = DEVICE_STATUS_TIMEOUT;
    }
    if (status == DEVICE_STATUS_OK &&
        (request->classes & CONFIGURATION_FACTORY_RESET_CLASS_MEETING_RECORDING)) {
        status = s_host.clear_meeting_recording(s_host.context);
        if (status == DEVICE_STATUS_OK && remaining_timeout_ms(deadline_us) == 0u) {
            status = DEVICE_STATUS_TIMEOUT;
        }
    }
    if (status == DEVICE_STATUS_OK &&
        (request->classes & CONFIGURATION_FACTORY_RESET_CLASS_PET_CACHE)) {
        status = s_host.clear_pet_cache(s_host.context);
        if (status == DEVICE_STATUS_OK && remaining_timeout_ms(deadline_us) == 0u) {
            status = DEVICE_STATUS_TIMEOUT;
        }
    }
    if (status == DEVICE_STATUS_OK) {
        status = s_host.verify_classes_absent(request->classes, s_host.context);
        if (status == DEVICE_STATUS_OK && remaining_timeout_ms(deadline_us) == 0u) {
            status = DEVICE_STATUS_TIMEOUT;
        }
    }
    if (status == DEVICE_STATUS_OK) {
        status = s_host.verify_personal_storage_absent(s_host.context);
        if (status == DEVICE_STATUS_OK && remaining_timeout_ms(deadline_us) == 0u) {
            status = DEVICE_STATUS_TIMEOUT;
        }
    }
    if (status != DEVICE_STATUS_OK) {
        /* PREPARED remains durable: reboot recovery must not guess whether a
         * partially completed erase can be retried safely.  The reversible
         * PREPARE fence is intentionally kept closed until reboot; reopening
         * persistence here could race an uncertain destructive state. */
        transaction_clear();
        return status;
    }
    if (remaining_timeout_ms(deadline_us) == 0u) {
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!configuration_factory_reset_journal_commit(&journal) ||
        journal_write(&journal) != DEVICE_STATUS_OK) {
        /* COMMITTED evidence may or may not have reached storage. Keep all
         * admission closed and leave the durable journal for fail-closed
         * recovery; never reopen a worker after destructive writes began. */
        if (mark_terminal_failure()) transaction_clear();
        return DEVICE_STATUS_IO_ERROR;
    }
    /* Publish the setup handoff while COMMITTED evidence is still durable.
     * If this write fails, leave COMMITTED for boot recovery; never clear the
     * journal and reboot into a paired surface with erased data. */
    if (remaining_timeout_ms(deadline_us) == 0u) {
        transaction_clear();
        return DEVICE_STATUS_TIMEOUT;
    }
    status = s_host.complete_reset(s_host.context);
    if (status != DEVICE_STATUS_OK) {
        /* COMMITTED is already durable; preserve the closed fence and leave
         * recovery evidence for the next boot. */
        transaction_clear();
        return status;
    }
    /* Keep COMMITTED journal durable until the caller confirms that the tool
     * result reached the Gateway or a durable outbox. */
    status = DEVICE_STATUS_OK;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return DEVICE_STATUS_TIMEOUT;
    s_transaction_active = false;
    s_reboot_pending = true;
    xSemaphoreGive(s_lock);
    return status;
}

void factory_reset_service_reboot_if_pending(bool result_durable) {
    if (!result_durable) return;
    if (!s_initialized || !s_lock) return;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    if (!s_reboot_pending || s_transaction_active) {
        xSemaphoreGive(s_lock);
        return;
    }
    /* Reserve the handoff and release the service mutex before persistence
     * calls.  NVS routing can block on its worker; service admission must not
     * be coupled to that latency or expose a callback-cycle deadlock. */
    s_transaction_active = true;
    xSemaphoreGive(s_lock);
    const bool pending = true;
    /* Record delivery before removing COMMITTED evidence.  If power fails
     * between the POST/outbox acknowledgement and journal cleanup, boot
     * recovery can prove that it is safe to finish the handoff. */
    if (pending && persistence_service_write_u8(
                       FACTORY_RESET_NAMESPACE, FACTORY_RESET_DELIVERY_KEY,
                       1u) != DEVICE_STATUS_OK) {
        transaction_clear();
        return;
    }
    const device_status_t journal_status = pending ? journal_clear() : DEVICE_STATUS_OK;
    if (journal_status != DEVICE_STATUS_OK) {
        transaction_clear();
        return;
    }
    /* The journal is the critical recovery evidence.  Once it is gone, a
     * marker-cleanup failure cannot make the destructive operation unsafe:
     * the marker is harmless and the next boot removes any orphan.  Do not
     * strand the transaction in an active state merely because this best-
     * effort cleanup write was interrupted. */
    (void)delivery_marker_clear();
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) return;
    s_reboot_pending = false;
    s_transaction_active = false;
    xSemaphoreGive(s_lock);
    if (pending) s_host.reboot_after_reset(s_host.context);
}

device_status_t factory_reset_service_recover(void) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(5000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (s_transaction_active || s_terminal_failure) {
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_BUSY;
    }
    /* Reserve recovery before touching Persistence.  Journal/marker I/O is
     * worker-backed and may block; keeping the service mutex free prevents a
     * callback cycle from deadlocking admission. */
    s_transaction_active = true;
    xSemaphoreGive(s_lock);
    configuration_factory_reset_journal_t journal = {0};
    device_status_t status = journal_read(&journal);
    if (status == DEVICE_STATUS_NOT_FOUND) {
        /* Tolerate an orphaned delivery marker from the final cleanup window;
         * no journal means there is no reset transaction to recover. */
        (void)delivery_marker_clear();
        transaction_clear();
        return DEVICE_STATUS_OK;
    }
    if (status != DEVICE_STATUS_OK) {
        transaction_clear();
        return status;
    }
    if (journal.stage == CONFIGURATION_FACTORY_RESET_STAGE_PREPARED) {
        /* Do not discard or replay an uncertain erase automatically. */
        transaction_clear();
        return DEVICE_STATUS_BUSY;
    }
    /* Verification and the idempotent setup write may block on the Persistence
     * worker; holding s_lock across those calls would unnecessarily couple
     * service admission to storage latency and make a callback cycle
     * deadlock-prone. */
    bool delivered = false;
    status = delivery_marker_read(&delivered);
    if (status != DEVICE_STATUS_OK) {
        transaction_clear();
        return status;
    }
    status = delivered ? s_host.verify_classes_absent(journal.classes, s_host.context)
                       : s_host.verify_recovery_state(journal.classes, s_host.context);
    /* Meeting/audio and Pet-cache storage are outside the fixed NVS key
     * inventory. Re-check them on every COMMITTED recovery, including the
     * delivery-marked window, before trusting the transaction evidence. */
    if (status == DEVICE_STATUS_OK) {
        status = s_host.verify_personal_storage_absent(s_host.context);
    }
    if (status == DEVICE_STATUS_OK && !delivered) {
        /* A COMMITTED journal is durable proof that the destructive erase
         * completed, but setup handoff may have been interrupted afterwards.
         * Re-run the idempotent setup request before removing the evidence so
         * a reboot cannot return to the paired surface with empty data. */
        status = s_host.complete_reset(s_host.context);
    }
    if (status != DEVICE_STATUS_OK) {
        transaction_clear();
        return status;
    }
    bool should_reboot = false;
    if (delivered) {
        /* Delivery was durably acknowledged before a possible power loss;
         * clear both pieces of evidence and finish exactly once. */
        /* Persistence operations stay outside s_lock.  The transaction gate
         * remains active, so execute/recover cannot race this cleanup. */
        const device_status_t journal_status = journal_clear();
        /* The journal is the safety-critical evidence.  Once it is
         * durably gone, failure to remove the delivery marker is harmless
         * orphan cleanup and must not suppress the one required reboot. */
        const device_status_t marker_status =
            journal_status == DEVICE_STATUS_OK ? delivery_marker_clear()
                                                : DEVICE_STATUS_OK;
        should_reboot = journal_status == DEVICE_STATUS_OK;
        status = journal_status != DEVICE_STATUS_OK ? journal_status
                                                    : DEVICE_STATUS_OK;
        if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) {
            /* Keep the active fence if state publication cannot be serialized;
             * the next boot will inspect the still-durable evidence. */
            return DEVICE_STATUS_TIMEOUT;
        }
        s_transaction_active = false;
        xSemaphoreGive(s_lock);
        (void)marker_status;
        if (status != DEVICE_STATUS_OK) return status;
    } else {
        /* COMMITTED erase is valid, but result delivery is not yet proven.
         * Keep the journal and let Gateway outbox replay call the explicit
         * reboot gate after the factory_reset envelope is delivered. */
        if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(1000)) != pdTRUE) {
            transaction_clear();
            return DEVICE_STATUS_TIMEOUT;
        }
        s_transaction_active = false;
        s_reboot_pending = true;
        xSemaphoreGive(s_lock);
        return DEVICE_STATUS_OK;
    }
    if (should_reboot) s_host.reboot_after_reset(s_host.context);
    return status;
}
