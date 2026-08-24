#include <limits.h>
#include <stdio.h>

#include "fault_domain.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    fault_domain_t domain = {0};
    fault_domain_snapshot_t snapshot = {0};

    fault_domain_reset(&domain, FAULT_DOMAIN_ID_STORAGE);
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.id == FAULT_DOMAIN_ID_STORAGE);
    CHECK(snapshot.phase == FAULT_DOMAIN_STOPPED);
    CHECK(snapshot.generation == 1u);
    CHECK(!snapshot.admission_open);
    CHECK(!fault_domain_begin_self_test(&domain));

    CHECK(fault_domain_begin_start(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_REINITIALIZING);
    CHECK(snapshot.generation == 2u);
    CHECK(!snapshot.admission_open);
    CHECK(fault_domain_begin_self_test(&domain));
    CHECK(fault_domain_mark_ready(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_READY);
    CHECK(snapshot.admission_open);

    CHECK(fault_domain_begin_quiesce(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_QUIESCING);
    CHECK(snapshot.generation == 3u);
    CHECK(!snapshot.admission_open);
    CHECK(fault_domain_mark_unknown_outcome(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_UNKNOWN_OUTCOME);
    CHECK(!snapshot.admission_open);
    CHECK(fault_domain_begin_quiesce(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.generation == 4u);
    CHECK(fault_domain_mark_stopped(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_STOPPED);

    CHECK(fault_domain_begin_start(&domain));
    CHECK(fault_domain_mark_unknown_outcome(&domain));
    CHECK(fault_domain_begin_quiesce(&domain));
    CHECK(fault_domain_mark_degraded(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_DEGRADED);
    CHECK(!snapshot.admission_open);
    CHECK(!fault_domain_begin_start(&domain));

    fault_domain_reset(&domain, FAULT_DOMAIN_ID_STORAGE);
    domain.generation = UINT32_MAX;
    CHECK(!fault_domain_begin_start(&domain));
    CHECK(fault_domain_get_snapshot(&domain, &snapshot));
    CHECK(snapshot.phase == FAULT_DOMAIN_STOPPED);
    CHECK(snapshot.generation == UINT32_MAX);

    puts("PASS Fault Domain lifecycle keeps admission closed until self-test and handles unknown outcomes");
    return 0;
}
