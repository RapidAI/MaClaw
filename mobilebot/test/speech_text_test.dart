import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/speech_text.dart';

void main() {
  test('strips markdown structure, keeps prose', () {
    const md =
        '# 标题\n\n**重要**：请查看 [文档](https://x.com)。\n'
        '- 第一项\n- 第二项\n\n`code` 与 <https://bare.link>\n';
    final out = plainTextForSpeech(md);
    expect(out, contains('标题'));
    expect(out, contains('重要：请查看 文档'));
    expect(out, contains('第一项'));
    expect(out, isNot(contains('**')));
    expect(out, isNot(contains('https')));
    expect(out, isNot(contains('`')));
  });

  test('fenced code is named, not read aloud', () {
    final out = plainTextForSpeech('结果：\n```dart\nvoid main() {}\n```\n完成');
    expect(out, contains('代码内容见屏幕'));
    expect(out, isNot(contains('void')));
    expect(out, contains('完成'));
  });

  test('images and strikethrough handled', () {
    final out = plainTextForSpeech('![图](a.png) ~~作废~~ 保留');
    expect(out.trim(), '作废 保留');
  });

  test('very long replies are truncated with a spoken pointer', () {
    final out = plainTextForSpeech('字' * 3000);
    expect(out.length, lessThan(1300));
    expect(out, endsWith('完整内容请查看屏幕'));
  });
}
