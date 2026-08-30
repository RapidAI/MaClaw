#include "services/pet_asset_retry_service.h"

#include <string.h>

typedef struct {
    char message_id[PET_ASSET_RETRY_SERVICE_MESSAGE_ID_CAPACITY];
    uint32_t consecutive_failures;
} pet_asset_retry_state_t;

static pet_asset_retry_state_t s_state;

void pet_asset_retry_service_init(void) {
    pet_asset_retry_service_reset();
}

void pet_asset_retry_service_reset(void) {
    memset(&s_state, 0, sizeof(s_state));
}

uint32_t pet_asset_retry_service_note_failure(const char *message_id) {
    const char *id = message_id ? message_id : "";
    if (strcmp(s_state.message_id, id) != 0) {
        memset(&s_state, 0, sizeof(s_state));
        const size_t length = strlen(id);
        const size_t copy_length = length < sizeof(s_state.message_id) - 1u
                                       ? length
                                       : sizeof(s_state.message_id) - 1u;
        memcpy(s_state.message_id, id, copy_length);
        s_state.message_id[copy_length] = '\0';
    }
    if (s_state.consecutive_failures != UINT32_MAX) {
        ++s_state.consecutive_failures;
    }
    return s_state.consecutive_failures;
}

bool pet_asset_retry_service_exhausted(uint32_t retry_limit) {
    return retry_limit != 0 && s_state.consecutive_failures >= retry_limit;
}
