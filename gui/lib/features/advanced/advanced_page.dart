import 'package:sidravia_gui/shared/widgets/core_restart_dialog.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:sidravia_gui/shared/widgets/connection_issue_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/application/gui_network_diagnosis.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';

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
      ScaffoldMessenger.of(context)
          .showSnackBar(const SnackBar(content: Text('打开日志目录失败，请确认日志目录存在后重试')));
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
    animation: Listenable.merge([
      controller,
      GuiPreferencesScope.maybeOf(context),
    ]),
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
      MapEntry('核心服务状态', d?.status ?? '暂无数据'),
      MapEntry('通信状态', controller.state.name),
      if (controller.failure != null) ...[
        MapEntry('通信错误', controller.failure!.code),
        MapEntry('错误说明', controller.failure!.message),
      ],
      MapEntry('版本', d?.productVersion ?? '暂无数据'),
      MapEntry('构建标识', d?.buildId ?? '暂无数据'),
      MapEntry('运行模式', d?.mode ?? '暂无数据'),
      MapEntry('配置数量', raw?.configurations.length.toString() ?? '暂无数据'),
      MapEntry('会话数量', raw?.sessions.length.toString() ?? '暂无数据'),
      MapEntry('会话状态', s?.state.wireValue ?? '暂无数据'),
      MapEntry('会话标识', s?.id ?? '暂无数据'),
      MapEntry('配置标识', c?.id ?? '暂无数据'),
      if (p.issue != null) MapEntry('最近错误', s!.lastAuthenticationFailure!.code),
    ];
    final appearance = GuiPreferencesScope.maybeOf(context);
    final expanded = appearance?.diagnosticsExpanded ?? _expanded;
    final remote = s?.protocolSocket.remoteEndpoint;
    return DesignPage(
      children: [
        DesignHeader(title: '连接与诊断', onBack: onBack),
        if (p.issue != null) ...[
          ConnectionIssueCard(failure: p.issue!),
          const SizedBox(height: 14),
        ],
        if (p.actionError != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: FeedbackPill(message: p.actionError!),
          ),
        DesignGroup(
          children: [
            DesignRow(title: '状态', value: p.statusTitle),
            DesignRow(
              title: '网卡',
              value: p.networkBinding?.displayName ?? '暂无数据',
            ),
            DesignRow(
              title: 'IP',
              value: p.networkBinding?.localIpv4Address ?? '暂无数据',
            ),
            DesignRow(
              title: '认证服务器',
              value: remote == null
                  ? '暂无数据'
                  : '${remote.address}:${remote.port}',
            ),
            DesignRow(
              title: '连接建立时间',
              value: p.establishedAt?.toLocal().toString() ?? '暂无数据',
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
        if (appearance?.feedback != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: FeedbackPill(message: appearance!.feedback!),
          ),
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
                  title: '暂无数据',
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
                                const SnackBar(content: Text('诊断信息已复制')),
                              );
                            }
                          } on Object {
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(content: Text('复制失败，请重试')),
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
        ],
        const SizedBox(height: 18),
        const DesignSectionTitle('高级排障'),
        DesignGroup(
          children: [
            DesignRow(
              title: '重启核心',
              subtitle: '重启后恢复原连接状态',
              onTap: controller.canRestartCore
                  ? () => confirmCoreRestart(
                      context,
                      controller,
                      GuiPreferencesScope.maybeOf(context)?.logLevel ??
                          controller.runningLogLevel ??
                          'info',
                    )
                  : null,
            ),
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
