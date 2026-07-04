import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_credits.dart';

void main() {
  test('trustedPhoneCreditsAccount only accepts digits-only phone credits', () {
    expect(
      trustedPhoneCreditsAccount('phone:19900001111'),
      'phone:19900001111',
    );
    expect(
      trustedPhoneCreditsAccount(' PHONE:19900001111 '),
      'phone:19900001111',
    );
    expect(isTrustedPhoneCreditsAccount('phone:19900001111'), isTrue);
    expect(trustedPhoneCreditsAccount('phone:199 0000-1111'), '');
    expect(trustedPhoneCreditsAccount('phone:user19900001111'), '');
    expect(trustedPhoneCreditsAccount('account:19900001111'), '');
    expect(isTrustedPhoneCreditsAccount('phone:user19900001111'), isFalse);
  });

  test('trustedBootstrapCreditsAccount falls back from LLM to user credits',
      () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-mobile',
        'phone_number': '19900001111',
        'tenant_id': 'tenant-a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
        'credits_account': 'phone:user19900001111',
      },
    });

    expect(trustedBootstrapCreditsAccount(bootstrap), 'phone:19900001111');
    expect(trustedBootstrapCreditsAccount(null), '');
  });
}
