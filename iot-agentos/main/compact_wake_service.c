#include "compact_wake_service.h"

#include "compact_audio_service.h"

#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_mn_models.h"
#include "esp_mn_speech_commands.h"
#include "esp_timer.h"
#include "model_path.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define COMPACT_WAKE_COMMAND_ID 1
#define COMPACT_WAKE_LABEL "ma ka long"
#define COMPACT_WAKE_COOLDOWN_US (2LL * 1000 * 1000)
#define COMPACT_WAKE_DIAGNOSTIC_INTERVAL_US (2LL * 1000 * 1000)

/* Phrase variants are recognizer vocabulary, not application policy.  The
 * application receives only the semantic confirmed-wake callback. */
static const char *const s_compact_wake_phonetics[] = {
    "ma ka long",
    "ma ga long",
    "ma ke long",
};

static const char *TAG = "compact_wake";

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
    compact_wake_service_callback_t callback;
    void *callback_arg;
} compact_wake_service_state_t;

static compact_wake_service_state_t s_compact_wake;
static portMUX_TYPE s_compact_wake_lock = portMUX_INITIALIZER_UNLOCKED;

static void compact_wake_service_recognizer_task(void *arg);
static void compact_wake_service_dispatcher_task(void *arg);

static bool compact_wake_service_active_locked(void) {
    return s_compact_wake.recognizer_task || s_compact_wake.recognizer_starting ||
           s_compact_wake.callback_pending || s_compact_wake.dispatcher_task ||
           s_compact_wake.dispatcher_starting;
}

static bool compact_wake_service_recognizer_active_locked(void) {
    return s_compact_wake.recognizer_task || s_compact_wake.recognizer_starting;
}

static esp_err_t compact_wake_service_wait_for_ready(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool ready = s_compact_wake.recognizer_ready;
        const bool active = compact_wake_service_active_locked();
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (ready) return ESP_OK;
        if (!active) return ESP_FAIL;
        if (xTaskGetTickCount() - started >= budget) return ESP_ERR_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(25));
    }
}

esp_err_t compact_wake_service_start(compact_wake_service_callback_t callback,
                                     void *callback_arg,
                                     uint32_t ready_timeout_ms) {
    if (!callback || ready_timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    taskENTER_CRITICAL(&s_compact_wake_lock);
    const bool active = compact_wake_service_active_locked();
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    if (active) return compact_wake_service_wait_for_ready(ready_timeout_ms);

    taskENTER_CRITICAL(&s_compact_wake_lock);
    if (compact_wake_service_active_locked()) {
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        return compact_wake_service_wait_for_ready(ready_timeout_ms);
    }
    s_compact_wake.recognizer_starting = true;
    s_compact_wake.recognizer_ready = false;
    s_compact_wake.recognizer_paused = false;
    s_compact_wake.recognizer_pause_acknowledged = false;
    s_compact_wake.recognizer_stop_requested = false;
    s_compact_wake.callback_pending = false;
    s_compact_wake.dispatcher_starting = false;
    s_compact_wake.callback_cancel_requested = false;
    s_compact_wake.callback = callback;
    s_compact_wake.callback_arg = callback_arg;
    taskEXIT_CRITICAL(&s_compact_wake_lock);

    TaskHandle_t task = NULL;
    /* Wake owns the recognizer runtime and its FreeRTOS lifetime.  The direct
     * I2S Audio HAL exposes only a bounded PCM capture contract, so adding a
     * new compact microphone topology cannot make task placement leak back
     * into the audio adapter or shared renderer.  Both current compact boards
     * qualify this worker on CPU1 with the same stack/priority budget. */
    const BaseType_t created = xTaskCreatePinnedToCore(
        compact_wake_service_recognizer_task, "maclaw_compact_wake", 10240,
        NULL, 4, &task, 1);
    taskENTER_CRITICAL(&s_compact_wake_lock);
    if (created != pdPASS) {
        s_compact_wake.recognizer_starting = false;
        s_compact_wake.callback = NULL;
        s_compact_wake.callback_arg = NULL;
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        return ESP_ERR_NO_MEM;
    }
    s_compact_wake.recognizer_task = task;
    s_compact_wake.recognizer_starting = false;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return compact_wake_service_wait_for_ready(ready_timeout_ms);
}

esp_err_t compact_wake_service_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    taskENTER_CRITICAL(&s_compact_wake_lock);
    if (!compact_wake_service_active_locked()) {
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_compact_wake.recognizer_paused = true;
    s_compact_wake.recognizer_stop_requested = true;
    s_compact_wake.callback_cancel_requested = true;
    taskEXIT_CRITICAL(&s_compact_wake_lock);

    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool active = compact_wake_service_active_locked();
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (!active) break;
        const TickType_t elapsed = xTaskGetTickCount() - started;
        if (elapsed >= budget) return ESP_ERR_TIMEOUT;
        TickType_t delay = budget - elapsed;
        const TickType_t polling = pdMS_TO_TICKS(25);
        if (delay > polling) delay = polling;
        vTaskDelay(delay == 0 ? 1 : delay);
    }
    taskENTER_CRITICAL(&s_compact_wake_lock);
    s_compact_wake.callback = NULL;
    s_compact_wake.callback_arg = NULL;
    s_compact_wake.callback_cancel_requested = false;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return ESP_OK;
}

void compact_wake_service_set_paused(bool paused) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    s_compact_wake.recognizer_paused = paused;
    if (!paused) s_compact_wake.recognizer_pause_acknowledged = false;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
}

bool compact_wake_service_wait_for_pause_ack(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool active = compact_wake_service_active_locked();
        const bool acknowledged = s_compact_wake.recognizer_pause_acknowledged;
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (!active || acknowledged) return true;
        if (xTaskGetTickCount() - started >= budget) return false;
        vTaskDelay(pdMS_TO_TICKS(5));
    }
}

static void compact_wake_service_recognizer_wait_for_registration(void) {
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool starting = s_compact_wake.recognizer_starting;
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (!starting) return;
        vTaskDelay(1);
    }
}

static void compact_wake_service_recognizer_mark_ready(void) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    s_compact_wake.recognizer_ready = true;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
}

static bool compact_wake_service_recognizer_should_stop(void) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    const bool stop = s_compact_wake.recognizer_stop_requested;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return stop;
}

static bool compact_wake_service_recognizer_is_paused(void) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    const bool paused = s_compact_wake.recognizer_paused;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return paused;
}

static void compact_wake_service_recognizer_ack_pause(bool acknowledged) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    s_compact_wake.recognizer_pause_acknowledged = acknowledged;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
}

static void compact_wake_service_recognizer_finish(void) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    s_compact_wake.recognizer_stop_requested = false;
    s_compact_wake.recognizer_paused = false;
    s_compact_wake.recognizer_pause_acknowledged = false;
    s_compact_wake.recognizer_ready = false;
    s_compact_wake.recognizer_task = NULL;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    vTaskDelete(NULL);
}

static esp_err_t compact_wake_service_queue_dispatch(void) {
    taskENTER_CRITICAL(&s_compact_wake_lock);
    const bool dispatch_active = s_compact_wake.callback_pending ||
                                 s_compact_wake.dispatcher_task ||
                                 s_compact_wake.dispatcher_starting;
    if (!dispatch_active) {
        s_compact_wake.callback_pending = true;
        s_compact_wake.dispatcher_starting = true;
        s_compact_wake.recognizer_stop_requested = true;
    }
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    if (dispatch_active) return ESP_ERR_INVALID_STATE;

    TaskHandle_t dispatcher = NULL;
    const BaseType_t created = xTaskCreateWithCaps(
        compact_wake_service_dispatcher_task, "maclaw_compact_wake_dispatch", 3072,
        NULL, 5, &dispatcher, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    taskENTER_CRITICAL(&s_compact_wake_lock);
    if (created == pdPASS) {
        s_compact_wake.dispatcher_task = dispatcher;
    } else {
        s_compact_wake.callback_pending = false;
        /* The recognizer will retire after this detection.  Its callback was
         * not dispatched, so the app restart supervisor must restore wake. */
        s_compact_wake.recognizer_stop_requested = true;
    }
    s_compact_wake.dispatcher_starting = false;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return created == pdPASS ? ESP_OK : ESP_ERR_NO_MEM;
}

static void compact_wake_service_dispatcher_wait_for_registration(void) {
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool starting = s_compact_wake.dispatcher_starting;
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (!starting) return;
        vTaskDelay(1);
    }
}

static bool compact_wake_service_dispatcher_wait_for_recognizer_exit(uint32_t timeout_ms) {
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_compact_wake_lock);
        const bool active = compact_wake_service_recognizer_active_locked();
        taskEXIT_CRITICAL(&s_compact_wake_lock);
        if (!active) return true;
        if (xTaskGetTickCount() - started >= budget) return false;
        vTaskDelay(pdMS_TO_TICKS(25));
    }
}

static bool compact_wake_service_dispatcher_take_callback(
    compact_wake_service_callback_t *callback, void **callback_arg) {
    if (callback) *callback = NULL;
    if (callback_arg) *callback_arg = NULL;
    taskENTER_CRITICAL(&s_compact_wake_lock);
    const bool invoke = !compact_wake_service_recognizer_active_locked() &&
                        s_compact_wake.callback_pending &&
                        !s_compact_wake.callback_cancel_requested;
    if (invoke) {
        if (callback) *callback = s_compact_wake.callback;
        if (callback_arg) *callback_arg = s_compact_wake.callback_arg;
    }
    s_compact_wake.callback_pending = false;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    return invoke;
}

static void compact_wake_service_dispatcher_finish(void) {
    const TaskHandle_t self = xTaskGetCurrentTaskHandle();
    taskENTER_CRITICAL(&s_compact_wake_lock);
    if (s_compact_wake.dispatcher_task == self) s_compact_wake.dispatcher_task = NULL;
    taskEXIT_CRITICAL(&s_compact_wake_lock);
    vTaskDeleteWithCaps(NULL);
}

/* Run the business callback only after the recognizer has released the
 * direct-I2S capture lease and model memory.  This guarantees the subsequent
 * command capture starts from a clean Audio-HAL generation on both compact
 * boards. */
static void compact_wake_service_dispatcher_task(void *arg) {
    (void)arg;
    compact_wake_service_dispatcher_wait_for_registration();
    const bool recognizer_released =
        compact_wake_service_dispatcher_wait_for_recognizer_exit(6000);
    compact_wake_service_callback_t callback = NULL;
    void *callback_arg = NULL;
    const bool callback_available = compact_wake_service_dispatcher_take_callback(
        &callback, &callback_arg);
    if (recognizer_released && callback_available && callback) {
        ESP_LOGI(TAG, "offline wake recognizer released; dispatching foreground callback");
        callback(callback_arg);
    } else if (!recognizer_released) {
        ESP_LOGW(TAG, "offline wake callback skipped: recognizer cleanup timed out");
    }
    compact_wake_service_dispatcher_finish();
}

/* The callback is dispatched only after all ESP-SR and Audio-HAL capture
 * resources have been released.  Starting a command capture from the old
 * renderer-owned detection loop could otherwise race the final wake I2S
 * transaction on direct-I2S profiles. */
static void compact_wake_service_recognizer_task(void *arg) {
    (void)arg;
    compact_wake_service_recognizer_wait_for_registration();
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
         command_err == ESP_OK && i < sizeof(s_compact_wake_phonetics) / sizeof(s_compact_wake_phonetics[0]);
         ++i) {
        command_err = esp_mn_commands_add(COMPACT_WAKE_COMMAND_ID, s_compact_wake_phonetics[i]);
    }
    esp_mn_error_t *command_errors = command_err == ESP_OK ? esp_mn_commands_update() : NULL;
    if (command_err != ESP_OK || command_errors != NULL) {
        ESP_LOGE(TAG, "offline wake disabled: word '%s' variants rejected (err=%s, rejected=%d)",
                 COMPACT_WAKE_LABEL, esp_err_to_name(command_err),
                 command_errors ? command_errors->num : 0);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    const compact_audio_calibration_t *calibration = compact_audio_service_calibration();
    if (!calibration) {
        ESP_LOGE(TAG, "offline wake disabled: audio calibration unavailable");
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    if (multinet->set_det_threshold) {
        const int threshold_err = multinet->set_det_threshold(
            model_data, calibration->wake_word_detection_threshold);
        if (threshold_err != 0) {
            ESP_LOGW(TAG, "offline wake threshold %.2f was not applied: %d",
                     (double)calibration->wake_word_detection_threshold, threshold_err);
        }
    }
    const int chunk_samples = multinet->get_samp_chunksize(model_data);
    const int sample_rate = multinet->get_samp_rate(model_data);
    if (chunk_samples <= 0 || sample_rate != (int)calibration->sample_rate) {
        ESP_LOGE(TAG, "offline wake disabled: model format is %d Hz / %d samples",
                 sample_rate, chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }
    compact_audio_wake_capture_t capture = {0};
    if (compact_audio_service_wake_capture_begin((size_t)chunk_samples, &capture) != ESP_OK) {
        ESP_LOGE(TAG, "offline wake disabled: no memory for %d-sample buffers", chunk_samples);
        multinet->destroy(model_data);
        esp_srmodel_deinit(models);
        goto finish;
    }

    compact_wake_service_recognizer_mark_ready();
    ESP_LOGI(TAG,
             "offline wake listening: model=%s phrase='%s' variants=%u threshold=%.2f rate=%d chunk=%d",
             model_name, COMPACT_WAKE_LABEL,
             (unsigned)(sizeof(s_compact_wake_phonetics) / sizeof(s_compact_wake_phonetics[0])),
             (double)calibration->wake_word_detection_threshold, sample_rate, chunk_samples);
    multinet->print_active_speech_commands(model_data);
    bool model_was_paused = false;
    int64_t last_detection_us = 0;
    int64_t last_audio_diagnostic_us = 0;
    while (!compact_wake_service_recognizer_should_stop()) {
        if (compact_wake_service_recognizer_is_paused()) {
            if (!model_was_paused) {
                multinet->clean(model_data);
                model_was_paused = true;
            }
            compact_wake_service_recognizer_ack_pause(true);
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        model_was_paused = false;
        compact_wake_service_recognizer_ack_pause(false);
        compact_audio_capture_stats_t stats = {0};
        esp_err_t read_err = compact_audio_service_wake_capture_read(&capture, 250, &stats);
        if (read_err != ESP_OK) {
            if (read_err != ESP_ERR_TIMEOUT) {
                ESP_LOGW(TAG, "offline wake microphone read failed: %s", esp_err_to_name(read_err));
            }
            continue;
        }
        const int64_t diagnostic_now_us = esp_timer_get_time();
        if (diagnostic_now_us - last_audio_diagnostic_us >= COMPACT_WAKE_DIAGNOSTIC_INTERVAL_US) {
            last_audio_diagnostic_us = diagnostic_now_us;
            ESP_LOGI(TAG, "offline wake mic: samples=%u input_peak=%ld peak=%ld level=%u gain=%.2f",
                     (unsigned)capture.frames, (long)stats.input_peak, (long)stats.peak,
                     (unsigned)stats.level,
                     (double)calibration->wake_word_gain_num / calibration->wake_word_gain_den);
        }
        const esp_mn_state_t state = multinet->detect(model_data, capture.mono);
        vTaskDelay(1);
        if (state == ESP_MN_STATE_TIMEOUT) {
            multinet->clean(model_data);
            continue;
        }
        if (state != ESP_MN_STATE_DETECTED) continue;
        esp_mn_results_t *result = multinet->get_results(model_data);
        if (!result || result->num == 0 || result->command_id[0] != COMPACT_WAKE_COMMAND_ID) {
            continue;
        }
        const int64_t now_us = esp_timer_get_time();
        if (now_us - last_detection_us < COMPACT_WAKE_COOLDOWN_US) {
            multinet->clean(model_data);
            continue;
        }
        last_detection_us = now_us;
        ESP_LOGI(TAG,
                 "offline wake word detected: %s phrase=%d text='%s' raw='%s' (prob=%.3f)",
                 COMPACT_WAKE_LABEL, result->phrase_id[0], result->string,
                 result->raw_string, (double)result->prob[0]);
        multinet->clean(model_data);
        if (compact_wake_service_queue_dispatch() != ESP_OK) {
            ESP_LOGE(TAG, "cannot queue offline wake callback");
        }
        break;
    }
    compact_audio_service_wake_capture_end(&capture);
    multinet->destroy(model_data);
    esp_srmodel_deinit(models);
    ESP_LOGI(TAG, "offline wake stopped and model memory released");
finish:
    compact_wake_service_recognizer_finish();
}
