#include "platform_storage_profile.h"

#include "legacy_storage_admission.h"

bool platform_storage_profile_allows_optional_flash_work(void) {
    return legacy_storage_admission_allows_optional_flash_work();
}
