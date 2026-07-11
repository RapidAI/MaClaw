import 'mobile_bootstrap.dart';

bool isTrustedPhoneCreditsAccount(String value) {
  return trustedPhoneCreditsAccount(value).isNotEmpty;
}

String trustedPhoneCreditsAccount(String value) {
  final normalized = value.trim().toLowerCase();
  if (!normalized.startsWith('phone:')) return '';
  final phone = normalized.substring('phone:'.length);
  if (phone.isEmpty) return '';
  final allDigits =
      phone.codeUnits.every((codeUnit) => codeUnit >= 48 && codeUnit <= 57);
  return allDigits ? normalized : '';
}

String trustedBootstrapCreditsAccount(MobileBootstrap? bootstrap) {
  if (bootstrap == null) return '';
  final llmCredits = trustedPhoneCreditsAccount(
    bootstrap.llmAccess.creditsAccount,
  );
  if (llmCredits.isNotEmpty) return llmCredits;
  return trustedPhoneCreditsAccount(bootstrap.user.creditsAccount);
}

bool isMobileLlmConfigured(MobileBootstrap? bootstrap) {
  if (bootstrap == null) return false;
  final status = bootstrap.llmAccess.status.trim().toLowerCase();
  final available = switch (status) {
    'available' || 'authorized' || 'active' || 'ready' || 'configured' => true,
    _ => false,
  };
  if (!available) return false;
  if (bootstrap.llmAccess.official) {
    return isTrustedPhoneCreditsAccount(bootstrap.llmAccess.creditsAccount);
  }
  if (bootstrap.llmAccess.desktopQrDelegated) {
    return bootstrap.llmAccess.authorizationId.trim().isNotEmpty;
  }
  return false;
}

/// Resolves first-tab mode: official Hub assistant vs own digital twin.
///
/// When [bootstrap] is null (pre-session), default to official labeling so the
/// shell does not flash "数字分身" before Hub entitlements load.
String resolveMobileAssistantMode(MobileBootstrap? bootstrap) {
  if (bootstrap == null) return mobileAssistantModeOfficial;
  final declared = bootstrap.assistantMode.trim().toLowerCase();
  if (declared == mobileAssistantModeOfficial ||
      declared == mobileAssistantModeDigitalTwin) {
    return declared;
  }
  if (bootstrap.entitlements.mobileOfficial && isMobileLlmConfigured(bootstrap)) {
    return mobileAssistantModeOfficial;
  }
  if (isMobileLlmConfigured(bootstrap)) {
    return mobileAssistantModeOfficial;
  }
  return mobileAssistantModeDigitalTwin;
}

bool usesDigitalTwinAssistant(MobileBootstrap? bootstrap) {
  return resolveMobileAssistantMode(bootstrap) == mobileAssistantModeDigitalTwin;
}

String mobileAssistantTabLabel(MobileBootstrap? bootstrap) {
  return usesDigitalTwinAssistant(bootstrap) ? '数字分身' : 'AI助手';
}
