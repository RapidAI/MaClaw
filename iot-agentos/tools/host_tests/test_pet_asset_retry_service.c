#include <assert.h>
#include <stdio.h>

#include "services/pet_asset_retry_service.h"

int main(void) {
    pet_asset_retry_service_init();
    assert(pet_asset_retry_service_note_failure("asset-1") == 1u);
    assert(!pet_asset_retry_service_exhausted(
        PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT));
    assert(pet_asset_retry_service_note_failure("asset-1") == 2u);
    assert(pet_asset_retry_service_note_failure("asset-1") == 3u);
    assert(pet_asset_retry_service_exhausted(
        PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT));
    assert(!pet_asset_retry_service_exhausted(0));

    /* The next ordered page head is a distinct message, so it must not
     * inherit a permanent failed-ACK decision from the previous asset. */
    assert(pet_asset_retry_service_note_failure("asset-2") == 1u);
    assert(!pet_asset_retry_service_exhausted(
        PET_ASSET_RETRY_SERVICE_DEFAULT_LIMIT));

    pet_asset_retry_service_reset();
    assert(pet_asset_retry_service_note_failure("asset-3") == 1u);
    puts("PASS pet asset ordered retry state");
    return 0;
}
