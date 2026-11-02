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
#include "mbedtls/base64.h"

#include "buddy_bpt1.h"
#include "buddy_display.h"
#include "buddy_font.h"
#include "buddy_pet.h"
#include "buddy_sound.h"
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
    cJSON_AddNumberToObject(disp, "w", buddy_display_glass_width());
    cJSON_AddNumberToObject(disp, "h", buddy_display_glass_height());
    cJSON_AddBoolToObject(disp, "round", buddy_display_glass_round());
    cJSON_AddStringToObject(disp, "panel", buddy_display_panel_id());
    cJSON_AddStringToObject(disp, "font", buddy_font_rev());
    cJSON_AddBoolToObject(root, "touch", buddy_touch_available());
    cJSON_AddBoolToObject(root, "key", buddy_key_available());
    cJSON_AddBoolToObject(root, "sound_available", buddy_sound_available());
    cJSON_AddStringToObject(root, "asset_mode", "bpt1");
    send_json(root);
}

void buddy_protocol_send_state_ack(bool applied, const char *page, int missing_glyphs, int64_t ts_ms) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_STATE_ACK);
    cJSON_AddBoolToObject(root, "applied", applied);
    cJSON_AddStringToObject(root, "page", page ? page : "standby");
    cJSON_AddNumberToObject(root, "missing_glyphs", missing_glyphs);
    if (ts_ms > 0) cJSON_AddNumberToObject(root, "ts_ms", (double)ts_ms);
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

static void add_sound_event(cJSON *sound, const char *name, bool enabled, int effect) {
    cJSON *event = cJSON_AddObjectToObject(sound, name);
    cJSON_AddBoolToObject(event, "enabled", enabled);
    cJSON_AddNumberToObject(event, "effect", effect);
}

void buddy_protocol_send_input(const char *gesture, int index) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_INPUT);
    cJSON_AddNumberToObject(root, "proto", BUDDY_PROTO_VERSION);
    cJSON *input = cJSON_AddObjectToObject(root, "input");
    cJSON_AddStringToObject(input, "gesture", gesture ? gesture : "tap");
    cJSON_AddStringToObject(input, "page", buddy_protocol_current_page());
    cJSON_AddNumberToObject(input, "index", index);
    send_json(root);
}

void buddy_protocol_send_sound(void) {
    buddy_sound_config_t cfg;
    memset(&cfg, 0, sizeof(cfg));
    buddy_sound_get_config(&cfg);
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_SOUND);
    cJSON_AddNumberToObject(root, "proto", BUDDY_PROTO_VERSION);
    cJSON *sound = cJSON_AddObjectToObject(root, "sound");
    cJSON_AddBoolToObject(sound, "master", cfg.master);
    add_sound_event(sound, "running", cfg.enabled[BUDDY_SOUND_RUNNING], cfg.effect[BUDDY_SOUND_RUNNING]);
    add_sound_event(sound, "complete", cfg.enabled[BUDDY_SOUND_COMPLETE], cfg.effect[BUDDY_SOUND_COMPLETE]);
    add_sound_event(sound, "standby", cfg.enabled[BUDDY_SOUND_STANDBY], cfg.effect[BUDDY_SOUND_STANDBY]);
    add_sound_event(sound, "attention", cfg.enabled[BUDDY_SOUND_ATTENTION], cfg.effect[BUDDY_SOUND_ATTENTION]);
    send_json(root);
}

void buddy_protocol_send_asset_ack(int frame, int bytes, bool ready) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "type", BUDDY_MSG_ASSET_ACK);
    cJSON_AddNumberToObject(root, "proto", BUDDY_PROTO_VERSION);
    cJSON *ack = cJSON_AddObjectToObject(root, "asset_ack");
    cJSON_AddNumberToObject(ack, "frame", frame);
    cJSON_AddNumberToObject(ack, "bytes", bytes);
    cJSON_AddBoolToObject(ack, "ready", ready);
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

static int64_t get_i64(cJSON *root, const char *name) {
    cJSON *item = obj(root, name);
    if (!cJSON_IsNumber(item)) return 0;
    return (int64_t)item->valuedouble;
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
        buddy_display_note_activity();
        buddy_state_set_prompt("收到无效的任务数据");
        buddy_protocol_send_state_ack(false, "prompt", 0, 0);
        return;
    }
    const int64_t ts_ms = get_i64(root, "ts_ms");
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
        buddy_display_note_activity();
        buddy_state_set_prompt("任务计数不一致，已暂停显示");
        buddy_protocol_send_log("error", "counts invariant broken");
        buddy_protocol_send_state_ack(false, "prompt", 0, ts_ms);
        return;
    }

    cJSON *seq_json = obj(root, "seq");
    const bool has_seq = cJSON_IsNumber(seq_json);
    const uint64_t seq = has_seq ? (uint64_t)seq_json->valuedouble : 0;
    if (!seq_accept(seq, has_seq)) {
        // A replay must not reset the idle timer. The glass stays dim while
        // the host retries a sequence the device has already applied.
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
            get_string(entry, "phase", current[count].phase, sizeof(current[count].phase));
            get_string(entry, "elapsed", current[count].elapsed, sizeof(current[count].elapsed));
            count++;
        }
    }

    buddy_attention_t attention;
    memset(&attention, 0, sizeof(attention));
    cJSON *att = obj(root, "attention");
    if (cJSON_IsObject(att)) {
        get_string(att, "kind", attention.kind, sizeof(attention.kind));
        get_string(att, "id", attention.id, sizeof(attention.id));
        get_string(att, "title", attention.title, sizeof(attention.title));
        if (strcmp(attention.kind, "pending") != 0 && strcmp(attention.kind, "failed") != 0 &&
            strcmp(attention.kind, "paused") != 0) {
            attention.title[0] = '\0';
        }
    }

    buddy_display_note_activity();
    buddy_state_apply_state(&counts, current, count, &attention, get_bool(root, "stale", false), seq);
    buddy_state_clear_prompt();
    buddy_protocol_send_state_ack(true, buddy_protocol_current_page(), buddy_font_missing_total(false), ts_ms);
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
    if (buddy_state_apply_weather(summary, get_int(weather, "temperatureC", 0), location,
                                  get_bool(root, "stale", false))) {
        buddy_state_weather_save();
    }
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

static void handle_celebrate(cJSON *root) {
    cJSON *celebrate = obj(root, "celebrate");
    if (!cJSON_IsObject(celebrate)) {
        buddy_protocol_send_log("warn", "celebrate without payload");
        return;
    }
    char title[BUDDY_TITLE_BYTES + 1] = {0};
    if (!get_string(celebrate, "title", title, sizeof(title)) || title[0] == '\0') {
        buddy_protocol_send_log("warn", "celebrate without title");
        return;
    }
    buddy_state_apply_celebrate(title, get_int(celebrate, "seconds", 5));
}

static void parse_sound_event(cJSON *sound, const char *name, bool *enabled, int *effect) {
    cJSON *event = obj(sound, name);
    if (!cJSON_IsObject(event)) return;
    *enabled = get_bool(event, "enabled", *enabled);
    // The sound module clamps the index to its generated clip count.
    *effect = get_int(event, "effect", *effect);
}

static void handle_sound(cJSON *root) {
    cJSON *sound = obj(root, "sound");
    if (!cJSON_IsObject(sound)) {
        buddy_protocol_send_log("warn", "sound without payload");
        return;
    }
    buddy_sound_config_t cfg;
    buddy_sound_get_config(&cfg);
    cfg.master = get_bool(sound, "master", cfg.master);
    parse_sound_event(sound, "running", &cfg.enabled[BUDDY_SOUND_RUNNING],
                      &cfg.effect[BUDDY_SOUND_RUNNING]);
    parse_sound_event(sound, "complete", &cfg.enabled[BUDDY_SOUND_COMPLETE],
                      &cfg.effect[BUDDY_SOUND_COMPLETE]);
    parse_sound_event(sound, "standby", &cfg.enabled[BUDDY_SOUND_STANDBY],
                      &cfg.effect[BUDDY_SOUND_STANDBY]);
    parse_sound_event(sound, "attention", &cfg.enabled[BUDDY_SOUND_ATTENTION],
                      &cfg.effect[BUDDY_SOUND_ATTENTION]);
    buddy_sound_set_config(&cfg);
}

static void handle_asset_chunk(cJSON *root) {
    cJSON *asset = obj(root, "asset");
    if (!cJSON_IsObject(asset)) return;
    cJSON *data = obj(asset, "data");
    if (!cJSON_IsString(data) || !data->valuestring) return;
    cJSON *crc_item = obj(asset, "crc32");
    const uint32_t crc = cJSON_IsNumber(crc_item) ? (uint32_t)crc_item->valuedouble : 0;
    static uint8_t decoded[800];
    size_t decoded_len = 0;
    if (mbedtls_base64_decode(decoded, sizeof(decoded), &decoded_len,
                              (const unsigned char *)data->valuestring,
                              strlen(data->valuestring)) != 0) {
        buddy_protocol_send_log("warn", "asset base64 invalid");
        return;
    }
    const int frame = get_int(asset, "frame", -1);
    const int offset = get_int(asset, "offset", -1);
    const buddy_pet_chunk_result_t result =
        buddy_pet_chunk(frame, get_int(asset, "frames", 0), get_int(asset, "w", 0),
                        get_int(asset, "h", 0), offset, get_int(asset, "total", 0), crc,
                        get_int(asset, "frame_ms", 450), decoded, decoded_len);
    if (result == BUDDY_PET_CHUNK_REJECTED) {
        // No ack: the host aborts the transfer and retries on the next push.
        buddy_protocol_send_log("warn", "asset chunk rejected");
        return;
    }
    buddy_protocol_send_asset_ack(frame, offset + (int)decoded_len,
                                  result == BUDDY_PET_CHUNK_READY);
    if (result == BUDDY_PET_CHUNK_READY) {
        buddy_protocol_send_log("info", "pet asset ready");
    }
}

static void handle_hello(cJSON *root) {
    const int proto = get_int(root, "proto", 0);
    if (proto != BUDDY_PROTO_VERSION) {
        buddy_state_set_prompt("主机与设备协议版本不匹配");
        buddy_protocol_send_log("error", "protocol version mismatch");
        buddy_protocol_send_state_ack(false, "prompt", 0, 0);
        return;
    }
    buddy_state_clear_prompt();
    seq_reset();
    buddy_protocol_send_hello();
    buddy_protocol_send_state_ack(true, buddy_protocol_current_page(), 0, 0);
}

static int s_parse_warnings;

/** USB-Serial-JTAG can hand us ROM console bytes right after a reset, and one
 * dropped byte can turn a line into a burst of parse errors. Logging every one
 * over the same link makes the backlog worse, so sample them. */
static void log_parse_warning(const char *msg) {
    if ((s_parse_warnings++ % 32) == 0) {
        buddy_protocol_send_log("warn", msg);
    }
}

static void handle_line(const char *line) {
    cJSON *root = cJSON_Parse(line);
    if (!root) {
        log_parse_warning("bad json");
        return;
    }
    char type[24] = {0};
    if (!get_string(root, "type", type, sizeof(type))) {
        cJSON_Delete(root);
        log_parse_warning("frame without type");
        return;
    }

    buddy_state_note_host();
    // Heartbeats, the minute clock, and weather refreshes keep the link and
    // the standby page current. They must not hold the backlight on, or the
    // idle dim would never happen while the host is connected. A task update,
    // a celebration, a mute, or a new session does light the glass.
    // State frames light the glass from handle_state, and only when the frame
    // is applied or shown as an error. A stale sequence is ignored there.
    if (strcmp(type, BUDDY_MSG_CELEBRATE) == 0 || strcmp(type, BUDDY_MSG_SOUND) == 0 ||
        strcmp(type, BUDDY_MSG_HELLO) == 0) {
        buddy_display_note_activity();
    }

    if (strcmp(type, BUDDY_MSG_STATE) == 0) {
        handle_state(root);
    } else if (strcmp(type, BUDDY_MSG_AMBIENT) == 0) {
        handle_ambient(root);
    } else if (strcmp(type, BUDDY_MSG_CLOCK) == 0) {
        handle_clock(root);
    } else if (strcmp(type, BUDDY_MSG_CELEBRATE) == 0) {
        handle_celebrate(root);
    } else if (strcmp(type, BUDDY_MSG_SOUND) == 0) {
        handle_sound(root);
    } else if (strcmp(type, BUDDY_MSG_ASSET_CHUNK) == 0) {
        handle_asset_chunk(root);
    } else if (strcmp(type, BUDDY_MSG_HELLO) == 0) {
        handle_hello(root);
    } else if (strcmp(type, BUDDY_MSG_HEARTBEAT) == 0) {
        // note_host above is the whole effect
    } else if (strcmp(type, BUDDY_MSG_PET) == 0) {
        const char *state = "";
        cJSON *pet = obj(root, "pet");
        if (cJSON_IsObject(pet)) {
            cJSON *item = obj(pet, "state");
            if (cJSON_IsString(item) && item->valuestring) state = item->valuestring;
        }
        buddy_state_set_pet(state);
    } else if (strcmp(type, BUDDY_MSG_STATE_ACK) == 0 || strcmp(type, BUDDY_MSG_LOG) == 0) {
        // Host never sends these; tolerate a misconfigured peer.
    } else {
        log_parse_warning("unknown type");
    }
    cJSON_Delete(root);
}

static char s_line[BUDDY_MAX_LINE_BYTES + 1];
static size_t s_line_len;
static bool s_line_overflow;

static void rx_line_byte(char c) {
    if (c == '\n') {
        if (s_line_overflow) {
            s_line_overflow = false;
            s_line_len = 0;
            log_parse_warning("line too long");
            return;
        }
        if (s_line_len > 0) {
            s_line[s_line_len] = '\0';
            handle_line(s_line);
        }
        s_line_len = 0;
        return;
    }
    if (c == '\r') return;
    if (s_line_overflow) return;
    if (s_line_len >= BUDDY_MAX_LINE_BYTES) {
        s_line_overflow = true;
        s_line_len = 0;
        return;
    }
    s_line[s_line_len++] = c;
}

static void handle_bpt1(const uint8_t *buf, int len) {
    // Binary frames never pass through the JSON line parser, which is what
    // marks the host alive. A pet transfer blocks heartbeats for several
    // seconds; without this the glass flips to offline mid-download.
    buddy_state_note_host();
    buddy_bpt1_info_t info;
    const uint8_t *data = NULL;
    if (buddy_bpt1_parse(buf, len, &info, &data) < 0 || !data) {
        buddy_protocol_send_log("warn", "pet frame rejected");
        return;
    }
    const buddy_pet_chunk_result_t result =
        buddy_pet_chunk(info.frame, info.frames, info.width, info.height, info.offset, info.total,
                        info.crc32, info.frame_ms, data, (size_t)info.data_len);
    if (result == BUDDY_PET_CHUNK_REJECTED) {
        buddy_protocol_send_log("warn", "pet frame rejected");
        return;
    }
    buddy_protocol_send_asset_ack(info.frame, info.offset + info.data_len, result == BUDDY_PET_CHUNK_READY);
    if (result == BUDDY_PET_CHUNK_READY) {
        buddy_protocol_send_log("info", "pet asset ready");
    }
}

static void rx_task(void *arg) {
    (void)arg;
    uint8_t chunk[128];
    uint8_t bin[BUDDY_BPT1_MAX_FRAME];
    int bin_len = 0;
    int bin_need = 0;

    for (;;) {
        const int n = usb_serial_jtag_read_bytes(chunk, sizeof(chunk), pdMS_TO_TICKS(200));
        for (int i = 0; i < n; ++i) {
            const uint8_t c = chunk[i];
            // A raw pet frame starts with 'B' only at a line boundary. JSON
            // lines start with '{', so the two never share a byte.
            if (bin_need == 0 && s_line_len == 0 && !s_line_overflow && c == 'B') {
                bin_need = BUDDY_BPT1_PREFIX;
                bin_len = 0;
            }
            if (bin_need > 0) {
                if (bin_len >= BUDDY_BPT1_MAX_FRAME) {
                    bin_need = 0;
                    bin_len = 0;
                    log_parse_warning("pet frame too long");
                    continue;
                }
                bin[bin_len++] = c;
                if (bin_len == BUDDY_BPT1_MAGIC_LEN &&
                    memcmp(bin, BUDDY_BPT1_MAGIC, BUDDY_BPT1_MAGIC_LEN) != 0) {
                    for (int j = 0; j < bin_len; ++j) rx_line_byte((char)bin[j]);
                    bin_need = 0;
                    bin_len = 0;
                    continue;
                }
                if (bin_len == BUDDY_BPT1_PREFIX) {
                    const int data_len = bin[24] | (bin[25] << 8);
                    if (data_len <= 0 || data_len > BUDDY_BPT1_MAX_DATA) {
                        bin_need = 0;
                        bin_len = 0;
                        log_parse_warning("pet frame length");
                        continue;
                    }
                    bin_need = BUDDY_BPT1_PREFIX + data_len;
                }
                if (bin_len == bin_need && bin_len > BUDDY_BPT1_PREFIX) {
                    handle_bpt1(bin, bin_len);
                    bin_need = 0;
                    bin_len = 0;
                }
                continue;
            }
            rx_line_byte((char)c);
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
        .rx_buffer_size = 16384,
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
