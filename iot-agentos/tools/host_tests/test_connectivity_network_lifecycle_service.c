#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/connectivity_network_lifecycle_service.h"

typedef struct {
    uint64_t now_ms;
    bool logical;
    bool physical;
    bool wifi;
    bool radio_stopped;
    int configured;
    int opened_callback_admission;
    int stops;
    int deinit_calls;
    device_status_t core_result;
    device_status_t wifi_result;
    bool core_claims_ready;
    bool wifi_claims_ready;
    bool stop_leaves_resources;
    bool deinit_consumes_budget;
} state_t;

static uint64_t now(void *context) { return ((state_t *)context)->now_ms; }
static device_status_t init_logical(void *context) {
    ((state_t *)context)->logical = true; return DEVICE_STATUS_OK;
}
static device_status_t configure(void *context) {
    ++((state_t *)context)->configured; return DEVICE_STATUS_OK;
}
static bool has_physical(void *context) { return ((state_t *)context)->physical || ((state_t *)context)->wifi; }
static bool core_ready(void *context) {
    state_t *s = context;
    return s->physical && s->core_claims_ready;
}
static device_status_t ensure_core(void *context) {
    state_t *s = context;
    if (s->core_result != DEVICE_STATUS_OK) return s->core_result;
    s->physical = true;
    return DEVICE_STATUS_OK;
}
static bool has_wifi(void *context) { return ((state_t *)context)->wifi; }
static bool wifi_ready(void *context) {
    state_t *s = context;
    return s->wifi && s->wifi_claims_ready;
}
static device_status_t init_wifi(void *context) {
    state_t *s = context;
    if (s->wifi_result != DEVICE_STATUS_OK) return s->wifi_result;
    s->wifi = true;
    return DEVICE_STATUS_OK;
}
static void open_admission(void *context) { ++((state_t *)context)->opened_callback_admission; }
static device_status_t stop_physical(uint32_t timeout_ms, bool *radio_stopped,
                                     void *context) {
    assert(timeout_ms > 0);
    state_t *s = context;
    ++s->stops;
    if (radio_stopped) *radio_stopped = s->wifi;
    s->radio_stopped = s->wifi;
    if (s->stop_leaves_resources) return DEVICE_STATUS_OK;
    s->wifi = false;
    s->physical = false;
    return DEVICE_STATUS_OK;
}
static device_status_t deinit_logical(uint32_t timeout_ms, void *context) {
    assert(timeout_ms > 0);
    state_t *s = context;
    ++s->deinit_calls;
    s->logical = false;
    if (s->deinit_consumes_budget) s->now_ms += timeout_ms;
    return DEVICE_STATUS_OK;
}
static connectivity_network_lifecycle_service_host_t host_for(state_t *state) {
    return (connectivity_network_lifecycle_service_host_t){
        .struct_size = sizeof(connectivity_network_lifecycle_service_host_t),
        .now_ms = now, .initialize_logical = init_logical,
        .configure_physical_lifecycle = configure, .physical_has_resources = has_physical,
        .physical_core_ready = core_ready, .ensure_physical_core = ensure_core,
        .wifi_has_resources = has_wifi, .wifi_ready = wifi_ready,
        .initialize_wifi = init_wifi, .open_wifi_callback_admission = open_admission,
        .stop_physical = stop_physical, .deinitialize_logical = deinit_logical,
        .context = state,
    };
}

int main(void) {
    state_t state = {.core_result = DEVICE_STATUS_OK, .wifi_result = DEVICE_STATUS_OK,
                     .core_claims_ready = true, .wifi_claims_ready = true};
    connectivity_network_lifecycle_service_host_t host = host_for(&state);
    assert(connectivity_network_lifecycle_service_init(&host) == DEVICE_STATUS_OK);
    assert(connectivity_network_lifecycle_service_ensure_wifi() == DEVICE_STATUS_OK);
    assert(state.logical && state.physical && state.wifi && state.configured == 1);
    assert(state.opened_callback_admission == 1);
    bool radio_stopped = false;
    assert(connectivity_network_lifecycle_service_stop(100, &radio_stopped) == DEVICE_STATUS_OK);
    assert(radio_stopped && state.radio_stopped && !state.logical && !state.physical && !state.wifi);

    /* Driver-route allocation failure must use the identical terminal
     * physical->logical rollback rather than leave a live logical generation
     * behind a failed cold-start. */
    state.wifi_result = DEVICE_STATUS_RESOURCE_EXHAUSTED;
    assert(connectivity_network_lifecycle_service_ensure_wifi() ==
           DEVICE_STATUS_RESOURCE_EXHAUSTED);
    assert(!state.logical && !state.physical && !state.wifi && state.stops == 2);
    state.wifi_result = DEVICE_STATUS_OK;

    /* A successful physical callback that leaves no ready core must not
     * publish a usable logical generation. */
    state.core_claims_ready = false;
    assert(connectivity_network_lifecycle_service_ensure_core() == DEVICE_STATUS_BUSY);
    assert(!state.logical && !state.physical);
    state.core_claims_ready = true;

    /* Likewise, a driver callback that returns OK but leaves Wi-Fi unready
     * must roll back before callback admission is opened. */
    state.wifi_claims_ready = false;
    assert(connectivity_network_lifecycle_service_ensure_wifi() == DEVICE_STATUS_BUSY);
    assert(!state.logical && !state.physical && !state.wifi);
    state.wifi_claims_ready = true;

    /* A terminal stop does not make a physical partial root a usable second
     * generation: a pre-existing non-ready residual is refused before any
     * new logical init. */
    state.wifi = true;
    assert(connectivity_network_lifecycle_service_ensure_core() == DEVICE_STATUS_BUSY);

    /* A physical stop callback may return OK while a callback/netif resource
     * remains live. Logical deinit must not run in that ambiguous window. */
    state.logical = true;
    state.physical = true;
    state.stop_leaves_resources = true;
    const int deinit_before_partial_stop = state.deinit_calls;
    assert(connectivity_network_lifecycle_service_stop(100, NULL) == DEVICE_STATUS_BUSY);
    assert(state.logical && state.physical && state.deinit_calls == deinit_before_partial_stop);
    state.stop_leaves_resources = false;
    assert(connectivity_network_lifecycle_service_stop(100, NULL) == DEVICE_STATUS_OK);
    assert(!state.logical && !state.physical && !state.wifi);

    /* Logical deinit must also be fenced by the same parent deadline. */
    state.logical = true;
    state.physical = true;
    state.deinit_consumes_budget = true;
    const int deinit_before_late = state.deinit_calls;
    assert(connectivity_network_lifecycle_service_stop(100, NULL) == DEVICE_STATUS_TIMEOUT);
    assert(state.deinit_calls == deinit_before_late + 1 && !state.logical);
    puts("PASS connectivity network lifecycle ordering and fail-closed partial root");
    return 0;
}
