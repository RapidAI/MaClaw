#include "connectivity_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/task.h"

#include "platform_connectivity.h"
#include "fault_domain.h"

/*
 * The service owns only the small, hardware-neutral observed/selected state.
 * Wi-Fi and ML307 remain independent transport adapters; they publish their
 * readiness here after their own start/recovery work.  Business code can then
 * ask one question -- is the selected uplink ready? -- without importing a
 * modem implementation or a board Kconfig symbol.
 */
static portMUX_TYPE s_connectivity_lock = portMUX_INITIALIZER_UNLOCKED;
/* EventGroup lifetime has a single teardown owner. The state lock protects
 * observations, but cannot span the waiter drain because waiters must take
 * it to release admission. Retain this static coordinator after deinit: a
 * second caller may arrive while a timed-out first stop has left the current
 * generation closed. */
static StaticSemaphore_t s_deinit_lock_storage;
static SemaphoreHandle_t s_deinit_lock;
static device_uplink_t s_active_uplink = DEVICE_UPLINK_WIFI;
static bool s_wifi_ready;
static bool s_cellular_ready;
static bool s_provisioning_active;
static bool s_provisioning_pairing_recovery;
/* This event group belongs to Connectivity rather than main.c.  The epoch is
 * the authoritative predicate; the event bit merely wakes a synchronous
 * adapter start.  Thus an already-set bit or an event from an older wait can
 * never by itself turn a new start attempt into a successful one. */
static EventGroupHandle_t s_wifi_attempt_events;
static uint32_t s_wifi_attempt_epoch;
static uint32_t s_wifi_ready_epoch;
static char s_wifi_attempt_network_id[33];
/* Every caller that can dereference the EventGroup takes one short-lived
 * admission.  Deinit first closes admission, wakes existing waiters, then
 * waits for these references to drain before deleting the group.  This avoids
 * deleting a FreeRTOS EventGroup while a station-start task is still blocked
 * in xEventGroupWaitBits(). */
static uint32_t s_wifi_attempt_users;
static bool s_connectivity_initialized;
static bool s_connectivity_stopping;
/* The EventGroup and its waiters are one Connectivity fault domain.  The
 * logical selected-uplink values may survive a service restart, but all
 * readiness observations and EventGroup borrowers are generation-local. */
static fault_domain_t s_fault_domain = {
    .struct_size = sizeof(fault_domain_t),
    .abi_version = FAULT_DOMAIN_ABI_VERSION,
    .id = FAULT_DOMAIN_ID_CONNECTIVITY,
    .phase = FAULT_DOMAIN_STOPPED,
    .generation = 1u,
};
/* This is a reversible logical admission fence, not a physical transport
 * stop.  It is intentionally independent from deinit so a failed system
 * sleep PREPARE can reopen normal traffic without rebuilding Wi-Fi/ML307. */
static bool s_system_sleep_preparing;
static uint32_t s_network_request_users;
/* The generic request count protects whole-Connectivity drain, while this
 * narrower fact proves a profile-private cellular request is still in flight.
 * It lets a late worker cancel its own already-admitted ML307 request after
 * an uplink selection changed, without letting an idle Wi-Fi generation touch
 * the cellular adapter merely by calling a cancellation API. */
static uint32_t s_cellular_network_request_users;
/* Physical modem lifecycle calls are separate from HTTP borrowers.  Only one
 * bounded lifecycle call may own the profile at once, and it must not overlap
 * an admitted cellular HTTP/stream borrower: prepare/start/quiesce can change
 * modem state below that request. A selected-uplink change may alter the
 * profile transport hint, so it must not overlap an adapter call already
 * inside the private adapter. System Sleep and deinit retain this admission
 * until it returns, then park or release the same generation. */
static uint32_t s_cellular_transport_operation_users;
/* ESP-IDF's default event loop belongs to the composition root, but a queued
 * callback may still start shared Connectivity/Gateway work while that root
 * is draining a System Sleep or teardown transaction. Keep this value-only
 * admission fence beside the rest of Connectivity lifecycle state; the root
 * never needs to retain a second counter or callback-drain semaphore. */
static StaticSemaphore_t s_wifi_event_callbacks_drained_storage;
static SemaphoreHandle_t s_wifi_event_callbacks_drained;
static bool s_wifi_event_callback_admission_open;
static uint32_t s_wifi_event_callbacks_inflight;
static bool s_wifi_event_system_sleep_preparing;
static bool s_wifi_event_system_sleep_was_admitted;
/* A selected-uplink change may touch a profile-owned transport hint after the
 * common state has been updated.  Count that short transaction separately
 * from HTTP borrowers so System Sleep PREPARE cannot run its physical
 * transport park halfway through a switch. */
static uint32_t s_transport_selection_users;
static connectivity_service_system_sleep_request_canceller_t
    s_system_sleep_request_canceller;
static void *s_system_sleep_request_canceller_context;
static connectivity_service_system_sleep_request_resumer_t
    s_system_sleep_request_resumer;
static void *s_system_sleep_request_resumer_context;
#define WIFI_ATTEMPT_READY_BIT BIT0

#define CONNECTIVITY_INIT_LOCK_TIMEOUT_MS 3000u

static SemaphoreHandle_t connectivity_deinit_lock(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_deinit_lock) {
        s_deinit_lock = xSemaphoreCreateMutexStatic(&s_deinit_lock_storage);
    }
    SemaphoreHandle_t lock = s_deinit_lock;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return lock;
}

static TickType_t connectivity_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t connectivity_remaining_ticks(TickType_t started,
                                               TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static SemaphoreHandle_t connectivity_wifi_event_callbacks_drained(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_wifi_event_callbacks_drained) {
        s_wifi_event_callbacks_drained =
            xSemaphoreCreateBinaryStatic(&s_wifi_event_callbacks_drained_storage);
    }
    SemaphoreHandle_t drained = s_wifi_event_callbacks_drained;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return drained;
}

static void wake_wifi_attempt_waiters_for_system_sleep(void);

void connectivity_service_open_wifi_event_callback_admission(void) {
    SemaphoreHandle_t drained = connectivity_wifi_event_callbacks_drained();
    if (!drained) return;
    /* A notification belongs to the preceding closed generation, not the
     * newly registered callback generation. */
    while (xSemaphoreTake(drained, 0) == pdTRUE) {}
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_connectivity_initialized && !s_connectivity_stopping &&
        !s_system_sleep_preparing && !s_wifi_event_system_sleep_preparing) {
        s_wifi_event_callback_admission_open = true;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_wifi_event_callback_enter(void) {
    bool entered = false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_connectivity_initialized && !s_connectivity_stopping &&
        s_wifi_event_callback_admission_open &&
        !s_system_sleep_preparing && !s_wifi_event_system_sleep_preparing) {
        ++s_wifi_event_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return entered;
}

void connectivity_service_wifi_event_callback_leave(void) {
    bool drained = false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_wifi_event_callbacks_inflight > 0) {
        --s_wifi_event_callbacks_inflight;
        drained = !s_wifi_event_callback_admission_open &&
                  s_wifi_event_callbacks_inflight == 0;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (drained && s_wifi_event_callbacks_drained) {
        xSemaphoreGive(s_wifi_event_callbacks_drained);
    }
}

device_status_t connectivity_service_stop_wifi_event_callback_admission(
    uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    bool already_drained = false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_wifi_event_callback_admission_open = false;
    already_drained = s_wifi_event_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (already_drained) return DEVICE_STATUS_OK;
    SemaphoreHandle_t drained = connectivity_wifi_event_callbacks_drained();
    if (!drained ||
        xSemaphoreTake(drained, connectivity_timeout_ticks(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_service_begin_network_request(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool admitted = s_connectivity_initialized && !s_connectivity_stopping &&
                         !s_system_sleep_preparing;
    if (admitted) ++s_network_request_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return admitted ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
}

/* Cellular HTTP must not reuse the transport-neutral admission unchanged:
 * that would let a Wi-Fi-selected generation enter the profile-private modem
 * seam.  Check the selected fault domain and take the shared drain reference
 * in one critical section, so a System Sleep/deinit marker cannot slip
 * between the selection check and request admission. An already-admitted
 * synchronous request is allowed to finish if a later configuration action
 * selects another uplink; its admission is linearized before that change and
 * the common drain still observes it. */
static device_status_t begin_cellular_network_request(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool admitted = s_connectivity_initialized && !s_connectivity_stopping &&
                          !s_system_sleep_preparing &&
                          s_active_uplink == DEVICE_UPLINK_CELLULAR &&
                          s_cellular_transport_operation_users == 0;
    if (admitted) {
        ++s_network_request_users;
        ++s_cellular_network_request_users;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return admitted ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
}

static void end_cellular_network_request(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_cellular_network_request_users) --s_cellular_network_request_users;
    if (s_network_request_users) --s_network_request_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_end_network_request(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_network_request_users) --s_network_request_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_set_system_sleep_request_canceller(
    connectivity_service_system_sleep_request_canceller_t canceller,
    void *context) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_system_sleep_request_canceller = canceller;
    s_system_sleep_request_canceller_context = context;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_set_system_sleep_request_resumer(
    connectivity_service_system_sleep_request_resumer_t resumer,
    void *context) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_system_sleep_request_resumer = resumer;
    s_system_sleep_request_resumer_context = context;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

device_status_t connectivity_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = connectivity_timeout_ticks(timeout_ms);
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    if (s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    s_wifi_event_system_sleep_preparing = true;
    s_wifi_event_system_sleep_was_admitted =
        s_wifi_event_callback_admission_open;
    s_wifi_event_callback_admission_open = false;
    const bool callbacks_drained = s_wifi_event_callbacks_inflight == 0;
    connectivity_service_system_sleep_request_canceller_t canceller =
        s_system_sleep_request_canceller;
    void *canceller_context = s_system_sleep_request_canceller_context;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    if (!callbacks_drained) {
        SemaphoreHandle_t drained = connectivity_wifi_event_callbacks_drained();
        const TickType_t remaining = connectivity_remaining_ticks(started, budget);
        if (!drained || remaining == 0 ||
            xSemaphoreTake(drained, remaining) != pdTRUE) {
            /* Both generic and callback admission remain closed until the
             * Power-owned reverse rollback. */
            return DEVICE_STATUS_TIMEOUT;
        }
    }

    /* A pre-fence Wi-Fi waiter otherwise remains blocked until its original
     * association timeout. Wake it now; it cannot publish readiness because
     * the predicate below includes s_system_sleep_preparing. */
    wake_wifi_attempt_waiters_for_system_sleep();

    /* Stop a long-poll or upload from consuming the whole parent transaction
     * budget.  This bridge deliberately knows neither Wi-Fi client nor ML307
     * implementation; it only tells the composition root to request bounded
     * cancellation of work it owns.  Keep admission closed on failure until
     * Power's mandatory rollback reopens it. */
    if (canceller) {
        /* Every participant consumes the same parent budget.  Passing the
         * original timeout here would let a slow cancel bridge run for a
         * second full interval and then still admit the physical park. */
        TickType_t cancel_remaining = connectivity_remaining_ticks(started, budget);
        if (cancel_remaining == 0) return DEVICE_STATUS_TIMEOUT;
        uint32_t cancel_timeout_ms =
            (uint32_t)cancel_remaining * (uint32_t)portTICK_PERIOD_MS;
        if (cancel_timeout_ms == 0u) cancel_timeout_ms = 1u;
        device_status_t cancel_status = canceller(cancel_timeout_ms, canceller_context);
        if (cancel_status != DEVICE_STATUS_OK) return cancel_status;
        if (connectivity_remaining_ticks(started, budget) == 0) {
            /* A callback that reports success after consuming its allowance
             * cannot prove the physical request lane is still parkable. */
            return DEVICE_STATUS_TIMEOUT;
        }
    }

    for (;;) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        const bool drained = s_wifi_attempt_users == 0 &&
                             s_network_request_users == 0 &&
                             s_cellular_network_request_users == 0 &&
                             s_cellular_transport_operation_users == 0 &&
                             s_transport_selection_users == 0;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (drained) break;
        if (connectivity_remaining_ticks(started, budget) == 0) {
            /* Retain the closed fence until the caller's required rollback.
             * This fail-closed interval prevents a just-timed-out request
             * from racing a hypothetical later electrical commit. */
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(1);
    }
    /* The generic request counter cannot see profile-retained physical work.
     * For Fangtang this parks the ML307 registration/PDP probe after all
     * admitted HTTP borrowers drain; Wi-Fi-only profiles provide a no-op.
     * It remains below the value-only Device API and is undone by ABORT. */
    const TickType_t remaining = connectivity_remaining_ticks(started, budget);
    if (remaining == 0) return DEVICE_STATUS_TIMEOUT;
    uint32_t remaining_ms = (uint32_t)remaining * (uint32_t)portTICK_PERIOD_MS;
    if (remaining_ms == 0u) remaining_ms = 1u;
    const device_status_t transport_status =
        platform_connectivity_prepare_system_sleep(remaining_ms);
    if (transport_status != DEVICE_STATUS_OK) return transport_status;
    /* A profile callback may synchronously consume its complete allowance
     * while still returning OK.  In that case its physical park result is too
     * late to prove safe completion of this parent transaction; keep the
     * logical fence closed and require the caller's ABORT path. */
    if (connectivity_remaining_ticks(started, budget) == 0) {
        return DEVICE_STATUS_TIMEOUT;
    }
    return DEVICE_STATUS_OK;
}

void connectivity_service_abort_system_sleep_prepare(void) {
    connectivity_service_system_sleep_request_resumer_t resumer;
    void *resumer_context;
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* Take the callback snapshot while the transaction remains closed. A
     * restart must not admit a new request until its old worker state has
     * been restored (or has logged its bounded failure). */
    resumer = s_system_sleep_request_resumer;
    resumer_context = s_system_sleep_request_resumer_context;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (resumer) resumer(resumer_context);
    /* Resume the private transport before reopening generic request admission.
     * The profile does not restart a modem; it only restores the generation
     * that PREPARE had parked. */
    platform_connectivity_abort_system_sleep_prepare();
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_system_sleep_preparing) {
        s_wifi_event_callback_admission_open =
            s_wifi_event_system_sleep_was_admitted;
        s_wifi_event_system_sleep_was_admitted = false;
        s_wifi_event_system_sleep_preparing = false;
        s_system_sleep_preparing = false;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

static EventGroupHandle_t acquire_wifi_attempt_events(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* A System Sleep PREPARE is a reversible, but deliberately fail-closed,
     * admission fence.  A station attempt owns mutable adapter state and its
     * EventGroup wait; do not let a late scan/connect path create or observe
     * a new attempt after the Power transaction has started draining network
     * work.  ABORT reopens this gate without rebuilding the Connectivity
     * generation. */
    EventGroupHandle_t events = !s_connectivity_stopping &&
                                  !s_system_sleep_preparing
                              ? s_wifi_attempt_events
                              : NULL;
    if (events) ++s_wifi_attempt_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return events;
}

static void release_wifi_attempt_events(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_wifi_attempt_users) --s_wifi_attempt_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

/* PREPARE has already closed future EventGroup admission, but an adapter may
 * have obtained a wait admission immediately before that marker.  Retain one
 * internal reference while setting the wake bit so deinit cannot delete the
 * EventGroup underneath this signal.  The waiter re-checks the same marker
 * and exits false; the subsequent PREPARE drain observes its release. */
static void wake_wifi_attempt_waiters_for_system_sleep(void) {
    EventGroupHandle_t events = NULL;
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_connectivity_initialized && !s_connectivity_stopping &&
        s_system_sleep_preparing && s_wifi_attempt_events) {
        events = s_wifi_attempt_events;
        ++s_wifi_attempt_users;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (!events) return;
    xEventGroupSetBits(events, WIFI_ATTEMPT_READY_BIT);
    release_wifi_attempt_events();
}

static esp_err_t connectivity_service_initialize_legacy(void) {
    SemaphoreHandle_t deinit_lock = connectivity_deinit_lock();
    if (!deinit_lock ||
        xSemaphoreTake(deinit_lock,
                       connectivity_timeout_ticks(CONNECTIVITY_INIT_LOCK_TIMEOUT_MS)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool initialized = s_connectivity_initialized;
    bool stopping = s_connectivity_stopping;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (initialized) {
        xSemaphoreGive(deinit_lock);
        return ESP_OK;
    }
    if (stopping) {
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (!fault_domain_begin_start(&s_fault_domain)) {
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }

    EventGroupHandle_t events = xEventGroupCreate();
    if (!events) {
        (void)fault_domain_mark_stopped(&s_fault_domain);
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_NO_MEM;
    }

    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized && !s_connectivity_stopping) {
        s_wifi_attempt_events = events;
        events = NULL;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (events) {
        vEventGroupDelete(events);
        (void)fault_domain_mark_stopped(&s_fault_domain);
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    /* EventGroup publication is Connectivity's physical self-test. Do not
     * reopen generic Wi-Fi/HTTP admission solely because allocation succeeded. */
    if (!fault_domain_begin_self_test(&s_fault_domain) ||
        !s_wifi_attempt_events ||
        !fault_domain_mark_ready(&s_fault_domain)) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        s_connectivity_initialized = false;
        events = s_wifi_attempt_events;
        s_wifi_attempt_events = NULL;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (events) vEventGroupDelete(events);
        (void)fault_domain_mark_stopped(&s_fault_domain);
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    /* Publish common admission only after the fault domain has crossed its
     * self-test barrier.  A concurrent Wi-Fi/HTTP caller may otherwise obtain
     * an EventGroup reference in the small interval before READY. */
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_connectivity_initialized = true;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    xSemaphoreGive(deinit_lock);
    return ESP_OK;
}

static esp_err_t connectivity_service_deinit_legacy(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = connectivity_timeout_ticks(timeout_ms);
    SemaphoreHandle_t deinit_lock = connectivity_deinit_lock();
    TickType_t remaining = connectivity_remaining_ticks(started, budget);
    if (!deinit_lock || remaining == 0 ||
        xSemaphoreTake(deinit_lock, remaining) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    EventGroupHandle_t events = NULL;
    fault_domain_snapshot_t domain_snapshot;
    if (!fault_domain_get_snapshot(&s_fault_domain, &domain_snapshot) ||
        (domain_snapshot.phase != FAULT_DOMAIN_READY &&
         domain_snapshot.phase != FAULT_DOMAIN_UNKNOWN_OUTCOME)) {
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized && !s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        xSemaphoreGive(deinit_lock);
        return ESP_OK;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (!fault_domain_begin_quiesce(&s_fault_domain)) {
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_connectivity_stopping = true;
    s_connectivity_initialized = false;
    s_wifi_ready = false;
    s_cellular_ready = false;
    s_system_sleep_preparing = false;
    s_wifi_ready_epoch = 0;
    s_provisioning_active = false;
    s_provisioning_pairing_recovery = false;
    s_wifi_event_callback_admission_open = false;
    s_wifi_event_system_sleep_preparing = false;
    s_wifi_event_system_sleep_was_admitted = false;
    events = s_wifi_attempt_events;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    /* Wake all current station waiters. They re-check the closed generation
     * before returning and release their EventGroup admission. */
    if (events) xEventGroupSetBits(events, WIFI_ATTEMPT_READY_BIT);
    for (;;) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        /* A deinit must not delete/reconfigure the Connectivity generation
         * while either a Wi-Fi attempt waiter or a transport-neutral HTTP
         * borrower still holds its short-lived logical admission. */
        bool drained = s_wifi_attempt_users == 0 && s_network_request_users == 0 &&
                       s_cellular_network_request_users == 0 &&
                       s_cellular_transport_operation_users == 0 &&
                       s_transport_selection_users == 0;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (drained) break;
        if (connectivity_remaining_ticks(started, budget) == 0) {
            /* Keep the generation closed. A later rollback/deinit retry may
             * consume the still-live waiter's admission; initialize must not
             * create a replacement EventGroup in the meantime. */
            xSemaphoreGive(deinit_lock);
            (void)fault_domain_mark_unknown_outcome(&s_fault_domain);
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(1);
    }

    taskENTER_CRITICAL(&s_connectivity_lock);
    /* Admission remained closed throughout the drain, so no caller can have
     * obtained a new reference after the zero-user observation above. */
    events = s_wifi_attempt_events;
    s_wifi_attempt_events = NULL;
    s_wifi_attempt_epoch = 0;
    s_wifi_ready_epoch = 0;
    s_wifi_attempt_network_id[0] = '\0';
    /* A new generation can be opened only by the explicit composition-root
     * initialize call.  Wi-Fi callbacks and stale start paths cannot lazily
     * resurrect an EventGroup after rollback has released its physical stack. */
    s_connectivity_stopping = false;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (events) vEventGroupDelete(events);
    if (!fault_domain_mark_stopped(&s_fault_domain)) {
        /* No EventGroup is published, so the next caller cannot access an old
         * handle. Still keep fault recovery closed rather than assuming the
         * lifecycle bookkeeping succeeded. */
        (void)fault_domain_mark_unknown_outcome(&s_fault_domain);
        xSemaphoreGive(deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    xSemaphoreGive(deinit_lock);
    return ESP_OK;
}

/* The selected-uplink state is common service policy. Its EventGroup and
 * shutdown wait are ESP-IDF implementation details, so translate them once
 * at this boundary instead of making every Wi-Fi or future modem profile
 * interpret ESP errors. */
static device_status_t connectivity_status_from_legacy_error(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t connectivity_service_initialize(void) {
    return connectivity_status_from_legacy_error(connectivity_service_initialize_legacy());
}

device_status_t connectivity_service_deinit(uint32_t timeout_ms) {
    return connectivity_status_from_legacy_error(
        connectivity_service_deinit_legacy(timeout_ms));
}
uint32_t connectivity_service_begin_wifi_attempt(const char *network_id) {
    if (!network_id || !network_id[0]) return 0;
    EventGroupHandle_t events = acquire_wifi_attempt_events();
    if (!events) return 0;
    taskENTER_CRITICAL(&s_connectivity_lock);
    ++s_wifi_attempt_epoch;
    if (s_wifi_attempt_epoch == 0) ++s_wifi_attempt_epoch;
    s_wifi_ready = false;
    s_wifi_ready_epoch = 0;
    /* Keep only the configured network identity, never credentials. */
    strncpy(s_wifi_attempt_network_id, network_id,
            sizeof(s_wifi_attempt_network_id) - 1);
    s_wifi_attempt_network_id[sizeof(s_wifi_attempt_network_id) - 1] = '\0';
    uint32_t epoch = s_wifi_attempt_epoch;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    /* A callback that wins this small race publishes readiness with the new
     * epoch, so wait_wifi_attempt() observes the predicate even if this clear
     * erases its wake bit. Conversely, a stale bit cannot pass the predicate. */
    xEventGroupClearBits(events, WIFI_ATTEMPT_READY_BIT);
    release_wifi_attempt_events();
    return epoch;
}

bool connectivity_service_wait_wifi_attempt(uint32_t attempt_epoch,
                                             uint32_t timeout_ms) {
    if (attempt_epoch == 0 || timeout_ms == 0) return false;
    EventGroupHandle_t events = acquire_wifi_attempt_events();
    if (!events) return false;
    TickType_t started = xTaskGetTickCount();
    TickType_t timeout = pdMS_TO_TICKS(timeout_ms);
    if (timeout == 0) timeout = 1;
    bool ready = false;
    for (;;) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        bool stopping = s_connectivity_stopping || s_system_sleep_preparing;
        ready = !stopping && s_wifi_attempt_epoch == attempt_epoch && s_wifi_ready &&
                s_wifi_ready_epoch == attempt_epoch;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (ready || stopping) break;

        TickType_t elapsed = xTaskGetTickCount() - started;
        if (elapsed >= timeout) break;
        (void)xEventGroupWaitBits(events, WIFI_ATTEMPT_READY_BIT,
                                   pdTRUE, pdFALSE, timeout - elapsed);
    }
    release_wifi_attempt_events();
    return ready;
}

bool connectivity_service_observe_wifi_disconnected(const char *network_id) {
    if (!network_id || !network_id[0]) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool accepted = s_connectivity_initialized && !s_connectivity_stopping &&
                           !s_system_sleep_preparing &&
                           s_wifi_attempt_epoch != 0 &&
                          strcmp(network_id, s_wifi_attempt_network_id) == 0;
    if (accepted) {
        s_wifi_ready = false;
        s_wifi_ready_epoch = 0;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return accepted;
}

bool connectivity_service_observe_wifi_got_ip(const char *connected_network_id) {
    if (!connected_network_id || !connected_network_id[0]) return false;
    EventGroupHandle_t events = acquire_wifi_attempt_events();
    if (!events) return false;
    bool accepted = false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* DHCP events carry no connection attempt ID.  Validate the adapter's
     * current association against the session's network identity before
     * publishing; otherwise a late old-candidate event can satisfy a new
     * candidate's synchronous wait after a scan/switch. */
    if (s_connectivity_initialized && !s_connectivity_stopping &&
        !s_system_sleep_preparing && s_wifi_attempt_epoch != 0 &&
        strcmp(connected_network_id, s_wifi_attempt_network_id) == 0) {
        s_wifi_ready = true;
        s_wifi_ready_epoch = s_wifi_attempt_epoch;
        accepted = true;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (accepted) xEventGroupSetBits(events, WIFI_ATTEMPT_READY_BIT);
    release_wifi_attempt_events();
    return accepted;
}

static bool acquire_transport_selection_admission(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* Uplink selection remains available before Connectivity initialization:
     * Fangtang restores durable policy before the composition root decides
     * whether to build Wi-Fi or ML307.  Once PREPARE begins, however, no new
     * profile-side selection transaction may enter. */
    /* Changing the profile hint is not merely a presentation update: on
     * Fangtang it can change which transport adapter owns subsequent work.
     * A request that was already admitted must finish/cancel against the same
     * physical hint, so selection waits for *all* generic and cellular request
     * borrowers to leave instead of racing an in-flight ML307 request. The
     * caller gets a fail-closed no-op and may retry after the synchronous
     * request returns. */
    const bool admitted = !s_connectivity_stopping && !s_system_sleep_preparing &&
                          s_network_request_users == 0 &&
                          s_cellular_network_request_users == 0 &&
                          s_cellular_transport_operation_users == 0;
    if (admitted) ++s_transport_selection_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return admitted;
}

bool connectivity_service_get_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot) {
    return fault_domain_get_snapshot(&s_fault_domain, out_snapshot);
}

static void release_transport_selection_admission(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_transport_selection_users) --s_transport_selection_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

/* The caller owns a transport-selection admission.  PREPARE may have closed
 * new admission while this bounded operation was in flight, but it waits for
 * that admission to drain before its profile-side physical park; completing
 * the already-admitted state/adapter transaction therefore remains coherent. */
static bool commit_active_uplink_under_admission(device_uplink_t uplink,
                                                  bool sync_platform_hint) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* Selection is durable configuration and is intentionally allowed before
     * the Wi-Fi attempt EventGroup exists: Fangtang restores it before the
     * composition root chooses whether to create a Wi-Fi or ML307 session.
     * It still invalidates the selected transport's runtime observation below;
     * deinit keeps all readiness publication closed. */
    const bool selection_changed = s_active_uplink != uplink;
    if (selection_changed) {
        s_active_uplink = uplink;
        /* A readiness observation belongs to one bounded transport session.
         * When a profile changes its selected uplink, a prior session of the
         * newly selected adapter must not make the shared query report ready
         * before that adapter has completed a fresh start/recovery cycle. */
        if (uplink == DEVICE_UPLINK_CELLULAR) {
            s_cellular_ready = false;
        } else {
            s_wifi_ready = false;
        }
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    /* This is deliberately outside the critical section: a profile adapter
     * may persist its hint or touch a modem-side guard.  The business-visible
     * state is already fail-closed (not ready) before that physical work
     * starts, so a stale prior session can never be reported as ready during
     * an uplink transition. */
    if (selection_changed && sync_platform_hint) {
        platform_connectivity_set_network_transport(uplink == DEVICE_UPLINK_CELLULAR);
    }
    return selection_changed;
}

void connectivity_service_set_active_uplink(device_uplink_t uplink) {
    if (uplink != DEVICE_UPLINK_WIFI && uplink != DEVICE_UPLINK_CELLULAR) return;
    if (!acquire_transport_selection_admission()) return;
    (void)commit_active_uplink_under_admission(uplink, true);
    release_transport_selection_admission();
}

bool connectivity_service_is_active_cellular(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool cellular = s_active_uplink == DEVICE_UPLINK_CELLULAR;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return cellular;
}

void connectivity_service_restore_selected_uplink(void) {
    if (!acquire_transport_selection_admission()) return;
    bool cellular = false;
    if (!platform_connectivity_load_transport_selection(&cellular)) cellular = false;
    (void)commit_active_uplink_under_admission(
        cellular ? DEVICE_UPLINK_CELLULAR : DEVICE_UPLINK_WIFI, true);
    release_transport_selection_admission();
}

bool connectivity_service_apply_startup_transport_toggle(uint32_t window_ms) {
    if (window_ms == 0) return false;
    if (!acquire_transport_selection_admission()) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool current_cellular = s_active_uplink == DEVICE_UPLINK_CELLULAR;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    bool selected_cellular = current_cellular;
    if (!platform_connectivity_apply_startup_transport_toggle(
            window_ms, current_cellular, &selected_cellular)) {
        release_transport_selection_admission();
        return false;
    }
    /* The profile callback owns the bounded boot gesture and durable choice.
     * Commit that choice through the same common/profile transport path as
     * every other uplink switch so the runtime adapter hint stays aligned. */
    (void)commit_active_uplink_under_admission(
        selected_cellular ? DEVICE_UPLINK_CELLULAR : DEVICE_UPLINK_WIFI, true);
    release_transport_selection_admission();
    return true;
}

void connectivity_service_set_wifi_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping ||
        s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return;
    }
    s_wifi_ready = ready;
    s_wifi_ready_epoch = ready ? s_wifi_attempt_epoch : 0;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_set_cellular_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping ||
        s_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return;
    }
    s_cellular_ready = ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_is_active_uplink_ready(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool ready = s_connectivity_initialized && !s_connectivity_stopping &&
                 !s_system_sleep_preparing &&
                 (s_active_uplink == DEVICE_UPLINK_CELLULAR
                     ? s_cellular_ready
                     : s_wifi_ready);
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return ready;
}

bool connectivity_service_get_snapshot(device_connectivity_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool available = s_connectivity_initialized && !s_connectivity_stopping &&
                           !s_system_sleep_preparing;
    out_snapshot->active_uplink = s_active_uplink;
    out_snapshot->wifi_ready = available && s_wifi_ready;
    out_snapshot->cellular_ready = available && s_cellular_ready;
    out_snapshot->ready = available && (s_active_uplink == DEVICE_UPLINK_CELLULAR
                              ? s_cellular_ready
                              : s_wifi_ready);
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return true;
}

void connectivity_service_begin_provisioning(bool pairing_recovery) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return;
    }
    s_provisioning_pairing_recovery = pairing_recovery;
    s_provisioning_active = true;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_end_provisioning(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    /* Clear the mode together with admission so a later portal handler or
     * background worker cannot mistake an ended normal setup session for a
     * credential-recovery flow. */
    s_provisioning_active = false;
    s_provisioning_pairing_recovery = false;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_is_provisioning_active(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool active = s_connectivity_initialized && !s_connectivity_stopping &&
                  s_provisioning_active;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return active;
}

bool connectivity_service_is_pairing_recovery_provisioning(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool pairing_recovery = s_connectivity_initialized && !s_connectivity_stopping &&
                            s_provisioning_active && s_provisioning_pairing_recovery;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return pairing_recovery;
}

static bool cellular_http_request_is_valid(
    const device_connectivity_http_request_t *request) {
    return request && request->method && request->method[0] && request->url &&
           request->url[0] && request->response && request->response_capacity >= 2 &&
           request->response_len && request->status_code && request->truncated &&
           request->timeout_ms > 0;
}

/* Linearize a physical cellular lifecycle call with the selected profile.
 * Unlike an HTTP admission this only protects the bounded adapter call, but
 * it takes the same lifecycle/sleep/selection predicate in one critical
 * section.  Sleep/deinit may close admission after this point; their drain
 * waits for the retained reference before touching the physical generation. */
static bool begin_cellular_transport_operation(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool admitted = s_connectivity_initialized && !s_connectivity_stopping &&
                          !s_system_sleep_preparing &&
                          s_active_uplink == DEVICE_UPLINK_CELLULAR &&
                          s_cellular_transport_operation_users == 0 &&
                          s_network_request_users == 0 &&
                          s_cellular_network_request_users == 0;
    if (admitted) ++s_cellular_transport_operation_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return admitted;
}

static void end_cellular_transport_operation(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_cellular_transport_operation_users) {
        --s_cellular_transport_operation_users;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

/* Quiesce is terminal for the current cellular session, so it must remain
 * available to a root teardown even after a persisted uplink selection has
 * moved to Wi-Fi. It still may not enter from a Wi-Fi-only normal generation:
 * require an observable current/physical cellular session before admitting
 * the profile-private drain. This preserves the fault-domain split while
 * letting a stale but live ML307 session be retired deterministically. */
static bool begin_cellular_transport_quiesce(void) {
    /* The profile query can take an adapter-private lock, so sample it before
     * taking the common service lock. A false-to-true transition only makes a
     * conservative retry necessary; the ensuing bounded quiesce is itself
     * serialized by the operation admission below. */
    const bool physically_ready = platform_connectivity_is_cellular_transport_ready();
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool session_exists = s_active_uplink == DEVICE_UPLINK_CELLULAR ||
                                s_cellular_ready ||
                                physically_ready;
    const bool admitted = s_connectivity_initialized && !s_connectivity_stopping &&
                          !s_system_sleep_preparing && session_exists &&
                          s_cellular_transport_operation_users == 0 &&
                          s_network_request_users == 0 &&
                          s_cellular_network_request_users == 0;
    if (admitted) ++s_cellular_transport_operation_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return admitted;
}

device_status_t connectivity_service_prepare_cellular_transport(void) {
    if (!begin_cellular_transport_operation()) return DEVICE_STATUS_BUSY;
    const device_status_t status = platform_connectivity_prepare_cellular_transport();
    end_cellular_transport_operation();
    return status;
}

device_status_t connectivity_service_start_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!begin_cellular_transport_operation()) return DEVICE_STATUS_BUSY;
    const device_status_t status =
        platform_connectivity_start_cellular_transport(timeout_ms);
    end_cellular_transport_operation();
    return status;
}

device_status_t connectivity_service_establish_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;

    /* Fail closed before the adapter touches UART, power or the modem. This
     * readiness predicate belongs to the bounded service generation, not to a
     * particular recovery task in the composition root. */
    if (!begin_cellular_transport_operation()) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        const bool sleep_preparing = s_system_sleep_preparing;
        const bool available =
            s_connectivity_initialized && !s_connectivity_stopping;
        const bool cellular_selected =
            s_active_uplink == DEVICE_UPLINK_CELLULAR;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (sleep_preparing || !cellular_selected) return DEVICE_STATUS_BUSY;
        return available ? DEVICE_STATUS_BUSY : DEVICE_STATUS_UNAVAILABLE;
    }
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_cellular_ready = false;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    device_status_t status = platform_connectivity_prepare_cellular_transport();
    if (status != DEVICE_STATUS_OK) {
        end_cellular_transport_operation();
        return status;
    }
    status = platform_connectivity_start_cellular_transport(timeout_ms);
    if (status != DEVICE_STATUS_OK) {
        end_cellular_transport_operation();
        return status;
    }

    /* A physical start can finish after lifecycle rollback or uplink selection
     * changes. Revalidate the same logical generation before publishing it. */
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool still_current = s_connectivity_initialized && !s_connectivity_stopping &&
                               !s_system_sleep_preparing &&
                               s_active_uplink == DEVICE_UPLINK_CELLULAR;
    if (still_current) s_cellular_ready = true;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    end_cellular_transport_operation();
    return still_current ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
}

bool connectivity_service_is_cellular_transport_ready(void) {
    /* The adapter's physical indication alone is insufficient: it can still
     * describe the previous ML307 session while a new service-owned start has
     * already failed closed.  Conversely, a published service readiness must
     * still reflect a later physical link loss.  Require both observations so
     * callers cannot bypass the bounded session transition above. */
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool published_ready = s_connectivity_initialized && !s_connectivity_stopping &&
                                 !s_system_sleep_preparing &&
                                 s_active_uplink == DEVICE_UPLINK_CELLULAR &&
                                 s_cellular_ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return published_ready && platform_connectivity_is_cellular_transport_ready();
}

bool connectivity_service_has_cellular_transport_session(void) {
    const bool physically_ready = platform_connectivity_is_cellular_transport_ready();
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool session_exists = s_connectivity_initialized && !s_connectivity_stopping &&
                                (s_active_uplink == DEVICE_UPLINK_CELLULAR ||
                                 s_cellular_ready || physically_ready);
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return session_exists;
}

device_status_t connectivity_service_quiesce_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* Close the service-side observation before asking the profile to drain
     * ML307 work. A timeout leaves the physical adapter to finish its own
     * bounded quiesce, but callers must already see this session as offline. */
    if (!begin_cellular_transport_quiesce()) return DEVICE_STATUS_BUSY;
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_cellular_ready = false;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    const device_status_t status =
        platform_connectivity_quiesce_cellular_transport(timeout_ms);
    end_cellular_transport_operation();
    return status;
}

device_status_t connectivity_service_deinit_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!begin_cellular_transport_quiesce()) return DEVICE_STATUS_BUSY;
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_cellular_ready = false;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    const device_status_t status = platform_connectivity_deinit_cellular_transport(timeout_ms);
    end_cellular_transport_operation();
    return status;
}

device_status_t connectivity_service_reinitialize_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!begin_cellular_transport_operation()) return DEVICE_STATUS_BUSY;
    const device_status_t status =
        platform_connectivity_reinitialize_cellular_transport(timeout_ms);
    taskENTER_CRITICAL(&s_connectivity_lock);
    s_cellular_ready = status == DEVICE_STATUS_OK &&
                       platform_connectivity_is_cellular_transport_ready();
    taskEXIT_CRITICAL(&s_connectivity_lock);
    end_cellular_transport_operation();
    return status;
}

device_status_t connectivity_service_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!cellular_http_request_is_valid(request)) return DEVICE_STATUS_INVALID_ARGUMENT;
    device_status_t admission = begin_cellular_network_request();
    if (admission != DEVICE_STATUS_OK) return admission;
    device_status_t status = platform_connectivity_cellular_http_request(request);
    end_cellular_network_request();
    return status;
}

device_status_t connectivity_service_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || !cellular_http_request_is_valid(&request->request) ||
        !request->body_reader || !request->stream_buffer ||
        request->stream_buffer_size == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    device_status_t admission = begin_cellular_network_request();
    if (admission != DEVICE_STATUS_OK) return admission;
    device_status_t status = platform_connectivity_cellular_http_stream_request(request);
    end_cellular_network_request();
    return status;
}

bool connectivity_service_cancel_cellular_foreground_request(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool active_request = s_cellular_network_request_users != 0;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return active_request && platform_connectivity_cancel_cellular_foreground_request();
}

bool connectivity_service_cancel_cellular_requests_for_owner(const void *owner) {
    if (!owner) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool active_request = s_cellular_network_request_users != 0;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return active_request && platform_connectivity_cancel_cellular_requests_for_owner(owner);
}

void connectivity_service_adapt_gateway_url(char *gateway_url,
                                             uint32_t gateway_url_capacity) {
    if (!gateway_url || gateway_url_capacity == 0) return;
    platform_connectivity_adapt_gateway_url(gateway_url, gateway_url_capacity,
                                            connectivity_service_is_active_cellular());
}
