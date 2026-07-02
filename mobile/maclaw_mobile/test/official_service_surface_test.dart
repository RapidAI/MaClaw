import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('mobile UI exposes no custom Hub URL configuration surface', () {
    final root = Directory('lib');
    final sources = root
        .listSync(recursive: true)
        .whereType<File>()
        .where((file) => file.path.endsWith('.dart'));

    final forbidden = <RegExp>[
      RegExp(r'custom\s+hub', caseSensitive: false),
      RegExp(r'自定义\s*(Hub|服务|地址)', caseSensitive: false),
      RegExp(r'(Hub|服务)\s*(URL|地址)\s*(设置|配置|输入)', caseSensitive: false),
      RegExp(r'custom(Hub|Service|Base)Url', caseSensitive: false),
      RegExp(
        r'(hubUrl|hub_url)\s*=\s*TextEditingController',
        caseSensitive: false,
      ),
      RegExp(
        r"""labelText:\s*['"][^'"]*(Hub|服务).*?(URL|地址)""",
        caseSensitive: false,
      ),
      RegExp(
        r"""hintText:\s*['"][^'"]*(Hub|服务).*?(URL|地址)""",
        caseSensitive: false,
      ),
    ];

    final offenders = <String>[];
    for (final file in sources) {
      final path = file.path.replaceAll('\\', '/');
      final text = file.readAsStringSync();
      final compactText = text.replaceAll(RegExp(r'\s+'), ' ');
      for (final pattern in forbidden) {
        if (pattern.hasMatch(text)) {
          offenders.add('$path matches ${pattern.pattern}');
        }
      }
      if (compactText.contains('saveSession(') &&
          compactText.contains('hubUrl:') &&
          !compactText.contains('hubUrl: result.hubUrl')) {
        offenders.add('$path saves a Hub URL outside HubCenter discovery');
      }
    }

    expect(offenders, isEmpty);
  });
}
