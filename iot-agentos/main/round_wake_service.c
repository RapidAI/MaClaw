#include "round_wake_service.h"

#include "round_audio_service.h"

#include "esp_log.h"
#include "esp_heap_caps.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#include "esp_timer.h"
#include "model_path.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define ROUND_WAKE_COMMAND_ID 1
#define ROUND_WAKE_LABEL "码卡龙"
#define ROUND_WAKE_DETECTION_THRESHOLD 0.24f
#define ROUND_WAKE_COOLDOWN_US (2LL * 1000 * 1000)
#define ROUND_WAKE_DIAGNOSTIC_INTERVAL_US (2LL * 1000 * 1000)

static const char *const s_round_wake_phonetics[] = {
    "ma ka long",
    "ma ga long",
    "ma ke long",
};

static const char *TAG = "round_wake";

typedef struct {
    TaskHandle_t recognizer_task;
    TaskHandle_t dispatcher_task;
    bool recognizer_starting;
    bool recognizer_ready;
    bool recognizer_paused;
    bool recognizer_pause_acknowledged;
    bool recognizer_stop_requested;
    bool callback_pending;
    bool dispatcher_starting;
    bool callback_cancel_requested;
    round_wake_service_callback_t callback;
    void *callback_arg;
} round_wake_service_state_t;

static round_wake_service_state_t s_round_wake;
static portMUX_TYPE s_round_wake_lock = portMUX_INITIALIZER_UNLOCKED;

static void round_wake_service_recognizer_task(void *arg);
static void round_wake_service_dispatcher_task(void *arg);
static void round_wake_service_recognizer_wait_for_registration(void);
static void round_wake_service_recognizer_mark_ready(void);
static bool round_wake_service_recognizer_should_stop(void);
static bool round_wake_service_recognizer_is_paused(void);
static void round_wake_service_recognizer_ack_pause(bool acknowledged);
static void round_wake_service_recognizer_finish(void);
static esp_err_t round_wake_service_queue_dispatch(void);
static void round_wake_service_dispatcher_wait_for_registration(void);
static bool round_wake_service_dispatcher_wait_for_recognizer_exit(uint32_t timeout_ms);
static bool round_wake_service_dispatcher_take_callback(round_wake_service_callback_t *callback,
                                                         void **callback_arg);
static void round_wake_service_dispatcher_finish(void);

static bool round_wake_service_active_locked(void) {
    return s_round_wake.recognizer_task || s_round_wake.recognizer_starting ||
           s_round_wake.callback_pending || s_round_wake.dispatcher_task ||
           s_round_wake.dispatcher_starting;
}

static bool round_wake_service_recognizer_active_locked(void) {
    return s_round_wake.recognizer_task || s_round_wake.recognizer_starting;
}

static esp_err_t round_wake_service_wait_for_ready(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        bool ready;
        bool recognizer_active;
        bool callback_active;
        taskENTER_CRITICAL(&s_round_wake_lock);
        ready = s_round_wake.recognizer_ready;
        recognizer_active = round_wake_service_recognizer_active_locked();
        callback_active = s_round_wake.callback_pending || s_round_wake.dispatcher_task ||
                          s_round_wake.dispatcher_starting;
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (ready) return ESP_OK;
        if (!recognizer_active) return callback_active ? ESP_ERR_INVALID_STATE : ESP_FAIL;
        if (xTaskGetTickCount() - started >= budget) return ESP_ERR_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(25));
    }
}

esp_err_t round_wake_service_start(round_wake_service_callback_t callback,
                                   void *callback_arg,
                                   uint32_t ready_timeout_ms) {
    if (!callback || ready_timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    taskENTER_CRITICAL(&s_round_wake_lock);
    const bool active = round_wake_service_active_locked();
    taskEXIT_CRITICAL(&s_round_wake_lock);
    if (active) return round_wake_service_wait_for_ready(ready_timeout_ms);

    taskENTER_CRITICAL(&s_round_wake_lock);
    /* A concurrent starter may have claimed the generation after the first
     * snapshot.  It observes the same explicit ready state rather than
     * spawning a second recognizer. */
    if (round_wake_service_active_locked()) {
        taskEXIT_CRITICAL(&s_round_wake_lock);
        return round_wake_service_wait_for_ready(ready_timeout_ms);
    }
    s_round_wake.recognizer_starting = true;
    s_round_wake.recognizer_ready = false;
    s_round_wake.recognizer_paused = false;
    s_round_wake.recognizer_pause_acknowledged = false;
    s_round_wake.recognizer_stop_requested = false;
    s_round_wake.callback_cancel_requested = false;
    s_round_wake.callback = callback;
    s_round_wake.callback_arg = callback_arg;
    taskEXIT_CRITICAL(&s_round_wake_lock);

    TaskHandle_t task = NULL;
    const BaseType_t created = xTaskCreatePinnedToCore(
        round_wake_service_recognizer_task, "maclaw_offline_wake", 10240, NULL,
        4, &task, 1);
    taskENTER_CRITICAL(&s_round_wake_lock);
    if (created != pdPASS) {
        s_round_wake.recognizer_starting = false;
        s_round_wake.callback = NULL;
        s_round_wake.callback_arg = NULL;
        taskEXIT_CRITICAL(&s_round_wake_lock);
        return ESP_ERR_NO_MEM;
    }
    s_round_wake.recognizer_task = task;
    s_round_wake.recognizer_starting = false;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return round_wake_service_wait_for_ready(ready_timeout_ms);
}

esp_err_t round_wake_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    taskENTER_CRITICAL(&s_round_wake_lock);
    if (!round_wake_service_active_locked()) {
        taskEXIT_CRITICAL(&s_round_wake_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_round_wake.recognizer_paused = true;
    s_round_wake.recognizer_stop_requested = true;
    s_round_wake.callback_cancel_requested = true;
    taskEXIT_CRITICAL(&s_round_wake_lock);

    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_round_wake_lock);
        const bool active = round_wake_service_active_locked();
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (!active) break;
        const TickType_t elapsed = xTaskGetTickCount() - started;
        if (elapsed >= budget) return ESP_ERR_TIMEOUT;
        TickType_t delay = budget - elapsed;
        const TickType_t polling = pdMS_TO_TICKS(25);
        if (delay > polling) delay = polling;
        vTaskDelay(delay == 0 ? 1 : delay);
    }
    taskENTER_CRITICAL(&s_round_wake_lock);
    s_round_wake.callback = NULL;
    s_round_wake.callback_arg = NULL;
    s_round_wake.callback_cancel_requested = false;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return ESP_OK;
}

void round_wake_service_set_paused(bool paused) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    s_round_wake.recognizer_paused = paused;
    if (!paused) s_round_wake.recognizer_pause_acknowledged = false;
    taskEXIT_CRITICAL(&s_round_wake_lock);
}

bool round_wake_service_wait_for_pause_ack(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_round_wake_lock);
        const bool recognizer_active = round_wake_service_recognizer_active_locked();
        const bool acknowledged = s_round_wake.recognizer_pause_acknowledged;
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (!recognizer_active || acknowledged) return true;
        if (xTaskGetTickCount() - started >= budget) return false;
        vTaskDelay(pdMS_TO_TICKS(5));
    }
}

static void round_wake_service_recognizer_wait_for_registration(void) {
    for (;;) {
        taskENTER_CRITICAL(&s_round_wake_lock);
        const bool starting = s_round_wake.recognizer_starting;
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (!starting) return;
        vTaskDelay(1);
    }
}

static void round_wake_service_recognizer_mark_ready(void) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    s_round_wake.recognizer_ready = true;
    taskEXIT_CRITICAL(&s_round_wake_lock);
}

static bool round_wake_service_recognizer_should_stop(void) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    const bool stop = s_round_wake.recognizer_stop_requested;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return stop;
}

static bool round_wake_service_recognizer_is_paused(void) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    const bool paused = s_round_wake.recognizer_paused;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return paused;
}

static void round_wake_service_recognizer_ack_pause(bool acknowledged) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    s_round_wake.recognizer_pause_acknowledged = acknowledged;
    taskEXIT_CRITICAL(&s_round_wake_lock);
}

static void round_wake_service_recognizer_finish(void) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    s_round_wake.recognizer_stop_requested = false;
    s_round_wake.recognizer_paused = false;
    s_round_wake.recognizer_pause_acknowledged = false;
    s_round_wake.recognizer_ready = false;
    s_round_wake.recognizer_task = NULL;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    vTaskDelete(NULL);
}

static esp_err_t round_wake_service_queue_dispatch(void) {
    taskENTER_CRITICAL(&s_round_wake_lock);
    const bool dispatch_active = s_round_wake.callback_pending ||
                                 s_round_wake.dispatcher_task ||
                                 s_round_wake.dispatcher_starting;
    if (!dispatch_active) {
        s_round_wake.callback_pending = true;
        s_round_wake.dispatcher_starting = true;
        s_round_wake.recognizer_stop_requested = true;
    }
    taskEXIT_CRITICAL(&s_round_wake_lock);
    if (dispatch_active) return ESP_ERR_INVALID_STATE;

    TaskHandle_t dispatcher = NULL;
    const BaseType_t created = xTaskCreateWithCaps(
        round_wake_service_dispatcher_task, "maclaw_wake_dispatch", 3072, NULL,
        5, &dispatcher, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_round_wake_lock);
    if (created == pdPASS) {
        s_round_wake.dispatcher_task = dispatcher;
    } else {
        s_round_wake.callback_pending = false;
    }
    s_round_wake.dispatcher_starting = false;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return created == pdPASS ? ESP_OK : ESP_ERR_NO_MEM;
}

static void round_wake_service_dispatcher_wait_for_registration(void) {
    for (;;) {
        taskENTER_CRITICAL(&s_round_wake_lock);
        const bool starting = s_round_wake.dispatcher_starting;
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (!starting) return;
        vTaskDelay(1);
    }
}

static bool round_wake_service_dispatcher_wait_for_recognizer_exit(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_round_wake_lock);
        const bool active = round_wake_service_recognizer_active_locked();
        taskEXIT_CRITICAL(&s_round_wake_lock);
        if (!active) return true;
        if (xTaskGetTickCount() - started >= budget) return false;
        vTaskDelay(pdMS_TO_TICKS(25));
    }
}

static bool round_wake_service_dispatcher_take_callback(round_wake_service_callback_t *callback,
                                                         void **callback_arg) {
    if (callback) *callback = NULL;
    if (callback_arg) *callback_arg = NULL;
    taskENTER_CRITICAL(&s_round_wake_lock);
    const bool invoke = !round_wake_service_recognizer_active_locked() &&
                        s_round_wake.callback_pending &&
                        !s_round_wake.callback_cancel_requested;
    if (invoke) {
        if (callback) *callback = s_round_wake.callback;
        if (callback_arg) *callback_arg = s_round_wake.callback_arg;
    }
    s_round_wake.callback_pending = false;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    return invoke;
}

static void round_wake_service_dispatcher_finish(void) {
    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_round_wake_lock);
    if (s_round_wake.dispatcher_task == self) s_round_wake.dispatcher_task = NULL;
    taskEXIT_CRITICAL(&s_round_wake_lock);
    vTaskDeleteWithCaps(NULL);
}

/* A deferred business callback runs only after the recognizer releases its
 * ESP-SR model and Audio-HAL capture lease.  Keeping the task entry here makes
 * that ordering a Wake-service invariant rather than a convention duplicated
 * in every round renderer. */
static void round_wake_service_dispatcher_task(void *arg) {
    (void)arg;
    round_wake_service_dispatcher_wait_for_registration();
    const bool recognizer_released =
        round_wake_service_dispatcher_wait_for_recognizer_exit(6000);
    round_wake_service_callback_t callback = NULL;
    void *callback_arg = NULL;
    const bool callback_available = round_wake_service_dispatcher_take_callback(
        &callback, &callback_arg);
    if (recognizer_released && callback_available && callback) {
        ESP_LOGI(TAG, "offline wake recognizer released; dispatching foreground callback");
        callback(callback_arg);
    } else if (!recognizer_released) {
        ESP_LOGW(TAG, "offline wake callback skipped: recognizer cleanup timed out");
    }
    round_wake_service_dispatcher_finish();
}

static void round_wake_service_recognizer_task(void *arg) {
    (void)arg;
    round_wake_service_recognizer_wait_for_registration();
    srmodel_list_t *models = esp_srmodel_init("model");
    if (!models) {
        ESP_LOGE(TAG, "offline wake disabled: cannot load ESP-SR model partition");
        goto finish;
    }

    char *model_name = esp_srmodel_filter(models, ESP_MN_PREFIX, ESP_MN_CHINESE);
    if (!model_name) {
        ESP_LOGE(TAG, "offline wake disabled: Chinese MultiNet model not found");
        esp_srmodel_deinit(models);
        goto finish;
    }
    esp_mn_iface_t *multinet = esp_mn_handle_from_name(model_name);
    if (!multinet) {
        ESP_LOGE(TAG, "offline wake disabled: unsupported model %s", model_name);
        esp_srmodel_deinit(models);
        goto finish;
    }
    model_iface_data_t *model_data = multinet->create(model_name, 4000);
    if (!model_data) {
        ESP_LOGE(TAG, "offline wake disabled: cannot create model %s", model_name);
        esp_srmodel_deinit(models);
        goto finish;
    }
    esp_err_t command_err = esp_mn_commands_alloc(multinet, model_data);
    for (size_t i = 0;
         command_err == ESP_OK && i < sizeof(s_round_wake_phonetics) / sizeof(s_round_wake_phonetics[0]);
         ++i) {
        command_err = esp_mn_commands_add(ROUND_WAKE_COMMAND_ID, s_round_wake_phonetics[i]);
    }
    esp_mn_error_t *command_errors = command_err == ESP_OK ? esp_mn_commands_update() : NULL;
    if (command_err != ESP_OK || command_errors != NULL) {
        ESP_LOGE(TAG, "offline wake disabled: word '%s' is not accepted (err=%s, rejected=%d)",
                 ROUND_WAKE_LABEL, esp_err_to_name(command_err),
                 command_errors ? command_errors->num : 0);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    if (multinet->set_det_threshold) {
        const int threshold_err = multinet->set_det_threshold(
            model_data, ROUND_WAKE_DETECTION_THRESHOLD);
        if (threshold_err != 0) {
            ESP_LOGW(TAG, "offline wake threshold %.2f was not applied: %d",
                     (double)ROUND_WAKE_DETECTION_THRESHOLD, threshold_err);
        }
    }
    const int chunk_samples = multinet->get_samp_chunksize(model_data);
    const int sample_rate = multinet->get_samp_rate(model_data);
    if (chunk_samples <= 0 || sample_rate != (int)round_audio_service_sample_rate()) {
        ESP_LOGE(TAG, "offline wake disabled: model audio format is %d Hz / %d samples",
                 sample_rate, chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    round_audio_wake_capture_t capture;
    if (round_audio_service_wake_capture_begin((size_t)chunk_samples, &capture) != ESP_OK) {
        ESP_LOGE(TAG, "offline wake disabled: no memory for %d-sample audio buffers",
                 chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    round_wake_service_recognizer_mark_ready();
    ESP_LOGI(TAG,
             "offline wake listening: model=%s phrase='%s' variants=%u threshold=%.2f rate=%d chunk=%d",
             model_name, ROUND_WAKE_LABEL,
             (unsigned)(sizeof(s_round_wake_phonetics) / sizeof(s_round_wake_phonetics[0])),
             (double)ROUND_WAKE_DETECTION_THRESHOLD, sample_rate, chunk_samples);
    multinet->print_active_speech_commands(model_data);
    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    int64_t last_audio_diagnostic_us = 0;
    while (true) {
        if (round_wake_service_recognizer_should_stop()) break;
        if (round_wake_service_recognizer_is_paused()) {
            if (!model_was_paused) {
                multinet->clean(model_data);
                model_was_paused = true;
            }
            round_wake_service_recognizer_ack_pause(true);
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        model_was_paused = false;
        round_wake_service_recognizer_ack_pause(false);
        round_audio_wake_pcm_stats_t wake_stats;
        const esp_err_t read_err = round_audio_service_wake_capture_read(
            &capture, 250, &wake_stats);
        if (read_err != ESP_OK) {
            if (read_err != ESP_ERR_TIMEOUT) {
                ESP_LOGW(TAG, "offline wake microphone read failed: %s",
                         esp_err_to_name(read_err));
            }
            continue;
        }
        const int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us - last_audio_diagnostic_us >= ROUND_WAKE_DIAGNOSTIC_INTERVAL_US) {
            last_audio_diagnostic_us = diagnostic_now_us;
            ESP_LOGI(TAG, "offline wake mic: peak=%ld rms=%lu bad=%u gain=%.2f",
                     (long)wake_stats.input_peak, (unsigned long)wake_stats.rms,
                     (unsigned)wake_stats.invalid_samples,
                     (double)wake_stats.gain_q8 / 256.0);
        }
        const esp_mn_state_t state = multinet->detect(model_data, capture.mono);
        /* MultiNet is compute-heavy. Yield once per inference chunk so IDLE1
         * can feed the watchdog while retaining the Audio-HAL cadence. */
        vTaskDelay(1);
        if (state == ESP_MN_STATE_TIMEOUT) {
            multinet->clean(model_data);
            continue;
        }
        if (state != ESP_MN_STATE_DETECTED) continue;
        esp_mn_results_t *result = multinet->get_results(model_data);
        if (!result || result->num == 0 || result->command_id[0] != ROUND_WAKE_COMMAND_ID) {
            continue;
        }
        const int64_t now_us = esp_timer_get_time();
        if (now_us - last_detection_us >= ROUND_WAKE_COOLDOWN_US) {
            last_detection_us = now_us;
            ESP_LOGI(TAG,
                     "offline wake word detected: %s phrase=%d text='%s' raw='%s' (prob=%.3f)",
                     ROUND_WAKE_LABEL, result->phrase_id[0], result->string,
                     result->raw_string, (double)result->prob[0]);
            multinet->clean(model_data);
            if (round_wake_service_queue_dispatch() != ESP_OK) {
                ESP_LOGE(TAG, "cannot queue offline wake callback");
            }
        } else {
            multinet->clean(model_data);
        }
    }

    round_audio_service_wake_capture_end(&capture);
    multinet->destroy(model_data);
    esp_srmodel_deinit(models);
    ESP_LOGI(TAG, "offline wake stopped and model memory released");

finish:
    round_wake_service_recognizer_finish();
}
