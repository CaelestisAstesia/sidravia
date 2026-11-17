import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_search_field.dart';

/// Searches only the frozen display text. Copying remains owned by the page.
class SnapshotCodeView extends StatefulWidget {
  const SnapshotCodeView({super.key, required this.text});
  final String text;
  @override
  State<SnapshotCodeView> createState() => _SnapshotCodeViewState();
}

class _SnapshotCodeViewState extends State<SnapshotCodeView> {
  final _query = TextEditingController();
  final _scroll = ScrollController();
  List<RegExpMatch> _matches = [];
  int _current = 0;
  double _textWidth = 0;

  TextStyle _style(BuildContext context) {
    final theme = Theme.of(context);
    final ordinary = theme.textTheme.bodyMedium!;
    return TextStyle(
      fontFamily: 'Source Code Pro',
      fontFamilyFallback: [
        ?ordinary.fontFamily,
        ...?ordinary.fontFamilyFallback,
      ],
      fontSize: 12,
      height: 1.6,
      color: theme.colorScheme.onSurface,
    );
  }

  void _find() {
    final query = _query.text;
    _matches = query.isEmpty
        ? []
        : RegExp(
            RegExp.escape(query),
            caseSensitive: false,
          ).allMatches(widget.text).toList();
    _current = 0;
    _locate();
  }

  void _locate() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          !_scroll.hasClients ||
          _matches.isEmpty ||
          _textWidth <= 0) {
        return;
      }
      final painter = TextPainter(
        text: TextSpan(text: widget.text, style: _style(context)),
        textScaler: MediaQuery.textScalerOf(context),
        textDirection: Directionality.of(context),
      )..layout(maxWidth: _textWidth);
      final offset = painter
          .getOffsetForCaret(
            TextPosition(offset: _matches[_current].start),
            Rect.zero,
          )
          .dy;
      painter.dispose();
      _scroll.jumpTo((offset - 8).clamp(0, _scroll.position.maxScrollExtent));
    });
  }

  @override
  void didUpdateWidget(covariant SnapshotCodeView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) _find();
  }

  @override
  void dispose() {
    _query.dispose();
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final spans = <TextSpan>[];
    var end = 0;
    for (var i = 0; i < _matches.length; i++) {
      final start = _matches[i].start;
      spans.add(TextSpan(text: widget.text.substring(end, start)));
      end = _matches[i].end;
      spans.add(
        TextSpan(
          text: widget.text.substring(start, end),
          style: TextStyle(
            backgroundColor: i == _current
                ? scheme.primaryContainer
                : scheme.surfaceContainerHighest,
            color: i == _current ? scheme.onPrimaryContainer : scheme.onSurface,
          ),
        ),
      );
    }
    spans.add(TextSpan(text: widget.text.substring(end)));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 10, 12, 0),
          child: DesignSearchField(
            controller: _query,
            hint: '查找字段或内容',
            onChanged: (_) => setState(_find),
          ),
        ),
        if (_query.text.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(left: 14, right: 4),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    _matches.isEmpty
                        ? '未找到匹配项'
                        : '${_current + 1} / ${_matches.length}',
                    style: TextStyle(
                      fontSize: 12,
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ),
                for (final previous in [true, false])
                  IconButton(
                    tooltip: previous ? '上一个匹配项' : '下一个匹配项',
                    onPressed: _matches.isEmpty
                        ? null
                        : () {
                            setState(
                              () => _current =
                                  (_current + (previous ? -1 : 1)) %
                                  _matches.length,
                            );
                            _locate();
                          },
                    icon: Icon(
                      previous
                          ? Icons.keyboard_arrow_up
                          : Icons.keyboard_arrow_down,
                      size: 20,
                    ),
                  ),
              ],
            ),
          ),
        SizedBox(
          height: 360,
          child: LayoutBuilder(
            builder: (context, constraints) {
              _textWidth = (constraints.maxWidth - 28).clamp(
                1,
                double.infinity,
              );
              return SingleChildScrollView(
                controller: _scroll,
                padding: const EdgeInsets.all(14),
                child: SelectableText.rich(
                  TextSpan(children: spans),
                  key: const ValueKey('runtime-snapshot-text'),
                  style: _style(context),
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}
