import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';

Future<void> confirmCoreRestart(
  BuildContext context,
  GuiController controller,
  String level,
) async {
  if (!controller.canRestartCore) return;
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('重启核心？'),
      content: const Text('重启会中断当前网络连接。完成后将恢复重启前的连接状态。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('取消'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(context, true),
          child: const Text('重启核心'),
        ),
      ],
    ),
  );
  if (confirmed == true && context.mounted) await controller.restartCore(level);
}
