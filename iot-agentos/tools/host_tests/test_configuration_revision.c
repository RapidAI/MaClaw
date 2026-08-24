#include <stdint.h>
#include <stdio.h>

#include "configuration_revision.h"

#define CHECK(expr) do { \
    if (!(expr)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #expr); \
        return 1; \
    } \
} while (0)

int main(void) {
    uint64_t next = 99u;
    CHECK(configuration_revision_next(0u, &next));
    CHECK(next == 1u);
    CHECK(configuration_revision_next(1u, &next));
    CHECK(next == 2u);

    next = 77u;
    CHECK(!configuration_revision_next(UINT64_MAX, &next));
    CHECK(next == 77u);
    CHECK(!configuration_revision_next(4u, NULL));

    puts("PASS configuration revision is monotonic and fails closed at overflow");
    return 0;
}
