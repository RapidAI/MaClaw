#include <assert.h>
#include <stdio.h>

#include "services/startup_pet_asset_admission_service.h"

typedef struct {
    startup_pet_asset_state_snapshot_t state;
    bool stopped;
    bool sleep_preparing;
    bool display_ready;
    bool capacity;
    bool dropped_cache;
    bool retry_available;
    bool retry_scheduled;
    bool worker_active;
    bool gateway_operational;
    bool installed;
    device_status_t start_status;
    int retries_taken;
    int retries_returned;
    int finishes;
    int starts;
    int pending_sets;
} test_state_t;

static bool snapshot(startup_pet_asset_state_snapshot_t *out, void *context) {
    *out = ((test_state_t *)context)->state;
    return true;
}
static bool stopped(void *context) { return ((test_state_t *)context)->stopped; }
static bool sleeping(void *context) { return ((test_state_t *)context)->sleep_preparing; }
static bool prepare(const pet_asset_descriptor_t *source,
                    pet_asset_descriptor_t *out, void *context) {
    test_state_t *state = context;
    *out = *source;
    return state->display_ready;
}
static bool capacity(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    return ((test_state_t *)context)->capacity;
}
static bool drop_cache(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    test_state_t *state = context;
    state->dropped_cache = true;
    return false;
}
static bool take_retry(uint32_t generation, uint32_t limit,
                       uint32_t *attempt, void *context) {
    test_state_t *state = context;
    if (!state->retry_available || generation != state->state.generation || limit == 0) return false;
    ++state->retries_taken;
    *attempt = (uint32_t)state->retries_taken;
    return true;
}
static void return_retry(uint32_t generation, void *context) {
    test_state_t *state = context;
    assert(generation == state->state.generation);
    ++state->retries_returned;
}
static bool schedule_retry(void *context) { return ((test_state_t *)context)->retry_scheduled; }
static void finish(uint32_t generation, void *context) {
    test_state_t *state = context;
    assert(generation == state->state.generation);
    ++state->finishes;
}
static bool worker_active(void *context) { return ((test_state_t *)context)->worker_active; }
static bool gateway_operational(void *context) {
    return ((test_state_t *)context)->gateway_operational;
}
static device_status_t start_worker(void *context) {
    test_state_t *state = context;
    ++state->starts;
    return state->start_status;
}
static bool installed(const pet_asset_descriptor_t *descriptor, void *context) {
    (void)descriptor;
    return ((test_state_t *)context)->installed;
}
static void set_pending(bool pending, void *context) {
    test_state_t *state = context;
    state->state.pending = pending;
    ++state->pending_sets;
}

static startup_pet_asset_admission_service_host_t host_for(test_state_t *state) {
    return (startup_pet_asset_admission_service_host_t){
        .struct_size = sizeof(startup_pet_asset_admission_service_host_t),
        .snapshot = snapshot, .stop_requested = stopped,
        .system_sleep_preparing = sleeping, .prepare_for_display = prepare,
        .capacity_available = capacity, .drop_stale_cache = drop_cache,
        .take_capacity_retry = take_retry, .return_capacity_retry = return_retry,
        .schedule_retry = schedule_retry, .finish_generation = finish,
        .worker_active = worker_active, .gateway_operational = gateway_operational,
        .start_worker = start_worker, .revision_installed = installed,
        .set_pending = set_pending, .context = state,
    };
}

static test_state_t state_for(void) {
    test_state_t state = {.display_ready = true, .capacity = true,
                          .retry_available = true, .retry_scheduled = true,
                          .gateway_operational = true, .start_status = DEVICE_STATUS_OK};
    state.state.pending = true;
    state.state.present = true;
    state.state.generation = 9;
    state.state.descriptor.frame_count = 2;
    return state;
}

int main(void) {
    uint32_t attempt = 0;
    device_status_t start_status = DEVICE_STATUS_INTERNAL_ERROR;

    test_state_t ready = state_for();
    startup_pet_asset_admission_service_host_t ready_host = host_for(&ready);
    assert(startup_pet_asset_admission_service_admit_pending(
               &ready_host, 6, &attempt, &start_status) ==
           STARTUP_PET_ASSET_ADMISSION_STARTED);
    assert(ready.starts == 1 && ready.finishes == 0 && start_status == DEVICE_STATUS_OK);

    test_state_t pressure = state_for();
    pressure.capacity = false;
    startup_pet_asset_admission_service_host_t pressure_host = host_for(&pressure);
    assert(startup_pet_asset_admission_service_admit_pending(
               &pressure_host, 6, &attempt, NULL) ==
           STARTUP_PET_ASSET_ADMISSION_RETRY_SCHEDULED);
    assert(pressure.dropped_cache && pressure.retries_taken == 1 && attempt == 1 &&
           pressure.finishes == 0);

    test_state_t timer_failure = state_for();
    timer_failure.capacity = false;
    timer_failure.retry_scheduled = false;
    startup_pet_asset_admission_service_host_t timer_failure_host = host_for(&timer_failure);
    assert(startup_pet_asset_admission_service_admit_pending(
               &timer_failure_host, 6, NULL, NULL) ==
           STARTUP_PET_ASSET_ADMISSION_FINISHED);
    assert(timer_failure.retries_taken == 1 && timer_failure.retries_returned == 1 &&
           timer_failure.finishes == 1);

    test_state_t unavailable = state_for();
    unavailable.gateway_operational = false;
    startup_pet_asset_admission_service_host_t unavailable_host = host_for(&unavailable);
    assert(startup_pet_asset_admission_service_admit_pending(
               &unavailable_host, 6, NULL, NULL) ==
           STARTUP_PET_ASSET_ADMISSION_NO_ACTION);
    assert(unavailable.starts == 0 && unavailable.finishes == 0);

    test_state_t clear_absent = state_for();
    clear_absent.state.present = false;
    clear_absent.gateway_operational = false;
    startup_pet_asset_admission_service_host_t clear_absent_host = host_for(&clear_absent);
    assert(startup_pet_asset_admission_service_admit_pending(
               &clear_absent_host, 6, NULL, &start_status) ==
           STARTUP_PET_ASSET_ADMISSION_STARTED);
    assert(clear_absent.starts == 1 && clear_absent.finishes == 0 &&
           start_status == DEVICE_STATUS_OK);

    test_state_t rearm = state_for();
    rearm.state.pending = false;
    startup_pet_asset_admission_service_host_t rearm_host = host_for(&rearm);
    assert(startup_pet_asset_admission_service_rearm_preempted(&rearm_host) ==
           STARTUP_PET_ASSET_ADMISSION_REARMED);
    assert(rearm.state.pending && rearm.pending_sets == 1);

    test_state_t unwinding = state_for();
    unwinding.state.pending = false;
    unwinding.worker_active = true;
    startup_pet_asset_admission_service_host_t unwinding_host = host_for(&unwinding);
    assert(startup_pet_asset_admission_service_rearm_preempted(&unwinding_host) ==
           STARTUP_PET_ASSET_ADMISSION_REARMED);
    assert(unwinding.state.pending && unwinding.pending_sets == 1);

    test_state_t rearm_timer_failure = state_for();
    rearm_timer_failure.state.pending = false;
    rearm_timer_failure.worker_active = true;
    rearm_timer_failure.retry_scheduled = false;
    startup_pet_asset_admission_service_host_t rearm_timer_failure_host = host_for(&rearm_timer_failure);
    assert(startup_pet_asset_admission_service_rearm_preempted(&rearm_timer_failure_host) ==
           STARTUP_PET_ASSET_ADMISSION_NO_ACTION);
    assert(!rearm_timer_failure.state.pending && rearm_timer_failure.pending_sets == 2);

    puts("PASS startup pet asset admission");
    return 0;
}
