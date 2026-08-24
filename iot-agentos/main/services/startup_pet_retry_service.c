#include "services/startup_pet_retry_service.h"

#include "esp_err.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_timer_mutex;
static SemaphoreHandle_t s_callback_drained;
static esp_timer_handle_t s_timer;
static bool s_initialized;
static bool s_callback_admission_open;
static bool s_system_sleep_preparing;
static bool s_stopped;
static uint32_t s_callbacks_inflight;
static bool s_due;

static device_status_t status_from_esp_err(esp_err_t err) {
    if (err == ESP_OK) return DEVICE_STATUS_OK;
    if (err == ESP_ERR_TIMEOUT) return DEVICE_STATUS_TIMEOUT;
    if (err == ESP_ERR_NO_MEM) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (err == ESP_ERR_INVALID_ARG) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (err == ESP_ERR_INVALID_STATE) return DEVICE_STATUS_BUSY;
    return DEVICE_STATUS_INTERNAL_ERROR;
}

static void retry_timer_cb(void *arg) {
    (void)arg;
    bool entered = false;
    taskENTER_CRITICAL(&s_lock);
    if (s_callback_admission_open && !s_system_sleep_preparing) {
        ++s_callbacks_inflight;
        entered = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!entered) return;

    taskENTER_CRITICAL(&s_lock);
    /* PREPARE may close admission after this callback took its first lock.
     * Revalidate at the side-effect boundary so a retiring timer callback can
     * never publish a due retry after a successful quiesce. */
    if (s_callback_admission_open && !s_system_sleep_preparing && !s_stopped) {
        s_due = true;
    }
    --s_callbacks_inflight;
    const bool drained = !s_callback_admission_open && s_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_lock);
    if (drained && s_callback_drained) xSemaphoreGive(s_callback_drained);
}

static esp_err_t ensure_timer(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool allowed = s_initialized && s_callback_admission_open &&
                         !s_system_sleep_preparing && !s_stopped;
    const esp_timer_handle_t existing = s_timer;
    taskEXIT_CRITICAL(&s_lock);
    if (!allowed) return ESP_ERR_INVALID_STATE;
    if (existing) return ESP_OK;

    esp_timer_handle_t timer = NULL;
    const esp_timer_create_args_t args = {
        .callback = retry_timer_cb,
        .name = "pet_retry",
    };
    esp_err_t err = esp_timer_create(&args, &timer);
    if (err != ESP_OK) return err;

    taskENTER_CRITICAL(&s_lock);
    if (!s_timer && s_initialized && s_callback_admission_open &&
        !s_system_sleep_preparing && !s_stopped) {
        s_timer = timer;
        timer = NULL;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (timer) (void)esp_timer_delete(timer);
    return timer ? ESP_ERR_INVALID_STATE : ESP_OK;
}

device_status_t startup_pet_retry_service_init(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_initialized) {
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_OK;
    }
    taskEXIT_CRITICAL(&s_lock);

    SemaphoreHandle_t timer_mutex = xSemaphoreCreateMutex();
    SemaphoreHandle_t callback_drained = xSemaphoreCreateBinary();
    if (!timer_mutex || !callback_drained) {
        if (timer_mutex) vSemaphoreDelete(timer_mutex);
        if (callback_drained) vSemaphoreDelete(callback_drained);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    s_timer_mutex = timer_mutex;
    s_callback_drained = callback_drained;
    s_callback_admission_open = true;
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lock);
    return DEVICE_STATUS_OK;
}

device_status_t startup_pet_retry_service_schedule(uint64_t delay_us) {
    if (delay_us == 0 || !s_timer_mutex) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (xSemaphoreTake(s_timer_mutex, 0) != pdTRUE) return DEVICE_STATUS_BUSY;
    const esp_err_t ensure_err = ensure_timer();
    esp_timer_handle_t timer = NULL;
    if (ensure_err == ESP_OK) {
        taskENTER_CRITICAL(&s_lock);
        if (s_callback_admission_open && !s_system_sleep_preparing && !s_stopped) timer = s_timer;
        taskEXIT_CRITICAL(&s_lock);
    }
    const esp_err_t start_err = timer ? esp_timer_start_once(timer, delay_us)
                                      : (ensure_err == ESP_OK ? ESP_ERR_INVALID_STATE : ensure_err);
    xSemaphoreGive(s_timer_mutex);
    return status_from_esp_err(start_err);
}

bool startup_pet_retry_service_take_due(void) {
    taskENTER_CRITICAL(&s_lock);
    const bool due = s_due;
    s_due = false;
    taskEXIT_CRITICAL(&s_lock);
    return due;
}

static device_status_t close_admission(uint32_t timeout_ms, bool terminal) {
    if (timeout_ms == 0 || !s_timer_mutex) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (xSemaphoreTake(s_timer_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    if (!s_initialized || s_system_sleep_preparing || (!terminal && s_stopped)) {
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_timer_mutex);
        return DEVICE_STATUS_BUSY;
    }
    if (terminal && s_stopped) {
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_timer_mutex);
        return DEVICE_STATUS_OK;
    }
    const bool was_stopped = s_stopped;
    s_system_sleep_preparing = !terminal;
    if (terminal) s_stopped = true;
    s_callback_admission_open = false;
    s_due = false;
    const esp_timer_handle_t timer = s_timer;
    const bool already_drained = s_callbacks_inflight == 0;
    taskEXIT_CRITICAL(&s_lock);

    if (timer) {
        const esp_err_t stop_err = esp_timer_stop(timer);
        if (stop_err != ESP_OK && stop_err != ESP_ERR_INVALID_STATE) {
            taskENTER_CRITICAL(&s_lock);
            s_system_sleep_preparing = false;
            s_stopped = was_stopped;
            s_callback_admission_open = !was_stopped;
            taskEXIT_CRITICAL(&s_lock);
            xSemaphoreGive(s_timer_mutex);
            return status_from_esp_err(stop_err);
        }
    }
    if (!already_drained &&
        xSemaphoreTake(s_callback_drained, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        /* The callback admitted before the close remains live. Keep admission
         * shut even though this caller timed out: rollback must decide when it
         * is safe to reopen rather than allowing an untracked retry. */
        xSemaphoreGive(s_timer_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    xSemaphoreGive(s_timer_mutex);
    return DEVICE_STATUS_OK;
}

device_status_t startup_pet_retry_service_stop(uint32_t timeout_ms) {
    return close_admission(timeout_ms, true);
}

device_status_t startup_pet_retry_service_prepare_system_sleep(uint32_t timeout_ms) {
    return close_admission(timeout_ms, false);
}

void startup_pet_retry_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lock);
    if (s_system_sleep_preparing) {
        s_system_sleep_preparing = false;
        s_callback_admission_open = true;
    }
    taskEXIT_CRITICAL(&s_lock);
}
