import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/features/assistant/assistant_screen.dart';
import 'package:maclaw_mobile/features/assistant/assistant_voice_input.dart';

void main() {
  test('assistant retry is available only when query has text', () {
    expect(canRetryAssistantQuery('  总结这个链接  '), isTrue);
    expect(canRetryAssistantQuery('   '), isFalse);
  });

  test('assistant speech locale follows app language', () {
    expect(assistantSpeechLocaleForLanguage('zh'), 'zh_CN');
    expect(assistantSpeechLocaleForLanguage('zh-Hant'), 'zh_TW');
    expect(assistantSpeechLocaleForLanguage('en'), 'en_US');
  });

  test('assistant citation markdown keeps source details shareable', () {
    const citation = SearchCitation(
      title: '官方公告',
      url: 'https://hubs.mypapers.top/news/1',
      snippet: '服务状态已恢复。',
    );

    expect(
      assistantCitationMarkdown(citation),
      '- 官方公告 https://hubs.mypapers.top/news/1\n  服务状态已恢复。',
    );
  });

  test('assistant search markdown keeps query answer and citations structured',
      () {
    const citation = SearchCitation(
      title: '官方公告',
      url: 'https://hubs.mypapers.top/news/1',
      snippet: '服务状态已恢复。',
    );

    final markdown = assistantSearchResultMarkdown(
      query: '排查今天的官方服务状态',
      answer: '官方服务运行正常。',
      citations: const [citation],
    );

    expect(markdown, contains('## 问题'));
    expect(markdown, contains('排查今天的官方服务状态'));
    expect(markdown, contains('## 结论'));
    expect(markdown, contains('官方服务运行正常。'));
    expect(markdown, contains('## 来源'));
    expect(markdown, contains('https://hubs.mypapers.top/news/1'));
  });

  test('assistant search draft title follows the emergency query', () {
    expect(
      assistantSearchDraftTitle('  排查今天的官方服务状态  '),
      '信息查询：排查今天的官方服务状态',
    );
    expect(assistantSearchDraftTitle('   '), '信息查询整理');
    expect(
      assistantSearchDraftTitle('${'a' * 40} status'),
      '信息查询：aaaaaaaaaaaaaaaaaaaaaaaaaaaa...',
    );
  });
}
