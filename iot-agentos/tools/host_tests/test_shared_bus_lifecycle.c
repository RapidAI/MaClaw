#include <stdio.h>

#include "shared_bus_lifecycle.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "check failed at %d: %s\n", __LINE__, #condition); \
        return 1; \
    } \
} while (0)

#define MAKE_READY(lifecycle) do { \
    shared_bus_lifecycle_reset(&(lifecycle), FAULT_DOMAIN_ID_SHARED_BUS); \
    CHECK(shared_bus_lifecycle_begin_reinitialize(&(lifecycle)) == \
          SHARED_BUS_LIFECYCLE_OK); \
    CHECK(shared_bus_lifecycle_mark_attached(&(lifecycle)) == \
          SHARED_BUS_LIFECYCLE_OK); \
    CHECK(shared_bus_lifecycle_begin_self_test(&(lifecycle)) == \
          SHARED_BUS_LIFECYCLE_OK); \
    CHECK(shared_bus_lifecycle_mark_ready(&(lifecycle)) == SHARED_BUS_LIFECYCLE_OK); \
} while (0)

int main(void) {
    shared_bus_lifecycle_t lifecycle = {0};
    /* A controller-bus creation failure occurs before any handle exists. It
     * may be retried, unlike a failed cleanup after an attached bus. */
    shared_bus_lifecycle_reset(&lifecycle, FAULT_DOMAIN_ID_SHARED_BUS);
    CHECK(shared_bus_lifecycle_begin_reinitialize(&lifecycle) ==
          SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_cancel_unattached_start(&lifecycle) ==
          SHARED_BUS_LIFECYCLE_OK);
    shared_bus_lifecycle_snapshot_t snapshot = {0};
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_STOPPED &&
          snapshot.attachment == SHARED_BUS_ATTACHMENT_DETACHED);

    MAKE_READY(lifecycle);
    shared_bus_lease_t codec = {0};
    shared_bus_lease_t touch = {0};
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_READY &&
          snapshot.domain.admission_open && snapshot.active_lease_count == 0u);
    const uint32_t first_generation = snapshot.domain.generation;

    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &codec) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &touch) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(codec.generation == first_generation && touch.generation == first_generation);
    CHECK(codec.token != touch.token);
    CHECK(shared_bus_lifecycle_begin_recovery(&lifecycle) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_QUIESCING &&
          !snapshot.domain.admission_open && snapshot.domain.generation != first_generation);
    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &codec) ==
          SHARED_BUS_LIFECYCLE_UNAVAILABLE);
    CHECK(!shared_bus_lifecycle_can_detach(&lifecycle));
    CHECK(shared_bus_lifecycle_release(&lifecycle, &codec) ==
          SHARED_BUS_LIFECYCLE_STALE_LEASE);
    CHECK(!shared_bus_lifecycle_can_detach(&lifecycle));
    CHECK(shared_bus_lifecycle_release(&lifecycle, &touch) ==
          SHARED_BUS_LIFECYCLE_STALE_LEASE);
    CHECK(shared_bus_lifecycle_can_detach(&lifecycle));
    CHECK(shared_bus_lifecycle_mark_detached(&lifecycle) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_get_snapshot(&lifecycle, &snapshot));
    CHECK(snapshot.domain.phase == FAULT_DOMAIN_STOPPED &&
          snapshot.attachment == SHARED_BUS_ATTACHMENT_DETACHED);

    CHECK(shared_bus_lifecycle_begin_reinitialize(&lifecycle) ==
          SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_mark_attached(&lifecycle) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_begin_self_test(&lifecycle) ==
          SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_mark_unknown(&lifecycle) ==
          SHARED_BUS_LIFECYCLE_UNKNOWN_OUTCOME);
    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &codec) ==
          SHARED_BUS_LIFECYCLE_UNAVAILABLE);
    CHECK(shared_bus_lifecycle_begin_recovery(&lifecycle) == SHARED_BUS_LIFECYCLE_OK);
    CHECK(shared_bus_lifecycle_can_detach(&lifecycle));
    CHECK(shared_bus_lifecycle_mark_detached(&lifecycle) == SHARED_BUS_LIFECYCLE_OK);

    MAKE_READY(lifecycle);
    shared_bus_lease_t leases[SHARED_BUS_LIFECYCLE_MAX_LEASES] = {{0}};
    for (uint32_t i = 0u; i < SHARED_BUS_LIFECYCLE_MAX_LEASES; ++i) {
        CHECK(shared_bus_lifecycle_acquire(&lifecycle, &leases[i]) ==
              SHARED_BUS_LIFECYCLE_OK);
    }
    CHECK(shared_bus_lifecycle_acquire(&lifecycle, &codec) ==
          SHARED_BUS_LIFECYCLE_RESOURCE_EXHAUSTED);
    return 0;
}
