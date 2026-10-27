#include "buddy_protocol.h"

#include <stdarg.h>
#include <stdio.h>
#include <string.h>

#include "cJSON.h"
#include "driver/usb_serial_jtag.h"
#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "buddy_display.h"
#include "buddy_font.h"
#include "buddy_state.h"
#include "buddy_touch.h"
#include "buddy_ui.h"
#include "buddy_protocol_generated.h"

#define FW_VERSION "buddy-0.1.0"
#define BOARD_NAME "echoear-2st"

static SemaphoreHandle_t s_tx_lock;
static SemaphoreHandle_t s_seq_lock;
static uint64_t s_last_seq;
static bool s_seq_seen;

static void send_json(cJSON *root) {
    if (!root) return;
    char *text = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    if (!text) return;
    size_t len = strlen(text);
    if (len + 1 > BUDDY_MAX_LINE_BYTES) {
        cJSON_free(text);
        return;
    }
    if (s_tx_lock) xSemaphoreTake(s_tx_lock, portMAX_DELAY);
    size_t written = 0;
    while (written < len) {
        int n = usb_serial_jtag_write_bytes((const uint8_t *)text + written,
                                            len - written, pdMS_TO_TICKS(200));
        if (n <= 0) break;
        written += (size_t)n;
    }
    usb_serial_jtag_write_bytes((const uint8_t *)"\n", 1, pdMS_TO_TICKS(200));
    if (s_tx_lock) xSemaphoreGive(s_tx_lock);
    cJSON_free(text);
}

void buddy_protocol_send_hello(void) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_HELLO);
    cJSON_AddNumberToObject(root, "proto", BUDDY_PROTO_VERSION);
    cJSON_AddStringToObject(root, "fw", FW_VERSION);
    cJSON_AddStringToObject(root, "board", BOARD_NAME);
    cJSON *disp = cJSON_AddObjectToObject(root, "disp");
    cJSON_AddNumberToObject(disp, "w", BUDDY_DISPLAY_WIDTH);
    cJSON_AddNumberToObject(disp, "h", BUDDY_DISPLAY_HEIGHT);
    cJSON_AddBoolToObject(disp, "round", BUDDY_DISPLAY_ROUND != 0);
    cJSON_AddBoolToObject(root, "touch", buddy_touch_available());
    send_json(root);
}

void buddy_protocol_send_state_ack(bool applied, const char *page, int missing_glyphs) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_STATE_ACK);
    cJSON_AddBoolToObject(root, "applied", applied);
    cJSON_AddStringToObject(root, "page", page ? page : "standby");
    cJSON_AddNumberToObject(root, "missing_glyphs", missing_glyphs);
    send_json(root);
}

void buddy_protocol_send_heartbeat(void) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_HEARTBEAT);
    cJSON_AddNumberToObject(root, "ts", (double)(esp_timer_get_time() / 1000000));
    cJSON_AddNumberToObject(root, "uptime_s", buddy_state_uptime_s());
    cJSON_AddNumberToObject(root, "heap_min", esp_get_minimum_free_heap_size());
    cJSON_AddStringToObject(root, "page", buddy_protocol_current_page());
    // Cumulative count of characters the renderer had to draw as a box.
    // The host uses it as the real-device missing-glyph metric.
    cJSON_AddNumberToObject(root, "missing_glyphs", buddy_font_missing_total(false));
    send_json(root);
}

void buddy_protocol_send_log(const char *level, const char *msg) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_LOG);
    cJSON_AddStringToObject(root, "level", level ? level : "info");
    cJSON_AddStringToObject(root, "msg", msg ? msg : "");
    send_json(root);
}

const char *buddy_protocol_current_page(void) {
    buddy_view_t view;
    buddy_state_get(&view);
    return buddy_ui_page_name(buddy_ui_page(&view));
}

static cJSON *obj(cJSON *root, const char *name) { return cJSON_GetObjectItemCaseSensitive(root, name); }

static bool get_string(cJSON *root, const char *name, char *out, size_t out_size) {
    cJSON *item = obj(root, name);
    if (!cJSON_IsString(item) || !item->valuestring) return false;
    if (out && out_size) {
        buddy_utf8_truncate(out, out_size, item->valuestring, out_size - 1);
    }
    return true;
}

static int get_int(cJSON *root, const char *name, int fallback) {
    cJSON *item = obj(root, name);
    return cJSON_IsNumber(item) ? item->valueint : fallback;
}

static bool get_bool(cJSON *root, const char *name, bool fallback) {
    cJSON *item = obj(root, name);
    if (cJSON_IsBool(item)) return cJSON_IsTrue(item);
    return fallback;
}

static bool seq_accept(uint64_t seq, bool has_seq) {
    if (!has_seq) return true;
    if (!s_seq_lock) return true;
    xSemaphoreTake(s_seq_lock, portMAX_DELAY);
    bool accept = true;
    if (s_seq_seen && seq <= s_last_seq) accept = false;
    if (accept) {
        s_last_seq = seq;
        s_seq_seen = true;
    }
    xSemaphoreGive(s_seq_lock);
    return accept;
}

/** A new host session restarts its sequence at 1; without this the device
 * would ignore every frame until the old counter was exceeded. */
static void seq_reset(void) {
    if (!s_seq_lock) return;
    xSemaphoreTake(s_seq_lock, portMAX_DELAY);
    s_last_seq = 0;
    s_seq_seen = false;
    xSemaphoreGive(s_seq_lock);
}

static void handle_state(cJSON *root) {
    cJSON *counts_json = obj(root, "counts");
    if (!cJSON_IsObject(counts_json)) {
        buddy_state_set_prompt("收到无效的任务数据");
        buddy_protocol_send_state_ack(false, "prompt", 0);
        return;
    }
    buddy_counts_t counts = {
        .total = get_int(counts_json, "total", 0),
        .in_progress = get_int(counts_json, "in_progress", 0),
        .pending = get_int(counts_json, "pending", 0),
        .paused = get_int(counts_json, "paused", 0),
        .done = get_int(counts_json, "done", 0),
        .failed = get_int(counts_json, "failed", 0),
    };
    const int sum = counts.in_progress + counts.pending + counts.paused + counts.done + counts.failed;
    if (counts.total < 0 || sum != counts.total) {
        buddy_state_set_prompt("任务计数不一致，已暂停显示");
        buddy_protocol_send_log("error", "counts invariant broken");
        buddy_protocol_send_state_ack(false, "prompt", 0);
        return;
    }

    cJSON *seq_json = obj(root, "seq");
    const bool has_seq = cJSON_IsNumber(seq_json);
    const uint64_t seq = has_seq ? (uint64_t)seq_json->valuedouble : 0;
    if (!seq_accept(seq, has_seq)) {
        buddy_protocol_send_log("warn", "stale seq ignored");
        return;
    }

    buddy_task_t current[BUDDY_MAX_TASKS];
    memset(current, 0, sizeof(current));
    int count = 0;
    cJSON *items = obj(root, "current");
    if (cJSON_IsArray(items)) {
        cJSON *entry = NULL;
        cJSON_ArrayForEach(entry, items) {
            if (count >= BUDDY_MAX_TASKS) break;
            char title[BUDDY_TITLE_BYTES + 1];
            if (!get_string(entry, "title", title, sizeof(title)) || title[0] == '\0') continue;
            strlcpy(current[count].title, title, sizeof(current[count].title));
            get_string(entry, "id", current[count].id, sizeof(current[count].id));
            cJSON *progress = obj(entry, "progress");
            current[count].progress = cJSON_IsNumber(progress) ? (float)progress->valuedouble : -1.0f;
            if (current[count].progress < -1.0f) current[count].progress = -1.0f;
            if (current[count].progress > 1.0f) current[count].progress = 1.0f;
            count++;
        }
    }

    buddy_state_apply_state(&counts, current, count, get_bool(root, "stale", false), seq);
    buddy_state_clear_prompt();
    buddy_protocol_send_state_ack(true, buddy_protocol_current_page(), buddy_font_missing_total(false));
}

static void handle_ambient(cJSON *root) {
    cJSON *weather = obj(root, "weather");
    if (!cJSON_IsObject(weather)) {
        buddy_protocol_send_log("warn", "ambient without weather");
        return;
    }
    char summary[BUDDY_LABEL_BYTES + 1] = {0};
    char location[BUDDY_LABEL_BYTES + 1] = {0};
    if (!get_string(weather, "summary", summary, sizeof(summary))) {
        buddy_protocol_send_log("warn", "ambient without summary");
        return;
    }
    get_string(weather, "location", location, sizeof(location));
    buddy_state_apply_weather(summary, get_int(weather, "temperatureC", 0), location,
                              get_bool(root, "stale", false));
    buddy_state_weather_save();
}

static void handle_clock(cJSON *root) {
    cJSON *clock = obj(root, "clock");
    if (!cJSON_IsObject(clock)) return;
    char date[24] = {0};
    char weekday[16] = {0};
    if (!get_string(clock, "date", date, sizeof(date))) return;
    get_string(clock, "weekday", weekday, sizeof(weekday));
    buddy_state_apply_clock(date, weekday);
}

static void handle_hello(cJSON *root) {
    const int proto = get_int(root, "proto", 0);
    if (proto != BUDDY_PROTO_VERSION) {
        buddy_state_set_prompt("主机与设备协议版本不匹配");
        buddy_protocol_send_log("error", "protocol version mismatch");
        buddy_protocol_send_state_ack(false, "prompt", 0);
        return;
    }
    buddy_state_clear_prompt();
    seq_reset();
    buddy_protocol_send_hello();
    buddy_protocol_send_state_ack(true, buddy_protocol_current_page(), 0);
}

static void handle_line(const char *line) {
    cJSON *root = cJSON_Parse(line);
    if (!root) {
        buddy_protocol_send_log("warn", "bad json");
        return;
    }
    char type[24] = {0};
    if (!get_string(root, "type", type, sizeof(type))) {
        cJSON_Delete(root);
        buddy_protocol_send_log("warn", "frame without type");
        return;
    }

    buddy_state_note_host();
    // Heartbeats keep the link "online" but must not hold the backlight on;
    // otherwise the idle screen-off policy could never trigger.
    if (strcmp(type, BUDDY_MSG_HEARTBEAT) != 0) {
        buddy_display_note_activity();
    }

    if (strcmp(type, BUDDY_MSG_STATE) == 0) {
        handle_state(root);
    } else if (strcmp(type, BUDDY_MSG_AMBIENT) == 0) {
        handle_ambient(root);
    } else if (strcmp(type, BUDDY_MSG_CLOCK) == 0) {
        handle_clock(root);
    } else if (strcmp(type, BUDDY_MSG_HELLO) == 0) {
        handle_hello(root);
    } else if (strcmp(type, BUDDY_MSG_HEARTBEAT) == 0) {
        // note_host above is the whole effect
    } else if (strcmp(type, BUDDY_MSG_PET) == 0) {
        // Emotion hints are not rendered yet; waking the screen is enough.
    } else if (strcmp(type, BUDDY_MSG_STATE_ACK) == 0 || strcmp(type, BUDDY_MSG_LOG) == 0) {
        // Host never sends these; tolerate a misconfigured peer.
    } else {
        buddy_protocol_send_log("warn", "unknown type");
    }
    cJSON_Delete(root);
}

static void rx_task(void *arg) {
    (void)arg;
    static char s_line[BUDDY_MAX_LINE_BYTES + 1];
    size_t len = 0;
    bool overflow = false;
    uint8_t chunk[128];

    for (;;) {
        const int n = usb_serial_jtag_read_bytes(chunk, sizeof(chunk), pdMS_TO_TICKS(200));
        for (int i = 0; i < n; ++i) {
            const char c = (char)chunk[i];
            if (c == '\n') {
                if (overflow) {
                    overflow = false;
                    len = 0;
                    buddy_protocol_send_log("warn", "line too long");
                    continue;
                }
                if (len > 0) {
                    s_line[len] = '\0';
                    handle_line(s_line);
                }
                len = 0;
                continue;
            }
            if (c == '\r') continue;
            if (overflow) continue;
            if (len >= BUDDY_MAX_LINE_BYTES) {
                overflow = true;
                len = 0;
                continue;
            }
            s_line[len++] = c;
        }
    }
}

static void heartbeat_task(void *arg) {
    (void)arg;
    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(BUDDY_HEARTBEAT_MS));
        buddy_protocol_send_heartbeat();
    }
}

esp_err_t buddy_protocol_start(void) {
    s_tx_lock = xSemaphoreCreateMutex();
    s_seq_lock = xSemaphoreCreateMutex();
    if (!s_tx_lock || !s_seq_lock) return ESP_ERR_NO_MEM;

    usb_serial_jtag_driver_config_t cfg = {
        .rx_buffer_size = 4096,
        .tx_buffer_size = 4096,
    };
    esp_err_t err = usb_serial_jtag_driver_install(&cfg);
    if (err != ESP_OK) return err;

    buddy_state_clear_prompt();
    buddy_protocol_send_hello();

    if (xTaskCreate(rx_task, "buddy_rx", 4096, NULL, 6, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    if (xTaskCreate(heartbeat_task, "buddy_hb", 3072, NULL, 4, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}
