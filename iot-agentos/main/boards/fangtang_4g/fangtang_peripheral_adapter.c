/* Fangtang-only battery and charge monitor implementation.
 *
 * This translation unit deliberately has no dependency on the shared compact
 * renderer.  It publishes only the normalized snapshot declared by the
 * private peripheral adapter contract, so profile ownership is real rather
 * than an implementation block appended to the renderer bridge.
 */

#include "fangtang_peripheral_adapter.h"

#include <limits.h>

#include "driver/gpio.h"
#include "esp_adc/adc_oneshot.h"
#include "esp_adc/adc_cali.h"
#include "esp_adc/adc_cali_scheme.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "sdkconfig.h"

static TaskHandle_t s_fangtang_power_task;
static SemaphoreHandle_t s_fangtang_power_task_stopped;
/* System Sleep retains the monitor task and its ADC/GPIO ownership, but parks
 * it at a verified no-sample boundary. Once PREPARE closes sampling admission,
 * including an ACK timeout, only the owning Power transaction's ABORT reuses
 * that same generation; this is intentionally separate from permanent
 * startup-rollback stop. */
static SemaphoreHandle_t s_fangtang_power_task_system_sleep_quiesced;
static bool s_fangtang_power_task_system_sleep_preparing;
static uint32_t s_fangtang_power_task_samples_inflight;
static portMUX_TYPE s_fangtang_power_task_lock = portMUX_INITIALIZER_UNLOCKED;
/* The lifecycle owner, not the task itself, clears this handle after it has
 * consumed the completion semaphore. This preserves a safe identity across
 * a timed-out join. */
static bool s_fangtang_power_task_stop_requested;
static adc_oneshot_unit_handle_t s_fangtang_battery_adc;
static adc_cali_handle_t s_fangtang_battery_cali;
static unsigned s_fangtang_battery_level;
static bool s_fangtang_battery_level_valid;
static bool s_fangtang_battery_charging;
static portMUX_TYPE s_fangtang_power_status_lock = portMUX_INITIALIZER_UNLOCKED;

#define FANGTANG_CHARGE_STATUS_GPIO ((gpio_num_t)CONFIG_MACLAW_FANGTANG_CHARGE_STATUS_GPIO)

static void fangtang_release_adc_resources(void) {
    if (s_fangtang_battery_cali) {
        (void)adc_cali_delete_scheme_curve_fitting(s_fangtang_battery_cali);
        s_fangtang_battery_cali = NULL;
    }
    if (s_fangtang_battery_adc) {
        (void)adc_oneshot_del_unit(s_fangtang_battery_adc);
        s_fangtang_battery_adc = NULL;
    }
}

static void fangtang_invalidate_telemetry(void) {
    taskENTER_CRITICAL(&s_fangtang_power_status_lock);
    s_fangtang_battery_level_valid = false;
    s_fangtang_battery_charging = false;
    taskEXIT_CRITICAL(&s_fangtang_power_status_lock);
}

static unsigned fangtang_battery_percent_from_mv(int battery_mv) {
    static const struct {
        int mv;
        unsigned percent;
    } levels[] = {
        {3300, 0}, {3500, 5}, {3650, 15}, {3750, 30},
        {3850, 50}, {3950, 70}, {4100, 85}, {4200, 100},
    };
    if (battery_mv <= levels[0].mv) return levels[0].percent;
    for (size_t i = 1; i < sizeof(levels) / sizeof(levels[0]); ++i) {
        if (battery_mv <= levels[i].mv) {
            const int range = levels[i].mv - levels[i - 1].mv;
            const int offset = battery_mv - levels[i - 1].mv;
            return levels[i - 1].percent +
                   (unsigned)((offset * (int)(levels[i].percent - levels[i - 1].percent)) /
                              range);
        }
    }
    return 100;
}

static bool fangtang_scale_adc_mv(int adc_mv, int *out_battery_mv) {
    if (!out_battery_mv || adc_mv < 0 ||
        CONFIG_MACLAW_FANGTANG_BATTERY_DIVIDER_NUMERATOR <= 0 ||
        CONFIG_MACLAW_FANGTANG_BATTERY_DIVIDER_DENOMINATOR <= 0) {
        return false;
    }
    const int64_t scaled_mv =
        ((int64_t)adc_mv * CONFIG_MACLAW_FANGTANG_BATTERY_DIVIDER_NUMERATOR) /
        CONFIG_MACLAW_FANGTANG_BATTERY_DIVIDER_DENOMINATOR;
    if (scaled_mv > INT_MAX) {
        *out_battery_mv = INT_MAX;
    } else {
        *out_battery_mv = (int)scaled_mv;
    }
    return true;
}

static void fangtang_power_task(void *arg) {
    (void)arg;
    int samples[3] = {0};
    unsigned sample_count = 0;
    unsigned sample_next = 0;
    unsigned ticks = 0;
    while (true) {
        taskENTER_CRITICAL(&s_fangtang_power_task_lock);
        const bool stop_requested = s_fangtang_power_task_stop_requested;
        const bool system_sleep_preparing = s_fangtang_power_task_system_sleep_preparing;
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        if (stop_requested) break;
        if (system_sleep_preparing) {
            /* PREPARE sets the marker before notifying us.  Acknowledgement
             * here proves no ADC/GPIO sample is in progress and keeps this
             * retained task dormant until ABORT or terminal lifecycle stop. */
            if (s_fangtang_power_task_system_sleep_quiesced) {
                xSemaphoreGive(s_fangtang_power_task_system_sleep_quiesced);
            }
            for (;;) {
                (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
                taskENTER_CRITICAL(&s_fangtang_power_task_lock);
                const bool stop_after_wait = s_fangtang_power_task_stop_requested;
                const bool still_preparing = s_fangtang_power_task_system_sleep_preparing;
                taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
                if (stop_after_wait) goto stopped;
                if (!still_preparing) break;
            }
            continue;
        }

        taskENTER_CRITICAL(&s_fangtang_power_task_lock);
        /* Recheck beside the admission increment, so PREPARE cannot observe a
         * zero count while this task is about to touch the peripheral. */
        if (s_fangtang_power_task_system_sleep_preparing) {
            taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
            continue;
        }
        ++s_fangtang_power_task_samples_inflight;
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        const int charge_level = gpio_get_level(FANGTANG_CHARGE_STATUS_GPIO);
        const bool charging_valid = charge_level == 0 || charge_level == 1;
        const bool charging = charge_level == CONFIG_MACLAW_FANGTANG_CHARGE_STATUS_ACTIVE_LEVEL;
        if (!charging_valid) {
            /* A failed GPIO read is not equivalent to "charging" or
             * "not charging".  Invalidate the complete normalized sample so
             * policy cannot act on stale battery/charger state. */
            fangtang_invalidate_telemetry();
            sample_count = 0;
            sample_next = 0;
            ESP_LOGW("fangtang_power", "charge status GPIO read failed");
            goto sample_done;
        }
        const bool sample_due = sample_count < 3 || (++ticks % 60) == 0;
        if (sample_due && s_fangtang_battery_adc) {
            int raw = 0;
            if (adc_oneshot_read(s_fangtang_battery_adc,
                                 (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
                                 &raw) == ESP_OK) {
                samples[sample_next] = raw;
                sample_next = (sample_next + 1) % 3;
                if (sample_count < 3) ++sample_count;
                int total = 0;
                for (unsigned i = 0; i < sample_count; ++i) total += samples[i];
                const int average = total / (int)sample_count;
                int adc_mv = 0;
                if (!s_fangtang_battery_cali ||
                    adc_cali_raw_to_voltage(s_fangtang_battery_cali, average, &adc_mv) != ESP_OK) {
                    fangtang_invalidate_telemetry();
                    sample_count = 0;
                    sample_next = 0;
                    goto sample_done;
                }
                int battery_mv = 0;
                if (!fangtang_scale_adc_mv(adc_mv, &battery_mv)) {
                    fangtang_invalidate_telemetry();
                    sample_count = 0;
                    sample_next = 0;
                    ESP_LOGW("fangtang_power", "invalid battery divider calibration");
                    goto sample_done;
                }
                const unsigned level = fangtang_battery_percent_from_mv(battery_mv);
                taskENTER_CRITICAL(&s_fangtang_power_status_lock);
                s_fangtang_battery_level = level;
                s_fangtang_battery_level_valid = true;
                s_fangtang_battery_charging = charging;
                taskEXIT_CRITICAL(&s_fangtang_power_status_lock);
                ESP_LOGI("fangtang_power", "adc=%d average=%d mv=%d battery=%u%% charging=%s",
                         raw, average, battery_mv, level, charging ? "yes" : "no");
            } else {
                /* Do not leave the last valid battery level visible after a
                 * failed ADC transaction; the next sample must re-establish
                 * validity through calibration before policy can use it. */
                fangtang_invalidate_telemetry();
                sample_count = 0;
                sample_next = 0;
                ESP_LOGW("fangtang_power", "battery ADC read failed");
            }
        } else {
            taskENTER_CRITICAL(&s_fangtang_power_status_lock);
            s_fangtang_battery_charging = charging;
            taskEXIT_CRITICAL(&s_fangtang_power_status_lock);
        }
sample_done:
        taskENTER_CRITICAL(&s_fangtang_power_task_lock);
        if (s_fangtang_power_task_samples_inflight) {
            --s_fangtang_power_task_samples_inflight;
        }
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1000)) != 0) break;
    }
stopped:
    if (s_fangtang_power_task_stopped) xSemaphoreGive(s_fangtang_power_task_stopped);
    vTaskDelete(NULL);
}

esp_err_t compact_peripheral_adapter_init(void) {
    if (s_fangtang_battery_adc || s_fangtang_battery_cali || s_fangtang_power_task) {
        return ESP_ERR_INVALID_STATE;
    }
    gpio_config_t charge = {
        .pin_bit_mask = 1ULL << FANGTANG_CHARGE_STATUS_GPIO,
        .mode = GPIO_MODE_INPUT,
    };
    esp_err_t err = gpio_config(&charge);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_power", "charge status GPIO: %s", esp_err_to_name(err));
        return err;
    }
    const adc_oneshot_unit_init_cfg_t adc_cfg = {
        .unit_id = CONFIG_MACLAW_FANGTANG_BATTERY_ADC_UNIT == 1
                       ? ADC_UNIT_1 : ADC_UNIT_2,
        .ulp_mode = ADC_ULP_MODE_DISABLE,
    };
    err = adc_oneshot_new_unit(&adc_cfg, &s_fangtang_battery_adc);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_power", "battery ADC unit: %s", esp_err_to_name(err));
        return err;
    }
    const adc_oneshot_chan_cfg_t channel_cfg = {
        .atten = ADC_ATTEN_DB_12,
        .bitwidth = ADC_BITWIDTH_12,
    };
    err = adc_oneshot_config_channel(
        s_fangtang_battery_adc,
        (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
        &channel_cfg);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_power", "battery ADC channel: %s", esp_err_to_name(err));
        fangtang_release_adc_resources();
        return err;
    }
    adc_cali_scheme_ver_t schemes = 0;
    err = adc_cali_check_scheme(&schemes);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_power", "ADC calibration scheme: %s", esp_err_to_name(err));
        fangtang_release_adc_resources();
        return err;
    }
    if ((schemes & ADC_CALI_SCHEME_VER_CURVE_FITTING) == 0) {
        ESP_LOGE("fangtang_power", "no supported ADC calibration scheme");
        fangtang_release_adc_resources();
        return ESP_ERR_NOT_SUPPORTED;
    }
    const adc_cali_curve_fitting_config_t cali_cfg = {
        .unit_id = adc_cfg.unit_id,
        .chan = (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
        .atten = channel_cfg.atten,
        .bitwidth = channel_cfg.bitwidth,
    };
    err = adc_cali_create_scheme_curve_fitting(&cali_cfg, &s_fangtang_battery_cali);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_power", "create ADC calibration: %s", esp_err_to_name(err));
        fangtang_release_adc_resources();
        return err;
    }
    s_fangtang_power_task_stopped = xSemaphoreCreateBinary();
    s_fangtang_power_task_system_sleep_quiesced = xSemaphoreCreateBinary();
    if (!s_fangtang_power_task_stopped || !s_fangtang_power_task_system_sleep_quiesced) {
        if (s_fangtang_power_task_stopped) vSemaphoreDelete(s_fangtang_power_task_stopped);
        if (s_fangtang_power_task_system_sleep_quiesced) {
            vSemaphoreDelete(s_fangtang_power_task_system_sleep_quiesced);
        }
        s_fangtang_power_task_stopped = NULL;
        s_fangtang_power_task_system_sleep_quiesced = NULL;
        fangtang_release_adc_resources();
        return ESP_ERR_NO_MEM;
    }
    s_fangtang_power_task_stop_requested = false;
    s_fangtang_power_task_system_sleep_preparing = false;
    s_fangtang_power_task_samples_inflight = 0;
    if (xTaskCreate(fangtang_power_task, "fangtang_power", 3072,
                    NULL, 1, &s_fangtang_power_task) != pdPASS) {
        vSemaphoreDelete(s_fangtang_power_task_stopped);
        vSemaphoreDelete(s_fangtang_power_task_system_sleep_quiesced);
        s_fangtang_power_task_stopped = NULL;
        s_fangtang_power_task_system_sleep_quiesced = NULL;
        fangtang_release_adc_resources();
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

esp_err_t compact_peripheral_adapter_stop_background_tasks(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    taskENTER_CRITICAL(&s_fangtang_power_task_lock);
    TaskHandle_t task = s_fangtang_power_task;
    if (!task) {
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        return ESP_OK;
    }
    if (xTaskGetCurrentTaskHandle() == task) {
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_fangtang_power_task_stop_requested) {
        s_fangtang_power_task_system_sleep_preparing = false;
        s_fangtang_power_task_stop_requested = true;
    }
    taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
    xTaskNotifyGive(task);
    if (!s_fangtang_power_task_stopped ||
        xSemaphoreTake(s_fangtang_power_task_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_fangtang_power_task_stopped);
    vSemaphoreDelete(s_fangtang_power_task_system_sleep_quiesced);
    s_fangtang_power_task_stopped = NULL;
    s_fangtang_power_task_system_sleep_quiesced = NULL;
    s_fangtang_power_task = NULL;
    s_fangtang_power_task_stop_requested = false;
    fangtang_release_adc_resources();
    ESP_LOGI("fangtang_power", "battery monitor stopped");
    return ESP_OK;
}

esp_err_t compact_peripheral_adapter_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_fangtang_power_task_lock);
    if (!s_fangtang_power_task || s_fangtang_power_task_stop_requested ||
        s_fangtang_power_task_system_sleep_preparing) {
        taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_fangtang_power_task_system_sleep_preparing = true;
    task = s_fangtang_power_task;
    taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
    while (xSemaphoreTake(s_fangtang_power_task_system_sleep_quiesced, 0) == pdTRUE) {}
    xTaskNotifyGive(task);
    if (xSemaphoreTake(s_fangtang_power_task_system_sleep_quiesced,
                       pdMS_TO_TICKS(timeout_ms)) == pdTRUE) {
        return ESP_OK;
    }
    /* Keep ADC/GPIO sampling closed after a timed-out acknowledgement. The
     * parent Power transaction owns reverse-order rollback; reopening this
     * monitor here could overlap its next sample with a later profile
     * participant that is still parked or diagnosing failed PREPARE. */
    return ESP_ERR_TIMEOUT;
}

void compact_peripheral_adapter_abort_system_sleep_prepare(void) {
    TaskHandle_t task = NULL;
    taskENTER_CRITICAL(&s_fangtang_power_task_lock);
    s_fangtang_power_task_system_sleep_preparing = false;
    task = s_fangtang_power_task;
    taskEXIT_CRITICAL(&s_fangtang_power_task_lock);
    if (task) xTaskNotifyGive(task);
}

bool compact_peripheral_adapter_get_power_status(unsigned *level_percent, bool *charging) {
    taskENTER_CRITICAL(&s_fangtang_power_status_lock);
    const bool valid = s_fangtang_battery_level_valid;
    if (level_percent) *level_percent = s_fangtang_battery_level;
    if (charging) *charging = s_fangtang_battery_charging;
    taskEXIT_CRITICAL(&s_fangtang_power_status_lock);
    return valid;
}
