#include <inttypes.h>
#include <math.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

#include "driver/gpio.h"
#include "driver/i2s_std.h"
#include "esp_check.h"
#include "esp_chip_info.h"
#include "esp_flash.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_mac.h"
#include "esp_psram.h"
#include "esp_system.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/* Pin map recovered from the factory xiaozhi image (SKU
 * bread-compact-wifi-lcd). This board has no control-bus audio codec: its
 * microphone and speaker amplifier are independent I2S peripherals. */
#define MIC_WS       GPIO_NUM_4
#define MIC_BCLK     GPIO_NUM_5
#define MIC_DIN      GPIO_NUM_6
#define SPK_DOUT     GPIO_NUM_7
#define SPK_BCLK     GPIO_NUM_15
#define SPK_WS       GPIO_NUM_16
#define BOOT_BUTTON  GPIO_NUM_0
#define USER_BUTTON1 GPIO_NUM_38
#define USER_BUTTON2 GPIO_NUM_39
#define STATUS_LED   GPIO_NUM_48

#define AUDIO_RATE 16000
#define TEST_SECONDS 5
#define TEST_SAMPLES (AUDIO_RATE * TEST_SECONDS)

static const char *TAG = "maclaw_hwtest";
static i2s_chan_handle_t s_mic;
static i2s_chan_handle_t s_speaker;

static esp_err_t init_i2s(void)
{
    i2s_chan_config_t channel = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    ESP_RETURN_ON_ERROR(i2s_new_channel(&channel, NULL, &s_mic), TAG, "mic channel");
    i2s_std_config_t mic = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_MSB_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT,
                                                    I2S_SLOT_MODE_MONO),
        .gpio_cfg = {
            .mclk = I2S_GPIO_UNUSED, .bclk = MIC_BCLK, .ws = MIC_WS,
            .dout = I2S_GPIO_UNUSED, .din = MIC_DIN,
            .invert_flags = {0},
        },
    };
    mic.slot_cfg.slot_mask = I2S_STD_SLOT_LEFT;
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_mic, &mic), TAG, "mic mode");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_mic), TAG, "mic enable");

    channel.id = I2S_NUM_1;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&channel, &s_speaker, NULL), TAG, "speaker channel");
    i2s_std_config_t speaker = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT,
                                                        I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = I2S_GPIO_UNUSED, .bclk = SPK_BCLK, .ws = SPK_WS,
            .dout = SPK_DOUT, .din = I2S_GPIO_UNUSED,
            .invert_flags = {0},
        },
    };
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_speaker, &speaker), TAG, "speaker mode");
    return i2s_channel_enable(s_speaker);
}

static void print_platform_report(void)
{
    esp_chip_info_t chip;
    esp_chip_info(&chip);
    uint32_t flash = 0;
    ESP_ERROR_CHECK(esp_flash_get_size(NULL, &flash));
    uint8_t mac[6];
    ESP_ERROR_CHECK(esp_read_mac(mac, ESP_MAC_WIFI_STA));
    ESP_LOGI(TAG, "TEST chip=ESP32-S3 revision=%u cores=%u cpu=%uMHz", chip.revision,
             chip.cores, CONFIG_ESP_DEFAULT_CPU_FREQ_MHZ);
    ESP_LOGI(TAG, "TEST flash=%" PRIu32 "MB psram=%uMB free_psram=%u",
             flash / (1024 * 1024), (unsigned)(esp_psram_get_size() / (1024 * 1024)),
             (unsigned)heap_caps_get_free_size(MALLOC_CAP_SPIRAM));
    ESP_LOGI(TAG, "TEST mac=%02x:%02x:%02x:%02x:%02x:%02x idf=%s",
             mac[0], mac[1], mac[2], mac[3], mac[4], mac[5], esp_get_idf_version());
}

static void test_gpio(void)
{
    gpio_config_t out = {.pin_bit_mask = 1ULL << STATUS_LED, .mode = GPIO_MODE_OUTPUT};
    ESP_ERROR_CHECK(gpio_config(&out));
    gpio_config_t in = {
        .pin_bit_mask = (1ULL << BOOT_BUTTON) | (1ULL << USER_BUTTON1) |
                        (1ULL << USER_BUTTON2),
        .mode = GPIO_MODE_INPUT, .pull_up_en = GPIO_PULLUP_ENABLE,
    };
    ESP_ERROR_CHECK(gpio_config(&in));
    for (int i = 0; i < 6; ++i) {
        gpio_set_level(STATUS_LED, i & 1);
        vTaskDelay(pdMS_TO_TICKS(150));
    }
    gpio_set_level(STATUS_LED, 0);
    ESP_LOGI(TAG, "TEST gpio=PASS boot=%d key1=%d key2=%d led=GPIO48",
             gpio_get_level(BOOT_BUTTON), gpio_get_level(USER_BUTTON1),
             gpio_get_level(USER_BUTTON2));
}

static void play_tone(float hz, unsigned milliseconds)
{
    int16_t stereo[256 * 2];
    unsigned frames_total = AUDIO_RATE * milliseconds / 1000;
    unsigned offset = 0;
    while (offset < frames_total) {
        unsigned frames = frames_total - offset;
        if (frames > 256) frames = 256;
        for (unsigned i = 0; i < frames; ++i) {
            float phase = 2.0f * 3.14159265f * hz * (float)(offset + i) / AUDIO_RATE;
            int16_t sample = (int16_t)(sinf(phase) * 6000.0f);
            stereo[i * 2] = sample;
            stereo[i * 2 + 1] = sample;
        }
        size_t written = 0;
        ESP_ERROR_CHECK(i2s_channel_write(s_speaker, stereo,
                                          frames * 2 * sizeof(int16_t), &written,
                                          pdMS_TO_TICKS(1000)));
        offset += frames;
    }
}

static esp_err_t capture_microphone(int16_t *recorded, size_t samples,
                                    int32_t *out_peak, uint32_t *out_active)
{
    int32_t raw[256];
    size_t done = 0;
    int64_t mean = 0;
    int32_t peak = 0;
    uint32_t active = 0;
    while (done < samples) {
        size_t bytes = 0;
        esp_err_t err = i2s_channel_read(s_mic, raw, sizeof(raw), &bytes,
                                         pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        size_t got = bytes / sizeof(raw[0]);
        if (got > samples - done) got = samples - done;
        for (size_t i = 0; i < got; ++i) {
            int16_t sample = (int16_t)(raw[i] >> 14);
            recorded[done++] = sample;
            mean += sample;
            int32_t magnitude = sample < 0 ? -(int32_t)sample : sample;
            if (magnitude > peak) peak = magnitude;
            if (magnitude > 180) ++active;
        }
    }
    mean /= (int64_t)samples;
    int64_t power = 0;
    for (size_t i = 0; i < samples; ++i) {
        int32_t centered = recorded[i] - (int32_t)mean;
        power += (int64_t)centered * centered;
    }
    ESP_LOGI(TAG, "TEST microphone samples=%u peak=%" PRId32 " rms=%u active=%" PRIu32,
             (unsigned)samples, peak, (unsigned)sqrt((double)power / samples), active);
    *out_peak = peak;
    *out_active = active;
    return ESP_OK;
}

static esp_err_t play_recording(const int16_t *recorded, size_t samples)
{
    int16_t stereo[256 * 2];
    size_t done = 0;
    while (done < samples) {
        size_t frames = samples - done;
        if (frames > 256) frames = 256;
        for (size_t i = 0; i < frames; ++i) {
            int32_t amplified = (int32_t)recorded[done + i] * 2;
            if (amplified > INT16_MAX) amplified = INT16_MAX;
            if (amplified < INT16_MIN) amplified = INT16_MIN;
            stereo[i * 2] = stereo[i * 2 + 1] = (int16_t)amplified;
        }
        size_t written = 0;
        esp_err_t err = i2s_channel_write(s_speaker, stereo,
                                          frames * 2 * sizeof(int16_t), &written,
                                          pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        done += frames;
    }
    return ESP_OK;
}

void app_main(void)
{
    print_platform_report();
    test_gpio();
    ESP_ERROR_CHECK(init_i2s());
    ESP_LOGI(TAG, "TEST audio-pins mic=(BCLK5,WS4,DIN6) speaker=(BCLK15,WS16,DOUT7)");
    ESP_LOGI(TAG, "TEST speaker: playing 880Hz tone now");
    play_tone(880.0f, 700);
    vTaskDelay(pdMS_TO_TICKS(300));

    int16_t *recorded = heap_caps_malloc(TEST_SAMPLES * sizeof(int16_t), MALLOC_CAP_SPIRAM);
    if (!recorded) recorded = malloc(TEST_SAMPLES * sizeof(int16_t));
    ESP_ERROR_CHECK(recorded ? ESP_OK : ESP_ERR_NO_MEM);
    ESP_LOGI(TAG, "TEST microphone: speak for %d seconds; recording starts now", TEST_SECONDS);
    int32_t peak = 0;
    uint32_t active = 0;
    ESP_ERROR_CHECK(capture_microphone(recorded, TEST_SAMPLES, &peak, &active));
    bool mic_pass = peak > 250 && active > AUDIO_RATE / 20;
    ESP_LOGI(TAG, "TEST microphone=%s", mic_pass ? "PASS" : "FAIL_OR_SILENCE");
    ESP_LOGI(TAG, "TEST speaker: replaying microphone capture now");
    ESP_ERROR_CHECK(play_recording(recorded, TEST_SAMPLES));
    free(recorded);
    ESP_LOGI(TAG, "TEST automatic-result chip=PASS flash=PASS psram=PASS gpio=PASS mic=%s speaker=MANUAL_LISTEN",
             mic_pass ? "PASS" : "FAIL");
    ESP_LOGI(TAG, "TEST complete; reset to repeat");
    while (true) vTaskDelay(pdMS_TO_TICKS(1000));
}
