import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/bots/widgets.dart';

void main() {
  group('linkifyMarkdownUrls', () {
    test('wraps a bare url in a markdown link', () {
      expect(
        linkifyMarkdownUrls('看 https://example.com/a 这个'),
        '看 [https://example.com/a](https://example.com/a) 这个',
      );
    });

    test('keeps CJK sentence punctuation outside the link', () {
      expect(
        linkifyMarkdownUrls('链接：https://a.com/b。'),
        '链接：[https://a.com/b](https://a.com/b)。',
      );
    });

    test('leaves existing markdown links alone', () {
      const s = '点 [这里](https://a.com/b) 和 <https://c.com/d>';
      expect(linkifyMarkdownUrls(s), s);
    });

    test('skips fenced code blocks and inline code', () {
      const s = '```\nhttps://a.com/x\n```\n`https://b.com/y` https://c.com/z';
      expect(
        linkifyMarkdownUrls(s),
        '```\nhttps://a.com/x\n```\n`https://b.com/y` [https://c.com/z](https://c.com/z)',
      );
    });

    test('trims trailing ascii punctuation', () {
      expect(
        linkifyMarkdownUrls('(https://a.com/p).'),
        '([https://a.com/p](https://a.com/p)).',
      );
    });

    test('does not nest a link whose display text is a url', () {
      const s = '[https://a.com/x](https://b.com/y)';
      expect(linkifyMarkdownUrls(s), s);
    });
  });
}
