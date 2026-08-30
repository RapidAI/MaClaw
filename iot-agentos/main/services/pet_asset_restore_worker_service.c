#include "services/pet_asset_restore_worker_service.h"

#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

typedef struct {
    const pet_asset_restore_worker_service_host_t *host;
    SemaphoreHandle_t completion;
    device_status_t status;
} pet_asset_restore_worker_job_t;

static bool host_valid(const pet_asset_restore_worker_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->run_restore;
}

static void pet_asset_restore_worker_task(void *arg) {
    pet_asset_restore_worker_job_t *job = (pet_asset_restore_worker_job_t *)arg;
    if (job && job->host) {
        job->status = job->host->run_restore(job->host->context);
    }
    if (job && job->completion) xSemaphoreGive(job->completion);
    vTaskDeleteWithCaps(NULL);
}

device_status_t pet_asset_restore_worker_service_run(
    const pet_asset_restore_worker_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;

    SemaphoreHandle_t completion = xSemaphoreCreateBinary();
    if (!completion) return DEVICE_STATUS_RESOURCE_EXHAUSTED;

    pet_asset_restore_worker_job_t job = {
        .host = host,
        .completion = completion,
        .status = DEVICE_STATUS_INTERNAL_ERROR,
    };
    TaskHandle_t task = NULL;
    if (xTaskCreatePinnedToCoreWithCaps(
            pet_asset_restore_worker_task, "maclaw_pet_restore", 8192, &job, 1,
            &task, 1, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT) != pdPASS) {
        vSemaphoreDelete(completion);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    /* This runs before connectivity allocates TLS buffers. The worker signals
     * before deleting its own stack and does not access `job` after the give. */
    (void)task;
    xSemaphoreTake(completion, portMAX_DELAY);
    vSemaphoreDelete(completion);
    return job.status;
}
