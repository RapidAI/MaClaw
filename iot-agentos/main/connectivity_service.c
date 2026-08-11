#include "connectivity_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/task.h"

#include "platform_connectivity.h"

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

static EventGroupHandle_t acquire_wifi_attempt_events(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    EventGroupHandle_t events = !s_connectivity_stopping ? s_wifi_attempt_events : NULL;
    if (events) ++s_wifi_attempt_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return events;
}

static void release_wifi_attempt_events(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (s_wifi_attempt_users) --s_wifi_attempt_users;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_initialize(void) {
    SemaphoreHandle_t deinit_lock = connectivity_deinit_lock();
    if (!deinit_lock ||
        xSemaphoreTake(deinit_lock,
                       connectivity_timeout_ticks(CONNECTIVITY_INIT_LOCK_TIMEOUT_MS)) != pdTRUE) {
        return false;
    }
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool initialized = s_connectivity_initialized;
    bool stopping = s_connectivity_stopping;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (initialized) {
        xSemaphoreGive(deinit_lock);
        return true;
    }
    if (stopping) {
        xSemaphoreGive(deinit_lock);
        return false;
    }

    EventGroupHandle_t events = xEventGroupCreate();
    if (!events) {
        xSemaphoreGive(deinit_lock);
        return false;
    }

    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized && !s_connectivity_stopping) {
        s_wifi_attempt_events = events;
        s_connectivity_initialized = true;
        events = NULL;
    }
    taskEXIT_CRITICAL(&s_connectivity_lock);
    if (events) vEventGroupDelete(events);
    xSemaphoreGive(deinit_lock);
    return true;
}

esp_err_t connectivity_service_deinit(uint32_t timeout_ms) {
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
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized && !s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        xSemaphoreGive(deinit_lock);
        return ESP_OK;
    }
    s_connectivity_stopping = true;
    s_connectivity_initialized = false;
    s_wifi_ready = false;
    s_cellular_ready = false;
    s_wifi_ready_epoch = 0;
    s_provisioning_active = false;
    s_provisioning_pairing_recovery = false;
    events = s_wifi_attempt_events;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    /* Wake all current station waiters. They re-check the closed generation
     * before returning and release their EventGroup admission. */
    if (events) xEventGroupSetBits(events, WIFI_ATTEMPT_READY_BIT);
    for (;;) {
        taskENTER_CRITICAL(&s_connectivity_lock);
        bool drained = s_wifi_attempt_users == 0;
        taskEXIT_CRITICAL(&s_connectivity_lock);
        if (drained) break;
        if (connectivity_remaining_ticks(started, budget) == 0) {
            /* Keep the generation closed. A later rollback/deinit retry may
             * consume the still-live waiter's admission; initialize must not
             * create a replacement EventGroup in the meantime. */
            xSemaphoreGive(deinit_lock);
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
    xSemaphoreGive(deinit_lock);
    return ESP_OK;
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
        bool stopping = s_connectivity_stopping;
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
    if (!s_connectivity_stopping && s_wifi_attempt_epoch != 0 &&
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

void connectivity_service_set_active_uplink(device_uplink_t uplink) {
    if (uplink != DEVICE_UPLINK_WIFI && uplink != DEVICE_UPLINK_CELLULAR) return;
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
    if (selection_changed) {
        platform_connectivity_set_network_transport(uplink == DEVICE_UPLINK_CELLULAR);
    }
}

bool connectivity_service_is_active_cellular(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool cellular = s_active_uplink == DEVICE_UPLINK_CELLULAR;
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return cellular;
}

bool connectivity_service_take_startup_transport_toggle(uint32_t window_ms) {
    if (window_ms == 0) return false;
    return platform_connectivity_take_startup_transport_toggle(window_ms);
}

void connectivity_service_restore_selected_uplink(void) {
    bool cellular = false;
    if (!platform_connectivity_load_transport_selection(&cellular)) cellular = false;
    connectivity_service_set_active_uplink(cellular ? DEVICE_UPLINK_CELLULAR
                                                    : DEVICE_UPLINK_WIFI);
}

bool connectivity_service_apply_startup_transport_toggle(uint32_t window_ms) {
    if (window_ms == 0) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool current_cellular = s_active_uplink == DEVICE_UPLINK_CELLULAR;
    taskEXIT_CRITICAL(&s_connectivity_lock);

    bool selected_cellular = current_cellular;
    if (!platform_connectivity_apply_startup_transport_toggle(
            window_ms, current_cellular, &selected_cellular)) {
        return false;
    }
    connectivity_service_set_active_uplink(selected_cellular ? DEVICE_UPLINK_CELLULAR
                                                              : DEVICE_UPLINK_WIFI);
    return true;
}

void connectivity_service_set_wifi_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return;
    }
    s_wifi_ready = ready;
    s_wifi_ready_epoch = ready ? s_wifi_attempt_epoch : 0;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

void connectivity_service_set_cellular_ready(bool ready) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    if (!s_connectivity_initialized || s_connectivity_stopping) {
        taskEXIT_CRITICAL(&s_connectivity_lock);
        return;
    }
    s_cellular_ready = ready;
    taskEXIT_CRITICAL(&s_connectivity_lock);
}

bool connectivity_service_is_active_uplink_ready(void) {
    taskENTER_CRITICAL(&s_connectivity_lock);
    bool ready = s_connectivity_initialized && !s_connectivity_stopping &&
                 (s_active_uplink == DEVICE_UPLINK_CELLULAR
                     ? s_cellular_ready
                     : s_wifi_ready);
    taskEXIT_CRITICAL(&s_connectivity_lock);
    return ready;
}

bool connectivity_service_get_snapshot(device_connectivity_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_connectivity_lock);
    const bool available = s_connectivity_initialized && !s_connectivity_stopping;
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
