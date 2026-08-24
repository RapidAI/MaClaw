#pragma once

/* Selected physical Storage profile seam.  Platform Storage owns the SPIFFS
 * mount transaction; this private bridge exposes only profile-local admission
 * for rebuildable, non-critical flash work. */

#include <stdbool.h>

bool platform_storage_profile_allows_optional_flash_work(void);
