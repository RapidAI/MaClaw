#include <assert.h>
#include <stdio.h>

#include "services/startup_pet_asset_sleep_service.h"

typedef struct {
    int64_t now_us;
    device_status_t state_status;
    device_status_t worker_status;
    device_status_t retry_status;
    device_status_t cache_status;
    bool state_prepared;
    bool restored_audio;
    bool audio_active;
    bool take_audio;
    int state_prepares;
    int worker_prepares;
    int retry_prepares;
    int cache_prepares;
    int state_aborts;
    int worker_aborts;
    int retry_aborts;
    int cache_aborts;
    int rearms;
    uint32_t worker_timeout;
    uint32_t retry_timeout;
    uint32_t cache_timeout;
    int64_t state_delay_us;
} test_state_t;

static int64_t now(void *context) { return ((test_state_t *)context)->now_us; }
static device_status_t prepare_state(void *context) {
    test_state_t *state = context;
    ++state->state_prepares;
    state->state_prepared = state->state_status == DEVICE_STATUS_OK;
    state->now_us += state->state_delay_us;
    return state->state_status;
}
static device_status_t prepare_worker(uint32_t timeout, void *context) {
    test_state_t *state = context;
    ++state->worker_prepares; state->worker_timeout = timeout;
    state->now_us += 1000;
    return state->worker_status;
}
static device_status_t prepare_retry(uint32_t timeout, void *context) {
    test_state_t *state = context;
    ++state->retry_prepares; state->retry_timeout = timeout;
    state->now_us += 1000;
    return state->retry_status;
}
static device_status_t prepare_cache(uint32_t timeout, void *context) {
    test_state_t *state = context;
    ++state->cache_prepares; state->cache_timeout = timeout;
    return state->cache_status;
}
static bool abort_state(bool *out_audio, void *context) {
    test_state_t *state = context;
    ++state->state_aborts;
    *out_audio = state->restored_audio;
    return state->state_prepared;
}
static void abort_worker(void *context) { ++((test_state_t *)context)->worker_aborts; }
static void abort_retry(void *context) { ++((test_state_t *)context)->retry_aborts; }
static void abort_cache(void *context) { ++((test_state_t *)context)->cache_aborts; }
static bool audio_active(void *context) { return ((test_state_t *)context)->audio_active; }
static bool take_audio(void *context) { return ((test_state_t *)context)->take_audio; }
static void rearm(void *context) { ++((test_state_t *)context)->rearms; }

static startup_pet_asset_sleep_service_host_t host_for(test_state_t *state) {
    return (startup_pet_asset_sleep_service_host_t){
        .struct_size = sizeof(startup_pet_asset_sleep_service_host_t),
        .monotonic_time_us = now, .prepare_state = prepare_state,
        .prepare_worker = prepare_worker, .prepare_retry = prepare_retry,
        .prepare_cache = prepare_cache, .abort_state = abort_state,
        .abort_worker = abort_worker, .abort_retry = abort_retry,
        .abort_cache = abort_cache, .server_audio_lease_active = audio_active,
        .take_audio_preemption = take_audio, .rearm_preempted = rearm,
        .context = state,
    };
}
static test_state_t state_for(void) {
    return (test_state_t){.state_status = DEVICE_STATUS_OK,
                          .worker_status = DEVICE_STATUS_OK,
                          .retry_status = DEVICE_STATUS_OK,
                          .cache_status = DEVICE_STATUS_OK};
}

int main(void) {
    test_state_t success = state_for();
    startup_pet_asset_sleep_service_host_t success_host = host_for(&success);
    assert(startup_pet_asset_sleep_service_prepare(&success_host, 10) == DEVICE_STATUS_OK);
    assert(success.state_prepares == 1 && success.worker_prepares == 1 &&
           success.retry_prepares == 1 && success.cache_prepares == 1);
    assert(success.worker_timeout == 10 && success.retry_timeout == 9 &&
           success.cache_timeout == 8);

    test_state_t late_state = state_for();
    late_state.now_us = 1000;
    /* The first child reports OK only after consuming the complete parent
     * allowance; no later worker PREPARE may be admitted. */
    late_state.state_status = DEVICE_STATUS_OK;
    late_state.state_delay_us = 10000;
    startup_pet_asset_sleep_service_host_t late_state_host = host_for(&late_state);
    assert(startup_pet_asset_sleep_service_prepare(&late_state_host, 10) ==
           DEVICE_STATUS_TIMEOUT);
    assert(late_state.worker_prepares == 0 && late_state.retry_prepares == 0 &&
           late_state.cache_prepares == 0);

    test_state_t failed_worker = state_for();
    failed_worker.worker_status = DEVICE_STATUS_TIMEOUT;
    startup_pet_asset_sleep_service_host_t failed_worker_host = host_for(&failed_worker);
    assert(startup_pet_asset_sleep_service_prepare(&failed_worker_host, 10) ==
           DEVICE_STATUS_TIMEOUT);
    assert(failed_worker.state_prepares == 1 && failed_worker.worker_prepares == 1 &&
           failed_worker.retry_prepares == 0 && failed_worker.cache_prepares == 0);
    startup_pet_asset_sleep_service_abort(&failed_worker_host);
    assert(failed_worker.cache_aborts == 1 && failed_worker.retry_aborts == 1 &&
           failed_worker.worker_aborts == 1 && failed_worker.state_aborts == 1);

    test_state_t audio = state_for();
    audio.restored_audio = true;
    audio.take_audio = true;
    startup_pet_asset_sleep_service_host_t audio_host = host_for(&audio);
    assert(startup_pet_asset_sleep_service_prepare(&audio_host, 10) == DEVICE_STATUS_OK);
    startup_pet_asset_sleep_service_abort(&audio_host);
    assert(audio.rearms == 1);

    test_state_t active_audio = state_for();
    active_audio.restored_audio = true;
    active_audio.take_audio = true;
    active_audio.audio_active = true;
    startup_pet_asset_sleep_service_host_t active_audio_host = host_for(&active_audio);
    assert(startup_pet_asset_sleep_service_prepare(&active_audio_host, 10) == DEVICE_STATUS_OK);
    startup_pet_asset_sleep_service_abort(&active_audio_host);
    assert(active_audio.rearms == 0);

    puts("PASS startup pet System Sleep transaction");
    return 0;
}
