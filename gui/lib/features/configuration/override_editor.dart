import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

const maxOverrideBytes = 16384;

final _tokens = RegExp(r'"(?:[^"\\]|\\.)*"|[{}\[\],:]|[^\s{}\[\],:"]+');

String? overrideSyntaxError(String text) {
  if (utf8.encode(text).length > maxOverrideBytes) return 'JSON 不能超过 16 KiB';
  try {
    if (jsonDecode(text) is! Map) return '请输入一个 JSON 对象';
  } on FormatException catch (error) {
    final offset = error.offset;
    if (offset == null) return 'JSON 格式无效';
    final lines = text.substring(0, offset.clamp(0, text.length)).split('\n');
    return 'JSON 格式无效（第 ${lines.length} 行，第 ${lines.last.length + 1} 列）';
  }
  try {
    decodeBindingCheckedJson(text, preserveUnsignedRevision: false);
  } on StackOverflowError {
    return 'JSON 嵌套过深';
  } on IpcProtocolException {
    return 'JSON 中有重复字段或无效内容';
  }
  return null;
}

// Reflow only whitespace between tokens. Keep string escapes, numbers and
// member order intact, including values a platform decoder could round.
String formatOverride(String text) {
  final error = overrideSyntaxError(text);
  if (error != null) throw FormatException(error);
  final tokens = _tokens.allMatches(text).map((m) => m.group(0)!).toList();
  final out = StringBuffer();
  var depth = 0;
  var byteCount = 0;
  void write(String chunk) {
    byteCount += utf8.encode(chunk).length;
    if (byteCount > maxOverrideBytes) {
      throw const FormatException('格式化结果超过 16 KiB');
    }
    out.write(chunk);
  }

  for (var i = 0; i < tokens.length; i++) {
    final token = tokens[i];
    final previous = i == 0 ? '' : tokens[i - 1];
    final next = i + 1 == tokens.length ? '' : tokens[i + 1];
    switch (token) {
      case '{' || '[':
        write(token);
        depth++;
        if (next != '}' && next != ']') write('\n${'  ' * depth}');
      case '}' || ']':
        depth--;
        if (previous != '{' && previous != '[') write('\n${'  ' * depth}');
        write(token);
      case ',':
        write(',\n${'  ' * depth}');
      case ':':
        write(': ');
      default:
        write(token);
    }
  }
  return out.toString();
}

class OverrideTextController extends TextEditingController {
  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    if (withComposing &&
        value.isComposingRangeValid &&
        !value.composing.isCollapsed) {
      return super.buildTextSpan(
        context: context,
        style: style,
        withComposing: true,
      );
    }
    final colors = Theme.of(context).colorScheme;
    final spans = <TextSpan>[];
    var offset = 0;
    for (final match in _tokens.allMatches(text)) {
      spans.add(TextSpan(text: text.substring(offset, match.start)));
      final token = match.group(0)!;
      spans.add(
        TextSpan(
          text: token,
          style: TextStyle(
            color: token.startsWith('"')
                ? colors.primary
                : RegExp(r'^[-0-9tfn]').hasMatch(token)
                ? colors.tertiary
                : null,
          ),
        ),
      );
      offset = match.end;
    }
    spans.add(TextSpan(text: text.substring(offset)));
    return TextSpan(style: style, children: spans);
  }
}

class OverrideEditor extends StatefulWidget {
  const OverrideEditor({
    super.key,
    required this.text,
    required this.onBack,
    required this.onComplete,
    required this.onChanged,
    required this.enabled,
    required this.loading,
    required this.onRestore,
    this.error,
    this.onReload,
  });

  final OverrideTextController text;
  final VoidCallback onBack, onComplete, onChanged;
  final VoidCallback? onRestore, onReload;
  final bool enabled, loading;
  final String? error;

  @override
  State<OverrideEditor> createState() => _OverrideEditorState();
}

class _OverrideEditorState extends State<OverrideEditor> {
  TextEditingValue? _beforeFormat;
  String? _formatError;

  void _format() {
    try {
      final undo = widget.text.value;
      final formatted = formatOverride(undo.text);
      if (formatted == undo.text) return;
      setState(() {
        _beforeFormat = undo;
        _formatError = null;
      });
      widget.text.value = TextEditingValue(
        text: formatted,
        selection: TextSelection.collapsed(offset: formatted.length),
      );
      widget.onChanged();
    } on FormatException catch (error) {
      setState(() => _formatError = error.message);
    }
  }

  void _undoFormat() {
    final before = _beforeFormat;
    if (before == null) return;
    _resetFormat();
    widget.text.value = before;
    widget.onChanged();
  }

  void _resetFormat() {
    setState(() {
      _beforeFormat = null;
      _formatError = null;
    });
  }

  @override
  Widget build(BuildContext context) => DesignPage(
    children: [
      DesignHeader(title: '协议上报覆写', onBack: widget.onBack),
      if (widget.loading) const LinearProgressIndicator(),
      if (widget.onReload != null)
        TextButton(onPressed: widget.onReload, child: const Text('重新读取')),
      DesignGroup(
        children: [
          if (widget.onReload == null)
            Padding(
              padding: const EdgeInsets.all(12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Semantics(header: true, child: const Text('覆写 JSON')),
                  const SizedBox(height: 8),
                  _editor(context),
                ],
              ),
            ),
          if (_formatError ?? widget.error case final String message)
            Padding(
              padding: const EdgeInsets.all(12),
              child: Text(
                message,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
        ],
      ),
      const SizedBox(height: 12),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          OutlinedButton(
            onPressed: widget.enabled ? _format : null,
            child: const Text('格式化'),
          ),
          if (_beforeFormat != null)
            TextButton(
              onPressed: widget.enabled ? _undoFormat : null,
              child: const Text('撤销格式化'),
            ),
          TextButton(
            onPressed: widget.onRestore == null
                ? null
                : () {
                    _resetFormat();
                    widget.onRestore!();
                  },
            child: const Text('恢复协议默认值'),
          ),
          FilledButton(
            onPressed: !widget.loading && widget.onReload == null
                ? widget.onComplete
                : null,
            child: const Text('完成编辑'),
          ),
        ],
      ),
    ],
  );

  Widget _editor(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final style =
          (Theme.of(context).textTheme.bodyMedium ?? const TextStyle())
              .copyWith(fontFamily: 'Source Code Pro');
      final direction = Directionality.of(context);
      final scaler = MediaQuery.textScalerOf(context);
      final gutter = scaler.scale(42).clamp(0.0, constraints.maxWidth / 3);
      final strut = StrutStyle.fromTextStyle(style, forceStrutHeight: true);
      return Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: gutter,
            child: ExcludeSemantics(
              child: ListenableBuilder(
                listenable: widget.text,
                builder: (context, _) {
                  final numbers = <String>[];
                  final painter = TextPainter(
                    textDirection: direction,
                    textScaler: scaler,
                    strutStyle: strut,
                  );
                  final lines = widget.text.text.split('\n');
                  for (var i = 0; i < lines.length; i++) {
                    numbers.add('${i + 1}');
                    painter.text = TextSpan(text: lines[i], style: style);
                    painter.layout(maxWidth: constraints.maxWidth - gutter);
                    final wrapped = painter.computeLineMetrics().length;
                    for (var j = 1; j < wrapped; j++) {
                      numbers.add('');
                    }
                  }
                  painter.dispose();
                  return Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: Text(
                      numbers.join('\n'),
                      textAlign: TextAlign.right,
                      strutStyle: strut,
                      style: style.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  );
                },
              ),
            ),
          ),
          Expanded(
            child: Semantics(
              label: '覆写 JSON',
              child: TextField(
                controller: widget.text,
                readOnly: !widget.enabled,
                minLines: 8,
                maxLines: null,
                style: style,
                strutStyle: strut,
                autocorrect: false,
                enableSuggestions: false,
                onChanged: (_) {
                  _resetFormat();
                  widget.onChanged();
                },
                decoration: const InputDecoration(
                  isCollapsed: true,
                  border: InputBorder.none,
                ),
              ),
            ),
          ),
        ],
      );
    },
  );
}
