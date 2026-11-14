import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/business_controller.dart';

class BusinessPanel extends StatelessWidget {
  const BusinessPanel({super.key, required this.controller});
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
      final stats = data.statistics;
      final traffic = stats?.trafficBytes;
      final balance = stats?.balanceCents;
      return Padding(
        padding: const EdgeInsets.only(top: 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (controller.stale || !data.current)
              Text(
                controller.stale ? '业务数据暂未更新 · 上次数据' : '已停止 · 上次数据',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            if (data.statisticsSupported) ...[
              Text('账户统计', style: Theme.of(context).textTheme.titleSmall),
              Text(
                '服务器历史计费流量：${traffic == null ? '暂无数据' : '${(traffic / (1024 * 1024)).toStringAsFixed(2)} MiB'}',
              ),
              Text(
                '余额：${balance == null ? '暂无数据' : '${balance ~/ 100}.${(balance % 100).toString().padLeft(2, '0')} 元'}',
              ),
              const Text('登录时服务器快照', style: TextStyle(fontSize: 12)),
              const SizedBox(height: 12),
            ],
            if (data.textSupported) ...[
              Text(
                '服务器消息 · 仅保留最近16条',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              if (data.messages.isEmpty) const Text('暂无消息'),
              for (final message in data.messages.reversed)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        message.receivedAt
                            .toLocal()
                            .toString()
                            .split('.')
                            .first,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      SelectableText(message.text),
                    ],
                  ),
                ),
            ],
          ],
        ),
      );
    },
  );
}
