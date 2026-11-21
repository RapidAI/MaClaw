/// Markdown body that renders closed mermaid fences as diagrams.
///
/// Other fences stay with [MarkdownBody]. A diagram widget is created only
/// after its slot has been on screen, so a long transcript does not start a
/// WebView for every fence at once.
library;

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import 'mermaid_diagram.dart';
import 'mermaid_source.dart';

/// Drop-in for [MarkdownBody] on chat and task surfaces.
///
/// Callers keep their style sheet, link handler, and selectability. Mermaid
/// fences (`mermaid` and `mmd`) become [MermaidDiagram]; everything else is
/// ordinary markdown.
class MermaidMarkdown extends StatelessWidget {
  const MermaidMarkdown({
    super.key,
    required this.data,
    this.selectable = false,
    this.styleSheet,
    this.onTapLink,
    this.dark,
    this.languageCode,
    this.diagramBuilder,
    this.lazy = true,
  });

  /// Message markdown, including fences that may still be streaming.
  final String data;

  final bool selectable;
  final MarkdownStyleSheet? styleSheet;
  final MarkdownTapLinkCallback? onTapLink;

  /// Overrides [ThemeData.brightness] for every diagram in this body.
  final bool? dark;

  /// `zh` selects Chinese refusal copy. Defaults to the ambient locale.
  final String? languageCode;

  /// Test stand-in. Production leaves this null so the bundled engine runs.
  final MermaidViewBuilder? diagramBuilder;

  /// When true, a fence shows a light placeholder until it is visible.
  final bool lazy;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: _children(),
    );
  }

  List<Widget> _children() {
    final split = splitMermaidFences(data);
    if (split.fences.isEmpty) return [_markdown(data)];

    final children = <Widget>[];
    var cursor = 0;
    for (final fence in split.fences) {
      _addMarkdown(children, data.substring(cursor, fence.start));
      children.add(_diagram(fence.body));
      cursor = fence.end;
    }
    _addMarkdown(children, data.substring(cursor));
    return children;
  }

  void _addMarkdown(List<Widget> children, String markdown) {
    if (markdown.trim().isEmpty) return;
    children.add(_markdown(markdown));
  }

  Widget _markdown(String markdown) {
    return MarkdownBody(
      data: markdown,
      selectable: selectable,
      styleSheet: styleSheet,
      onTapLink: onTapLink,
    );
  }

  Widget _diagram(String source) {
    Widget diagram() {
      return MermaidDiagram(
        source: source,
        dark: dark,
        languageCode: languageCode,
        viewBuilder: diagramBuilder,
      );
    }

    if (!lazy) return diagram();
    return _LazyMermaidSlot(key: ValueKey(source), builder: diagram);
  }
}

/// Placeholder until the fence intersects the nearest scroll viewport.
class _LazyMermaidSlot extends StatefulWidget {
  const _LazyMermaidSlot({super.key, required this.builder});

  final Widget Function() builder;

  @override
  State<_LazyMermaidSlot> createState() => _LazyMermaidSlotState();
}

class _LazyMermaidSlotState extends State<_LazyMermaidSlot> {
  static const double _placeholderHeight = 96;

  final GlobalKey _slotKey = GlobalKey();
  ScrollPosition? _position;
  bool _visible = false;
  bool _retriedLayout = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _attachAndCheck());
  }

  @override
  void dispose() {
    _position?.removeListener(_checkVisible);
    super.dispose();
  }

  void _attachAndCheck() {
    if (!mounted || _visible) return;
    final scrollable = Scrollable.maybeOf(context);
    final next = scrollable?.position;
    if (!identical(next, _position)) {
      _position?.removeListener(_checkVisible);
      _position = next;
      _position?.addListener(_checkVisible);
    }
    _checkVisible();
  }

  void _checkVisible() {
    if (!mounted || _visible) return;
    final box = _slotKey.currentContext?.findRenderObject();
    if (box is! RenderBox || !box.hasSize || !box.attached) {
      if (_retriedLayout) return;
      _retriedLayout = true;
      WidgetsBinding.instance.addPostFrameCallback((_) => _attachAndCheck());
      return;
    }
    final viewport = _viewportOf(box);
    if (viewport == null) {
      // No viewport to measure against: show the diagram rather than hide it
      // behind a placeholder nobody can scroll to.
      setState(() => _visible = true);
      return;
    }
    final topLeft = box.localToGlobal(Offset.zero, ancestor: viewport);
    final bottom = topLeft.dy + box.size.height;
    if (bottom > 0 && topLeft.dy < viewport.size.height) {
      setState(() => _visible = true);
    }
  }

  /// Nearest viewport above the slot, found by walking elements.
  ///
  /// A render-tree walk misses the viewport here: the slot sits under boxes
  /// that the render walk cannot cross in every host (a SliverViewport is
  /// reached through a sliver, and the test binding's viewport is not an
  /// ancestor of the slot's render object). The element tree exposes the
  /// enclosing [Scrollable], whose [RenderBox] is the clip the user scrolls.
  RenderBox? _viewportOf(RenderBox box) {
    final slot = _slotKey.currentContext as Element?;
    if (slot == null) return null;
    RenderBox? viewport;
    slot.visitAncestorElements((ancestor) {
      if (ancestor.widget is! Scrollable) return true;
      final render = ancestor.findRenderObject();
      if (render is! RenderBox || !render.hasSize) return true;
      viewport = render;
      return false;
    });
    return viewport;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return KeyedSubtree(
      key: _slotKey,
      child: _visible
          ? widget.builder()
          : SizedBox(
              key: const Key('mermaid-lazy-placeholder'),
              height: _placeholderHeight,
              width: double.infinity,
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(6),
                ),
                child: const Center(
                  child: SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                ),
              ),
            ),
    );
  }
}

