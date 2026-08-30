#pragma once

#include <stdbool.h>
#include <stddef.h>

/* Value-only validation for enterprise EAP server identity.  The physical
 * owner supplies the validated name to ESP-IDF; this module has no SDK or
 * certificate-storage dependency. */
bool wifi_enterprise_trust_policy_valid_domain(const char *domain,
                                               size_t capacity);
