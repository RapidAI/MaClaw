#include <stdio.h>

#include "firmware_identity_sleep_gate.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    volatile bool preparing = false;
    volatile uint32_t observers = 0;

    CHECK(firmware_identity_sleep_gate_begin(&preparing, &observers));
    CHECK(observers == 1);

    /* This models PREPARE closing admission while a pre-fence reader is still
     * formatting a snapshot.  No later observer may enter, but the old one
     * can finish so the owner can observe an actual drained safe point. */
    __atomic_store_n(&preparing, true, __ATOMIC_RELEASE);
    CHECK(!firmware_identity_sleep_gate_begin(&preparing, &observers));
    CHECK(observers == 1);
    firmware_identity_sleep_gate_end(&observers);
    CHECK(observers == 0);
    CHECK(!firmware_identity_sleep_gate_begin(&preparing, &observers));

    /* ABORT reopens exactly the same gate; it does not fabricate an in-flight
     * observer or require a worker recreation. */
    __atomic_store_n(&preparing, false, __ATOMIC_RELEASE);
    CHECK(firmware_identity_sleep_gate_begin(&preparing, &observers));
    CHECK(observers == 1);
    firmware_identity_sleep_gate_end(&observers);
    CHECK(observers == 0);

    puts("PASS Firmware Identity System Sleep observer admission");
    return 0;
}
