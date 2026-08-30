#include "wifi_enterprise_trust_policy.h"

#include <string.h>

bool wifi_enterprise_trust_policy_valid_domain(const char *domain,
                                               size_t capacity) {
    if (!domain || capacity < 2u) return false;
    const size_t length = strnlen(domain, capacity);
    if (length == 0u || length >= capacity || length > 253u ||
        domain[0] == '.' || domain[0] == '-' || domain[length - 1u] == '.' ||
        domain[length - 1u] == '-') return false;
    size_t label_length = 0u;
    bool has_dot = false;
    for (size_t i = 0u; i < length; ++i) {
        const unsigned char c = (unsigned char)domain[i];
        if (c == '.') {
            if (label_length == 0u || label_length > 63u) return false;
            has_dot = true;
            label_length = 0u;
            continue;
        }
        if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
              (c >= '0' && c <= '9') || c == '-')) return false;
        if (label_length == 0u && c == '-') return false;
        ++label_length;
        if (label_length > 63u) return false;
    }
    /* Require a DNS-style name with an explicit suffix; this prevents an
     * accidental short name from becoming an ambiguous certificate binding. */
    return has_dot && label_length > 0u && label_length <= 63u &&
           domain[length - 1u] != '-';
}
