import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/widgets/connection_issue_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_network_diagnosis.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';

class AdvancedPage extends StatefulWidget {
  const AdvancedPage({
    super.key,
    required this.controller,
    required this.onBack,
    this.onOpenLogDirectory,
  });
  final GuiController controller;
  final VoidCallback onBack;
  final Future<bool> Function()? onOpenLogDirectory;
  @override
  State<AdvancedPage> createState() => _AdvancedPageState();
}

class _AdvancedPageState extends State<AdvancedPage> {
  GuiController get controller => widget.controller;
  VoidCallback get onBack => widget.onBack;

  bool _expanded = false;
  bool _openingLogs = false;

  Future<void> _openLogs() async {
    final open = widget.onOpenLogDirectory;
    if (open == null || _openingLogs) return;
    setState(() => _openingLogs = true);
    var success = false;
    try {
      success = await open();
    } on Object {
      /* fixed safe feedback below */
    }
    if (!mounted) return;
    setState(() => _openingLogs = false);
    if (!success) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('打开日志目录失败，请确认日志目录存在后重试。')));
    }
  }

  void _showDiagnosis() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) controller.setDiagnosisVisible(true);
    });
  }

  @override
  void initState() {
    super.initState();
    _showDiagnosis();
  }

  @override
  void didUpdateWidget(AdvancedPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != controller) {
      oldWidget.controller.setDiagnosisVisible(false);
      _showDiagnosis();
    }
  }

  @override
  void dispose() {
    controller.setDiagnosisVisible(false);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) => _build(context),
  );

  Widget _build(BuildContext context) {
    final p = controller.connectionPresentation;
    final raw = controller.snapshot;
    final c = raw?.configurations.length == 1
        ? raw!.configurations.single
        : null;
    final s = raw?.sessions.length == 1 ? raw!.sessions.single : null;
    final d = controller.snapshot?.daemon;
    final rows = <MapEntry<String, String>>[
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
      MapEntry('session_count', raw?.sessions.length.toString() ?? '暂不可用'),
      MapEntry('session_state', s?.state.wireValue ?? '暂不可用'),
      MapEntry('session_id', s?.id ?? '暂不可用'),
      MapEntry('configuration_id', c?.id ?? '暂不可用'),
      if (p.issue != null)
        MapEntry('last_error', s!.lastAuthenticationFailure!.code),
    ];
    final appearance = AppearanceScope.maybeOf(context);
    final expanded = appearance?.diagnosticsExpanded ?? _expanded;
    final remote = s?.protocolSocket.remoteEndpoint;
    return DesignPage(
      children: [
        DesignHeader(title: '连接与诊断', onBack: onBack),
        if (p.issue != null) ...[
          ConnectionIssueCard(failure: p.issue!),
          const SizedBox(height: 14),
        ],
        if (p.actionError != null) DesignHelper(p.actionError!),
        DesignGroup(
          children: [
            DesignRow(title: '状态', value: p.statusTitle),
            DesignRow(
              title: '网络适配器',
              value: p.networkBinding?.displayName ?? '尚无会话绑定观察',
            ),
            DesignRow(
              title: '本机 IPv4',
              value: p.networkBinding?.localIpv4Address ?? '暂不可用',
            ),
            DesignRow(
              title: '实际远端端点',
              value: remote == null
                  ? '尚无协议 socket 观察'
                  : '${remote.address}:${remote.port}',
            ),
            DesignRow(
              title: '认证建立',
              value: p.establishedAt?.toLocal().toString() ?? '暂不可用',
            ),
            if (p.establishedAt != null)
              DesignRow(title: '连接时长', value: p.statusDetail),
          ],
        ),
        const SizedBox(height: 18),
        DesignGroup(
          children: [
            DesignRow(
              title: '技术信息',
              trailing: Icon(expanded ? Icons.expand_less : Icons.expand_more),
              onTap: () {
                if (appearance != null) {
                  appearance.diagnosticsExpanded = !expanded;
                } else {
                  setState(() => _expanded = !expanded);
                }
              },
            ),
          ],
        ),
        if (appearance?.feedback != null) DesignHelper(appearance!.feedback!),
        if (expanded) ...[
          const SizedBox(height: 14),
          DesignGroup(
            children: [
              DesignRow(
                title: '网络诊断',
                value: controller.diagnosisBusy
                    ? '正在观察…'
                    : controller.diagnosisStale
                    ? '观察已过期，请刷新'
                    : '最近观察',
              ),
              if (controller.diagnosisUnavailable != null)
                DesignRow(
                  title: '暂不可用',
                  value: controller.diagnosisUnavailable,
                ),
              if (controller.diagnosisError != null)
                DesignRow(title: '诊断错误', value: controller.diagnosisError),
              if (controller.diagnosisTargetLabel != null)
                DesignRow(
                  title: controller.diagnosisStale ? '上次诊断对象（已过期）' : '诊断对象',
                  value: controller.diagnosisTargetLabel,
                ),
              if (controller.diagnosis != null)
                for (final row in diagnosisRows(controller.diagnosis!))
                  DesignRow(title: row.key, value: row.value, diagnostic: true),
            ],
          ),
          const SizedBox(height: 14),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: controller.canDiagnose
                      ? () => controller.refreshDiagnosis()
                      : null,
                  child: const Text('刷新网络诊断'),
                ),
              ),
              const SizedBox(width: 9),
              Expanded(
                child: OutlinedButton(
                  onPressed: controller.canDiagnose
                      ? () => controller.refreshDiagnosis(probe: true)
                      : null,
                  child: const Text('执行有限 IP 探测'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 7),
          const DesignHelper(
            '刷新只读路由；探测仅发送一次有界 IP 回显。未收到回复不代表断网；收到回显也不证明认证成功或 Internet 可用。路由、会话与 socket 是不同时间的观察。',
          ),
          const SizedBox(height: 16),
          DesignGroup(
            children: [
              for (final row in rows)
                DesignRow(title: row.key, value: row.value, diagnostic: true),
            ],
          ),
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
              Expanded(
                child: OutlinedButton(
                  onPressed: widget.onOpenLogDirectory == null || _openingLogs
                      ? null
                      : _openLogs,
                  child: Text(
                    _openingLogs
                        ? '正在打开日志目录…'
                        : widget.onOpenLogDirectory == null
                        ? '打开日志目录（暂不可用）'
                        : '打开日志目录',
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 7),
          const DesignHelper('查看本实例的运行日志，便于排查连接问题。'),
        ],
        const SizedBox(height: 18),
        const DesignSectionTitle('高级排障'),
        DesignGroup(
          children: [
            DesignRow(
              title: '移除当前会话',
              subtitle: controller.capabilities.canResetSession
                  ? '清理当前 Session 记录'
                  : '没有可清理的会话或当前不可操作',
              onTap: controller.capabilities.canResetSession
                  ? _confirmReset
                  : null,
            ),
          ],
        ),
      ],
    );
  }

  Future<void> _confirmReset() async {
    final session = controller.capabilities.retainedSession;
    if (session == null || !controller.capabilities.canResetSession) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除当前会话？'),
        content: const Text('此操作会清理当前 Session 记录。'),
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
    if (confirmed == true && mounted) await controller.resetSession(session.id);
  }
}
