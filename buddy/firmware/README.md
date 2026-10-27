# buddy firmware

ESP-IDF 6.0.2 project for the MaClaw USB companion display
(EchoEar-2ST / ESP32-S3-R8, ST77916 360×360 round panel).

## Build

```bat
cd buddy\firmware
build.cmd -B build build
```

`build.cmd` clears `MSYSTEM` (export.bat refuses to run under a Git Bash
environment) and calls `C:\esp\v6.0.2\esp-idf\export.bat`.

Output: `build/buddy_firmware.bin` — 1.87 MB, 49% of the 3.625 MB app
partition. `CONFIG_ESP_CONSOLE_NONE` and `CONFIG_ESP_CONSOLE_SECONDARY_NONE`
keep the USB port free of ESP-IDF log output; diagnostics travel as protocol
`log` frames.

## Layout

| Path | Purpose |
| --- | --- |
| `main/buddy_display.c` | QSPI ST77916 init (vendor sequence), LEDC backlight, PSRAM framebuffer, 40-row internal-DMA stripes |
| `main/buddy_font.c` | ASCII 5×7 + packed 24×24 CJK (`cjk24_cjk.bin` U+4E00–U+9FFF + generated `cjk24_ext.bin` punctuation/full-width), box fallback counts missing glyphs |
| `main/buddy_ui.c` | Task page (counters/title/progress), standby page (pet + weather + date), prompt page |
| `main/buddy_protocol.c` | USB-Serial/JTAG driver, line framing, cJSON parse, seq gate, acks, heartbeat |
| `main/buddy_state.c` | Shared view + NVS weather cache (namespace `buddy`) |
| `main/buddy_touch.c` | Best-effort CST816 polling on I2C 11/12; missing controller is non-fatal |
| `main/main.c` | Boot order: NVS → state → display → touch → protocol → UI task |
| `components/cjson`, `components/esp_lcd_st77916` | Vendored components (no network needed) |
| `tools/generate_buddy_font.py` | Regenerates `cjk24_ext.bin` + index from `C:\Windows\Fonts\msyh.ttc` |

## Partitioning

`partitions.csv` is a byte-for-byte copy of `iot-agentos/partitions.csv`
(16 MB layout): app at `0x10000` (3.625 MB), `model`/`storage` untouched, so
flashing only the app keeps NVS and recording data. `tools/flash-buddy.ps1`
backs up the previous app image before writing.
