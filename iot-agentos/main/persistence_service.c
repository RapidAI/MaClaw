#include "persistence_service.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "esp_memory_utils.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "platform_nvs.h"
#include "task_registry.h"

/*
 * Single owner for serialized NVS transactions used by domain services.
 *
 * Flash operations briefly disable the instruction/data caches, and ESP-IDF
 * asserts when the running task's stack is not in internal RAM at that point
 * (esp_task_stack_is_sane_cache_disabled).  Several callers live on
 * PSRAM-stack tasks (gateway poll, gateway startup), so every operation is
 * executed inline when the caller's stack is internal and otherwise forwarded
 * to a dedicated internal-stack worker.  The worker is the only task besides
 * internal-stack callers that ever touches NVS through this service.
 */

typedef enum {
    PERSIST_OP_READ_BLOB,
    PERSIST_OP_WRITE_BLOB,
    PERSIST_OP_ERASE_KEY,
    PERSIST_OP_READ_I64,
    PERSIST_OP_READ_I32,
    PERSIST_OP_READ_U8,
    PERSIST_OP_WRITE_U8,
    PERSIST_OP_READ_STRING,
    PERSIST_OP_STOP,
}
 persist_op_t;

typedef struct {
    persist_op_t op;
    const char *name_space;
    const char *key;
    void *value;
    size_t *inout_size;
    size_t size;
    uint8_t u8_value;
    device_status_t result;
}
 persist_request_t;

/* Offload channel for PSRAM-stack callers.  Each routed request owns a small
 * completion semaphore, so a caller timing out can never leave a stale token
 * for the next caller or a pointer into a returned stack frame. */
static QueueHandle_t s_worker_queue;
static TaskHandle_t s_worker_task;
static SemaphoreHandle_t s_worker_start_gate;
static SemaphoreHandle_t s_worker_stopped;
static portMUX_TYPE s_worker_state_lock = portMUX_INITIALIZER_UNLOCKED;
/* The worker may not consume routed flash work until its immutable Registry
 * identity exists.  Its terminal result remains observable after the task
 * handle is cleared, so a failed retirement can never look like a clean stop. */
static bool s_worker_retiring;
static esp_err_t s_worker_exit_status = ESP_OK;
static bool s_worker_registry_retirement_failed;
typedef struct {
    persist_request_t request;
    SemaphoreHandle_t done;
    unsigned references;
}
 persist_route_job_t;
typedef struct {
    persist_op_t op;
    persist_route_job_t *job;
}
 persist_worker_message_t;
/* Admission owns the lifetime of every public request, including callers
 * running on internal stacks. Platform NVS owns the physical transaction lock;
 * these are service-owned lifecycle shells. They are
 * retained after deinit because a task may already be blocked on either mutex
 * when teardown begins.  Deleting such a mutex would turn a clean shutdown
 * into a FreeRTOS use-after-free. */
static SemaphoreHandle_t s_lifecycle_mutex;
static SemaphoreHandle_t s_deinit_mutex;
static SemaphoreHandle_t s_calls_drained;
static unsigned s_active_calls;
static volatile bool s_accepting;
static volatile bool s_stopping;
/* System Sleep PREPARE is intentionally lighter than deinit: the worker and
 * its queue stay alive, but new requests are closed after all admitted flash
 * transactions complete.  This makes rollback reversible and leaves the
 * physical NVS ownership below Persistence Service. */
static bool s_system_sleep_preparing;
/* STOP is a once-per-worker-generation message.  If its sender times out,
 * later deinit callers wait for the original completion instead of queuing a
 * second sentinel behind a worker which may already have exited. */
static bool s_worker_stop_queued;

/* Persistence stop has several dependent waits: it first serializes teardown,
 * then closes request admission, drains admitted callers, sends STOP and joins
 * the internal-stack worker.  These are one lifecycle transaction, not a
 * sequence of fresh timeout allowances. */
static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}


static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}


static esp_err_t stop_persistence_registry_entry(void *context, uint32_t timeout_ms) {
    (void)context;
    return device_status_to_platform_error(persistence_service_deinit(timeout_ms));
}


static bool valid_name(const char *value) {
    return value && value[0];
}


static bool admission_enter(void) {
    if (!s_lifecycle_mutex ||
        xSemaphoreTake(s_lifecycle_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return false;
    }

    bool admitted = s_accepting;
    if (admitted) ++s_active_calls;
    xSemaphoreGive(s_lifecycle_mutex);
    return admitted;
}


static void admission_exit(void) {
    if (!s_lifecycle_mutex ||
        xSemaphoreTake(s_lifecycle_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        /* The mutex is a permanent lifecycle shell, so this can only happen
         * for a scheduler-level fault.  Keep shutdown fail-closed rather than
         * risking use of a reclaimed queue. */
        return;
    }

    if (s_active_calls) --s_active_calls;
    if (!s_accepting && s_active_calls == 0 && s_calls_drained) {
        xSemaphoreGive(s_calls_drained);
    }

    xSemaphoreGive(s_lifecycle_mutex);
}


static void route_job_release(persist_route_job_t *job) {
    if (!job) return;
    if (__atomic_sub_fetch(&job->references, 1u, __ATOMIC_ACQ_REL) == 0) {
        if (job->done) vSemaphoreDelete(job->done);
        free(job);
    }

}


static void destroy_worker_resources(void) {
    if (s_worker_queue) {
        vQueueDelete(s_worker_queue);
        s_worker_queue = NULL;
    }

    if (s_worker_stopped) {
        vSemaphoreDelete(s_worker_stopped);
        s_worker_stopped = NULL;
    }

    if (s_worker_start_gate) {
        vSemaphoreDelete(s_worker_start_gate);
        s_worker_start_gate = NULL;
    }

    taskENTER_CRITICAL(&s_worker_state_lock);
    s_worker_task = NULL;
    s_worker_retiring = false;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    s_worker_stop_queued = false;
}

static device_status_t persistence_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}


/* The current stack frame address always lies inside the running task's
 * stack, so it tells us whether that stack is in internal RAM. */
static bool calling_task_stack_is_internal(void) {
    return esp_ptr_internal(__builtin_frame_address(0));
}


static device_status_t execute_inline(const persist_request_t *req) {
    if (!platform_nvs_is_initialized()) return DEVICE_STATUS_BUSY;
    device_status_t status;
    switch (req->op) {
        case PERSIST_OP_READ_BLOB:
            status = platform_nvs_read_blob(req->name_space, req->key,
                                            req->value, req->inout_size);
            break;
        case PERSIST_OP_WRITE_BLOB:
            status = platform_nvs_write_blob(req->name_space, req->key,
                                             req->value, req->size);
            break;
        case PERSIST_OP_ERASE_KEY:
            status = platform_nvs_erase_key(req->name_space, req->key);
            break;
        case PERSIST_OP_READ_I64:
            status = platform_nvs_read_i64(req->name_space, req->key,
                                           req->value);
            break;
        case PERSIST_OP_READ_I32:
            status = platform_nvs_read_i32(req->name_space, req->key,
                                           req->value);
            break;
        case PERSIST_OP_READ_U8:
            status = platform_nvs_read_u8(req->name_space, req->key,
                                          req->value);
            break;
        case PERSIST_OP_WRITE_U8:
            status = platform_nvs_write_u8(req->name_space, req->key,
                                           req->u8_value);
            break;
        case PERSIST_OP_READ_STRING:
            status = platform_nvs_read_string(req->name_space, req->key,
                                              req->value, req->inout_size);
            break;
        default:
            status = DEVICE_STATUS_INVALID_ARGUMENT;
            break;
    }

    return status;
}

static void persistence_worker_task(void *arg) {
    (void)arg;
    if (!s_worker_start_gate ||
        xSemaphoreTake(s_worker_start_gate, portMAX_DELAY) != pdTRUE) {
        goto finish;
    }
    persist_worker_message_t message;
    while (true) {
        if (xQueueReceive(s_worker_queue, &message, portMAX_DELAY) != pdTRUE) {
            continue;
        }

        if (message.op == PERSIST_OP_STOP) break;
        persist_route_job_t *job = message.job;
        if (!job) continue;
        /* The request carries only pointers into caller-owned buffers; those
         * are consumed here while caches are enabled (the Platform NVS write copies into
         * NVS's internal page buffer before commit disables them). */
        job->request.result = execute_inline(&job->request);
        xSemaphoreGive(job->done);
        route_job_release(job);
    }

finish: {
    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_worker_state_lock);
    s_worker_retiring = true;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    /* Registry identity is the authoritative worker lifetime.  Do not clear
     * the handle or publish stopped until it has retired, otherwise a direct
     * service deinit or a System Sleep observer can create/reopen work while
     * an owner sweep can still find this old worker. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_STORAGE, (void *)self, 10);
    taskENTER_CRITICAL(&s_worker_state_lock);
    s_worker_exit_status = registry_err;
    if (s_worker_task == self) s_worker_task = NULL;
    s_worker_retiring = false;
    if (registry_err != ESP_OK) {
        s_worker_registry_retirement_failed = true;
        __atomic_store_n(&s_accepting, false, __ATOMIC_RELEASE);
        __atomic_store_n(&s_stopping, true, __ATOMIC_RELEASE);
    }
    taskEXIT_CRITICAL(&s_worker_state_lock);
    /* The final completion signal is the hand-off of all service-owned worker
     * resources.  Do not access queue/semaphore/registry state after giving
     * it: deinit may immediately reclaim those objects. */
    if (s_worker_stopped) xSemaphoreGive(s_worker_stopped);
    vTaskDelete(NULL);
}
}


static device_status_t execute(persist_request_t *req) {
    if (!admission_enter()) return DEVICE_STATUS_BUSY;
    device_status_t err;
    if (calling_task_stack_is_internal()) {
        err = execute_inline(req);
    }
 else if (!s_worker_queue) {
        err = DEVICE_STATUS_BUSY;
    }
 else {
        persist_route_job_t *job = calloc(1, sizeof(*job));
        if (!job) {
            err = DEVICE_STATUS_RESOURCE_EXHAUSTED;
        }
 else if (!(job->done = xSemaphoreCreateBinary())) {
            free(job);
            err = DEVICE_STATUS_RESOURCE_EXHAUSTED;
        }
 else {
            job->request = *req;
            /* Both caller and worker own a reference once the message can be
             * observed by the worker.  Establish this before queueing: a high
             * priority worker may run immediately after xQueueSend(). */
            __atomic_store_n(&job->references, 2u, __ATOMIC_RELEASE);
            bool lifecycle_locked = s_lifecycle_mutex &&
                                    xSemaphoreTake(s_lifecycle_mutex,
                                                   pdMS_TO_TICKS(3000)) == pdTRUE;
            if (!lifecycle_locked || !s_accepting || !s_worker_queue) {
                if (lifecycle_locked) xSemaphoreGive(s_lifecycle_mutex);
                err = DEVICE_STATUS_BUSY;
                route_job_release(job);
                route_job_release(job);
            }
 else {
                xSemaphoreGive(s_lifecycle_mutex);
                persist_worker_message_t message = {
                    .op = job->request.op,
                    .job = job,
                }
;
                if (xQueueSend(s_worker_queue, &message, pdMS_TO_TICKS(1000)) != pdTRUE) {
                    err = DEVICE_STATUS_TIMEOUT;
                    route_job_release(job);
                    route_job_release(job);
                }
 else {
                    /* The worker and caller each release one reference. */
                    if (xSemaphoreTake(job->done, portMAX_DELAY) == pdTRUE) {
                    err = job->request.result;
                    route_job_release(job);
                    }
 else {
                        /* A binary semaphore wait with portMAX_DELAY only
                         * fails if the scheduler is already compromised. The
                         * caller's stack-backed request remains admitted. */
                        err = DEVICE_STATUS_TIMEOUT;
                        route_job_release(job);
                    }

                }

            }

        }

    }

    admission_exit();
    return err;
}


device_status_t persistence_service_init(void) {
    if (!platform_nvs_is_initialized()) return DEVICE_STATUS_BUSY;
    if (!s_lifecycle_mutex) s_lifecycle_mutex = xSemaphoreCreateMutex();
    if (!s_deinit_mutex) s_deinit_mutex = xSemaphoreCreateMutex();
    if (!s_calls_drained) s_calls_drained = xSemaphoreCreateBinary();
    if (!s_lifecycle_mutex || !s_deinit_mutex || !s_calls_drained) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    if (xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    if (s_accepting) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_OK;
    }

    /* A bounded deinit may have closed admission but still be waiting for a
     * routed caller or the worker.  Reopening this generation would race the
     * pending STOP sentinel; callers must finish its deinit first. */
    taskENTER_CRITICAL(&s_worker_state_lock);
    const bool retirement_failed = s_worker_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    if (retirement_failed ||
        (__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
         (s_worker_task || s_worker_queue))) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_BUSY;
    }

    /* A previous timed-out stop may have been completed by the last caller
     * after its waiter timed out.  Never let that old binary signal satisfy a
     * later deinit before the current generation's calls have actually left. */
    while (xSemaphoreTake(s_calls_drained, 0) == pdTRUE) {
    }

    if (!s_worker_queue) {
        s_worker_queue = xQueueCreate(2, sizeof(persist_worker_message_t));
    }

    if (!s_worker_start_gate) s_worker_start_gate = xSemaphoreCreateBinary();
    if (!s_worker_stopped) s_worker_stopped = xSemaphoreCreateBinary();
    bool resources_ready = s_worker_queue && s_worker_start_gate && s_worker_stopped;
    if (!resources_ready) {
        destroy_worker_resources();
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    TaskHandle_t worker = NULL;
    taskENTER_CRITICAL(&s_worker_state_lock);
    worker = s_worker_task;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    if (!worker) {
        /* Internal stack by design: this task performs flash transactions. */
        if (xTaskCreate(persistence_worker_task, "maclaw_persist", 4096, NULL, 4,
                        &worker) != pdPASS) {
            worker = NULL;
        }
    }

    if (!worker) {
        destroy_worker_resources();
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    s_worker_stop_queued = false;
    while (xSemaphoreTake(s_worker_start_gate, 0) == pdTRUE) {
    }
    while (xSemaphoreTake(s_worker_stopped, 0) == pdTRUE) {
    }
    taskENTER_CRITICAL(&s_worker_state_lock);
    s_worker_task = worker;
    s_worker_retiring = false;
    s_worker_exit_status = ESP_OK;
    s_worker_registry_retirement_failed = false;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "persistence_worker",
        .context = (void *)worker,
        .stop = stop_persistence_registry_entry,
    }
);
    if (registry_err != ESP_OK) {
        __atomic_store_n(&s_accepting, false, __ATOMIC_RELEASE);
        __atomic_store_n(&s_stopping, true, __ATOMIC_RELEASE);
        /* The worker is waiting behind its start gate. Release it only after
         * recording the failed registration so it can take the ordinary STOP
         * path and publish one terminal completion. */
        xSemaphoreGive(s_worker_start_gate);
        xSemaphoreGive(s_deinit_mutex);
        (void)persistence_service_deinit(500);
        return registry_err == ESP_ERR_NO_MEM ? DEVICE_STATUS_RESOURCE_EXHAUSTED : DEVICE_STATUS_INTERNAL_ERROR;
    }

    xSemaphoreGive(s_worker_start_gate);
    __atomic_store_n(&s_stopping, false, __ATOMIC_RELEASE);
    __atomic_store_n(&s_accepting, true, __ATOMIC_RELEASE);
    xSemaphoreGive(s_deinit_mutex);
    return DEVICE_STATUS_OK;
}


device_status_t persistence_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_deinit_mutex || !s_lifecycle_mutex) return DEVICE_STATUS_OK;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_deinit_mutex, remaining) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    taskENTER_CRITICAL(&s_worker_state_lock);
    TaskHandle_t worker = s_worker_task;
    const esp_err_t exit_status = s_worker_exit_status;
    const bool retirement_failed = s_worker_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    if (!worker) {
        /* A missing worker handle while its resources remain is not a clean
         * stop: no owner can prove the completion semaphore belongs to an
         * exited generation. Keep this service fail-closed rather than
         * deleting queue/semaphore objects a late worker might still touch. */
        if (__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) && s_worker_queue &&
            s_worker_stopped &&
            (remaining = stop_remaining_ticks(started, budget)) != 0 &&
            xSemaphoreTake(s_worker_stopped, remaining) == pdTRUE) {
            destroy_worker_resources();
        }

        const bool resources_remaining = s_worker_queue || s_worker_start_gate || s_worker_stopped;
        xSemaphoreGive(s_deinit_mutex);
        if (retirement_failed) return persistence_status_from_esp_err(exit_status);
        return resources_remaining ? DEVICE_STATUS_TIMEOUT : DEVICE_STATUS_OK;
    }

    if (xTaskGetCurrentTaskHandle() == worker) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_BUSY;
    }

    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lifecycle_mutex, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    /* Clear a completion belonging to an earlier, timed-out stop while no
     * caller can update active_calls. */
    while (xSemaphoreTake(s_calls_drained, 0) == pdTRUE) {
    }

    bool wait_for_calls = s_active_calls != 0;
    __atomic_store_n(&s_accepting, false, __ATOMIC_RELEASE);
    __atomic_store_n(&s_stopping, true, __ATOMIC_RELEASE);
    xSemaphoreGive(s_lifecycle_mutex);
    remaining = stop_remaining_ticks(started, budget);
    if (wait_for_calls &&
        (remaining == 0 || xSemaphoreTake(s_calls_drained, remaining) != pdTRUE)) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    if (!s_worker_queue || !s_worker_stopped) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    BaseType_t queued = pdTRUE;
    if (!s_worker_stop_queued) {
        persist_worker_message_t stop_request = {.op = PERSIST_OP_STOP}
;
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0) {
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }

        queued = xQueueSend(s_worker_queue, &stop_request, remaining);
        if (queued == pdTRUE) s_worker_stop_queued = true;
    }

    remaining = stop_remaining_ticks(started, budget);
    if (queued != pdTRUE || remaining == 0 ||
        xSemaphoreTake(s_worker_stopped, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    taskENTER_CRITICAL(&s_worker_state_lock);
    const esp_err_t completed_exit_status = s_worker_exit_status;
    const bool completed_retirement_failed = s_worker_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    if (completed_retirement_failed || completed_exit_status != ESP_OK) {
        xSemaphoreGive(s_deinit_mutex);
        return completed_exit_status == ESP_ERR_TIMEOUT ? DEVICE_STATUS_TIMEOUT
                                                         : DEVICE_STATUS_INTERNAL_ERROR;
    }
    destroy_worker_resources();
    xSemaphoreGive(s_deinit_mutex);
    return DEVICE_STATUS_OK;
}


bool persistence_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_worker_state_lock);
    const bool healthy = !s_worker_retiring && !s_worker_registry_retirement_failed;
    const bool has_worker = s_worker_task != NULL;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    return healthy && __atomic_load_n(&s_accepting, __ATOMIC_ACQUIRE) && has_worker &&
           s_worker_queue && s_worker_start_gate && s_worker_stopped;
}

device_status_t persistence_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_deinit_mutex || !s_lifecycle_mutex || !s_calls_drained) {
        return DEVICE_STATUS_UNAVAILABLE;
    }

    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_deinit_mutex, remaining) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lifecycle_mutex, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }

    taskENTER_CRITICAL(&s_worker_state_lock);
    const bool worker_retiring = s_worker_retiring;
    const bool registry_retirement_failed = s_worker_registry_retirement_failed;
    const bool worker_running = s_worker_task != NULL;
    taskEXIT_CRITICAL(&s_worker_state_lock);
    if (!s_accepting || s_stopping || s_system_sleep_preparing || worker_retiring ||
        registry_retirement_failed || !worker_running || !s_worker_queue ||
        !s_worker_start_gate || !s_worker_stopped) {
        xSemaphoreGive(s_lifecycle_mutex);
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_BUSY;
    }

    /* Discard only an old deinit/prepare completion while holding the same
     * admission mutex that prevents a late admitted request from escaping the
     * following closed state. */
    while (xSemaphoreTake(s_calls_drained, 0) == pdTRUE) {
    }
    const bool wait_for_calls = s_active_calls != 0;
    s_system_sleep_preparing = true;
    __atomic_store_n(&s_accepting, false, __ATOMIC_RELEASE);
    xSemaphoreGive(s_lifecycle_mutex);

    remaining = stop_remaining_ticks(started, budget);
    if (!wait_for_calls ||
        (remaining != 0 && xSemaphoreTake(s_calls_drained, remaining) == pdTRUE)) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_OK;
    }

    /* Keep Persistence admission closed. Platform NVS serialisation does not
     * authorise a new mutation while a timed-out pre-fence request may still
     * be unwinding across the parent electrical transaction; only Power's
     * reverse-order ABORT may reopen this generation. */
    xSemaphoreGive(s_deinit_mutex);
    return DEVICE_STATUS_TIMEOUT;
}

void persistence_service_abort_system_sleep_prepare(void) {
    if (!s_deinit_mutex || !s_lifecycle_mutex) return;
    if (xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) return;
    if (xSemaphoreTake(s_lifecycle_mutex, pdMS_TO_TICKS(3000)) == pdTRUE) {
        if (s_system_sleep_preparing) {
            s_system_sleep_preparing = false;
            taskENTER_CRITICAL(&s_worker_state_lock);
            const bool can_reopen = !s_worker_retiring &&
                                    !s_worker_registry_retirement_failed &&
                                    s_worker_task != NULL;
            taskEXIT_CRITICAL(&s_worker_state_lock);
            if (!s_stopping && can_reopen && s_worker_queue && s_worker_start_gate &&
                s_worker_stopped) {
                __atomic_store_n(&s_accepting, true, __ATOMIC_RELEASE);
            }
        }
        xSemaphoreGive(s_lifecycle_mutex);
    }
    xSemaphoreGive(s_deinit_mutex);
}


device_status_t persistence_service_read_blob(const char *name_space, const char *key,
                                        void *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !inout_size) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_READ_BLOB, .name_space = name_space, .key = key,
        .value = out_value, .inout_size = inout_size,
    }
;
    return execute(&request);
}


device_status_t persistence_service_write_blob(const char *name_space, const char *key,
                                         const void *value, size_t size) {
    if (!valid_name(name_space) || !valid_name(key) || !value || !size) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_WRITE_BLOB, .name_space = name_space, .key = key,
        .value = (void *)value, .size = size,
    }
;
    return execute(&request);
}

device_status_t persistence_service_erase_key(const char *name_space, const char *key) {
    if (!valid_name(name_space) || !valid_name(key)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    persist_request_t request = {
        .op = PERSIST_OP_ERASE_KEY, .name_space = name_space, .key = key,
    };
    return execute(&request);
}


device_status_t persistence_service_read_i64(const char *name_space, const char *key,
                                       int64_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_READ_I64, .name_space = name_space, .key = key,
        .value = out_value,
    }
;
    return execute(&request);
}


device_status_t persistence_service_read_i32(const char *name_space, const char *key,
                                       int32_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_READ_I32, .name_space = name_space, .key = key,
        .value = out_value,
    }
;
    return execute(&request);
}


device_status_t persistence_service_read_u8(const char *name_space, const char *key,
                                      uint8_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_READ_U8, .name_space = name_space, .key = key,
        .value = out_value,
    }
;
    return execute(&request);
}


device_status_t persistence_service_write_u8(const char *name_space, const char *key,
                                       uint8_t value) {
    if (!valid_name(name_space) || !valid_name(key)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_WRITE_U8, .name_space = name_space, .key = key,
        .u8_value = value,
    }
;
    return execute(&request);
}


device_status_t persistence_service_read_string(const char *name_space, const char *key,
                                          char *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value || !inout_size) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    persist_request_t request = {
        .op = PERSIST_OP_READ_STRING, .name_space = name_space, .key = key,
        .value = out_value, .inout_size = inout_size,
    }
;
    return execute(&request);
}
