/*
 * Fangtang-4G board-adapter transition unit.
 *
 * CMake selects this translation unit only for the Fangtang profile.  It is
 * intentionally a one-owner bridge while the legacy Bread/Fangtang combined
 * adapter is dismantled: including the legacy implementation keeps every
 * physical side effect in exactly one translation unit, so there is no second
 * display, audio, GPIO scanner, or power-sampling owner during the cutover.
 *
 * Move Fangtang-specific NV3023, GPIO0, ML307 presentation and battery code
 * from ../../board_port_bread_compact.c here in independently verified
 * increments.  Do not add Fangtang-only business policy to this file; shared
 * policy continues above the Device API boundary.
 */

#include "sdkconfig.h"
#include "esp_err.h"
#include "ml307_transport.h"
#include "configuration_service.h"
#include "persistence_service.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang adapter may only be compiled for CONFIG_MACLAW_BOARD_FANGTANG_4G"
#endif

/* The compact legacy adapter still owns shared direct-I2S and common scene
 * mechanics during this transition.  Do not let it instantiate Fangtang's
 * modem-enable operation: the physical guard/power sequence has moved below
 * into this profile adapter, while the application continues to use only the
 * hardware-neutral Device API. */
#define MACLAW_FANGTANG_EXTERNAL_CELLULAR_PREPARATION 1
#define MACLAW_FANGTANG_EXTERNAL_CELLULAR_CANCELLATION 1
#define MACLAW_FANGTANG_EXTERNAL_CELLULAR_START 1
#define MACLAW_FANGTANG_EXTERNAL_CELLULAR_HTTP 1
#define MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR 1
#define MACLAW_FANGTANG_EXTERNAL_CONNECTIVITY_CONFIGURATION 1
#define MACLAW_FANGTANG_EXTERNAL_POWER_TELEMETRY 1
#define MACLAW_FANGTANG_EXTERNAL_POWER_STATUS_GETTER 1
#define MACLAW_FANGTANG_EXTERNAL_POWER_MONITOR_STOP 1
static esp_err_t fangtang_board_power_init(void);
static esp_err_t fangtang_board_stop_power_monitor(uint32_t timeout_ms);
#include "../../board_port_bread_compact.c"
#undef MACLAW_FANGTANG_EXTERNAL_CELLULAR_PREPARATION
#undef MACLAW_FANGTANG_EXTERNAL_CELLULAR_CANCELLATION
#undef MACLAW_FANGTANG_EXTERNAL_CELLULAR_START
#undef MACLAW_FANGTANG_EXTERNAL_CELLULAR_HTTP
#undef MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR
#undef MACLAW_FANGTANG_EXTERNAL_CONNECTIVITY_CONFIGURATION
#undef MACLAW_FANGTANG_EXTERNAL_POWER_TELEMETRY
#undef MACLAW_FANGTANG_EXTERNAL_POWER_STATUS_GETTER
#undef MACLAW_FANGTANG_EXTERNAL_POWER_MONITOR_STOP

static TaskHandle_t s_fangtang_power_task;
static SemaphoreHandle_t s_fangtang_power_task_stopped;

static unsigned fangtang_battery_percent_from_adc(int adc) {
    static const struct {
        int adc;
        unsigned percent;
    } levels[] = {
        {1650, 0}, {1750, 5}, {1850, 15}, {1950, 30},
        {2050, 50}, {2150, 70}, {2250, 85}, {2350, 100},
    };
    if (adc <= levels[0].adc) return levels[0].percent;
    for (size_t i = 1; i < sizeof(levels) / sizeof(levels[0]); ++i) {
        if (adc <= levels[i].adc) {
            const int range = levels[i].adc - levels[i - 1].adc;
            const int offset = adc - levels[i - 1].adc;
            return levels[i - 1].percent +
                   (unsigned)((offset * (int)(levels[i].percent - levels[i - 1].percent)) /
                              range);
        }
    }
    return 100;
}

static void fangtang_power_task(void *arg) {
    (void)arg;
    int samples[3] = {0};
    unsigned sample_count = 0;
    unsigned sample_next = 0;
    unsigned ticks = 0;
    while (true) {
        const bool charging = gpio_get_level(FANGTANG_CHARGE_STATUS_GPIO) != 0;
        const bool sample_due = sample_count < 3 || (++ticks % 60) == 0;
        if (sample_due && s_battery_adc) {
            int raw = 0;
            if (adc_oneshot_read(s_battery_adc,
                                 (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
                                 &raw) == ESP_OK) {
                samples[sample_next] = raw;
                sample_next = (sample_next + 1) % 3;
                if (sample_count < 3) ++sample_count;
                int total = 0;
                for (unsigned i = 0; i < sample_count; ++i) total += samples[i];
                const int average = total / (int)sample_count;
                const unsigned level = fangtang_battery_percent_from_adc(average);
                taskENTER_CRITICAL(&s_power_status_lock);
                s_battery_level = level;
                s_battery_level_valid = true;
                s_battery_charging = charging;
                taskEXIT_CRITICAL(&s_power_status_lock);
                ESP_LOGI("fangtang_port", "power: adc=%d average=%d battery=%u%% charging=%s",
                         raw, average, level, charging ? "yes" : "no");
            }
        } else {
            taskENTER_CRITICAL(&s_power_status_lock);
            s_battery_charging = charging;
            taskEXIT_CRITICAL(&s_power_status_lock);
        }
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1000)) != 0) break;
    }
    s_fangtang_power_task = NULL;
    if (s_fangtang_power_task_stopped) xSemaphoreGive(s_fangtang_power_task_stopped);
    vTaskDelete(NULL);
}

static esp_err_t fangtang_board_power_init(void) {
    gpio_config_t charge = {
        .pin_bit_mask = 1ULL << FANGTANG_CHARGE_STATUS_GPIO,
        .mode = GPIO_MODE_INPUT,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&charge), "fangtang_port", "charge status GPIO");
    adc_oneshot_unit_init_cfg_t adc_cfg = {
        .unit_id = CONFIG_MACLAW_FANGTANG_BATTERY_ADC_UNIT == 1
                       ? ADC_UNIT_1 : ADC_UNIT_2,
        .ulp_mode = ADC_ULP_MODE_DISABLE,
    };
    ESP_RETURN_ON_ERROR(adc_oneshot_new_unit(&adc_cfg, &s_battery_adc),
                        "fangtang_port", "battery ADC unit");
    adc_oneshot_chan_cfg_t channel_cfg = {
        .atten = ADC_ATTEN_DB_12,
        .bitwidth = ADC_BITWIDTH_12,
    };
    ESP_RETURN_ON_ERROR(adc_oneshot_config_channel(
                            s_battery_adc,
                            (adc_channel_t)CONFIG_MACLAW_FANGTANG_BATTERY_ADC_CHANNEL,
                            &channel_cfg),
                        "fangtang_port", "battery ADC channel");
    s_fangtang_power_task_stopped = xSemaphoreCreateBinary();
    if (!s_fangtang_power_task_stopped) return ESP_ERR_NO_MEM;
    if (xTaskCreate(fangtang_power_task, "fangtang_power", 3072,
                    NULL, 1, &s_fangtang_power_task) != pdPASS) {
        vSemaphoreDelete(s_fangtang_power_task_stopped);
        s_fangtang_power_task_stopped = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

/* This is intentionally a task-only stop.  The ADC unit remains valid for
 * diagnostic telemetry and board port remains boot-lifetime; destroying it
 * would require an explicit, complete board deinit transaction. */
static esp_err_t fangtang_board_stop_power_monitor(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_fangtang_power_task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == s_fangtang_power_task) return ESP_ERR_INVALID_STATE;

    xTaskNotifyGive(s_fangtang_power_task);
    if (!s_fangtang_power_task_stopped ||
        xSemaphoreTake(s_fangtang_power_task_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    vSemaphoreDelete(s_fangtang_power_task_stopped);
    s_fangtang_power_task_stopped = NULL;
    ESP_LOGI("fangtang_port", "battery power monitor stopped");
    return ESP_OK;
}

/* The sampling task above is the sole writer of this snapshot.  Keeping the
 * reader in the same profile adapter completes the power-telemetry seam
 * without exposing Fangtang ADC/GPIO details to Device API callers or
 * introducing a second state store during the bridge transition. */
bool board_port_get_power_status(unsigned *level_percent, bool *charging) {
    taskENTER_CRITICAL(&s_power_status_lock);
    const bool valid = s_battery_level_valid;
    if (level_percent) *level_percent = s_battery_level;
    if (charging) *charging = s_battery_charging;
    taskEXIT_CRITICAL(&s_power_status_lock);
    return valid;
}

/* Runs synchronously before the legacy compact scanner is created.  The
 * selector therefore owns GPIO0 for this bounded interval and hands it off at
 * a quiescent point, rather than making a second scanner race normal input. */
static bool s_fangtang_boot_toggle_latched;

void fangtang_board_run_boot_network_selector(void) {
    const uint32_t window_ms = 1800;
    bool released = gpio_get_level(BUTTON_ACTIVATE) != 0;
    bool previous_released = released;
    int64_t pressed_at = 0;
    int64_t first_release_at = 0;
    const int64_t deadline = esp_timer_get_time() + (int64_t)window_ms * 1000;
    s_fangtang_boot_toggle_latched = false;
    ESP_LOGI("fangtang_port", "GPIO0 startup network selector active for %u ms",
             (unsigned)window_ms);
    while (esp_timer_get_time() < deadline && !s_fangtang_boot_toggle_latched) {
        int64_t now = esp_timer_get_time();
        released = gpio_get_level(BUTTON_ACTIVATE) != 0;
        if (previous_released && !released) {
            pressed_at = now;
        } else if (!previous_released && released && pressed_at) {
            const int64_t duration = now - pressed_at;
            pressed_at = 0;
            if (duration < 2500000) {
                if (first_release_at && now - first_release_at <= 500000) {
                    s_fangtang_boot_toggle_latched = true;
                    break;
                }
                first_release_at = now;
            } else {
                first_release_at = 0;
            }
        }
        if (first_release_at && now - first_release_at > 500000) {
            first_release_at = 0;
        }
        previous_released = released;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    ESP_LOGI("fangtang_port", "GPIO0 startup network selector closed: %s",
             s_fangtang_boot_toggle_latched ? "toggle" : "unchanged");
}

bool board_port_wait_for_boot_network_toggle(uint32_t window_ms) {
    (void)window_ms;
    const bool requested = s_fangtang_boot_toggle_latched;
    s_fangtang_boot_toggle_latched = false;
    return requested;
}

/* The legacy device stores its preferred link in a vendor namespace.  Keep
 * that one-time migration here, then own the normalized choice in MaClaw's
 * namespace so no domain service has to know this board existed. */
bool board_port_load_transport_selection(bool *out_cellular) {
    if (!out_cellular) return false;
    bool cellular = CONFIG_MACLAW_FANGTANG_DEFAULT_4G;
    bool saved = false;
    esp_err_t err = configuration_service_load_transport_selection(cellular, &cellular, &saved);
    if (err == ESP_OK) {
        if (!saved) {
            /* The first Configuration-Service image may have no normalized
             * selection yet.  Import the vendor state below before accepting
             * the board default. */
        } else {
            *out_cellular = cellular;
            ESP_LOGI("fangtang_port", "restored transport selection: %s",
                     cellular ? "4G" : "Wi-Fi");
            return true;
        }
    }
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_port", "cannot load transport configuration: %s",
                 esp_err_to_name(err));
        return false;
    }
    /* Pre-Configuration-Service vendor images stored only this board's link
     * type.  Read it once as an adapter-local import and immediately write
     * the normalized snapshot. */
    int32_t stock_type = 0;
    esp_err_t stock_err = persistence_service_read_i32("network", "type", &stock_type);
    if (stock_err == ESP_OK && (stock_type == 0 || stock_type == 1)) {
        cellular = stock_type == 1;
        esp_err_t import_err = configuration_service_set_transport_selection(cellular);
        if (import_err != ESP_OK) {
            ESP_LOGE("fangtang_port", "cannot import stock transport selection: %s",
                     esp_err_to_name(import_err));
            return false;
        }
    } else if (stock_err != ESP_OK && stock_err != ESP_ERR_NVS_NOT_FOUND) {
        ESP_LOGE("fangtang_port", "cannot read stock transport selection: %s",
                 esp_err_to_name(stock_err));
        return false;
    }
    *out_cellular = cellular;
    ESP_LOGI("fangtang_port", "restored transport selection: %s",
             *out_cellular ? "4G" : "Wi-Fi");
    return true;
}

bool board_port_apply_startup_transport_toggle(uint32_t window_ms,
                                               bool current_cellular,
                                               bool *out_cellular) {
    if (!out_cellular) return false;
    *out_cellular = current_cellular;
    if (!board_port_wait_for_boot_network_toggle(window_ms)) return false;
    *out_cellular = !current_cellular;
    esp_err_t err = configuration_service_set_transport_selection(*out_cellular);
    if (err != ESP_OK) {
        ESP_LOGE("fangtang_port", "cannot save transport selection: %s",
                 esp_err_to_name(err));
        *out_cellular = current_cellular;
        return false;
    }
    ESP_LOGI("fangtang_port", "startup transport toggle selected: %s",
             *out_cellular ? "4G" : "Wi-Fi");
    return true;
}

void board_port_adapt_gateway_url(char *gateway_url, size_t capacity,
                                  bool cellular_active) {
    if (!gateway_url || capacity == 0 || !cellular_active) return;
    /* ML307R-DL-MBRH0S01 cannot negotiate the Hub's ECDSA-only certificate
     * with its native HTTPS engine. Only rewrite the standard production
     * origin; custom/customer endpoints retain their exact configured URL. */
    if (!strcmp(gateway_url, "https://hub.mypapers.top") ||
        !strcmp(gateway_url, "http://hub.mypapers.top")) {
        strlcpy(gateway_url, "http://hub.mypapers.top:9399", capacity);
        ESP_LOGW("fangtang_port", "cellular transport selected Hub direct HTTP endpoint");
    }
}

esp_err_t board_port_prepare_cellular_transport(void) {
    if (CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO < 0 ||
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO < 0) {
        return ESP_ERR_INVALID_ARG;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO >= 0) {
        gpio_config_t guard = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
            .mode = GPIO_MODE_OUTPUT,
            .pull_down_en = GPIO_PULLDOWN_ENABLE,
        };
        esp_err_t err = gpio_config(&guard);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_GUARD_LEVEL);
        if (err != ESP_OK) return err;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO >= 0) {
        gpio_config_t power = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
            .mode = GPIO_MODE_OUTPUT,
        };
        esp_err_t err = gpio_config(&power);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_POWER_ACTIVE_LEVEL);
        if (err != ESP_OK) return err;
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    return ESP_OK;
}

esp_err_t board_port_start_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(board_port_prepare_cellular_transport(), "fangtang_port",
                        "cellular hardware preparation");
    return ml307_transport_start(CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO,
                                 CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO,
                                 CONFIG_MACLAW_FANGTANG_MODEM_UART_BAUD,
                                 (int)timeout_ms, CONFIG_MACLAW_FANGTANG_MODEM_APN);
}

bool board_port_is_cellular_transport_ready(void) {
    return ml307_transport_is_ready();
}

static esp_err_t fangtang_stream_body_reader(void *context, void *buffer,
                                              size_t requested, size_t *read_bytes) {
    if (!context || !buffer || !read_bytes || requested > UINT32_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    const device_connectivity_stream_request_t *request = context;
    uint32_t read = 0;
    device_status_t status = request->body_reader(request->body_reader_context,
                                                   buffer, (uint32_t)requested, &read);
    if (status != DEVICE_STATUS_OK) return device_status_to_platform_error(status);
    *read_bytes = read;
    return ESP_OK;
}

esp_err_t board_port_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!request || request->body_len > SIZE_MAX || request->response_capacity > SIZE_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    size_t response_len = 0;
    esp_err_t err = ml307_transport_http_request(
        request->method, request->url, request->content_type, request->authorization,
        request->extra_header_name, request->extra_header_value, request->body,
        (size_t)request->body_len, request->response, (size_t)request->response_capacity,
        &response_len, request->status_code, request->truncated,
        (int)request->timeout_ms, request->foreground);
    if (response_len > UINT32_MAX) return ESP_ERR_INVALID_SIZE;
    *request->response_len = (uint32_t)response_len;
    return err;
}

esp_err_t board_port_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || request->request.body_len > SIZE_MAX ||
        request->request.response_capacity > SIZE_MAX ||
        request->stream_buffer_size > SIZE_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    size_t response_len = 0;
    esp_err_t err = ml307_transport_http_request_stream(
        request->request.method, request->request.url, request->request.content_type,
        request->request.authorization, request->request.extra_header_name,
        request->request.extra_header_value, (size_t)request->request.body_len,
        fangtang_stream_body_reader, (void *)request, request->stream_buffer,
        (size_t)request->stream_buffer_size, request->request.response,
        (size_t)request->request.response_capacity,
        &response_len, request->request.status_code,
        request->request.truncated, (int)request->request.timeout_ms,
        request->request.foreground);
    if (response_len > UINT32_MAX) return ESP_ERR_INVALID_SIZE;
    *request->request.response_len = (uint32_t)response_len;
    return err;
}

bool board_port_cancel_cellular_foreground_request(void) {
    return ml307_transport_cancel_foreground();
}
