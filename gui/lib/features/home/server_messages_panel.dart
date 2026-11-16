import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/business_controller.dart';
import 'package:sidravia_gui/ipc/business_models.dart';

class ServerMessagesPanel extends StatelessWidget {
  const ServerMessagesPanel({super.key, required this.controller});
  final BusinessController controller;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: controller,
    builder: (context, _) {
      final data = controller.snapshot;
      if (data == null) {
        return controller.feedback == null
            ? const SizedBox.shrink()
            : Padding(
                padding: const EdgeInsets.only(top: 20),
                child: Text(controller.feedback!),
              );
      }
      if (!data.statisticsSupported && !data.textSupported) {
        return const SizedBox.shrink();
      }
      final messages = data.messages.reversed.toList();
      return Padding(
        padding: const EdgeInsets.only(top: 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (!data.statisticsSupported &&
                (controller.stale || !data.current)) ...[
              Text(
                controller.stale ? '业务数据暂未更新 · 上次数据' : '已停止 · 上次数据',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
            ],
            if (data.textSupported) ...[
              Text('服务器消息', style: Theme.of(context).textTheme.titleSmall),
              const SizedBox(height: 8),
              if (messages.isEmpty) const Text('暂无消息'),
              if (messages.isNotEmpty) _Message(message: messages.first),
              if (messages.length > 1)
                ExpansionTile(
                  tilePadding: EdgeInsets.zero,
                  childrenPadding: EdgeInsets.zero,
                  title: Text(
                    '查看其余 ${messages.length - 1} 条消息',
                    style: const TextStyle(fontSize: 12),
                  ),
                  children: [
                    for (final message in messages.skip(1))
                      _Message(message: message),
                  ],
                ),
            ],
          ],
        ),
      );
    },
  );
}

class _Message extends StatelessWidget {
  const _Message({required this.message});
  final ServerTextMessage message;
  @override
  Widget build(BuildContext context) => SizedBox(
    width: double.infinity,
    child: Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            message.receivedAt.toLocal().toString().split('.').first,
            style: TextStyle(
              fontSize: 11,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 4),
          SelectableText(
            message.text,
            style: const TextStyle(fontSize: 12, height: 1.5),
          ),
        ],
      ),
    ),
  );
}
