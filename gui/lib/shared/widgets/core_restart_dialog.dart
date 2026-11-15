import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';

String? coreRestartUnavailableReason(GuiController controller) {
  if (controller.canRestartCore) return null;
  if (!controller.supportsCoreRestart) return '当前环境不支持重启核心';
  if (controller.busy) return '正在处理其他操作';
  if (controller.state != GuiConnectionState.ready) return '请先连接后台服务';
  if ((controller.snapshot?.sessions.length ?? 0) > 1) return '多个会话暂不可重启';
  return '当前状态暂不可重启';
}

Future<void> confirmCoreRestart(
  BuildContext context,
  GuiController controller,
  String level,
) async {
  if (!controller.canRestartCore) return;
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => DesignConfirmationDialog(
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
