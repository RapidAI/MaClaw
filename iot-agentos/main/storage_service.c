#include "storage_service.h"

#include "platform_storage.h"

bool storage_service_allows_optional_flash_work(void) {
    return platform_storage_allows_optional_flash_work();
}
