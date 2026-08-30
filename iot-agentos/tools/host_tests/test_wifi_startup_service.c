#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "services/wifi_startup_service.h"

typedef struct {
    bool started;
    bool enterprise_enabled;
    bool connect_ready;
    bool scan_enabled;
    int configure_count;
    int connect_count;
    int disconnect_count;
    int selected_count;
    int publish_down_count;
    int scan_count;
    int wait_count;
    int ready_on_wait;
    uint32_t next_attempt;
    const char *selected_ssid;
    device_status_t disconnect_status;
} state_t;

static device_status_t ok(void *context) { (void)context; return DEVICE_STATUS_OK; }
static void policy(bool auto_connect, bool expected_disconnect, void *context) {
    (void)auto_connect; (void)expected_disconnect; (void)context;
}
static device_status_t configure_station(const wifi_startup_service_station_config_t *config,
                                         void *context) {
    state_t *state = context;
    assert(config && config->ssid && config->password);
    ++state->configure_count;
    return DEVICE_STATUS_OK;
}
static bool enterprise_enabled(void *context) { return ((state_t *)context)->enterprise_enabled; }
static device_status_t configure_enterprise(const wifi_startup_service_enterprise_config_t *config,
                                             void *context) {
    (void)config;
    ((state_t *)context)->enterprise_enabled = true;
    return DEVICE_STATUS_OK;
}
static device_status_t disable_enterprise(void *context) {
    ((state_t *)context)->enterprise_enabled = false;
    return DEVICE_STATUS_OK;
}
static bool started(void *context) { return ((state_t *)context)->started; }
static device_status_t start(void *context) { ((state_t *)context)->started = true; return DEVICE_STATUS_OK; }
static device_status_t connect(void *context) { ++((state_t *)context)->connect_count; return DEVICE_STATUS_OK; }
static device_status_t disconnect(void *context) {
    state_t *state = context;
    ++state->disconnect_count;
    return state->disconnect_status;
}
static device_status_t scan(uint32_t maximum, wifi_startup_service_scan_observer_t observer,
                            void *observer_context, void *context) {
    state_t *state = context;
    ++state->scan_count;
    if (!state->scan_enabled) return DEVICE_STATUS_IO_ERROR;
    assert(maximum >= 2);
    assert(observer("weak", -80, observer_context));
    assert(observer("strong", -40, observer_context));
    return DEVICE_STATUS_OK;
}
static void selected(const char *ssid, const char *password, void *context) {
    (void)password;
    state_t *state = context;
    state->selected_ssid = ssid;
    ++state->selected_count;
}
static uint32_t begin(const char *ssid, void *context) {
    (void)ssid;
    return ++((state_t *)context)->next_attempt;
}
static bool wait(uint32_t attempt, uint32_t timeout, void *context) {
    (void)attempt;
    (void)timeout;
    state_t *state = context;
    ++state->wait_count;
    return state->ready_on_wait ? state->wait_count >= state->ready_on_wait
                                : state->connect_ready;
}
static void publish(const char *ssid, bool ready, void *context) {
    (void)ssid;
    if (!ready) ++((state_t *)context)->publish_down_count;
}
static bool portal(void *context) { (void)context; return false; }

static wifi_startup_service_host_t host_for(state_t *state) {
    return (wifi_startup_service_host_t){
        .ensure_network = ok, .ensure_station = ok, .set_station_policy = policy,
        .configure_station = configure_station, .enterprise_enabled = enterprise_enabled,
        .configure_enterprise = configure_enterprise, .disable_enterprise = disable_enterprise,
        .wifi_started = started, .wifi_start = start, .wifi_connect = connect,
        .wifi_disconnect = disconnect, .scan_visible = scan,
        .select_saved_network = selected, .begin_attempt = begin, .wait_attempt = wait,
        .publish_network_ready = publish, .setup_portal_active = portal, .context = state,
    };
}

static wifi_startup_service_request_t request_for(void) {
    static const wifi_startup_service_saved_network_t saved[] = {
        {.ssid = "weak", .password = "one"}, {.ssid = "strong", .password = "two"},
    };
    return (wifi_startup_service_request_t){
        .ssid = "primary", .password = "secret", .saved_networks = saved,
        .saved_network_count = 2, .scan_maximum_records = 8,
        .candidate_connect_timeout_ms = 100, .connect_timeout_ms = 200,
    };
}

int main(void) {
    state_t saved = {.connect_ready = true, .scan_enabled = true};
    wifi_startup_service_host_t host = host_for(&saved);
    wifi_startup_service_request_t request = request_for();
    assert(wifi_startup_service_connect(&host, &request) == DEVICE_STATUS_OK);
    assert(saved.scan_count == 1 && saved.selected_count == 1);
    assert(strcmp(saved.selected_ssid, "strong") == 0);

    state_t fallback = {.connect_ready = true, .scan_enabled = false};
    host = host_for(&fallback);
    assert(wifi_startup_service_connect(&host, &request) == DEVICE_STATUS_OK);
    assert(fallback.selected_count == 0 && fallback.connect_count == 1 && fallback.started);

    state_t enterprise = {.connect_ready = true};
    host = host_for(&enterprise);
    request.saved_network_count = 0;
    request.enterprise = true;
    assert(wifi_startup_service_connect(&host, &request) == DEVICE_STATUS_OK);
    assert(enterprise.enterprise_enabled && enterprise.scan_count == 0);

    state_t timeout = {.connect_ready = false, .scan_enabled = false};
    host = host_for(&timeout);
    request.enterprise = false;
    assert(wifi_startup_service_connect(&host, &request) == DEVICE_STATUS_TIMEOUT);
    assert(timeout.publish_down_count >= 1);

    /* Candidate failure falls back using the final actual selection. This
     * includes a failed driver disconnect: it is diagnostic only and must not
     * suppress a new Connectivity attempt for the next candidate. */
    state_t candidate_fallback = {
        .scan_enabled = true,
        .disconnect_status = DEVICE_STATUS_IO_ERROR,
    };
    host = host_for(&candidate_fallback);
    request.saved_network_count = 2;
    assert(wifi_startup_service_connect(&host, &request) == DEVICE_STATUS_TIMEOUT);
    assert(candidate_fallback.disconnect_count == 1);
    assert(candidate_fallback.connect_count == 3);
    assert(strcmp(candidate_fallback.selected_ssid, "weak") == 0);

    puts("PASS Wi-Fi startup policy preserves saved selection and fallback");
    return 0;
}
