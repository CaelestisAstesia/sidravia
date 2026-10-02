import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

class AdvancedPage extends StatelessWidget {
  const AdvancedPage({
    super.key,
    required this.controller,
    required this.detailsOnly,
    required this.onBack,
    this.onDiagnostics,
  });
  final GuiController controller;
  final bool detailsOnly;
  final VoidCallback onBack;
  final VoidCallback? onDiagnostics;
  @override
  Widget build(BuildContext context) {
    final c = controller.capabilities.configuration;
    final s = controller.capabilities.retainedSession;
    final rows = <MapEntry<String, String>>[
      MapEntry('状态', _state(s)),
      if (s?.selectedNetworkBinding != null)
        MapEntry('网络适配器', s!.selectedNetworkBinding!.displayName)
      else
        const MapEntry('网络适配器', '暂不可用'),
      if (s?.selectedNetworkBinding?.localIpv4Address case final ip?
          when ip.isNotEmpty)
        MapEntry('本机 IPv4', ip)
      else
        const MapEntry('本机 IPv4', '暂不可用'),
      MapEntry(
        '认证建立',
        s?.authenticationEstablishedAt?.toLocal().toString() ?? '暂不可用',
      ),
    ];
    if (!detailsOnly) {
      rows.addAll([
        MapEntry('daemon', controller.snapshot?.daemon.status ?? '暂不可用'),
        MapEntry('session_id', s?.id ?? '暂不可用'),
        MapEntry('configuration_id', c?.id ?? '暂不可用'),
        MapEntry('auth_protocol', c?.authenticationProtocolId ?? '暂不可用'),
        MapEntry('last_error', s?.lastAuthenticationFailure?.code ?? '暂不可用'),
      ]);
    }
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(24, 16, 24, 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _BackHeader(title: detailsOnly ? '连接详情' : '技术诊断', onBack: onBack),
          Card(
            child: Column(
              children: [
                for (var i = 0; i < rows.length; i++)
                  _InfoRow(entry: rows[i], first: i == 0),
              ],
            ),
          ),
          if (detailsOnly && onDiagnostics != null)
            Padding(
              padding: const EdgeInsets.only(top: 24),
              child: OutlinedButton(
                onPressed: onDiagnostics,
                child: const Text('技术诊断'),
              ),
            ),
          if (!detailsOnly)
            Padding(
              padding: const EdgeInsets.only(top: 24),
              child: Row(
                children: [
                  Expanded(
                    child: OutlinedButton(
                      onPressed: null,
                      child: const Text('复制诊断信息（暂不可用）'),
                    ),
                  ),
                  const SizedBox(width: 9),
                  Expanded(
                    child: OutlinedButton(
                      onPressed: null,
                      child: const Text('打开日志目录（暂不可用）'),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  static String _state(SessionSummary? s) => switch (s?.state) {
    'authenticated' => '已连接',
    'authenticating' => '正在认证',
    'waiting_for_network' => '等待网络',
    'waiting_before_retry' => '等待重试',
    'blocked_by_error' => '认证失败',
    _ => '未连接',
  };
}

class _InfoRow extends StatelessWidget {
  const _InfoRow({required this.entry, required this.first});
  final MapEntry<String, String> entry;
  final bool first;
  @override
  Widget build(BuildContext context) => Container(
    decoration: BoxDecoration(
      border: first
          ? null
          : Border(top: BorderSide(color: Theme.of(context).dividerColor)),
    ),
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
    child: Row(
      children: [
        Expanded(
          child: Text(
            entry.key,
            style: const TextStyle(fontWeight: FontWeight.w600),
          ),
        ),
        Flexible(
          child: Text(
            entry.value,
            textAlign: TextAlign.right,
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      ],
    ),
  );
}

class _BackHeader extends StatelessWidget {
  const _BackHeader({required this.title, required this.onBack});
  final String title;
  final VoidCallback onBack;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 24),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextButton.icon(
          onPressed: onBack,
          style: TextButton.styleFrom(padding: EdgeInsets.zero),
          icon: const Icon(Icons.chevron_left),
          label: const Text('返回'),
        ),
        const SizedBox(height: 9),
        Text(
          title,
          style: Theme.of(context).textTheme.headlineSmall
              ?.copyWith(fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}
