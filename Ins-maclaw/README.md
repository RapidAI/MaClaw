# Ins-maclaw

Small native GUI/TUI bootstrap installer for MaClaw release packages.

It uses the same release discovery chain as the MaClaw online updater:

1. GitHub latest `latest.json`
2. Cloudflare R2 `latest.json`
3. Tencent COS `latest.json`
4. GitHub release asset URL plus R2/COS asset fallbacks
5. SHA-256 verification when the release manifest provides a digest

Supported targets:

- Windows: downloads `<Brand>-Setup.exe`; release includes GUI-subsystem and console-subsystem bootstrap variants
- macOS: downloads `<Brand>-Universal.pkg`
- Linux: downloads the matching AppImage for `amd64`/`arm64` and Ubuntu 22.04/24.04 WebKit ABI

Default brand is `maclaw`, displayed as `MaClaw (原厂品牌)` in Chinese and `MaClaw (Original Brand)` in English.
Optional OEM brand is `tigerclaw`, displayed as `TigerClaw (奇安信 OEM 版)` in Chinese and `TigerClaw (QiAnXin OEM Edition)` in English.

## Language

Ins-maclaw detects the operating system UI language automatically. Chinese systems use Simplified Chinese; other systems use English. Set `INS_MACLAW_LANG=zh` / `INS_MACLAW_LANG=en`, or pass `-lang zh` / `-lang en`, to force a language for testing.

## Usage

```powershell
# Auto mode: double-click/Explorer opens GUI on Windows; terminal opens TUI
Ins-maclaw.exe

# Explicit modes
Ins-maclaw.exe -mode gui
Ins-maclaw.exe -mode tui
Ins-maclaw.exe -mode cli

# Direct brand/check/download controls
Ins-maclaw.exe -brand maclaw
Ins-maclaw.exe -brand tigerclaw
Ins-maclaw.exe -check
Ins-maclaw.exe -no-launch
Ins-maclaw.exe -lang zh
```

`-gui` and `-tui` are shorthand aliases for `-mode gui` and `-mode tui`. Windows release builds include GUI-subsystem launchers for double-click use and `*-tui.exe` console-subsystem launchers for terminal/script use. GUI mode uses a native wizard with a branded side panel, language indicator, step markers, brand selection, progress bar, live progress text, and final result page. Windows GUI builds also embed product version metadata and the MaClaw icon when `windres` is available during packaging.

## Build

```powershell
powershell -ExecutionPolicy Bypass -File .\Ins-maclaw\build.ps1
```

Outputs are written to `dist/Ins-maclaw/`.
