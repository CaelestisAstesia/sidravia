import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/theme/design_metrics.dart';

import 'design_header.dart';
import 'design_groups.dart';

/// Shared bounded shell. Bodies retain their own scrolling and domain actions.
class DesignDialog extends StatelessWidget {
  const DesignDialog.selection({
    super.key,
    required this.title,
    required this.body,
  }) : actions = const [],
       _information = false;
  const DesignDialog.information({
    super.key,
    required this.title,
    required this.body,
    this.actions = const [],
  }) : _information = true;
  final String title;
  final Widget body;
  final List<Widget> actions;
  final bool _information;

  @override
  Widget build(BuildContext context) => Dialog(
    insetPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 24),
    child: SizedBox(
      width: _information
          ? DesignMetrics.informationDialogWidth
          : DesignMetrics.dialogWidth,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight:
              MediaQuery.sizeOf(context).height *
              DesignMetrics.dialogMaxHeightRatio,
        ),
        child: Padding(
          padding: _information
              ? const EdgeInsets.all(DesignMetrics.dialogPadding)
              : EdgeInsets.zero,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: _information
                    ? EdgeInsets.zero
                    : const EdgeInsets.all(DesignMetrics.dialogPadding),
                child: DesignHeader(
                  title: title,
                  titleFontSize: DesignMetrics.dialogTitleFont,
                  actions: actions,
                  onBack: () => Navigator.pop(context),
                  bottomSpacing: _information ? DesignMetrics.dialogPadding : 0,
                ),
              ),
              Flexible(child: body),
            ],
          ),
        ),
      ),
    ),
  );
}

/// Confirmation content and outcomes stay with the caller; platform/theme defaults stay intact.
class DesignConfirmationDialog extends StatelessWidget {
  const DesignConfirmationDialog({
    super.key,
    required this.title,
    this.content,
    required this.actions,
  });
  final Widget title;
  final Widget? content;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) =>
      AlertDialog(title: title, content: content, actions: actions);
}

Future<bool?> showDesignConfirmation(
  BuildContext context, {
  required Widget title,
  Widget? content,
}) => showDialog<bool>(
  context: context,
  builder: (context) => DesignConfirmationDialog(
    title: title,
    content: content,
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

Future<T?> showDesignChoice<T>(
  BuildContext context, {
  required String title,
  required T value,
  required List<(T, String)> options,
}) => showDialog<T>(
  context: context,
  builder: (context) => DesignDialog.selection(
    title: title,
    body: SingleChildScrollView(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(
          DesignMetrics.dialogPadding,
          0,
          DesignMetrics.dialogPadding,
          DesignMetrics.dialogPadding,
        ),
        child: DesignGroup(
          children: [
            for (final (option, label) in options)
              DesignRow(
                title: label,
                trailing: option == value
                    ? const Icon(Icons.check, size: 18)
                    : const SizedBox(width: 18),
                onTap: () => Navigator.pop(context, option),
              ),
          ],
        ),
      ),
    ),
  ),
);

class DesignChoiceRow<T> extends StatelessWidget {
  const DesignChoiceRow({
    super.key,
    required this.title,
    required this.value,
    required this.options,
    required this.onChanged,
    this.helper,
  });
  final String title;
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T>? onChanged;
  final String? helper;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      DesignRow(
        title: title,
        value: options.where((e) => e.$1 == value).firstOrNull?.$2 ?? '暂无数据',
        onTap: onChanged == null
            ? null
            : () async {
                final selected = await showDesignChoice<T>(
                  context,
                  title: title,
                  value: value,
                  options: options,
                );
                if (selected != null && context.mounted) {
                  onChanged?.call(selected);
                }
              },
      ),
      if (helper != null)
        Padding(
          padding: const EdgeInsets.fromLTRB(
            DesignMetrics.rowPadding,
            0,
            DesignMetrics.rowPadding,
            DesignMetrics.rowPadding,
          ),
          child: DesignHelper(helper!),
        ),
    ],
  );
}
