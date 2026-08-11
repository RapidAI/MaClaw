#include "platform_storage.h"

#include "board_port.h"

bool platform_storage_allows_optional_flash_work(void) {
    return board_port_allows_optional_flash_work();
}
