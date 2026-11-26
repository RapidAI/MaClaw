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

  test('long answers are summarized at a sentence end, not cut mid-word', () {
    final body = List.generate(40, (i) => '第${i + 1}项结果都已经核对完毕。').join();
    final out = plainTextForSpeech(body);
    expect(out.length, lessThan(maxSpeechSummaryChars + 60));
    expect(out, contains('第1项结果'));
    expect(out, endsWith('以上是要点，完整内容请看屏幕'));
    // A sentence is never halved by the summary.
    expect(out, isNot(contains('。以上')));
  });

  test('short answers are spoken whole', () {
    const md = '北京今天多云，最高 18 度。';
    expect(plainTextForSpeech(md), md);
  });

  test('segments split at sentence ends and stay under the model budget', () {
    final segs = splitSpeechSegments(
      '第一句话说完了。第二句也结束了！第三句呢？第四句接着。',
    );
    expect(segs.length, greaterThan(1));
    expect(segs.every((s) => s.length <= speechSegmentMaxChars), isTrue);
    expect(segs.join(), contains('第一句话说完了'));
  });

  test('a very long segment without punctuation is hard split', () {
    final segs = splitSpeechSegments('字' * 200);
    expect(segs.length, 200 ~/ speechSegmentMaxChars + 1);
    expect(segs.every((s) => s.length <= speechSegmentMaxChars), isTrue);
  });

  test('very long replies are truncated with a spoken pointer', () {
    final out = plainTextForSpeech('字' * 3000);
    expect(out.length, lessThan(1300));
    expect(out, endsWith('完整内容请查看屏幕'));
  });
}
