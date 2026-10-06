import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

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
    final p = controller.connectionPresentation;
    final raw = controller.snapshot;
    final c = raw?.configurations.length == 1
        ? raw!.configurations.single
        : null;
    final s = raw?.sessions.length == 1 ? raw!.sessions.single : null;
    final d = controller.snapshot?.daemon;
    final rows = detailsOnly
        ? <MapEntry<String, String>>[
            MapEntry('状态', p.statusTitle),
            MapEntry('认证协议', p.protocol ?? '暂不可用'),
            if (p.networkBinding != null) ...[
              MapEntry('网络适配器', p.networkBinding!.displayName),
              MapEntry('本机 IPv4', p.networkBinding!.localIpv4Address),
            ],
            if (p.establishedAt != null)
              MapEntry('认证建立', p.establishedAt!.toLocal().toString()),
          ]
        : <MapEntry<String, String>>[
            MapEntry('daemon', d?.status ?? '暂不可用'),
            MapEntry('ipc_state', controller.state.name),
            if (controller.failure != null) ...[
              MapEntry('ipc_error', controller.failure!.code),
              MapEntry('ipc_message', controller.failure!.message),
            ],
            MapEntry('version', d?.productVersion ?? '暂不可用'),
            MapEntry('build_id', d?.buildId ?? '暂不可用'),
            MapEntry('mode', d?.mode ?? '暂不可用'),
            MapEntry(
              'configuration_count',
              raw?.configurations.length.toString() ?? '暂不可用',
            ),
            MapEntry(
              'session_count',
              raw?.sessions.length.toString() ?? '暂不可用',
            ),
            MapEntry('session_state', s?.state.wireValue ?? '暂不可用'),
            MapEntry('session_id', s?.id ?? '暂不可用'),
            MapEntry('configuration_id', c?.id ?? '暂不可用'),
            if (s?.lastAuthenticationFailure != null)
              MapEntry('last_error', s!.lastAuthenticationFailure!.code),
          ];
    return DesignPage(
      children: [
        DesignHeader(
          title: detailsOnly ? '连接详情' : '诊断',
          onBack: onBack,
          bottomSpacing: detailsOnly ? 24 : 14,
        ),
        if (!detailsOnly) ...[
          const DesignHelper(
            '用于排障和 issue reporting。连续日志与底层控制仍由 sidraviactl / 日志系统承担。',
          ),
          const SizedBox(height: 16),
        ],
        DesignGroup(
          children: [
            for (final row in rows)
              DesignRow(
                title: row.key,
                value: row.value,
                diagnostic: !detailsOnly,
              ),
          ],
        ),
        if (detailsOnly) ...[
          const SizedBox(height: 7),
          const DesignHelper('这里只显示理解当前连接所需的信息；更底层的数据放在诊断。'),
          const SizedBox(height: 24),
          DesignGroup(
            children: [
              DesignRow(
                title: '诊断',
                subtitle: '查看 daemon、session、错误代码等工程信息',
                onTap: onDiagnostics,
              ),
            ],
          ),
        ] else ...[
          const SizedBox(height: 14),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: d == null && controller.failure == null
                      ? null
                      : () async {
                          try {
                            await Clipboard.setData(
                              ClipboardData(
                                text: rows
                                    .map((r) => '${r.key}: ${r.value}')
                                    .join('\n'),
                              ),
                            );
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(content: Text('诊断信息已复制。')),
                              );
                            }
                          } on Object {
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(content: Text('复制失败，请重试。')),
                              );
                            }
                          }
                        },
                  child: Text(
                    d == null && controller.failure == null
                        ? '复制诊断信息（暂不可用）'
                        : '复制诊断信息',
                  ),
                ),
              ),
              const SizedBox(width: 9),
              const Expanded(
                child: OutlinedButton(
                  onPressed: null,
                  child: Text('打开日志目录（暂不可用）'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 7),
          const DesignHelper(
            '更深层操作：sidraviactl status、日志查询、原始 IPC / Session 管理。日志目录缺少现有平台封装。',
          ),
        ],
      ],
    );
  }
}
