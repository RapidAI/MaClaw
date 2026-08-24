#include <stdio.h>

#include "wake_deadline_sleep_gate.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    volatile bool preparing = false;
    volatile uint32_t callbacks = 0;

    CHECK(wake_deadline_sleep_gate_begin(&preparing));
    CHECK(preparing);
    CHECK(!wake_deadline_sleep_gate_begin(&preparing));

    /* A callback selected before PREPARE may drain, but the service must not
     * reopen delivery merely because its bounded wait timed out. */
    callbacks = 1;
    CHECK(!wake_deadline_sleep_gate_callbacks_drained(&callbacks));
    CHECK(preparing);
    callbacks = 0;
    CHECK(wake_deadline_sleep_gate_callbacks_drained(&callbacks));
    CHECK(preparing);

    /* Only the parent transaction's explicit ABORT reopens admission. */
    wake_deadline_sleep_gate_abort(&preparing);
    CHECK(!preparing);
    CHECK(wake_deadline_sleep_gate_begin(&preparing));
    wake_deadline_sleep_gate_abort(&preparing);
    CHECK(!preparing);

    puts("PASS Wake Deadline System Sleep gate retains closed admission until ABORT");
    return 0;
}
