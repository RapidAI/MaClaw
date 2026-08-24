#include <stdio.h>

#include "input_scanner_lifecycle_gate.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    input_scanner_lifecycle_gate_t gate = {0};

    CHECK(input_scanner_lifecycle_gate_allows_start(&gate, false));
    CHECK(!input_scanner_lifecycle_gate_allows_start(&gate, true));

    /* A timeout/error can leave a profile-private scanner task publishing to
     * its old queue. The next generation stays closed rather than risking a
     * second scanner or a stale callback. */
    input_scanner_lifecycle_gate_note_stop_failed(&gate);
    CHECK(gate.scanner_recovery_required);
    CHECK(!input_scanner_lifecycle_gate_allows_start(&gate, false));

    /* Only an observed successful join reopens the next input generation. */
    input_scanner_lifecycle_gate_note_stop_succeeded(&gate);
    CHECK(!gate.scanner_recovery_required);
    CHECK(input_scanner_lifecycle_gate_allows_start(&gate, false));

    puts("PASS Input scanner lifecycle permits restart only after successful join");
    return 0;
}
