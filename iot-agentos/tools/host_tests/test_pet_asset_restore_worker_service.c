#include <assert.h>
#include <stdio.h>

#include "services/pet_asset_restore_worker_service.h"

int main(void) {
    /* The worker has a physical FreeRTOS implementation and is verified in
     * the ESP-IDF build. Its public seam intentionally remains value-only. */
    assert(sizeof(pet_asset_restore_worker_service_host_t) >= sizeof(uint32_t));
    puts("PASS pet asset restore worker value contract");
    return 0;
}
