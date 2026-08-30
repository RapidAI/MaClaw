#include "services/gateway_tool_result_outbox_policy.h"

#include <stdint.h>
#include <string.h>

static int ranges_overlap(const void *a, size_t a_size,
                          const void *b, size_t b_size) {
    if (!a || !b || a_size == 0 || b_size == 0) return 0;
    const uintptr_t a_start = (uintptr_t)a;
    const uintptr_t b_start = (uintptr_t)b;
    /* Subtraction avoids wrapping end-address arithmetic on hostile pointers. */
    return (a_start < b_start) ? (b_start - a_start < a_size)
                               : (a_start - b_start < b_size);
}

static void skip_ws(const char **p, const char *end) {
    while (*p < end && (**p == ' ' || **p == '\t' || **p == '\r' || **p == '\n')) ++*p;
}

static int is_hex(unsigned char c) {
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') ||
           (c >= 'A' && c <= 'F');
}

static int parse_string(const char **p, const char *end, const char **body,
                        size_t *body_len) {
    if (*p >= end || **p != '"') return 0;
    const char *start = ++*p;
    int escaped = 0;
    while (*p < end) {
        const unsigned char c = (unsigned char)**p;
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
        } else if (c == '\\') {
            escaped = 1;
            ++*p;
        } else if (c == '"') {
            if (body) *body = start;
            if (body_len) *body_len = (size_t)(*p - start);
            ++*p;
            return 1;
        } else {
            ++*p;
        }
    }
    return 0;
}

static int equals_literal(const char *body, size_t len, const char *literal) {
    const size_t n = strlen(literal);
    return len == n && memcmp(body, literal, n) == 0;
}

static int is_ascii_token(const char *body, size_t len, size_t max_len) {
    if (!body || len == 0 || len > max_len) return 0;
    for (size_t i = 0; i < len; ++i) {
        const unsigned char c = (unsigned char)body[i];
        if (c < 0x21u || c > 0x7eu) return 0;
    }
    return 1;
}

/* Syntax-only JSON value walker. It deliberately has no allocator or cJSON
 * dependency, so recovery can reject a corrupt blob before transport replay. */
static int parse_value(const char **p, const char *end, unsigned depth) {
    if (depth > 16u) return 0;
    skip_ws(p, end);
    if (*p >= end) return 0;
    if (**p == '"') return parse_string(p, end, NULL, NULL);
    if (**p == '{') {
        ++*p;
        skip_ws(p, end);
        if (*p < end && **p == '}') { ++*p; return 1; }
        while (*p < end) {
            if (!parse_string(p, end, NULL, NULL)) return 0;
            skip_ws(p, end);
            if (*p >= end || *(*p)++ != ':') return 0;
            if (!parse_value(p, end, depth + 1u)) return 0;
            skip_ws(p, end);
            if (*p >= end) return 0;
            if (**p == '}') { ++*p; return 1; }
            if (*(*p)++ != ',') return 0;
            skip_ws(p, end);
        }
        return 0;
    }
    if (**p == '[') {
        ++*p;
        skip_ws(p, end);
        if (*p < end && **p == ']') { ++*p; return 1; }
        while (*p < end) {
            if (!parse_value(p, end, depth + 1u)) return 0;
            skip_ws(p, end);
            if (*p >= end) return 0;
            if (**p == ']') { ++*p; return 1; }
            if (*(*p)++ != ',') return 0;
            skip_ws(p, end);
        }
        return 0;
    }
    if ((size_t)(end - *p) >= 4u && memcmp(*p, "true", 4u) == 0) { *p += 4; return 1; }
    if ((size_t)(end - *p) >= 5u && memcmp(*p, "false", 5u) == 0) { *p += 5; return 1; }
    if ((size_t)(end - *p) >= 4u && memcmp(*p, "null", 4u) == 0) { *p += 4; return 1; }
    /* Number grammar is intentionally conservative but accepts the forms
     * emitted by cJSON (integer/decimal/exponent, including a minus sign). */
    const char *start = *p;
    if (**p == '-') ++*p;
    if (*p >= end || **p < '0' || **p > '9') return 0;
    while (*p < end && **p >= '0' && **p <= '9') ++*p;
    if (*p < end && **p == '.') {
        ++*p;
        if (*p >= end || **p < '0' || **p > '9') return 0;
        while (*p < end && **p >= '0' && **p <= '9') ++*p;
    }
    if (*p < end && (**p == 'e' || **p == 'E')) {
        ++*p;
        if (*p < end && (**p == '+' || **p == '-')) ++*p;
        if (*p >= end || **p < '0' || **p > '9') return 0;
        while (*p < end && **p >= '0' && **p <= '9') ++*p;
    }
    return *p > start;
}

static int validate_envelope_schema(const char *payload, size_t stored_size) {
    const char *p = payload;
    const char *end = payload + stored_size - 1u;
    unsigned seen = 0;
    int status_ok = 0;
    int succeeded = 0;
    skip_ws(&p, end);
    if (p >= end || *p++ != '{') return 0;
    skip_ws(&p, end);
    if (p < end && *p == '}') return 0;
    while (p < end) {
        const char *key = NULL;
        size_t key_len = 0;
        if (!parse_string(&p, end, &key, &key_len)) return 0;
        skip_ws(&p, end);
        if (p >= end || *p++ != ':') return 0;
        skip_ws(&p, end);
        unsigned bit = 0;
        if (equals_literal(key, key_len, "clientId")) bit = 1u;
        else if (equals_literal(key, key_len, "resultId")) bit = 2u;
        else if (equals_literal(key, key_len, "toolCallId")) bit = 4u;
        else if (equals_literal(key, key_len, "toolName")) bit = 8u;
        else if (equals_literal(key, key_len, "conversationId")) bit = 16u;
        else if (equals_literal(key, key_len, "status")) bit = 32u;
        else if (equals_literal(key, key_len, "idempotencyKey")) bit = 64u;
        else if (equals_literal(key, key_len, "result")) bit = 128u;
        else if (equals_literal(key, key_len, "error")) bit = 256u;
        else return 0; /* unknown top-level fields cannot be replayed safely */
        if (seen & bit) return 0;
        if (bit) {
            if (bit == 128u || bit == 256u) {
                if (!parse_value(&p, end, 0u)) return 0;
                seen |= bit;
            } else {
                const char *value = NULL;
                size_t value_len = 0;
                if (!parse_string(&p, end, &value, &value_len) || value_len == 0) return 0;
                seen |= bit;
                if (bit == 32u) {
                    succeeded = equals_literal(value, value_len, "succeeded");
                    if (!succeeded && !equals_literal(value, value_len, "failed")) return 0;
                    status_ok = 1;
                }
                if (bit == 1u || bit == 2u || bit == 4u || bit == 8u || bit == 16u ||
                    bit == 64u) {
                    if (!is_ascii_token(value, value_len, bit == 64u ? 63u : 255u)) return 0;
                }
            }
        }
        skip_ws(&p, end);
        if (p >= end) return 0;
        if (*p == '}') { ++p; break; }
        if (*p++ != ',') return 0;
        skip_ws(&p, end);
    }
    skip_ws(&p, end);
    /* The producer emits exactly one outcome branch. Requiring the matching
     * branch prevents replaying an envelope whose status and payload disagree. */
    const unsigned required = succeeded ? 128u : 256u;
    const unsigned forbidden = succeeded ? 256u : 128u;
    return p == end && status_ok && (seen & 63u) == 63u && (seen & required) != 0u &&
           (seen & forbidden) == 0u;
}

static int queue_header_valid(const char *queue, size_t size) {
    if (!queue || size < GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES) return 0;
    uint32_t magic = 0;
    uint16_t version = 0;
    uint16_t reserved = 0;
    memcpy(&magic, queue, sizeof(magic));
    memcpy(&version, queue + sizeof(magic), sizeof(version));
    memcpy(&reserved, queue + sizeof(magic) + sizeof(version), sizeof(reserved));
    return magic == GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_MAGIC &&
           version == GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_VERSION && reserved == 0;
}

static void queue_header_write(char *queue) {
    const uint32_t magic = GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_MAGIC;
    const uint16_t version = GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_VERSION;
    const uint16_t reserved = 0;
    memcpy(queue, &magic, sizeof(magic));
    memcpy(queue + sizeof(magic), &version, sizeof(version));
    memcpy(queue + sizeof(magic) + sizeof(version), &reserved, sizeof(reserved));
}

static device_status_t upgrade_legacy_queue(const char *legacy, size_t size,
                                            char *out, size_t capacity,
                                            size_t *out_size) {
    if (!legacy || !out || !out_size || size < sizeof(uint32_t) ||
        size > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY ||
        capacity < GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES ||
        ranges_overlap(legacy, size, out, capacity))
        return DEVICE_STATUS_INVALID_ARGUMENT;
    /* A blob carrying the current magic is a damaged current-format record,
     * not a legacy queue. Never reinterpret it with a weaker decoder. */
    uint32_t leading_word = 0;
    memcpy(&leading_word, legacy, sizeof(leading_word));
    if (leading_word == GATEWAY_TOOL_RESULT_OUTBOX_FORMAT_MAGIC)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    size_t in_offset = 0;
    size_t out_offset = GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES;
    uint32_t sequence = 0;
    queue_header_write(out);
    while (in_offset < size) {
        if (size - in_offset < sizeof(uint32_t)) return DEVICE_STATUS_INVALID_ARGUMENT;
        uint32_t len = 0;
        memcpy(&len, legacy + in_offset, sizeof(len));
        if (len == 0 || len > size - in_offset - sizeof(uint32_t) ||
            len > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY || sequence == UINT32_MAX)
            return DEVICE_STATUS_INVALID_ARGUMENT;
        if (gateway_tool_result_outbox_validate_record(
                legacy + in_offset + sizeof(uint32_t), len, len) != DEVICE_STATUS_OK)
            return DEVICE_STATUS_INVALID_ARGUMENT;
        if (out_offset > capacity ||
            out_offset > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY ||
            GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES > capacity - out_offset ||
            GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES >
                GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY - out_offset ||
            len > capacity - out_offset - GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES)
            return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        ++sequence;
        memcpy(out + out_offset, &len, sizeof(len));
        memcpy(out + out_offset + sizeof(len), &sequence, sizeof(sequence));
        memmove(out + out_offset + GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES,
                legacy + in_offset + sizeof(uint32_t), len);
        in_offset += sizeof(uint32_t) + len;
        out_offset += GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + len;
        if (out_offset > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY)
            return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (in_offset != size) return DEVICE_STATUS_INVALID_ARGUMENT;
    *out_size = out_offset;
    return DEVICE_STATUS_OK;
}

device_status_t gateway_tool_result_outbox_upgrade_legacy(const char *legacy_queue,
                                                          size_t legacy_size,
                                                          char *out_queue,
                                                          size_t out_capacity,
                                                          size_t *out_size) {
    return upgrade_legacy_queue(legacy_queue, legacy_size, out_queue,
                                out_capacity, out_size);
}

device_status_t gateway_tool_result_outbox_validate_record(const char *payload,
                                                           size_t stored_size,
                                                           size_t capacity) {
    if (!payload || capacity == 0 || stored_size < 3u || stored_size > capacity ||
        stored_size > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY ||
        payload[0] != '{' || payload[stored_size - 2u] != '}' ||
        payload[stored_size - 1u] != '\0' ||
        /* The terminator must be the first NUL in the stored span.  Using
         * strlen here would read past a truncated/corrupt persistence blob. */
        memchr(payload, '\0', stored_size - 1u) != NULL) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return validate_envelope_schema(payload, stored_size) ? DEVICE_STATUS_OK
                                                          : DEVICE_STATUS_INVALID_ARGUMENT;
}

static device_status_t validate_queue(const char *queue, size_t size, size_t capacity) {
    if (!queue || size < GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES || size > capacity ||
        size > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!queue_header_valid(queue, size)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (size == GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES) return DEVICE_STATUS_OK;
    size_t offset = GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES;
    uint32_t previous_sequence = 0;
    while (offset < size) {
        if (size - offset < GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES)
            return DEVICE_STATUS_INVALID_ARGUMENT;
        uint32_t len = 0;
        uint32_t sequence = 0;
        memcpy(&len, queue + offset, sizeof(len));
        memcpy(&sequence, queue + offset + sizeof(len), sizeof(sequence));
        if (len == 0 || len > size - offset - GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES ||
            len > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY || sequence == 0 ||
            (previous_sequence != 0 && sequence <= previous_sequence))
            return DEVICE_STATUS_INVALID_ARGUMENT;
        if (gateway_tool_result_outbox_validate_record(
                queue + offset + GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES, len, len) != DEVICE_STATUS_OK)
            return DEVICE_STATUS_INVALID_ARGUMENT;
        previous_sequence = sequence;
        offset += GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + len;
    }
    return offset == size ? DEVICE_STATUS_OK : DEVICE_STATUS_INVALID_ARGUMENT;
}

device_status_t gateway_tool_result_outbox_validate_queue(const char *queue,
                                                          size_t queue_size,
                                                          size_t capacity) {
    return validate_queue(queue, queue_size, capacity);
}

device_status_t gateway_tool_result_outbox_append(const char *queue, size_t queue_size,
                                                 const char *record, size_t record_size,
                                                 char *out_queue, size_t out_capacity,
                                                 size_t *out_size) {
    if (!record || !out_queue || !out_size ||
        gateway_tool_result_outbox_validate_record(record, record_size, record_size) != DEVICE_STATUS_OK)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    if (queue_size > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY ||
        (queue_size != 0 && !queue)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    /* The queue prefix is copied before the append record is written.  An
     * exact in-place append (queue == out_queue) is safe because memmove is
     * used and the source remains address-identical; a merely overlapping
     * pair, however, can overwrite unread records before sequence discovery.
     * Reject that ambiguous alias rather than attempting to infer caller
     * buffer layout. */
    if (queue && out_queue != queue &&
        ranges_overlap(queue, queue_size, out_queue, out_capacity)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    size_t normalized_queue_size = queue_size;
    bool legacy_queue = false;
    if (queue_size != 0 && validate_queue(queue, queue_size, queue_size) != DEVICE_STATUS_OK) {
        /* A bounded one-time migration keeps devices upgraded from the old
         * length-only format recoverable. Malformed legacy data remains
         * rejected; callers persist the normalized result atomically with the
         * appended record. */
        const device_status_t upgrade_status = upgrade_legacy_queue(
            queue, queue_size, out_queue, out_capacity, &normalized_queue_size);
        if (upgrade_status != DEVICE_STATUS_OK) return upgrade_status;
        legacy_queue = true;
    }
    if (record_size > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY -
                           GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES -
                           GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES ||
        queue_size > SIZE_MAX - GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES - record_size) {
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    const size_t base = queue_size == 0 ? GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES : normalized_queue_size;
    if (base > SIZE_MAX - GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES - record_size)
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    const size_t needed = base + GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + record_size;
    if (needed > out_capacity || needed > GATEWAY_TOOL_RESULT_OUTBOX_CAPACITY)
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (queue_size && !legacy_queue) memmove(out_queue, queue, queue_size);
    else queue_header_write(out_queue);
    uint32_t sequence = 0;
    if (normalized_queue_size > GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES) {
        size_t offset = GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES;
        const char *sequence_queue = legacy_queue ? out_queue : queue;
        while (offset < normalized_queue_size) {
            memcpy(&sequence, sequence_queue + offset + sizeof(uint32_t), sizeof(sequence));
            uint32_t len = 0;
            memcpy(&len, sequence_queue + offset, sizeof(len));
            offset += GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + len;
        }
    }
    if (sequence == UINT32_MAX) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    ++sequence;
    const uint32_t len = (uint32_t)record_size;
    memcpy(out_queue + base, &len, sizeof(len));
    memcpy(out_queue + base + sizeof(len), &sequence, sizeof(sequence));
    /* The value contract permits caller-owned buffers to overlap (for
     * example, an in-place append using a record borrowed from the queue).
     * Keep the copy overlap-safe just like the queue-prefix move above. */
    memmove(out_queue + base + GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES,
            record, record_size);
    *out_size = needed;
    return DEVICE_STATUS_OK;
}

device_status_t gateway_tool_result_outbox_peek(const char *queue, size_t queue_size,
                                                char *out_record, size_t out_capacity,
                                                size_t *out_record_size) {
    if (!out_record || !out_record_size || validate_queue(queue, queue_size, queue_size) != DEVICE_STATUS_OK)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    if (queue_size == GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    uint32_t len = 0;
    memcpy(&len, queue + GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES, sizeof(len));
    const size_t bytes = len;
    if (bytes > out_capacity) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    memmove(out_record, queue + GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES +
                         GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES, bytes);
    *out_record_size = bytes;
    return DEVICE_STATUS_OK;
}

device_status_t gateway_tool_result_outbox_pop(const char *queue, size_t queue_size,
                                               char *out_queue, size_t out_capacity,
                                               size_t *out_size) {
    if (!out_queue || !out_size || validate_queue(queue, queue_size, queue_size) != DEVICE_STATUS_OK)
        return DEVICE_STATUS_INVALID_ARGUMENT;
    if (queue_size == GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES) {
        *out_size = 0;
        return DEVICE_STATUS_OK;
    }
    uint32_t len = 0;
    memcpy(&len, queue + GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES, sizeof(len));
    const size_t first_end = GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES +
                             GATEWAY_TOOL_RESULT_OUTBOX_RECORD_PREFIX_BYTES + len;
    const size_t remaining = queue_size - first_end;
    if (remaining > out_capacity) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (remaining) {
        queue_header_write(out_queue);
        memmove(out_queue + GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES,
                queue + first_end, remaining);
        *out_size = GATEWAY_TOOL_RESULT_OUTBOX_HEADER_BYTES + remaining;
    } else {
        *out_size = 0;
    }
    return DEVICE_STATUS_OK;
}
