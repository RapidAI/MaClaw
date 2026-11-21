/// Pure parsing and sanitising of chat-supplied Mermaid sources.
///
/// Nothing in this file touches a WebView. Tests and the markdown builder
/// share it so a fence can be classified without loading the diagram engine.
library;

/// Diagrams larger than this are refused before the engine sees them.
const int maxMermaidSourceLength = 50000;

/// Node-id estimate. A cheap source scan, not a real parse.
const int maxMermaidNodeCount = 750;

/// Edge-operator estimate (`-->`, `---`, `==>`, `-.->`).
const int maxMermaidEdgeCount = 1500;

/// Why a source was not handed to the renderer.
enum MermaidRejectReason { empty, tooLong, overBudget }

/// A fenced block that should become a diagram, or a reason it should not.
class MermaidSourceDecision {
  const MermaidSourceDecision.accept(this.source) : reason = null;

  const MermaidSourceDecision.reject(this.reason) : source = '';

  final String source;
  final MermaidRejectReason? reason;

  bool get accepted => reason == null;
}

/// Canonical diagram-type keywords, keyed by lower-case alias.
///
/// Mermaid 11.14 detectors are case-sensitive (`sequenceDiagram`, `gitGraph`,
/// …) even when the grammar later accepts other casings. Mapping here avoids
/// "No diagram type detected" on model output that only got the case wrong.
const Map<String, String> mermaidDiagramTypeKeywords = {
  'graph': 'graph',
  'flowchart': 'flowchart',
  'sequencediagram': 'sequenceDiagram',
  'statediagram': 'stateDiagram',
  'statediagram-v2': 'stateDiagram-v2',
  'classdiagram': 'classDiagram',
  'erdiagram': 'erDiagram',
  'gantt': 'gantt',
  'pie': 'pie',
  'gitgraph': 'gitGraph',
  'journey': 'journey',
  'mindmap': 'mindmap',
  'timeline': 'timeline',
  'quadrantchart': 'quadrantChart',
  'sankey-beta': 'sankey-beta',
  'xychart-beta': 'xychart-beta',
  'requirementdiagram': 'requirementDiagram',
  'c4context': 'C4Context',
  'c4container': 'C4Container',
  'c4component': 'C4Component',
  'c4dynamic': 'C4Dynamic',
  'c4deployment': 'C4Deployment',
  'block-beta': 'block-beta',
  'packet-beta': 'packet-beta',
  'architecture-beta': 'architecture-beta',
  'kanban': 'kanban',
};

/// Info-string aliases that mean "render this fence as Mermaid".
const Set<String> mermaidFenceLanguages = {
  'mermaid',
  'mmd',
};

String canonicalMermaidDiagramKeyword(String alias) {
  return mermaidDiagramTypeKeywords[alias.toLowerCase()] ?? alias;
}

bool isMermaidFenceLanguage(String? language) {
  if (language == null) return false;
  final lang = language.trim().toLowerCase();
  return mermaidFenceLanguages.contains(lang);
}

/// Repair harmless casing errors commonly emitted by language models.
///
/// Only the first token of a line is rewritten, and only when it is a known
/// diagram or statement keyword. Node ids and quoted labels are left alone.
String sanitizeMermaidCode(String raw) {
  final diagramType = RegExp(
    r'^(\s*)(Graph|Flowchart|SequenceDiagram|StateDiagram-v2|StateDiagram|'
    r'ClassDiagram|ErDiagram|Gantt|Pie|Gitgraph|Journey|Mindmap|Timeline|'
    r'Quadrantchart|Sankey-beta|Xychart-beta|RequirementDiagram|C4Context|'
    r'C4Container|C4Component|C4Dynamic|C4Deployment|Block-beta|Packet-beta|'
    r'Architecture-beta|Kanban)\b',
    caseSensitive: false,
  );
  final keyword = RegExp(
    r'^(\s*)(Subgraph|ClassDef|Style|Click|Note|Loop|Alt|Else|Opt|Par|'
    r'Critical|Break|Rect|Activate|Deactivate|Direction)\b',
    caseSensitive: false,
  );
  final endLine = RegExp(r'^(\s*)End\s*$', caseSensitive: false);
  return raw.split('\n').map((line) {
    final diagramMatch = diagramType.firstMatch(line);
    if (diagramMatch != null) {
      final lead = diagramMatch.group(1) ?? '';
      final word = diagramMatch.group(2) ?? '';
      return '$lead${canonicalMermaidDiagramKeyword(word)}'
          '${line.substring(diagramMatch.end)}';
    }
    final ended = endLine.firstMatch(line);
    if (ended != null) {
      return '${ended.group(1) ?? ''}end';
    }
    final keywordMatch = keyword.firstMatch(line);
    if (keywordMatch == null) return line;
    final lead = keywordMatch.group(1) ?? '';
    final word = keywordMatch.group(2) ?? '';
    final normalized = word.toLowerCase() == 'classdef'
        ? 'classDef'
        : word.toLowerCase();
    return '$lead$normalized${line.substring(keywordMatch.end)}';
  }).join('\n');
}

/// True when a source-level estimate already exceeds the render budget.
bool exceedsMermaidRenderBudget(String source) {
  final nodeIds = <String>{};
  final nodes = RegExp(
    r'(?:^|[\s;])([A-Za-z][\w-]*)(?:\s*(?:\[|\(|\{|-->|---|==>))',
    multiLine: true,
  );
  for (final match in nodes.allMatches(source)) {
    final id = match.group(1);
    if (id == null) continue;
    nodeIds.add(id);
    if (nodeIds.length > maxMermaidNodeCount) return true;
  }
  final edges = RegExp(r'(?:-->|---|==>|-.->)');
  return edges.allMatches(source).length > maxMermaidEdgeCount;
}

/// Decide whether [raw] may be rendered. Empty and over-budget sources are
/// rejected here so the WebView never has to.
MermaidSourceDecision prepareMermaidSource(String raw) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) {
    return const MermaidSourceDecision.reject(MermaidRejectReason.empty);
  }
  if (trimmed.length > maxMermaidSourceLength) {
    return const MermaidSourceDecision.reject(MermaidRejectReason.tooLong);
  }
  final source = sanitizeMermaidCode(trimmed);
  if (exceedsMermaidRenderBudget(source)) {
    return const MermaidSourceDecision.reject(MermaidRejectReason.overBudget);
  }
  return MermaidSourceDecision.accept(source);
}

/// One fenced code block extracted from markdown, mermaid or otherwise.
class MarkdownFence {
  const MarkdownFence({
    required this.language,
    required this.body,
    required this.start,
    required this.end,
  });

  final String language;
  final String body;

  /// Inclusive start offset in the original markdown.
  final int start;

  /// Exclusive end offset in the original markdown.
  final int end;

  bool get isMermaid => isMermaidFenceLanguage(language);
}

final RegExp _fencePattern = RegExp(
  r'^[ ]{0,3}(`{3,}|~{3,})[ \t]*([^\s`]*)[^\n]*\n([\s\S]*?)^[ ]{0,3}\1[ ]*$',
  multiLine: true,
);

/// Closed fenced blocks only. A streaming fence that has not closed yet is
/// left as ordinary markdown so the reader still sees the text arrive.
List<MarkdownFence> extractMarkdownFences(String markdown) {
  return [
    for (final match in _fencePattern.allMatches(markdown))
      MarkdownFence(
        language: match.group(2) ?? '',
        body: match.group(3) ?? '',
        start: match.start,
        end: match.end,
      ),
  ];
}

/// Markdown with mermaid fences removed, plus the fences in source order.
///
/// Non-mermaid fences stay in [markdown]. Callers that only need to know
/// whether a message contains a diagram can check [fences].
class SplitMermaidMarkdown {
  const SplitMermaidMarkdown({required this.markdown, required this.fences});

  final String markdown;
  final List<MarkdownFence> fences;
}

SplitMermaidMarkdown splitMermaidFences(String markdown) {
  final fences = extractMarkdownFences(markdown).where((f) => f.isMermaid);
  if (fences.isEmpty) {
    return SplitMermaidMarkdown(markdown: markdown, fences: const []);
  }
  final kept = <MarkdownFence>[];
  final buffer = StringBuffer();
  var cursor = 0;
  for (final fence in fences) {
    buffer.write(markdown.substring(cursor, fence.start));
    kept.add(fence);
    cursor = fence.end;
  }
  buffer.write(markdown.substring(cursor));
  return SplitMermaidMarkdown(markdown: buffer.toString(), fences: kept);
}

/// Short, localised refusal. Parser errors from the engine are not rewritten.
String mermaidRejectMessage(MermaidRejectReason reason, {String? languageCode}) {
  final zh = _isChinese(languageCode);
  switch (reason) {
    case MermaidRejectReason.empty:
      return zh ? 'Mermaid 图表是空的。' : 'The Mermaid diagram is empty.';
    case MermaidRejectReason.tooLong:
      return zh
          ? 'Mermaid 图表超过 $maxMermaidSourceLength 个字符，未渲染。'
          : 'The Mermaid diagram exceeds $maxMermaidSourceLength characters.';
    case MermaidRejectReason.overBudget:
      return zh
          ? 'Mermaid 图表超过 $maxMermaidNodeCount 个节点或 $maxMermaidEdgeCount 条连线的渲染上限。'
          : 'The Mermaid diagram exceeds the $maxMermaidNodeCount node or $maxMermaidEdgeCount edge rendering limit.';
  }
}

bool _isChinese(String? languageCode) {
  final code = (languageCode ?? '').toLowerCase();
  return code.startsWith('zh');
}
