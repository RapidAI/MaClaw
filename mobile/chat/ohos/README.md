# HarmonyOS (OHOS) Build Configuration

## Prerequisites

1. Install DevEco Studio 4.0+ (HarmonyOS IDE)
2. Install flutter_ohos SDK fork: https://gitee.com/aspect-aspect/flutter_ohos
3. Set environment variable: `export OHOS_SDK_HOME=/path/to/ohos-sdk`

## Setup

```bash
# Switch to the OHOS flutter fork
flutter config --ohos-sdk $OHOS_SDK_HOME

# Verify
flutter doctor -v
```

## Build

```bash
flutter build hap --release
```

## Notes

- The OHOS entry point is `ohos/entry/src/main/ets/entryability/EntryAbility.ets`
- Native plugins (WebRTC, push) require OHOS-specific implementations in `ohos/entry/`
- HMS Push Kit replaces FCM on HarmonyOS devices
