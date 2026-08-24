#include "services/pet_asset_service.h"

#include <limits.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"

static bool copy_cstring(char *out, size_t out_capacity, const char *source) {
    if (!out || !out_capacity || !source) return false;
    const size_t length = strlen(source);
    if (length >= out_capacity) return false;
    memcpy(out, source, length + 1);
    return true;
}

static bool hub_media_url_allowed(const char *url) {
    static const char media_prefix[] = "/api/im-gateway/v1/media/";
    return url && url[0] == '/' &&
           strncmp(url, media_prefix, sizeof(media_prefix) - 1) == 0;
}

static bool json_number(cJSON *root, const char *key, int *value) {
    cJSON *node = root ? cJSON_GetObjectItemCaseSensitive(root, key) : NULL;
    if (!cJSON_IsNumber(node) || !value || !isfinite(node->valuedouble) ||
        node->valuedouble < (double)INT_MIN ||
        node->valuedouble > (double)INT_MAX) {
        return false;
    }
    const int parsed = (int)node->valuedouble;
    if (node->valuedouble != (double)parsed) return false;
    *value = parsed;
    return true;
}

static const char *json_string(cJSON *root, const char *key) {
    cJSON *node = root ? cJSON_GetObjectItemCaseSensitive(root, key) : NULL;
    return cJSON_IsString(node) && node->valuestring ? node->valuestring : NULL;
}

static bool valid_sha256(const char *hash) {
    if (!hash || strlen(hash) != 64) return false;
    for (size_t i = 0; i < 64; ++i) {
        const char ch = hash[i];
        if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') ||
              (ch >= 'A' && ch <= 'F'))) {
            return false;
        }
    }
    return true;
}

static int hex_nibble(char ch) {
    if (ch >= '0' && ch <= '9') return ch - '0';
    if (ch >= 'a' && ch <= 'f') return ch - 'a' + 10;
    if (ch >= 'A' && ch <= 'F') return ch - 'A' + 10;
    return -1;
}

static bool cache_descriptor_fields_valid(const pet_asset_descriptor_t *descriptor) {
    size_t ignored_frame_bytes = 0;
    if (!descriptor || !descriptor->revision[0] ||
        strlen(descriptor->revision) >= sizeof(descriptor->revision) ||
        descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES ||
        descriptor->frame_ms < 50 || descriptor->frame_ms > 10000 ||
        !pet_asset_service_frame_bytes(descriptor->width, descriptor->height,
                                       &ignored_frame_bytes)) {
        return false;
    }
    for (int i = 0; i < descriptor->frame_count; ++i) {
        if (!valid_sha256(descriptor->sha256[i])) return false;
    }
    return true;
}

static bool parse_int_token(char **cursor, int *out) {
    if (!cursor || !*cursor || !out) return false;
    char *end = NULL;
    long value = strtol(*cursor, &end, 10);
    if (end == *cursor || value < INT_MIN || value > INT_MAX) return false;
    *cursor = end;
    *out = (int)value;
    return true;
}

static bool parse_geometry(const char *line, pet_asset_descriptor_t *out) {
    if (!line || !out) return false;
    char local[64];
    if (strlen(line) >= sizeof(local)) return false;
    if (!copy_cstring(local, sizeof(local), line)) return false;
    char *cursor = local;
    if (!parse_int_token(&cursor, &out->width)) return false;
    while (*cursor == ' ') ++cursor;
    if (!parse_int_token(&cursor, &out->height)) return false;
    while (*cursor == ' ') ++cursor;
    if (!parse_int_token(&cursor, &out->frame_ms)) return false;
    while (*cursor == ' ') ++cursor;
    if (!parse_int_token(&cursor, &out->frame_count)) return false;
    while (*cursor == ' ') ++cursor;
    return *cursor == '\0';
}

/* Cache metadata is a small commit record, not a forgiving user-facing text
 * format.  Its writer always terminates every field with one LF.  Accepting
 * blank lines (as strtok_r would) can turn a truncated/interrupted record into
 * a different valid token sequence, so require an exact non-empty line for
 * every required field and no bytes after the final digest. */
static bool next_cache_line(char **cursor, char **out_line) {
    if (!cursor || !*cursor || !out_line) return false;
    char *line = *cursor;
    char *newline = strchr(line, '\n');
    if (!newline || newline == line) return false;
    *newline = '\0';
    *cursor = newline + 1;
    *out_line = line;
    return true;
}

bool pet_asset_service_frame_bytes(int width, int height, size_t *out_bytes) {
    if (!out_bytes || width < 32 || height < 32 ||
        width > (int)PET_ASSET_SERVICE_MAX_DIMENSION ||
        height > (int)PET_ASSET_SERVICE_MAX_DIMENSION) {
        return false;
    }
    const size_t pixels = (size_t)width * (size_t)height;
    if (pixels > SIZE_MAX / PET_ASSET_SERVICE_BYTES_PER_PIXEL) return false;
    *out_bytes = pixels * PET_ASSET_SERVICE_BYTES_PER_PIXEL;
    return *out_bytes != 0;
}

bool pet_asset_service_calculate_memory_requirements(
    const pet_asset_descriptor_t *descriptor,
    uint32_t retained_target_bytes,
    uint32_t max_target_allocation_bytes,
    pet_asset_memory_requirements_t *out) {
    if (!out || !cache_descriptor_fields_valid(descriptor)) return false;
    size_t frame_bytes = 0;
    if (!pet_asset_service_frame_bytes(descriptor->width, descriptor->height,
                                       &frame_bytes) ||
        frame_bytes > UINT32_MAX ||
        (size_t)descriptor->frame_count > UINT32_MAX / frame_bytes) {
        return false;
    }
    const uint32_t source_bytes = (uint32_t)(frame_bytes * (size_t)descriptor->frame_count);
    if (retained_target_bytes > UINT32_MAX - source_bytes) return false;

    out->source_bytes = source_bytes;
    out->total_external_bytes = source_bytes + retained_target_bytes;
    out->max_external_allocation_bytes = (uint32_t)frame_bytes;
    if (max_target_allocation_bytes > out->max_external_allocation_bytes) {
        out->max_external_allocation_bytes = max_target_allocation_bytes;
    }
    return true;
}

bool pet_asset_service_parse_hub_descriptor(const void *hub_object,
                                            pet_asset_descriptor_t *out) {
    cJSON *object = (cJSON *)hub_object;
    if (!cJSON_IsObject(object) || !out) return false;
    memset(out, 0, sizeof(*out));

    const char *encoding = json_string(object, "encoding");
    const char *revision = json_string(object, "revision");
    cJSON *urls = cJSON_GetObjectItemCaseSensitive(object, "urls");
    cJSON *hashes = cJSON_GetObjectItemCaseSensitive(object, "sha256");
    size_t ignored_frame_bytes = 0;
    if (!encoding || strcmp(encoding, "rgb565a8") || !revision || !revision[0] ||
        strlen(revision) >= sizeof(out->revision) ||
        !json_number(object, "width", &out->width) ||
        !json_number(object, "height", &out->height) ||
        !pet_asset_service_frame_bytes(out->width, out->height, &ignored_frame_bytes) ||
        !cJSON_IsArray(urls) || !cJSON_IsArray(hashes)) {
        return false;
    }
    cJSON *frame_ms = cJSON_GetObjectItemCaseSensitive(object, "frameMs");
    if (!frame_ms) {
        out->frame_ms = PET_ASSET_SERVICE_DEFAULT_FRAME_MS;
    } else if (!json_number(object, "frameMs", &out->frame_ms) ||
               out->frame_ms < 50 || out->frame_ms > 10000) {
        return false;
    }

    const int count = cJSON_GetArraySize(urls);
    if (count < 1 || count > (int)PET_ASSET_SERVICE_MAX_FRAMES ||
        cJSON_GetArraySize(hashes) != count) {
        return false;
    }
    if (!copy_cstring(out->encoding, sizeof(out->encoding), encoding) ||
        !copy_cstring(out->revision, sizeof(out->revision), revision)) {
        return false;
    }
    for (int i = 0; i < count; ++i) {
        cJSON *entry = cJSON_GetArrayItem(urls, i);
        cJSON *hash = cJSON_GetArrayItem(hashes, i);
        if (!cJSON_IsString(entry) || !entry->valuestring ||
            !hub_media_url_allowed(entry->valuestring) ||
            strlen(entry->valuestring) >= sizeof(out->urls[i]) ||
            !cJSON_IsString(hash) || !hash->valuestring ||
            strlen(hash->valuestring) != 64) {
            return false;
        }
        if (!valid_sha256(hash->valuestring)) return false;
        if (!copy_cstring(out->urls[i], sizeof(out->urls[i]), entry->valuestring) ||
            !copy_cstring(out->sha256[i], sizeof(out->sha256[i]), hash->valuestring)) {
            return false;
        }
    }
    out->frame_count = count;
    return true;
}

bool pet_asset_service_format_cache_metadata(const pet_asset_descriptor_t *descriptor,
                                             char *out, size_t out_capacity,
                                             size_t *out_length) {
    if (!out || !out_length || !cache_descriptor_fields_valid(descriptor)) return false;
    size_t used = 0;
    int written = snprintf(out, out_capacity, "MACLAW_PET_V2\n%s\n%d %d %d %d\n",
                           descriptor->revision, descriptor->width, descriptor->height,
                           descriptor->frame_ms, descriptor->frame_count);
    if (written < 0 || (size_t)written >= out_capacity) return false;
    used = (size_t)written;
    for (int i = 0; i < descriptor->frame_count; ++i) {
        written = snprintf(out + used, out_capacity - used, "%s\n", descriptor->sha256[i]);
        if (written < 0 || (size_t)written >= out_capacity - used) return false;
        used += (size_t)written;
    }
    *out_length = used;
    return true;
}

bool pet_asset_service_parse_cache_metadata(const char *data, size_t length,
                                            pet_asset_descriptor_t *out) {
    if (!data || !out || length == 0 ||
        length >= PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY ||
        memchr(data, '\0', length) != NULL) {
        return false;
    }
    char local[PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY];
    memcpy(local, data, length);
    local[length] = '\0';
    memset(out, 0, sizeof(*out));
    char *cursor = local;
    char *magic = NULL;
    char *revision = NULL;
    char *geometry = NULL;
    if (!next_cache_line(&cursor, &magic) ||
        !next_cache_line(&cursor, &revision) ||
        !next_cache_line(&cursor, &geometry)) {
        return false;
    }
    if (!magic || strcmp(magic, "MACLAW_PET_V2") || !revision || !revision[0] ||
        strlen(revision) >= sizeof(out->revision) || !geometry ||
        !parse_geometry(geometry, out)) {
        return false;
    }
    if (!copy_cstring(out->encoding, sizeof(out->encoding), "rgb565a8") ||
        !copy_cstring(out->revision, sizeof(out->revision), revision)) {
        return false;
    }
    if (out->frame_count < 1 || out->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        return false;
    }
    for (int i = 0; i < out->frame_count; ++i) {
        char *hash = NULL;
        if (!next_cache_line(&cursor, &hash) || !valid_sha256(hash)) return false;
        if (!copy_cstring(out->sha256[i], sizeof(out->sha256[i]), hash)) return false;
    }
    return *cursor == '\0' && cache_descriptor_fields_valid(out);
}

bool pet_asset_service_sha256_matches_hex(const uint8_t digest[32],
                                          const char expected_hex[65]) {
    if (!digest || !valid_sha256(expected_hex)) return false;
    unsigned difference = 0;
    for (size_t i = 0; i < 32; ++i) {
        const int high = hex_nibble(expected_hex[i * 2]);
        const int low = hex_nibble(expected_hex[i * 2 + 1]);
        difference |= (unsigned)(digest[i] ^ (uint8_t)((high << 4) | low));
    }
    return difference == 0;
}

bool pet_asset_service_limit_frame_count(const pet_asset_descriptor_t *source,
                                         uint32_t max_frame_count,
                                         pet_asset_descriptor_t *out) {
    if (!out || !cache_descriptor_fields_valid(source) || max_frame_count == 0) {
        return false;
    }
    *out = *source;
    if (max_frame_count < (uint32_t)out->frame_count) {
        out->frame_count = (int)max_frame_count;
    }
    return out->frame_count > 0;
}

bool pet_asset_service_next_memory_fallback(const pet_asset_descriptor_t *source,
                                            uint32_t attempted_frame_count,
                                            uint32_t available_frame_count,
                                            uint32_t *out_frame_count,
                                            uint32_t *out_frame_ms) {
    if (!out_frame_count || !out_frame_ms || !cache_descriptor_fields_valid(source) ||
        attempted_frame_count < 2 ||
        attempted_frame_count > (uint32_t)source->frame_count ||
        available_frame_count == 0) {
        return false;
    }
    uint32_t next_count = attempted_frame_count > 4 ? 4 :
                          attempted_frame_count > 2 ? 2 : 1;
    if (next_count > available_frame_count) next_count = available_frame_count;
    if (next_count == 0 || next_count >= attempted_frame_count) return false;

    *out_frame_count = next_count;
    *out_frame_ms = (uint32_t)source->frame_ms *
                    (uint32_t)source->frame_count / next_count;
    return *out_frame_ms > 0;
}
