#include "buddy_sound.h"

#include <stdio.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "driver/i2s_std.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "nvs.h"

#include "buddy_i2c.h"
#include "buddy_protocol.h"
#include "buddy_sound_index.h"

#define SOUND_I2C_ADDR 0x18
#define SOUND_MUTE_REG 0x31
#define SOUND_VOLUME_REG 0x32
#define SOUND_VOLUME_VALUE 0xB3  // ~70%, the board's verified speech level
#define SOUND_MCLK_GPIO GPIO_NUM_10
#define SOUND_BCLK_GPIO GPIO_NUM_15
#define SOUND_WS_GPIO GPIO_NUM_16
#define SOUND_DOUT_GPIO GPIO_NUM_14
#define SOUND_PA_GPIO GPIO_NUM_18
#define SOUND_RATE 16000
#define SOUND_CHUNK_BYTES 2048

extern const uint8_t _binary_sounds_pcm_start[];
extern const uint8_t _binary_sounds_pcm_end[];

static i2c_master_dev_handle_t s_codec;
static i2s_chan_handle_t s_tx;
static QueueHandle_t s_queue;
static SemaphoreHandle_t s_cfg_lock;
static buddy_sound_config_t s_cfg;
static bool s_available;

static buddy_sound_config_t default_config(void) {
    buddy_sound_config_t cfg;
    memset(&cfg, 0, sizeof(cfg));
    cfg.master = true;
    for (int i = 0; i < BUDDY_SOUND_EVENT_COUNT; ++i) {
        cfg.enabled[i] = true;
        cfg.effect[i] = 0;
    }
    return cfg;
}

static void load_config(void) {
    s_cfg = default_config();
    nvs_handle_t h;
    if (nvs_open("buddy", NVS_READONLY, &h) != ESP_OK) return;
    buddy_sound_config_t saved;
    size_t len = sizeof(saved);
    if (nvs_get_blob(h, "sound", &saved, &len) == ESP_OK && len == sizeof(saved)) {
        s_cfg = saved;
        for (int i = 0; i < BUDDY_SOUND_EVENT_COUNT; ++i) {
            if (s_cfg.effect[i] < 0 || s_cfg.effect[i] >= BUDDY_SOUND_COUNT) {
                s_cfg.effect[i] = 0;
            }
        }
    }
    nvs_close(h);
}

static void save_config(void) {
    nvs_handle_t h;
    if (nvs_open("buddy", NVS_READWRITE, &h) != ESP_OK) return;
    (void)nvs_set_blob(h, "sound", &s_cfg, sizeof(s_cfg));
    (void)nvs_commit(h);
    nvs_close(h);
}

static bool codec_write(uint8_t reg, uint8_t value) {
    if (!s_codec) return false;
    const uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_codec, bytes, sizeof(bytes), 1000) == ESP_OK;
}

static bool codec_read(uint8_t reg, uint8_t *value) {
    if (!s_codec || !value) return false;
    return i2c_master_transmit_receive(s_codec, &reg, 1, value, 1, 1000) == ESP_OK;
}

static void codec_mute(bool muted) {
    uint8_t value = 0;
    if (!codec_read(SOUND_MUTE_REG, &value)) return;
    value = muted ? (uint8_t)(value | 0x60u) : (uint8_t)(value & ~0x60u);
    codec_write(SOUND_MUTE_REG, value);
}

static esp_err_t init_i2s(void) {
    i2s_chan_config_t channels = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    channels.dma_desc_num = 4;
    channels.dma_frame_num = 256;
    channels.auto_clear_after_cb = true;
    esp_err_t err = i2s_new_channel(&channels, &s_tx, NULL);
    if (err != ESP_OK) return err;

    i2s_std_config_t tx_config = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(SOUND_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(
            I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {
            .mclk = SOUND_MCLK_GPIO,
            .bclk = SOUND_BCLK_GPIO,
            .ws = SOUND_WS_GPIO,
            .dout = SOUND_DOUT_GPIO,
            .din = I2S_GPIO_UNUSED,
            .invert_flags = {.mclk_inv = false, .bclk_inv = false, .ws_inv = false},
        },
    };
    tx_config.clk_cfg.mclk_multiple = I2S_MCLK_MULTIPLE_256;
    err = i2s_channel_init_std_mode(s_tx, &tx_config);
    if (err != ESP_OK) return err;
    // The ES8311 expects a live 4.096 MHz MCLK while it is programmed.
    return i2s_channel_enable(s_tx);
}

static esp_err_t init_codec(void) {
    // EchoEar-2ST ES8311 slave-mode table for 16 kHz / 16-bit stereo,
    // external 4.096 MHz MCLK (verified in iot-agentos).
    static const uint8_t init[][2] = {
        {0x00, 0x1F}, {0x00, 0x00}, {0x00, 0x80},
        {0x01, 0x3F}, {0x02, 0x00}, {0x03, 0x10}, {0x04, 0x20},
        {0x05, 0x00}, {0x06, 0x03}, {0x07, 0x00}, {0x08, 0xFF},
        {0x0B, 0x00}, {0x0C, 0x00}, {0x10, 0x1F}, {0x11, 0x7F},
        {0x09, 0x0C}, {0x0A, 0x4C}, {0x0D, 0x01}, {0x0E, 0x02},
        {0x12, 0x00}, {0x13, 0x10}, {0x14, 0x1A}, {0x15, 0x40},
        {0x16, 0x24}, {0x1B, 0x0A}, {0x1C, 0x6A}, {0x37, 0x08},
        {0x44, 0x58}, {0x45, 0x00}, {0x31, 0x60},
    };
    if (!codec_write(init[0][0], init[0][1])) return ESP_ERR_INVALID_STATE;
    vTaskDelay(pdMS_TO_TICKS(20));
    for (size_t i = 1; i < sizeof(init) / sizeof(init[0]); ++i) {
        if (!codec_write(init[i][0], init[i][1])) return ESP_ERR_INVALID_STATE;
    }
    if (!codec_write(SOUND_VOLUME_REG, SOUND_VOLUME_VALUE)) return ESP_ERR_INVALID_STATE;
    return ESP_OK;
}

static esp_err_t init_pa(void) {
    const gpio_config_t config = {
        .pin_bit_mask = 1ULL << SOUND_PA_GPIO,
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    esp_err_t err = gpio_config(&config);
    if (err != ESP_OK) return err;
    return gpio_set_level(SOUND_PA_GPIO, 0);
}

static void play_effect(int index) {
    if (index < 0 || index >= BUDDY_SOUND_COUNT) return;
    const uint32_t offset = s_sound_offsets[index];
    const uint32_t length = s_sound_lengths[index];
    const size_t available = (size_t)(_binary_sounds_pcm_end - _binary_sounds_pcm_start);
    if ((size_t)offset + length > available) return;

    gpio_set_level(SOUND_PA_GPIO, 1);
    codec_mute(false);
    vTaskDelay(pdMS_TO_TICKS(2));

    const uint8_t *pcm = _binary_sounds_pcm_start + offset;
    size_t sent = 0;
    while (sent < length) {
        size_t chunk = length - sent;
        if (chunk > SOUND_CHUNK_BYTES) chunk = SOUND_CHUNK_BYTES;
        size_t written = 0;
        if (i2s_channel_write(s_tx, pcm + sent, chunk, &written, 1000) != ESP_OK || written == 0) {
            break;
        }
        sent += written;
    }

    // Let the 64 ms DMA queue drain before muting the analogue path.
    vTaskDelay(pdMS_TO_TICKS(90));
    codec_mute(true);
    gpio_set_level(SOUND_PA_GPIO, 0);

    char msg[32];
    snprintf(msg, sizeof(msg), "sound %d", index + 1);
    buddy_protocol_send_log("info", msg);
}

static void sound_task(void *arg) {
    (void)arg;
    int index = 0;
    for (;;) {
        if (xQueueReceive(s_queue, &index, portMAX_DELAY) != pdTRUE) continue;
        if (!s_available) continue;
        play_effect(index);
    }
}

esp_err_t buddy_sound_init(void) {
    if (!s_cfg_lock) s_cfg_lock = xSemaphoreCreateMutex();
    if (!s_cfg_lock) return ESP_ERR_NO_MEM;
    load_config();

    i2c_master_bus_handle_t bus = buddy_i2c_bus();
    if (!bus) return ESP_ERR_INVALID_STATE;
    const i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = SOUND_I2C_ADDR,
        .scl_speed_hz = 400000,
    };
    esp_err_t err = i2c_master_bus_add_device(bus, &dev_cfg, &s_codec);
    if (err != ESP_OK) return err;

    err = init_i2s();
    if (err != ESP_OK) return err;
    err = init_codec();
    if (err != ESP_OK) return err;
    err = init_pa();
    if (err != ESP_OK) return err;

    s_queue = xQueueCreate(4, sizeof(int));
    if (!s_queue) return ESP_ERR_NO_MEM;
    if (xTaskCreate(sound_task, "buddy_sound", 4096, NULL, 4, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    s_available = true;
    return ESP_OK;
}

bool buddy_sound_available(void) { return s_available; }

void buddy_sound_set_config(const buddy_sound_config_t *cfg) {
    if (!cfg || !s_cfg_lock) return;
    xSemaphoreTake(s_cfg_lock, portMAX_DELAY);
    s_cfg = *cfg;
    for (int i = 0; i < BUDDY_SOUND_EVENT_COUNT; ++i) {
        if (s_cfg.effect[i] < 0 || s_cfg.effect[i] >= BUDDY_SOUND_COUNT) {
            s_cfg.effect[i] = 0;
        }
    }
    save_config();
    xSemaphoreGive(s_cfg_lock);
}

void buddy_sound_get_config(buddy_sound_config_t *out) {
    if (!out || !s_cfg_lock) return;
    xSemaphoreTake(s_cfg_lock, portMAX_DELAY);
    *out = s_cfg;
    xSemaphoreGive(s_cfg_lock);
}

void buddy_sound_event(int event) {
    if (event < 0 || event >= BUDDY_SOUND_EVENT_COUNT) return;
    if (!s_available || !s_queue || !s_cfg_lock) return;
    xSemaphoreTake(s_cfg_lock, portMAX_DELAY);
    const bool play = s_cfg.master && s_cfg.enabled[event];
    const int effect = s_cfg.effect[event];
    xSemaphoreGive(s_cfg_lock);
    if (!play) return;
    (void)xQueueSend(s_queue, &effect, 0);
}
