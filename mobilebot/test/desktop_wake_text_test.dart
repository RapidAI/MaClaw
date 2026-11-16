import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/bots/desktop_wake_text.dart';

void main() {
  test('an offline intranet computer keeps its own sentence', () {
    expect(
      desktopWakeFailureText('HubApiException(NODE_OFFLINE, 502): 这台内网电脑暂时连不上'),
      '这台内网电脑暂时连不上',
    );
  });

  test('a refused public connection stays the service line', () {
    expect(
      desktopWakeFailureText('connect: connection refused'),
      '电脑服务暂时连不上',
    );
  });
}
