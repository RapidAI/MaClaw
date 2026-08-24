#include "configuration_revision.h"

#include <stdint.h>

bool configuration_revision_next(uint64_t current, uint64_t *out_next) {
    if (!out_next || current == UINT64_MAX) return false;
    *out_next = current + 1u;
    return true;
}
