#pragma once
#include <stdbool.h>
#include <stddef.h>
#include "device_api.h"

/* Value-only entropy barrier. Hardware RNG details stay private. */
device_status_t entropy_service_init(void);
bool entropy_service_fill(void *buffer, size_t length);
bool entropy_service_ready(void);
