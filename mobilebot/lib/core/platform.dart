import 'dart:io' show Platform;

/// HarmonyOS NEXT (纯血鸿蒙) detection. The OpenHarmony Flutter fork reports
/// `Platform.operatingSystem == 'ohos'`; stock Dart has no `Platform.isOhos`.
final bool isOhos = Platform.operatingSystem == 'ohos';

/// Sideloadable self-update channel exists only where installing a downloaded
/// package is allowed: Android APK. HarmonyOS NEXT updates must come through
/// AppGallery; iOS through the App Store.
final bool supportsSideloadUpdate = Platform.isAndroid && !isOhos;

/// Speech input needs `record` + `speech_to_text` platform plugins — neither
/// has an OpenHarmony port today, so the mic affordance is hidden on ohos.
final bool supportsVoiceInput =
    (Platform.isAndroid || Platform.isIOS) && !isOhos;

/// Text-to-speech readback — `flutter_tts` covers Android/iOS; it has no
/// OpenHarmony port today, so voice-assistant readback is hidden on ohos.
final bool supportsVoiceOutput =
    (Platform.isAndroid || Platform.isIOS) && !isOhos;
