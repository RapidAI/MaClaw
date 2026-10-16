#include <stdbool.h>
#include <stdint.h>

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_lcd_nv3023.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define LCD_HOST SPI3_HOST
#define LCD_WIDTH 240
#define LCD_HEIGHT 240
#define LCD_MOSI GPIO_NUM_10
#define LCD_CLK GPIO_NUM_9
#define LCD_DC GPIO_NUM_8
#define LCD_RST GPIO_NUM_18
#define LCD_CS GPIO_NUM_14
#define LCD_BACKLIGHT GPIO_NUM_13
#define LCD_Y_OFFSET 80

static const char *TAG = "nv3023_test";
static esp_lcd_panel_io_handle_t s_panel_io;
static esp_lcd_panel_handle_t s_panel;
static uint16_t s_frame[LCD_WIDTH * LCD_HEIGHT];

extern const uint8_t _binary_doraemon_frame0_rgb565a8_start[];
extern const uint8_t _binary_doraemon_frame0_rgb565a8_end[];

/* Original xingzhi-cube-0.85tft-ml307 initialization table. */
static const nv3023_lcd_init_cmd_t s_init[] = {
    {0xff, (uint8_t[]){0xa5}, 1, 0},
    {0x3e, (uint8_t[]){0x09}, 1, 0},
    {0x3a, (uint8_t[]){0x65}, 1, 0},
    {0x82, (uint8_t[]){0x00}, 1, 0},
    {0x98, (uint8_t[]){0x00}, 1, 0},
    {0x63, (uint8_t[]){0x0f}, 1, 0},
    {0x64, (uint8_t[]){0x0f}, 1, 0},
    {0xb4, (uint8_t[]){0x34}, 1, 0},
    {0xb5, (uint8_t[]){0x30}, 1, 0},
    {0x83, (uint8_t[]){0x03}, 1, 0},
    {0x86, (uint8_t[]){0x04}, 1, 0},
    {0x87, (uint8_t[]){0x16}, 1, 0},
    {0x88, (uint8_t[]){0x0a}, 1, 0},
    {0x89, (uint8_t[]){0x27}, 1, 0},
    {0x93, (uint8_t[]){0x63}, 1, 0},
    {0x96, (uint8_t[]){0x81}, 1, 0},
    {0xc3, (uint8_t[]){0x10}, 1, 0},
    {0xe6, (uint8_t[]){0x00}, 1, 0},
    {0x99, (uint8_t[]){0x01}, 1, 0},
    {0x70, (uint8_t[]){0x09}, 1, 0},
    {0x71, (uint8_t[]){0x1d}, 1, 0},
    {0x72, (uint8_t[]){0x14}, 1, 0},
    {0x73, (uint8_t[]){0x0a}, 1, 0},
    {0x74, (uint8_t[]){0x11}, 1, 0},
    {0x75, (uint8_t[]){0x16}, 1, 0},
    {0x76, (uint8_t[]){0x38}, 1, 0},
    {0x77, (uint8_t[]){0x0b}, 1, 0},
    {0x78, (uint8_t[]){0x08}, 1, 0},
    {0x79, (uint8_t[]){0x3e}, 1, 0},
    {0x7a, (uint8_t[]){0x07}, 1, 0},
    {0x7b, (uint8_t[]){0x0d}, 1, 0},
    {0x7c, (uint8_t[]){0x16}, 1, 0},
    {0x7d, (uint8_t[]){0x0f}, 1, 0},
    {0x7e, (uint8_t[]){0x14}, 1, 0},
    {0x7f, (uint8_t[]){0x05}, 1, 0},
    {0xa0, (uint8_t[]){0x04}, 1, 0},
    {0xa1, (uint8_t[]){0x28}, 1, 0},
    {0xa2, (uint8_t[]){0x0c}, 1, 0},
    {0xa3, (uint8_t[]){0x11}, 1, 0},
    {0xa4, (uint8_t[]){0x0b}, 1, 0},
    {0xa5, (uint8_t[]){0x23}, 1, 0},
    {0xa6, (uint8_t[]){0x45}, 1, 0},
    {0xa7, (uint8_t[]){0x07}, 1, 0},
    {0xa8, (uint8_t[]){0x0a}, 1, 0},
    {0xa9, (uint8_t[]){0x3b}, 1, 0},
    {0xaa, (uint8_t[]){0x0d}, 1, 0},
    {0xab, (uint8_t[]){0x18}, 1, 0},
    {0xac, (uint8_t[]){0x14}, 1, 0},
    {0xad, (uint8_t[]){0x0f}, 1, 0},
    {0xae, (uint8_t[]){0x19}, 1, 0},
    {0xaf, (uint8_t[]){0x08}, 1, 0},
    {0xff, (uint8_t[]){0x00}, 1, 0},
    {0x11, NULL, 0, 120},
    {0x29, NULL, 0, 10},
};

static uint16_t rgb(uint8_t r, uint8_t g, uint8_t b)
{
    uint16_t value = (uint16_t)(((r & 0xf8) << 8) | ((g & 0xfc) << 3) | (b >> 3));
    return (uint16_t)((value << 8) | (value >> 8));
}

static esp_err_t draw_frame(void)
{
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        const int gram_y = LCD_Y_OFFSET + y;
        const uint8_t columns[] = {
            0x00, 0x00,
            (uint8_t)((LCD_WIDTH - 1) >> 8), (uint8_t)(LCD_WIDTH - 1),
        };
        const uint8_t rows[] = {
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
        };
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x2a,
                                                      columns, sizeof(columns)),
                            TAG, "window CASET y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x2b,
                                                      rows, sizeof(rows)),
                            TAG, "window RASET y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_color(
                                s_panel_io, 0x2c, &s_frame[y * LCD_WIDTH],
                                LCD_WIDTH * sizeof(uint16_t)),
                            TAG, "window RAMWR y=%d", y);
    }
    return ESP_OK;
}

static esp_err_t draw_colmod_comparison(void)
{
    const uint8_t colmod_65 = 0x65;
    const uint8_t colmod_55 = 0x55;
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        const int gram_y = LCD_Y_OFFSET + y;
        const uint8_t rows[] = {
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
            (uint8_t)(gram_y >> 8), (uint8_t)gram_y,
        };
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x2b,
                                                      rows, sizeof(rows)),
                            TAG, "window RASET y=%d", y);

        /* Left: the board vendor's 0x65. Right: conventional 16-bit 0x55.
         * Switching COLMOD before each RAM write isolates pixel unpacking;
         * the panel keeps the already-written GRAM contents unchanged. */
        const uint8_t left_columns[] = {0x00, 0x00, 0x00, 0x77};
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x3a,
                                                      &colmod_65, 1),
                            TAG, "COLMOD 0x65 y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x2a,
                                                      left_columns,
                                                      sizeof(left_columns)),
                            TAG, "left CASET y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_color(
                                s_panel_io, 0x2c, &s_frame[y * LCD_WIDTH],
                                (LCD_WIDTH / 2) * sizeof(uint16_t)),
                            TAG, "left RAMWR y=%d", y);

        const uint8_t right_columns[] = {0x00, 0x78, 0x00, 0xef};
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x3a,
                                                      &colmod_55, 1),
                            TAG, "COLMOD 0x55 y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(s_panel_io, 0x2a,
                                                      right_columns,
                                                      sizeof(right_columns)),
                            TAG, "right CASET y=%d", y);
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_color(
                                s_panel_io, 0x2c,
                                &s_frame[y * LCD_WIDTH + LCD_WIDTH / 2],
                                (LCD_WIDTH / 2) * sizeof(uint16_t)),
                            TAG, "right RAMWR y=%d", y);
    }
    return ESP_OK;
}

static void quadrant_pattern(void)
{
    const uint16_t colors[] = {
        rgb(255, 0, 0), rgb(0, 255, 0), rgb(0, 0, 255), rgb(255, 255, 255),
    };
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            s_frame[y * LCD_WIDTH + x] =
                colors[(y >= LCD_HEIGHT / 2) * 2 + (x >= LCD_WIDTH / 2)];
        }
    }
}

static void stripe_pattern(bool vertical)
{
    const uint16_t colors[] = {
        rgb(255, 0, 0), rgb(255, 255, 0), rgb(0, 255, 0), rgb(0, 255, 255),
        rgb(0, 0, 255), rgb(255, 0, 255), rgb(255, 255, 255), rgb(0, 0, 0),
    };
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            int axis = vertical ? x : y;
            s_frame[y * LCD_WIDTH + x] = colors[axis / 30];
        }
    }
}

static void grid_pattern(void)
{
    const uint16_t bg = rgb(12, 18, 28);
    const uint16_t line = rgb(255, 255, 255);
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            bool border = x == 0 || y == 0 || x == LCD_WIDTH - 1 ||
                          y == LCD_HEIGHT - 1;
            bool grid = (x % 30 == 0) || (y % 30 == 0);
            s_frame[y * LCD_WIDTH + x] = border || grid ? line : bg;
        }
    }
}

static void pet_comparison_pattern(void)
{
    const uint8_t *source = _binary_doraemon_frame0_rgb565a8_start;
    const size_t source_bytes = (size_t)(_binary_doraemon_frame0_rgb565a8_end -
                                         _binary_doraemon_frame0_rgb565a8_start);
    const uint16_t bg = rgb(8, 17, 28);
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            s_frame[y * LCD_WIDTH + x] = bg;
        }
    }
    if (source_bytes != 256 * 256 * 3) {
        ESP_LOGE(TAG, "invalid embedded pet bytes: %u", (unsigned)source_bytes);
        return;
    }

    /* The same verified RGB565+A8 pet is rendered twice. The left copy uses
     * canonical RGB565; the right copy swaps R/B in software. This isolates
     * panel/MADCTL color order from media generation, byte order and scaling. */
    const int size = 112;
    const int top = 34;
    for (int y = 0; y < size; ++y) {
        int sy = y * 255 / (size - 1);
        for (int x = 0; x < size; ++x) {
            int sx = x * 255 / (size - 1);
            size_t index = ((size_t)sy * 256 + sx) * 3;
            uint16_t pet = (uint16_t)source[index] |
                           (uint16_t)((uint16_t)source[index + 1] << 8);
            uint8_t alpha = source[index + 2];
            for (int copy = 0; copy < 2; ++copy) {
                uint16_t value = pet;
                if (copy == 1) {
                    value = (uint16_t)((value & 0x07e0) |
                                       ((value & 0x001f) << 11) |
                                       ((value & 0xf800) >> 11));
                }
                uint8_t pr = (uint8_t)(((value >> 11) & 0x1f) * 255 / 31);
                uint8_t pg = (uint8_t)(((value >> 5) & 0x3f) * 255 / 63);
                uint8_t pb = (uint8_t)((value & 0x1f) * 255 / 31);
                uint8_t r = (uint8_t)(((unsigned)pr * alpha + 8u * (255u - alpha) + 127u) / 255u);
                uint8_t g = (uint8_t)(((unsigned)pg * alpha + 17u * (255u - alpha) + 127u) / 255u);
                uint8_t b = (uint8_t)(((unsigned)pb * alpha + 28u * (255u - alpha) + 127u) / 255u);
                int dx = 5 + copy * 118 + x;
                s_frame[(top + y) * LCD_WIDTH + dx] = rgb(r, g, b);
            }
        }
    }

    const uint16_t swatches[] = {
        rgb(255, 0, 0), rgb(0, 255, 0), rgb(0, 0, 255),
        rgb(255, 255, 255), rgb(255, 255, 0), rgb(0, 255, 255),
    };
    for (int i = 0; i < 6; ++i) {
        for (int y = 186; y < 226; ++y) {
            for (int x = i * 40; x < (i + 1) * 40; ++x) {
                s_frame[y * LCD_WIDTH + x] = swatches[i];
            }
        }
    }
}

static uint16_t wire_word(uint16_t canonical, bool swap_rb, bool swap_bytes)
{
    if (swap_rb) {
        canonical = (uint16_t)((canonical & 0x07e0) |
                               ((canonical & 0x001f) << 11) |
                               ((canonical & 0xf800) >> 11));
    }
    return swap_bytes ? (uint16_t)((canonical << 8) | (canonical >> 8))
                      : canonical;
}

static void pet_four_way_pattern(void)
{
    const uint8_t *source = _binary_doraemon_frame0_rgb565a8_start;
    const size_t source_bytes = (size_t)(_binary_doraemon_frame0_rgb565a8_end -
                                         _binary_doraemon_frame0_rgb565a8_start);
    const uint16_t bg_canonical = (uint16_t)(((8 & 0xf8) << 8) |
                                             ((17 & 0xfc) << 3) | (28 >> 3));
    if (source_bytes != 256 * 256 * 3) {
        ESP_LOGE(TAG, "invalid embedded pet bytes: %u", (unsigned)source_bytes);
        return;
    }

    /* Four complete RGB565 transport hypotheses, rendered at once:
     * TL canonical+byte swap, TR R/B swap+byte swap,
     * BL canonical raw bytes, BR R/B swap raw bytes. */
    for (int quadrant = 0; quadrant < 4; ++quadrant) {
        const bool swap_rb = (quadrant & 1) != 0;
        const bool swap_bytes = (quadrant & 2) == 0;
        const int left = (quadrant & 1) ? 120 : 0;
        const int top = (quadrant & 2) ? 120 : 0;
        const uint16_t bg = wire_word(bg_canonical, swap_rb, swap_bytes);
        for (int y = 0; y < 120; ++y) {
            const int sy = y * 255 / 119;
            for (int x = 0; x < 120; ++x) {
                const int sx = x * 255 / 119;
                const size_t index = ((size_t)sy * 256 + sx) * 3;
                uint16_t pet = (uint16_t)source[index] |
                               (uint16_t)((uint16_t)source[index + 1] << 8);
                const uint8_t alpha = source[index + 2];
                if (swap_rb) {
                    pet = (uint16_t)((pet & 0x07e0) |
                                     ((pet & 0x001f) << 11) |
                                     ((pet & 0xf800) >> 11));
                }
                uint32_t pr = (pet >> 11) & 0x1f;
                uint32_t pg = (pet >> 5) & 0x3f;
                uint32_t pb = pet & 0x1f;
                uint32_t br = (bg_canonical >> 11) & 0x1f;
                uint32_t bgc = (bg_canonical >> 5) & 0x3f;
                uint32_t bb = bg_canonical & 0x1f;
                uint32_t inv = 255u - alpha;
                uint16_t blended = (uint16_t)(
                    (((pr * alpha + br * inv + 127u) / 255u) << 11) |
                    (((pg * alpha + bgc * inv + 127u) / 255u) << 5) |
                    ((pb * alpha + bb * inv + 127u) / 255u));
                s_frame[(top + y) * LCD_WIDTH + left + x] =
                    wire_word(blended, false, swap_bytes);
            }
        }
    }
}

static void pet_colmod_comparison_pattern(void)
{
    const uint8_t *source = _binary_doraemon_frame0_rgb565a8_start;
    const size_t source_bytes = (size_t)(_binary_doraemon_frame0_rgb565a8_end -
                                         _binary_doraemon_frame0_rgb565a8_start);
    const uint16_t bg_canonical = (uint16_t)(((8 & 0xf8) << 8) |
                                             ((17 & 0xfc) << 3) | (28 >> 3));
    if (source_bytes != 256 * 256 * 3) {
        ESP_LOGE(TAG, "invalid embedded pet bytes: %u", (unsigned)source_bytes);
        return;
    }

    /* The previous four-way test showed that software R/B exchange is the
     * blue-looking branch. Keep that and the vendor/LVGL byte swap fixed here,
     * so the only variable is COLMOD (0x65 left, 0x55 right). */
    const uint16_t bg = wire_word(bg_canonical, true, true);
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            s_frame[y * LCD_WIDTH + x] = bg;
        }
    }

    const int pet_size = 118;
    const int pet_top = 8;
    for (int copy = 0; copy < 2; ++copy) {
        const int left = copy * 120 + 1;
        for (int y = 0; y < pet_size; ++y) {
            const int sy = y * 255 / (pet_size - 1);
            for (int x = 0; x < pet_size; ++x) {
                const int sx = x * 255 / (pet_size - 1);
                const size_t index = ((size_t)sy * 256 + sx) * 3;
                uint16_t pet = (uint16_t)source[index] |
                               (uint16_t)((uint16_t)source[index + 1] << 8);
                const uint8_t alpha = source[index + 2];
                pet = (uint16_t)((pet & 0x07e0) |
                                 ((pet & 0x001f) << 11) |
                                 ((pet & 0xf800) >> 11));
                const uint32_t pr = (pet >> 11) & 0x1f;
                const uint32_t pg = (pet >> 5) & 0x3f;
                const uint32_t pb = pet & 0x1f;
                const uint32_t br = (bg_canonical >> 11) & 0x1f;
                const uint32_t bgc = (bg_canonical >> 5) & 0x3f;
                const uint32_t bb = bg_canonical & 0x1f;
                const uint32_t inv = 255u - alpha;
                const uint16_t blended = (uint16_t)(
                    (((pr * alpha + br * inv + 127u) / 255u) << 11) |
                    (((pg * alpha + bgc * inv + 127u) / 255u) << 5) |
                    ((pb * alpha + bb * inv + 127u) / 255u));
                s_frame[(pet_top + y) * LCD_WIDTH + left + x] =
                    wire_word(blended, false, true);
            }
        }
    }

    /* Same six reference colors beneath both pets: red, green, blue, white,
     * yellow, cyan. Correct unpacking must preserve all six distinctly. */
    const uint16_t swatches[] = {
        wire_word(0xf800, true, true), wire_word(0x07e0, true, true),
        wire_word(0x001f, true, true), wire_word(0xffff, true, true),
        wire_word(0xffe0, true, true), wire_word(0x07ff, true, true),
    };
    for (int copy = 0; copy < 2; ++copy) {
        const int copy_left = copy * 120;
        for (int i = 0; i < 6; ++i) {
            for (int y = 150; y < 230; ++y) {
                for (int x = copy_left + i * 20;
                     x < copy_left + (i + 1) * 20; ++x) {
                    s_frame[y * LCD_WIDTH + x] = swatches[i];
                }
            }
        }
    }
}

/* Deterministic follow-up for cases where neither pet looks fully correct.
 * The 4x4 cells are bit 15 at top-left through bit 0 at bottom-right. On a
 * normal RGB565 path they form five red cells, six green cells and five blue
 * cells (or blue/green/red when MADCTL BGR is enabled), with intensity rising
 * toward the more significant bit of each channel. This reveals byte/bit
 * routing errors without relying on the colors of an illustration. */
static void rgb565_bit_mapping_pattern(void)
{
    const uint16_t border = wire_word(0xffff, false, true);
    for (int cell = 0; cell < 16; ++cell) {
        const int bit = 15 - cell;
        const uint16_t value = wire_word((uint16_t)(1u << bit), false, true);
        const int left = (cell % 4) * 60;
        const int top = (cell / 4) * 60;
        for (int y = top; y < top + 60; ++y) {
            for (int x = left; x < left + 60; ++x) {
                const bool edge = x == left || y == top || x == left + 59 ||
                                  y == top + 59;
                s_frame[y * LCD_WIDTH + x] = edge ? border : value;
            }
        }
    }
}

static void full_color_unlock_pattern(void)
{
    const uint8_t *source = _binary_doraemon_frame0_rgb565a8_start;
    const size_t source_bytes = (size_t)(_binary_doraemon_frame0_rgb565a8_end -
                                         _binary_doraemon_frame0_rgb565a8_start);
    const uint16_t bg_canonical = (uint16_t)(((8 & 0xf8) << 8) |
                                             ((17 & 0xfc) << 3) | (28 >> 3));
    for (int y = 0; y < LCD_HEIGHT; ++y) {
        for (int x = 0; x < LCD_WIDTH; ++x) {
            s_frame[y * LCD_WIDTH + x] = wire_word(bg_canonical, false, true);
        }
    }
    if (source_bytes != 256 * 256 * 3) {
        ESP_LOGE(TAG, "invalid embedded pet bytes: %u", (unsigned)source_bytes);
        return;
    }

    /* Keep media canonical and use only the byte swap confirmed by the stock
     * LVGL configuration. This test isolates IDMOFF from all guessed R/B
     * compensation. */
    const int pet_size = 136;
    const int pet_left = 52;
    for (int y = 0; y < pet_size; ++y) {
        const int sy = y * 255 / (pet_size - 1);
        for (int x = 0; x < pet_size; ++x) {
            const int sx = x * 255 / (pet_size - 1);
            const size_t index = ((size_t)sy * 256 + sx) * 3;
            const uint16_t pet = (uint16_t)source[index] |
                                 (uint16_t)((uint16_t)source[index + 1] << 8);
            const uint8_t alpha = source[index + 2];
            const uint32_t inv = 255u - alpha;
            const uint32_t pr = (pet >> 11) & 0x1f;
            const uint32_t pg = (pet >> 5) & 0x3f;
            const uint32_t pb = pet & 0x1f;
            const uint32_t br = (bg_canonical >> 11) & 0x1f;
            const uint32_t bgc = (bg_canonical >> 5) & 0x3f;
            const uint32_t bb = bg_canonical & 0x1f;
            const uint16_t blended = (uint16_t)(
                (((pr * alpha + br * inv + 127u) / 255u) << 11) |
                (((pg * alpha + bgc * inv + 127u) / 255u) << 5) |
                ((pb * alpha + bb * inv + 127u) / 255u));
            s_frame[y * LCD_WIDTH + pet_left + x] =
                wire_word(blended, false, true);
        }
    }

    /* Three 0..255 ramps make low-color mode unmistakable: full color is
     * smooth; idle mode collapses each ramp to two levels. */
    for (int x = 0; x < LCD_WIDTH; ++x) {
        const uint8_t level = (uint8_t)(x * 255 / (LCD_WIDTH - 1));
        const uint16_t ramps[] = {
            (uint16_t)(((level & 0xf8) << 8)),
            (uint16_t)(((level & 0xfc) << 3)),
            (uint16_t)(level >> 3),
        };
        for (int band = 0; band < 3; ++band) {
            for (int y = 150 + band * 30; y < 180 + band * 30; ++y) {
                s_frame[y * LCD_WIDTH + x] = wire_word(ramps[band], false, true);
            }
        }
    }
}

static void init_panel(void)
{
    gpio_config_t backlight_config = {
        .pin_bit_mask = 1ULL << LCD_BACKLIGHT,
        .mode = GPIO_MODE_OUTPUT,
    };
    ESP_ERROR_CHECK(gpio_config(&backlight_config));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 0));

    spi_bus_config_t bus_config = NV3023_PANEL_BUS_SPI_CONFIG(
        LCD_CLK, LCD_MOSI, LCD_HEIGHT * 80 * sizeof(uint16_t));
    ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &bus_config, SPI_DMA_CH_AUTO));

    esp_lcd_panel_io_spi_config_t io_config =
        NV3023_PANEL_IO_SPI_CONFIG(LCD_CS, LCD_DC, NULL, NULL);
    ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi((esp_lcd_spi_bus_handle_t)LCD_HOST,
                                             &io_config, &s_panel_io));

    nv3023_vendor_config_t vendor_config = {
        .init_cmds = s_init,
        .init_cmds_size = sizeof(s_init) / sizeof(s_init[0]),
    };
    esp_lcd_panel_dev_config_t panel_config = {
        .reset_gpio_num = LCD_RST,
        /* Feed canonical RGB565. The previous red/green/blue ramps appeared
         * yellow/magenta/cyan, proving that BGR plus the module's inversion
         * polarity was producing complement(BGR(pixel)). */
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB,
        .bits_per_pixel = 16,
        .vendor_config = &vendor_config,
    };
    ESP_ERROR_CHECK(esp_lcd_new_panel_nv3023(s_panel_io, &panel_config, &s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_reset(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_init(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_swap_xy(s_panel, false));
    ESP_ERROR_CHECK(esp_lcd_panel_mirror(s_panel, false, true));
    /* This assembled panel needs INVON for natural (non-complemented) color. */
    ESP_ERROR_CHECK(esp_lcd_panel_invert_color(s_panel, true));
    /* NV3023A resets into 8-color idle mode. The stock initialization table
     * does not contain IDMOFF, so request full 262K-color display explicitly. */
    ESP_ERROR_CHECK(esp_lcd_panel_io_tx_param(s_panel_io, 0x38, NULL, 0));
    ESP_ERROR_CHECK(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 1));
}

void app_main(void)
{
    init_panel();
    ESP_LOGI(TAG, "NV3023 240x240 full-screen test started, y offset=%d",
             LCD_Y_OFFSET);
    while (true) {
        full_color_unlock_pattern();
        ESP_ERROR_CHECK(draw_frame());
        vTaskDelay(pdMS_TO_TICKS(60000));
    }
}
