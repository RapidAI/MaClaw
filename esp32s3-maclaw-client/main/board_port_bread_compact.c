#include "board_port.h"
#include "font_cjk24.h"

#include <math.h>
#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2s_std.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_vendor.h"
#include "esp_log.h"
#include "nvs.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#define LCD_HOST SPI3_HOST
#define LCD_WIDTH 240
#define LCD_HEIGHT 320
/* This assembled S3 unit uses a 240x320 ST7789 panel. The S3 carrier routes
 * CS and backlight separately on GPIO41/GPIO42; GPIO18 is only the optional
 * lamp output. */
#define LCD_MOSI GPIO_NUM_47
#define LCD_CLK GPIO_NUM_21
#define LCD_DC GPIO_NUM_40
#define LCD_RST GPIO_NUM_45
#define LCD_CS GPIO_NUM_41
#define LCD_BACKLIGHT GPIO_NUM_42

#define BUTTON_BOOT GPIO_NUM_0
#define BUTTON_ACTIVATE GPIO_NUM_0
#define BUTTON_VOLUME_UP GPIO_NUM_38
#define BUTTON_VOLUME_DOWN GPIO_NUM_39
#define MIC_WS GPIO_NUM_4
#define MIC_BCLK GPIO_NUM_5
#define MIC_DIN GPIO_NUM_6
#define SPK_DOUT GPIO_NUM_7
#define SPK_BCLK GPIO_NUM_15
#define SPK_WS GPIO_NUM_16
#define AUDIO_RATE 16000
#define AUDIO_SECONDS 6
#define LCD_STRIPE_ROWS 16
#define THINKING_MOUTH_FRAME_MS 420
#define LCD_FRAME_PIXELS ((size_t)LCD_WIDTH * LCD_HEIGHT)
#define LCD_FRAME_BYTES (LCD_FRAME_PIXELS * sizeof(uint16_t))

static const char *TAG = "maclaw_bread";
static board_port_button_cb_t s_button_cb;
static void *s_button_arg;
static board_port_wake_word_cb_t s_wake_cb;
static void *s_wake_arg;
static esp_lcd_panel_handle_t s_panel;
static i2s_chan_handle_t s_rx;
static i2s_chan_handle_t s_tx;
static SemaphoreHandle_t s_audio_mutex;
static SemaphoreHandle_t s_lcd_mutex;
static SemaphoreHandle_t s_lcd_transfer_done;
static uint16_t *s_framebuffers[2];
static uint16_t *s_render_target;
static uint16_t *s_present_staging;
static unsigned s_front_frame;
static bool s_front_frame_valid;
static bool s_audio_ready;
static bool s_speaker_enabled;
static bool s_audio_stream_owned;
static unsigned s_thinking_mouth_frame;
static bool s_thinking_surface_visible;
static bool s_recording_mode;
static bool s_recording_active;
static bool s_foreground_surface;
static bool s_recording_paused;
static uint32_t s_recording_elapsed;
static uint16_t s_recording_levels[24];
static uint16_t s_recording_smoothed_level;
static bool s_wake_paused;
static char s_state[16] = "idle";
static char s_wifi_ssid[33];
static char s_ambient_time[16];
static char s_ambient_location[24];
static char s_ambient_date[24];
static char s_ambient_weekday[24];
static char s_ambient_weather[32];
static int s_ambient_temperature;
static bool s_ambient_weather_valid;
static bool s_ambient_weather_stale;
static bool s_wifi_connected;
static bool s_gateway_ready;
static unsigned s_output_volume = 70;

#define RESPONSE_TEXT_CAPACITY 2048
#define RESPONSE_LINES_PER_PAGE 6
static bool s_response_active;
static unsigned s_response_page;
static char s_response_title[64];
static char s_response_text[RESPONSE_TEXT_CAPACITY];

extern const uint8_t _binary_cjk24_cjk_bin_start[];
extern const uint8_t _binary_cjk24_cjk_bin_end[];

#define DYNAMIC_GLYPH_BYTES 72
#define DYNAMIC_GLYPH_CACHE_CAPACITY 24
typedef struct {
    uint32_t codepoint;
    uint32_t last_used;
    uint8_t bitmap[DYNAMIC_GLYPH_BYTES];
    bool used;
} compact_dynamic_glyph_t;
static compact_dynamic_glyph_t s_dynamic_glyphs[DYNAMIC_GLYPH_CACHE_CAPACITY];
static uint32_t s_dynamic_glyph_clock;
static portMUX_TYPE s_glyph_lock = portMUX_INITIALIZER_UNLOCKED;

#define UI_GATEWAY_READY_KEY "ui_ready"

static uint16_t state_color(const char *state);
static void present_composed_frame(void);
static void draw_text24_clipped(int x, int y, const char *text,
                                uint16_t fg, uint16_t bg, int max_glyphs);
static void draw_text24_centered(int y, const char *text,
                                 uint16_t fg, uint16_t bg, int max_glyphs);
static void fill_rect_solid(int x, int y, int width, int height, uint16_t fill);

static void persist_gateway_ready(bool ready) {
    nvs_handle_t nvs;
    if (nvs_open("maclaw", NVS_READWRITE, &nvs) != ESP_OK) return;
    if (nvs_set_u8(nvs, UI_GATEWAY_READY_KEY, ready ? 1 : 0) == ESP_OK) (void)nvs_commit(nvs);
    nvs_close(nvs);
}

static bool load_gateway_ready(void) {
    nvs_handle_t nvs;
    uint8_t ready = 0;
    if (nvs_open("maclaw", NVS_READONLY, &nvs) != ESP_OK) return false;
    (void)nvs_get_u8(nvs, UI_GATEWAY_READY_KEY, &ready);
    nvs_close(nvs);
    return ready != 0;
}

static bool lcd_color_transfer_done(esp_lcd_panel_io_handle_t io,
                                    esp_lcd_panel_io_event_data_t *event,
                                    void *user_ctx) {
    (void)io;
    (void)event;
    BaseType_t task_woken = pdFALSE;
    xSemaphoreGiveFromISR((SemaphoreHandle_t)user_ctx, &task_woken);
    return task_woken == pdTRUE;
}

static esp_err_t panel_draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels) {
    while (xSemaphoreTake(s_lcd_transfer_done, 0) == pdTRUE) {}
    esp_err_t err = esp_lcd_panel_draw_bitmap(s_panel, x0, y0, x1, y1, pixels);
    if (err != ESP_OK) return err;
    return xSemaphoreTake(s_lcd_transfer_done, pdMS_TO_TICKS(1000)) == pdTRUE
               ? ESP_OK : ESP_ERR_TIMEOUT;
}

static esp_err_t draw_bitmap_sync(int x0, int y0, int x1, int y1, const void *pixels) {
    if (!s_render_target) return panel_draw_bitmap_sync(x0, y0, x1, y1, pixels);
    if (!pixels || x0 < 0 || y0 < 0 || x1 > LCD_WIDTH || y1 > LCD_HEIGHT ||
        x1 <= x0 || y1 <= y0) return ESP_ERR_INVALID_ARG;
    int width = x1 - x0;
    int height = y1 - y0;
    const uint16_t *source = pixels;
    for (int row = 0; row < height; ++row) {
        memcpy(s_render_target + (size_t)(y0 + row) * LCD_WIDTH + x0,
               source + (size_t)row * width, (size_t)width * sizeof(uint16_t));
    }
    return ESP_OK;
}

static bool begin_composed_frame(void) {
    if (!s_framebuffers[0] || !s_framebuffers[1] || !s_present_staging) return false;
    s_render_target = s_framebuffers[s_front_frame ^ 1u];
    return true;
}

static bool begin_screen_frame(void) {
    if (begin_composed_frame()) return true;
    ESP_LOGW(TAG, "LCD framebuffer unavailable; drawing screen directly");
    return false;
}

static void finish_screen_frame(bool composed) {
    if (composed) present_composed_frame();
}

static void present_composed_frame(void) {
    if (!s_render_target) return;
    unsigned back = s_front_frame ^ 1u;
    uint16_t *next = s_framebuffers[back];
    uint16_t *previous = s_framebuffers[s_front_frame];
    s_render_target = NULL;
    for (int y = 0; y < LCD_HEIGHT; y += LCD_STRIPE_ROWS) {
        int rows = LCD_HEIGHT - y < LCD_STRIPE_ROWS ? LCD_HEIGHT - y : LCD_STRIPE_ROWS;
        int first_changed = -1;
        int last_changed = -1;
        for (int row = 0; row < rows; ++row) {
            const uint16_t *next_row = next + (size_t)(y + row) * LCD_WIDTH;
            const uint16_t *old_row = previous + (size_t)(y + row) * LCD_WIDTH;
            if (!s_front_frame_valid ||
                memcmp(next_row, old_row, LCD_WIDTH * sizeof(uint16_t)) != 0) {
                if (first_changed < 0) first_changed = row;
                last_changed = row;
            }
        }
        if (first_changed < 0) continue;
        int changed_rows = last_changed - first_changed + 1;
        size_t bytes = (size_t)LCD_WIDTH * changed_rows * sizeof(uint16_t);
        memcpy(s_present_staging,
               next + (size_t)(y + first_changed) * LCD_WIDTH, bytes);
        ESP_ERROR_CHECK_WITHOUT_ABORT(panel_draw_bitmap_sync(
            0, y + first_changed, LCD_WIDTH, y + first_changed + changed_rows,
            s_present_staging));
    }
    s_front_frame = back;
    s_front_frame_valid = true;
}

static uint16_t color(uint8_t r, uint8_t g, uint8_t b) {
    uint16_t p = (uint16_t)(((r & 0xf8) << 8) | ((g & 0xfc) << 3) | (b >> 3));
    return (uint16_t)((p << 8) | (p >> 8));
}

static void fill_screen(uint16_t c) {
    if (!s_panel) return;
    if (s_render_target) {
        for (size_t i = 0; i < LCD_FRAME_PIXELS; ++i) s_render_target[i] = c;
        return;
    }
    uint16_t *line = heap_caps_malloc(LCD_WIDTH * 16 * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!line) return;
    for (size_t i = 0; i < LCD_WIDTH * 16; ++i) line[i] = c;
    for (int y = 0; y < LCD_HEIGHT; y += 16) {
        int y2 = y + 16 < LCD_HEIGHT ? y + 16 : LCD_HEIGHT;
        draw_bitmap_sync(0, y, LCD_WIDTH, y2, line);
    }
    heap_caps_free(line);
}

static const uint8_t *glyph5x7(char c) {
    static const uint8_t blank[5] = {0, 0, 0, 0, 0};
    static const uint8_t question[5] = {2, 1, 0x59, 9, 6};
    static const uint8_t colon[5] = {0, 0x36, 0x36, 0, 0};
    static const uint8_t slash[5] = {0x20, 0x10, 0x08, 0x04, 0x02};
    static const uint8_t percent[5] = {0x63, 0x13, 0x08, 0x64, 0x63};
    static const uint8_t dash[5] = {0x08, 0x08, 0x08, 0x08, 0x08};
    static const uint8_t dot[5] = {0, 0x60, 0x60, 0, 0};
    static const uint8_t letters[][5] = {
        {0x7E,0x11,0x11,0x11,0x7E},{0x7F,0x49,0x49,0x49,0x36},{0x3E,0x41,0x41,0x41,0x22},
        {0x7F,0x41,0x41,0x22,0x1C},{0x7F,0x49,0x49,0x49,0x41},{0x7F,0x09,0x09,0x09,0x01},
        {0x3E,0x41,0x49,0x49,0x7A},{0x7F,0x08,0x08,0x08,0x7F},{0,0x41,0x7F,0x41,0},
        {0x20,0x40,0x41,0x3F,0x01},{0x7F,0x08,0x14,0x22,0x41},{0x7F,0x40,0x40,0x40,0x40},
        {0x7F,0x02,0x0C,0x02,0x7F},{0x7F,0x04,0x08,0x10,0x7F},{0x3E,0x41,0x41,0x41,0x3E},
        {0x7F,0x09,0x09,0x09,0x06},{0x3E,0x41,0x51,0x21,0x5E},{0x7F,0x09,0x19,0x29,0x46},
        {0x46,0x49,0x49,0x49,0x31},{0x01,0x01,0x7F,0x01,0x01},{0x3F,0x40,0x40,0x40,0x3F},
        {0x1F,0x20,0x40,0x20,0x1F},{0x3F,0x40,0x38,0x40,0x3F},{0x63,0x14,0x08,0x14,0x63},
        {0x03,0x04,0x78,0x04,0x03},{0x61,0x51,0x49,0x45,0x43},
    };
    static const uint8_t digits[][5] = {
        {0x3E,0x51,0x49,0x45,0x3E},{0,0x42,0x7F,0x40,0},{0x42,0x61,0x51,0x49,0x46},
        {0x21,0x41,0x45,0x4B,0x31},{0x18,0x14,0x12,0x7F,0x10},{0x27,0x45,0x45,0x45,0x39},
        {0x3C,0x4A,0x49,0x49,0x30},{0x01,0x71,0x09,0x05,0x03},{0x36,0x49,0x49,0x49,0x36},
        {0x06,0x49,0x49,0x29,0x1E},
    };
    if (c >= 'a' && c <= 'z') c -= 'a' - 'A';
    if (c >= 'A' && c <= 'Z') return letters[c - 'A'];
    if (c >= '0' && c <= '9') return digits[c - '0'];
    if (c == ' ') return blank;
    if (c == ':') return colon;
    if (c == '/') return slash;
    if (c == '%') return percent;
    if (c == '-') return dash;
    if (c == '.') return dot;
    return question;
}

static void draw_ascii_at(int x0, int y, const char *text, uint16_t fg, uint16_t bg) {
    if (!text || !*text) return;
    const int scale = 3;
    size_t len = strlen(text);
    if (len > 12) len = 12;
    int width = (int)len * 6 * scale;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * 7 * scale * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int py = 0; py < 7 * scale; ++py) {
        for (int px = 0; px < width; ++px) bitmap[py * width + px] = bg;
    }
    for (size_t ch = 0; ch < len; ++ch) {
        const uint8_t *g = glyph5x7(text[ch]);
        for (int gx = 0; gx < 5; ++gx) for (int gy = 0; gy < 7; ++gy) {
            if (!(g[gx] & (1u << gy))) continue;
            for (int sx = 0; sx < scale; ++sx) for (int sy = 0; sy < scale; ++sy) {
                int x = (int)ch * 6 * scale + gx * scale + sx;
                bitmap[(gy * scale + sy) * width + x] = fg;
            }
        }
    }
    if (x0 < 0) x0 = 0;
    if (x0 + width > LCD_WIDTH) x0 = LCD_WIDTH - width;
    draw_bitmap_sync(x0, y, x0 + width, y + 7 * scale, bitmap);
    heap_caps_free(bitmap);
}

static void draw_ascii_centered(int y, const char *text, uint16_t fg, uint16_t bg) {
    size_t len = text ? strlen(text) : 0;
    if (len > 12) len = 12;
    draw_ascii_at((LCD_WIDTH - (int)len * 18) / 2, y, text, fg, bg);
}

static uint32_t utf8_next(const char **cursor) {
    const uint8_t *s = (const uint8_t *)*cursor;
    if (!*s) return 0;
    uint32_t cp;
    size_t count;
    if (s[0] < 0x80) { cp = s[0]; count = 1; }
    else if ((s[0] & 0xe0) == 0xc0 && s[1] && (s[1] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x1f) << 6) | (s[1] & 0x3f); count = 2;
    } else if ((s[0] & 0xf0) == 0xe0 && s[1] && s[2] &&
               (s[1] & 0xc0) == 0x80 && (s[2] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x0f) << 12) | ((s[1] & 0x3f) << 6) | (s[2] & 0x3f); count = 3;
    } else if ((s[0] & 0xf8) == 0xf0 && s[1] && s[2] && s[3] &&
               (s[1] & 0xc0) == 0x80 && (s[2] & 0xc0) == 0x80 &&
               (s[3] & 0xc0) == 0x80) {
        cp = ((s[0] & 0x07) << 18) | ((s[1] & 0x3f) << 12) |
             ((s[2] & 0x3f) << 6) | (s[3] & 0x3f); count = 4;
    } else { cp = '?'; count = 1; }
    *cursor += count;
    return cp;
}

static const uint32_t *cjk24_rows(uint32_t codepoint) {
    for (size_t i = 0; i < sizeof(s_maclaw_cjk24) / sizeof(s_maclaw_cjk24[0]); ++i) {
        if (s_maclaw_cjk24[i].codepoint == codepoint) return s_maclaw_cjk24[i].rows;
    }
    return NULL;
}

static bool full_cjk24_copy(uint32_t codepoint, uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    if (codepoint < 0x4E00 || codepoint >= 0xA000) return false;
    size_t offset = (size_t)(codepoint - 0x4E00) * DYNAMIC_GLYPH_BYTES;
    size_t available = (size_t)(_binary_cjk24_cjk_bin_end - _binary_cjk24_cjk_bin_start);
    if (offset + DYNAMIC_GLYPH_BYTES > available) return false;
    memcpy(bitmap, _binary_cjk24_cjk_bin_start + offset, DYNAMIC_GLYPH_BYTES);
    return true;
}

static bool dynamic_glyph_copy(uint32_t codepoint, uint8_t bitmap[DYNAMIC_GLYPH_BYTES]) {
    bool found = false;
    taskENTER_CRITICAL(&s_glyph_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            memcpy(bitmap, s_dynamic_glyphs[i].bitmap, DYNAMIC_GLYPH_BYTES);
            s_dynamic_glyphs[i].last_used = ++s_dynamic_glyph_clock;
            found = true;
            break;
        }
    }
    taskEXIT_CRITICAL(&s_glyph_lock);
    return found;
}

static bool glyph24_pixel(uint32_t codepoint, const uint32_t *rows,
                          const uint8_t *dynamic, int row, int col) {
    if (rows) return (rows[row] & (1u << (23 - col))) != 0;
    if (dynamic) return (dynamic[row * 3 + col / 8] & (1u << (7 - col % 8))) != 0;
    return codepoint < 0x80 && row < 14 && col < 10 &&
           (glyph5x7((char)codepoint)[col / 2] & (1u << (row / 2)));
}

static int text24_advance(uint32_t codepoint) {
    // Measure and render Latin with one source of truth. Hand-grouping narrow
    // and wide letters still made mixed-case prose look uneven and caused the
    // wrapper to disagree with what was actually painted.
    if (codepoint >= 0x20 && codepoint <= 0x7E)
        return s_maclaw_ascii24_advance[codepoint - 0x20];
    return 25;
}

static bool response_break(uint32_t codepoint) {
    return codepoint == '\n' || codepoint == '\r';
}

// Returns the byte after one panel-width line. The line width uses the exact
// glyph advances of the renderer, so mixed Chinese, punctuation and numbers
// wrap without dropping UTF-8 bytes or splitting a character.
static const char *response_next_line(const char *cursor, char *line, size_t line_size) {
    if (!line || line_size == 0) return cursor;
    line[0] = '\0';
    if (!cursor) return cursor;
    int width = 0;
    size_t used = 0;
    while (*cursor) {
        const char *before = cursor;
        uint32_t cp = utf8_next(&cursor);
        if (response_break(cp)) {
            while (*cursor == '\n' || *cursor == '\r') ++cursor;
            break;
        }
        int advance = text24_advance(cp);
        if (width + advance > LCD_WIDTH - 28 && used > 0) {
            cursor = before;
            break;
        }
        size_t bytes = (size_t)(cursor - before);
        if (used + bytes >= line_size) {
            cursor = before;
            break;
        }
        memcpy(line + used, before, bytes);
        used += bytes;
        width += advance;
    }
    line[used] = '\0';
    if (!used && *cursor) {
        const char *before = cursor;
        (void)utf8_next(&cursor);
        size_t bytes = (size_t)(cursor - before);
        if (bytes < line_size) {
            memcpy(line, before, bytes);
            line[bytes] = '\0';
        }
    }
    return cursor;
}

static unsigned response_page_count(void) {
    const char *cursor = s_response_text;
    unsigned lines = 0;
    char line[96];
    while (*cursor && lines < 240) {
        const char *next = response_next_line(cursor, line, sizeof(line));
        if (next == cursor) break;
        cursor = next;
        ++lines;
    }
    return lines ? (lines + RESPONSE_LINES_PER_PAGE - 1) / RESPONSE_LINES_PER_PAGE : 1;
}

static void draw_response_page(void) {
    const uint16_t bg = color(8, 17, 28);
    const uint16_t header = color(14, 31, 47);
    const uint16_t footer = color(11, 24, 38);
    const uint16_t accent = color(76, 168, 207);
    const uint16_t title = color(244, 248, 251);
    const uint16_t body = color(214, 227, 237);
    const uint16_t muted = color(145, 172, 191);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    fill_rect_solid(0, 0, LCD_WIDTH, 60, header);
    fill_rect_solid(14, 18, 18, 42, accent);
    draw_text24_clipped(28, 18, s_response_title[0] ? s_response_title : "处理结果",
                        title, header, 8);
    fill_rect_solid(14, 59, LCD_WIDTH - 14, 1, color(31, 62, 82));

    const char *cursor = s_response_text;
    unsigned skip = s_response_page * RESPONSE_LINES_PER_PAGE;
    char line[96];
    while (*cursor && skip--) cursor = response_next_line(cursor, line, sizeof(line));
    for (int row = 0; row < RESPONSE_LINES_PER_PAGE && *cursor; ++row) {
        cursor = response_next_line(cursor, line, sizeof(line));
        // response_next_line() already clips by the actual pixel width.  The
        // former ten-glyph cap silently discarded the rest of longer Latin
        // lines (for example "ROUTE SOURCE" became "ROUTE SOUR").
        draw_text24_clipped(14, 72 + row * 33, line, body, bg, 32);
    }

    unsigned pages = response_page_count();
    char indicator[16];
    snprintf(indicator, sizeof(indicator), "%u / %u", s_response_page + 1, pages);
    fill_rect_solid(0, 278, LCD_WIDTH, 42, footer);
    draw_text24_clipped(14, 287, pages > 1 ? "音量翻页" : "激活键返回",
                        muted, footer, 4);
    // The old centered page number occupied the same horizontal band as the
    // Chinese hint. Anchor it at the right edge in the compact ASCII renderer.
    const int indicator_width = (int)strlen(indicator) * 18;
    draw_ascii_at(LCD_WIDTH - 12 - indicator_width, 289, indicator, accent, footer);
    finish_screen_frame(composed);
}

static int text24_width(const char *text, int max_glyphs) {
    int width = 0;
    const char *cursor = text ? text : "";
    for (int count = 0; *cursor && count < max_glyphs; ++count) {
        width += text24_advance(utf8_next(&cursor));
    }
    return width > 0 ? width - 1 : 0;
}

static void draw_text24_clipped(int x, int y, const char *text, uint16_t fg, uint16_t bg,
                                int max_glyphs) {
    if (!text || !*text || y < 0 || y + 24 > LCD_HEIGHT) return;
    int width = text24_width(text, max_glyphs);
    if (x < 0) x = 0;
    if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
    if (width <= 0) return;
    const int stripe_rows = 8;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * stripe_rows * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int strip = 0; strip < 24; strip += stripe_rows) {
        for (int i = 0; i < width * stripe_rows; ++i) bitmap[i] = bg;
        const char *cursor = text;
        int pen = 0;
        for (int count = 0; *cursor && count < max_glyphs; ++count) {
            uint32_t cp = utf8_next(&cursor);
            const uint32_t *rows = cjk24_rows(cp);
            uint8_t dynamic_bitmap[DYNAMIC_GLYPH_BYTES];
            const uint8_t *dynamic = !rows &&
                                             (dynamic_glyph_copy(cp, dynamic_bitmap) ||
                                              full_cjk24_copy(cp, dynamic_bitmap))
                                         ? dynamic_bitmap : NULL;
            for (int row = strip; row < strip + stripe_rows; ++row) {
                for (int col = 0; col < 24; ++col) {
                    int px = pen + col;
                    if (px >= 0 && px < width && glyph24_pixel(cp, rows, dynamic, row, col)) {
                        bitmap[(row - strip) * width + px] = fg;
                    }
                }
            }
            pen += text24_advance(cp);
        }
        draw_bitmap_sync(x, y + strip, x + width, y + strip + stripe_rows, bitmap);
    }
    heap_caps_free(bitmap);
}

static void draw_text24_centered(int y, const char *text, uint16_t fg, uint16_t bg,
                                 int max_glyphs) {
    int width = text24_width(text, max_glyphs);
    draw_text24_clipped((LCD_WIDTH - width) / 2, y, text, fg, bg, max_glyphs);
}

static void draw_progress_bar(int x, int y, int width, int height, unsigned value,
                              uint16_t track, uint16_t fill) {
    if (value > 100) value = 100;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * height * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    int filled = width * (int)value / 100;
    for (int py = 0; py < height; ++py) {
        for (int px = 0; px < width; ++px) bitmap[py * width + px] = px < filled ? fill : track;
    }
    draw_bitmap_sync(x, y, x + width, y + height, bitmap);
    heap_caps_free(bitmap);
}

static void fill_rect_solid(int x, int y, int width, int height, uint16_t fill) {
    if (width <= 0 || height <= 0) return;
    if (s_render_target) {
        if (x < 0) { width += x; x = 0; }
        if (y < 0) { height += y; y = 0; }
        if (x + width > LCD_WIDTH) width = LCD_WIDTH - x;
        if (y + height > LCD_HEIGHT) height = LCD_HEIGHT - y;
        for (int row = 0; row < height; ++row) {
            uint16_t *dst = s_render_target + (size_t)(y + row) * LCD_WIDTH + x;
            for (int col = 0; col < width; ++col) dst[col] = fill;
        }
        return;
    }
    const int stripe_rows = 12;
    uint16_t *bitmap = heap_caps_malloc((size_t)width * stripe_rows * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!bitmap) return;
    for (int i = 0; i < width * stripe_rows; ++i) bitmap[i] = fill;
    for (int py = 0; py < height; py += stripe_rows) {
        int rows = height - py < stripe_rows ? height - py : stripe_rows;
        draw_bitmap_sync(x, y + py, x + width, y + py + rows, bitmap);
    }
    heap_caps_free(bitmap);
}

static bool robot_rounded_box(float x, float y, float left, float top,
                              float right, float bottom, float radius) {
    float cx = x < left + radius ? left + radius :
               (x > right - radius ? right - radius : x);
    float cy = y < top + radius ? top + radius :
               (y > bottom - radius ? bottom - radius : y);
    float dx = x - cx;
    float dy = y - cy;
    return dx * dx + dy * dy <= radius * radius;
}

static uint16_t robot_rgb_mix(uint8_t ar, uint8_t ag, uint8_t ab,
                              uint8_t br, uint8_t bg, uint8_t bb, unsigned mix) {
    if (mix > 255) mix = 255;
    unsigned inv = 255 - mix;
    return color((uint8_t)((ar * inv + br * mix) / 255),
                 (uint8_t)((ag * inv + bg * mix) / 255),
                 (uint8_t)((ab * inv + bb * mix) / 255));
}

static void draw_robot_face_at(const char *state, int offset_x, int offset_y,
                               int scale_percent, uint16_t bg) {
    const bool listening = !strcmp(state, "listening");
    const bool thinking = !strcmp(state, "thinking");
    const bool speaking = !strcmp(state, "speaking");
    const bool alert = !strcmp(state, "alert");
    const bool done = !strcmp(state, "done");
    const uint8_t glow_r = alert ? 255 : thinking ? 177 : speaking || done ? 72 : 55;
    const uint8_t glow_g = alert ? 86 : thinking ? 140 : speaking || done ? 238 : 218;
    const uint8_t glow_b = alert ? 86 : thinking ? 255 : speaking || done ? 158 : 246;
    int width = (240 * scale_percent + 99) / 100;
    int height = (210 * scale_percent + 99) / 100;
    uint16_t *bitmap = NULL;
    size_t bitmap_bytes = (size_t)width * height * sizeof(uint16_t);
    // During double-buffered composition the robot bitmap is copied into a
    // PSRAM frame and never reaches SPI DMA directly. Requiring scarce
    // internal DMA memory here made the head disappear under TLS/HTTP load.
    if (s_render_target) {
        bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!bitmap) bitmap = heap_caps_malloc(bitmap_bytes, MALLOC_CAP_DMA);
    if (!bitmap) {
        ESP_LOGE(TAG, "robot bitmap allocation failed: %u bytes", (unsigned)bitmap_bytes);
        return;
    }

    for (int py = 0; py < height; ++py) {
        float y = ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            float x = ((float)px + 0.5f) * 100.0f / scale_percent;
            uint16_t pixel = bg;

            /* Collar, antenna, and softly rounded ear pods. */
            if (robot_rounded_box(x, y, 91, 181, 149, 207, 11))
                pixel = robot_rgb_mix(38, 67, 91, 96, 133, 158, (unsigned)(175 - y / 2));
            if (robot_rounded_box(x, y, 114, 23, 126, 58, 6))
                pixel = robot_rgb_mix(52, 82, 105, 173, 204, 220, (unsigned)(220 - y * 2));
            float antenna_dx = x - 120.0f, antenna_dy = y - 18.0f;
            float antenna_d2 = antenna_dx * antenna_dx + antenna_dy * antenna_dy;
            if (antenna_d2 < 13.0f * 13.0f)
                pixel = antenna_d2 < 8.0f * 8.0f
                            ? color(glow_r, glow_g, glow_b)
                            : robot_rgb_mix(18, 52, 72, glow_r, glow_g, glow_b, 130);
            float ear_ly = y - 117.0f, ear_lx = x - 26.0f, ear_rx = x - 214.0f;
            float ear_ld2 = ear_lx * ear_lx + ear_ly * ear_ly;
            float ear_rd2 = ear_rx * ear_rx + ear_ly * ear_ly;
            if (ear_ld2 < 27.0f * 27.0f || ear_rd2 < 27.0f * 27.0f) {
                float d2 = ear_ld2 < ear_rd2 ? ear_ld2 : ear_rd2;
                pixel = d2 < 18.0f * 18.0f
                            ? robot_rgb_mix(25, 56, 79, glow_r, glow_g, glow_b, 76)
                            : color(69, 103, 128);
            }

            /* A pearl-metal shell with real RGB565 shading instead of flat blocks. */
            bool outer = robot_rounded_box(x, y, 27, 43, 213, 194, 43);
            bool inner = robot_rounded_box(x, y, 35, 51, 205, 186, 36);
            if (outer) pixel = color(50, 83, 109);
            if (inner) {
                int shade = (int)(205 - y * 54 / 186 - x * 20 / 240);
                if (shade < 116) shade = 116;
                pixel = color((uint8_t)(shade + 22), (uint8_t)(shade + 42),
                              (uint8_t)(shade + 55));
                if (x + y < 136) pixel = robot_rgb_mix(180, 207, 220, 240, 252, 255, 108);
            }

            /* Deep glass face, blue edge light, and a restrained reflection. */
            bool glass_edge = robot_rounded_box(x, y, 47, 67, 193, 171, 29);
            bool glass = robot_rounded_box(x, y, 53, 73, 187, 165, 23);
            if (glass_edge) pixel = robot_rgb_mix(24, 69, 93, glow_r, glow_g, glow_b, 70);
            if (glass) {
                unsigned glass_mix = (unsigned)((y - 73) * 65 / 92);
                pixel = robot_rgb_mix(13, 38, 61, 4, 17, 32, glass_mix);
                if (y < 91 && x + y < 230)
                    pixel = robot_rgb_mix(13, 38, 61, 70, 113, 138, 48);
            }

            if (glass) {
                float eye_y = thinking ? 104.0f : 116.0f;
                float eye_shift = thinking ? -3.0f : 0.0f;
                float left_dx = (x - (87.0f + eye_shift)) / 17.0f;
                float right_dx = (x - (153.0f + eye_shift)) / 17.0f;
                float eye_dy = (y - eye_y) / (alert ? 8.0f : 16.0f);
                float left_d = left_dx * left_dx + eye_dy * eye_dy;
                float right_d = right_dx * right_dx + eye_dy * eye_dy;
                float eye_d = left_d < right_d ? left_d : right_d;

                if (done) {
                    float left_curve = 109.0f + fabsf(x - 87.0f) * 0.30f;
                    float right_curve = 109.0f + fabsf(x - 153.0f) * 0.30f;
                    if ((fabsf(y - left_curve) < 3 && fabsf(x - 87) < 19) ||
                        (fabsf(y - right_curve) < 3 && fabsf(x - 153) < 19))
                        pixel = color(glow_r, glow_g, glow_b);
                } else if (eye_d < 1.55f) {
                    if (eye_d < 1.0f) pixel = color(glow_r, glow_g, glow_b);
                    else pixel = robot_rgb_mix(7, 25, 42, glow_r, glow_g, glow_b, 86);
                    if (!alert && ((x < 87 && x > 78) || (x < 153 && x > 144)) &&
                        y > eye_y - 9 && y < eye_y - 3)
                        pixel = color(236, 255, 255);
                }

                /* Cheeks and distinct mouth language for every application state. */
                float cheek_l = (x - 67) * (x - 67) + (y - 143) * (y - 143);
                float cheek_r = (x - 173) * (x - 173) + (y - 143) * (y - 143);
                if (cheek_l < 7 * 7 || cheek_r < 7 * 7)
                    pixel = robot_rgb_mix(8, 27, 44, glow_r, glow_g, glow_b, 82);
                if (listening) {
                    float md = (x - 120) * (x - 120) / 17.0f / 17.0f +
                               (y - 145) * (y - 145) / 12.0f / 12.0f;
                    if (md < 1.0f && md > 0.38f) pixel = color(glow_r, glow_g, glow_b);
                } else if (thinking) {
                    const float dot_x[3] = {107.0f, 120.0f, 133.0f};
                    for (unsigned i = 0; i < 3; ++i) {
                        float dx = x - dot_x[i];
                        float dy = y - 146.0f;
                        float radius = i == s_thinking_mouth_frame ? 4.5f : 2.5f;
                        if (dx * dx + dy * dy < radius * radius) {
                            pixel = i == s_thinking_mouth_frame
                                        ? color(242, 249, 255)
                                        : robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 112);
                        }
                    }
                } else if (alert) {
                    if (robot_rounded_box(x, y, 91, 141, 149, 148, 3))
                        pixel = color(glow_r, glow_g, glow_b);
                } else {
                    float smile_y = 139.0f + (x - 120.0f) * (x - 120.0f) / 430.0f;
                    if (fabsf(y - smile_y) < (speaking ? 3.5f : 2.5f) && fabsf(x - 120) < 29)
                        pixel = color(glow_r, glow_g, glow_b);
                    if (speaking && y > 145 && y < 151 && fabsf(x - 120) < 19)
                        pixel = robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 178);
                }
            }

            /* Two physical status lamps make the head read as a real device. */
            float lamp_l = (x - 57) * (x - 57) + (y - 184) * (y - 184);
            float lamp_r = (x - 183) * (x - 183) + (y - 184) * (y - 184);
            if (lamp_l < 6 * 6 || lamp_r < 6 * 6)
                pixel = color(glow_r, glow_g, glow_b);
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    draw_bitmap_sync(offset_x, offset_y, offset_x + width, offset_y + height, bitmap);
    heap_caps_free(bitmap);
}

/* Animate only the small glass-mouth region. Repainting the complete 240x320
 * framebuffer for three dots wastes SPI bandwidth and can make the otherwise
 * double-buffered thinking page appear to pulse under TLS load. */
static void draw_thinking_mouth_frame(void) {
    const int source_left = 99;
    const int source_top = 137;
    const int source_width = 42;
    const int source_height = 18;
    const int scale_percent = 92;
    const int output_x = 10 + source_left * scale_percent / 100;
    const int output_y = source_top * scale_percent / 100;
    const int width = (source_width * scale_percent + 99) / 100;
    const int height = (source_height * scale_percent + 99) / 100;
    uint16_t *bitmap = s_present_staging;
    const uint8_t glow_r = 177, glow_g = 140, glow_b = 255;

    // esp_lcd queues DMA from the supplied buffer. A task-stack bitmap can be
    // reclaimed while the transfer is still completing and corrupt the task
    // stack, which caused the reset seen on the first animation frame. Reuse
    // the board's persistent internal-DMA staging allocation instead.
    if (!bitmap) return;

    for (int py = 0; py < height; ++py) {
        float y = source_top + ((float)py + 0.5f) * 100.0f / scale_percent;
        for (int px = 0; px < width; ++px) {
            float x = source_left + ((float)px + 0.5f) * 100.0f / scale_percent;
            unsigned glass_mix = (unsigned)((y - 73.0f) * 65.0f / 92.0f);
            uint16_t pixel = robot_rgb_mix(13, 38, 61, 4, 17, 32, glass_mix);
            const float dot_x[3] = {107.0f, 120.0f, 133.0f};
            for (unsigned i = 0; i < 3; ++i) {
                float dx = x - dot_x[i];
                float dy = y - 146.0f;
                float radius = i == s_thinking_mouth_frame ? 4.5f : 2.5f;
                if (dx * dx + dy * dy < radius * radius) {
                    pixel = i == s_thinking_mouth_frame
                                ? color(242, 249, 255)
                                : robot_rgb_mix(8, 25, 40, glow_r, glow_g, glow_b, 112);
                }
            }
            bitmap[(size_t)py * width + px] = pixel;
        }
    }
    draw_bitmap_sync(output_x, output_y, output_x + width, output_y + height, bitmap);

    // Keep the front framebuffer authoritative. Every later double-buffered
    // screen starts by comparing against it; without this patch the mouth
    // animation lives only on the LCD and can be unexpectedly restored by a
    // subsequent partial presentation.
    if (s_front_frame_valid && s_framebuffers[s_front_frame]) {
        uint16_t *front = s_framebuffers[s_front_frame];
        for (int row = 0; row < height; ++row) {
            memcpy(front + (size_t)(output_y + row) * LCD_WIDTH + output_x,
                   bitmap + (size_t)row * width, (size_t)width * sizeof(uint16_t));
        }
    }
}

static void thinking_mouth_task(void *arg) {
    (void)arg;
    TickType_t next_frame = xTaskGetTickCount();
    while (true) {
        vTaskDelayUntil(&next_frame, pdMS_TO_TICKS(THINKING_MOUTH_FRAME_MS));
        xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
        if (s_thinking_surface_visible && !s_recording_active &&
            !s_response_active && s_foreground_surface &&
            !strcmp(s_state, "thinking")) {
            s_thinking_mouth_frame = (s_thinking_mouth_frame + 1) % 3;
            draw_thinking_mouth_frame();
        }
        xSemaphoreGiveRecursive(s_lcd_mutex);
    }
}

static void show_state_screen(const char *state) {
    s_thinking_surface_visible = false;
    uint16_t bg = state_color(state);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    bool ambient = !strcmp(state, "idle") || !strcmp(state, "quiet");
    if (ambient) {
        draw_ascii_centered(8, s_ambient_time[0] ? s_ambient_time : "--:--:--",
                            color(240, 248, 255), bg);
        char calendar[64];
        snprintf(calendar, sizeof(calendar), "%s %s", s_ambient_date, s_ambient_weekday);
        draw_text24_centered(38, calendar, color(166, 194, 216), bg, 10);
        // Keep a clean 66-pixel header for clock/calendar. The previous face
        // started at y=45 after scaling, so its antenna painted through the
        // calendar row. This compact version starts below the header.
        draw_robot_face_at(state, 54, 60, 55, bg);
        draw_text24_centered(188, "码卡龙已就绪", color(242, 249, 255), bg, 8);
        char weather[96];
        if (s_ambient_weather_valid) {
            snprintf(weather, sizeof(weather), "%s %s %dC%s", s_ambient_location,
                     s_ambient_weather, s_ambient_temperature,
                     s_ambient_weather_stale ? " *" : "");
        } else {
            snprintf(weather, sizeof(weather), "%s 天气同步中", s_ambient_location);
        }
        draw_text24_centered(226, weather, color(121, 210, 224), bg, 10);
        draw_text24_centered(278, s_gateway_ready ? "在线" :
                             (s_wifi_connected ? "服务连接中" : "网络连接中"),
                             s_gateway_ready ? color(91, 224, 149) : color(245, 184, 75), bg, 9);
    } else {
        const char *label = !strcmp(state, "listening") ? "正在听取" :
                            !strcmp(state, "thinking") ? "正在思考" :
                            !strcmp(state, "speaking") ? "正在回复" :
                            !strcmp(state, "alert") ? "请注意" :
                            !strcmp(state, "done") ? "处理完成" : "码卡龙";
        draw_robot_face_at(state, 10, 0, 92, bg);
        draw_text24_centered(226, label, color(255, 255, 255), bg, 8);
        draw_text24_centered(274, "请稍候", color(145, 220, 235), bg, 8);
    }
    finish_screen_frame(composed);
    s_thinking_surface_visible = !ambient && !strcmp(state, "thinking");
}

static void show_status(const char *title, const char *line) {
    s_thinking_surface_visible = false;
    uint16_t bg = state_color(s_state);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    // Message/ready surfaces use the same visual identity as the reusable pet
    // states. Keeping the face here avoids the old bare "MACLAW / READY" page
    // while still leaving two calm, high-contrast rows for status copy.
    draw_robot_face_at(s_state, 54, 4, 55, bg);
    draw_text24_centered(190, title && title[0] ? title : "码卡龙",
                         color(248, 252, 255), bg, 9);
    draw_text24_centered(236, line && line[0] ? line : "设备已就绪",
                         color(121, 210, 224), bg, 10);
    draw_text24_centered(280, "请使用激活键", color(157, 184, 205), bg, 8);
    finish_screen_frame(composed);
}

static void lcd_startup_pattern(void) {
    if (!s_panel) return;
    const uint16_t bars[] = {
        color(255, 0, 0), color(0, 255, 0), color(0, 80, 255),
        color(255, 255, 255), color(0, 0, 0), color(255, 190, 0),
    };
    const int bar_height = LCD_HEIGHT / (int)(sizeof(bars) / sizeof(bars[0]));
    uint16_t *line = heap_caps_malloc(LCD_WIDTH * 16 * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!line) return;
    for (size_t bar = 0; bar < sizeof(bars) / sizeof(bars[0]); ++bar) {
        for (size_t i = 0; i < LCD_WIDTH * 16; ++i) line[i] = bars[bar];
        int y_start = (int)bar * bar_height;
        int y_end = bar + 1 == sizeof(bars) / sizeof(bars[0]) ? LCD_HEIGHT : y_start + bar_height;
        for (int y = y_start; y < y_end; y += 16) {
            int y2 = y + 16 < y_end ? y + 16 : y_end;
            draw_bitmap_sync(0, y, LCD_WIDTH, y2, line);
        }
    }
    heap_caps_free(line);
}

static uint16_t state_color(const char *state) {
    if (!strcmp(state, "listening")) return color(22, 120, 255);
    if (!strcmp(state, "thinking")) return color(140, 75, 235);
    if (!strcmp(state, "speaking")) return color(16, 185, 115);
    if (!strcmp(state, "alert")) return color(225, 55, 55);
    if (!strcmp(state, "done")) return color(30, 160, 100);
    return color(18, 24, 38);
}

static esp_err_t audio_init(void) {
    if (s_audio_ready) return ESP_OK;
    i2s_chan_config_t cfg = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
    cfg.dma_desc_num = 8;
    cfg.dma_frame_num = 256;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&cfg, NULL, &s_rx), TAG, "mic channel");
    i2s_std_config_t mic = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_MSB_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT, I2S_SLOT_MODE_MONO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = MIC_BCLK, .ws = MIC_WS,
                     .dout = I2S_GPIO_UNUSED, .din = MIC_DIN, .invert_flags = {0}},
    };
    mic.slot_cfg.slot_mask = I2S_STD_SLOT_LEFT;
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_rx, &mic), TAG, "mic mode");
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_rx), TAG, "mic enable");

    cfg.id = I2S_NUM_1;
    /* Once a descriptor has been transmitted it must become silence. Without
     * this, an underrun can replay the final PCM block until TX is stopped. */
    cfg.auto_clear_after_cb = true;
    ESP_RETURN_ON_ERROR(i2s_new_channel(&cfg, &s_tx, NULL), TAG, "speaker channel");
    i2s_std_config_t speaker = {
        .clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(AUDIO_RATE),
        .slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO),
        .gpio_cfg = {.mclk = I2S_GPIO_UNUSED, .bclk = SPK_BCLK, .ws = SPK_WS,
                     .dout = SPK_DOUT, .din = I2S_GPIO_UNUSED, .invert_flags = {0}},
    };
    ESP_RETURN_ON_ERROR(i2s_channel_init_std_mode(s_tx, &speaker), TAG, "speaker mode");
    /* Keep BCLK/WS running for the direct-I2S amplifier. Repeatedly enabling
     * and disabling TX made some Bread Compact amplifiers miss an entire short
     * playback while their serial input was waking. auto_clear_after_cb above
     * makes every completed descriptor silence, so an enabled idle channel no
     * longer repeats the final tone. */
    ESP_RETURN_ON_ERROR(i2s_channel_enable(s_tx), TAG, "speaker enable");
    s_speaker_enabled = true;
    s_audio_ready = true;
    ESP_LOGI(TAG, "Bread Compact direct-I2S audio ready (continuous clocks, silent idle)");
    return ESP_OK;
}

static esp_err_t read_mono(int16_t *mono, size_t capacity, size_t *read, uint16_t *level) {
    int32_t raw[512];
    size_t bytes = 0;
    ESP_RETURN_ON_ERROR(i2s_channel_read(s_rx, raw, sizeof(raw), &bytes, pdMS_TO_TICKS(1000)), TAG, "mic read");
    size_t count = bytes / sizeof(raw[0]);
    if (count > capacity) count = capacity;
    int32_t peak = 0;
    for (size_t i = 0; i < count; ++i) {
        int16_t sample = (int16_t)(raw[i] >> 14);
        mono[i] = sample;
        int32_t magnitude = sample < 0 ? -(int32_t)sample : sample;
        if (magnitude > peak) peak = magnitude;
    }
    if (read) *read = count;
    if (level) *level = peak > 12000 ? 1000 : (uint16_t)(peak * 1000 / 12000);
    return ESP_OK;
}

static void button_task(void *arg) {
    (void)arg;
    bool previous = gpio_get_level(BUTTON_ACTIVATE) != 0;
    bool volume_up_stable = gpio_get_level(BUTTON_VOLUME_UP) != 0;
    bool volume_down_stable = gpio_get_level(BUTTON_VOLUME_DOWN) != 0;
    // The two side keys are not guaranteed to share the same electrical
    // polarity on Bread Compact revisions. Treat each settled boot level as
    // its own released state so GPIO38 and GPIO39 both dispatch a press edge.
    const bool volume_up_idle = volume_up_stable;
    const bool volume_down_idle = volume_down_stable;
    bool volume_up_candidate = volume_up_stable;
    bool volume_down_candidate = volume_down_stable;
    int64_t volume_up_changed_at = 0;
    int64_t volume_down_changed_at = 0;
    int64_t pressed_at = 0;
    int64_t short_pending_at = 0;
    bool long_sent = false;
    ESP_LOGI(TAG, "side key idle levels: volume_up(GPIO%d)=%d volume_down(GPIO%d)=%d",
             BUTTON_VOLUME_UP, volume_up_idle, BUTTON_VOLUME_DOWN, volume_down_idle);
    while (true) {
        int64_t now = esp_timer_get_time();
        bool released = gpio_get_level(BUTTON_ACTIVATE) != 0;
        if (previous && !released) {
            pressed_at = now;
            long_sent = false;
        }
        /* Fire while the key is still held. Waiting for release made a valid
         * long press look unresponsive and also allowed contact bounce on the
         * release edge to turn it into an ordinary short press. */
        if (!released && pressed_at && !long_sent && now - pressed_at >= 2500000) {
            long_sent = true;
            short_pending_at = 0;
            ESP_LOGI(TAG, "activate long hold detected");
            if (s_button_cb) s_button_cb(BOARD_BUTTON_LONG, s_button_arg);
        }
        if (!previous && released && pressed_at) {
            int64_t duration = now - pressed_at;
            if (long_sent || duration >= 2500000) {
                short_pending_at = 0;
            } else if (short_pending_at && now - short_pending_at <= 500000) {
                short_pending_at = 0;
                if (s_button_cb) s_button_cb(BOARD_BUTTON_DOUBLE, s_button_arg);
            } else {
                short_pending_at = now;
            }
            pressed_at = 0;
        }
        previous = released;
        if (short_pending_at && now - short_pending_at > 500000) {
            short_pending_at = 0;
            if (s_button_cb) s_button_cb(BOARD_BUTTON_SHORT, s_button_arg);
        }

        bool volume_up_level = gpio_get_level(BUTTON_VOLUME_UP) != 0;
        if (volume_up_level != volume_up_candidate) {
            volume_up_candidate = volume_up_level;
            volume_up_changed_at = now;
        }
        if (volume_up_stable != volume_up_candidate && volume_up_changed_at &&
            now - volume_up_changed_at >= 30000) {
            volume_up_stable = volume_up_candidate;
            // Dispatch on the debounced press edge. Waiting for release made
            // short taps easy to lose when one 20 ms scan landed inside the
            // contact bounce window, which left paging apparently inert.
            if (volume_up_stable != volume_up_idle) {
                ESP_LOGI(TAG, "volume up key pressed (GPIO%d level=%d idle=%d)",
                         BUTTON_VOLUME_UP, volume_up_stable, volume_up_idle);
                if (s_button_cb) s_button_cb(BOARD_INPUT_VOLUME_UP, s_button_arg);
            }
        }

        bool volume_down_level = gpio_get_level(BUTTON_VOLUME_DOWN) != 0;
        if (volume_down_level != volume_down_candidate) {
            volume_down_candidate = volume_down_level;
            volume_down_changed_at = now;
        }
        if (volume_down_stable != volume_down_candidate && volume_down_changed_at &&
            now - volume_down_changed_at >= 30000) {
            volume_down_stable = volume_down_candidate;
            if (volume_down_stable != volume_down_idle) {
                ESP_LOGI(TAG, "volume down key pressed (GPIO%d level=%d idle=%d)",
                         BUTTON_VOLUME_DOWN, volume_down_stable, volume_down_idle);
                if (s_button_cb) s_button_cb(BOARD_INPUT_VOLUME_DOWN, s_button_arg);
            }
        }
        vTaskDelay(pdMS_TO_TICKS(20));
    }
}

esp_err_t board_port_init(board_port_button_cb_t cb, void *arg) {
    s_button_cb = cb;
    s_button_arg = arg;
    s_gateway_ready = load_gateway_ready();
    s_audio_mutex = xSemaphoreCreateMutex();
    if (!s_audio_mutex) return ESP_ERR_NO_MEM;
    s_lcd_mutex = xSemaphoreCreateRecursiveMutex();
    if (!s_lcd_mutex) return ESP_ERR_NO_MEM;
    s_lcd_transfer_done = xSemaphoreCreateBinary();
    if (!s_lcd_transfer_done) return ESP_ERR_NO_MEM;
    gpio_config_t backlight = {.pin_bit_mask = 1ULL << LCD_BACKLIGHT, .mode = GPIO_MODE_OUTPUT};
    ESP_ERROR_CHECK(gpio_config(&backlight));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 0));
    spi_bus_config_t bus = {.mosi_io_num = LCD_MOSI, .miso_io_num = GPIO_NUM_NC,
                            .sclk_io_num = LCD_CLK, .quadwp_io_num = GPIO_NUM_NC,
                            .quadhd_io_num = GPIO_NUM_NC,
                            .max_transfer_sz = LCD_WIDTH * 16 * sizeof(uint16_t)};
    ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &bus, SPI_DMA_CH_AUTO));
    esp_lcd_panel_io_handle_t io = NULL;
    esp_lcd_panel_io_spi_config_t io_cfg = {.cs_gpio_num = LCD_CS, .dc_gpio_num = LCD_DC,
        .spi_mode = 3, .pclk_hz = 20 * 1000 * 1000, .trans_queue_depth = 10,
        .lcd_cmd_bits = 8, .lcd_param_bits = 8,
        .on_color_trans_done = lcd_color_transfer_done, .user_ctx = s_lcd_transfer_done};
    ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi(LCD_HOST, &io_cfg, &io));
    esp_lcd_panel_dev_config_t panel_cfg = {.reset_gpio_num = LCD_RST,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB, .bits_per_pixel = 16};
    ESP_ERROR_CHECK(esp_lcd_new_panel_st7789(io, &panel_cfg, &s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_reset(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_init(s_panel));
    ESP_ERROR_CHECK(esp_lcd_panel_invert_color(s_panel, true));
    ESP_ERROR_CHECK(esp_lcd_panel_disp_on_off(s_panel, true));
    ESP_ERROR_CHECK(gpio_set_level(LCD_BACKLIGHT, 1));
    for (size_t i = 0; i < 2; ++i) {
        s_framebuffers[i] = heap_caps_malloc(LCD_FRAME_BYTES,
                                             MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (!s_framebuffers[i]) {
            ESP_LOGW(TAG, "cannot allocate LCD framebuffer %u", (unsigned)i);
            break;
        }
        memset(s_framebuffers[i], 0, LCD_FRAME_BYTES);
    }
    s_present_staging = heap_caps_malloc(
        (size_t)LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t),
        MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL);
    if (!s_framebuffers[0] || !s_framebuffers[1] || !s_present_staging) {
        for (size_t i = 0; i < 2; ++i) {
            if (s_framebuffers[i]) heap_caps_free(s_framebuffers[i]);
            s_framebuffers[i] = NULL;
        }
        if (s_present_staging) heap_caps_free(s_present_staging);
        s_present_staging = NULL;
        ESP_LOGW(TAG, "LCD double buffering disabled: insufficient memory");
    } else {
        ESP_LOGI(TAG, "LCD double buffering ready: %u bytes PSRAM + %u bytes DMA",
                 (unsigned)(LCD_FRAME_BYTES * 2),
                 (unsigned)(LCD_WIDTH * LCD_STRIPE_ROWS * sizeof(uint16_t)));
    }
    lcd_startup_pattern();
    vTaskDelay(pdMS_TO_TICKS(1500));
    fill_screen(state_color("idle"));
    gpio_config_t button = {.pin_bit_mask = (1ULL << BUTTON_ACTIVATE) |
                                            (1ULL << BUTTON_VOLUME_UP) | (1ULL << BUTTON_VOLUME_DOWN),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE};
    ESP_ERROR_CHECK(gpio_config(&button));
    ESP_RETURN_ON_ERROR(audio_init(), TAG, "audio init");
    if (xTaskCreate(button_task, "bread_button", 3072, NULL, 4, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    // The mouth animation is decorative state feedback. Give its floating-point
    // renderer enough stack headroom, but never make the essential buttons,
    // display, microphone, or speaker unavailable if this task cannot start.
    if (xTaskCreate(thinking_mouth_task, "bread_thinking", 3072, NULL, 2, NULL) != pdPASS) {
        ESP_LOGW(TAG, "thinking mouth animation disabled: cannot create task");
    }
    return ESP_OK;
}

esp_err_t board_port_adjust_output_volume(int delta_percent, unsigned *out_percent) {
    int next = (int)s_output_volume + delta_percent;
    if (next < 0) next = 0;
    if (next > 100) next = 100;
    s_output_volume = (unsigned)next;
    if (out_percent) *out_percent = s_output_volume;
    return ESP_OK;
}

void board_port_set_pet_state(const char *state) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    const char *next_state = state ? state : "idle";
    bool ambient = !strcmp(next_state, "idle") || !strcmp(next_state, "quiet");
    // A completed answer remains the foreground owner until the application
    // explicitly dismisses it. Late idle/quiet state updates must not erase it.
    if (ambient && s_response_active && s_foreground_surface) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    strlcpy(s_state, next_state, sizeof(s_state));
    if (!strcmp(next_state, "thinking")) s_thinking_mouth_frame = 0;
    if (!s_recording_active) {
        s_response_active = false;
        s_foreground_surface = !ambient;
        show_state_screen(s_state);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_command_display_lock(bool locked) {s_foreground_surface = locked;}
void board_port_set_command_cancel_enabled(bool enabled) {(void)enabled;}
void board_port_set_pet_profile(const char *skin, bool motion_enabled) {(void)skin;(void)motion_enabled;}
void board_port_set_recording_mode(bool meeting) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_recording_mode = meeting;
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_recording_visual(bool active, bool paused, uint32_t elapsed) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    bool new_session = active && !s_recording_active;
    bool visual_changed = !active || new_session || paused != s_recording_paused ||
                          elapsed != s_recording_elapsed;
    s_recording_active = active;
    if (active) s_thinking_surface_visible = false;
    if (active) s_response_active = false;
    s_foreground_surface = active;
    s_recording_paused = active && paused;
    s_recording_elapsed = active ? elapsed : 0;
    if (!active) {
        memset(s_recording_levels, 0, sizeof(s_recording_levels));
        s_recording_smoothed_level = 0;
        show_state_screen(s_state);
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    if (new_session) {
        memset(s_recording_levels, 0, sizeof(s_recording_levels));
        s_recording_smoothed_level = 0;
    }
    if (!visual_changed) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    uint16_t bg = color(10,19,30);
    uint16_t accent = paused ? color(244,178,58) : color(241,76,85);
    uint16_t cyan = color(72,205,220);
    uint16_t muted = color(91,118,138);
    bool composed = begin_screen_frame();
    fill_screen(bg);
    fill_rect_solid(16, 16, 208, 4, accent);
    fill_rect_solid(16, 300, 208, 4, accent);
    fill_rect_solid(28, 43, 20, 20, accent);
    fill_rect_solid(34, 49, 8, 8, color(255,235,238));
    draw_text24_clipped(62, 42, paused ? "已暂停" : "正在听取",
                        color(245,250,255), bg, 7);
    draw_text24_centered(78, s_recording_mode ? "会议录音" : "语音指令",
                         paused ? color(244,178,58) : cyan, bg, 8);
    char timer[16];
    snprintf(timer, sizeof(timer), "%02lu:%02lu", (unsigned long)(elapsed / 60), (unsigned long)(elapsed % 60));
    draw_ascii_centered(112, timer, color(255,255,255), bg);
    fill_rect_solid(20, 158, 200, 1, muted);
    for (int column = 0; column < 24; ++column) {
        uint16_t level = paused ? 0 : s_recording_levels[column];
        if (level > 1000) level = 1000;
        int half = 2 + (int)(level * 42u / 1000u);
        int x = 22 + column * 8;
        fill_rect_solid(x, 205 - half, 5, half * 2 + 1, paused ? muted : cyan);
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
    draw_ascii_centered(226, level_label, paused ? muted : cyan, bg);
    draw_text24_centered(260, s_recording_mode ? "按激活键停止保存" : "说完后自动处理",
                         color(163,188,207), bg, 9);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_audio_level(uint16_t level, uint32_t elapsed) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_recording_active) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    s_recording_smoothed_level = level > s_recording_smoothed_level
                                     ? (uint16_t)((s_recording_smoothed_level + level * 3u) / 4u)
                                     : (uint16_t)((s_recording_smoothed_level * 7u + level) / 8u);
    memmove(&s_recording_levels[0], &s_recording_levels[1],
            (24 - 1) * sizeof(s_recording_levels[0]));
    s_recording_levels[23] = s_recording_smoothed_level;
    uint16_t bg = color(10,19,30);
    uint16_t cyan = color(72,205,220);
    uint16_t muted = color(91,118,138);
    bool composed = begin_screen_frame();
    if (composed) {
        memcpy(s_render_target, s_framebuffers[s_front_frame], LCD_FRAME_BYTES);
    }
    fill_rect_solid(18, 160, 204, 90, bg);
    fill_rect_solid(20, 205, 200, 1, muted);
    for (int column = 0; column < 24; ++column) {
        uint16_t history_level = s_recording_paused ? 0 : s_recording_levels[column];
        if (history_level > 1000) history_level = 1000;
        int half = 2 + (int)(history_level * 42u / 1000u);
        int x = 22 + column * 8;
        fill_rect_solid(x, 205 - half, 5, half * 2 + 1,
                        s_recording_paused ? muted : cyan);
    }
    char level_label[20];
    snprintf(level_label, sizeof(level_label), "MIC %u%%",
             (unsigned)(s_recording_smoothed_level / 10u));
    draw_ascii_centered(226, level_label, s_recording_paused ? muted : cyan, bg);
    finish_screen_frame(composed);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_push_recording_pcm(const int16_t *samples, size_t count) {
    (void)samples;
    (void)count;
    // The direct-I2S port already feeds its normalized level into the shared
    // 24-column waveform history; raw PCM remains available for future renderers.
}
void board_port_show_text(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    ESP_LOGI(TAG, "display: %s | %s", title?title:"", text?text:"");
    s_response_active = false;
    s_foreground_surface = true;
    if (title && !strcmp(title, "SETUP MODE")) {
        show_status("设备设置", "正在准备配置");
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return;
    }
    bool ready = title && text &&
                 (!strcmp(title, "MACLAW") || !strcmp(title, "码卡龙")) &&
                 (!strcmp(text, "READY") || !strcmp(text, "Ready") ||
                  !strcmp(text, "ready"));
    if (ready) {
        strlcpy(s_state, "idle", sizeof(s_state));
        s_foreground_surface = false;
        show_state_screen(s_state);
    } else {
        show_status(title, text);
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_upload_progress(size_t done, size_t total, const char *stage) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    uint16_t bg = color(9,35,64);
    unsigned percent = total ? (unsigned)(done * 100 / total) : 0;
    s_response_active = false;
    s_foreground_surface = true;
    bool composed = begin_screen_frame();
    fill_screen(bg);
    draw_text24_centered(66, "会议录音", color(255,255,255), bg, 8);
    draw_text24_centered(112, stage && stage[0] ? stage : "正在上传", color(170,215,235), bg, 9);
    draw_progress_bar(24, 184, 192, 18, percent, color(28,80,111), color(72,205,220));
    char label[16]; snprintf(label, sizeof(label), "%u%%", percent > 100 ? 100 : percent);
    draw_ascii_centered(226, label, color(255,255,255), bg);
    /* Keep the comma with the continuation line. A comma stranded after
     * “上传中” reads like broken Chinese punctuation on the compact panel. */
    draw_text24_centered(260, "上传中", color(150,195,215), bg, 9);
    draw_text24_centered(290, "，请勿断电", color(150,195,215), bg, 9);
    finish_screen_frame(composed);
    ESP_LOGI(TAG,"upload %u/%u %s",(unsigned)done,(unsigned)total,stage?stage:"");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_response(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = true;
    s_response_page = 0;
    strlcpy(s_response_title, title && title[0] ? title : "码卡龙", sizeof(s_response_title));
    strlcpy(s_response_text, text && text[0] ? text : "没有收到文字回复", sizeof(s_response_text));
    draw_response_page();
    ESP_LOGI(TAG, "response pages=%u: %s", response_page_count(), s_response_text);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

bool board_port_navigate_response(int page_delta) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    if (!s_response_active || page_delta == 0) {
        xSemaphoreGiveRecursive(s_lcd_mutex);
        return false;
    }
    unsigned pages = response_page_count();
    if (pages > 1) {
        int next = (int)s_response_page + page_delta;
        // Wrap at both ends so every press produces visible feedback instead
        // of being silently swallowed on the first or last page.
        if (next < 0) next = (int)pages - 1;
        if (next >= (int)pages) next = 0;
        if ((unsigned)next != s_response_page) {
            s_response_page = (unsigned)next;
            draw_response_page();
            ESP_LOGI(TAG, "response page changed: %u/%u", s_response_page + 1, pages);
        }
    }
    xSemaphoreGiveRecursive(s_lcd_mutex);
    return true;
}
int board_port_cache_glyph(uint32_t codepoint, const uint8_t bitmap[72]) {
    if (!bitmap || codepoint < 0x20 || codepoint > 0xffff) return 0;
    size_t replacement = 0;
    uint32_t oldest = UINT32_MAX;
    taskENTER_CRITICAL(&s_glyph_lock);
    for (size_t i = 0; i < DYNAMIC_GLYPH_CACHE_CAPACITY; ++i) {
        if (s_dynamic_glyphs[i].used && s_dynamic_glyphs[i].codepoint == codepoint) {
            replacement = i; oldest = 0; break;
        }
        if (!s_dynamic_glyphs[i].used) { replacement = i; oldest = 0; break; }
        if (s_dynamic_glyphs[i].last_used < oldest) {
            oldest = s_dynamic_glyphs[i].last_used; replacement = i;
        }
    }
    s_dynamic_glyphs[replacement].codepoint = codepoint;
    memcpy(s_dynamic_glyphs[replacement].bitmap, bitmap, DYNAMIC_GLYPH_BYTES);
    s_dynamic_glyphs[replacement].last_used = ++s_dynamic_glyph_clock;
    s_dynamic_glyphs[replacement].used = true;
    taskEXIT_CRITICAL(&s_glyph_lock);
    return 1;
}
void board_port_show_qrcode(esp_qrcode_handle_t qrcode, const char *ssid) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_thinking_surface_visible = false;
    s_foreground_surface = true;
    s_response_active = false;
    int modules = esp_qrcode_get_size(qrcode);
    int scale = modules > 0 ? 180 / (modules + 8) : 0;
    if (scale < 1) { board_port_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    int side = (modules + 8) * scale;
    uint16_t *qr = heap_caps_malloc((size_t)side * side * sizeof(uint16_t), MALLOC_CAP_DMA);
    if (!qr) { board_port_show_text("setup", ssid); xSemaphoreGiveRecursive(s_lcd_mutex); return; }
    for (int y = 0; y < side; ++y) for (int x = 0; x < side; ++x) {
        int mx = x / scale - 4, my = y / scale - 4;
        bool dark = mx >= 0 && my >= 0 && mx < modules && my < modules &&
                    esp_qrcode_get_module(qrcode, mx, my);
        qr[y * side + x] = dark ? color(0, 0, 0) : color(255, 255, 255);
    }
    fill_screen(color(255, 255, 255));
    int qr_y = (LCD_HEIGHT - side) / 2 - 20;
    draw_bitmap_sync((LCD_WIDTH - side) / 2, qr_y, (LCD_WIDTH + side) / 2, qr_y + side, qr);
    heap_caps_free(qr);
    draw_ascii_centered(LCD_HEIGHT - 42, "WIFI SETUP", color(0, 0, 0), color(255, 255, 255));
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_show_ready_prompt(const char *title, const char *text) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    s_wifi_connected = true;
    s_gateway_ready = true;
    s_response_active = false;
    persist_gateway_ready(true);
    strlcpy(s_state, "idle", sizeof(s_state));
    s_foreground_surface = false;
    show_state_screen(s_state);
    /* Guard against a queued idle/ambient repaint produced just before the
     * successful handshake. Re-assert the ready footer after that older draw
     * has drained from the LCD task. */
    vTaskDelay(pdMS_TO_TICKS(120));
    show_state_screen(s_state);
    ESP_LOGI(TAG, "ready: %s | %s", title ? title : "", text ? text : "");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_cancel_ready_prompt(void) {}
bool board_port_wake_from_idle(void) {return false;}
void board_port_set_wifi_status(const char *ssid, bool connected) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    strlcpy(s_wifi_ssid, ssid ? ssid : "", sizeof(s_wifi_ssid));
    /* Once the authenticated gateway is ready, a transient STA disconnect
     * must not demote the UI. The networking layer reconnects independently. */
    if (!s_gateway_ready || connected) s_wifi_connected = connected;
    if (!s_recording_active && !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) show_state_screen(s_state);
    ESP_LOGI(TAG,"wifi %s %s",ssid?ssid:"",connected?"on":"off");
    xSemaphoreGiveRecursive(s_lcd_mutex);
}
void board_port_set_ambient(const char *time, const char *location, const char *date, const char *weekday,
 const char *weather, int temp, bool valid, bool stale) {
    xSemaphoreTakeRecursive(s_lcd_mutex, portMAX_DELAY);
    strlcpy(s_ambient_time, time ? time : "", sizeof(s_ambient_time));
    strlcpy(s_ambient_location, location ? location : "", sizeof(s_ambient_location));
    strlcpy(s_ambient_date, date ? date : "", sizeof(s_ambient_date));
    strlcpy(s_ambient_weekday, weekday ? weekday : "", sizeof(s_ambient_weekday));
    strlcpy(s_ambient_weather, weather ? weather : "", sizeof(s_ambient_weather));
    s_ambient_temperature = temp;
    s_ambient_weather_valid = valid;
    s_ambient_weather_stale = stale;
    if (!s_recording_active && !s_foreground_surface &&
        (!strcmp(s_state, "idle") || !strcmp(s_state, "quiet"))) show_state_screen(s_state);
    xSemaphoreGiveRecursive(s_lcd_mutex);
}

esp_err_t board_port_audio_stream_start(void) {
    board_port_pause_wake_word(true);
    if (xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(5000)) != pdTRUE) {
        board_port_pause_wake_word(false);
        return ESP_ERR_TIMEOUT;
    }
    esp_err_t err = audio_init();
    if (err != ESP_OK) {
        xSemaphoreGive(s_audio_mutex);
        board_port_pause_wake_word(false);
        return err;
    }
    s_audio_stream_owned = true;
    return ESP_OK;
}
esp_err_t board_port_audio_stream_read(int16_t *mono, size_t capacity, size_t *read, uint16_t *level) {
    if (!mono || !read) return ESP_ERR_INVALID_ARG;
    return read_mono(mono, capacity, read, level);
}
void board_port_audio_stream_stop(void) {
    if (s_audio_stream_owned) {
        s_audio_stream_owned = false;
        xSemaphoreGive(s_audio_mutex);
    }
    board_port_pause_wake_word(false);
}
void board_port_pause_wake_word(bool paused) {s_wake_paused = paused;}
esp_err_t board_port_start_wake_word(board_port_wake_word_cb_t cb, void *arg) {s_wake_cb=cb;s_wake_arg=arg;(void)s_wake_cb;(void)s_wake_arg;return ESP_OK;}
esp_err_t board_port_stop_wake_word(void) {s_wake_cb=NULL;s_wake_arg=NULL;return ESP_OK;}

esp_err_t board_port_capture_wav(uint8_t **out, size_t *out_len) {
    if (!out || !out_len) return ESP_ERR_INVALID_ARG;
    *out=NULL;*out_len=0;
    if (xSemaphoreTake(s_audio_mutex,pdMS_TO_TICKS(1500))!=pdTRUE) return ESP_ERR_TIMEOUT;
    size_t samples=AUDIO_RATE*AUDIO_SECONDS, len=44+samples*2;
    uint8_t *wav=heap_caps_malloc(len,MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT);
    if(!wav){xSemaphoreGive(s_audio_mutex);return ESP_ERR_NO_MEM;}
    memset(wav,0,44);memcpy(wav,"RIFF",4);uint32_t v=len-8;memcpy(wav+4,&v,4);memcpy(wav+8,"WAVEfmt ",8);
    v=16;memcpy(wav+16,&v,4);uint16_t s=1;memcpy(wav+20,&s,2);memcpy(wav+22,&s,2);v=AUDIO_RATE;memcpy(wav+24,&v,4);
    v=AUDIO_RATE*2;memcpy(wav+28,&v,4);s=2;memcpy(wav+32,&s,2);s=16;memcpy(wav+34,&s,2);memcpy(wav+36,"data",4);v=samples*2;memcpy(wav+40,&v,4);
    int16_t *pcm = (int16_t *)(wav + 44);
    size_t done = 0;
    uint16_t smoothed_level = 0;
    uint32_t last_ui_second = UINT32_MAX;
    while (done < samples) {
        size_t got = 0;
        uint16_t level = 0;
        esp_err_t err = read_mono(pcm + done, samples - done, &got, &level);
        if (err != ESP_OK) {
            free(wav);
            xSemaphoreGive(s_audio_mutex);
            return err;
        }
        if (got == 0) continue;
        done += got;
        smoothed_level = level > smoothed_level
                             ? (uint16_t)((smoothed_level + level * 3u) / 4u)
                             : (uint16_t)((smoothed_level * 7u + level) / 8u);
        uint32_t elapsed = (uint32_t)(done / AUDIO_RATE);
        // The normal six-second command capture is synchronous, unlike the
        // meeting stream. Feed the same shared recording UI from this loop so
        // its timer and waveform remain live on every supported board.
        board_port_push_recording_pcm(pcm + done - got, got);
        board_port_set_audio_level(smoothed_level, elapsed);
        if (elapsed != last_ui_second) {
            board_port_set_recording_visual(true, false, elapsed);
            last_ui_second = elapsed;
        }
    }
    xSemaphoreGive(s_audio_mutex);*out=wav;*out_len=len;return ESP_OK;
}

static esp_err_t write_stereo(const int16_t *source, size_t frames, unsigned channels) {
    int16_t stereo[512];
    size_t done = 0;
    while (done < frames) {
        size_t count = frames - done;
        if (count > 256) count = 256;
        for (size_t i = 0; i < count; ++i) {
            int32_t left = source[(done + i) * channels];
            int32_t right = channels == 2 ? source[(done + i) * 2 + 1] : left;
            stereo[i * 2] = (int16_t)(left * (int32_t)s_output_volume / 100);
            stereo[i * 2 + 1] = (int16_t)(right * (int32_t)s_output_volume / 100);
        }
        size_t written = 0;
        size_t expected = count * 2 * sizeof(int16_t);
        esp_err_t err = i2s_channel_write(s_tx, stereo, expected, &written, pdMS_TO_TICKS(1000));
        if (err != ESP_OK) return err;
        if (written != expected) return ESP_ERR_TIMEOUT;
        done += count;
    }
    return ESP_OK;
}

static esp_err_t speaker_play_begin(void) {
    if (!s_audio_ready || !s_tx || !s_speaker_enabled) return ESP_ERR_INVALID_STATE;
    return ESP_OK;
}

static esp_err_t speaker_play_end(esp_err_t playback_err) {
    if (playback_err != ESP_OK) return playback_err;
    /* Queue at least one complete DMA ring of silence. i2s_channel_write can
     * return while the last speech descriptor is still queued; filling a full
     * ring forces that descriptor onto the wire before this call returns and
     * leaves all reusable buffers silent. Keep TX enabled so the amplifier
     * remains locked and ready for the next prompt. */
    int16_t silence[256] = {0};
    for (unsigned block = 0; block < 8; ++block) {
        esp_err_t err = write_stereo(silence, 256, 1);
        if (err != ESP_OK) return err;
    }
    return ESP_OK;
}

esp_err_t board_port_play_wav(const uint8_t *wav, size_t len) {
    if (!wav || len < 44 || memcmp(wav, "RIFF", 4) != 0 ||
        memcmp(wav + 8, "WAVE", 4) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    const uint8_t *format_chunk = NULL;
    const uint8_t *audio_data = NULL;
    size_t format_size = 0;
    size_t audio_size = 0;
    for (size_t offset = 12; offset + 8 <= len;) {
        const uint8_t *chunk = wav + offset;
        uint32_t chunk_size = (uint32_t)chunk[4] | ((uint32_t)chunk[5] << 8) |
                              ((uint32_t)chunk[6] << 16) | ((uint32_t)chunk[7] << 24);
        offset += 8;
        if (chunk_size > len - offset) return ESP_ERR_INVALID_SIZE;
        if (memcmp(chunk, "fmt ", 4) == 0) {
            format_chunk = wav + offset;
            format_size = chunk_size;
        } else if (memcmp(chunk, "data", 4) == 0) {
            audio_data = wav + offset;
            audio_size = chunk_size;
        }
        size_t padded = (size_t)chunk_size + (chunk_size & 1u);
        if (padded > len - offset) return ESP_ERR_INVALID_SIZE;
        offset += padded;
    }
    if (!format_chunk || format_size < 16 || !audio_data || !audio_size) {
        return ESP_ERR_INVALID_ARG;
    }
    uint16_t format = (uint16_t)format_chunk[0] | ((uint16_t)format_chunk[1] << 8);
    uint16_t channels = (uint16_t)format_chunk[2] | ((uint16_t)format_chunk[3] << 8);
    uint32_t rate = (uint32_t)format_chunk[4] | ((uint32_t)format_chunk[5] << 8) |
                    ((uint32_t)format_chunk[6] << 16) | ((uint32_t)format_chunk[7] << 24);
    uint16_t bits = (uint16_t)format_chunk[14] | ((uint16_t)format_chunk[15] << 8);
    if (format != 1 || bits != 16 || (channels != 1 && channels != 2) || rate != AUDIO_RATE) {
        return ESP_ERR_NOT_SUPPORTED;
    }
    size_t frame_bytes = channels * sizeof(int16_t);
    if (audio_size % frame_bytes != 0) return ESP_ERR_INVALID_SIZE;
    size_t frames = audio_size / frame_bytes;
    const int16_t *samples = (const int16_t *)audio_data;
    int32_t peak = 0;
    uint64_t square_sum = 0;
    size_t sample_count = frames * channels;
    for (size_t i = 0; i < sample_count; ++i) {
        int32_t sample = samples[i];
        int32_t magnitude = sample < 0 ? -sample : sample;
        if (magnitude > peak) peak = magnitude;
        square_sum += (uint64_t)((int64_t)sample * sample);
    }
    unsigned rms = sample_count
                       ? (unsigned)sqrt((double)square_sum / (double)sample_count)
                       : 0;
    ESP_LOGI(TAG,
             "WAV playback start: %u Hz %u-bit %u ch frames=%u duration=%ums peak=%ld rms=%u volume=%u%%",
             (unsigned)rate, (unsigned)bits, (unsigned)channels, (unsigned)frames,
             (unsigned)((frames * 1000u) / rate), (long)peak, rms, s_output_volume);
    if (xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
    int64_t started_at = esp_timer_get_time();
    esp_err_t err = speaker_play_begin();
    if (err == ESP_OK) {
        err = write_stereo(samples, frames, channels);
        err = speaker_play_end(err);
    }
    xSemaphoreGive(s_audio_mutex);
    ESP_LOGI(TAG, "WAV playback end: result=%s elapsed=%lldms",
             esp_err_to_name(err), (long long)((esp_timer_get_time() - started_at) / 1000));
    return err;
}
esp_err_t board_port_play_ack_chime(void) {
    if (xSemaphoreTake(s_audio_mutex, pdMS_TO_TICKS(1500)) != pdTRUE) return ESP_ERR_TIMEOUT;
    int16_t mono[256];
    esp_err_t err = speaker_play_begin();
    if (err == ESP_OK) {
        for (int block = 0; block < 10 && err == ESP_OK; ++block) {
            for (int i = 0; i < 256; ++i) {
                mono[i] = sinf(2 * 3.14159265f * 660 * (block * 256 + i) / AUDIO_RATE) * 9000;
            }
            err = write_stereo(mono, 256, 1);
        }
        err = speaker_play_end(err);
    }
    xSemaphoreGive(s_audio_mutex);
    return err;
}
esp_err_t board_port_play_ack_voice(void) {return board_port_play_ack_chime();}
