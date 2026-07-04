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
