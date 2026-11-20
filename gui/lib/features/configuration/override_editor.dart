import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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
    required this.clearRequested,
    required this.enabled,
    required this.loading,
    required this.onRestore,
    this.error,
    this.onReload,
  });

  final OverrideTextController text;
  final VoidCallback onBack, onComplete;
  final ValueChanged<bool> onChanged;
  final ValueGetter<bool>? onRestore;
  final VoidCallback? onReload;
  final bool clearRequested;
  final bool enabled, loading;
  final String? error;

  @override
  State<OverrideEditor> createState() => _OverrideEditorState();
}

typedef _Edit = ({TextEditingValue value, bool clearRequested});

class _OverrideEditorState extends State<OverrideEditor> {
  static const _historyLimit = 128;
  final _undo = <_Edit>[];
  final _redo = <_Edit>[];
  late _Edit _current;
  bool _applying = false;
  bool _composing = false;
  String? _formatError;

  @override
  void initState() {
    super.initState();
    _current = (
      value: widget.text.value,
      clearRequested: widget.clearRequested,
    );
    widget.text.addListener(_onTextChanged);
  }

  @override
  void didUpdateWidget(OverrideEditor oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) {
      oldWidget.text.removeListener(_onTextChanged);
      widget.text.addListener(_onTextChanged);
      _undo.clear();
      _redo.clear();
    }
    if (oldWidget.text != widget.text || oldWidget.loading && !widget.loading) {
      _current = (
        value: widget.text.value,
        clearRequested: widget.clearRequested,
      );
    }
  }

  @override
  void dispose() {
    widget.text.removeListener(_onTextChanged);
    super.dispose();
  }

  void _onTextChanged() {
    if (_applying || !widget.enabled) return;
    final value = widget.text.value;
    final composing =
        value.isComposingRangeValid && !value.composing.isCollapsed;
    if (composing != _composing) setState(() => _composing = composing);
    if (composing) return;
    if (value.text == _current.value.text) {
      _current = (value: value, clearRequested: _current.clearRequested);
      return;
    }
    _record((value: value, clearRequested: false));
    widget.onChanged(false);
  }

  void _record(_Edit edit) {
    if (edit.value.text == _current.value.text &&
        edit.clearRequested == _current.clearRequested) {
      return;
    }
    setState(() {
      if (_undo.length == _historyLimit) _undo.removeAt(0);
      _undo.add(_current);
      _redo.clear();
      _current = edit;
      _formatError = null;
    });
  }

  void _apply(_Edit edit) {
    _applying = true;
    try {
      widget.text.value = edit.value;
    } finally {
      _applying = false;
    }
    widget.onChanged(edit.clearRequested);
  }

  void _moveHistory(List<_Edit> from, List<_Edit> to) {
    if (!widget.enabled || _composing || from.isEmpty) return;
    setState(() {
      to.add(_current);
      _current = from.removeLast();
      _formatError = null;
    });
    _apply(_current);
  }

  void _format() {
    if (!widget.enabled || _composing) return;
    try {
      final before = widget.text.text;
      final formatted = formatOverride(before);
      if (formatted == before) return;
      final edit = (
        value: TextEditingValue(
          text: formatted,
          selection: TextSelection.collapsed(offset: formatted.length),
        ),
        clearRequested: _current.clearRequested,
      );
      _record(edit);
      _apply(edit);
    } on FormatException catch (error) {
      setState(() => _formatError = error.message);
    }
  }

  Future<void> _confirmRestore() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => DesignConfirmationDialog(
        title: const Text('恢复协议默认值？'),
        content: const Text('此操作会移除草稿中的自定义覆写。已保存的规则要等你返回连接配置并保存后才会清除。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final restore = widget.onRestore;
    if (restore == null) return;
    _applying = true;
    try {
      final clearRequested = restore();
      _record((value: widget.text.value, clearRequested: clearRequested));
    } finally {
      _applying = false;
    }
    setState(() {
      _composing = false;
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
              padding: const EdgeInsets.fromLTRB(12, 6, 12, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  _toolbar(context),
                  const SizedBox(height: 4),
                  CallbackShortcuts(
                    bindings: {
                      const SingleActivator(
                        LogicalKeyboardKey.keyZ,
                        control: true,
                      ): () =>
                          _moveHistory(_undo, _redo),
                      const SingleActivator(
                        LogicalKeyboardKey.keyZ,
                        meta: true,
                      ): () =>
                          _moveHistory(_undo, _redo),
                      const SingleActivator(
                        LogicalKeyboardKey.keyY,
                        control: true,
                      ): () =>
                          _moveHistory(_redo, _undo),
                      const SingleActivator(
                        LogicalKeyboardKey.keyZ,
                        control: true,
                        shift: true,
                      ): () =>
                          _moveHistory(_redo, _undo),
                      const SingleActivator(
                        LogicalKeyboardKey.keyZ,
                        meta: true,
                        shift: true,
                      ): () =>
                          _moveHistory(_redo, _undo),
                    },
                    child: _editor(context),
                  ),
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
      const SizedBox(height: 18),
      Align(
        alignment: Alignment.center,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 320),
          child: SizedBox(
            width: double.infinity,
            child: FilledButton(
              onPressed:
                  !widget.loading && !_composing && widget.onReload == null
                  ? widget.onComplete
                  : null,
              child: const Text('保存修改'),
            ),
          ),
        ),
      ),
      const SizedBox(height: 8),
      const Align(
        alignment: Alignment.center,
        child: DesignHelper('返回连接配置并保存后生效'),
      ),
      const SizedBox(height: 24),
      const DesignSectionTitle('恢复默认'),
      DesignGroup(
        children: [
          DesignRow(
            title: '恢复协议默认值',
            subtitle: '清除自定义协议覆写',
            danger: true,
            onTap: widget.onRestore == null ? null : _confirmRestore,
          ),
        ],
      ),
    ],
  );

  Widget _toolbar(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final touch =
        theme.platform == TargetPlatform.android ||
        theme.platform == TargetPlatform.iOS;
    final tapTarget = touch
        ? MaterialTapTargetSize.padded
        : MaterialTapTargetSize.shrinkWrap;
    final outline = BorderSide(color: colors.outline);
    final shape = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(6),
    );
    final iconStyle = IconButton.styleFrom(
      side: outline,
      shape: shape,
      minimumSize: const Size(26, 26),
      padding: EdgeInsets.zero,
      tapTargetSize: tapTarget,
      visualDensity: VisualDensity.standard,
    );
    return OverflowBar(
      alignment: MainAxisAlignment.spaceBetween,
      overflowAlignment: OverflowBarAlignment.end,
      overflowSpacing: 4,
      children: [
        Semantics(header: true, child: const Text('JSON')),
        Wrap(
          alignment: WrapAlignment.end,
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: 4,
          runSpacing: 4,
          children: [
            IconButton.outlined(
              tooltip: '撤销',
              style: iconStyle,
              onPressed: widget.enabled && !_composing && _undo.isNotEmpty
                  ? () => _moveHistory(_undo, _redo)
                  : null,
              icon: const Icon(Icons.undo, size: 15),
            ),
            IconButton.outlined(
              tooltip: '重做',
              style: iconStyle,
              onPressed: widget.enabled && !_composing && _redo.isNotEmpty
                  ? () => _moveHistory(_redo, _undo)
                  : null,
              icon: const Icon(Icons.redo, size: 15),
            ),
            OutlinedButton(
              onPressed: widget.enabled && !_composing ? _format : null,
              style: OutlinedButton.styleFrom(
                side: outline,
                shape: shape,
                minimumSize: const Size(0, 26),
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                tapTargetSize: tapTarget,
                visualDensity: VisualDensity.standard,
                textStyle: theme.textTheme.bodySmall,
                elevation: 0,
              ),
              child: const Text('格式化'),
            ),
          ],
        ),
      ],
    );
  }

  Widget _editor(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final style =
          (Theme.of(context).textTheme.bodyMedium ?? const TextStyle())
              .copyWith(fontFamily: 'Source Code Pro');
      final direction = Directionality.of(context);
      final scaler = MediaQuery.textScalerOf(context);
      final strut = StrutStyle.fromTextStyle(style, forceStrutHeight: true);
      const gutterGap = 6.0;
      // Keep implicit decoration and focus borders out of the editor.
      const fieldPadding = 5.0;
      final lineCount = '\n'.allMatches(widget.text.text).length + 1;
      final sample = lineCount.toString().padLeft(3, '0');
      final measure = TextPainter(
        text: TextSpan(text: sample, style: style),
        textDirection: direction,
        textScaler: scaler,
        strutStyle: strut,
      )..layout();
      final gutter = (measure.width.ceilToDouble() + gutterGap).clamp(
        0.0,
        constraints.maxWidth / 3,
      );
      measure.dispose();
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
                    painter.layout(
                      maxWidth:
                          (constraints.maxWidth - gutter - 2 * fieldPadding)
                              .clamp(0.0, double.infinity),
                    );
                    final wrapped = painter.computeLineMetrics().length;
                    for (var j = 1; j < wrapped; j++) {
                      numbers.add('');
                    }
                  }
                  painter.dispose();
                  return Padding(
                    padding: const EdgeInsets.only(
                      right: gutterGap,
                      top: fieldPadding,
                    ),
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
            child: DecoratedBox(
              decoration: BoxDecoration(
                border: Border.all(
                  color: Theme.of(context).colorScheme.outline,
                ),
                borderRadius: BorderRadius.circular(8),
              ),
              child: Padding(
                padding: const EdgeInsets.all(fieldPadding),
                child: Semantics(
                  label: '覆写 JSON',
                  child: TextField(
                    controller: widget.text,
                    readOnly: !widget.enabled,
                    minLines: 12,
                    maxLines: null,
                    style: style,
                    strutStyle: strut,
                    autocorrect: false,
                    enableSuggestions: false,
                    decoration: null,
                  ),
                ),
              ),
            ),
          ),
        ],
      );
    },
  );
}
