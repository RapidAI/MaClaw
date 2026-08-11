#include "persistence_service.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "esp_memory_utils.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "nvs.h"
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
    PERSIST_OP_READ_I64,
    PERSIST_OP_READ_I32,
    PERSIST_OP_READ_U8,
    PERSIST_OP_WRITE_U8,
    PERSIST_OP_READ_STRING,
    PERSIST_OP_STOP,
} persist_op_t;

typedef struct {
    persist_op_t op;
    const char *name_space;
    const char *key;
    void *value;
    size_t *inout_size;
    size_t size;
    uint8_t u8_value;
    esp_err_t result;
} persist_request_t;

static SemaphoreHandle_t s_lock;
/* Offload channel for PSRAM-stack callers.  Each routed request owns a small
 * completion semaphore, so a caller timing out can never leave a stale token
 * for the next caller or a pointer into a returned stack frame. */
static QueueHandle_t s_worker_queue;
static TaskHandle_t s_worker_task;
static SemaphoreHandle_t s_worker_stopped;
typedef struct {
    persist_request_t request;
    SemaphoreHandle_t done;
    unsigned references;
} persist_route_job_t;
typedef struct {
    persist_op_t op;
    persist_route_job_t *job;
} persist_worker_message_t;
/* Admission owns the lifetime of every public request, including callers
 * running on internal stacks.  `s_lock` belongs to main.c and deliberately
 * survives this service; these are service-owned lifecycle shells.  They are
 * retained after deinit because a task may already be blocked on either mutex
 * when teardown begins.  Deleting such a mutex would turn a clean shutdown
 * into a FreeRTOS use-after-free. */
static SemaphoreHandle_t s_lifecycle_mutex;
static SemaphoreHandle_t s_deinit_mutex;
static SemaphoreHandle_t s_calls_drained;
static unsigned s_active_calls;
static volatile bool s_accepting;
static volatile bool s_stopping;
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
    return persistence_service_deinit(timeout_ms);
}

static bool valid_name(const char *value) {
    return value && value[0];
}

static bool lock(void) {
    return s_lock && xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) == pdTRUE;
}

static void unlock(void) {
    if (s_lock) xSemaphoreGive(s_lock);
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
    s_worker_task = NULL;
    s_worker_stop_queued = false;
}

/* The current stack frame address always lies inside the running task's
 * stack, so it tells us whether that stack is in internal RAM. */
static bool calling_task_stack_is_internal(void) {
    return esp_ptr_internal(__builtin_frame_address(0));
}

static esp_err_t execute_inline(const persist_request_t *req) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err;
    switch (req->op) {
        case PERSIST_OP_READ_BLOB:
            err = nvs_open(req->name_space, NVS_READONLY, &nvs);
            if (err == ESP_OK) {
                err = nvs_get_blob(nvs, req->key, req->value, req->inout_size);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_WRITE_BLOB:
            err = nvs_open(req->name_space, NVS_READWRITE, &nvs);
            if (err == ESP_OK) {
                err = nvs_set_blob(nvs, req->key, req->value, req->size);
                if (err == ESP_OK) err = nvs_commit(nvs);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_READ_I64:
            err = nvs_open(req->name_space, NVS_READONLY, &nvs);
            if (err == ESP_OK) {
                err = nvs_get_i64(nvs, req->key, req->value);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_READ_I32:
            err = nvs_open(req->name_space, NVS_READONLY, &nvs);
            if (err == ESP_OK) {
                err = nvs_get_i32(nvs, req->key, req->value);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_READ_U8:
            err = nvs_open(req->name_space, NVS_READONLY, &nvs);
            if (err == ESP_OK) {
                err = nvs_get_u8(nvs, req->key, req->value);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_WRITE_U8:
            err = nvs_open(req->name_space, NVS_READWRITE, &nvs);
            if (err == ESP_OK) {
                err = nvs_set_u8(nvs, req->key, req->u8_value);
                if (err == ESP_OK) err = nvs_commit(nvs);
                nvs_close(nvs);
            }
            break;
        case PERSIST_OP_READ_STRING:
            err = nvs_open(req->name_space, NVS_READONLY, &nvs);
            if (err == ESP_OK) {
                err = nvs_get_str(nvs, req->key, req->value, req->inout_size);
                nvs_close(nvs);
            }
            break;
        default:
            err = ESP_ERR_INVALID_ARG;
            break;
    }
    unlock();
    return err;
}

static void persistence_worker_task(void *arg) {
    (void)arg;
    persist_worker_message_t message;
    while (true) {
        if (xQueueReceive(s_worker_queue, &message, portMAX_DELAY) != pdTRUE) {
            continue;
        }
        if (message.op == PERSIST_OP_STOP) break;
        persist_route_job_t *job = message.job;
        if (!job) continue;
        /* The request carries only pointers into caller-owned buffers; those
         * are consumed here while caches are enabled (nvs_set_blob copies into
         * NVS's internal page buffer before commit disables them). */
        job->request.result = execute_inline(&job->request);
        xSemaphoreGive(job->done);
        route_job_release(job);
    }
    /* The final completion signal is the hand-off of all service-owned worker
     * resources.  Do not access queue/semaphore/registry state after giving
     * it: deinit may immediately reclaim those objects. */
    if (s_worker_stopped) xSemaphoreGive(s_worker_stopped);
    vTaskDelete(NULL);
}

static esp_err_t execute(persist_request_t *req) {
    if (!admission_enter()) return ESP_ERR_INVALID_STATE;
    esp_err_t err;
    if (calling_task_stack_is_internal()) {
        err = execute_inline(req);
    } else if (!s_worker_queue) {
        err = ESP_ERR_INVALID_STATE;
    } else {
        persist_route_job_t *job = calloc(1, sizeof(*job));
        if (!job) {
            err = ESP_ERR_NO_MEM;
        } else if (!(job->done = xSemaphoreCreateBinary())) {
            free(job);
            err = ESP_ERR_NO_MEM;
        } else {
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
                err = ESP_ERR_INVALID_STATE;
                route_job_release(job);
                route_job_release(job);
            } else {
                xSemaphoreGive(s_lifecycle_mutex);
                persist_worker_message_t message = {
                    .op = job->request.op,
                    .job = job,
                };
                if (xQueueSend(s_worker_queue, &message, pdMS_TO_TICKS(1000)) != pdTRUE) {
                    err = ESP_ERR_TIMEOUT;
                    route_job_release(job);
                    route_job_release(job);
                } else {
                    /* The worker and caller each release one reference. */
                    if (xSemaphoreTake(job->done, portMAX_DELAY) == pdTRUE) {
                    err = job->request.result;
                    route_job_release(job);
                    } else {
                        /* A binary semaphore wait with portMAX_DELAY only
                         * fails if the scheduler is already compromised. The
                         * caller's stack-backed request remains admitted. */
                        err = ESP_ERR_TIMEOUT;
                        route_job_release(job);
                    }
                }
            }
        }
    }
    admission_exit();
    return err;
}

esp_err_t persistence_service_init(SemaphoreHandle_t transaction_mutex) {
    if (!transaction_mutex) return ESP_ERR_INVALID_ARG;
    if (s_lock && s_lock != transaction_mutex) return ESP_ERR_INVALID_STATE;
    s_lock = transaction_mutex;
    if (!s_lifecycle_mutex) s_lifecycle_mutex = xSemaphoreCreateMutex();
    if (!s_deinit_mutex) s_deinit_mutex = xSemaphoreCreateMutex();
    if (!s_calls_drained) s_calls_drained = xSemaphoreCreateBinary();
    if (!s_lifecycle_mutex || !s_deinit_mutex || !s_calls_drained) {
        return ESP_ERR_NO_MEM;
    }
    if (xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    if (s_accepting) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_OK;
    }
    /* A bounded deinit may have closed admission but still be waiting for a
     * routed caller or the worker.  Reopening this generation would race the
     * pending STOP sentinel; callers must finish its deinit first. */
    if (__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
        (s_worker_task || s_worker_queue)) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    /* A previous timed-out stop may have been completed by the last caller
     * after its waiter timed out.  Never let that old binary signal satisfy a
     * later deinit before the current generation's calls have actually left. */
    while (xSemaphoreTake(s_calls_drained, 0) == pdTRUE) {
    }
    if (!s_worker_queue) {
        s_worker_queue = xQueueCreate(2, sizeof(persist_worker_message_t));
    }
    if (!s_worker_stopped) s_worker_stopped = xSemaphoreCreateBinary();
    bool resources_ready = s_worker_queue && s_worker_stopped;
    if (!resources_ready) {
        destroy_worker_resources();
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_NO_MEM;
    }
    if (!s_worker_task) {
        /* Internal stack by design: this task performs flash transactions. */
        if (xTaskCreate(persistence_worker_task, "maclaw_persist", 4096, NULL, 4,
                        &s_worker_task) != pdPASS) {
            s_worker_task = NULL;
        }
    }
    if (!s_worker_task) {
        destroy_worker_resources();
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_NO_MEM;
    }
    s_worker_stop_queued = false;
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_STORAGE,
        .name = "persistence_worker",
        .context = NULL,
        .stop = stop_persistence_registry_entry,
    });
    if (registry_err != ESP_OK) {
        xSemaphoreGive(s_deinit_mutex);
        (void)persistence_service_deinit(500);
        return registry_err;
    }
    __atomic_store_n(&s_stopping, false, __ATOMIC_RELEASE);
    __atomic_store_n(&s_accepting, true, __ATOMIC_RELEASE);
    xSemaphoreGive(s_deinit_mutex);
    return ESP_OK;
}

esp_err_t persistence_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_deinit_mutex || !s_lifecycle_mutex) return ESP_OK;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_deinit_mutex, remaining) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    TaskHandle_t worker = s_worker_task;
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
        const bool resources_remaining = s_worker_queue || s_worker_stopped;
        xSemaphoreGive(s_deinit_mutex);
        return resources_remaining ? ESP_ERR_TIMEOUT : ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == worker) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_INVALID_STATE;
    }
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lifecycle_mutex, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_TIMEOUT;
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
        return ESP_ERR_TIMEOUT;
    }
    if (!s_worker_queue || !s_worker_stopped) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_TIMEOUT;
    }
    BaseType_t queued = pdTRUE;
    if (!s_worker_stop_queued) {
        persist_worker_message_t stop_request = {.op = PERSIST_OP_STOP};
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0) {
            xSemaphoreGive(s_deinit_mutex);
            return ESP_ERR_TIMEOUT;
        }
        queued = xQueueSend(s_worker_queue, &stop_request, remaining);
        if (queued == pdTRUE) s_worker_stop_queued = true;
    }
    remaining = stop_remaining_ticks(started, budget);
    if (queued != pdTRUE || remaining == 0 ||
        xSemaphoreTake(s_worker_stopped, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return ESP_ERR_TIMEOUT;
    }
    destroy_worker_resources();
    xSemaphoreGive(s_deinit_mutex);
    return ESP_OK;
}

bool persistence_service_is_initialized(void) {
    return __atomic_load_n(&s_accepting, __ATOMIC_ACQUIRE) && s_worker_task &&
           s_worker_queue && s_worker_stopped;
}

esp_err_t persistence_service_read_blob(const char *name_space, const char *key,
                                        void *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !inout_size) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_READ_BLOB, .name_space = name_space, .key = key,
        .value = out_value, .inout_size = inout_size,
    };
    return execute(&request);
}

esp_err_t persistence_service_write_blob(const char *name_space, const char *key,
                                         const void *value, size_t size) {
    if (!valid_name(name_space) || !valid_name(key) || !value || !size) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_WRITE_BLOB, .name_space = name_space, .key = key,
        .value = (void *)value, .size = size,
    };
    return execute(&request);
}

esp_err_t persistence_service_read_i64(const char *name_space, const char *key,
                                       int64_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_READ_I64, .name_space = name_space, .key = key,
        .value = out_value,
    };
    return execute(&request);
}

esp_err_t persistence_service_read_i32(const char *name_space, const char *key,
                                       int32_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_READ_I32, .name_space = name_space, .key = key,
        .value = out_value,
    };
    return execute(&request);
}

esp_err_t persistence_service_read_u8(const char *name_space, const char *key,
                                      uint8_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_READ_U8, .name_space = name_space, .key = key,
        .value = out_value,
    };
    return execute(&request);
}

esp_err_t persistence_service_write_u8(const char *name_space, const char *key,
                                       uint8_t value) {
    if (!valid_name(name_space) || !valid_name(key)) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_WRITE_U8, .name_space = name_space, .key = key,
        .u8_value = value,
    };
    return execute(&request);
}

esp_err_t persistence_service_read_string(const char *name_space, const char *key,
                                          char *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value || !inout_size) {
        return ESP_ERR_INVALID_ARG;
    }
    persist_request_t request = {
        .op = PERSIST_OP_READ_STRING, .name_space = name_space, .key = key,
        .value = out_value, .inout_size = inout_size,
    };
    return execute(&request);
}
