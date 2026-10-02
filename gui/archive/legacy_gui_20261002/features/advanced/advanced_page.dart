import 'package:flutter/material.dart';

import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/design/sidravia_layout.dart';

class AdvancedPage extends StatelessWidget {
  const AdvancedPage({super.key, required this.controller});

  final GuiController controller;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = SidraviaLayout.isCompactWidth(constraints.maxWidth);
        return AnimatedBuilder(
          animation: controller,
          builder: (context, _) => ListView(
            padding: SidraviaLayout.pagePadding(compact: compact),
            children: [
              Text(
                '高级',
                style: compact
                    ? Theme.of(context).textTheme.headlineSmall
                    : Theme.of(context).textTheme.displaySmall,
              ),
              const SizedBox(height: 6),
              Text(
                '查看连接链路和本机服务的详细信息。',
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 22),
              _DetailsSurface(controller: controller, compact: compact),
            ],
          ),
        );
      },
    );
  }
}

class _DetailsSurface extends StatelessWidget {
  const _DetailsSurface({required this.controller, required this.compact});

  final GuiController controller;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final snapshot = controller.snapshot;
    final session = controller.capabilities.retainedSession;
    final configuration = controller.capabilities.configuration;
    if (snapshot == null) {
      return Card(
        child: Padding(
          padding: EdgeInsets.all(SidraviaLayout.cardPadding(compact: compact)),
          child: Text(_stateMessage(controller.state)),
        ),
      );
    }
    return Card(
      child: Padding(
        padding: EdgeInsets.all(SidraviaLayout.cardPadding(compact: compact)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _SectionLabel(icon: Icons.device_hub_outlined, title: '连接链路'),
            const SizedBox(height: 12),
            _DetailRow(
              label: '认证协议',
              value: configuration?.authenticationProtocolId,
            ),
            _DetailRow(label: '连接意图', value: session?.intent),
            _DetailRow(
              label: '网络接口',
              value: session?.selectedNetworkBinding?.displayName,
            ),
            _DetailRow(
              label: '接口标识',
              value: session?.selectedNetworkBinding?.interfaceId,
            ),
            _DetailRow(
              label: '本机 IPv4',
              value: session?.selectedNetworkBinding?.localIpv4Address,
            ),
            const SizedBox(height: 24),
            _SectionLabel(icon: Icons.schedule_outlined, title: '认证时间'),
            const SizedBox(height: 12),
            _DetailRow(
              label: '认证建立时间',
              value: _formatDateTime(session?.authenticationEstablishedAt),
            ),
            _DetailRow(
              label: '下次重试时间',
              value: _formatDateTime(session?.nextRetryAt),
            ),
            _DetailRow(
              label: '状态更新时间',
              value: _formatDateTime(session?.updatedAt),
            ),
            const SizedBox(height: 24),
            _SectionLabel(icon: Icons.memory_outlined, title: '本机服务'),
            const SizedBox(height: 12),
            _DetailRow(label: '服务状态', value: snapshot.daemon.status),
            _DetailRow(label: '运行模式', value: snapshot.daemon.mode),
            _DetailRow(label: '版本', value: snapshot.daemon.productVersion),
            _DetailRow(label: '构建标识', value: snapshot.daemon.buildId),
            if (session?.lastAuthenticationFailure case final failure?) ...[
              const SizedBox(height: 24),
              _SectionLabel(icon: Icons.error_outline, title: '最近一次认证失败'),
              const SizedBox(height: 12),
              _DetailRow(label: '错误代码', value: failure.code),
              _DetailRow(label: '说明', value: failure.description),
              _DetailRow(label: '建议', value: failure.handlingRecommendation),
            ],
          ],
        ),
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  const _SectionLabel({required this.icon, required this.title});
  final IconData icon;
  final String title;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Icon(icon, size: 19, color: Theme.of(context).colorScheme.primary),
      const SizedBox(width: 8),
      Text(
        title,
        style: Theme.of(context).textTheme.titleMedium
            ?.copyWith(fontWeight: FontWeight.w700),
      ),
    ],
  );
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.value});
  final String label;
  final String? value;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 7),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 112,
          child: Text(
            label,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Text(
            value == null || value!.isEmpty ? '暂无数据' : value!,
            textAlign: TextAlign.end,
            style: Theme.of(context).textTheme.bodyMedium,
          ),
        ),
      ],
    ),
  );
}

String _stateMessage(GuiConnectionState state) => switch (state) {
  GuiConnectionState.bootstrapping => '正在连接本机服务…',
  GuiConnectionState.stale => '连接状态已过期，请返回仪表板重试。',
  GuiConnectionState.failed => '本机服务不可用。',
  GuiConnectionState.unsupported => '当前平台暂不支持。',
  GuiConnectionState.ready => '暂无连接详情。',
};

String? _formatDateTime(DateTime? value) {
  if (value == null) return null;
  final local = value.toLocal();
  final date =
      '${local.year}-${local.month.toString().padLeft(2, '0')}-${local.day.toString().padLeft(2, '0')}';
  final time =
      '${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}';
  return '$date $time';
}
