#pragma once

#include <stddef.h>
#include <stdint.h>

#define MALLOC_CAP_8BIT (1u << 3)
#define MALLOC_CAP_SPIRAM (1u << 10)
#define MALLOC_CAP_INTERNAL (1u << 11)

/* Defined by the host test so allocation failure can be injected. */
void *heap_caps_malloc(size_t size, uint32_t caps);
void heap_caps_free(void *ptr);
