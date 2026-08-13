#pragma once

/* Compile-time compact Audio selection; only compact_audio_service owns it. */
#include "sdkconfig.h"

#define MACLAW_COMPACT_AUDIO_ADAPTER_IMPLEMENTATION 1
#if CONFIG_MACLAW_BOARD_FANGTANG_4G
#include "boards/fangtang_4g/fangtang_audio_adapter.h"
#else
#include "boards/bread_compact/bread_audio_adapter.h"
#endif
#undef MACLAW_COMPACT_AUDIO_ADAPTER_IMPLEMENTATION
