#pragma once

#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

// Decodes an in-memory MPEG Layer I/II/III stream and plays it through the
// active board port. ID3 metadata and variable-bit-rate streams are accepted.
esp_err_t mp3_player_play(const uint8_t *mp3, size_t mp3_len);
