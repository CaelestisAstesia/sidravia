import 'package:flutter/material.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/application/business_controller.dart';
import 'package:sidravia_gui/ipc/business_models.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

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

/// One centered group for the actual network binding and optional login values.
class ConnectionInformationView extends StatelessWidget {
  const ConnectionInformationView({
    super.key,
    this.controller,
    this.contextText = '',
    this.detailText = '',
    this.issue,
  });
  final BusinessController? controller;
  final String contextText, detailText;
  final SessionAuthenticationFailure? issue;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: controller ?? const AlwaysStoppedAnimation(0),
    builder: (context, _) {
      final data = controller?.snapshot;
      final stats = data?.statistics;
      final traffic = stats?.trafficBytes;
      final balance = stats?.balanceCents;
      final lines = <String>{
        if (issue == null && detailText.isNotEmpty) detailText,
        if (issue != null) issue!.description,
        if (issue != null) issue!.handlingRecommendation,
        if (contextText.isNotEmpty && contextText != issue?.description)
          contextText,
      }.toList();
      return Column(
        key: const ValueKey('connection-information'),
        mainAxisSize: MainAxisSize.min,
        children: [
          for (var i = 0; i < lines.length; i++) ...[
            if (i > 0) const SizedBox(height: 8),
            Text(
              shortUiText(lines[i]),
              key: lines[i] == contextText
                  ? const ValueKey('home-context')
                  : null,
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 12,
                height: 1.45,
                fontWeight: FontWeight.w400,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (data?.statisticsSupported == true) ...[
            if (lines.isNotEmpty) const SizedBox(height: 8),
            _InformationLine(
              label: '历史计费流量',
              value: traffic == null
                  ? '暂无数据'
                  : '${(traffic / (1024 * 1024)).toStringAsFixed(2)} MiB',
            ),
            const SizedBox(height: 8),
            _InformationLine(
              label: '余额',
              value: balance == null
                  ? '暂无数据'
                  : '${balance ~/ 100}.${(balance % 100).toString().padLeft(2, '0')} 元',
            ),
          ],
        ],
      );
    },
  );
}

class _InformationLine extends StatelessWidget {
  const _InformationLine({required this.label, required this.value});
  final String label, value;
  @override
  Widget build(BuildContext context) {
    final style = TextStyle(
      fontSize: 12,
      height: 1.45,
      fontWeight: FontWeight.w400,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    );
    return Wrap(
      alignment: WrapAlignment.center,
      crossAxisAlignment: WrapCrossAlignment.center,
      spacing: 4,
      children: [
        Text('$label ·', style: style),
        Text(value.replaceAll(' ', '\u00a0'), style: style, softWrap: false),
      ],
    );
  }
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
