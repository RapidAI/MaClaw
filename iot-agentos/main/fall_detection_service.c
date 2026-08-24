#include "fall_detection_service.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "fall_detection_classifier.h"
#include "persistence_service.h"

/* Raw samples arrive at 125 Hz on the current COM6 adapter.  Sampling the
 * HAL at 10 Hz keeps shared-I2C traffic bounded without classifying a single
 * noisy sample as a fall.  These are deliberately conservative engineering
 * defaults, not medical thresholds; field calibration remains required. */
#define FALL_SAMPLE_PERIOD_MS 100u
#define FALL_CONFIRMATION_WINDOW_US 15000000LL
#define FALL_DETECTION_NAMESPACE "fall_detect"
#define FALL_DETECTION_STORE_KEY "config"
#define FALL_DETECTION_STORE_MAGIC 0x46444331u /* FDC1 */
#define FALL_DETECTION_STORE_VERSION 2u
#define FALL_DETECTION_REPLAY_COUNT 4u
#define FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY 64u
#define FALL_DETECTION_ERROR_CAPACITY 112u

static const char *TAG = "fall_detection";

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t revision;
} fall_detection_store_v1_t;

typedef struct {
    char key[FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY];
    int32_t status;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t configuration_revision;
    char detail[FALL_DETECTION_ERROR_CAPACITY];
} fall_detection_replay_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t revision;
    uint32_t replay_next;
    fall_detection_replay_t replay[FALL_DETECTION_REPLAY_COUNT];
} fall_detection_store_t;

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_available;
static bool s_enabled;
static uint32_t s_configuration_revision;
static fall_detection_state_t s_state = FALL_DETECTION_STATE_UNAVAILABLE;
static uint32_t s_suspected_count;
static uint64_t s_confirmation_deadline_us;
/* Changes whenever a candidate window becomes invalid.  Callbacks run outside
 * the service lock because UI/display work can block, so this generation
 * prevents a disable/re-enable request from presenting a stale candidate. */
static uint32_t s_event_generation;
static fall_detection_callback_t s_callback;
static void *s_callback_context;
static device_power_lease_t s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_stopped;
/* A retained worker reaches this binary safe-point before future physical
 * sleep.  It remains parked rather than being destroyed/recreated, so ABORT
 * cannot lose a monitoring generation because task creation fails. */
static SemaphoreHandle_t s_system_sleep_quiesced;
static volatile bool s_stop_requested;
static volatile bool s_system_sleep_preparing;
static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
/* Only one init/deinit transaction may create, join or reclaim the optional
 * sensor worker.  Public tool admission alone cannot serialize two rollback
 * callers that both observe the same stopped semaphore. */
static SemaphoreHandle_t s_deinit_mutex;
static StaticSemaphore_t s_deinit_mutex_storage;
static uint32_t s_tool_admissions;
/* Physical touch/button cancellation is not a Gateway tool transaction, but
 * it can release a Power lease after leaving s_lock.  Count it separately so
 * lifecycle never advances to Power teardown while that release is in flight.
 */
static uint32_t s_user_action_admissions;
/* Application presentation runs synchronously on the classifier task, yet it
 * is an explicit lifecycle borrower: a client callback admitted just before
 * rollback must finish before the service can release the Power domain. */
static uint32_t s_callback_admissions;
/* A classifier iteration can obtain a Motion sample, acquire/release a Power
 * lease and invoke presentation. Treat it as an explicit PREPARE borrower so
 * the sleep marker cannot be crossed between the loop's first check and those
 * side effects. */
static uint32_t s_system_sleep_evaluations;
/* Serialises the read/check/write replay transaction.  Persistence Service
 * serialises individual NVS calls; it cannot make a load followed by save
 * atomic relative to another Gateway retry. */
static SemaphoreHandle_t s_mutation_mutex;

static bool admit_tool(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stop_requested &&
        !__atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE)) {
        ++s_tool_admissions;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void release_tool(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_tool_admissions > 0) --s_tool_admissions;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static bool admit_user_action(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stop_requested &&
        !__atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE)) {
        ++s_user_action_admissions;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void release_user_action(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_user_action_admissions > 0) --s_user_action_admissions;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static bool admit_callback(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (!s_stop_requested &&
        !__atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE)) {
        ++s_callback_admissions;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void release_callback(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_callback_admissions > 0) --s_callback_admissions;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static bool begin_system_sleep_evaluation(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool admitted = s_initialized && !s_stop_requested &&
                          !__atomic_load_n(&s_system_sleep_preparing,
                                           __ATOMIC_ACQUIRE);
    if (admitted) ++s_system_sleep_evaluations;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void end_system_sleep_evaluation(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_system_sleep_evaluations) --s_system_sleep_evaluations;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

/* A direct task notification lets PREPARE bring the sampler to its next
 * admission check immediately.  A plain vTaskDelay would leave the Power
 * transaction waiting for up to one sampling period even though there is no
 * further Motion work to perform.  Notifications are otherwise harmless:
 * they merely start the next normal sampling iteration early. */
static void wait_for_next_sample_or_lifecycle_change(void) {
    (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(FALL_SAMPLE_PERIOD_MS));
}

/* A profile declaration says that this product family can expose Motion; it
 * does not prove that an optional sensor completed its private bootstrap.  Do
 * one value-only read before allocating the retained classifier worker so a
 * definitively absent adapter is represented exactly like a profile with no
 * Motion capability.  Transient BUSY/TIMEOUT/IO results remain retryable in
 * the worker and must not erase a hardware capability at boot. */
static bool motion_hal_is_definitively_unavailable_at_boot(void) {
    device_motion_sample_t sample = {0};
    const device_status_t status = device_motion_get_sample(&sample);
    if (status == DEVICE_STATUS_UNAVAILABLE || status == DEVICE_STATUS_NOT_FOUND) {
        ESP_LOGW(TAG, "motion HAL unavailable at startup: %d", (int)status);
        return true;
    }
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "motion HAL startup probe deferred to sampler: %d", (int)status);
    }
    return false;
}

static bool valid_store(const fall_detection_store_t *store) {
    return store && store->magic == FALL_DETECTION_STORE_MAGIC &&
           store->version == FALL_DETECTION_STORE_VERSION && store->enabled <= 1;
}

/* A missing namespace is a normal first boot.  Other read errors are treated
 * as a startup failure instead of silently converting user safety preferences
 * to an implicit default. */
static esp_err_t load_store(fall_detection_store_t *out_store, bool *out_migrated) {
    if (!out_store || !persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    if (out_migrated) *out_migrated = false;
    fall_detection_store_t store = {
        .magic = FALL_DETECTION_STORE_MAGIC,
        .version = FALL_DETECTION_STORE_VERSION,
        .enabled = 1,
        .revision = 1,
    };
    size_t size = 0;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                                  FALL_DETECTION_STORE_KEY,
                                                  NULL, &size));
    if (err == ESP_ERR_NOT_FOUND) {
        *out_store = store;
        return ESP_OK;
    }
    if (err != ESP_OK) return err;
    if (size == sizeof(fall_detection_store_v1_t)) {
        fall_detection_store_v1_t legacy = {0};
        err = device_status_to_platform_error(persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                            FALL_DETECTION_STORE_KEY, &legacy, &size));
        if (err != ESP_OK || legacy.magic != FALL_DETECTION_STORE_MAGIC ||
            legacy.version != 1u || legacy.enabled > 1) return ESP_ERR_INVALID_STATE;
        store = (fall_detection_store_t){
            .magic = FALL_DETECTION_STORE_MAGIC,
            .version = FALL_DETECTION_STORE_VERSION,
            .enabled = legacy.enabled,
            .revision = legacy.revision ? legacy.revision : 1u,
        };
        if (out_migrated) *out_migrated = true;
    } else if (size == sizeof(store)) {
        err = device_status_to_platform_error(persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                            FALL_DETECTION_STORE_KEY, &store, &size));
        if (err != ESP_OK || !valid_store(&store)) return ESP_ERR_INVALID_STATE;
    } else {
        return ESP_ERR_INVALID_SIZE;
    }
    if (store.revision == 0) store.revision = 1;
    *out_store = store;
    return ESP_OK;
}

static bool valid_idempotency_key(const char *key) {
    if (!key || !key[0]) return false;
    size_t length = strlen(key);
    if (length >= FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY) return false;
    for (size_t i = 0; i < length; ++i) {
        if ((unsigned char)key[i] > 0x7fu) return false;
    }
    return true;
}

static esp_err_t save_store(const fall_detection_store_t *store) {
    if (!valid_store(store)) return ESP_ERR_INVALID_ARG;
    return device_status_to_platform_error(persistence_service_write_blob(FALL_DETECTION_NAMESPACE,
                                          FALL_DETECTION_STORE_KEY,
                                          store, sizeof(*store)));
}

static void notify_if_current(fall_detection_event_t event, uint32_t generation) {
    /* The callback runs synchronously on the classifier worker, so deinit's
     * worker join is also its callback drain.  Sample the stop generation
     * under the lifecycle lock first; s_stop_requested is not protected by
     * the classifier-state lock. */
    if (!admit_callback()) return;
    taskENTER_CRITICAL(&s_lock);
    bool current = s_enabled && s_event_generation == generation;
    fall_detection_callback_t callback = s_callback;
    void *context = s_callback_context;
    taskEXIT_CRITICAL(&s_lock);
    /* Callback ownership is sampled atomically with the current-generation
     * test.  Deinit may clear the service fields after this point, but never
     * invalidates the application-owned callback context itself. */
    if (current && callback) callback(event, context);
    release_callback();
}

static bool begin_confirmation_window(void) {
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t lease_status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_FALL_DETECTION, &lease);
    if (lease_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "suspected fall ignored: cannot acquire presentation lease (%d)",
                 (int)lease_status);
        return false;
    }
    uint64_t deadline = (uint64_t)esp_timer_get_time() + FALL_CONFIRMATION_WINDOW_US;
    bool accepted = false;
    uint32_t event_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    /* Tool execution can disable monitoring between the task's last sample
     * and this commit.  Do not resurrect a prompt after the user's explicit
     * opt-out; release the lease obtained before taking the short lock. */
    if (!__atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE) && s_enabled &&
        s_state == FALL_DETECTION_STATE_MONITORING) {
        s_confirmation_lease = lease;
        s_state = FALL_DETECTION_STATE_PENDING_CONFIRMATION;
        s_confirmation_deadline_us = deadline;
        ++s_suspected_count;
        ++s_event_generation;
        if (s_event_generation == 0) ++s_event_generation;
        event_generation = s_event_generation;
        accepted = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!accepted) {
        device_power_lease_release(lease);
        return false;
    }
    ESP_LOGW(TAG, "suspected fall: local confirmation window opened");
    notify_if_current(FALL_DETECTION_EVENT_SUSPECTED, event_generation);
    return true;
}

static void finish_confirmation_window(void) {
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    uint32_t event_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    if (s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
        s_state = FALL_DETECTION_STATE_MONITORING;
        event_generation = s_event_generation;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (lease == DEVICE_POWER_LEASE_INVALID) return;
    device_power_lease_release(lease);
    ESP_LOGW(TAG, "suspected fall was not cancelled locally");
    notify_if_current(FALL_DETECTION_EVENT_CONFIRMED, event_generation);
}

static void fall_detection_task(void *arg) {
    (void)arg;
    fall_detection_classifier_t classifier = {0};
    fall_detection_classifier_reset(&classifier);
    unsigned consecutive_read_errors = 0;
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const bool stopping = s_stop_requested;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (stopping) break;
        if (!begin_system_sleep_evaluation()) {
            taskENTER_CRITICAL(&s_lifecycle_lock);
            const bool stopped_while_waiting = s_stop_requested;
            taskEXIT_CRITICAL(&s_lifecycle_lock);
            if (stopped_while_waiting) break;
        } else {
            uint64_t deadline_us;
            fall_detection_state_t state;
            taskENTER_CRITICAL(&s_lock);
            deadline_us = s_confirmation_deadline_us;
            state = s_state;
            bool enabled = s_enabled;
            taskEXIT_CRITICAL(&s_lock);
            uint64_t now_us = (uint64_t)esp_timer_get_time();
            if (!enabled) {
                /* Disable is a policy boundary, not merely a hidden prompt.  Drop
                 * the in-flight classifier evidence so later re-enable cannot
                 * combine motion from two different user-consent periods. */
                fall_detection_classifier_reset(&classifier);
            }
            if (state == FALL_DETECTION_STATE_PENDING_CONFIRMATION &&
                now_us >= deadline_us) {
                finish_confirmation_window();
            } else if (enabled && state == FALL_DETECTION_STATE_MONITORING) {
                device_motion_sample_t sample = {0};
                device_status_t status = device_motion_get_sample(&sample);
                if (status == DEVICE_STATUS_OK) {
                    consecutive_read_errors = 0;
                    if (fall_detection_classifier_observe(&classifier, &sample)) {
                        (void)begin_confirmation_window();
                    }
                } else if (++consecutive_read_errors == 10) {
                    ESP_LOGW(TAG, "motion HAL unavailable during monitoring: %d", (int)status);
                    consecutive_read_errors = 0;
                }
            }
            end_system_sleep_evaluation();
            wait_for_next_sample_or_lifecycle_change();
            continue;
        }
        taskENTER_CRITICAL(&s_lock);
        const bool system_sleep_preparing =
            __atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE);
        taskEXIT_CRITICAL(&s_lock);
        if (system_sleep_preparing) {
            /* The worker owns all classifier/confirmation side effects. It
             * must acknowledge the closed boundary before Power can continue,
             * then remain parked until ABORT or terminal deinit wakes it. */
            if (s_system_sleep_quiesced) xSemaphoreGive(s_system_sleep_quiesced);
            for (;;) {
                (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
                taskENTER_CRITICAL(&s_lifecycle_lock);
                const bool stop_after_wait = s_stop_requested;
                taskEXIT_CRITICAL(&s_lifecycle_lock);
                taskENTER_CRITICAL(&s_lock);
                const bool still_preparing =
                    __atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE);
                taskEXIT_CRITICAL(&s_lock);
                if (stop_after_wait) goto stopped;
                if (!still_preparing) break;
            }
            continue;
        }
        wait_for_next_sample_or_lifecycle_change();
    }
stopped:
    /* Publish worker exit before the final completion hand-off.  Deinit reads
     * this handle under the same lock, so it cannot notify a recycled task
     * handle after consuming s_stopped. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_task = NULL;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

device_status_t fall_detection_service_init(fall_detection_callback_t callback,
                                            void *context) {
    if (!callback || !persistence_service_is_initialized()) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_deinit_mutex) {
        s_deinit_mutex = xSemaphoreCreateMutexStatic(&s_deinit_mutex_storage);
    }
    if (!s_deinit_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    bool same_callback = s_callback == callback && s_callback_context == context;
    taskEXIT_CRITICAL(&s_lock);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool stopping = s_stop_requested;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (initialized) {
        xSemaphoreGive(s_deinit_mutex);
        if (stopping) return DEVICE_STATUS_BUSY;
        return same_callback ? (s_available ? DEVICE_STATUS_OK
                                            : DEVICE_STATUS_UNAVAILABLE)
                             : DEVICE_STATUS_BUSY;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stop_requested = false;
    s_system_sleep_evaluations = 0;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    /* A prior aborted transaction must never carry a closed admission marker
     * into a new optional-service generation, including a profile that has no
     * motion hardware and therefore creates no worker to clear it. */
    __atomic_store_n(&s_system_sleep_preparing, false, __ATOMIC_RELEASE);
    if (!device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR) ||
        motion_hal_is_definitively_unavailable_at_boot()) {
        taskENTER_CRITICAL(&s_lock);
        s_initialized = true;
        s_available = false;
        s_enabled = false;
        s_state = FALL_DETECTION_STATE_UNAVAILABLE;
        taskEXIT_CRITICAL(&s_lock);
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    /* Load before publishing service state so a task can never observe an
     * uninitialised preference. */
    s_mutation_mutex = xSemaphoreCreateMutex();
    s_stopped = xSemaphoreCreateBinary();
    s_system_sleep_quiesced = xSemaphoreCreateBinary();
    if (!s_mutation_mutex || !s_stopped || !s_system_sleep_quiesced) {
        if (s_mutation_mutex) vSemaphoreDelete(s_mutation_mutex);
        if (s_stopped) vSemaphoreDelete(s_stopped);
        if (s_system_sleep_quiesced) vSemaphoreDelete(s_system_sleep_quiesced);
        s_mutation_mutex = NULL;
        s_stopped = NULL;
        s_system_sleep_quiesced = NULL;
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    fall_detection_store_t store;
    bool store_migrated = false;
    esp_err_t store_err = load_store(&store, &store_migrated);
    if (store_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot load persisted configuration: %s", esp_err_to_name(store_err));
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        vSemaphoreDelete(s_system_sleep_quiesced);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        s_system_sleep_quiesced = NULL;
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (store_migrated && save_store(&store) != ESP_OK) {
        ESP_LOGE(TAG, "cannot persist migrated configuration schema");
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        vSemaphoreDelete(s_system_sleep_quiesced);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        s_system_sleep_quiesced = NULL;
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    taskENTER_CRITICAL(&s_lock);
    __atomic_store_n(&s_system_sleep_preparing, false, __ATOMIC_RELEASE);
    s_callback = callback;
    s_callback_context = context;
    /* Mark initialization in progress before task creation so a concurrent
     * caller cannot create a second classifier task.  It is not published as
     * available until FreeRTOS accepted the worker. */
    s_initialized = true;
    s_available = false;
    s_enabled = store.enabled != 0;
    s_configuration_revision = store.revision;
    s_state = s_enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    TaskHandle_t created_task = NULL;
    if (xTaskCreate(fall_detection_task, "maclaw_fall", 4096, NULL, 3,
                    &created_task) != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_initialized = false;
        s_available = false;
        s_enabled = false;
        s_state = FALL_DETECTION_STATE_UNAVAILABLE;
        s_callback = NULL;
        s_callback_context = NULL;
        taskEXIT_CRITICAL(&s_lock);
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        vSemaphoreDelete(s_system_sleep_quiesced);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        s_system_sleep_quiesced = NULL;
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    /* The worker owns no externally visible task identity until this
     * lifecycle publication.  PREPARE, ABORT and deinit all sample/clear the
     * handle under this same lock, so a late notification cannot target a
     * recycled FreeRTOS handle. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_task = created_task;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    taskENTER_CRITICAL(&s_lock);
    s_available = true;
    s_enabled = store.enabled != 0;
    s_state = s_enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    xSemaphoreGive(s_deinit_mutex);
    ESP_LOGI(TAG, "monitoring started: freefall/impact/orientation/stillness pipeline");
    return DEVICE_STATUS_OK;
}

device_status_t fall_detection_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;

    /* Startup rollback invokes every optional service boundary, including
     * profiles which failed before this service was ever initialized.  An
     * inactive service owns no task or Power/Persistence borrower, so it is
     * already quiescent; treating its absent retained mutex as a timeout
     * incorrectly blocks the rest of the fail-closed rollback chain. */
    taskENTER_CRITICAL(&s_lock);
    const bool initialized_at_entry = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized_at_entry) return DEVICE_STATUS_OK;

    /* Profiles without a motion sensor publish an initialized-but-unavailable
     * service so callers can distinguish an unsupported capability from an
     * initialization fault.  That generation creates no worker, semaphore,
     * mutation lock, lease or callback borrower.  It must therefore close
     * immediately during a parent rollback instead of spending the parent's
     * last few milliseconds waiting for the retained lifecycle mutex.  Do
     * not use this shortcut for the normal init publication window: that one
     * has already allocated the worker resources even while availability is
     * temporarily false. */
    taskENTER_CRITICAL(&s_lock);
    const bool unavailable_without_worker = !s_available &&
                                            s_initialized && s_stopped == NULL &&
                                            s_mutation_mutex == NULL;
    taskEXIT_CRITICAL(&s_lock);
    /* s_task is published, cleared and consumed only under the lifecycle
     * lock.  Keep this fast path on that ownership boundary too: sampling the
     * handle under s_lock could otherwise race a concurrent worker exit and
     * misclassify a live classifier as the no-IMU profile. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool no_classifier_worker = s_task == NULL;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (unavailable_without_worker && no_classifier_worker) {
        taskENTER_CRITICAL(&s_lock);
        s_initialized = false;
        s_enabled = false;
        s_state = FALL_DETECTION_STATE_UNAVAILABLE;
        s_callback = NULL;
        s_callback_context = NULL;
        taskEXIT_CRITICAL(&s_lock);
    }
    if (unavailable_without_worker && no_classifier_worker) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        s_stop_requested = false;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        __atomic_store_n(&s_system_sleep_preparing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_OK;
    }

    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    if (!s_deinit_mutex || xSemaphoreTake(s_deinit_mutex, budget) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    bool initialized = s_initialized;
    s_stop_requested = true;
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!initialized) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        s_stop_requested = false;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_OK;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    lease = s_confirmation_lease;
    s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
    s_confirmation_deadline_us = 0;
    s_enabled = false;
    s_available = false;
    s_state = FALL_DETECTION_STATE_UNAVAILABLE;
    ++s_event_generation;
    if (s_event_generation == 0) ++s_event_generation;
    taskEXIT_CRITICAL(&s_lock);
    device_power_lease_release(lease);
    if (task) {
        xTaskNotifyGive(task);
        TickType_t elapsed = xTaskGetTickCount() - started;
        TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
        if (!s_stopped || remaining == 0 ||
            xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }
    } else if (s_stopped) {
        /* A bounded prior stop may have timed out just before the worker
         * published completion.  Consume that handoff before deleting its
         * semaphore; otherwise the exiting task could give a freed object. */
        TickType_t elapsed = xTaskGetTickCount() - started;
        TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        uint32_t admissions = s_tool_admissions;
        admissions += s_user_action_admissions;
        admissions += s_callback_admissions;
        admissions += s_system_sleep_evaluations;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (admissions == 0) break;
        if ((xTaskGetTickCount() - started) >= budget) {
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    TickType_t elapsed = xTaskGetTickCount() - started;
    TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (s_mutation_mutex &&
        (remaining == 0 || xSemaphoreTake(s_mutation_mutex, remaining) != pdTRUE)) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    if (s_mutation_mutex) {
        xSemaphoreGive(s_mutation_mutex);
        vSemaphoreDelete(s_mutation_mutex);
        s_mutation_mutex = NULL;
    }
    if (s_stopped) {
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
    }
    if (s_system_sleep_quiesced) {
        vSemaphoreDelete(s_system_sleep_quiesced);
        s_system_sleep_quiesced = NULL;
    }
    taskENTER_CRITICAL(&s_lock);
    s_initialized = false;
    __atomic_store_n(&s_system_sleep_preparing, false, __ATOMIC_RELEASE);
    s_callback = NULL;
    s_callback_context = NULL;
    taskEXIT_CRITICAL(&s_lock);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stop_requested = false;
    s_system_sleep_evaluations = 0;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    xSemaphoreGive(s_deinit_mutex);
    ESP_LOGI(TAG, "monitoring stopped");
    return DEVICE_STATUS_OK;
}

device_status_t fall_detection_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    if (budget == 0) budget = 1;
    if (!s_deinit_mutex || xSemaphoreTake(s_deinit_mutex, budget) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }

    taskENTER_CRITICAL(&s_lock);
    const bool initialized = s_initialized;
    const bool available = s_available;
    const bool already_preparing =
        __atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE);
    SemaphoreHandle_t quiesced = s_system_sleep_quiesced;
    taskEXIT_CRITICAL(&s_lock);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!initialized) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    /* Profiles without Motion HAL intentionally publish this optional service
     * as initialized-but-unavailable. They own no worker or pending event, so
     * they are already quiescent and must not make every shared sleep request
     * fail merely because this hardware capability is absent. */
    if (!available) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_OK;
    }
    if (!task || !quiesced || already_preparing) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_BUSY;
    }

    /* Do not retain a stale acknowledgement from a timed-out old PREPARE.
     * Only the worker's acknowledgement after this admission marker proves
     * it has stopped sampling, advancing confirmation windows and presenting
     * fall events. */
    while (xSemaphoreTake(quiesced, 0) == pdTRUE) {
    }
    __atomic_store_n(&s_system_sleep_preparing, true, __ATOMIC_RELEASE);

    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t admissions = s_tool_admissions +
                                    s_user_action_admissions +
                                    s_callback_admissions +
                                    s_system_sleep_evaluations;
        const bool stopping = s_stop_requested;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (stopping) {
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_BUSY;
        }
        if (admissions == 0) break;
        const TickType_t elapsed = xTaskGetTickCount() - started;
        if (elapsed >= budget) {
            /* Leave the sampler admission closed. It may be returning from a
             * pre-fence sample/callback; Power ABORT is the only path that
             * can safely unpark it after all sibling rollback is complete. */
            xSemaphoreGive(s_deinit_mutex);
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(1);
    }

    /* The worker normally wakes every 100 ms, but notification makes a short
     * transaction independent of its sampling cadence. */
    xTaskNotifyGive(task);
    const TickType_t elapsed = xTaskGetTickCount() - started;
    const TickType_t remaining = elapsed >= budget ? 0 : budget - elapsed;
    if (remaining == 0 || xSemaphoreTake(quiesced, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_mutex);
        return DEVICE_STATUS_TIMEOUT;
    }
    xSemaphoreGive(s_deinit_mutex);
    return DEVICE_STATUS_OK;
}

void fall_detection_service_abort_system_sleep_prepare(void) {
    TaskHandle_t task = NULL;
    if (!s_deinit_mutex ||
        xSemaphoreTake(s_deinit_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return;
    }
    taskENTER_CRITICAL(&s_lock);
    const bool was_preparing =
        __atomic_load_n(&s_system_sleep_preparing, __ATOMIC_ACQUIRE);
    const bool should_unpark = was_preparing && s_initialized && s_available;
    __atomic_store_n(&s_system_sleep_preparing, false, __ATOMIC_RELEASE);
    taskEXIT_CRITICAL(&s_lock);
    if (should_unpark) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        task = s_task;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
    }
    if (task) xTaskNotifyGive(task);
    xSemaphoreGive(s_deinit_mutex);
}

bool fall_detection_service_is_available(void) {
    taskENTER_CRITICAL(&s_lock);
    bool available = s_available;
    taskEXIT_CRITICAL(&s_lock);
    return available;
}

bool fall_detection_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    return initialized;
}

bool fall_detection_service_get_snapshot(fall_detection_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_lock);
    out_snapshot->available = s_available;
    out_snapshot->enabled = s_enabled;
    out_snapshot->state = s_state;
    out_snapshot->suspected_count = s_suspected_count;
    out_snapshot->confirmation_deadline_us = s_confirmation_deadline_us;
    out_snapshot->configuration_revision = s_configuration_revision;
    taskEXIT_CRITICAL(&s_lock);
    /* Availability is an observed service state, not merely a profile claim:
     * an optional sensor may be absent even on a Motion-capable profile. */
    return true;
}

static cJSON *configuration_result_json(bool enabled, uint32_t revision, bool replayed) {
    cJSON *result = cJSON_CreateObject();
    if (!result || !cJSON_AddBoolToObject(result, "available", true) ||
        !cJSON_AddBoolToObject(result, "enabled", enabled) ||
        !cJSON_AddNumberToObject(result, "configurationRevision", revision) ||
        !cJSON_AddBoolToObject(result, "replayed", replayed)) {
        cJSON_Delete(result);
        return NULL;
    }
    return result;
}

static esp_err_t update_enabled(bool enabled, const char *idempotency_key,
                                cJSON **out_result, char *error,
                                size_t error_size) {
    if (out_result) *out_result = NULL;
    if (!valid_idempotency_key(idempotency_key)) {
        if (error && error_size) {
            strlcpy(error, "idempotencyKey must be 1..63 ASCII characters", error_size);
        }
        return ESP_ERR_INVALID_ARG;
    }
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    bool available = s_available;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized || !available) return ESP_ERR_NOT_SUPPORTED;
    if (!s_mutation_mutex ||
        xSemaphoreTake(s_mutation_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }

    fall_detection_store_t store;
    bool migrated = false;
    esp_err_t load_err = load_store(&store, &migrated);
    if (load_err != ESP_OK) {
        xSemaphoreGive(s_mutation_mutex);
        return load_err;
    }
    for (size_t i = 0; i < FALL_DETECTION_REPLAY_COUNT; ++i) {
        fall_detection_replay_t *replay = &store.replay[i];
        if (strcmp(replay->key, idempotency_key)) continue;
        if (replay->status != ESP_OK) {
            if (error && error_size) strlcpy(error, replay->detail, error_size);
            xSemaphoreGive(s_mutation_mutex);
            return (esp_err_t)replay->status;
        }
        cJSON *cached = configuration_result_json(replay->enabled != 0,
                                                  replay->configuration_revision, true);
        if (!cached) {
            xSemaphoreGive(s_mutation_mutex);
            return ESP_ERR_NO_MEM;
        }
        *out_result = cached;
        xSemaphoreGive(s_mutation_mutex);
        return ESP_OK;
    }

    /* The persisted store is the mutation truth.  Runtime state can lag it
     * during initialization or after a recoverable task failure, so calculate
     * both the revision and no-op behavior from the just-loaded record. */
    bool changed = (store.enabled != (enabled ? 1u : 0u));
    store.enabled = enabled ? 1 : 0;
    if (changed) store.revision = store.revision == UINT32_MAX ? 1u : store.revision + 1u;
    if (!store.revision) store.revision = 1u;

    /* Persist the successful mutation and its response atomically.  A no-op
     * still receives a replay entry: otherwise a lost ACK would make a retry
     * indistinguishable from a fresh command. */
    fall_detection_replay_t *replay =
        &store.replay[store.replay_next % FALL_DETECTION_REPLAY_COUNT];
    memset(replay, 0, sizeof(*replay));
    strlcpy(replay->key, idempotency_key, sizeof(replay->key));
    replay->status = ESP_OK;
    replay->enabled = store.enabled;
    replay->configuration_revision = store.revision;
    store.replay_next = (store.replay_next + 1u) % FALL_DETECTION_REPLAY_COUNT;

    /* Allocate the response before committing durable state.  An allocation
     * failure must not report a failed tool call after it has already changed
     * the user's monitoring preference. */
    cJSON *result = configuration_result_json(enabled, store.revision, false);
    if (!result) {
        xSemaphoreGive(s_mutation_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = save_store(&store);
    if (err != ESP_OK) {
        cJSON_Delete(result);
        xSemaphoreGive(s_mutation_mutex);
        return err;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    s_enabled = enabled;
    s_configuration_revision = store.revision;
    if (!enabled && s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
    }
    ++s_event_generation;
    if (s_event_generation == 0) ++s_event_generation;
    s_state = enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    device_power_lease_release(lease);
    ESP_LOGI(TAG, "monitoring %s (configuration revision=%lu)",
             enabled ? "enabled" : "disabled", (unsigned long)store.revision);
    *out_result = result;
    xSemaphoreGive(s_mutation_mutex);
    return ESP_OK;
}

static cJSON *status_json(void) {
    fall_detection_snapshot_t snapshot = {0};
    if (!fall_detection_service_get_snapshot(&snapshot)) return NULL;
    cJSON *result = cJSON_CreateObject();
    if (!result || !cJSON_AddBoolToObject(result, "available", snapshot.available) ||
        !cJSON_AddBoolToObject(result, "enabled", snapshot.enabled) ||
        !cJSON_AddNumberToObject(result, "state", snapshot.state) ||
        !cJSON_AddNumberToObject(result, "suspectedCount", snapshot.suspected_count) ||
        !cJSON_AddNumberToObject(result, "configurationRevision",
                                 snapshot.configuration_revision)) {
        cJSON_Delete(result);
        return NULL;
    }
    return result;
}

esp_err_t fall_detection_service_execute_tool(const char *name, cJSON *arguments,
                                              const char *idempotency_key,
                                              cJSON **out_result, char *error,
                                              size_t error_size) {
    if (out_result) *out_result = NULL;
    if (error && error_size) error[0] = '\0';
    if (!name || !out_result) return ESP_ERR_INVALID_ARG;
    if (!admit_tool()) return ESP_ERR_INVALID_STATE;
    if (!strcmp(name, "fall_detection_status")) {
        *out_result = status_json();
        esp_err_t result = *out_result ? ESP_OK : ESP_ERR_NO_MEM;
        release_tool();
        return result;
    }
    if (strcmp(name, "fall_detection_set")) {
        release_tool();
        return ESP_ERR_NOT_SUPPORTED;
    }
    if (!idempotency_key || !idempotency_key[0]) {
        if (error && error_size) strlcpy(error, "idempotencyKey is required", error_size);
        release_tool();
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *enabled = arguments ? cJSON_GetObjectItemCaseSensitive(arguments, "enabled") : NULL;
    if (!cJSON_IsBool(enabled)) {
        if (error && error_size) strlcpy(error, "enabled boolean is required", error_size);
        release_tool();
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *result = NULL;
    esp_err_t err = update_enabled(cJSON_IsTrue(enabled), idempotency_key, &result,
                                   error, error_size);
    if (err != ESP_OK) {
        if (error && error_size && !error[0]) {
            snprintf(error, error_size, "fall detection unavailable: %s", esp_err_to_name(err));
        }
        release_tool();
        return err;
    }
    *out_result = result;
    release_tool();
    return ESP_OK;
}

bool fall_detection_service_cancel_from_user(void) {
    if (!admit_user_action()) return false;
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    if (s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
        s_state = FALL_DETECTION_STATE_MONITORING;
        ++s_event_generation;
        if (s_event_generation == 0) ++s_event_generation;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (lease == DEVICE_POWER_LEASE_INVALID) {
        release_user_action();
        return false;
    }
    device_power_lease_release(lease);
    ESP_LOGI(TAG, "suspected fall cancelled by local user interaction");
    release_user_action();
    return true;
}
