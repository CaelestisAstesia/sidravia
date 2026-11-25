import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

/// Owns only editing/reveal presentation; the page owns the password draft.
class ConfigurationPasswordRow extends StatefulWidget {
  const ConfigurationPasswordRow({
    super.key,
    required this.controller,
    required this.credentialStored,
    required this.enabled,
    required this.onCancel,
    this.error,
  });

  final TextEditingController controller;
  final bool credentialStored, enabled;
  final String? error;
  final VoidCallback onCancel;

  @override
  State<ConfigurationPasswordRow> createState() =>
      _ConfigurationPasswordRowState();
}

class _ConfigurationPasswordRowState extends State<ConfigurationPasswordRow> {
  bool _editing = false, _revealed = false;

  @override
  void didUpdateWidget(covariant ConfigurationPasswordRow oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!widget.enabled) _revealed = false;
  }

  void _cancel() {
    widget.controller.clear();
    setState(() {
      _editing = false;
      _revealed = false;
    });
    widget.onCancel();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.credentialStored &&
        !_editing &&
        widget.controller.text.isEmpty) {
      return LayoutBuilder(
        builder: (context, constraints) => DesignRow(
          title: '密码',
          trailing: SizedBox(
            width: (constraints.maxWidth - 40) * .65,
            child: Wrap(
              alignment: WrapAlignment.end,
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: 8,
              children: [
                Text(
                  '已保存密码',
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
                OutlinedButton(
                  style: AppButtonStyles.inlineAction(context),
                  onPressed: widget.enabled
                      ? () => setState(() => _editing = true)
                      : null,
                  child: const Text('更改'),
                ),
              ],
            ),
          ),
        ),
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        DesignInputRow(
          label: '密码',
          controller: widget.controller,
          enabled: widget.enabled,
          obscureText: !_revealed,
          hint: '输入密码',
          error: widget.error,
          helper: widget.credentialStored ? null : '首次连接请填写账号和密码',
          trailing: IconButton(
            tooltip: _revealed ? '隐藏密码' : '显示密码',
            onPressed: widget.enabled
                ? () => setState(() => _revealed = !_revealed)
                : null,
            icon: Icon(
              _revealed
                  ? Icons.visibility_off_outlined
                  : Icons.visibility_outlined,
              size: 18,
            ),
          ),
        ),
        if (widget.credentialStored)
          Align(
            alignment: AlignmentDirectional.centerEnd,
            child: Padding(
              padding: const EdgeInsets.only(right: 12, bottom: 4),
              child: TextButton(
                style: AppButtonStyles.inlineAction(context),
                onPressed: widget.enabled ? _cancel : null,
                child: const Text('取消更改'),
              ),
            ),
          ),
      ],
    );
  }
}
