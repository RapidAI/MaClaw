#include <stdio.h>

#include "services/gateway_lifecycle_service.h"
#include "services/gateway_transport.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static unsigned s_dispatcher_prepare_calls;
static unsigned s_transport_prepare_calls;
static unsigned s_resume_supervisor_prepare_calls;
static unsigned s_resumed_worker_prepare_calls;
static unsigned s_capability_prepare_calls;
static unsigned s_dispatcher_abort_calls;
static unsigned s_transport_abort_calls;
static unsigned s_resume_supervisor_abort_calls;
static unsigned s_resumed_worker_abort_calls;
static unsigned s_capability_abort_calls;
static unsigned s_dispatcher_commit_calls;
static unsigned s_transport_commit_calls;
static unsigned s_resume_supervisor_commit_calls;
static unsigned s_resumed_worker_commit_calls;
static unsigned s_capability_commit_calls;
static bool s_active_cellular;
static unsigned s_cellular_foreground_cancel_calls;
static unsigned s_cellular_owner_cancel_calls;
static const void *s_last_cellular_owner;
static bool s_cancel_consumes_budget;
static int64_t s_now_us = 1000;

int64_t esp_timer_get_time(void) { return s_now_us; }
device_status_t gateway_transport_cancel_active_requests(
    gateway_transport_cancel_mask_t mask, uint32_t timeout_ms) {
    CHECK(mask == GATEWAY_TRANSPORT_CANCEL_ALL);
    CHECK(timeout_ms != 0);
    if (s_cancel_consumes_budget) {
        s_now_us += (int64_t)timeout_ms * 1000;
    }
    return DEVICE_STATUS_OK;
}
bool device_connectivity_is_active_cellular(void) { return s_active_cellular; }
bool device_connectivity_cancel_cellular_foreground_request(void) {
    ++s_cellular_foreground_cancel_calls;
    return true;
}
bool device_connectivity_cancel_cellular_requests_for_owner(const void *owner) {
    ++s_cellular_owner_cancel_calls;
    s_last_cellular_owner = owner;
    return owner != NULL;
}
uintptr_t meeting_service_worker_owner_token(void) { return 0x1234u; }

device_status_t meeting_service_prepare_resume_supervisor_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0); ++s_resume_supervisor_prepare_calls; return DEVICE_STATUS_OK;
}
device_status_t meeting_service_prepare_resumed_worker_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0); ++s_resumed_worker_prepare_calls; return DEVICE_STATUS_OK;
}
device_status_t meeting_service_prepare_capability_refresh_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0); ++s_capability_prepare_calls; return DEVICE_STATUS_OK;
}
device_status_t gateway_transport_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0); ++s_transport_prepare_calls; return DEVICE_STATUS_OK;
}
device_status_t gateway_dispatcher_prepare_system_sleep(uint32_t timeout_ms) {
    CHECK(timeout_ms != 0); ++s_dispatcher_prepare_calls; return DEVICE_STATUS_OK;
}
void meeting_service_abort_resume_supervisor_system_sleep_prepare(void) {
    ++s_resume_supervisor_abort_calls;
}
void meeting_service_abort_resumed_worker_system_sleep_prepare(void) {
    ++s_resumed_worker_abort_calls;
}
void meeting_service_abort_capability_refresh_system_sleep_prepare(void) {
    ++s_capability_abort_calls;
}
void gateway_transport_abort_system_sleep_prepare(void) { ++s_transport_abort_calls; }
void gateway_dispatcher_abort_system_sleep_prepare(void) { ++s_dispatcher_abort_calls; }
device_status_t meeting_service_commit_resume_supervisor_network_restart(void) {
    ++s_resume_supervisor_commit_calls; return DEVICE_STATUS_OK;
}
device_status_t meeting_service_commit_resumed_worker_network_restart(void) {
    ++s_resumed_worker_commit_calls; return DEVICE_STATUS_OK;
}
device_status_t meeting_service_commit_capability_refresh_network_restart(void) {
    ++s_capability_commit_calls; return DEVICE_STATUS_OK;
}
device_status_t gateway_transport_commit_prepared_network_restart(void) {
    ++s_transport_commit_calls; return DEVICE_STATUS_OK;
}
device_status_t gateway_dispatcher_commit_prepared_network_restart(void) {
    ++s_dispatcher_commit_calls; return DEVICE_STATUS_OK;
}

int main(void) {
    CHECK(gateway_lifecycle_service_init() == DEVICE_STATUS_OK);
    CHECK(gateway_lifecycle_service_commit_prepared_network_restart() == DEVICE_STATUS_BUSY);
    /* A cancellation bridge that returns OK only after consuming the parent
     * deadline must block all later worker PREPARE stages. */
    s_cancel_consumes_budget = true;
    const unsigned prepare_before_late_cancel = s_resume_supervisor_prepare_calls;
    CHECK(gateway_lifecycle_service_prepare_system_sleep(100) == DEVICE_STATUS_TIMEOUT);
    CHECK(s_resume_supervisor_prepare_calls == prepare_before_late_cancel);
    gateway_lifecycle_service_abort_system_sleep_prepare();
    s_cancel_consumes_budget = false;
    s_now_us = 1000;
    s_dispatcher_abort_calls = 0;
    s_transport_abort_calls = 0;
    s_capability_abort_calls = 0;
    s_resumed_worker_abort_calls = 0;
    s_resume_supervisor_abort_calls = 0;
    /* Runtime restart must use its own terminal-fence entry point rather than
     * advertising a reversible System Sleep PREPARE. */
    CHECK(gateway_lifecycle_service_prepare_network_restart(100) == DEVICE_STATUS_OK);
    CHECK(s_resume_supervisor_prepare_calls == 1 && s_resumed_worker_prepare_calls == 1 &&
          s_capability_prepare_calls == 1 && s_transport_prepare_calls == 1 &&
          s_dispatcher_prepare_calls == 1);
    /* The current profile reports Wi-Fi, modelling an uplink selection that
     * raced after a cellular request was admitted. Lifecycle retirement must
     * still ask the request-level cellular seam to cancel that old borrower. */
    CHECK(!s_active_cellular);
    CHECK(s_cellular_foreground_cancel_calls == 1);
    CHECK(s_cellular_owner_cancel_calls == 1);
    CHECK(s_last_cellular_owner == (const void *)0x1234u);
    /* A stray System Sleep ABORT must not revive a generation that the
     * terminal Connectivity restart path has already fenced. */
    gateway_lifecycle_service_abort_system_sleep_prepare();
    CHECK(s_dispatcher_abort_calls == 0 && s_transport_abort_calls == 0 &&
          s_capability_abort_calls == 0 && s_resumed_worker_abort_calls == 0 &&
          s_resume_supervisor_abort_calls == 0);
    CHECK(gateway_lifecycle_service_commit_prepared_network_restart() == DEVICE_STATUS_OK);
    CHECK(s_dispatcher_commit_calls == 1 && s_transport_commit_calls == 1 &&
          s_capability_commit_calls == 1 && s_resumed_worker_commit_calls == 1 &&
          s_resume_supervisor_commit_calls == 1);
    CHECK(s_dispatcher_abort_calls == 0 && s_transport_abort_calls == 0 &&
          s_capability_abort_calls == 0 && s_resumed_worker_abort_calls == 0 &&
          s_resume_supervisor_abort_calls == 0);
    CHECK(gateway_lifecycle_service_commit_prepared_network_restart() == DEVICE_STATUS_BUSY);
    /* The next transaction is independent and its explicit abort still
     * performs the historical reverse restore. */
    CHECK(gateway_lifecycle_service_prepare_system_sleep(100) == DEVICE_STATUS_OK);
    gateway_lifecycle_service_abort_system_sleep_prepare();
    CHECK(s_dispatcher_abort_calls == 1 && s_transport_abort_calls == 1 &&
          s_capability_abort_calls == 1 && s_resumed_worker_abort_calls == 1 &&
          s_resume_supervisor_abort_calls == 1);
    puts("PASS Gateway lifecycle restart commit retires prepared generations without System Sleep ABORT");
    return 0;
}
