#pragma once

/*
 * Internal-stack configuration persistence worker.
 *
 * The service owns FreeRTOS queue/task lifecycle, Storage Registry identity
 * and the reversible System Sleep admission fence. The composition root
 * supplies the value-only configuration transaction because it retains the
 * runtime projections updated after durable commits.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_service.h"
#include "device_api.h"

typedef struct {
    unsigned percent;
    uint32_t screen_sleep_seconds;
    bool brightness;
    bool screen_sleep;
    bool display_policy;
    bool display_policy_has_brightness;
    bool display_policy_has_screen_sleep;
    bool output_volume_policy;
    bool gateway_token;
    bool checkpoint_current_snapshot;
    bool hub_authenticated;
    char token[CONFIGURATION_GATEWAY_TOKEN_CAPACITY];
} configuration_persistence_request_t;

typedef struct {
    device_status_t status;
    uint64_t configuration_revision;
} configuration_persistence_reply_t;

typedef struct {
    uint32_t struct_size;
    device_status_t (*run_transaction)(
        const configuration_persistence_request_t *request,
        configuration_persistence_reply_t *out_reply, void *context);
    void *context;
} configuration_persistence_worker_service_host_t;

device_status_t configuration_persistence_worker_service_init(
    const configuration_persistence_worker_service_host_t *host);

/* Serializes a durable transaction on the internal-stack worker. The two
 * timeout values preserve the caller's mutex-admission, queue-admission and
 * completion budgets separately; reply is written only for a matching worker
 * completion. */
device_status_t configuration_persistence_worker_service_submit(
    const configuration_persistence_request_t *request,
    uint32_t mutex_timeout_ms, uint32_t queue_timeout_ms,
    uint32_t completion_timeout_ms,
    configuration_persistence_reply_t *out_reply);

/* Submit one transaction against a single monotonic deadline.  Each worker
 * phase derives its remaining budget from that same deadline, so mutex,
 * queue, and completion waits cannot cumulatively exceed the caller's parent
 * budget.  The deadline is expressed in the platform monotonic microsecond
 * domain supplied by the composition root. */
device_status_t configuration_persistence_worker_service_submit_until(
    const configuration_persistence_request_t *request,
    int64_t deadline_us, configuration_persistence_reply_t *out_reply);

/* Normal stop is terminal. System Sleep PREPARE instead fences admission and
 * retains the same worker generation for a later ABORT. */
device_status_t configuration_persistence_worker_service_stop(uint32_t timeout_ms);
device_status_t configuration_persistence_worker_service_prepare_system_sleep(
    uint32_t timeout_ms);
void configuration_persistence_worker_service_abort_system_sleep_prepare(void);
