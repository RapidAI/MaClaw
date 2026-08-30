#include "services/gateway_ack_outbox_policy.h"

#include <stdint.h>
#include <string.h>

static void skip_ws(const char **p, const char *end) {
    while (*p < end && (**p == ' ' || **p == '\t' || **p == '\r' || **p == '\n')) ++*p;
}

static int is_hex(unsigned char c) {
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') ||
           (c >= 'A' && c <= 'F');
}

/* Parse one JSON string without allocating or unescaping it. */
static int parse_string(const char **p, const char *end, const char **body,
                        size_t *body_len) {
    if (*p >= end || **p != '"') return 0;
    const char *start = ++*p;
    int escaped = 0;
    while (*p < end) {
        unsigned char c = (unsigned char)**p;
        if (c < 0x20u) return 0;
        if (escaped) {
            if (c == 'u') {
                if ((size_t)(end - *p) < 5u || !is_hex((unsigned char)(*p)[1]) ||
                    !is_hex((unsigned char)(*p)[2]) || !is_hex((unsigned char)(*p)[3]) ||
                    !is_hex((unsigned char)(*p)[4])) return 0;
                *p += 5;
                escaped = 0;
                continue;
            }
            if (!(c == '"' || c == '\\' || c == '/' || c == 'b' || c == 'f' ||
                  c == 'n' || c == 'r' || c == 't')) return 0;
            escaped = 0;
            ++*p;
            continue;
        }
        if (c == '\\') {
            escaped = 1;
            ++*p;
            continue;
        }
        if (c == '"') {
            if (body) *body = start;
            if (body_len) *body_len = (size_t)(*p - start);
            ++*p;
            return 1;
        }
        ++*p;
    }
    return 0;
}

static int string_equals(const char *body, size_t len, const char *literal) {
    const size_t n = strlen(literal);
    return len == n && memcmp(body, literal, n) == 0;
}

static int parse_ack_schema(const char *payload, size_t stored_size) {
    const char *p = payload;
    const char *end = payload + stored_size - 1u; /* exclude NUL */
    unsigned seen = 0;
    skip_ws(&p, end);
    if (p >= end || *p++ != '{') return 0;
    skip_ws(&p, end);
    if (p < end && *p == '}') return 0; /* all three fields are mandatory */
    while (p < end) {
        const char *key = NULL;
        size_t key_len = 0;
        if (!parse_string(&p, end, &key, &key_len)) return 0;
        skip_ws(&p, end);
        if (p >= end || *p++ != ':') return 0;
        skip_ws(&p, end);
        unsigned bit = 0;
        if (string_equals(key, key_len, "clientId")) bit = 1u;
        else if (string_equals(key, key_len, "messageIds")) bit = 2u;
        else if (string_equals(key, key_len, "status")) bit = 4u;
        else return 0; /* reject unknown fields to keep replay contract closed */
        if (seen & bit) return 0;
        seen |= bit;

        if (bit == 1u || bit == 4u) {
            const char *value = NULL;
            size_t value_len = 0;
            if (!parse_string(&p, end, &value, &value_len) || value_len == 0) return 0;
            if (bit == 4u && !string_equals(value, value_len, "delivered") &&
                !string_equals(value, value_len, "failed")) return 0;
        } else {
            if (p >= end || *p++ != '[') return 0;
            skip_ws(&p, end);
            size_t count = 0;
            if (p < end && *p == ']') return 0; /* ACK must identify a message */
            while (p < end) {
                const char *id = NULL;
                size_t id_len = 0;
                if (!parse_string(&p, end, &id, &id_len) || id_len == 0) return 0;
                ++count;
                skip_ws(&p, end);
                if (p >= end) return 0;
                if (*p == ']') { ++p; break; }
                if (*p++ != ',') return 0;
                skip_ws(&p, end);
            }
            if (count == 0) return 0;
        }
        skip_ws(&p, end);
        if (p >= end) return 0;
        if (*p == '}') { ++p; break; }
        if (*p++ != ',') return 0;
        skip_ws(&p, end);
    }
    skip_ws(&p, end);
    return seen == 7u && p == end;
}

device_status_t gateway_ack_outbox_validate_record(const char *payload,
                                                   size_t stored_size,
                                                   size_t capacity) {
    if (!payload || capacity == 0 || stored_size < 3u || stored_size > capacity ||
        stored_size > GATEWAY_ACK_OUTBOX_CAPACITY ||
        payload[0] != '{' || payload[stored_size - 2u] != '}' ||
        payload[stored_size - 1u] != '\0') {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* Reject embedded NULs and trailing bytes: the persisted size must describe
     * exactly one envelope, preventing ambiguous replay after corruption. */
    if (memchr(payload, '\0', stored_size - 1u) != NULL) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return parse_ack_schema(payload, stored_size) ? DEVICE_STATUS_OK
                                                   : DEVICE_STATUS_INVALID_ARGUMENT;
}
