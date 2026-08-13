#pragma once

/* Compile-time compact Connectivity selection; only its service can reach
 * profile transport details such as the ML307 implementation. */
#include "sdkconfig.h"

#define MACLAW_COMPACT_CONNECTIVITY_ADAPTER_IMPLEMENTATION 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_cellular_adapter.h"
#else
#include "boards/bread_compact/bread_connectivity_adapter.h"
#endif
#undef MACLAW_COMPACT_CONNECTIVITY_ADAPTER_IMPLEMENTATION
