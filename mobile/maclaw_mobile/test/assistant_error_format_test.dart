import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';

void main() {
  test('formatAssistantError maps receive timeout to friendly Chinese', () {
    final error = DioException(
      requestOptions: RequestOptions(path: '/api/mobile/search'),
      type: DioExceptionType.receiveTimeout,
      message: 'The request took longer than 0:00:15.000000 to receive data.',
    );
    final text = formatAssistantError(error);
    expect(text, contains('超时'));
    expect(text, isNot(contains('DioException')));
    expect(text, isNot(contains('0:00:15')));
  });

  test('formatAssistantError keeps short StateError messages', () {
    expect(
      formatAssistantError(StateError('助手未返回结果，请重试。')),
      contains('助手未返回结果'),
    );
  });
}
