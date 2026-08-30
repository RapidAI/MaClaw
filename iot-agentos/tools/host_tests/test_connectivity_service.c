#include <stdio.h>
#include <stdlib.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "connectivity_service.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

struct host_event_group {
    EventBits_t bits;
};

static TickType_t s_ticks;
static int s_mutex;
static int s_binary;
static bool s_binary_available;
static unsigned s_platform_prepare_calls;
static unsigned s_platform_abort_calls;
static unsigned s_platform_transport_set_calls;
static unsigned s_platform_load_selection_calls;
static unsigned s_platform_startup_toggle_calls;
static unsigned s_platform_cellular_prepare_calls;
static unsigned s_platform_cellular_start_calls;
static unsigned s_platform_cellular_quiesce_calls;
static unsigned s_platform_cellular_http_calls;
static unsigned s_platform_cellular_stream_calls;
static unsigned s_platform_cellular_cancel_foreground_calls;
static unsigned s_platform_cellular_cancel_owner_calls;
static bool s_platform_cellular_ready;
static bool s_startup_toggle_result;
static bool s_startup_toggle_selected_cellular;
static bool s_cancel_foreground_during_cellular_http;
static bool s_cancel_foreground_result;
static bool s_cancel_owner_during_cellular_stream;
static bool s_cancel_owner_result;
static bool s_switch_uplink_during_cellular_http;
static bool s_quiesce_during_cellular_http;
static device_status_t s_quiesce_during_cellular_http_status;
static bool s_switch_uplink_during_cellular_prepare;
static bool s_switch_uplink_during_cellular_start;
static bool s_switch_uplink_during_cellular_quiesce;
static unsigned s_event_set_calls;
static unsigned s_canceller_calls;
static unsigned s_resumer_calls;
static bool s_cancel_consumes_budget;

TickType_t xTaskGetTickCount(void) { return s_ticks; }
void vTaskDelay(TickType_t ticks) { s_ticks += ticks ? ticks : 1; }
SemaphoreHandle_t xSemaphoreCreateMutexStatic(StaticSemaphore_t *storage) {
    (void)storage;
    return &s_mutex;
}
SemaphoreHandle_t xSemaphoreCreateBinaryStatic(StaticSemaphore_t *storage) {
    (void)storage;
    return &s_binary;
}
BaseType_t xSemaphoreTake(SemaphoreHandle_t semaphore, TickType_t timeout) {
    (void)semaphore;
    if (semaphore == &s_binary) {
        if (!s_binary_available) {
            s_ticks += timeout;
            return pdFALSE;
        }
        s_binary_available = false;
    }
    return pdTRUE;
}
BaseType_t xSemaphoreGive(SemaphoreHandle_t semaphore) {
    (void)semaphore;
    if (semaphore == &s_binary) s_binary_available = true;
    return pdTRUE;
}
EventGroupHandle_t xEventGroupCreate(void) {
    return calloc(1, sizeof(struct host_event_group));
}
void vEventGroupDelete(EventGroupHandle_t event_group) { free(event_group); }
EventBits_t xEventGroupSetBits(EventGroupHandle_t event_group, EventBits_t bits) {
    ++s_event_set_calls;
    return (event_group->bits |= bits);
}
EventBits_t xEventGroupClearBits(EventGroupHandle_t event_group, EventBits_t bits) {
    return (event_group->bits &= ~bits);
}
EventBits_t xEventGroupWaitBits(EventGroupHandle_t event_group, EventBits_t bits,
                                BaseType_t clear_on_exit, BaseType_t wait_for_all_bits,
                                TickType_t ticks_to_wait) {
    (void)wait_for_all_bits;
    EventBits_t observed = event_group->bits;
    if ((observed & bits) == 0) s_ticks += ticks_to_wait;
    if (clear_on_exit) event_group->bits &= ~bits;
    return observed;
}
void vTaskDelete(void *task) { (void)task; }

void platform_connectivity_set_network_transport(bool cellular) {
    (void)cellular;
    ++s_platform_transport_set_calls;
}
device_status_t platform_connectivity_prepare_cellular_transport(void) {
    ++s_platform_cellular_prepare_calls;
    if (s_switch_uplink_during_cellular_prepare) {
        connectivity_service_set_active_uplink(DEVICE_UPLINK_WIFI);
    }
    return DEVICE_STATUS_OK;
}
device_status_t platform_connectivity_start_cellular_transport(uint32_t timeout_ms) {
    (void)timeout_ms;
    ++s_platform_cellular_start_calls;
    if (s_switch_uplink_during_cellular_start) {
        connectivity_service_set_active_uplink(DEVICE_UPLINK_WIFI);
    }
    s_platform_cellular_ready = true;
    return DEVICE_STATUS_OK;
}
bool platform_connectivity_is_cellular_transport_ready(void) {
    return s_platform_cellular_ready;
}
device_status_t platform_connectivity_quiesce_cellular_transport(uint32_t timeout_ms) {
    (void)timeout_ms;
    ++s_platform_cellular_quiesce_calls;
    if (s_switch_uplink_during_cellular_quiesce) {
        connectivity_service_set_active_uplink(DEVICE_UPLINK_WIFI);
    }
    s_platform_cellular_ready = false;
    return DEVICE_STATUS_OK;
}
device_status_t platform_connectivity_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0);
    ++s_platform_prepare_calls;
    return DEVICE_STATUS_OK;
}
void platform_connectivity_abort_system_sleep_prepare(void) { ++s_platform_abort_calls; }
device_status_t platform_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    (void)request;
    ++s_platform_cellular_http_calls;
    if (s_switch_uplink_during_cellular_http) {
        connectivity_service_set_active_uplink(DEVICE_UPLINK_WIFI);
    }
    if (s_quiesce_during_cellular_http) {
        s_quiesce_during_cellular_http_status =
            connectivity_service_quiesce_cellular_transport(10);
    }
    if (s_cancel_foreground_during_cellular_http) {
        s_cancel_foreground_result = connectivity_service_cancel_cellular_foreground_request();
    }
    return DEVICE_STATUS_OK;
}
device_status_t platform_connectivity_deinit_cellular_transport(uint32_t timeout_ms) {
    (void)timeout_ms;
    return DEVICE_STATUS_OK;
}
device_status_t platform_connectivity_reinitialize_cellular_transport(uint32_t timeout_ms) {
    (void)timeout_ms;
    return DEVICE_STATUS_OK;
}
device_status_t platform_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    (void)request;
    ++s_platform_cellular_stream_calls;
    if (s_cancel_owner_during_cellular_stream) {
        s_cancel_owner_result = connectivity_service_cancel_cellular_requests_for_owner(
            (const void *)request);
    }
    return DEVICE_STATUS_OK;
}
bool platform_connectivity_cancel_cellular_foreground_request(void) {
    ++s_platform_cellular_cancel_foreground_calls;
    return true;
}
bool platform_connectivity_cancel_cellular_requests_for_owner(const void *owner) {
    if (!owner) return false;
    ++s_platform_cellular_cancel_owner_calls;
    return true;
}
bool platform_connectivity_load_transport_selection(bool *out_cellular) {
    ++s_platform_load_selection_calls;
    if (out_cellular) *out_cellular = false;
    return true;
}
bool platform_connectivity_apply_startup_transport_toggle(uint32_t window_ms,
                                                          bool current_cellular,
                                                          bool *out_cellular) {
    (void)window_ms;
    (void)current_cellular;
    ++s_platform_startup_toggle_calls;
    if (out_cellular) *out_cellular = s_startup_toggle_selected_cellular;
    return s_startup_toggle_result;
}
void platform_connectivity_adapt_gateway_url(char *gateway_url,
                                             uint32_t gateway_url_capacity,
                                             bool cellular_active) {
    (void)gateway_url;
    (void)gateway_url_capacity;
    (void)cellular_active;
}

static device_status_t cancel_for_sleep(uint32_t timeout_ms, void *context) {
    (void)context;
    CHECK(timeout_ms != 0);
    ++s_canceller_calls;
    if (s_cancel_consumes_budget) s_ticks += timeout_ms;
    return DEVICE_STATUS_OK;
}
static void resume_after_sleep(void *context) {
    (void)context;
    ++s_resumer_calls;
}

static device_status_t read_empty_stream(void *context, void *buffer,
                                         uint32_t requested, uint32_t *read_bytes) {
    (void)context;
    (void)buffer;
    (void)requested;
    if (read_bytes) *read_bytes = 0;
    return DEVICE_STATUS_OK;
}

static device_connectivity_http_request_t make_cellular_request(
    char *response, uint32_t *response_len, int *status_code, bool *truncated) {
    return (device_connectivity_http_request_t){
        .method = "GET",
        .url = "https://example.invalid/test",
        .response = response,
        .response_capacity = 2,
        .response_len = response_len,
        .status_code = status_code,
        .truncated = truncated,
        .timeout_ms = 10,
    };
}

int main(void) {
    CHECK(connectivity_service_initialize() == DEVICE_STATUS_OK);
    fault_domain_snapshot_t fault_snapshot = {0};
    CHECK(connectivity_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.id == FAULT_DOMAIN_ID_CONNECTIVITY);
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_READY);
    CHECK(fault_snapshot.generation == 2u);
    CHECK(fault_snapshot.admission_open);
    const uint32_t initial_epoch = connectivity_service_begin_wifi_attempt("test-ap");
    CHECK(initial_epoch != 0);
    CHECK(connectivity_service_observe_wifi_got_ip("test-ap"));
    CHECK(connectivity_service_is_active_uplink_ready());

    /* Wi-Fi-selected generations must not enter the profile-private cellular
     * seam merely because their logical Connectivity service is otherwise
     * ready. This prevents a Wi-Fi fault from touching the independent 4G
     * fault domain. */
    CHECK(connectivity_service_prepare_cellular_transport() == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_start_cellular_transport(10) == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_BUSY);
    char cellular_response[2] = {0};
    uint32_t cellular_response_len = 0;
    int cellular_status_code = 0;
    bool cellular_truncated = false;
    device_connectivity_http_request_t cellular_request = make_cellular_request(
        cellular_response, &cellular_response_len, &cellular_status_code,
        &cellular_truncated);
    char stream_buffer[4] = {0};
    device_connectivity_stream_request_t cellular_stream_request = {
        .request = cellular_request,
        .body_reader = read_empty_stream,
        .stream_buffer = stream_buffer,
        .stream_buffer_size = sizeof(stream_buffer),
    };
    CHECK(connectivity_service_cellular_http_request(&cellular_request) == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_cellular_http_stream_request(&cellular_stream_request) ==
          DEVICE_STATUS_BUSY);
    CHECK(s_platform_cellular_prepare_calls == 0);
    CHECK(s_platform_cellular_start_calls == 0);
    CHECK(s_platform_cellular_quiesce_calls == 0);
    CHECK(s_platform_cellular_http_calls == 0);
    CHECK(s_platform_cellular_stream_calls == 0);
    CHECK(!connectivity_service_cancel_cellular_foreground_request());
    CHECK(!connectivity_service_cancel_cellular_requests_for_owner(&cellular_request));
    CHECK(s_platform_cellular_cancel_foreground_calls == 0);
    CHECK(s_platform_cellular_cancel_owner_calls == 0);

    /* Default-loop callbacks remain physically registered by the composition
     * root, but their lifecycle fence is now Connectivity-owned. A callback
     * admitted before PREPARE is allowed to leave and drain; a queued callback
     * after the marker cannot enter until ABORT restores the old generation. */
    connectivity_service_open_wifi_event_callback_admission();
    CHECK(connectivity_service_wifi_event_callback_enter());
    connectivity_service_wifi_event_callback_leave();

    /* A waiter that acquired its EventGroup before PREPARE must be actively
     * released by the transaction fence rather than sleeping until its normal
     * Wi-Fi timeout. The host EventGroup model makes that bounded wake
     * observable without a concurrent task. */
    const uint32_t pending_epoch = connectivity_service_begin_wifi_attempt("test-ap");
    CHECK(pending_epoch != 0 && pending_epoch != initial_epoch);

    connectivity_service_set_system_sleep_request_canceller(cancel_for_sleep, NULL);
    connectivity_service_set_system_sleep_request_resumer(resume_after_sleep, NULL);
    /* A bridge that reports OK only after consuming the caller's remaining
     * budget must not allow the physical transport PREPARE to proceed. */
    s_cancel_consumes_budget = true;
    const unsigned prepare_before_late_cancel = s_platform_prepare_calls;
    CHECK(connectivity_service_prepare_system_sleep(50) == DEVICE_STATUS_TIMEOUT);
    CHECK(s_platform_prepare_calls == prepare_before_late_cancel);
    connectivity_service_abort_system_sleep_prepare();
    s_cancel_consumes_budget = false;
    s_canceller_calls = 0;
    s_resumer_calls = 0;
    s_platform_abort_calls = 0;
    const unsigned event_sets_before_prepare = s_event_set_calls;
    CHECK(connectivity_service_prepare_system_sleep(50) == DEVICE_STATUS_OK);
    CHECK(s_canceller_calls == 1);
    CHECK(s_platform_prepare_calls == 1);
    CHECK(s_event_set_calls == event_sets_before_prepare + 1);
    CHECK(!connectivity_service_wait_wifi_attempt(pending_epoch, 50));
    CHECK(connectivity_service_begin_wifi_attempt("test-ap") == 0);
    /* Selection changes include profile-side transport operations, so they
     * must obey the same PREPARE admission fence as Wi-Fi attempts. */
    connectivity_service_set_active_uplink(DEVICE_UPLINK_CELLULAR);
    CHECK(!connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == 0);
    connectivity_service_restore_selected_uplink();
    CHECK(s_platform_load_selection_calls == 0);
    CHECK(!connectivity_service_apply_startup_transport_toggle(10));
    CHECK(s_platform_startup_toggle_calls == 0);
    CHECK(connectivity_service_prepare_cellular_transport() == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_start_cellular_transport(10) == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_establish_cellular_transport(10) == DEVICE_STATUS_BUSY);
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_BUSY);
    CHECK(!connectivity_service_is_cellular_transport_ready());
    CHECK(s_platform_cellular_prepare_calls == 0);
    CHECK(s_platform_cellular_start_calls == 0);
    CHECK(s_platform_cellular_quiesce_calls == 0);
    CHECK(!connectivity_service_observe_wifi_got_ip("test-ap"));
    CHECK(!connectivity_service_observe_wifi_disconnected("test-ap"));
    connectivity_service_set_wifi_ready(true);
    connectivity_service_set_cellular_ready(true);
    CHECK(!connectivity_service_is_active_uplink_ready());
    device_connectivity_snapshot_t snapshot = {0};
    CHECK(connectivity_service_get_snapshot(&snapshot));
    CHECK(!snapshot.ready && !snapshot.wifi_ready && !snapshot.cellular_ready);
    CHECK(!connectivity_service_wifi_event_callback_enter());

    connectivity_service_abort_system_sleep_prepare();
    CHECK(s_resumer_calls == 1);
    CHECK(s_platform_abort_calls == 1);
    CHECK(connectivity_service_wifi_event_callback_enter());
    connectivity_service_wifi_event_callback_leave();
    const uint32_t resumed_epoch = connectivity_service_begin_wifi_attempt("test-ap");
    CHECK(resumed_epoch != 0 && resumed_epoch != initial_epoch);
    CHECK(connectivity_service_observe_wifi_got_ip("test-ap"));
    CHECK(connectivity_service_is_active_uplink_ready());
    connectivity_service_set_active_uplink(DEVICE_UPLINK_CELLULAR);
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == 1);
    s_startup_toggle_result = true;
    s_startup_toggle_selected_cellular = false;
    CHECK(connectivity_service_apply_startup_transport_toggle(10));
    CHECK(!connectivity_service_is_active_cellular());
    CHECK(s_platform_startup_toggle_calls == 1);
    CHECK(s_platform_transport_set_calls == 2);
    connectivity_service_set_active_uplink(DEVICE_UPLINK_CELLULAR);
    const unsigned transport_sets_before_cellular_operation =
        s_platform_transport_set_calls;
    /* Lifecycle calls use a separate physical-operation admission. A profile
     * adapter must not observe its transport hint changing halfway through a
     * prepare/start/quiesce or combined establish transaction. */
    s_switch_uplink_during_cellular_prepare = true;
    CHECK(connectivity_service_prepare_cellular_transport() == DEVICE_STATUS_OK);
    s_switch_uplink_during_cellular_prepare = false;
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == transport_sets_before_cellular_operation);
    s_switch_uplink_during_cellular_start = true;
    CHECK(connectivity_service_start_cellular_transport(10) == DEVICE_STATUS_OK);
    s_switch_uplink_during_cellular_start = false;
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == transport_sets_before_cellular_operation);
    CHECK(s_platform_cellular_prepare_calls == 1);
    CHECK(s_platform_cellular_start_calls == 1);
    s_switch_uplink_during_cellular_quiesce = true;
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_OK);
    s_switch_uplink_during_cellular_quiesce = false;
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == transport_sets_before_cellular_operation);
    CHECK(s_platform_cellular_quiesce_calls == 1);
    s_switch_uplink_during_cellular_prepare = true;
    s_switch_uplink_during_cellular_start = true;
    CHECK(connectivity_service_establish_cellular_transport(10) == DEVICE_STATUS_OK);
    s_switch_uplink_during_cellular_prepare = false;
    s_switch_uplink_during_cellular_start = false;
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_platform_transport_set_calls == transport_sets_before_cellular_operation);
    CHECK(s_platform_cellular_prepare_calls == 2);
    CHECK(s_platform_cellular_start_calls == 2);
    CHECK(connectivity_service_is_cellular_transport_ready());
    /* An uplink update must not alter the adapter hint while this request is
     * in flight. The old borrower can then finish or be cancelled against the
     * same cellular physical session. */
    s_switch_uplink_during_cellular_http = true;
    s_quiesce_during_cellular_http = true;
    s_cancel_foreground_during_cellular_http = true;
    CHECK(connectivity_service_cellular_http_request(&cellular_request) == DEVICE_STATUS_OK);
    s_switch_uplink_during_cellular_http = false;
    s_quiesce_during_cellular_http = false;
    s_cancel_foreground_during_cellular_http = false;
    CHECK(connectivity_service_is_active_cellular());
    CHECK(s_quiesce_during_cellular_http_status == DEVICE_STATUS_BUSY);
    CHECK(s_platform_cellular_quiesce_calls == 1);
    s_cancel_owner_during_cellular_stream = true;
    CHECK(connectivity_service_cellular_http_stream_request(&cellular_stream_request) ==
          DEVICE_STATUS_OK);
    s_cancel_owner_during_cellular_stream = false;
    CHECK(s_platform_cellular_http_calls == 1);
    CHECK(s_platform_cellular_stream_calls == 1);
    CHECK(s_cancel_foreground_result);
    CHECK(s_cancel_owner_result);
    CHECK(s_platform_cellular_cancel_foreground_calls == 1);
    CHECK(s_platform_cellular_cancel_owner_calls == 1);
    /* Once the synchronous cellular borrower returns, selection is allowed;
     * the later Wi-Fi generation cannot issue a stray modem cancellation. */
    connectivity_service_set_active_uplink(DEVICE_UPLINK_WIFI);
    CHECK(!connectivity_service_cancel_cellular_foreground_request());
    CHECK(!connectivity_service_cancel_cellular_requests_for_owner(&cellular_request));
    CHECK(s_platform_cellular_cancel_foreground_calls == 1);
    CHECK(s_platform_cellular_cancel_owner_calls == 1);
    /* Selection is durable policy, not proof that a prior cellular physical
     * session already vanished. Terminal root teardown may still drain that
     * last ML307 generation, but a fresh Wi-Fi-only generation cannot enter
     * this seam without current/published cellular session evidence. */
    s_platform_cellular_ready = true;
    CHECK(connectivity_service_has_cellular_transport_session());
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_OK);
    CHECK(s_platform_cellular_quiesce_calls == 2);
    s_platform_cellular_ready = false;
    CHECK(!connectivity_service_has_cellular_transport_session());
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_BUSY);
    connectivity_service_set_active_uplink(DEVICE_UPLINK_CELLULAR);
    CHECK(connectivity_service_quiesce_cellular_transport(10) == DEVICE_STATUS_OK);
    CHECK(s_platform_cellular_quiesce_calls == 3);

    CHECK(connectivity_service_stop_wifi_event_callback_admission(10) == DEVICE_STATUS_OK);
    CHECK(!connectivity_service_wifi_event_callback_enter());
    connectivity_service_open_wifi_event_callback_admission();
    CHECK(connectivity_service_wifi_event_callback_enter());
    connectivity_service_wifi_event_callback_leave();

    CHECK(connectivity_service_deinit(50) == DEVICE_STATUS_OK);
    CHECK(connectivity_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_STOPPED);
    CHECK(fault_snapshot.generation == 3u);
    CHECK(!fault_snapshot.admission_open);
    /* Reinitialization receives a fresh logical generation. The previous
     * EventGroup/readiness observation cannot be reused after the lifecycle
     * self-test completes. */
    CHECK(connectivity_service_initialize() == DEVICE_STATUS_OK);
    CHECK(connectivity_service_get_fault_domain_snapshot(&fault_snapshot));
    CHECK(fault_snapshot.phase == FAULT_DOMAIN_READY);
    CHECK(fault_snapshot.generation == 4u);
    CHECK(connectivity_service_deinit(50) == DEVICE_STATUS_OK);
    puts("PASS Connectivity fault-domain restart closes old Wi-Fi admission until new self-test");
    return 0;
}
