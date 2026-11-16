import 'package:sidravia_gui/shared/widgets/core_restart_dialog.dart';
import 'package:sidravia_gui/features/diagnostics/runtime_snapshot.dart';
import 'package:sidravia_gui/shared/theme/gui_preferences.dart';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/features/diagnostics/snapshot_code_view.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/application/gui_controller.dart';
import 'package:sidravia_gui/shared/widgets/design_widgets.dart';
import 'package:sidravia_gui/shared/widgets/feedback_pill.dart';

class ConnectionDiagnosticsPage extends StatefulWidget {
  const ConnectionDiagnosticsPage({
    super.key,
    required this.controller,
    required this.onBack,
    this.onOpenLogDirectory,
  });
  final GuiController controller;
  final VoidCallback onBack;
  final Future<bool> Function()? onOpenLogDirectory;
  @override
  State<ConnectionDiagnosticsPage> createState() =>
      _ConnectionDiagnosticsPageState();
}

class _ConnectionDiagnosticsPageState extends State<ConnectionDiagnosticsPage> {
  GuiController get controller => widget.controller;
  VoidCallback get onBack => widget.onBack;

  String? _captured;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _captured ??= _capture();
  }

  @override
  void didUpdateWidget(covariant ConnectionDiagnosticsPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.controller, widget.controller)) {
      _captured = _capture();
    }
  }

  String _capture() => captureRuntimeSnapshot(controller);

  Future<void> _copySnapshot() async {
    final text = _captured;
    if (text == null) return;
    try {
      await Clipboard.setData(ClipboardData(text: text));
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('快照已复制')));
      }
    } on Object {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('复制失败，请重试')));
      }
    }
  }

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

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([
      controller,
      controller.presentationClock,
      GuiPreferencesScope.maybeOf(context),
    ]),
    builder: (context, _) => _build(context),
  );

  Widget _build(BuildContext context) {
    final p = controller.connectionPresentation;
    final scheme = Theme.of(context).colorScheme;
    final linked = controller.state == GuiConnectionState.ready;
    return DesignPage(
      children: [
        DesignHeader(title: '连接与诊断', onBack: onBack),
        if (p.actionError != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: FeedbackPill(
              message: p.actionError!,
              onDismiss: () => controller.dismissNotice(p.actionError!),
            ),
          ),
        const DesignSectionTitle('运行状态快照'),
        if (!linked)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: Text(
              '后台未连接 · 上次获取的信息',
              style: TextStyle(fontSize: 12, color: scheme.onSurfaceVariant),
            ),
          ),
        Container(
          decoration: BoxDecoration(
            color: scheme.surfaceContainerLow,
            border: Border.all(color: scheme.outlineVariant),
            borderRadius: BorderRadius.circular(12),
          ),
          clipBehavior: Clip.antiAlias,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 4,
                ),
                decoration: BoxDecoration(
                  border: Border(
                    bottom: BorderSide(color: scheme.outlineVariant),
                  ),
                ),
                child: Row(
                  children: [
                    const Expanded(
                      child: Text('JSON', style: TextStyle(fontSize: 12)),
                    ),
                    IconButton(
                      tooltip: '刷新快照',
                      onPressed: () => setState(() => _captured = _capture()),
                      icon: const Icon(Icons.refresh_rounded, size: 19),
                    ),
                    IconButton(
                      tooltip: '复制快照',
                      onPressed: _copySnapshot,
                      icon: const Icon(Icons.copy_outlined, size: 18),
                    ),
                  ],
                ),
              ),
              SnapshotCodeView(text: _captured ?? ''),
            ],
          ),
        ),
        const SizedBox(height: 24),
        const DesignSectionTitle('高级排障'),
        DesignGroup(
          children: [
            DesignRow(
              title: _openingLogs ? '正在打开日志目录…' : '打开日志目录',
              subtitle: widget.onOpenLogDirectory == null
                  ? (kIsWeb ? '浏览器无法打开本机目录' : '当前环境无法打开本机目录')
                  : '查看当前实例日志',
              onTap: widget.onOpenLogDirectory == null || _openingLogs
                  ? null
                  : _openLogs,
            ),
            DesignRow(
              title: '重启核心',
              subtitle:
                  coreRestartUnavailableReason(controller) ?? '重启后恢复原连接状态',
              onTap: controller.canRestartCore
                  ? () => confirmCoreRestart(
                      context,
                      controller,
                      GuiPreferencesScope.maybeOf(context)?.logLevel ??
                          controller.runningLogLevel ??
                          'debug',
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
      builder: (context) => DesignConfirmationDialog(
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
